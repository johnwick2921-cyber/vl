package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// oct round 2 D5.4-SCALP — OctD5.4-R11 [@04:47] "giảm size lại", R13 [@05:00]
// "ăn ít lại": an entry against the 5m trigger sizes the reduced tier. The
// evaluator stamps mentor.FlagCounter5m (knob counter_5m_reduce_size); the sizing
// call site reads it. The course names no count — the reduced tier is the
// course's "giảm size" tier.
func TestRound2D54Counter5mSizesTheReducedTier(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "PHL",
		Price: 29722.25, Stop: 29709.5, Target: 29760, Flag: mentor.FlagCounter5m}
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	got, err := at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 3 || got.Tier != "counter_5m" {
		t.Fatalf("counter-5m PHL at the production call site: got %d (%s), want 3 (counter_5m)", got.Contracts, got.Tier)
	}
	// the same entry without the flag (a with-trigger entry, or knob off — the
	// evaluator stamps nothing) keeps today's table.
	in.Flag = ""
	if got, _ := at.mentorSizeFor(in, mentorExtraFor(in, false)); got.Contracts != 5 || got.Tier != "base" {
		t.Fatalf("unflagged PHL: got %d (%s), want 5 (base)", got.Contracts, got.Tier)
	}
}
