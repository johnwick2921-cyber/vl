package kernel

import (
	"fmt"
	"math"
	"strings"

	"vl/market"
)

// F-1 (planner-touch-fills, 2026-10-14) — A REACTION AT THE LEVEL, FOR EVERY
// CONDITION.
//
// Release #22 (#515, sweep_trigger.go) made a reclaim wait for its SWEEP. Every
// other level play kept counting its confirm from plan publication, so a hold /
// acceptance / reject confirm read MET from closes printed far from the level:
// trade #653 (armed_orders 366, NY 10-14 v2 S1 hold, time_hold 3 above OR-L
// 30506.25) logged "wait_confirm MET (time_hold 3min [stored])" at 08:52:36 CT
// from three 1m closes 40–140 pts ABOVE the level; the market_in_zone limit then
// rested at the far edge of the zone 30509–30517 ("beyond") and filled at
// 08:57:18 inside a −70 pt dump — the hold was decided (it FAILED, 5m close
// 30475.25 at 09:00) only after the fill. Trade #650 (row 355, reject at VWAP
// 30798.92, confirm touch) rested beyond the zone and filled on the first touch,
// a 1m candle that CLOSED BELOW the level (30785.75) — the opposite of a
// rejection.
//
// The fix, the #515 mechanism generalised (the sweep path itself is untouched):
// for every non-breakout, non-sweep condition (TouchGatedCondition) the confirm
// counts ONLY after the first CLOSED 1m bar since plan publication that reached
// the level — its range touched the scenario's economics.entry_zone or the
// confirm ref_price. The required closes (hold N closes, acceptance closes)
// come AFTER that touch; a "touch" confirm is MET only by a touch candle whose
// CLOSE rejects — back on the trade side of the ref (long: above, short:
// below; the condition's own "Rejected" definition, scenario_facts.go). No
// touch → NOT MET, refusal no_touch, and the reason in the verdict detail (the
// plan card renders it). The arm gate (trader/armed_executor.go) holds every
// market_in_zone leg of such a scenario until this verdict is MET, so no
// "beyond" limit rests on the far side of the zone before the reaction exists.

// TouchSetupHoldSentence is the planner-prompt sentence for the hold (class 45:
// the prompt states the machine rule it will be judged by).
const TouchSetupHoldSentence = "LEVEL PLAYS WAIT FOR A REACTION AT THE LEVEL (F-1): for hold, acceptance, reject and fvg_entry the confirm counts only AFTER a closed 1m bar has reached the level (the entry_zone or the confirm ref_price) since the plan was published — hold/acceptance closes printed away from the level are not the trigger, and a touch confirm is met only by a touch candle that CLOSES back on the trade side of the ref (long above, short below); a market_in_zone leg of these plays sends NO order, whatever wait_confirm says, until that confirm is machine-MET. "

// TouchRefusalNoTouch is the verdict refusal code for a level play whose level
// has not been reached since the reference instant.
const TouchRefusalNoTouch = "no_touch"

// ConfirmRefusalMissingTape (N5, rescan 10-14) — no CLOSED 1m bar exists in
// [since, now): the gate cannot know whether the level was reached or swept, so
// the verdict is UNKNOWN (never "price has not reached…", a market claim), and
// nothing is counted. At the 08:22:41 boot the #515 sweep gate counted a hold
// on an empty cache while the 08:19/08:20 bars had in fact traded through.
const ConfirmRefusalMissingTape = "missing_tape"

// hasClosedBar reports whether the tape holds at least one CLOSED 1m bar in
// [sinceMs, nowMs).
func hasClosedBar(bars []market.Kline, sinceMs, nowMs int64) bool {
	for _, b := range bars {
		if b.OpenTime >= sinceMs && b.OpenTime < nowMs && EvaluateBucketClose(b.OpenTime, 1, nowMs).Closed {
			return true
		}
	}
	return false
}

// missingTapeVerdict is the UNKNOWN verdict of a gate with no closed bar.
func missingTapeVerdict(c PlanConfirm, sinceMs, nowMs int64) ConfirmVerdict {
	v := ConfirmVerdict{Rule: c.Rule, RefPrice: c.RefPrice, Side: c.Side, EvaluatedMs: nowMs, Outcome: confirmationUnknown,
		ReferenceMs: &sinceMs, ReferenceSource: "plan publication", RuleSource: ResolveConfirm(c).Source,
		Refusal: ConfirmRefusalMissingTape, Detail: "no bars yet — no closed 1m bar since plan publication; UNKNOWN does not satisfy"}
	finishConfirmation(&v)
	return v
}

// VerdictMissingTape reports whether a verdict (or any leg) is UNKNOWN for an
// empty tape — a hold that must not be counted as a market event.
func VerdictMissingTape(v ConfirmVerdict) bool {
	if v.Refusal == ConfirmRefusalMissingTape {
		return true
	}
	for _, l := range v.Legs {
		if l.Refusal == ConfirmRefusalMissingTape {
			return true
		}
	}
	return false
}

// touchReferenceSource names the reference instant of a touch-gated confirm.
const touchReferenceSource = "first touch of the level (first closed 1m bar to reach the zone/ref)"

// TouchGatedCondition reports whether a scenario condition is a LEVEL play whose
// confirm must be a reaction AT the level: every known condition that is not a
// sweep setup (reclaim / sweep_reclaim — the #515 gate, kept byte-identical) and
// not a breakout-class play (breakout_retest and the waterfalls, whose confirm IS
// the break away from the level).
func TouchGatedCondition(condition string) bool {
	switch strings.ToLower(strings.TrimSpace(condition)) {
	case "hold", "acceptance", "reject", "fvg_entry":
		return true
	}
	return false
}

// TouchSetupWaits reports whether the arm gate must hold this leg (nil = the
// single arm) until the scenario's touch-gated confirm is MET: a level play
// with a confirm whose leg is a market_in_zone limit — the order that otherwise
// rests "beyond" the zone and can only fill on the move INTO the level. A
// planned_order / legacy leg keeps its authored play (the resting order AT the
// level is that policy's definition, entry_policy.go) and is held only by an
// authored wait_confirm, as before.
func TouchSetupWaits(sc PlanScenario, leg *PlanArmLeg) bool {
	// A machine (Picture) scenario is judged by its own deterministic rule and
	// eligibility window (picture_plan_source.go), never by a planner confirm.
	if sc.Confirm == nil || !TouchGatedCondition(sc.Condition) || IsMachineScenario(sc) {
		return false
	}
	return EffectiveArmPolicy(sc.Arm, leg) == EntryPolicyMarketInZone
}

// ArmLegWaitsForConfirm is the arm gate's ONE hold predicate: a sweep setup
// (R22 F1), an authored wait_confirm (S2b), or a touch-gated market_in_zone leg
// (F-1).
func ArmLegWaitsForConfirm(sc PlanScenario, leg PlanArmLeg) bool {
	return SweepSetupWaits(sc, leg.WaitConfirm) || TouchSetupWaits(sc, &leg)
}

// VerdictWaitsForTouch reports whether a verdict (or any of its legs) is held
// because price has not reached the level.
func VerdictWaitsForTouch(v ConfirmVerdict) bool {
	if v.Refusal == TouchRefusalNoTouch {
		return true
	}
	for _, l := range v.Legs {
		if l.Refusal == TouchRefusalNoTouch {
			return true
		}
	}
	return false
}

// touchBand is the price band whose reach is "the touch": the economics
// entry_zone joined with the confirm ref (ok=false without a positive ref —
// fail-closed, never guessed).
func touchBand(s PlanScenario) (lo, hi float64, ok bool) {
	if s.Confirm == nil || !(s.Confirm.RefPrice > 0) || math.IsInf(s.Confirm.RefPrice, 0) {
		return 0, 0, false
	}
	lo, hi = s.Confirm.RefPrice, s.Confirm.RefPrice
	if s.Economics != nil && len(s.Economics.EntryZone) == 2 {
		a, b := s.Economics.EntryZone[0], s.Economics.EntryZone[1]
		if a > 0 && b > 0 && !math.IsInf(a, 0) && !math.IsInf(b, 0) {
			lo, hi = math.Min(lo, math.Min(a, b)), math.Max(hi, math.Max(a, b))
		}
	}
	return lo, hi, true
}

// barReaches reports whether a bar's range reached the band.
func barReaches(b market.Kline, lo, hi float64) bool {
	return b.Low <= hi && b.High >= lo
}

// firstTouchOfBand returns the open time of the earliest CLOSED 1m bar in
// [sinceMs, nowMs) whose range reached [lo, hi]. It walks the bars directly (no
// confirmationTape) so the tape counters are not double-counted; the earliest
// touching bar wins whatever the input order — the firstSweepOfRef shape.
func firstTouchOfBand(lo, hi float64, bars []market.Kline, sinceMs, nowMs int64) (openMs int64, ok bool) {
	for _, b := range bars {
		if b.OpenTime < sinceMs || b.OpenTime >= nowMs {
			continue
		}
		if !EvaluateBucketClose(b.OpenTime, 1, nowMs).Closed {
			continue
		}
		if barReaches(b, lo, hi) && (!ok || b.OpenTime < openMs) {
			openMs, ok = b.OpenTime, true
		}
	}
	return openMs, ok
}

// touchRejectSide is the side of the ref a rejecting touch candle closes on:
// the scenario's DIRECTION (long → above, short → below). The stored
// confirm.side of a touch is not a reliable trade side (since 09-20 the planner
// wrote reject-long touches "below" 16 times and reject-short touches "above"
// 53 times), so it is used only when the direction is absent.
func touchRejectSide(s PlanScenario) string {
	switch strings.ToLower(strings.TrimSpace(s.Direction)) {
	case "long":
		return "above"
	case "short":
		return "below"
	}
	return strings.ToLower(strings.TrimSpace(s.Confirm.Side))
}

// touchRejectAfter evaluates a "touch" confirm after the first touch: MET at
// the first CLOSED 1m bar (at or after the first touch) that came FROM the
// trade side (opened on it — long: above the ref, short: below), reached the
// band AND closed back on the trade side. A touch candle that closed through
// the level is not a rejection, and neither is a candle that arrived from the
// far side (rescan 10-14: #654's short reject was "met" by a support bounce
// whose candle came down from ABOVE 30466).
func touchRejectAfter(s PlanScenario, lo, hi float64, bars []market.Kline, sinceMs, nowMs, touchMs int64) ConfirmVerdict {
	c := *s.Confirm
	side := touchRejectSide(s)
	v := ConfirmVerdict{Rule: c.Rule, RefPrice: c.RefPrice, Side: c.Side, EvaluatedMs: nowMs,
		ReferenceMs: &touchMs, ReferenceSource: touchReferenceSource, RuleSource: ResolveConfirm(c).Source}
	if side != "above" && side != "below" {
		v.Detail = "no trade side to judge the touch candle's close (no direction, no confirm side) — not MET"
		finishConfirmation(&v)
		return v
	}
	touches := 0
	for _, b := range confirmationTape(bars, sinceMs, nowMs) {
		if b.OpenTime < touchMs {
			continue
		}
		e := EvaluateBucketClose(b.OpenTime, 1, nowMs)
		if !e.Closed || !barReaches(b, lo, hi) {
			continue
		}
		touches++
		v.Bucket = &e
		fromSide := (side == "above" && b.Open > c.RefPrice) || (side == "below" && b.Open < c.RefPrice)
		if fromSide && ((side == "above" && b.Close > c.RefPrice) || (side == "below" && b.Close < c.RefPrice)) {
			v.Met = true
			v.EventMs, v.EventKnown = e.CloseMs, true
			v.EventSource = "closed 1m touch candle that rejected the level"
			v.Detail = fmt.Sprintf("touch candle closed %.2f, %s %.2f — the level rejected", b.Close, side, c.RefPrice)
			finishConfirmation(&v)
			return v
		}
	}
	v.Detail = fmt.Sprintf("price reached the level (%d closed 1m touch candle(s)) but none came from %s and closed back %s %.2f — a touch that closes through the level, or arrives from the far side, is not a rejection", touches, side, side, c.RefPrice)
	finishConfirmation(&v)
	return v
}

// touchGatedConfirm evaluates part one of a level play's confirm with the first
// touch of the level as its reference instant. gated=false → the scenario is not
// a level play (or has no confirm) and the caller evaluates as before.
func touchGatedConfirm(s PlanScenario, bars []market.Kline, sinceMs, nowMs int64) (v ConfirmVerdict, gated bool) {
	if s.Confirm == nil || !TouchGatedCondition(s.Condition) {
		return ConfirmVerdict{}, false
	}
	c := *s.Confirm
	if !hasClosedBar(bars, sinceMs, nowMs) {
		return missingTapeVerdict(c, sinceMs, nowMs), true // N5: UNKNOWN, not "no touch"
	}
	lo, hi, ok := touchBand(s)
	var at int64
	if ok {
		at, ok = firstTouchOfBand(lo, hi, bars, sinceMs, nowMs)
	}
	if !ok {
		band := fmt.Sprintf("ref %.2f", c.RefPrice)
		if hi > lo {
			band = fmt.Sprintf("zone %.2f–%.2f (ref %.2f)", lo, hi, c.RefPrice)
		}
		v = ConfirmVerdict{Rule: c.Rule, RefPrice: c.RefPrice, Side: c.Side, EvaluatedMs: nowMs,
			ReferenceMs: &sinceMs, ReferenceSource: "plan publication", RuleSource: ResolveConfirm(c).Source,
			Refusal: TouchRefusalNoTouch,
			Detail: fmt.Sprintf("waiting for the touch: no closed 1m bar has reached the %s since plan publication — a %s confirm before price reaches the level is not the trigger",
				band, strings.ToLower(strings.TrimSpace(s.Condition)))}
		finishConfirmation(&v)
		return v, true
	}
	if strings.EqualFold(strings.TrimSpace(c.Rule), "touch") {
		return touchRejectAfter(s, lo, hi, bars, sinceMs, nowMs, at), true
	}
	v = evaluateConfirmAfter(c, bars, sinceMs, nowMs, &at)
	v.Detail = strings.ReplaceAll(v.Detail, v.ReferenceSource, touchReferenceSource)
	v.ReferenceSource = touchReferenceSource
	return v, true
}
