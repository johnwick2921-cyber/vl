package trader

import (
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// REL17-F2 restart rebuild — call-site tests. The production site is
// mentorRebuildLivePosAtBoot / mentorBuildLivePosFromBook, reached through the
// mentorRebuildBook seam so a test can pin the broker book without a live link.

func f2Trader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "f2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sc := store.StrategyConfig{
		RiskControl: store.RiskControlConfig{MentorMode: true},
		DayPlan:     &store.DayPlanConfig{PlanEnabled: true},
	}
	at := &AutoTrader{id: "tF2", store: st, exchange: "ninjatrader"}
	at.config.Exchange = "ninjatrader"
	at.config.NinjaTraderSymbol = "MNQ"
	at.config.StrategyConfig = &sc
	return at, st
}

// f2PinBook pins the boot-rebuild book source for the test and restores it.
func f2PinBook(t *testing.T, orders []ntwire.NT8Order, have bool, age time.Duration) {
	t.Helper()
	prev := mentorRebuildBook
	mentorRebuildBook = func(at *AutoTrader, now time.Time) ([]ntwire.NT8Order, bool, time.Duration) {
		return orders, have, age
	}
	t.Cleanup(func() { mentorRebuildBook = prev })
}

func f2Arm(signalID, condition string, contracts, leg1Qty int, entry, stop, target float64, exitMode *string) *store.ArmedOrderDB {
	row := &store.ArmedOrderDB{
		TraderID: "tF2", PlanID: "mentor", Scenario: "isb-1", Side: "long",
		State: store.StateFilled, SignalID: signalID, EntryPx: entry, StopPx: stop,
		TargetPx: target, Origin: store.ArmOriginMentor, Condition: condition,
		Contracts: store.IntPtr(contracts),
		Leg1TP:    target, FillPrice: entry, FillQuantity: contracts, ExitMode: exitMode,
	}
	if leg1Qty > 0 {
		row.Leg1Qty = store.IntPtr(leg1Qty)
	}
	return row
}

func f2Pos(signalID string, total, residual int, entry float64) *store.TraderPosition {
	return &store.TraderPosition{
		TraderID: "tF2", ExchangeID: "NT8", ExchangeType: "ninjatrader",
		Symbol: "MNQ", Side: "LONG", EntryQuantity: float64(total),
		Quantity: float64(residual), EntryPrice: entry, EntryOrderID: signalID,
		Status: "OPEN", Source: "system",
	}
}

func f2StopOrder(name string, stopPrice float64, qty int) ntwire.NT8Order {
	return ntwire.NT8Order{Name: name, State: "working", StopPrice: stopPrice, Quantity: qty, Symbol: "MNQ", Action: "sell"}
}

func f2TPOrder(name string, limit float64) ntwire.NT8Order {
	return ntwire.NT8Order{Name: name, State: "working", LimitPrice: limit, Symbol: "MNQ", Action: "sell"}
}

func TestMentorRebuildSplitPositionResumes(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000001"
	// Original row stop 31000; the broker has since moved it to entry (BE).
	arm := f2Arm(sig, "ISB", 4, 2, 31020, 31000, 31060, nil)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 4, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 31040, 2),  // leg 1 stop (already scaled)
		f2StopOrder(sig+"-sl2", 31020, 2), // runner at BE (at entry)
		f2TPOrder(sig+"-tp", 31040),       // leg 1 TP
		f2TPOrder(sig+"-tp2", 31060),      // runner target
	}, true, 0)

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	lp, ok := at.mentorLivePos[sig]
	if !ok || lp == nil {
		t.Fatal("position not registered")
	}
	if lp.Pos.Mode != "B" {
		t.Fatalf("mode=%q want B", lp.Pos.Mode)
	}
	if !lp.Pos.ArmedBE {
		t.Fatal("ArmedBE=false want true (broker stop at entry)")
	}
	if !lp.Pos.Scaled {
		t.Fatal("Scaled=false want true (residual 2 == runner 2)")
	}
	if lp.Legs[0].Wire != 1 || lp.Legs[1].Wire != 2 {
		t.Fatalf("legs not split: %+v %+v", lp.Legs[0], lp.Legs[1])
	}
	if lp.Legs[1].Stop != 31020 {
		t.Fatalf("runner stop=%v want 31020 (broker)", lp.Legs[1].Stop)
	}
}

func TestMentorRebuildMissingStopRefused(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000002"
	arm := f2Arm(sig, "ISB", 2, 0, 31020, 31000, 31040, nil) // single bracket
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	// Fresh book but NO protective stop for this signal.
	f2PinBook(t, []ntwire.NT8Order{f2TPOrder(sig+"-tp", 31040)}, true, 0)

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 0 || refused != 1 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 0/1/true", rebuilt, refused, ran)
	}
	if _, ok := at.mentorLivePos[sig]; ok {
		t.Fatal("position must NOT be registered when the stop is missing")
	}
	alerts, _ := st.Alert().List(at.id, 100)
	found := false
	for _, a := range alerts {
		if a.Kind == "mentor-restart-rebuild" {
			found = true
		}
	}
	if !found {
		t.Fatal("no P0 mentor-restart-rebuild alert raised")
	}
}

func TestMentorRebuildUsesBrokerStopNotOriginal(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000003"
	arm := f2Arm(sig, "ISB", 2, 0, 31020, 31000, 31040, nil) // single bracket
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	// The broker stop is 31020 (BE), NOT the row's original 31000.
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 31020, 2),
		f2TPOrder(sig+"-tp", 31040),
	}, true, 0)

	if _, refused, _ := at.mentorRebuildLivePosAtBoot(time.Now()); refused != 0 {
		t.Fatalf("refused=%d want 0", refused)
	}
	lp, ok := at.mentorLivePos[sig]
	if !ok || lp == nil {
		t.Fatal("position not registered")
	}
	if lp.Pos.Stop != 31020 {
		t.Fatalf("Pos.Stop=%v want 31020 (broker truth, not the original 31000)", lp.Pos.Stop)
	}
}

func TestMentorRebuildDerivedModePreChange(t *testing.T) {
	for _, tc := range []struct {
		condition string
		want      string
	}{
		{"SWING4H", "swing"},
		{"ISB", "B"},
		{"PHL", "B"},
	} {
		at, st := f2Trader(t)
		sig := "aa07e583-0000-0000-0000-000000000004"
		arm := f2Arm(sig, tc.condition, 2, 0, 31020, 31000, 31040, nil) // ExitMode nil = pre-change
		if err := st.ArmedOrders().UpsertArm(arm); err != nil {
			t.Fatal(err)
		}
		if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
			t.Fatal(err)
		}
		f2PinBook(t, []ntwire.NT8Order{
			f2StopOrder(sig+"-sl", 31000, 2),
			f2TPOrder(sig+"-tp", 31040),
		}, true, 0)
		if _, refused, _ := at.mentorRebuildLivePosAtBoot(time.Now()); refused != 0 {
			t.Fatalf("%s: refused want 0", tc.condition)
		}
		lp, ok := at.mentorLivePos[sig]
		if !ok || lp == nil {
			t.Fatalf("%s: not registered", tc.condition)
		}
		if lp.Pos.Mode != tc.want {
			t.Fatalf("%s: mode=%q want %q", tc.condition, lp.Pos.Mode, tc.want)
		}
	}
}

func TestMentorRebuildPersistedModeWins(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000005"
	mode := "C"
	arm := f2Arm(sig, "ISB", 2, 0, 31020, 31000, 31040, &mode)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 31000, 2),
		f2TPOrder(sig+"-tp", 31040),
	}, true, 0)
	if _, refused, _ := at.mentorRebuildLivePosAtBoot(time.Now()); refused != 0 {
		t.Fatal("refused want 0")
	}
	lp := at.mentorLivePos[sig]
	if lp == nil || lp.Pos.Mode != "C" {
		t.Fatalf("mode=%v want C (persisted wins over derivation)", lp)
	}
}

func TestMentorRebuildOnceWiresAndLatches(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000007"
	arm := f2Arm(sig, "ISB", 2, 0, 31020, 31000, 31040, nil)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 31020, 2),
		f2TPOrder(sig+"-tp", 31040),
	}, true, 0)

	at.mentorRebuildLivePosOnce(time.Now())
	if _, ok := at.mentorLivePos[sig]; !ok {
		t.Fatal("the Once hook did not rebuild the position")
	}
	if !at.mentorRebuildDone {
		t.Fatal("the Once hook did not latch done")
	}
	// Idempotent: a second call is a no-op (no double registration, no reset).
	at.mentorRebuildLivePosOnce(time.Now())
	if n := len(at.mentorLivePos); n != 1 {
		t.Fatalf("registered %d positions, want 1", n)
	}
}

func TestMentorRebuildNoFreshBookRetries(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-000000000006"
	arm := f2Arm(sig, "ISB", 2, 0, 31020, 31000, 31040, nil) // single bracket
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 31020)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, nil, false, 0) // no fresh book yet

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if ran || rebuilt != 0 || refused != 0 {
		t.Fatalf("ran=%v rebuilt=%d refused=%d want false/0/0", ran, rebuilt, refused)
	}
	if _, ok := at.mentorLivePos[sig]; ok {
		t.Fatal("nothing may be registered without a fresh book")
	}
	alerts, _ := st.Alert().List(at.id, 100)
	for _, a := range alerts {
		if a.Kind == "mentor-restart-rebuild" {
			t.Fatal("no P0 while waiting for a fresh book")
		}
	}
}
