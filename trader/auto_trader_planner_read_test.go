package trader

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
	"vl/telemetry"
)

const validTraderPlanJSON = `{
  "reasoning": "Balance below PDH; fade edges, long the reclaim.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15480"},
  "levels": [
    {"price": 15480, "label": "PWL", "grade": "A", "instruction": "fade"},
    {"price": 15520, "label": "RN 15525", "grade": "B", "instruction": "fade"},
    {"price": 15575, "label": "RN 15575", "grade": "B", "instruction": "fade"},
    {"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"},
    {"price": 15650, "label": "RN 15650", "grade": "B", "instruction": "fade"},
    {"price": 15700, "label": "RN 15700", "grade": "B", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "sweep 15480 reclaim", "condition": "sweep_reclaim", "direction": "long", "target_chain": [15550, 15620], "invalid": "2x5m<15470", "quality": "A", "confirm":{"rule":"touch","ref_price":15480,"side":"below"},"economics":{"entry_zone":[15480,15480],"geometry":{"entry":15480,"stop":15470,"target":15620},"first_obstacle":{"price":15550,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":7.0,"r_to_arm_target":14.0}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 15620",
  "death": {"price": 15620, "side": "above", "rule": "2x5m"},
  "flip": {"price": 15480, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

func plannerTestTrader(t *testing.T) *AutoTrader {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Plan().Close(); _ = st.Close() })
	return &AutoTrader{
		id: "t1", exchange: "ninjatrader", store: st,
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}},
	}
}

func TestRunPlannerReadCoreSuccess(t *testing.T) {
	at := plannerTestTrader(t)
	ver, lc, err := at.runPlannerReadCore("NY", "2026-08-14", "deepseek-reasoner", "hashA", "", "",
		func() (string, error) { return validTraderPlanJSON, nil })
	if err != nil || ver != 1 || lc != "active" {
		t.Fatalf("success: ver=%d lc=%q err=%v", ver, lc, err)
	}
	got, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if got == nil || got.Lifecycle != "active" || got.ModelID != "deepseek-reasoner" {
		t.Fatalf("stored plan wrong: %+v", got)
	}
}

func TestRunPlannerReadCoreFailClosed(t *testing.T) {
	at := plannerTestTrader(t)
	ver, lc, err := at.runPlannerReadCore("NY", "2026-08-14", "deepseek-reasoner", "hashB", "", "",
		func() (string, error) { return "", errors.New("timeout") })
	if err != nil || lc != "no_trade" {
		t.Fatalf("fail-closed: ver=%d lc=%q err=%v want no_trade", ver, lc, err)
	}
	got, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if got == nil || got.Lifecycle != "no_trade" || got.TriggerReason != "planner_fail_closed" {
		t.Fatalf("fail-closed plan not written correctly: %+v", got)
	}
}

// Owner ruling 2026-08-31 — the per-side COUNT concept is deleted: a plan with
// 1 level above while the machine map offered 3 writes ACTIVE with NO WARN, NO
// note, and no thin_side key anywhere in the stored doc. (Previously this exact
// shape fail-closed ASIA 3×, then WARNed — both behaviors are gone.)
const thinAbovePlanJSON = `{
  "reasoning": "thin above: price sits at the top of the stack; only one level above in the map",
  "bias": {"direction": "long", "conviction": "low", "flip_condition": "2x5m < 29500"},
  "levels": [
    {"price": 29500, "label": "PDL", "grade": "A", "instruction": "reclaim"},
    {"price": 29550, "label": "RN 29550", "grade": "B", "instruction": "reclaim"},
    {"price": 29600, "label": "RN 29600", "grade": "B", "instruction": "fade"},
    {"price": 30000, "label": "RN 30000", "grade": "B", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "hold 29550", "condition": "hold", "direction": "long", "target_chain": [29700], "invalid": "2x5m<29540", "quality": "B", "economics":{"entry_zone":[29550,29550],"geometry":{"entry":29550,"stop":29540,"target":29700},"first_obstacle":{"price":29700,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":15.0,"r_to_arm_target":15.0}, "confirm": {"rule": "time_hold", "ref_price": 29550, "side": "above"}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 30000",
  "death": {"price": 30000, "side": "above", "rule": "2x5m"},
  "flip": {"price": 29500, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

func TestRunPlannerReadThinAboveWritesCleanNoArtifacts(t *testing.T) {
	at := plannerTestTrader(t)
	facts := kernel.PlanFacts{Price: 29614, DATR: 300} // PDH/PDL 0 → gap rules skipped
	machine := map[float64]string{                     // rich map: 3 below + 3 above; the plan carries only 1 above
		29500: "PDL", 29550: "RN 29550", 29600: "RN 29600",
		30000: "RN 30000", 30100: "RN 30100", 30200: "RN 30200",
	}
	ver, lc, err := at.runPlannerReadCoreWithFactsGrades("ASIA", "2026-08-26", "owner_reset",
		"deepseek-v4-pro", "hashQ", "", "", "", "FULLPROMPT", facts, nil, machine, nil, true,
		func(userPrompt string) (string, error) { return thinAbovePlanJSON, nil })
	// Count is deleted (owner ruling 2026-08-31): no WARN, no note, plan writes.
	if err != nil || ver != 1 || lc != "active" {
		t.Fatalf("thin-above + rich map must write active: ver=%d lc=%q err=%v", ver, lc, err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-26", "ASIA")
	if row == nil || row.TriggerReason != "owner_reset" {
		t.Fatalf("stored row wrong: %+v", row)
	}
	if strings.Contains(row.Doc, "thin_side") || strings.Contains(row.Doc, "thin-side") {
		t.Fatalf("no side-count artifact may be stamped, got %s", row.Doc)
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
}

// TestRunPlannerReadRepairCarriesVerbatimReject — planner-speed wave 3
// (2026-08-31): attempt ≥2 is the REPAIR call (instruction header + rejected
// output + errors verbatim + law excerpts) and NOT the full playbook.
func TestRunPlannerReadRepairCarriesVerbatimReject(t *testing.T) {
	at := plannerTestTrader(t)
	facts := kernel.PlanFacts{Price: 15550, DATR: 300}
	machine := map[float64]string{15480: "PWL", 15700: "RN 15700"}
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGrades("ASIA", "2026-08-26", "owner_reset",
		"deepseek-v4-pro", "hashQB", "", "", "", "FULLPROMPT", facts, nil, machine, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			if len(blocks) == 1 {
				return "not json", nil // attempt 1 fails the parse gate
			}
			return validTraderPlanJSON, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("repair-then-success: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(blocks))
	}
	if blocks[0] != "FULLPROMPT" {
		t.Fatalf("attempt 1 must be the full author prompt, got %q", blocks[0])
	}
	if !strings.Contains(blocks[1], "You are repairing a rejected plan") ||
		!strings.Contains(blocks[1], "no JSON object found in planner output") ||
		!strings.Contains(blocks[1], "Rejected plan output (verbatim)") ||
		!strings.Contains(blocks[1], "not json") ||
		!strings.Contains(blocks[1], "Applicable law") {
		t.Fatalf("attempt 2 must be the repair prompt (header+errors+output+law), got %q", blocks[1])
	}
	if strings.Contains(blocks[1], "FULLPROMPT") {
		t.Fatalf("repair prompt must NOT re-send the full playbook, got %q", blocks[1])
	}
}

// TestRunPlannerReadRepairFallbackAndRegression — 3.4/3.6: a malformed repair
// falls back to one full re-author (reject block rides) when the repair's
// defect is DIFFERENT from the author's. W26 F-1: an IDENTICAL repeated defect
// no longer reaches attempt 3 — see TestRunPlannerReadRepeatedDefectEndsRead.
func TestRunPlannerReadRepairFallbackAndRegression(t *testing.T) {
	at := plannerTestTrader(t)
	facts := kernel.PlanFacts{Price: 15550, DATR: 300}
	machine := map[float64]string{15480: "PWL", 15700: "RN 15700"}
	blocks := []string{}
	base := telemetry.RepairRegressionCount()
	_, lc, err := at.runPlannerReadCoreWithFactsGrades("ASIA", "2026-08-26", "owner_reset",
		"deepseek-v4-pro", "hashQC", "", "", "", "FULLPROMPT", facts, nil, machine, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			if len(blocks) == 1 {
				return "not json", nil // attempt 1 parse-fails ("no JSON object found")
			}
			if len(blocks) == 2 {
				// A DIFFERENT defect (valid JSON, zero scenarios) — the repair
				// made progress, so the full re-author still gets its chance.
				return `{"bias":{"direction":"long"},"levels":[],"scenarios":[]}`, nil
			}
			return validTraderPlanJSON, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("fallback-then-success: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(blocks))
	}
	if !strings.Contains(blocks[1], "You are repairing a rejected plan") {
		t.Fatalf("attempt 2 must be the repair prompt, got %q", blocks[1])
	}
	// CLASS 45 E4 (2026-09-02): the re-author's corrections lead the prompt AND
	// close it. Was: a single "PREVIOUS ATTEMPT REJECTED" tail only.
	if !strings.Contains(blocks[2], "## CORRECTIONS FROM THIS READ — read these FIRST") ||
		!strings.Contains(blocks[2], "## CORRECTIONS FROM THIS READ (repeated") ||
		!strings.Contains(blocks[2], "FULLPROMPT") {
		t.Fatalf("attempt 3 must be the full re-author with corrections at TOP and TAIL, got %q", blocks[2])
	}
	if hi, lo := strings.Index(blocks[2], "CORRECTIONS FROM THIS READ"), strings.Index(blocks[2], "FULLPROMPT"); hi > lo {
		t.Fatalf("the corrections must LEAD the prompt, not trail the playbook")
	}
	if delta := telemetry.RepairRegressionCount() - base; delta != 0 {
		t.Fatalf("DIFFERENT defects must not increment the regression counter, delta=%d", delta)
	}
}

// TestRunPlannerReadRepeatedDefectEndsRead — W26 F-1: two IDENTICAL parse
// defects end the read at once (fail-closed: no plan written, the active plan
// is kept), instead of burning a third ~15 min max-reasoning attempt. The
// 10-14 read burned attempts 2 AND 3 on the identical "no JSON object found"
// defect and held the planner stream ~45 min.
func TestRunPlannerReadRepeatedDefectEndsRead(t *testing.T) {
	at := plannerTestTrader(t)
	facts := kernel.PlanFacts{Price: 15550, DATR: 300}
	machine := map[float64]string{15480: "PWL", 15700: "RN 15700"}
	calls := 0
	base := telemetry.RepairRegressionCount()
	_, lc, _ := at.runPlannerReadCoreWithFactsGrades("ASIA", "2026-08-26", "owner_reset",
		"deepseek-v4-pro", "hashQD", "", "", "", "FULLPROMPT", facts, nil, machine, nil, false, // wake path: opportunistic
		func(userPrompt string) (string, error) {
			calls++
			return "still not json", nil // identical parse defect every attempt
		})
	if calls != 2 {
		t.Fatalf("a repeated identical parse defect must end the read after attempt 2, got %d calls", calls)
	}
	if lc != "kept_active" {
		t.Fatalf("a failed wake read keeps the active plan (lc=%q), never a no_trade", lc)
	}
	if delta := telemetry.RepairRegressionCount() - base; delta < 1 {
		t.Fatalf("repeated-defect regression counter must increment, delta=%d", delta)
	}
}

func TestRunPlannerReadCoreRetryThenSuccess(t *testing.T) {
	at := plannerTestTrader(t)
	n := 0
	_, lc, err := at.runPlannerReadCore("NY", "2026-08-14", "m", "hashC", "", "", func() (string, error) {
		n++
		if n == 1 {
			return "not json", nil // attempt 1 parse-fails
		}
		if n == 2 {
			// A DIFFERENT defect (valid JSON, zero scenarios) — W26 F-1 keeps
			// retrying on distinct defects; only an IDENTICAL repeat fast-fails.
			return `{"bias":{"direction":"long"},"levels":[],"scenarios":[]}`, nil
		}
		return validTraderPlanJSON, nil
	})
	if err != nil || lc != "active" {
		t.Fatalf("retry-then-success: lc=%q err=%v (calls=%d)", lc, err, n)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts (1+2 retries), got %d", n)
	}
}

// TestRunPlannerReadWallClockCap — W26 F-1: the READ is wall-clock bounded to
// one wake interval, so a slow max-reasoning attempt × 3 can never hold the
// planner stream for 45+ min (10-14 held it ~45 min and blocked every wake).
func TestRunPlannerReadWallClockCap(t *testing.T) {
	at := plannerTestTrader(t)
	clk := time.Date(2026, 8, 26, 10, 0, 0, 0, kernel.CTLocation())
	testNow = func() time.Time { return clk }
	t.Cleanup(func() { testNow = nil })
	facts := kernel.PlanFacts{Price: 15550, DATR: 300}
	machine := map[float64]string{15480: "PWL"}
	calls := 0
	_, lc, _ := at.runPlannerReadCoreWithFactsGrades("ASIA", "2026-08-26", "owner_reset",
		"deepseek-v4-pro", "hashQE", "", "", "", "FULLPROMPT", facts, nil, machine, nil, false,
		func(userPrompt string) (string, error) {
			calls++
			// The first attempt consumes the whole wake interval (30 min).
			clk = clk.Add(31 * time.Minute)
			return "not json", nil
		})
	if calls != 1 {
		t.Fatalf("the wall-clock cap must stop the read after attempt 1 once the wake interval elapsed, got %d calls", calls)
	}
	if lc != "kept_active" {
		t.Fatalf("cap fail-closed keeps the active plan (lc=%q)", lc)
	}
}
