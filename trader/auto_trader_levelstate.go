package trader

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// repairConsumedOnce runs the T4 legacy-row repair exactly once per process.
var repairConsumedOnce sync.Once

// W7 — LEVEL-STATE WRITER (the audit's dead wire: store.LevelStateStore —
// times_tested / consumed / freshness / re-arm cooldown — had ZERO production
// callers). Runs each bar-close cycle while a plan is active. It:
//
//   - EnsureLevel   → persists a level's durable identity (type|price-bin),
//     PRESERVING prior-session state so a burned level stays burned.
//   - MarkConsumed  → when the evaluator shows price accepted THROUGH the level.
//   - RecordPlay    → on a fresh sweep/rejection, decays freshness one grade.
//     Debounced by re-arm: RecordPlay fires only when ReArmEligible
//     (persisted cooldown elapsed) — this is the READER of the state.
//
// A burned level re-touched inside the active window emits a telemetry gate-block +
// a P1 alert. It does NOT touch the executor prompt (no golden change); surfacing
// persisted freshness INTO RenderPlanStatus is a deliberate prompt-regression
// follow-up, flagged in the W7 report — not silently done here.
func (at *AutoTrader) recordLevelState() {
	at.recordLevelStateAt(time.Now())
}

func (at *AutoTrader) recordLevelStateAt(now time.Time) {
	if !at.dayPlanEnabled() || at.store == nil || !kernel.HasTraderPlanProvider(at.id) {
		return
	}
	symbol := at.config.NinjaTraderSymbol
	if symbol == "" || market.FuturesBarsProvider == nil {
		return
	}
	plan := kernel.ActivePlanFor(at.id, symbol)
	if plan == nil {
		return
	}
	bars := market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return
	}
	nowMs := now.UnixMilli()
	// H1/H2 — the day-trade lock that seats levels is the OWNER's resolved
	// proximity_filter_atr, threaded into the detector/scorer (never a hardcoded
	// 1.5 that the config cannot move). max_levels resolves from config (D2 —
	// the planner and the state writers must seat the SAME count).
	maxLevels, _, _, _, _ := resolveSessionPlanCfg(at.dayPlanCfg(), at.activeSessionName(now))
	_, price, dATR := kernel.AssembleScoredLevelsBand(at.id, bars, at.sessionRegistry(now), symbol, maxLevels, now, at.proximityFilterATR(), at.confluenceBandPts())
	if price <= 0 {
		return
	}
	// W15.B — per-SESSION acceptance rule (the override was persisted + rendered
	// but read by nothing); falls back to strategy-level, then 2x5m.
	rule := at.acceptanceRuleFor(at.activeSessionName(now))

	ls := at.store.LevelState()
	// T4 invariant repair (forensics hygiene 2026-08-28): legacy consumed rows
	// without their consuming touch are stamped once per process — a consumed
	// row always carries times_tested ≥ 1 from here on.
	repairConsumedOnce.Do(func() {
		if n, err := ls.RepairConsumedWithoutTouch(); err == nil && n > 0 {
			logger.Infof("🩹 level-state repair: %d legacy consumed rows stamped with their consuming touch (T4 invariant)", n)
		}
	})
	// H3 — the ACTIVATION WINDOW (hide levels >1.5×ATR from the candidate set) is
	// a SPEC INTERNAL CONSTANT, not the owner's proximity_filter_atr. The two were
	// cross-fed here: proximity_filter_atr governs which levels are GENERATED and
	// SEATED (above), ActivationWindowK governs which seated levels are currently
	// LIVE near price (here). Naming the constant at the call site kills the
	// ambiguity.
	active := kernel.ActivePlanLevels(plan.Doc.Levels, price, dATR, kernel.ActivationWindowK)
	// R2 4.7 (2026-08-25) — level-state writers obey min_grade: sub-floor
	// levels get no persisted state (the table they came from can't have them).
	if _, _, _, minGrade, _ := resolveSessionPlanCfg(at.dayPlanCfg(), at.activeSessionName(now)); minGrade != "" {
		active = kernel.FilterPlanLevelsByMinGrade(active, minGrade)
	}
	for _, l := range active {
		typ := kernel.LevelTypeFromLabel(l.Label)
		bin := kernel.LevelBinIndex(l.Price)
		key := store.MakeLevelKey(at.id, symbol, typ, "", bin)

		// Identity: create fresh (grade→initial freshness) or preserve prior state.
		if err := ls.EnsureLevel(&store.LevelStateDB{
			TraderID:  at.id, // P0-cleanup — trader-scoped identity
			Symbol:    symbol,
			LevelType: typ,
			BinIndex:  bin,
			Price:     l.Price,
			Freshness: gradeToFreshness(l.Grade),
		}); err != nil {
			continue
		}

		dir := kernel.DirAbove
		if l.Price < price {
			dir = kernel.DirBelow
		}

		cur, err := ls.Get(key)
		if err != nil || cur == nil {
			continue
		}

		// P1c — WINDOWED consumption. The old evaluation fed the FULL ~33h
		// 1-minute cache into EvaluateLevelFacts, so any level that price merely
		// SAT beyond (support below price all session) read "accepted through"
		// and burned within minutes of the plan's birth — 2026-08-17 NY burned
		// 12 of 12 levels this way (pre-H10 rows additionally counted 2×1m as
		// 2×5m). The verdict is now: touched in-window AND accepted through on
		// rule-TF closes, judged ONLY on bars since this level row was born.
		sinceMs := int64(0)
		if !cur.CreatedAt.IsZero() {
			sinceMs = cur.CreatedAt.UnixMilli()
		}
		f := kernel.EvaluateLevelFacts(kernel.BarsSince(bars, sinceMs), l.Price, dir, rule, 3, nowMs)

		// A burned level (persisted consumed/done) re-entering the active window and
		// re-touched is a fact the state is meant to catch — surface it (no prompt change).
		if (cur.Consumed || cur.Freshness == store.FreshnessDone) && f.StillValid {
			telemetry.IncGateBlock(at.id, "level_burned_retouch")
			at.emitAlert("P1", "level-burned", "burned:"+key,
				"Consumed level re-touched: "+l.Label, "role-flipped — tradeable both directions")
			// P1c — a consumed level is never deleted: it role-flips and stays on
			// the map; the retouch is exactly the tradeable event. No `continue`.
		}

		if kernel.ConsumedSince(bars, l.Price, rule, sinceMs, nowMs) {
			_ = ls.MarkConsumed(key, nowMs) // touched AND accepted through in-window → role-flip
			continue
		}

		// A fresh sweep/rejection decays freshness — but only once per re-arm cooldown.
		// ReArmEligible READS the persisted last_play_ms (the reader wire): true only
		// when not consumed, not done, and the cooldown has elapsed.
		if f.Rejected || f.Swept {
			if ok, _ := store.ReArmEligible(cur, nowMs, store.ReArmCooldownMin, true); ok {
				_, _ = ls.RecordPlay(key, nowMs)
			}
		}
	}
}

// gradeToFreshness maps a plan level's quality grade (A|B|C) to the initial
// freshness a NEW level row starts at, so a C-grade level enters more decayed than
// an A. (EnsureLevel preserves freshness on levels that already exist.)
func gradeToFreshness(grade string) string {
	switch grade {
	case "B":
		return store.FreshnessB
	case "C":
		return store.FreshnessC
	default:
		return store.FreshnessA
	}
}

// recordScenarioState (W16/R1) persists each scenario's live status for the
// active plan, so the card can stop painting every play "armed".
//
// Runs beside recordLevelState on the same cadence and behind the same gates.
// Storage is the system_config key the API already reads
// ("scenario_status:<plan_id>"), whose only writer until now was the sandbox
// seeder — so this needs no new table, no migration, and no API change: the
// existing passthrough starts returning real data the moment this writes.
//
// It NEVER touches the executor prompt (same discipline as recordLevelState) —
// scenario status is a reporting surface, so no golden can move.
//
// Scenarios whose anchor level cannot be resolved are OMITTED from the map.
// The FE falls back for a missing id, which is the honest outcome: better to
// keep saying nothing than to invent a status. If NO scenario resolves, the key
// is not written at all.
func (at *AutoTrader) recordScenarioState() {
	at.recordScenarioStateAt(time.Now())
}

func (at *AutoTrader) recordScenarioStateAt(now time.Time) {
	if !at.dayPlanEnabled() || at.store == nil || !kernel.HasTraderPlanProvider(at.id) {
		return
	}
	symbol := at.config.NinjaTraderSymbol
	if symbol == "" || market.FuturesBarsProvider == nil {
		return
	}
	plan := kernel.ActivePlanFor(at.id, symbol)
	if plan == nil {
		return
	}
	bars := market.FuturesBarsProvider(symbol, kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return
	}
	maxLevels, _, _, _, _ := resolveSessionPlanCfg(at.dayPlanCfg(), at.activeSessionName(now))
	_, price, dATR := kernel.AssembleScoredLevelsBand(at.id, bars, at.sessionRegistry(now), symbol, maxLevels, now, at.proximityFilterATR(), at.confluenceBandPts())
	if price <= 0 {
		return
	}
	rule := at.acceptanceRuleFor(at.activeSessionName(now))

	// ActivePlanProvider only ever returns a live plan, so planLive is true here;
	// the expired projection is the API's job when it serves a rolled plan.
	// H3 — the activation-window k is the SPEC INTERNAL CONSTANT (see
	// recordLevelState); proximity_filter_atr governs generation/seating only.
	// FIX 7 (F1, 2026-08-27) — evaluate triggers ONLY on bars closed AFTER the
	// plan was born: the full-cache evaluation let pre-plan sweeps/rejects read
	// as "triggered now" (the 13 false-positive machine-trigger lines of
	// 2026-08-26). The arm gate shares this evaluator.
	windowed := kernel.BarsSince(bars, plan.BirthMs)
	statuses, evals := kernel.EvaluatePlanScenarios(
		plan.Doc, windowed, price, dATR, kernel.ActivationWindowK, rule, true, now.UnixMilli())

	identities := at.observeScenarioIdentity(&plan.Doc, plan.PlanID, plan.Version, evals, now)
	if len(statuses) == 0 {
		// Nothing resolvable — say nothing rather than write an empty verdict.
		return
	}
	blob, err := json.Marshal(statuses)
	if err != nil {
		return
	}
	resolvedPlanID := plan.PlanID
	if resolvedPlanID == "" || plan.Version <= 0 {
		at.logWarnf("scenario record unavailable: missing plan identity/version")
		return
	}
	key := store.ScenarioStatusKey(at.id, resolvedPlanID, plan.Version)
	if err := at.store.SetSystemConfig(key, string(blob)); err != nil {
		at.logWarnf("🎯 scenario-state write failed for %s: %v", key, err)
		return
	}
	// A1/A4 (fail-register wave) — persist the verdict BASIS (machine vs
	// prose-anchor heuristic) and the unevaluable list, so the card renders
	// honestly instead of dressing a heuristic as a machine verdict.
	basis := map[string]string{}
	var unevaluable []string
	for _, e := range evals {
		if e.HasAnchor {
			basis[e.ID] = e.Basis
		} else {
			unevaluable = append(unevaluable, e.ID)
		}
	}
	// C1: per-scenario confirm verdicts (MET / NOT MET) for the card chips.
	// F2 (waterfall-class wave) — two-leg scenarios report the OVERALL verdict
	// plus every leg; a partial never renders as MET.
	confirms := map[string]kernel.ConfirmVerdict{}
	for _, sc := range plan.Doc.Scenarios {
		if sc.Confirm != nil || (kernel.IsBreakdownCondition(sc.Condition) && sc.Breakdown != nil) {
			confirms[sc.ID] = kernel.EvaluateScenarioConfirm(sc, bars, plan.BirthMs, now.UnixMilli())
		}
	}
	if metaBlob, mErr := json.Marshal(map[string]any{"level_identity": identities, "basis": basis, "unevaluable": unevaluable, "confirm": confirms, "observed_at": now}); mErr == nil {
		_ = at.store.SetSystemConfig(store.ScenarioMetaKey(at.id, resolvedPlanID, plan.Version), string(metaBlob))
		recordResearchPermissions(resolvedPlanID, plan.Version, string(metaBlob), now, evals)
	}
	// INVALIDATION-WIRED (2026-09-03) — stamp WHEN a scenario first read
	// invalidated, once. The evaluator is stateless, so without this the gate's
	// refusal could only say "as of now"; the arm that cost $140 on 09-03 was
	// armed twelve minutes after the verdict, and twelve minutes is the point.
	for _, e := range evals {
		if !e.HasAnchor || e.Status != kernel.ScenarioInvalidated {
			continue
		}
		wrote, err := at.store.RecordScenarioDeath(at.id, store.ScenarioDeath{
			PlanID: resolvedPlanID, Version: plan.Version, ScenarioID: e.ID,
			Anchor: e.Anchor, Price: price, Cause: e.Status,
			Condition: fmt.Sprintf("%s at %.2f using %s: %s", e.Status, e.Anchor, rule, e.Reason),
			Basis:     e.Basis, ObservedAt: now,
		})
		if err != nil {
			at.logWarnf("scenario death record failed: v%d %s: %v", plan.Version, e.ID, err)
		} else if wrote {
			at.logInfof("scenario death recorded: %s v%d %s cause=%s anchor=%.2f price=%.2f first observed=%s basis=%s — %s", plan.Session, plan.Version, e.ID, e.Status, e.Anchor, price, kernel.FormatCT(now), e.Basis, e.Reason)
		}
	}
	at.observePlanExhaustionAt(plan, statuses, now)
	if at.scenarioStateLog != string(blob) {
		at.scenarioStateLog = string(blob)
		for _, e := range evals {
			if e.HasAnchor {
				// FIX 7 (F1) — the label says what it is: an ESTIMATE.
				at.logInfof("🎯 scenario %s → ≈%s @ %.2f (%s — anchor estimate; arm gate uses this verdict, authored invalidation is separate)", e.ID, e.Status, e.Anchor, e.Reason)
			} else {
				// A4: unevaluable is owner-relevant — WARN so it reaches the
				// log_events sink + dashboard, not just the file log.
				at.logWarnf("🎯 scenario %s UNEVALUABLE — %s (instruction/trigger has no price that snaps to a plan level ±2pts)", e.ID, e.Reason)
			}
		}
	}
}
