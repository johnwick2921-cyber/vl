package ninjatrader

// GATE #563 adversarial probes (reviewer-owned, NOT part of the PR) — the
// production send sites (PlaceStopEntry / MoveStopForSignalLeg /
// ModifyBracketLeg) against a loopback TCP fake.

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// G8 — leg 1 refused by wireLeg1TP (TP on the wrong side) drops the split, but
// leg 2 is checked only `if leg2Qty > 0 && leg1Qty > 0`, so an UNVALIDATED,
// un-rounded leg2_qty/leg2_tp still rides a frame with no leg 1.
func TestG563LegOneDroppedLeavesLegTwoOnTheWire(t *testing.T) {
	s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "g563-l1drop"
	if _, err := tr.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 99, 1, 104.1); err != nil {
		t.Fatalf("place: %v", err)
	}
	frame := readRawFrame(t, raw)
	var p ntwire.SignalPayload
	_ = json.Unmarshal(frame, &p)
	t.Logf("frame leg1=(%d,%.2f) leg2=(%d,%.2f)", p.Leg1Qty, p.Leg1TP, p.Leg2Qty, p.Leg2TP)
	if p.Leg1Qty != 0 {
		t.Fatalf("fixture: leg 1 must be dropped (TP 99 below a long entry 100), got %d", p.Leg1Qty)
	}
	if bytes.Contains(frame, []byte("leg2_qty")) || bytes.Contains(frame, []byte("leg2_tp")) {
		t.Fatalf("DEFECT: leg 1 dropped but leg 2 still on the wire (unvalidated, off-grid): %s", frame)
	}
}

// G9 — the CURRENT live AddOn (2026-10-14-a) treats move_stop leg=3 as
// "leg != 2 → leg 1" AND "leg != 1 → leg 2": it would move BOTH split legs.
// The send sites have no MinAddonBuildLeg3 floor for leg 3.
func TestG563LegThreeMoveRefusedBelowLeg3Build(t *testing.T) {
	s, _, moves := leg3WireServer(t, "2026-10-14-a")
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "g563-oldmove"
	err := tr.MoveStopForSignalLeg("sig-old", "long", 99, 3)
	select {
	case m := <-moves:
		t.Fatalf("DEFECT (defence-in-depth): move_stop leg=%d sent to an AddOn below %s (err=%v) — the old AddOn moves legs 1 AND 2", m.Leg, ntwire.MinAddonBuildLeg3, err)
	case <-time.After(300 * time.Millisecond):
	}
	if err == nil {
		t.Fatalf("a leg-3 move to a pre-leg-3 AddOn must be refused with an error")
	}
}

func TestG563ModifyLegThreeRefusedBelowLeg3Build(t *testing.T) {
	s, mods, _ := leg3WireServer(t, "2026-10-14-a")
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.traderID = "g563-oldmod"
	err := tr.ModifyBracketLeg("sig-old", "long", 3, 0, 105.25)
	select {
	case m := <-mods:
		t.Fatalf("DEFECT (defence-in-depth): modify_bracket leg=%d sent to an AddOn below %s (err=%v) — the old AddOn sets the TP of legs 1 AND 2", m.Leg, ntwire.MinAddonBuildLeg3, err)
	case <-time.After(300 * time.Millisecond):
	}
	if err == nil {
		t.Fatalf("a leg-3 modify to a pre-leg-3 AddOn must be refused with an error")
	}
}

// G10 — leg-2 TP bounds at the send site: AT leg 1's TP and AT the target are
// both refused (strictly between); one tick inside each bound is sent.
func TestG563LegTwoTPBoundsAtTheSendSite(t *testing.T) {
	for _, c := range []struct {
		name  string
		side  string
		entry float64
		stop  float64
		tgt   float64
		l1tp  float64
		l2tp  float64
		sent  bool
		want  float64
	}{
		{"long at leg1", "long", 100, 98, 106, 102, 102, false, 0},
		{"long at target", "long", 100, 98, 106, 102, 106, false, 0},
		{"long one tick in (low)", "long", 100, 98, 106, 102, 102.25, true, 102.25},
		{"long one tick in (high)", "long", 100, 98, 106, 102, 105.75, true, 105.75},
		{"long rounds to nearest", "long", 100, 98, 106, 102, 104.13, true, 104.25},
		{"short at leg1", "short", 100, 102, 94, 98, 98, false, 0},
		{"short at target", "short", 100, 102, 94, 98, 94, false, 0},
		{"short one tick in", "short", 100, 102, 94, 98, 97.75, true, 97.75},
		{"short wrong side", "short", 100, 102, 94, 98, 99, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, raw := rawSignalServer(t, ntwire.MinAddonBuildLeg3)
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			tr.traderID = "g563-bounds"
			if _, err := tr.PlaceStopEntry("MNQ", c.side, 5, c.entry, c.stop, c.tgt, 3, c.l1tp, 1, c.l2tp); err != nil {
				t.Fatalf("place: %v", err)
			}
			var p ntwire.SignalPayload
			if err := json.Unmarshal(readRawFrame(t, raw), &p); err != nil {
				t.Fatal(err)
			}
			if (p.Leg2Qty > 0) != c.sent || (c.sent && p.Leg2TP != c.want) {
				t.Fatalf("leg2=(%d, %.2f), want sent=%v tp=%.2f", p.Leg2Qty, p.Leg2TP, c.sent, c.want)
			}
		})
	}
}
