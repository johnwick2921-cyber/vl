package kernel

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"vl/logger"
	"vl/market"
)

// C1 (F3) — machine evaluation of a scenario's structured confirmation, using
// the SAME acceptance machinery as plan death (A2-consistent rule semantics).

// confirmAcceptanceRule maps the confirm vocabulary onto acceptance rules.
// E1 (entry-mechanics 2026-08-30): the 15m confirm variant is DEAD for
// AUTHORSHIP (schema-rejected confirm_rule_15m_removed) — the mapping below is
// LEGACY evaluation tolerance so stored pre-entry-mechanics docs keep
// evaluating. 1m_mss and time_hold never route through the acceptance
// machinery (they are evaluated by their own primitives).
func confirmAcceptanceRule(rule string) string {
	if n, err := strconv.Atoi(strings.TrimSuffix(rule, "x5m_close")); err == nil && n > 0 {
		return fmt.Sprintf("%dx5m", n)
	}
	switch rule {
	case "15m_close": // legacy: stored docs only
		return "15m-close"
	case "1x5m_close":
		return "5m-close" // the A2-fixed one-close rule
	default:
		return "2x5m" // "2x5m_close"
	}
}

// ConfirmVerdict is one scenario's machine-computed confirmation state.
type ConfirmVerdict struct {
	EventSource     string       `json:"event_source,omitempty"`
	Outcome         string       `json:"outcome"`
	Bucket          *BucketClose `json:"bucket,omitempty"`
	EvaluatedMs     int64        `json:"evaluated_ms"`
	ReferenceMs     *int64       `json:"reference_ms,omitempty"`
	ReferenceSource string       `json:"reference_source"`
	EventMs         int64        `json:"event_ms,omitempty"`
	EventKnown      bool         `json:"event_known"`
	Refusal         string       `json:"refusal,omitempty"`

	Rule     string           `json:"rule"`
	RefPrice float64          `json:"ref_price"`
	Side     string           `json:"side"`
	Met      bool             `json:"met"`
	Detail   string           `json:"detail"`         // e.g. "last 15m close 29641.00"
	Legs     []ConfirmVerdict `json:"legs,omitempty"` // F2: the per-leg states of a two-leg confirm

	// RuleSource (W2) — where the count/duration of Rule came from: "stored"
	// (the scenario's own confirm) or "authoring_default" (BD_MIN_CLOSES /
	// ACCEPT_HOLD_MIN because nothing valid was stored). Absent on records
	// written before W2 — unknown, never assumed "stored".
	RuleSource string `json:"rule_source,omitempty"`
}

// ConfirmVerdict.Met on a two-leg scenario is the OVERALL verdict (leg1 &&
// leg2); a partial never reports Met=true.

// EvaluateConfirm computes MET/NOT-MET for one confirm object over the bars
// since the plan's birth (touch-gated like plan death; windowed identically).
func EvaluateConfirm(c PlanConfirm, bars []market.Kline, sinceMs, nowMs int64) ConfirmVerdict {
	v := evaluateConfirmAfter(c, bars, sinceMs, nowMs, nil)
	recordConfirmationVerdict(v)
	return v
}

// StaleConfirmATR is the staleness threshold in 5m Wilder ATR14 multiples (env
// STALE_CONFIRM_ATR, default 2.0). Citation: register S2 (mega-research
// 2026-08-26) — the shipped 1.0×dATR unit (~350pt daily-range proxy) marked
// only 38/2,908 (1.3%) of the week's MET confirms stale; at 2.0×ATR5m (~40-70
// pt) the rule marks ~37%, matching the empirical stale mass (median |price−ref|
// = 58.75 pt). The old dATR path is DELETED — this is ATR-only.
func StaleConfirmATR() float64 {
	if v := os.Getenv("STALE_CONFIRM_ATR"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return 2.0
}

// staleConfirmATRLogged guards the fail-open log (once per process) so a
// missing 5m ATR doesn't spam the prompt path every cycle.
var staleConfirmATRLogged bool

// StaleConfirmATR5m computes the Wilder ATR14 on 5m buckets of the 1m snapshot
// — the same math the min-SL gate uses via the 5m structure ATR. 0 = unavailable.
func StaleConfirmATR5m(bars []market.Kline) float64 {
	if len(bars) == 0 {
		return 0
	}
	five := AggregateBars(bars, 5*60_000)
	if len(five) < 14 {
		return 0
	}
	return market.ExportCalculateATR(five, 14)
}

// staleConfirmAnnotation builds the "(stale — …)" parenthetical for a MET
// confirm whose ref context has drifted > STALE_CONFIRM_ATR × ATR5m from the
// current price since plan write (register S2: ATR-only, the dATR path is gone).
// The context is the confirm's own RefPrice (validated into the prose at write
// time), overridden by the FVG distal anchor when the scenario is an fvg_entry
// play. FAIL-OPEN: a missing 5m ATR skips the annotation (logged once), never
// gates. Advisory text only — the AI stays the judge.
func staleConfirmAnnotation(s PlanScenario, v ConfirmVerdict, nowPrice, atr5m float64) string {
	if !v.Met || nowPrice <= 0 {
		return ""
	}
	if atr5m <= 0 {
		if !staleConfirmATRLogged {
			staleConfirmATRLogged = true
			logger.Warnf("⚠️ stale-confirm annotation skipped this cycle: 5m ATR unavailable (fail-open)")
		}
		return ""
	}
	ctx := v.RefPrice
	if s.Fvg != nil && (s.Fvg.Lo > 0 || s.Fvg.Hi > 0) {
		if a, ok := ScenarioAnchor(s, nil); ok {
			ctx = a
		}
	}
	if ctx <= 0 {
		return ""
	}
	if math.Abs(nowPrice-ctx) > StaleConfirmATR()*atr5m {
		return fmt.Sprintf("(stale — written %.2f context, price now %.2f; treat as expired)", ctx, nowPrice)
	}
	return ""
}

// EvaluateScenarioConfirm computes the OVERALL confirm state of one scenario.
// Two-leg scenarios (Confirm2, or the waterfall-class breakdown plays whose
// legs are machine-derived) report each leg and an aggregate: leg 1 MET + leg 2
// NOT MET renders as overall NOT MET — a partial never prints as a bare "MET"
// (F2, the S2 10:54 artifact). Leg 2 is windowed from leg 1's FIRST fire time
// (a retest touch that happened BEFORE the breakdown cannot count).
func EvaluateScenarioConfirm(s PlanScenario, bars []market.Kline, sinceMs, nowMs int64) ConfirmVerdict {
	v := scenarioConfirm(s, bars, sinceMs, nowMs)
	if s.Confirm != nil || (IsBreakdownCondition(s.Condition) && s.Breakdown != nil) {
		recordConfirmationVerdict(v) // exactly the set that recorded before the split
	}
	return v
}

// scenarioConfirm is EvaluateScenarioConfirm without the live telemetry (one
// evaluation; ScenarioTriggerAt — the grader's retrospective read — reuses it).
func scenarioConfirm(s PlanScenario, bars []market.Kline, sinceMs, nowMs int64) ConfirmVerdict {
	if IsBreakdownCondition(s.Condition) && s.Breakdown != nil {
		st := BreakdownContinueState(s, bars, sinceMs, nowMs)
		// W2 (a): leg 1 is judged on the rule the resolver read — the STORED
		// confirm.rule, BD_MIN_CLOSES only as the named authoring default.
		c := st.Rule.AsConfirm()
		side := c.Side
		leg1 := evaluateConfirmAfter(c, bars, sinceMs, nowMs, nil)
		leg1.RuleSource = st.Rule.Source
		leg1.Met = st.Leg1Met
		leg1.Outcome = "NOT MET"
		if leg1.Met {
			leg1.Outcome = "MET"
		}
		if st.Reclaimed {
			leg1.Detail = "reclaimed by completed 5m close — the breakdown is void · " + leg1.Detail
		}
		leg2 := ConfirmVerdict{Rule: "retest_fail", RefPrice: c.RefPrice, Side: side, Met: st.Leg2Met,
			EvaluatedMs: nowMs, ReferenceSource: "part one recorded 5m close", Bucket: st.Bucket, Detail: retestLegDetail(s, st)}
		if st.Leg1Known {
			leg2.ReferenceMs = &st.Leg1At
		} else {
			leg2.Outcome = confirmationUnknown
			leg2.Refusal = "missing_reference"
			leg2.Detail = "part one reference instant is missing; UNKNOWN does not satisfy"
		}
		if st.Leg2Met {
			leg2.EventMs, leg2.EventKnown = st.Leg2At, true
			e := EvaluateBucketClose(st.Leg2At-1, AcceptanceIntervalMinutes("5m-close"), nowMs)
			leg2.Bucket = &e
		}
		if st.ReclaimedAt != nil {
			e := EvaluateBucketClose(*st.ReclaimedAt-1, AcceptanceIntervalMinutes("5m-close"), nowMs)
			leg2.Bucket = &e
		}
		finishConfirmation(&leg2)
		v := leg2
		v.Rule, v.RefPrice, v.Side, v.RuleSource = c.Rule, c.RefPrice, c.Side, st.Rule.Source
		v.Met = st.Leg1Met && st.Leg2Met
		v.Legs = []ConfirmVerdict{leg1, leg2}
		return v
	}
	if s.Confirm == nil {
		return ConfirmVerdict{}
	}
	return orderedScenarioConfirm(s, bars, sinceMs, nowMs)
}

// AcceptHoldMin resolves ACCEPT_HOLD_MIN (E6, default 10) — the minutes of
// 1m closes a time_hold confirm requires price to hold beyond its ref.
func AcceptHoldMin() int {
	if v := strings.TrimSpace(os.Getenv("ACCEPT_HOLD_MIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 10
}

// ConfirmReferenceInstant is the single reference lookup. Zero is allowed as
// an actual instant only when ok=true; a missing witness never becomes birth.
func ConfirmReferenceInstant(c PlanConfirm, bars []market.Kline, sinceMs, nowMs int64) (instant int64, ok bool) {
	v := evaluateConfirmAfter(c, bars, sinceMs, nowMs, nil)
	return v.EventMs, v.EventKnown
}

// retestLegDetail renders the leg-2 state of a waterfall-class play.
func retestLegDetail(s PlanScenario, st BreakdownState) string {
	if st.Reclaimed {
		return "reclaimed — the breakdown is void"
	}
	if !st.Leg1Met {
		return "waiting for the breakdown leg"
	}
	if strings.EqualFold(strings.TrimSpace(s.Breakdown.EntryMode), "immediate") {
		// E3: entry signal is the confirming close — W2: the count the
		// resolver read (stored rule, else the BD_MIN_CLOSES default).
		if st.Rule.Closes > 1 {
			return fmt.Sprintf("immediate mode — entry signal is the %dth confirming close", st.Rule.Closes)
		}
		return "immediate mode — entry signal is the confirming close"
	}
	if st.Leg2Met {
		return "pullback failed to reclaim — entry live"
	}
	return "retest not yet failed"
}

// RenderConfirmLines renders the per-scenario advisory lines for the executor
// prompt: machine truth the AI reasons FROM — never a gate.
//
// ADDENDUM S (2026-08-26, quiet-day audit) — two prompt-text annotations on
// top, still zero gates:
//  1. STALE: a MET confirm whose ref context is now > STALE_CONFIRM_ATR × dATR
//     from price is annotated "MET (stale — written X context, price now Y;
//     treat as expired)".
//  2. CONFLICT: opposite-direction confirms MET in the same cycle get one
//     trailing line "CONFLICT: opposing confirms MET — structural ambiguity,
//     default WAIT unless fresh trigger".
func RenderConfirmLines(doc PlanDoc, bars []market.Kline, sinceMs, nowMs int64, nowPrice, atr5m float64) string {
	var b strings.Builder
	metLong, metShort := false, false
	for _, s := range doc.Scenarios {
		if s.Confirm == nil && !(IsBreakdownCondition(s.Condition) && s.Breakdown != nil) {
			continue
		}
		v := EvaluateScenarioConfirm(s, bars, sinceMs, nowMs)
		if len(v.Legs) == 0 && v.Rule == "" {
			continue
		}
		if len(v.Legs) == 0 {
			// Single-leg scenarios keep the legacy byte-identical line.
			met := "NOT MET"
			if v.Met {
				met = "MET"
				dir := strings.ToLower(strings.TrimSpace(s.Direction))
				if dir != "long" && dir != "short" {
					if strings.EqualFold(v.Side, "above") {
						dir = "long"
					} else if strings.EqualFold(v.Side, "below") {
						dir = "short"
					}
				}
				switch dir {
				case "long":
					metLong = true
				case "short":
					metShort = true
				}
				if a := staleConfirmAnnotation(s, v, nowPrice, atr5m); a != "" {
					met += " " + a
				}
			}
			b.WriteString(fmt.Sprintf("  %s confirm: %s %s %.2f — %s (%s)\n",
				s.ID, strings.ReplaceAll(v.Rule, "_", " "), v.Side, v.RefPrice, met, v.Detail))
			continue
		}
		// F2 — two-leg rendering: every leg prints, a partial NEVER prints as
		// a bare "MET".
		var legs strings.Builder
		for i, l := range v.Legs {
			lm := "NOT MET"
			if l.Met {
				lm = "MET"
			}
			legs.WriteString(fmt.Sprintf("leg %d/%d %s — %s", i+1, len(v.Legs),
				strings.ReplaceAll(l.Rule, "_", " "), lm))
			if i < len(v.Legs)-1 {
				legs.WriteString(" · ")
			}
		}
		overall := "NOT MET"
		if v.Met {
			overall = "MET"
			dir := strings.ToLower(strings.TrimSpace(s.Direction))
			if dir == "long" {
				metLong = true
			} else if dir == "short" {
				metShort = true
			}
			if a := staleConfirmAnnotation(s, v, nowPrice, atr5m); a != "" {
				overall += " " + a
			}
		}
		b.WriteString(fmt.Sprintf("  %s confirm: %s → overall %s (%s)\n", s.ID, legs.String(), overall, v.Detail))
	}
	if metLong && metShort {
		b.WriteString("CONFLICT: opposing confirms MET — structural ambiguity, default WAIT unless fresh trigger\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "Machine-computed confirmations (advisory — you remain the judge):\n" + b.String()
}
