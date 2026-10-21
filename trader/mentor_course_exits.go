package trader

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"vl/kernel/mentor"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── RELEASE #25 COURSE-EXITS KNOBS (lane mentor-course-exits, owner "do all
// now" 2026-10-14) — the ONE resolver site ──────────────────────────────────
//
// Each knob is resolved HERE from the trader's strategy (absent key → the
// COMMON-25 knob-rule default), applied to the evaluator in
// mentorEvaluatorConfig, and printed READ per trader on the boot line. A bad
// stored value fails closed to the default and is COUNTED.

// mentorBoolKnob resolves a *bool knob: nil → def.
func mentorBoolKnob(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// mentorSwingEMA34FirstTarget — CX-1, nil → ON (case a).
func mentorSwingEMA34FirstTarget(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorSwingEMA34IsFirstTarget, true)
}

// mentorSwingRunnerOn — CX-2, nil → ON (course-defaults-30, owner "fix all":
// the course runs the 4h swing with a runner [D5.2 p2 @06:19–06:23]).
func mentorSwingRunnerOn(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorSwingRunner, true)
}

// mentorResonanceRunnerTPOn — CX-3, nil → ON (case a, a bug fix).
func mentorResonanceRunnerTPOn(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorResonanceRunnerTPOldHigh, true)
}

// mentorBEBeforePartialOn — CX-4, nil → ON (course-defaults-30, owner "fix all"
// — reverses R3): BE at half-way to +1R is MANDATORY [D1.2 p1 @12:34–13:12, @15:57].
func mentorBEBeforePartialOn(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorBEBeforePartial, true)
}

// mentorISBC3PartialAnyProfit — CX-5, nil → ON = today's rule (the ISB
// candle-3 partial at any profit short of +1R); false = leg 1 keeps its +1R.
func mentorISBC3PartialAnyProfit(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorISBC3PartialAnyProfit, true)
}

// mentorReverseISBAsPHL — CX-6, nil → ON (course-defaults-30, owner "fix all"):
// the mentor names the reverse ISB the Potential Low 2 / Lower High [D5.4 p1 @03:01–03:02].
func mentorReverseISBAsPHL(rc *store.RiskControlConfig) bool {
	return rc == nil || mentorBoolKnob(rc.MentorReverseISBAsPHL, true)
}

// mentorEMA9VisitCap — CX-8: shared (default) | per_level | exempt; a bad
// value fails closed to shared and is counted.
func mentorEMA9VisitCap(at *AutoTrader, rc *store.RiskControlConfig) string {
	if rc == nil {
		return mentor.EMA9VisitShared
	}
	mode, ok := mentor.NormalizeEMA9VisitCap(rc.MentorEMA9VisitCap)
	if !ok {
		at.mentorCount("ema9_visit_cap_bad_value")
	}
	return mode
}

// mentorPositiveKnob resolves a float knob: 0 → def; negative/NaN → def,
// counted under name+"_bad_value".
func mentorPositiveKnob(at *AutoTrader, v, def float64, name string) float64 {
	if v == 0 {
		return def
	}
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		at.mentorCount(name + "_bad_value")
		return def
	}
	return v
}

// mentorCourseExitsResolved is the per-trader resolved view (boot line + tests).
type mentorCourseExitsResolved struct {
	SwingEMA34FirstTarget bool
	SwingRunner           bool
	ResonanceRunnerTP     bool
	BEBeforePartial       bool
	ISBC3AnyProfit        bool
	ReverseISBAsPHL       bool
	EMA9VisitCap          string
	ISBMaxPts             float64
	KeypairRatio          float64
	BoxFreshRTHPair       bool
	BoxMaxHeightPts       float64
}

func mentorCourseExitsResolve(at *AutoTrader, rc *store.RiskControlConfig) mentorCourseExitsResolved {
	out := mentorCourseExitsResolved{
		SwingEMA34FirstTarget: mentorSwingEMA34FirstTarget(rc),
		SwingRunner:           mentorSwingRunnerOn(rc),
		ResonanceRunnerTP:     mentorResonanceRunnerTPOn(rc),
		BEBeforePartial:       mentorBEBeforePartialOn(rc),
		ISBC3AnyProfit:        mentorISBC3PartialAnyProfit(rc),
		ReverseISBAsPHL:       mentorReverseISBAsPHL(rc),
		EMA9VisitCap:          mentorEMA9VisitCap(at, rc),
		KeypairRatio:          1,
		ISBMaxPts:             30, // course-defaults-30: stop < 30 taken, ≥ 30 refused
		BoxFreshRTHPair:       rc != nil && mentorBoolKnob(rc.MentorBoxFreshRTHPair, false),
	}
	if rc != nil {
		out.ISBMaxPts = mentorPositiveKnob(at, rc.MentorISBMaxPts, 30, "isb_max_pts")
		out.KeypairRatio = mentorPositiveKnob(at, rc.MentorKeypairCandleRatio, 1, "keypair_candle_ratio")
		out.BoxMaxHeightPts = mentorPositiveKnob(at, rc.MentorBoxMaxHeightPts, 0, "box_max_height_pts")
	}
	return out
}

// applyMentorCourseExits writes the kernel half of the knobs onto the
// evaluator config (mentorEvaluatorConfig calls it).
func applyMentorCourseExits(at *AutoTrader, cfg *mentor.Config, rc *store.RiskControlConfig) {
	r := mentorCourseExitsResolve(at, rc)
	cfg.CourseExits.SwingEMA34IsFirstTarget = r.SwingEMA34FirstTarget
	cfg.CourseExits.ReverseISBAsPHL = r.ReverseISBAsPHL
	cfg.CourseExits.EMA9VisitCap = r.EMA9VisitCap
	cfg.CourseExits.ISBMaxStopPts = r.ISBMaxPts
	cfg.CourseExits.KeypairCandleRatio = r.KeypairRatio
	cfg.CourseExits.BoxFreshRTHPair = r.BoxFreshRTHPair
	cfg.CourseExits.BoxMaxHeightPts = r.BoxMaxHeightPts
}

// mentorCourseExitsBootLine READS every course-exits knob for THIS trader
// (never a literal); a value the trader cannot resolve prints n/a.
func (at *AutoTrader) mentorCourseExitsBootLine() string {
	if at == nil || at.config.StrategyConfig == nil {
		return "🧑‍🏫 mentor course-exits knobs: n/a (no strategy bound)"
	}
	r := mentorCourseExitsResolve(at, at.mentorRiskControl())
	onoff := func(b bool) string {
		if b {
			return "ON"
		}
		return "OFF"
	}
	num := func(v float64) string {
		if v <= 0 {
			return "off"
		}
		return fmt.Sprintf("%g", v)
	}
	return fmt.Sprintf("🧑‍🏫 mentor course-exits knobs: swing_ema34_is_first_target=%s · swing_runner=%s (swing contracts %d) · resonance_runner_tp_old_high=%s · be_before_partial=%s · isb_c3_partial_any_profit=%s · reverse_isb_as_phl=%s · ema9_visit_cap=%s · isb_max_pts=%s · keypair_candle_ratio=%g · box_fresh_rth_pair=%s · box_max_height_pts=%s",
		onoff(r.SwingEMA34FirstTarget), onoff(r.SwingRunner), at.mentorSwingContractsRead(), onoff(r.ResonanceRunnerTP),
		onoff(r.BEBeforePartial), onoff(r.ISBC3AnyProfit), onoff(r.ReverseISBAsPHL), r.EMA9VisitCap,
		num(r.ISBMaxPts), r.KeypairRatio, onoff(r.BoxFreshRTHPair), num(r.BoxMaxHeightPts))
}

// mentorSwingContractsRead reads the resolved swing size (the existing
// mentor_swing4h_contracts knob, which IS CX-2's swing_contracts).
func (at *AutoTrader) mentorSwingContractsRead() int {
	_, _, _, _, swing, _, _ := at.mentorKnobs()
	return swing
}

// ── CX-2: the swing runner ─────────────────────────────────────────────────

// mentorSwingRunnerMinPts is the runner's take-profit distance floor: the top
// of the course's per-4h-candle band ("50–100 points per 4-hour candle",
// 00-METHOD §8 table, D5.2). The runner is held by the 4h (BE at +1R, the
// hold-close at the end of the 2nd 4h candle — the swing's own rules); this TP
// only caps it. [C] the course names no runner TP: "hô tới đâu luôn".
const mentorSwingRunnerMinPts = 100.0

// mentorSwingRunnerLegs is the CX-2 split for a SWING4H arm with the runner
// knob ON and n ≥ 2: leg 1 = ceil(n/2) at the swing's first target
// (max(1R, the 5m EMA 34) — in.Target), the runner = the rest with its TP at
// max(100 pts, 2 × the first-target distance) beyond the entry. ok=false →
// the caller keeps today's single bracket (knob OFF, not a swing, n < 2, or
// no first target), the knob-ON skips counted.
func (at *AutoTrader) mentorSwingRunnerLegs(in mentor.Intent, n int, forkMode string) (leg1Qty int, leg1TP, runnerTP float64, ok bool) {
	if forkMode != "swing" || !mentorSwingRunnerOn(at.mentorRiskControl()) {
		return 0, 0, 0, false
	}
	if n < 2 {
		at.mentorCount("swing_runner_needs_2_contracts")
		at.logInfof("🧑‍🏫 mentor swing runner ON but the swing size is %d — a runner needs ≥ 2 contracts (mentor_swing4h_contracts); single bracket kept [D5.2 p2 @06:19 \"vô at least là 2 … để có đường bắn bớt\"]", n)
		return 0, 0, 0, false
	}
	first := in.Target
	dist := math.Abs(first - in.Price)
	if first <= 0 || dist <= 0 {
		at.mentorCount("swing_runner_no_first_target")
		at.logWarnf("🧑‍🏫 mentor swing runner: no first target on the swing intent (target %.2f) — single bracket kept", first)
		return 0, 0, 0, false
	}
	leg1, _ := mentorSplitLegs(n, 0)
	run := math.Max(mentorSwingRunnerMinPts, 2*dist)
	runnerTP = in.Price + run
	if in.Side == mentor.SideShort {
		runnerTP = in.Price - run
	}
	at.mentorCount("swing_runner_split")
	return leg1, first, runnerTP, true
}

// mentorSwingCloseQty (CX-2) sizes the swing hold-close. A split swing (the
// row carries a leg-1 qty) may already have taken leg 1 at the first target,
// so the close is capped at the broker's open qty on that side — never an
// over-close that would reverse the account. A read failure refuses (the
// resting stop still protects), counted. A single-bracket swing: unchanged.
func (at *AutoTrader) mentorSwingCloseQty(r store.ArmedOrderDB, side string, qty float64) (float64, bool) {
	if r.Leg1Qty == nil || *r.Leg1Qty <= 0 {
		return qty, true
	}
	open, ok := mentorDriveOpenQty(at)
	if !ok {
		at.mentorCount("close_refused_open_qty_unknown")
		at.logWarnf("🧑‍🏫 mentor swing hold-close REFUSED — split swing (runner) and the broker's open qty is unreadable; never an over-close; the resting stop protects, retried next 4h read")
		return 0, false
	}
	have := open[strings.ToLower(side)]
	if have <= 0 {
		at.mentorCount("close_skipped_side_flat")
		at.logInfof("🧑‍🏫 mentor swing hold-close: the %s side is already flat at the broker — nothing to close", side)
		return 0, false
	}
	if have < qty {
		at.mentorCount("close_capped_open_qty")
		return have, true
	}
	return qty, true
}

// ── CX-3: the resonance runner's TP, really sent ───────────────────────────

// mentorResonanceRunnerTP sends the mode-A runner TP move the evaluator's
// RunnerTarget asks for (the level beyond the old high — "70-80% là nó sẽ phá
// luôn cái đỉnh bên tay trái" [D2.4 p1 @01:58–02:09]) through the SAME
// leg-scoped modify_bracket path the ISB candle-3 partial uses
// (modifyBracketWire), addressed to the RUNNER (wire leg 2). Leg 1's +1R TP
// stays (R-RES). Every branch that sends nothing is logged AND counted.
func (at *AutoTrader) mentorResonanceRunnerTP(nt *ntTrader.TCPTrader, p *mentorLivePos, runnerTarget float64) {
	if !mentorResonanceRunnerTPOn(at.mentorRiskControl()) {
		if runnerTarget > 0 {
			at.logInfof("🧑‍🏫 mentor resonance runner TP %.2f NOT sent — mentor_resonance_runner_tp_old_high OFF (logged only, the pre-#25 behaviour)", runnerTarget)
		}
		at.mentorCount("modify_bracket_resonance_logged")
		return
	}
	runner := mentorRunnerLeg(p)
	if runnerTarget <= 0 {
		at.mentorCount("resonance_runner_tp_none")
		if runner == nil {
			return
		}
		at.logInfof("🧑‍🏫 mentor resonance runner TP: no level beyond the old high on record — the runner keeps its TP %.2f", runner.TP)
		return
	}
	if runner == nil || (runner.Wire != 2 && runner.Wire != 3) {
		at.mentorCount("resonance_runner_tp_not_split")
		at.logWarnf("🧑‍🏫 mentor resonance runner TP %.2f NOT sent — one bracket on the wire (no separate runner leg); a TP modify would move the WHOLE position past the 1:1 partial", runnerTarget)
		return
	}
	side := p.Pos.Side
	tick := at.mentorInstrumentTick()
	want := runnerTarget
	if tick > 0 {
		want = math.Round(want/tick) * tick
	}
	if runner.TP > 0 && !mentorStopTighter(side, want, runner.TP) {
		// long: the new TP must sit ABOVE the resting one (further out).
		at.mentorCount("resonance_runner_tp_not_beyond")
		at.logInfof("🧑‍🏫 mentor resonance runner TP %.2f is not beyond the resting %.2f — nothing sent", want, runner.TP)
		return
	}
	if modifyBracketWire == nil {
		at.mentorCount("modify_bracket_unwired")
		at.logWarnf("🧑‍🏫 mentor resonance runner modify_bracket unwired (TP %.2f) — logged, NOT sent", want)
		return
	}
	if err := modifyBracketWire(nt, runner.SignalID, side, runner.Wire, want); err != nil {
		at.mentorCount("modify_bracket_failed")
		at.logErrorf("🧑‍🏫 mentor resonance runner modify_bracket FAILED for %s (TP %.2f): %v — the runner keeps %.2f", runner.SignalID, want, err, runner.TP)
		return
	}
	at.mentorCount("resonance_runner_tp_sent")
	at.logInfof("🧑‍🏫 mentor resonance runner modify_bracket SENT: %s leg %d TP %.2f → %.2f (the level beyond the old high) [D2.4 p1 @01:58–02:09; mentor_resonance_runner_tp_old_high]", runner.SignalID, runner.Wire, runner.TP, want)
	runner.TP = want
}

// ── CX-4: break-even half-way to +1R, BEFORE the partial ───────────────────

// mentorBEBeforePartialStep (knob ON, candle mode, stage 0 — leg 1's partial
// not yet confirmed): on the first closed candle whose close is at least
// half-way to leg 1's +1R target (+0.5R) — and past break-even by a tick —
// BOTH legs' stops go to break-even [D1.2 p1 @12:41–13:12 "kéo stop … về break
// even … ở mức 1-1"; @15:57 "Bắt buộc … maintain risk reward ở mức 1-1"].
// course-defaults-30 (owner "fix all") makes mentor_be_before_partial ON by
// default, which REVERSES owner ruling R3 (BE only after the confirmed
// partial); this runs only with mentor_be_before_partial ON. Returns whether
// BE armed.
func (at *AutoTrader) mentorBEBeforePartialStep(nt *ntTrader.TCPTrader, p *mentorLivePos, c float64) bool {
	pos := &p.Pos
	if pos.ArmedBE || pos.R <= 0 || !mentorBEBeforePartialOn(at.mentorRiskControl()) {
		return false
	}
	if c <= 0 {
		// No close is not a close: a 0 reads as "past half-way" for a short
		// and would send a break-even NT8 refuses below the market.
		at.mentorCount("be_before_partial_no_close")
		return false
	}
	side := pos.Side
	long := side == "long"
	half := pos.Entry + pos.R/2
	if !long {
		half = pos.Entry - pos.R/2
	}
	tick := at.mentorInstrumentTick()
	be := mentorBEStopPrice(side, pos.Entry, tick)
	if (long && (c < half || c < be+tick)) || (!long && (c > half || c > be-tick)) {
		return false
	}
	moved := 0
	for i := range p.Legs {
		leg := &p.Legs[i]
		if leg.SignalID == "" || leg.Qty <= 0 {
			continue
		}
		err := at.mentorMoveLegStop(nt, side, leg, be)
		if errors.Is(err, errMentorStopBrokerTighter) {
			moved++
			continue
		}
		if errors.Is(err, errMentorStopThroughMarket) {
			// m198 #43-B: the wrong-side BE closed the WHOLE symbol at market
			// (a full flatten) — the position is flat; a sibling leg's move
			// would be a redundant close on an already-flat position. Stop.
			at.mentorCount("stop_move_skipped_leg_closed")
			return false
		}
		if !mentorMoveApplied(err) {
			if err != nil && !mentorMoveErrAlreadyLogged(err) {
				at.logWarnf("🧑‍🏫 mentor BE before the partial: %s leg %d move FAILED: %v", leg.SignalID, leg.Wire, err)
			}
			continue
		}
		leg.Stop = be
		moved++
	}
	if moved == 0 {
		at.mentorCount("be_before_partial_not_sent")
		return false // re-tried next candle; a failed send never arms BE
	}
	pos.ArmedBE = true
	pos.Stop = be
	at.mentorCount("be_before_partial")
	at.logInfof("🧑‍🏫 mentor BE before the partial: close %.2f is past half-way %.2f to +1R — stops → break-even %.2f (entry %.2f) [D1.2 p1 @12:41–13:12, @15:57; mentor_be_before_partial ON — overrides owner ruling R3's after-partial BE]",
		c, half, be, pos.Entry)
	return true
}
