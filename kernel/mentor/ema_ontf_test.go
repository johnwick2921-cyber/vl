package mentor

import (
	"math"
	"testing"

	"vl/market"
)

// emaOntfBars builds one CLOSED 2m candle per close value (two 1m bars in one
// bucket, both closing at c). base must be 2m-aligned.
func emaOntfBars(base int64, closes ...float64) []market.Kline {
	bars := make([]market.Kline, 0, len(closes)*2)
	for i, c := range closes {
		o := base + int64(i)*120_000
		bars = append(bars,
			market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: c, High: c + 1, Low: c - 1, Close: c, Final: true},
			market.Kline{OpenTime: o + 60_000, CloseTime: o + 119_999, Open: c, High: c + 1, Low: c - 1, Close: c, Final: true},
		)
	}
	return bars
}

// TestEMAOnTF pins the m308 helper: the EMA (seeded with the first close,
// k = 2/(n+1)) over CLOSED tfMin-minute candles, with any forming tail bucket
// excluded — the same clock as ClosedCandleTF (BarCloseInstant).
func TestEMAOnTF(t *testing.T) {
	const k = 2.0 / 35.0
	base := int64(120_000_000_000)

	// A flat series leaves the EMA exactly at that close.
	if got := EMAOnTF(emaOntfBars(base, 100, 100, 100), 2, 34); math.Abs(got-100) > 1e-9 {
		t.Fatalf("flat 100s → EMA = %.10f, want 100", got)
	}
	// 100,100,100,101 → 100 + k*(101-100).
	want := 100 + k
	if got := EMAOnTF(emaOntfBars(base, 100, 100, 100, 101), 2, 34); math.Abs(got-want) > 1e-9 {
		t.Fatalf("100,100,100,101 → EMA = %.10f, want %.10f", got, want)
	}
	// A trailing FORMING bucket (one 1m bar in the next 2m bucket) is excluded:
	// the EMA is unchanged from the closed series.
	forming := append(emaOntfBars(base, 100, 100, 100, 101),
		market.Kline{OpenTime: base + 4*120_000, CloseTime: base + 4*120_000 + 59_999, Open: 200, High: 201, Low: 199, Close: 200, Final: false})
	if got := EMAOnTF(forming, 2, 34); math.Abs(got-want) > 1e-9 {
		t.Fatalf("a forming 2m bucket must be excluded → EMA = %.10f, want %.10f", got, want)
	}
	// period <= 0 → 0 (fail-closed).
	if got := EMAOnTF(emaOntfBars(base, 100), 2, 0); got != 0 {
		t.Fatalf("period 0 → EMA = %.10f, want 0", got)
	}
	// Empty bars → 0.
	if got := EMAOnTF(nil, 2, 34); got != 0 {
		t.Fatalf("empty bars → EMA = %.10f, want 0", got)
	}
	// tfMin=1 reads the 1m closes directly.
	if got := EMAOnTF([]market.Kline{
		{OpenTime: base, CloseTime: base + 59_999, Close: 100, Final: true},
		{OpenTime: base + 60_000, CloseTime: base + 119_999, Close: 101, Final: true},
	}, 1, 34); math.Abs(got-(100+k)) > 1e-9 {
		t.Fatalf("1m EMA = %.10f, want %.10f", got, 100+k)
	}
}
