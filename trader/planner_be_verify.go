package trader

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ntTrader "vl/trader/ninjatrader"
)

// ── N-G (release #22 rescan) — THE PLANNER AUTO-BE GETS AN NT8 VERDICT ───────
//
// maybeMoveStopToBreakeven logged "stop moved" when it SENT the move_stop and
// set breakevenDone on a nil send error. A change NT8 refused was never
// retried and never alarmed, while the mentor's moves get a verdict (F1,
// mentorVerifyPendingStopMoves). Position 654's BE at 10:08:27 did apply
// (snapshot 112688) — this file makes that a READ fact instead of a hope.
//
// One pending move per (trader, symbol_side). It is judged on the FIRST fresh
// broker book received more than 2 s after the send:
//   - every "<signal>-sl"/"-sl2" within half a tick of the BE → CONFIRMED;
//   - the broker stop TIGHTER than the BE → kept (never re-sent: that would
//     widen it);
//   - no such stop in the book → left to the protection net (P2-1 places the
//     tightest known stop);
//   - otherwise NOT APPLIED → breakevenDone is cleared (the next monitor beat
//     re-sends), an ERROR line, a counter and ONE P0 per signal.

type plannerBEPending struct {
	signalID string
	side     string // "LONG" | "SHORT" as GetPositions reports it
	want     float64
	sendAt   time.Time
	beKey    string // the breakevenDone key (symbol_side)

	staleWarned bool
}

// plannerBEPendingMoves: trader id + "|" + beKey → *plannerBEPending.
var plannerBEPendingMoves sync.Map

// plannerBECounters: trader id + "|" + kind → *atomic.Int64. A COUNTER,
// never an inference (checklist 35): kinds are confirmed, broker_tighter,
// no_order, not_applied, stale_book, no_signal.
var plannerBECounters sync.Map

func incPlannerBE(traderID, kind string) {
	v, _ := plannerBECounters.LoadOrStore(traderID+"|"+kind, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// PlannerBECount returns this trader's count of planner auto-BE verdicts of
// one kind (tests, reports).
func PlannerBECount(traderID, kind string) int64 {
	if v, ok := plannerBECounters.Load(traderID + "|" + kind); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}

// plannerBEHalfTick is half of the instrument tick (MNQ 0.25 → 0.125).
func (at *AutoTrader) plannerBEHalfTick() float64 {
	return ntTrader.InstrumentTickSize(at.futuresSymbol()) / 2
}

// recordPlannerBEPending is called right after a SENT auto-BE move_stop. The
// signal id is the one the move_stop frame addressed (the same resolution
// MoveStopToBreakeven makes); empty → no verdict is possible, said and counted.
func (at *AutoTrader) recordPlannerBEPending(nt *ntTrader.TCPTrader, symbol, side, beKey string, want float64, now time.Time) {
	sid := ""
	if nt != nil {
		sid = nt.EntrySignalIDFor(side)
	}
	if sid == "" {
		incPlannerBE(at.id, "no_signal")
		at.logWarnf("🎯 auto-breakeven verify: %s %s — no entry signal id to read the broker book by; NO verdict is possible for this move (counted)", symbol, side)
		return
	}
	plannerBEPendingMoves.Store(at.id+"|"+beKey, &plannerBEPending{
		signalID: sid, side: strings.ToUpper(strings.TrimSpace(side)), want: want, sendAt: now, beKey: beKey,
	})
}

// prunePlannerBEPending drops this trader's pending verdicts for positions
// that are no longer open (called beside pruneBreakevenDone).
func (at *AutoTrader) prunePlannerBEPending(openKeys map[string]bool) {
	prefix := at.id + "|"
	plannerBEPendingMoves.Range(func(k, _ any) bool {
		key, _ := k.(string)
		if strings.HasPrefix(key, prefix) && !openKeys[strings.TrimPrefix(key, prefix)] {
			plannerBEPendingMoves.Delete(k)
		}
		return true
	})
}

// verifyPlannerBEMove renders the verdict for this position's pending auto-BE
// move, if one is pending and a fresh enough book has arrived. Called on the
// monitor beat for every open position, before the trigger check.
func (at *AutoTrader) verifyPlannerBEMove(beKey string, now time.Time) {
	key := at.id + "|" + beKey
	v, ok := plannerBEPendingMoves.Load(key)
	if !ok {
		return
	}
	mv := v.(*plannerBEPending)
	nt, _ := at.trader.(*ntTrader.TCPTrader)
	book, recvAt, have := mentorStopBook(nt)
	if !have || recvAt.IsZero() {
		return // no book at all → no verdict yet; the protection net owns the position
	}
	// A verdict needs the FIRST snapshot NEWER than send+2s; an older one
	// proves nothing (the move may not have reached NT8 yet).
	if !recvAt.After(mv.sendAt.Add(2 * time.Second)) {
		return
	}
	if age := now.Sub(recvAt); age > snapshotMaxAge() {
		if !mv.staleWarned {
			mv.staleWarned = true
			incPlannerBE(at.id, "stale_book")
			at.logWarnf("🎯 auto-breakeven verify: stale book (%s old) — no verdict yet for signal %s (want %.2f)", age.Round(time.Second), shortID(mv.signalID), mv.want)
		}
		return
	}
	plannerBEPendingMoves.Delete(key)
	stops := mentorBrokerStopOrders(book, mv.signalID, 0)
	if len(stops) == 0 {
		incPlannerBE(at.id, "no_order")
		at.logWarnf("🎯 auto-breakeven verify: NO live stop order for signal %s in the broker book — leaving it to the protection net (it places the tightest known stop, the BE %.2f)", shortID(mv.signalID), mv.want)
		return
	}
	half := at.plannerBEHalfTick()
	brokerPx := stops[0].StopPrice
	applied := true
	for _, o := range stops {
		brokerPx = o.StopPrice
		if math.Abs(o.StopPrice-mv.want) > half {
			applied = false
			break
		}
	}
	if applied {
		incPlannerBE(at.id, "confirmed")
		at.logInfof("🎯 auto-breakeven CONFIRMED at NT8: signal %s stop %.2f", shortID(mv.signalID), mv.want)
		return
	}
	if stopTighter(mv.side, brokerPx, mv.want) {
		incPlannerBE(at.id, "broker_tighter")
		at.logWarnf("🎯 auto-breakeven: broker stop %.2f is TIGHTER than the BE %.2f for signal %s — kept, nothing re-sent", brokerPx, mv.want, shortID(mv.signalID))
		return
	}
	// NOT APPLIED: the frame was sent, the broker still holds the wider stop.
	incPlannerBE(at.id, "not_applied")
	at.breakevenMu.Lock()
	if at.breakevenDone != nil {
		at.breakevenDone[mv.beKey] = false // the next monitor beat re-sends
	}
	at.breakevenMu.Unlock()
	at.logErrorf("🚨 auto-breakeven NOT APPLIED at NT8: signal %s want %.2f broker %.2f — breakeven re-armed, the next monitor beat re-sends (not_applied count %d)",
		shortID(mv.signalID), mv.want, brokerPx, PlannerBECount(at.id, "not_applied"))
	at.emitAlert("P0", "stop-move-not-applied",
		fmt.Sprintf("auto-be-not-applied:%s", mv.signalID),
		"Auto-breakeven not applied at NT8",
		fmt.Sprintf("signal %s: wanted the stop at %.2f (breakeven), the broker still holds %.2f. The bot re-sends on the next monitor beat; check NinjaTrader if this repeats.", mv.signalID, mv.want, brokerPx))
}
