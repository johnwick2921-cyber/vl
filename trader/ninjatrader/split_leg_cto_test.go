package ninjatrader

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── REVIEW-SPLIT-2 Go folds (CTO 2026-10-04), production call sites ────────

// P2: leg1_tp gets the wire treatment of the trade target (nearest tick) and
// the split actually sent is recorded for the exit drive. Mutant: skip
// wireLeg1TP → the raw 102.13 goes out → RED; skip the record → RED.
func TestSplitLeg1TPRoundedOnTheWireAndRecorded(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "split-pin"
	sid, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102.13)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty != 3 || p.Leg1TP != 102.25 {
		t.Fatalf("frame leg1 = (%d, %.2f), want (3, 102.25) — leg1_tp to the nearest tick", p.Leg1Qty, p.Leg1TP)
	}
	got, ok := tr.SplitSentFor(sid)
	if !ok || got.Leg1Qty != 3 || got.Leg1TP != 102.25 {
		t.Fatalf("SplitSentFor = (%+v, %v), want the split the frame carried", got, ok)
	}
}

// P2: a leg-1 TP on the loss side of the entry drops the split — the single
// bracket goes out, and nothing is recorded (the drive must not address a
// leg 2 the AddOn never built). Mutant: no side check → leg1_qty on the wire → RED.
func TestSplitLeg1TPWrongSideSendsTheSingleBracket(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "split-pin-short"
	sid, err := tr.PlaceStopEntry("MNQ", "short", 5, 100, 102, 94, 3, 101)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg1_qty")) || bytes.Contains(frame, []byte("leg1_tp")) {
		t.Fatalf("a short leg-1 TP above the entry must drop the split, got %s", frame)
	}
	if _, ok := tr.SplitSentFor(sid); ok {
		t.Fatal("a dropped split must not be recorded")
	}
}

// The per-leg stop move: the frame names the leg, and the widen ban is judged
// per LEG (both legs share the signal id). Mutant: key stopBySignal by signal
// only → leg 2's independent 29590 reads as a widen of leg 1's 29600 → RED.
func TestMoveStopForSignalLegNamesTheLegAndGuardsPerLeg(t *testing.T) {
	s, st, conn, moves := moveStopServer(t)
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildSplitLegs}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.FarSideBuildID() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.StartCloseSync("split-leg-trader", "fixture", "ninjatrader", st)

	if err := tr.MoveStopForSignalLeg("sig", "long", 29600, 1); err != nil {
		t.Fatalf("leg 1 move: %v", err)
	}
	if err := tr.MoveStopForSignalLeg("sig", "long", 29590, 2); err != nil {
		t.Fatalf("leg 2's own move judged against leg 1's stop: %v", err)
	}
	if err := tr.MoveStopForSignalLeg("sig", "long", 29580, 2); err == nil {
		t.Fatal("leg 2 widen (29590 → 29580) was NOT refused")
	}
	if err := tr.MoveStopForSignalLeg("sig", "long", 29700, 3); err == nil {
		t.Fatal("leg 3 does not exist — must be refused")
	}
	var got []ntwire.MoveStopPayload
	for i := 0; i < 2; i++ {
		select {
		case p := <-moves:
			got = append(got, p)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for move_stop %d/2", i+1)
		}
	}
	if got[0].Leg != 1 || got[0].NewStopLoss != 29600 || got[1].Leg != 2 || got[1].NewStopLoss != 29590 {
		t.Fatalf("frames = %+v, want leg 1 @29600 then leg 2 @29590", got)
	}
}

func splitRow(t *testing.T, st *store.Store, qty float64) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	row := &store.TraderPosition{
		TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: "p-split",
		Symbol: "MNQ", Side: "LONG", Quantity: qty, EntryQuantity: qty,
		EntryPrice: 29350, EntryTime: now - 60_000, EntryOrderID: "sig-split",
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101",
		CreatedAt: now - 60_000, UpdatedAt: now - 60_000,
	}
	if err := st.Position().CreateOpenPosition(row); err != nil {
		t.Fatal(err)
	}
}

// P1-2: both legs' stops fire on one tick — same signal, reason, qty and
// second. The leg keeps the two receipts distinct, so both apply and the row
// closes. Mutant: drop the leg from the identity → the second dedupes, the row
// stays OPEN with 2 lots → RED.
func TestSplitTwoSameSecondStopExitsBothApply(t *testing.T) {
	tr, st := newExitParkFixture(t)
	splitRow(t, st, 4)
	at := time.Now().UTC().Format(time.RFC3339)
	for _, leg := range []int{1, 2} {
		tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, ntwire.PositionClosePayload{
			SignalID: "sig-split", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
			Quantity: 2, ExitPrice: 29345, ExitReason: "sl", ExitTime: at, Leg: leg,
		})
	}
	if open, _ := st.Position().GetOpenPositionBySymbol("t1", "MNQ", "LONG"); open != nil {
		t.Fatalf("both legs' stop exits must apply and close the row, still OPEN at %.0f lots", open.Quantity)
	}
}

// The single bracket's receipt identity is unchanged: the SAME frame replayed
// still dedupes (the leg joins the identity only when set). Mutant: insert
// the leg for every frame → no change here, but the pre-upgrade receipts'
// keys move — pinned by the exact key below.
func TestSingleBracketReceiptKeyUnchanged(t *testing.T) {
	tr, st := newExitParkFixture(t)
	splitRow(t, st, 4)
	frame := ntwire.PositionClosePayload{
		SignalID: "sig-split", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 2, ExitPrice: 29345, ExitReason: "sl", ExitTime: "2026-10-05T01:00:00Z",
	}
	tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, frame)
	tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, frame)
	open, _ := st.Position().GetOpenPositionBySymbol("t1", "MNQ", "LONG")
	if open == nil || open.Quantity != 2 {
		t.Fatalf("a replayed single-bracket frame must dedupe: row %+v", open)
	}
	var keys []string
	if err := st.GormDB().Table("nt8_exit_receipts").Pluck("id", &keys).Error; err != nil {
		t.Fatalf("read receipt keys: %v", err)
	}
	// The pre-split identity, computed the pre-split way.
	ms, _ := time.Parse(time.RFC3339, frame.ExitTime)
	want := receiptKeyForTest([]any{"Sim101", "MNQ", "LONG", "sig-split", "sl", uint64(0), 2.0, ms.UTC().UnixMilli()})
	if len(keys) != 1 || keys[0] != want {
		t.Fatalf("single-bracket receipt key = %v, want the pre-split key %s", keys, want)
	}
}

// receiptKeyForTest is the pre-split receipt key formula, written out here so
// a change to the production formula cannot move both sides at once.
func receiptKeyForTest(parts []any) string {
	b, _ := json.Marshal(parts)
	return fmt.Sprintf("nt8-exit-v2-%x", sha256.Sum256(b))
}
