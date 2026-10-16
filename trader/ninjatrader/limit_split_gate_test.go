package ninjatrader

import (
	"bytes"
	"encoding/json"
	"testing"

	ntwire "vl/provider/ninjatrader"
)

// ── dispatch-31 fold-564-b D3 — the LIMIT path's two fail-closed split gates ─
// placeLimitEntry mirrors placeStopEntry's split handling, but the new copies
// were unpinned. These are the production-call-site pins: M564-17 (the build
// floor) and M564-18 (the wireLeg1TP profit-side check) must go RED.

// TestLimitSplitWiresWhenProven — with the split-legs build proven, the limit
// entry carries the leg-1 split and records it (the positive pin the other two
// tests are the negative of).
func TestLimitSplitWiresWhenProven(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "limit-split"
	sid, err := tr.PlaceLimitEntryWithSplit("MNQ", "long", 4, 100, 98, 110, 2, 105)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Quantity != 4 || p.Leg1Qty != 2 || p.Leg1TP != 105 {
		t.Fatalf("frame = qty %d leg1 %d@%.2f, want 4/2/105", p.Quantity, p.Leg1Qty, p.Leg1TP)
	}
	if got, ok := tr.SplitSentFor(sid); !ok || got.Leg1Qty != 2 || got.Leg1TP != 105 {
		t.Fatalf("SplitSentFor = (%+v, %v), want the split the frame carried", got, ok)
	}
}

// TestLimitSplitDroppedWhenBuildNotProven — M564-17. A far side that proves only
// the stop-slot build (2026-09-05-g2, below the split-legs floor 2026-10-04-d1)
// must NOT receive the two-OCO-pair frame: the split degrades to the single
// bracket and nothing is recorded.
func TestLimitSplitDroppedWhenBuildNotProven(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildStopSlot)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "limit-noproof"
	sid, err := tr.PlaceLimitEntryWithSplit("MNQ", "long", 4, 100, 98, 110, 2, 105)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg1_qty")) || bytes.Contains(frame, []byte("leg1_tp")) {
		t.Fatalf("an unproven build must drop the split, got %s", frame)
	}
	if _, ok := tr.SplitSentFor(sid); ok {
		t.Fatal("a dropped split must not be recorded")
	}
}

// TestLimitSplitDroppedWhenLeg1TPWrongSide — M564-18. A leg-1 TP on the loss
// side of the limit (short, leg1 101 above the entry 100) drops the split —
// the single bracket goes out, and nothing is recorded (never a leg 1 that
// exits at a 1-tick loss on fill).
func TestLimitSplitDroppedWhenLeg1TPWrongSide(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildSplitLegs)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "limit-wrongside"
	sid, err := tr.PlaceLimitEntryWithSplit("MNQ", "short", 4, 100, 102, 94, 2, 101)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	if bytes.Contains(frame, []byte("leg1_qty")) || bytes.Contains(frame, []byte("leg1_tp")) {
		t.Fatalf("a short leg-1 TP above the entry must drop the split, got %s", frame)
	}
	if _, ok := tr.SplitSentFor(sid); ok {
		t.Fatal("a dropped split must not be recorded")
	}
}
