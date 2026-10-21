package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// ── MENTOR-SIZE-4 dispatch-path pins (owner order 2026-10-16) ──────────────
// "set to 4 at least, sell 2 first so it's equal": every intraday tier sizes
// 4 MNQ and the split sells 2 first (leg 1) + holds 2 (the runner). These
// pins run the PRODUCTION dispatch path (mentorDispatchIntent → sizing →
// mentorPlaceIntent) and the split writer (mentorArmIntent), so a revert of
// any default goes RED.

// TestMentorSplitLegsFourIsTwoTwo pins the split math for the new default:
// 4 → (2,2) — equal halves, exactly the owner's "sell 2 first so it's equal".
// The spent-day runner cap (2) does not change a 4-lot: still (2,2).
func TestMentorSplitLegsFourIsTwoTwo(t *testing.T) {
	if l1, l2 := mentorSplitLegs(4, 0); l1 != 2 || l2 != 2 {
		t.Fatalf("mentorSplitLegs(4) = (%d,%d), want (2,2)", l1, l2)
	}
	if l1, l2 := mentorSplitLegs(4, mentorSpentDayRunnerCap); l1 != 2 || l2 != 2 {
		t.Fatalf("mentorSplitLegs(4, spentCap=%d) = (%d,%d), want (2,2)", mentorSpentDayRunnerCap, l1, l2)
	}
}

// TestMentorSize4ArmRowSplitsTwoTwo pins the PRODUCTION row writer: a 4-lot
// mentor arm authors Contracts=4 and Leg1Qty=2 (leg 1 sells 2 first; the
// runner holds the other 2). Mutant: leg1_qty not stamped, or the split
// math regressed to a non-equal half → RED.
func TestMentorSize4ArmRowSplitsTwoTwo(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-4", Setup: "ISB",
		Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 4, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)

	var rows []store.ArmedOrderDB
	if err := ledger.DB().Where("scenario LIKE ?", "isb-4-%").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ONE entry must author ONE row, got %d", len(rows))
	}
	r := rows[0]
	if r.Contracts == nil || *r.Contracts != 4 {
		t.Fatalf("the one row must carry Contracts=4, got %v", r.Contracts)
	}
	if r.Leg1Qty == nil || *r.Leg1Qty != 2 {
		t.Fatalf("the one row must carry leg1_qty=2 (sell 2 first), got %v", r.Leg1Qty)
	}
	// R1 (release #30 fold): leg 1's TP is ALWAYS +1R at NT8, every setup.
	if r.Leg1TP != 105 {
		t.Fatalf("leg1_tp must be +1R = 105, got %.2f", r.Leg1TP)
	}
	if r.TargetPx != 110 {
		t.Fatalf("the runner target must be the trade target 110, got %.2f", r.TargetPx)
	}
}

// TestMentorSize4DispatchSizesFour drives mentorDispatchIntent (the production
// injector switch) for every intraday tier and pins the sized count: base /
// ISB in range / ISB old extreme / 20–25 stop / confluence / big → 4; swing →
// 2. Mutant: any default back to 5 or 3 → the recorder sees a different count
// → RED.
func TestMentorSize4DispatchSizesFour(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t, at)
	ct := time.FixedZone("CT", -5*3600)
	setMentorNowSource(func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) })
	t.Cleanup(func() { setMentorNowSource(nil) })
	t.Setenv("MENTOR_PLACE", "on")
	// the confluence flag rides the intent's Confluence field through the seam
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return in.Confluence }
	t.Cleanup(func() { mentorConfluenceForIntent = nil })

	base := func(setup string, stop, target float64) mentor.Intent {
		return mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: setup,
			Price: 21000, Stop: 21000 - stop, Target: 21000 + target, StopPts: stop, TargetPts: target}
	}
	cases := []struct {
		name  string
		in    mentor.Intent
		extra mentorTierInputs
		want  int
	}{
		{"base ISB", base("ISB", 5.75, 11.5), mentorTierInputs{}, 4},
		{"ISB in range", func() mentor.Intent { in := base("ISB", 5.75, 11.5); in.Flag = mentor.FlagISBInRange; return in }(), mentorTierInputs{}, 4},
		{"ISB old extreme", func() mentor.Intent { in := base("ISB", 5.75, 11.5); in.Flag = mentor.FlagISBAtOldExtreme; return in }(), mentorTierInputs{}, 4},
		{"20-25 stop", base("PLH", 25, 50), mentorTierInputs{}, 4},
		{"confluence", func() mentor.Intent { in := base("PHL", 12, 35); in.Confluence = true; return in }(), mentorTierInputs{}, 4},
		{"big", func() mentor.Intent { in := base("PHL", 12, 35); in.Confluence = true; return in }(), mentorTierInputs{RoomMultiple: 2.5, HTFAgree: true}, 4},
		{"swing", base("SWING4H", 12, 24), mentorTierInputs{}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			placed := 0
			placedN := 0
			mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++; placedN = n }
			t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
			c.in.ArmID = "size4"
			c.in.ExpiryMs = time.Now().UnixMilli() + 60_000
			at.mentorDispatchIntent(c.in, c.extra, 0, 0)
			if placed != 1 {
				t.Fatalf("placement recorder fired %d times, want 1", placed)
			}
			if placedN != c.want {
				t.Fatalf("sized %d, want %d", placedN, c.want)
			}
		})
	}
}
