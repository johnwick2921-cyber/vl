package trader

import (
	"fmt"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// ── W-EXEC-TRUTH W3 (e) D13 — THE STRICT NUDGE ──────────────────────────────
//
// Under plan_mode=strict the AI decision is refused as a market entry (R5,
// EntryGate leg 0) — 64 refusals 09-10..22, several of them citing the very
// scenario whose arm was waiting for the next 2-minute scan. The decision is
// now a NUDGE: when it cites a matched scenario whose DOC arm is an enabled
// market_in_zone arm, ONE armed pass runs immediately, scoped to that
// scenario's placement (the FULL pass: every authoring gate and the admitted
// set as for the scan, idempotent — it acts only on 'armed' rows), and the
// decision record carries
// the executor's verdict. The decision itself stays refused: Success is false
// and nothing flips it to the arm path.
//
// It fires ONLY for:
//   - the exact leg-0 strict refusal (StrictNonArmRefusalPrefix) — a feed-down,
//     hold, breaker, session or any other refusal never nudges;
//   - an active plan (ActivePlanFor), a Matched citation (ClassifyCitation);
//   - the cited scenario's doc arm Enabled with an effective market_in_zone leg.
//
// Kernel-dropped decisions (validateDecision) never reach executeDecision and
// so never nudge — unchanged.

// strictNudge is the wall-clock entry (clock-seams.list). The production
// caller, executeDecisionWithRecordAt, calls strictNudgeAt with admitEntry's
// OWN instant so the gate and the nudge judge one moment.
func (at *AutoTrader) strictNudge(decision *kernel.Decision, refusal string) (string, bool) {
	return at.strictNudgeAt(decision, refusal, time.Now())
}

// strictNudgeAt runs the nudge at now. ok=false: this refusal does not nudge
// (the record keeps the refusal alone). ok=true: the verdict to append.
func (at *AutoTrader) strictNudgeAt(decision *kernel.Decision, refusal string, now time.Time) (string, bool) {
	if at == nil || at.store == nil || decision == nil || !strings.HasPrefix(refusal, StrictNonArmRefusalPrefix) {
		return "", false
	}
	if decision.Action != "open_long" && decision.Action != "open_short" {
		return "", false
	}
	plan := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if plan == nil {
		return "", false
	}
	cit := kernel.ClassifyCitation(decision.Action, decision.CitedScenario, plan.Doc)
	if !cit.Matched {
		return "", false
	}
	var sc *kernel.PlanScenario
	for i := range plan.Doc.Scenarios {
		if strings.EqualFold(plan.Doc.Scenarios[i].ID, cit.Cited) {
			sc = &plan.Doc.Scenarios[i]
			break
		}
	}
	if sc == nil || !scenarioHasZoneArm(*sc) {
		return "", false
	}
	at.logInfof("🚦 strict: decision cites %s → armed pass", sc.ID)
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	// Never nil: a nil snapshot would skip the HTF veto (fail-open).
	snap := kernel.StructureSnapshot(bars, now.UnixMilli())
	scope := &armedPassScope{Scenario: sc.ID, Trigger: "nudge"}
	// The FULL pass (CTO D14 ruling): every authoring gate, the admitted set,
	// then placement — the scope filters placement rows only.
	at.maybeManageArmedOrdersAtOpts(snap, now, armedPassOpts{scope: scope}) // takes armedPassMu
	return at.nudgeVerdict(plan, scope), true
}

// nudgeVerdict renders what the scoped pass did, in the executor's words:
//
//	placed limit <far> signal <sid>
//	already working: limit <x> signal <sid>
//	already filled @ <x>
//	waiting: price below|above zone <lo>–<hi> (last <p>) | waiting: price unknown
//	waiting: confirm not met (<rule>)
//	refused: <class>: <gate text>
func (at *AutoTrader) nudgeVerdict(plan *kernel.ActivePlan, scope *armedPassScope) string {
	if scope.placed {
		return fmt.Sprintf("placed limit %.2f signal %s", scope.limit, scope.signal)
	}
	if rows, err := at.store.ArmedOrders().ListForPlan(plan.PlanID); err == nil {
		for i := len(rows) - 1; i >= 0; i-- {
			r := rows[i]
			if r.TraderID != at.id || !strings.EqualFold(r.Scenario, scope.Scenario) || strings.TrimSpace(r.SignalID) == "" {
				continue
			}
			if !store.IsTerminalArmState(r.State) && r.State != store.StateCancelPending {
				return fmt.Sprintf("already working: limit %.2f signal %s", r.EntryPx, r.SignalID)
			}
		}
		for i := len(rows) - 1; i >= 0; i-- {
			r := rows[i]
			if r.TraderID == at.id && strings.EqualFold(r.Scenario, scope.Scenario) && r.State == store.StateFilled && r.Version == plan.Version {
				return fmt.Sprintf("already filled @ %.2f", r.FillPrice)
			}
		}
	}
	if scope.reason != "" {
		return scope.reason
	}
	return "refused: not_armed: no armed row for " + scope.Scenario + " this pass"
}
