package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// W-MENTOR-EXACT (D3.4 p3 @07:52 "tự tin, mạnh mẽ, hold cái lệnh đó, at least là
// risk reward 1-2"): confluence is a LONGER HOLD (exit C, ≥ 1:2), never a size-up.
// The invented "big" tier (confluence + 4h&1h agree + room ≥ 2× + target ≥ 30)
// is REMOVED, so HTFAgree changes nothing about size. This pin runs the real
// call-site path (mentorExtraFor → mentorSizeFor). Mutant: restore the big-tier
// branch → a confluence + HTFAgree entry sizes big → RED.
func TestMentorConfluenceNeverAddsSizeRegardlessOfHTFAgree(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t, at)
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return true }
	t.Cleanup(func() { mentorConfluenceForIntent = nil })

	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "PHL",
		Price: 21000, Stop: 20988, Target: 21035, StopPts: 12, TargetPts: 35}

	in.HTFAgree = true
	got, err := at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 4 || got.Tier != "confluence" {
		t.Fatalf("confluence + 4h&1h agree: got %d %s, want 4 confluence (no big tier)", got.Contracts, got.Tier)
	}

	in.HTFAgree = false
	got, err = at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 4 || got.Tier != "confluence" {
		t.Fatalf("confluence without 4h&1h agree: got %d %s, want 4 confluence", got.Contracts, got.Tier)
	}

	// A 29-pt target also sizes confluence — the old "big needs target" gate is gone.
	in.HTFAgree = true
	in.Target, in.TargetPts = 21029, 29
	got, err = at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != "confluence" || got.Contracts != 4 {
		t.Fatalf("a 29-pt confluence target: got %d %s, want 4 confluence", got.Contracts, got.Tier)
	}
}
