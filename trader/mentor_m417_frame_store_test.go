package trader

import (
	"testing"

	"vl/kernel/mentor"
)

// TestMentorArmIntentStoresFrameOnlyWhenKnown pins m417 F1 at the production
// seam (mentorArmIntent): the entry's ExecTFMin is stored to mentorExecFrames
// only when it is KNOWN (1, 2 or 5). ExecTFMin == 0 — a box/ORB/trendline
// entry, or a 1m read whose authoring site did not stamp — DELETES the key so
// the X8 trap-dodge falls back to the 2m (the pre-m409 behaviour), instead of
// reading the 1m on a 2m-drawn box at 11:00 CT.
// MUTANT: make the 0 branch Store(1) again → the first case goes RED.
func TestMentorArmIntentStoresFrameOnlyWhenKnown(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	now := b3Clock(10, 30)
	cases := []struct {
		name       string
		tf         int
		wantVal    int
		wantExists bool
	}{
		{"unknown frame 0 deletes", 0, 0, false},
		{"explicit 1m stores 1", 1, 1, true},
		{"2m stores 2", 2, 2, true},
		{"5m stores 5", 5, 5, true},
	}
	var keys []string
	for _, tc := range cases {
		armID := "m417-" + tc.name
		at.mentorArmIntent(mentor.Intent{
			Action: mentor.PlaceStopLimitEntry, ArmID: armID,
			Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29620,
			Setup: "ISB", ExpiryMs: now.UnixMilli() + 60_000,
			ExecTFMin: tc.tf,
		}, mentorSizeChoice{Contracts: 1, Tier: "base"},
			now.UnixMilli(), now.UnixMilli(), "B", 0)
		key := mentorRunnerTargetKey(at.id, at.mentorScenarioFor(armID))
		keys = append(keys, key)
		v, ok := mentorExecFrames.Load(key)
		if ok != tc.wantExists {
			t.Fatalf("%s: present=%v want %v", tc.name, ok, tc.wantExists)
		}
		if ok && v.(int) != tc.wantVal {
			t.Fatalf("%s: stored %d want %d", tc.name, v.(int), tc.wantVal)
		}
	}
	for _, k := range keys {
		mentorExecFrames.Delete(k)
	}
}
