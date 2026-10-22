package trader

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	ntTrader "vl/trader/ninjatrader"
)

// ── MENTOR EXIT DRIVE LOOP (part 2 of the split-legs exit, DS-107) ─────────
//
// The loop runs once per CLOSED 1m candle (right after the evaluator Tick) and
// drives the exits of FILLED mentor positions. It owns the state write-back
// (ArmedBE / Scaled / per-leg Stop) and moves stops ONLY through the
// signal-keyed move_stop frame (MoveStopForSignal) — never reduce_position,
// never a market close (the native bracket exits; EOD flat still applies).
//
// The pure rules it calls:
//   - mentorRR11Stop   — lesson-1.2 "keep live R:R at 1:1" [D1.2 p1 @11:36–16:07]
//   - mentorLegStopB   — the per-leg B stop (1:1 + the runner's candle trail)
//   - mentorBEHalfDistance — the B BE trigger (half the distance to the trade target)
//     — mentor_trail_mode=off only. The default, candle (owner ruling R3,
//     release #22), drives a split position's runner in mentorExitDriveCandle
//     (mentor_trail_r22.go): BE once leg 1's partial FILLS at NT8, then 1 tick
//     beyond each closed candle.

// mentorRR11Stop is the lesson-1.2 rule [D1.2 p1 @11:36–16:07]: the LIVE R:R
// must stay at 1:1 at all times. For a leg with target T, on a close c:
//
//	long:  newStop = max(stop, 2·c − T)   — never widens
//	short: newStop = min(stop, 2·c − T)   — never widens
//
// At the half-way close 2·c − T equals the entry, so the B "BE at half the
// distance" is exactly this formula's first value — one rule, not two.
// Worked (long entry 10 / T 20): close 15 → 10 (BE); close 18 → 16;
// close 16 after that → max(16, 12) = 16 (never widens).
func mentorRR11Stop(side string, stop, close, target float64) float64 {
	if strings.EqualFold(side, "short") {
		if candidate := 2*close - target; candidate < stop {
			return candidate
		}
		return stop
	}
	if candidate := 2*close - target; candidate > stop {
		return candidate
	}
	return stop
}

// mentorLegStopB computes one leg's B-mode stop on ONE closed candle. The 1:1
// rule (2·c − T) is applied first; the runner's candle trail (after leg 1's
// TP) is then taken as the TIGHTER of the two — never wider than either.
// trailPrice is the candle extreme the runner trails behind (long: the low,
// short: the high); applyTrail=false → no trail (leg 1, or the runner before
// leg 1's TP, or trail_tf off).
func mentorLegStopB(side string, stop, close, target, trailPrice float64, applyTrail bool) float64 {
	next := mentorRR11Stop(side, stop, close, target)
	if applyTrail {
		if strings.EqualFold(side, "short") {
			if trailPrice < next {
				next = trailPrice
			}
		} else {
			if trailPrice > next {
				next = trailPrice
			}
		}
	}
	return next
}

// mentorISBPartialTP (item 8, R1 release #30) resolves the ISB leg-1 exit on
// the fill candle's close: the CLOSE itself when it is in profit SHORT of +1R
// — i.e. leg 1 did NOT fill inside the candle. A close PAST +1R means the
// native +1R TP already filled inside the candle (the 1:1 rule), so the
// candle-3 step has nothing to do → 0. A losing close → 0 (the +1R TP keeps
// resting; the stop rules own a losing leg [D2.1 p1 @11:08–11:23, deferred]).
func mentorISBPartialTP(pos mentorPosition, c, h, l float64) float64 {
	long := pos.Side == "long"
	oneR := pos.Entry + pos.R
	if !long {
		oneR = pos.Entry - pos.R
	}
	if long {
		if h >= oneR {
			return 0 // +1R TP already filled inside the candle (R1)
		}
		if c > pos.Entry {
			return c
		}
		return 0
	}
	if l <= oneR {
		return 0 // +1R TP already filled inside the candle (R1)
	}
	if c < pos.Entry {
		return c
	}
	return 0
}

// mentorExitDrive runs the exit rules on every filled mentor position for the
// just-closed 1m candle. It is called right after the evaluator Tick, once per
// CLOSED bar (the forming-bar guard below is defence in depth — the caller
// already feeds closed bars). A fill pokes the event loop so this runs without
// waiting for the next bar.
func (at *AutoTrader) mentorExitDrive(bars []market.Kline) {
	if at == nil || !at.mentorEnabled() || len(bars) == 0 {
		return
	}
	last := bars[len(bars)-1]
	if !last.Final {
		// Never drive an exit off a FORMING bar (its close is still moving).
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		return
	}
	// REL17-F2: rebuild open mentor positions at boot (once, after the first
	// fresh broker book) BEFORE the first exit-drive pass — otherwise a
	// restart leaves an open mentor position with no BE/1:1/trail.
	at.mentorRebuildLivePosOnce(time.Now())
	// F1 STOP-MOVE TRUTH: render verdicts on any pending stop move against the
	// broker's latest book BEFORE this candle's drive computes new moves — a
	// just-corrected belief feeds the 1:1/trail math, not a stale one.
	at.mentorVerifyPendingStopMoves(nt)
	// Release #22 (P1-2): the same broker-book verdict for a SENT leg-1 TP
	// modify (the ISB candle-3 partial) — the AddOn sends no success ack.
	at.mentorVerifyPendingTPModifies(nt)
	c, h, l := last.Close, last.High, last.Low
	// CTO gate (release #4): the loop must drop a position once the account
	// is flat on its side — nothing else ever unregistered it, so a closed
	// trade kept sending move_stop every candle. A read error proves
	// nothing (keep); a just-filled position may not be in the snapshot yet,
	// so it takes TWO consecutive flat reads after the fill candle.
	openSides, sidesOK := mentorDriveOpenSides(at)
	openQty, qtyOK := mentorDriveOpenQty(at)
	for _, p := range at.mentorLivePosList() {
		if p == nil || p.Pos.Symbol == "" {
			continue
		}
		p.BarsSinceFill++
		if sidesOK && !openSides[p.Pos.Side] {
			p.FlatReads++
			if p.BarsSinceFill >= 2 && p.FlatReads >= 2 {
				at.mentorUnregisterLivePos(p.Legs[0].SignalID)
				at.mentorCount("exit_drive_flat_unregistered")
				at.logInfof("🧑‍🏫 mentor exit-drive: %s %s is flat — dropped from the loop", p.Pos.Symbol, p.Pos.Side)
			}
			continue
		}
		p.FlatReads = 0
		// I7 fail-safe: the broker's own snapshot shows the side reduced to the
		// runner's qty (leg 1 gone) while leg 1's TP receipt never arrived — a
		// parked/failed/no-owner close. Latch Scaled with the SAME next-candle
		// rule; NEVER latch on a price guess alone.
		if qtyOK {
			at.mentorLatchLeg1GoneIfRunnerRemains(p, openQty[p.Pos.Side])
		}
		// X8 @18:02–18:14: a position entered on the 2m trails on CLOSED 2m
		// candles — the same buckets the exec-2m ISB read built.
		if p.TrailTFMin > 1 {
			p.trailBar, p.trailBarOK = mentor.ClosedCandleTF(bars, p.TrailTFMin)
		}
		at.mentorExitDrivePos(nt, p, c, h, l, last.OpenTime)
		brokerStop, brokerKnown := at.mentorBrokerStopForPos(nt, p)
		at.mentorLogPositionState(&p.Pos, "exit-drive", brokerStop, brokerKnown)
	}
}

// mentorDriveOpenSides is the loop's open-side read (a seam so the call-site
// pin can drive flat/open without a live broker).
var mentorDriveOpenSides = func(at *AutoTrader) (map[string]bool, bool) { return at.mentorOpenSidesForDrive() }

// mentorDriveOpenQty is the loop's per-side QUANTITY read (I7 fail-safe: detect
// leg 1 gone when the broker's qty drops to the runner's qty). ok=false on any
// read error — never a confident "leg 1 gone". A seam so the pin can drive it.
var mentorDriveOpenQty = func(at *AutoTrader) (map[string]float64, bool) { return at.mentorOpenQtyForDrive() }

// mentorOpenSidesForDrive reads the account's open position sides from the
// broker book. ok=false on any read error — never a confident "flat".
func (at *AutoTrader) mentorOpenSidesForDrive() (map[string]bool, bool) {
	if at == nil || at.trader == nil {
		return nil, false
	}
	pos, err := at.trader.GetPositions()
	if err != nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, p := range pos {
		side, _ := p["side"].(string)
		qty := 0.0
		switch v := p["positionAmt"].(type) {
		case float64:
			qty = v
		case int:
			qty = float64(v)
		}
		if qty == 0 {
			if q, ok := p["quantity"].(float64); ok {
				qty = q
			}
		}
		if s := strings.ToLower(strings.TrimSpace(side)); (s == "long" || s == "short") && qty != 0 {
			out[s] = true
		}
	}
	return out, true
}

// mentorOpenQtyForDrive reads the account's per-side open quantity (the I7
// leg-1-gone evidence). Sums every row on a side; ok=false on any read error.
func (at *AutoTrader) mentorOpenQtyForDrive() (map[string]float64, bool) {
	if at == nil || at.trader == nil {
		return nil, false
	}
	pos, err := at.trader.GetPositions()
	if err != nil {
		return nil, false
	}
	out := map[string]float64{}
	for _, p := range pos {
		side, _ := p["side"].(string)
		s := strings.ToLower(strings.TrimSpace(side))
		if s != "long" && s != "short" {
			continue
		}
		qty := 0.0
		switch v := p["positionAmt"].(type) {
		case float64:
			qty = v
		case int:
			qty = float64(v)
		}
		if qty == 0 {
			if q, ok := p["quantity"].(float64); ok {
				qty = q
			}
		}
		// P2 (rel9 review): positionAmt is SIGNED (short < 0) — Abs so the
		// leg-1-gone comparison reads a magnitude, never a negative qty.
		out[s] += math.Abs(qty)
	}
	return out, true
}

// mentorLatchLeg1GoneIfRunnerRemains (I7) latches Scaled when the broker snapshot
// shows the side reduced to the runner's qty — leg 1 is gone even though its TP
// receipt never arrived (a parked/failed/no-owner close). Only for a SPLIT with
// a live runner; never latches on a price guess. Uses the same next-candle latch
// (Leg1ExitedAtMs = now) as the receipt path.
func (at *AutoTrader) mentorLatchLeg1GoneIfRunnerRemains(p *mentorLivePos, qty float64) {
	if p == nil || p.Legs[0].Wire != 1 || p.Pos.Leg2 <= 0 {
		return // single bracket or no runner — nothing to latch
	}
	if qty <= 0 || qty > float64(p.Pos.Leg2) {
		return // side flat (handled above) or leg 1 still present
	}
	if at.mentorLatchLeg1Scaled(p.Legs[0].SignalID, mentorClockNow().UnixMilli()) {
		at.mentorCount("leg1_at_target")
		at.logInfof("🧑‍🏫 mentor leg 1 gone on the broker snapshot (qty %.0f ≤ runner %d) — Scaled latched [I7]", qty, p.Pos.Leg2)
	}
}

// mentorExitDrivePos drives ONE position on one closed candle (c/h/l).
// candleOpenMs is the candle's OpenTime (ms) — the I6 next-candle rule compares
// leg 1's confirmation instant against it.
func (at *AutoTrader) mentorExitDrivePos(nt *ntTrader.TCPTrader, p *mentorLivePos, c, h, l float64, candleOpenMs int64) {
	pos := &p.Pos
	side := pos.Side
	long := side == "long"
	trail := mentorTrailEnabled(at.mentorTrailTF())

	switch pos.Mode {
	case "swing":
		// SWING: no moves — the swing rules own it [D5.2 p1 @05:52–06:09].
		return
	case "C":
		// C (confluence): the stop NEVER moves [D3.4 p3 @07:38: "hold… at least
		// risk reward 1-2"]. No action.
		return
	case "A-resonance":
		// A (resonance): leg 1's +1R TP stays (the 1:1 partial still fires),
		// the RUNNER's TP out to the level beyond the old high (CX-3), NO
		// trail, NO 1:1 tightening [D2.4 p1]. m134 (b)/(d): the runner's stop
		// is NOT moved at arm — the deferred break-even runs on this CLOSED 1m
		// candle (after the 1:1 partial is confirmed, or once price is on the
		// profit side).
		scaledNow, _ := at.mentorScaledState(p)
		at.mentorExitDriveResonance(nt, p, c, scaledNow, at.mentorExitedLegs(p))
		return
	}

	// ── Mode D guard: the spent-day runner is capped at 2 (placed by DS-103;
	// the loop ASSERTS it, never reduces). ────────────────────────────────
	if p.SpentDay {
		if runner := mentorRunnerLeg(p); runner != nil && runner.Qty > mentorSpentDayRunnerCap {
			at.mentorCount("spent_day_runner_over_cap")
			at.logWarnf("🧑‍🏫 mentor spent-day runner %d over the cap %d — asserting, not reducing [D5.1 p1 @16:24-16:31]", runner.Qty, mentorSpentDayRunnerCap)
		}
	}

	// scaledNow / exitedAtMs are read ONCE under mentorExitMu (I6): the receipt
	// (mentorMarkLeg1Scaled) and the I7 fallback write Scaled/Leg1ExitedAtMs from
	// other goroutines, so an unlocked read races.
	scaledNow, exitedAtMs := at.mentorScaledState(p)
	// exited is the per-leg "closed on its own TP receipt" snapshot, read ONCE
	// under mentorExitMu (release #31 fold, D1): the receipt writes
	// Legs[i].Exited from the close-sync goroutine, so an unlocked read races.
	exited := at.mentorExitedLegs(p)
	// scaledBefore is whether leg 1's exit was confirmed on a PRIOR candle: the
	// runner's trail begins on the NEXT candle after the confirmation, never on
	// the candle that crosses it (I6 next-candle rule).
	scaledBefore := scaledNow && mentorLeg1ExitedBefore(exitedAtMs, candleOpenMs)
	// The runner's candle TRAIL reads closed candles of the frame the entry was
	// read on: "nếu như mình trade khung nào thì mình gồng theo khung đó, theo
	// cây nến của khung đó" [X8 @18:02–18:14]. A 1m entry trails on THIS 1m
	// candle (unchanged); a 2m entry (X5-10 exec-2m schedule) trails only on a
	// 2m candle that closed with this 1m bar, and the I6 next-candle rule is
	// read against THAT candle's open. Release #31 fold (D8): BE also reads the
	// entry's frame — a 2m entry's break-even uses the 2m close (and the mode-B
	// BE-half uses the 2m high/low), waiting for the 2m candle to close. The
	// 1:1 live rule keeps the 1m close.
	// Release #31 (rule 4): the ISB candle-3 partial ALSO reads the frame the
	// entry was read on — for a 2m entry the fill candle IS the 2m candle, so
	// the partial waits for it to close (trailBarOK) and reads ITS close/high/
	// low, not the 1m's.
	trailC, trailH, trailL, trailOK, trailScaledBefore := c, h, l, true, scaledBefore
	if p.TrailTFMin > 1 {
		trailC = p.trailBar.Close
		trailH, trailL, trailOK = p.trailBar.High, p.trailBar.Low, p.trailBarOK
		trailScaledBefore = scaledNow && mentorLeg1ExitedBefore(exitedAtMs, p.trailBar.OpenTime)
	}
	// leg1OnWire: leg 1 has its OWN bracket on the wire (a split was sent), so
	// its exit is confirmed by the broker receipt — never candle-marked. A single
	// bracket (Wire 0) has no leg-1 receipt and keeps the candle-priced 1:1.
	leg1OnWire := p.Legs[0].Wire == 1
	// wasArmed is whether BE was ALREADY armed on a PRIOR candle: the candle
	// that arms BE only arms BE — the 1:1/trail starts on the NEXT candle.
	wasArmed := pos.ArmedBE

	// leg 1's target: its own resting TP (+1R default when unset). For an ISB
	// fill it is RESOLVED once on the fill candle's close (item 8, step (2)).
	leg1Target := p.Legs[0].TP
	if leg1Target == 0 {
		if long {
			leg1Target = pos.Entry + pos.R
		} else {
			leg1Target = pos.Entry - pos.R
		}
	}
	runnerTarget := pos.Target
	if runnerTarget == 0 {
		runnerTarget = leg1Target
	}

	// ── (2) ISB partial (item 8): leg 1 IS the partial — it leaves at +1R TP
	// or at the candle-3 close (the fill candle's close), whichever comes
	// first, ONCE — and the close only when in profit. The resolution reads
	// the JUST-closed fill candle (BarsSinceFill == 1): its high/low decides
	// whether +1R printed first. Release #22 (P1-2): a close in profit short
	// of +1R is a REAL modify_bracket on leg 1 (modifyBracketWire, assigned),
	// verified on the next fresh book — it was LOG-only from 10-05 to 10-14.
	// Runs in both mentor_trail_mode values (a bug fix, not the R3 knob).
	// Release #31 (rule 4): the fill candle is the frame the entry was read on
	// — a 2m entry waits for the 2m candle to close (trailOK) and reads ITS
	// close/high/low (trailC/trailH/trailL), not the 1m's. A 1m entry keeps
	// dev's BarsSinceFill == 1 gate (a later candle never resolves it, D3).
	if pos.Origin == "ISB" && !p.ISBPartialDone && p.BarsSinceFill >= 1 {
		switch {
		case p.TrailTFMin > 1 && !trailOK:
			// the 2m fill candle has not closed yet — wait for it (the 1m close
			// is NOT the fill candle).
		case p.TrailTFMin > 1:
			// 2m entry: resolve once the 2m fill candle closed, on ITS
			// close/high/low.
			p.ISBPartialDone = true
			if !scaledNow {
				if tp := at.mentorISBPartialStep(nt, p, trailC, trailH, trailL); tp > 0 {
					leg1Target = tp
				}
			}
		case p.BarsSinceFill == 1:
			// 1m entry: dev's gate — resolve ONLY on the just-closed 1m fill
			// candle (a skipped bar 1 latches done, never resolves late, D3).
			p.ISBPartialDone = true
			if !scaledNow {
				if tp := at.mentorISBPartialStep(nt, p, c, h, l); tp > 0 {
					leg1Target = tp
				}
			}
		default:
			// 1m entry whose fill candle was skipped (BarsSinceFill > 1): dev
			// behaviour — latch done, never resolve on a later candle (D3).
			p.ISBPartialDone = true
		}
	}

	// ── (2b) R1 re-modify: the marketable leg-1 TP limit is re-priced to the
	// NEW close ∓ 8 ticks on every later candle until leg 1 fills — never back
	// to "none"; the leg keeps its stop throughout.
	if pos.Origin == "ISB" && p.ISBPartialDone && p.BarsSinceFill >= 2 && !scaledNow && leg1OnWire {
		at.mentorISBMarketableReModify(nt, p, c)
	}

	// ── (2c) R-7 (fold 2): a failed first marketable modify is retried ONCE
	// on the next candle — the course's compulsory candle-3 sale is not
	// silently skipped. The retry re-checks in-profit itself; a second failure
	// leaves the +1R TP resting (fail-safe).
	if pos.Origin == "ISB" && p.ISBPartialRetryCount == 1 && !scaledNow && p.BarsSinceFill >= 2 {
		p.ISBPartialRetryCount = 2
		at.mentorSellISBLeg1Marketable(nt, p, c)
	}

	// ── R3 (release #22, mentor_trail_mode=candle, the default): a SPLIT
	// position's runner goes to break-even once leg 1's partial is CONFIRMED
	// filled at NT8, then trails 1 tick beyond each closed candle. A single
	// bracket has no leg-1 partial to confirm and keeps the rule below.
	if leg1OnWire && mentorRunnerLeg(p) != nil && at.mentorTrailMode() == mentorTrailModeCandle {
		// fold #565 (D8): the BE close is the entry's frame close — a 2m entry's
		// break-even reads the 2m close and waits for the 2m candle to close.
		// #563: the per-leg exited flags pick the runner legs still on the wire.
		at.mentorExitDriveCandle(nt, p, trailC, trailH, trailL, scaledNow, trailScaledBefore, trail && trailOK, trailOK, exited)
		return
	}

	// ── (1) B BE: arm both legs' stops to entry once price covers HALF the
	// distance to the trade target (mentorBEHalfDistance). ─────────────────
	if !pos.ArmedBE {
		// fold #565 (D8): a 2m entry's BE-half reads the 2m high/low and waits
		// for the 2m candle to close (trailOK); a 1m entry keeps the 1m high/low.
		beH, beL := h, l
		if p.TrailTFMin > 1 {
			beH, beL = trailH, trailL
		}
		half := mentorBEHalfDistance(*pos)
		armed := false
		if p.TrailTFMin > 1 && !trailOK {
			armed = false // the 2m candle has not closed — BE-half waits
		} else if long {
			armed = beH >= pos.Entry+half
		} else {
			armed = beL <= pos.Entry+half
		}
		if armed {
			// Release #22: BE counts as armed only when a move was APPLIED (sent,
			// already at the broker, or the broker already tighter) — a refused
			// or failed move never writes the belief; BE is re-tried next candle.
			applied := false
			for i := range p.Legs {
				leg := &p.Legs[i]
				if leg.SignalID == "" || leg.Qty <= 0 || exited[i] {
					continue
				}
				// F-6 (release #22): leg 1 of a split whose partial is CONFIRMED
				// filled is closed — never send it a move (#651 01:05:00 sent BE
				// to the gone leg 1, then WARNed "NO live stop order" at 01:06).
				// D1 (release #31 fold): the runner partial (wire 2) is NOT leg 1
				// — it goes to break-even too.
				if leg1OnWire && leg.Wire == 1 && scaledNow {
					at.mentorCount("stop_move_skipped_leg_closed")
					continue
				}
				err := at.mentorMoveLegStop(nt, side, leg, pos.Entry)
				if errors.Is(err, errMentorStopBrokerTighter) {
					applied = true // the broker is already past BE (belief corrected)
					continue
				}
				if errors.Is(err, errMentorStopThroughMarket) {
					// m198 #43-B: the wrong-side BE closed the WHOLE symbol at
					// market (the close wire is a full flatten) — the position
					// is flat, so every remaining leg would be a redundant close
					// on an already-flat position. Stop the pass.
					at.mentorCount("stop_move_skipped_leg_closed")
					return
				}
				if !mentorMoveApplied(err) {
					if !mentorMoveErrAlreadyLogged(err) {
						at.logWarnf("🧑‍🏫 mentor BE leg %d move FAILED: %v", i, err)
					}
					continue
				}
				leg.Stop = pos.Entry
				applied = true
			}
			if applied {
				pos.ArmedBE = true
				pos.Stop = pos.Entry
				for i := range p.Legs {
					if p.Legs[i].Final && p.Legs[i].Qty > 0 && mentorStopTighter(side, p.Legs[i].Stop, pos.Stop) {
						pos.Stop = p.Legs[i].Stop
					}
				}
				at.mentorCount("be_armed_both_legs")
			} else {
				at.mentorCount("be_not_armed_no_move_applied")
			}
		}
	}

	// ── (3) the 1:1 point → the runner's trail begins NEXT candle. For a SPLIT
	// (leg1OnWire), leg 1 exits at its native TP and Scaled is marked ONLY on the
	// broker's confirmation (position_close receipt or the I7 snapshot fallback) —
	// the candle-price guess is GONE. A SINGLE bracket (Wire 0) has no leg-1
	// receipt, so its 1:1 point stays candle-priced (entry ± R). (Final does NOT
	// mean "exited" — canonical semantics: Final marks the RUNNER.)
	if !scaledNow && !leg1OnWire {
		scaleAt := pos.Entry + pos.R
		if !long {
			scaleAt = pos.Entry - pos.R
		}
		if (long && h >= scaleAt) || (!long && l <= scaleAt) {
			if at.mentorSetScaledCandle(p) {
				at.mentorCount("leg1_at_target")
			}
		}
	}

	// ── (4) the 1:1 rule on every closed candle (+ the runner's trail). ────
	// Skipped on the candle that JUST armed BE (wasArmed=false): the course is
	// "BE at half the distance, THEN live 1:1 on every closed candle".
	if pos.ArmedBE && wasArmed {
		for i := range p.Legs {
			leg := &p.Legs[i]
			if leg.SignalID == "" || leg.Qty <= 0 || exited[i] {
				continue
			}
			// Canonical semantics: Final marks the RUNNER (the leg that holds
			// to the trade target). Leg 1 (Final=false, wire 1) is the partial
			// that exits at its own TP — once Scaled its stop is moot. D1
			// (release #31 fold): the runner partial (wire 2) is a RUNNER too —
			// it goes to BE and trails until its own line TP closes it.
			isRunner := leg.Final || leg.Wire == 2
			if !isRunner && scaledNow {
				continue
			}
			target := leg1Target
			applyTrail := false
			trailPrice := 0.0
			if isRunner {
				target = runnerTarget
				if leg.Wire == 2 && leg.TP > 0 {
					target = leg.TP // the runner partial's own line TP (it exits there)
				}
				applyTrail = trail && trailOK && trailScaledBefore // the 1:1 point printed on a PRIOR candle (of the trail frame)
				if applyTrail {
					// Item 7 ruling [X8 @15:40–17:13]: 1 tick BEYOND the closed
					// candle's low (long) / high (short), never exactly at it —
					// the candle of the frame the entry was read on.
					if long {
						trailPrice = trailL - at.mentorInstrumentTick()
					} else {
						trailPrice = trailH + at.mentorInstrumentTick()
					}
				}
			}
			next := mentorLegStopB(side, leg.Stop, c, target, trailPrice, applyTrail)
			if next == leg.Stop {
				continue
			}
			if err := at.mentorMoveLegStop(nt, side, leg, next); !mentorMoveApplied(err) {
				if !mentorMoveErrAlreadyLogged(err) {
					at.logWarnf("🧑‍🏫 mentor %s leg %d stop move FAILED: %v", side, i, err)
				}
				continue
			}
			leg.Stop = next
			if isRunner {
				pos.Stop = next
			}
		}
	}
}

// mentorMoveStopForSignalWire is the last hop for a signal-keyed mentor stop
// move (the seam tests substitute to capture per-leg moves; production binds
// it to TCPTrader.MoveStopForSignal — the SAME move_stop frame, no C# change).
var mentorMoveStopForSignalWire = func(nt *ntTrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
	return nt.MoveStopForSignalLeg(signalID, side, newStop, leg)
}

// errMentorStopManual is the sentinel a latched leg returns from
// mentorMoveLegStop: the leg is under manual management, nothing is sent, and
// the callers skip the WARN (the latch message already logged once).
var errMentorStopManual = errors.New("mentor stop move: leg under manual management")

// errMentorStopThroughMarket is the sentinel mentorMoveLegStop returns when the
// new stop is through the market and the leg was CLOSED at market instead —
// nothing was sent, the leg is closing, and the callers skip their own WARN.
var errMentorStopThroughMarket = errors.New("mentor stop move: stop through the market — leg closed at market instead")

// mentorStopMoveWrongSide reports whether newStop sits on the WRONG side of the
// market at price: a stop that NT8 would refuse (a buy stop below the market)
// or that would trigger immediately (the exit already happened). A short's
// protective stop is a buy stop and must be ABOVE price; a long's is a sell
// stop and must be BELOW price.
func mentorStopMoveWrongSide(side string, newStop, price float64) bool {
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		return newStop <= price
	}
	return newStop >= price
}

// mentorStopMovePriceMaxAge is the wrong-side guard's freshness window: a 1m
// bar older than this is stale and the move is sent as before (today's
// behaviour), logged.
const mentorStopMovePriceMaxAge = 90 * time.Second

// mentorLatestPriceSeam reads the freshest 1m close from BarCache (price,
// fresh). Tests substitute it to pin the wrong-side guard deterministically.
var mentorLatestPriceSeam = func(at *AutoTrader) (float64, bool) {
	if at == nil || market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 1)
	if len(bars) == 0 {
		return 0, false
	}
	b := bars[len(bars)-1]
	return b.Close, time.Since(time.UnixMilli(b.OpenTime)) <= mentorStopMovePriceMaxAge
}

// mentorCloseLegAtMarketWire is the last hop for the wrong-side market close
// (tests substitute it to capture closes; production binds it to the
// TCPTrader's CloseShort/CloseLong on the futures symbol). NOTE: the close
// wire is a FULL FLATTEN of the symbol's position — the qty is advisory and
// ignored by the AddOn (ClosePositionPayload.Quantity).
var mentorCloseLegAtMarketWire = func(at *AutoTrader, nt *ntTrader.TCPTrader, side string, qty int) error {
	if nt == nil {
		return fmt.Errorf("mentor leg close: no armed trader")
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		_, err := nt.CloseLong(at.futuresSymbol(), float64(qty))
		return err
	case "short":
		_, err := nt.CloseShort(at.futuresSymbol(), float64(qty))
		return err
	}
	return fmt.Errorf("mentor leg close: unknown side %q", side)
}

// mentorMoveLegStop sends one leg's stop move through the signal-keyed
// move_stop frame, guarded by mentorNeverWiden (never widen, D1.2 p2 @00:08–00:24)
// against the belief AND — release #22, P2-3 — against the BROKER's own stop
// (mentorJudgeAgainstBroker): no fresh book refuses, no live stop for the leg
// sends nothing (F-6), a broker stop already tighter is kept. A latched leg
// (two broker refusals) sends NOTHING — the belief is the broker truth and the
// owner manages it by hand. Returns nil only when a frame was SENT;
// errMentorStopAtBroker when the broker already holds newStop (nothing sent,
// the caller may write the belief — mentorMoveApplied).
func (at *AutoTrader) mentorMoveLegStop(nt *ntTrader.TCPTrader, side string, leg *mentorLeg, newStop float64) error {
	if leg == nil || leg.SignalID == "" {
		return fmt.Errorf("mentor leg stop move: no leg signal id")
	}
	if at != nil && at.mentorStopMoveManualBlocked(leg.SignalID, leg.Wire) {
		return errMentorStopManual
	}
	if refuse, why := mentorNeverWiden(side, leg.Stop, newStop); refuse {
		at.mentorCount("widen_refused")
		return fmt.Errorf("mentor leg stop move refused: %s", why)
	}
	// P0 stop-move-wrong-side (2026-10-21, c6cdfdc3): BEFORE any move_stop, read
	// the freshest 1m close. A stop through the market (short: newStop <= price;
	// long: newStop >= price) is refused by NT8 or triggers immediately — the
	// exit has effectively happened, so CLOSE the leg at market (reduce-only)
	// instead of sending. A stale price keeps today's behaviour (send), logged.
	if price, fresh := mentorLatestPriceSeam(at); fresh {
		if mentorStopMoveWrongSide(side, newStop, price) {
			at.mentorCount("stop_move_through_market_close")
			at.logWarnf("🧑‍🏫 mentor stop move %s leg %d → %.2f is through the market (price %.2f) — closing the leg at market instead (= the BE exit)",
				leg.SignalID, leg.Wire, newStop, price)
			if err := mentorCloseLegAtMarketWire(at, nt, side, leg.Qty); err != nil {
				at.mentorCount("close_failed")
				return err
			}
			at.mentorCount("close_sent")
			// m151 (5): the close wire is a FULL FLATTEN — mark the position's
			// legs exited so the next candle does not close it again.
			at.mentorMarkPosExitedAtMarket(leg.SignalID)
			return errMentorStopThroughMarket
		}
	} else {
		at.logInfof("🧑‍🏫 mentor stop move %s leg %d → %.2f: no fresh 1m price (stale) to judge wrong-side against — sending as before",
			leg.SignalID, leg.Wire, newStop)
	}
	if err := at.mentorJudgeAgainstBroker(nt, side, leg, newStop); err != nil {
		return err
	}
	if err := mentorMoveStopForSignalWire(nt, leg.SignalID, side, newStop, leg.Wire); err != nil {
		at.mentorCount("move_stop_failed")
		return err
	}
	at.mentorCount("move_stop_sent")
	// F1: the wire seam returned nil, but NT8 may still refuse the move (no
	// live bracket) and reply with NO ack — so a nil return only proves the
	// frame was SENT, not that the stop moved. Record a pending check for the
	// broker-book verdict before the caller writes leg.Stop = newStop.
	if mentorStopMoveVerifyEnabled() {
		at.mentorRecordPendingStopMove(leg.SignalID, side, leg.Wire, newStop)
	}
	return nil
}

// ── F1 STOP-MOVE TRUTH ──────────────────────────────────────────────────────
//
// The wire seam returning nil only proves the move_stop frame was SENT — NT8
// replies with NO ack on a refused move (no live bracket), so the Go side used
// to write leg.Stop = next on faith and the bot believed a stop NT8 never
// applied. This verifier closes that hole: every sent move records a pending
// check, and on the first snapshot NEWER than send+2s it reads the broker's own
// book and renders one verdict:
//
//	within half a tick of X            → CONFIRMED (INFO)
//	exists but at another price Y       → NOT APPLIED (ERROR + P0 + re-send ONCE)
//	still at Y after the re-send        → belief = broker truth, P0 "manage by hand"
//	no live stop order for the leg      → leave to the protection net (F4), WARN once
//	book older than snapshotMaxAge      → no verdict yet, WARN once
//
// Never widen: the verifier never sends a price wider than the broker's current
// stop — it either re-sends the SAME wanted price or adopts the broker's price.

// mentorStopMoveVerifyEnabled is the F1 kill-switch (default ON).
func mentorStopMoveVerifyEnabled() bool {
	switch strings.ToLower(os.Getenv("MENTOR_STOP_MOVE_VERIFY")) {
	case "off", "0", "false", "disabled":
		return false
	}
	return true
}

// mentorPendingStopMove is one SENT but not-yet-VERIFIED stop move, keyed
// "<signalID>#<wireLeg>" (wireLeg 0 = the single bracket's own stop).
type mentorPendingStopMove struct {
	signalID      string
	side          string
	wireLeg       int
	want          float64   // the price the loop asked NT8 for (X)
	sendAt        time.Time // when the frame was SENT (the seam returned nil)
	noOrderWarned bool      // the no-stop-order WARN fired once
	staleWarned   bool      // the stale-book WARN fired once
}

// mentorStopMoveEpisode is the F1 miss-count ledger for one (signal, wire leg).
// It SURVIVES a superseding move (mentorRecordPendingStopMove never resets it)
// so a drive that re-sends a fresh want every candle still accumulates misses
// and reaches "manage by hand" on the second consecutive miss. Reset only on a
// CONFIRMED verdict or a tighter-broker adoption.
type mentorStopMoveEpisode struct {
	misses  int   // consecutive wider misses since the last CONFIRM
	startMs int64 // the episode start (the P0 event id is keyed on it, so one P0 per episode)
}

// mentorStopMoveManual is the F1 manual-management latch for one (signal, wire
// leg): after two misses the drive sends NOTHING more for it until the position
// is gone, or the owner moves the broker stop by hand (see mentorSweepManualLatches).
type mentorStopMoveManual struct {
	signalID     string
	wireLeg      int
	logged       bool    // the "under manual management" line fired once
	adoptedPrice float64 // the broker price adopted at latch time (owner-override change detection)
}

// mentorStopMoveKey builds the shared "<signalID>#<wireLeg>" key.
func mentorStopMoveKey(signalID string, wireLeg int) string {
	return fmt.Sprintf("%s#%d", signalID, wireLeg)
}

// mentorStopMoveManualBlocked reports whether a leg is latched, logging the
// manual-management line ONCE on the first blocked attempt.
func (at *AutoTrader) mentorStopMoveManualBlocked(signalID string, wireLeg int) bool {
	if at == nil || at.mentorStopMoveManual == nil {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	m, ok := at.mentorStopMoveManual[mentorStopMoveKey(signalID, wireLeg)]
	if !ok {
		return false
	}
	if !m.logged {
		m.logged = true
		at.logWarnf("🧑‍🏫 mentor leg %s#%d under manual management — no automatic stop moves", signalID, wireLeg)
	}
	return true
}

// mentorClearStopMoveLatch removes the manual latch for a leg (a snapshot showed
// broker == belief again, or the position is gone).
func (at *AutoTrader) mentorClearStopMoveLatch(key string) {
	if at == nil {
		return
	}
	at.mentorExitMu.Lock()
	delete(at.mentorStopMoveManual, key)
	at.mentorExitMu.Unlock()
}

// mentorResetStopMoveEpisode drops the miss count (a CONFIRM or a tighter-broker
// adoption ended the episode).
func (at *AutoTrader) mentorResetStopMoveEpisode(key string) {
	if at == nil {
		return
	}
	at.mentorExitMu.Lock()
	delete(at.mentorStopMoveEpisodes, key)
	at.mentorExitMu.Unlock()
}

// mentorBumpStopMoveEpisode increments the miss count and returns the episode's
// startMs (creating the episode at the first miss). Callers must hold no lock.
func (at *AutoTrader) mentorBumpStopMoveEpisode(key string) (misses int, startMs int64) {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorStopMoveEpisodes == nil {
		at.mentorStopMoveEpisodes = map[string]*mentorStopMoveEpisode{}
	}
	ep := at.mentorStopMoveEpisodes[key]
	if ep == nil {
		ep = &mentorStopMoveEpisode{startMs: time.Now().UnixMilli()}
		at.mentorStopMoveEpisodes[key] = ep
	}
	ep.misses++
	return ep.misses, ep.startMs
}

// mentorRecordPendingStopMove registers a sent move for the broker-book verdict.
// A newer move for the same (signal, wire leg) supersedes the older one — the
// newest want is the only one that matters.
func (at *AutoTrader) mentorRecordPendingStopMove(signalID, side string, wireLeg int, want float64) {
	if at == nil || signalID == "" {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorPendingStopMoves == nil {
		at.mentorPendingStopMoves = map[string]*mentorPendingStopMove{}
	}
	at.mentorPendingStopMoves[mentorStopMoveKey(signalID, wireLeg)] = &mentorPendingStopMove{
		signalID: signalID,
		side:     side,
		wireLeg:  wireLeg,
		want:     want,
		sendAt:   time.Now(),
	}
}

// mentorDeletePendingStopMove drops a verdict-rendered entry.
func (at *AutoTrader) mentorDeletePendingStopMove(key string) {
	if at == nil {
		return
	}
	at.mentorExitMu.Lock()
	delete(at.mentorPendingStopMoves, key)
	at.mentorExitMu.Unlock()
}

// mentorStopBook is the broker-book read seam for F1: the latest snapshot and
// its local receipt instant. Production binds to TCPTrader.OrderSnapshotBook;
// tests substitute a fixed book.
var mentorStopBook = func(nt *ntTrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
	if nt == nil {
		return ntwire.OrderSnapshotPayload{}, time.Time{}, false
	}
	return nt.OrderSnapshotBook()
}

// mentorBrokerStopOrders returns the broker's live stop order(s) for a signal
// wire leg: wire 1 → "<signal>-sl", wire 2 → "<signal>-sl2", wire 3 →
// "<signal>-sl3" (release #31), wire 0 (single bracket) → every live -sl/-sl2/
// -sl3 of the signal (a leg-less move_stop moves them together).
func mentorBrokerStopOrders(book ntwire.OrderSnapshotPayload, signalID string, wireLeg int) []ntwire.NT8Order {
	if signalID == "" {
		return nil
	}
	var names []string
	switch wireLeg {
	case 1:
		names = []string{signalID + "-sl"}
	case 2:
		names = []string{signalID + "-sl2"}
	case 3:
		names = []string{signalID + "-sl3"}
	default:
		names = []string{signalID + "-sl", signalID + "-sl2", signalID + "-sl3"}
	}
	out := make([]ntwire.NT8Order, 0, 2)
	for _, o := range book.Orders {
		for _, n := range names {
			if o.Name == n {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// mentorAdoptBrokerStop writes the broker's price into the driver's leg belief
// (the runner also mirrors into pos.Stop). Never widens: the adopted price IS
// the broker truth, so nothing is ever sent to a wider level. Returns false
// when no live leg matched (the position is gone).
func (at *AutoTrader) mentorAdoptBrokerStop(signalID string, wireLeg int, brokerStop float64) bool {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	p := at.mentorLivePos[signalID]
	if p == nil {
		return false
	}
	adopted := false
	for i := range p.Legs {
		leg := &p.Legs[i]
		if leg.SignalID != signalID || leg.Qty <= 0 || leg.Wire != wireLeg {
			continue
		}
		leg.Stop = brokerStop
		if leg.Final {
			p.Pos.Stop = brokerStop
		}
		adopted = true
	}
	return adopted
}

// mentorBrokerStopForPos reports the broker's resting stop for the state line's
// leg — the runner when split, the single bracket when Wire 0. ok=false when
// the book or the order is unknown (renders n/a, never a guess).
func (at *AutoTrader) mentorBrokerStopForPos(nt *ntTrader.TCPTrader, p *mentorLivePos) (float64, bool) {
	if p == nil {
		return 0, false
	}
	leg := &p.Legs[0]
	for i := range p.Legs {
		if p.Legs[i].Final && p.Legs[i].Qty > 0 {
			leg = &p.Legs[i]
			break
		}
	}
	if leg.SignalID == "" || leg.Qty <= 0 {
		return 0, false
	}
	book, _, ok := mentorStopBook(nt)
	if !ok {
		return 0, false
	}
	orders := mentorBrokerStopOrders(book, leg.SignalID, leg.Wire)
	if len(orders) == 0 {
		return 0, false
	}
	return orders[0].StopPrice, true
}

// mentorLatchStopMoveManual latches a leg into manual management: the drive
// sends nothing more for it until the latch clears.
func (at *AutoTrader) mentorLatchStopMoveManual(signalID string, wireLeg int, adoptedPrice float64) {
	if at == nil {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorStopMoveManual == nil {
		at.mentorStopMoveManual = map[string]*mentorStopMoveManual{}
	}
	at.mentorStopMoveManual[mentorStopMoveKey(signalID, wireLeg)] = &mentorStopMoveManual{
		signalID:     signalID,
		wireLeg:      wireLeg,
		adoptedPrice: adoptedPrice,
	}
}

// mentorSweepManualLatches clears a manual latch only when (a) the position is
// gone (unregistered, or Qty ≤ 0 on that leg), or (b) the owner moved the broker
// stop BY HAND — the broker price CHANGES from the price adopted at latch time,
// which the drive re-adopts into the belief WITHOUT sending anything.
func (at *AutoTrader) mentorSweepManualLatches(nt *ntTrader.TCPTrader) {
	if at == nil {
		return
	}
	at.mentorExitMu.Lock()
	if len(at.mentorStopMoveManual) == 0 {
		at.mentorExitMu.Unlock()
		return
	}
	type man struct {
		key string
		m   *mentorStopMoveManual
	}
	list := make([]man, 0, len(at.mentorStopMoveManual))
	for k, v := range at.mentorStopMoveManual {
		list = append(list, man{k, v})
	}
	at.mentorExitMu.Unlock()

	book, recvAt, ok := mentorStopBook(nt)
	if !ok || recvAt.IsZero() || time.Since(recvAt) > snapshotMaxAge() {
		return // no fresh book → can't clear anything
	}
	halfTick := at.mentorInstrumentTick() / 2
	for _, e := range list {
		at.mentorExitMu.Lock()
		p := at.mentorLivePos[e.m.signalID]
		at.mentorExitMu.Unlock()
		if p == nil {
			at.mentorClearStopMoveLatch(e.key)
			continue
		}
		// Leg gone (Qty ≤ 0) → the latch is moot.
		found := false
		for i := range p.Legs {
			if p.Legs[i].SignalID == e.m.signalID && p.Legs[i].Wire == e.m.wireLeg && p.Legs[i].Qty > 0 {
				found = true
				break
			}
		}
		if !found {
			at.mentorClearStopMoveLatch(e.key)
			continue
		}
		// Owner override: the broker stop CHANGED from the adopted-at-latch price
		// (the owner moved it by hand) → clear the latch and re-adopt, no send.
		stops := mentorBrokerStopOrders(book, e.m.signalID, e.m.wireLeg)
		if len(stops) > 0 && math.Abs(stops[0].StopPrice-e.m.adoptedPrice) > halfTick {
			newPrice := stops[0].StopPrice
			at.mentorClearStopMoveLatch(e.key)
			if at.mentorAdoptBrokerStop(e.m.signalID, e.m.wireLeg, newPrice) {
				at.logInfof("🧑‍🏫 mentor leg %s#%d manual management cleared — broker stop changed to %.2f (re-adopted, nothing sent)", e.m.signalID, e.m.wireLeg, newPrice)
			}
		}
	}
}

// mentorVerifyPendingStopMoves renders one verdict per pending stop move against
// the broker's latest order book. Called once per CLOSED candle, BEFORE the
// per-position drive, so a just-corrected belief feeds the 1:1/trail math.
func (at *AutoTrader) mentorVerifyPendingStopMoves(nt *ntTrader.TCPTrader) {
	if at == nil || !mentorStopMoveVerifyEnabled() {
		return
	}
	at.mentorExitMu.Lock()
	hasPending := len(at.mentorPendingStopMoves) > 0
	type entry struct {
		key string
		mv  *mentorPendingStopMove
	}
	list := make([]entry, 0, len(at.mentorPendingStopMoves))
	for k, v := range at.mentorPendingStopMoves {
		list = append(list, entry{k, v})
	}
	at.mentorExitMu.Unlock()

	if !hasPending {
		at.mentorSweepManualLatches(nt)
		return
	}

	book, recvAt, ok := mentorStopBook(nt)
	now := time.Now()
	// No book at all → no verdict; the protection net (F4) owns the move.
	if !ok || recvAt.IsZero() {
		return
	}
	stale := now.Sub(recvAt) > snapshotMaxAge()
	halfTick := at.mentorInstrumentTick() / 2

	for _, e := range list {
		mv := e.mv
		key := e.key
		// A verdict needs the FIRST snapshot NEWER than send+2s; an older one
		// proves nothing (the move may not have reached NT8 yet).
		if !recvAt.After(mv.sendAt.Add(2 * time.Second)) {
			continue
		}
		// F-6 (release #22): a verdict for a leg that is KNOWN closed (leg 1
		// after its confirmed partial, a flat side, a dropped position) is a
		// LATE confirmation — recorded and ignored, never acted on: no WARN,
		// no re-send, no P0, no belief write. #651 (10-14 01:06/01:07) and
		// #645/#647 WARNed "NO live stop order" for exactly these legs.
		if closed, why := at.mentorLegKnownClosed(mv.signalID, mv.side, mv.wireLeg); closed {
			at.mentorCount("stop_move_verify_leg_closed")
			at.logInfof("🧑‍🏫 mentor stop-move verify: %s leg %d is closed (%s) — late verdict recorded, ignored (F-6)", mv.signalID, mv.wireLeg, why)
			at.mentorDeletePendingStopMove(key)
			continue
		}
		if stale {
			// No fresh book → no verdict yet; warn once and wait.
			if !mv.staleWarned {
				mv.staleWarned = true
				at.mentorCount("stop_move_stale_book")
				at.logWarnf("🧑‍🏫 mentor stop-move verify: stale book (%s old) — no verdict yet for %s leg %d", now.Sub(recvAt).Round(time.Second), mv.signalID, mv.wireLeg)
			}
			continue
		}

		stops := mentorBrokerStopOrders(book, mv.signalID, mv.wireLeg)
		if len(stops) == 0 {
			// No live stop order → leave it to the protection net (F4); warn
			// once, then stop managing this move.
			if !mv.noOrderWarned {
				at.mentorCount("stop_move_no_order")
				at.logWarnf("🧑‍🏫 mentor stop-move verify: NO live stop order for %s leg %d — leaving to the protection net (F4)", mv.signalID, mv.wireLeg)
			}
			at.mentorDeletePendingStopMove(key)
			continue
		}

		// All matching stops must be within half a tick of the wanted price.
		applied := true
		brokerPrice := 0.0
		for _, o := range stops {
			if math.Abs(o.StopPrice-mv.want) > halfTick {
				applied = false
				brokerPrice = o.StopPrice
				break
			}
			brokerPrice = o.StopPrice
		}
		if applied {
			at.mentorCount("stop_move_confirmed")
			at.logInfof("\U0001F9D1\u200D\U0001F3EB stop move CONFIRMED at NT8: %s leg %d stop %.2f", mv.signalID, mv.wireLeg, mv.want)
			at.mentorDeletePendingStopMove(key)
			at.mentorResetStopMoveEpisode(key)
			at.mentorClearStopMoveLatch(key)
			continue
		}

		// Mismatch. A broker stop TIGHTER than the wanted price is adopted at
		// once — never re-sent (a re-send would WIDEN it). Tighter means the
		// broker sits closer to the position than the loop wanted: long → above,
		// short → below.
		tighter := false
		switch mv.side {
		case "long":
			tighter = brokerPrice > mv.want+halfTick
		case "short":
			tighter = brokerPrice < mv.want-halfTick
		}
		if tighter {
			at.mentorCount("stop_move_broker_tighter")
			if at.mentorAdoptBrokerStop(mv.signalID, mv.wireLeg, brokerPrice) {
				at.logWarnf("🧑‍🏫 stop move: broker stop %.2f is TIGHTER than wanted %.2f — kept", brokerPrice, mv.want)
			}
			at.mentorDeletePendingStopMove(key)
			at.mentorResetStopMoveEpisode(key)
			at.mentorClearStopMoveLatch(key)
			continue
		}

		// Wider (a refusing NT8): the frame was sent but NOT applied. Misses are
		// counted per (signal, leg) and SURVIVE superseding moves, so a fresh
		// want every candle still reaches "manage by hand" on the second miss.
		misses, startMs := at.mentorBumpStopMoveEpisode(key)
		if misses == 1 {
			at.mentorCount("stop_move_not_applied")
			at.logErrorf("🚨 STOP MOVE NOT APPLIED at NT8: %s leg %d want %.2f broker %.2f", mv.signalID, mv.wireLeg, mv.want, brokerPrice)
			// ONE P0 per episode (the event id keys on the episode start, never
			// on the wanted price — a fresh want must not fire a fresh P0).
			at.emitAlert("P0", "stop-move-not-applied",
				fmt.Sprintf("stop-move:%s#%d:%d", mv.signalID, mv.wireLeg, startMs),
				"Stop move not applied at NT8",
				fmt.Sprintf("%s leg %d: wanted %.2f, broker at %.2f", mv.signalID, mv.wireLeg, mv.want, brokerPrice))
			// Re-send the SAME wanted price ONCE (wider case only).
			if err := mentorMoveStopForSignalWire(nt, mv.signalID, mv.side, mv.want, mv.wireLeg); err != nil {
				at.mentorCount("stop_move_resend_failed")
				at.logErrorf("🚨 mentor stop-move re-send FAILED for %s leg %d: %v", mv.signalID, mv.wireLeg, err)
				continue // keep the pending check; the next candle re-renders
			}
			at.mentorCount("stop_move_resend_sent")
			// Rewind the send clock so the re-send is re-verified next snapshot.
			at.mentorExitMu.Lock()
			if cur := at.mentorPendingStopMoves[key]; cur != nil {
				cur.sendAt = now
			}
			at.mentorExitMu.Unlock()
			continue
		}

		// Second consecutive miss → broker truth wins; manage by hand + latch.
		at.mentorCount("stop_move_not_applied_twice")
		at.emitAlert("P0", "stop-move-not-applied-twice",
			fmt.Sprintf("stop-move-twice:%s#%d:%d", mv.signalID, mv.wireLeg, startMs),
			"Stop not applied twice — manage by hand",
			fmt.Sprintf("%s leg %d: wanted %.2f, broker at %.2f", mv.signalID, mv.wireLeg, mv.want, brokerPrice))
		if at.mentorAdoptBrokerStop(mv.signalID, mv.wireLeg, brokerPrice) {
			at.logErrorf("🚨 mentor stop-move: %s leg %d belief corrected to broker stop %.2f — manage by hand", mv.signalID, mv.wireLeg, brokerPrice)
		}
		at.mentorLatchStopMoveManual(mv.signalID, mv.wireLeg, brokerPrice)
		at.mentorDeletePendingStopMove(key)
	}

	at.mentorSweepManualLatches(nt)
}

// mentorArmResonanceOnISB flips an open PHL/PLH position into mode A when a
// same-direction ISB entry passes every placement gate EXCEPT the never-add
// gate, within 3 candles of the fill: leg 1's +1R TP STAYS (the 1:1 partial
// still fires), the RUNNER's TP moves out to the level beyond the old high
// (CX-3), NO trail, NO 1:1 tightening [D2.4 p1]. m134 (b)/(c): arming does NOT
// move any stop — the break-even is deferred to the closed-1m exit drive
// (mentorExitDriveResonance). m151: called from the add-gate refusal in
// mentorPlaceIntent (the resonance ISB is a hold signal, not an entry), and the
// mode-A flip is PERSISTED so a restart rebuilds A, not the row's B.
func (at *AutoTrader) mentorArmResonanceOnISB(isbSide string) {
	if at == nil || !at.mentorEnabled() {
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		return
	}
	for _, p := range at.mentorLivePosList() {
		if p == nil {
			continue
		}
		armed, _, _ := mentorMaybeArmResonance(at, &p.Pos, isbSide, p.BarsSinceFill, p.RunnerTarget)
		if !armed {
			continue
		}
		// m151 (4): persist mode A on the open position's armed row so a restart
		// rebuilds A (plus the pending BE), not the row's original B.
		at.mentorPersistResonanceMode(p)
		// Mode A, CX-3 (release #25): the RUNNER's TP moves out to the level
		// beyond the old high through the leg-scoped modify_bracket (wire leg
		// 2) — leg 1's +1R TP stays (R-RES). It was logged as "WOULD set TP …
		// UNWIRED" and never sent from 10-05 to 10-14. Knob
		// mentor_resonance_runner_tp_old_high (nil → ON); OFF = logged only.
		at.mentorResonanceRunnerTP(nt, p, p.RunnerTarget)
		at.logInfof("🧑‍🏫 mentor resonance ARMED: %s %s → mode A (runner BE deferred to the exit drive; no trail; the 1:1 partial still fires) [D2.4 p1]", p.Pos.Symbol, p.Pos.Side)
	}
}

// mentorPersistResonanceMode writes mode A onto the open position's armed row
// (m151 fix 4): the arm flips the live position in memory only, so a restart
// would rebuild the row's original B while the runner-TP modify stays out at
// the broker. Counted either way; a failed or empty write WARNs, never silent.
func (at *AutoTrader) mentorPersistResonanceMode(p *mentorLivePos) {
	if at == nil || p == nil || at.store == nil {
		return
	}
	sig := p.Legs[0].SignalID
	if sig == "" {
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		at.mentorCount("resonance_persist_failed")
		at.logWarnf("🧑‍🏫 mentor resonance mode A NOT persisted for %s — no armed ledger (a restart would rebuild B)", sig)
		return
	}
	n, err := ledger.SetExitModeBySignal(sig, "A-resonance")
	if err != nil || n == 0 {
		at.mentorCount("resonance_persist_failed")
		at.logWarnf("🧑‍🏫 mentor resonance mode A NOT persisted for %s — %d row(s) changed, err=%v (a restart would rebuild B)", sig, n, err)
		return
	}
	at.mentorCount("resonance_persisted")
}

// mentorResonanceBEMinRR is the minimum distance BEYOND entry (in R) the close
// must reach before a mode-A position's break-even is placed while leg 1's 1:1
// partial has NOT yet confirmed. A BE stop one tick from price gets scratched;
// 0.25R gives it room [CTO default, m151 — the owner may change it; disclosed
// in the guide].
const mentorResonanceBEMinRR = 0.25

// mentorExitDriveResonance is the mode-A break-even on one CLOSED 1m candle
// (plan §Exits B). m134 (b)/(d): the stop moves to break-even only AFTER the
// 1:1 partial is confirmed (scaledNow) OR — with no partial yet — once the
// close is at least 0.25R beyond entry (mirrored for short). Once armed, mode A
// holds the native bracket (no trail, no 1:1 tightening).
// m134 (c) + m151 (1): the leg.Stop / ArmedBE belief is written ONLY after
// EVERY leg that needs BE was applied by the broker; an unapplied leg is
// retried on the next closed candle (tracked by leg.Stop).
// m151 (2): ALL open legs move to BE — leg 1 (wire 1) included [D2.4 @02:29].
func (at *AutoTrader) mentorExitDriveResonance(nt *ntTrader.TCPTrader, p *mentorLivePos, c float64, scaledNow bool, exited [3]bool) {
	if p == nil || p.Pos.ArmedBE {
		return
	}
	pos := &p.Pos
	legs := mentorResonanceOpenLegs(p, exited)
	if len(legs) == 0 {
		return
	}
	side := pos.Side
	tick := at.mentorInstrumentTick()
	be := mentorBEStopPrice(side, pos.Entry, tick)
	// m151 (3): before the partial is confirmed, BE needs the close at least
	// 0.25R beyond entry (long; mirrored for short). After the partial it moves
	// on the next closed candle — the wrong-side guard in mentorMoveLegStop keeps
	// it on the protective side (a through-the-market BE is closed, not sent).
	if !scaledNow {
		dist := pos.R * mentorResonanceBEMinRR
		reached := (side == "long" && c >= pos.Entry+dist) || (side != "long" && c <= pos.Entry-dist)
		if !reached {
			at.mentorCount("resonance_be_waiting")
			return
		}
	}
	allApplied := true
	brokerTighter := false
	for _, leg := range legs {
		if legAtOrBeyondBE(side, leg.Stop, be) {
			continue // already at BE (or a tighter broker stop) from a prior candle
		}
		err := at.mentorMoveLegStop(nt, side, leg, be)
		switch {
		case errors.Is(err, errMentorStopBrokerTighter):
			// m151 (6): the broker holds a tighter stop; mentorJudgeAgainstBroker
			// already wrote leg.Stop to it, so the belief matches.
			brokerTighter = true
		case errors.Is(err, errMentorLegClosed):
			// closed on its own TP — not a blocker.
			at.mentorCount("stop_move_skipped_leg_closed")
		case errors.Is(err, errMentorStopThroughMarket):
			// m176 (Sonnet probe 8): the wrong-side BE closed the WHOLE symbol at
			// market (the close wire is a full flatten) — the position is flat, so
			// every remaining open leg would be a redundant close on an
			// already-flat position. Stop the leg loop here.
			at.mentorCount("stop_move_skipped_leg_closed")
			return
		case !mentorMoveApplied(err):
			allApplied = false
			if err != nil && !mentorMoveErrAlreadyLogged(err) {
				at.logWarnf("🧑‍🏫 mentor resonance BE leg %d move FAILED: %v", leg.Wire, err)
			}
		default:
			leg.Stop = be
		}
	}
	if !allApplied {
		return // at least one leg is not at BE — ArmedBE stays false, retried next candle
	}
	pos.Stop = be
	pos.ArmedBE = true
	if brokerTighter {
		at.mentorCount("resonance_be_broker_tighter")
	} else {
		at.mentorCount("resonance_be_after_partial_or_profit")
	}
	at.logInfof("🧑‍🏫 mentor resonance BE: %s stop(s) → %.2f (entry %.2f) [D2.4 p1 @03:36–03:45]", legs[0].SignalID, be, pos.Entry)
}

// legAtOrBeyondBE reports whether a leg's stop is already at break-even or on
// the protective side of it (a tighter broker stop). Used to skip legs already
// applied on a prior candle (m151 fix 1: per-leg retry, tracked by leg.Stop).
func legAtOrBeyondBE(side string, stop, be float64) bool {
	if strings.EqualFold(side, "short") {
		return stop <= be
	}
	return stop >= be
}

// mentorMarkPosExitedAtMarket marks every leg of a live position exited after a
// successful wrong-side market close (m151 fix 5): the close wire is a FULL
// FLATTEN of the symbol's position, so the exit drive must not retry the move
// on the next candle (Sonnet saw 3 full-flatten closes on 3 candles).
func (at *AutoTrader) mentorMarkPosExitedAtMarket(signalID string) {
	if at == nil || signalID == "" {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	p := at.mentorLivePos[signalID]
	if p == nil {
		return
	}
	for i := range p.Legs {
		p.Legs[i].Exited = true
	}
}

// pokeMentorExitDrive wakes the per-trader event loop after a mentor fill so
// the exit drive re-evaluates the freshly registered position promptly.
func (at *AutoTrader) pokeMentorExitDrive() {
	if at == nil {
		return
	}
	if l := at.armedEvent.Load(); l != nil {
		l.poke()
	}
}

// isISBEntryIntent reports whether an evaluator intent is an ISB entry (the
// resonance trigger: a same-side ISB within 3 candles of a PHL/PLH fill).
func isISBEntryIntent(in mentor.Intent) bool {
	if !strings.EqualFold(in.Setup, "ISB") {
		return false
	}
	return in.Action == mentor.PlaceStopEntry || in.Action == mentor.PlaceStopLimitEntry
}

// mentorInstrumentTick is the instrument tick for the exit drive (MNQ 0.25 fallback).
func (at *AutoTrader) mentorInstrumentTick() float64 {
	if t := market.FuturesTickSize(at.futuresSymbol()); t > 0 {
		return t
	}
	return 0.25
}
