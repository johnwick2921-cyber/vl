// F-2 / F-14 — the Go mentor DefaultConfig and the Studio
// MENTOR_TUNING_DEFAULTS table in MentorTuningPanel.tsx are the SAME knobs,
// read by two different runtimes that can only drift silently: the TS table
// drives the Studio placeholders ("default: …"), the Go DefaultConfig drives
// the real decisions. A knob whose default differs means the owner is shown a
// default the machine will not run.
//
// This test parses the TS defaults block directly, so it fails the moment the
// two files disagree — no mirrored literal to keep in sync.

package mentor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mentorTuningDefaultsBlock returns the raw source between
// "MENTOR_TUNING_DEFAULTS = {" and "} as const".
func mentorTuningDefaultsBlock(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "web", "src", "components", "strategy", "MentorTuningPanel.tsx")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(b)
	start := strings.Index(src, "MENTOR_TUNING_DEFAULTS = {")
	if start < 0 {
		t.Fatal("MENTOR_TUNING_DEFAULTS block not found in MentorTuningPanel.tsx")
	}
	rest := src[start:]
	end := strings.Index(rest, "} as const")
	if end < 0 {
		t.Fatal("MENTOR_TUNING_DEFAULTS close (\"} as const\") not found")
	}
	return rest[:end]
}

// tsDefault returns the literal value a MENTOR_TUNING_DEFAULTS key holds,
// stripped of any trailing comment. The key must match the TS source exactly
// (bare for identifiers, quoted for quoted keys).
func tsDefault(t *testing.T, block, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*([^,\n]*)`)
	m := re.FindStringSubmatch(block)
	if m == nil {
		t.Fatalf("key %q not found in MENTOR_TUNING_DEFAULTS", key)
	}
	v := strings.TrimSpace(m[1])
	if i := strings.Index(v, "//"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// goLiteral renders a Go default in the TS literal spelling the table uses:
// true/false, a decimal number, or a single-quoted string.
func goLiteral(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprint(x)
	case float64:
		return fmt.Sprint(x)
	case string:
		return "'" + x + "'"
	default:
		return fmt.Sprint(v)
	}
}

func TestMentorTuningDefaultsParityWithTS(t *testing.T) {
	block := mentorTuningDefaultsBlock(t)
	cfg := DefaultConfig()

	// Every knob the Studio exposes a "default: …" placeholder for, against the
	// Go default the kernel/trader actually runs. Adding a knob to the table
	// means adding it here too — that is the point of the test.
	rows := []struct {
		key   string
		goVal any
	}{
		{"trigger_school", cfg.TriggerSchool},
		{"ping_pong_min_gap_pts", cfg.PingPongMinGapPts},
		{"ping_pong_candle_max_pts", cfg.PingPongCandleMaxPts},
		{"ping_pong_candle_lookback", cfg.PingPongCandleLookback},
		{"level_max_visits", cfg.LevelMaxVisits},
		{"orb_gate_enabled", cfg.OrbGateEnabled},
		{"isb_reverse_ema9_enabled", cfg.ISBReverseEMA9Enabled},
		{"htf_gate_news_only", cfg.HTFGateNewsOnly},
		{"exec_2m_after_30m", cfg.Exec2mAfter30m},
		{"day_gate_spent_pts", cfg.DayGateSpentPts},
		{"day_gate_target_cap_pts", cfg.DayGateTargetCapPts},
		{"swing_max_stop_pts", cfg.Swing.MaxStopPts},
		{"'15m_ib_requires_5m_isb'", FifteenMinRequire5mISB},
		{"tf_conflict_end", TFConflictBothBoxes},
		{"news_wait_both_lines", cfg.NewsWaitBothLines},
		{"news_bars_excluded", cfg.NewsBarsExcluded},
		{"isb_box_escape", ISBBoxEscapeBody},
		{"ema34_loss_relock", EmaRelockUntilZoneExit},
		{"orb_edge_veto_pts", cfg.OrbEdgeVetoPts},
		{"d43_trend_source", D43TrendStructure},
		{"level_entries_need_trigger_side", cfg.LevelEntriesNeedTriggerSide},
		{"trendline_is_target", cfg.TrendlineIsTarget},
		{"trendline_between_boxes", cfg.TrendlineBetweenBoxes},
		{"leg_reset_on_wave_break", cfg.LegResetOnWaveBreak},
		{"flag_wall_target", cfg.FlagWallTarget},
		{"box_new_zone_after_escape", cfg.BoxNewZoneAfterEscape},
		{"orb_escape_completely", cfg.OrbEscapeCompletely},
	}

	for _, r := range rows {
		want := goLiteral(r.goVal)
		got := tsDefault(t, block, r.key)
		if got != want {
			t.Errorf("%s: TS default %s != Go default %s", r.key, got, want)
		}
	}
}
