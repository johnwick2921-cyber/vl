package trader

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── MENTOR LIVE POSITION REGISTRY (the exit-drive's input) ─────────────────
//
// dev's mentor entry is ONE armed row → ONE position row (no split legs — #353
// was rejected and redesigned into one-entry-two-brackets). The live position
// the exit-drive loop drives is that whole position as a SINGLE leg: Legs[0] =
// the whole position (Final = the runner), Legs[1] empty. The registry is keyed
// by the entry signal id and guarded by mentorExitMu.

type mentorLeg struct {
	SignalID string  // the leg's own entry signal id (move_stop key)
	Qty      int     // contracts in this leg
	TP       float64 // the leg's own take-profit (leg 1 = +1R / fill-candle close; runner = the trade target)
	Stop     float64 // the leg's CURRENT resting stop (the loop writes back here)
	Final    bool    // marks the RUNNER — the leg that holds to the trade target
	Wire     int     // the AddOn leg this addresses on move_stop: 1 (-sl), 2 (-sl2), 3 (-sl3); 0 = the single bracket (every leg)
	// Exited (release #31 fold): the broker's position_close receipt for THIS
	// leg's own TP landed — the leg is closed and must receive no further
	// BE/trail move. Only the runner partial (wire 2) sets it in a 3-pair plan;
	// leg 1 uses Pos.Scaled, the runner (Final) uses the whole-position close.
	Exited bool
}

type mentorLivePos struct {
	Pos           mentorPosition // the exit-driver state (mode, entry, stop, R, …)
	Legs          [3]mentorLeg   // single bracket: [0] = the whole position; 2-pair split: [0] = leg 1, [1] = the runner; 3-pair (release #31): [0] = leg 1, [1] = the runner partial, [2] = the runner
	FillBarOpen   int64          // the fill candle's OpenTime
	FillBarClose  float64        // the fill candle's close (ISB leg-1 TP)
	BarsSinceFill int            // closed 1m candles since the fill
	FlatReads     int            // consecutive closed candles the account read flat on this side
	RunnerTarget  float64        // #360 item 5: the level beyond the old high (resonance runner target); 0 = none
	SpentDay      bool           // §7 spent day → mode D (runner ≤ 2)
	Confluence    bool           // R2 confluence flag → mode C
	// ISBPartialDone (release #22) — the ISB candle-3 partial was resolved (sent,
	// skipped or refused) and must never run again. A restart-rebuilt position
	// starts DONE: its fill candle is unknown, so a resolution after the restart
	// would read the WRONG candle and send a real modify off it (fail-closed:
	// leg 1 keeps the +1R TP already resting at NT8).
	ISBPartialDone bool
	// A SENT leg-1 TP modify awaiting the NT8 verdict (release #22,
	// mentorVerifyPendingTPModifies). TPModWant 0 = none pending. Guarded by
	// mentorExitMu.
	TPModWant        float64
	TPModSentAt      time.Time
	TPModStaleWarned bool
	// ISBPartialRetryCount (R-7, fold 2): 0 = no retry owed; 1 = the first
	// marketable modify SEND failed and is retried once on the next candle;
	// 2 = the retry was consumed (never retried again — fail-safe: the +1R TP
	// keeps resting).
	ISBPartialRetryCount int
	// TPModConfirmedWant (R-3 fold 2): the last leg-1 TP want the broker
	// CONFIRMED. While TPModWant == TPModConfirmedWant the verdict already
	// rendered — mentorVerifyPendingTPModifies skips it so a resting marketable
	// limit does not re-render CONFIRMED (and re-count it) every candle. A new
	// modify (a new want) resets it to 0.
	TPModConfirmedWant float64
	// BEWaitLogged — the "close not past break-even, BE waits" line fired once.
	BEWaitLogged bool
	// ModeSource / ModeWhy (P2-2 rel22) say WHERE Pos.Mode came from — the
	// staged fork, the row's persisted exit_mode, the SWING4H rule or the
	// course default — and why. Set by mentorBuildLivePos; read by the
	// once-per-arm mode line.
	ModeSource string
	ModeWhy    string
	// TrailTFMin is the frame the ENTRY was read on, and so the frame the
	// runner trails on: "nếu như mình trade khung nào thì mình gồng theo khung
	// đó, theo cây nến của khung đó" [X8 @18:02–18:14]. 2 = the X5-10 exec-2m
	// schedule read the setup — the ISB pair or PHL/PLH — on closed 2m candles
	// (carried from the intent's ExecTFMin via mentorExecFrames); 0 = the 1m
	// (every other setup, and every named setup when exec_2m_after_30m is
	// explicitly OFF).
	TrailTFMin int
	// X8Exited (m262 one-shot latch): the X8 trap-dodge market close was
	// SENT for this position — never send a second close while the fill is in
	// flight (never rely on NT8 idempotence). Set before the send; reset on a
	// failed send so the next candle may retry. Guarded by mentorExitMu.
	X8Exited bool
	// trailBar / trailBarOK: the closed TrailTFMin candle THIS drive pass
	// trails on, set by mentorExitDrive for a 2m position (ok=false while the
	// 2m candle is still forming — the trail waits for it to close).
	trailBar   market.Kline
	trailBarOK bool
}

// mentorRunnerLeg returns the RUNNER leg (Final=true) of a live position, or
// nil when none is registered. In a 3-pair plan (release #31) the runner is
// Legs[2] (wire leg 3); in a 2-pair plan it is Legs[1] (wire leg 2).
func mentorRunnerLeg(p *mentorLivePos) *mentorLeg {
	if p == nil {
		return nil
	}
	for i := range p.Legs {
		if p.Legs[i].Final && p.Legs[i].Qty > 0 && p.Legs[i].SignalID != "" {
			return &p.Legs[i]
		}
	}
	return nil
}

// mentorExitedLegs returns a snapshot of which wire legs have closed on their
// own TP receipt, read under mentorExitMu. The receipt (mentorMarkLeg2Exited)
// writes Exited from the close-sync goroutine, so an unlocked read races — the
// same shape as mentorScaledState (I6).
func (at *AutoTrader) mentorExitedLegs(p *mentorLivePos) [3]bool {
	var ex [3]bool
	if at == nil || p == nil {
		return ex
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	for i := range p.Legs {
		ex[i] = p.Legs[i].Exited
	}
	return ex
}

// mentorRunnerLegs (release #31 fold, D1) returns every leg the exit drive must
// manage as a RUNNER after leg 1's +1R partial: the Final runner (wire 3 in a
// 3-pair, wire 2 in a 2-pair) AND the runner partial (wire 2 in a 3-pair — the
// contract that sells at the first line). Excluded: leg 1 (wire 1), any leg
// whose own TP receipt already closed it (the Exited snapshot — read under
// mentorExitMu by the caller), and empty legs.
func mentorRunnerLegs(p *mentorLivePos, exited [3]bool) []*mentorLeg {
	if p == nil {
		return nil
	}
	var out []*mentorLeg
	for i := range p.Legs {
		leg := &p.Legs[i]
		if leg.SignalID == "" || leg.Qty <= 0 || exited[i] {
			continue
		}
		if leg.Wire == 1 {
			continue // leg 1's +1R partial: its stop is moot once Scaled
		}
		if leg.Final || leg.Wire == 2 {
			out = append(out, leg)
		}
	}
	return out
}

// mentorResonanceOpenLegs (m151 fix 2) returns EVERY open leg of a live position
// — leg 1 (wire 1) included — for the mode-A deferred break-even. The course
// moves the whole position to BE ("stop at break-even, let it run" [D2.4
// @02:29]); the runner-only helper (mentorRunnerLegs) is the trail's shape, not
// the resonance's. Excluded: empty legs and legs whose own TP receipt already
// closed them (the Exited snapshot).
func mentorResonanceOpenLegs(p *mentorLivePos, exited [3]bool) []*mentorLeg {
	if p == nil {
		return nil
	}
	var out []*mentorLeg
	for i := range p.Legs {
		leg := &p.Legs[i]
		if leg.SignalID == "" || leg.Qty <= 0 || exited[i] {
			continue
		}
		out = append(out, leg)
	}
	return out
}

// mentorFillExitMode resolves the exit branch a FILLED mentor arm is driven by
// (P2-2, rel22 mentor audit). It is never "": the exit drive has no case for
// an empty mode and ran B's stop moves on it — row 347 (10-13 17:50:54 CT) was
// driven with mode="" while its row says exit_mode='B', and a lost C would
// have broken "do NOT move the stop up at all" [D4.2 p1 @ 14:57]. Order:
//
//  1. the branch staged at entry (in memory, keyed by the row's scenario —
//     a B20 confluence upgrade may have moved it to C after authoring);
//  2. the branch PERSISTED on the row at authoring (armed_orders.exit_mode) —
//     the staging is lost across a restart or a second instance of the trader
//     between authoring and fill;
//  3. the setup's own rule: SWING4H holds by the 4h → "swing";
//  4. the COURSE default, B "normal" — trail every closed candle
//     [00-METHOD section 6 "Mode B — normal", D2.4 p1 @ 07:01]. No strategy-level
//     exit-mode default exists to read first (no such knob).
//
// class is the counter suffix (exit_mode_<class>); why is the log text.
func mentorFillExitMode(staged string, r store.ArmedOrderDB) (mode, class, why string) {
	if s := strings.TrimSpace(staged); s != "" {
		return s, "staged", "the branch staged at entry"
	}
	mode, derived := mentorRebuildExitMode(r)
	switch {
	case !derived:
		return mode, "persisted", "the staged branch was MISSING (restart or second instance between authoring and fill) — read from the row's persisted exit_mode"
	case mode == "swing":
		return mode, "derived_swing", "no staged or persisted branch — SWING4H holds by the 4h (the setup's own rule)"
	default:
		return mode, "course_default", "no staged or persisted branch and no strategy default exists — the course default B, normal: trail each closed candle [D2.4 p1 @ 07:01]"
	}
}

// mentorNoteFillExitMode logs and counts, ONCE per arm, which exit mode a
// registered fill is driven by and why (P2-2). A re-registration of the same
// signal with the same mode (a completing fill frame, the partial-then-full
// path) is silent; a mode CHANGE on re-registration is logged again.
func (at *AutoTrader) mentorNoteFillExitMode(r store.ArmedOrderDB, lp *mentorLivePos, priorMode string, hadPrior bool) {
	if lp == nil || (hadPrior && priorMode == lp.Pos.Mode) {
		return
	}
	at.mentorCount("exit_mode_" + lp.ModeSource)
	if lp.ModeSource == "staged" {
		at.logInfof("🧑‍🏫 mentor exit mode for %s (signal %s, row %d): %s — %s", r.Scenario, shortID(r.SignalID), r.ID, lp.Pos.Mode, lp.ModeWhy)
		return
	}
	at.logWarnf("🧑‍🏫 mentor exit mode for %s (signal %s, row %d): %s — %s [P2-2]", r.Scenario, shortID(r.SignalID), r.ID, lp.Pos.Mode, lp.ModeWhy)
}

// mentorLivePosMode reads a registered signal's mode (guarded).
func (at *AutoTrader) mentorLivePosMode(key string) (string, bool) {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	p, ok := at.mentorLivePos[key]
	if !ok || p == nil {
		return "", false
	}
	return p.Pos.Mode, true
}

// registerMentorLivePos (DS-107, one-row entry) builds the single-leg live
// position from the ONE filled mentor arm row and registers it under the entry
// signal id. Legs[0] = the whole position (Final = the runner); Legs[1] empty.
// Unconditional: the FULL-fill path owns the authoritative (re)registration —
// a completing fill frame may grow the position, so an overwrite is correct.
func (at *AutoTrader) registerMentorLivePos(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) {
	if lp := at.mentorBuildLivePos(r, u); lp != nil {
		priorMode, hadPrior := at.mentorLivePosMode(r.SignalID)
		at.mentorRegisterLivePos(r.SignalID, lp)
		at.mentorNoteFillExitMode(r, lp, priorMode, hadPrior) // P2-2: once per arm, which mode and why
		// I13 (rel9 fix): a FULL fill consumes the staged exit branch — prune it
		// HERE, never in the shared builder (a partial-fill build must leave the
		// staged C/swing mode for the remainder's full fill to read).
		at.deleteMentorExitMode(r.Scenario)
		// P3 (rel9): the shape counter fires on an ACTUAL registration.
		at.mentorCountRegistrationShape(lp)
		// A full fill proves the position is OPEN — clear any flat tombstone the
		// drive wrote on a stale flat read, so later partial sweeps stay correct.
		at.mentorClearFlatSignal(r.SignalID)
	}
}

// mentorBuildLivePos builds the single-leg live position from the ONE filled
// mentor arm row WITHOUT registering it (nil when the row names no side or
// signal). Split from registerMentorLivePos so the B1 expiry path can register
// the built position only when the signal is not already live (register-if-
// absent) instead of overwriting an in-flight position's BE/1:1/trail state.
func (at *AutoTrader) mentorBuildLivePos(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) *mentorLivePos {
	if r.SignalID == "" {
		return nil
	}
	side := strings.ToLower(strings.TrimSpace(r.Side))
	if side == "" {
		return nil
	}
	n := u.Quantity
	if n < 1 && r.FillQuantity > 0 {
		n = r.FillQuantity
	}
	if n < 1 {
		n = 1
	}
	entry := u.FillPrice
	pos := mentorPosition{
		Symbol:    at.futuresSymbol(),
		Side:      side,
		Origin:    r.Condition,
		Entry:     entry,
		Stop:      r.StopPx,
		Target:    r.TargetPx,
		R:         math.Abs(entry - r.StopPx),
		Contracts: n,
		Leg1:      n, // the whole position is one leg
		Leg2:      0,
		Leg1TP:    r.TargetPx,
	}
	// B5 (L8): the branch is per arm/signal id, never per side. P2-2 (rel22):
	// resolved explicitly — staged → persisted row → SWING4H → course default B;
	// never "" (an empty mode was driven as B silently).
	mode, modeClass, modeWhy := mentorFillExitMode(at.mentorExitMode(r.Scenario), r)
	pos.Mode = mode
	lp := &mentorLivePos{Pos: pos, ModeSource: modeClass, ModeWhy: modeWhy}
	// REL18-A #7: the runner target is keyed trader+scenario — two traders mint
	// the same scenario, and a bare-scenario key would hand B's fill A's target.
	if v, ok := mentorRunnerTargets.Load(mentorRunnerTargetKey(at.id, r.Scenario)); ok {
		lp.RunnerTarget, _ = v.(float64)
	}
	// X8 @18:02–18:14: the runner trails on the frame the entry was read on —
	// carried from the intent's ExecTFMin, keyed trader+scenario like the
	// runner target. Absent → the 1m (TrailTFMin 0).
	if v, ok := mentorExecFrames.Load(mentorRunnerTargetKey(at.id, r.Scenario)); ok {
		lp.TrailTFMin, _ = v.(int)
	}
	lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: n, TP: r.TargetPx, Stop: r.StopPx, Final: true}
	// REVIEW-SPLIT-2 P2: the split the entry frame ACTUALLY carried (after the
	// far-side gate and the leg1_tp check) registers as two legs under the one
	// signal id — leg 1 (-sl/-tp, its own TP) and the runner (-sl2/-tp2, the
	// trade target) — so every stop move names its leg. No record (single
	// bracket, or a restart lost it) → the single-leg view, whose leg-less
	// move_stop moves every live leg together (tightening only, never a widen).
	if split, ok := mentorSentSplit(at, r.SignalID); ok {
		if split.Leg1Qty < n {
			// Full split: leg 1 (-sl/-tp, its own TP) + the runner (-sl2/-tp2,
			// the trade target). Release #31: when the frame also carried a
			// leg-2 partial (leg2_qty/leg2_tp), the runner partial rides
			// -sl2/-tp2 and the runner itself is -sl3/-tp3.
			leg1, rest := split.Leg1Qty, n-split.Leg1Qty
			lp.Pos.Leg1, lp.Pos.Leg2, lp.Pos.Leg1TP = leg1, rest, split.Leg1TP
			lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: leg1, TP: split.Leg1TP, Stop: r.StopPx, Final: false, Wire: 1}
			if split.Leg2Qty > 0 && split.Leg2Qty < rest {
				// 3-pair plan: leg 2 = the runner partial (its own line TP),
				// leg 3 = the runner (the trade target, trails).
				runner := rest - split.Leg2Qty
				lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: split.Leg2Qty, TP: split.Leg2TP, Stop: r.StopPx, Final: false, Wire: 2}
				lp.Legs[2] = mentorLeg{SignalID: r.SignalID, Qty: runner, TP: r.TargetPx, Stop: r.StopPx, Final: true, Wire: 3}
			} else {
				lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: rest, TP: r.TargetPx, Stop: r.StopPx, Final: true, Wire: 2}
			}
		} else {
			// I10: a partial fill ≤ leg 1's quantity means ONLY leg 1 is live —
			// the wire holds only the leg-1 bracket at 1:1. Register leg 1 only,
			// with Leg1TP = split.Leg1TP (the 1:1 target), NOT the trade target.
			lp.Pos.Leg1, lp.Pos.Leg2 = n, 0
			lp.Pos.Leg1TP = split.Leg1TP
			lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: n, TP: split.Leg1TP, Stop: r.StopPx, Final: false, Wire: 1}
		}
	}
	// NOTE (I13 rel9 fix): the staged exit branch has been READ into lp.Pos.Mode
	// above but is NOT pruned here — the prune moved to the REGISTRATION sites
	// (registerMentorLivePos after a FULL fill, and mentorCancelArm's terminal
	// branches). Pruning in this shared builder lost the staged C/swing mode when
	// a partial fill (B1 expiry / I4 cancel) built the position and the remainder
	// then filled before the cancel settled — the full-fill rebuild read Mode "".
	return lp
}

// mentorSentSplit reads the split the entry frame carried (a seam so the
// registration pin can drive it without a live AddOn connection).
var mentorSentSplit = func(at *AutoTrader, signalID string) (ntTrader.SentSplit, bool) {
	nt := at.armedTrader()
	if nt == nil {
		return ntTrader.SentSplit{}, false
	}
	return nt.SplitSentFor(signalID)
}

// mentorRegisterLivePos stores a filled position under its signal id. A nil
// position or an empty key is ignored (fail-closed: the loop only sees what was
// actually registered).
func (at *AutoTrader) mentorRegisterLivePos(key string, p *mentorLivePos) {
	if at == nil || key == "" || p == nil {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorLivePos == nil {
		at.mentorLivePos = map[string]*mentorLivePos{}
	}
	at.mentorLivePos[key] = p
}

// mentorRegisterLivePosIfAbsent (B1 defensive fold, CTO 2026-10-05) registers a
// position ONLY when the signal is not already live — an atomic check-and-set
// under mentorExitMu. Returns true when it wrote. The B1 expiry path calls this
// so a partial-then-expiry registration can never reset an ALREADY-registered
// position's BE-armed / Scaled / trail state mid-trade (the full-fill path owns
// the authoritative overwrite and is unchanged).
func (at *AutoTrader) mentorRegisterLivePosIfAbsent(key string, p *mentorLivePos) bool {
	if at == nil || key == "" || p == nil {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorLivePos == nil {
		at.mentorLivePos = map[string]*mentorLivePos{}
	}
	if _, exists := at.mentorLivePos[key]; exists {
		return false
	}
	at.mentorLivePos[key] = p
	return true
}

// mentorUnregisterLivePos removes a position when its side goes flat.
func (at *AutoTrader) mentorUnregisterLivePos(key string) {
	if at == nil || key == "" {
		return
	}
	at.mentorExitMu.Lock()
	delete(at.mentorLivePos, key)
	// P3 (rel9): tombstone the signal so a stale partial-fill sweep (B1 expiry /
	// I4 cancel, re-run every bar while the row is cancel_pending) can never
	// RE-register a position the drive already dropped as flat.
	if at.mentorFlatSignals == nil {
		at.mentorFlatSignals = map[string]struct{}{}
	}
	at.mentorFlatSignals[key] = struct{}{}
	at.mentorExitMu.Unlock()
	// I9 (U3): forget the split record ONLY when the armed row is TERMINAL.
	// While the row is still working its REMAINDER may still be at the broker —
	// a forget here would leave a later re-registration without its split legs.
	if at.mentorArmedRowTerminal(key) {
		at.mentorForgetSplitMaps(key)
	}
}

// mentorForgetSplitMaps forgets the split record for a signal (the U3 cleanup).
// Called from the flat-unregister (terminal rows only) and the cancel-settlement
// path (P3 rel9: a row still cancel_pending at flat-unregister is forgotten HERE
// once it later settles terminal).
func (at *AutoTrader) mentorForgetSplitMaps(signalID string) {
	if at == nil || signalID == "" {
		return
	}
	if nt := at.armedTrader(); nt != nil {
		nt.ForgetSignalMaps(signalID)
	}
}

// mentorFlatSignal reports whether the drive unregistered the signal as flat
// (a tombstone, so a stale partial-fill sweep cannot resurrect it).
func (at *AutoTrader) mentorFlatSignal(key string) bool {
	if at == nil || key == "" {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	_, ok := at.mentorFlatSignals[key]
	return ok
}

// mentorClearFlatSignal clears the flat tombstone (a full fill proves the
// position is OPEN again).
func (at *AutoTrader) mentorClearFlatSignal(key string) {
	if at == nil || key == "" {
		return
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	delete(at.mentorFlatSignals, key)
}

// mentorCountRegistrationShape fires the leg-shape counter for a position that
// was ACTUALLY registered. Called only from the two registration sites — never
// from the shared builder, so a register-if-absent miss never bumps the counter
// (canon 35, "counters record, never infer").
func (at *AutoTrader) mentorCountRegistrationShape(lp *mentorLivePos) {
	if lp == nil {
		return
	}
	if lp.Legs[1].Qty > 0 && lp.Legs[1].SignalID != "" {
		at.mentorCount("exit_drive_split_registered")
	} else if lp.Legs[0].Wire == 1 {
		at.mentorCount("exit_drive_leg1_only_registered")
	}
}

// mentorArmedRowTerminal reports whether the armed row carrying signalID has
// reached a TERMINAL state. An unknown/unreadable row reads terminal (fail-safe
// for the forget — a read failure never blocks the unregister).
func (at *AutoTrader) mentorArmedRowTerminal(signalID string) bool {
	if at == nil || signalID == "" || at.store == nil {
		return true
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return true
	}
	var r store.ArmedOrderDB
	if err := ledger.DB().Where("signal_id = ?", signalID).First(&r).Error; err != nil {
		return true
	}
	return store.IsTerminalArmState(r.State)
}

// mentorCloseReceiptMs resolves the receipt instant from the position_close
// payload: the broker's ExitTime (RFC3339) when parseable, else the clock now.
func mentorCloseReceiptMs(p ntwire.PositionClosePayload) int64 {
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(p.ExitTime)); err == nil {
		return ts.UnixMilli()
	}
	return mentorClockNow().UnixMilli()
}

// mentorLeg1ExitedBefore (I6) is the pure next-candle rule: leg 1 counts as
// "exited on a PRIOR candle" only when its confirmation instant predates the
// candle's OPEN. exitedAtMs <= 0 means no confirmation (a single bracket, which
// is candle-marked) — treated as prior, so the single-leg trail keeps its old
// timing.
func mentorLeg1ExitedBefore(exitedAtMs, candleOpenMs int64) bool {
	if exitedAtMs <= 0 {
		return true
	}
	return exitedAtMs < candleOpenMs
}

// mentorScaledState reads the shared Scaled/Leg1ExitedAtMs under mentorExitMu —
// the receipt (mentorMarkLeg1Scaled) and the I7 fallback write them from other
// goroutines, so an unlocked read races (I6).
func (at *AutoTrader) mentorScaledState(p *mentorLivePos) (scaled bool, exitedAtMs int64) {
	if at == nil || p == nil {
		return false, 0
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	return p.Pos.Scaled, p.Pos.Leg1ExitedAtMs
}

// mentorSetScaledCandle marks Scaled for the SINGLE-leg candle-priced 1:1 point
// (no confirmation instant — Leg1ExitedAtMs stays 0). Guarded because Pos is
// shared. Returns true when this call FIRST marked it.
func (at *AutoTrader) mentorSetScaledCandle(p *mentorLivePos) bool {
	if at == nil || p == nil {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if p.Pos.Scaled {
		return false
	}
	p.Pos.Scaled = true
	return true
}

// mentorLatchLeg1Scaled marks a live position's leg 1 as scaled (confirmed
// exited) at the given instant, under mentorExitMu. Idempotent — a later latch
// never rewinds an earlier one. Returns true when this call FIRST marked it.
func (at *AutoTrader) mentorLatchLeg1Scaled(key string, atMs int64) bool {
	if at == nil || key == "" {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	lp, ok := at.mentorLivePos[key]
	if !ok || lp == nil || lp.Pos.Scaled {
		return false
	}
	lp.Pos.Scaled = true
	lp.Pos.Leg1ExitedAtMs = atMs
	return true
}

// mentorMarkLeg1Scaled (B2, BUILD-ALL L9 bookkeeping half) marks a live position
// scaled ONLY on the broker's position_close receipt of LEG 1's TP. A whole-
// position close (leg 0), a runner close (leg 2), a stop exit, or a manual close
// is NOT a scale-out and leaves Scaled untouched. The candle-price guess that
// used to set Scaled in the exit drive is gone — the trail now begins only once
// the broker confirms leg 1 actually exited (and only on candles that OPEN after
// that confirmation, I6).
//
// Release #31 fold (D1): a leg-2 TP receipt (the runner partial's own line)
// marks that leg Exited — the leg keeps no stop and receives no further BE/trail
// move once its own TP closed it.
func (at *AutoTrader) mentorMarkLeg1Scaled(p ntwire.PositionClosePayload) {
	if at == nil || p.SignalID == "" {
		return
	}
	// #47: a broker exit that FLATTENS the signal closes its Limits.openTrade —
	// the ORIGINAL stop ("sl") is the only exit that counts as a loss; a
	// break-even, trailed stop, partial, manual, news or EOD flat drops it.
	switch {
	case p.ExitReason == "sl":
		at.mentorEnqueueExit(p.SignalID, true)
	case p.ExitReason == "manual":
		at.mentorEnqueueExit(p.SignalID, false)
	case p.ExitReason == "tp" && p.Leg == 2:
		at.mentorEnqueueExit(p.SignalID, false) // the runner scaled — the position is flat
	}
	if p.Leg == 1 && p.ExitReason == "tp" {
		if at.mentorLatchLeg1Scaled(p.SignalID, mentorCloseReceiptMs(p)) {
			at.mentorCount("leg1_at_target")
			at.logInfof("🧑‍🏫 mentor leg 1 scaled on the broker TP receipt: %s (%s)", p.SignalID, p.PositionSide)
		}
		return
	}
	if p.Leg == 2 && p.ExitReason == "tp" {
		if at.mentorMarkLeg2Exited(p.SignalID) {
			at.mentorCount("leg2_exited")
			at.logInfof("🧑‍🏫 mentor leg 2 (runner partial) closed on the broker TP receipt: %s (%s)", p.SignalID, p.PositionSide)
		}
	}
}

// mentorMarkLeg2Exited marks a live position's wire-2 leg (the runner partial)
// as closed on its own TP receipt. Idempotent; returns true when this call
// FIRST marked it.
func (at *AutoTrader) mentorMarkLeg2Exited(key string) bool {
	if at == nil || key == "" {
		return false
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	lp, ok := at.mentorLivePos[key]
	if !ok || lp == nil {
		return false
	}
	if lp.Legs[1].Exited {
		return false
	}
	lp.Legs[1].Exited = true
	return true
}

// mentorRegisterPartialFillIfAbsent (B1 + I4) registers a partially filled
// mentor arm's FILLED quantity in the exit drive (register-if-absent) and pokes
// the drive + bumps the funnel. No-op unless the row is a partially filled
// mentor arm with a signal id. Returns true when it registered.
func (at *AutoTrader) mentorRegisterPartialFillIfAbsent(r store.ArmedOrderDB) bool {
	if !isMentorArmOrigin(r) || strings.TrimSpace(r.SignalID) == "" || r.FillQuantity <= 0 || mentorRemainderToCancel(r) <= 0 {
		return false
	}
	// P3 (rel9): never RE-register a position the drive already unregistered as
	// flat (the side closed; a stale partial-fill sweep must not resurrect it).
	if at.mentorFlatSignal(r.SignalID) {
		return false
	}
	lp := at.mentorBuildLivePos(r, ntwire.OrderUpdatePayload{
		SignalID:  r.SignalID,
		Quantity:  r.FillQuantity,
		FillPrice: r.FillPrice,
	})
	if lp == nil || !at.mentorRegisterLivePosIfAbsent(r.SignalID, lp) {
		return false
	}
	at.mentorNoteFillExitMode(r, lp, "", false) // P2-2: registered now (it was absent) — once per arm
	// P3 (rel9): the shape counter fires ONLY when a registration actually
	// happened — the sweep re-runs every bar while the row is cancel_pending,
	// and a register-if-absent miss must not bump the counter (canon 35).
	at.mentorCountRegistrationShape(lp)
	at.pokeMentorExitDrive()
	at.mentorFunnel.bumpFilled()
	return true
}

// mentorLivePosList returns every live mentor position (the exit-drive loop
// iterates it). The slice is a fresh copy; the pointed-to structs are the live
// state the loop owns the write-back for.
func (at *AutoTrader) mentorLivePosList() []*mentorLivePos {
	if at == nil {
		return nil
	}
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	out := make([]*mentorLivePos, 0, len(at.mentorLivePos))
	for _, p := range at.mentorLivePos {
		out = append(out, p)
	}
	return out
}

// mentorRunnerTargets carries an intent's RunnerTarget (#360, item 5) from the
// authoring (mentorArmIntent, keyed by the ledger scenario) to the fill-time
// live position, without a schema change.
var mentorRunnerTargets sync.Map

// mentorExecFrames carries an intent's ExecTFMin (the frame its entry was READ
// on — 2 under the X5-10 exec-2m schedule) from the authoring
// (mentorArmIntent, keyed trader+scenario) to the fill-time live position, the
// same way as mentorRunnerTargets: no schema change. Only a 2m entry is stored;
// a 1m entry leaves no key (absent = the 1m).
var mentorExecFrames sync.Map

// ── REL17-F2 RESTART REBUILD ────────────────────────────────────────────────
//
// The exit-drive's input (mentorLivePos) is built only from IN-PROCESS fill
// events. After a restart the open mentor position gets no BE, no 1:1 and no
// trail. This pass rebuilds it once at boot from the broker's own book, before
// the first exit-drive pass, and never guesses a stop.

// mentorRestartRebuildOn resolves the kill-switch (env MENTOR_RESTART_REBUILD,
// default ON). Safety fixes ship ON; the switch lets the owner turn it off
// without a release.
func mentorRestartRebuildOn() bool {
	if v := strings.TrimSpace(os.Getenv("MENTOR_RESTART_REBUILD")); v != "" {
		return !strings.EqualFold(v, "off") && v != "0" && !strings.EqualFold(v, "false")
	}
	return true
}

// mentorRebuildBook is the boot-rebuild's book source (a seam so tests can pin
// the book without a live TCP link). Production = liveBook.
var mentorRebuildBook = func(at *AutoTrader, now time.Time) ([]ntwire.NT8Order, bool, time.Duration) {
	return at.liveBook(now)
}

// mentorRebuildOpenPositions is the boot-rebuild's positions source (a seam so
// tests can pin an unreadable positions table). Production reads the store.
var mentorRebuildOpenPositions = func(at *AutoTrader) ([]*store.TraderPosition, error) {
	return at.store.Position().GetOpenPositions(at.id)
}

// mentorRebuildLivePosOnce is the boot hook, called at the top of
// mentorExitDrive before the first pass. It retries every candle until a FRESH
// broker book lets the rebuild run, then latches done.
func (at *AutoTrader) mentorRebuildLivePosOnce(now time.Time) {
	if at == nil || !mentorRestartRebuildOn() {
		return
	}
	at.mentorRebuildMu.Lock()
	done := at.mentorRebuildDone
	at.mentorRebuildMu.Unlock()
	if done {
		return
	}
	_, _, ran := at.mentorRebuildLivePosAtBoot(now)
	if !ran {
		return // no fresh book yet — retry on the next candle
	}
	at.mentorRebuildMu.Lock()
	at.mentorRebuildDone = true
	at.mentorRebuildMu.Unlock()
	// L7: the boot line is READ from the counters, never literal.
	snap := at.mentorCountSnapshot()
	at.logInfof("🧑‍🏫 mentor positions rebuilt: %d (refused %d)", snap["rebuild_ok"], snap["rebuild_refused"])
	if snap["swing_rebuild_ok"] > 0 || snap["swing_rebuild_refused"] > 0 {
		at.logInfof("🧑‍🏫 mentor swing rebuilt: %d under BE/hold (refused %d)", snap["swing_rebuild_ok"], snap["swing_rebuild_refused"])
	}
}

// mentorRebuildLivePosAtBoot rebuilds every open mentor position from the
// broker book. ran=false when there is no fresh book yet (the caller retries).
// Returns (rebuilt, refused, ran).
func (at *AutoTrader) mentorRebuildLivePosAtBoot(now time.Time) (rebuilt, refused int, ran bool) {
	if at == nil || at.store == nil || !at.mentorEnabled() {
		return 0, 0, false
	}
	book, have, age := mentorRebuildBook(at, now)
	if !have || (snapshotMaxAge() > 0 && age > snapshotMaxAge()) {
		return 0, 0, false
	}
	positions, err := mentorRebuildOpenPositions(at)
	if err != nil {
		// DS-105 P2: an unreadable positions table must NOT latch done — the
		// rebuild retries next candle (ran=false). WARN once per boot.
		at.mentorRebuildWarnPositionsOnce(err)
		return 0, 0, false
	}
	ran = true
	for _, pos := range positions {
		if pos == nil || pos.EntryOrderID == "" || !strings.EqualFold(pos.Symbol, at.futuresSymbol()) {
			continue
		}
		side := strings.ToLower(strings.TrimSpace(pos.Side))
		if side != "long" && side != "short" {
			continue
		}
		if at.mentorLivePosExists(pos.EntryOrderID) {
			continue // already under management (a reconnect, not a restart)
		}
		row, ok := at.mentorArmedRowForSignal(pos.EntryOrderID)
		if !ok || !isMentorArmOrigin(*row) {
			continue // not a mentor arm
		}
		lp, missing := at.mentorBuildLivePosFromBook(*row, pos, book)
		if missing != "" {
			at.mentorCount("rebuild_refused")
			refused++
			at.logErrorf("🧑‍🏫 mentor position %s NOT under exit management after restart — %s; manage by hand", row.SignalID, missing)
			at.emitAlert("P0", "mentor-restart-rebuild", "mentor-rebuild-"+row.SignalID,
				"mentor position not under exit management after restart",
				"manage by hand — "+missing+" (signal "+row.SignalID+")")
			continue
		}
		at.mentorRebuildTrailFrame(*row, lp)
		at.mentorRegisterLivePos(pos.EntryOrderID, lp)
		at.mentorCountRegistrationShape(lp)
		at.mentorCount("rebuild_ok")
		rebuilt++
		at.logInfof("🧑‍🏫 mentor restart rebuild: %s %s %d contracts under exit management (mode %s)", pos.Symbol, side, lp.Pos.Contracts, lp.Pos.Mode)
		// FIX-SWING-RESTART: a FILLED swing's BE/hold management runs in the
		// EVALUATOR (manageSwingPosition → mentorMoveStopBE / mentorClosePosition),
		// which needs TWO in-memory registries the 1m rebuild does not cover:
		// the trader's mentorLiveArms entry and the evaluator's State.Swing.Pos.
		// Rebuild both from the SAME proven records. Fail-closed: a swing that
		// cannot be proven is never half-registered and never moved — its stop
		// stays exactly where NT8 has it.
		if strings.EqualFold(strings.TrimSpace(row.Condition), "SWING4H") {
			if ok, why := at.mentorRebuildSwingArm(*row, pos, lp); ok {
				at.mentorCount("swing_rebuild_ok")
				at.logInfof("🧑‍🏫 mentor swing %s under BE/hold management after restart (arm %q)", row.SignalID, mentorArmIDFromScenario(row.Scenario))
			} else {
				at.mentorCount("swing_rebuild_refused")
				at.logErrorf("🧑‍🏫 mentor swing %s NOT under BE/hold management after restart — %s; manage by hand", row.SignalID, why)
				at.emitAlert("P0", "mentor-swing-restart", "mentor-swing-"+row.SignalID,
					"open swing not under BE/hold management after restart",
					"manage by hand — "+why+" (signal "+row.SignalID+")")
			}
		}
	}
	return rebuilt, refused, true
}

// mentorRebuildTrailFrame restores a rebuilt position's trail frame (X8
// @18:02–18:14: the frame the entry was read on). The frame lives in process
// memory (mentorExecFrames), so a reload keeps it; a real restart does not. An
// ISB or PHL/PLH rebuilt while exec_2m_after_30m is ON may have been read on
// the 2m — the frame is then UNKNOWN: it trails on the 1m (the shipped
// default; the 1m trail is never wider than the 2m one), WARN'd and counted,
// never silent.
func (at *AutoTrader) mentorRebuildTrailFrame(r store.ArmedOrderDB, lp *mentorLivePos) {
	if lp == nil {
		return
	}
	if v, ok := mentorExecFrames.Load(mentorRunnerTargetKey(at.id, r.Scenario)); ok {
		lp.TrailTFMin, _ = v.(int)
		return
	}
	if !isMentorExec2mSetup(strings.TrimSpace(r.Condition)) || !mentorExec2mKnobOn(at.mentorRiskControl()) {
		return
	}
	at.mentorCount("trail_tf_unknown_after_restart")
	at.logWarnf("🧑‍🏫 mentor restart rebuild: %s is a %s entered while exec_2m_after_30m is ON — the frame it was read on (1m or 2m) does not survive a restart; its runner trails on closed 1m candles [X8 @18:02–18:14]", r.SignalID, strings.TrimSpace(r.Condition))
}

// isMentorExec2mSetup reports whether an armed row's Condition names one of the
// setups the X5-10 exec-2m schedule reads on the 2m (F2: the ISB pair AND
// PHL/PLH).
func isMentorExec2mSetup(cond string) bool {
	switch strings.ToUpper(cond) {
	case "ISB", "PHL", "PLH":
		return true
	default:
		return false
	}
}

// mentorExec2mKnobOn reads the X5-10 exec_2m_after_30m knob as stored (nil →
// ON, the release #31 DefaultConfig value) without the tuning resolver's side
// effects. It must agree with the kernel DefaultConfig (ONE canonicalizer per
// identifier): a restart rebuild on a strategy with no mentor_tuning (the live
// MNQ trader) reads the knob ON, matching the frame the entry was actually read
// on.
func mentorExec2mKnobOn(rc *store.RiskControlConfig) bool {
	if rc == nil || rc.MentorTuning == nil || rc.MentorTuning.Exec2mAfter30m == nil {
		return true // nil → ON (release #31 default)
	}
	return *rc.MentorTuning.Exec2mAfter30m
}

// mentorRebuildSwingArm restores a FILLED swing's two in-memory registries from
// the proven live-pos rebuild (lp carries the entry, the live stop and the
// BE-already-moved flag). Fail-closed: a trader mismatch, an unreadable arm id,
// an unreadable entry bucket, a contract mismatch, or a degenerate evaluator
// position refuses (false) — the caller leaves the stop as it is at NT8 and
// raises the alert. A refusal after the registry write unregisters the arm so a
// swing is never half-rebuilt.
func (at *AutoTrader) mentorRebuildSwingArm(r store.ArmedOrderDB, pos *store.TraderPosition, lp *mentorLivePos) (bool, string) {
	if strings.TrimSpace(r.TraderID) != at.id {
		return false, "arm row trader " + r.TraderID + " != caller " + at.id
	}
	armID := mentorArmIDFromScenario(r.Scenario)
	if armID == "" {
		return false, "no readable arm id"
	}
	// The evaluator's EntryBucket is the 4h bucket of the swing's 5m bar (the
	// ArmID "swing-<open>"), NOT the fill time — a fill that crossed a 4h
	// boundary would shift the hold-close by a candle.
	var openTime int64
	if _, err := fmt.Sscanf(armID, "swing-%d", &openTime); err != nil || openTime <= 0 {
		return false, "no readable entry bucket from arm id " + armID
	}
	// Contract match: the position's size must equal the row's filled contracts,
	// else the swing's own close would size from a different trade.
	rowQty := float64(r.FillQuantity)
	if rowQty <= 0 && r.Contracts != nil {
		rowQty = float64(*r.Contracts)
	}
	posQty := pos.EntryQuantity
	if posQty < 1 {
		posQty = pos.Quantity
	}
	if rowQty > 0 && posQty > 0 && rowQty != posQty {
		return false, fmt.Sprintf("contract mismatch (position %.0f vs row %.0f)", posQty, rowQty)
	}
	entryBucket := kernel.FourHBucketStart(openTime, kernel.CTLocation())
	at.mentorRegisterLiveArm(armID, r.ID, lp.Pos.Side, lp.Pos.Entry, r.Scenario)
	if at.mentorEval == nil {
		at.mentorUnregisterLiveArm(armID)
		return false, "evaluator not constructed"
	}
	// SWING-RISK (release #22): the kernel measures the risk itself
	// (mentor.SwingRisk) from the row's ENTRY stop — the same definition the
	// live fill uses — never from NT8's live stop, which may already sit at BE.
	if !at.mentorEval.State.Swing.RebuildSwingPos(mentor.SwingRebuild{
		Side: mentor.Side(lp.Pos.Side), Entry: lp.Pos.Entry, EntryStop: r.StopPx, LiveStop: lp.Pos.Stop,
		Target: lp.Pos.Target, EntryBucket: entryBucket, ArmID: armID, MovedBE: lp.Pos.ArmedBE,
	}) {
		at.mentorUnregisterLiveArm(armID)
		return false, "degenerate swing position (entry or risk unreadable)"
	}
	return true, ""
}

// mentorRebuildWarnPositionsOnce logs the unreadable-positions WARN once per
// boot (the rebuild retries every candle until the table reads).
func (at *AutoTrader) mentorRebuildWarnPositionsOnce(err error) {
	at.mentorRebuildMu.Lock()
	if at.mentorRebuildPositionsWarned {
		at.mentorRebuildMu.Unlock()
		return
	}
	at.mentorRebuildPositionsWarned = true
	at.mentorRebuildMu.Unlock()
	at.logWarnf("🧑‍🏫 mentor restart rebuild: open positions unreadable — will retry: %v", err)
}

// mentorLivePosExists reports whether a signal is already registered (guarded).
func (at *AutoTrader) mentorLivePosExists(key string) bool {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	_, ok := at.mentorLivePos[key]
	return ok
}

// mentorArmedRowForSignal fetches the armed row by signal id.
func (at *AutoTrader) mentorArmedRowForSignal(signalID string) (*store.ArmedOrderDB, bool) {
	if at == nil || at.store == nil || signalID == "" {
		return nil, false
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return nil, false
	}
	var r store.ArmedOrderDB
	if err := ledger.DB().Where("signal_id = ?", signalID).First(&r).Error; err != nil {
		return nil, false
	}
	return &r, true
}

// mentorRebuildExitMode resolves the exit branch for a rebuilt position: the
// persisted column when present, else derived from the setup (SWING4H → swing;
// everything else → B). derived=true means the mode was NOT persisted and the
// caller must log that it was derived.
func mentorRebuildExitMode(r store.ArmedOrderDB) (mode string, derived bool) {
	if r.ExitMode != nil && strings.TrimSpace(*r.ExitMode) != "" {
		return *r.ExitMode, false
	}
	if strings.EqualFold(strings.TrimSpace(r.Condition), "SWING4H") {
		return "swing", true
	}
	return "B", true
}

// mentorBuildLivePosFromBook rebuilds a live position from an armed row, its
// OPEN position row, and the broker book. missing != "" is the fail-closed
// reason — the caller registers NOTHING and raises a P0. Never guesses a stop.
func (at *AutoTrader) mentorBuildLivePosFromBook(r store.ArmedOrderDB, pos *store.TraderPosition, book []ntwire.NT8Order) (*mentorLivePos, string) {
	side := strings.ToLower(strings.TrimSpace(r.Side))
	if side != "long" && side != "short" {
		return nil, "no readable side"
	}
	total := int(pos.EntryQuantity)
	if total < 1 {
		total = int(pos.Quantity)
	}
	if total < 1 {
		total = 1
	}
	residual := int(pos.Quantity)
	if residual < 1 {
		residual = 1
	}
	entry := pos.EntryPrice
	if entry <= 0 {
		entry = r.FillPrice
	}
	if entry <= 0 {
		return nil, "no entry price"
	}

	// Bracket children: -sl/-sl2/-sl3 carry StopPrice, -tp/-tp2/-tp3 carry
	// LimitPrice. Only orders LIVE AT THE EXCHANGE count (a dying order is not
	// protection).
	var stop1, stop2, stop3, tp1, tp2, tp3 float64
	var qty1, qty2 int
	for i := range book {
		o := book[i]
		if !strings.HasPrefix(o.Name, r.SignalID) || !o.IsLiveAtExchange() {
			continue
		}
		switch {
		case strings.HasSuffix(strings.ToLower(o.Name), "-sl3"):
			mentorSetIfZero(&stop3, o.StopPrice)
		case strings.HasSuffix(strings.ToLower(o.Name), "-sl2"):
			mentorSetIfZero(&stop2, o.StopPrice)
			if o.Quantity > 0 {
				qty2 = o.Quantity
			}
		case strings.HasSuffix(strings.ToLower(o.Name), "-sl"):
			mentorSetIfZero(&stop1, o.StopPrice)
			if o.Quantity > 0 {
				qty1 = o.Quantity
			}
		case strings.HasSuffix(strings.ToLower(o.Name), "-tp3"):
			mentorSetIfZero(&tp3, o.LimitPrice)
		case strings.HasSuffix(strings.ToLower(o.Name), "-tp2"):
			mentorSetIfZero(&tp2, o.LimitPrice)
		case strings.HasSuffix(strings.ToLower(o.Name), "-tp"):
			mentorSetIfZero(&tp1, o.LimitPrice)
		}
	}

	// Split detection: the row's persisted split, or a leg-2 bracket in the book.
	splitLive := false
	if r.Leg1Qty != nil && *r.Leg1Qty > 0 && *r.Leg1Qty < total {
		splitLive = true
	}
	if stop2 > 0 || tp2 > 0 {
		splitLive = true
	}

	var leg1Qty, leg2Qty int
	var leg1Stop, leg2Stop, leg3Stop float64
	var leg1TP, leg2TP, leg3TP float64
	// leg3Live: a third OCO pair sits in the book (release #31). The row's
	// leg-2 partial (leg2_qty) only SIZES leg 2; it never declares leg 3 live
	// by itself — a row whose leg 2 was planned but not sent (old AddOn,
	// wireLeg2TP refused, leg 1 dropped) has no -sl3/-tp3, and the runner is
	// then the 2-pair wire-2 leg (D4).
	leg3Live := false
	if splitLive && (stop3 > 0 || tp3 > 0) {
		leg3Live = true
	}
	if splitLive {
		if r.Leg1Qty != nil && *r.Leg1Qty > 0 {
			leg1Qty = *r.Leg1Qty
		} else if qty1 > 0 {
			leg1Qty = qty1
		} else {
			leg1Qty = (total + 1) / 2
		}
		leg2Qty = total - leg1Qty
		if leg2Qty <= 0 {
			leg2Qty = qty2
		}
		leg1Stop, leg2Stop, leg3Stop = stop1, stop2, stop3
		leg1TP = tp1
		if leg1TP <= 0 {
			leg1TP = r.Leg1TP
		}
		leg2TP = tp2
		if leg2TP <= 0 {
			leg2TP = r.Leg2TP
		}
		leg3TP = tp3
		if leg3TP <= 0 {
			leg3TP = r.TargetPx
		}
		if leg3Live {
			// The runner partial is leg 2; the runner is leg 3.
			partQty := leg2Qty
			if r.Leg2Qty != nil && *r.Leg2Qty > 0 && *r.Leg2Qty < leg2Qty {
				partQty = *r.Leg2Qty
			} else if qty2 > 0 && qty2 < leg2Qty {
				partQty = qty2
			}
			if partQty <= 0 || partQty >= leg2Qty {
				leg3Live = false // a degenerate split — fall back to the 2-pair view
			} else {
				leg2Qty = partQty
				leg2TP = tp2
				if leg2TP <= 0 {
					leg2TP = r.Leg2TP
				}
			}
		}
	} else {
		leg1Qty, leg2Qty = total, 0
		leg1Stop, leg2Stop = stop1, 0
		leg1TP = tp1
		if leg1TP <= 0 {
			leg1TP = r.TargetPx
		}
		leg2TP = 0
	}

	// The runner's stop is the one the exit drive moves (BE / 1:1 / trail).
	runnerStop := leg2Stop
	if leg3Live {
		runnerStop = leg3Stop
	}
	if !splitLive {
		runnerStop = leg1Stop
	}
	if runnerStop <= 0 {
		return nil, "no live protective stop in the broker book"
	}

	mode, derived := mentorRebuildExitMode(r)
	if derived {
		at.logInfof("🧑‍🏫 mentor restart rebuild: exit mode derived from setup (%s) for %s", mode, r.SignalID)
	}

	// ArmedBE: the runner stop is at or through the entry (on the protective
	// side). Scaled: leg 1 already gone (broker qty is down to the runner).
	armedBE := false
	if side == "long" {
		armedBE = runnerStop >= entry
	} else {
		armedBE = runnerStop <= entry
	}
	// D5 (release #31 fold): leg 1 is gone exactly when the broker's open qty
	// is down to the runner (total − leg1). The old `residual <= runnerQty`
	// compared against only leg 3's qty and read Scaled=false after leg 1
	// filled (2 ≤ 1).
	scaled := splitLive && residual <= total-leg1Qty

	pos2 := mentorPosition{
		Symbol:    at.futuresSymbol(),
		Side:      side,
		Origin:    r.Condition,
		Entry:     entry,
		Stop:      runnerStop,
		Target:    r.TargetPx,
		R:         math.Abs(entry - r.StopPx),
		Contracts: total,
		Leg1:      leg1Qty,
		Leg2:      total - leg1Qty,
		Mode:      mode,
		ArmedBE:   armedBE,
		Scaled:    scaled,
		Leg1TP:    leg1TP,
	}
	// Release #22: the fill candle is unknown after a restart — never resolve
	// the ISB candle-3 partial off a later candle (it is now a REAL modify).
	lp := &mentorLivePos{Pos: pos2, ISBPartialDone: true}
	if splitLive {
		lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: leg1Qty, TP: leg1TP, Stop: leg1Stop, Final: false, Wire: 1}
		if leg3Live {
			// D5 noise: when leg 2's own TP already filled before the restart,
			// its -sl2/-tp2 are gone from the book (stop2 == 0) — mark the leg
			// Exited so the drive never moves a dead wire-2 stop.
			lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: leg2Qty, TP: leg2TP, Stop: leg2Stop, Final: false, Wire: 2, Exited: leg2Stop <= 0}
			lp.Legs[2] = mentorLeg{SignalID: r.SignalID, Qty: total - leg1Qty - leg2Qty, TP: leg3TP, Stop: leg3Stop, Final: true, Wire: 3}
		} else {
			lp.Legs[1] = mentorLeg{SignalID: r.SignalID, Qty: leg2Qty, TP: leg2TP, Stop: leg2Stop, Final: true, Wire: 2}
		}
	} else {
		lp.Legs[0] = mentorLeg{SignalID: r.SignalID, Qty: total, TP: leg1TP, Stop: leg1Stop, Final: true, Wire: 0}
	}
	return lp, ""
}

// mentorSetIfZero writes v into dst only when dst is unset (0) and v is
// positive — first-live-wins for a bracket child the book may show once.
func mentorSetIfZero(dst *float64, v float64) {
	if *dst <= 0 && v > 0 {
		*dst = v
	}
}
