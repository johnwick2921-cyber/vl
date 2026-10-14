package trader

import (
	"fmt"
	"strconv"

	"vl/kernel"
	"vl/store"
)

// R22 F1 (owner ruling R4, 2026-10-14) — SWEEP SETUPS WAIT FOR THEIR TRIGGER.
//
// The arm gate holds every leg of a sweep setup (reclaim / sweep_reclaim) until
// the scenario's confirm is machine-MET — and that confirm now counts only
// closes AFTER the sweep of its ref (kernel/sweep_trigger.go). This is the
// existing wait_confirm DORMANT state of the armed_orders lifecycle (the row
// stays `armed`, nothing is sent to NT8), applied whatever the plan authored:
// no conditional order type and no wire change is needed, which is why it is
// the smallest honest fix. A leg the plan already authored wait_confirm is
// unchanged except for the corrected confirm.
//
// noteSweepWait records the hold ONCE per (plan, version, scenario, leg) — one
// INFO line + one system counter — when the hold comes from the sweep rule and
// not from an authored wait_confirm (that is the new behaviour; counters
// record, never infer).
func (at *AutoTrader) noteSweepWait(plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, authoredWait bool, v kernel.ConfirmVerdict) {
	if at == nil || plan == nil || (authoredWait && !kernel.VerdictWaitsForSweep(v)) {
		return // an authored wait_confirm still short of its close is the pre-R22 hold, already logged by the verdict line
	}
	key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":sweep_wait"
	if !armRefusalChanged(&at.armRefusalLast, key, "sweep_wait") {
		return
	}
	shown := ""
	if at.store != nil {
		ckey := fmt.Sprintf("sweep_waits_for_trigger:%s:%s:%s", at.id, kernel.PlanTradeDateFor(plan), plan.Session)
		if n, err := store.IncSystemCounter(at.store, ckey); err != nil {
			at.logWarnf("⏳ sweep-wait counter write failed: %v", err)
		} else {
			shown = fmt.Sprintf(" · sweep holds this session: %d", n)
		}
	}
	at.logInfof("⏳ arm %s %s leg %d (%s) HELD — a sweep setup waits for its trigger before any order reaches NT8: %s%s",
		plan.Session, sc.ID, li+1, sc.Condition, v.Detail, shown)
}
