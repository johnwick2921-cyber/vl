package trader

import (
	"fmt"
	"strings"

	"vl/kernel/mentor"
	"vl/market"
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

// mentorISBPartialTP (item 8) resolves the ISB leg-1 exit point ONCE, on the
// fill candle's close: +1R if the fill candle reached it first, else the
// candle-3 (fill-candle) close when that is in profit. A losing close resolves
// to 0 — no scale-out there, the stop rules own a losing leg [D1.4 p1
// @11:59–13:28; D2.2 p3 @12:13]. The "whichever comes first" is read off the
// candle's high (long) / low (short): a high past +1R means +1R printed before
// the close.
func mentorISBPartialTP(pos mentorPosition, c, h, l float64) float64 {
	long := pos.Side == "long"
	if long {
		if h >= pos.Entry+pos.R {
			return pos.Entry + pos.R
		}
		if c > pos.Entry {
			return c
		}
		return 0
	}
	if l <= pos.Entry-pos.R {
		return pos.Entry - pos.R
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
	c, h, l := last.Close, last.High, last.Low
	// CTO gate (release #4): the loop must drop a position once the account
	// is flat on its side — nothing else ever unregistered it, so a closed
	// trade kept sending move_stop every candle. A read error proves
	// nothing (keep); a just-filled position may not be in the snapshot yet,
	// so it takes TWO consecutive flat reads after the fill candle.
	openSides, sidesOK := mentorDriveOpenSides(at)
	for _, p := range at.mentorLivePosList() {
		if p == nil || p.Pos.Symbol == "" {
			continue
		}
		p.BarsSinceFill++
		if sidesOK && !openSides[p.Pos.Side] {
			p.FlatReads++
			if p.BarsSinceFill >= 2 && p.FlatReads >= 2 {
				at.mentorUnregisterLivePos(p.Legs[0].SignalID)
				mentorCount("exit_drive_flat_unregistered")
				at.logInfof("🧑‍🏫 mentor exit-drive: %s %s is flat — dropped from the loop", p.Pos.Symbol, p.Pos.Side)
			}
			continue
		}
		p.FlatReads = 0
		at.mentorExitDrivePos(nt, p, c, h, l)
		at.mentorLogPositionState(&p.Pos, "exit-drive")
	}
}

// mentorDriveOpenSides is the loop's open-side read (a seam so the call-site
// pin can drive flat/open without a live broker).
var mentorDriveOpenSides = func(at *AutoTrader) (map[string]bool, bool) { return at.mentorOpenSidesForDrive() }

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

// mentorExitDrivePos drives ONE position on one closed candle (c/h/l).
func (at *AutoTrader) mentorExitDrivePos(nt *ntTrader.TCPTrader, p *mentorLivePos, c, h, l float64) {
	pos := &p.Pos
	side := pos.Side
	long := side == "long"
	trail := mentorTrailEnabled(at.mentorTrailTF())

	switch pos.Mode {
	case "swing":
		// SWING: no moves — the swing rules own it [D4.2].
		return
	case "C":
		// C (confluence): the stop NEVER moves [D4.2 p1 @14:57]. No action.
		return
	case "A-resonance":
		// A (resonance): both stops already at BE, leg 1's TP out to the
		// runner target (logged, unwired), NO trail, NO 1:1 tightening
		// [D2.4 p1]. Nothing to do on a candle — the native bracket holds.
		return
	}

	// ── Mode D guard: the spent-day runner is capped at 2 (placed by DS-103;
	// the loop ASSERTS it, never reduces). ────────────────────────────────
	if p.SpentDay && p.Legs[1].Qty > mentorSpentDayRunnerCap {
		mentorCount("spent_day_runner_over_cap")
		at.logWarnf("🧑‍🏫 mentor spent-day runner %d over the cap %d — asserting, not reducing [D5.1]", p.Legs[1].Qty, mentorSpentDayRunnerCap)
	}

	// scaledBefore is whether leg 1's TP was ALREADY confirmed exited on a
	// PRIOR candle (the broker receipt set Scaled): the runner's trail begins
	// on the NEXT candle after leg 1's TP (never on the candle that crosses it).
	scaledBefore := pos.Scaled
	// runnerPresent distinguishes the split (leg 1 + runner) from the single
	// bracket: only the split has a broker leg-1 TP to wait for.
	runnerPresent := p.Legs[1].Qty > 0 && p.Legs[1].SignalID != ""
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
	// first, ONCE — and the close only when in profit. modify_bracket is
	// LOG-only (Q2 unproven). The resolution reads the JUST-closed fill candle
	// (BarsSinceFill == 1): its high/low decides whether +1R printed first.
	if pos.Origin == "ISB" && !pos.Scaled && p.BarsSinceFill == 1 {
		if tp := mentorISBPartialTP(*pos, c, h, l); tp > 0 {
			p.Legs[0].TP = tp
			leg1Target = tp
			at.logInfof("🧑‍🏫 mentor ISB leg1 modify_bracket WOULD set TP %.2f (candle-3 close / +1R, whichever first) — UNWIRED (Q2 unproven), logged not sent", tp)
			mentorCount("modify_bracket_isb_logged")
		}
	}

	// ── (1) B BE: arm both legs' stops to entry once price covers HALF the
	// distance to the trade target (mentorBEHalfDistance). ─────────────────
	if !pos.ArmedBE {
		half := mentorBEHalfDistance(*pos)
		armed := false
		if long {
			armed = h >= pos.Entry+half
		} else {
			armed = l <= pos.Entry+half
		}
		if armed {
			for i := range p.Legs {
				leg := &p.Legs[i]
				if leg.SignalID == "" || leg.Qty <= 0 {
					continue
				}
				if err := at.mentorMoveLegStop(nt, side, leg, pos.Entry); err != nil {
					at.logWarnf("🧑‍🏫 mentor BE leg %d move FAILED: %v", i, err)
					continue
				}
				leg.Stop = pos.Entry
			}
			pos.ArmedBE = true
			pos.Stop = pos.Entry
			mentorCount("be_armed_both_legs")
		}
	}

	// ── (3) the 1:1 point → the runner's trail begins NEXT candle. For a
	// SPLIT, leg 1 exits at its native TP and Scaled is marked ONLY on the
	// broker's position_close receipt of leg 1's TP (B2, BUILD-ALL L9) — the
	// candle-price guess that used to set it here is GONE, so the trail never
	// begins before the broker actually confirms leg 1 exited. A SINGLE leg has
	// no broker leg-1 TP to wait for, so its 1:1 point stays candle-priced.
	// (Final does NOT mean "exited" — canonical semantics: Final marks the
	// RUNNER.)
	if !pos.Scaled && !runnerPresent {
		scaleAt := pos.Entry + pos.R
		if !long {
			scaleAt = pos.Entry - pos.R
		}
		if (long && h >= scaleAt) || (!long && l <= scaleAt) {
			pos.Scaled = true
			mentorCount("leg1_at_target")
		}
	}

	// ── (4) the 1:1 rule on every closed candle (+ the runner's trail). ────
	// Skipped on the candle that JUST armed BE (wasArmed=false): the course is
	// "BE at half the distance, THEN live 1:1 on every closed candle".
	if pos.ArmedBE && wasArmed {
		for i := range p.Legs {
			leg := &p.Legs[i]
			if leg.SignalID == "" || leg.Qty <= 0 {
				continue
			}
			// Canonical semantics: Final marks the RUNNER (the leg that holds
			// to the trade target). Leg 1 (Final=false) is the partial that
			// exits at its own TP — once Scaled its stop is moot.
			isRunner := leg.Final
			if !isRunner && pos.Scaled {
				continue
			}
			target := leg1Target
			applyTrail := false
			trailPrice := 0.0
			if isRunner {
				target = runnerTarget
				applyTrail = trail && scaledBefore // the 1:1 point printed on a PRIOR candle
				if applyTrail {
					// Item 7 ruling [X8 @15:40–17:13]: 1 tick BEYOND the closed
					// candle's low (long) / high (short), never exactly at it.
					if long {
						trailPrice = l - at.mentorInstrumentTick()
					} else {
						trailPrice = h + at.mentorInstrumentTick()
					}
				}
			}
			next := mentorLegStopB(side, leg.Stop, c, target, trailPrice, applyTrail)
			if next == leg.Stop {
				continue
			}
			if err := at.mentorMoveLegStop(nt, side, leg, next); err != nil {
				at.logWarnf("🧑‍🏫 mentor %s leg %d stop move FAILED: %v", side, i, err)
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

// mentorMoveLegStop sends one leg's stop move through the signal-keyed
// move_stop frame, guarded by mentorNeverWiden (never widen, D1.2 p2 @00:08).
func (at *AutoTrader) mentorMoveLegStop(nt *ntTrader.TCPTrader, side string, leg *mentorLeg, newStop float64) error {
	if leg == nil || leg.SignalID == "" {
		return fmt.Errorf("mentor leg stop move: no leg signal id")
	}
	if refuse, why := mentorNeverWiden(side, leg.Stop, newStop); refuse {
		mentorCount("widen_refused")
		return fmt.Errorf("mentor leg stop move refused: %s", why)
	}
	if err := mentorMoveStopForSignalWire(nt, leg.SignalID, side, newStop, leg.Wire); err != nil {
		mentorCount("move_stop_failed")
		return err
	}
	mentorCount("move_stop_sent")
	return nil
}

// mentorArmResonanceOnISB flips an open PHL/PLH position into mode A the moment
// a same-direction ISB intent appears within 3 candles of the fill: both stops
// move to break-even NOW, leg 1's TP would move out to the runner target
// (logged, unwired), NO trail, NO 1:1 tightening [D2.4 p1]. Called from the
// evaluator's intent loop when an ISB entry is emitted.
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
		armed, _, _ := mentorMaybeArmResonance(&p.Pos, isbSide, p.BarsSinceFill, p.RunnerTarget)
		if !armed {
			continue
		}
		// Move BOTH legs' stops to BE immediately (the resonance arms "at that
		// moment", not on the next candle).
		for i := range p.Legs {
			leg := &p.Legs[i]
			if leg.SignalID == "" || leg.Qty <= 0 {
				continue
			}
			if err := at.mentorMoveLegStop(nt, p.Pos.Side, leg, p.Pos.Entry); err != nil {
				at.logWarnf("🧑‍🏫 mentor resonance BE leg %d move FAILED: %v", i, err)
				continue
			}
			leg.Stop = p.Pos.Entry
		}
		// Mode A: leg 1's TP would move OUT to the runner target (Q2 — the
		// modify_bracket frame is UNWIRED for live until one owner-attended SIM
		// proof, so we LOG the change we WOULD make; stops still moved).
		runnerTarget := p.Pos.Target
		if runnerTarget == 0 {
			runnerTarget = p.Legs[0].TP
		}
		if runnerTarget > 0 {
			at.logInfof("🧑‍🏫 mentor resonance leg1 modify_bracket WOULD set TP %.2f (runner target) — UNWIRED (Q2 unproven), logged not sent", runnerTarget)
			mentorCount("modify_bracket_resonance_logged")
		}
		at.logInfof("🧑‍🏫 mentor resonance ARMED: %s %s → mode A (BE, no trail, no 1:1) [D2.4 p1]", p.Pos.Symbol, p.Pos.Side)
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
