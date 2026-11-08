package trader

import (
	"testing"
	"time"

	"vl/market"
	nt "vl/provider/ninjatrader"
	"vl/store"
)

// m471 lane 2 — ONE EXIT IN FLIGHT per position.
//
// Live 2026-10-30 14:45:21, trader 12345 (vl_2026-10-30.log:33900/33903): the
// monitor beat and runCycle each ran the EOD flat for the same OPEN row in one
// second. Each sent a whole-position "-lx" limit exit (EOD_FLAT_LIMIT_TICKS=2),
// the AddOn sized both to the 1-lot, and the second fill opened LONG 1 with no
// stop (rows 723/724; nt8_order_snapshots 219327-219332 show both -lx working).

// L2-1: repeat flattens of one OPEN position — the EOD flat run twice (monitor
// beat + runCycle) and then the daily-loss flat — send exactly ONE close order.
func TestRepeatFlattenOfOnePositionSendsExactlyOneCloseOrder(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	offset := 15
	for _, side := range []string{"long", "short"} {
		t.Run(side, func(t *testing.T) {
			acks := make(chan nt.OrderUpdatePayload, 4)
			at, _ := eodFixture(t, now, side, time.Second, ackPump(t, acks), acks, &offset)
			r := &limitFlattenRecorder{MockTrader: &MockTrader{}}
			at.trader = r
			at.config.LimitCloseTicks = 2
			at.config.LimitCloseMarketAfterS = 10
			t.Cleanup(at.stopLimitFlattens)

			if !at.enforceEODFlatAt(now) { // monitor-beat caller
				t.Fatal("fixture: the EOD flat did not act")
			}
			if !at.enforceEODFlatAt(now) { // runCycle caller, row still OPEN
				t.Fatal("fixture: the second EOD flat did not act")
			}
			open, err := at.store.Position().GetOpenPositions(at.id)
			if err != nil || len(open) != 1 {
				t.Fatalf("fixture: open rows = %d err=%v", len(open), err)
			}
			at.flattenPositionAt(open[0], "🔴 DAILY-LOSS FLAT", now) // another caller, same row

			if r.limits != 1 || r.closes != 0 {
				t.Fatalf("one OPEN position got %d limit exit(s) and %d market close(s) from three flatten calls — want exactly one close order (a second whole-position -lx reverses the position)", r.limits, r.closes)
			}
		})
	}
}

// L2-1: a caller with no durable ID (and a duplicate row of the same position)
// is covered by the (account, symbol, side) key.
func TestRepeatFlattenKeyedByAccountSymbolSideCoversIDlessCallers(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "SHORT")
	at.flattenPosition(p, "🕒 EOD-FLAT")

	idless := *p
	idless.ID = 0
	at.flattenPosition(&idless, "ID-less caller")

	dup := *p
	dup.ID = p.ID + 100 // a duplicate row of the same account/symbol/side
	at.flattenPosition(&dup, "duplicate row")

	noAcct := store.TraderPosition{Symbol: "MNQ", Side: "SHORT"} // names no account
	at.flattenPosition(&noAcct, "no account")

	if r.limits != 1 || r.closes != 0 {
		t.Fatalf("ID-less / duplicate callers sent %d limit exit(s) and %d market close(s); want 1 and 0", r.limits, r.closes)
	}
	other := *p
	other.ID = 0
	other.Side = "LONG" // a different side is a different exit
	at.flattenPosition(&other, "other side")
	if r.limits != 2 {
		t.Fatalf("a different side must not be deduped: limits=%d", r.limits)
	}
}

// L2-1: once the in-flight exit's market deadline has passed with the exit still
// unresolved (ambiguous rows made the timer refuse), a repeat flatten escalates
// THAT exit to the market close — and the timer callback then sends nothing.
func TestRepeatFlattenPastDeadlineEscalatesTheSameExitToMarket(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	at.flattenPosition(p, "🕒 EOD-FLAT")
	pending := at.limitFlattens[p.ID]
	if pending == nil {
		t.Fatal("no in-flight record")
	}
	at.limitFlattenMu.Lock()
	pending.deadline = time.Now().Add(-time.Second) // the fallback window has elapsed
	at.limitFlattenMu.Unlock()

	at.flattenPosition(p, "🕒 EOD-FLAT")
	if r.limits != 1 || r.closes != 1 {
		t.Fatalf("past the deadline: limits=%d closes=%d, want 1 limit then 1 market escalation", r.limits, r.closes)
	}
	at.flattenPosition(p, "🕒 EOD-FLAT") // inside the market hold
	at.finishLimitFlatten(pending)      // the timer firing late
	if r.limits != 1 || r.closes != 1 {
		t.Fatalf("after escalation: limits=%d closes=%d, want no further order", r.limits, r.closes)
	}
}

// L2-1 over the REAL wire (TCPTrader → TCPServer → fake AddOn socket): two EOD
// flats for one OPEN row put ONE close_position frame on the wire.
func TestRepeatEODFlatPutsOneLimitExitFrameOnTheWire(t *testing.T) {
	r := crRigFor(t, crEODNow(t), nil)
	r.a.config.LimitCloseTicks = 2
	r.a.config.LimitCloseMarketAfterS = 3600
	t.Cleanup(r.a.stopLimitFlattens)
	r.open(t, r.a, crAcctA, 1)

	for i := 0; i < 2; i++ {
		if !r.a.enforceEODFlatAt(r.now) {
			t.Fatal("fixture: EOD flat did not act")
		}
	}
	got := r.w.settle(t)
	if len(got) != 1 || got[0].LimitPrice <= 0 {
		t.Fatalf("two EOD flats of one OPEN row put %d close_position frame(s) on the wire, want ONE limit exit: %+v", len(got), got)
	}
}

// L2-2: the protection-through-stop flatten of a naked LONG whose price is
// already below its stop closes at MARKET even with limit exits on, and a pass
// inside the in-flight hold sends nothing more.
func TestProtectionThroughStopFlattenClosesAtMarketNeverLimit(t *testing.T) {
	for _, side := range []string{"LONG", "SHORT"} {
		t.Run(side, func(t *testing.T) {
			at, r, _ := limitFlattenFixture(t, side)
			at.config.LimitCloseTicks = 2
			at.config.LimitCloseMarketAfterS = 10
			prev := market.FuturesBarsProvider
			market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline {
				return []market.Kline{{Close: 29990}}
			}
			t.Cleanup(func() { market.FuturesBarsProvider = prev })

			v := protectionVerdict{Action: protectionFlatten, StopPx: 29995, NeedQty: 1, Why: "test"}
			now := time.Now()
			at.actOnProtection(v, "MNQ", side, 1, "sig-p", "monitor", 0, now)
			if r.closes != 1 || r.cancels != 1 || r.limits != 0 {
				t.Fatalf("first pass: closes=%d cancels=%d limits=%d (limit prices %v) — an emergency close must be ONE market close + bracket cancel, never a limit",
					r.closes, r.cancels, r.limits, r.limitPx)
			}
			at.actOnProtection(v, "MNQ", side, 1, "sig-p", "monitor", 0, now.Add(exitInFlightHold/3))
			if r.closes != 1 || r.limits != 0 {
				t.Fatalf("a pass inside the in-flight hold re-sent: closes=%d limits=%d", r.closes, r.limits)
			}
			at.actOnProtection(v, "MNQ", side, 1, "sig-p", "monitor", 0, now.Add(exitInFlightHold+time.Second))
			if r.closes != 2 || r.limits != 0 {
				t.Fatalf("a pass after the hold must retry at market: closes=%d limits=%d", r.closes, r.limits)
			}
		})
	}
}

// L2-2 + L2-1: with a limit exit already resting for the position, the
// protection flatten escalates THAT exit to market (NT8's Flatten cancels the
// -lx) instead of adding an order; the timer callback then sends nothing.
func TestProtectionFlattenEscalatesARestingLimitExit(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	at.flattenPosition(p, "🕒 EOD-FLAT")
	pending := at.limitFlattens[p.ID]
	v := protectionVerdict{Action: protectionFlatten, StopPx: 29995, NeedQty: 1, Why: "test"}
	now := time.Now()
	at.actOnProtection(v, "MNQ", "LONG", 1, "sig-p", "monitor", 0, now)
	if r.limits != 1 || r.closes != 1 || r.cancels != 1 {
		t.Fatalf("protection with a -lx resting: limits=%d closes=%d bracket-cancels=%d — want THAT exit escalated to one market close at once, not left to the limit's fallback",
			r.limits, r.closes, r.cancels)
	}
	at.actOnProtection(v, "MNQ", "LONG", 1, "sig-p", "monitor", 0, now.Add(time.Minute))
	at.finishLimitFlatten(pending)
	at.flattenPosition(p, "🕒 EOD-FLAT")
	if r.limits != 1 || r.closes != 1 {
		t.Fatalf("limits=%d closes=%d — want the one resting limit escalated by ONE market close, nothing stacked", r.limits, r.closes)
	}
}

// L2-3: the position ends another way (its bracket -sl filled) while its -lx
// rests. At the fallback the row is CLOSED: no market close (it could hit a
// replacement), but a cancel_order names the -lx's close signal.
func TestLimitExitIsCancelledWhenItsPositionClosesAnotherWay(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	at.flattenPosition(p, "🔴 DAILY-LOSS FLAT")
	if err := at.store.GormDB().Model(&store.TraderPosition{}).Where("id = ?", p.ID).Update("status", "CLOSED").Error; err != nil {
		t.Fatal(err)
	}
	fireLimitFlatten(t, at, p)
	if len(r.cancelOrders) != 1 || r.cancelOrders[0] != "close-1" || r.closes != 0 || r.cancels != 0 {
		t.Fatalf("cancel_order=%v closes=%d bracket-cancels=%d — want ONE cancel_order for the resting -lx's close signal and nothing else",
			r.cancelOrders, r.closes, r.cancels)
	}
	if len(at.limitFlattens) != 0 || len(at.limitFlattensByKey) != 0 {
		t.Fatal("the closed position's exit record was kept")
	}
}

// L2-3: Stop cancels a still-resting -lx (its fallback timer dies with Stop).
func TestStopCancelsARestingLimitExit(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "SHORT")
	at.flattenPosition(p, "🕒 EOD-FLAT")
	at.stopLimitFlattens()
	if len(r.cancelOrders) != 1 || r.cancelOrders[0] != "close-1" || r.closes != 0 {
		t.Fatalf("Stop: cancel_order=%v closes=%d — want the resting -lx cancelled by its close signal", r.cancelOrders, r.closes)
	}
}

// L2-3 over the REAL wire: the cancel_order frame names the SAME signal id as
// the close_position frame that placed the -lx (the AddOn names it
// "<signal_id>-lx").
func TestLimitExitCancelFrameNamesTheCloseSignal(t *testing.T) {
	r := crRigFor(t, crEODNow(t), nil)
	r.a.config.LimitCloseTicks = 2
	r.a.config.LimitCloseMarketAfterS = 3600
	t.Cleanup(r.a.stopLimitFlattens)
	p := r.open(t, r.a, crAcctA, 1)
	if !r.a.enforceEODFlatAt(r.now) {
		t.Fatal("fixture: EOD flat did not act")
	}
	if err := r.st.GormDB().Model(&store.TraderPosition{}).Where("id = ?", p.ID).Update("status", "CLOSED").Error; err != nil {
		t.Fatal(err)
	}
	pending := r.a.limitFlattens[p.ID]
	if pending == nil {
		t.Fatal("no in-flight record")
	}
	pending.timer.Stop()
	r.a.finishLimitFlatten(pending)
	closes := r.w.settle(t)
	r.w.mu.Lock()
	cancels := append([]nt.CancelOrderPayload(nil), r.w.cancels...)
	r.w.mu.Unlock()
	if len(closes) != 1 || closes[0].LimitPrice <= 0 {
		t.Fatalf("want one limit close_position frame, got %+v", closes)
	}
	if len(cancels) != 1 || cancels[0].SignalID != closes[0].SignalID || cancels[0].Account != crAcctA {
		t.Fatalf("cancel_order frames %+v do not name the -lx close signal %q on %s", cancels, closes[0].SignalID, crAcctA)
	}
}

// L2-3: a working -lx with NO position is an order that opens one — the
// account is not contract-free. With the position still open it is protective.
func TestOrphanLimitExitCommitsTheAccount(t *testing.T) {
	lx := nt.NT8Order{OrderID: "o1", Symbol: "MNQ", Name: "3a019958-3d45-4a23-a2c1-41211fd7f838-lx",
		Action: "buy", Type: "limit", LimitPrice: 28429.75, Quantity: 1, State: "Working"}
	v := adjudicateAccountContract([]nt.NT8Order{lx}, true, time.Second, time.Minute, 219333, 0)
	if v.Action != contractLive || v.OrphanExits != 1 || v.FirstName != lx.Name {
		t.Fatalf("orphan -lx with 0 positions: verdict=%+v, want live naming the -lx", v)
	}
	if r := v.Refusal(); r == "" {
		t.Fatal("no refusal text")
	}
	withPos := adjudicateAccountContract([]nt.NT8Order{lx}, true, time.Second, time.Minute, 1, 1)
	if withPos.Action != contractLive || withPos.OrphanExits != 0 || withPos.WorkingEntries != 0 {
		t.Fatalf("-lx beside its position is protective, not an entry: %+v", withPos)
	}
}
