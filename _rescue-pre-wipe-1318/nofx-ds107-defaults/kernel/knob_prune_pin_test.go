package kernel

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nofx/market"
)

// ── W-KNOB-PRUNE (2026-09-18) — BYTE-IDENTICAL PINS AT THE SHIPPED DEFAULTS ──
//
// Every knob the prune removes or folds has a production path whose output at
// the shipped default must not move. These goldens were WRITTEN AT THE BASE
// COMMIT (origin/dev 0dd27940, before any prune edit) and re-run after it, so a
// pass is a before/after proof, not self-consistency. The one deliberate
// exception is htf_score_multiplier (1.2 → 1.0, owner ruling): its golden was
// re-pinned after the change and the seat diff is in the wave report.
//
// Regenerate: KNOB_PRUNE_WRITE_GOLDEN=1 go test ./kernel -run TestKnobPrunePin

func knobPruneFixtureBars() ([]market.Kline, time.Time) {
	now := time.Date(2030, 9, 12, 12, 0, 0, 0, CTLocation())
	start := now.Add(-48 * time.Hour)
	bars := make([]market.Kline, 48*60)
	for i := range bars {
		s := start.Add(time.Duration(i) * time.Minute)
		p := 20000 + 60*math.Sin(float64(i)/13) + 20*math.Cos(float64(i)/71)
		bars[i] = market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: p - 1, High: p + 3, Low: p - 3, Close: p + 1, Volume: float64(100 + i%31)}
	}
	return bars, now
}

func knobPruneGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "knob_prune", name)
	if os.Getenv("KNOB_PRUNE_WRITE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s: output moved from the pinned golden (knob prune must be byte-identical at the shipped default)\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// Seating: the planner's ranked table at the shipped defaults (max_levels 12,
// htf_seats nil, the HTF multiplier the scorer applies, seat_1h_zone ON).
// Pins seat_1h_zone (removed → hard-wired ON) and htf_score_multiplier.
func TestKnobPrunePin_Seating(t *testing.T) {
	bars, now := knobPruneFixtureBars()
	seated, pool, price, datr, _ := AssembleResearchLevels("knob-prune", bars, DefaultSessionRegistry(), "MNQ", 12, nil, HTFScoreMultiplier, now, 2, "")
	seated = Seat1HZone(seated, 12)
	data, err := json.MarshalIndent(struct {
		HTFMult      float64
		Pool, Seated []ScoredLevel
		Price, DATR  float64
	}{HTFScoreMultiplier, pool, seated, price, datr}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	knobPruneGolden(t, "seating.json", append(data, '\n'))
}

// Executor KEY LEVELS block at the shipped defaults (max_levels 8, proximity
// 1.5, seat_1h_zone ON, no min_grade). Pins seat_1h_zone on the executor path.
func TestKnobPrunePin_ExecutorKeyLevels(t *testing.T) {
	bars, now := knobPruneFixtureBars()
	block := BuildKeyLevelsBlockOpts("knob-prune", bars, DefaultSessionRegistry(), "MNQ", 8, now, 1.5, true, "")
	knobPruneGolden(t, "executor_keylevels.txt", []byte(block))
}

// Planner prompt at the shipped defaults (scenario_cap 3, structure_map OFF,
// acceptance 5m_close). Pins scenario_cap (folded → constant 3 unless stored)
// and structure_map (UI removed, default OFF).
func TestKnobPrunePin_PlannerPromptDefaults(t *testing.T) {
	p := BuildPlannerPrompt(PlannerInput{MaxLevels: 8, ScenarioCap: 3})
	knobPruneGolden(t, "planner_prompt_defaults.txt", []byte(p))
	if q := BuildPlannerPrompt(PlannerInput{MaxLevels: 8}); q != p {
		t.Fatal("ScenarioCap 0 must render the shipped default 3 byte-identically")
	}
}
