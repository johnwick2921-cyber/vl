package ninjatrader

import (
	"bytes"
	"encoding/json"
	"testing"

	ntwire "vl/provider/ninjatrader"
)

// ── release #31 (leg-3 bracket) Go wire pins, production call sites ─────────

// TestLeg3FiveContractSplit — 5 contracts split 3/1/1: leg1=3 @ +1R, leg2=1 at
// the first line beyond the leg-1 exit, leg3=1 trails to the trade target. The
// frame carries leg2_qty/leg2_tp and the sent split records them. Mutant: drop
// leg2 from the frame (or the record) → RED.
func TestLeg3FiveContractSplit(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-pin"
	// entry 100, stop 98 (R=2) → leg1=3 @ +1R=102; leg2=1 @ 104 (a line strictly
	// between 102 and the target 106); leg3=1 @ 106.
	sid, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 104)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty != 3 || p.Leg1TP != 102 {
		t.Fatalf("frame leg1 = (%d, %.2f), want (3, 102)", p.Leg1Qty, p.Leg1TP)
	}
	if p.Leg2Qty != 1 || p.Leg2TP != 104 {
		t.Fatalf("frame leg2 = (%d, %.2f), want (1, 104)", p.Leg2Qty, p.Leg2TP)
	}
	got, ok := tr.SplitSentFor(sid)
	if !ok || got.Leg1Qty != 3 || got.Leg1TP != 102 || got.Leg2Qty != 1 || got.Leg2TP != 104 {
		t.Fatalf("SplitSentFor = (%+v, %v), want the split the frame carried", got, ok)
	}
}

// TestLeg3OldAddonBuildDropsToTwoPairPlan — an AddOn that proves the 2-pair
// build but not the leg-3 build gets leg2 zeroed (the 2-pair plan), never a
// frame it would misread. Mutant: drop the MinAddonBuildLeg3 gate → leg2 on the
// wire → RED.
func TestLeg3OldAddonBuildDropsToTwoPairPlan(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-old"
	if _, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 104); err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg2_qty")) || bytes.Contains(frame, []byte("leg2_tp")) {
		t.Fatalf("an AddOn below the leg-3 build must get the 2-pair plan, got %s", frame)
	}
}

// TestLeg3TPOutOfRangeDropsLeg2 — a leg-2 TP not strictly between leg 1's TP and
// the trade target drops ONLY leg 2 (the 2-pair plan stands). Mutant: no range
// check → leg2 on the wire → RED.
func TestLeg3TPOutOfRangeDropsLeg2(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-range"
	if _, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102, 1, 107); err != nil { // 107 >= target 106
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg2_qty")) || bytes.Contains(frame, []byte("leg2_tp")) {
		t.Fatalf("a leg-2 TP at/past the trade target must drop leg 2, got %s", frame)
	}
}

// TestLeg3ShortSideRoundsAndSends — the short side: entry 100, stop 102 (R=2),
// target 94 → leg1=3 @ 98, leg2=1 @ 96 (a line strictly between 98 and 94),
// leg3=1 @ 94. Mirrors the long pin so the side math is not one-sided.
func TestLeg3ShortSideRoundsAndSends(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "leg3-short"
	if _, err := tr.PlaceStopEntry("MNQ", "short", 5, 100, 102, 94, 3, 98, 1, 96); err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty != 3 || p.Leg1TP != 98 || p.Leg2Qty != 1 || p.Leg2TP != 96 {
		t.Fatalf("frame = leg1(%d, %.2f) leg2(%d, %.2f), want leg1(3, 98) leg2(1, 96)", p.Leg1Qty, p.Leg1TP, p.Leg2Qty, p.Leg2TP)
	}
}
