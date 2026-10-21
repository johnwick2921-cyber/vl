package mentor

import (
	"testing"
)

// TestDropArm (FIX-MENTOR-PHANTOM-ARM) — DropArm removes the evaluator's arm for
// an ArmID whose placement the trader refused as a dead setup: the ISB arm, or
// the swing Pending. It never touches a FILLED swing position, and empty /
// unknown ids are no-ops.
func TestDropArm(t *testing.T) {
	e := New(DefaultConfig())
	e.State.ISBArms["isb-1"] = ISBArm{Inside: 0, Side: SideLong}
	e.State.Swing.Pending = &swingPosition{ArmID: "swing-123", Side: SideShort, Entry: 10100, Stop: 10130}
	e.State.Swing.Pos = &swingPosition{ArmID: "swing-123", Side: SideShort, Entry: 10100, Stop: 10130}

	e.DropArm("isb-1")
	if _, ok := e.State.ISBArms["isb-1"]; ok {
		t.Fatalf("DropArm must delete the ISB arm")
	}
	if e.State.Swing.Pending == nil {
		t.Fatalf("an unrelated DropArm must not touch the swing Pending")
	}

	e.DropArm("swing-123")
	if e.State.Swing.Pending != nil {
		t.Fatalf("DropArm must clear the swing Pending for its ArmID")
	}
	if e.State.Swing.Pos == nil {
		t.Fatalf("DropArm must NEVER clear a FILLED swing position")
	}

	// empty / unknown ids are no-ops.
	e.DropArm("")
	e.DropArm("nope")
	if e.State.Swing.Pos == nil {
		t.Fatalf("DropArm('') must be a no-op")
	}
}
