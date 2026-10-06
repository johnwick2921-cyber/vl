package trader

import (
	"encoding/json"
	"fmt"
	"time"
	"vl/kernel"
	"vl/levelidentity"
	"vl/store"
)

// Recording only: even a panic cannot refuse a plan or interrupt evaluation.
func (at *AutoTrader) containLevelIdentity() {
	if r := recover(); r != nil {
		at.logWarnf("🪪 level identity recording panic contained: %v", r)
	}
}
func (at *AutoTrader) stampPlanIdentity(doc *kernel.PlanDoc, candidates []kernel.MapCandidate) (out kernel.IdentityWarnings) {
	defer at.containLevelIdentity()
	out = kernel.StampAuthoredIdentity(doc, candidates)
	for id, r := range out.Scenarios {
		if r.Level == nil {
			at.logWarnf("🪪 scenario %s level_id=%v WARN accepted: %s", id, identityIDText(r.LevelID), r.Basis)
			continue
		}
		at.logInfof("🪪 scenario %s level_id=%s formed_close_ms=%d tf=%s", id, *r.LevelID, *r.Level.FormedCloseMs, *r.Level.TF)
	}
	for _, c := range candidates {
		if c.ID == nil {
			_, reason := levelidentity.ID(kernel.IdentityInputs(c.Identity))
			at.logInfof("🪪 map %.2f id=NULL: missing %s", c.Price, reason)
		}
	}
	return out
}
func identityIDText(id *string) string {
	if id == nil || *id == "" {
		return "NULL"
	}
	return *id
}
func (at *AutoTrader) recordPlanIdentity(planID string, version int, w kernel.IdentityWarnings, doc *kernel.PlanDoc, now time.Time) {
	defer at.containLevelIdentity()
	if at.store == nil {
		return
	}
	for id, r := range w.Scenarios {
		kind := "unresolved"
		switch r.Basis {
		case "candidate_id":
			kind = "named"
		case "legacy:no_level_id":
			kind = "unnamed"
		}
		at.recordIdentityEvent(planID, version, id, kind, r, now)
		// P7 (WAVE 1a-plan, #190) — a zone-accepted scenario disagrees with
		// the evaluator BY DESIGN (FVG distal-edge anchors, seated S/D+OB
		// zone edges); the publish path must not count it.
		if r.Disagreed && !zoneAcceptedIdentity(r) {
			at.recordIdentityEvent(planID, version, id, "heuristic_disagreed", r, now)
		}
	}
}
func (at *AutoTrader) recordIdentityEvent(planID string, version int, scenario, kind string, r kernel.ScenarioIdentity, now time.Time) {
	raw, err := json.Marshal(r)
	if err != nil {
		at.logWarnf("🪪 identity evidence unavailable: %v", err)
		return
	}
	wrote, err := at.store.RecordLevelIdentityEvent(at.id, store.LevelIdentityEvent{PlanID: planID, Version: version, ScenarioID: scenario, Kind: kind, Detail: raw, At: now})
	if err != nil {
		at.logWarnf("🪪 identity event write failed: %v", err)
		return
	}
	if wrote && kind == "heuristic_disagreed" {
		at.logWarnf("🪪 heuristic-disagreed plan=%s v%d scenario=%s id=%s candidate=%.2f evaluator=%.2f — recorded; evaluator unchanged", planID, version, scenario, identityIDText(r.LevelID), r.Level.Price, *r.EvaluatorAnchor)
	}
}
func (at *AutoTrader) observeScenarioIdentity(doc *kernel.PlanDoc, planID string, version int, evals []kernel.ScenarioEval, now time.Time) (out map[string]kernel.ScenarioIdentity) {
	defer at.containLevelIdentity()
	out = map[string]kernel.ScenarioIdentity{}
	if doc == nil {
		return out
	}
	byID := map[string]kernel.ScenarioEval{}
	for _, e := range evals {
		byID[e.ID] = e
	}
	for _, sc := range doc.Scenarios {
		e := byID[sc.ID]
		r := kernel.ResolveScenarioIdentity(sc, doc.IdentityLevels, e.Anchor, e.HasAnchor)
		out[sc.ID] = r
		if r.Disagreed && at.store != nil && !zoneAcceptedIdentity(r) {
			at.recordIdentityEvent(planID, version, sc.ID, "heuristic_disagreed", r, now)
		}
	}
	return out
}

func levelIdentityBootLine(doc *kernel.PlanDoc, c *store.LevelIdentityCounts, b *store.IdentityBackfillResult) string {
	mapText := "map ids=n/a (no-formation=n/a; no captured map)"
	if doc != nil && doc.IdentityLevels != nil {
		ids, missing := 0, 0
		for _, l := range doc.IdentityLevels {
			if _, ok := kernel.LevelByID(l.ID, doc.IdentityLevels); ok {
				ids++
			} else if _, ok := kernel.LevelByReferenceID(l.ID, doc.IdentityLevels); ok {
				ids++ // W-GEOMETRY-REFUSAL (F3): ref| ids count as resolved
			}
			if l.FormedCloseMs == nil {
				missing++
			}
		}
		mapText = fmt.Sprintf("map ids=%d/%d (no-formation=%d)", ids, len(doc.IdentityLevels), missing)
	}
	counts := "scenarios named=n/a unnamed=n/a[WARN] unresolved=n/a[WARN] · heuristic-disagreed=n/a"
	if c != nil {
		counts = fmt.Sprintf("scenarios named=%d unnamed=%d[WARN] unresolved=%d[WARN] · heuristic-disagreed=%d", c.Named, c.Unnamed, c.Unresolved, c.HeuristicDisagreed)
	}
	backfill := "backfill recomputed=n/a unrecomputable=n/a untouched=n/a"
	if b != nil {
		backfill = fmt.Sprintf("backfill recomputed=%d unrecomputable=%d untouched=%d", b.Recomputed, b.Unrecomputable, b.Untouched)
	}
	return "🪪 level identity: " + mapText + " · " + counts + " · " + backfill
}
func (at *AutoTrader) logLevelIdentityBootAt(now time.Time) {
	defer at.containLevelIdentity()
	if at.exchange != "ninjatrader" || at.config.StrategyConfig == nil || at.dayPlanCfg() == nil || !at.dayPlanCfg().PlanEnabled {
		return
	}
	var doc *kernel.PlanDoc
	if at.store == nil {
		at.logInfof("%s · trader=%s", levelIdentityBootLine(nil, nil, nil), at.id)
		at.logInfof("%s · trader=%s", writeTruthBootLine(nil, at.id), at.id)
		return
	}
	if p, err := at.store.Plan().GetLatestPlanForTraderSession(plannerTradeDateCT(now), at.activeSessionName(now), at.id); err == nil && p != nil {
		// WAVE 1a-plan P2 — the boot line reads the ONE fold: an owner overlay
		// that adds an identity level must be visible in the boot line.
		if d, ok := resolveActivePlanDoc(at.store, p); ok {
			doc = &d
		}
	}
	var counts *store.LevelIdentityCounts
	if c, err := at.store.LevelIdentityCounts(at.id); err == nil {
		counts = &c
	} else {
		at.logWarnf("🪪 counters unavailable: %v", err)
	}
	var backfill *store.IdentityBackfillResult
	if b, err := at.store.BackfillLevelIdentity(at.id); err == nil {
		backfill = &b
	} else {
		at.logWarnf("🪪 backfill unavailable: %v", err)
	}
	at.logInfof("%s · trader=%s · counters=recorded unique plan-version scenarios; legacy IDs stay NULL", levelIdentityBootLine(doc, counts, backfill), at.id)
	at.logInfof("%s · trader=%s", writeTruthBootLine(at.store, at.id), at.id)
}

// zoneAcceptedIdentity (P7, #190; skeptic F12) — a zone-accepted scenario
// disagrees with the evaluator anchor by design: an FVG entry anchors at the
// DISTAL edge, and a seated S/D+OB zone edge is a band, not a price. The ONE
// predicate is the KERNEL's zone-aware one — the same test the write check
// runs (kernel.IdentityAgreesZoneAware) — so an anchor inside the resolved
// level's [lo−tol, hi+tol] agrees, whatever the level's label or type. The
// P7 label/type allowlist was wrong in BOTH directions: a plain FVG zone
// level (label "FVG") at a zone edge still recorded, and an out-of-zone
// anchor naming a Demand/Supply/OB label stopped recording.
func zoneAcceptedIdentity(r kernel.ScenarioIdentity) bool {
	if r.Level == nil || r.EvaluatorAnchor == nil {
		return false
	}
	return kernel.IdentityAgreesZoneAware(*r.Level, *r.EvaluatorAnchor)
}
