package trader

import (
	"math"
	"testing"
)

// TestCandleTrailStopAtTheExtreme (m186 W-MENTOR-EXACT, D2.4 p1 @07:01–08:49)
// pins the candle trail at the CLOSED candle's low (long) / high (short) — the
// course puts the stop at the extreme ("Đặt ở dưới đáy cây nến đó"; "Em move
// lên đây") and says nothing about a tick offset. The former 1-tick-beyond
// offset was an item-7 ruling, not the course. Mutant (v := l - tick / h + tick)
// → RED.
func TestCandleTrailStopAtTheExtreme(t *testing.T) {
	const tick = 0.25

	// long: the stop sits AT the low, rounded AWAY (down) to the grid.
	if got := mentorCandleTrailStop("long", 100.00, 99.00, tick); got != 99.00 {
		t.Fatalf("long trail = %.4f, want 99.00 (at the low, no tick offset)", got)
	}
	// short: the stop sits AT the high.
	if got := mentorCandleTrailStop("short", 100.00, 99.00, tick); got != 100.00 {
		t.Fatalf("short trail = %.4f, want 100.00 (at the high, no tick offset)", got)
	}

	// An off-grid low rounds AWAY from the market (down for long), never inside.
	l := 99.37
	got := mentorCandleTrailStop("long", 100.00, l, tick)
	if got > l || math.Abs(got-l) > tick {
		t.Fatalf("off-grid long trail = %.4f, want within one tick at/below the low %.4f", got, l)
	}

	// tick <= 0 returns the raw extreme (no grid to round to).
	if got := mentorCandleTrailStop("long", 100.00, 99.00, 0); got != 99.00 {
		t.Fatalf("tick<=0 long trail = %.4f, want 99.00", got)
	}
}
