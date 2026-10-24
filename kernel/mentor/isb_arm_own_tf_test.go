package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// m406 (the audit gap 1 fix): the ISB stacking tick loop used to hard-code 2m
// for every arm with TFMin>1, so a 5m arm (the evening-slow leg) ticked on 2m
// candles and its 4th-candle cancel landed on the wrong candle size
// [N12, D4.2 p2 @08:21–09:01]. These tests drive the PRODUCTION evaluator path
// (e.Tick) with a seeded arm and alternating 1m bars that never form an
// inside-bar pair (no new arm is placed) and whose bodies always stay inside
// the seeded mother I1 (the arm never escapes).

// m406BarAt builds a 1m bar whose body alternates so consecutive bars are never
// an inside-bar pair, while every bar stays inside the mother I1 [90, 110].
func m406BarAt(openMs int64, even bool) market.Kline {
	if even {
		return market.Kline{Open: 100, High: 103, Low: 99, Close: 102, OpenTime: openMs, CloseTime: openMs + 59_999}
	}
	return market.Kline{Open: 98, High: 99, Low: 95, Close: 96, OpenTime: openMs, CloseTime: openMs + 59_999}
}

// driveArmTicks seeds one arm at the given TF, then ticks the evaluator one 1m
// bar at a time from 09:00 and records the `now` at which the seeded arm emits
// an ExtendArm or a stacking CancelArm, plus the total cancel count.
func driveArmTicks(t *testing.T, tfMin, nBars int) (stamps []int64, cancels int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.ORB = ORB{Day: 0, High: 200, Low: 50, Drawn: true, Escaped: SideLong}
	mother := market.Kline{Open: 95, Close: 105, High: 110, Low: 90}
	e.State.ISBArms["a"] = ISBArm{FirstBar: mother, Inside: 0, TFMin: tfMin}

	base := auditMs(2026, 9, 15, 9, 0, 0)
	for i := 2; i <= nBars; i++ {
		bars := make([]market.Kline, i)
		for j := 0; j < i; j++ {
			bars[j] = m406BarAt(base+int64(j)*60_000, j%2 == 0)
		}
		now := bars[len(bars)-1].CloseTime + 1
		for _, in := range e.Tick(bars, now) {
			if in.ArmID != "a" {
				continue
			}
			switch in.Action {
			case ExtendArm:
				stamps = append(stamps, now)
			case CancelArm:
				if strings.HasPrefix(in.Reason, "ISB stacking") {
					cancels++
					stamps = append(stamps, now)
				}
			}
		}
	}
	return stamps, cancels
}

// TestFiveMinuteArmTicksOnItsOwnFrame pins the m406 fix: a 5m arm advances
// exactly once per CLOSED 5m candle (not per 1m or 2m bar), so its 4th-candle
// cancel lands on the 4th 5m candle. The holds fire at the 09:05 and 09:10 5m
// bucket closes and the cancel at the 09:15 close — exactly three arm ticks.
// MUTANT: hard-code 2 again in the tick loop → the arm ticks at 09:02/09:04 and
// cancels at 09:06 → RED.
func TestFiveMinuteArmTicksOnItsOwnFrame(t *testing.T) {
	stamps, cancels := driveArmTicks(t, 5, 20)
	want := []int64{
		auditMs(2026, 9, 15, 9, 5, 0),
		auditMs(2026, 9, 15, 9, 10, 0),
		auditMs(2026, 9, 15, 9, 15, 0),
	}
	if len(stamps) != 3 || cancels != 1 {
		t.Fatalf("5m arm ticked %d times (cancels=%d), want 3 (2 holds + 1 cancel) — stamps %v", len(stamps), cancels, stamps)
	}
	for i, w := range want {
		if stamps[i] != w {
			t.Fatalf("5m arm tick %d at %d, want %d (the %dth closed 5m candle)", i, stamps[i], w, i+2)
		}
	}
}

// TestTwoMinuteArmUnchanged pins that the per-TF fix does not move the 2m arm:
// a 2m arm still advances exactly once per CLOSED 2m candle — holds at 09:02
// and 09:04, cancel at 09:06 — the pre-m406 behaviour, byte-identical.
func TestTwoMinuteArmUnchanged(t *testing.T) {
	stamps, cancels := driveArmTicks(t, 2, 8)
	want := []int64{
		auditMs(2026, 9, 15, 9, 2, 0),
		auditMs(2026, 9, 15, 9, 4, 0),
		auditMs(2026, 9, 15, 9, 6, 0),
	}
	if len(stamps) != 3 || cancels != 1 {
		t.Fatalf("2m arm ticked %d times (cancels=%d), want 3 (2 holds + 1 cancel) — stamps %v", len(stamps), cancels, stamps)
	}
	for i, w := range want {
		if stamps[i] != w {
			t.Fatalf("2m arm tick %d at %d, want %d (the %dth closed 2m candle)", i, stamps[i], w, i+2)
		}
	}
}
