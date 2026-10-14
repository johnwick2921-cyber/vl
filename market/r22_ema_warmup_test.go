package market

// R22 TIME-WEEKLY item 5 (MATH-INDICATORS F-2): the prompt's "EMA200" rows
// were mostly an SMA200. fetchDepth = clamp(maxPeriod+count, 200, 2500) = 220
// bars for EMA200 with count 20, so the last value carried only 20 recurrence
// steps after its SMA200 seed — (199/201)^20 ≈ 0.82 of it was still the SMA.
// Measured on real bars: 5m EMA200 up to 58.9 pts off the warmed EMA.
//
// Driven through GetWithTimeframes — the production read both prompts use —
// with the futures bar provider serving a long tape, against an oracle EMA
// over the WHOLE tape (independent recurrence, seeded at the first close).

import (
	"math"
	"testing"
)

func r22TrendTape(n int) []Kline {
	out := make([]Kline, n)
	for i := range out {
		p := 20000 + 0.5*float64(i) + 25*math.Sin(float64(i)/17)
		out[i] = Kline{OpenTime: int64(i) * 300_000, CloseTime: int64(i+1)*300_000 - 1, Open: p, High: p + 2, Low: p - 2, Close: p, Volume: 100}
	}
	return out
}

func withR22Provider(t *testing.T, tape []Kline) {
	t.Helper()
	prev := FuturesBarsProvider
	FuturesBarsProvider = func(_, _ string, count int) []Kline {
		if count > 0 && len(tape) > count {
			return tape[len(tape)-count:]
		}
		return tape
	}
	t.Cleanup(func() { FuturesBarsProvider = prev })
}

func oracleEMA(tape []Kline, p int) float64 {
	a := 2.0 / float64(p+1)
	e := tape[0].Close
	for _, b := range tape[1:] {
		e = a*b.Close + (1-a)*e
	}
	return e
}

func TestR22PromptEMA200IsWarmed(t *testing.T) {
	tape := r22TrendTape(3000)
	withR22Provider(t, tape)
	d, err := GetWithTimeframes("MNQ", []string{"5m"}, "5m", 20, IndicatorPeriods{EMA: []int{20, 50, 200}})
	if err != nil {
		t.Fatal(err)
	}
	s := d.TimeframeData["5m"].EMAByPeriod[200]
	if len(s) == 0 {
		t.Fatal("EMA200 series empty on a 3,000-bar tape")
	}
	got, want := s[len(s)-1], oracleEMA(tape, 200)
	t.Logf("prompt EMA200 = %.2f · warmed EMA200 = %.2f · Δ %.2f", got, want, got-want)
	if math.Abs(got-want) > 1.0 {
		t.Errorf("prompt EMA200 %.2f is %.2f pts off the warmed EMA200 %.2f — an SMA200 seed labelled EMA", got, got-want, want)
	}
	if cur := d.CurrentEMAByPeriod[200]; math.Abs(cur-want) > 1.0 {
		t.Errorf("current_ema200 %.2f is %.2f pts off the warmed EMA200 %.2f", cur, cur-want, want)
	}
}

// Too few bars to warm → no value at all (rendered n/a by the prompt), never
// an SMA under an EMA label.
func TestR22PromptEMA200ThinTapeHasNoValue(t *testing.T) {
	withR22Provider(t, r22TrendTape(300))
	d, err := GetWithTimeframes("MNQ", []string{"5m"}, "5m", 20, IndicatorPeriods{EMA: []int{20, 200}})
	if err != nil {
		t.Fatal(err)
	}
	s, configured := d.TimeframeData["5m"].EMAByPeriod[200]
	if !configured {
		t.Fatal("a configured period must stay present (empty), so the prompt can say n/a rather than drop it")
	}
	if len(s) != 0 {
		t.Errorf("EMA200 on 300 bars = %v; want no value (the SMA seed still weighs %.0f%%)", s[len(s)-1], 100*math.Pow(199.0/201.0, 100))
	}
	if v := d.CurrentEMAByPeriod[200]; v != 0 {
		t.Errorf("current_ema200 on 300 bars = %.2f; want 0 (unwarmed → the prompt prints n/a)", v)
	}
	if len(d.TimeframeData["5m"].EMAByPeriod[20]) == 0 {
		t.Error("EMA20 is warm on 300 bars and must still render")
	}
}
