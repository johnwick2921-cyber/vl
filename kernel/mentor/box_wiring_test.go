package mentor

import (
	"testing"

	"vl/market"
)

// TestBoxEdgeIsALocation — BOX RULING part 2: a box edge is one of the four
// setup locations, "used again and again". Both the ISB-side verdict and the
// PHL/PLH-side level check must accept the edges.
func TestBoxEdgeIsALocation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	levels := []Level{
		{Key: "ftgh:100:90:top", Kind: KindFTGHEdge, Price: 100},
		{Key: "ftgl:100:90:bottom", Kind: KindFTGLEdge, Price: 90},
	}
	ref := market.Kline{High: 100.2, Low: 99.8}
	if ok, where := LocationVerdict(ref, levels, TriggerLine{}, nil, cfg); !ok || where != "box_edge" {
		t.Fatalf("box edge must be a location: ok=%v where=%q", ok, where)
	}
	if !levelIsLocation(levels[0], levels) || !levelIsLocation(levels[1], levels) {
		t.Fatal("box edges must pass levelIsLocation")
	}
}

// TestBoxBanFilter — BOX RULING part 2: "NEVER trade inside the box, neither
// the candle nor your entry point" [D3.2 p1 @ 06:59]. Entries whose price, or
// whose reference candle close, sits inside a box are dropped.
func TestBoxBanFilter(t *testing.T) {
	boxes := []Box{{Kind: FTGH, Top: 100, Bottom: 90, Key: "ftgh:100:90"}}
	cur := market.Kline{Close: 95} // the candle is INSIDE the box
	ints := []Intent{
		{Action: PlaceStopEntry, Price: 97},                 // entry inside the box
		{Action: PlaceStopLimitEntry, Price: 92, Limit: 92}, // stop-limit inside
		{Action: PlaceStopEntry, Price: 101},                // entry outside, candle inside
		{Action: CancelArm, Reason: "test"},                 // cancels are never banned
	}
	got, _ := boxBanFilter(ints, boxes, cur)
	if len(got) != 1 || got[0].Action != CancelArm {
		t.Fatalf("inside-box entries must all be dropped, cancels kept: %+v", got)
	}
	// no boxes at all: everything passes through
	if got, _ := boxBanFilter(ints, nil, cur); len(got) != len(ints) {
		t.Fatalf("no boxes must ban nothing: %d vs %d", len(got), len(ints))
	}
}

// TestBoxEdgeExemptFromInvalidLevel — BOX RULING part 2: the invalid-level
// rule does NOT apply to box edges. A wrong-way close at a box edge emits no
// level_invalid and never marks the edge ISB-only; at a key level the same
// close does both.
func TestBoxEdgeExemptFromInvalidLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	wrongWay := []Intent{
		{Action: CancelArm, Reason: "close through"},
		{Action: LevelInvalid, LevelKey: "x"},
	}
	e := New(cfg)
	edge := Level{Key: "ftgh:100:90:top", Kind: KindFTGHEdge, Price: 100}
	// A resting order at each level, so the wrong-way cancel binds to it (an
	// id-less cancel with nothing resting is dropped — CTO replay-noise fix).
	e.State.LevelArms = map[string]LevelArm{
		edge.Key:          {ArmID: "lvl-1"},
		"key_level:100:1": {ArmID: "lvl-2"},
	}
	out := handleTouchIntents(e, edge, wrongWay)
	if len(out) != 1 || out[0].Action != CancelArm || out[0].ArmID != "lvl-1" {
		t.Fatalf("box edge must not emit level_invalid: %+v", out)
	}
	if e.State.ISBOnly[edge.Key] {
		t.Fatal("box edge must never be marked ISB-only")
	}
	key := Level{Key: "key_level:100:1", Kind: KindKeyLevel, Price: 100}
	out = handleTouchIntents(e, key, wrongWay)
	if len(out) != 2 || !e.State.ISBOnly[key.Key] {
		t.Fatalf("key level wrong-way must emit level_invalid + ISB-only: %+v state=%v", out, e.State.ISBOnly)
	}
}

// CTO (release 10-05-1, replay noise): a wrong-way touch with NO resting order
// at the level emits no CancelArm (nothing to cancel); with one resting, the
// cancel carries that order's ArmID. Mutant: keep the id-less cancel → RED.
func TestWrongWayCancelBindsToTheRestingOrderOrIsDropped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	wrongWay := []Intent{
		{Action: CancelArm, Reason: "close through"},
		{Action: LevelInvalid, LevelKey: "k"},
	}
	key := Level{Key: "key_level:100:1", Kind: KindKeyLevel, Price: 100}
	e := New(cfg)
	out := handleTouchIntents(e, key, wrongWay)
	if len(out) != 1 || out[0].Action != LevelInvalid {
		t.Fatalf("nothing resting: only level_invalid may go out, got %+v", out)
	}
	e2 := New(cfg)
	e2.State.LevelArms = map[string]LevelArm{key.Key: {ArmID: "lvl-9"}}
	out = handleTouchIntents(e2, key, wrongWay)
	if len(out) != 2 || out[0].Action != CancelArm || out[0].ArmID != "lvl-9" {
		t.Fatalf("a resting order: the cancel must carry its ArmID, got %+v", out)
	}
}
