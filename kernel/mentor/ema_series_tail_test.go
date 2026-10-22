package mentor

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"vl/market"
)

// TestEMASeriesChartTailEqualsLevels pins the m134 display curve against the
// levels the evaluator actually trades: on a random 1m tape the LAST
// emaSeriesChart point must equal the EMA9 / EMA34 EMALevels prices (both use
// the same emaValue recurrence, seeded with the first close), so the curve the
// chart draws lands exactly on the level lines — not a parallel approximation.
func TestEMASeriesChartTailEqualsLevels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMATFMinutes = 1
	cfg.EMAPeriod9 = 9
	cfg.EMAPeriod34 = 34

	rng := rand.New(rand.NewSource(20261021))
	base := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC).UnixMilli()
	bars := make([]market.Kline, 480) // 8h of 1m bars
	price := 29900.0
	for i := range bars {
		close := price + (rng.Float64()-0.5)*8
		hi := math.Max(price, close) + rng.Float64()*4
		lo := math.Min(price, close) - rng.Float64()*4
		bars[i] = market.Kline{
			OpenTime:  base + int64(i)*60_000,
			CloseTime: base + int64(i+1)*60_000 - 1,
			Open:      price,
			High:      hi,
			Low:       lo,
			Close:     close,
		}
		price = close
	}

	series := emaSeriesChart(bars, cfg.EMAPeriod9, cfg.EMAPeriod34)
	if want := len(bars) - emaWarmupBars(cfg.EMAPeriod9, cfg.EMAPeriod34); len(series) != want {
		t.Fatalf("series length = %d, want %d (the unconverged head of %d bars is dropped)", len(series), want, emaWarmupBars(cfg.EMAPeriod9, cfg.EMAPeriod34))
	}
	last := series[len(series)-1]

	var got9, got34 float64
	for _, lv := range EMALevels(bars, cfg) {
		switch lv.Kind {
		case KindEMA9:
			got9 = lv.Price
		case KindEMA34:
			got34 = lv.Price
		}
	}
	if math.Abs(last.Ema9-got9) > 1e-9 {
		t.Fatalf("tail Ema9 = %.10f, EMALevels EMA9 = %.10f", last.Ema9, got9)
	}
	if math.Abs(last.Ema34-got34) > 1e-9 {
		t.Fatalf("tail Ema34 = %.10f, EMALevels EMA34 = %.10f", last.Ema34, got34)
	}
}

// TestEMASeriesChartGuardsNonPositivePeriod pins the m150 guard: a non-positive
// period disables that curve (values 0 across the series, no NaN/Inf from
// k = 2/(n+1)), while the other curve keeps its normal values.
func TestEMASeriesChartGuardsNonPositivePeriod(t *testing.T) {
	bars := make([]market.Kline, 300)
	for i := range bars {
		bars[i] = market.Kline{OpenTime: int64(i+1) * 60_000, Close: 100 + float64(i)*0.1}
	}
	s := emaSeriesChart(bars, 0, 34) // period9 disabled
	if len(s) == 0 {
		t.Fatal("a 300-bar series must emit points past the warmup head")
	}
	for _, p := range s {
		if p.Ema9 != 0 {
			t.Fatalf("disabled EMA9 must stay 0, got %f", p.Ema9)
		}
		if math.IsNaN(p.Ema34) || math.IsInf(p.Ema34, 0) {
			t.Fatalf("EMA34 must be finite, got %f", p.Ema34)
		}
	}
	// All-zero Ema9 ⇒ the frontend drops the curve; the tail Ema34 is still valid.
	if s[len(s)-1].Ema34 == 0 {
		t.Fatalf("EMA34 must not be zero on a valid period")
	}
}

// TestEMASeriesChartConvergedHead is the m327 (c) pin: every EMITTED point (the
// head `warmup` bars are dropped) must equal, within 0.01 pts, an EMA seeded
// from LONG history before the window — the old seed-at-window-start left the
// first ~100 points up to 6.9 pts off. MUTANT: emit from the first bar
// (seed-at-window-start) → the first emitted points differ by >0.01 → RED.
func TestEMASeriesChartConvergedHead(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	mk := func(n int, price float64) []market.Kline {
		bars := make([]market.Kline, n)
		p := price
		for i := range bars {
			c := p + (rng.Float64()-0.5)*20
			bars[i] = market.Kline{OpenTime: int64(i+1) * 60_000, Close: c}
			p = c
		}
		return bars
	}
	pre := mk(400, 30000.0) // long history BEFORE the window
	win := mk(400, 30000.0) // the displayed window (seeded at win[0].Close)
	full := append(append([]market.Kline{}, pre...), win...)

	series := emaSeriesChart(win, 9, 34)
	if len(series) == 0 {
		t.Fatal("the window must emit points past the warmup head")
	}
	warmup := emaWarmupBars(9, 34)
	// Reference: EMA seeded from pre[0], run through the full tape up to each
	// emitted window index.
	k9, k34 := 2.0/10.0, 2.0/35.0
	r9, r34 := pre[0].Close, pre[0].Close
	for i := range full[1:] {
		r9 += k9 * (full[1+i].Close - r9)
		r34 += k34 * (full[1+i].Close - r34)
		if i+1 >= len(pre) {
			wi := i + 1 - len(pre) // window index
			if wi >= warmup {
				p := series[wi-warmup]
				if math.Abs(p.Ema9-r9) > 0.01 {
					t.Fatalf("window bar %d: Ema9 %.4f vs long-seeded %.4f (>0.01)", wi, p.Ema9, r9)
				}
				if math.Abs(p.Ema34-r34) > 0.01 {
					t.Fatalf("window bar %d: Ema34 %.4f vs long-seeded %.4f (>0.01)", wi, p.Ema34, r34)
				}
			}
		}
	}
}
