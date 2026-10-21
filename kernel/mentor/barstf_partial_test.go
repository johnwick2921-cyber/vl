package mentor

import (
	"testing"

	"vl/market"
)

func kline1m(minute int64, c float64) market.Kline {
	return market.Kline{OpenTime: minute * 60_000, Open: c - 0.5, High: c + 0.5, Low: c - 0.5, Close: c}
}

// TestBarsTFHolesMarksInteriorHole pins m134 (b) detect-only: a 5m bucket with
// an interior missing minute is reported as holey; a complete bucket is not.
func TestBarsTFHolesMarksInteriorHole(t *testing.T) {
	bars := []market.Kline{
		kline1m(0, 10), kline1m(1, 10), kline1m(3, 10), kline1m(4, 10), // bucket 0: minute 2 missing
		kline1m(5, 11), kline1m(6, 11), kline1m(7, 11), kline1m(8, 11), kline1m(9, 11), // bucket 5: complete
	}
	holes := BarsTFHoles(bars, 5)
	if !holes[0] {
		t.Fatalf("bucket 0 (missing minute 2) must be reported holey, got %v", holes)
	}
	if holes[5] {
		t.Fatalf("complete bucket 5 must not be holey, got %v", holes)
	}
	if holes[10] {
		t.Fatalf("no such bucket, got %v", holes)
	}
}

// TestBarsTFHolesFormingTailNotHoley pins that the forming tail (a short LAST
// bucket) is not a hole.
func TestBarsTFHolesFormingTailNotHoley(t *testing.T) {
	bars := []market.Kline{
		kline1m(0, 10), kline1m(1, 10), kline1m(2, 10), kline1m(3, 10), kline1m(4, 10),
		kline1m(5, 11), kline1m(6, 11), // forming tail: bucket 5 has only 2 minutes
	}
	holes := BarsTFHoles(bars, 5)
	if holes[0] || holes[5] {
		t.Fatalf("no hole expected, got %v", holes)
	}
}

// TestEmaValueByteIdenticalAcrossSessionBreak is the m139 REVIEW-BACK pin: emaValue
// must be byte-identical to the plain recurrence across the 16:00→17:00 CME break
// AND a weekend — a gap-skip here shifts every EMA on a normal day. MUTANT:
// re-add a gap-skip to emaValue → RED.
func TestEmaValueByteIdenticalAcrossSessionBreak(t *testing.T) {
	// 5m buckets. Minutes: 0..955 are 15:55 CT-equivalent run (just a grid);
	// a 60-minute session-break gap, then a weekend gap, then more bars.
	var bars []market.Kline
	add := func(minute int64, c float64) {
		bars = append(bars, market.Kline{OpenTime: minute * 60_000, Open: c, High: c + 1, Low: c - 1, Close: c})
	}
	for m := int64(0); m <= 955; m += 5 { // 15:55 = minute 955
		add(m, 100+0.02*float64(m/5))
	}
	add(1015, 200) // 16:55 CT reopen? no — 17:00 CT = minute 1020; keep it simple: a break then a bar
	// weekend: a multi-day gap.
	add(1015+2*24*60, 210) // two days later
	for m := int64(1015 + 2*24*60 + 5); m <= 1015+2*24*60+50; m += 5 {
		add(m, 210+0.02*float64((m-(1015+2*24*60))/5))
	}

	got := emaValue(bars, 50)
	want := plainEMA(bars, 50)
	if got != want {
		t.Fatalf("emaValue across session break + weekend = %.6f, want the plain recurrence %.6f — a gap-skip is back", got, want)
	}
}

// plainEMA is the byte-for-byte pre-m134 recurrence: every bar advances the EMA
// regardless of the gap between bars. This is the ground truth the pin holds.
func plainEMA(bars []market.Kline, period int) float64 {
	k := 2.0 / float64(period+1)
	ema := bars[0].Close
	for _, b := range bars[1:] {
		ema = ema + k*(b.Close-ema)
	}
	return ema
}
