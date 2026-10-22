package trader

import (
	"math"
	"testing"
)

// TestCandleTrailStopOneTickBeyond (m283 P2 — CTO ruling) pins the candle trail
// one tick BEYOND the CLOSED candle's low (long) / high (short): the course's
// exit is the candle BREAKING past the prior extreme, so a stop AT the low
// would fire on a mere touch, not a break ("khi nào có một cây nến VƯỢT QUA được
// đáy cây nến trước đó … nó sẽ đi theo cái hướng mà nó phá" [D2.4 p1 @08:11–08:24,
// @08:46–08:49]; "đặt ở DƯỚI đáy cây nến 2 phút đó" [X8 part1 @16:18–16:31]).
// Mutant (drop the tick offset → AT the extreme) → RED.
func TestCandleTrailStopOneTickBeyond(t *testing.T) {
	const tick = 0.25

	// long: the stop sits one tick BELOW the low, rounded AWAY (down) to the grid.
	if got := mentorCandleTrailStop("long", 100.00, 99.00, tick); got != 98.75 {
		t.Fatalf("long trail = %.4f, want 98.75 (low 99.00 − 1 tick)", got)
	}
	// short: the stop sits one tick ABOVE the high.
	if got := mentorCandleTrailStop("short", 100.00, 99.00, tick); got != 100.25 {
		t.Fatalf("short trail = %.4f, want 100.25 (high 100.00 + 1 tick)", got)
	}

	// An off-grid low rounds AWAY from the market (down for long), never inside.
	l := 99.37
	got := mentorCandleTrailStop("long", 100.00, l, tick)
	if got > l-tick || math.Abs(got-(l-tick)) > tick {
		t.Fatalf("off-grid long trail = %.4f, want within one tick at/below (low − tick) %.4f", got, l-tick)
	}

	// tick <= 0 returns the raw extreme (no tick to go beyond with).
	if got := mentorCandleTrailStop("long", 100.00, 99.00, 0); got != 99.00 {
		t.Fatalf("tick<=0 long trail = %.4f, want 99.00", got)
	}
}
