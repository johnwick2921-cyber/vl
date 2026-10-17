package trader

import (
	"fmt"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// validateAuthoredScenariosAt runs inside the existing candidate retry loop.
// UNKNOWN is explicit and accepted; no arm/level/entry-gate policy changes.
func (at *AutoTrader) validateAuthoredScenariosAt(doc *kernel.PlanDoc, session, tradeDate string, now time.Time) error {
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), "1m", kernel.AISVPBarCount)
	}
	var dead []string
	for _, sc := range doc.Scenarios {
		verdict := kernel.EvaluateAuthoredInvalidationAt(sc, bars, now)
		if !verdict.Known {
			at.logWarnf("plan liveness write: %s %s %s — %s; ACCEPTED for this check", tradeDate, session, sc.ID, verdict.Reason)
			if _, err := at.store.RecordPlanLivenessEvent(store.LivenessAuthoredUnknown, fmt.Sprintf("%s:%s:%s:%s:%d", at.id, tradeDate, session, sc.ID, now.UnixNano()), now, verdict.Reason); err != nil {
				at.logWarnf("plan liveness telemetry write failed: %v", err)
			}
		} else if verdict.Invalidated {
			dead = append(dead, sc.ID+": "+verdict.Reason)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	reason := strings.Join(dead, "; ")
	at.logWarnf("plan liveness born-dead REFUSAL: %s %s at %s — %s; re-author within existing attempts", tradeDate, session, kernel.FormatCT(now), reason)
	if _, err := at.store.RecordPlanLivenessEvent(store.LivenessBornDeadRefusal, fmt.Sprintf("%s:%s:%s:%d", at.id, tradeDate, session, now.UnixNano()), now, reason); err != nil {
		at.logWarnf("plan liveness telemetry write failed: %v", err)
	}
	return fmt.Errorf("born-dead authored scenario: %s", reason)
}

// observePlanExhaustionAt is WARN-only per the corrected dispatch. It never
// changes wake clocks, arms, cutoff decisions, or the replan budget.
func (at *AutoTrader) observePlanExhaustionAt(plan *kernel.ActivePlan, states map[string]string, now time.Time) {
	if plan == nil || plan.PlanID == "" || plan.Version <= 0 || len(plan.Doc.Scenarios) == 0 {
		return
	}
	for _, sc := range plan.Doc.Scenarios {
		switch states[sc.ID] {
		case kernel.ScenarioInvalidated, kernel.ScenarioExpired:
		default:
			return
		}
	}
	budget := store.GetReplanBudget(at.store, at.id, kernel.PlanTradeDateFor(plan), plan.Session, at.replanCapFor(plan.Session))
	detail := fmt.Sprintf("v%d tradeable 0/%d by current scenario evaluator; %s, no exhaustion wake; replan budget %d remaining; wake_min_interval_min=%d; cutoff=%dm, cooldown=%dm unchanged", plan.Version, len(plan.Doc.Scenarios), planExhaustionPolicy(), budget.Left(), at.dayPlanCfg().WakeMinIntervalMinutes(), wakeCutoffMinutes(), wakeCooldownMinutes())
	wrote, err := at.store.RecordPlanLivenessEvent(store.LivenessExhaustionWarning, fmt.Sprintf("%s:%s:v%d", at.id, plan.PlanID, plan.Version), now, detail)
	if err != nil {
		at.logWarnf("plan exhaustion warning record failed: %v", err)
	} else if wrote {
		at.logWarnf("plan exhaustion observed at %s: %s", kernel.FormatCT(now), detail)
	}
}

// PlanLivenessBootLine reads persisted event counts. Before the first current
// plan snapshot there is no defensible tradeable number, so startup says n/a.
func PlanLivenessBootLine(st *store.Store) string {
	counts, err := st.PlanLivenessCounts()
	if err != nil {
		return "plan liveness: tradeable=n/a · exhausted-warnings=UNKNOWN · born-dead refusals=UNKNOWN · deaths recorded=UNKNOWN (event store unavailable)"
	}
	return fmt.Sprintf("plan liveness: tradeable=n/a · exhausted-warnings=%d · born-dead refusals=%d · deaths recorded=%d · authored UNKNOWN=%d · exhaustion=%s · flip→reread=n/a(strategy loads at trader start; W-FLIP-REREAD)", counts.ExhaustionWarnings, counts.BornDeadRefusals, counts.DeathsRecorded, counts.AuthoredUnknown, planExhaustionPolicy())
}

func planExhaustionPolicy() string { return "warn-only" }
