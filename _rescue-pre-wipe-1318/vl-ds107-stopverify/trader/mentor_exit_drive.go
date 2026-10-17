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
	// F1 STOP-MOVE TRUTH: render verdicts on any pending stop move against the
	// broker's latest book BEFORE this candle's drive computes new moves — a
	// just-corrected belief feeds the 1:1/trail math, not a stale one.
	at.mentorVerifyPendingStopMoves(nt)
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
				mentorCount("exit_drive_flat_unregistered")
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
		mentorCount("leg1_at_target")
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
		// SWING: no moves — the swing rules own it [D4.2].
		return
	case "C":
		// C (confluence): the stop NEVER moves [D3.4 p3 @07:38: "hold… at least
		// risk reward 1-2"]. No action.
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

	// scaledNow / exitedAtMs are read ONCE under mentorExitMu (I6): the receipt
	// (mentorMarkLeg1Scaled) and the I7 fallback write Scaled/Leg1ExitedAtMs from
	// other goroutines, so an unlocked read races.
	scaledNow, exitedAtMs := at.mentorScaledState(p)
	// scaledBefore is whether leg 1's exit was confirmed on a PRIOR candle: the
	// runner's trail begins on the NEXT candle after the confirmation, never on
	// the candle that crosses it (I6 next-candle rule).
	scaledBefore := scaledNow && mentorLeg1ExitedBefore(exitedAtMs, candleOpenMs)
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
	// first, ONCE — and the close only when in profit. modify_bracket is
	// LOG-only (Q2 unproven). The resolution reads the JUST-closed fill candle
	// (BarsSinceFill == 1): its high/low decides whether +1R printed first.
	if pos.Origin == "ISB" && !scaledNow && p.BarsSinceFill == 1 {
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
					if !errors.Is(err, errMentorStopManual) {
						at.logWarnf("🧑‍🏫 mentor BE leg %d move FAILED: %v", i, err)
					}
					continue
				}
				leg.Stop = pos.Entry
			}
			pos.ArmedBE = true
			pos.Stop = pos.Entry
			mentorCount("be_armed_both_legs")
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
				mentorCount("leg1_at_target")
			}
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
			if !isRunner && scaledNow {
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
				if !errors.Is(err, errMentorStopManual) {
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

// mentorMoveLegStop sends one leg's stop move through the signal-keyed
// move_stop frame, guarded by mentorNeverWiden (never widen, D2.3 p1 @18:08).
// A latched leg (two broker refusals) sends NOTHING — the belief is the broker
// truth and the owner manages it by hand.
func (at *AutoTrader) mentorMoveLegStop(nt *ntTrader.TCPTrader, side string, leg *mentorLeg, newStop float64) error {
	if leg == nil || leg.SignalID == "" {
		return fmt.Errorf("mentor leg stop move: no leg signal id")
	}
	if at != nil && at.mentorStopMoveManualBlocked(leg.SignalID, leg.Wire) {
		return errMentorStopManual
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
// wire leg: wire 1 → "<signal>-sl", wire 2 → "<signal>-sl2", wire 0 (single
// bracket) → every live -sl/-sl2 of the signal (a leg-less move_stop moves them
// together).
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
	default:
		names = []string{signalID + "-sl", signalID + "-sl2"}
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
		if stale {
			// No fresh book → no verdict yet; warn once and wait.
			if !mv.staleWarned {
				mv.staleWarned = true
				mentorCount("stop_move_stale_book")
				at.logWarnf("🧑‍🏫 mentor stop-move verify: stale book (%s old) — no verdict yet for %s leg %d", now.Sub(recvAt).Round(time.Second), mv.signalID, mv.wireLeg)
			}
			continue
		}

		stops := mentorBrokerStopOrders(book, mv.signalID, mv.wireLeg)
		if len(stops) == 0 {
			// No live stop order → leave it to the protection net (F4); warn
			// once, then stop managing this move.
			if !mv.noOrderWarned {
				mentorCount("stop_move_no_order")
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
			mentorCount("stop_move_confirmed")
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
			mentorCount("stop_move_broker_tighter")
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
			mentorCount("stop_move_not_applied")
			at.logErrorf("🚨 STOP MOVE NOT APPLIED at NT8: %s leg %d want %.2f broker %.2f", mv.signalID, mv.wireLeg, mv.want, brokerPrice)
			// ONE P0 per episode (the event id keys on the episode start, never
			// on the wanted price — a fresh want must not fire a fresh P0).
			at.emitAlert("P0", "stop-move-not-applied",
				fmt.Sprintf("stop-move:%s#%d:%d", mv.signalID, mv.wireLeg, startMs),
				"Stop move not applied at NT8",
				fmt.Sprintf("%s leg %d: wanted %.2f, broker at %.2f", mv.signalID, mv.wireLeg, mv.want, brokerPrice))
			// Re-send the SAME wanted price ONCE (wider case only).
			if err := mentorMoveStopForSignalWire(nt, mv.signalID, mv.side, mv.want, mv.wireLeg); err != nil {
				mentorCount("stop_move_resend_failed")
				at.logErrorf("🚨 mentor stop-move re-send FAILED for %s leg %d: %v", mv.signalID, mv.wireLeg, err)
				continue // keep the pending check; the next candle re-renders
			}
			mentorCount("stop_move_resend_sent")
			// Rewind the send clock so the re-send is re-verified next snapshot.
			at.mentorExitMu.Lock()
			if cur := at.mentorPendingStopMoves[key]; cur != nil {
				cur.sendAt = now
			}
			at.mentorExitMu.Unlock()
			continue
		}

		// Second consecutive miss → broker truth wins; manage by hand + latch.
		mentorCount("stop_move_not_applied_twice")
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
				if !errors.Is(err, errMentorStopManual) {
					at.logWarnf("🧑‍🏫 mentor resonance BE leg %d move FAILED: %v", i, err)
				}
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
