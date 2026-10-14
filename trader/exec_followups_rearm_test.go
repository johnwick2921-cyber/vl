package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── CTO FOLD (PR #526) — a same-version arm that REACHED THE BROKER stays spent ──
//
// store.UpsertArm's W3 D15 pin ("a row that reached the broker is terminal for
// its plan version") returned early ONLY for market_in_zone. A planned_order or
// legacy row that the owner cancelled in NT8 — or that filled and stopped out —
// fell into the mint branch and became placement N+1 under the SAME version:
// manual cancel never won, and a stopped trade re-placed at the same level.
// These drive the REAL arm pass (maybeManageArmedOrdersAt) over a real socket.

// rearmFixture is liveArmFixture with the arm's entry policy chosen.
func rearmFixture(t *testing.T, traderID, policy string) (*AutoTrader, *store.Store, chan struct{ sid string }, time.Time) {
	t.Helper()
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC) // Thu 10:00 CT
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	at, st, sigs, _ := shadowWireHarnessAt(t, cfg, now)
	at.id = traderID
	live := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "reject", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "above"},
			Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110, Policy: policy}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	structuralTestMap(&live, structuralTestZone{100, 95.5, 100, "PDH"}, structuralTestZone{110, 110, 111, "target"})
	blob, _ := json.Marshal(live)
	shadowPlanAtTime(t, at, st, string(blob), now)
	t.Cleanup(func() { kernel.SetTraderPlanProviders(traderID, kernel.TraderPlanProviders{}) })
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNearAt(100, now) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	out := make(chan struct{ sid string }, 8)
	go func() {
		for s := range sigs {
			out <- struct{ sid string }{s.SignalID}
		}
	}()
	return at, st, out, now
}

func nextSig(t *testing.T, sigs chan struct{ sid string }, d time.Duration) string {
	t.Helper()
	select {
	case s := <-sigs:
		return s.sid
	case <-time.After(d):
		return ""
	}
}

// flatBroker re-states the broker as flat with an empty book at `at` (the
// AddOn's periodic snapshot after the order / position is gone).
func flatBroker(at *AutoTrader, when time.Time) {
	// The cancel / stop-out lands minutes after the send live: B3's 55 s
	// dedupe of the FIRST send is history by then.
	at.trader.(*ntTrader.TCPTrader).AgeOrderGuardForTest(5 * time.Minute)
	srv := at.trader.(*ntTrader.TCPTrader).GetServer()
	srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, when)
}

func rowsFor(t *testing.T, st *store.Store, traderID string) []store.ArmedOrderDB {
	t.Helper()
	var rows []store.ArmedOrderDB
	if err := st.ArmedOrders().DB().Where("trader_id = ?", traderID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestManualCancelInNT8StaysCancelledForThePlanVersion — the owner cancels the
// resting limit in NinjaTrader; the next arm pass under the SAME plan version
// must not mint and place a new one. For every entry policy.
func TestManualCancelInNT8StaysCancelledForThePlanVersion(t *testing.T) {
	for _, policy := range []string{kernel.EntryPolicyPlannedOrder, ""} {
		name := policy
		if name == "" {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			withMaintenanceDir(t)
			id := "fold-cancel-" + name
			at, st, sigs, now := rearmFixture(t, id, policy)
			at.maybeManageArmedOrdersAt(nil, now)
			sid := nextSig(t, sigs, 2*time.Second)
			if sid == "" {
				t.Fatalf("fixture: pass 1 must place the %s arm", name)
			}
			// The owner cancels it in NT8: the AddOn reports the order cancelled.
			at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: "cancelled", Symbol: "MNQ", Account: "Sim101"}, st.ArmedOrders())
			flatBroker(at, now.Add(time.Second))
			get := warnPlusCapture(t)
			before := gateBlocks(id, "arm_slot_spent")
			for i := 2; i <= 3; i++ {
				at.maybeManageArmedOrdersAt(nil, now.Add(time.Duration(i)*time.Second))
			}
			if s := nextSig(t, sigs, 500*time.Millisecond); s != "" {
				t.Fatalf("FOLD: the owner cancelled the %s order in NT8 and the SAME plan version placed it again (signal %s); rows=%+v", name, s, rowsFor(t, st, id))
			}
			rows := rowsFor(t, st, id)
			if len(rows) != 1 || rows[0].State != store.StateCancelled {
				t.Fatalf("FOLD: the cancelled row must stay the only row, cancelled; got %+v", rows)
			}
			if gateBlocks(id, "arm_slot_spent") != before+1 {
				t.Fatalf("FOLD: the refused re-arm must be counted ONCE (arm_slot_spent %d → %d)", before, gateBlocks(id, "arm_slot_spent"))
			}
			if !hasLine(get(), "re-arm REFUSED", "slot spent", "S1") {
				t.Fatalf("FOLD: the refused re-arm must be said, loudly; got %v", get())
			}
		})
	}
}

// TestFilledThenStoppedDoesNotRePlaceSameVersion — a planned_order fills, the
// position stops out, the broker is flat again: the SAME plan version must not
// re-place the same level (the re-place loop D15 was written to stop).
func TestFilledThenStoppedDoesNotRePlaceSameVersion(t *testing.T) {
	withMaintenanceDir(t)
	id := "fold-stopped"
	at, st, sigs, now := rearmFixture(t, id, kernel.EntryPolicyPlannedOrder)
	at.maybeManageArmedOrdersAt(nil, now)
	sid := nextSig(t, sigs, 2*time.Second)
	if sid == "" {
		t.Fatal("fixture: pass 1 must place the arm")
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: "filled", FillPrice: 100, Quantity: 1, Symbol: "MNQ", Account: "Sim101"}, st.ArmedOrders())
	open, err := st.Position().GetOpenPositions(id)
	if err != nil || len(open) != 1 {
		t.Fatalf("fixture: the fill must materialize one OPEN position: %v %+v", err, open)
	}
	// Stopped out at 95: the position closes, the broker is flat.
	if _, err := st.Position().ClosePosition(open[0].ID, 95, sid+"-sl", -10, 0, "stop_loss"); err != nil {
		t.Fatal(err)
	}
	flatBroker(at, now.Add(time.Second))
	for i := 2; i <= 3; i++ {
		at.maybeManageArmedOrdersAt(nil, now.Add(time.Duration(i)*time.Second))
	}
	if s := nextSig(t, sigs, 500*time.Millisecond); s != "" {
		t.Fatalf("FOLD: a planned_order that filled and stopped out was re-placed under the SAME version (signal %s); rows=%+v", s, rowsFor(t, st, id))
	}
}

// TestNewPlanVersionStillReArmsASpentSlot — the exception that stays: a NEW
// version re-authorizes the scenario, and it places again.
func TestNewPlanVersionStillReArmsASpentSlot(t *testing.T) {
	withMaintenanceDir(t)
	id := "fold-newver"
	at, st, sigs, now := rearmFixture(t, id, kernel.EntryPolicyPlannedOrder)
	at.maybeManageArmedOrdersAt(nil, now)
	sid := nextSig(t, sigs, 2*time.Second)
	if sid == "" {
		t.Fatal("fixture: pass 1 must place the arm")
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: "cancelled", Symbol: "MNQ", Account: "Sim101"}, st.ArmedOrders())
	flatBroker(at, now.Add(time.Second))
	// The planner publishes v2 of the same plan.
	led := st.ArmedOrders()
	r := rowsFor(t, st, id)[0]
	r2 := store.ArmedOrderDB{TraderID: id, PlanID: r.PlanID, Session: r.Session, Scenario: "S1", Version: r.Version + 1,
		State: "armed", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, Policy: kernel.EntryPolicyPlannedOrder}
	if err := led.UpsertArm(&r2); err != nil {
		t.Fatal(err)
	}
	rows := rowsFor(t, st, id)
	if len(rows) != 2 || rows[1].State != store.StateArmed || rows[1].PlacementSeq != r.PlacementSeq+1 {
		t.Fatalf("a NEW plan version must still mint the next placement: %+v", rows)
	}
}

// ── CTO RULING (PR #526) — the bot's OWN cancels re-arm within the version ──

// TestBotCancelReArmsSameVersion — (d) a cancel the BOT requested re-arms under
// the same plan version (today's behaviour): (d1) the band's synchronous sweep,
// whose prompt NT8 ack settles the row "cancelled in NT8" before any
// RequestCancel — the request is now stamped at the send; (d2) a
// RequestCancel-first path, as the install / maintenance withdraw uses.
func TestBotCancelReArmsSameVersion(t *testing.T) {
	for _, path := range []string{"band_sync_sweep", "withdraw_request_cancel"} {
		t.Run(path, func(t *testing.T) {
			withMaintenanceDir(t)
			id := "ruling-bot-" + path
			at, st, sigs, now := rearmFixture(t, id, kernel.EntryPolicyPlannedOrder)
			at.maybeManageArmedOrdersAt(nil, now)
			sid := nextSig(t, sigs, 2*time.Second)
			if sid == "" {
				t.Fatal("fixture: pass 1 must place the arm")
			}
			led := st.ArmedOrders()
			cancelled := ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: "cancelled", Symbol: "MNQ", Account: "Sim101"}
			switch path {
			case "band_sync_sweep":
				ch := make(chan ntwire.OrderUpdatePayload, 4)
				at.armedSyncSeam = &armedSyncSeam{
					Cancel:  func(s string) error { ch <- cancelled; return nil },
					Stream:  func() <-chan ntwire.OrderUpdatePayload { return ch },
					Timeout: time.Second,
				}
				if n, unacked := at.cancelArmedOrdersSync("no-trade band opened — no_trade_band: TEST lunch no-trade window", at.windowSweepPace(now.Add(time.Second))); n != 1 || unacked != 0 {
					t.Fatalf("fixture: the band sweep must cancel the one row, acked (n=%d unacked=%d)", n, unacked)
				}
				at.armedSyncSeam = nil
			case "withdraw_request_cancel":
				r := rowsFor(t, st, id)[0]
				if err := led.RequestCancel(r.ID, store.WithdrawReasonPrefix+"install pending job j1", now.Add(time.Second).UnixMilli()); err != nil {
					t.Fatal(err)
				}
				at.onArmedOrderUpdate(cancelled, led)
			}
			r := rowsFor(t, st, id)[0]
			if r.State != store.StateCancelled || r.CancelRequestedAtMs <= 0 {
				t.Fatalf("fixture: the bot's cancel must settle cancelled WITH its request on record: state=%s requested=%d reason=%q", r.State, r.CancelRequestedAtMs, r.StateReason)
			}
			flatBroker(at, now.Add(2*time.Second))
			at.maybeManageArmedOrdersAt(nil, now.Add(3*time.Second))
			if s := nextSig(t, sigs, 2*time.Second); s == "" || s == sid {
				t.Fatalf("RULING: a cancel the BOT requested (%s) must re-arm under the SAME version — no new placement; rows=%+v", path, rowsFor(t, st, id))
			}
		})
	}
}

// TestAbsentFromSnapshotWithoutRequestStaysSpent — (e) the stale-working
// reaper settles a quiet order the fresh broker book no longer lists
// ("absent from a fresh NT8 order_snapshot") with NO bot cancel request: the
// order ended at the broker by someone else's hand, so the slot is spent.
func TestAbsentFromSnapshotWithoutRequestStaysSpent(t *testing.T) {
	withMaintenanceDir(t)
	id := "ruling-absent"
	at, st, sigs, now := rearmFixture(t, id, kernel.EntryPolicyPlannedOrder)
	at.maybeManageArmedOrdersAt(nil, now)
	sid := nextSig(t, sigs, 2*time.Second)
	if sid == "" {
		t.Fatal("fixture: pass 1 must place the arm")
	}
	led := st.ArmedOrders()
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: "Working", Symbol: "MNQ", Account: "Sim101"}, led)
	rows := rowsFor(t, st, id)
	if rows[0].State != store.StateWorking {
		t.Fatalf("fixture: the order must be working, got %s", rows[0].State)
	}
	// Quiet for longer than the stale window, and the fresh book lacks it.
	later := time.Now().Add(30 * time.Minute)
	flatBroker(at, later)
	at.reconcileStaleWorking(led, rows, later, 5, nil)
	r := rowsFor(t, st, id)[0]
	if r.State != store.StateCancelled || r.CancelRequestedAtMs != 0 || !strings.HasPrefix(r.StateReason, "absent from a fresh NT8 order_snapshot") {
		t.Fatalf("fixture: the reaper must settle it absent with no request: state=%s requested=%d reason=%q", r.State, r.CancelRequestedAtMs, r.StateReason)
	}
	before := gateBlocks(id, "arm_slot_spent")
	at.maybeManageArmedOrdersAt(nil, now.Add(3*time.Second))
	if s := nextSig(t, sigs, 500*time.Millisecond); s != "" {
		t.Fatalf("RULING: an order absent from the book with no bot request must stay spent — re-placed as %s", s)
	}
	if got := rowsFor(t, st, id); len(got) != 1 || gateBlocks(id, "arm_slot_spent") != before+1 {
		t.Fatalf("RULING: one row, one counted refusal: rows=%d arm_slot_spent %d→%d", len(got), before, gateBlocks(id, "arm_slot_spent"))
	}
}
