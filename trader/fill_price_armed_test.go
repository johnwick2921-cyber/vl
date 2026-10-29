package trader

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m447 (7) at the ARMED entry fill handler: onArmedOrderUpdate -> the "filled"
// order_update -> materializeArmedEntry judges the fill price against the stored
// 1m bars of its minute. The fill is recorded exactly as before either way.
func armedFillFx(t *testing.T) (*AutoTrader, *store.Store, *checks.FillGuard, *[]string) {
	t.Helper()
	at, st := resetTrader(t, store.StrategyConfig{})
	s := ntwire.NewTCPServer(nil)
	at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	var alerts []string
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	g.SetAlert(func(kind, text string) { alerts = append(alerts, kind+" :: "+text) })
	saved := checks.DefaultFills
	checks.DefaultFills = g
	t.Cleanup(func() { checks.DefaultFills = saved })
	m := (time.Now().UTC().UnixMilli() - 90_000) / 60_000 * 60_000 // the minute that just closed
	var bars []ntwire.Bar
	for i := int64(-2); i <= 1; i++ {
		bars = append(bars, ntwire.Bar{T: m + i*60_000, O: 29000, H: 29005, L: 28998, C: 29002, V: 10, Final: true})
	}
	s.BarCache().Upsert("MNQ", "1m", bars)
	return at, st, g, &alerts
}

func TestArmedEntryFillFarPriceIsCountedAndTheFillStillMaterializes(t *testing.T) {
	at, st, g, alerts := armedFillFx(t)
	ledger := st.ArmedOrders()
	if err := ledger.UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-29:NY:trader-1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", EntryPx: 29000, StopPx: 28990, TargetPx: 29020, State: "working", SignalID: "sig-far"}); err != nil {
		t.Fatal(err)
	}
	// the minute of the fill is the one that closed 30-90 s ago: the guard sees
	// time.Now() as the fill time, whose bar is still forming -> it WAITS, then
	// Sweep judges it when that bar closes.
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-far", State: "filled", FillPrice: 29500, Quantity: 1}, ledger)
	if s := g.Stats(); s.Judged+int64(s.Pending) != 1 {
		t.Fatalf("the armed fill must reach the guard: %+v", s)
	}
	// the fill is recorded as before: the position materialized at the fill price.
	rows, _ := st.Position().GetOpenPositions(at.id)
	if len(rows) != 1 || rows[0].EntryPrice != 29500 {
		t.Fatalf("the check must not touch materialization: %+v", rows)
	}
	arms, _ := ledger.ListForPlan("2026-10-29:NY:trader-1")
	if len(arms) != 1 || arms[0].State != "filled" {
		t.Fatalf("the arm must still settle filled: %+v", arms)
	}
	_ = alerts
}

// The judgment itself at this handler with a closed minute: an armed fill whose
// time falls in a minute that has closed is judged at once.
func TestArmedEntryFillJudgedImmediatelyWhenItsMinuteIsClosed(t *testing.T) {
	at, _, g, alerts := armedFillFx(t)
	nt8 := at.armedTrader()
	m := (time.Now().UTC().UnixMilli() - 90_000) / 60_000 * 60_000
	nt8.NoteFill("entry", "arm|x|sig|1", "MNQ", "LONG", 29500, m+10_000, 29000)
	if s := g.Stats(); s.Far != 1 || len(*alerts) != 1 || !strings.Contains((*alerts)[0], "29500.00") {
		t.Fatalf("far armed-entry fill: %+v %v", s, *alerts)
	}
	nt8.NoteFill("entry", "arm|x|sig|2", "MNQ", "LONG", 29003, m+10_000, 29000)
	if s := g.Stats(); s.Far != 1 || s.Judged != 2 {
		t.Fatalf("an in-band fill is quiet: %+v", s)
	}
}

// RegisterBrokerTruthChecks registers BOTH m447 checks with the datatruth
// contract, and a required-but-unwired fill_price/broker_agreement blocks.
func TestRegisterBrokerTruthChecksRegistersBoth(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	saved := checks.DefaultFills
	checks.DefaultFills = checks.NewFillGuard(checks.DefaultFillParams(), nil)
	t.Cleanup(func() { checks.DefaultFills = saved })
	datatruth.Require(CheckBrokerAgreement, checks.CheckFillPrice)
	if b, rs := datatruth.Evaluate("MNQ", time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)); !b || len(rs) != 2 {
		t.Fatalf("both required-but-unwired checks must block: %v %+v", b, rs)
	}
	RegisterBrokerTruthChecks()
	snap := datatruth.Snapshot()
	if _, ok := snap[CheckBrokerAgreement]; !ok {
		t.Fatalf("broker_agreement not registered: %v", snap)
	}
	if _, ok := snap[checks.CheckFillPrice]; !ok {
		t.Fatalf("fill_price not registered: %v", snap)
	}
	// fill_price has no trader dependency: with nothing recorded it is clear.
	if b, rs := datatruth.Evaluate("MNQ", time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)); len(rs) != 1 || rs[0].Check != CheckBrokerAgreement || !b {
		t.Fatalf("only broker_agreement (no trader registered) should block now: %v %+v", b, rs)
	}
}
