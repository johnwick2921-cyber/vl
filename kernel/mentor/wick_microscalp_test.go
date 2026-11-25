package mentor

import (
	"reflect"
	"testing"

	"vl/market"
)

// D4.3 wick microscalp — the TARGET half (m465 P5 replaced the pre-m465
// single far-wick target, fed by the trigger direction and never wired to a
// knob, with the course's 5m-wick ladder): "Target thì là những cái râu của
// khung 5 phút" [D4.3 @09:39–09:46]; never "100-200 điểm" [@07:30–07:49].

func wick5m(i int, o, h, l, c float64) market.Kline {
	ot := auditMs(2026, 9, 14, 8, 0, 0) + int64(i)*300_000
	return market.Kline{OpenTime: ot, CloseTime: ot + 299_999, Open: o, High: h, Low: l, Close: c}
}

// TestWickTargetLadderLongAndShortMirror — the ladder is the nearby 5m wick
// tops above a demand box (nearest first), and the mirror under a supply box.
// A candle without the wick offers no target; nothing past 100 pts.
func TestWickTargetLadderLongAndShortMirror(t *testing.T) {
	z := WickZone{Side: SideLong, Bottom: 28880.50, Top: 28885.50}
	c5 := []market.Kline{
		wick5m(0, 28900, 28933.50, 28880.50, 28905.50), // upper wick to 28,933.50 (Ex.1 ray)
		wick5m(1, 28905, 28919.75, 28882.00, 28890.00), // upper wick to 28,919.75 (Ex.1 ray)
		wick5m(2, 28890, 28950.00, 28888.00, 28950.00), // closes AT its high: no upper wick → no target
		wick5m(3, 28950, 28990.00, 28940.00, 28960.00), // 104.5 over the box top → past the 100-pt cap
	}
	if got, want := wickTargetLadder(c5, z), []float64{28919.75, 28933.50}; !reflect.DeepEqual(got, want) {
		t.Fatalf("long ladder = %v, want %v (Ex.1 rays, nearest first) [D4.3 @07:40–07:44]", got, want)
	}
	// Short mirror (R27): the lower-wick bottoms under the supply box.
	zs := WickZone{Side: SideShort, Bottom: 29628, Top: 29642}
	s5 := []market.Kline{
		wick5m(0, 29620, 29630, 29600, 29610), // lower wick to 29,600
		wick5m(1, 29610, 29615, 29590, 29590), // closes AT its low: no lower wick
		wick5m(2, 29612, 29620, 29606, 29615), // lower wick to 29,606
	}
	if got, want := wickTargetLadder(s5, zs), []float64{29606, 29600}; !reflect.DeepEqual(got, want) {
		t.Fatalf("short ladder = %v, want %v (nearest first, under the box)", got, want)
	}
	// Only the last 12 closed 5m candles are "nearby".
	var many []market.Kline
	many = append(many, wick5m(0, 28900, 28910, 28890, 28895)) // 13 candles back: out of range
	for i := 1; i <= 12; i++ {
		many = append(many, wick5m(i, 28890, 28890, 28880, 28890)) // no upper wick
	}
	if got := wickTargetLadder(many, z); len(got) != 0 {
		t.Fatalf("a wick 13 candles back is not nearby: got %v", got)
	}
}

// TestWickMicroscalpLevelNeverBecomesALocation — the 5m-wick TARGET is
// target-only; the zone EDGE (KindWickZone) is the location.
func TestWickMicroscalpLevelNeverBecomesALocation(t *testing.T) {
	if levelIsLocation(Level{Key: "wick_microscalp:long:110.00", Kind: KindWickMicroscalp, Price: 110}, nil) {
		t.Fatal("the wick-microscalp target must never be a PHL/PLH location")
	}
}
