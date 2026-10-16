package trader

import (
	"encoding/json"
	"os"
	"testing"

	"vl/store"
)

// fdrPinPre25CourseEntries sets the release #25 course-entry knobs to the
// values the LIVE bot ran on the fidelity day (10-13, before #25): the
// fidelity test proves the harness reproduces the live arms, so it must run
// the live behaviour. The #25 defaults are measured by TestFullDayReplay with
// FDR_TUNING (knob ON vs OFF), not by the fidelity pin.
func fdrPinPre25CourseEntries(cfg *store.StrategyConfig) {
	off := false
	if cfg.RiskControl.MentorTuning == nil {
		cfg.RiskControl.MentorTuning = &store.MentorTuning{}
	}
	t := cfg.RiskControl.MentorTuning
	t.FifteenMinIBRequires5mISB = "off"
	t.TFConflictEnd = "first_box"
	t.NewsBarsExcluded = &off // release #29: the live 10-13 day ran the item-18 drop, not the freeze
	t.ISBBoxEscape = "close"
	t.EMA34LossRelock = "session"
	t.D43TrendSource = "trigger_line"
	t.TrendlineIsTarget = &off
	t.LegResetOnWaveBreak = &off
	// B52 (release #29) ships ON by default; the fidelity day ran BEFORE #29,
	// so the harness must pin the pre-#29 box world to reproduce the live arms.
	// The #29 box changes are measured by TestFullDayReplay (knob ON), not here.
	t.BoxNewZoneAfterEscape = &off
}

// fdrPinPre30Defaults pins every knob whose DEFAULT changed in
// course-defaults-30 back to the value the LIVE fidelity day ran (the live day
// predates #30): the fidelity test reproduces the live arms, so it must run
// the live behaviour. The #30 defaults are measured by TestFullDayReplay (the
// FULL_DAY_REPLAY=1 run), not by the fidelity pin.
func fdrPinPre30Defaults(cfg *store.StrategyConfig) {
	off, on := false, true
	if cfg.RiskControl.MentorTuning == nil {
		cfg.RiskControl.MentorTuning = &store.MentorTuning{}
	}
	t := cfg.RiskControl.MentorTuning
	// course-entry knobs whose #30 default flipped.
	t.HTFGateNewsOnly = &off        // was OFF (all-day gate)
	t.NewsWaitBothLines = &off      // was OFF
	t.TrendlineBetweenBoxes = &off  // was OFF
	t.FlagWallTarget = &off         // was OFF
	// course-exits + other risk knobs whose #30 default flipped.
	cfg.RiskControl.MentorDayOffRecheck = &on     // was ON (2026-10-09 ruling)
	cfg.RiskControl.MentorBEBeforePartial = &off  // was OFF (R3)
	cfg.RiskControl.MentorSwingRunner = &off      // was OFF
	cfg.RiskControl.MentorReverseISBAsPHL = &off  // was OFF
	cfg.RiskControl.MentorStopAfterLoss = &off    // was OFF
	if cfg.RiskControl.MentorSwing4HContracts == 0 {
		cfg.RiskControl.MentorSwing4HContracts = 1 // was 1
	}
	// ISBMaxStopPts is left at the #30 default 30: every live fidelity arm's
	// stop is < 30 pts, so the cap does not change the reproduced arms.
}

// fdrApplyTuningEnv merges FDR_TUNING (a JSON object of mentor_tuning keys,
// e.g. {"isb_box_escape":"close"}) onto the replay strategy — the ON-vs-OFF
// switch for the release #25 replay table. FDR_PRE25=1 first pins every
// course-entry knob to its pre-#25 value (the all-OFF baseline). Unset = the
// fixture strategy as stored (every #25 knob at its default).
func fdrApplyTuningEnv(t *testing.T, cfg *store.StrategyConfig) {
	t.Helper()
	if os.Getenv("FDR_PRE25") == "1" {
		fdrPinPre25CourseEntries(cfg)
	}
	raw := os.Getenv("FDR_TUNING")
	if raw == "" {
		return
	}
	if cfg.RiskControl.MentorTuning == nil {
		cfg.RiskControl.MentorTuning = &store.MentorTuning{}
	}
	if err := json.Unmarshal([]byte(raw), cfg.RiskControl.MentorTuning); err != nil {
		t.Fatalf("FDR_TUNING: %v", err)
	}
	t.Logf("FDR_TUNING applied: %s (FDR_PRE25=%s)", raw, os.Getenv("FDR_PRE25"))
}
