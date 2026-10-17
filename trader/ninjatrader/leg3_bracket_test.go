package ninjatrader

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── release #31 (leg-3 bracket) Go wire pins, production call sites ─────────

// TestLeg3FiveContractSplit — 5 contracts split 3/1/1: leg1=3 @ +1R, leg2=1 at
// the first line beyond the leg-1 exit, leg3=1 trails to the trade target. The
// frame carries leg2_qty/leg2_tp and the sent split records them. Mutant: drop
// leg2 from the frame (or the record) → RED.
func TestLeg3FiveContractSplit(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-pin"
	// entry 100, stop 98 (R=2) → leg1=3 @ +1R=102; leg2=1 @ 104 (a line strictly
	// between 102 and the target 106); leg3=1 @ 106.
	sid, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 104)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty != 3 || p.Leg1TP != 102 {
		t.Fatalf("frame leg1 = (%d, %.2f), want (3, 102)", p.Leg1Qty, p.Leg1TP)
	}
	if p.Leg2Qty != 1 || p.Leg2TP != 104 {
		t.Fatalf("frame leg2 = (%d, %.2f), want (1, 104)", p.Leg2Qty, p.Leg2TP)
	}
	got, ok := tr.SplitSentFor(sid)
	if !ok || got.Leg1Qty != 3 || got.Leg1TP != 102 || got.Leg2Qty != 1 || got.Leg2TP != 104 {
		t.Fatalf("SplitSentFor = (%+v, %v), want the split the frame carried", got, ok)
	}
}

// TestLeg3OldAddonBuildDropsToTwoPairPlan — an AddOn that proves the 2-pair
// build but not the leg-3 build gets leg2 zeroed (the 2-pair plan), never a
// frame it would misread. Mutant: drop the MinAddonBuildLeg3 gate → leg2 on the
// wire → RED.
func TestLeg3OldAddonBuildDropsToTwoPairPlan(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-old"
	if _, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 104); err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg2_qty")) || bytes.Contains(frame, []byte("leg2_tp")) {
		t.Fatalf("an AddOn below the leg-3 build must get the 2-pair plan, got %s", frame)
	}
}

// TestLeg3TPOutOfRangeDropsLeg2 — a leg-2 TP not strictly between leg 1's TP and
// the trade target drops ONLY leg 2 (the 2-pair plan stands). Mutant: no range
// check → leg2 on the wire → RED.
func TestLeg3TPOutOfRangeDropsLeg2(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-range"
	if _, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 107); err != nil { // 107 >= target 106
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg2_qty")) || bytes.Contains(frame, []byte("leg2_tp")) {
		t.Fatalf("a leg-2 TP at/past the trade target must drop leg 2, got %s", frame)
	}
}

// TestLeg3ShortSideRoundsAndSends — the short side: entry 100, stop 102 (R=2),
// target 94 → leg1=3 @ 98, leg2=1 @ 96 (a line strictly between 98 and 94),
// leg3=1 @ 94. Mirrors the long pin so the side math is not one-sided.
func TestLeg3ShortSideRoundsAndSends(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-short"
	if _, err := tr.PlaceStopEntry("MNQ", "short", 5, 100, 102, 94, 3, 98, 1, 96); err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty != 3 || p.Leg1TP != 98 || p.Leg2Qty != 1 || p.Leg2TP != 96 {
		t.Fatalf("frame = leg1(%d, %.2f) leg2(%d, %.2f), want leg1(3, 98) leg2(1, 96)", p.Leg1Qty, p.Leg1TP, p.Leg2Qty, p.Leg2TP)
	}
}

// leg3WireServer is a loopback fixture whose reader keeps modify_bracket and
// move_stop frames (rawSignalServer's reader discards every non-signal frame).
func leg3WireServer(t *testing.T, buildID string) (*ntwire.TCPServer, chan ntwire.ModifyBracketPayload, chan ntwire.MoveStopPayload) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "l3.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel(); _ = st.Close() })

	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mods := make(chan ntwire.ModifyBracketPayload, 4)
	moves := make(chan ntwire.MoveStopPayload, 4)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch env.Type {
			case ntwire.FrameModifyBracket:
				var p ntwire.ModifyBracketPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					mods <- p
				}
			case ntwire.FrameMoveStop:
				var p ntwire.MoveStopPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					moves <- p
				}
			}
		}
	}()
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: buildID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && !ntwire.FarSideProven(s.FarSideBuildID(), buildID); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !ntwire.FarSideProven(s.FarSideBuildID(), buildID) {
		t.Fatal("fixture: far-side build never proven")
	}
	return s, mods, moves
}

// TestLeg3ModifyAndMoveNameWireLeg3 — release #31's runner is wire leg 3:
// ModifyBracketLeg(leg=3) and MoveStopForSignalLeg(leg=3) must name leg 3 on
// the wire (the AddOn's HandleModifyBracket / HandleMoveStop honour 3). Mutant:
// the leg guard still refusing 3 → the frame never leaves → RED.
func TestLeg3ModifyAndMoveNameWireLeg3(t *testing.T) {
	s, mods, moves := leg3WireServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-mod"

	if err := tr.ModifyBracketLeg("sig-leg3", "long", 3, 0, 105.25); err != nil {
		t.Fatalf("modify leg 3: %v", err)
	}
	if err := tr.MoveStopForSignalLeg("sig-leg3", "long", 99, 3); err != nil {
		t.Fatalf("move leg 3: %v", err)
	}

	var mod ntwire.ModifyBracketPayload
	select {
	case mod = <-mods:
	case <-time.After(2 * time.Second):
		t.Fatal("no modify_bracket frame reached the wire")
	}
	if mod.Leg != 3 {
		t.Fatalf("modify_bracket leg = %d, want 3", mod.Leg)
	}
	if mod.NewTakeProfit != 105.25 {
		t.Fatalf("modify_bracket new_take_profit = %.2f, want 105.25", mod.NewTakeProfit)
	}

	var move ntwire.MoveStopPayload
	select {
	case move = <-moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move_stop frame reached the wire")
	}
	if move.Leg != 3 {
		t.Fatalf("move_stop leg = %d, want 3", move.Leg)
	}
	if move.NewStopLoss != 99 {
		t.Fatalf("move_stop new_stop_loss = %.2f, want 99", move.NewStopLoss)
	}
}
