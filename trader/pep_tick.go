package trader

import (
	"errors"
	"fmt"
	"math"
	"time"

	"vl/kernel"
	"vl/kernel/pep"
	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── PEP: THE DRIVER ─────────────────────────────────────────────────────────

// pepBars1mDepth is the 1m history the pep evaluator reads each pass. Style E's
// EMA is carried ACROSS sessions (the owner's PDF) and the ring is shared with
// the mentor; 1500 1m bars cover the 17:00 CT reopen to the 15:00 flat with
// slack, far more than the EMA's 54-minute 99.8% warm-up needs.
const pepBars1mDepth = 1500

// pepMaxSlipOfStop bounds how far the live price may be from the evaluator's
// reference entry (a fraction of the stop distance) before the entry is refused
// as chasing: a market that already moved that far since the signal is not the
// signal that was evaluated. Safety rail (not a strategy number); named in the
// guide.
const pepMaxSlipOfStop = 0.5

// pepRRTolerance is how much the ROUNDED bracket's reward:risk may fall below
// the intended one (stop rounded OUTWARD, target to the nearest tick) before the
// entry is refused. Style A is exactly 1:1, so a hard "R:R >= 1" floor would
// refuse every trade on rounding noise; the rounding may cost at most this share.
const pepRRTolerance = 0.10

// pepFlatAlertAttempts: a position still open this many flat attempts (one per
// beat) after the flat time is told to the owner as a P0.
const pepFlatAlertAttempts = 3

// pepFlatVerifyWindow is how long past the flat time an EMPTY book is still
// checked against the broker's.
const pepFlatVerifyWindow = 15 * time.Minute

// pepClosedBars drops the FORMING tail bar (the NT8 cache's newest bar is
// usually still building). The pep evaluator additionally ignores any bar whose
// close is not before `now`.
func pepClosedBars(bars []market.Kline) []market.Kline {
	if n := len(bars); n > 0 && !bars[n-1].Final {
		return bars[:n-1]
	}
	return bars
}

// noteLiveBarsForPepPass is the sink's pep half: a FINAL 1m frame of this
// trader's own instrument kicks the event loop (event-driven, not scan-driven).
func (at *AutoTrader) noteLiveBarsForPepPass(symbol, tf string, bars []ntwire.Bar) {
	if at == nil || len(bars) == 0 || !at.pepEnabled() || tf != "1m" {
		return
	}
	root := instrumentRoot(symbol)
	if root == "" || root != instrumentRoot(at.futuresSymbol()) {
		return
	}
	for _, b := range bars {
		if b.Final {
			if l := at.armedEvent.Load(); l != nil {
				l.poke()
			}
			return
		}
	}
}

// pepEventPassAt runs one pep event pass (the event loop calls it; <= 1/s by
// the loop's own gap). Returns whether it evaluated.
func (at *AutoTrader) pepEventPassAt(now time.Time) bool {
	if at == nil || !at.pepEnabled() || market.FuturesBarsProvider == nil {
		return false
	}
	// at.futuresSymbol(), never a literal: a pep trader on ES reads ES.
	bars := pepClosedBars(market.FuturesBarsProvider(at.futuresSymbol(), "1m", pepBars1mDepth))
	if len(bars) == 0 {
		return false
	}
	return at.pepEvalOnce(bars, now)
}

// pepTick is the 2-minute cycle's fallback (runCycle): the same pass, on the
// same bars, behind the same mutex.
func (at *AutoTrader) pepTick() {
	if !at.pepEnabled() {
		return
	}
	at.pepEventPassAt(time.Now())
}

// pepBlock counts and logs ONE fail-closed refusal per change of (code, class)
// so a block that repeats every bar is one event, not one per bar.
func (at *AutoTrader) pepBlock(code, detail string) {
	key := code
	if at.pep.lastBlock == key {
		return
	}
	at.pep.lastBlock = key
	telemetry.IncGateBlock(at.id, code)
	at.logWarnf("⏸ pep: %s - %s. Exits and protection continue; pep waits for a clean evaluation.", code, detail)
}

// pepEvalOnce is the whole pass: durable state in, one pure Evaluate, one intent
// handled. It holds pep.mu so the event loop and the cycle never interleave.
func (at *AutoTrader) pepEvalOnce(bars []market.Kline, now time.Time) bool {
	at.pep.mu.Lock()
	defer at.pep.mu.Unlock()
	if !at.pepEnabled() || at.store == nil {
		return false
	}
	day := pepDayNow(now)
	if day == "" {
		at.pepBlock(pep.CodeTZUnavailable, "America/Chicago zoneinfo unavailable")
		return false
	}
	// THE DAY'S TRADE COUNT IS READ FROM THE DATABASE EVERY PASS (a restart or a
	// reload must never reset it). An unreadable record is a block: fail closed.
	dayState, derr := at.store.PepDay().Day(at.id, day)
	if derr != nil {
		at.pepBlock("pep_day_unreadable", fmt.Sprintf("the one-trade-per-day record could not be read (%v)", derr))
		return false
	}
	positions, perr := at.store.Position().GetOpenPositions(at.id)
	if perr != nil {
		at.pepBlock("pep_positions_unreadable", fmt.Sprintf("the open-position read failed (%v)", perr))
		return false
	}
	cfg := at.pepConfig()
	st := pep.State{Day: day, Trades: dayState.Counted, PositionOpen: len(positions) > 0}
	if len(positions) > 0 {
		// a position from an earlier CT day reads as carried over: the evaluator
		// answers with the flat intent, whatever else is wrong
		if d := pepDayNow(time.UnixMilli(positions[0].EntryTime)); d != "" && d != day {
			st.Day = d
		}
	} else if last := dayState.Last; last != nil && last.State == store.PepDaySending &&
		now.UnixMilli()-last.UpdatedAt < int64(2*time.Minute/time.Millisecond) {
		// the entry reached the wire a moment ago and the position row is not
		// materialized yet: treat it as open (never a second entry on top of it)
		st.PositionOpen = true
	}
	res := pep.Evaluate(bars, now.UnixMilli(), cfg, st)
	at.pepNoteTruth(res, cfg, dayState, len(positions), now)

	for _, in := range res.Intents {
		switch in.Kind {
		case pep.KindFlat:
			at.pepFlatLocked(now, in.Reason)
		case pep.KindEnter:
			at.pepSendEntry(in, now)
		}
	}
	if len(res.Intents) > 0 {
		at.pep.lastBlock = ""
		return true
	}
	blocked := false
	for _, r := range res.Refusals {
		if r.Severity == pep.SevBlock {
			at.pepBlock(r.Code, r.Detail)
			blocked = true
			break
		}
	}
	if !blocked {
		at.pep.lastBlock = ""
	}
	return true
}

// pepWire is the bracket pep sends, judged on the prices the wire carries.
type pepWire struct {
	Live, Stop, Target float64
	Class, Reason      string // non-empty Class = refused
}

// pepWireCheck builds and judges the entry's bracket on the WIRE-ROUNDED prices
// (the same rounding the sender applies: the stop AWAY from the entry, the
// target to the nearest tick - ntTrader.WireBracket). live is the price a market
// order would fill near; the bracket keeps the evaluator's DISTANCES from it
// (kernel/pep Intent: a live caller that fills at market recomputes the levels
// from StopDist/TargetDist and the actual fill).
//
// Refusals, each with its own class:
//
//	pep_no_live_price      no usable live price
//	pep_slippage           the live price is more than pepMaxSlipOfStop x the
//	                       stop distance from the evaluator's reference entry
//	pep_wire_rounding      the wire rounding refused the side
//	pep_bracket_wrong_side the rounded stop/target are not on the right side of
//	                       the entry, or collapsed onto it
//	pep_rr_distorted       the rounded bracket's reward:risk fell more than
//	                       pepRRTolerance below the intended one
func pepWireCheck(in pep.Intent, live, tick float64) pepWire {
	w := pepWire{Live: live}
	long := in.Side == pep.SideLong
	side := "short"
	if long {
		side = "long"
	}
	if !(live > 0) || math.IsNaN(live) || math.IsInf(live, 0) {
		w.Class, w.Reason = "pep_no_live_price", "no usable live price to anchor the bracket"
		return w
	}
	if !(in.StopDist > 0) || !(in.TargetDist > 0) {
		w.Class, w.Reason = "pep_bracket_wrong_side", fmt.Sprintf("intent distances stop %.4f target %.4f", in.StopDist, in.TargetDist)
		return w
	}
	if slip := math.Abs(live - in.Entry); slip > pepMaxSlipOfStop*in.StopDist {
		w.Class = "pep_slippage"
		w.Reason = fmt.Sprintf("live %.2f is %.2f pts from the signal's reference entry %.2f (> %.0f%% of the %.2f pt stop) - not the signal that was evaluated",
			live, slip, in.Entry, 100*pepMaxSlipOfStop, in.StopDist)
		return w
	}
	rawStop, rawTarget := live-in.StopDist, live+in.TargetDist
	if !long {
		rawStop, rawTarget = live+in.StopDist, live-in.TargetDist
	}
	wEntry, wStop, wTarget, err := ntTrader.WireBracket(side, live, rawStop, rawTarget, tick)
	if err != nil {
		w.Class, w.Reason = "pep_wire_rounding", err.Error()
		return w
	}
	w.Stop, w.Target = wStop, wTarget
	minGap := tick
	if tick <= 0 {
		minGap = 0
	}
	risk, reward := wEntry-wStop, wTarget-wEntry
	if !long {
		risk, reward = wStop-wEntry, wEntry-wTarget
	}
	if risk <= minGap/2 || reward <= minGap/2 {
		w.Class = "pep_bracket_wrong_side"
		w.Reason = fmt.Sprintf("rounded bracket entry %.2f stop %.2f target %.2f is not a %s bracket", wEntry, wStop, wTarget, side)
		return w
	}
	intended := in.TargetDist / in.StopDist
	if got := reward / risk; got < intended*(1-pepRRTolerance) {
		w.Class = "pep_rr_distorted"
		w.Reason = fmt.Sprintf("rounded bracket reward:risk %.3f is more than %.0f%% below the intended %.3f (entry %.2f stop %.2f target %.2f)",
			got, 100*pepRRTolerance, intended, wEntry, wStop, wTarget)
		return w
	}
	return w
}

// pepSendEntry turns one entry intent into ONE market order with its native
// bracket: wire check -> PepSourced decision -> executeDecisionWithRecordAt
// (admission, durable claim, the single OpenWithBracket send). Called with
// pep.mu held.
func (at *AutoTrader) pepSendEntry(in pep.Intent, now time.Time) {
	sym := at.futuresSymbol()
	cfg := at.pepConfig()
	action := "open_long"
	if in.Side == pep.SideShort {
		action = "open_short"
	}

	// The broker's book must be flat: pep never flattens an orphan (the AI path's
	// reconcile-before-open would), and it never stacks on a position it cannot
	// explain. An unreadable book is not an empty book.
	if held, herr := at.ntHeldPosition(sym); herr != nil {
		at.pepBlock("pep_book_unreadable", fmt.Sprintf("the broker book could not be read (%v)", herr))
		return
	} else if held != "" {
		at.pepBlock("pep_broker_not_flat", fmt.Sprintf("the broker holds a %s %s - pep sends no entry on top of it and flattens nothing it did not open", held, sym))
		return
	}

	md, merr := openEntryMarketRead(sym, at.exchange)
	live := 0.0
	if merr == nil && md != nil {
		live = md.CurrentPrice
	}
	w := pepWireCheck(in, live, ntTrader.InstrumentTickSize(sym))
	if w.Class != "" {
		at.pepBlock(w.Class, w.Reason)
		return
	}
	pv := market.FuturesPointValue(sym)
	if pv <= 0 {
		at.pepBlock("pep_no_point_value", fmt.Sprintf("no point value for %s", sym))
		return
	}
	d := &kernel.Decision{
		Symbol:          sym,
		Action:          action,
		StopLoss:        w.Stop,
		TakeProfit:      w.Target,
		PositionSizeUSD: float64(cfg.Contracts) * w.Live * pv,
		Leverage:        1,
		Confidence:      80,
		Reasoning:       fmt.Sprintf("pep %s: %s", in.Style, in.Reason),
		PepSourced:      true,
		PepContracts:    cfg.Contracts,
	}
	day := in.Day
	if day == "" {
		day = pepDayNow(now)
	}
	at.pep.pending = &pepPending{intent: in, signalID: pepSignalID(in), day: day}
	defer func() { at.pep.pending = nil }()

	rec := &store.DecisionAction{Action: action, Symbol: sym, Leverage: 1, Price: w.Live,
		StopLoss: w.Stop, TakeProfit: w.Target, Confidence: 80, Reasoning: d.Reasoning, Timestamp: now}
	err := at.executeDecisionWithRecordAt(d, rec, now)

	// A claim that never advanced to the wire is withdrawn so the day's trade is
	// not burned by a refusal that sent nothing. A claim that reached "sending"
	// stays consumed whatever the outcome (fail closed: never a second entry).
	if p := at.pep.pending; p != nil && p.row != nil && !p.sending && (err != nil || !rec.Success) {
		note := "never reached the wire"
		if err != nil {
			note += ": " + err.Error()
		} else if rec.Error != "" {
			note += ": " + rec.Error
		}
		if rerr := at.store.PepDay().Release(p.row.RowKey, note, time.Now().UnixMilli()); rerr != nil {
			at.logErrorf("🚨 pep: could not release the claim %s (%v) - the day's slot stays consumed (fail closed)", p.row.RowKey, rerr)
		} else {
			at.logWarnf("↩ pep: claim %s released - %s", p.row.RowKey, note)
		}
	}
	switch {
	case err != nil:
		rec.Success, rec.Error = false, err.Error()
		at.logErrorf("❌ pep %s %s failed: %v", action, sym, err)
		telemetry.IncGateBlock(at.id, "pep_entry_failed")
	case !rec.Success:
		at.logWarnf("⛔ pep %s %s REFUSED: %s", action, sym, rec.Error)
	default:
		at.logInfof("📈 pep %s: %s %s x%d SL %.2f TP %.2f (signal %s)", in.Style, action, sym, cfg.Contracts, w.Stop, w.Target, pepSignalID(in))
	}
	dr := &store.DecisionRecord{
		Timestamp: now.UTC(), Success: rec.Success,
		ExecutionLog: []string{fmt.Sprintf("pep %s: %s", in.Style, in.Reason)},
		Decisions:    []store.DecisionAction{*rec},
	}
	if !rec.Success {
		dr.ErrorMessage = rec.Error
	}
	_ = at.saveDecision(dr)
}

// pepClaimBeforeSend is the durable one-trade-per-day claim, called by
// executeDecisionWithRecordAt AFTER the whole admission chain passed and BEFORE
// anything is sent. It returns the refusal text, "" when the claim is held.
// Fail closed: a missing pending entry, a missing store or any write error is a
// refusal - no row, no send.
func (at *AutoTrader) pepClaimBeforeSend(d *kernel.Decision, now time.Time) string {
	p := at.pep.pending
	if p == nil || at.store == nil {
		telemetry.IncGateBlock(at.id, "pep_no_pending_claim")
		return "pep_no_pending_claim: a pep entry reached the send with no recorded signal - refused (fail closed)"
	}
	cfg := at.pepConfig()
	row := store.PepDayDB{
		TraderID: at.id, Day: p.day, Style: string(p.intent.Style),
		Side: string(p.intent.Side), Symbol: d.Symbol, Contracts: d.PepContracts,
		SignalID: p.signalID, RefEntry: p.intent.Entry,
		BoxHigh: p.intent.BoxHigh, BoxLow: p.intent.BoxLow,
		Stop: d.StopLoss, Target: d.TakeProfit,
	}
	claimed, err := at.store.PepDay().ClaimEntry(row, cfg.MaxTradesPerDay, now.UnixMilli())
	if errors.Is(err, store.ErrPepDayCapReached) {
		telemetry.IncGateBlock(at.id, pep.CodeMaxTrades)
		return fmt.Sprintf("%s: the day's %d trade(s) are already recorded for %s", pep.CodeMaxTrades, cfg.MaxTradesPerDay, p.day)
	}
	if err != nil {
		telemetry.IncGateBlock(at.id, "pep_day_unwritable")
		at.logErrorf("🚨 pep: the one-trade-per-day record could not be written (%v) - entry refused (fail closed)", err)
		return fmt.Sprintf("pep_day_unwritable: the one-trade-per-day record could not be written (%v) - refused (fail closed)", err)
	}
	p.row = claimed
	return ""
}

// pepMarkSending advances the claim to "sending" immediately before the wire.
// An error here means nothing was sent and the caller returns it.
func (at *AutoTrader) pepMarkSending(d *kernel.Decision) error {
	p := at.pep.pending
	if p == nil || p.row == nil || at.store == nil {
		return errors.New("pep_no_claim: the entry has no durable claim - refused at the wire (fail closed)")
	}
	if err := at.store.PepDay().MarkSending(p.row.RowKey, d.StopLoss, d.TakeProfit, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("pep_claim_not_advanced: %w", err)
	}
	p.sending = true
	return nil
}

// ── the flat ────────────────────────────────────────────────────────────────

// pepFlatBeat is the wall-clock half of the time exit, run on the 60-second
// monitor beat beside enforceEODFlat: the flat does not wait for a bar.
func (at *AutoTrader) pepFlatBeat(now time.Time) {
	if at == nil || !at.pepEnabled() || at.store == nil || at.trader == nil {
		return
	}
	cfg := at.pepConfig()
	loc := kernel.CTLocation()
	ct := now.In(loc)
	flatAt := time.Date(ct.Year(), ct.Month(), ct.Day(), cfg.FlatMin/60, cfg.FlatMin%60, 0, 0, loc)
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		at.logWarnf("⚠️ PEP-FLAT: open-position read failed (%v) - flatness UNVERIFIED this beat", err)
		return
	}
	carried := false
	for _, p := range positions {
		if d := pepDayNow(time.UnixMilli(p.EntryTime)); d != "" && d != pepDayNow(now) {
			carried = true
		}
	}
	if now.Before(flatAt) && !carried {
		at.pep.flatAttempts = 0
		return
	}
	// Past the flat time with nothing open the beat only VERIFIES flatness for a
	// quarter of an hour; after that an empty book is just an empty book.
	if len(positions) == 0 && now.After(flatAt.Add(pepFlatVerifyWindow)) {
		return
	}
	// The monitor beat carries other safety work and must never wait on a pass
	// that is mid-send: that pass emits its own flat intent when it is due.
	if !at.pep.mu.TryLock() {
		return
	}
	defer at.pep.mu.Unlock()
	reason := fmt.Sprintf("flat time %s CT reached", pepClock(cfg.FlatMin))
	if carried && now.Before(flatAt) {
		reason = "position carried over from an earlier CT day"
	}
	at.pepFlatLocked(now, reason)
}

func pepClock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// pepFlatLocked is pep's own time exit (pep.mu held): cancel what rests, then
// flatten every open position via the trader close path, then say what the
// BROKER reports. Idempotent: it is re-run every beat while a position stays.
// The session flat (14:45 CT) never reaches a pep trader (enforceEODFlatAt
// returns at once), so this is the ONLY flat and it is never early.
func (at *AutoTrader) pepFlatLocked(now time.Time, reason string) {
	if at.store == nil || at.trader == nil {
		return
	}
	// CANCEL FIRST: pep rests no entry, but a stray armed row of this trader
	// must not fill after the flat. (A no-op for a pep trader in practice.)
	if n, unacked := at.cancelArmedOrdersSync("pep flat"); n > 0 || unacked > 0 {
		at.logWarnf("🔒 PEP-FLAT: %d armed order(s) cancelled, %d unconfirmed", n, unacked)
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		at.logWarnf("⚠️ PEP-FLAT (%s): open-position read failed (%v) - flatness UNVERIFIED", reason, err)
		return
	}
	if len(positions) == 0 {
		v := at.flatTruthAt(0, now)
		switch {
		case v.Disagrees():
			telemetry.IncGateBlock(at.id, "flat_claim_refused_broker_disagrees")
			at.logErrorf("🚨 PEP-FLAT (%s): NOT FLAT - our table shows no open position and the BROKER REPORTS %d. %s. Nothing is auto-closed from the broker's answer alone; this needs an owner.",
				reason, v.BrokerOpen, v.Why())
		case v.ProvenFlat():
			at.pep.flatAttempts = 0
		default:
			at.logWarnf("⚠️ PEP-FLAT (%s): local table shows no open position but flatness is UNVERIFIED - %s", reason, v.Why())
		}
		return
	}
	at.pep.flatAttempts++
	at.logWarnf("🕒 PEP-FLAT (%s): flattening %d open position(s) via the trader close path (attempt %d).", reason, len(positions), at.pep.flatAttempts)
	for _, p := range positions {
		at.flattenPosition(p, "🕒 PEP-FLAT")
	}
	if at.pep.flatAttempts >= pepFlatAlertAttempts {
		at.emitAlert("P0", "pep_flat_failed", fmt.Sprintf("pep-flat-failed:%s:%s", at.id, pepDayNow(now)),
			"Pep flat did not complete",
			fmt.Sprintf("Trader %q (%s): a pep position is still open after %d flat attempts past %s CT. Check NT8 and flatten by hand if needed.",
				at.name, at.id, at.pep.flatAttempts, pepClock(at.pepConfig().FlatMin)))
	}
	logger.Infof("pep flat sent for trader %s", at.id)
}
