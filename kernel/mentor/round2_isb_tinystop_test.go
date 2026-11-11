package mentor

import (
	"testing"

	"vl/market"
)

// G2 (oct round 2): OctD4.3-R57 [Oct D4.3 @ 56:00] the 4th non-filling candle
// cancels the order "lập tức", unconditionally; R53 @ 51:00 names the body-inside-I1
// hold as the single exception, and no October note repeats the September tiny-stop
// exception (D5.1-R26, m465). Real bars, Thu 29 Oct 2026 CT: I1 13:52 H 29478.00 L
// 29468.50 (stop 9.50 + 2 x 1.5 = 12.50 pts); 13:53/13:54/13:55 stay inside I1 and
// never fill (highs 29478.50/29478.00/29478.00 under the 29479.50 entry). October
// cancels after 13:55; the knob isb_stack_tiny_stop_pts=12.5 keeps the September hold.
func TestG2ISBTinyStopExceptionOffByDefault(t *testing.T) {
	i1 := market.Kline{Open: 29_470, Close: 29_476, High: 29_478.00, Low: 29_468.50}
	in := func(h float64) market.Kline {
		return market.Kline{Open: 29_471, Close: 29_474, High: h, Low: 29_469.0}
	}
	run := func(cfg Config) (cancelledAt int) {
		arm := &ISBArm{FirstBar: i1}
		for n, h := range []float64{29_478.5 - 0.5, 29_478.0, 29_478.0} { // 13:53..13:55 (the arm's candles 2..4)
			if ints := ISBStackTick(arm, in(h), cfg); len(ints) > 0 && ints[0].Action == CancelArm {
				return n + 2
			}
		}
		return 0
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	if stop, _, _ := ISBStopVerdict(i1, cfg); stop != 12.5 {
		t.Fatalf("fixture drift: stop %.2f, want 12.50", stop)
	}
	if cfg.ISBStackTinyStopPts != 0 {
		t.Fatalf("G2: ISBStackTinyStopPts must default 0 (October has no tiny-stop exception), got %g", cfg.ISBStackTinyStopPts)
	}
	if got := run(cfg); got != 4 {
		t.Fatalf("October: the 4th non-filling candle cancels, cancelled at candle %d", got)
	}
	cfg.ISBStackTinyStopPts = 12.5
	if got := run(cfg); got != 0 {
		t.Fatalf("knob 12.5 (September): the 12.50-pt stop is kept past the 4th candle, cancelled at %d", got)
	}
}
