package kernel

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"vl/market"
)

// BreakdownContinue — the WATERFALL-CLASS play (F1, 2026-08-28). The 8th
// scenario condition: momentum-follow through a broken level, the class the
// plan lacked during the 2026-08-28 -347pt crash (missed-200pt report:
// bias right, trigger set wrong for a waterfall — every authored short was a
// retest-FADE and no continuation entry existed, $0-by-own-rules).
//
//   - breakdown_continue: SHORT — N closes BELOW a stated level with
//     displacement ≥ BD_MIN_DISP_ATR × ATR5m and no reclaim close; entry =
//     the pullback-that-fails (shallow retrace < BD_MAX_PULLBACK × the
//     breakdown leg that cannot reclaim the level) or immediate on the 2nd
//     confirming close (entry_mode).
//   - breakup_continue:  the LONG mirror.
//
// ARMABLE: the retest entry is a resting limit AT the broken level — exactly
// where an order beats a 2-minute brain. The arm chains on confirm leg 1
// (breakdown confirmed) and rests at the level until price retests it.

// PlanBreakdownContinue is the machine-verifiable schema of a
// breakdown_continue / breakup_continue scenario (fvg-style: the model
// declares, the validator re-checks the facts from bars).
type PlanBreakdownContinue struct {
	Level      float64 `json:"level"`               // the broken level (the retest)
	LevelLabel string  `json:"level_label"`         // display label, e.g. "VWAP 29657.39"
	EntryMode  string  `json:"entry_mode"`          // pullback | immediate
	BreakLeg   float64 `json:"break_leg,omitempty"` // declared displacement in pts (0 = validator computes)
	Pullback   float64 `json:"pullback,omitempty"`  // declared max pullback in pts (0 = auto BD_MAX_PULLBACK × leg)
}

// ---- env knobs (zero literals) ----

func bdMinDispATR() float64 {
	if v := os.Getenv("BD_MIN_DISP_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 1.0
}

func bdMaxPullbackFrac() float64 {
	if v := os.Getenv("BD_MAX_PULLBACK"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 && n < 1 {
			return n
		}
	}
	return 0.4
}

// bdConfirmCloses — E3 (entry-mechanics 2026-08-30): the breakdown floor
// relaxes 2→1 confirming close (BD_MIN_CLOSES, default 1). Displacement
// (BD_MIN_DISP_ATR) and the reclaim-check are UNCHANGED — the entry law now
// rides on displacement quality, not on a double close.
func bdConfirmCloses() int {
	if v := os.Getenv("BD_MIN_CLOSES"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func bdMaxLevelDistATR() float64 {
	if v := os.Getenv("BD_MAX_LEVEL_DIST_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	// 5.0 — generous on purpose: the reference case (2026-08-28 v4, VWAP 57pts
	// behind price at birth) must validate. The retest may never come (that is
	// what the arm is for — it rests, correctly unfilled, if price never returns).
	return 5.0
}

func bdMinSLATR() float64 {
	if v := os.Getenv("BD_MIN_SL_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 1.0
}

// IsBreakdownCondition reports whether the condition string is one of the two
// waterfall-class plays.
func IsBreakdownCondition(condition string) bool {
	c := strings.ToLower(strings.TrimSpace(condition))
	return c == "breakdown_continue" || c == "breakup_continue"
}

// HasBreakdownScenario reports whether any scenario is waterfall-class.
func HasBreakdownScenario(d *PlanDoc) bool {
	if d == nil {
		return false
	}
	for _, s := range d.Scenarios {
		if IsBreakdownCondition(s.Condition) {
			return true
		}
	}
	return false
}

// breakdownShort reports whether the play is the SHORT mirror.
func breakdownShort(condition string) bool {
	return strings.EqualFold(strings.TrimSpace(condition), "breakdown_continue")
}

// BreakdownState is the machine-evaluated two-leg trigger state of one
// waterfall-class scenario against the 1m snapshot.
type BreakdownState struct {
	// Rule (W2) is leg 1's resolved rule — the stored confirm.rule, or the
	// BD_MIN_CLOSES authoring default with its reason. Every reader of this
	// state (render, record, desk, arm, validator, planner facts) counts it.
	Rule        ResolvedConfirmRule
	Bucket      *BucketClose
	Leg1At      int64
	Leg1Known   bool
	Leg2At      int64
	ReclaimedAt *int64

	Leg1Met     bool    // N closes beyond the level (the breakdown)
	Leg2Met     bool    // the retest that failed to reclaim (pullback) / auto with leg1 (immediate)
	Reclaimed   bool    // a close came back across the level — the play is void
	BreakLegPts float64 // measured displacement (max excursion beyond the level)
	LastClose   float64 // last evaluated close
}

// BreakdownContinueState evaluates both trigger legs from the 1m bars.
// sinceMs scopes the tape to the plan's birth (bars before birth are ignored).
// Leg 1 uses the same BEST-RUN semantics as EvaluateConfirm (the longest
// consecutive run of closes beyond the level — a single non-beyond close
// resets the run, matching the plan-status "best run N/M" convention).
func BreakdownContinueState(sc PlanScenario, bars []market.Kline, sinceMs, nowMs int64) BreakdownState {
	var st BreakdownState
	if !IsBreakdownCondition(sc.Condition) || sc.Breakdown == nil {
		return st
	}
	st.Rule, _ = ResolveScenarioConfirm(sc) // W2: leg 1's count — the stored rule
	if sc.Breakdown.Level <= 0 {
		return st
	}
	lvl, short := sc.Breakdown.Level, breakdownShort(sc.Condition)
	st.Leg1At, st.Leg1Known = ConfirmReferenceInstant(st.Rule.AsConfirm(), bars, sinceMs, nowMs)
	judge, last := closedConfirmationBuckets(bars, sinceMs, nowMs, AcceptanceIntervalMinutes("5m-close"))
	st.Bucket = last
	touched := false
	for _, b := range judge {
		e := EvaluateBucketClose(b.OpenTime, AcceptanceIntervalMinutes("5m-close"), nowMs)
		st.LastClose = b.Close
		beyond := (short && b.Close < lvl) || (!short && b.Close > lvl)
		if beyond {
			exc := b.High - lvl
			if short {
				exc = lvl - b.Low
			}
			if exc > st.BreakLegPts {
				st.BreakLegPts = exc
			}
		}
		if !st.Leg1Known || e.CloseMs <= st.Leg1At {
			continue
		}
		if !beyond && st.ReclaimedAt == nil {
			at := e.CloseMs
			st.ReclaimedAt = &at
		}
		if (short && b.High >= lvl) || (!short && b.Low <= lvl) {
			touched = true
		}
		if touched && beyond && st.Leg2At == 0 {
			st.Leg2At = e.CloseMs
		}
	}
	st.Reclaimed = st.ReclaimedAt != nil
	st.Leg1Met = st.Leg1Known && !st.Reclaimed
	st.Leg2Met = st.Leg1Met && st.Leg2At != 0
	if strings.EqualFold(strings.TrimSpace(sc.Breakdown.EntryMode), "immediate") {
		st.Leg2Met = st.Leg1Met
		if st.Leg1Met {
			st.Leg2At = st.Leg1At
		}
	}
	return st
}

// ValidateBreakdownContinueScenarios re-verifies every waterfall-class scenario
// against the bars at write time (the model declares, the math verifies).
// price is the current price at write; atr5m the 5m Wilder ATR14.
// VOID PARITY (2026-09-02): the tape and the window come from the resolver
// (VoidScope), never from the caller — the prompt's VOID list reads the SAME
// scope, so the two can no longer hold different opinions about a level.
func ValidateBreakdownContinueScenarios(d *PlanDoc, scope VoidScope, atr5m, price float64, nowMs int64) error {
	bars := scope.Bars
	if d == nil {
		return nil
	}
	for i := range d.Scenarios {
		s := &d.Scenarios[i]
		if !IsBreakdownCondition(s.Condition) {
			continue
		}
		if s.Breakdown == nil {
			return fmt.Errorf("%s %s requires the breakdown{} facts object (level + entry_mode)", s.ID, s.Condition)
		}
		bd := s.Breakdown
		short := breakdownShort(s.Condition)
		if short && !strings.EqualFold(strings.TrimSpace(s.Direction), "short") {
			return fmt.Errorf("%s breakdown_continue must be SHORT (got %s)", s.ID, s.Direction)
		}
		if !short && !strings.EqualFold(strings.TrimSpace(s.Direction), "long") {
			return fmt.Errorf("%s breakup_continue must be LONG (got %s)", s.ID, s.Direction)
		}
		if bd.Level <= 0 {
			return fmt.Errorf("%s breakdown{} needs level > 0", s.ID)
		}
		if atr5m > 0 && price > 0 && math.Abs(price-bd.Level) > bdMaxLevelDistATR()*atr5m {
			return fmt.Errorf("%s broken level %.2f is %.1f pts from price %.2f (> %.1f×ATR5m=%.1f — the retest is unreachable; author a nearer broken level)",
				s.ID, bd.Level, math.Abs(price-bd.Level), price, bdMaxLevelDistATR(), bdMaxLevelDistATR()*atr5m)
		}
		if !strings.EqualFold(strings.TrimSpace(bd.EntryMode), "pullback") &&
			!strings.EqualFold(strings.TrimSpace(bd.EntryMode), "immediate") {
			return fmt.Errorf("%s breakdown{} entry_mode must be pullback|immediate (got %q)", s.ID, bd.EntryMode)
		}
		// Facts: the tape must show the displacement (or the arm's resting entry
		// is authorized blind). Pullback authoring additionally requires the
		// FULL leg 1 (N confirming closes). Immediate-mode authoring is legal
		// as soon as the displacement exists — the 2nd confirming close is the
		// ENTRY trigger itself, so requiring it at write time would make the
		// play un-authorable mid-waterfall (PRE-SUNDAY F1 ruling: immediate is
		// plan-authorable on the AI path only; arms stay pullback-only).
		st := BreakdownContinueState(*s, bars, scope.SinceMs, nowMs)
		immediate := strings.EqualFold(strings.TrimSpace(bd.EntryMode), "immediate")
		// Reclaimed FIRST: a close back across voids the play no matter how many
		// confirming closes the floor requires — the honest message for the
		// rehearsal-S4 class (E3 keeps this check unchanged).
		if st.Reclaimed {
			return fmt.Errorf("%s %s: a close came back across %.2f — the breakdown is void; %s", s.ID, s.Condition, bd.Level, BreakdownReclaimedHint)
		}
		if !immediate && !st.Leg1Met {
			// W2: the count is the resolver's — the STORED rule, named with its
			// source (a 2x5m_close is refused until the tape shows TWO closes).
			return fmt.Errorf("%s %s: the tape shows NO confirming close beyond %.2f yet (%d confirming close(s) needed — rule %s, displacement + reclaim-check unchanged) — author it only after the displacement exists, or author the conditional thesis now as an acceptance play (confirm 1x5m_close beyond the level, arm wait_confirm:true — nothing is sent until that close prints) (or set entry_mode=immediate and accept the confirming-close trigger)",
				s.ID, s.Condition, bd.Level, st.Rule.Closes, st.Rule.Label())
		}
		displacement := st.BreakLegPts
		if immediate {
			displacement = Evaluate1mDisplacement(bars, bd.Level, short, scope.SinceMs, nowMs).Pts
		}
		if atr5m > 0 && displacement < bdMinDispATR()*atr5m {
			return fmt.Errorf("%s %s: measured displacement %.2f pts < BD_MIN_DISP_ATR %.1f×ATR5m (%.1f pts) — not a displacement move, %s",
				s.ID, s.Condition, displacement, bdMinDispATR(), bdMinDispATR()*atr5m, BreakdownDisplacementHint)
		}
		if bd.DeclaredBreakLeg() > 0 && st.BreakLegPts > 0 {
			_ = bd // declared leg accepted; the machine value is what the arm uses
		}
		if s.Arm != nil && s.Arm.Enabled {
			if !strings.EqualFold(strings.TrimSpace(bd.EntryMode), "pullback") {
				return fmt.Errorf("%s %s arm requires entry_mode=pullback (an immediate-mode entry cannot rest as a limit)", s.ID, s.Condition)
			}
			if !s.Arm.WaitConfirm {
				return fmt.Errorf("%s %s arm requires wait_confirm:true (it must chain on confirm leg 1 before resting at the level)", s.ID, s.Condition)
			}
			if s.Confirm == nil {
				return fmt.Errorf("%s %s arm requires a confirm{} (leg 1) to chain on", s.ID, s.Condition)
			}
			if atr5m > 0 && math.Abs(s.Arm.Entry-s.Arm.Stop) < bdMinSLATR()*atr5m {
				return fmt.Errorf("%s %s arm stop distance %.2f pts < %.1f×ATR5m (%.1f pts) — respect min-SL", s.ID, s.Condition,
					math.Abs(s.Arm.Entry-s.Arm.Stop), bdMinSLATR(), bdMinSLATR()*atr5m)
			}
		}
	}
	return nil
}

// DeclaredBreakLeg surfaces the declared displacement (0 = auto).
func (b *PlanBreakdownContinue) DeclaredBreakLeg() float64 {
	if b == nil {
		return 0
	}
	return b.BreakLeg
}

// BreakdownContinueEntryPx derives the resting entry for an armed
// waterfall-class scenario: the broken level, offset one tick INTO the trade's
// favor (short → level + tick, long → level − tick) — the same convention as
// reject arms.
func BreakdownContinueEntryPx(sc PlanScenario, tick float64) float64 {
	if !IsBreakdownCondition(sc.Condition) || sc.Breakdown == nil || sc.Breakdown.Level <= 0 {
		return 0
	}
	if tick <= 0 {
		tick = 0.25
	}
	if breakdownShort(sc.Condition) {
		return sc.Breakdown.Level + tick
	}
	return sc.Breakdown.Level - tick
}
