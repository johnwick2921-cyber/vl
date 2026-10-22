package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
)

// TestMentorArmIntentPersistsTheSnapshot pins m134 DS-108 (a) at the PRODUCTION
// seam: mentorArmIntent writes the evaluator snapshot to the armed_orders row's
// mentor_snapshot_json, and the row reads it BACK. MUTANT: drop the
// MentorSnapshot assignment (or never read at.mentorEval) → the row's snapshot
// is NULL → RED. This replaces the old TestArmSnapshotJSON, which only marshaled
// the inner helper and could pass even if the write were never wired.
func TestMentorArmIntentPersistsTheSnapshot(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	at.mentorEval = &mentor.Evaluator{State: mentor.State{EMA9: 29901.5, EMA34: 29850.25}}

	now := b3Clock(10, 30)
	armID := "snap-readback"
	at.mentorArmIntent(
		mentor.Intent{
			Action: mentor.PlaceStopLimitEntry, ArmID: armID,
			Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29620,
			Setup: "ISB", ExpiryMs: now.UnixMilli() + 60_000,
		},
		mentorSizeChoice{Contracts: 1, Tier: "base"},
		now.UnixMilli(), now.UnixMilli(), "B", 0,
	)

	live, ok := at.mentorLiveArmFor(armID)
	if !ok {
		t.Fatalf("arm %q not in the live registry", armID)
	}
	row := readMentorArmRowByID(t, ledger, live.RowID)
	if row.MentorSnapshot == nil {
		t.Fatal("the armed_orders row must carry a non-NULL mentor_snapshot_json")
	}
	if !strings.Contains(*row.MentorSnapshot, `"ema9":29901.5`) {
		t.Fatalf("the row's snapshot must read back the evaluator value it was authored under:\n%s", *row.MentorSnapshot)
	}
}
