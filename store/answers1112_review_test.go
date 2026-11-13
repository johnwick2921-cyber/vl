package store

import (
	"strings"
	"testing"
)

// TestAnswers1112ReviewPHLRoomNoteNamesConfluenceRoom — answers-1112 review
// (2026-11-12, P2): at the phl_room_multiple default 1 a CONFLUENCE PHL/PLH
// asks 1 × its 2R leg 1 = 2R (Sept D3.4 p3 "at least 1-2"); at the revert value
// 2 it asks 4R. The note said "still asks 2x the 2R leg 1" (= 4R) — prose about
// code where nothing compared it to the code. The kernel pin is
// kernel/mentor TestAnswers1112ReviewConfluencePHLRoomIsTwoR.
func TestAnswers1112ReviewPHLRoomNoteNamesConfluenceRoom(t *testing.T) {
	e, ok := knobRegistry["risk_control.mentor_tuning.phl_room_multiple"]
	if !ok {
		t.Fatal("phl_room_multiple missing from the registry")
	}
	if strings.Contains(e.Note, "still asks 2x") {
		t.Fatalf("note still claims the confluence room is unchanged (2x the 2R leg 1 = 4R); at the default 1 it is 2R: %q", e.Note)
	}
	if !strings.Contains(e.Note, "confluence PHL/PLH asks 1 × its 2R leg 1 = 2R") || !strings.Contains(e.Note, "4R at 2") {
		t.Fatalf("note must name the confluence room at the default (2R) and at the revert value (4R): %q", e.Note)
	}
}
