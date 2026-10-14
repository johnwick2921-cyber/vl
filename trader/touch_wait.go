package trader

import (
	"fmt"
	"strconv"
	"strings"

	"vl/kernel"
	"vl/store"
	"vl/telemetry"
)

// F-1 (planner-touch-fills, 2026-10-14) — LEVEL PLAYS WAIT FOR A REACTION AT
// THE LEVEL. The arm gate holds a level play's market_in_zone leg (hold /
// acceptance / reject / fvg_entry — kernel.TouchSetupWaits) until its confirm is
// machine-MET, and that confirm now counts only after price has reached the
// level (kernel/touch_trigger.go). The hold is the existing wait_confirm DORMANT
// state (the row is never authorized, nothing reaches NT8) — the #515 sweep-wait
// shape, generalised; no wire change.
//
// noteConfirmWait is the hold's ONE note: the sweep rule's (unchanged) and the
// touch rule's.
func (at *AutoTrader) noteConfirmWait(plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, leg kernel.PlanArmLeg, v kernel.ConfirmVerdict) {
	if kernel.SweepSetupWaits(sc, leg.WaitConfirm) { // exactly the legs that reached it before F-1 (#515 byte-identical)
		at.noteSweepWait(plan, sc, li, leg.WaitConfirm, v)
	}
	at.noteTouchWait(plan, sc, li, leg, v)
}

// noteTouchWait records the touch hold ONCE per (plan, version, scenario, leg) —
// one INFO line, one system counter confirm_waits_for_touch:<condition>:<trader>:
// <date>:<session> and one gate-block tick — when the hold is NEW behaviour:
// either the leg authored no wait_confirm (before F-1 it rested at once), or the
// verdict is held because the level was never reached (before F-1 those closes
// counted). An authored wait still short of its post-touch closes is the
// pre-F-1 hold, already explained by the verdict line (counters record, never
// infer).
func (at *AutoTrader) noteTouchWait(plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, leg kernel.PlanArmLeg, v kernel.ConfirmVerdict) {
	if at == nil || plan == nil || !kernel.TouchSetupWaits(sc, &leg) {
		return
	}
	if leg.WaitConfirm && !kernel.VerdictWaitsForTouch(v) {
		return
	}
	key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":touch_wait"
	if !armRefusalChanged(&at.armRefusalLast, key, "touch_wait") {
		return
	}
	cond := strings.ToLower(strings.TrimSpace(sc.Condition))
	telemetry.IncGateBlock(at.id, "confirm_waits_for_touch")
	shown := ""
	if at.store != nil {
		ckey := fmt.Sprintf("confirm_waits_for_touch:%s:%s:%s:%s", cond, at.id, kernel.PlanTradeDateFor(plan), plan.Session)
		if n, err := store.IncSystemCounter(at.store, ckey); err != nil {
			at.logWarnf("⏳ touch-wait counter write failed: %v", err)
		} else {
			shown = fmt.Sprintf(" · %s touch holds this session: %d", cond, n)
		}
	}
	at.logInfof("⏳ arm %s %s leg %d (%s) HELD — a level play waits for its reaction AT the level before any order reaches NT8 (no resting limit beyond the zone): %s%s",
		plan.Session, sc.ID, li+1, cond, v.Detail, shown)
}
