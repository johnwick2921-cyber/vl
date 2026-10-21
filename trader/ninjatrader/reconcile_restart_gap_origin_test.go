package ninjatrader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── RT-11 (risk-truth wave, finding N11) — a bot arm that FILLED during a
// restart gap is not adopted as MANUAL ────────────────────────────────────────
//
// The untracked branch of reconcilePositions flags an adopted NT8 position
// origin=manual when "nothing evidenced this trader's own entry". The evidence
// it read was the in-memory fill ring and FILLED arms in the ring's window —
// and a restart takes both away from a resting arm that fills while the
// process is down:
//
//   - the ring is a field of the TCPTrader; a new process starts it empty, and
//     the AddOn does not replay fill frames on (re)connect;
//   - the arm never receives its "filled" order_update, so the ledger still
//     reads working (or cancel_pending); the post-restart order_snapshot lists
//     only the bracket children, because a Filled entry is omitted from the
//     snapshot (VLTraderTCPClient.cs SendOrderSnapshot, D3), and the class-33
//     boot sweep REFUSES to cancel a signal whose children are resting
//     (arm_cancel_safety.go) — so nothing ever moves the row to filled.
//
// Flagged manual, the bot's own position would leave the bot record and the
// bot-only risk reads — the harmful direction. These tests replay that restart
// at the production call site (reconcilePositions, twice: first sighting, then
// past the untracked grace) on a FRESH TCPTrader, and pin the other side too:
// the R1 shape (a 20-lot with no bot order/arm behind it) is still manual.

// restingArm writes a bot arm exactly as the armed pass leaves one resting at
// the broker — authorized (UpsertArm), signal registered before the socket
// write (BeginPlacement), receipt applied (ConfirmPlacement → working) — then
// stamps it as authored by the PREVIOUS process and last touched before the
// crash, as the ledger reads after a restart.
func restingArm(t *testing.T, w *lateFillWire, scenario, side, sid string, px float64) *store.ArmedOrderDB {
	t.Helper()
	stop, target := px+20, px-50
	if strings.EqualFold(side, "long") {
		stop, target = px-20, px+50
	}
	r := &store.ArmedOrderDB{TraderID: lateFillTrader, PlanID: "2026-10-14:NY", Scenario: scenario, Version: 2,
		State: store.StateArmed, Side: side, EntryPx: px, StopPx: stop, TargetPx: target}
	if err := w.st.ArmedOrders().UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	if sid == "" {
		return r // authorized, never placed — nothing exists at the broker
	}
	if err := w.st.ArmedOrders().BeginPlacement(r.ID, sid); err != nil {
		t.Fatal(err)
	}
	if err := w.st.ArmedOrders().ConfirmPlacement(r.ID, "order_update working"); err != nil {
		t.Fatal(err)
	}
	if err := w.st.ArmedOrders().DB().Model(&store.ArmedOrderDB{}).Where("id = ?", r.ID).
		UpdateColumns(map[string]any{"boot_id": "4242-1791990000000", "updated_at": time.Now().Add(-20 * time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	return r
}

// postRestartBook seeds the order_snapshot NT8 sends on (re)connect: the arm's
// bracket children rest, the filled entry is absent (D3).
func postRestartBook(w *lateFillWire, sid string, entry float64) {
	w.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", BuildID: "test", EmittedMs: time.Now().UnixMilli(), Reason: "connect",
		Orders: []ntwire.NT8Order{
			{OrderID: "o-sl", Symbol: "MNQ", Name: sid + "-sl", Action: "buy", Type: "stop", StopPrice: entry + 20, Quantity: 1, State: "Accepted"},
			{OrderID: "o-tp", Symbol: "MNQ", Name: sid + "-tp", Action: "buy", Type: "limit", LimitPrice: entry - 50, Quantity: 1, State: "Working"},
		}}, time.Now())
}

// adoptAfterRestart seeds NT8's position and runs the two production reconcile
// passes on the fixture's FRESH trader (sighting, then past the grace).
func adoptAfterRestart(t *testing.T, w *lateFillWire, side string, qty int, px float64) *store.TraderPosition {
	t.Helper()
	w.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: strings.ToLower(side), Quantity: qty, AvgPrice: px}})
	w.tr.reconcilePositions(lateFillTrader, lateFillExchange, "ninjatrader", w.st)
	w.tr.untrackedSince["MNQ|"+strings.ToUpper(side)] = time.Now().UTC().UnixMilli() - untrackedGraceMs - 1
	w.tr.reconcilePositions(lateFillTrader, lateFillExchange, "ninjatrader", w.st)
	rows, err := w.st.Position().GetOpenPositions(lateFillTrader)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected one materialized row, got %d (%v)", len(rows), err)
	}
	return rows[0]
}

func TestRestartGapArmFillIsNotAdoptedAsManual(t *testing.T) {
	const sid, px = "sig-gap-s1", 29400.0
	for _, tc := range []struct {
		name      string
		state     string
		atRestart func(t *testing.T, w *lateFillWire, arm *store.ArmedOrderDB)
	}{
		{name: "arm still working at restart", state: store.StateWorking, atRestart: func(*testing.T, *lateFillWire, *store.ArmedOrderDB) {}},
		{
			// The dead process had asked for a cancel; the fill raced it. The
			// row is cancel_pending, and the settlement pass cannot confirm it
			// (the children are still in the book under this signal).
			name: "cancel requested before the crash, fill raced it", state: store.StateCancelPending,
			atRestart: func(t *testing.T, w *lateFillWire, arm *store.ArmedOrderDB) {
				if err := w.st.ArmedOrders().RequestCancel(arm.ID, "gate_changed", time.Now().Add(-21*time.Minute).UnixMilli()); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w := newLateFillWire(t) // a FRESH trader = the process that just booted
			warns := captureLateWarns(t)
			arm := restingArm(t, w, "S1", "short", sid, px)
			tc.atRestart(t, w, arm)
			postRestartBook(w, sid, px)

			// The restart wiped exactly what the manual verdict read.
			if _, _, ok := w.tr.RecentFillFor(sid); ok {
				t.Fatal("fixture: the fill ring of a fresh process must be empty")
			}
			if got, ok := w.st.ArmedOrders().StateOf(arm.ID); !ok || got != tc.state {
				t.Fatalf("fixture: arm #%d state = %q, want %q", arm.ID, got, tc.state)
			}
			if filled, err := w.st.ArmedOrders().ListFilledSince(lateFillTrader, time.Now().Add(-time.Hour)); err != nil || len(filled) != 0 {
				t.Fatalf("fixture: no FILLED arm may exist (the fill frame never arrived): %d rows (%v)", len(filled), err)
			}

			manualBefore, _ := telemetry.ManualAdoptionCounts()
			keptBefore := telemetry.ManualAdoptKeptBotCount()
			row := adoptAfterRestart(t, w, "SHORT", 1, px)
			manualAfter, _ := telemetry.ManualAdoptionCounts()

			if row.Origin == store.OriginManual {
				t.Fatalf("pos %d — a bot arm (#%d %s, signal %s) that filled during the restart gap was adopted origin=manual: it leaves the bot record and the daily-loss flatten", row.ID, arm.ID, tc.state, sid)
			}
			if store.CanonicalOrigin(row.Origin) != store.OriginBot {
				t.Fatalf("pos %d origin = %q, want bot", row.ID, row.Origin)
			}
			if keptAfter := telemetry.ManualAdoptKeptBotCount(); keptAfter != keptBefore+1 {
				t.Fatalf("keeping the adoption bot must be COUNTED: kept-bot counter %d → %d, want +1", keptBefore, keptAfter)
			}
			if manualAfter != manualBefore {
				t.Fatalf("the manual adoption counter moved %d → %d for a bot arm's fill", manualBefore, manualAfter)
			}
			if !warns.has(fmt.Sprintf("pos %d", row.ID)) || !warns.has("kept origin=bot") || !warns.has(fmt.Sprintf("armed #%d", arm.ID)) {
				t.Fatalf("keeping an unattributed adoption as bot must be LOGGED naming the position and the arm: %q", warns.lines)
			}
			if line := materializedLine(t, warns); strings.Contains(line, "manual/NT8-side") {
				t.Fatalf("the 🧩 line calls a restart-gap arm fill a manual entry:\n%s", line)
			}
			if got, _ := w.st.ArmedOrders().StateOf(arm.ID); got != tc.state {
				t.Fatalf("reconcile must not touch the arm row: state %q → %q", tc.state, got)
			}
		})
	}
}

// The other side of the guard: the R1 shape — a 20-lot NT8 position with NO
// bot order or arm behind it — is still flagged manual, also when the ledger
// holds rows that are NOT evidence for it (a terminal same-side arm, a working
// arm of the OTHER side, a same-side arm authorized but never placed).
func TestRestartGapGenuineManualStillFlagged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, w *lateFillWire)
	}{
		{name: "no bot order or arm at all (R1)", setup: func(*testing.T, *lateFillWire) {}},
		{
			name: "only arms that cannot explain it",
			setup: func(t *testing.T, w *lateFillWire) {
				done := restingArm(t, w, "S1", "short", "sig-done", 31231)
				if err := w.st.ArmedOrders().SetState(done.ID, store.StateCancelled, "cancelled in NT8"); err != nil {
					t.Fatal(err)
				}
				restingArm(t, w, "S2", "long", "sig-other-side", 31180)
				restingArm(t, w, "S3", "short", "", 31260) // never placed: no signal id
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w := newLateFillWire(t)
			warns := captureLateWarns(t)
			tc.setup(t, w)
			manualBefore, _ := telemetry.ManualAdoptionCounts()
			keptBefore := telemetry.ManualAdoptKeptBotCount()
			row := adoptAfterRestart(t, w, "SHORT", 20, 31231.1125)
			manualAfter, _ := telemetry.ManualAdoptionCounts()
			if keptAfter := telemetry.ManualAdoptKeptBotCount(); keptAfter != keptBefore {
				t.Fatalf("the R1 shape bumped the kept-bot counter %d → %d", keptBefore, keptAfter)
			}
			if row.Origin != store.OriginManual {
				t.Fatalf("pos %d — a 20-lot with no bot order/arm behind it must be flagged manual (R1), got origin=%q", row.ID, row.Origin)
			}
			if manualAfter != manualBefore+1 {
				t.Fatalf("manual adoption counter %d → %d, want +1", manualBefore, manualAfter)
			}
			if !warns.has("flagged origin=manual") || warns.has("kept origin=bot") {
				t.Fatalf("the R1 shape must log the manual flag and never the kept-bot line: %q", warns.lines)
			}
		})
	}
}
