package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// Release #52 W11 — the "invented-constant" knobs (B8, B10, B11, B27, C15, C16).
// Every number below is OURS, not the course's; each knob's DEFAULT equals the
// value the kernel hard-coded before, so these pins prove two things at the
// production call sites: (1) the default changes nothing (the pre-existing
// suites still pass untouched), (2) a non-default value reaches the decision.

// ── B8: 2m ISB stacking cancel candle + tiny-stop exception ─────────────────

// w11StackRun replays TestExec2mOnISBStackingCounts2mCandles's fixture under
// cfg and returns, per 1m step 4..N, whether the 2m arm was cancelled.
func w11StackRun(t *testing.T, cfg Config, lastStep int) (cancelAt int, insideAfter int) {
	t.Helper()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = true
	e := New(cfg)
	seed := []market.Kline{
		rthBars(0, 121, 121.5, 120, 120.5),
		rthBars(1, 121, 121.5, 120, 121),
		rthBars(2, 99, 106, 98.5, 106),
		rthBars(3, 101, 102, 100, 101.5),
	}
	now := seed[len(seed)-1].CloseTime + 1
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.ISBArms = map[string]ISBArm{
		"arm-2m": {FirstBar: market.Kline{High: 106, Low: 98.5}, Inside: 0, Side: SideLong, TFMin: 2, LastTick: seed[3].CloseTime},
	}
	bars := append([]market.Kline(nil), seed...)
	for i := 4; i <= lastStep; i++ {
		bars = append(bars, rthBars(i, 102, 102.5, 101.5, 102))
		now = bars[len(bars)-1].CloseTime + 1
		for _, in := range e.Tick(bars, now) {
			if in.ArmID == "arm-2m" && in.Action == CancelArm && cancelAt == 0 {
				cancelAt = i
			}
		}
	}
	return cancelAt, e.State.ISBArms["arm-2m"].Inside
}

// TestW11B8Stack2mCancelCandleKnob — Tick call site: the default (4) cancels at
// the 4th stacked 2m candle (09:09, step 9) exactly as before; ISBStackCancelCandle2m
// = 6 holds through candle 5 and cancels at the 6th (09:13, step 13). MUTANT: the
// evaluator reading the constant 4 instead of cfg.isbStackCancelCandle(arm.TFMin)
// cancels at step 9 for the knob row → RED.
func TestW11B8Stack2mCancelCandleKnob(t *testing.T) {
	def := DefaultConfig()
	if at, _ := w11StackRun(t, def, 13); at != 9 {
		t.Fatalf("default 2m cancel candle must cancel at 09:09 (step 9), got step %d", at)
	}
	knob := DefaultConfig()
	knob.ISBStackCancelCandle2m = 6
	if at, _ := w11StackRun(t, knob, 9); at != 0 {
		t.Fatalf("cancel candle 6 must still hold at 09:09, cancelled at step %d", at)
	}
	if at, _ := w11StackRun(t, knob, 13); at != 13 {
		t.Fatalf("cancel candle 6 must cancel at 09:13 (step 13), got step %d", at)
	}
}

// TestW11B8CancelCandleKnobIsTwoMinuteOnly — the course 4 stays on the 1m and 5m.
func TestW11B8CancelCandleKnobIsTwoMinuteOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBStackCancelCandle2m = 8
	inside := market.Kline{Open: 101, High: 102, Low: 100, Close: 101.5}
	for _, tf := range []int{0, 1, 5} {
		arm := &ISBArm{FirstBar: market.Kline{High: 106, Low: 98.5}, Side: SideLong, TFMin: tf}
		var got []Intent
		for i := 0; i < 3; i++ {
			got = ISBStackTick(arm, inside, cfg)
		}
		if len(got) != 1 || got[0].Action != CancelArm {
			t.Fatalf("TFMin %d arm must still cancel on candle 4 under the 2m knob, got %v", tf, got)
		}
	}
	// A knob under 3 is not a stack: falls back to the course 4.
	cfg.ISBStackCancelCandle2m = 1
	if got := cfg.isbStackCancelCandle(2); got != 4 {
		t.Fatalf("a 2m cancel candle under 3 must fall back to 4, got %d", got)
	}
}

// TestW11B8TinyStopKeepsTheArmPastTheCancelCandle — the tiny-stop exception
// [Ngày 5.1 part2 @02:50–03:40] at the Tick call site. The arm's ISB candle is
// 7.5 pts wide + 2 x 1.5 buffer = a 10.5-pt stop. Knob OFF (default): cancelled
// at candle 4. Knob 12: kept (the stop is under 12). Knob 10: cancelled (10.5 > 10).
// MUTANT: drop the tiny-stop early return in ISBStackTick → the 12 row cancels → RED.
func TestW11B8TinyStopKeepsTheArmPastTheCancelCandle(t *testing.T) {
	off := DefaultConfig()
	if at, _ := w11StackRun(t, off, 9); at != 9 {
		t.Fatalf("tiny-stop OFF must cancel at step 9, got %d", at)
	}
	tiny := DefaultConfig()
	tiny.ISBStackTinyStopPts = 12
	at, inside := w11StackRun(t, tiny, 13)
	if at != 0 {
		t.Fatalf("a 10.5-pt stop under the 12-pt tiny-stop knob must not cancel, cancelled at step %d", at)
	}
	if inside < 4 {
		t.Fatalf("the kept arm must keep counting inside candles, got inside=%d", inside)
	}
	small := DefaultConfig()
	small.ISBStackTinyStopPts = 10
	if at, _ := w11StackRun(t, small, 9); at != 9 {
		t.Fatalf("a 10.5-pt stop above a 10-pt tiny-stop knob must still cancel, got step %d", at)
	}
}

// TestW11B8TinyStopStillCancelsWhenABodyLeavesI1 — the exception only lifts the
// candle-count cancel; a body outside I1 cancels at once.
func TestW11B8TinyStopStillCancelsWhenABodyLeavesI1(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBStackTinyStopPts = 12
	arm := &ISBArm{FirstBar: market.Kline{High: 106, Low: 98.5}, Side: SideLong, TFMin: 1}
	out := market.Kline{Open: 107, High: 108, Low: 106.5, Close: 107.5}
	got := ISBStackTick(arm, out, cfg)
	if len(got) != 1 || got[0].Action != CancelArm || arm.Inside != -1 {
		t.Fatalf("a body outside I1 must cancel at once even with a tiny stop, got %v inside=%d", got, arm.Inside)
	}
}

// ── B10: evening "slow" cutoff / start hour / window ────────────────────────

func w11EveningBars(now int64) []market.Kline {
	ct := func(h, m int) int64 { return auditMs(2026, 9, 15, h, m, 0) }
	_ = now
	mk := func(open int64, h, l float64) market.Kline {
		return market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: l, High: h, Low: l, Close: h}
	}
	return []market.Kline{
		mk(ct(15, 30), 230, 120), // a 110-pt afternoon swing, 15:30 CT
		mk(ct(18, 0), 121, 120),
		mk(ct(18, 1), 122, 121),
	}
}

// TestW11B10EveningSlowKnobs — eveningSlow reads the three knobs. At 18:30 CT:
// default (100 pts from 15:00) the 110-pt swing makes the night STRONG; a 150-pt
// cutoff makes it slow; starting the range at the 17:00 reopen ignores the 15:30
// swing so it is slow; a 60-minute trailing window ignores it too.
func TestW11B10EveningSlowKnobs(t *testing.T) {
	now := auditMs(2026, 9, 15, 18, 30, 0)
	bars := w11EveningBars(now)
	if eveningSlow(now, bars, DefaultConfig()) {
		t.Fatal("default: a 110-pt range since 15:00 is a strong night")
	}
	c := DefaultConfig()
	c.EveningSlowPts = 150
	if !eveningSlow(now, bars, c) {
		t.Fatal("a 150-pt cutoff must read the 110-pt night as slow")
	}
	c = DefaultConfig()
	c.EveningSlowFromHour = 17
	if !eveningSlow(now, bars, c) {
		t.Fatal("a range starting at 17:00 must ignore the 15:30 swing (slow)")
	}
	c = DefaultConfig()
	c.EveningSlowWindowMin = 60
	if !eveningSlow(now, bars, c) {
		t.Fatal("a 60-minute trailing window must ignore the 15:30 swing (slow)")
	}
	// Overnight 01:30 CT reads yesterday's start hour.
	over := auditMs(2026, 9, 16, 1, 30, 0)
	c = DefaultConfig()
	c.EveningSlowFromHour = 17
	if !eveningSlow(over, bars, c) {
		t.Fatal("overnight with a 17:00 start must read yesterday's 17:00 (slow)")
	}
	if eveningSlow(over, bars, DefaultConfig()) {
		t.Fatal("overnight default must read yesterday's 15:00 (the swing counts: strong)")
	}
	// A hand-built zero Config (no DefaultConfig) must not turn the night off:
	// zero numbers fall back to the 100 / 15:00 defaults.
	if eveningSlow(now, bars, Config{}) {
		t.Fatal("a zero Config must read the 100-pt / 15:00 defaults")
	}
}

// w11EveningISBBars is an evening fixture whose closed 5m pair (19:00, 19:05) is
// a long ISB while the last two closed 2m candles (19:08-09 doji, 19:10-11) are
// not: the read frame decides whether an ISB forms at all.
func w11EveningISBBars() []market.Kline {
	ct := func(h, m int) int64 { return auditMs(2026, 9, 15, h, m, 0) }
	k := func(h, m int, o, hi, lo, c float64) market.Kline {
		op := ct(h, m)
		return market.Kline{OpenTime: op, CloseTime: op + 59_999, Open: o, High: hi, Low: lo, Close: c}
	}
	return []market.Kline{
		k(18, 58, 121, 122, 120, 120.5),
		k(18, 59, 121, 121.5, 120, 121),
		// 5m bucket 19:00-19:04: green, 99 -> 106, range 98.5-106
		k(19, 0, 99, 100, 98.5, 100),
		k(19, 1, 100, 102, 99.5, 102),
		k(19, 2, 102, 104, 101.5, 104),
		k(19, 3, 104, 105, 103.5, 105),
		k(19, 4, 105, 106, 104.5, 106),
		// 5m bucket 19:05-19:09: body 101 -> 101.5 inside the first candle
		k(19, 5, 101, 101.5, 100.9, 101.2),
		k(19, 6, 101.2, 101.8, 101, 101.4),
		k(19, 7, 101.4, 102.1, 101.2, 101.6),
		k(19, 8, 101.5, 101.8, 101.1, 101.5),
		k(19, 9, 101.6, 101.9, 101.2, 101.5),
		// 19:10-19:11 forming 5m bucket / the last closed 2m candle
		k(19, 10, 101.5, 102, 101, 101.8),
		k(19, 11, 101.8, 102.2, 101.4, 102),
	}
}

// TestW11B10EveningSlowKnobReachesTheFrameChoice — Tick call site (eval.go
// `case inEveningWindow(now) && eveningSlow(now, bars, e.Cfg)`): with the default
// 100-pt cutoff this 7.5-pt night is slow, so the ISB is read on the 5m (the
// closed 5m pair is an ISB) and is refused at the pre-open day gate
// (isb_day_not_measured proves an ISB formed); with EveningSlowPts = 5 the same
// night is strong, the read is the 2m (doji pair) and NO ISB forms.
// MUTANT: pass DefaultConfig() instead of e.Cfg at the call site → the knob row
// still reads slow → RED.
func TestW11B10EveningSlowKnobReachesTheFrameChoice(t *testing.T) {
	bars := w11EveningISBBars()
	now := bars[len(bars)-1].CloseTime + 1 // 19:12 CT
	run := func(pts float64) map[string]int {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.Exec2mAfter30m = true
		if pts > 0 {
			cfg.EveningSlowPts = pts
		}
		e := newISBEval(cfg)
		e.Tick(bars, now)
		return e.State.Refusals
	}
	slow := run(0)
	if slow["isb_day_not_measured"] == 0 {
		t.Fatalf("default cutoff: the slow night must read the 5m ISB (isb_day_not_measured); refusals %v", slow)
	}
	strong := run(5)
	if strong["isb_day_not_measured"] != 0 {
		t.Fatalf("a 5-pt cutoff makes the night strong: the 2m read finds no ISB; refusals %v", strong)
	}
}

// ── B27: ISB size-cut definitions ───────────────────────────────────────────

// TestW11B27OldExtremeBandKnob — the band reaches isbFlagsFor through Tick's
// production resolver: a candle 4 pts from an old extreme is NOT "at" it at the
// default ±2 and IS at ±5.
func TestW11B27OldExtremeBandKnob(t *testing.T) {
	cur := market.Kline{High: 100, Low: 98}
	lv := []Level{{Kind: KindOldExtreme, Price: 104}}
	if touchesOldExtreme(cur, lv, DefaultConfig().ISBOldExtremePts) {
		t.Fatal("default ±2: an extreme 4 pts above the high is not touched")
	}
	if !touchesOldExtreme(cur, lv, 5) {
		t.Fatal("±5: an extreme 4 pts above the high is touched")
	}
	if touchesOldExtreme(market.Kline{High: 100, Low: 98}, []Level{{Kind: KindOldExtreme, Price: 99}}, 0) == false {
		t.Fatal("band 0: a candle whose range contains the extreme is still an exact touch")
	}
	e := New(DefaultConfig())
	e.Cfg.ISBOldExtremePts = 5
	if got := e.isbFlagsFor(cur, SideLong, lv, nil); !HasFlag(got, FlagISBAtOldExtreme) {
		t.Fatalf("isbFlagsFor must read Cfg.ISBOldExtremePts, flags %q", got)
	}
	e.Cfg.ISBOldExtremePts = 2
	if got := e.isbFlagsFor(cur, SideLong, lv, nil); HasFlag(got, FlagISBAtOldExtreme) {
		t.Fatalf("isbFlagsFor at ±2 must not flag, flags %q", got)
	}
}

// TestW11B27InRangeGapKnob — the in-range gap is its own knob, and 0 inherits the
// ping-pong gap so a stored ping_pong_min_gap_pts keeps reading the same.
func TestW11B27InRangeGapKnob(t *testing.T) {
	levels := []Level{{Kind: KindKeyLevel, Price: 80}, {Kind: KindKeyLevel, Price: 130}} // a 50-pt gap, price 100
	cur := market.Kline{High: 101, Low: 99, Close: 100}
	e := New(DefaultConfig())
	if HasFlag(e.isbFlagsFor(cur, SideLong, levels, nil), FlagISBInRange) {
		t.Fatal("default: a 50-pt key-level gap is not narrower than 50")
	}
	e.Cfg.ISBInRangeGapPts = 60
	if !HasFlag(e.isbFlagsFor(cur, SideLong, levels, nil), FlagISBInRange) {
		t.Fatal("a 60-pt in-range gap must flag a 50-pt key-level gap as in range")
	}
	e.Cfg.ISBInRangeGapPts = 0
	e.Cfg.PingPongMinGapPts = 60 // an owner who already tuned the ping-pong gap
	if !HasFlag(e.isbFlagsFor(cur, SideLong, levels, nil), FlagISBInRange) {
		t.Fatal("0 must inherit PingPongMinGapPts (60) — behaviour for stored values is unchanged")
	}
}

// ── B11: near-box on PHL/PLH ────────────────────────────────────────────────

// w11NearBoxPHLTick drives the TestNearBoxPHLTickRefused fixture under cfg and
// returns the near_box refusal count.
func w11NearBoxPHLTick(t *testing.T, mutate func(*Config)) int {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Exec2mAfter30m = false
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 0
	mutate(&cfg)
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := append(ftghFixture(mk),
		mk(9, 94, 95, 92, 93),
		mk(10, 93, 96, 90, 92),
	)
	now := bars[len(bars)-1].CloseTime + 1
	e := nearBoxEval(cfg, now)
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 90},
		{Key: "old-high:106", Kind: KindOldExtreme, Role: OldHigh, Price: 106},
	}
	e.State.Trigger = TriggerLine{}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 80}}
	e.Tick(bars, now)
	return e.State.Refusals["near_box"]
}

// TestW11B11NearBoxPHLScopeKnob — the PHL/PLH leg of the near-box skip is OUR
// extension of the course's ISB example. Default ON refuses (unchanged, same as
// TestNearBoxPHLTickRefused); NearBoxRoomPHL=false lets the same PHL past the
// near-box check; NearBoxRoomMultiple=0 turns the skip off for every setup.
// MUTANT: drop `e.Cfg.NearBoxRoomPHL &&` at the PHL call site → the OFF row
// still counts near_box → RED.
func TestW11B11NearBoxPHLScopeKnob(t *testing.T) {
	if got := w11NearBoxPHLTick(t, func(*Config) {}); got != 1 {
		t.Fatalf("default: near_box = %d, want 1", got)
	}
	if got := w11NearBoxPHLTick(t, func(c *Config) { c.NearBoxRoomPHL = false }); got != 0 {
		t.Fatalf("NearBoxRoomPHL=false: near_box = %d, want 0", got)
	}
	if got := w11NearBoxPHLTick(t, func(c *Config) { c.NearBoxRoomMultiple = 0 }); got != 0 {
		t.Fatalf("NearBoxRoomMultiple=0: near_box = %d, want 0", got)
	}
}

// TestW11B11ISBNearBoxIgnoresThePHLScope — switching the PHL scope off must NOT
// touch the ISB skip (the course's own example).
func TestW11B11ISBNearBoxIgnoresThePHLScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exec2mAfter30m = false
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 0
	cfg.NearBoxRoomPHL = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := append(ftghFixture(mk),
		mk(9, 90, 93, 89, 92),
		mk(10, 91, 92.5, 89.5, 91.5),
	)
	now := bars[len(bars)-1].CloseTime + 1
	e := nearBoxEval(cfg, now)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 80}}
	e.Tick(bars, now)
	if got := e.State.Refusals["near_box"]; got != 1 {
		t.Fatalf("the ISB near-box skip must survive NearBoxRoomPHL=false, near_box = %d", got)
	}
}

// ── C16: PHL/PLH target shy ─────────────────────────────────────────────────

// TestW11C16PHLTargetShyKnob — phlTarget reads Config.PHLTargetShyPts (the 5 is
// ours, "gần đỉnh cũ" gives no distance): 5 short of the old extreme by default,
// AT the extreme at 0, 12 short at 12; mirrored for a short.
func TestW11C16PHLTargetShyKnob(t *testing.T) {
	ex := Level{Kind: KindOldExtreme, Price: 29_431.75}
	for _, c := range []struct {
		shy       float64
		long, srt float64
	}{
		{DefaultConfig().PHLTargetShyPts, 29_426.75, 29_436.75},
		{0, 29_431.75, 29_431.75},
		{12, 29_419.75, 29_443.75},
	} {
		cfg := DefaultConfig()
		cfg.PHLTargetShyPts = c.shy
		if got := phlTarget(ex, 29_400, SideLong, nil, cfg); got != c.long {
			t.Fatalf("shy %g long target = %.2f, want %.2f", c.shy, got, c.long)
		}
		if got := phlTarget(ex, 29_460, SideShort, nil, cfg); got != c.srt {
			t.Fatalf("shy %g short target = %.2f, want %.2f", c.shy, got, c.srt)
		}
	}
}

// ── C15 hygiene: the box-return walk reads the configured box frame ─────────

// TestW11C15BoxReturnWalkReadsTheConfiguredBoxFrame — Tick used a fresh
// DefaultBoxCfg() for the box-return walk while latchBoxes read e.Cfg.Box; both
// now read baseBoxCfg(). Fixture: the FTGH [104,106], a bar closing below it
// (outside), then a bar whose high stops 3 pts short of the top. At the default
// TouchBandPts (0.25) that is no return; with Cfg.Box.TouchBandPts = 4 it IS a
// return and BoxRefs records it — proving the walk follows the configured frame.
// With the default Config nothing changes. MUTANT: restore DefaultBoxCfg() at
// the walk → the band-4 row records no return → RED.
func TestW11C15BoxReturnWalkReadsTheConfiguredBoxFrame(t *testing.T) {
	if New(DefaultConfig()).baseBoxCfg() != DefaultBoxCfg() {
		t.Fatal("the default Config.Box must equal DefaultBoxCfg() (no live behaviour change)")
	}
	run := func(band float64) int {
		cfg := DefaultConfig()
		cfg.Exec2mAfter30m = false
		cfg.Enabled = true
		cfg.EMALocationTFMinutes = 0
		if band > 0 {
			cfg.Box.TouchBandPts = band
		}
		t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
		mk := func(i int, o, h, l, c float64) market.Kline {
			return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
		}
		bars := append(ftghFixture(mk),
			mk(9, 101, 101.5, 99, 100),  // closes BELOW the box bottom 104: outside
			mk(10, 100, 103, 99.5, 101), // high 103: 3 pts short of the top 106, 1 under the bottom 104
		)
		now := bars[len(bars)-1].CloseTime + 1
		e := nearBoxEval(cfg, now)
		e.State.Trigger = TriggerLine{}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 80}}
		e.Tick(bars, now)
		// The fixture's own bar 8 already touches the bottom edge (a return the
		// walk records); the pin is whether bar 10 (the 3-pt-short bar) is
		// recorded as a later return.
		for _, ms := range e.State.BoxRefs {
			if ms == bars[10].OpenTime {
				return 1
			}
		}
		return 0
	}
	if got := run(0); got != 0 {
		t.Fatalf("default band: a bar 1 pt under the box is no return, but BoxRefs recorded bar 10")
	}
	if got := run(4); got == 0 {
		t.Fatal("band 4 configured on Cfg.Box: the walk must read it and record bar 10 as a return")
	}
}
