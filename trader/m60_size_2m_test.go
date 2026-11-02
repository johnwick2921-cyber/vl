package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// m465 P4 (release #60) — D3.1-R30: a 2m entry has a bigger stop than a 1m
// entry, so the size goes DOWN. "khi anh chị vô lệnh khung 2 phút, stop loss
// lớn hơn khung 1 phút. Thì bây giờ stop loss lớn, mình phải làm gì? Mình phải
// giảm size." [D3.1 p1 @14:09–14:35]. The course example: a 2m entry candle
// measured 15.50 pts / 62 ticks. The course names NO sizing number — the bot's
// reduced tier (3, the course's "giảm size" tier from D4.1 p1 @04:37–05:04) is
// the cut; the exact count is a mentor question in the release report.

// TestM60Size2mEntryReduced pins the size TABLE (mentorContractsFor) — the
// course example: ISB, stop 15.5 pts, read on the 2m, base 5 / reduced 3 → 3.
// The same entry read on the 1m keeps the base 5; SWING4H is untouched.
// Named RED: drop the exec_2m_reduce branch → the 2m case sizes 5.
func TestM60Size2mEntryReduced(t *testing.T) {
	const base, conf, big, reduced, swing4h, spent, mx = 5, 5, 5, 3, 2, 2, 20
	cases := []struct {
		name     string
		in       mentorTierInputs
		want     int
		wantTier string
	}{
		{"ISB 2m stop 15.5 [D3.1 p1 @14:09–14:35]", mentorTierInputs{Setup: "ISB", StopPts: 15.5, ExecTFMin: 2}, 3, "exec_2m_reduce"},
		{"ISB 1m stop 15.5", mentorTierInputs{Setup: "ISB", StopPts: 15.5, ExecTFMin: 1}, 5, "base"},
		{"ISB frame unknown (0) stop 15.5", mentorTierInputs{Setup: "ISB", StopPts: 15.5}, 5, "base"},
		{"PHL 2m", mentorTierInputs{Setup: "PHL", StopPts: 11.25, ExecTFMin: 2}, 3, "exec_2m_reduce"},
		{"PLH 2m (D3.1-R33 example 11.25 pts)", mentorTierInputs{Setup: "PLH", StopPts: 11.25, ExecTFMin: 2}, 3, "exec_2m_reduce"},
		{"ISB 5m (slow evening)", mentorTierInputs{Setup: "ISB", StopPts: 12, ExecTFMin: 5}, 3, "exec_2m_reduce"},
		{"BOX 2m — not an ISB/PHL/PLH entry", mentorTierInputs{Setup: "BOX", StopPts: 12, ExecTFMin: 2}, 5, "base"},
		{"SWING4H untouched by the frame", mentorTierInputs{Setup: "SWING4H", StopPts: 40, ExecTFMin: 2}, 2, "swing4h"},
		{"knob OFF → the 1m tiers", mentorTierInputs{Setup: "ISB", StopPts: 15.5, ExecTFMin: 2, Exec2mReduceOff: true}, 5, "base"},
		{"old-extreme ISB on 2m keeps its own (earlier) reduced tier", mentorTierInputs{Setup: "ISB", StopPts: 15.5, ExecTFMin: 2, ISBOldExtreme: true}, 3, "isb_old_extreme"},
		{"pullback warning still ranks first", mentorTierInputs{Setup: "PHL", StopPts: 10, ExecTFMin: 2, PullbackWarn: true}, 3, "pullback_warn"},
	}
	for _, c := range cases {
		got, err := mentorContractsFor(c.in, base, conf, big, reduced, swing4h, spent, mx)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Contracts != c.want || got.Tier != c.wantTier {
			t.Fatalf("%s: got %d (tier %s), want %d (tier %s) — why: %s", c.name, got.Contracts, got.Tier, c.want, c.wantTier, got.Why)
		}
	}
}

// TestM60Size2mProductionCallSite runs the PRODUCTION sizing call site
// (mentorSizeFor → mentorKnobs → mentorContractsFor): the intent's ExecTFMin
// must reach the table, and the knob mentor_2m_reduce_size=false must turn
// the cut off. Named RED: mentorSizeFor not copying in.ExecTFMin → 5.
func TestM60Size2mProductionCallSite(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Side: mentor.SideLong, Setup: "ISB",
		Price: 29096.75, Stop: 29081.25, Target: 29130, ExecTFMin: 2} // 15.50 pts — the D3.1 p1 @14:09 measure
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	got, err := at.mentorSizeFor(in, mentorExtraFor(in, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 3 || got.Tier != "exec_2m_reduce" {
		t.Fatalf("2m ISB at the production call site: got %d (%s), want 3 (exec_2m_reduce)", got.Contracts, got.Tier)
	}
	if n := at.mentorCountSnapshot()["tier_exec_2m_reduce"]; n != 1 {
		t.Fatalf("tier_exec_2m_reduce counter = %d, want 1", n)
	}
	in1 := in
	in1.ExecTFMin = 1
	if got, _ := at.mentorSizeFor(in1, mentorExtraFor(in1, false)); got.Contracts != 5 || got.Tier != "base" {
		t.Fatalf("1m ISB: got %d (%s), want 5 (base)", got.Contracts, got.Tier)
	}
	off := false
	atOff := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, Mentor2mReduceSize: &off})
	if got, _ := atOff.mentorSizeFor(in, mentorExtraFor(in, false)); got.Contracts != 5 || got.Tier != "base" {
		t.Fatalf("knob mentor_2m_reduce_size=false: got %d (%s), want 5 (base)", got.Contracts, got.Tier)
	}
	// An owner-raised reduced knob is honoured (stored values are sacred).
	atR := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorReducedContracts: 4})
	if got, _ := atR.mentorSizeFor(in, mentorExtraFor(in, false)); got.Contracts != 4 || got.Tier != "exec_2m_reduce" {
		t.Fatalf("reduced knob 4: got %d (%s), want 4 (exec_2m_reduce)", got.Contracts, got.Tier)
	}
}
