package trader

// P0 stop-move-wrong-side (2026-10-21, c6cdfdc3): a stop move THROUGH the
// market (short: newStop <= price; long: newStop >= price) must CLOSE the leg
// at market instead of sending a move_stop NT8 would refuse or that would
// trigger immediately. Pins at the production call site mentorMoveLegStop plus
// the auto-breakeven path.

import (
	"errors"
	"sync"
	"testing"

	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// newWrongSideAT builds a minimal AutoTrader with the broker judged as "applied
// every move", the price seam pinned to (price, fresh), and both the move and
// close seams captured.
func newWrongSideAT(t *testing.T, price float64, fresh bool) (*AutoTrader, func() []driveMove, func() []int) {
	t.Helper()
	at := &AutoTrader{
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTrailMode: "off"}}},
		trader: &nttrader.TCPTrader{},
	}
	oldBroker := mentorBrokerLegStopRead
	mentorBrokerLegStopRead = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, leg *mentorLeg) (float64, int) {
		return leg.Stop, mentorBrokerStopLive
	}
	t.Cleanup(func() { mentorBrokerLegStopRead = oldBroker })

	oldPrice := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(_ *AutoTrader) (float64, bool) { return price, fresh }
	t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })

	var mu sync.Mutex
	var moves []driveMove
	var closes []int
	oldMove := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		mu.Lock()
		moves = append(moves, driveMove{signalID, side, newStop})
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = oldMove })

	oldClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, qty int) error {
		mu.Lock()
		closes = append(closes, qty)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })
	t.Cleanup(func() { at.resetMentorCountersForTest() })

	return at,
		func() []driveMove { mu.Lock(); defer mu.Unlock(); return append([]driveMove(nil), moves...) },
		func() []int { mu.Lock(); defer mu.Unlock(); return append([]int(nil), closes...) }
}

func TestMentorStopMoveWrongSideShortCloses(t *testing.T) {
	at, moves, closes := newWrongSideAT(t, 29823.0, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 2, Stop: 29870.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "short", leg, 29820.75)
	if !errors.Is(err, errMentorStopThroughMarket) {
		t.Fatalf("short BE through the market: err = %v, want errMentorStopThroughMarket", err)
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no move_stop must be sent, got %+v", got)
	}
	if got := closes(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("want one close-at-market of the leg's qty 2, got %+v", got)
	}
	if n := at.mentorCountSnapshot()["stop_move_through_market_close"]; n != 1 {
		t.Fatalf("stop_move_through_market_close = %d, want 1", n)
	}
}

func TestMentorStopMoveWrongSideShortSends(t *testing.T) {
	at, moves, closes := newWrongSideAT(t, 29815.0, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 2, Stop: 29870.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "short", leg, 29820.75)
	if err != nil {
		t.Fatalf("short BE on the profit side must send: %v", err)
	}
	if got := moves(); len(got) != 1 || got[0].newStop != 29820.75 {
		t.Fatalf("want one move_stop to 29820.75, got %+v", got)
	}
	if got := closes(); len(got) != 0 {
		t.Fatalf("no close, got %+v", got)
	}
}

func TestMentorStopMoveWrongSideLongCloses(t *testing.T) {
	at, moves, closes := newWrongSideAT(t, 29820.75, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 3, Stop: 29780.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "long", leg, 29821.0)
	if !errors.Is(err, errMentorStopThroughMarket) {
		t.Fatalf("long BE through the market: err = %v, want errMentorStopThroughMarket", err)
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no move_stop must be sent, got %+v", got)
	}
	if got := closes(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("want one close-at-market of the leg's qty 3, got %+v", got)
	}
}

func TestMentorStopMoveWrongSideLongSends(t *testing.T) {
	at, moves, closes := newWrongSideAT(t, 29825.0, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 2, Stop: 29805.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "long", leg, 29820.0)
	if err != nil {
		t.Fatalf("long BE on the profit side must send: %v", err)
	}
	if got := moves(); len(got) != 1 || got[0].newStop != 29820.0 {
		t.Fatalf("want one move_stop to 29820.00, got %+v", got)
	}
	if got := closes(); len(got) != 0 {
		t.Fatalf("no close, got %+v", got)
	}
}

func TestMentorStopMoveWrongSideShortBoundaryCloses(t *testing.T) {
	// Boundary: short newStop == price is THROUGH the market (<=) — close, never send.
	at, moves, closes := newWrongSideAT(t, 29820.75, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 2, Stop: 29870.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "short", leg, 29820.75)
	if !errors.Is(err, errMentorStopThroughMarket) {
		t.Fatalf("short newStop == price must close: %v", err)
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no move_stop, got %+v", got)
	}
	if got := closes(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("want one close, got %+v", got)
	}
}

func TestMentorStopMoveWrongSideLongBoundaryCloses(t *testing.T) {
	// Boundary: long newStop == price is THROUGH the market (>=) — close, never send.
	at, moves, closes := newWrongSideAT(t, 29820.75, true)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 3, Stop: 29780.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "long", leg, 29820.75)
	if !errors.Is(err, errMentorStopThroughMarket) {
		t.Fatalf("long newStop == price must close: %v", err)
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no move_stop, got %+v", got)
	}
	if got := closes(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("want one close, got %+v", got)
	}
}

func TestMentorStopMoveWrongSideStaleSends(t *testing.T) {
	// fresh=false: a stale price keeps today's behaviour — the move is SENT.
	at, moves, closes := newWrongSideAT(t, 29823.0, false)
	leg := &mentorLeg{SignalID: "sig-1", Qty: 2, Stop: 29870.0, Wire: 0}
	err := at.mentorMoveLegStop(at.trader.(*nttrader.TCPTrader), "short", leg, 29820.75)
	if err != nil {
		t.Fatalf("stale price must send as before: %v", err)
	}
	if got := moves(); len(got) != 1 {
		t.Fatalf("want one move_stop, got %+v", got)
	}
	if got := closes(); len(got) != 0 {
		t.Fatalf("no close on a stale price, got %+v", got)
	}
}

// TestAutoBreakevenWrongSideCloses pins the auto-breakeven half of the fix: the
// AI-era auto-BE moves the stop to entry; when the market is already through
// entry it must close at market instead of sending the invalid move_stop.
func TestAutoBreakevenWrongSideCloses(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	on := bePtr(true)
	at := &AutoTrader{
		id:       "autobe-wrong-side",
		exchange: "ninjatrader",
		trader:   &nttrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			RiskControl: store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 40},
		}},
	}
	oldPrice := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(_ *AutoTrader) (float64, bool) { return 29823.0, true }
	t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })

	oldWire := moveStopWire
	moved := false
	moveStopWire = func(_ *nttrader.TCPTrader, _ string, _ float64) error { moved = true; return nil }
	t.Cleanup(func() { moveStopWire = oldWire })

	oldClose := mentorCloseLegAtMarketWire
	closedQty := -1
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, qty int) error { closedQty = qty; return nil }
	t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })
	t.Cleanup(func() { at.resetMentorCountersForTest() })

	// short +40 pts in profit (entry 29820.75, mark 29780.75) → BE trigger fires;
	// entry is through the market (price 29823) → close, no move.
	at.maybeMoveStopToBreakeven("MNQ", "SHORT", 29820.75, 29780.75)
	if moved {
		t.Fatal("auto-BE wrong side: must NOT send move_stop")
	}
	if closedQty != 0 {
		t.Fatalf("auto-BE wrong side: want a close-at-market (qty 0 = all), got qty %d", closedQty)
	}
	if n := at.mentorCountSnapshot()["stop_move_through_market_close"]; n != 1 {
		t.Fatalf("stop_move_through_market_close = %d, want 1", n)
	}
}
