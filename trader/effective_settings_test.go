package trader

// W-EXEC-TRUTH W1 (g) — the settings rows against the PRODUCTION resolvers they
// name, at the functions' own call sites (canon 53): every value a row reports
// must equal what the AutoTrader method / store method / kernel function
// returns for the same stored config.

import (
	"encoding/json"
	"strings"
	"testing"

	"vl/store"
)

func effRowsFor(t *testing.T, raw, venue, session string) map[string]EffectiveKnob {
	t.Helper()
	rows, err := EffectiveSettings(raw, venue, session, store.ExplicitZeroRecord{})
	if err != nil {
		t.Fatalf("EffectiveSettings: %v", err)
	}
	out := make(map[string]EffectiveKnob, len(rows))
	for _, r := range rows {
		out[r.Path] = r
	}
	return out
}

// jsonVal normalises a row value the way the API client sees it.
func jsonVal(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %v: %v", v, err)
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	ab, _ := json.Marshal(jsonVal(t, a))
	bb, _ := json.Marshal(jsonVal(t, b))
	return string(ab) == string(bb)
}

// The two W1-INTEGRATE adapters report the SAME value as today's production
// resolver across the whole matrix, with the origin its branch implies.
func TestEffectiveAdaptersMatchProductionResolvers(t *testing.T) {
	halt := func(n *int) *store.StrategyConfig {
		return &store.StrategyConfig{RiskControl: store.RiskControlConfig{ConsecutiveLossHalt: n}}
	}
	for _, tc := range []struct {
		name   string
		cfg    *store.StrategyConfig
		env    string
		want   int
		origin string
	}{
		{"saved 3", halt(store.IntPtr(3)), "", 3, store.SourceSaved},
		{"saved 3 beats env 5", halt(store.IntPtr(3)), "5", 3, store.SourceSaved},
		{"saved 0 = OFF beats env 5 (W1)", halt(store.IntPtr(0)), "5", 0, store.SourceSaved},
		{"unset, env 5", halt(nil), "5", 5, "env BREAKER_HALT_N"},
		{"unset, env 0 = off", halt(nil), "0", 0, "env BREAKER_HALT_N"},
		{"unset, env invalid → default, named", halt(nil), "x", 8, store.SourceShippedDefault + " (env BREAKER_HALT_N invalid)"},
		{"unset, no env → default", halt(nil), "", 8, store.SourceShippedDefault},
		{"nil config", nil, "", 8, store.SourceShippedDefault},
	} {
		t.Setenv("BREAKER_HALT_N", tc.env)
		n, src := effBreakerHalt(tc.cfg)
		if rt := breakerHaltN(tc.cfg); n != rt || n != tc.want {
			t.Fatalf("%s: row %d, breakerHaltN %d, want %d", tc.name, n, rt, tc.want)
		}
		if src != tc.origin {
			t.Fatalf("%s: origin %q, want %q", tc.name, src, tc.origin)
		}
	}

	zero, three := 0, 3
	for _, tc := range []struct {
		name    string
		dp      *store.DayPlanConfig
		session string
		want    int
		origin  string
	}{
		{"nil block", nil, "NY", 2, store.SourceShippedDefault},
		{"strategy 3", &store.DayPlanConfig{ReplanCap: store.IntPtr(3)}, "NY", 3, store.SourceStrategyValue},
		{"strategy 0 is 0 (W1)", &store.DayPlanConfig{ReplanCap: store.IntPtr(0)}, "NY", 0, store.SourceStrategyValue},
		{"strategy unset → default", &store.DayPlanConfig{}, "NY", 2, store.SourceShippedDefault},
		{"session 0 override", &store.DayPlanConfig{ReplanCap: store.IntPtr(3), Sessions: []store.DayPlanSessionOverride{{Session: "NY", ReplanCap: &zero}}}, "NY", 0, store.SourceSessionOverride},
		{"session override, other case", &store.DayPlanConfig{Sessions: []store.DayPlanSessionOverride{{Session: "ny", ReplanCap: &three}}}, "NY", 3, store.SourceSessionOverride},
		{"other session inherits", &store.DayPlanConfig{ReplanCap: store.IntPtr(3), Sessions: []store.DayPlanSessionOverride{{Session: "NY", ReplanCap: &zero}}}, "ASIA", 3, store.SourceStrategyValue},
	} {
		n, src := effReplanCap(tc.dp, tc.session)
		if rt := tc.dp.ReplanCapFor(tc.session); n != rt || n != tc.want {
			t.Fatalf("%s: row %d, ReplanCapFor %d, want %d", tc.name, n, rt, tc.want)
		}
		if src != tc.origin {
			t.Fatalf("%s: origin %q, want %q", tc.name, src, tc.origin)
		}
	}
}

// Every row whose resolver is an AutoTrader method (or a trader function)
// reports exactly what that method returns for an AutoTrader carrying the same
// parsed config.
func TestEffectiveRowsMatchAutoTraderMethods(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	for _, raw := range []string{
		`{}`,
		`{"strategy_type":"ai_trading","ai_config":{"risk_control":{
			"max_contracts_per_order":1,"reentry_cooldown_minutes":20,"daily_loss_limit_usd":450,
			"consecutive_loss_halt":3,"breakeven_enabled":true,"breakeven_trigger_points":30,
			"trailing_enabled":true,"trailing_atr_mult":0,"trailing_arm":"immediate","min_risk_reward_ratio":2}},
		  "day_plan":{"plan_enabled":true,"proximity_filter_atr":0.3,"approval_required":true,"fade_or_wide_k":1.5,
			"replan_cap":4,"sessions_enabled":["NY","ASIA"],
			"picture_htf":{"enabled":true,"min_rr":1.5},
			"sessions":[{"session":"NY","enable":false,"replan_cap":1}]}}`,
		`{"strategy_type":"ai_trading","ai_config":{"risk_control":{"guardrails_enabled":false,"daily_loss_limit_usd":450}},
		  "day_plan":{"proximity_filter_atr":9,"picture_htf":{"min_rr":4}}}`,
	} {
		rows := effRowsFor(t, raw, "ninjatrader", "NY")
		parsed, err := (&store.Strategy{Config: raw}).ParseConfig()
		if err != nil {
			t.Fatal(err)
		}
		at := &AutoTrader{exchange: "ninjatrader", config: AutoTraderConfig{StrategyConfig: parsed}}
		rc := parsed.RiskControl

		limit, src, enforced := at.deskGuardrail()
		var wantDL any = limit
		if !enforced {
			wantDL = "off (" + src + ")"
		}
		_, mult, period, arm, armPts := trailingConfig(rc)
		pictureRR, _ := at.pictureMinRR(at.pictureHtfResolvedConfig().MinRR)
		checks := map[string]any{
			rcPath + "max_contracts_per_order":  at.resolveMaxContracts(),
			rcPath + "daily_loss_limit_usd":     wantDL,
			rcPath + "reentry_cooldown_minutes": at.reentryCooldownMinutes(),
			rcPath + "consecutive_loss_halt":    breakerHaltN(parsed),
			rcPath + "breakeven_trigger_points": breakevenTriggerPoints(rc),
			rcPath + "trailing_atr_mult":        mult,
			rcPath + "trailing_atr_period":      period,
			rcPath + "trailing_arm":             arm,
			rcPath + "trailing_arm_points":      armPts,
			dpPath + "proximity_filter_atr":     at.proximityFilterATR(),
			dpPath + "approval_required":        at.approvalRequired(),
			dpPath + "plan_enabled":             at.dayPlanEnabled(),
			dpPath + "fade_or_wide_k":           at.fadeORWideK(),
			dpPath + "replan_cap":               at.replanCapFor("NY"),
			dpPath + "picture_htf.min_rr":       pictureRR,
			spPath + "enable":                   at.sessionEnabledForStrategy("NY"),
			spPath + "replan_cap":               at.replanCapFor("NY"),
		}
		for path, want := range checks {
			row, ok := rows[path]
			if !ok {
				t.Fatalf("no row for %s", path)
			}
			if !sameJSON(t, row.Effective, want) {
				t.Fatalf("%s: row %v, production %v (config %s)", path, row.Effective, want, raw)
			}
		}

		// The breakeven trigger row IS the boundary the production decision uses.
		if hlBool(rc.BreakevenEnabled, false) {
			trig := breakevenTriggerPoints(rc)
			if fire, _ := breakevenTrigger(rc, "long", 100, 100+trig); !fire {
				t.Fatalf("breakevenTrigger must fire at the reported trigger %v", trig)
			}
			if fire, _ := breakevenTrigger(rc, "long", 100, 100+trig-0.25); fire {
				t.Fatalf("breakevenTrigger must not fire below the reported trigger %v", trig)
			}
		}
	}
}

// A saved value above the range is CLAMPED by the engine's in-place ClampLimits;
// the row reports the clamped value and says the saved one lost.
func TestEffectiveMinRRClampedAboveRange(t *testing.T) {
	rows := effRowsFor(t, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"min_risk_reward_ratio":15}}}`, "ninjatrader", "")
	row := rows[rcPath+"min_risk_reward_ratio"]
	clamped, _ := (&store.Strategy{Config: `{"ai_config":{"risk_control":{"min_risk_reward_ratio":15}}}`}).ParseConfig()
	clamped.ClampLimits()
	want, _ := store.ResolveMinRiskReward(clamped)
	if !sameJSON(t, row.Effective, want) || row.Origin != OriginClampLimits+" — saved 15 not used" {
		t.Fatalf("clamped min R:R: %+v (engine steady state %v)", row, want)
	}
}

// Presence mirrors StrategyConfig.UnmarshalJSON: the legacy flat shape is read
// when no ai_config is stored, ai_config wins when both are, and null is absent.
func TestEffectiveStoredPresenceMirrorsParse(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		present bool
		value   string
	}{
		{`{"risk_control":{"min_risk_reward_ratio":2}}`, true, "2"},
		{`{"ai_config":{"risk_control":{"min_risk_reward_ratio":2.5}},"risk_control":{"min_risk_reward_ratio":9}}`, true, "2.5"},
		{`{"ai_config":{"risk_control":{"min_risk_reward_ratio":null}}}`, false, ""},
		{`{"ai_config":null,"risk_control":{"min_risk_reward_ratio":4}}`, true, "4"},
		{`{}`, false, ""},
	} {
		row := effRowsFor(t, tc.raw, "ninjatrader", "")[rcPath+"min_risk_reward_ratio"]
		if row.Stored.Present != tc.present || (tc.present && string(row.Stored.Value) != tc.value) {
			t.Fatalf("%s: stored %+v (%s), want present=%v %s", tc.raw, row.Stored, row.Stored.Value, tc.present, tc.value)
		}
		parsed, _ := (&store.Strategy{Config: tc.raw}).ParseConfig()
		v, _ := store.ResolveMinRiskReward(parsed)
		if tc.present && tc.value != "0" && !sameJSON(t, row.Effective, v) {
			t.Fatalf("%s: effective %v, parse says %v", tc.raw, row.Effective, v)
		}
	}
}

// Secret-shaped paths never carry a value out, stored or effective — proved at
// the row builder, because at 853981d2 the schema walk does not yet list these
// ai_config.* paths as rows (W1 (f) adds them).
func TestEffectiveSecretRowsRedacted(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","ai_config":{"indicators":{
		"external_data_sources":[{"name":"feed","url":"https://h.example/?apikey=PLANTED-URL-77",
			"headers":{"Authorization":"Bearer PLANTED-HDR-42"}}]}}}`
	x, err := newEffCtx(raw, "ninjatrader", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"ai_config.indicators.external_data_sources.headers",
		"ai_config.indicators.external_data_sources.url",
	} {
		row := buildEffectiveRow(x, p)
		b, _ := json.Marshal(row)
		if strings.Contains(string(b), "PLANTED") {
			t.Fatalf("%s leaked: %s", p, b)
		}
		if !row.Stored.Present || string(row.Stored.Value) != `"redacted"` || row.Effective != EffectiveRedacted {
			t.Fatalf("%s: %s", p, b)
		}
	}
	// A non-secret sibling keeps its stored value.
	if row := buildEffectiveRow(x, "ai_config.indicators.external_data_sources.name"); string(row.Stored.Value) != `["feed"]` {
		t.Fatalf("name: %+v (%s)", row, row.Stored.Value)
	}
}

// A path with no registered resolver says so, is counted, and never borrows a
// default — absent reads unset, present reads saved.
func TestEffectiveUnresolvedSaysNA(t *testing.T) {
	rows := effRowsFor(t, `{"strategy_type":"grid_trading","grid_config":{"grid_count":12}}`, "binance", "")
	gc := rows["grid_config.grid_count"]
	if gc.Resolved || gc.Effective != EffectiveNoResolver || gc.Origin != store.SourceSaved || gc.Resolver != "none" {
		t.Fatalf("grid_count: %+v", gc)
	}
	up := rows["grid_config.upper_price"]
	if up.Resolved || up.Effective != EffectiveNoResolver || up.Origin != OriginUnset || up.Stored.Present {
		t.Fatalf("upper_price: %+v", up)
	}
	cov := EffectiveCoverageOf(effRowsSlice(rows))
	if cov.Resolved != len(effectiveResolvers) {
		t.Fatalf("every registered resolver is a row: resolved %d, table %d", cov.Resolved, len(effectiveResolvers))
	}
}

func effRowsSlice(m map[string]EffectiveKnob) []EffectiveKnob {
	out := make([]EffectiveKnob, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

// condition_status: the row's map is exactly what the arm seam resolves, and the
// origin names which layer decided which condition.
func TestEffectiveConditionStatusRow(t *testing.T) {
	t.Setenv("LIVE_CONDITIONS", "breakout_retest")
	t.Setenv("SHADOW_CONDITIONS", "")
	raw := `{"day_plan":{"condition_status":{"reclaim":"shadow"},
		"sessions":[{"session":"NY","condition_status":{"fvg_entry":"live"}}]}}`
	row := effRowsFor(t, raw, "ninjatrader", "NY")[dpPath+"condition_status"]
	m, ok := row.Effective.(map[string]string)
	if !ok {
		t.Fatalf("effective %T", row.Effective)
	}
	if m["fvg_entry"] != "live" || m["reclaim"] != "shadow" || m["breakout_retest"] != "live" || m["hold"] != "live" {
		t.Fatalf("resolved map %v", m)
	}
	want := "session override (fvg_entry) · strategy value (reclaim) · env LIVE_CONDITIONS (breakout_retest) · shipped default (rest)"
	if row.Origin != want || row.Scope != "session:NY" {
		t.Fatalf("origin %q scope %q", row.Origin, row.Scope)
	}
}

// MENTOR-SIZE-TRUTH (DS-105 R2 P2): in mentor mode resolveMaxContracts returns
// mentor_max_contracts, NOT max_contracts_per_order — the W1 rows must say so
// instead of reporting the per-order cap as effective.
func TestEffectiveMaxContractsMentorModeTruth(t *testing.T) {
	mentorRaw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{
		"mentor_mode":true,"max_contracts_per_order":2}}}`
	rows := effRowsFor(t, mentorRaw, "ninjatrader", "NY")

	mcp := rows[rcPath+"max_contracts_per_order"]
	if got := jsonVal(t, mcp.Effective); got != "not applied in mentor mode" {
		t.Fatalf("mentor mode: max_contracts_per_order effective = %v, want 'not applied in mentor mode'", got)
	}
	if !strings.Contains(mcp.Origin, "mentor_max_contracts") {
		t.Fatalf("mentor mode: max_contracts_per_order origin = %q, want it to name mentor_max_contracts", mcp.Origin)
	}

	mmc, ok := rows[rcPath+"mentor_max_contracts"]
	if !ok {
		t.Fatalf("no mentor_max_contracts row")
	}
	if got := jsonVal(t, mmc.Effective); got != float64(20) {
		t.Fatalf("mentor mode: mentor_max_contracts effective = %v, want 20", got)
	}

	// AI mode is unchanged: the per-order cap still maps to resolveMaxContracts
	// (whose production value here is the Stage-A 1-contract clamp, not the raw 2).
	aiRaw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"mentor_mode":false,"max_contracts_per_order":2}}}`
	aiRows := effRowsFor(t, aiRaw, "ninjatrader", "NY")
	aiParsed, _ := (&store.Strategy{Config: aiRaw}).ParseConfig()
	aiAT := &AutoTrader{exchange: "ninjatrader", config: AutoTraderConfig{StrategyConfig: aiParsed}}
	if got := jsonVal(t, aiRows[rcPath+"max_contracts_per_order"].Effective); got != float64(aiAT.resolveMaxContracts()) {
		t.Fatalf("ai mode: max_contracts_per_order effective = %v, want production resolveMaxContracts = %v", got, aiAT.resolveMaxContracts())
	}
	if got := jsonVal(t, aiRows[rcPath+"mentor_max_contracts"].Effective); got != "\u2014 (AI mode; not applied)" {
		t.Fatalf("ai mode: mentor_max_contracts effective = %v, want the not-applied marker", got)
	}
}

// FIX-AUTOBE-OFF-IN-MENTOR (owner ruling 2026-10-09): the breakeven_enabled /
// trailing_enabled effective rows must say "not applied while mentor mode is
// ON" in mentor mode, and resolve normally (with the 0B seam open) in AI mode.
// RED (named): making the exitMech mentor branch `if false && …` leaves the
// mentor rows reporting the raw strategy value instead of the marker.
func TestEffectiveSettingsExitMechNotAppliedInMentorMode(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0") // seam open: the AI-mode rows resolve to the strategy value

	mentorRaw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{
		"mentor_mode":true,"breakeven_enabled":true,"breakeven_trigger_points":40,
		"trailing_enabled":true,"trailing_arm":"immediate"}}}`
	rows := effRowsFor(t, mentorRaw, "ninjatrader", "NY")

	for _, path := range []string{"breakeven_enabled", "trailing_enabled"} {
		k := rcPath + path
		if got := jsonVal(t, rows[k].Effective); got != "not applied while mentor mode is ON (owner ruling 2026-10-09)" {
			t.Fatalf("mentor mode: %s effective = %v, want the not-applied marker", k, got)
		}
		if !strings.HasPrefix(rows[k].Origin, "mentor_mode on") {
			t.Fatalf("mentor mode: %s origin = %q, want it to start with 'mentor_mode on'", k, rows[k].Origin)
		}
	}

	aiRaw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{
		"mentor_mode":false,"breakeven_enabled":true,"breakeven_trigger_points":40,
		"trailing_enabled":true,"trailing_arm":"immediate"}}}`
	aiRows := effRowsFor(t, aiRaw, "ninjatrader", "NY")

	if got := jsonVal(t, aiRows[rcPath+"breakeven_enabled"].Effective); got != true {
		t.Fatalf("ai mode: breakeven_enabled effective = %v, want true", got)
	}
	if got := jsonVal(t, aiRows[rcPath+"trailing_enabled"].Effective); got != true {
		t.Fatalf("ai mode: trailing_enabled effective = %v, want true", got)
	}
	if got := jsonVal(t, aiRows[rcPath+"breakeven_trigger_points"].Effective); got != float64(40) {
		t.Fatalf("ai mode: breakeven_trigger_points effective = %v, want 40", got)
	}
}
