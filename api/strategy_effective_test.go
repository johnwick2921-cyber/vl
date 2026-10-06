// W-EXEC-TRUTH W1 (g) — GET /api/strategies/:id/effective at the PRODUCTION
// call site: the real router (NewServer → setupRoutes), the real JWT + ownership
// path, a real sqlite store. Expected values come from CALLING the production
// resolvers on the stored row as the trader loads it (Strategy.ParseConfig) —
// never from re-implemented rules.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"vl/auth"
	"vl/config"
	"vl/kernel"
	"vl/manager"
	"vl/store"
	"vl/trader"
)

const effUser = "u-effective"

type effResp struct {
	StrategyID string                   `json:"strategy_id"`
	Session    *string                  `json:"session"`
	Venue      *string                  `json:"venue"`
	Settings   []trader.EffectiveKnob   `json:"settings"`
	Coverage   trader.EffectiveCoverage `json:"coverage"`
}

func newEffectiveServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "eff.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Plan().Close(); _ = st.Close() })
	auth.SetJWTSecret("effective-test-secret")
	seedTokenOwner(t, st, effUser, "eff@test") // M3 H2: a token needs its account row
	tok, err := auth.GenerateJWT(effUser, "eff@test")
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	return NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0), st, tok
}

func effPutStrategy(t *testing.T, st *store.Store, id, user, raw string) {
	t.Helper()
	if err := st.Strategy().Create(&store.Strategy{ID: id, UserID: user, Name: id, Config: raw}); err != nil {
		t.Fatalf("create strategy %s: %v", id, err)
	}
}

func effDo(t *testing.T, s *Server, tok, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func effGet(t *testing.T, s *Server, tok, url string) effResp {
	t.Helper()
	rec := effDo(t, s, tok, http.MethodGet, url, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d body=%s", url, rec.Code, rec.Body.String())
	}
	var out effResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return out
}

func effRow(t *testing.T, r effResp, path string) trader.EffectiveKnob {
	t.Helper()
	for _, k := range r.Settings {
		if k.Path == path {
			return k
		}
	}
	t.Fatalf("no row for %s", path)
	return trader.EffectiveKnob{}
}

// runtimeConfig re-reads the stored row exactly as the trader loads it
// (manager/trader_manager.go: strategy.ParseConfig()).
func runtimeConfig(t *testing.T, st *store.Store, user, id string) *store.StrategyConfig {
	t.Helper()
	row, err := st.Strategy().Get(user, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	cfg, err := row.ParseConfig()
	if err != nil {
		t.Fatalf("parse %s: %v", id, err)
	}
	return cfg
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("effective %v (%T) is not a number", v, v)
	}
	return f
}

// FIX-PLANNER (2026-09-26): the new death_reread_retry_min row shows on the
// effective-settings API with the PRODUCTION resolver's value and an honest
// origin: shipped default when absent (nil = wake_min_interval_min, 30), saved
// when the strategy stored one. The row is LIVE (origin never "folded").
func TestFpDeathRereadRetryMinRowValueAndOrigin(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "drr-absent", effUser, `{"strategy_type":"ai_trading","day_plan":{}}`)
	effPutStrategy(t, st, "drr-saved", effUser, `{"strategy_type":"ai_trading","day_plan":{"death_reread_retry_min":5}}`)

	const path = "day_plan.death_reread_retry_min"
	cases := []struct {
		id     string
		origin string
		want   float64
	}{
		{"drr-absent", store.SourceShippedDefault, store.DefaultWakeMinIntervalMin},
		{"drr-saved", store.SourceSaved, 5},
	}
	for _, tc := range cases {
		row := effRow(t, effGet(t, s, tok, "/api/strategies/"+tc.id+"/effective"), path)
		cfg := runtimeConfig(t, st, effUser, tc.id)
		want := float64(cfg.DayPlan.DeathRereadRetryMinutes())
		if got := num(t, row.Effective); got != want || got != tc.want {
			t.Fatalf("%s: effective %v, production resolver says %v, want %v", tc.id, got, want, tc.want)
		}
		if row.Origin != tc.origin {
			t.Fatalf("%s: origin %q, want %q", tc.id, row.Origin, tc.origin)
		}
		if !row.Resolved {
			t.Fatalf("%s: the row must be resolved", tc.id)
		}
	}
}

// MISSING / ZERO / SAVED produce three different origins for one knob, and the
// value is the one the production resolver returns for the stored row.
func TestEffectiveMinRROriginsAbsentZeroSaved(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "rr-absent", effUser, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"max_positions":1}}}`)
	effPutStrategy(t, st, "rr-zero", effUser, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"min_risk_reward_ratio":0}}}`)
	effPutStrategy(t, st, "rr-saved", effUser, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"min_risk_reward_ratio":2.5}}}`)

	const path = "ai_config.risk_control.min_risk_reward_ratio"
	cases := []struct {
		id, origin  string
		present     bool
		storedValue string
	}{
		{"rr-absent", store.SourceSchemaDefault, false, ""},
		{"rr-zero", store.SourceSchemaDefault + " — saved 0 not used", true, "0"},
		{"rr-saved", store.SourceSaved, true, "2.5"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		row := effRow(t, effGet(t, s, tok, "/api/strategies/"+tc.id+"/effective"), path)
		want, _ := store.ResolveMinRiskReward(runtimeConfig(t, st, effUser, tc.id))
		if got := num(t, row.Effective); got != want {
			t.Fatalf("%s: effective %v, production resolver says %v", tc.id, got, want)
		}
		if row.Origin != tc.origin {
			t.Fatalf("%s: origin %q, want %q", tc.id, row.Origin, tc.origin)
		}
		if row.Stored.Present != tc.present || (tc.present && string(row.Stored.Value) != tc.storedValue) {
			t.Fatalf("%s: stored %+v (value %s), want present=%v value=%s", tc.id, row.Stored, row.Stored.Value, tc.present, tc.storedValue)
		}
		if !tc.present && row.Stored.Value != nil {
			t.Fatalf("%s: an absent value must be ABSENT, got %s", tc.id, row.Stored.Value)
		}
		if row.Scope != "strategy" || row.Resolver == "none" || !row.Resolved {
			t.Fatalf("%s: scope/resolver %q/%q resolved=%v", tc.id, row.Scope, row.Resolver, row.Resolved)
		}
		seen[row.Origin] = true
	}
	if len(seen) != 3 {
		t.Fatalf("absent / zero / saved must give three origins, got %v", seen)
	}
}

// ENV origins: each env var the runtime reads is named as the origin when it is
// what decided the value, with scope "process env".
func TestEffectiveEnvOrigins(t *testing.T) {
	t.Cleanup(config.Init) // runs AFTER t.Setenv restores the env (LIFO)
	t.Setenv("STAGE_A_CONTRACT_CAP", "3")
	t.Setenv("BREAKER_HALT_N", "5")
	t.Setenv("RISK_MAX_DAILY_LOSS_USD", "275")
	t.Setenv("EXIT_MECHS_SUSPENDED", "1")
	config.Init() // RISK_MAX_DAILY_LOSS_USD is read at boot

	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "env", effUser, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{
		"max_contracts_per_order":5,"breakeven_enabled":true}}}`)
	r := effGet(t, s, tok, "/api/strategies/env/effective")
	cfg := runtimeConfig(t, st, effUser, "env")

	mc := effRow(t, r, "ai_config.risk_control.max_contracts_per_order")
	if want := kernel.ResolveMaxContracts(cfg.RiskControl.MaxContractsPerOrder, 2); num(t, mc.Effective) != float64(want) || want != 3 {
		t.Fatalf("max_contracts_per_order: effective %v, production %d (want the env cap 3)", mc.Effective, want)
	}
	if mc.Origin != "env STAGE_A_CONTRACT_CAP — saved 5 not used" || mc.Scope != "process env" {
		t.Fatalf("max_contracts_per_order origin/scope %q/%q", mc.Origin, mc.Scope)
	}

	br := effRow(t, r, "ai_config.risk_control.consecutive_loss_halt")
	if num(t, br.Effective) != 5 || br.Origin != "env BREAKER_HALT_N" || br.Scope != "process env" || br.Stored.Present {
		t.Fatalf("consecutive_loss_halt: %+v", br)
	}

	dl := effRow(t, r, "ai_config.risk_control.daily_loss_limit_usd")
	if want := kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD; num(t, dl.Effective) != want || want != 275 {
		t.Fatalf("daily_loss_limit_usd: effective %v, runtime env fallback %v", dl.Effective, want)
	}
	if dl.Origin != "env RISK_MAX_DAILY_LOSS_USD" || dl.Scope != "process env" {
		t.Fatalf("daily_loss_limit_usd origin/scope %q/%q", dl.Origin, dl.Scope)
	}

	be := effRow(t, r, "ai_config.risk_control.breakeven_enabled")
	if be.Effective != false || be.Origin != "suspended (EXIT_MECHS_SUSPENDED) — saved true not used" || be.Scope != "process env" {
		t.Fatalf("breakeven_enabled while suspended: %+v", be)
	}

	// The seam opened: the saved toggle is the effective value again.
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	be = effRow(t, effGet(t, s, tok, "/api/strategies/env/effective"), "ai_config.risk_control.breakeven_enabled")
	if be.Effective != true || be.Origin != store.SourceSaved || be.Scope != "venue:ninjatrader" {
		t.Fatalf("breakeven_enabled with the seam open: %+v", be)
	}

	// Without the env cap the Stage-A default ceiling is what bound.
	t.Setenv("STAGE_A_CONTRACT_CAP", "")
	mc = effRow(t, effGet(t, s, tok, "/api/strategies/env/effective"), "ai_config.risk_control.max_contracts_per_order")
	if want := kernel.ResolveMaxContracts(5, 2); num(t, mc.Effective) != float64(want) || mc.Origin != "clamp (kernel.ClampStageAContracts) — saved 5 not used" {
		t.Fatalf("stage-A default clamp: %+v (production %d)", mc, want)
	}
}

// PRECEDENCE + SESSION SCOPE: ?session= resolves the per-session chain; a
// session override says so and scopes to that session; the name is
// canonicalised where it enters (class 28); no session → per-session rows n/a.
func TestEffectiveSessionScopedRows(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "sess", effUser, `{"strategy_type":"ai_trading","day_plan":{
		"plan_enabled":true,"plan_mode":"advisory","replan_cap":3,
		"sessions":[{"session":"NY","plan_mode":"strict","replan_cap":0,"max_trades":2}]}}`)
	dp := runtimeConfig(t, st, effUser, "sess").DayPlan

	ny := effGet(t, s, tok, "/api/strategies/sess/effective?session=ny")
	if ny.Session == nil || *ny.Session != "NY" {
		t.Fatalf("session must be canonicalised to NY, got %v", ny.Session)
	}
	// The per-session row: the NY override is the stored value and it won. The
	// strategy-level row: its stored "advisory" LOST in NY, and says so.
	for p, origin := range map[string]string{
		"day_plan.sessions.plan_mode": store.SourceSessionOverride,
		"day_plan.plan_mode":          store.SourceSessionOverride + ` — saved "advisory" not used`,
	} {
		row := effRow(t, ny, p)
		want, _ := store.ResolvePlanMode(dp, "NY")
		if row.Effective != want || row.Origin != origin || row.Scope != "session:NY" {
			t.Fatalf("%s @NY: %+v (production %q, want origin %q)", p, row, want, origin)
		}
	}
	rc := effRow(t, ny, "day_plan.sessions.replan_cap")
	if num(t, rc.Effective) != float64(dp.ReplanCapFor("NY")) || rc.Origin != store.SourceSessionOverride || rc.Scope != "session:NY" {
		t.Fatalf("replan_cap @NY: %+v (production %d)", rc, dp.ReplanCapFor("NY"))
	}
	mt := effRow(t, ny, "day_plan.sessions.max_trades")
	if n, ok := dp.MaxTradesFor("NY"); !ok || num(t, mt.Effective) != float64(n) || mt.Origin != store.SourceSessionOverride {
		t.Fatalf("max_trades @NY: %+v (production %d,%v)", mt, n, ok)
	}

	asia := effGet(t, s, tok, "/api/strategies/sess/effective?session=ASIA")
	pm := effRow(t, asia, "day_plan.sessions.plan_mode")
	if want, _ := store.ResolvePlanMode(dp, "ASIA"); pm.Effective != want || pm.Origin != store.SourceStrategyValue || pm.Scope != "strategy" || pm.Stored.Present {
		t.Fatalf("plan_mode @ASIA inherits the strategy value: %+v", pm)
	}
	rcA := effRow(t, asia, "day_plan.sessions.replan_cap")
	if num(t, rcA.Effective) != float64(dp.ReplanCapFor("ASIA")) || rcA.Origin != store.SourceStrategyValue {
		t.Fatalf("replan_cap @ASIA: %+v (production %d)", rcA, dp.ReplanCapFor("ASIA"))
	}

	none := effGet(t, s, tok, "/api/strategies/sess/effective")
	if none.Session != nil {
		t.Fatalf("an unnamed session is null, got %q", *none.Session)
	}
	if row := effRow(t, none, "day_plan.sessions.plan_mode"); row.Effective != trader.EffectiveNoSession {
		t.Fatalf("per-session row without a session must say n/a, got %v", row.Effective)
	}

	if rec := effDo(t, s, tok, http.MethodGet, "/api/strategies/sess/effective?session=N%20Y!", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed session must be 400, got %d", rec.Code)
	}
}

// OFF: a guardrail switched off reads off, with the switch that did it.
func TestEffectiveOffReadsOff(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "off", effUser, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{
		"daily_loss_limit_usd":450,"daily_loss_enabled":false}}}`)
	row := effRow(t, effGet(t, s, tok, "/api/strategies/off/effective"), "ai_config.risk_control.daily_loss_limit_usd")
	if row.Effective != "off (daily_loss_enabled OFF)" || row.Origin != "saved value (daily_loss_enabled OFF)" {
		t.Fatalf("daily loss switched off: %+v", row)
	}
	tog := effRow(t, effGet(t, s, tok, "/api/strategies/off/effective"), "ai_config.risk_control.daily_loss_enabled")
	gp := kernel.ResolveStrategyGuardrails(runtimeConfig(t, st, effUser, "off").RiskControl, 0)
	if tog.Effective != gp.DailyLossEnabled || tog.Origin != store.SourceSaved {
		t.Fatalf("daily_loss_enabled: %+v (production %v)", tog, gp.DailyLossEnabled)
	}
}

// SAVE-THEN-RELOAD: a config saved through the production create/update
// handlers reads back with effective values equal to the production resolvers
// on the reloaded row.
func TestEffectiveSaveThenReloadMatchesRuntime(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	rec := effDo(t, s, tok, http.MethodPost, "/api/strategies", `{"name":"W1g","config":{
		"strategy_type":"ai_trading",
		"ai_config":{"risk_control":{"min_risk_reward_ratio":2.5,"max_contracts_per_order":1}},
		"day_plan":{"plan_enabled":true,"plan_mode":"direction","sessions":[{"session":"NY","plan_mode":"strict"}]}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	check := func(label string, wantRR float64, wantMode string) {
		r := effGet(t, s, tok, "/api/strategies/"+created.ID+"/effective?session=NY")
		cfg := runtimeConfig(t, st, effUser, created.ID)
		rr, _ := store.ResolveMinRiskReward(cfg)
		if row := effRow(t, r, "ai_config.risk_control.min_risk_reward_ratio"); num(t, row.Effective) != rr || rr != wantRR || row.Origin != store.SourceSaved {
			t.Fatalf("%s: min_rr row %+v, runtime %v, want %v", label, row, rr, wantRR)
		}
		mode, _ := store.ResolvePlanMode(cfg.DayPlan, "NY")
		if row := effRow(t, r, "day_plan.plan_mode"); row.Effective != mode || mode != wantMode {
			t.Fatalf("%s: plan_mode row %+v, runtime %q, want %q", label, row, mode, wantMode)
		}
		if row := effRow(t, r, "ai_config.risk_control.max_contracts_per_order"); num(t, row.Effective) != float64(kernel.ResolveMaxContracts(cfg.RiskControl.MaxContractsPerOrder, 2)) {
			t.Fatalf("%s: max_contracts row %+v", label, row)
		}
	}
	check("after create", 2.5, "strict")

	rec = effDo(t, s, tok, http.MethodPut, "/api/strategies/"+created.ID, `{"config":{
		"ai_config":{"risk_control":{"min_risk_reward_ratio":4}},
		"day_plan":{"plan_enabled":true,"plan_mode":"direction","sessions":[]}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	check("after update", 4, "direction")
}

// DEFECT PIN (reported as a CLASS NN candidate, not fixed in (g)): the Studio
// save path runs ClampLimits BEFORE persisting (api/strategy.go create + update),
// so a min_risk_reward_ratio the owner never set is WRITTEN as 3 and reads back
// as a saved value. The effective VALUE is right; the ORIGIN the DB can carry is
// already gone at save. This test fails the day the save path stops doing that —
// update it then, deliberately.
func TestEffectiveClampAtSaveErasesOriginPin(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	rec := effDo(t, s, tok, http.MethodPost, "/api/strategies", `{"name":"unset-rr","config":{"strategy_type":"ai_trading","ai_config":{"risk_control":{}}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	row := effRow(t, effGet(t, s, tok, "/api/strategies/"+created.ID+"/effective"), "ai_config.risk_control.min_risk_reward_ratio")
	if !row.Stored.Present || string(row.Stored.Value) != "3" || row.Origin != store.SourceSaved {
		t.Fatalf("pin moved — the save path no longer writes the clamp default back: %+v (%s)", row, row.Stored.Value)
	}
	_ = st
}

// SECRETS never leave the server, stored or effective.
func TestEffectiveRedactsSecrets(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "sec", effUser, `{"strategy_type":"ai_trading","ai_config":{"indicators":{
		"external_data_sources":[{"name":"feed","type":"api","url":"https://h.example/?apikey=PLANTED-URL-77","method":"GET",
			"headers":{"Authorization":"Bearer PLANTED-HDR-42"}}]}}}`)
	rec := effDo(t, s, tok, http.MethodGet, "/api/strategies/sec/effective", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "PLANTED") {
		t.Fatalf("a planted secret reached the response body")
	}
	var r effResp
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	for _, k := range r.Settings {
		if strings.Contains(k.Path, "external_data_sources.headers") || strings.Contains(k.Path, "external_data_sources.url") {
			if k.Effective != trader.EffectiveRedacted || (k.Stored.Present && string(k.Stored.Value) != `"redacted"`) {
				t.Fatalf("secret row not redacted: %+v", k)
			}
		}
	}
}

// OWNERSHIP: another user's strategy is 404 (never 403); no JWT is 401.
func TestEffectiveOwnership(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "mine", effUser, `{}`)
	effPutStrategy(t, st, "theirs", "someone-else", `{}`)
	if rec := effDo(t, s, tok, http.MethodGet, "/api/strategies/theirs/effective", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("another user's strategy must be 404, got %d", rec.Code)
	}
	if rec := effDo(t, s, "", http.MethodGet, "/api/strategies/mine/effective", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no JWT must be 401, got %d", rec.Code)
	}
	if rec := effDo(t, s, tok, http.MethodGet, "/api/strategies/mine/effective", ""); rec.Code != http.StatusOK {
		t.Fatalf("owner read must be 200, got %d", rec.Code)
	}
}

// COVERAGE is counted from the rows and pinned, so a silently shrinking
// resolver table fails here. total grows when W1 (f) widens the schema walk;
// resolved does not move unless a resolver is added or removed.
func TestEffectiveCoverageCounted(t *testing.T) {
	const wantResolved = effectiveResolvedPin
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "cov", effUser, `{}`)
	r := effGet(t, s, tok, "/api/strategies/cov/effective?session=NY")
	c := r.Coverage
	if c.Total != len(r.Settings) {
		t.Fatalf("total %d != rows %d", c.Total, len(r.Settings))
	}
	resolved := 0
	for _, k := range r.Settings {
		if k.Resolved {
			resolved++
		} else if k.Effective != trader.EffectiveNoResolver && k.Effective != trader.EffectiveRedacted {
			t.Fatalf("unresolved row %s must say %q, got %v", k.Path, trader.EffectiveNoResolver, k.Effective)
		}
	}
	if c.Resolved != resolved || c.Resolved != wantResolved {
		t.Fatalf("resolved: coverage %d, rows %d, pin %d", c.Resolved, resolved, wantResolved)
	}
	if len(c.Unresolved) != c.Total-c.Resolved {
		t.Fatalf("unresolved list %d != total-resolved %d", len(c.Unresolved), c.Total-c.Resolved)
	}
	if schema := len(store.EnumerateSchemaKnobs()); c.Total < schema {
		t.Fatalf("every enumerated schema path must be a row: total %d < schema %d", c.Total, schema)
	}
	if c.NotEnumerated == nil || c.Unresolved == nil {
		t.Fatalf("computed lists are [] never null: %+v", c)
	}
	// W1 (f) walks the real MarshalJSON paths, so every registered resolver
	// path is a schema path: a registration the walk cannot reach is a typo
	// or a path the save never writes.
	if len(c.NotEnumerated) != 0 {
		t.Fatalf("registered resolver paths the schema walk does not produce: %v", c.NotEnumerated)
	}
}

// effectiveResolvedPin is the number of registered resolvers at this revision
// (trader/effective_settings.go). Change it ONLY with the table.
const effectiveResolvedPin = 88 // MENTOR-SIZE-TRUTH 2026-10-06: +1 (mentor_max_contracts); FIX-PLANNER 2026-09-26: +1 (death_reread_retry_min); PLANNER A6 2026-09-25: +1 (planner_fresh_tape); PLANNER B1 2026-09-25: +1 (zone_place_within_pts); W-EXEC-TRUTH W3: +4 (entry_policy_default, zone_max_pts, zone_rest_max_min, min_hold_min)
