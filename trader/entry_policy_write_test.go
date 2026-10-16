package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"vl/kernel"
	"vl/store"
)

// W-EXEC-TRUTH W3 (K) — the write loop under the entry policy, driven through
// the PRODUCTION write path (runPlannerReadCoreWithFactsGradesClock → the
// parse stamp + hold floor → writeTimeZoneVerdicts → applyWriteTimeArmDisable)
// with the shipped default (entry_policy_default unset = market_in_zone).

// accPlan is one acceptance scenario (a time_hold play legacy could never
// arm) whose arm geometry, zone and hold are the variables.
type accPlan struct {
	zone          [2]float64
	entry, stop   float64
	target        float64
	holdMin       int // 0 = no hold_min stored
	enabled       bool
	obstacle      float64
	confirmRef    float64
	targetChain   []float64
	omitEconomics bool
}

func defaultAccPlan() accPlan {
	return accPlan{zone: [2]float64{15484, 15494}, entry: 15488, stop: 15460, target: 15620, holdMin: 5, enabled: true, obstacle: 15550, confirmRef: 15480}
}

func (p accPlan) json(t *testing.T) string {
	t.Helper()
	confirm := map[string]any{"rule": "time_hold", "ref_price": p.confirmRef, "side": "above"}
	if p.holdMin > 0 {
		confirm["hold_min"] = p.holdMin
	}
	risk := math.Abs(p.entry - p.stop)
	chain := p.targetChain
	if chain == nil {
		chain = []float64{p.obstacle, p.target}
	}
	sc := map[string]any{
		"id": "S1", "trigger": "acceptance above 15480", "condition": "acceptance", "direction": "long",
		"target_chain": chain, "invalid": "2x5m<15470", "quality": "A", "confirm": confirm,
		"arm": map[string]any{"enabled": p.enabled, "entry": p.entry, "stop": p.stop, "target": p.target, "wait_confirm": true},
	}
	if !p.omitEconomics {
		sc["economics"] = map[string]any{
			"entry_zone":      []float64{p.zone[0], p.zone[1]},
			"first_obstacle":  map[string]any{"price": p.obstacle, "level": "fixture reference", "family": "reference", "response": "pass_through"},
			"r_to_obstacle":   math.Abs(p.obstacle-p.entry) / risk,
			"r_to_arm_target": math.Abs(p.target-p.entry) / risk,
		}
	}
	doc := map[string]any{
		"reasoning": "Balance above PWL; accept above it.",
		"bias":      map[string]any{"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15480"},
		"levels": []any{
			map[string]any{"price": 15480, "label": "PWL", "grade": "A", "instruction": "accept"},
			map[string]any{"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"},
		},
		"scenarios":       []any{sc},
		"no_trade":        []string{"first 5m"},
		"death_condition": "acceptance above 15620",
		"death":           map[string]any{"price": 15620, "side": "above", "rule": "2x5m"},
		"flip":            map[string]any{"price": 15480, "side": "below", "rule": "2x5m", "flip_to": "short"},
		"day_type":        "balance",
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runAcc drives the write loop with a fixed reply sequence (the last reply
// repeats) and returns the lifecycle, the prompts, and the stored doc.
func runAcc(t *testing.T, at *AutoTrader, replies ...string) (string, []string, *kernel.PlanDoc, error) {
	t.Helper()
	feasStubBars(t)
	var blocks []string
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashW3", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) {
			blocks = append(blocks, userPrompt)
			i := len(blocks) - 1
			if i >= len(replies) {
				i = len(replies) - 1
			}
			return replies[i], nil
		})
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil || row.Lifecycle != "active" {
		return lc, blocks, nil, err
	}
	var doc kernel.PlanDoc
	if uerr := json.Unmarshal([]byte(row.Doc), &doc); uerr != nil {
		t.Fatalf("doc unmarshal: %v", uerr)
	}
	return lc, blocks, &doc, err
}

// (a)+R4 at the write site: an acceptance arm — refused by legacy since GAR-F4
// — is STAMPED market_in_zone at parse, passes the zone and the gates at the
// zone's worst fills, and is stored ENABLED with the policy.
func TestW3WriteLoopAcceptanceArmStoredWithPolicy(t *testing.T) {
	at := plannerTestTrader(t)
	lc, blocks, doc, err := runAcc(t, at, defaultAccPlan().json(t))
	if err != nil || lc != "active" || doc == nil {
		t.Fatalf("acceptance arm must write: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 1 {
		t.Fatalf("a clean arm writes on attempt 1, took %d", len(blocks))
	}
	a := doc.Scenarios[0].Arm
	if a == nil || !a.Enabled || a.Policy != kernel.EntryPolicyMarketInZone || a.DisabledReason != "" {
		t.Fatalf("stored arm: %+v", a)
	}
	// The composer's kind table follows the stored policy: a limit.
	if kind, refusal := armLegKindFor(doc.Scenarios[0], kernel.PlanArmLeg{}); kind != kernel.ArmKindLimit || refusal != "" {
		t.Fatalf("armLegKindFor under market_in_zone: %q %q", kind, refusal)
	}
}

// entry_policy_default=legacy (the explicit off): the same plan is the pre-W3
// behaviour — the acceptance arm is refused by the legacy validator on every
// attempt and no plan is written with it.
func TestW3WriteLoopLegacyDefaultKeepsTheOldLaw(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.StrategyConfig.DayPlan.EntryPolicyDefault = store.EntryPolicyLegacy
	lc, blocks, doc, _ := runAcc(t, at, defaultAccPlan().json(t))
	if doc != nil || lc == "active" {
		t.Fatalf("legacy: the acceptance arm must not write (GAR-F4), lc=%q", lc)
	}
	if len(blocks) < 2 || !strings.Contains(blocks[1], "breakout_retest is a normal AI play and never arms") {
		t.Fatalf("legacy repair must carry the legacy refusal")
	}
}

// (c) a zone violation is HINTED (the "entry zone:" marker routes the zone
// law), then on the last attempt the arm is written DISABLED with the zone
// code as arm_disabled_reason + the counter — never a silent write.
func TestW3WriteLoopZoneViolationHintedThenDisabled(t *testing.T) {
	at := plannerTestTrader(t)
	wide := defaultAccPlan()
	wide.zone = [2]float64{15480.5, 15494} // 13.5 pt > zone_max_pts 10
	lc, blocks, doc, err := runAcc(t, at, wide.json(t))
	if err != nil || lc != "active" || doc == nil {
		t.Fatalf("last attempt writes the arm disabled: lc=%q err=%v", lc, err)
	}
	if len(blocks) != 3 {
		t.Fatalf("hint, hint, disable = 3 attempts, got %d", len(blocks))
	}
	for _, frag := range []string{"S1 entry zone: zone_too_wide", "13.50 pts wide > zone_max_pts 10", kernel.RepairEntryZoneLaw} {
		if !strings.Contains(blocks[1], frag) {
			t.Fatalf("repair prompt missing %q", frag)
		}
	}
	a := doc.Scenarios[0].Arm
	if a.Enabled || a.DisabledReason != kernel.ZoneTooWide {
		t.Fatalf("want disabled zone_too_wide, got enabled=%v reason=%q", a.Enabled, a.DisabledReason)
	}
	if n, err := store.SystemCounter(at.store, "arm_disabled_at_write:t1:2026-08-14:NY:"+kernel.ZoneTooWide); err != nil || n != 1 {
		t.Fatalf("counter = %d, %v", n, err)
	}
	// NOT gated by write_time_feasibility (D5): OFF still judges the zone.
	at2 := plannerTestTrader(t)
	off := false
	at2.config.StrategyConfig.DayPlan.WriteTimeFeasibility = &off
	_, _, doc2, _ := runAcc(t, at2, wide.json(t))
	if doc2 == nil || doc2.Scenarios[0].Arm.Enabled || doc2.Scenarios[0].Arm.DisabledReason != kernel.ZoneTooWide {
		t.Fatal("the zone check must run with write_time_feasibility OFF (D5)")
	}
}

// A hint the planner acts on: attempt 2 fixes the zone and the arm stays enabled.
func TestW3WriteLoopZoneHintRedToGreen(t *testing.T) {
	at := plannerTestTrader(t)
	wide := defaultAccPlan()
	wide.zone = [2]float64{15480.5, 15494}
	lc, blocks, doc, err := runAcc(t, at, wide.json(t), defaultAccPlan().json(t))
	if err != nil || lc != "active" || doc == nil || len(blocks) != 2 || !doc.Scenarios[0].Arm.Enabled {
		t.Fatalf("repair-then-success: lc=%q err=%v attempts=%d", lc, err, len(blocks))
	}
}

// D10 — hold 2 on an ARMED market_in_zone acceptance is refused at parse on
// every attempt (its own law in the repair prompt); the same scenario UNARMED
// writes.
func TestW3WriteLoopHoldFloor(t *testing.T) {
	at := plannerTestTrader(t)
	short := defaultAccPlan()
	short.holdMin = 2
	lc, blocks, doc, _ := runAcc(t, at, short.json(t))
	if doc != nil || lc == "active" {
		t.Fatalf("hold 2 armed must not write, lc=%q", lc)
	}
	if len(blocks) < 2 || !strings.Contains(blocks[1], kernel.ArmableHoldFloorMarker) || !strings.Contains(blocks[1], kernel.RepairArmableHoldFloorLaw) {
		t.Fatal("the repair prompt must carry the armable hold floor refusal + its law")
	}
	at2 := plannerTestTrader(t)
	unarmed := short
	unarmed.enabled = false
	lc2, _, doc2, err := runAcc(t, at2, unarmed.json(t))
	if err != nil || lc2 != "active" || doc2 == nil {
		t.Fatalf("unarmed hold 2 must write: lc=%q err=%v", lc2, err)
	}
	// The knob: min_hold_min 1 admits hold 2 armed.
	at3 := plannerTestTrader(t)
	one := 1
	at3.config.StrategyConfig.DayPlan.MinHoldMin = &one
	if lc3, _, doc3, _ := runAcc(t, at3, short.json(t)); lc3 != "active" || doc3 == nil || !doc3.Scenarios[0].Arm.Enabled {
		t.Fatalf("min_hold_min 1 must admit hold 2: lc=%q", lc3)
	}
}

// Zone boundaries through the write loop — every verdict the production path
// reaches, inclusive bounds, ±1 tick, width max/max+1 tick, inward rounding,
// R:R at the FAR bound vs the authored entry, min-SL composed from the NEAR
// bound, and the trigger side.
func TestW3WriteLoopZoneBoundaries(t *testing.T) {
	type tc struct {
		name   string
		mut    func(p *accPlan)
		reason string // "" = stored enabled
	}
	cases := []tc{
		{"entry at lo (inclusive)", func(p *accPlan) { p.entry = 15484 }, ""},
		{"entry at hi (inclusive)", func(p *accPlan) { p.entry = 15494; p.target = 15640; p.targetChain = []float64{15550, 15640} }, ""},
		{"entry 1 tick below lo", func(p *accPlan) { p.entry = 15483.75 }, kernel.ZoneEntryOutside},
		{"width = max", func(p *accPlan) { p.zone = [2]float64{15484, 15494} }, ""},
		{"width = max + 1 tick", func(p *accPlan) { p.zone = [2]float64{15483.75, 15494} }, kernel.ZoneTooWide},
		{"trigger side: lo = ref − 1 tick admitted", func(p *accPlan) { p.zone = [2]float64{15479.75, 15489}; p.entry = 15484 }, ""},
		{"trigger side: lo = ref − 2 ticks refused", func(p *accPlan) { p.zone = [2]float64{15479.5, 15489}; p.entry = 15484 }, kernel.ZoneTriggerSide},
		{"bracket: stop inside the zone", func(p *accPlan) { p.stop = 15485; p.entry = 15490 }, kernel.ZoneBracket},
		{"inward rounding leaves no tick", func(p *accPlan) { p.zone = [2]float64{15488.1, 15488.2}; p.entry = 15488.15 }, kernel.ZoneEmpty},
		{"no zone at all", func(p *accPlan) { p.zone = [2]float64{0, 0} }, ""}, // economics refuses a zero zone first (hard reject) — see below
		// R:R (the arm floor here is the schema default 3.0): at the authored
		// entry 15484 → (15570−15484)/24 = 3.58 ≥ 3; at the FAR edge 15494 →
		// 76/34 = 2.24 < 3 → refused rr (D7).
		{"R:R passes at entry, fails at the far edge", func(p *accPlan) {
			p.entry, p.target, p.obstacle, p.targetChain = 15484, 15570, 15530, []float64{15530, 15570}
		}, "rr"},
		// min-SL: authored stop 15480 is tight; composed from the NEAR edge
		// (15484 − 1.5×ATR5m 8 = 15472) the near-edge distance is 12.00 = the
		// floor → admitted. Composed from the far edge it would be 4.50 → min_sl.
		{"tight stop composed from the near edge", func(p *accPlan) { p.stop = 15479.75; p.entry = 15486 }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := defaultAccPlan()
			c.mut(&p)
			at := plannerTestTrader(t)
			lc, blocks, doc, err := runAcc(t, at, p.json(t))
			if c.name == "no zone at all" {
				// A zero zone never reaches the zone verdict: the new-authoring
				// economics contract refuses it as a hard reject (a different law).
				if doc != nil && doc.Scenarios[0].Arm.Enabled {
					t.Fatal("a missing zone must never be written enabled")
				}
				return
			}
			if err != nil || lc != "active" || doc == nil {
				t.Fatalf("lc=%q err=%v attempts=%d last prompt=%q", lc, err, len(blocks), lastN(blocks, 400))
			}
			a := doc.Scenarios[0].Arm
			if c.reason == "" {
				if !a.Enabled || a.DisabledReason != "" {
					t.Fatalf("must be admitted, got disabled %q; repair=%q", a.DisabledReason, lastN(blocks, 400))
				}
				return
			}
			if a.Enabled || a.DisabledReason != c.reason {
				t.Fatalf("want disabled %q, got enabled=%v reason=%q", c.reason, a.Enabled, a.DisabledReason)
			}
		})
	}
}

func lastN(blocks []string, n int) string {
	if len(blocks) == 0 {
		return ""
	}
	s := blocks[len(blocks)-1]
	if len(s) > n {
		return s[:n]
	}
	return s
}

// The zone verdict at the unit level: the gate chain judges the FAR bound for
// R:R and the near bound for min-SL (the stop composed from the near bound),
// and provenance is a label that never refuses (no frozen map here).
func TestW3ZoneLegVerdictWorstFills(t *testing.T) {
	at := plannerTestTrader(t)
	feasStubBars(t)
	d, err := kernel.ParsePlanDocForAuthoring(defaultAccPlan().json(t), 12, 5, at.plannerAuthoringOpts())
	if err != nil {
		t.Fatal(err)
	}
	sc := d.Scenarios[0]
	is, label, ok := at.zoneLegVerdict(d, sc, kernel.PlanArmLeg{Entry: sc.Arm.Entry, Stop: sc.Arm.Stop, Target: sc.Arm.Target}, "long", at.lazyArmVetoSnapshot()(), 8, "", at.config.StrategyConfig, "NY", 0.25, 10)
	if !ok {
		t.Fatalf("admitted leg refused: %+v", is)
	}
	for _, frag := range []string{"[15484.00, 15494.00]", "far=15494.00", "near=15484.00", "provenance=planner_only(frozen_zone_map_missing)"} {
		if !strings.Contains(label, frag) {
			t.Errorf("label %q missing %q", label, frag)
		}
	}
}

// The boot line READS every knob with its origin letter; D12 warns only when
// the default policy meets one_setup ON.
func TestW3EntryLawBootLineAndOneSetupWarn(t *testing.T) {
	line := entryLawBootLine(nil)
	want := "🎛 entry law: write_feas=on · entry_policy_default=market_in_zone[I] zone_max_pts=10[I] zone_rest_max_min=30[I] zone_place_within_pts=25[I] min_hold_min=3[I]"
	if line != want {
		t.Fatalf("boot line:\n got %q\nwant %q", line, want)
	}
	z, r, h := 6.5, 45, 4
	w := 20.0
	dp := &store.DayPlanConfig{EntryPolicyDefault: "planned_order", ZoneMaxPts: &z, ZoneRestMaxMin: &r, MinHoldMin: &h, ZonePlaceWithinPts: &w}
	if got := entryLawBootLine(dp); !strings.Contains(got, "entry_policy_default=planned_order[O] zone_max_pts=6.5[O] zone_rest_max_min=45[O] zone_place_within_pts=20[O] min_hold_min=4[O]") {
		t.Fatalf("saved values must read [O]: %q", got)
	}
	bogus := &store.DayPlanConfig{EntryPolicyDefault: "market"}
	if got := entryLawBootLine(bogus); !strings.Contains(got, "entry_policy_default=market_in_zone[I]") {
		t.Fatalf("an invalid saved value resolves to the default [I]: %q", got)
	}
	cfg := &store.StrategyConfig{DayPlan: &store.DayPlanConfig{}}
	if w := entryPolicyOneSetupWarn(cfg); !strings.Contains(w, "one_setup_enabled=true[I]") || !strings.Contains(w, "play_not_reject") {
		t.Fatalf("D12 WARN missing: %q", w)
	}
	off := false
	cfg.DayPlan.OneSetupEnabled = &off
	if w := entryPolicyOneSetupWarn(cfg); w != "" {
		t.Fatalf("one_setup OFF → no WARN, got %q", w)
	}
	on := true
	cfg.DayPlan.OneSetupEnabled = &on
	cfg.DayPlan.EntryPolicyDefault = store.EntryPolicyLegacy
	if w := entryPolicyOneSetupWarn(cfg); w != "" {
		t.Fatalf("legacy policy → no WARN, got %q", w)
	}
}

// The four knobs are rows of the effective-settings table, each resolved by
// its store resolver with the source it reports.
func TestW3EffectiveSettingsRows(t *testing.T) {
	for _, p := range []string{"entry_policy_default", "zone_max_pts", "zone_rest_max_min", "min_hold_min"} {
		r, ok := effectiveResolvers[dpPath+p]
		if !ok {
			t.Fatalf("%s: no effective resolver", p)
		}
		if !strings.HasPrefix(r.name, "store.Resolve") {
			t.Errorf("%s resolver %q is not the store resolver", p, r.name)
		}
	}
	rows := effRowsFor(t, `{"day_plan":{"plan_enabled":true,"zone_max_pts":7.5,"entry_policy_default":"legacy"}}`, "ninjatrader", "")
	if r := rows["day_plan.zone_max_pts"]; fmt.Sprint(r.Effective) != "7.5" || r.Origin != store.SourceSaved {
		t.Fatalf("zone_max_pts row: %+v", r)
	}
	if r := rows["day_plan.entry_policy_default"]; fmt.Sprint(r.Effective) != "legacy" {
		t.Fatalf("entry_policy_default row: %+v", r)
	}
	if r := rows["day_plan.zone_rest_max_min"]; fmt.Sprint(r.Effective) != "30" || r.Origin != store.SourceShippedDefault {
		t.Fatalf("zone_rest_max_min row: %+v", r)
	}
}
