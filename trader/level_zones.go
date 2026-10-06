package trader

import (
	"time"
	"vl/kernel"
)

func (at *AutoTrader) logLevelZonesBootAt(now time.Time) {
	if at.exchange != "ninjatrader" || at.config.StrategyConfig == nil || at.dayPlanCfg() == nil || !at.dayPlanCfg().PlanEnabled {
		return
	}
	strategy := "UNKNOWN"
	if at.store != nil {
		if row, err := at.store.Trader().GetByID(at.id); err == nil && row != nil {
			strategy = row.StrategyID
		}
	}
	cap, _, _, _, _ := resolveSessionPlanCfg(at.dayPlanCfg(), at.activeSessionName(now))
	at.logInfof("%s · trader=%s bound-strategy=%s · session cap re-resolved each read; no backfill", kernel.ZoneBootLine(kernel.ResolveZoneOptions(cap)), at.id, strategy)
}
