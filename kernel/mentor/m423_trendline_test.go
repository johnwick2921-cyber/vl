package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// m423 — trendline T1 (rejection touch, whole-candle 5m break) and T2
// (non-adjacent same-role swings pair). Fixture: the real Friday 2026-10-23
// MNQ 1m bars (DB `bars` symbol 'MNQ' tf '1m', read-only; 10-22 17:00 CT →
// 10-23 15:58 CT), aggregated to the 2m build frame after 09:00 CT exactly as
// the evaluator does.

func m423Bars(t *testing.T) []market.Kline {
	t.Helper()
	return m413Load(t, "testdata/m423_mnq_1m_20261023.csv")
}

// m423Upto returns the bars an evaluator would hold at hh:mm CT on 10-23 (all
// 1m bars opened before it) and that instant in epoch ms.
func m423Upto(t *testing.T, bars []market.Kline, h, m int) ([]market.Kline, int64) {
	t.Helper()
	now := time.Date(2026, time.October, 23, h, m, 0, 0, ctime()).UnixMilli()
	n := 0
	for n < len(bars) && bars[n].OpenTime < now {
		n++
	}
	return bars[:n], now
}

func m423At(t *testing.T, h, m int) int64 {
	return time.Date(2026, time.October, 23, h, m, 0, 0, ctime()).UnixMilli()
}

func m423Find(lines []Trendline, side Side, p0Ms int64, p0Px float64, p1Ms int64, p1Px float64) (Trendline, bool) {
	for _, tl := range lines {
		if tl.Side == side && tl.P0T == p0Ms && tl.P1T == p1Ms && math.Abs(tl.P0Px-p0Px) < 1e-9 && math.Abs(tl.P1Px-p1Px) < 1e-9 {
			return tl, true
		}
	}
	return Trendline{}, false
}

// TestM423Fixture1023OffersTheObviousLines — T2 through the PRODUCTION call
// site (Tick → trendlinesBuildExec → the evaluator's chart set): on the real
// 10-23 tape the day's obvious lines are offered although their anchors are not
// neighbours — the support 08:36 28986.25 → 09:26 29077.50, the descending
// resistance 11:02 29306 → 12:58 29224.75, and 12:58 → 15:16 29177.75.
// Mutant (pair only ADJACENT same-role swings, the pre-m423 loop) → none of the
// three exists → RED.
func TestM423Fixture1023OffersTheObviousLines(t *testing.T) {
	withFlatNear(t) // m428 R5: the non-adjacent pairing is back, with the tight cap (R2 near distance ON)
	all := m423Bars(t)
	cases := []struct {
		name         string
		h, m         int
		side         Side
		p0h, p0m     int
		p0px         float64
		p1h, p1m     int
		p1px         float64
		wantValidAt  bool
		notDeadAtNow bool
	}{
		{"support 08:36 → 09:26", 9, 40, SideLong, 8, 36, 28986.25, 9, 26, 29077.5, false, true},
		{"resistance 11:02 → 12:58", 13, 20, SideShort, 11, 2, 29306, 12, 58, 29224.75, true, true},
		{"resistance 12:58 → 15:16", 15, 30, SideShort, 12, 58, 29224.75, 15, 16, 29177.75, true, true},
	}
	for _, tc := range cases {
		bars, now := m423Upto(t, all, tc.h, tc.m)
		e := New(DefaultConfig())
		e.Cfg.Enabled = true
		e.Tick(bars, now)
		tl, ok := m423Find(e.State.ChartTrendlines, tc.side, m423At(t, tc.p0h, tc.p0m), tc.p0px, m423At(t, tc.p1h, tc.p1m), tc.p1px)
		if !ok {
			t.Fatalf("%s: not offered at %02d:%02d CT (T2 non-adjacent pairing); %d lines held", tc.name, tc.h, tc.m, len(e.State.ChartTrendlines))
		}
		if tc.notDeadAtNow && tl.Dead {
			t.Fatalf("%s: dead at %02d:%02d CT — no 5m candle was wholly beyond it yet", tc.name, tc.h, tc.m)
		}
		if tc.wantValidAt && tl.ValidAt == 0 {
			t.Fatalf("%s: never reached its 3rd-touch return by %02d:%02d CT", tc.name, tc.h, tc.m)
		}
	}
}

// TestM423NonAdjacentPairIsStableAcrossTicks — a drawn line is rebuilt
// identically on every later tick (the anchor choice reads history up to the
// later anchor only). The 08:36 → 09:26 support, offered at 09:40, is still the
// same line (same key, same anchors) at 09:50 and 10:00 (a later tick may retire it: one non-adjacent line per side at a time, m428 R5).
func TestM423NonAdjacentPairIsStableAcrossTicks(t *testing.T) {
	withFlatNear(t) // m428 R5: the non-adjacent pairing is back, with the tight cap (R2 near distance ON)
	all := m423Bars(t)
	var keys []string
	for _, hm := range [][2]int{{9, 40}, {9, 50}, {10, 0}} {
		bars, now := m423Upto(t, all, hm[0], hm[1])
		lines, _ := trendlinesBuildExec(bars, time.UnixMilli(now), true)
		tl, ok := m423Find(lines, SideLong, m423At(t, 8, 36), 28986.25, m423At(t, 9, 26), 29077.5)
		if !ok {
			t.Fatalf("08:36→09:26 missing at %02d:%02d CT", hm[0], hm[1])
		}
		keys = append(keys, tl.key())
	}
	if keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("line key moved between ticks: %v", keys)
	}
}

// m423Support is a rising support line 100 → 102 over 10 minutes (0.2/min).
func m423Support() Trendline {
	return Trendline{Side: SideLong, P0Idx: 0, P0Px: 100, P0T: 0, P1Idx: 10, P1Px: 102, P1T: 10 * 60_000}
}

// TestM423TouchIsARejection — T1: a touch that can make a line VALID is a
// REJECTION — the candle's extreme comes within the 0.5-pt band of the line and
// it closes on the line's own side. A wick BEYOND the band is not a touch, and
// neither is a candle that closes through. Mutant (the old "high reaches, low
// reaches" test) → the wick-through and close-through candles count → RED.
func TestM423TouchIsARejection(t *testing.T) {
	tl := m423Support() // value 101 at minute 5
	mk := func(o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: 5 * 60_000, CloseTime: 5*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	if !tl.touch(mk(102, 102.5, 101.1, 102)) {
		t.Fatal("a low within the band of the line that closes above it is a rejection (touch)")
	}
	if tl.touch(mk(102, 102.5, 100.0, 102)) {
		t.Fatal("a wick that goes BEYOND the band (low 100.0 vs the line ≈101.1) and returns is NOT a touch")
	}
	if tl.touch(mk(102, 102.2, 101.2, 100.9)) {
		t.Fatal("a candle that touches the band but CLOSES through the line (close 100.9 < ≈101.3) is NOT a touch")
	}
	if tl.touch(mk(104, 105, 103, 104)) {
		t.Fatal("a candle wholly above the band is not a touch")
	}
	res := Trendline{Side: SideShort, P0Idx: 0, P0Px: 102, P0T: 0, P1Idx: 10, P1Px: 100, P1T: 10 * 60_000}
	if !res.touch(mk(100, 101.0, 99.5, 100)) {
		t.Fatal("resistance: a high within the band that closes below the line is a rejection")
	}
	if res.touch(mk(100, 102.0, 99.5, 100)) {
		t.Fatal("resistance: a wick beyond the band is not a touch")
	}
	if res.touch(mk(100, 101.0, 99.5, 101.5)) {
		t.Fatal("resistance: a close above the line is not a touch")
	}
}

// TestM423WickThroughReturnDoesNotMakeALineValid — T1 at the BUILD call site on
// the return tape: bar 13 comes back to the line. As a rejection (low in the
// band, closes above) it is the 3rd touch (valid, covered by
// TestTrendlineReturnTouchValidatesAfterLeave). Make its wick go 1.2 pts THROUGH
// the line and return, or make it close through → the line stays FORMING.
func TestM423WickThroughReturnDoesNotMakeALineValid(t *testing.T) {
	for _, tc := range []struct {
		name         string
		o, h, l, c   float64
		wantValidate bool
	}{
		{"rejection (control)", 100.4, 101.5, 99.1, 101.2, true},
		{"wick 1.2 through the line, closes above", 100.4, 101.5, 98.0, 101.2, false},
		{"touches the band, closes through the line", 100.4, 101.5, 99.1, 98.9, false},
	} {
		bars, mk := trendlineReturnTape()
		bars[13] = mk(13, tc.o, tc.h, tc.l, tc.c)
		now := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
		var got Trendline
		for _, tl := range TrendlinesBuild(bars, now) {
			if tl.P1Idx == 8 {
				got = tl
			}
		}
		if got.P1Idx != 8 {
			t.Fatalf("%s: the P1=8 line is missing", tc.name)
		}
		if (got.ValidAt != 0) != tc.wantValidate {
			t.Fatalf("%s: ValidAt=%d, want valid=%v", tc.name, got.ValidAt, tc.wantValidate)
		}
	}
}

// TestM423CrossedUsesTheWholeCandleLife — a wick beyond the line's whole span
// over the candle (not only at the open) is a cross: a rising support line is
// higher at the candle's close than at its open, and the wick is judged against
// the line over the candle's life.
func TestM423CrossedUsesTheWholeCandleLife(t *testing.T) {
	tl := m423Support()
	c := market.Kline{OpenTime: 5 * 60_000, CloseTime: 5*60_000 + 59_999, Low: 100.9}
	if tl.wickBeyond(c) {
		t.Fatal("low 100.9 is within the 0.5 band of the line (≈101.0–101.2): not beyond")
	}
	c.Low = 100.4
	if !tl.wickBeyond(c) {
		t.Fatal("low 100.4 is beyond the band at every instant of the candle (line ≥ 101.0): a cross")
	}
}
