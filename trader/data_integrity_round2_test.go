package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── DATA-INTEGRITY GATE, ROUND 2 (m445, reviewers A + B) ────────────────────
//
// MUTANTS (each shown RED in the round-2 report):
//   T1  dataIntegrityVerdictAt passes keyLevels=false for a mentor trader
//                                  -> TestMentorTraderAsksTheKeyLevelVerdictAtEverySeam RED
//   T2  dataIntegrityVerdictAt passes keyLevels=true for every trader
//                                  -> TestMentorTraderAsksTheKeyLevelVerdictAtEverySeam RED
//   T3  the unknown clock ignores CME-open (the old code)
//                                  -> TestIntegrityUnknownGraceIsOpenTimeOnly RED
//   T4  the decision/agent WARN is logged every time (the old code)
//                                  -> TestIntegrityDecisionRefusalIsLoggedOncePerEpisode RED
//   S1  mentorSeedAtStart does not measure the series   -> TestMentorSeededSeriesRTHHoleBlocksUntilTheRefillReseeds RED
//   S2  mentorReseedOnRefill does not re-measure        -> TestMentorSeededSeriesRTHHoleBlocksUntilTheRefillReseeds RED
//   S3  dataIntegrityVerdictAt never reads the seed leg -> TestMentorSeededSeriesRTHHoleBlocksUntilTheRefillReseeds RED
//   T5  the mentor placement WARN is logged every time (the old code)
//                                  -> TestIntegrityMentorPlacementRefusalIsLoggedOncePerEpisode RED

func warnHook(t *testing.T) *warnCaptureHook {
	t.Helper()
	h := &warnCaptureHook{}
	prev := logger.Log.Hooks
	logger.Log.AddHook(h)
	t.Cleanup(func() { logger.Log.ReplaceHooks(prev) })
	return h
}

// A mentor trader decides on the evaluator's SEEDED state as well as the
// per-pass window, so every seam it crosses must ask the key-level verdict; a
// non-mentor trader never reads the seeded key levels and must not (a clean
// planner/AI path must not be paused by an old RTH hole it cannot read).
func TestMentorTraderAsksTheKeyLevelVerdictAtEverySeam(t *testing.T) {
	// 1. authoring seam
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	now := mentorStaleBlockClock(9, 0)
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 30*time.Second)
	f := &intFake{}
	armIntegrityGate(t, f)
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if len(f.askedKeyLevels) == 0 {
		t.Fatal("the authoring seam never asked the verdict")
	}
	for _, k := range f.askedKeyLevels {
		if !k {
			t.Fatalf("a MENTOR trader's authoring seam must ask the key-level verdict: %v", f.askedKeyLevels)
		}
	}

	// 2. the armed pass
	pnow := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	mat, _, _, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, pnow)
	mentorStaleBlockFeed(t, pnow, 30*time.Second)
	staleStepReceipt(t, time.Second)
	mat.config.StrategyConfig.RiskControl.MentorMode = true
	f2 := &intFake{}
	armIntegrityGate(t, f2)
	mat.mentorOnlyPlacementPass(pnow, nil)
	if len(f2.askedKeyLevels) == 0 {
		t.Fatal("the armed pass never asked the verdict")
	}
	for _, k := range f2.askedKeyLevels {
		if !k {
			t.Fatalf("the mentor armed pass must ask the key-level verdict: %v", f2.askedKeyLevels)
		}
	}

	// 3. a non-mentor trader's AI decision must NOT
	f3 := &intFake{}
	armIntegrityGate(t, f3)
	nat, _ := pauseTrader(t)
	nat.id = "integrity-nonmentor-1"
	_, _ = nat.AdmitManualEntryBracketAt("MNQ", "open_long", 29590, 29630, time.Now())
	if len(f3.askedKeyLevels) == 0 {
		t.Fatal("the agent door never asked the verdict")
	}
	for _, k := range f3.askedKeyLevels {
		if k {
			t.Fatalf("a non-mentor trader never reads the seeded key levels: %v", f3.askedKeyLevels)
		}
	}
}

// A restart over the weekend sits UNKNOWN through the whole closure (nothing
// arrives to judge). The grace is open-market time: counting the closure pushed
// "UNKNOWN" and withdrew resting entries 20 s after the Sunday reopen, before
// the first live bar could land the boot scan (reviewer B probe).
func TestIntegrityUnknownGraceIsOpenTimeOnly(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	sat := ctTimeAt(t, "2026-10-24", "10:00")
	withProvider(t, []market.Kline{{OpenTime: sat.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{class: ntTrader.IntegrityClassUnknown}
	armIntegrityGate(t, fk)
	at.dataIntegrityBeat(sat)
	at.dataIntegrityBeat(sat.Add(24 * time.Hour))
	sun := ctTimeAt(t, "2026-10-25", "17:00").Add(20 * time.Second)
	at.dataIntegrityBeat(sun)
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 0 {
		t.Fatalf("20 s after the reopen the unknown detector has stood 20 s of OPEN time: nothing may be pushed (%q)", rec.all())
	}
	if r, _ := at.integrityWithdrawReason(sun); r != "" {
		t.Fatalf("...and nothing may be withdrawn: %q", r)
	}
	// past the grace of OPEN time it is told, once
	at.dataIntegrityBeat(sun.Add(integrityUnknownGrace + time.Minute))
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 1 {
		t.Fatalf("an unknown that stands past the grace of open time is told once: %q", rec.all())
	}
}

// ONE WARN per episode per path and class on the decision / agent doors; the
// gate-block counter still records every refusal; the next episode logs again.
func TestIntegrityDecisionRefusalIsLoggedOncePerEpisode(t *testing.T) {
	f := &intFake{}
	armIntegrityGate(t, f)
	at, _ := pauseTrader(t)
	at.id = "integrity-warn-1"
	hook := warnHook(t)
	before := gateBlocks(at.id, ntTrader.IntegrityClassHole)
	for i := 0; i < 5; i++ {
		if _, refused := at.AdmitManualEntryBracketAt("MNQ", "open_long", 29590, 29630, time.Now()); !refused {
			t.Fatal("must be refused")
		}
	}
	if n := hook.count("data-integrity: MNQ"); n != 1 {
		t.Fatalf("5 refusals in one episode must log ONE WARN, got %d", n)
	}
	if got := gateBlocks(at.id, ntTrader.IntegrityClassHole); got != before+5 {
		t.Fatalf("the counter records every refusal: %d -> %d", before, got)
	}
	// the episode ends (gate clears, an entry is admitted past it), the next one logs again
	f.blockedFor = func(int) bool { return false }
	_, _ = at.AdmitManualEntryBracketAt("MNQ", "open_long", 29590, 29630, time.Now())
	f.blockedFor = nil
	_, _ = at.AdmitManualEntryBracketAt("MNQ", "open_long", 29590, 29630, time.Now())
	if n := hook.count("data-integrity: MNQ"); n != 2 {
		t.Fatalf("a NEW episode logs again, got %d WARN(s)", n)
	}
}

func TestIntegrityMentorPlacementRefusalIsLoggedOncePerEpisode(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	seedMeasured(at)
	now := mentorStaleBlockClock(9, 0)
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 30*time.Second)
	f := &intFake{}
	armIntegrityGate(t, f)
	mentorPlaceRecorderForTest = func(mentor.Intent, int) {}
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	hook := warnHook(t)
	before := gateBlocks(at.id, ntTrader.IntegrityClassHole)
	for i := 0; i < 4; i++ {
		at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	}
	if n := hook.count("mentor placement REFUSED — data_integrity"); n != 1 {
		t.Fatalf("4 refusals in one episode must log ONE WARN, got %d", n)
	}
	if got := gateBlocks(at.id, ntTrader.IntegrityClassHole); got != before+4 || at.mentorCountSnapshot()["placement_refused_data_integrity"] != 4 {
		t.Fatalf("every refusal is still counted: gate %d -> %d, funnel %v", before, got, at.mentorCountSnapshot())
	}
	// clear, then a new episode
	f.blockedFor = func(int) bool { return false }
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	f.blockedFor = nil
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	if n := hook.count("mentor placement REFUSED — data_integrity"); n != 2 {
		t.Fatalf("a new episode logs again, got %d", n)
	}
}

// ── the mentor's SEEDED series, at the production seed and re-seed ──────────
//
// TestMentorSeededSeriesRTHHoleBlocksUntilTheRefillReseeds runs the real seed
// (mentorSeedAtStart) and the real re-seed (mentorEvalOnce → mentorReseedOnRefill)
// on the 10-14 tape with a 40-minute RTH hole on 10-13 (10:00–10:40 CT) that
// the per-pass 1500-minute window cannot reach and the tracker never sees: the
// mentor verdict must block on it at every mentor seam, name it in CT, and resume
// only when the history refill is in the store and the evaluator has re-seeded
// from the fuller series.
func TestMentorSeededSeriesRTHHoleBlocksUntilTheRefillReseeds(t *testing.T) {
	bars := loadSeedStateBars(t)
	at := seedStateTrader(t)
	at.exchange = "ninjatrader"
	f := &intFake{blockedFor: func(int) bool { return false }} // the tracker says clear: only the seed leg is under test
	armIntegrityGate(t, f)

	holeFrom := time.Date(2026, time.October, 13, 10, 0, 0, 0, kernel.CTLocation())
	holeTo := holeFrom.Add(40 * time.Minute)
	inHole := func(b seedStateBar) bool {
		return b.k.OpenTime >= holeFrom.UnixMilli() && b.k.OpenTime < holeTo.UnixMilli()
	}
	bh := store.NewBarHistoryStore(at.store.GormDB())
	storeLast := seedStateCT(8, 15, 0).UnixMilli()
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool { return !b.refill && b.k.OpenTime <= storeLast && !inHole(b) })); err != nil {
		t.Fatal(err)
	}
	seedAt := seedStateCT(8, 22, 40)
	setMentorNowSource(func() time.Time { return seedAt })
	at.mentorSeedAtStart()

	// the per-pass window (1500 open minutes back from 08:22 on 10-14) does not reach 10-13 10:00
	now := seedAt
	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, false)
	if !v.Blocked || v.Class != ntTrader.IntegrityClassHole || len(v.Windows) != 1 || v.Windows[0] != "10-13 10:00–10:40 CT" {
		t.Fatalf("the seeded series holds an RTH hole: the mentor verdict must block on it and name it: %+v", v)
	}
	if !strings.Contains(v.Reason, "series the mentor's key levels were seeded from") || !strings.Contains(v.Reason, ntTrader.IntegrityActionSeeded) {
		t.Fatalf("the refusal must say which input is holed and the action: %q", v.Reason)
	}
	// ...at the admission chain a mentor arm crosses (production call site)
	reason, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "k"})
	if !refused || !strings.HasPrefix(reason, "data_integrity: ") {
		t.Fatalf("a mentor arm must be refused at the chain for the seeded hole: refused=%v %q", refused, reason)
	}
	// ...and a trader that is NOT in mentor mode never reads the seeded key levels
	at.config.StrategyConfig.RiskControl.MentorMode = false
	if nv := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, now, false); nv.Blocked {
		t.Fatalf("a non-mentor trader must not be paused by the mentor's seeded series: %+v", nv)
	}
	at.config.StrategyConfig.RiskControl.MentorMode = true

	// the owner reloads NT8 history: the replay releases the 40 minutes and
	// announces the refill; the next tick re-seeds from the fuller series
	tick := seedStateCT(8, 23, 0).UnixMilli()
	var fill []seedStateBar
	for _, b := range bars {
		if inHole(b) {
			fill = append(fill, b)
		}
	}
	if len(fill) == 0 {
		t.Fatal("fixture: the tape has no bars in the hole window")
	}
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool {
		return inHole(b) || b.refill || b.k.OpenTime > storeLast && b.k.OpenTime <= tick
	})); err != nil {
		t.Fatal(err)
	}
	ntTrader.NoteHistoryRefill("MNQ", "1m", len(fill), "replay released the 10-13 10:00 hole to the store")
	var full []market.Kline
	for _, b := range bars {
		if b.k.OpenTime <= tick {
			full = append(full, b.k)
		}
	}
	setMentorNowSource(func() time.Time { return seedStateCT(8, 24, 0) })
	if !at.mentorEvalOnce(full[len(full)-mentorBars1mDepth:]) {
		t.Fatal("the tick did not run")
	}
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, seedStateCT(8, 24, 0), false); v.Blocked {
		t.Fatalf("after the refill re-seeded the evaluator from the filled series the gate must resume: %+v", v)
	}
}

// The reach is a code-derived number: the 1m retention default.
func TestSeedReachEqualsTheOneMinuteRetention(t *testing.T) {
	t.Setenv("BAR_RETENTION_DAYS", "")
	if got := store.RetentionDaysFor("1m"); got != ntTrader.IntegritySeedReachDays {
		t.Fatalf("IntegritySeedReachDays (%d) must equal the 1m retention default (%d)", ntTrader.IntegritySeedReachDays, got)
	}
}
