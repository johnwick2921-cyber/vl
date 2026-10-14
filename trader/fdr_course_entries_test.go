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
	t.ISBBoxEscape = "close"
	t.EMA34LossRelock = "session"
	t.D43TrendSource = "trigger_line"
	t.TrendlineIsTarget = &off
	t.LegResetOnWaveBreak = &off
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
