package mentor

import "testing"

// TestStampLeaveSafetyNet pins the untagged-setup SAFETY NET itself (CTO
// 02:18:38Z FOLD): an untagged ENTRY is dropped named untagged_setup, a
// tagged entry is stamped with StopPts/TargetPts/SpentDay, and a non-entry
// action passes untouched. Mutant: the refusal disabled → RED (the net was
// previously only caught indirectly by the per-site tag assertions).
func TestStampLeaveSafetyNet(t *testing.T) {
	entry := func(action Action, setup string) Intent {
		return Intent{
			Action: action,
			Setup:  setup,
			Price:  100,
			Stop:   95,
			Target: 110,
		}
	}

	out := []Intent{
		entry(PlaceStopEntry, ""),      // untagged stop entry → dropped
		entry(PlaceStopLimitEntry, ""), // untagged limit entry → dropped
		entry(PlaceStopEntry, "PHL"),   // tagged → stamped
		{Action: CancelArm},            // non-entry → untouched
	}

	kept, refusals := stampLeave(out, DaySpent)

	if len(refusals) != 2 || refusals[0] != "untagged_setup" || refusals[1] != "untagged_setup" {
		t.Fatalf("want 2 untagged_setup refusals, got %v", refusals)
	}
	if len(kept) != 2 {
		t.Fatalf("want 2 kept, got %d", len(kept))
	}
	if kept[0].Action != PlaceStopEntry || kept[0].Setup != "PHL" {
		t.Fatalf("kept[0] not the tagged stop entry: %+v", kept[0])
	}
	if kept[0].StopPts != 5 || kept[0].TargetPts != 10 || !kept[0].SpentDay {
		t.Fatalf("tagged entry not stamped: StopPts=%v TargetPts=%v SpentDay=%v",
			kept[0].StopPts, kept[0].TargetPts, kept[0].SpentDay)
	}
	if kept[1].Action != CancelArm {
		t.Fatalf("non-entry action changed: %+v", kept[1])
	}
}
