package mentor

// preOctCfg is DefaultConfig with every OCT-EXACT A1 gate switched back ON
// (build step A1, CTO-decisions.md section A1, owner order 2026-11-14): the
// pre-October behaviour (HTF 4h/1h gate, §7 day gate, SWING4H, ORB part 2, the
// open-5m gate, news wait/freeze, G1 leg budget, G2 loss box, mid-range reject
// side, break direction lock, orb_isb_mostly_inside 0.80). The older tests in
// this package pin THOSE gates' mechanics (their fixtures and goldens are
// Sept-era), so they build their evaluator from this config; the October
// defaults themselves are pinned by oct_a1_test.go and the trader tests. The
// revert of each gate is exactly this value.
func preOctCfg() Config {
	c := DefaultConfig()
	c.HTFGateEnabled = true
	c.DayGateEnabled = true
	c.Swing4HEnabled = true
	c.OrbPart2Enabled = true
	c.Open5mGateEnabled = true
	c.LossBoxEnabled = true
	c.NewsWaitBothLines = true
	c.NewsBarsExcluded = true
	c.LegBudgetEnabled = true
	c.MidRangeRejectSide = true
	c.BreakDirectionLock = true
	c.OrbISBFractionOutside = 0.80
	return c
}
