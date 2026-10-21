package mentor

import (
	"testing"

	"vl/market"
)

func kline1m(minute int64, c float64) market.Kline {
	return market.Kline{OpenTime: minute * 60_000, Open: c - 0.5, High: c + 0.5, Low: c - 0.5, Close: c}
}

// TestBarsTFHolesMarksInteriorHole pins m134 (b): a 5m bucket with an interior
// missing minute is reported as holey; a complete bucket and the forming tail
// are not.
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

// TestEmaValueSkipsHoleBar pins m134 (b): a bar that closes a HOLE must not move
// the EMA across it — the restart chart hole corruption. MUTANT: remove the gap
// skip → the EMA jumps toward the hole-closing bar → RED.
func TestEmaValueSkipsHoleBar(t *testing.T) {
	full := make([]market.Kline, 0, 62)
	for i := 0; i < 60; i++ {
		full = append(full, kline1m(int64(i*5), 100+0.05*float64(i)))
	}
	holed := append([]market.Kline(nil), full...)
	holed = append(holed, market.Kline{OpenTime: (60*5 + 35) * 60_000, Open: 200, High: 200, Low: 200, Close: 200})

	ema := emaValue(holed, 50)
	if ema > 110 {
		t.Fatalf("EMA jumped across the hole to %.4f — the hole-closing bar moved it", ema)
	}
}
