package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-WRITE-TIME-FEASIBILITY (2026-09-18) — call-site tests.
//
// These drive runPlannerReadCoreWithFactsGrades (the real planner call site,
// with prompt visibility) with a plan whose arm the gate-at-arm chain would
// refuse, through a stubbed FuturesBarsProvider that yields a known 5m ATR so
// the min-SL floor is deterministic.

// feasBarsFor returns a stub provider: 1m bars (the AISVP interval the arm seam
// aggregates to 5m) with per-minute true range tr and close close1m, so
// armSeamATR5mFromBars yields ATR14 = tr (each 5m bucket TR = tr) and the
// stop-side predicate's last-tape close reads close1m (SHOULD-FIX 7).
func feasBarsFor(close1m, tr float64) func(symbol, tf string, n int) []market.Kline {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	return func(symbol, tf string, n int) []market.Kline {
		out := make([]market.Kline, 0, n)
		for i := 0; i < n; i++ {
			// One range move per 5-minute bucket; the other minutes stay flat so
			// each aggregated bucket has TR exactly tr.
			k := market.Kline{OpenTime: start + int64(i)*60_000, Open: close1m, Close: close1m}
			if i%5 == 0 {
				k.High, k.Low = close1m+tr, close1m
				k.Close = close1m + tr
			} else {
				k.High, k.Low = close1m+tr, close1m+tr
				k.Close = close1m + tr
			}
			out = append(out, k)
		}
		return out
	}
}

func feasStubBars(t *testing.T) {
	t.Helper()
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = feasBarsFor(108, 8)
	t.Cleanup(func() { market.FuturesBarsProvider = old })
}

// infeasibleFeasPlanJSON: entry 15550 / stop 15540 → stop distance 10.00 <
// 12.00 floor (1.5 × ATR5m 8.0). The feasible variant widens the stop to 15530.
const infeasibleFeasPlanJSON = `{
  "reasoning": "Balance below PDH; fade edges, long the reclaim.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15480"},
  "levels": [
    {"price": 15480, "label": "PWL", "grade": "A", "instruction": "fade"},
    {"price": 15520, "label": "RN 15525", "grade": "B", "instruction": "fade"},
    {"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "sweep 15480 reclaim", "condition": "sweep_reclaim", "direction": "long", "target_chain": [15550, 15620], "invalid": "2x5m<15470", "quality": "A", "confirm":{"rule":"touch","ref_price":15480,"side":"below"},"economics":{"entry_zone":[15480,15480],"geometry":{"entry":15550,"stop":15540,"target":15620},"first_obstacle":{"price":15560,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":1.0,"r_to_arm_target":7.0},"arm":{"enabled":true,"entry":15550,"stop":15540,"target":15620,"wait_confirm":true}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 15620",
  "death": {"price": 15620, "side": "above", "rule": "2x5m"},
  "flip": {"price": 15480, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

func feasPlannerTrader(t *testing.T, writeFeas *bool) *AutoTrader {
	t.Helper()
	at := plannerTestTrader(t)
	at.config.StrategyConfig.DayPlan.WriteTimeFeasibility = writeFeas
	// W-EXEC-TRUTH W3: these tests pin the LEGACY feasibility path (an arm with
	// no entry policy). With the shipped default (market_in_zone) the arm would
	// be stamped and judged by writeTimeZoneVerdicts instead — that path is
	// pinned in entry_policy_write_test.go. "legacy" = the explicit off.
	at.config.StrategyConfig.DayPlan.EntryPolicyDefault = store.EntryPolicyLegacy
	return at
}

// reclaimFeasPlanJSON (CTO stop-side amendment, 2026-09-18): a LONG reclaim
// whose level (arm entry 15480) is already BELOW the read-time price 15550, so
// the executor's stop-side guard would cancel the stop entry at placement
// (trigger 15480.50 ≤ price). The stop is 20pt wide so the min-SL floor
// (1.5×ATR5m 8.0 = 12.0) does NOT fire first — the stop-side predicate is the
// only refusal.
const reclaimFeasPlanJSON = `{
  "reasoning": "Reclaim the broken level.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15470"},
  "levels": [
    {"price": 15480, "label": "PWL", "grade": "A", "instruction": "reclaim"},
    {"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "reclaim 15480", "condition": "reclaim", "direction": "long", "target_chain": [15550, 15620], "invalid": "2x5m<15470", "quality": "A", "confirm":{"rule":"1x5m_close","ref_price":15480,"side":"above"},"economics":{"entry_zone":[15480,15480],"geometry":{"entry":15480,"stop":15460,"target":15620},"first_obstacle":{"price":15550,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":3.5,"r_to_arm_target":7.0},"arm":{"enabled":true,"entry":15480,"stop":15460,"target":15620}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 15620",
  "death": {"price": 15620, "side": "above", "rule": "2x5m"},
  "flip": {"price": 15470, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

// feasClock returns the fixed authoring clock the seam walk (class 60/113)
// requires of every test that drives the planner.
func feasClock() func() time.Time {
	now := time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

// (b) HINT RED→GREEN at the planner call site: an arm whose COMPOSED leg R:R
// is below the arm minimum (the case the executor really refuses, N1) feeds the
// repair prompt; the next attempt (higher target) writes active.
func TestWriteTimeFeasibilityHintRedToGreen(t *testing.T) {
	at := feasPlannerTrader(t, nil) // nil = ON (owner default)
	feasStubBars(t)
	red := strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15560`)
	red = strings.Replace(red, `"target_chain": [15550, 15620]`, `"target_chain": [15550, 15560]`, 1)
	red = strings.Replace(red, `"r_to_arm_target":7.0`, `"r_to_arm_target":1.0`, 1)
	green := strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15630`)
	green = strings.Replace(green, `"target_chain": [15550, 15620]`, `"target_chain": [15550, 15630]`, 1)
	green = strings.Replace(green, `"r_to_arm_target":7.0`, `"r_to_arm_target":8.0`, 1)
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashFeas1", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			if len(blocks) == 1 {
				return red, nil
			}
			return green, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("repair-then-success: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(blocks))
	}
	if blocks[0] != "FULLPROMPT" {
		t.Fatalf("attempt 1 must be the full author prompt, got %q", blocks[0])
	}
	// The repair prompt carries the refusal verbatim plus the fix vocabulary.
	for _, frag := range []string{
		"write-time feasibility:",
		"S1 would be refused at arm —",
		"below arm min",
		"widen the stop past the min-SL floor",
	} {
		if !strings.Contains(blocks[1], frag) {
			t.Fatalf("repair prompt missing %q:\n%s", frag, blocks[1])
		}
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil {
		t.Fatalf("no stored plan")
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if doc.Scenarios[0].Arm == nil || !doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("feasible re-write must keep the arm enabled, got %s", row.Doc)
	}
}

// (c) LAST ATTEMPT → arm.enabled=false + arm_disabled_reason (the reason
// CLASS) + counter, never a silent write. The refusal is the composed-leg R:R
// (the case the executor really refuses, N1) — the old min-SL fixture is an arm
// the executor would have PLACED with the floored stop.
func TestWriteTimeFeasibilityLastAttemptDisablesArm(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	red := strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15560`)
	red = strings.Replace(red, `"target_chain": [15550, 15620]`, `"target_chain": [15550, 15560]`, 1)
	red = strings.Replace(red, `"r_to_arm_target":7.0`, `"r_to_arm_target":1.0`, 1)
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashFeas2", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) { return red, nil })
	if err != nil || lc != "active" {
		t.Fatalf("last-attempt write: lc=%q err=%v", lc, err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil {
		t.Fatalf("no stored plan")
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Scenarios) == 0 || doc.Scenarios[0].Arm == nil {
		t.Fatalf("doc lost its arm: %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("last attempt must write the arm DISABLED, got enabled: %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.DisabledReason != "rr" {
		t.Fatalf("arm_disabled_reason must be the rr CLASS (SHOULD-FIX 6), got %q", doc.Scenarios[0].Arm.DisabledReason)
	}
	key := "arm_disabled_at_write:t1:2026-08-14:NY:rr"
	if n, err := store.SystemCounter(at.store, key); err != nil || n != 1 {
		t.Fatalf("counter %q = %d, %v (want 1)", key, n, err)
	}
}

// CTO RECHECK S6 — the disable WARN carries BOTH the class and the verbose
// verdict (an arm first authored infeasible on the last attempt still shows
// the numbers on that line). Asserted via the counter + doc, the verbose half
// rides writeTimeFeasibilityHint which the repair prompt test already pins.

// CTO B3 knob wiring — GeometryRefIDsEnabled resolves nil = ON, false = OFF
// (the same seam DS-102's executor uses).
func TestGeometryRefIDsKnobResolution(t *testing.T) {
	var nilCfg *store.DayPlanConfig
	if !nilCfg.GeometryRefIDsEnabled() {
		t.Fatalf("nil config must resolve ON (the owner default)")
	}
	c := &store.DayPlanConfig{}
	if !c.GeometryRefIDsEnabled() {
		t.Fatalf("nil pointer must resolve ON (the owner default)")
	}
	off := false
	c.GeometryReferenceLevels = &off
	if c.GeometryRefIDsEnabled() {
		t.Fatalf("explicit false must resolve OFF")
	}
}

// CTO heads-up (msg 1789746728638-241104) + RECHECK item 2 — the doc's
// zone_map is byte-identical before/after the verdicts AND after the disable
// path, on a REJECT fixture that actually enters the geometry path; the STORED
// row's zone_map equals facts.Zones exactly.
func TestWriteTimeFeasibilityNeverMutatesZoneMap(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	// In-memory half: verdicts + the disable path leave the map byte-identical.
	uLo, uHi := 15470.0, 15490.0
	unitZones := &kernel.LevelZoneMap{Zones: []kernel.LevelZone{{
		Anchor:  15480,
		Lo:      &uLo,
		Hi:      &uHi,
		Sources: []kernel.ZoneSource{{Price: 15480, Label: "PWL", TF: "1m"}},
	}}}
	var unitDoc kernel.PlanDoc
	if err := json.Unmarshal([]byte(infeasibleFeasPlanJSON), &unitDoc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	unitDoc.Zones = unitZones
	before, _ := json.Marshal(unitZones)
	_ = at.writeTimeFeasibilityVerdicts(&unitDoc, 8, at.config.StrategyConfig, "NY")
	mid, _ := json.Marshal(unitDoc.Zones)
	at.applyWriteTimeArmDisable(&unitDoc, []writeTimeFeasibilityIssue{{Scenario: "S1", Class: "rr", Verbose: "R:R 1.00 below arm min 2.00"}}, "2026-08-14", "NY")
	after, _ := json.Marshal(unitDoc.Zones)
	if string(mid) != string(before) || string(after) != string(before) {
		t.Fatalf("zone_map mutated: before=%s mid=%s after=%s", before, mid, after)
	}
	// Stored-row half: a reject at a real zone runs the whole pipeline and the
	// stored zone_map equals facts.Zones exactly.
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())
	l := identityTestLevel(now, 15480)
	l.Label = "PWL"
	candidates := kernel.BuildMapCandidates([]kernel.ScoredLevel{{DetectedLevel: l, Grade: "A", Score: 1}}, 15480, 10, kernel.MapCandidateOpts{})
	if len(candidates) != 1 || candidates[0].ID == nil {
		t.Fatalf("map did not expose identity: %+v", candidates)
	}
	lo, hi := 15470.0, 15490.0
	tLo, tHi := 15540.0, 15560.0
	zones := &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
		{Anchor: 15480, Lo: &lo, Hi: &hi, Sources: []kernel.ZoneSource{{Price: 15480, Label: "PWL", TF: l.FormationTF}}},
		{Anchor: 15550, Lo: &tLo, Hi: &tHi, Sources: []kernel.ZoneSource{{Price: 15550, Label: "RN 15550", TF: l.FormationTF}}},
	}}
	facts := kernel.PlanFacts{Price: 15550, DATR: 300, Zones: zones, IdentityMap: candidates}
	plan := strings.Replace(class39LegsPlanJSON("15550"),
		`"condition": "reject", "direction": "long"`,
		`"condition": "reject", "level_id": "`+*candidates[0].ID+`", "direction": "long"`, 1)
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashZP", "", "", "", "FULLPROMPT", facts, nil,
		map[float64]string{15480: "PWL", 15700: "RN 15700"}, nil, true,
		func(userPrompt string) (string, error) { return plan, nil })
	if err != nil || lc != "active" {
		t.Fatalf("real-zone reject must write active: lc=%q err=%v", lc, err)
	}
	want, _ := json.Marshal(zones)
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if doc.Zones == nil {
		t.Fatalf("stored doc lost its zone_map: %s", row.Doc)
	}
	got, _ := json.Marshal(doc.Zones)
	if string(got) != string(want) {
		t.Fatalf("stored zone_map mutated: want=%s got=%s", want, got)
	}
}

// CTO RECHECK item 5 (deferred parity, now buildable on the merged dev): the
// write site and ArmGeometryVerdict agree on a ref| ONH reject play — knob ON
// admits (no issue), OFF refuses. Same doc, same knob, both functions.
func TestWriteTimeFeasibilityParityWithArmGeometryVerdict(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	id := kernel.ReferenceLevelID("MNQ", "ONH", 29897, 29897, "2026-09-17", "1m")
	sym, kind := "MNQ", "ONH"
	identity := kernel.PlanLevel{Symbol: &sym, Kind: &kind, Lo: p(29897), Hi: p(29897),
		OriginDate: pStr("2026-09-17"), TF: pStr("1m"), Label: "ONH", Price: 29897, ID: id, Names: []string{"ONH"}}
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{identity},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: ""}}},
			{Anchor: 29950, Lo: p(29950), Hi: p(29954), Sources: []kernel.ZoneSource{{Kind: "SUPPLY", Price: 29952, Label: "Supply·1h", TF: "1h"}}},
		}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", LevelID: id, Condition: "reject", Direction: "long",
			Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 29897, Stop: 29887, Target: 29950}}},
	}
	for _, tc := range []struct {
		name    string
		on      bool
		wantBad bool
	}{
		{"ON admits", true, false},
		{"OFF refuses", false, true},
	} {
		at := feasPlannerTrader(t, nil)
		if !tc.on {
			f := false
			at.config.StrategyConfig.DayPlan.GeometryReferenceLevels = &f
		}
		issues := at.writeTimeFeasibilityVerdicts(doc, 20, at.config.StrategyConfig, "NY")
		if tc.wantBad && len(issues) == 0 {
			t.Fatalf("%s: write site must refuse the null-width line", tc.name)
		}
		if !tc.wantBad && len(issues) != 0 {
			t.Fatalf("%s: write site must admit the ref| ONH play, got %+v", tc.name, issues)
		}
		_, why := ArmGeometryVerdict(doc, doc.Scenarios[0], tc.on)
		if (why != "") != tc.wantBad {
			t.Fatalf("%s: ArmGeometryVerdict disagrees with the write site (why=%q)", tc.name, why)
		}
	}
}

func pStr(s string) *string { return &s }

// (d) KNOB OFF → byte-identical behaviour: the arm writes enabled on attempt 1,
// no repair, no disabled_reason stamp.
func TestWriteTimeFeasibilityOffIsByteIdentical(t *testing.T) {
	off := false
	at := feasPlannerTrader(t, &off)
	feasStubBars(t)
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashFeas3", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			return infeasibleFeasPlanJSON, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("OFF must write attempt 1: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 1 {
		t.Fatalf("OFF must not repair, got %d attempts", len(blocks))
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Scenarios) == 0 || doc.Scenarios[0].Arm == nil || !doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("OFF must keep the arm enabled (today's WARN-only behaviour), got %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.DisabledReason != "" {
		t.Fatalf("OFF must not stamp arm_disabled_reason, got %q", doc.Scenarios[0].Arm.DisabledReason)
	}
}

// TestWriteTimeFeasibilityStopSideHintText pins the amendment's hint words
// (CTO 2026-09-18): the trigger, its relation to price, and the two fixes.
func TestWriteTimeFeasibilityStopSideHintText(t *testing.T) {
	hint := writeTimeFeasibilityHint([]writeTimeFeasibilityIssue{
		{Scenario: "S1", Cond: "reclaim", Kind: "stop_side", Class: "stop_side_wrong", Trigger: 15480.50, Price: 15550.00, Side: "long"},
	})
	for _, frag := range []string{
		"write-time feasibility:",
		"S1 reclaim trigger 15480.50 is already below price 15550.00",
		"a stop entry there fills at market on placement",
		"author the trigger ahead of price",
		"author a reject/limit at the level",
	} {
		if !strings.Contains(hint, frag) {
			t.Fatalf("hint missing %q: %s", frag, hint)
		}
	}
	if strings.Contains(hint, "would be refused at arm") {
		t.Fatalf("stop-side hint must not carry the gate-kind suffix: %s", hint)
	}
}

// TestWriteTimeFeasibilityStopSideLastAttemptDisables — the amendment's (c): a
// reclaim whose stop trigger is already behind the read-time price rides the
// repair hint, and the last attempt writes arm.enabled=false with
// disabled_reason stop_side_wrong + the counter.
func TestWriteTimeFeasibilityStopSideLastAttemptDisables(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	// The stop-side predicate re-reads the last 1m close at verdict time
	// (SHOULD-FIX 7): make the tape say 15550 so the reclaim trigger 15480.50
	// is already through it.
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = feasBarsFor(15550, 0)
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashFeas4", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			return reclaimFeasPlanJSON, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("last-attempt write: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(blocks))
	}
	for _, frag := range []string{
		"trigger 15480.50 is already below price 15550.00",
		"a stop entry there fills at market on placement",
	} {
		if !strings.Contains(blocks[1], frag) {
			t.Fatalf("repair prompt missing %q:\n%s", frag, blocks[1])
		}
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil {
		t.Fatalf("no stored plan")
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if doc.Scenarios[0].Arm == nil || doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("last attempt must write the arm DISABLED, got %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.DisabledReason != "stop_side_wrong" {
		t.Fatalf("disabled_reason must be stop_side_wrong, got %q", doc.Scenarios[0].Arm.DisabledReason)
	}
	key := "arm_disabled_at_write:t1:2026-08-14:NY:stop_side_wrong"
	if n, err := store.SystemCounter(at.store, key); err != nil || n != 1 {
		t.Fatalf("counter %q = %d, %v (want 1)", key, n, err)
	}
}

// CTO BLOCKER 1 (RED→GREEN): with the frozen zone map + identity map stamped
// before judging, a reject play anchored at a REAL zone is ADMITTED at write
// with the knob ON — no hint, no disabled arm, exactly one attempt.
func TestWriteTimeFeasibilityRejectPlayAtRealZoneAdmitted(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())
	// A real machine identity for the class-39 fixture's PWL anchor at 15480.
	l := identityTestLevel(now, 15480)
	l.Label = "PWL"
	candidates := kernel.BuildMapCandidates([]kernel.ScoredLevel{{DetectedLevel: l, Grade: "A", Score: 1}}, 15480, 10, kernel.MapCandidateOpts{})
	if len(candidates) != 1 || candidates[0].ID == nil {
		t.Fatalf("map did not expose identity: %+v", candidates)
	}
	lo, hi := 15470.0, 15490.0
	tLo, tHi := 15540.0, 15560.0
	zones := &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
		{
			Anchor: 15480, Lo: &lo, Hi: &hi,
			Sources: []kernel.ZoneSource{{Price: 15480, Label: "PWL", TF: l.FormationTF}},
		},
		{ // the distinct complete target the composition requires beyond the entry zone
			Anchor: 15550, Lo: &tLo, Hi: &tHi,
			Sources: []kernel.ZoneSource{{Price: 15550, Label: "RN 15550", TF: l.FormationTF}},
		},
	}}
	facts := kernel.PlanFacts{Price: 15550, DATR: 300, Zones: zones, IdentityMap: candidates}
	// New-authoring plans carry the authored level_id; the stamp resolves it
	// against the frozen identity map (LevelByID re-checks the hash).
	plan := strings.Replace(class39LegsPlanJSON("15550"),
		`"condition": "reject", "direction": "long"`,
		`"condition": "reject", "level_id": "`+*candidates[0].ID+`", "direction": "long"`, 1)
	calls := 0
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashB1", "", "", "", "FULLPROMPT", facts, nil,
		map[float64]string{15480: "PWL", 15700: "RN 15700"}, nil, true,
		func(userPrompt string) (string, error) {
			calls++
			return plan, nil
		})
	if err != nil || lc != "active" {
		t.Fatalf("real-zone reject must be ADMITTED at write: lc=%q err=%v", lc, err)
	}
	if calls != 1 {
		t.Fatalf("admission must not burn a retry, got %d calls", calls)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if doc.Scenarios[0].Arm == nil || !doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("admitted reject arm must stay enabled, got %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.DisabledReason != "" {
		t.Fatalf("admitted arm must carry no disabled_reason, got %q", doc.Scenarios[0].Arm.DisabledReason)
	}
}

// CTO SHOULD-FIX 8 (burned attempts): a hard validator keeps its attempt, the
// write-time check runs LAST, and a plan whose arm only becomes infeasible on
// the final attempt still writes with arm.enabled=false — the hint was seen.
func TestWriteTimeFeasibilityNeverPreemptsHardRejects(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	good := class39LegsPlanJSON("15550")
	bad := strings.Replace(good, `"target_chain": [15550, 15620]`, `"target_chain": [15620]`, 1) // hard economics reject
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashS8", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15700: "RN 15700"}, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			switch len(blocks) {
			case 1:
				return bad, nil // hard validator burns attempt 1
			default:
				return good, nil // attempts 2-3: reject arm, no zone map → geometry refusal at write
			}
		})
	if err != nil || lc != "active" {
		t.Fatalf("burned-attempts write: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 3 {
		t.Fatalf("hard reject + hint + last-attempt = 3 calls, got %d", len(blocks))
	}
	// The hint rides the ATTEMPT-3 prompt (the repair from attempt 2's verdict).
	// This is the documented burned-attempts cost: the model sees the hint but
	// has no attempt left to act on it (CTO SHOULD-FIX 8).
	if !strings.Contains(blocks[2], "write-time feasibility:") {
		t.Fatalf("attempt 3 must carry the feasibility hint, got %q", blocks[2][:120])
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if doc.Scenarios[0].Arm == nil || doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("last attempt must write the arm disabled, got %s", row.Doc)
	}
	if doc.Scenarios[0].Arm.DisabledReason != "geometry_no_provenance" {
		t.Fatalf("disabled_reason must be the geometry class, got %q", doc.Scenarios[0].Arm.DisabledReason)
	}
}
