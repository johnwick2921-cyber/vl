package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestKeyLevelsPruneKeepsTheMoreRecent — §4.3 step 5 [D5.3 p1 @ 12:55–17:30]:
// two levels < 20 pts apart → delete one, KEEP THE MORE RECENT.
func TestKeyLevelsPruneKeepsTheMoreRecent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1 // mechanics walk on 1m; the DEFAULT source is 1H RTH
	cfg.KeyLevelRTHOnly = false
	cfg.KeyLevelPrunePts = 20
	// alternate colour every bar; opens walk up 10 pts at a time →
	// of every pair < 20 pts apart only the more recent survives: the kept
	// set is {110, 130, 150, 170} (every pair >= 20 apart, newest-first wins).
	bars := []market.Kline{}
	for i := 0; i < 8; i++ {
		b := market.Kline{OpenTime: int64(i) * 60_000, Open: 100 + float64(i)*10}
		if i%2 == 0 {
			b.Close = b.Open + 1 // green
		} else {
			b.Close = b.Open - 1 // red
		}
		bars = append(bars, b)
	}
	got := KeyLevels(bars, cfg)
	if len(got) != 4 {
		t.Fatalf("levels = %d, want 4 (pairs <20 apart collapse newest-first): %+v", len(got), got)
	}
	for _, want := range []float64{110, 130, 150, 170} {
		found := false
		for _, l := range got {
			if l.Price == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("kept set %+v missing %v (every pair < 20 keeps the more recent)", got, want)
		}
	}
	for i, a := range got {
		for j := i + 1; j < len(got); j++ {
			if abs(a.Price-got[j].Price) < cfg.KeyLevelPrunePts {
				t.Fatalf("kept pair %v/%v closer than prune: %+v", a.Price, got[j].Price, got)
			}
		}
	}
}

// TestKeyLevelsLineAtSecondCandleOpen — §4.3 step 4 [D5.3 p1 @ 12:55]:
// at a colour change the line is the OPEN of the SECOND candle
// (red→green: open of the green; green→red: open of the red).
func TestKeyLevelsLineAtSecondCandleOpen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.KeyLevelRTHOnly = false
	cfg.KeyLevelPrunePts = 0.5 // no pruning — keep every change
	bars := []market.Kline{
		{OpenTime: 1, Open: 100, Close: 101}, // green
		{OpenTime: 2, Open: 103, Close: 102}, // red (change) → level at 103
		{OpenTime: 3, Open: 104, Close: 105}, // green (change) → level at 104
		{OpenTime: 4, Open: 106, Close: 107}, // green (no change)
	}
	got := KeyLevels(bars, cfg)
	if len(got) != 2 {
		t.Fatalf("levels = %d, want 2: %+v", len(got), got)
	}
	if got[0].Price != 103 || got[1].Price != 104 {
		t.Fatalf("levels = %v, want [103 104] (opens of the SECOND candle of each change)", got)
	}
}

// TestKeyLevels1HAnchoredRTHOnly — KEY-LEVEL RULING item 1: the level source
// is the 1H series ANCHORED at the market open 08:30 CT; external (ETH) candles
// never produce a level, and the 15:00 candle is outside RTH. The first RTH
// candle is 08:30–09:29; a colour change there draws a level at ITS OPEN (the
// 08:30 1m open).
func TestKeyLevels1HAnchoredRTHOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelPrunePts = 0.5
	ms := int64(60_000)
	// real-UTC epoch ms for 2026-09-15 CT wall times (EPOCH RULING:
	// America/Chicago via auditMs — never wall minutes on the epoch).
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, time.September, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + ms - 1, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{}
	// ETH candle 07:30–08:29: RED overall. Colour changes on ETH candles are
	// invisible — the walk starts at the market open.
	for m := 7*60 + 30; m < 8*60+30; m++ {
		hh, mm := m/60, m%60
		bars = append(bars, mk(hh, mm, 101, 102, 99, 100)) // red
	}
	// RTH candle 08:30–09:29: GREEN overall.
	for m := 8*60 + 30; m < 9*60+30; m++ {
		hh, mm := m/60, m%60
		bars = append(bars, mk(hh, mm, 103, 104, 102.5, 104)) // green
	}
	// 09:30 candle RED — the green→red change draws a level at ITS OPEN (the
	// 09:30 1m open = 107).
	for m := 9*60 + 30; m < 10*60+30; m++ {
		hh, mm := m/60, m%60
		bars = append(bars, mk(hh, mm, 107, 108, 106.5, 106.6)) // red
	}
	// 10:30 candle RED again — no change, no level.
	bars = append(bars, mk(10, 30, 106.6, 107, 106, 106.2))
	// the 15:00 candle opens outside RTH: green vs the last red would be a
	// change, but external candles never draw.
	bars = append(bars, mk(15, 0, 106, 107, 105.5, 106.8)) // green, excluded
	got := KeyLevels(bars, cfg)
	if len(got) != 1 || got[0].Price != 107 {
		t.Fatalf("1H-anchored RTH walk: levels = %+v, want exactly one at 107 (09:30 candle open)", got)
	}
}

// TestKeyLevelsOnRecorded1HWeek — KEY-LEVEL RULING final on the bot's own
// tape (canon 53): the recorded 1H week 2026-09-28..10-02 walked as the RTH
// series. Every level must be the OPEN of a colour-changing 1H RTH candle
// (never a wick, never an ETH candle), every kept pair must respect the 20-pt
// prune, and the week must draw levels.
func TestKeyLevelsOnRecorded1HWeek(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := loadFixture(t, "mnq_1h_2026-09-28to10-02", "1h")
	candles := keyLevel1HBars(bars)
	if len(candles) < 25 {
		t.Fatalf("1H RTH candles = %d, want ~35 (5 days x 7)", len(candles))
	}
	got := KeyLevels(bars, cfg)
	if len(got) == 0 {
		t.Fatal("no key levels drawn on the recorded 1H week")
	}
	for _, l := range got {
		found := false
		for i := 1; i < len(candles); i++ {
			if candles[i].Open == l.Price &&
				candleColour(candles[i-1]) != candleColour(candles[i]) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("level %+v is not the open of a colour-changing 1H RTH candle", l)
		}
	}
	for i := 1; i < len(got); i++ {
		if abs(got[i].Price-got[i-1].Price) < cfg.KeyLevelPrunePts {
			t.Fatalf("levels %d/%d are %.2f apart (< %v prune): %+v", i-1, i, abs(got[i].Price-got[i-1].Price), cfg.KeyLevelPrunePts, got)
		}
	}
	t.Logf("%d key levels from %d 1H RTH candles on the recorded week", len(got), len(candles))
}

// countColourChanges counts raw colour transitions on closed bars (§4.3 step 2).
func countColourChanges(bars []market.Kline) int {
	n := 0
	for i := 1; i < len(bars); i++ {
		if candleColour(bars[i-1]) != candleColour(bars[i]) {
			n++
		}
	}
	return n
}

// TestKeyLevelsDisabledIsNil — L4: with mentor_mode OFF the evaluator emits
// nothing (byte-identical bot).
// TestKeyLevelDeletedBy1HBodyCloseOnly — KEY-LEVEL RULING final, slide 31
// step 4 + slide 32: "Xác nhận" is a 1H candle CLOSE judged by the BODY. A 1H
// RTH candle whose BODY closes through the level (open one side, close the
// other) deletes it; a 1H wick through does NOT. The evaluator persists the
// deletion in State.DeletedLevels and the level leaves the set at once.
func TestKeyLevelDeletedBy1HBodyCloseOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1 // draw the level deterministically on the 1m walk; the DELETION runs on the anchored 1H RTH candles either way
	ms := int64(60_000)
	_ = ms
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		// real-UTC epochs (EPOCH RULING): 2026-09-15 CT bars.
		t0 := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: t0, CloseTime: t0 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	// 08:30–08:31 red→green change draws the level at the 08:31 OPEN = 100.
	head := []market.Kline{
		mk(8, 30, 101, 101.5, 100.5, 101),  // red
		mk(8, 31, 100, 100.5, 99.5, 100.4), // green — level at 100
	}
	// pad the rest of the 08:30 1H candle (green; the 08:30 candle must NOT
	// cross the level itself — its open 101 and close 100.8 both sit above 100)
	pad := func() []market.Kline {
		out := make([]market.Kline, 58)
		for j := 0; j < 58; j++ {
			out[j] = mk(8, 32+j, 100.6, 101.2, 100.4, 100.8)
		}
		return out
	}
	// the 09:30 1H candle is the confirmer: BODY case — open below, close above
	// (crosses 100 → delete); WICK case — wick pokes above, body stays below.
	hour := func(bodyThrough bool) []market.Kline {
		out := make([]market.Kline, 60)
		for j := 0; j < 60; j++ {
			if bodyThrough {
				out[j] = mk(9, 30+j, 99.5, 101.8, 99.2, 101.2)
			} else {
				out[j] = mk(9, 30+j, 99.6, 101.5, 99.3, 99.8)
			}
		}
		return out
	}
	// one bar of the 10:30 candle so the 09:30 candle is closed on the final tick
	next := mk(10, 30, 100.8, 101.4, 100.4, 101)

	makeBars := func(bodyThrough bool) []market.Kline {
		bars := append([]market.Kline{}, head...)
		bars = append(bars, pad()...)
		bars = append(bars, hour(bodyThrough)...)
		bars = append(bars, next)
		return bars
	}
	body := makeBars(true)
	wick := makeBars(false)
	now := next.OpenTime + 59_999

	t.Run("body close through deletes", func(t *testing.T) {
		e := New(cfg)
		e.Tick(body, now)
		deleted := false
		for k := range e.State.DeletedLevels {
			if k == keyLevelKey(head[1]) {
				deleted = true
			}
		}
		if !deleted {
			t.Fatalf("a 1H BODY close through must delete the level; state=%v", e.State.DeletedLevels)
		}
	})
	t.Run("wick through keeps", func(t *testing.T) {
		e := New(cfg)
		e.Tick(wick, now)
		if len(e.State.DeletedLevels) != 0 {
			t.Fatalf("a 1H wick through must NOT delete the level; state=%v", e.State.DeletedLevels)
		}
	})
	t.Run("forming 1H candle does not delete", func(t *testing.T) {
		// confirmation rule (b): deletion needs a CLOSED 1H candle. Mid-candle
		// (the 09:30 candle is still forming, its close time not reached) the
		// level survives even though the candle would cross; once it closes the
		// same history deletes it. Stage 1 feeds the history only UP TO midNow
		// (production shape: bars never extend past now).
		e := New(cfg)
		midNow := mk(9, 45, 0, 0, 0, 0).OpenTime + 59_999 // inside the 09:30 candle
		var upto []market.Kline
		for _, b := range body {
			if b.CloseTime <= midNow {
				upto = append(upto, b)
			}
		}
		e.Tick(upto, midNow)
		if len(e.State.DeletedLevels) != 0 {
			t.Fatalf("a still-FORMING 1H candle must not delete the level; state=%v", e.State.DeletedLevels)
		}
		e.Tick(body, now) // the 09:30 candle has now closed
		deleted := false
		for k := range e.State.DeletedLevels {
			if k == keyLevelKey(head[1]) {
				deleted = true
			}
		}
		if !deleted {
			t.Fatalf("after the candle CLOSED the deletion must fire; state=%v", e.State.DeletedLevels)
		}
	})
}

func TestKeyLevelsDisabledIsNil(t *testing.T) {
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	if got := KeyLevels(bars, Config{}); got != nil {
		t.Fatalf("disabled config produced levels: %+v", got)
	}
	if got := EMALevels(bars, Config{}); got != nil {
		t.Fatalf("disabled config produced EMA levels: %+v", got)
	}
}

// TestEMA34LevelTracksCloses — EMA 34 computed on the recorded 1m day; the
// level is a single price equal to the standard EMA seeded at the first close
// (§11; DS-106: ema_periods knob).
func TestEMA34LevelTracksCloses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMATFMinutes = 1
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	got := EMALevels(bars, cfg)
	if len(got) != 2 {
		t.Fatalf("EMA levels = %d, want 2 (34 and 9): %+v", len(got), got)
	}
	if got[0].Kind != KindEMA34 || got[1].Kind != KindEMA9 {
		t.Fatalf("kinds = %v/%v, want ema34/ema9", got[0].Kind, got[1].Kind)
	}
	if got[0].Price != emaValue(bars, 34) || got[1].Price != emaValue(bars, 9) {
		t.Fatalf("EMA values mismatch the standard EMA computation")
	}
}
