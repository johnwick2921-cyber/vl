package mentor

import (
	"testing"
	"time"
)

// D2-G6 + reread D2.3b-F3 (oct round 2) — the WE12 cancel demo, Thu 10-29
// 21:10-21:22 CT [OctD2.3-R69 @ 1:18:10, frames s_04713 / s_04718 / s_04720]:
// the old-high ray 28,876.50 (the 20:58 swing high, H 28,876.25) is broken by
// a green candle whose body CLOSES above it (the 21:12 candle, C 28,880.75;
// the next bar opens 28,880.25). "Nó break cái đỉnh này rồi, thì [lower high]
// gì tầm này nữa" — the PLH ladder is over; the bot used to carry the ladder
// on and refuse (or arm) rung by rung. Real stored 1m bars, the production
// rollingPHLTick.
func TestD2G6LeftExtremeCloseEndsTheLadder(t *testing.T) {
	bars := m413Load(t, "testdata/round2_mnq_1m_20261029_1940ct.csv")
	from := phl1At(t, bars, 2026, time.October, 29, 21, 5)
	broke := phl1At(t, bars, 2026, time.October, 29, 21, 12)
	to := phl1At(t, bars, 2026, time.October, 29, 21, 22)

	cfg := DefaultConfig()
	e := phl1Evaluator(cfg, SideShort, bars[from].OpenTime, bars[to].Close)
	steps := phl1RunRolling(e, bars, from, to)
	named := false
	for i, s := range steps {
		idx := from + i
		for _, r := range s.refuse {
			if r == "rolling_left_extreme_broken" {
				named = true
				if idx < broke {
					t.Fatalf("%s: named before the closing break (21:12); ladder:%s", s.at, phl1Dump(steps))
				}
			}
		}
		if idx >= broke && len(s.places) > 0 {
			t.Fatalf("%s: a rung armed after the left high 28,876.25 was broken on a close; ladder:%s", s.at, phl1Dump(steps))
		}
	}
	if !named {
		t.Fatalf("the break of the left high must end the ladder with the named refusal rolling_left_extreme_broken; ladder:%s\nrefusals=%v", phl1Dump(steps), e.State.Refusals)
	}
	if st := e.State.RollingPHL; st.Ladder || st.ArmID != "" {
		t.Fatalf("the ladder must be over: %+v", st)
	}

	// Knob OFF = today: the name never appears.
	off := DefaultConfig()
	off.RollingLeftBreakCancel = false
	e2 := phl1Evaluator(off, SideShort, bars[from].OpenTime, bars[to].Close)
	for _, s := range phl1RunRolling(e2, bars, from, to) {
		for _, r := range s.refuse {
			if r == "rolling_left_extreme_broken" {
				t.Fatalf("rolling_left_break_cancel=false must not name it (%s)", s.at)
			}
		}
	}
}

// TestD2G6AWickPokeKeepsTheLadder — a wick through the left high that closes
// back below it is not a break: the per-rung refusal stays, the ladder lives
// (the 21:11 candle H 28,881.75 C 28,871.00).
func TestD2G6AWickPokeKeepsTheLadder(t *testing.T) {
	bars := m413Load(t, "testdata/round2_mnq_1m_20261029_1940ct.csv")
	from := phl1At(t, bars, 2026, time.October, 29, 21, 5)
	wick := phl1At(t, bars, 2026, time.October, 29, 21, 11)
	e := phl1Evaluator(DefaultConfig(), SideShort, bars[from].OpenTime, bars[wick].Close)
	for _, s := range phl1RunRolling(e, bars, from, wick) {
		for _, r := range s.refuse {
			if r == "rolling_left_extreme_broken" {
				t.Fatalf("%s: a wick that closes back inside is not a break", s.at)
			}
		}
	}
}
