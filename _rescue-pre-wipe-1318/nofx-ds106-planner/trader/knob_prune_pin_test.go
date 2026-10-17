package trader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// ── W-KNOB-PRUNE (2026-09-18) — BYTE-IDENTICAL PINS AT THE SHIPPED DEFAULTS ──
//
// Written at the base commit (origin/dev 0dd27940) BEFORE the prune and re-run
// after it. The configs are the JSON the live strategies actually store
// (sqlite3 -readonly data.db, 2026-09-18 00:47 CT): "seed" is what the Studio
// wrote for every non-owner strategy, "owner" is the MNQ strategy
// a5b7662e-7bf7-49bb-9f09-7efa48f95ac8 verbatim (structure_map, scenario_cap 5,
// realign_cap 10, wake_on_htf_ob, levels_fresh_by_tf all NON-default), "empty"
// is {} and "nil" is no day_plan block at all.
//
// Regenerate: KNOB_PRUNE_WRITE_GOLDEN=1 go test ./trader -run TestKnobPrunePin

const knobPruneSeedJSON = `{"acceptance_rule":"5m_close","approval_required":false,"eod_flat_ct":"14:45","evening_digest":true,"last_entry_ct":"13:00","max_levels":8,"plan_enabled":true,"plan_mode":"advisory","planner_timeframes":["D","4h","1h","15m"],"proximity_filter_atr":1.5,"replan_cap":2,"scenario_cap":3,"sessions_enabled":["NY"]}`

const knobPruneOwnerJSON = `{"plan_enabled":true,"plan_mode":"strict","one_setup_enabled":false,"planner_timeframes":["D","4h","1h","15m","5m"],"structure_map":true,"proximity_filter_atr":1,"max_levels":12,"scenario_cap":5,"flip_reread":true,"acceptance_rule":"5m_close","replan_cap":4,"sessions_enabled":["NY"],"approval_required":false,"evening_digest":true,"realign_cap":10,"last_entry_ct":"13:00","eod_flat_ct":"14:45","sessions":[{"session":"NY","replan_cap":4,"acceptance_rule":"5m_close","min_grade":"B","max_trades":10},{"session":"ASIA","enable":true,"replan_cap":4,"acceptance_rule":"5m_close","min_grade":"B","max_trades":7},{"session":"LONDON","enable":true,"replan_cap":4,"acceptance_rule":"5m_close","min_grade":"B","max_trades":10}],"wake_on_htf_ob":true,"levels_fresh_by_tf":true}`

// knobPruneAllOffJSON is the ONLY stored shape that turns level-event wakes
// off: every legacy wake switch explicitly false (nil reads ON).
const knobPruneAllOffJSON = `{"plan_enabled":true,"wake_on_15m_zone":false,"wake_on_htf_zone":false,"wake_on_htf_ob":false,"wake_on_seated_invalidation":false,"wake_on_ifvg":false}`

func knobPruneConfigs(t *testing.T) map[string]*store.DayPlanConfig {
	t.Helper()
	out := map[string]*store.DayPlanConfig{"nil": nil}
	for name, raw := range map[string]string{"empty": `{}`, "seed": knobPruneSeedJSON, "owner": knobPruneOwnerJSON, "alloff": knobPruneAllOffJSON} {
		var dp store.DayPlanConfig
		if err := json.Unmarshal([]byte(raw), &dp); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = &dp
	}
	return out
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
		t.Fatalf("%s: output moved from the pinned golden\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// Wake decisions: the W6 collector on a fixture that forms a 15m reversal zone,
// 1h/4h S/D zones and (where the detector finds them) OBs / iFVGs, under every
// stored config shape. Pins wake_on_15m_zone / wake_on_htf_ob / wake_on_ifvg
// (collapsed into wake_on_level_events) and wake_min_interval_min (folded).
func TestKnobPrunePin_WakeCandidates(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	b15 := zonePattern15m(now.UnixMilli())
	rows := make([][4]float64, 0, len(b15))
	for _, b := range b15 {
		rows = append(rows, [4]float64{b.Open, b.High, b.Low, b.Close})
	}
	byTF := map[string][]market.Kline{"15m": b15, "1h": wakeBars(60, now.UnixMilli(), rows), "4h": wakeBars(240, now.UnixMilli(), rows)}
	fetch := func(tf string, count int) []market.Kline { return byTF[tf] }
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-30 * 24 * time.Hour),
		Doc: `{"levels":[{"label":"Demand 1h","price":104.0},{"label":"Supply 1h","price":99.0}]}`}
	type cand struct {
		Key, Kind, Tier, Desc string
		Prio                  int
		BirthMs               int64
	}
	out := map[string]struct {
		Candidates []cand
		Interval   int
	}{}
	for name, cfg := range knobPruneConfigs(t) {
		cs := collectLevelWakeCandidates(cfg, fetch, "MNQ", row, now)
		list := []cand{}
		for _, c := range cs {
			list = append(list, cand{c.key, c.kind, c.tier, c.desc, c.prio, c.birthMs})
		}
		out[name] = struct {
			Candidates []cand
			Interval   int
		}{list, cfg.WakeMinIntervalMinutes()}
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	knobPruneGolden(t, "wake_candidates.json", append(data, '\n'))
}

// NEW coupling pinned (review of #172): WakeOnHTFOrderBlocks() ANDs the legacy
// wake_on_htf_ob with the single switch. Pre-prune the OB class was gated only
// by its own field, so {wake_on_level_events:false, wake_on_htf_ob:true} would
// have yielded the two OB candidates; the intended single-switch semantics is
// that OFF disables EVERY level-event class, OBs included. Inert for every
// stored strategy today (none stores the new switch); pinned so it cannot
// drift. Kept out of the pre-prune golden map on purpose — this is a new rule,
// not a before/after identity.
func TestKnobPrunePin_WakeCandidates_SingleSwitchOwnsOB(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	b15 := zonePattern15m(now.UnixMilli())
	rows := make([][4]float64, 0, len(b15))
	for _, b := range b15 {
		rows = append(rows, [4]float64{b.Open, b.High, b.Low, b.Close})
	}
	byTF := map[string][]market.Kline{"15m": b15, "1h": wakeBars(60, now.UnixMilli(), rows), "4h": wakeBars(240, now.UnixMilli(), rows)}
	fetch := func(tf string, count int) []market.Kline { return byTF[tf] }
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-30 * 24 * time.Hour),
		Doc: `{"levels":[{"label":"Demand 1h","price":104.0},{"label":"Supply 1h","price":99.0}]}`}
	var dp store.DayPlanConfig
	if err := json.Unmarshal([]byte(`{"plan_enabled":true,"wake_on_level_events":false,"wake_on_htf_ob":true}`), &dp); err != nil {
		t.Fatal(err)
	}
	if dp.WakeOnHTFOrderBlocks() {
		t.Fatal("wake_on_level_events=false must switch the OB class off too")
	}
	if cands := collectLevelWakeCandidates(&dp, fetch, "MNQ", row, now); len(cands) != 0 {
		t.Fatalf("single switch OFF + legacy wake_on_htf_ob=true must yield ZERO wake candidates, got %+v", cands)
	}
	// And the fixture DOES produce OBs when the switch is on (so the zero above is the gate, not the fixture).
	on := true
	dp.WakeOnLevelEvents = &on
	obs := 0
	for _, c := range collectLevelWakeCandidates(&dp, fetch, "MNQ", row, now) {
		if c.kind == "ob" {
			obs++
		}
	}
	if obs == 0 {
		t.Fatal("fixture must yield OB candidates with the switch on")
	}
}

// Config resolvers every folded knob feeds, per stored shape. Pins
// acceptance_rule, realign_cap, evening_digest, scenario_cap,
// wake_min_interval_min, structure_map, levels_fresh_by_tf and the HTF
// multiplier the planner resolves (the last one re-pinned at 1.0, see report).
func TestKnobPrunePin_Resolvers(t *testing.T) {
	type res struct {
		Acceptance   string
		RealignCap   int
		ScenarioCap  int
		Digest       bool
		WakeInterval int
		StructureMap bool
		Freshness    string
		HTFMult      float64
		MaxLevels    int
	}
	out := map[string]res{}
	for name, cfg := range knobPruneConfigs(t) {
		at := &AutoTrader{id: "t1", exchange: "ninjatrader", config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{DayPlan: cfg}}}
		sm, _ := store.ResolveStructureMap(at.config.StrategyConfig)
		maxLevels, _, mult, _, _ := resolveSessionPlanCfg(cfg, "NY")
		out[name] = res{
			Acceptance:   cfg.AcceptanceRuleFor("NY"),
			RealignCap:   at.RealignCap(),
			ScenarioCap:  at.scenarioCap(),
			Digest:       at.eveningDigestEnabled(),
			WakeInterval: cfg.WakeMinIntervalMinutes(),
			StructureMap: sm,
			Freshness:    freshnessBootLabel(cfg),
			HTFMult:      mult,
			MaxLevels:    maxLevels,
		}
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	knobPruneGolden(t, "resolvers.json", append(data, '\n'))
}
