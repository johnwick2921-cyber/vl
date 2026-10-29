package trader

import (
	"errors"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── SWING EXIT STATE (release #54, m441) ────────────────────────────────────
//
// The swing's BE and hold-close are evaluator intents (manageSwingPosition) the
// trader executes (mentorMoveStopBE / mentorClosePosition). Three live defects
// sat here (system check 10-29, N2 / N3 / #15):
//
//   N2  A stopped-out swing stayed "open" in the evaluator until its hold
//       deadline, then sent a BE (moving ANOTHER trade's stop) or a close (the
//       AddOn runs close_position as a Flatten of the whole instrument).
//   N3  Both intents were single-shot: the evaluator cleared its state at EMIT,
//       so a refusal was final — and the log promised "retried next 4h read".
//   #15 The hold-close was a market Flatten, which cancels every resting arm on
//       the account.
//
// The model now: the evaluator keeps the swing until THIS file reports back —
// SwingBEApplied when the broker took the stop, SwingEnded when the position is
// provably gone — and every action is gated on the account holding a position
// that carries THIS arm's own signal id.

// mentorSwingOwn is the verdict on "does the account hold the position this
// swing arm owns".
type mentorSwingOwn int

const (
	// mentorSwingUnproven — nothing can be proven right now (row not filled
	// yet, positions table unreadable, no record of the signal anywhere). Act
	// on nothing; the evaluator re-emits on the next closed 5m bar.
	mentorSwingUnproven mentorSwingOwn = iota
	// mentorSwingLive — an OPEN position (or a working bracket at the broker)
	// carries this arm's own signal id.
	mentorSwingLive
	// mentorSwingGone — the arm's position is provably over: its row never
	// filled and is terminal, or the position carrying its signal is CLOSED and
	// the broker holds no bracket for it. The evaluator state must be cleared.
	mentorSwingGone
)

// mentorSwingPositionState reads the positions table for the arm's signal id:
// open=true when an OPEN row carries it, closed=true when a CLOSED row does.
// A seam so a test can pin an unreadable table; production reads the store.
var mentorSwingPositionState = func(at *AutoTrader, signalID string) (open, closed bool, err error) {
	if at == nil || at.store == nil || signalID == "" {
		return false, false, errors.New("no store / signal id")
	}
	ps := at.store.Position()
	if ps == nil {
		return false, false, errors.New("no position store")
	}
	opens, err := ps.GetOpenPositions(at.id)
	if err != nil {
		return false, false, err
	}
	for _, p := range opens {
		if p != nil && p.EntryOrderID == signalID {
			return true, false, nil
		}
	}
	rows, err := ps.ListClosedByEntryOrderIDs(at.id, []string{signalID})
	if err != nil {
		return false, false, err
	}
	return false, len(rows) > 0, nil
}

// mentorSwingBracketAtBroker reports whether the broker's FRESH order book holds
// a working stop carrying the signal id ("<signal>-sl", -sl2, -sl3) — a bracket
// only exists while the position it protects is open. known=false when there
// is no fresh book (nothing proven either way).
func (at *AutoTrader) mentorSwingBracketAtBroker(nt *ntTrader.TCPTrader, signalID string) (present, known bool) {
	if nt == nil || signalID == "" {
		return false, false
	}
	book, recvAt, ok := mentorStopBook(nt)
	if !ok || recvAt.IsZero() || mentorClockNow().Sub(recvAt) > snapshotMaxAge() {
		return false, false
	}
	working := ntwire.OrderSnapshotPayload{Orders: book.WorkingOrders()}
	return len(mentorBrokerStopOrders(working, signalID, 0)) > 0, true
}

// mentorSwingOwnership answers, for one swing arm, whether the account holds a
// position carrying that arm's OWN signal id. It is the gate for every swing
// BE / hold-close (N2): the evaluator's belief that the swing is open is never
// enough — the broker side may hold a DIFFERENT trade by now, and the AddOn's
// close_position is a whole-instrument flatten.
//
// Proof of life: an OPEN positions row with EntryOrderID == the arm's signal id,
// or a working bracket stop for that signal in the fresh broker book. Proof of
// death: the row never filled (cancelled / rejected, no fill qty), or a CLOSED
// positions row carries the signal and no bracket for it is still working.
// Anything else is unproven — never guessed either way.
func (at *AutoTrader) mentorSwingOwnership(nt *ntTrader.TCPTrader, arm mentorLiveArm) (r store.ArmedOrderDB, qty float64, own mentorSwingOwn, why string) {
	if at == nil || at.store == nil || at.store.ArmedOrders() == nil {
		at.mentorCount("swing_fill_no_ledger")
		at.logErrorf("🧑‍🏫 mentor swing ownership check REFUSED — no armed ledger")
		return store.ArmedOrderDB{}, 0, mentorSwingUnproven, "no armed ledger"
	}
	if err := at.store.ArmedOrders().DB().First(&r, arm.RowID).Error; err != nil {
		at.mentorCount("swing_fill_row_gone")
		at.logWarnf("🧑‍🏫 mentor swing ownership check REFUSED — ArmID row %d unreadable: %v", arm.RowID, err)
		return store.ArmedOrderDB{}, 0, mentorSwingUnproven, "arm row unreadable"
	}
	filled := r.State == store.StateFilled || r.FillQuantity > 0
	if !filled {
		// A terminal row that never filled (cancelled / rejected / expired ...)
		// never became a position; one still in flight may yet fill.
		if store.IsTerminalArmState(r.State) {
			return r, 0, mentorSwingGone, "the arm's order never filled (" + r.State + ")"
		}
		return r, 0, mentorSwingUnproven, "the arm's order has not filled yet (" + r.State + ")"
	}
	if r.FillQuantity > 0 {
		qty = float64(r.FillQuantity)
	} else if r.Contracts != nil && *r.Contracts > 0 {
		qty = float64(*r.Contracts)
	}
	open, closed, err := mentorSwingPositionState(at, r.SignalID)
	if err != nil {
		at.mentorCount("swing_ownership_unreadable")
		at.logWarnf("🧑‍🏫 mentor swing ownership: positions unreadable for signal %s (%v) — nothing acted on, retried on the next 5m bar", r.SignalID, err)
		return r, qty, mentorSwingUnproven, "positions unreadable"
	}
	bracket, bookKnown := at.mentorSwingBracketAtBroker(nt, r.SignalID)
	switch {
	case open || (bookKnown && bracket):
		if qty <= 0 {
			at.mentorCount("swing_fill_unknown_qty")
			at.logWarnf("🧑‍🏫 mentor swing fill REFUSED — ArmID row %d filled but no contracts (FillQuantity=0, Contracts absent): cannot size its own close; never guessed", arm.RowID)
			return r, 0, mentorSwingUnproven, "filled but no attributable contracts"
		}
		return r, qty, mentorSwingLive, ""
	case closed:
		return r, qty, mentorSwingGone, "the position carrying signal " + r.SignalID + " is closed"
	}
	return r, qty, mentorSwingUnproven, "no open position or working bracket carries signal " + r.SignalID
}

// mentorSwingFinish clears the evaluator's swing state for an arm whose position
// is provably over (stop / target / close fill, or it never existed) and prunes
// the trader's registry entry. Idempotent; a receipt for another arm is a no-op.
func (at *AutoTrader) mentorSwingFinish(armID, why string) {
	if at == nil || at.mentorEval == nil || armID == "" {
		return
	}
	if at.mentorEval.SwingEnded(armID) {
		at.mentorCount("swing_state_ended")
		at.logInfof("🧑‍🏫 mentor swing %q is over (%s) — evaluator swing state cleared, no further BE / hold-close will be sent for it [N2]", armID, why)
	}
	at.mentorUnregisterLiveArm(armID)
	at.mentorSwingExitSentClear(armID)
}

// mentorSwingReconcile runs at the top of every evaluator tick (under the N11
// mutex): when the evaluator believes a swing is open, check the arm still owns
// a position and clear the state the moment it does not. This is the receipt
// path for a stop / target fill — the evaluator is pure in bars and never sees
// the broker, so without it a stopped-out swing stayed "open" until its hold
// deadline and then acted on a LATER trade (system check N2, trade 707 stopped
// out 10-28 22:58).
func (at *AutoTrader) mentorSwingReconcile() {
	if at == nil || at.mentorEval == nil {
		return
	}
	p := at.mentorEval.State.Swing.Pos
	if p == nil || p.ArmID == "" {
		return
	}
	arm, ok := at.mentorLiveArmFor(p.ArmID)
	if !ok {
		return // restart: the rebuild (mentorRebuildSwingArm) re-registers it
	}
	if _, _, own, why := at.mentorSwingOwnership(at.armedTrader(), arm); own == mentorSwingGone {
		at.mentorSwingFinish(p.ArmID, why)
	}
}

// mentorSwingApplyBE moves every live leg of the swing's position to the arm's
// entry through mentorMoveLegStop — signal-scoped, never-widen against the
// broker's own stop, and the F1 broker-book verdict on the next fresh book: the
// verification the intraday legs have and the old side-keyed moveStopWire (which
// compared against a stale per-side stop, N1) did not. applied=true once any leg
// holds the BE; ended=true when the BE was through the market and the leg was
// closed at market instead (the BE exit happened).
func (at *AutoTrader) mentorSwingApplyBE(nt *ntTrader.TCPTrader, r store.ArmedOrderDB, arm mentorLiveArm, qty float64) (applied, ended bool) {
	at.mentorExitMu.Lock()
	lp := at.mentorLivePos[r.SignalID]
	at.mentorExitMu.Unlock()
	var legs []*mentorLeg
	if lp != nil {
		scaled, _ := at.mentorScaledState(lp)
		exited := at.mentorExitedLegs(lp)
		for i := range lp.Legs {
			leg := &lp.Legs[i]
			if leg.SignalID == "" || leg.Qty <= 0 || exited[i] {
				continue
			}
			if leg.Wire == 1 && lp.Pos.Leg2 > 0 && scaled {
				at.mentorCount("stop_move_skipped_leg_closed") // leg 1's partial is filled — never move a gone leg
				continue
			}
			legs = append(legs, leg)
		}
	} else {
		// No live position registered for the signal: one leg-less move, its
		// belief the arm row's recorded (entry) stop.
		legs = []*mentorLeg{{SignalID: r.SignalID, Qty: int(qty), Stop: r.StopPx, Final: true}}
	}
	side := arm.Side
	for _, leg := range legs {
		err := at.mentorMoveLegStop(nt, side, leg, arm.Entry)
		switch {
		case errors.Is(err, errMentorStopThroughMarket):
			return false, true
		case errors.Is(err, errMentorStopBrokerTighter):
			applied = true // the broker is already past BE
			continue
		case !mentorMoveApplied(err):
			if err != nil && !mentorMoveErrAlreadyLogged(err) {
				at.logWarnf("🧑‍🏫 mentor swing BE %s leg %d move FAILED: %v", leg.SignalID, leg.Wire, err)
			}
			continue
		}
		leg.Stop = arm.Entry
		applied = true
	}
	if applied && lp != nil {
		at.mentorExitMu.Lock()
		lp.Pos.ArmedBE = true
		lp.Pos.Stop = arm.Entry
		at.mentorExitMu.Unlock()
	}
	return applied, false
}

// ── hold-close: the marketable limit, one position, a bounded fallback ──────

// mentorSwingExitSent records, per arm, when the swing's marketable-limit exit
// was sent — so the NEXT closed 5m bar, finding the position still open, sends
// ONE market flatten instead of another limit (mirrors X8's grace + fallback).
type mentorSwingExitRecord struct {
	limitAt time.Time
}

func (at *AutoTrader) mentorSwingExitSentGet(armID string) (mentorSwingExitRecord, bool) {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	rec, ok := at.mentorSwingExitSent[armID]
	return rec, ok
}

func (at *AutoTrader) mentorSwingExitSentSet(armID string, rec mentorSwingExitRecord) {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorSwingExitSent == nil {
		at.mentorSwingExitSent = map[string]mentorSwingExitRecord{}
	}
	at.mentorSwingExitSent[armID] = rec
}

func (at *AutoTrader) mentorSwingExitSentClear(armID string) {
	at.mentorExitMu.Lock()
	delete(at.mentorSwingExitSent, armID)
	at.mentorExitMu.Unlock()
}

// mentorSwingSendClose sends the swing's hold-close for ONE position. First
// attempt: the X8 marketable limit (close_position with limit_price through the
// market) — the AddOn submits one "-lx" limit whose fill cancels only THIS
// position's bracket, instead of a market Flatten that cancels every resting
// arm on the account (#15). No fresh price or a send error takes the market
// close (a close always wins over preserving an arm). A later attempt that finds
// the position STILL open past the grace sends the market close once per bar.
func (at *AutoTrader) mentorSwingSendClose(nt *ntTrader.TCPTrader, armID, side string, qty float64) error {
	if rec, ok := at.mentorSwingExitSentGet(armID); ok {
		if time.Since(rec.limitAt) < mentorX8LimitFallbackAfter {
			return nil // the limit is still in flight
		}
		at.mentorCount("swing_close_market_fallback")
		at.logWarnf("🧑‍🏫 mentor swing hold-close: the marketable limit for %q left the position open past %s — market close", armID, mentorX8LimitFallbackAfter)
		return mentorCloseLegAtMarketWire(at, nt, side, int(qty))
	}
	if limit := at.mentorMarketableExitLimit(side); limit > 0 {
		err := mentorX8LimitWire(at, nt, side, limit)
		if err == nil {
			at.mentorSwingExitSentSet(armID, mentorSwingExitRecord{limitAt: time.Now()})
			at.mentorCount("swing_close_limit_sent")
			at.logInfof("🧑‍🏫 mentor swing hold-close: marketable limit exit %s @ %.2f for %q (only this position's bracket is cancelled; other resting arms stay) [#15]", side, limit, armID)
			return nil
		}
		at.logWarnf("🧑‍🏫 mentor swing hold-close: limit exit failed (%v) — market close instead", err)
	}
	return mentorCloseLegAtMarketWire(at, nt, side, int(qty))
}

// mentorSwingReportBE feeds the executor's BE receipt to the evaluator.
func (at *AutoTrader) mentorSwingReportBE(armID string) {
	if at != nil && at.mentorEval != nil {
		at.mentorEval.SwingBEApplied(armID)
	}
}
