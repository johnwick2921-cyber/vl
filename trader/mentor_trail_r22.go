package trader

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	ntwire "vl/provider/ninjatrader"
	ntTrader "vl/trader/ninjatrader"
)

// ── RELEASE #22 MENTOR EXIT (owner ruling R3 · audit P1-2 / P1-3 / P2-3 · F-6) ─
//
// Owner, verbatim (10-14 02:1x CT): "why not trail 1:1 as mentor mode set up …
// fix all now". Ruling R3: after leg 1's +1R partial FILLS (NT8-confirmed,
// never belief) the runner's stop moves to break-even, then it trails behind
// each CLOSED candle, like the mentor.
//
// Course rules this file implements (quoted from ~/mm-course, the Sep-26 course
// wins over the extras):
//   - Scale out at 1:1, then trail: "Khi anh chị trade [tới] risk-reward 1-1, CỨ
//     BÁN BỚT, xong rồi mình GỒNG lệnh" [D2.4 p1 @16:17]; normal case "move stop
//     THEO TỪNG CÂY NẾN ĐÃ ĐÓNG XONG" — behind every CLOSED candle [D2.4 p1 @07:01].
//   - Which candle: the words name no frame; the lesson demonstrates the
//     1-MINUTE chart [D2.4 p1 @08:35, seen — "(Sep 2026) · 1 · CME"] and allows
//     tighter 30s/45s [@09:08]. The trail_tf knob (1m default) keeps that frame;
//     the drive runs on closed 1m candles.
//   - Wick or body: the candle's LOW for a long / HIGH for a short — "đặt ở DƯỚI
//     ĐÁY cây nến … tới khi nào có một cây nến QUÉT CÁI ĐÁY thì … OUT" [X8
//     @15:40–17:13]: the low (a wick extreme), not the body. 1 tick BEYOND it
//     (item-7 ruling), never exactly at it.
//   - Break-even = the entry: "move the stop up to break-even (10)" — the entry
//     of 10 [D1.2 p1 @10:31–15:20]. The course is SILENT on ± ticks: the
//     conservative default here is the entry EXACTLY when it is on the tick grid
//     (a stop-limit fill always is); an averaged off-grid fill rounds TOWARD
//     PROFIT so the stop never sits a fraction of a tick on the loss side
//     (MATH-EXECUTOR P3-1).
//   - Never widen: "kéo stop loss ra xa" is on the do-not list [D2.3 p1 @18:08]
//     — judged here against the BROKER's own stop, not the bot's belief (P2-3).

const (
	mentorTrailModeCandle = "candle"
	mentorTrailModeOff    = "off"
)

// mentorTrailModeBadWarned dedups the bad-value WARN per (trader, raw value);
// the counter still records every resolution of a bad value.
var mentorTrailModeBadWarned sync.Map

// mentorTrailMode resolves the mentor_trail_mode knob: ""/candle → candle (R3,
// the shipped default), off → the pre-R3 rule. Any other value fails closed to
// candle (the shipped default: the runner still reaches break-even) — logged
// once and counted on every resolution.
func (at *AutoTrader) mentorTrailMode() string {
	raw := ""
	if at != nil && at.config.StrategyConfig != nil {
		raw = at.config.StrategyConfig.RiskControl.MentorTrailMode
	}
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "", mentorTrailModeCandle:
		return mentorTrailModeCandle
	case mentorTrailModeOff:
		return mentorTrailModeOff
	default:
		at.mentorCount("trail_mode_bad_value")
		if _, seen := mentorTrailModeBadWarned.LoadOrStore(at.id+"|"+raw, true); !seen {
			at.logWarnf("🧑‍🏫 mentor_trail_mode %q invalid (candle/off) — failing closed to candle, the shipped default (R3)", raw)
		}
		return mentorTrailModeCandle
	}
}

// mentorGridEps keeps an ON-GRID price fixed under Floor/Ceil (the same 1e-6 of
// a tick the wire uses in ninjatrader.WireStop).
const mentorGridEps = 1e-6

// mentorStopTighter reports whether stop a is TIGHTER (closer to the position,
// less open risk) than stop b: long → higher, short → lower.
func mentorStopTighter(side string, a, b float64) bool {
	if strings.EqualFold(side, "short") {
		return a < b
	}
	return a > b
}

// mentorBEStopPrice is the runner's break-even stop: the entry exactly when on
// the tick grid; an off-grid (averaged) entry rounds TOWARD PROFIT — long up,
// short down — so a BE stop never books a fraction of a tick of loss.
func mentorBEStopPrice(side string, entry, tick float64) float64 {
	if tick <= 0 {
		return entry
	}
	if strings.EqualFold(side, "short") {
		return math.Floor(entry/tick+mentorGridEps) * tick
	}
	return math.Ceil(entry/tick-mentorGridEps) * tick
}

// mentorCandleTrailStop is the candle trail for ONE closed candle: 1 tick
// beyond its low (long) / high (short), on the grid and rounded AWAY from the
// market (long down, short up), so rounding never pulls it inside the rule.
func mentorCandleTrailStop(side string, h, l, tick float64) float64 {
	if strings.EqualFold(side, "short") {
		v := h + tick
		if tick <= 0 {
			return v
		}
		return math.Ceil(v/tick-mentorGridEps) * tick
	}
	v := l - tick
	if tick <= 0 {
		return v
	}
	return math.Floor(v/tick+mentorGridEps) * tick
}

// mentorPartialTPPrice rounds the ISB candle-3 partial's leg-1 TP to the tick
// in the PROTECTIVE direction for a take-profit: toward the entry (long down,
// short up). The limit then never asks for more than the candle close the
// course says to sell at, so it fills at least as readily as the rule.
func mentorPartialTPPrice(side string, tp, tick float64) float64 {
	if tick <= 0 {
		return tp
	}
	if strings.EqualFold(side, "short") {
		return math.Ceil(tp/tick-mentorGridEps) * tick
	}
	return math.Floor(tp/tick+mentorGridEps) * tick
}

// ── BROKER-JUDGED NEVER-WIDEN (P2-3) ────────────────────────────────────────

const (
	mentorBrokerStopUnknown = iota // no fresh broker book — nothing can be judged
	mentorBrokerStopNone           // fresh book, no standing stop for that leg (closed, dying, or never placed)
	mentorBrokerStopLive           // fresh book, a standing stop at a known price
)

// Sentinels mentorMoveLegStop returns when it SENDS NOTHING. Each one has
// already been logged AND counted where it was decided, so callers skip their
// own WARN (mentorMoveErrAlreadyLogged).
var (
	errMentorStopBookUnknown   = errors.New("mentor stop move: no fresh broker book to judge never-widen against")
	errMentorLegClosed         = errors.New("mentor stop move: no live broker stop for the leg (closed or protection-net owned)")
	errMentorStopBrokerTighter = errors.New("mentor stop move: the broker's stop is already tighter")
	errMentorStopAtBroker      = errors.New("mentor stop move: the broker already holds this stop")
)

// mentorMoveApplied reports whether the caller may write the new stop into its
// belief: the frame was SENT (pending the F1 verdict) or the broker already
// holds that exact stop.
func mentorMoveApplied(err error) bool {
	return err == nil || errors.Is(err, errMentorStopAtBroker)
}

// mentorMoveErrAlreadyLogged reports whether a mentorMoveLegStop error was
// already logged + counted at its decision site.
func mentorMoveErrAlreadyLogged(err error) bool {
	return errors.Is(err, errMentorStopManual) ||
		errors.Is(err, errMentorStopBookUnknown) ||
		errors.Is(err, errMentorLegClosed) ||
		errors.Is(err, errMentorStopBrokerTighter) ||
		errors.Is(err, errMentorStopAtBroker)
}

// mentorBrokerLegStopRead reads the broker's OWN stop for one wire leg from the
// latest order book: wire 1 → "<signal>-sl", wire 2 → "<signal>-sl2", wire 0 →
// every leg (the TIGHTEST counts: moving them together must widen none). A
// dying stop (cancel in flight — e.g. the OCO pulling leg 1's stop after its TP
// filled) reads as NONE: the leg is closing and is never moved. A seam so the
// pure rule pins can mirror a broker that applied every move; production reads
// TCPTrader.OrderSnapshotBook through mentorStopBook.
var mentorBrokerLegStopRead = func(at *AutoTrader, nt *ntTrader.TCPTrader, side string, leg *mentorLeg) (float64, int) {
	if leg == nil {
		return 0, mentorBrokerStopUnknown
	}
	book, recvAt, ok := mentorStopBook(nt)
	if !ok || recvAt.IsZero() || time.Since(recvAt) > snapshotMaxAge() {
		return 0, mentorBrokerStopUnknown
	}
	price, found := 0.0, false
	for _, o := range mentorBrokerStopOrders(book, leg.SignalID, leg.Wire) {
		switch o.Liveness() {
		case ntwire.LivenessTerminal:
			continue
		case ntwire.LivenessDying:
			return 0, mentorBrokerStopNone
		}
		if o.StopPrice <= 0 {
			continue
		}
		if !found || mentorStopTighter(side, o.StopPrice, price) {
			price, found = o.StopPrice, true
		}
	}
	if !found {
		return 0, mentorBrokerStopNone
	}
	return price, mentorBrokerStopLive
}

// mentorLegKnownClosed reports whether a wire leg is KNOWN closed: the position
// is no longer under exit management (flat-unregistered), it is leg 1 of a
// split whose partial is CONFIRMED filled (Scaled — the broker TP receipt or
// the I7 snapshot), or the broker's positions show the side flat. A read error
// proves nothing (false, never a confident "closed").
func (at *AutoTrader) mentorLegKnownClosed(signalID, side string, wireLeg int) (bool, string) {
	if at == nil || signalID == "" {
		return false, ""
	}
	at.mentorExitMu.Lock()
	p := at.mentorLivePos[signalID]
	scaled, split := false, false
	if p != nil {
		scaled = p.Pos.Scaled
		split = p.Legs[1].Qty > 0 && p.Legs[1].SignalID != ""
	}
	at.mentorExitMu.Unlock()
	if p == nil {
		return true, "the position is no longer under exit management"
	}
	if wireLeg == 1 && split && scaled {
		return true, "leg 1's partial is already filled at NT8"
	}
	if sides, ok := mentorDriveOpenSides(at); ok && !sides[strings.ToLower(side)] {
		return true, "the account is flat on that side"
	}
	return false, ""
}

// mentorJudgeAgainstBroker is the P2-3 half of never-widen: before ANY mentor
// stop move is sent, read the broker's own stop for that leg and
//   - no fresh book          → REFUSE (fail-closed: nothing can be judged; the
//     broker keeps the stop it has, which cannot be a widen);
//   - no live stop for leg   → SEND NOTHING (F-6: a closed leg, or a stop the
//     protection net owns — the AddOn would refuse "no live bracket" anyway);
//   - broker at the new stop → nothing to send (belief = newStop);
//   - broker tighter         → REFUSE, keep the broker's stop, correct the belief
//     (the owner moved it by hand — a drive move must never loosen it).
//
// Every branch that sends nothing is logged AND counted here.
func (at *AutoTrader) mentorJudgeAgainstBroker(nt *ntTrader.TCPTrader, side string, leg *mentorLeg, newStop float64) error {
	price, state := mentorBrokerLegStopRead(at, nt, side, leg)
	switch state {
	case mentorBrokerStopUnknown:
		at.mentorCount("stop_move_refused_book_unknown")
		at.logWarnf("🧑‍🏫 mentor stop move %s leg %d → %.2f REFUSED — no fresh broker book to judge never-widen against (belief %.2f); nothing sent, re-judged next candle [P2-3]",
			leg.SignalID, leg.Wire, newStop, leg.Stop)
		return errMentorStopBookUnknown
	case mentorBrokerStopNone:
		if closed, why := at.mentorLegKnownClosed(leg.SignalID, side, leg.Wire); closed {
			at.mentorCount("stop_move_skipped_leg_closed")
			at.logInfof("🧑‍🏫 mentor stop move %s leg %d → %.2f NOT sent — %s (F-6)", leg.SignalID, leg.Wire, newStop, why)
			return errMentorLegClosed
		}
		at.mentorCount("stop_move_skipped_no_broker_stop")
		at.logWarnf("🧑‍🏫 mentor stop move %s leg %d → %.2f NOT sent — the broker book has no live stop for that leg; leaving to the protection net (F4)",
			leg.SignalID, leg.Wire, newStop)
		return errMentorLegClosed
	}
	halfTick := at.mentorInstrumentTick() / 2
	if math.Abs(newStop-price) <= halfTick {
		at.mentorCount("stop_move_already_at_broker")
		leg.Stop = newStop
		return errMentorStopAtBroker
	}
	if mentorStopTighter(side, price, newStop) {
		at.mentorCount("widen_refused_broker")
		at.logWarnf("🧑‍🏫 mentor stop move %s leg %d → %.2f REFUSED — the broker's stop %.2f is already tighter (moved by hand?); kept, belief corrected, nothing sent [P2-3]",
			leg.SignalID, leg.Wire, newStop, price)
		leg.Stop = price
		return errMentorStopBrokerTighter
	}
	return nil
}

// ── R3: BE AFTER THE CONFIRMED +1R PARTIAL, THEN THE CANDLE TRAIL ───────────

// mentorExitDriveCandle (mentor_trail_mode=candle) drives a SPLIT position's
// runner on one closed candle (h/l). Three stages, each re-derivable from the
// ledger + the broker after a restart (mentorBuildLivePosFromBook: Scaled =
// the broker qty is down to the runner, ArmedBE = the broker's runner stop is
// at/through the entry):
//
//	0. leg 1's partial NOT confirmed filled → nothing moves. Never a price
//	   belief: only the broker's TP receipt (mentorMarkLeg1Scaled) or the I7
//	   broker-snapshot qty drop sets Scaled.
//	1. confirmed, BE not armed → the runner's stop goes to break-even; that
//	   candle only arms BE.
//	2. BE armed → on every closed candle that OPENED after the confirmation
//	   (I6), the runner's stop goes 1 tick beyond that candle's low (long) /
//	   high (short) — only when that is TIGHTER; a lower low (long) / higher
//	   high (short) moves nothing. trail_tf=off stops at stage 1.
//
// Every move goes through mentorMoveLegStop (belief + broker never-widen, the
// real move_stop frame, the F1 verdict on the next fresh book).
func (at *AutoTrader) mentorExitDriveCandle(nt *ntTrader.TCPTrader, p *mentorLivePos, c, h, l float64, scaledNow, scaledBefore, trail bool) {
	pos := &p.Pos
	runner := &p.Legs[1]
	if !runner.Final || runner.Qty <= 0 || runner.SignalID == "" {
		return
	}
	if !scaledNow {
		// stage 0. CX-4 (knob mentor_be_before_partial, default OFF = R3):
		// BE at half-way to +1R before the partial [D1.2 p1 @12:41–13:12].
		at.mentorBEBeforePartialStep(nt, p, c)
		return
	}
	side := pos.Side
	tick := at.mentorInstrumentTick()
	if !pos.ArmedBE {
		be := mentorBEStopPrice(side, pos.Entry, tick)
		// A break-even stop is only a stop while price is on the PROFIT side of
		// it: with the close back through the entry, a sell stop above (buy
		// stop below) the market is refused by NT8 and F1 would escalate a
		// P0 per candle. Wait — the original stop still protects — and send
		// BE on the first close that is past it by at least a tick. Logged
		// once per position, counted every candle it waits.
		if (side == "long" && c < be+tick) || (side != "long" && c > be-tick) {
			at.mentorCount("be_after_partial_waiting_price")
			if !p.BEWaitLogged {
				p.BEWaitLogged = true
				at.logInfof("🧑‍🏫 mentor BE after the +1R partial: %s close %.2f is not past break-even %.2f — the runner keeps its stop %.2f; BE goes out on the first close past it", runner.SignalID, c, be, runner.Stop)
			}
			return
		}
		err := at.mentorMoveLegStop(nt, side, runner, be)
		if errors.Is(err, errMentorStopBrokerTighter) {
			// The broker is already past break-even (moved by hand, or a stop
			// the restart rebuild read) — BE is satisfied; the trail runs on.
			pos.ArmedBE = true
			pos.Stop = runner.Stop
			at.mentorCount("be_after_partial_broker_tighter")
			return
		}
		if !mentorMoveApplied(err) {
			if err != nil && !mentorMoveErrAlreadyLogged(err) {
				at.logWarnf("🧑‍🏫 mentor BE after the +1R partial: %s leg %d move FAILED: %v", runner.SignalID, runner.Wire, err)
			}
			return // re-tried next candle (a FAILED send never arms BE)
		}
		runner.Stop = be
		pos.Stop = be
		pos.ArmedBE = true
		at.mentorCount("be_after_partial")
		how := "SENT — awaiting the NT8 verdict"
		if err != nil {
			how = "already at the broker — nothing sent"
		}
		at.logInfof("🧑‍🏫 mentor BE after the +1R partial: %s runner (leg %d) stop → %.2f (entry %.2f) — %s [R3; D2.4 p1 @16:17]",
			runner.SignalID, runner.Wire, be, pos.Entry, how)
		return
	}
	if !trail || !scaledBefore {
		return
	}
	cand := mentorCandleTrailStop(side, h, l, tick)
	if !mentorStopTighter(side, cand, runner.Stop) {
		return // a lower low (long) / higher high (short): never a widen, nothing to send
	}
	err := at.mentorMoveLegStop(nt, side, runner, cand)
	if !mentorMoveApplied(err) {
		if err != nil && !mentorMoveErrAlreadyLogged(err) {
			at.logWarnf("🧑‍🏫 mentor candle trail: %s leg %d move FAILED: %v", runner.SignalID, runner.Wire, err)
		}
		pos.Stop = runner.Stop // a broker-tighter refusal corrected the runner's belief
		return
	}
	runner.Stop = cand
	pos.Stop = cand
	at.mentorCount("trail_candle_moved")
	extreme, word := l, "low"
	if strings.EqualFold(side, "short") {
		extreme, word = h, "high"
	}
	frame := "1m"
	if p.TrailTFMin > 1 {
		frame = fmt.Sprintf("%dm", p.TrailTFMin)
		at.mentorCount("trail_candle_moved_2m")
	}
	at.logInfof("🧑‍🏫 mentor candle trail: %s runner (leg %d) stop → %.2f — 1 tick beyond the closed %s candle's %s %.2f [R3; D2.4 p1 @07:01; X8 @15:40, @18:02 the frame the entry was read on]",
		runner.SignalID, runner.Wire, cand, frame, word, extreme)
}

// ── P1-2: THE ISB CANDLE-3 PARTIAL — R1 (release #30 fold) ─────────────────

// mentorISBPartialStep resolves the ISB candle-3 partial ONCE, on the fill
// candle's close [D1.4 p1 @11:59–13:28 "phải BÁN BỚT. Bắt buộc"]:
//   - leg 1's +1R TP always rests at NT8 from the fill (R1 — never held out);
//   - when the fill candle traded THROUGH +1R, the +1R TP already filled
//     inside the candle (the 1:1 rule) — nothing to do at the close;
//   - when the close is in profit SHORT of +1R, leg 1 is sold at MARKET at that
//     close (a market exit, never a resting limit) [R1];
//   - a losing close moves nothing; the +1R TP keeps resting (fail-safe).
func (at *AutoTrader) mentorISBPartialStep(nt *ntTrader.TCPTrader, p *mentorLivePos, c, h, l float64) float64 {
	pos := &p.Pos
	tp := mentorISBPartialTP(*pos, c, h, l)
	if tp == 0 {
		// A losing close OR a close past +1R: nothing moves; the +1R TP keeps
		// resting at NT8 (a past-+1R candle already filled it inside the candle).
		at.mentorCount("isb_partial_no_sale")
		at.logInfof("🧑‍🏫 mentor ISB candle-3 partial: fill candle close %.2f — leg 1 keeps its +1R TP at NT8 (in-profit short of +1R is the only market-sell case) [D2.2 p3 @12:13]", c)
		return 0
	}
	if !mentorISBC3PartialAnyProfit(at.mentorRiskControl()) {
		// CX-5 (knob false): "bán bớt ở 1:1" [D2.2 p3 @12:13] — leg 1 keeps its
		// resting +1R TP; no market sell at the candle-3 close.
		at.mentorCount("isb_partial_wait_1r")
		at.logInfof("🧑‍🏫 mentor ISB candle-3 partial: close %.2f in profit but mentor_isb_c3_partial_any_profit OFF — leg 1 keeps its +1R TP (D2.2 p3 @12:13 \"bán bớt ở 1:1\"); nothing sold", c)
		return 0
	}
	return at.mentorSellISBLeg1Market(nt, p, tp)
}

// mentorSellISBLeg1Market (R1) market-sells leg 1 at the fill candle's close.
// Refused (logged + counted, nothing sent) when the position is ONE bracket
// (a partial market close cannot target a leg the wire does not carry), or when
// the close is not in profit after the exit-side guard. The leg-1 TP belief is
// written only on the NT8 fill receipt — a failed send or a refused close
// leaves leg 1's +1R TP resting (fail-safe).
func (at *AutoTrader) mentorSellISBLeg1Market(nt *ntTrader.TCPTrader, p *mentorLivePos, tp float64) float64 {
	pos := &p.Pos
	leg := &p.Legs[0]
	long := pos.Side == "long"
	if leg.Wire != 1 || leg.SignalID == "" || leg.Qty <= 0 || p.Legs[1].Qty <= 0 {
		at.mentorCount("isb_partial_not_split")
		at.logWarnf("🧑‍🏫 mentor ISB candle-3 partial at %.2f NOT sold — one bracket on the wire (no separate leg 1); a partial market close cannot target a leg the wire does not carry, so its +1R TP stays", tp)
		return 0
	}
	if (long && tp <= pos.Entry) || (!long && tp >= pos.Entry) {
		at.mentorCount("isb_partial_not_in_profit")
		at.logInfof("🧑‍🏫 mentor ISB candle-3 partial: close %.2f is not in profit (entry %.2f) — leg 1 keeps its +1R TP", tp, pos.Entry)
		return 0
	}
	if closeLegWire == nil {
		at.mentorCount("close_leg_unwired")
		at.logWarnf("🧑‍🏫 mentor ISB leg1 market close unwired (close %.2f) — logged, NOT sent; leg 1 keeps its +1R TP", tp)
		return 0
	}
	side := strings.ToLower(pos.Side)
	if side != "long" && side != "short" {
		at.mentorCount("close_leg_bad_side")
		at.logWarnf("🧑‍🏫 mentor ISB leg1 market close REFUSED — unknown side %q", pos.Side)
		return 0
	}
	if err := closeLegWire(nt, leg.SignalID, 1, side, leg.Qty); err != nil {
		at.mentorCount("close_leg_failed")
		at.logErrorf("🧑‍🏫 mentor ISB leg1 market close FAILED for %s (close %.2f): %v — leg 1 keeps its +1R TP", leg.SignalID, tp, err)
		return 0
	}
	at.mentorCount("close_leg_sent")
	at.logInfof("🧑‍🏫 mentor ISB leg1 market close SENT: %s leg 1 (%d contracts) at the fill candle's close %.2f — awaiting the NT8 fill; the +1R TP is cancelled by the AddOn on the same frame [D1.4 p1 @11:59–13:28]",
		leg.SignalID, leg.Qty, tp)
	return tp
}

// mentorVerifyPendingTPModifies renders the NT8 verdict on every SENT leg-1 TP
// modify against the broker's own book — the AddOn sends no success ack, so a
// nil send proves only that the frame LEFT. On the first fresh snapshot newer
// than send+2s:
//
//	"<signal>-tp" within half a tick of the want → CONFIRMED (belief written)
//	"<signal>-tp" at another price             → NOT APPLIED (ERROR + P1; the
//	                                              belief takes the broker's TP)
//	no "<signal>-tp" and leg 1 already filled  → recorded, ignored (F-6)
//	no "<signal>-tp" otherwise                 → WARN, counted
//	book older than snapshotMaxAge             → no verdict yet, WARN once
func (at *AutoTrader) mentorVerifyPendingTPModifies(nt *ntTrader.TCPTrader) {
	if at == nil {
		return
	}
	type pend struct {
		p      *mentorLivePos
		sig    string
		want   float64
		sentAt time.Time
		scaled bool
	}
	at.mentorExitMu.Lock()
	var list []pend
	for _, p := range at.mentorLivePos {
		if p != nil && p.TPModWant > 0 {
			list = append(list, pend{p, p.Legs[0].SignalID, p.TPModWant, p.TPModSentAt, p.Pos.Scaled})
		}
	}
	at.mentorExitMu.Unlock()
	if len(list) == 0 {
		return
	}
	book, recvAt, ok := mentorStopBook(nt)
	if !ok || recvAt.IsZero() {
		return
	}
	now := time.Now()
	stale := now.Sub(recvAt) > snapshotMaxAge()
	halfTick := at.mentorInstrumentTick() / 2
	clear := func(p *mentorLivePos, tpBelief float64) {
		at.mentorExitMu.Lock()
		p.TPModWant = 0
		p.TPModSentAt = time.Time{}
		if tpBelief > 0 {
			p.Legs[0].TP = tpBelief
		}
		at.mentorExitMu.Unlock()
	}
	for _, e := range list {
		if !recvAt.After(e.sentAt.Add(2 * time.Second)) {
			continue
		}
		if stale {
			at.mentorExitMu.Lock()
			warned := e.p.TPModStaleWarned
			e.p.TPModStaleWarned = true
			at.mentorExitMu.Unlock()
			if !warned {
				at.mentorCount("modify_bracket_stale_book")
				at.logWarnf("🧑‍🏫 mentor modify_bracket verify: stale book (%s old) — no verdict yet for %s leg 1 TP %.2f", now.Sub(recvAt).Round(time.Second), e.sig, e.want)
			}
			continue
		}
		var tpOrder *ntwire.NT8Order
		for i := range book.Orders {
			o := book.Orders[i]
			if o.Name == e.sig+"-tp" && o.Liveness() != ntwire.LivenessTerminal {
				tpOrder = &book.Orders[i]
				break
			}
		}
		switch {
		case tpOrder == nil && e.scaled:
			at.mentorCount("modify_bracket_leg_closed")
			at.logInfof("🧑‍🏫 mentor modify_bracket verify: %s leg 1 already filled — late verdict recorded, ignored (F-6)", e.sig)
			clear(e.p, 0)
		case tpOrder == nil:
			at.mentorCount("modify_bracket_no_order")
			at.logWarnf("🧑‍🏫 mentor modify_bracket verify: NO live leg-1 TP order for %s (wanted %.2f) — nothing re-sent", e.sig, e.want)
			clear(e.p, 0)
		case math.Abs(tpOrder.LimitPrice-e.want) <= halfTick:
			at.mentorCount("modify_bracket_confirmed")
			at.logInfof("🧑‍🏫 modify_bracket CONFIRMED at NT8: %s leg 1 TP %.2f (ISB candle-3 partial)", e.sig, e.want)
			clear(e.p, e.want)
		default:
			at.mentorCount("modify_bracket_not_applied")
			at.logErrorf("🚨 MODIFY NOT APPLIED at NT8: %s leg 1 TP want %.2f broker %.2f — leg 1 keeps the broker's TP; nothing re-sent", e.sig, e.want, tpOrder.LimitPrice)
			at.emitAlert("P1", "modify-bracket-not-applied", "modify-bracket:"+e.sig,
				"ISB partial not applied at NT8",
				e.sig+": leg 1 TP not moved to the candle-3 close — the +1R TP still rests")
			clear(e.p, tpOrder.LimitPrice)
		}
	}
}
