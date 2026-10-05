package trader

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"vl/kernel"
	"vl/store"
)

// deskConfirmation reads the same versioned record as the plan card. It never
// evaluates bars or promotes the separate anchor-status estimate to a record.
func (at *AutoTrader) deskConfirmation(now time.Time) DeskLine {
	const source = "versioned scenario_meta.confirm (recorded evaluator)"
	missing := func(why string) DeskLine { return deskUnknown(13, "confirmation", "CONFIRMATION", source, why) }
	plan := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if plan == nil || at.store == nil {
		return missing("no active plan or store")
	}
	raw, err := at.store.GetSystemConfig(store.ScenarioMetaKey(at.id, plan.PlanID, plan.Version))
	if err != nil {
		return missing("confirmation record unavailable")
	}
	var meta struct {
		Confirm    map[string]kernel.ConfirmVerdict `json:"confirm"`
		ObservedAt time.Time                        `json:"observed_at"`
	}
	if json.Unmarshal([]byte(raw), &meta) != nil || meta.ObservedAt.IsZero() {
		return missing("dated confirmation record unavailable for this version")
	}
	var lines []string
	unknown := false
	for _, sc := range plan.Doc.Scenarios {
		if sc.Confirm == nil && !(kernel.IsBreakdownCondition(sc.Condition) && sc.Breakdown != nil) {
			continue
		}
		v, ok := meta.Confirm[sc.ID]
		if !ok || v.Outcome == "" {
			lines = append(lines, sc.ID+" UNKNOWN — recorded bucket evidence unavailable")
			unknown = true
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s", sc.ID, v.Outcome, v.Detail))
		unknown = unknown || v.Outcome == "UNKNOWN"
	}
	if len(lines) == 0 {
		return missing("no recorded confirmation conditions for this version")
	}
	l := DeskLine{N: 13, Key: "confirmation", Label: "CONFIRMATION", Source: source, State: "ok", Verified: !unknown, AsOfMs: meta.ObservedAt.UnixMilli(), Text: fmt.Sprintf("%s v%d recorded: %s · ≈ activation is a separate estimate; order authorization is separate", plan.Session, plan.Version, strings.Join(lines, " | "))}
	if unknown {
		l.State = "unknown"
		l.Reason = "one or more confirmation references or records are unavailable"
	}
	if now.Before(meta.ObservedAt) || now.Sub(meta.ObservedAt) > store.ScenarioSnapshotMaxAge {
		l.State = "stale"
		l.Verified = false
		l.Reason = "stored confirmation snapshot is outside the freshness bound"
	}
	return l
}
