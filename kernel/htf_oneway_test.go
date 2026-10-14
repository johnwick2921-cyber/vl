package kernel

import (
	"strings"
	"testing"

	"vl/market"
)

// F-4 (planner-touch-fills, 2026-10-14) — a one-way move without a completed
// 3-swing set must read TRENDING for the HTF veto; a genuine range must not.
//
// oneWayTape: 36 1h bars of a flat 100–110 zigzag (equal highs, equal lows →
// the 3-swing rule reads RANGING), then `crash` bars falling 5 pts each from
// 100. A falling leg makes no confirmed fractal low (k=2 needs two higher lows
// AFTER it), so the swing set cannot see the break — the 10-14 07:00–09:00
// shape on a TF whose swings had not caught up.
func oneWayTape(crash int) []market.KlineBar {
	var out []market.KlineBar
	t := int64(1_791_000_000_000)
	px := []float64{105, 108, 110, 107, 103, 100}
	for i := 0; i < 36; i++ {
		c := px[i%len(px)]
		out = append(out, market.KlineBar{Time: t, Open: c, High: c + 0.5, Low: c - 0.5, Close: c})
		if c == 110 {
			out[len(out)-1].High = 110
		}
		if c == 100 {
			out[len(out)-1].Low = 100
		}
		t += 3_600_000
	}
	c := 100.0
	for i := 0; i < crash; i++ {
		o := c
		c -= 5
		out = append(out, market.KlineBar{Time: t, Open: o, High: o + 0.25, Low: c - 0.25, Close: c})
		t += 3_600_000
	}
	return out
}

func oneWayNow(bars []market.KlineBar) int64 { return bars[len(bars)-1].Time + 3_600_000 }

func TestStructureOneWayCrashReadsTrendingForTheVeto(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "1h")
	t.Setenv("STRUCTURE_ONEWAY_ATR", "")
	bars := oneWayTape(4) // last close 80: 20 pts under the 100 swing low
	st := ComputeStructureState(bars, 60, 0, oneWayNow(bars))
	if st.Trend != "RANGING" {
		t.Fatalf("fixture: the 3-swing rule must read the zigzag + crash as RANGING, got %s", st.Trend)
	}
	if st.OneWay == nil || st.OneWay.Dir != "down" || st.OneWay.Extreme != 100 || st.OneWay.Close != 80 {
		t.Fatalf("a close ≥ 1×ATR (%.2f) under the last swing low 100 is a one-way move: %+v", st.Atr, st.OneWay)
	}
	if st.VetoTrend() != "TRENDING_DOWN" {
		t.Fatalf("the veto must read the one-way RANGING TF as TRENDING_DOWN, got %s", st.VetoTrend())
	}
	blocked, why := HTFVetoVerdict(map[string]StructureState{"1h": st}, "open_long", "1h")
	if !blocked || !strings.Contains(why, "TRENDING_DOWN") || !strings.Contains(why, "one-way") {
		t.Fatalf("a long into a one-way 1h crash must be vetoed with the one-way evidence: blocked=%v %q", blocked, why)
	}
	if b, _ := HTFVetoVerdict(map[string]StructureState{"1h": st}, "open_short", "1h"); b {
		t.Fatal("a short WITH the one-way move is never vetoed")
	}
	// cross: both TFs one-way → veto; one genuine range → fail-open.
	t.Setenv("HTF_VETO_MODE", "cross")
	if b, why := HTFVetoVerdict(map[string]StructureState{"1h": st, "4h": st}, "open_long", "1h"); !b || !strings.Contains(why, "cross-check") {
		t.Fatalf("cross with both TFs one-way down must veto a long: %v %q", b, why)
	}
}

func TestStructureGenuineRangeStaysFailOpen(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "1h")
	t.Setenv("STRUCTURE_ONEWAY_ATR", "")
	poke := oneWayTape(0) // + one bar closing 2 pts under the 100 low (< 1×ATR ≈ 3.7)
	poke = append(poke, market.KlineBar{Time: poke[len(poke)-1].Time + 3_600_000, Open: 100, High: 100.25, Low: 97.75, Close: 98})
	for crash, bars := range [][]market.KlineBar{oneWayTape(0), poke} { // no break, and a break smaller than 1×ATR
		st := ComputeStructureState(bars, 60, 0, oneWayNow(bars))
		if st.Trend != "RANGING" || st.OneWay != nil || st.VetoTrend() != "RANGING" {
			t.Fatalf("crash=%d: a genuine range (or a sub-ATR poke) must stay RANGING with no one-way evidence: trend=%s atr=%.2f oneway=%+v", crash, st.Trend, st.Atr, st.OneWay)
		}
		if b, why := HTFVetoVerdict(map[string]StructureState{"1h": st}, "open_long", "1h"); b {
			t.Fatalf("crash=%d: RANGING still fails open: %q", crash, why)
		}
	}
}

// The knob is READ: a stricter multiple turns the same crash back into a range.
func TestStructureOneWayKnobIsRead(t *testing.T) {
	t.Setenv("STRUCTURE_ONEWAY_ATR", "50")
	bars := oneWayTape(4)
	if st := ComputeStructureState(bars, 60, 0, oneWayNow(bars)); st.OneWay != nil {
		t.Fatalf("STRUCTURE_ONEWAY_ATR=50 must not see a 20-pt break as one-way: %+v", st.OneWay)
	}
	if StructureOneWayATR() != 50 {
		t.Fatalf("resolver must read the env: %v", StructureOneWayATR())
	}
}

// The native 4h input: absent bars → ABSENT key (never a fabricated RANGING);
// closed bars → the 4h state.
func TestWithNativeHTFStructureAbsentNotFabricated(t *testing.T) {
	snap := WithNativeHTFStructure(map[string]StructureState{"1h": {Trend: "RANGING"}}, nil, 1)
	if _, ok := snap["4h"]; ok {
		t.Fatal("no 4h bars → no 4h key")
	}
	var bars []market.Kline
	for _, b := range oneWayTape(4) {
		bars = append(bars, market.Kline{OpenTime: b.Time, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
	}
	// Re-time the series onto 4h bars: the same shape, 4h apart.
	for i := range bars {
		bars[i].OpenTime = 1_791_000_000_000 + int64(i)*4*3_600_000
	}
	now := bars[len(bars)-1].OpenTime + 4*3_600_000
	snap = WithNativeHTFStructure(map[string]StructureState{}, bars, now)
	if st, ok := snap["4h"]; !ok || st.VetoTrend() != "TRENDING_DOWN" {
		t.Fatalf("the native 4h state must be present and read the one-way crash: %+v", snap)
	}
}
