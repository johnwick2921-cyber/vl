package mentor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ct returns CT epoch millis for the given day offset and hour:minute.
func ctMs(t *testing.T, dayOffset, hour, minute int) int64 {
	t.Helper()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, ctime())
	ms := base.AddDate(0, 0, dayOffset).Add(time.Duration(hour)*time.Hour +
		time.Duration(minute)*time.Minute).UnixMilli()
	return ms
}

func mkBar(open, close float64, openMs int64, tfMin int) market.Kline {
	return market.Kline{
		Open:      open,
		Close:     close,
		High:      max(open, close),
		Low:       min(open, close),
		OpenTime:  openMs,
		CloseTime: openMs + int64(tfMin)*60_000 - 1,
	}
}

// TestSeedMissingColdStart: an empty store names every source, fail-closed.
func TestSeedMissingColdStart(t *testing.T) {
	now := ctMs(t, 0, 10, 0)
	got := SeedMissing(nil, now)
	if len(got) != 5 {
		t.Fatalf("want 5 missing sources, got %d: %v", len(got), got)
	}
	for _, s := range got {
		if !strings.HasPrefix(s, "bar history depth: ") {
			t.Fatalf("source %q does not carry the depth prefix", s)
		}
	}

	e := New(DefaultConfig())
	if m := Seed(e, nil, now); len(m) != 5 {
		t.Fatalf("Seed returned %d missing, want 5", len(m))
	}
	if len(e.SourcesMissing()) != 5 {
		t.Fatalf("SourcesMissing() = %v, want 5 entries", e.SourcesMissing())
	}
}

// TestSeedKeyLevelsGolden: SeedLevels == a full key-level walk over the same
// stored 1h candles (the rebuild golden).
func TestSeedKeyLevelsGolden(t *testing.T) {
	// 8 days × 7 RTH hours (08:00..14:00 CT) of 1m bars, one colour per hour —
	// Seed builds the 1h walk from the 1m aggregation (P1: 1m only).
	var bars1m []market.Kline
	for d := 0; d < 8; d++ {
		for h := 8; h < 15; h++ {
			open := 1000.0 + float64(d*10+h)
			cl := open + 2
			if (d+h)%2 == 1 {
				cl = open - 2
			}
			for m := 0; m < 60; m++ {
				bars1m = append(bars1m, mkBar(open, cl, ctMs(t, d, h, m), 1))
			}
		}
	}
	now := ctMs(t, 8, 9, 0)

	e := New(DefaultConfig())
	Seed(e, bars1m, now)

	want := keyLevelsFromCandles(keyLevel1HBars(bars1m), e.Cfg.KeyLevelPrunePts)
	if len(e.State.SeedLevels) != len(want) {
		t.Fatalf("SeedLevels len %d, rebuild %d", len(e.State.SeedLevels), len(want))
	}
	for i := range want {
		if e.State.SeedLevels[i].Price != want[i].Price ||
			e.State.SeedLevels[i].Kind != want[i].Kind {
			t.Fatalf("level %d: seed %+v, rebuild %+v", i, e.State.SeedLevels[i], want[i])
		}
	}
}

// TestSeedIncrementalEMA: seed over the first half of the tape, then advance —
// the incremental EMA must equal a full rebuild over the whole tape.
func TestSeedIncrementalEMA(t *testing.T) {
	cfg := DefaultConfig()
	var bars []market.Kline
	cl := 100.0
	for i := 0; i < 100; i++ {
		cl += 0.5
		if i%3 == 1 {
			cl -= 1.0
		}
		bars = append(bars, mkBar(cl-0.25, cl, ctMs(t, 0, 8, 30)+int64(i)*60_000, 1))
	}
	nowMid := bars[49].CloseTime
	e := New(cfg)
	Seed(e, bars[:50], nowMid)
	if e.State.EMA34 == 0 || e.State.EMA9 == 0 {
		t.Fatal("seed did not set the 1m EMAs")
	}

	// The full tape closes bar by bar — the incremental path must track.
	for i := 50; i < 100; i++ {
		e.seededLevels(bars[:i+1], bars[i].CloseTime)
	}
	want34 := emaValue(bars, cfg.EMAPeriod34)
	want9 := emaValue(bars, cfg.EMAPeriod9)
	if abs(e.State.EMA34-want34) > 1e-9 {
		t.Fatalf("EMA34 incremental %.6f, rebuild %.6f", e.State.EMA34, want34)
	}
	if abs(e.State.EMA9-want9) > 1e-9 {
		t.Fatalf("EMA9 incremental %.6f, rebuild %.6f", e.State.EMA9, want9)
	}
}

// TestSeedFailClosed: with a missing source the sweep drops entries, keeps
// cancels; without one nothing is dropped.
func TestSeedFailClosed(t *testing.T) {
	ins := []Intent{
		{Action: PlaceStopEntry, Side: SideLong, Price: 100},
		{Action: PlaceStopLimitEntry, Side: SideShort, Price: 101},
		{Action: CancelArm, ArmID: "a"},
		{Action: ExtendArm, ArmID: "a", ExpiryMs: 123},
	}
	got, _ := failClosedFilter(ins)
	if len(got) != 2 || got[0].Action != CancelArm || got[1].Action != ExtendArm {
		t.Fatalf("failClosedFilter kept %v", got)
	}
}

// TestSeedSwingLineWarm: 9 days of 1h closes → the swing line is seeded as the
// EMA recurrence over all closed 4h candles, bucket start = the current one.
func TestSeedSwingLineWarm(t *testing.T) {
	cfg := DefaultConfig()
	// 19 days of around-the-clock 1m bars: Seed builds the 1h aggregation and
	// the 4h buckets (17:00 CT anchor) from it (P1: 1m only).
	bars1m := minuteTape(t, 19)
	now := ctMs(t, 18, 20, 0) // day 18, 20:00 CT — inside the 17:00–21:00 bucket
	closed := fourHClosedBuckets(barsTF(bars1m, 60), now)
	if len(closed) < FourHEMA34Min {
		t.Fatalf("fixture too short: %d closed 4h candles", len(closed))
	}

	e := New(cfg)
	if m := Seed(e, bars1m, now); len(m) != 0 {
		t.Fatalf("unexpected missing: %v", m)
	}
	var closes []float64
	for _, b := range closed {
		closes = append(closes, b.Close)
	}
	want := emaFromCloses(closes, cfg.Swing.EMAPeriod)
	if abs(e.State.Swing.Line-want) > 1e-9 {
		t.Fatalf("seeded swing line %.6f, want %.6f", e.State.Swing.Line, want)
	}
	if e.State.Swing.BucketStart != fourHBucketStart(now, ctime()) {
		t.Fatalf("bucket start %d, want current bucket %d",
			e.State.Swing.BucketStart, fourHBucketStart(now, ctime()))
	}
	if e.State.Swing.EmaCount != len(closed) {
		t.Fatalf("EmaCount %d, want %d", e.State.Swing.EmaCount, len(closed))
	}
}

// TestSeedSwingFlipIncremental: a 4h flip advances the warm line by the SAME
// EMA recurrence (not a cold recompute) — parity with a full rebuild.
func TestSeedSwingFlipIncremental(t *testing.T) {
	cfg := DefaultConfig()
	// 9 days of around-the-clock 1m bars (P1: 1m only — the 4h buckets are
	// aggregated with the 17:00 CT anchor).
	bars1m := minuteTape(t, 9)
	now := ctMs(t, 8, 20, 0)
	e := New(cfg)
	Seed(e, bars1m, now)
	line0 := e.State.Swing.Line
	count0 := e.State.Swing.EmaCount

	// New closed 4h bucket: day 8 21:00–day 9 01:00 (the 17:00 anchor).
	// 5m bars inside the OLD bucket (so bucketClose finds the close) + one
	// inside the new bucket to force the flip.
	oldStart := e.State.Swing.BucketStart
	oldClose := 0.0
	var bars5m []market.Kline
	for m := 0; m < 48; m++ {
		t5 := oldStart + int64(m)*5*60_000
		c := 21000 + float64(m)
		if m == 47 {
			oldClose = c
		}
		bars5m = append(bars5m, mkBar(c-0.25, c, t5, 5))
	}
	newStart := oldStart + 240*60_000
	// fill the NEW bucket too, then close it: the flip fires when the new
	// bucket is CLOSED (swingLine needs ≥2 closed buckets to return a line).
	for m := 0; m < 48; m++ {
		t5 := newStart + int64(m)*5*60_000
		c := 21100 + float64(m)
		bars5m = append(bars5m, mkBar(c-0.25, c, t5, 5))
	}
	now2 := newStart + 240*60_000 // the new bucket has closed

	out := SwingTick(&e.State.Swing, bars5m, cfg.Swing, now2)
	_ = out
	k := 2.0 / float64(cfg.Swing.EMAPeriod+1)
	want := line0 + k*(oldClose-line0)
	if abs(e.State.Swing.Line-want) > 1e-9 {
		t.Fatalf("flip line %.6f, want incremental %.6f", e.State.Swing.Line, want)
	}
	if e.State.Swing.EmaCount != count0+1 {
		t.Fatalf("EmaCount %d, want %d", e.State.Swing.EmaCount, count0+1)
	}
	if e.State.Swing.BucketStart != newStart+240*60_000 {
		t.Fatalf("BucketStart %d, want current bucket %d",
			e.State.Swing.BucketStart, newStart+240*60_000)
	}
}

// TestSeedLineNamesDepths: the one-liner reports per-source depths.
func TestSeedLineNamesDepths(t *testing.T) {
	e := New(DefaultConfig())
	Seed(e, nil, ctMs(t, 0, 9, 0))
	l := e.seedLine
	for _, want := range []string{"4h EMA34 0/102", "1m EMA34 0/102", "1H RTH levels 0 candles", "levels 0"} {
		if !strings.Contains(l, want) {
			t.Fatalf("seed line %q missing %q", l, want)
		}
	}
}

// TestSeedWarmupGate: between the absolute minimum (34) and the stated warm-up
// (102) the source is still REFUSED and named with the warm-up denominator —
// a barely-formed EMA must never trade (CTO 07:00 ruling).
func TestSeedWarmupGate(t *testing.T) {
	short := minuteTape(t, 9) // 55 closed 4h candles: above 34, below 102
	long := minuteTape(t, 19) // 114 closed 4h candles: warm
	now := ctMs(t, 18, 20, 0)

	miss := SeedMissing(short, now)
	want := "bar history depth: 4h EMA34 warm-up (55/102 4h candles)"
	found := false
	for _, s := range miss {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("55 4h candles must be refused as %q, got %v", want, miss)
	}

	if m := SeedMissing(long, now); len(m) != 0 {
		t.Fatalf("114 4h candles must be warm, got %v", m)
	}
}

// minuteTape builds days of around-the-clock 1m bars — the P1 seed input
// (1m only, every higher timeframe aggregated from it).
func minuteTape(t *testing.T, days int) []market.Kline {
	t.Helper()
	var bars []market.Kline
	cl := 20000.0
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			for m := 0; m < 60; m++ {
				cl += 0.05
				if (d+h+m)%5 == 0 {
					cl -= 0.1
				}
				bars = append(bars, mkBar(cl-0.05, cl, ctMs(t, d, h, m), 1))
			}
		}
	}
	return bars
}

func hourTape(t *testing.T, days int) []market.Kline {
	t.Helper()
	var bars []market.Kline
	cl := 20000.0
	day, hour := 0, 0
	for i := 0; i < days*24; i++ {
		cl += 2
		if i%5 == 0 {
			cl -= 3
		}
		bars = append(bars, mkBar(cl-0.5, cl, ctMs(t, day, hour, 0), 60))
		hour++
		if hour == 24 {
			hour, day = 0, day+1
		}
	}
	return bars
}

// TestSeedFailClosedTick: the sweep runs INSIDE Tick — the same ISB tape that
// emits an entry when unseeded must emit NO entry when seeded with a missing
// source (cancels still flow). The mutant that disables the sweep turns this
// RED: the placement comes back.
func TestSeedFailClosedTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // item 25: this fixture pins the seed fail-closed sweep, not the room rule
	head := rthBars(0, 121, 121.5, 120.5, 120)
	mother := rthBars(1, 98, 106, 97, 105)
	c1 := rthBars(2, 103, 104, 99, 100)
	c2 := rthBars(3, 100, 101.5, 99.5, 101)
	c3 := rthBars(4, 100, 101.5, 99.5, 101)
	c4 := rthBars(5, 100, 101.5, 99.5, 101)
	bars := []market.Kline{head, mother, c1, c2, c3, c4}

	count := func(e *Evaluator) (placements, cancels int) {
		for i := 2; i <= len(bars); i++ {
			now := bars[i-1].CloseTime + 1
			for _, in := range e.Tick(bars[:i], now) {
				switch in.Action {
				case PlaceStopLimitEntry, PlaceStopEntry:
					placements++
				case CancelArm:
					cancels++
				}
			}
		}
		return
	}

	setup := func() *Evaluator {
		e := New(cfg)
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
		e.State.ORB = ORB{Day: dayStartCT(rthBars(0, 0, 0, 0, 0).OpenTime), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(rthBars(0, 0, 0, 0, 0).OpenTime).In(ctime())), Verdict: DayTrade}
		return e
	}

	// Control: unseeded, the tape emits the arm (legacy behaviour).
	ctrl := setup()
	p, c := count(ctrl)
	if p != 1 {
		t.Fatalf("control placements = %d, want 1 (tape must emit when unseeded)", p)
	}

	// Seeded with a missing source: the same tape must refuse entries, but
	// the ISB stacking cancel still flows.
	seeded := setup()
	Seed(seeded, nil, bars[len(bars)-1].CloseTime)
	if len(seeded.SourcesMissing()) == 0 {
		t.Fatal("nil bars must leave every source missing")
	}
	p, c = count(seeded)
	if p != 0 {
		t.Fatalf("seeded-missing placements = %d, want 0 (fail-closed)", p)
	}
	if c == 0 {
		t.Fatalf("seeded-missing cancels = 0, want the stacking cancel to flow")
	}
}

// TestSeed1hEquals1mAggregation — P1 (CTO 12:47:11Z): Seed reads 1m ONLY. The
// seeded 1h walk must equal the 1m aggregation, and the 4h buckets must equal
// the all-hours 1h aggregation bucketed on the 17:00 CT anchor.
func TestSeed1hEquals1mAggregation(t *testing.T) {
	bars1m := minuteTape(t, 9)
	now := ctMs(t, 8, 20, 0)

	e := New(DefaultConfig())
	Seed(e, bars1m, now)

	// 1h: the level walk equals keyLevel1HBars over the same 1m tape.
	wantLevels := keyLevelsFromCandles(keyLevel1HBars(bars1m), e.Cfg.KeyLevelPrunePts)
	if len(e.State.SeedLevels) != len(wantLevels) {
		t.Fatalf("seeded levels %d != 1m-aggregated walk %d",
			len(e.State.SeedLevels), len(wantLevels))
	}
	for i := range wantLevels {
		if e.State.SeedLevels[i].Price != wantLevels[i].Price {
			t.Fatalf("level %d: seeded %.2f, 1m-aggregated %.2f",
				i, e.State.SeedLevels[i].Price, wantLevels[i].Price)
		}
	}

	// 4h: the swing's consumed count equals the all-hours 1h aggregation
	// bucketed on the 17:00 CT anchor.
	want4h := fourHClosedBuckets(barsTF(bars1m, 60), now)
	if e.State.Swing.EmaCount != len(want4h) {
		t.Fatalf("seeded 4h count %d != all-hours aggregation %d",
			e.State.Swing.EmaCount, len(want4h))
	}
}

func formingSeedTape(t *testing.T, throughMinute int, priorClose, partialClose, finalClose float64) []market.Kline {
	t.Helper()
	var bars []market.Kline
	for minute := 8*60 + 30; minute <= throughMinute; minute++ {
		hour, min := minute/60, minute%60
		close := priorClose
		if minute >= 9*60+30 {
			close = partialClose
			if minute > 10*60+15 {
				close = finalClose
			}
		}
		bars = append(bars, mkBar(100, close, ctMs(t, 0, hour, min), 1))
	}
	return bars
}

func TestSeedDropsForming1HCandleUntilTickClosesIt(t *testing.T) {
	bars := formingSeedTape(t, 10*60+31, 101, 105, 103)
	seedNow := ctMs(t, 0, 10, 15)
	seedEnd := 10*60 + 15 - (8*60 + 30) + 1
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	Seed(e, bars[:seedEnd], seedNow)

	for _, c := range e.State.Seed1HBars {
		if c.OpenTime == ctMs(t, 0, 9, 30) {
			t.Fatal("Seed retained the still-forming 09:30 candle")
		}
	}

	// The 10:31 minute is present but still forming at this timestamp; the
	// preceding 09:30–10:29 hour is closed and must be appended exactly once.
	e.Tick(bars, ctMs(t, 0, 10, 31))
	var found int
	for _, c := range e.State.Seed1HBars {
		if c.OpenTime != ctMs(t, 0, 9, 30) {
			continue
		}
		found++
		if c.Open != 100 || c.High != 105 || c.Low != 100 || c.Close != 103 ||
			c.CloseTime != ctMs(t, 0, 10, 29)+59_999 {
			t.Fatalf("09:30 candle = %+v, want full 09:30–10:29 OHLC 100/105/100/103", c)
		}
	}
	if found != 1 {
		t.Fatalf("09:30 candle occurrence count = %d, want exactly one; bars=%+v", found, e.State.Seed1HBars)
	}
}

func TestSeedFormingCandleColourFlipUsesClosedColour(t *testing.T) {
	for _, tc := range []struct {
		name         string
		partialClose float64
		finalClose   float64
		wantLevel    bool
	}{
		{name: "remove forming-only flip", partialClose: 99, finalClose: 101, wantLevel: false},
		{name: "add final closed flip", partialClose: 101, finalClose: 99, wantLevel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bars := formingSeedTape(t, 10*60+29, 101, tc.partialClose, tc.finalClose)
			cfg := DefaultConfig()
			cfg.Enabled = true
			e := New(cfg)
			Seed(e, bars[:10*60+15-(8*60+30)+1], ctMs(t, 0, 10, 15))
			e.Tick(bars, ctMs(t, 0, 10, 30))

			var found bool
			for _, level := range e.State.SeedLevels {
				if level.AtTime == ctMs(t, 0, 9, 30) {
					found = true
					if level.Kind != KindKeyLevel || level.Price != 100 {
						t.Fatalf("09:30 level = %+v, want key level at 100", level)
					}
				}
			}
			if found != tc.wantLevel {
				t.Fatalf("09:30 level present = %v, want %v; levels=%+v", found, tc.wantLevel, e.State.SeedLevels)
			}
			if e.State.Seed1HLastColour != (tc.finalClose > 100) {
				t.Fatalf("last seeded colour = %v, want closed candle colour %v", e.State.Seed1HLastColour, tc.finalClose > 100)
			}
		})
	}
}

func TestSeedClosedHistoryAtMidnightIsByteIdentical(t *testing.T) {
	bars := formingSeedTape(t, 14*60+59, 101, 101, 101)
	cfg := DefaultConfig()
	e := New(cfg)
	Seed(e, bars, ctMs(t, 1, 0, 0))

	// This is the pre-filter output for already-closed history. Comparing the
	// serialized tuple keeps the no-forming-candle path byte-for-byte stable.
	candles := keyLevel1HBars(bars)
	want, err := json.Marshal(struct {
		Bars      []market.Kline `json:"bars"`
		Levels    []Level        `json:"levels"`
		Watermark int64          `json:"watermark"`
		Colour    bool           `json:"colour"`
	}{candles, keyLevelsFromCandles(candles, cfg.KeyLevelPrunePts), candles[len(candles)-1].OpenTime, candleColour(candles[len(candles)-1])})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(struct {
		Bars      []market.Kline `json:"bars"`
		Levels    []Level        `json:"levels"`
		Watermark int64          `json:"watermark"`
		Colour    bool           `json:"colour"`
	}{e.State.Seed1HBars, e.State.SeedLevels, e.State.Seed1HWatermark, e.State.Seed1HLastColour})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("closed-history seed changed at midnight:\n got %s\nwant %s", got, want)
	}
}

func TestSeedDropsScheduledFormingCandleWithClosedStoredBars(t *testing.T) {
	seedBars := formingSeedTape(t, 10*60+14, 101, 105, 103)
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	Seed(e, seedBars, ctMs(t, 0, 10, 15)+30_000)

	for _, c := range e.State.Seed1HBars {
		if c.OpenTime == ctMs(t, 0, 9, 30) {
			t.Fatalf("Seed included scheduled-forming 09:30 candle ending at %d", c.CloseTime)
		}
	}

	bars := formingSeedTape(t, 10*60+31, 101, 105, 103)
	for i := len(seedBars); i < len(bars); i++ {
		e.Tick(bars[:i+1], bars[i].OpenTime)
	}
	var found int
	for _, c := range e.State.Seed1HBars {
		if c.OpenTime != ctMs(t, 0, 9, 30) {
			continue
		}
		found++
		if c.Open != 100 || c.High != 105 || c.Low != 100 || c.Close != 103 ||
			c.CloseTime != ctMs(t, 0, 10, 29)+59_999 {
			t.Fatalf("09:30 candle = %+v, want full 09:30–10:29 OHLC 100/105/100/103", c)
		}
	}
	if found != 1 {
		t.Fatalf("09:30 candle occurrence count = %d, want exactly one", found)
	}
}

func TestSeed14h30CandleClosesAt1500(t *testing.T) {
	bars := formingSeedTape(t, 14*60+59, 101, 101, 101)
	e := New(DefaultConfig())
	Seed(e, bars, ctMs(t, 0, 15, 0))

	var found int
	for _, c := range e.State.Seed1HBars {
		if c.OpenTime >= ctMs(t, 0, 15, 0) {
			t.Fatalf("post-close candle entered RTH seed: %+v", c)
		}
		if c.OpenTime == ctMs(t, 0, 14, 30) {
			found++
			if c.CloseTime != ctMs(t, 0, 14, 59)+59_999 {
				t.Fatalf("14:30 candle CloseTime = %d, want last stored minute close %d", c.CloseTime, ctMs(t, 0, 14, 59)+59_999)
			}
		}
	}
	if found != 1 {
		t.Fatalf("14:30 candle count at 15:00 = %d, want one", found)
	}
}

// TestSeedSwingWatermarkBlocksOldTouches (S2 pin, production call site Seed +
// Tick): a boot over a long tape must stamp the swing watermark at the newest
// closed 5m bar, and the first tick must walk NOTHING old — an old touch must
// never become a live order. Mutant: drop the LastBarTime assignment in Seed →
// (b1) turns RED.
func TestSeedSwingWatermarkBlocksOldTouches(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// 19 days of around-the-clock 1m bars: ≥102 closed 4h candles so the swing
	// is allowed (not refused by the warm-up gate), and every touch in the tape
	// is OLD by the time the bot boots at the end of the tape.
	bars1m := minuteTape(t, 19)
	now := ctMs(t, 19, 0, 0) // just past the last bar (day 18 23:59)
	e := New(cfg)
	if m := Seed(e, bars1m, now); len(m) != 0 {
		t.Fatalf("fixture: unexpected missing sources %v", m)
	}
	b5 := closedBuckets(bars1m, now, cfg)
	if len(b5) == 0 {
		t.Fatal("fixture: no closed 5m bars")
	}
	newest := b5[len(b5)-1].OpenTime
	// (b1) the seed stamped the swing watermark at the newest closed 5m bar.
	if e.State.Swing.LastBarTime != newest {
		t.Fatalf("seed LastBarTime = %d, want the newest closed 5m bar %d",
			e.State.Swing.LastBarTime, newest)
	}
	// (b2) the first tick (no new bars) emits zero swing entries.
	out := e.Tick(bars1m, now)
	for _, in := range out {
		if in.Action == PlaceStopEntry && in.Setup == "SWING4H" {
			t.Fatalf("first tick replayed an old swing entry: %+v", in)
		}
	}
}
