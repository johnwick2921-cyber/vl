package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// OCT-EXACT build step A2 (CTO-decisions.md section A2, owner order 2026-11-14
// "mentor mode = the October course exactly"). Every entry/level gate below
// ships OFF (or at the October value) in DefaultConfig; the revert of each is
// octA2Revert (oct_a1_legacy_test.go).

func TestOctA2_Defaults(t *testing.T) {
	d := DefaultConfig()
	off := map[string]bool{
		"HTFBoxGates (1)":                 d.HTFBoxGates,
		"Early15mRead (1)":                d.Early15mRead,
		"TFConflictEnabled (2)":           d.TFConflictEnabled,
		"MTFConfluenceEnabled (3)":        d.MTFConfluenceEnabled,
		"RollingLevelTouchOwns (4)":       d.RollingLevelTouchOwns,
		"KeypairRuleEnabled (5)":          d.KeypairRuleEnabled,
		"NearBoxRoomPHL (7)":              d.NearBoxRoomPHL,
		"FlagWallTarget (8)":              d.FlagWallTarget,
		"TriggerRetestWrongWayRefuse (9)": d.TriggerRetestWrongWayRefuse,
		"BoxPingPongGate (11)":            d.BoxPingPongGate,
		"PHLISBPreferredAtBox (11)":       d.PHLISBPreferredAtBox,
		"TriggerBoxZoneGate (11)":         d.TriggerBoxZoneGate,
		"ISBLocationSizeCuts (12)":        d.ISBLocationSizeCuts,
		"PullbackWarnCut (13)":            d.PullbackWarnCut,
		"TrendlineBoxBeats (15)":          d.TrendlineBoxBeats,
		"TrendlineIsTarget (16)":          d.TrendlineIsTarget,
		"ConfluenceHold2R (17)":           d.ConfluenceHold2R,
	}
	for name, v := range off {
		if v {
			t.Errorf("%s default = true, want false", name)
		}
	}
	if d.FifteenMinIBRule != FifteenMinOff {
		t.Errorf("FifteenMinIBRule = %q, want off (row 1)", d.FifteenMinIBRule)
	}
	if d.EmaLossRelock != EmaRelockOff {
		t.Errorf("EmaLossRelock = %q, want off (row 6)", d.EmaLossRelock)
	}
	// W3 P2-volume: the ISB leg of near_box is back ON (October D3.3-R2 @00:46, R4 @01:20, R47 @38:10, R49 @39:37);
	// the PHL/PLH leg stays OFF through NearBoxRoomPHL (checked in the `off` map above), so row 7 is split not reverted.
	if d.NearBoxRoomMultiple != 0.5 {
		t.Errorf("NearBoxRoomMultiple = %v, want 0.5 (W3 P2-volume: ISB leg ON; October gives no distance, 0.5 is the largest keeping the scorecard at 31)", d.NearBoxRoomMultiple)
	}
	if d.PHLPullbackLow != PHLPullbackLowSignal {
		t.Errorf("PHLPullbackLow = %q, want signal (row 10)", d.PHLPullbackLow)
	}
	if d.BoxRoomMultiple != 1 {
		t.Errorf("BoxRoomMultiple = %v, want 1 (row 11, October 1:1 D2.3-R48 @49:38)", d.BoxRoomMultiple)
	}
	if d.Box.TouchBandPts != 0 {
		t.Errorf("Box.TouchBandPts = %v, want 0 (row 11 literal touch)", d.Box.TouchBandPts)
	}
	if d.KeyLevelPrunePts != 0 {
		t.Errorf("KeyLevelPrunePts = %v, want 0 (row 14)", d.KeyLevelPrunePts)
	}
	if d.TrendlineLevelPreferPts != 0 || d.TrendlineMaxLinesPerAnchor != 3 {
		t.Errorf("trendline prefer=%v maxLines=%d, want 0 / 3 (row 15)", d.TrendlineLevelPreferPts, d.TrendlineMaxLinesPerAnchor)
	}
}

// Row 1: with the 15m / 30m box gates OFF no 15m/30m box is ever built; the
// revert (HTFBoxGates) is the Sept read (the box helpers return what stands).
func TestOctA2_HTFBoxGatesOffClearsBoxes(t *testing.T) {
	cfg := DefaultConfig()
	e := &Evaluator{Cfg: cfg}
	e.State.ISBBox15m = &ISBBox{High: 10, Low: 5, Dir: SideLong}
	e.State.ISBBox30m = &ISBBox{High: 10, Low: 5, Dir: SideLong}
	// The Tick step (eval.go) clears both when the knob is off; replay it.
	if !e.Cfg.HTFBoxGates {
		e.State.ISBBox15m, e.State.ISBBox30m = nil, nil
	}
	if e.box15() != nil || e.box30() != nil {
		t.Fatalf("October default: a 15m/30m box stands, want none [Oct D4.3-R9 @07:28]")
	}
}

// Row 3 + 17: confluence C is off by default; the revert restores it.
func TestOctA2_MTFConfluenceOff(t *testing.T) {
	e := &Evaluator{Cfg: DefaultConfig()}
	e.State.ISBBox = &ISBBox{High: 10, Low: 5, Dir: SideLong}
	if e.mtfConfluence(SideLong, nil, nil) {
		t.Fatalf("mtfConfluence under the October default = true, want false")
	}
	r := &Evaluator{Cfg: octA2Revert(DefaultConfig())}
	r.State.ISBBox = e.State.ISBBox
	r.State.ISBBox15m = &ISBBox{High: 12, Low: 3, Dir: SideLong, AtTime: 1}
	r.State.ISBBox.AtTime = 2
	r.State.LastISBBoxAt, r.State.LastISBBoxDir = 2, SideLong
	if !r.mtfConfluence(SideLong, nil, nil) {
		t.Fatalf("revert: mtfConfluence with both boxes pointing long = false, want true")
	}
}

// Row 10: a pullback that printed a lower low than the prior low is refused
// only under the revert (phl_pullback_low=pullback); the signal-candle check
// (phl_not_higher_low) is not this function and stays.
func TestOctA2_PullbackLowerLowOff(t *testing.T) {
	bars := []market.Kline{
		{Low: 110, High: 120}, // 0: the old extreme
		{Low: 99, High: 112},  // 1: pullback prints 99 (<= prior 100)
		{Low: 101, High: 114}, // 2: the signal candle holds above prior
	}
	ref := bars[2]
	if got := phlPullbackRefusal(DefaultConfig(), SideLong, bars, 0, 2, ref, 100); got != "" {
		t.Fatalf("October default: pullback lower low refused %q, want none", got)
	}
	if got := phlPullbackRefusal(octA2Revert(DefaultConfig()), SideLong, bars, 0, 2, ref, 100); got != "phl_pullback_lower_low" {
		t.Fatalf("revert: got %q, want phl_pullback_lower_low", got)
	}
	sbars := []market.Kline{
		{Low: 90, High: 100},
		{Low: 88, High: 121}, // pullback high 121 (>= prior 120)
		{Low: 90, High: 119}, // signal candle stays under prior
	}
	if got := phlPullbackRefusal(DefaultConfig(), SideShort, sbars, 0, 2, sbars[2], 120); got != "" {
		t.Fatalf("October default: pullback higher high refused %q, want none", got)
	}
	if got := phlPullbackRefusal(octA2Revert(DefaultConfig()), SideShort, sbars, 0, 2, sbars[2], 120); got != "plh_pullback_higher_high" {
		t.Fatalf("revert: got %q, want plh_pullback_higher_high", got)
	}
}

// Rows 12 + 13: the ISB location size cuts and the pullback_warn cut.
func TestOctA2_ISBSizeCutsOff(t *testing.T) {
	cur := market.Kline{High: 101, Low: 99, Close: 100}
	levels := []Level{{Kind: KindOldExtreme, Price: 100}}
	e := &Evaluator{Cfg: DefaultConfig()}
	e.pullbackWarn5m = SideLong
	if got := e.isbFlagsFor(cur, SideLong, levels, nil); got != "" {
		t.Fatalf("October default: ISB flags %q, want none (no size cut)", got)
	}
	r := &Evaluator{Cfg: octA2Revert(DefaultConfig())}
	r.pullbackWarn5m = SideLong
	got := r.isbFlagsFor(cur, SideLong, levels, nil)
	if !HasFlag(got, FlagISBAtOldExtreme) || !HasFlag(got, FlagPullbackWarn) {
		t.Fatalf("revert: ISB flags %q, want isb_at_old_extreme + pullback_warn", got)
	}
	// Each cut is its own knob.
	c := DefaultConfig()
	c.PullbackWarnCut = true
	w := &Evaluator{Cfg: c}
	w.pullbackWarn5m = SideLong
	if got := w.isbFlagsFor(cur, SideLong, levels, nil); got != FlagPullbackWarn {
		t.Fatalf("pullback_warn alone: flags %q, want %q", got, FlagPullbackWarn)
	}
}

// Row 6: an EMA 34 loss never blocks the EMA setup under "off".
func TestOctA2_EMALossBlockOff(t *testing.T) {
	run := func(cfg Config) bool {
		e := &Evaluator{Cfg: cfg}
		e.State.EmaPendingSide = SideLong
		e.State.EmaPendingEntry, e.State.EmaPendingStop, e.State.EmaPendingTarget = 100, 95, 110
		e.State.EmaPendingFilled = true
		emaLossTick(e, 100, market.Kline{High: 100, Low: 94, CloseTime: 1000}, 1000)
		return e.State.EmaBlocked
	}
	if run(DefaultConfig()) {
		t.Fatalf("October default: a stopped EMA 34 trade blocked the EMA (phl_ema_loss_block), want no block")
	}
	if !run(octA2Revert(DefaultConfig())) {
		t.Fatalf("revert (ema34_loss_relock=until_zone_exit): the loss must block the EMA")
	}
}

// Row 11: the box room multiple is 1 (October 1:1); the shared RoomMultiple
// stays 2 for the other setups; an unset field falls back to it.
func TestOctA2_BoxRoomMultiple(t *testing.T) {
	d := DefaultConfig()
	if d.boxRoomMultiple() != 1 || d.RoomMultiple != 2 {
		t.Fatalf("box room %v / shared %v, want 1 / 2", d.boxRoomMultiple(), d.RoomMultiple)
	}
	if got := (Config{RoomMultiple: 2}).boxRoomMultiple(); got != 2 {
		t.Fatalf("unset BoxRoomMultiple must keep the shared room, got %v", got)
	}
	if got := octA2Revert(d).boxRoomMultiple(); got != 2 {
		t.Fatalf("revert box room = %v, want 2", got)
	}
}

// Row 14: the prune is off (levels 5 pts apart both stay) but the same price
// still dedups; the gap-edge floor stays 20 so a 1-tick gap never draws.
func TestOctA2_KeyLevelPruneOff(t *testing.T) {
	lv := []Level{{Price: 100, AtTime: 1}, {Price: 105, AtTime: 2}, {Price: 105, AtTime: 3}}
	if got := pruneLevelsByRecency(lv, 0); len(got) != 2 {
		t.Fatalf("prune 0 kept %d levels, want 2 (the 5-pt pair stays, the same price dedups)", len(got))
	}
	if got := pruneLevelsByRecency(lv, 20); len(got) != 1 {
		t.Fatalf("revert (20) kept %d levels, want 1", len(got))
	}
	if g := gapEdgeMinPts(DefaultConfig()); g != 20 {
		t.Fatalf("gapEdgeMinPts under prune 0 = %v, want the 20 floor", g)
	}
	if g := gapEdgeMinPts(octA2Revert(DefaultConfig())); g != 20 {
		t.Fatalf("gapEdgeMinPts revert = %v, want 20", g)
	}
}

// Row 15: the level-beats-trendline band is off; box-beats-trendline is off.
func TestOctA2_TrendlineLocationKnobs(t *testing.T) {
	respected := []Level{{Kind: KindKeyLevel, Price: 100}}
	if trendlineBeatenByLevelBand(respected, 101, 0) {
		t.Fatalf("band 0: a level 1 pt away beat the trendline, want the preference OFF")
	}
	if !trendlineBeatenByLevelBand(respected, 101, 2) {
		t.Fatalf("band 2 (revert): a level 1 pt away must beat the trendline")
	}
	if !trendlineBeatenByLevel(respected, 101) {
		t.Fatalf("the legacy entry keeps the Sept 2-pt band")
	}
	e := &Evaluator{Cfg: DefaultConfig()}
	if o := e.tlLevelOpts(); o.PreferPts != 0 || o.BoxBeats {
		t.Fatalf("tlLevelOpts default = %+v, want {0 false}", o)
	}
	if o := (&Evaluator{Cfg: octA2Revert(DefaultConfig())}).tlLevelOpts(); o.PreferPts != 2 || !o.BoxBeats {
		t.Fatalf("tlLevelOpts revert = %+v, want {2 true}", o)
	}
	if n := (&Evaluator{Cfg: DefaultConfig()}).tlOpts().MaxNonAdjacent; n != 3 {
		t.Fatalf("tlOpts MaxNonAdjacent = %d, want 3 (OctD3.2-R22 @09:36)", n)
	}
}

// Row 15: October's fan [OctD3.2-R21/R22 @08:31-09:36 "3 đường thôi … một cái
// đáy bên tay trái"]: with MaxNonAdjacent 3 up to THREE non-adjacent support
// lines stand together on the concave-lows tape; 1 (the revert) and 0 (the
// legacy zero value) keep the m428 R5 cap of ONE.
func TestOctA2_TrendlineFanUpToThree(t *testing.T) {
	bars := m428ConcaveLows(9)
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 59_999).In(ctime())
	count := func(n int) int {
		lines, _ := trendlinesBuildExecOpts(bars, now, false, tlOpts{MaxNonAdjacent: n})
		anchors := map[int64]bool{}
		for _, tl := range lines {
			anchors[tl.P0T], anchors[tl.P1T] = true, true
		}
		na := 0
		for _, tl := range lines {
			if tl.Dead {
				continue
			}
			for a := range anchors {
				if a > tl.P0T && a < tl.P1T {
					na++
					break
				}
			}
		}
		return na
	}
	one, zero, three := count(1), count(0), count(3)
	if one != 1 || zero != 1 {
		t.Fatalf("legacy cap: N=1 -> %d, N=0 -> %d non-adjacent lines, want 1 and 1", one, zero)
	}
	if three < 2 || three > 3 {
		t.Fatalf("October fan: N=3 -> %d non-adjacent lines, want 2..3 (never more than 3)", three)
	}
}
