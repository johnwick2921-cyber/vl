package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// The 4h EMA 34 feeds ONLY the swing line (State.Swing.Line); intraday setups
// read the 1m EMA 34 and the 1H level set. A store with 94 of the 102 closed
// 4h candles therefore blocks SWING4H entries only. Every other missing source
// still blocks ALL entries. Cancels always pass.

// fullDepth meets every floor.
var fullDepth = seedDepth{fourH: FourHEMA34Warmup, oneM: 300, oneH: 20, today: 1, m15: 10}

// seededWith marks e seeded with the given depth, as Seed would leave it.
func seededWith(e *Evaluator, d seedDepth, now int64) {
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
	e.depth = d
	e.depth4hBucket = fourHBucketStart(now, ctime())
	e.missing = missingFromDepth(d)
}

// scopeISBTick runs the standard long ISB through Tick on an evaluator seeded
// with depth d and returns the intents and the evaluator.
func scopeISBTick(d seedDepth) ([]Intent, *Evaluator) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	e := newISBEval(cfg)
	bars := isbFixture()
	now := bars[len(bars)-1].CloseTime + 1
	seededWith(e, d, now)
	return e.Tick(bars, now), e
}

// scopeSwingTick runs a warm swing reject (§8) through Tick on an evaluator
// seeded with depth d. mutate runs after seeding, before the tick.
func scopeSwingTick(t *testing.T, d seedDepth, mutate func(e *Evaluator, now int64)) ([]Intent, *Evaluator) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // item 25: this fixture pins SEED scoping, not the swing room rule
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // touches the line, closes back below
	}
	bars := swingTape(t, cur)
	now := cur[1].OpenTime + 5*60_000 // the touching 5m candle has CLOSED (Tick drops a forming tail)
	e := New(cfg)
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	seededWith(e, d, now)
	if mutate != nil {
		mutate(e, now)
	}
	return e.Tick(bars, now), e
}

func entriesOf(ins []Intent, setup string) int {
	n := 0
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == setup {
			n++
		}
	}
	return n
}

// (1) 94/102 4h + everything else met → an intraday ISB is EMITTED and a
// SWING4H entry is refused "4h EMA34 warm-up".
func TestSeedScopeShort4hBlocksOnlyTheSwing(t *testing.T) {
	short4h := fullDepth
	short4h.fourH = 94

	ins, e := scopeISBTick(short4h)
	if entriesOf(ins, "ISB") != 1 {
		t.Fatalf("94/102 4h: the intraday ISB must be emitted; intents %+v, refusals %v", ins, e.State.Refusals)
	}
	if e.State.Refusals["seed_missing_source"] != 0 {
		t.Fatalf("a short 4h EMA must not refuse intraday entries: %v", e.State.Refusals)
	}

	// control: with every floor met the same tape emits exactly one SWING4H entry.
	ctl, _ := scopeSwingTick(t, fullDepth, nil)
	if entriesOf(ctl, "SWING4H") != 1 {
		t.Fatalf("control (full depth): want exactly one SWING4H entry, got %+v", ctl)
	}
	sw, e2 := scopeSwingTick(t, short4h, nil)
	if n := entriesOf(sw, "SWING4H"); n != 0 {
		t.Fatalf("94/102 4h: the SWING4H entry must be refused, got %d: %+v", n, sw)
	}
	if e2.State.Refusals["seed_missing_4h_ema_warmup"] == 0 {
		t.Fatalf("the swing refusal must be counted seed_missing_4h_ema_warmup: %v", e2.State.Refusals)
	}
}

// (2) a short 1m EMA 34 (or any non-4h source) refuses ALL entries.
func TestSeedScopeShort1mEMARefusesEverything(t *testing.T) {
	short1m := fullDepth
	short1m.oneM = 50
	ins, e := scopeISBTick(short1m)
	if n := entriesOf(ins, "ISB"); n != 0 {
		t.Fatalf("1m EMA34 short: the intraday ISB must be refused, got %d: %+v", n, ins)
	}
	if e.State.Refusals["seed_missing_source"] == 0 {
		t.Fatalf("the refusal must be counted seed_missing_source: %v", e.State.Refusals)
	}
	sw, _ := scopeSwingTick(t, short1m, nil)
	if n := entriesOf(sw, "SWING4H"); n != 0 {
		t.Fatalf("1m EMA34 short: the swing is refused too, got %d", n)
	}
	// 4h short AND 1m short: still everything refused.
	both := short1m
	both.fourH = 94
	if ins, _ := scopeISBTick(both); entriesOf(ins, "ISB") != 0 {
		t.Fatalf("4h and 1m short: the intraday ISB must be refused: %+v", ins)
	}
	// the 1H level history is short too → everything refused through Tick.
	if ins, _ := scopeISBTick(seedDepth{fourH: 102, oneM: 300, oneH: 1, today: 1, m15: 10}); entriesOf(ins, "ISB") != 0 {
		t.Fatalf("1H levels short: the intraday ISB must be refused: %+v", ins)
	}
}

// The scope: ONLY the 4h EMA blocks just the swing; every other source blocks
// everything (today's session and the closed 15m are re-observed from the live
// bars, so their scope is pinned here on the classifier).
func TestSeedMissingScope(t *testing.T) {
	cases := []struct {
		name       string
		d          seedDepth
		all, swing bool
	}{
		{"nothing missing", fullDepth, false, false},
		{"4h only", seedDepth{fourH: 94, oneM: 300, oneH: 20, today: 1, m15: 10}, false, true},
		{"1m EMA", seedDepth{fourH: 102, oneM: 50, oneH: 20, today: 1, m15: 10}, true, false},
		{"1H levels", seedDepth{fourH: 102, oneM: 300, oneH: 1, today: 1, m15: 10}, true, false},
		{"today's session", seedDepth{fourH: 102, oneM: 300, oneH: 20, today: 0, m15: 10}, true, false},
		{"closed 15m", seedDepth{fourH: 102, oneM: 300, oneH: 20, today: 1, m15: 0}, true, false},
		{"4h and 1m", seedDepth{fourH: 94, oneM: 50, oneH: 20, today: 1, m15: 10}, true, true},
	}
	for _, c := range cases {
		all, swing := missingScope(missingFromDepth(c.d))
		if all != c.all || swing != c.swing {
			t.Fatalf("%s: scope = all:%v swing:%v, want all:%v swing:%v (missing %v)", c.name, all, swing, c.all, c.swing, missingFromDepth(c.d))
		}
	}
}

// Cancels always pass, whatever is missing.
func TestSeedScopedFilterKeepsCancels(t *testing.T) {
	ins := []Intent{
		{Action: PlaceStopEntry, Setup: "SWING4H"},
		{Action: PlaceStopLimitEntry, Setup: "ISB"},
		{Action: CancelArm, ArmID: "a"},
	}
	m4h := []string{missing4hPrefix + " warm-up (94/102 4h candles)"}
	kept, ref := scopedFailClosedFilter(append([]Intent(nil), ins...), m4h)
	if len(kept) != 2 || kept[0].Setup != "ISB" || kept[1].Action != CancelArm || len(ref) != 1 || ref[0] != "seed_missing_4h_ema_warmup" {
		t.Fatalf("4h-only: kept %+v refusals %v", kept, ref)
	}
	kept, ref = scopedFailClosedFilter(append([]Intent(nil), ins...), []string{"bar history depth: 1m EMA34 warm-up (50/102 1m bars)"})
	if len(kept) != 1 || kept[0].Action != CancelArm || len(ref) != 2 {
		t.Fatalf("1m short: kept %+v refusals %v", kept, ref)
	}
}

// (3) the re-check: the 102nd closed 4h candle arrives → the swing is allowed
// in the SAME Tick, no restart.
func TestSeedScopeRecheckAllowsTheSwingAt102(t *testing.T) {
	d := fullDepth
	d.fourH = FourHEMA34Warmup - 1
	sw, e := scopeSwingTick(t, d, func(e *Evaluator, now int64) {
		e.depth4hBucket = fourHBucketStart(now, ctime()) - 4*3600_000 // one rollover observed on this tick
	})
	if n := entriesOf(sw, "SWING4H"); n != 1 {
		t.Fatalf("102nd candle reached: the swing must be allowed, got %d: %+v (refusals %v, missing %v)", n, sw, e.State.Refusals, e.SourcesMissing())
	}
	if e.depth.fourH != FourHEMA34Warmup || len(e.SourcesMissing()) != 0 {
		t.Fatalf("depth %d, missing %v — want exactly the %d warm-up and nothing missing", e.depth.fourH, e.SourcesMissing(), FourHEMA34Warmup)
	}
	// 101 is still refused (never lowered).
	d2 := fullDepth
	d2.fourH = FourHEMA34Warmup - 2
	sw2, _ := scopeSwingTick(t, d2, func(e *Evaluator, now int64) {
		e.depth4hBucket = fourHBucketStart(now, ctime()) - 4*3600_000
	})
	if entriesOf(sw2, "SWING4H") != 0 {
		t.Fatalf("101 closed 4h candles must still refuse the swing: %+v", sw2)
	}
}
