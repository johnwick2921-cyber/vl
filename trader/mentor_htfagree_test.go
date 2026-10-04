package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// The 20-contract tier needs confluence + 4h AND 1h agreement + room ≥ 2× +
// target ≥ 30 pts. HTFAgree was never set, so 20 was unreachable
// (SETTINGS-VS-LESSONS-1004 P1-2). The evaluator stamps it on the entry intent
// and mentorExtraFor carries it into the size table; this pin runs the real
// call-site path (mentorExtraFor → mentorSizeFor).
func TestMentorBigTierReachableOnlyWhen4hAnd1hAgree(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return true }

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "PHL",
		Price: 21000, Stop: 20988, Target: 21035, StopPts: 12, TargetPts: 35}

	in.HTFAgree = true
	got, err := at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 20 || got.Tier != "big" {
		t.Fatalf("confluence + 4h&1h agree + room 2.9x + target 35: got %d %s, want 20 big", got.Contracts, got.Tier)
	}

	in.HTFAgree = false
	got, err = at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts == 20 || got.Tier != "confluence" {
		t.Fatalf("4h and 1h NOT agreeing: got %d %s, want the confluence tier (10), never 20", got.Contracts, got.Tier)
	}

	// the other tier conditions still hold with agreement: a 29-pt target is not big.
	in.HTFAgree = true
	in.Target, in.TargetPts = 21029, 29
	got, err = at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts == 20 {
		t.Fatalf("agreement alone must not make a 29-pt target big: %d %s", got.Contracts, got.Tier)
	}
}
