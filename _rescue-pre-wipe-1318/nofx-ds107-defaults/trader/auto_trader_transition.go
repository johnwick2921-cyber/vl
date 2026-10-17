package trader

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// G4 (regime wave, 2026-08-21) — TRANSITION STAND-DOWN state machine. Runs per
// cycle after the G2 snapshot is computed: a counter-trend CHoCH/MSS on the
// plan's bias TF (15m) that the flip has NOT yet confirmed opens TRANSITION;
// a plan replacement (flip confirmed → re-plan), a with-trend BOS resumption,
// or TRANSITION_MAX_MIN expiry closes it. While open, plan-direction entries
// are refused by the executor gate (kernel.TransitionStanddownVerdict).
//
// Card chip: the state is mirrored to system_config
// (store.TransitionKey(planID, version)) so the plan card can render
// "⏸ TRANSITION — awaiting confirmation".

// observeTransitionStanddown advances the state machine and wires ctx for the
// executor gate. Never fails the cycle: any unresolved input leaves the gate
// dormant (fail-open).
// Clock seam (class 60): the entry point owns the wall clock and does nothing
// else; the rule lives in the …At body so a test can state its own hour.
func (at *AutoTrader) observeTransitionStanddown(ctx *kernel.Context) {
	at.observeTransitionStanddownAt(time.Now(), ctx)
}

func (at *AutoTrader) observeTransitionStanddownAt(nowT time.Time, ctx *kernel.Context) {
	ctx.TransitionActive = false
	if at.store == nil {
		return
	}
	sc := at.GetStrategyConfig()
	if sc == nil || !sc.TransitionStanddownEnabled() {
		at.transition = kernel.TransitionState{}
		at.persistTransition(false)
		return
	}
	plan := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if plan == nil {
		at.transition = kernel.TransitionState{}
		at.persistTransition(false)
		return
	}
	bias := strings.ToLower(strings.TrimSpace(plan.Doc.Bias.Direction))
	if bias != "long" && bias != "short" {
		at.transition = kernel.TransitionState{}
		at.persistTransition(false)
		return
	}
	now := nowT.UnixMilli()

	// (a) flip/death confirmed → the planner re-planned (new plan identity):
	// close instantly, the new plan's own structure stands.
	if at.transition.Active && (at.transition.PlanID != plan.PlanID || at.transition.PlanVersion != plan.Version) {
		at.logInfof("⏸ TRANSITION closed — plan %s v%d replaced (flip confirmed → re-plan).", at.transition.PlanID, at.transition.PlanVersion)
		at.transition = kernel.TransitionState{}
		at.transitionClosedAtMs = 0 // a new plan's events stand on their own
	}

	st15, ok := ctx.Structure["15m"]
	oppDir := "down"
	if bias == "short" {
		oppDir = "up"
	}

	if at.transition.Active {
		// (b) resumption: a with-trend BOS on 15m after the trigger closes it.
		trendDir := "down"
		if bias == "long" {
			trendDir = "up"
		}
		for _, e := range st15.LastEvents {
			if e.Type == "BOS" && e.Dir == trendDir && e.TimeMs > at.transition.SinceMs {
				at.logInfof("⏸ TRANSITION closed — BOS resumption %s @%s.", e.Dir, kernel.ClockCT(time.UnixMilli(e.TimeMs)))
				at.transitionClosedAtMs = at.transition.SinceMs // don't reopen on the same trigger
				at.transition = kernel.TransitionState{}
				break
			}
		}
	}
	if at.transition.Active {
		// (c) timer expiry.
		if now-at.transition.SinceMs >= int64(kernel.TransitionMaxMin())*60_000 {
			at.logWarnf("⏸ TRANSITION closed — %dmin cap expired (unconfirmed %s).", kernel.TransitionMaxMin(), at.transition.Detail)
			at.transitionClosedAtMs = at.transition.SinceMs // don't reopen on the same trigger
			at.transition = kernel.TransitionState{}
		}
	}

	if !at.transition.Active && ok {
		// Open on a counter-trend CHoCH/MSS born AFTER the plan AND after any
		// trigger this cycle already closed (the cap/resumption must not loop).
		for _, e := range st15.LastEvents {
			if (e.Type == "CHoCH" || e.Type == "MSS") && e.Dir == oppDir && e.TimeMs > plan.BirthMs && e.TimeMs > at.transitionClosedAtMs {
				detail := fmt.Sprintf("%s-%s 15m @%.2f %s", e.Type, e.Dir, e.Price, kernel.ClockCT(time.UnixMilli(e.TimeMs)))
				at.transition = kernel.TransitionState{
					Active:      true,
					Dir:         bias,
					Detail:      detail,
					SinceMs:     e.TimeMs,
					PlanID:      plan.PlanID,
					PlanVersion: plan.Version,
				}
				at.logWarnf("⏸ TRANSITION OPENED — plan %s v%d %s: %s (stand-down until flip, resumption, or %dmin cap).",
					plan.PlanID, plan.Version, bias, detail, kernel.TransitionMaxMin())
				break
			}
		}
	}

	if at.transition.Active {
		ctx.TransitionActive = true
		ctx.TransitionDir = at.transition.Dir
		ctx.TransitionDetail = at.transition.Detail
	}
	at.persistTransition(at.transition.Active)
}

// persistTransition mirrors the state for the plan card chip (idempotent).
func (at *AutoTrader) persistTransition(active bool) {
	if at.store == nil {
		return
	}
	st := at.transition
	if !active {
		st = kernel.TransitionState{Active: false}
	}
	key := store.TransitionKey(st.PlanID, st.PlanVersion)
	if key == "" {
		return
	}
	blob, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = at.store.SetSystemConfig(key, string(blob))
}

// maybeWakePlannerOnMSS is G4.6 (addendum): a fresh structure MSS on the
// plan's bias TF (15m) is the FOURTH planner wake-up — one wake per MSS event
// (deduped by plan version + event instant). Death replans, first reads and
// owner rereads/resets are the other three.
// Clock seam (class 60): the entry point owns the wall clock and does nothing
// else; the rule lives in the …At body so a test can state its own hour.
func (at *AutoTrader) maybeWakePlannerOnMSS(session, tradeDate string, row *store.PlanDB) {
	at.maybeWakePlannerOnMSSAt(time.Now(), session, tradeDate, row)
}

func (at *AutoTrader) maybeWakePlannerOnMSSAt(now time.Time, session, tradeDate string, row *store.PlanDB) {
	if market.FuturesBarsProvider == nil {
		return
	}
	// W-FLIP-OWNS-THE-BREACH R1/R3 (2026-09-17): same gate as the level-event
	// wake — a breached (or stale-skipped) flip line defers the MSS wake. On
	// 2026-09-17 22:42:33 CT this path authored ASIA v2 on the tape the flip
	// evaluator had refused the same second (flip=stale_bars, age 453s).
	if at.wakeDeferredByFlip(now, session, row) {
		return
	}
	bars1m := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	snap := kernel.StructureSnapshot(bars1m, now.UnixMilli())
	st15, ok := snap["15m"]
	if !ok {
		return
	}
	var mss *kernel.StructureEvent
	for i := range st15.LastEvents {
		if st15.LastEvents[i].Type == "MSS" && st15.LastEvents[i].TimeMs > row.CreatedAt.UnixMilli() {
			e := st15.LastEvents[i]
			mss = &e
		}
	}
	if mss == nil {
		return
	}
	key := fmt.Sprintf("%s:%d:%d", row.PlanID, row.Version, mss.TimeMs)
	if at.lastMSSWakeKey == key {
		return // one wake per MSS event
	}
	// W6 — the MSS wake shares the planner-wake clock with the level-event
	// wake: both fire at most once per wake_min_interval_min, whichever
	// class goes first. This makes the two wake classes budget-parity
	// siblings instead of a same-cycle double fire.
	if at.config.StrategyConfig != nil && at.config.StrategyConfig.DayPlan != nil &&
		!at.lastPlannerWakeAt.IsZero() &&
		time.Since(at.lastPlannerWakeAt) < time.Duration(at.config.StrategyConfig.DayPlan.WakeMinIntervalMinutes())*time.Minute {
		at.logWarnf("🗓️ structure MSS on %s %s — SKIPPED: %.0fm elapsed < wake_min_interval_min (%dm).", session, tradeDate, time.Since(at.lastPlannerWakeAt).Minutes(), at.config.StrategyConfig.DayPlan.WakeMinIntervalMinutes())
		return
	}
	// W6-D (2026-08-25) — wakes are UNLIMITED and spend NO budget: an MSS wake
	// can never consume the death re-plan cap, and can never cause a
	// replans_exhausted NO-TRADE. The dedupe key + the shared min-interval
	// throttle are the only frequency limits.
	at.lastMSSWakeKey = key
	at.lastPlannerWakeAt = now // W6 — shared wake clock
	at.logWarnf("🗓️ structure MSS on %s %s (%s) — waking the planner (G4.6, 4th wake-up).", session, tradeDate, mss.Detail)
	// C5 — an MSS wake strands owner overlays exactly like a death re-plan does:
	// make the loss audible BEFORE the read (the P1 alert names the count).
	at.warnIfReplanOrphansOverlays(row)
	// P0.4-G — carry the prior plan's levels for continuity (the owner's
	// complaint: every re-plan dropped the old map and the anchors moved).
	// W6-C (2026-08-25) — the wake re-read is NON-fatal (a failed read keeps
	// the active plan; failClosed=false) and runs ASYNC so a slow/timing-out
	// planner can never stall the decision loop for minutes.
	go func() {
		_ = at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, "structure_mss", "structure MSS: "+mss.Detail, priorPlanLevelLines(row), false)
		// C5 — sticky owner edits survive the MSS wake exactly like the death path.
		if fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); fErr == nil && fresh != nil && fresh.Version != row.Version {
			at.carryOwnerEditsInto(fresh.PlanID, row.Version, fresh.Version)
		}
	}()
}
