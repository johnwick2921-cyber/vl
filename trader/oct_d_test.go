package trader

import (
	"math"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// OCT-EXACT build step D, D2 (CTO-decisions.md section D; mentor mode only: the
// reader takes the mentor RiskControlConfig and the split frame is authored only
// by mentorArmIntent, the mentor origin).

// octLeg1Default runs one test with the PRODUCTION unset default of
// mentor_leg1_fixed_pts (15); the trader test binary runs with 0 (TestMain).
func octLeg1Default(t *testing.T) {
	t.Helper()
	prev := mentorLeg1FixedPtsDefault
	mentorLeg1FixedPtsDefault = 15
	t.Cleanup(func() { mentorLeg1FixedPtsDefault = prev })
}

// D2: the default is 15, stored 0 reverts, out of range falls back; reader and
// resolver agree.
func TestOctD2_FixedPtsDefaultAndRevert(t *testing.T) {
	octLeg1Default(t)
	f := func(v float64) *float64 { return &v }
	rc := func(v *float64) *store.RiskControlConfig {
		return &store.RiskControlConfig{MentorTuning: &store.MentorTuning{MentorLeg1FixedPts: v}}
	}
	at, _ := resetTrader(t, store.StrategyConfig{})
	for _, c := range []struct {
		name string
		rc   *store.RiskControlConfig
		want float64
	}{
		{"nil rc", nil, 15}, {"empty rc", &store.RiskControlConfig{}, 15}, {"nil knob", rc(nil), 15},
		{"stored 0 = the revert", rc(f(0)), 0}, {"stored 20", rc(f(20)), 20},
		{"negative falls back", rc(f(-1)), 15}, {"over 100 falls back", rc(f(101)), 15},
	} {
		if got := mentorLeg1FixedPts(c.rc); got != c.want {
			t.Errorf("%s: reader = %v, want %v", c.name, got, c.want)
		}
		if c.rc != nil {
			if got := mentorTuningResolve(at, c.rc).Leg1FixedPts; got != c.want {
				t.Errorf("%s: resolver = %v, want %v", c.name, got, c.want)
			}
		}
	}
}

// D2, the video's own trade: 02 Nov 28,152.25 buy stop, "bán ở 20 điểm và để lại
// đúng 1 MNQ" [Oct D4.2 @14:18 R18]; partial at +15/20 [@12:16 R14]. With the
// bot's tighter stop (12.25 pts, so 1R < 15) leg 1 goes at +15 and ONE contract
// runs; with the video's own 16-pt stop 1R already exceeds 15 and the 1:1 floor
// stands.
func TestOctD2_Leg1FixedPartialOneRunner(t *testing.T) {
	long := mentor.Intent{Setup: "PHL", Side: mentor.SideLong, Price: 28152.25, Stop: 28140, StopPts: 12.25, Target: 28250, TargetPts: 97.75}
	short := mentor.Intent{Setup: "PLH", Side: mentor.SideShort, Price: 28152.25, Stop: 28164.5, StopPts: 12.25, Target: 28054.5, TargetPts: 97.75}
	videoStop := long
	videoStop.Stop, videoStop.StopPts = 28135.5, 16.75
	nearTarget := long
	nearTarget.Target, nearTarget.TargetPts = 28160, 7.75
	isb := long
	isb.Setup = "ISB"
	box := long
	box.Setup = "BOX"
	cases := []struct {
		name    string
		in      mentor.Intent
		n       int
		mode    string
		forkTP  float64
		fixed   float64
		wantQty int
		wantTP  float64
	}{
		{"PHL long 5-lot: 4 at +15, 1 runner", long, 5, "B", 0, 15, 4, 28167.25},
		{"PLH short 5-lot mirror", short, 5, "B", 0, 15, 4, 28137.25},
		{"3-lot: 2 + 1", long, 3, "B", 0, 15, 2, 28167.25},
		{"2-lot: 1 + 1", long, 2, "B", 0, 15, 1, 28167.25},
		{"1-lot: single bracket", long, 1, "B", 0, 15, 0, 0},
		{"video 16.75-pt stop: 1R > 15, the 1:1 floor stands", videoStop, 5, "B", 0, 15, 3, 28169},
		{"target closer than N+1: +1R partial", nearTarget, 5, "B", 0, 15, 3, 28164.5},
		{"knob 0 = today (ceil(n/2) at +1R)", long, 5, "B", 0, 0, 3, 28164.5},
		{"ISB keeps its own first target", isb, 5, "B", 0, 15, 3, 28164.5},
		{"BOX is not in the rule", box, 5, "B", 0, 15, 3, 28164.5},
		{"confluence C keeps its 2R", long, 5, "C", 28176.75, 15, 3, 28176.75},
		{"swing single", long, 5, "swing", 0, 15, 0, 0},
	}
	for _, c := range cases {
		qty, tp := mentorLeg1ForFrameFixed(c.in, c.n, c.mode, c.forkTP, c.fixed)
		if qty != c.wantQty || math.Abs(tp-c.wantTP) > 1e-9 {
			t.Errorf("%s: (%d, %.2f), want (%d, %.2f)", c.name, qty, tp, c.wantQty, c.wantTP)
		}
	}
}

// D2 through the PRODUCTION writer (mentorArmIntent): the row carries leg1_qty 4
// and leg1_tp +15 with the default, the pre-October split with the stored 0.
func TestOctD2_ArmRowCarriesTheFixedPartial(t *testing.T) {
	octLeg1Default(t)
	for _, c := range []struct {
		name    string
		fixed   *float64
		wantQty int
		wantTP  float64
	}{
		{"default 15", nil, 4, 28167.25},
		{"stored 0 reverts", func() *float64 { v := 0.0; return &v }(), 3, 28164.5},
	} {
		sc := store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true,
			MentorTuning: &store.MentorTuning{MentorLeg1FixedPts: c.fixed}}}
		at, st := resetTrader(t, sc)
		ledger := st.ArmedOrders()
		in := mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "phl-d2", Setup: "PHL", Side: mentor.SideLong,
			Price: 28152.25, Stop: 28140, Target: 28250, StopPts: 12.25, TargetPts: 97.75,
			ExpiryMs: time.Now().UnixMilli() + 60_000}
		at.mentorArmIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100, "B", 0)
		var rows []store.ArmedOrderDB
		if err := ledger.DB().Where("scenario LIKE ?", "phl-d2-%").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: %d rows, want 1", c.name, len(rows))
		}
		r := rows[0]
		if r.Leg1Qty == nil || *r.Leg1Qty != c.wantQty || math.Abs(r.Leg1TP-c.wantTP) > 1e-9 {
			t.Errorf("%s: row leg1 = (%d, %.2f), want (%d, %.2f)", c.name, intOrNeg(r.Leg1Qty), r.Leg1TP, c.wantQty, c.wantTP)
		}
	}
}

func intOrNeg(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}
