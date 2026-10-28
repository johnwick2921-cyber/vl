package mentor

import "testing"

// Release #52 B19 — knob orb_escape_completely. The ORB source: the candle
// must be COMPLETELY outside the 2-minute ORB ("Completely nằm ở ngoài…
// Còn không nếu mà nó vẫn còn vòng vòng trong ORB 2 phút mình không trade"
// [extra "Routine trước khi trade" part1 @06:47–07:16]). Default OFF = the
// body test (D4.1 p1 @20:36); ON = wicks included.
//
// Every pin drives the PRODUCTION call site (Evaluator.Tick -> ORBAdvanceMode
// with Cfg.OrbEscapeCompletely).
//
// Named mutants (each run RED, then restored):
//   B19a the knob is ignored  — eval.go
//      `ORBAdvanceMode(prevORB, bars, now, e.Cfg.OrbEscapeCompletely)` →
//      `ORBAdvanceMode(prevORB, bars, now, false)`
//      → TestB19CompletelyOutsideNeedsTheWickOutsideAtTick RED.
//   B19b wick test dropped    — orb.go orbEscapeSide
//      `case c.Low > orb.High:` → `case c.Open > orb.High && c.Close > orb.High:`
//      → TestB19CompletelyOutsideNeedsTheWickOutsideAtTick RED.
//   B19c restart replay ignores the knob — orb.go (draw loop)
//      `orbEscapeSide(orb, b, completely)` → `orbBodyEscape(orb, b)`
//      → TestB19RestartReplayHonoursTheKnob RED.

// TestB19CompletelyOutsideNeedsTheWickOutsideAtTick: ORB 99-101. The 08:32
// candle has its BODY wholly above (open 101.5, close 102) but its lower wick
// dips back to 100.75 (inside). Default: that is an escape (body test).
// Knob ON: NOT an escape; the 08:33 candle (low 101.25, wholly above) is.
// The short side mirrors it.
func TestB19CompletelyOutsideNeedsTheWickOutsideAtTick(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }

	// ---- default OFF: the body test (today's behaviour) ----
	bars := append(orbDayBars(day), clockBar(at(8, 32), 101.5, 102.25, 100.75, 102))
	off := New(clockCfg())
	tickAll(off, bars, 2)
	if off.State.ORB.Escaped != SideLong {
		t.Fatalf("knob OFF: a body wholly above with a wick back inside must still escape long, got %q", off.State.ORB.Escaped)
	}

	// ---- ON: wick-inclusive ----
	cfg := clockCfg()
	cfg.OrbEscapeCompletely = true
	on := New(cfg)
	tickAll(on, bars, 2)
	if !on.State.ORB.Drawn {
		t.Fatalf("ORB not drawn: %+v", on.State.ORB)
	}
	if on.State.ORB.Escaped != "" {
		t.Fatalf("knob ON: a candle whose wick sits inside the ORB is not COMPLETELY outside, escaped %q", on.State.ORB.Escaped)
	}
	long := []Intent{{Action: PlaceStopEntry, Side: SideLong, Price: 102, Reason: "PHL"}}
	if kept, refused := orbGateFilter(long, on.State.ORB, BarCloseInstant(bars[len(bars)-1]), on.Cfg); len(kept) != 0 || len(refused) != 1 || refused[0] != "orb_not_escaped" {
		t.Fatalf("knob ON: the gate must still refuse (orb_not_escaped): kept %+v refused %v", kept, refused)
	}
	bars = append(bars, clockBar(at(8, 33), 101.5, 102.5, 101.25, 102.25)) // low 101.25 > high 101
	on.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	if on.State.ORB.Escaped != SideLong {
		t.Fatalf("knob ON: a candle wholly above (low 101.25 > 101) must escape long, got %q", on.State.ORB.Escaped)
	}

	// ---- short mirror, knob ON ----
	sbars := append(orbDayBars(day), clockBar(at(8, 32), 98.5, 99.25, 97.75, 98)) // body below, upper wick back inside (99.25 > 99)
	s := New(cfg)
	tickAll(s, sbars, 2)
	if s.State.ORB.Escaped != "" {
		t.Fatalf("knob ON short: an upper wick back inside the ORB is not COMPLETELY outside, escaped %q", s.State.ORB.Escaped)
	}
	sbars = append(sbars, clockBar(at(8, 33), 98.5, 98.75, 97.5, 98)) // high 98.75 < 99
	s.Tick(sbars, BarCloseInstant(sbars[len(sbars)-1]))
	if s.State.ORB.Escaped != SideShort {
		t.Fatalf("knob ON short: a candle wholly below (high 98.75 < 99) must escape short, got %q", s.State.ORB.Escaped)
	}
}

// TestB19RestartReplayHonoursTheKnob: a restarted bot draws the ORB and replays
// the escape history in ONE Tick (the draw loop). Bars: 08:32 body-escape with
// a wick inside, then inside candles. Knob ON: still not escaped after the
// replay; knob OFF: long.
func TestB19RestartReplayHonoursTheKnob(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	bars := append(orbDayBars(day),
		clockBar(at(8, 32), 101.5, 102.25, 100.75, 102),
		clockBar(at(8, 33), 101.5, 101.75, 100.5, 101))
	for _, tc := range []struct {
		completely bool
		want       Side
	}{{false, SideLong}, {true, ""}} {
		cfg := clockCfg()
		cfg.OrbEscapeCompletely = tc.completely
		e := New(cfg)
		e.Tick(bars, BarCloseInstant(bars[len(bars)-1])) // ONE tick: draw + replay
		if e.State.ORB.Escaped != tc.want {
			t.Fatalf("completely=%v: restart replay escaped %q, want %q", tc.completely, e.State.ORB.Escaped, tc.want)
		}
	}
}
