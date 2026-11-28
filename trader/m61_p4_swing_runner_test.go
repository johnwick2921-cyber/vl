package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// Release #61 m466 P4 — SWING4H fidelity, trader half: the runner's take-profit
// (G65 D5.2-R6) and the two kernel swing ISB knobs resolved onto the evaluator
// (G63 D5.2-R26, G66 D5.2-R25).

func m61SwingRow(t *testing.T, st *store.Store, armID string) store.ArmedOrderDB {
	t.Helper()
	var rows []store.ArmedOrderDB
	if err := st.ArmedOrders().DB().Where("scenario LIKE ?", "%"+armID+"%").Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("want ONE swing row for %s, got %d (%v)", armID, len(rows), err)
	}
	return rows[0]
}

// TestM61SwingRunnerNoTP — G65 D5.2-R6 [p1 @05:40–06:14] "em đánh một lệnh lower
// high ở khung 5 phút nhưng em gồng theo khung 4 giờ" (one 4h candle = 152 pts);
// R32: first target the 5m EMA 34, "rồi tới đâu thì tới". The swing runner has
// NO take-profit: it exits by its stop / BE, the 4h hold-close (swing4h.go
// manageSwingPosition) or the EOD flat. The NT8 bracket is an OCO that needs a
// TP leg (WireBracket rounds it, the AddOn cannot drop one), so "none" parks it
// at entry ± 1,000 pts and counts swing_runner_no_tp.
// Long 29,500, stop 29,470, first target 29,540, 2 contracts:
//
//	none (default) → leg 1 = 1 @ 29,540, runner TP 30,500 (sentinel)
//	capped (pre-#61) → runner TP max(100, 2 × 40) = 29,600
//
// Mutant: keep math.Max(mentorSwingRunnerMinPts, 2*dist) under none → 29,600 → RED.
func TestM61SwingRunnerNoTP(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "swing-61", Setup: "SWING4H",
		Side: mentor.SideLong, Price: 29500, Stop: 29470, Target: 29540,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	choice := mentorSizeChoice{Contracts: 2, Tier: "swing4h", Why: "test"}

	// none (the default: the key absent).
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.mentorArmIntent(in, choice, 1000, 1100, "swing", 0)
	r := m61SwingRow(t, st, "swing-61")
	if r.Leg1Qty == nil || *r.Leg1Qty != 1 || r.Leg1TP != 29540 {
		t.Fatalf("none: leg 1 = 1 contract at the first target 29,540; got leg1=%v tp %.2f", r.Leg1Qty, r.Leg1TP)
	}
	if r.TargetPx != 30500 {
		t.Fatalf("none: the runner has no TP — parked at entry + 1,000 = 30,500; got %.2f", r.TargetPx)
	}
	if n := at.mentorCountSnapshot()["swing_runner_no_tp"]; n != 1 {
		t.Fatalf("swing_runner_no_tp = %d, want 1", n)
	}

	// short mirror: entry − 1,000.
	short := in
	short.ArmID, short.Side, short.Price, short.Stop, short.Target = "swing-62", mentor.SideShort, 29500, 29530, 29460
	at4, st4 := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at4.mentorArmIntent(short, choice, 1000, 1100, "swing", 0)
	if r := m61SwingRow(t, st4, "swing-62"); r.TargetPx != 28500 {
		t.Fatalf("none (short): runner TP parked at entry − 1,000 = 28,500; got %.2f", r.TargetPx)
	}

	// capped: the pre-#61 cap.
	at2, st2 := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingRunnerTP: "capped"}})
	at2.mentorArmIntent(in, choice, 1000, 1100, "swing", 0)
	if r := m61SwingRow(t, st2, "swing-61"); r.TargetPx != 29600 {
		t.Fatalf("capped: runner TP max(100, 2 × 40) above 29,500 = 29,600; got %.2f", r.TargetPx)
	}
	if n := at2.mentorCountSnapshot()["swing_runner_no_tp"]; n != 0 {
		t.Fatalf("capped: swing_runner_no_tp = %d, want 0", n)
	}

	// an unknown stored value fails closed to the course (none), counted.
	at3, st3 := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingRunnerTP: "sometimes"}})
	at3.mentorArmIntent(in, choice, 1000, 1100, "swing", 0)
	if r := m61SwingRow(t, st3, "swing-61"); r.TargetPx != 30500 {
		t.Fatalf("bad value: fails closed to none (30,500); got %.2f", r.TargetPx)
	}
	if n := at3.mentorCountSnapshot()["swing_runner_tp_bad_value"]; n == 0 {
		t.Fatal("a bad mentor_swing_runner_tp must be counted swing_runner_tp_bad_value")
	}
}

// TestM61SwingRunnerNoTPSingleBracketUntouched — a single-bracket swing (runner
// OFF, or 1 contract) keeps its first target as the bracket's TP: the "none"
// sentinel is the RUNNER's only, never the whole position's.
func TestM61SwingRunnerNoTPSingleBracketUntouched(t *testing.T) {
	in := mentor.Intent{Action: mentor.PlaceStopEntry, ArmID: "swing-63", Setup: "SWING4H",
		Side: mentor.SideLong, Price: 29500, Stop: 29470, Target: 29540,
		ExpiryMs: time.Now().UnixMilli() + 60_000}
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 1, Tier: "swing4h", Why: "test"}, 1000, 1100, "swing", 0)
	if r := m61SwingRow(t, st, "swing-63"); r.Leg1Qty != nil || r.TargetPx != 29540 {
		t.Fatalf("1 contract: single bracket at the first target; got leg1=%v target %.2f", r.Leg1Qty, r.TargetPx)
	}
	if n := at.mentorCountSnapshot()["swing_runner_no_tp"]; n != 0 {
		t.Fatalf("single bracket: swing_runner_no_tp = %d, want 0", n)
	}
}

// TestM61SwingISBKnobsResolveOntoTheEvaluator — G63/G66: the two kernel knobs
// resolve from the stored strategy onto cfg.Swing (absent → the course
// defaults: not_required, keep ON), a stored pre-#61 value is honoured, an
// unknown close-back value fails closed to not_required and is counted, and the
// course-exits boot line READS all three m466 P4 knobs.
func TestM61SwingISBKnobsResolveOntoTheEvaluator(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	cfg := at.mentorEvaluatorConfig()
	if cfg.Swing.ISBCloseBackRequired || cfg.Swing.ISBCancelOnCloseThrough {
		t.Fatalf("absent keys → not_required + keep ON; got %+v", cfg.Swing)
	}
	line := at.mentorCourseExitsBootLine()
	for _, want := range []string{"swing_isb_close_back=not_required", "swing_isb_keep=ON", "swing_runner_tp=none"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q missing %q", line, want)
		}
	}

	off := false
	at2, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true,
		MentorSwingISBCloseBack: "required", MentorSwingISBKeep: &off, MentorSwingRunnerTP: "capped"}})
	cfg2 := at2.mentorEvaluatorConfig()
	if !cfg2.Swing.ISBCloseBackRequired || !cfg2.Swing.ISBCancelOnCloseThrough {
		t.Fatalf("stored required + keep OFF must reach the kernel; got %+v", cfg2.Swing)
	}
	line2 := at2.mentorCourseExitsBootLine()
	for _, want := range []string{"swing_isb_close_back=required", "swing_isb_keep=OFF", "swing_runner_tp=capped"} {
		if !strings.Contains(line2, want) {
			t.Fatalf("boot line %q missing %q", line2, want)
		}
	}

	at3, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorSwingISBCloseBack: "always"}})
	if cfg3 := at3.mentorEvaluatorConfig(); cfg3.Swing.ISBCloseBackRequired {
		t.Fatal("an unknown close-back value fails closed to not_required (the course)")
	}
	if n := at3.mentorCountSnapshot()["swing_isb_close_back_bad_value"]; n == 0 {
		t.Fatal("a bad mentor_swing_isb_close_back must be counted swing_isb_close_back_bad_value")
	}
}
