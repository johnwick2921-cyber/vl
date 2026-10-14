package trader

import (
	"vl/kernel"
	"vl/market"
	"vl/store"
)

// R22 F8 — the adherence grade reads whether the cited scenario's REQUIRED
// trigger existed at the entry fill, with the SAME evaluation the arm gate runs
// (kernel.ScenarioTriggerAt over the overlay-resolved doc as of the entry).
//
// Returns "" (no change to the grade) whenever the answer is not knowable:
// no plan link, the plan row/doc is gone, the scenario is not in the doc, the
// scenario has no required trigger, or the 1m tape does not cover the window
// from plan birth to the entry. Never a guessed "missed".
func (at *AutoTrader) entryTriggerVerdict(p *store.TraderPosition, bars []market.Kline) (verdict, detail string) {
	if at == nil || at.store == nil || p == nil || p.PlanID == "" || p.PlanVersion <= 0 || p.CitedScenarioID == "" || p.EntryTime <= 0 {
		return "", ""
	}
	row, err := at.store.Plan().GetPlan(p.PlanID, p.PlanVersion)
	if err != nil || row == nil {
		return "", ""
	}
	doc, ok := resolveActivePlanDocAsOf(at.store, row, p.EntryTime)
	if !ok {
		return "", ""
	}
	birth := row.CreatedAt.UnixMilli()
	if len(bars) == 0 || bars[0].OpenTime > birth || bars[len(bars)-1].OpenTime < p.EntryTime-60_000 {
		return "", "" // the tape does not cover birth → entry: not verifiable
	}
	for _, sc := range doc.Scenarios {
		if sc.ID != p.CitedScenarioID {
			continue
		}
		if !kernel.ScenarioTriggerRequired(sc) {
			return "", ""
		}
		v := kernel.ScenarioTriggerAt(sc, bars, birth, p.EntryTime)
		if v.Met {
			return kernel.TriggerMet, v.Detail
		}
		return kernel.TriggerMissed, v.Detail
	}
	return "", ""
}
