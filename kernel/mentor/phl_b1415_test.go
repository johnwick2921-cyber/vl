package mentor

import (
	"testing"

	"vl/market"
)

// B15 (CTO 20:48:40Z, D3.3 p1 @05:18-05:34): the PHL target is the FIRST
// obstacle in the way — the nearest level beyond the entry (the same
// nextLevelBeyond the ISB and box paths use), capped at the old extreme
// minus PHLTargetShyPts. The room rule and the 1:1 floor are then measured
// to that target.
//
// These tests pin the levels-aware core (PHLPLHR2Levels / phlTarget). The
// eval.go call-site patch (DS-103) switches the evaluator to it.

// A key level between the entry and the old extreme becomes the target.
// MUTANT: drop the obstacle cap in phlTarget → Target comes back as the old
// extreme minus the shy → this test goes RED.
func TestPHLPLHR2LevelsFirstObstacleTarget(t *testing.T) {
	cfg := workedCfg()
	levels := []Level{
		{Key: "key_level:29420", Kind: KindKeyLevel, Price: 29_420},
	}
	in, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, cfg)
	if !ok {
		t.Fatalf("first-obstacle PHL refused: %s", reason)
	}
	if in.Target != 29_420 {
		t.Fatalf("target = %.2f, want the key level 29420 in the way", in.Target)
	}
}

// A key level BEYOND the old extreme minus the shy does not change the
// target — the cap holds ("gần đỉnh cũ", D2.2 p1 @06:11).
func TestPHLPLHR2LevelsCapAtExtremeShy(t *testing.T) {
	cfg := workedCfg()
	levels := []Level{
		{Key: "key_level:29430", Kind: KindKeyLevel, Price: 29_430},
	}
	in, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, cfg)
	if !ok {
		t.Fatalf("capped PHL refused: %s", reason)
	}
	if in.Target != 29_425.75 {
		t.Fatalf("target = %.2f, want the capped 29425.75 (extreme minus shy)", in.Target)
	}
}

// The room rule is measured to the OBSTACLE, not the old extreme (D5.3 p1
// @09:16-10:13: entry→target must be ≥ 2× the take).
// MUTANT: measure the room to the old extreme minus the shy instead → the
// setup ships → this test goes RED.
func TestPHLPLHR2LevelsRoomToObstacle(t *testing.T) {
	cfg := workedCfg()
	levels := []Level{
		{Key: "key_level:29410", Kind: KindKeyLevel, Price: 29_410}, // reward 14.25 < 2×8.25
	}
	if _, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, cfg); ok {
		t.Fatalf("room to the obstacle must refuse; got ok, reason=%q", reason)
	}
}

// The 1:1 floor is measured to the obstacle too (D1.2 p1 @07:48).
// MUTANT: apply the floor to the old-extreme target instead → the setup
// ships → this test goes RED.
func TestPHLPLHR2LevelsFloorToObstacle(t *testing.T) {
	cfg := workedCfg()
	cfg.RoomMultiple = 1 // isolate the floor
	levels := []Level{
		{Key: "key_level:29401", Kind: KindKeyLevel, Price: 29_401}, // reward 5.25 < risk 8.25
	}
	if _, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, cfg); ok || reason == "" {
		t.Fatalf("obstacle below the 1:1 floor must refuse; ok=%v reason=%q", ok, reason)
	}
}

// The short mirror: a PLH with a key level between the entry and the old low
// targets that level.
func TestPHLPLHR2LevelsShortObstacle(t *testing.T) {
	cfg := workedCfg()
	shortTouch := workedTouch()
	shortTouch.ApproachedFrom = SideShort
	shortTouch.RefBar = market.Kline{Open: 29_410, High: 29_412, Low: 29_407, Close: 29_410} // sell stop at the low
	// short: entry = RefBar.Low − 1.0 = 29_406, stop = RefBar.High = 29_412
	// (risk 6), old extreme below (old low), key level 29_390 in the way.
	levels := []Level{
		{Key: "key_level:29390", Kind: KindKeyLevel, Price: 29_390},
	}
	in, ok, reason := PHLPLHR2Levels(shortTouch, Level{Kind: KindOldExtreme, Price: 29_380}, 0, 3, 29_415, levels, cfg)
	if !ok {
		t.Fatalf("short obstacle PHL refused: %s", reason)
	}
	if in.Target != 29_390 {
		t.Fatalf("short target = %.2f, want the key level 29390 in the way", in.Target)
	}
}

// B14 (CTO 20:48:40Z, D2.2 p3 @03:13-04:33): the higher-low check must run
// against the LEFT same-role-as-the-STOP swing (the left LOW for a long).
// This test pins the contract at the function level: given the left low the
// setup ships; given the left HIGH (the current call site's bug — it passes
// the prior same-role-as-the-TARGET swing) a valid higher low is refused.
// The eval.go fix is DS-103's patch; this documents what it must pass.
func TestPHLPLHR2PriorSwingMustBeTheLeftLow(t *testing.T) {
	cfg := workedCfg()
	// stop = 29,387.5. The left low is 29,380 → a higher low → ships.
	if _, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, nil, cfg); !ok {
		t.Fatalf("higher low against the LEFT low must ship: %s", reason)
	}
	// The left HIGH (29,431.75, the target's role) must NOT be passed as
	// priorSwing — doing so refuses this valid higher low.
	if _, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_431.75, nil, cfg); ok {
		t.Fatalf("priorSwing = the left HIGH must refuse (it is the bug); got ok, reason=%q", reason)
	}
}
