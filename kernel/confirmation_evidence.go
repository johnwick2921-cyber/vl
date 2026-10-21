package kernel

import (
	"fmt"
	"vl/market"
	"strings"
	"time"
)

// evaluateConfirmAfter is pure: the caller supplies both clocks. after is an
// event boundary, not a new bar-open window (a 5m close can follow a touch
// inside that same bucket).
func evaluateConfirmAfter(c PlanConfirm, bars []market.Kline, sinceMs, nowMs int64, after *int64) (v ConfirmVerdict) {
	res := ResolveConfirm(c) // W2: the ONE resolver — the stored rule, never an env re-read
	v = ConfirmVerdict{Rule: c.Rule, RefPrice: c.RefPrice, Side: c.Side, EvaluatedMs: nowMs, ReferenceMs: &sinceMs, ReferenceSource: "plan publication", RuleSource: res.Source}
	if after != nil {
		v.ReferenceMs = after
		v.ReferenceSource = "part one recorded event"
	}
	defer func() { finishConfirmation(&v) }()
	w := confirmationTape(bars, sinceMs, nowMs) // W2 (d): ordered, one bar per minute
	if len(w) == 0 {
		v.Detail = "no bars yet"
		return
	}
	above := strings.EqualFold(c.Side, "above")
	switch c.Rule {
	case "touch":
		for _, b := range w {
			if b.OpenTime >= nowMs {
				continue
			}
			e := EvaluateBucketClose(b.OpenTime, 1, nowMs)
			v.Bucket = &e
			if b.Low > c.RefPrice || b.High < c.RefPrice {
				continue
			}
			if after != nil && (!e.Closed || (confirmationOrderedSequence && e.CloseMs <= *after)) {
				continue
			}
			v.Met = true
			v.Detail = "level touched"
			// OHLC establishes a touch by this closed minute's end, never its
			// exact tick time. A forming touch has no ordered reference yet.
			if e.Closed {
				v.EventMs, v.EventKnown = e.CloseMs, true
				v.EventSource = "closed 1m touch observation (OHLC upper bound)"
			}
			return
		}
		v.Detail = "no recorded touch after the reference"
	case "1m_mss":
		m := evaluateMSSAfter(w, c.Side, nowMs, after)
		v.Met, v.Detail = m.Met, m.Detail
		if m.Met {
			e := EvaluateBucketClose(m.BreakTimeMs, 1, nowMs)
			v.Bucket = &e
			v.EventMs, v.EventKnown = e.CloseMs, true
			v.EventSource = "completed 1m MSS close"
		}
	case "time_hold":
		need, run, best := res.HoldMin, 0, 0 // A5: stored hold_min, else the named authoring default
		for _, b := range w {
			if b.OpenTime >= nowMs {
				continue
			}
			e := EvaluateBucketClose(b.OpenTime, 1, nowMs)
			if !v.EventKnown {
				v.Bucket = &e
			}
			if !e.Closed {
				continue
			}
			if after != nil && confirmationOrderedSequence && e.CloseMs <= *after {
				continue
			}
			if (above && b.Close > c.RefPrice) || (!above && b.Close < c.RefPrice) {
				run++
				if run > best {
					best = run
				}
				if run >= need && !v.EventKnown {
					v.EventMs, v.EventKnown, v.Bucket = e.CloseMs, true, &e
				}
			} else {
				run = 0
			}
		}
		v.Met = best >= need
		v.EventSource = "completed 1m hold run"
		v.Detail = fmt.Sprintf("price held %s %.2f for %d/%d min of completed 1m closes", c.Side, c.RefPrice, best, need)
	default:
		r := AcceptanceRunEver(w, confirmAcceptanceRule(c.Rule), c.RefPrice, above, nowMs, after)
		v.Met = r.Found
		v.EventSource = "completed rule-timeframe close"
		v.EventMs, v.EventKnown, v.Bucket = r.FirstAt, r.Found, r.Bucket
		if r.Bucket == nil {
			v.Detail = "no bars at the rule timeframe yet"
			return
		}
		v.Detail = fmt.Sprintf("best run %d/%d completed closes %s %.2f", r.Best, r.Need, c.Side, c.RefPrice)
	}
	return
}

func finishConfirmation(v *ConfirmVerdict) {
	if v.Outcome == "" {
		v.Outcome = "NOT MET"
		if v.Met {
			v.Outcome = "MET"
		}
	}
	if v.Bucket != nil {
		v.Detail += " · " + v.Bucket.String()
		if !v.Met && !v.Bucket.Closed && v.Refusal == "" {
			v.Refusal = "forming_bucket"
		}
	} else {
		v.Detail += " · bucket=UNKNOWN (no eligible bucket evidence)"
	}
	if v.ReferenceMs != nil {
		v.Detail += fmt.Sprintf(" · reference=%s (%s)", FormatCT(time.UnixMilli(*v.ReferenceMs)), v.ReferenceSource)
	} else {
		v.Detail += " · reference=UNKNOWN"
	}
}

func orderedScenarioConfirm(s PlanScenario, bars []market.Kline, sinceMs, nowMs int64) ConfirmVerdict {
	// R22 F1 (owner R4): a sweep setup's part one counts closes only after the
	// sweep of its ref (sweep_trigger.go); every other scenario is unchanged.
	v1, gated := sweepGatedConfirm(s, bars, sinceMs, nowMs)
	if !gated {
		// F-1 (planner-touch-fills): a LEVEL play's part one counts only after
		// the first touch of the level (touch_trigger.go); breakouts unchanged.
		v1, gated = touchGatedConfirm(s, bars, sinceMs, nowMs)
	}
	if !gated {
		v1 = evaluateConfirmAfter(*s.Confirm, bars, sinceMs, nowMs, nil)
	}
	if s.Confirm2 == nil {
		return v1
	}
	// Part two chains on part one's OWN event (for an ungated scenario this is
	// exactly ConfirmReferenceInstant — the same evaluation).
	ref, ok := v1.EventMs, v1.EventKnown
	var v2 ConfirmVerdict
	if !ok {
		v2 = ConfirmVerdict{Rule: s.Confirm2.Rule, RefPrice: s.Confirm2.RefPrice, Side: s.Confirm2.Side,
			Outcome: confirmationUnknown, EvaluatedMs: nowMs, ReferenceSource: "part one unavailable", Refusal: "missing_reference", Detail: "part one reference instant is missing; UNKNOWN does not satisfy"}
		finishConfirmation(&v2)
	} else {
		v2 = evaluateConfirmAfter(*s.Confirm2, bars, sinceMs, nowMs, &ref)
		if v1.EventSource != "" {
			source := "part one: " + v1.EventSource
			v2.Detail = strings.ReplaceAll(v2.Detail, v2.ReferenceSource, source)
			v2.ReferenceSource = source
		}
		if !v2.Met {
			prior := evaluateConfirmAfter(*s.Confirm2, bars, sinceMs, nowMs, nil)
			if prior.EventKnown && prior.EventMs <= ref {
				v2.Refusal = "out_of_order"
				v2.Detail = "out of order: earlier part-two event cannot satisfy this sequence · " + v2.Detail
			}
		}
	}
	v := v2
	v.Rule, v.RefPrice, v.Side = v1.Rule, v1.RefPrice, v1.Side
	v.Met = v1.Met && v2.Met
	v.Legs = []ConfirmVerdict{v1, v2}
	return v
}
