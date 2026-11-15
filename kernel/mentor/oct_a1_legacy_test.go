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
	return octDRevert(octBCRevert(octA2Revert(c)))
}

// octA2Revert applies every OCT-EXACT A2 revert (CTO-decisions.md section A2):
// the Sept entry/level gates the October build turned OFF. Mechanics tests of
// those gates build from it (preOctCfg); the October defaults are pinned by
// oct_a2_test.go.
func octA2Revert(c Config) Config {
	c.HTFBoxGates = true
	c.Early15mRead = true
	c.FifteenMinIBRule = FifteenMinRequire5mISB
	c.TFConflictEnabled = true
	c.MTFConfluenceEnabled = true
	c.RollingLevelTouchOwns = true
	c.KeypairRuleEnabled = true
	c.EmaLossRelock = EmaRelockUntilZoneExit
	c.NearBoxRoomPHL = true
	c.NearBoxRoomMultiple = 2
	c.FlagWallTarget = true
	c.TriggerRetestWrongWayRefuse = true
	c.PHLPullbackLow = PHLPullbackLowPullback
	c.BoxPingPongGate = true
	c.BoxRoomMultiple = 0 // unset = the shared RoomMultiple (2), so legacy tests that tune RoomMultiple still drive the box room
	c.PHLISBPreferredAtBox = true
	c.TriggerBoxZoneGate = true
	c.Box.TouchBandPts = 0.25
	c.ISBLocationSizeCuts = true
	c.PullbackWarnCut = true
	c.KeyLevelPrunePts = 20
	c.TrendlineLevelPreferPts = 2
	c.TrendlineBoxBeats = true
	c.TrendlineMaxLinesPerAnchor = 1
	c.TrendlineIsTarget = true
	c.ConfluenceHold2R = true
	return c
}

// octBCRevert applies the OCT-EXACT BC reverts (CTO-decisions.md sections B/C):
// the section-B knobs back to their pre-October OFF values and the C-rulings
// back to the Sept behaviour. Mechanics tests of those paths build from
// preOctCfg; the October defaults are pinned by oct_bc_test.go.
func octBCRevert(c Config) Config {
	c.PHL5mBoxSameDirAllowed = false
	c.Swing.CoverLaterWick = false
	c.RollingStaleTouchReleases = false
	c.RollingKeypairTapeExempt = false
	c.ISBTargetFillClose = false
	c.PHLMinAbovePriorPts = 0 // B6: 15 -> 0
	c.BoxTrendlinePartialNotRoom = false
	c.LevelFirstTouchLock = true
	c.ISBRoomMultiple = 0 // C4 revert: 0 = follow RoomMultiple (the stored mentor_isb_room_multiple=2 is the same on a default RoomMultiple)
	c.Counter5mScalpPts = 0
	c.ISBRevOldExtremeRoom = false // P4-missesB WE8: the reverse ISB keeps the r1 fill-close next to an old extreme
	c.ISBRevFillClose = false      // close r1: the reverse ISB keeps the September level-or-refuse target
	c.BoxCarryFirstLatched = false // P3-missesA E9: the carried box is the newest live box per role, the seed scan stops at a pre-open box
	c.BoxCounter5mScalp = false    // P3-missesA E9: a box return against the 5m stays refused (box_against_5m_trend / box_trigger_side)
	return c
}
