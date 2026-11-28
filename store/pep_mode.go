package store

// Trading-mode names: a trader is exactly ONE of these (owner ruling
// 2026-10-29). The dashboard chip, /api/my-traders and /api/traders/:id/config
// all read them from here.
const (
	TradingModeAI     = "ai"     // AI / planner (the default)
	TradingModeMentor = "mentor" // risk_control.mentor_mode
	TradingModePep    = "pep"    // pep.pep_enabled
)

// TradingMode names the mode this strategy runs. A strategy that turns pep ON
// together with mentor mode or the day plan is a CONFLICT (TradingModeConflict);
// it is reported as the conflicting mode "conflict" so no surface paints a
// trader as pep or mentor while it is in fact refused at start.
func (c *StrategyConfig) TradingMode() string {
	if c == nil {
		return TradingModeAI
	}
	if c.TradingModeConflict() != nil {
		return "conflict"
	}
	switch {
	case c.PepEnabled():
		return TradingModePep
	case c.RiskControl.MentorMode:
		return TradingModeMentor
	}
	return TradingModeAI
}
