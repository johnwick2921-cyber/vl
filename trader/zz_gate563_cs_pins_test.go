package trader

// release #31 (leg-3 bracket) — C# mutant pins for the surviving gate #563
// mutants. The AddOn is the other half of the wire contract; these pins read
// the .cs so a drift (or an anchored mutant) fails this suite even though NT8's
// compile only happens on the owner's F5.

import (
	"strings"
	"testing"
)

// M27 — the leg-3 stop must join the leg-3 OCO group (exitOco3), never leg 2's
// (exitOco2): one stop firing in leg 2's group would cancel the OTHER leg-3
// order and leave a contract naked.
func TestG563AddonLeg3StopInItsOwnOcoGroup(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"TimeInForce.Gtc, leg3Qty, 0, b.Sl, exitOco3, signalId + \"-sl3\",",
		"TimeInForce.Gtc, leg3Qty, b.Tp, 0, exitOco3, signalId + \"-tp3\",",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the leg-3 OCO group: missing %q", want)
		}
	}
}

// M28 — leg 2's partial is capped at the frame's Leg2Qty (never the whole
// runner), so the remainder can split into leg 3.
func TestG563AddonLeg2PartialCappedByFrame(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "int leg2Partial = (b.Leg2Qty > 0 && b.Leg2Tp > 0) ? Math.Min(b.Leg2Qty, leg2Qty) : 0;") {
		t.Error("AddOn lost the frame-capped leg-2 partial: the whole runner rides leg 2")
	}
}

// M29 — leg 3's TP is the trade target (b.Tp), never the leg-2 line.
func TestG563AddonLeg3TPAtTradeTarget(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "TimeInForce.Gtc, leg3Qty, b.Tp, 0, exitOco3, signalId + \"-tp3\",") {
		t.Error("AddOn lost the leg-3 TP at the trade target")
	}
}

// M30 — AmendBracketQuantity must keep splitting the runner into leg 2 + leg 3
// on a later fill.
func TestG563AddonAmendSplitsLeg3(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "int leg2Partial = (pb.Leg2Tp > 0) ? Math.Min(pb.Leg2Qty, leg2Qty) : leg2Qty;") {
		t.Error("AddOn lost the amend leg-3 split")
	}
}

// M31 — BOTH netting-flat sweep sites must cancel leg 3's pair (a leg-3 stop
// left resting would close a position the bot believes flat).
func TestG563AddonNettingFlatSweepCancelsLeg3(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"if (pb.SlOrder3 != null) toCancel.Add(pb.SlOrder3);",
		"if (pb.TpOrder3 != null) toCancel.Add(pb.TpOrder3);",
	} {
		if n := strings.Count(src, want); n != 2 {
			t.Errorf("AddOn lost a leg-3 cancel from the netting-flat sweeps: %q appears %d times, want 2", want, n)
		}
	}
}

// M32 — a leg-3 exit must clear its bracket refs (a dead -sl3/-tp3 would keep
// the record "live" and block the all-flat record removal).
func TestG563AddonLeg3ExitClearsRefs(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "if (isLeg3) { pb.SlOrder3 = null; pb.TpOrder3 = null; }") {
		t.Error("AddOn lost the leg-3 exit ref clear")
	}
}

// D2 — BOTH strip chains test -sl3/-tp3 (strip 4) BEFORE -sl/-tp, so a leg-3
// close reports the trade's one signal id with its reason.
func TestG563AddonStripsLeg3InBothChains(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		`if (signalId.EndsWith("-sl3")) { exitReason = "sl"; signalId = signalId.Substring(0, signalId.Length - 4); }`,
		`if (signalId.EndsWith("-tp3")) { exitReason = "tp"; signalId = signalId.Substring(0, signalId.Length - 4); }`,
		`if (signalId.EndsWith("-sl3") || signalId.EndsWith("-tp3"))`,
		`if (orderName.EndsWith("-sl3") || orderName.EndsWith("-tp3"))`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the leg-3 strip: missing %q", want)
		}
	}
}

// D10 — a leg 2 that already exited must never be created or amended to a
// zero-qty -sl2/-tp2; the whole remainder rides leg 3.
func TestG563AddonNoZeroQtyLeg2AfterExit(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "if (leg2Partial > 0)") {
		t.Error("AddOn lost the zero-qty leg-2 guard")
	}
}
