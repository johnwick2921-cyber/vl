package mentor

import (
	"strings"
	"testing"
)

// OCT-EXACT build step BC (CTO-decisions.md sections B and C, owner order
// 2026-11-14 "mentor mode = the October course exactly"). The reverts are
// octBCRevert (oct_a1_legacy_test.go).

// Section B: eight knobs ON at their October values.
func TestOctBC_SectionBDefaults(t *testing.T) {
	d := DefaultConfig()
	on := map[string]bool{
		"PHL5mBoxSameDirAllowed (B1, Oct D4.3 @1:08:24 R70/R71)":     d.PHL5mBoxSameDirAllowed,
		"Swing.CoverLaterWick (B2, Oct D5.2-R33 @19:40)":             d.Swing.CoverLaterWick,
		"RollingStaleTouchReleases (B3)":                             d.RollingStaleTouchReleases,
		"RollingKeypairTapeExempt (B4)":                              d.RollingKeypairTapeExempt,
		"ISBTargetFillClose (B5, Oct D2.2 @28:37)":                   d.ISBTargetFillClose,
		"BoxTrendlinePartialNotRoom (B8, Sept D3.3 p1 @05:15-07:46)": d.BoxTrendlinePartialNotRoom,
	}
	for name, v := range on {
		if !v {
			t.Errorf("%s default = false, want true", name)
		}
	}
	if d.PHLMinAbovePriorPts != 15 {
		t.Errorf("PHLMinAbovePriorPts = %v, want 15 (B6, Oct D4.2-R41)", d.PHLMinAbovePriorPts)
	}
}

// B6 pinned on the video's own example: the Sun 01 Nov entry 6.50 pts above the
// old low "em cũng không gọi nó là Potential High Low" (Oct D4.2 @36:45-40:20)
// is refused by the DEFAULT config now (15), and the knob at 0 restores today.
func TestOctBC_B6DefaultRefusesTheSixPointFiveExample(t *testing.T) {
	bars := m413Load(t, "testdata/round2_mnq_1m_20261101_2000ct.csv")
	at := func(h, m int) int { return phl1At(t, bars, 2026, 11, 1, h, m) }
	old := bars[at(20, 36)].Low
	ref := bars[at(20, 56)] // entry 28,771.00 = old low + 11.25: inside the 15-pt stand-off
	tr := Touch{Outcome: TouchReject, RefBar: ref, ApproachedFrom: SideLong}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLRoomMultiple = 1
	_, ok, why := PHLPLHR2Levels(tr, Level{Price: 28800, Role: OldHigh}, 0, 100, old, nil, cfg)
	if ok || phlRefusalKey(why) != "phl_min_above_old_extreme" {
		t.Fatalf("default config: ok=%v why=%q, want phl_min_above_old_extreme", ok, why)
	}
	cfg.PHLMinAbovePriorPts = 0
	if _, ok0, why0 := PHLPLHR2Levels(tr, Level{Price: 28800, Role: OldHigh}, 0, 100, old, nil, cfg); !ok0 {
		t.Fatalf("revert 0 must restore today's behaviour, refused: %s", why0)
	}
}

// C3: the first touch closing through only CANCELS that order (Oct D5.2-R23/R24).
func TestOctBC_C3FirstTouchThroughOnlyCancels(t *testing.T) {
	wrongWay := []Intent{
		{Action: CancelArm, Reason: "close through"},
		{Action: LevelInvalid, LevelKey: "k"},
	}
	key := Level{Key: "key_level:100:1", Kind: KindKeyLevel, Price: 100}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelBreakRetest = KeyLevelBreakRetestOff // isolate the first-touch rule from the Uno/retest paths
	if cfg.LevelFirstTouchLock {
		t.Fatal("default LevelFirstTouchLock must be false (October)")
	}
	e := New(cfg)
	e.State.LevelArms = map[string]LevelArm{key.Key: {ArmID: "lvl-9"}}
	out := handleTouchIntents(e, key, wrongWay)
	if len(out) != 1 || out[0].Action != CancelArm || out[0].ArmID != "lvl-9" {
		t.Fatalf("October: only the order is cancelled, got %+v", out)
	}
	if e.State.ISBOnly[key.Key] {
		t.Fatal("October: the level must NOT be locked ISB-only")
	}
	if n := e.State.Refusals["level_invalid_first_touch"]; n != 0 {
		t.Fatalf("October: no level_invalid_first_touch refusal, got %d", n)
	}
	// revert = today
	cfg.LevelFirstTouchLock = true
	e2 := New(cfg)
	e2.State.LevelArms = map[string]LevelArm{key.Key: {ArmID: "lvl-9"}}
	out = handleTouchIntents(e2, key, wrongWay)
	if len(out) != 2 || !e2.State.ISBOnly[key.Key] {
		t.Fatalf("revert (lock on): cancel + level_invalid + ISB-only, got %+v isbOnly=%v", out, e2.State.ISBOnly)
	}
}

// C4: the ISB room is 1R (Oct D2.3-R48 @49:38), revert 2. Production Tick on the
// MTF fixture's long ISB (entry 104.5, stop 100.5, risk 4): the only level beyond
// is 111 = 1.63R away. 1R room trades it; the Sept 2x room refuses it "room".
func TestOctBC_C4ISBRoomIsOneR(t *testing.T) {
	d := DefaultConfig()
	if d.ISBRoomMultiple != 1 || d.RoomMultiple != 2 {
		t.Fatalf("ISBRoomMultiple=%v RoomMultiple=%v, want 1 / 2 (RoomMultiple stays the shared Sept value)", d.ISBRoomMultiple, d.RoomMultiple)
	}
	z := d
	z.ISBRoomMultiple = 0
	if z.isbRoomMultiple() != 2 || d.isbRoomMultiple() != 1 {
		t.Fatalf("isbRoomMultiple: default %v (want 1), 0 -> %v (want follow RoomMultiple 2)", d.isbRoomMultiple(), z.isbRoomMultiple())
	}
	run := func(isbRoom float64) (bool, map[string]int) {
		e, bars, now := mtfFixture(t, false)
		e.Cfg.RoomMultiple = 2
		e.Cfg.ISBRoomMultiple = isbRoom
		e.Cfg.ISBTargetFillClose = false // B5 (fill-candle-close target) bypasses the room refusal; isolate the room rule
		e.State.SeedLevels = []Level{
			{Key: "K-below", Kind: KindKeyLevel, Price: 90},
			{Key: "K-far", Kind: KindKeyLevel, Price: 111},
		}
		ins := e.Tick(bars, now)
		return longISBEmitted(ins), e.State.Refusals
	}
	if ok, ref := run(1); !ok {
		t.Fatalf("1R room: the ISB with a level 1.63R away must trade; refusals %v", ref)
	}
	if ok, ref := run(2); ok || ref["room"] == 0 {
		t.Fatalf("revert 2x: the same ISB must be refused room; emitted=%v refusals %v", ok, ref)
	}
	// the PHL/PLH and box rooms are untouched by C4.
	if d.PHLRoomMultiple != 1 || d.boxRoomMultiple() != 1 {
		t.Fatalf("PHL/box rooms changed: %v %v", d.PHLRoomMultiple, d.boxRoomMultiple())
	}
}

// C1: a counter-5m PHL/PLH at the EMA 34 is a scalp with a 15-pt first target
// (Oct D5.4 Ex.1/Ex.2), floored at 1R and never beyond its own target.
func TestOctBC_C1CounterFiveMinuteScalpTarget(t *testing.T) {
	mk := func(side Side, price, stop, target float64, anchor string) []Intent {
		return []Intent{{
			Action: PlaceStopEntry, Side: side, Setup: "PHL", Price: price, Stop: stop, Target: target,
			AnchorKey: anchor, RunnerTarget: target + 5, RunnerPartialTP: price + 7, Reason: "PHL at the EMA 34",
		}}
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	if cfg.Counter5mScalpPts != 15 {
		t.Fatalf("default Counter5mScalpPts = %v, want 15", cfg.Counter5mScalpPts)
	}
	e := New(cfg)

	// long under a SELL 5m trigger, EMA 34 anchor, stop 8, level target 40 away.
	out := e.stampCounter5m(mk(SideLong, 28392.50, 28384.50, 28432.50, string(KindEMA34)), SideShort)
	in := out[0]
	if !HasFlag(in.Flag, FlagCounter5m) {
		t.Fatalf("the counter-5m flag (reduced size) must stand, flag=%q", in.Flag)
	}
	if in.Target != 28407.50 || in.TargetPts != 15 || in.RunnerTarget != 0 || in.RunnerPartialTP != 0 {
		t.Fatalf("scalp: target %.2f pts %.2f runner %.2f partial %.2f, want 28407.50 / 15 / 0 / 0", in.Target, in.TargetPts, in.RunnerTarget, in.RunnerPartialTP)
	}
	if !strings.Contains(in.Reason, "counter-5m scalp") {
		t.Fatalf("reason must name the scalp: %q", in.Reason)
	}

	// short mirror, the location-gate EMA 34 key.
	out = e.stampCounter5m(mk(SideShort, 100, 108, 60, string(KindEMA34HTF)), SideLong)
	if out[0].Target != 85 {
		t.Fatalf("short scalp target = %.2f, want 85", out[0].Target)
	}

	// a stop wider than 15 floors the scalp at 1R, never a sub-1:1 target.
	out = e.stampCounter5m(mk(SideLong, 100, 80, 150, string(KindEMA34)), SideShort)
	if out[0].Target != 120 {
		t.Fatalf("20-pt stop: target = %.2f, want the 1R floor 120", out[0].Target)
	}
	// never farther than the room's own (shorter) target.
	out = e.stampCounter5m(mk(SideLong, 100, 95, 110, string(KindEMA34)), SideShort)
	if out[0].Target != 110 {
		t.Fatalf("a 10-pt target is already shorter than 15: %.2f", out[0].Target)
	}
	// not at the EMA 34 (a key level), or with the trigger: untouched.
	out = e.stampCounter5m(mk(SideLong, 100, 92, 140, "key_level:99:1"), SideShort)
	if out[0].Target != 140 {
		t.Fatalf("a non-EMA anchor keeps its target, got %.2f", out[0].Target)
	}
	out = e.stampCounter5m(mk(SideLong, 100, 92, 140, string(KindEMA34)), SideLong)
	if out[0].Target != 140 || HasFlag(out[0].Flag, FlagCounter5m) {
		t.Fatalf("an entry WITH the 5m trigger is no scalp: %+v", out[0])
	}
	// revert 0 = today (flag, no scalp target).
	cfg.Counter5mScalpPts = 0
	e0 := New(cfg)
	out = e0.stampCounter5m(mk(SideLong, 100, 92, 140, string(KindEMA34)), SideShort)
	if out[0].Target != 140 || !HasFlag(out[0].Flag, FlagCounter5m) {
		t.Fatalf("revert 0: flag only, target unchanged: %+v", out[0])
	}
}

// C5: the 3-candle rule stays (no video trade is refused by it: scorecard with
// PHLMinCandlesFromExtreme=0 leaves the pass set unchanged, 22/55 both ways).
func TestOctBC_C5ThreeCandleRuleStays(t *testing.T) {
	if got := DefaultConfig().PHLMinCandlesFromExtreme; got != 3 {
		t.Fatalf("PHLMinCandlesFromExtreme = %d, want 3 (C5 kept)", got)
	}
}
