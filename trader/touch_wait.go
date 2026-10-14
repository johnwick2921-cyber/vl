package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
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
	if at == nil || plan == nil {
		return
	}
	if kernel.VerdictMissingTape(v) {
		// N5: an empty tape is UNKNOWN — said once, never counted as a hold.
		key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":confirm_wait"
		if armRefusalChanged(&at.armRefusalLast, key, "missing_tape") {
			at.logInfof("⏳ arm %s %s leg %d (%s) HELD — confirm UNKNOWN: no closed 1m bar since plan publication (nothing counted)",
				plan.Session, sc.ID, li+1, strings.ToLower(strings.TrimSpace(sc.Condition)))
		}
		return
	}
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

// armConfirmFresh is the arm gate's step after a MET confirm (rescan 10-14, CTO
// binding input): the trigger must be FRESH — its EVENT (v.EventMs, never the
// evaluation instant: a #519 backfill after a feed gap must not turn an
// outage-time touch into a new trigger) no older than day_plan.zone_rest_max_min
// (a trigger is good for as long as its limit may rest), and price no farther
// from the level than STALE_CONFIRM_ATR × ATR5m (the executor prompt's
// staleness rule, now consulted by the gate). Fresh → the MET line, arming.
// Stale → held: one WARN + one counter confirm_stale:<reason>:<trader>:<date>:
// <session> per (plan, version, scenario, leg). The grader asks the SAME
// question at the entry fill (adherence_trigger.go).
func (at *AutoTrader) armConfirmFresh(ledger *store.ArmedOrderStore, plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, authored, leg kernel.PlanArmLeg, zl zoneLeg, v kernel.ConfirmVerdict, bars []market.Kline, now time.Time, scope *armedPassScope) bool {
	fresh, class, why := at.confirmFreshAt(sc, v, bars, plan.BirthMs, now.UnixMilli())
	if fresh {
		at.logInfof("⚔️ arm %s leg %d wait_confirm MET (%s) — arming", sc.ID, li+1, kernel.ConfirmGatedRuleLabel(sc)) // W2: the rule(s) gated + source
		return true
	}
	at.respecHeldLeg(ledger, plan, sc, li, authored, leg, zl, now)
	scope.note(sc.ID, "waiting: confirm stale ("+why+")")
	key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":confirm_stale"
	if !armRefusalChanged(&at.armRefusalLast, key, class) {
		return false
	}
	telemetry.IncGateBlock(at.id, "confirm_stale")
	shown := ""
	if at.store != nil {
		ckey := fmt.Sprintf("confirm_stale:%s:%s:%s:%s", class, at.id, kernel.PlanTradeDateFor(plan), plan.Session)
		if n, err := store.IncSystemCounter(at.store, ckey); err != nil {
			at.logWarnf("⏳ confirm-stale counter write failed: %v", err)
		} else {
			shown = fmt.Sprintf(" · confirm_stale:%s this session: %d", class, n)
		}
	}
	at.logWarnf("⏳ arm %s %s leg %d (%s) HELD — confirm MET but STALE: %s%s",
		plan.Session, sc.ID, li+1, strings.ToLower(strings.TrimSpace(sc.Condition)), why, shown)
	return false
}

// confirmFreshAt is the ONE freshness read for the gate and the grade: price =
// the last CLOSED 1m close before nowMs, ATR5m = kernel.StaleConfirmATR5m on
// the closed tape, max age = the strategy's resolved zone_rest_max_min.
func (at *AutoTrader) confirmFreshAt(sc kernel.PlanScenario, v kernel.ConfirmVerdict, bars []market.Kline, birthMs, nowMs int64) (fresh bool, class, why string) {
	closed := make([]market.Kline, 0, len(bars))
	for _, b := range bars {
		if b.OpenTime+60_000 <= nowMs {
			closed = append(closed, b)
		}
	}
	price := 0.0
	if n := len(closed); n > 0 {
		price = closed[n-1].Close
	}
	maxMin, _ := store.ResolveZoneRestMaxMin(at.dayPlanCfg())
	return kernel.ConfirmFresh(sc, v, bars, birthMs, kernel.ConfirmFreshInput{NowMs: nowMs, Price: price, ATR5m: kernel.StaleConfirmATR5m(closed), MaxAgeMs: int64(maxMin) * 60_000})
}

// holdForConfirm is the arm gate's NOT-MET branch: the hold's note, and the
// re-spec of a working order this leg already has (respecHeldLeg).
func (at *AutoTrader) holdForConfirm(ledger *store.ArmedOrderStore, plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, authored, leg kernel.PlanArmLeg, zl zoneLeg, v kernel.ConfirmVerdict, now time.Time) {
	at.noteConfirmWait(plan, sc, li, leg, v)
	at.respecHeldLeg(ledger, plan, sc, li, authored, leg, zl, now)
}

// respecHeldLeg — F-1: a leg HELD for its confirm (not met, or stale) keeps an
// order an earlier version placed after its OWN reaction, but a newer version
// that re-priced it (zone moved, or the authored bracket/entry ≥ 2 ticks) must
// still cancel it. The hold used to `continue` before the re-spec — with F-1
// holding every level play under a new version until a new reaction, a
// stale-zone limit could otherwise rest on. Same decision, same filled-arm
// guard, same counters as the admitted path (respecWorkingArm).
func (at *AutoTrader) respecHeldLeg(ledger *store.ArmedOrderStore, plan *kernel.ActivePlan, sc kernel.PlanScenario, li int, authored, leg kernel.PlanArmLeg, zl zoneLeg, now time.Time) {
	if ledger == nil || plan == nil {
		return
	}
	existing, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return
	}
	for i := range existing {
		r := existing[i]
		if r.TraderID == at.id && r.PlanID == plan.PlanID && r.Scenario == sc.ID && r.LegIndex == li {
			at.respecWorkingArm(ledger, plan, sc, li, r, authored, leg, zl, now)
			return
		}
	}
}
