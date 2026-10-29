package trader

import (
	"strings"
	"sync"
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
// armedAlertMu guards the alert log: the router-fed fill consumer is a goroutine.
var armedAlertMu sync.Mutex

func armedAlertCount(a *[]string) int {
	armedAlertMu.Lock()
	defer armedAlertMu.Unlock()
	return len(*a)
}

func armedFillFx(t *testing.T) (*AutoTrader, *store.Store, *checks.FillGuard, *[]string, *ntwire.TCPServer) {
	t.Helper()
	at, st := resetTrader(t, store.StrategyConfig{})
	s := ntwire.NewTCPServer(nil)
	at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	var alerts []string
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	g.SetAlert(func(kind, text string) {
		armedAlertMu.Lock()
		defer armedAlertMu.Unlock()
		alerts = append(alerts, kind+" :: "+text)
	})
	saved := checks.SetDefaultFills(g)
	t.Cleanup(func() { checks.SetDefaultFills(saved) })
	m := (time.Now().UTC().UnixMilli() - 90_000) / 60_000 * 60_000 // the minute that just closed
	var bars []ntwire.Bar
	for i := int64(-2); i <= 1; i++ {
		bars = append(bars, ntwire.Bar{T: m + i*60_000, O: 29000, H: 29005, L: 28998, C: 29002, V: 10, Final: true})
	}
	s.BarCache().Upsert("MNQ", "1m", bars)
	return at, st, g, &alerts, s
}

// waitFills waits for the router-fed fill consumer (a goroutine) to have handed
// n fills to the guard: Judged + Pending counts every one that reached it.
func waitFills(t *testing.T, g *checks.FillGuard, n int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := g.Stats(); s.Judged+int64(s.Pending) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the fill never reached the guard: %+v", g.Stats())
}

func armFar(t *testing.T, at *AutoTrader, st *store.Store, sig string) *store.ArmedOrderStore {
	t.Helper()
	ledger := st.ArmedOrders()
	if err := ledger.UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-29:NY:trader-1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", EntryPx: 29000, StopPx: 28990, TargetPx: 29020, State: "working", SignalID: sig}); err != nil {
		t.Fatal(err)
	}
	return ledger
}

// m447 review defect 1. ONE armed entry fill reaches the bot as TWO frames from
// the same AddOn handler: the order_update (-> onArmedOrderUpdate ->
// materializeArmedEntry) and the fill frame (-> router -> handleFillInbound).
// Both production paths are driven here, in both orders; the fill must be counted
// ONCE, alert ONCE, and NOT pause (the first far fill alerts, the second pauses).
func TestArmedEntryOneFillFromBothFramesIsCountedOnce(t *testing.T) {
	for _, fillFrameFirst := range []bool{true, false} {
		at, st, g, alerts, srv := armedFillFx(t)
		ledger := armFar(t, at, st, "sig-far")
		m := (time.Now().UTC().UnixMilli() - 90_000) / 60_000 * 60_000
		fill := ntwire.FillPayload{SignalID: "sig-far", Symbol: "MNQ", Account: "Sim101", Status: "filled",
			FillPrice: 29500, FillTime: time.UnixMilli(m + 20_000).UTC().Format(time.RFC3339), Side: "buy", Quantity: 1}
		upd := ntwire.OrderUpdatePayload{SignalID: "sig-far", State: "filled", FillPrice: 29500, Quantity: 1, Symbol: "MNQ", Account: "Sim101"}
		if fillFrameFirst {
			srv.FeedFillForTest(fill)
			waitFills(t, g, 1)
			at.onArmedOrderUpdate(upd, ledger)
		} else {
			at.onArmedOrderUpdate(upd, ledger)
			srv.FeedFillForTest(fill)
			waitFills(t, g, 1)
		}
		time.Sleep(150 * time.Millisecond) // a (wrong) second judgment would have landed by now
		s := g.Stats()
		blocked, why, _ := g.Check()("MNQ", time.Now())
		if s.Far != 1 || s.Judged != 1 || s.Pending != 0 || armedAlertCount(alerts) != 1 || blocked {
			t.Fatalf("fillFrameFirst=%v: ONE far fill must count once, alert once, not pause: stats=%+v alerts=%d blocked=%v (%s)", fillFrameFirst, s, armedAlertCount(alerts), blocked, why)
		}
		// and the fill is recorded as before: the position materialized at the fill price.
		rows, _ := st.Position().GetOpenPositions(at.id)
		if len(rows) != 1 || rows[0].EntryPrice != 29500 {
			t.Fatalf("the check must not touch materialization: %+v", rows)
		}
		arms, _ := ledger.ListForPlan("2026-10-29:NY:trader-1")
		if len(arms) != 1 || arms[0].State != "filled" {
			t.Fatalf("the arm must still settle filled: %+v", arms)
		}
	}
}

// The judgment itself at this handler with a closed minute: an armed fill whose
// time falls in a minute that has closed is judged at once.
func TestArmedEntryFillJudgedImmediatelyWhenItsMinuteIsClosed(t *testing.T) {
	at, _, g, alerts, _ := armedFillFx(t)
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
	saved := checks.SetDefaultFills(checks.NewFillGuard(checks.DefaultFillParams(), nil))
	t.Cleanup(func() { checks.SetDefaultFills(saved) })
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
