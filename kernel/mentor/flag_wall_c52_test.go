package mentor

import "testing"

// c52 W9 — flag wall (B21, B22) and the trendline first-test preference (B7-R7).

func flatLine(side Side, px float64, tfMin int, dead bool) Trendline {
	return Trendline{Side: side, P0Px: px, P0T: 0, P1Px: px, P1T: 600_000, TFMin: tfMin, Dead: dead}
}

// B21 — "joining the two trendlines gives one flag; the target is the upper edge
// of THAT flag" [Cách vẽ trendline/part1 @00:05:04–00:06:06]. On a multi-line
// day the flag the entry sits in is the NEAREST live pair, not the oldest pair
// of the day. Built through buildFlagWalls + wallFor (the production calls in
// Tick's leave-defer). MUTANT: restore "keep the FIRST support / resistance" in
// buildFlagWalls → the old pair 80/130 wins, the wall is 130, not 106 → RED.
func TestC52W9B21NearestFlagWall(t *testing.T) {
	const bt = int64(600_000)
	fw := buildFlagWalls([]Trendline{
		flatLine(SideLong, 80, 0, false),   // oldest pair of the day
		flatLine(SideShort, 130, 0, false), // oldest pair of the day
		flatLine(SideLong, 95, 0, false),   // the flag price sits in now
		flatLine(SideShort, 106, 0, false),
		flatLine(SideLong, 98, 0, true), // broken: deleted, never a wall
		flatLine(SideShort, 101, 0, true),
	})
	if w, ok := fw.wallFor(100, SideLong, bt); !ok || w != 106 {
		t.Fatalf("long wall = %v ok=%v, want the NEAREST live resistance 106 (not the oldest 130)", w, ok)
	}
	if w, ok := fw.wallFor(100, SideShort, bt); !ok || w != 95 {
		t.Fatalf("short wall = %v ok=%v, want the NEAREST live support 95 (not the oldest 80)", w, ok)
	}
	// Outside the nearest flag but inside the older one: the nearest resistance
	// above 108 is 130 and the nearest support below is 106? no — 106 is a
	// resistance. Support below 108 is 95: still a flag [95,130].
	if w, ok := fw.wallFor(108, SideLong, bt); !ok || w != 130 {
		t.Fatalf("above the inner flag the next live resistance 130 is the wall, got %v ok=%v", w, ok)
	}
}

// B21 — the traded-frame lines are preferred over a persistent 1H/4H line; an
// HTF line is a wall only when no traded-frame line of that side brackets the
// entry. MUTANT: drop the frame preference (take the nearest of ALL) → the 4H
// line at 103 wins over the 1m resistance at 106 → RED.
func TestC52W9B21PrefersTradedFrameOverHTF(t *testing.T) {
	const bt = int64(600_000)
	fw := buildFlagWalls([]Trendline{
		flatLine(SideLong, 95, 0, false),
		flatLine(SideShort, 106, 0, false),
		flatLine(SideShort, 103, 240, false), // nearer, but 4H
	})
	if w, ok := fw.wallFor(100, SideLong, bt); !ok || w != 106 {
		t.Fatalf("wall = %v ok=%v, want the traded-frame resistance 106, not the 4H 103", w, ok)
	}
	// No traded-frame resistance above the entry: the 4H line is the wall.
	fw2 := buildFlagWalls([]Trendline{
		flatLine(SideLong, 95, 0, false),
		flatLine(SideShort, 103, 240, false),
	})
	if w, ok := fw2.wallFor(100, SideLong, bt); !ok || w != 103 {
		t.Fatalf("no traded-frame resistance: the 4H line 103 is the wall, got %v ok=%v", w, ok)
	}
	// A traded-frame resistance BELOW the entry does not bracket it: HTF used.
	fw3 := buildFlagWalls([]Trendline{
		flatLine(SideLong, 95, 0, false),
		flatLine(SideShort, 99, 0, false),
		flatLine(SideShort, 110, 60, false),
	})
	if w, ok := fw3.wallFor(100, SideLong, bt); !ok || w != 110 {
		t.Fatalf("1m resistance 99 is below the entry; the 1H 110 is the wall, got %v ok=%v", w, ok)
	}
}

// B22 — the flag-target sentence is about a POTENTIAL HIGH/LOW order only
// [Cách vẽ trendline/part1 @00:05:47–00:06:12]. ISB, BOX, ORB-ISB and SWING4H
// stop entries inside a live flag are neither retargeted nor refused for room;
// PHL / PLH (and a reverse ISB retagged PHL/PLH) still are. MUTANT: drop the
// flagWallSetup filter in applyFlagWallTarget → the ISB/BOX/SWING4H intents
// are cut to 106 / refused → RED.
func TestC52W9B22FlagWallOnlyOnPHLPLH(t *testing.T) {
	const bt = int64(600_000)
	fw := buildFlagWalls([]Trendline{
		flatLine(SideLong, 95, 0, false),
		flatLine(SideShort, 106, 0, false),
	})
	cfg := DefaultConfig()
	mk := func(setup string, stop float64) Intent {
		return Intent{Action: PlaceStopEntry, Side: SideLong, Setup: setup, Price: 100, Stop: stop, Target: 120}
	}
	var refused []string
	ref := func(r string) { refused = append(refused, r) }

	for _, setup := range []string{"ISB", "BOX", "SWING4H", "ORB"} {
		// stop 90 → a 106 wall would leave < 1:1 and refuse the entry if it applied.
		got := applyFlagWallTarget([]Intent{mk(setup, 90)}, fw, bt, cfg, ref)
		if len(got) != 1 || got[0].Target != 120 {
			t.Fatalf("%s: must keep its own target 120 and not be refused, got %+v", setup, got)
		}
	}
	if len(refused) != 0 {
		t.Fatalf("non-PHL/PLH setups must record no flag_wall_* refusal, got %v", refused)
	}
	for _, setup := range []string{"PHL", "PLH"} {
		in := mk(setup, 97)
		if setup == "PLH" {
			in = Intent{Action: PlaceStopEntry, Side: SideShort, Setup: "PLH", Price: 100, Stop: 103, Target: 80}
		}
		got := applyFlagWallTarget([]Intent{in}, fw, bt, cfg, ref)
		if len(got) != 1 || (setup == "PHL" && got[0].Target != 106) || (setup == "PLH" && got[0].Target != 95) {
			t.Fatalf("%s: the flag wall must still retarget, got %+v", setup, got)
		}
	}
}
