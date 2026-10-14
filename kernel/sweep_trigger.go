package kernel

import (
	"fmt"
	"strings"

	"vl/market"
)

// RELEASE #22 F1 — SWEEP SETUPS WAIT FOR THEIR TRIGGER (owner ruling R4,
// 2026-10-14).
//
// A reclaim is "sweep the level, then close back through it". Its confirm{} is
// a close rule (1x5m_close | 1m_mss) on the reclaimed side of the ref, and the
// evaluator counted EVERY such close after plan publication — with no sweep.
// When the plan was born with price already on the reclaimed side, the very
// first completed 5m close read MET: trade #649 (armed_orders 350, 10-13 ASIA
// v2 S2, ONH 30849.75) logged "wait_confirm MET" at 20:15:01 CT, 2.5 minutes
// after birth, with price ~55 pts under the ref. The market_in_zone limit then
// rested and filled on the rally at 30843.50 — the highest 1m high between
// birth and fill was 30844.25, the ref was never traded. Row 233 (10-02) is the
// same shape.
//
// The fix: for a sweep setup, the confirm counts only closes AFTER the first
// closed 1m bar that traded AT/THROUGH the ref on the sweep side (the side
// opposite the reclaim close). No sweep → NOT MET, with the reason. The arm
// gate (trader/armed_executor.go) holds every leg of a sweep setup until this
// verdict is MET, whatever wait_confirm the plan authored, so no fillable order
// reaches NT8 before the trigger. A "touch" confirm is itself the sweep
// evidence (the bar's range reached the ref), so it is not re-gated.

// SweepRefusalNoSweep is the verdict refusal code for a sweep setup whose ref
// has not been traded since the reference instant.
const SweepRefusalNoSweep = "no_sweep"

// SweepSetupCondition reports whether a scenario condition is a SWEEP setup
// whose entry must wait for its trigger: the reclaim family.
func SweepSetupCondition(condition string) bool {
	switch strings.ToLower(strings.TrimSpace(condition)) {
	case "reclaim", "sweep_reclaim":
		return true
	}
	return false
}

// SweepSetupWaits reports whether the arm gate must hold a leg of this scenario
// until its confirm is MET. True for every sweep setup, and for any leg the plan
// authored wait_confirm (the pre-existing chain).
func SweepSetupWaits(sc PlanScenario, authoredWait bool) bool {
	return authoredWait || SweepSetupCondition(sc.Condition)
}

// firstSweepOfRef returns the open time of the earliest CLOSED 1m bar in
// [sinceMs, nowMs) whose range reached c.RefPrice on the sweep side: a reclaim
// that closes BELOW the ref is swept by a high AT/ABOVE it; one that closes
// ABOVE is swept by a low AT/BELOW it. An unknown side has no sweep side — it
// returns ok=false (fail-closed, never guessed).
//
// It walks the bars directly (no confirmationTape) so the tape counters are not
// double-counted; the earliest touching bar wins whatever the input order.
func firstSweepOfRef(c PlanConfirm, bars []market.Kline, sinceMs, nowMs int64) (openMs int64, ok bool) {
	side := strings.ToLower(strings.TrimSpace(c.Side))
	if side != "above" && side != "below" || c.RefPrice <= 0 {
		return 0, false
	}
	for _, b := range bars {
		if b.OpenTime < sinceMs || b.OpenTime >= nowMs {
			continue
		}
		if !EvaluateBucketClose(b.OpenTime, 1, nowMs).Closed {
			continue
		}
		swept := (side == "below" && b.High >= c.RefPrice) || (side == "above" && b.Low <= c.RefPrice)
		if swept && (!ok || b.OpenTime < openMs) {
			openMs, ok = b.OpenTime, true
		}
	}
	return openMs, ok
}

// sweepGatedConfirm evaluates part one of a sweep setup's confirm with the
// sweep as its reference instant. gated=false → the scenario is not a sweep
// setup (or its part one is a touch) and the caller evaluates as before.
func sweepGatedConfirm(s PlanScenario, bars []market.Kline, sinceMs, nowMs int64) (v ConfirmVerdict, gated bool) {
	if s.Confirm == nil || !SweepSetupCondition(s.Condition) || strings.EqualFold(strings.TrimSpace(s.Confirm.Rule), "touch") {
		return ConfirmVerdict{}, false
	}
	c := *s.Confirm
	at, ok := firstSweepOfRef(c, bars, sinceMs, nowMs)
	if !ok {
		way := "above"
		if strings.EqualFold(strings.TrimSpace(c.Side), "above") {
			way = "below"
		}
		v = ConfirmVerdict{Rule: c.Rule, RefPrice: c.RefPrice, Side: c.Side, EvaluatedMs: nowMs,
			ReferenceMs: &sinceMs, ReferenceSource: "plan publication", RuleSource: ResolveConfirm(c).Source,
			Refusal: SweepRefusalNoSweep,
			Detail: fmt.Sprintf("waiting for the sweep: no closed 1m bar has traded %s %.2f since plan publication — a %s close before the sweep is not the trigger",
				way, c.RefPrice, strings.ToLower(strings.TrimSpace(s.Condition)))}
		finishConfirmation(&v)
		return v, true
	}
	v = evaluateConfirmAfter(c, bars, sinceMs, nowMs, &at)
	const src = "sweep of the ref (first closed 1m bar through it)"
	v.Detail = strings.ReplaceAll(v.Detail, v.ReferenceSource, src)
	v.ReferenceSource = src
	return v, true
}

// VerdictWaitsForSweep reports whether a verdict (or any of its legs) is held
// because its sweep has not happened.
func VerdictWaitsForSweep(v ConfirmVerdict) bool {
	if v.Refusal == SweepRefusalNoSweep {
		return true
	}
	for _, l := range v.Legs {
		if l.Refusal == SweepRefusalNoSweep {
			return true
		}
	}
	return false
}

// ScenarioTriggerRequired reports whether a scenario's entry must follow its
// confirm: a sweep setup, or an arm (or any arm leg) authored wait_confirm.
func ScenarioTriggerRequired(sc PlanScenario) bool {
	if SweepSetupCondition(sc.Condition) {
		return true
	}
	if sc.Arm == nil {
		return false
	}
	if sc.Arm.WaitConfirm {
		return true
	}
	for _, l := range sc.Arm.Legs {
		if l.WaitConfirm {
			return true
		}
	}
	return false
}

// ScenarioTriggerAt is the grader's read of one scenario's trigger at an
// instant (the entry fill): the SAME evaluation the arm gate runs, without the
// live-telemetry side effects (it is a retrospective read, not a cycle).
func ScenarioTriggerAt(sc PlanScenario, bars []market.Kline, sinceMs, atMs int64) ConfirmVerdict {
	return scenarioConfirm(sc, bars, sinceMs, atMs)
}
