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
	if len(series) != len(bars) {
		t.Fatalf("series length = %d, want %d (one point per bar)", len(series), len(bars))
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
	bars := []market.Kline{
		{OpenTime: 1, Close: 100},
		{OpenTime: 60_001, Close: 101},
		{OpenTime: 120_001, Close: 102},
	}
	s := emaSeriesChart(bars, 0, 34) // period9 disabled
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
