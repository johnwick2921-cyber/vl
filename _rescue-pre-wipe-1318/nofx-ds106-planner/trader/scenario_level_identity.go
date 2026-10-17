package trader

import (
	"encoding/json"
	"fmt"
	"nofx/kernel"
	"nofx/levelidentity"
	"nofx/store"
	"time"
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
func (at *AutoTrader) recordPlanIdentity(planID string, version int, w kernel.IdentityWarnings, now time.Time) {
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
		if r.Disagreed {
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
		if r.Disagreed && at.store != nil {
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
		_ = json.Unmarshal([]byte(p.Doc), &doc)
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
