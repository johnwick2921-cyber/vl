package market

import (
	"reflect"
	"testing"
)

func synthKlines(n int) []Kline {
	ks := make([]Kline, n)
	for i := 0; i < n; i++ {
		// a gently varying price so EMAs differ across periods
		c := 100.0 + float64(i)*0.5 + float64(i%5)*0.3
		ks[i] = Kline{OpenTime: int64(i) * 60000, Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 10}
	}
	return ks
}

// TestCalculateTimeframeSeries_EMAPeriods proves the configured-period path is
// the legacy calculation once WARMED: with periods [20,50] on a tape long enough
// to warm both (EMAWarmupBars 68 / 165) the EMAByPeriod entries equal the legacy
// EMA20Values/EMA50Values exactly, and a custom [9,21,200] keeps every
// configured key PRESENT (EMA200 empty → the prompt prints n/a). nil → no map.
//
// R22 F-2: the configured series used to share the legacy "need `period` bars"
// guard, so a short fetch printed the SMA seed under an EMA label.
func TestCalculateTimeframeSeries_EMAPeriods(t *testing.T) {
	klines := synthKlines(200)

	// [20,50] → EMAByPeriod must equal the legacy fixed series, value-for-value.
	s := calculateTimeframeSeries(klines, "5m", 20, IndicatorPeriods{EMA: []int{20, 50}})
	if !reflect.DeepEqual(s.EMAByPeriod[20], s.EMA20Values) {
		t.Fatalf("EMAByPeriod[20] must equal EMA20Values (identical calc)\n got: %v\nwant: %v", s.EMAByPeriod[20], s.EMA20Values)
	}
	if !reflect.DeepEqual(s.EMAByPeriod[50], s.EMA50Values) {
		t.Fatalf("EMAByPeriod[50] must equal EMA50Values\n got: %v\nwant: %v", s.EMAByPeriod[50], s.EMA50Values)
	}

	// [9,21,200] → all three keys present; shorter period has >= as many points.
	s3 := calculateTimeframeSeries(klines, "5m", 20, IndicatorPeriods{EMA: []int{9, 21, 200}})
	for _, p := range []int{9, 21, 200} {
		if _, ok := s3.EMAByPeriod[p]; !ok {
			t.Fatalf("EMAByPeriod missing configured period %d", p)
		}
	}
	if len(s3.EMAByPeriod[9]) < len(s3.EMAByPeriod[200]) {
		t.Fatalf("EMA9 should have >= values than EMA200; got %d vs %d", len(s3.EMAByPeriod[9]), len(s3.EMAByPeriod[200]))
	}
	if n := len(s3.EMAByPeriod[200]); n != 0 {
		t.Fatalf("EMA200 on 200 bars is its SMA seed, not an EMA — want no values, got %d", n)
	}
	// Thin tape: EMA20 not warmed on 60 bars → present, empty (legacy keeps its own guard).
	thin := calculateTimeframeSeries(synthKlines(60), "5m", 20, IndicatorPeriods{EMA: []int{20}})
	if v, ok := thin.EMAByPeriod[20]; !ok || len(v) != 0 {
		t.Fatalf("EMA20 on 60 bars must be present and empty (not warmed), got ok=%v n=%d", ok, len(v))
	}
	// The legacy fixed fields are STILL computed (back-compat readers depend on them).
	if len(s3.EMA20Values) == 0 {
		t.Fatalf("legacy EMA20Values must still be computed for back-compat readers")
	}

	// empty ip → no EMAByPeriod map (legacy output path, byte-identical).
	s0 := calculateTimeframeSeries(klines, "5m", 20, IndicatorPeriods{})
	if s0.EMAByPeriod != nil {
		t.Fatalf("empty ip must leave EMAByPeriod nil; got %v", s0.EMAByPeriod)
	}
}

// TestCalculateTimeframeSeries_BollRsiAtrPeriods proves the same byte-identical
// superset property for BOLL/RSI/ATR: default periods reproduce the legacy fixed
// series exactly, custom periods populate the *ByPeriod maps.
func TestCalculateTimeframeSeries_BollRsiAtrPeriods(t *testing.T) {
	klines := synthKlines(60)
	s := calculateTimeframeSeries(klines, "5m", 20, IndicatorPeriods{
		BOLL: []int{20}, RSI: []int{7, 14}, ATR: []int{14},
	})

	// BOLL period 20 must equal the legacy fixed BOLL series, value-for-value.
	if b := s.BOLLByPeriod[20]; b == nil || !reflect.DeepEqual(b.Upper, s.BOLLUpper) ||
		!reflect.DeepEqual(b.Middle, s.BOLLMiddle) || !reflect.DeepEqual(b.Lower, s.BOLLLower) {
		t.Fatalf("BOLLByPeriod[20] must equal the legacy BOLLUpper/Middle/Lower")
	}
	// RSI 7/14 must equal the legacy fixed RSI series.
	if !reflect.DeepEqual(s.RSIByPeriod[7], s.RSI7Values) {
		t.Fatalf("RSIByPeriod[7] must equal RSI7Values")
	}
	if !reflect.DeepEqual(s.RSIByPeriod[14], s.RSI14Values) {
		t.Fatalf("RSIByPeriod[14] must equal RSI14Values")
	}
	// ATR 14 must equal the legacy fixed ATR14.
	if s.ATRByPeriod[14] != s.ATR14 {
		t.Fatalf("ATRByPeriod[14] must equal ATR14; got %v vs %v", s.ATRByPeriod[14], s.ATR14)
	}

	// custom BOLL [10,12] populates both periods.
	s2 := calculateTimeframeSeries(klines, "5m", 20, IndicatorPeriods{BOLL: []int{10, 12}})
	for _, p := range []int{10, 12} {
		if b := s2.BOLLByPeriod[p]; b == nil || len(b.Upper) == 0 {
			t.Fatalf("BOLLByPeriod[%d] must be populated", p)
		}
	}
}
