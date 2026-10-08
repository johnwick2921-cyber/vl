package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── PLANNER A6 — a born-dead / flip-met retry re-sights the model on the
// tape that exists NOW (completed bars between the read clock and the
// refusal, bounded, never the forming bar, reason verbatim), instead of
// retrying blind against the same stale read that was just refused. ──

func boolPtr(b bool) *bool { return &b }

// a6BornDeadPlan mirrors the row-339 reason shape: S1's authored condition
// "2x5m<15470" breaches on the fixture tape (5m closes 15460 at 19:00 and
// 15455 at 19:05), while the structured flip (15450) and death (15620) lines
// stay unmet — exactly one defect, the born-dead one.
const a6BornDeadPlan = `{
  "reasoning": "Balance below PDH; fade edges, long the reclaim.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15450"},
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
  "flip": {"price": 15450, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

// a6RecoveredPlan passes the SAME validator chain on the SAME tape: invalid
// 2x5m<15440 and flip below 15450 are unmet by closes 15460/15455.
const a6RecoveredPlan = `{
  "reasoning": "Fresh tape re-sighted; long the reclaim above 15480.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15450"},
  "levels": [
    {"price": 15480, "label": "PWL", "grade": "A", "instruction": "fade"},
    {"price": 15520, "label": "RN 15525", "grade": "B", "instruction": "fade"},
    {"price": 15575, "label": "RN 15575", "grade": "B", "instruction": "fade"},
    {"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"},
    {"price": 15650, "label": "RN 15650", "grade": "B", "instruction": "fade"},
    {"price": 15700, "label": "RN 15700", "grade": "B", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "sweep 15480 reclaim", "condition": "sweep_reclaim", "direction": "long", "target_chain": [15550, 15620], "invalid": "2x5m<15440", "quality": "A", "confirm":{"rule":"touch","ref_price":15480,"side":"below"},"economics":{"entry_zone":[15480,15480],"geometry":{"entry":15480,"stop":15470,"target":15620},"first_obstacle":{"price":15550,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":7.0,"r_to_arm_target":14.0}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 15620",
  "death": {"price": 15620, "side": "above", "rule": "2x5m"},
  "flip": {"price": 15450, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

// a6Tape is the row-339-shaped tape: two 5m buckets close below 15470 between
// read (19:00) and publish (19:07), then a FORMING 1m bar whose close (99999)
// does not exist yet and must never appear in the fresh tape.
func a6Tape() []market.Kline {
	loc := kernel.CTLocation()
	bars := []market.Kline{}
	add := func(h, m int, close float64) {
		open := time.Date(2026, 9, 23, h, m, 0, 0, loc).UnixMilli()
		bars = append(bars, market.Kline{OpenTime: open, Close: close, CloseTime: open + 60_000})
	}
	// 18:55 bucket → closes at 19:00 with 15460.
	add(18, 55, 15470)
	add(18, 56, 15468)
	add(18, 57, 15466)
	add(18, 58, 15464)
	add(18, 59, 15460)
	// 19:00 bucket → closes at 19:05 with 15455.
	add(19, 0, 15458)
	add(19, 1, 15456)
	add(19, 2, 15455)
	add(19, 3, 15455)
	add(19, 4, 15455)
	// 19:05 and 19:06 are completed 1m bars inside the window.
	add(19, 5, 15480)
	add(19, 6, 15485)
	// The forming bar: its close is the future.
	open := time.Date(2026, 9, 23, 19, 7, 0, 0, loc).UnixMilli()
	bars = append(bars, market.Kline{OpenTime: open, Close: 99999, CloseTime: open + 60_000})
	return bars
}

// TestPlannerBornDeadRetryCarriesTheFreshTape is the A6 pin at the production
// call site: attempt 1 is refused born-dead (row-339 reason shape) and
// attempt 2's prompt must carry the completed tape between read and refusal,
// never the forming bar, with the reason verbatim.
func TestPlannerBornDeadRetryCarriesTheFreshTape(t *testing.T) {
	at := plannerTestTrader(t)
	loc := kernel.CTLocation()
	read := time.Date(2026, 9, 23, 19, 0, 0, 0, loc)
	publish := time.Date(2026, 9, 23, 19, 7, 0, 0, loc)

	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline { return a6Tape() }
	defer func() { market.FuturesBarsProvider = orig }()

	facts := kernel.PlanFacts{Price: 15550, DATR: 300, ReadAt: read}
	machine := map[float64]string{15480: "PWL", 15700: "RN 15700"}
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreObserved(
		func() time.Time { return read },    // authoring (read-side) clock
		func() time.Time { return publish }, // publish clock — the fixture refusal clock
		nil,
		"ASIA", "2026-09-23", "owner_reset", "deepseek-v4-pro", "hashA6", "", "", "", "FULLPROMPT",
		facts, nil, machine, nil, true, 0,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			if len(blocks) == 1 {
				return a6BornDeadPlan, nil
			}
			return a6RecoveredPlan, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("born-dead-then-recover: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(blocks))
	}
	attempt2 := blocks[1]
	for _, want := range []string{
		"FRESH TAPE SINCE YOUR READ (previous attempt refused born-dead / flip-met)",
		"2x5m<15470",                 // the breached condition, verbatim
		"19:07 CT 1m close 15485.00", // a completed 1m close in the window
		"19:05 CT 5m close 15455.00", // the completed 5m close the born check judged
		"born-dead check itself is unchanged",
	} {
		if !strings.Contains(attempt2, want) {
			t.Fatalf("attempt 2 prompt missing %q:\n%s", want, attempt2)
		}
	}
	// The forming bar's close does not exist — it must never be printed.
	if strings.Contains(attempt2, "99999") {
		t.Fatalf("forming bar leaked into attempt 2:\n%s", attempt2)
	}
	// A close at the read clock is not between read and refusal.
	if strings.Contains(attempt2, "19:00 CT 1m close") {
		t.Fatalf("read-clock close leaked into the window:\n%s", attempt2)
	}
}

// TestPlannerBornDeadRetryKnobOffIsByteIdenticalToday — L4: planner_fresh_tape
// false reproduces today's blind retry (no fresh block) on the SAME refusal.
func TestPlannerBornDeadRetryKnobOffIsByteIdenticalToday(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, PlannerFreshTape: boolPtr(false)}
	loc := kernel.CTLocation()
	read := time.Date(2026, 9, 23, 19, 0, 0, 0, loc)
	publish := time.Date(2026, 9, 23, 19, 7, 0, 0, loc)

	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline { return a6Tape() }
	defer func() { market.FuturesBarsProvider = orig }()

	facts := kernel.PlanFacts{Price: 15550, DATR: 300, ReadAt: read}
	machine := map[float64]string{15480: "PWL", 15700: "RN 15700"}
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreObserved(
		func() time.Time { return read },    // authoring (read-side) clock
		func() time.Time { return publish }, // publish clock
		nil,
		"ASIA", "2026-09-23", "owner_reset", "deepseek-v4-pro", "hashA6off", "", "", "", "FULLPROMPT",
		facts, nil, machine, nil, true, 0,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			if len(blocks) == 1 {
				return a6BornDeadPlan, nil
			}
			return a6RecoveredPlan, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("knob-off recover: lc=%q err=%v", lc, err)
	}
	if strings.Contains(blocks[1], "FRESH TAPE SINCE YOUR READ") {
		t.Fatalf("planner_fresh_tape=false must reproduce the blind retry, got:\n%s", blocks[1])
	}
}
