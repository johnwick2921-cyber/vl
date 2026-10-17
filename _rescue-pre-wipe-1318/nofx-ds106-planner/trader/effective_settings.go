// W-EXEC-TRUTH W1 (g) — every settings row shows its EFFECTIVE value, its
// ORIGIN and its SCOPE.
//
// THE INPUT IS THE STORED ROW, never a copy something else has already touched:
//
//   - NOT the running engine's config pointer: kernel/engine_analysis.go calls
//     ClampLimits on it in place every cycle, so after one cycle an unset
//     min_risk_reward_ratio reads 3.0 there and ResolveMinRiskReward answers
//     "saved value" about a value nobody saved.
//   - NOT GET /api/strategies/:id's config: attachPublishConfig runs
//     ClampLimits before serving it, which erases exactly the same origins.
//
// So presence comes from the raw stored JSON, typed values from
// store.(*Strategy).ParseConfig (the trader's own load path, manager/
// trader_manager.go), and each effective value from the PRODUCTION function that
// decides it (canon 53). Where the engine applies ClampLimits in place, the
// steady-state value is read from a SEPARATE parsed copy that has been clamped
// — the original stays unclamped so the origin survives.
//
// A path with no registered resolver says so ("n/a — no resolver registered")
// and is COUNTED (EffectiveCoverageOf); it never borrows a default (L7).

package trader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nofx/kernel"
	"nofx/store"
)

// Effective-value words that are not values.
const (
	EffectiveNoResolver = "n/a — no resolver registered"
	EffectiveRedacted   = "redacted"
	EffectiveNoSession  = "n/a — per-session setting; name a session (?session=NY|ASIA|LONDON)"
	EffectiveNoVenue    = "n/a — venue unknown (the day plan runs on ninjatrader only)"
)

// Origin vocabulary: store's source words (store/resolve_source.go) plus these.
const (
	OriginSuspended   = "suspended (EXIT_MECHS_SUSPENDED)"
	OriginBackfilled  = "backfilled default (applyMissingDefaults)"
	OriginFolded      = "code constant (folded)"
	OriginClampLimits = "clamp (StrategyConfig.ClampLimits)"
	OriginUnset       = "unset"
	OriginNA          = "n/a"
)

// Scope vocabulary: where the winning value lives (where you would change it).
const (
	ScopeStrategy   = "strategy"
	ScopeProcessEnv = "process env"
	ScopeFutures    = "venue:ninjatrader"
)

func scopeSession(session string) string {
	if session == "" {
		return "session:n/a"
	}
	return "session:" + session
}

func originEnv(name string) string { return "env " + name }

// EffectiveStored is what the stored row carries at the path. Value is ABSENT
// when Present is false — absent and an empty/zero value are different answers.
type EffectiveStored struct {
	Present bool            `json:"present"`
	Value   json.RawMessage `json:"value,omitempty"`
}

// EffectiveKnob is one settings row.
type EffectiveKnob struct {
	Path      string          `json:"path"`
	Status    string          `json:"status"`
	UILabel   string          `json:"ui_label"`
	Stored    EffectiveStored `json:"stored"`
	Effective any             `json:"effective"`
	Origin    string          `json:"origin"`
	Scope     string          `json:"scope"`
	Resolver  string          `json:"resolver"`
	Resolved  bool            `json:"resolved"`
}

// EffectiveCoverage is counted from the rows, never typed.
type EffectiveCoverage struct {
	Resolved   int      `json:"resolved"`
	Total      int      `json:"total"`
	Unresolved []string `json:"unresolved"`
	// NotEnumerated lists registered resolver paths the schema walk
	// (store.EnumerateSchemaKnobs) did not produce — at 853981d2 every
	// ai_config.* path, because the walk skips json:"-" fields (W1 (f) fixes
	// the walk). An empty list is [] (computed), never null.
	NotEnumerated []string `json:"not_enumerated"`
}

// effCtx is everything a resolver may read. cfg is ParseConfig'd and never
// clamped; clamped is a SEPARATE parse with ClampLimits applied (the engine's
// steady state). at is a minimal AutoTrader over cfg so resolvers that are
// AutoTrader methods run at their production call site.
type effCtx struct {
	cfg     *store.StrategyConfig
	clamped *store.StrategyConfig
	at      *AutoTrader
	session string
	venue   string
	root    json.RawMessage
	st      effStoredRaw // the current row's stored value (set per row)
	path    string
	// zeros — the strategy's save-time confirmation record (system_config
	// settings_truth_zero:<id>): whether a Studio save confirmed an explicit 0.
	zeros store.ExplicitZeroRecord
}

type effStoredRaw struct {
	present bool
	value   json.RawMessage
}

type effResult struct {
	value  any
	origin string
	scope  string
	// compound: the origin already names, per entry, which layer decided it
	// (a map-valued row) — no single "saved X not used" verdict applies.
	compound bool
}

type effResolver struct {
	name string
	fn   func(x *effCtx) effResult
}

func (x *effCtx) dp() *store.DayPlanConfig { return x.cfg.DayPlan }
func (x *effCtx) rc() store.RiskControlConfig {
	return x.cfg.RiskControl
}

// EffectiveSettings resolves every settings row for one stored strategy config.
// raw is the strategies.config column verbatim; venue is the exchange type the
// strategy trades on ("" = unknown); session is a canonical session name or "";
// zeros is the strategy's confirmation record (StrategyStore.ExplicitZeroRecordOf)
// — an explicit 0 on the breaker or the strategy replan cap says in its origin
// whether a Studio save confirmed it, and when.
func EffectiveSettings(raw, venue, session string, zeros store.ExplicitZeroRecord) ([]EffectiveKnob, error) {
	x, err := newEffCtx(raw, venue, session)
	if err != nil {
		return nil, err
	}
	x.zeros = zeros
	paths := effectivePaths()
	out := make([]EffectiveKnob, 0, len(paths))
	for _, p := range paths {
		out = append(out, buildEffectiveRow(x, p))
	}
	return out, nil
}

// newEffCtx parses the stored row TWICE: once for the typed values the
// resolvers read (never clamped), once for the engine's clamped steady state.
func newEffCtx(raw, venue, session string) (*effCtx, error) {
	parsed, err := (&store.Strategy{Config: raw}).ParseConfig()
	if err != nil {
		return nil, err
	}
	clamped, err := (&store.Strategy{Config: raw}).ParseConfig()
	if err != nil {
		return nil, err
	}
	clamped.ClampLimits()
	return &effCtx{
		cfg:     parsed,
		clamped: clamped,
		at:      &AutoTrader{exchange: venue, config: AutoTraderConfig{StrategyConfig: parsed}},
		session: session,
		venue:   venue,
		root:    json.RawMessage(raw),
	}, nil
}

// effectivePaths is the enumerated schema UNION every registered resolver path,
// sorted. The union keeps a resolved value from being hidden by an incomplete
// schema walk; EffectiveCoverageOf names every path that only the registry knew.
func effectivePaths() []string {
	set := map[string]bool{}
	for _, p := range store.EnumerateSchemaKnobs() {
		set[p] = true
	}
	for p := range effectiveResolvers {
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// EffectiveCoverageOf counts the rows. Unresolved and NotEnumerated are [] when
// empty (computed), never null.
func EffectiveCoverageOf(rows []EffectiveKnob) EffectiveCoverage {
	cov := EffectiveCoverage{Total: len(rows), Unresolved: []string{}, NotEnumerated: []string{}}
	for _, r := range rows {
		if r.Resolved {
			cov.Resolved++
		} else {
			cov.Unresolved = append(cov.Unresolved, r.Path)
		}
	}
	enum := map[string]bool{}
	for _, p := range store.EnumerateSchemaKnobs() {
		enum[p] = true
	}
	for p := range effectiveResolvers {
		if !enum[p] {
			cov.NotEnumerated = append(cov.NotEnumerated, p)
		}
	}
	sort.Strings(cov.NotEnumerated)
	return cov
}

func buildEffectiveRow(x *effCtx, path string) EffectiveKnob {
	k := EffectiveKnob{Path: path, Scope: ScopeStrategy, Resolver: "none"}
	if e, ok := store.LookupKnob(path); ok {
		k.Status, k.UILabel = string(e.Status), e.UILabel()
	} else {
		k.Status, k.UILabel = "unclassified", "unclassified — no registry entry"
	}

	st := lookupStored(x.root, path, x.session)
	secret := isSecretPath(path)
	k.Stored.Present = st.present
	if st.present {
		if secret {
			k.Stored.Value = json.RawMessage(`"` + EffectiveRedacted + `"`)
		} else {
			k.Stored.Value = st.value
		}
	}

	if secret {
		k.Effective = EffectiveRedacted
		k.Origin = presenceOnlyOrigin(st.present)
		return k
	}
	r, ok := effectiveResolvers[path]
	if !ok {
		k.Effective = EffectiveNoResolver
		k.Origin = presenceOnlyOrigin(st.present)
		return k
	}

	x.st, x.path = st, path
	res := r.fn(x)
	k.Effective = res.value
	k.Origin = res.origin
	if res.scope != "" {
		k.Scope = res.scope
	}
	k.Resolver = r.name
	k.Resolved = true

	// A saved value that lost is said to have lost — the defect class this row
	// exists to expose is a saved value that silently does not apply.
	if st.present && !res.compound && savedValueLost(path, res.origin) {
		k.Origin += " — saved " + compactJSON(st.value) + " not used"
	}
	return k
}

func presenceOnlyOrigin(present bool) string {
	if present {
		return store.SourceSaved
	}
	return OriginUnset
}

// savedValueLost: a stored value exists and something else decided the
// effective value — a default, an env var, a clamp, a suspension, a session
// override (on a strategy-level row) or the strategy value (on a per-session
// row). The saved kinds are "saved value", "strategy value" on a strategy row
// and "session override" on a per-session row.
func savedValueLost(path, origin string) bool {
	if strings.HasPrefix(origin, store.SourceSaved) || origin == OriginNA || origin == OriginUnset {
		return false
	}
	if isSessionPath(path) {
		return !strings.HasPrefix(origin, store.SourceSessionOverride)
	}
	return !strings.HasPrefix(origin, store.SourceStrategyValue)
}

func isSessionPath(path string) bool { return strings.HasPrefix(path, "day_plan.sessions.") }

func compactJSON(v json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, v); err != nil {
		return string(v)
	}
	return b.String()
}

// ── REDACTION ────────────────────────────────────────────────────────────────
//
// An explicit set: the NofxOS key, external data-source headers and URLs (a URL
// commonly carries a key in its query string), and any leaf whose name reads as
// a credential. Redacted rows never carry the stored value or an effective one.

var secretLeafRe = regexp.MustCompile(`(?i)(api_?key|secret|token|password|passphrase|private_?key|credential)`)

func isSecretPath(path string) bool {
	if strings.Contains(path, "external_data_sources.headers") || strings.Contains(path, "external_data_sources.url") {
		return true
	}
	leaf := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		leaf = path[i+1:]
	}
	return leaf == "nofxos_api_key" || secretLeafRe.MatchString(leaf)
}

// ── STORED-VALUE PRESENCE ────────────────────────────────────────────────────

// lookupStored walks the raw stored JSON along a schema path. It mirrors
// StrategyConfig.UnmarshalJSON: an ai_config.* path reads the ai_config object
// when one is stored, else the legacy flat top level. A JSON null reads as
// absent (the decoder gives the Go field its zero value / nil). Arrays of
// objects: day_plan.sessions selects the named session (case-insensitively, as
// DayPlanConfig.SessionOverride does); with no session, and for every other
// array, the per-element values are collected.
func lookupStored(root json.RawMessage, path, session string) effStoredRaw {
	segs := strings.Split(path, ".")
	cur := root
	if segs[0] == "ai_config" {
		top := decodeObject(root)
		if top == nil {
			return effStoredRaw{}
		}
		if ai, ok := top["ai_config"]; ok && !isJSONNull(ai) {
			cur = ai
		}
		segs = segs[1:]
	}
	return walkStored(cur, segs, session, strings.HasPrefix(path, "day_plan.sessions."))
}

func walkStored(cur json.RawMessage, segs []string, session string, sessionsPath bool) effStoredRaw {
	if len(segs) == 0 {
		if isJSONNull(cur) {
			return effStoredRaw{}
		}
		return effStoredRaw{present: true, value: cur}
	}
	obj := decodeObject(cur)
	if obj == nil {
		return effStoredRaw{}
	}
	child, ok := obj[segs[0]]
	if !ok || isJSONNull(child) {
		return effStoredRaw{}
	}
	rest := segs[1:]
	if len(rest) > 0 {
		if arr := decodeArray(child); arr != nil {
			if sessionsPath && segs[0] == "sessions" {
				return walkSessions(arr, rest, session)
			}
			var vals []json.RawMessage
			for _, el := range arr {
				if r := walkStored(el, rest, session, false); r.present {
					vals = append(vals, r.value)
				}
			}
			if len(vals) == 0 {
				return effStoredRaw{}
			}
			b, _ := json.Marshal(vals)
			return effStoredRaw{present: true, value: b}
		}
	}
	return walkStored(child, rest, session, sessionsPath)
}

func walkSessions(arr []json.RawMessage, rest []string, session string) effStoredRaw {
	collected := map[string]json.RawMessage{}
	for _, el := range arr {
		var head struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(el, &head)
		if session != "" {
			if strings.EqualFold(head.Session, session) {
				return walkStored(el, rest, session, false)
			}
			continue
		}
		if r := walkStored(el, rest, session, false); r.present {
			collected[head.Session] = r.value
		}
	}
	if session != "" || len(collected) == 0 {
		return effStoredRaw{}
	}
	b, _ := json.Marshal(collected)
	return effStoredRaw{present: true, value: b}
}

func decodeObject(v json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(v, &m); err != nil {
		return nil
	}
	return m
}

func decodeArray(v json.RawMessage) []json.RawMessage {
	t := bytes.TrimSpace(v)
	if len(t) == 0 || t[0] != '[' {
		return nil
	}
	var a []json.RawMessage
	if err := json.Unmarshal(t, &a); err != nil {
		return nil
	}
	return a
}

func isJSONNull(v json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

// storedEquals reports whether the stored JSON denotes the effective value.
// Strings compare trimmed and case-insensitively (a canonicaliser upper-casing
// "b" to "B" applied the saved value; it did not replace it).
func storedEquals(stored json.RawMessage, eff any) bool {
	var a any
	if err := json.Unmarshal(stored, &a); err != nil {
		return false
	}
	eb, err := json.Marshal(eff)
	if err != nil {
		return false
	}
	var b any
	if err := json.Unmarshal(eb, &b); err != nil {
		return false
	}
	return reflect.DeepEqual(foldJSON(a), foldJSON(b))
}

func foldJSON(v any) any {
	switch t := v.(type) {
	case string:
		return strings.ToUpper(strings.TrimSpace(t))
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = foldJSON(t[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = foldJSON(val)
		}
		return out
	}
	return v
}

// ── ORIGIN HELPERS ───────────────────────────────────────────────────────────

// presenceOrigin is for resolvers of the shape "a usable saved value, else the
// default": saved when the stored value is what took effect, else defOrigin
// (buildEffectiveRow then appends "saved X not used" when one was stored).
func presenceOrigin(x *effCtx, eff any, defOrigin string) string {
	if x.st.present && storedEquals(x.st.value, eff) {
		return store.SourceSaved
	}
	return defOrigin
}

// clampRow is for values the engine reads after ClampLimits: the clamped
// copy's value, "saved value" when that is exactly what was stored, the
// applyMissingDefaults backfill when an ABSENT field was filled at parse, else
// the clamp.
func clampRow(x *effCtx, get func(*store.StrategyConfig) any, parsedZero func(*store.StrategyConfig) bool) effResult {
	eff := get(x.clamped)
	switch {
	case x.st.present && storedEquals(x.st.value, eff):
		return effResult{value: eff, origin: store.SourceSaved}
	case !x.st.present && parsedZero != nil && !parsedZero(x.cfg) && reflect.DeepEqual(get(x.cfg), eff):
		return effResult{value: eff, origin: OriginBackfilled}
	default:
		return effResult{value: eff, origin: OriginClampLimits}
	}
}

func scopeForSource(src, session string, fallback string) string {
	switch {
	case strings.HasPrefix(src, store.SourceSessionOverride):
		return scopeSession(session)
	case strings.HasPrefix(src, "env "):
		return ScopeProcessEnv
	}
	return fallback
}

// envSet mirrors the validity test of the reader it labels, so a set-but-invalid
// variable is NOT reported as the origin (the reader ignored it).
func envIntSet(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 0
}

func envFloatSet(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	_, err := strconv.ParseFloat(v, 64)
	return err == nil
}

// ── BREAKER + REPLAN CAP: the production resolvers themselves ────────────────
//
// W1 (a)(b): store.ResolveBreakerHalt and store.ResolveReplanCap are the ONE
// resolver each (trader.breakerHaltN and DayPlanConfig.ReplanCapFor delegate to
// them), so these rows call them directly — value and source together.

func effBreakerHalt(cfg *store.StrategyConfig) (int, string) {
	return store.ResolveBreakerHalt(cfg)
}

func effReplanCap(dp *store.DayPlanConfig, session string) (int, string) {
	return store.ResolveReplanCap(dp, session)
}

// explicitZeroOrigin appends, to the origin of a value decided by an EXPLICIT
// 0 at the strategy level, whether a Studio save confirmed that 0 and when
// (store.ExplicitZeroVerdict — the same words the 🩺 boot report prints; CTO
// ruling msg 1790176346377 R2). decidedByStrategy says the winning source is
// the strategy-level stored value (a session override's 0 always meant 0).
// The prefix stays the resolver's source, so OriginLetter and savedValueLost
// read it unchanged.
func explicitZeroOrigin(x *effCtx, knob, src string, decidedByStrategy bool) string {
	if !decidedByStrategy {
		return src
	}
	for _, k := range store.ExplicitZeroKnobs(x.cfg) {
		if k == knob {
			return src + " — " + store.ExplicitZeroVerdict(knob, x.zeros)
		}
	}
	return src
}

// ── RESOLVER TABLE ───────────────────────────────────────────────────────────

const (
	rcPath = "ai_config.risk_control."
	dpPath = "day_plan."
	spPath = "day_plan.sessions."
)

// effectiveResolvers maps a schema path (the REAL marshal path) to the
// production function that decides its value. Every entry names that function;
// a path absent here reads "n/a — no resolver registered".
var effectiveResolvers = buildEffectiveResolvers()

func buildEffectiveResolvers() map[string]effResolver {
	m := map[string]effResolver{}
	add := func(path, name string, fn func(x *effCtx) effResult) { m[path] = effResolver{name: name, fn: fn} }

	// ── Risk control ─────────────────────────────────────────────────────────
	add(rcPath+"min_risk_reward_ratio", "store.ResolveMinRiskReward (engine: after StrategyConfig.ClampLimits in place)", func(x *effCtx) effResult {
		v, src := store.ResolveMinRiskReward(x.cfg)
		if vc, _ := store.ResolveMinRiskReward(x.clamped); vc != v {
			return effResult{value: vc, origin: OriginClampLimits}
		}
		return effResult{value: v, origin: src}
	})
	add(rcPath+"min_confidence", "store.(*StrategyConfig).ClampLimits (kernel/engine_analysis.go reads the clamped config)", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.RiskControl.MinConfidence }, nil)
	})
	add(rcPath+"max_positions", "kernel.ResolveConcurrentCap over StrategyConfig.ClampLimits (steady state)", func(x *effCtx) effResult {
		n, _ := kernel.ResolveConcurrentCap(x.clamped.RiskControl.MaxPositions, kernel.LoadRiskLimitsFromConfig().MaxConcurrentTrades)
		r := clampRow(x, func(c *store.StrategyConfig) any { return c.RiskControl.MaxPositions }, nil)
		r.value = n
		return r
	})

	posture := func(x *effCtx) kernel.StrategyGuardrailPosture {
		return kernel.ResolveStrategyGuardrails(x.rc(), kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	}
	postureBool := func(path string, get func(kernel.StrategyGuardrailPosture) bool) {
		add(rcPath+path, "kernel.ResolveStrategyGuardrails", func(x *effCtx) effResult {
			v := get(posture(x))
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
		})
	}
	postureBool("guardrails_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.MasterEnabled })
	postureBool("daily_loss_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.DailyLossEnabled })
	postureBool("daily_profit_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.DailyProfitEnabled })
	postureBool("max_daily_trades_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.MaxDailyTradesEnabled })
	postureBool("blackout_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.BlackoutEnabled })
	postureBool("consistency_enabled", func(p kernel.StrategyGuardrailPosture) bool { return p.ConsistencyEnabled })
	add(rcPath+"daily_profit_target_usd", "kernel.ResolveStrategyGuardrails", func(x *effCtx) effResult {
		v := posture(x).DailyProfitTargetUSD
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(rcPath+"max_daily_trades", "kernel.ResolveStrategyGuardrails", func(x *effCtx) effResult {
		v := posture(x).MaxDailyTrades
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(rcPath+"daily_loss_limit_usd", "trader.(*AutoTrader).deskGuardrail", func(x *effCtx) effResult {
		limit, src, enforced := x.at.deskGuardrail()
		if !enforced {
			return effResult{value: "off (" + src + ")", origin: store.SourceSaved + " (" + src + ")"}
		}
		if src == "studio" {
			return effResult{value: limit, origin: store.SourceSaved}
		}
		if envFloatSet("RISK_MAX_DAILY_LOSS_USD") {
			return effResult{value: limit, origin: originEnv("RISK_MAX_DAILY_LOSS_USD"), scope: ScopeProcessEnv}
		}
		return effResult{value: limit, origin: store.SourceShippedDefault + " (RISK_MAX_DAILY_LOSS_USD unset)", scope: ScopeProcessEnv}
	})
	directRead := func(path, name string, get func(store.RiskControlConfig) any) {
		add(rcPath+path, name, func(x *effCtx) effResult {
			v := get(x.rc())
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
		})
	}
	directRead("consistency_max_day_pct", "kernel.ConsistencyBreached(rc.ConsistencyMaxDayPct) — direct read, kernel/engine_analysis.go", func(rc store.RiskControlConfig) any { return rc.ConsistencyMaxDayPct })
	directRead("blackout_start_ct", "kernel.InBlackoutWindow(rc.BlackoutStartCT, …) — direct read, kernel/engine_analysis.go", func(rc store.RiskControlConfig) any { return rc.BlackoutStartCT })
	directRead("blackout_end_ct", "kernel.InBlackoutWindow(…, rc.BlackoutEndCT) — direct read, kernel/engine_analysis.go", func(rc store.RiskControlConfig) any { return rc.BlackoutEndCT })
	add(rcPath+"reentry_cooldown_minutes", "trader.(*AutoTrader).reentryCooldownMinutes", func(x *effCtx) effResult {
		v := x.at.reentryCooldownMinutes()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(rcPath+"consecutive_loss_halt", "store.ResolveBreakerHalt (trader.breakerHaltN delegates)", func(x *effCtx) effResult {
		n, src := effBreakerHalt(x.cfg)
		origin := explicitZeroOrigin(x, store.KnobBreaker, src, src == store.SourceSaved)
		return effResult{value: n, origin: origin, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	add(rcPath+"max_contracts_per_order", "trader.(*AutoTrader).resolveMaxContracts → kernel.ResolveMaxContracts", func(x *effCtx) effResult {
		n, src := kernel.ResolveMaxContractsWithSource(x.rc().MaxContractsPerOrder, int(maxFuturesContracts))
		scope := ScopeFutures
		if strings.HasPrefix(src, "env ") || strings.HasPrefix(src, "clamp") {
			scope = ScopeProcessEnv
		}
		return effResult{value: n, origin: src, scope: scope}
	})
	add(rcPath+"max_notional_leverage", "kernel.ResolveNotionalLeverage (trader.enforcePositionValueRatio)", func(x *effCtx) effResult {
		v, src := kernel.ResolveNotionalLeverageWithSource(x.rc().MaxNotionalLeverage, futuresMaxNotionalLeverage)
		return effResult{value: v, origin: src, scope: ScopeFutures}
	})
	add(rcPath+"hold_discipline", "trader.hlBool(hold_discipline, false) — holdLockSuppressesClose", func(x *effCtx) effResult {
		v := hlBool(x.rc().HoldDisciplineEnabled, false)
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	exitMech := func(path, name string, on func(store.RiskControlConfig) bool) {
		add(rcPath+path, name, func(x *effCtx) effResult {
			v := on(x.rc())
			if v && exitMechsSuspended() {
				return effResult{value: false, origin: OriginSuspended, scope: ScopeProcessEnv}
			}
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeFutures}
		})
	}
	exitMech("breakeven_enabled", "trader.breakevenTrigger gate + exitMechsSuspended (exitMechSuspendedRefuse)", func(rc store.RiskControlConfig) bool {
		return hlBool(rc.BreakevenEnabled, false)
	})
	exitMech("trailing_enabled", "trader.trailingConfig + exitMechsSuspended (exitMechSuspendedRefuse)", func(rc store.RiskControlConfig) bool {
		on, _, _, _, _ := trailingConfig(rc)
		return on
	})
	add(rcPath+"breakeven_trigger_points", "trader.breakevenTriggerPoints", func(x *effCtx) effResult {
		v := breakevenTriggerPoints(x.rc())
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeFutures}
	})
	trailing := func(path string, pick func(mult float64, period int, arm string, armPts float64) any) {
		add(rcPath+path, "trader.trailingConfig", func(x *effCtx) effResult {
			_, mult, period, arm, armPts := trailingConfig(x.rc())
			v := pick(mult, period, arm, armPts)
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeFutures}
		})
	}
	trailing("trailing_atr_mult", func(m float64, _ int, _ string, _ float64) any { return m })
	trailing("trailing_atr_period", func(_ float64, p int, _ string, _ float64) any { return p })
	trailing("trailing_arm", func(_ float64, _ int, a string, _ float64) any { return a })
	trailing("trailing_arm_points", func(_ float64, _ int, _ string, ap float64) any { return ap })

	// ── Indicators / coin source: the parse-time backfill and the clamp ───────
	add("ai_config.indicators.klines.primary_timeframe", "store.(*Strategy).ParseConfig → applyMissingDefaults", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.Indicators.Klines.PrimaryTimeframe },
			func(c *store.StrategyConfig) bool { return c.Indicators.Klines.PrimaryTimeframe == "" })
	})
	add("ai_config.indicators.klines.selected_timeframes", "store.(*Strategy).ParseConfig → applyMissingDefaults, then StrategyConfig.ClampLimits", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.Indicators.Klines.SelectedTimeframes },
			func(c *store.StrategyConfig) bool { return len(c.Indicators.Klines.SelectedTimeframes) == 0 })
	})
	add("ai_config.indicators.klines.primary_count", "store.(*Strategy).ParseConfig → applyMissingDefaults, then StrategyConfig.ClampLimits", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.Indicators.Klines.PrimaryCount },
			func(c *store.StrategyConfig) bool { return c.Indicators.Klines.PrimaryCount == 0 })
	})
	add("ai_config.coin_source.source_type", "store.(*Strategy).ParseConfig → applyMissingDefaults, then StrategyConfig.ClampLimits (NormalizeProductSchema)", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.CoinSource.SourceType },
			func(c *store.StrategyConfig) bool { return c.CoinSource.SourceType == "" })
	})

	// ── Regime ───────────────────────────────────────────────────────────────
	add("regime.htf_veto", "store.ResolveHTFVeto", func(x *effCtx) effResult {
		v, src := store.ResolveHTFVeto(x.cfg)
		return effResult{value: v, origin: src}
	})
	add("regime.transition_standdown", "store.(*StrategyConfig).TransitionStanddownEnabled", func(x *effCtx) effResult {
		v := x.cfg.TransitionStanddownEnabled()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})

	// ── Day plan: strategy-level rows ────────────────────────────────────────
	add(dpPath+"plan_enabled", "trader.(*AutoTrader).dayPlanEnabled", func(x *effCtx) effResult {
		if x.venue == "" {
			return effResult{value: EffectiveNoVenue, origin: OriginNA}
		}
		v := x.at.dayPlanEnabled()
		if x.venue != "ninjatrader" {
			return effResult{value: v, origin: "venue " + x.venue + " (day plan is ninjatrader-only)", scope: "venue:" + x.venue}
		}
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(dpPath+"plan_mode", "store.ResolvePlanMode", func(x *effCtx) effResult {
		v, src := store.ResolvePlanMode(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	add(dpPath+"min_scenario_quality", "store.MinScenarioQualityForWithSource (= DayPlanConfig.MinScenarioQualityFor)", func(x *effCtx) effResult {
		v, src := store.MinScenarioQualityForWithSource(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	add(dpPath+"replan_cap", "store.ResolveReplanCap (DayPlanConfig.ReplanCapFor delegates)", func(x *effCtx) effResult {
		v, src := effReplanCap(x.dp(), x.session)
		origin := explicitZeroOrigin(x, store.KnobReplanStrategy, src, src == store.SourceStrategyValue)
		return effResult{value: v, origin: origin, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	add(dpPath+"one_setup_enabled", "store.ResolveOneSetup", func(x *effCtx) effResult {
		v, _, src, _ := store.ResolveOneSetup(x.cfg)
		return effResult{value: v, origin: src}
	})
	add(dpPath+"one_setup_min_grade", "store.ResolveOneSetup", func(x *effCtx) effResult {
		_, g, _, src := store.ResolveOneSetup(x.cfg)
		return effResult{value: g, origin: src}
	})
	add(dpPath+"structure_map", "store.ResolveStructureMap", func(x *effCtx) effResult {
		v, src := store.ResolveStructureMap(x.cfg)
		return effResult{value: v, origin: src}
	})
	// W-EXEC-TRUTH W3 (2026-09-23) — the entry-policy knobs, each through its
	// ONE resolver (the write site, the prompt and the boot line read the same).
	add(dpPath+"entry_policy_default", "store.ResolveEntryPolicyDefault", func(x *effCtx) effResult {
		v, src := store.ResolveEntryPolicyDefault(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"zone_max_pts", "store.ResolveZoneMaxPts", func(x *effCtx) effResult {
		v, src := store.ResolveZoneMaxPts(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"zone_rest_max_min", "store.ResolveZoneRestMaxMin", func(x *effCtx) effResult {
		v, src := store.ResolveZoneRestMaxMin(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"zone_place_within_pts", "store.ResolveZonePlaceWithinPts", func(x *effCtx) effResult {
		v, src := store.ResolveZonePlaceWithinPts(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"min_hold_min", "store.ResolveMinHoldMin", func(x *effCtx) effResult {
		v, src := store.ResolveMinHoldMin(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"proximity_filter_atr", "trader.(*AutoTrader).proximityFilterATR → kernel.ResolveProximityK", func(x *effCtx) effResult {
		v := x.at.proximityFilterATR()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(dpPath+"max_levels", "trader.resolveSessionPlanCfg", func(x *effCtx) effResult {
		v, _, _, _, _ := resolveSessionPlanCfg(x.dp(), x.session)
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(dpPath+"htf_seats", "trader.resolveSessionPlanCfg (nil → kernel.LegacyHtfSeats)", func(x *effCtx) effResult {
		_, seats, _, _, _ := resolveSessionPlanCfg(x.dp(), x.session)
		if seats == nil {
			return effResult{value: kernel.LegacyHtfSeats, origin: store.SourceShippedDefault}
		}
		if x.st.present && storedEquals(x.st.value, *seats) {
			return effResult{value: *seats, origin: store.SourceSaved}
		}
		return effResult{value: *seats, origin: "clamp (trader.resolveSessionPlanCfg 0–6)"}
	})
	add(dpPath+"planner_timeframes", "trader.resolveSessionPlanCfg", func(x *effCtx) effResult {
		_, _, _, _, tfs := resolveSessionPlanCfg(x.dp(), x.session)
		return effResult{value: tfs, origin: presenceOrigin(x, tfs, store.SourceShippedDefault)}
	})
	dpBool := func(path, name, def string, get func(*store.DayPlanConfig) bool) {
		add(dpPath+path, name, func(x *effCtx) effResult {
			v := get(x.dp())
			return effResult{value: v, origin: presenceOrigin(x, v, def)}
		})
	}
	dpBool("flip_reread", "store.(*DayPlanConfig).FlipRereadEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).FlipRereadEnabled)
	dpBool("death_reread", "store.(*DayPlanConfig).DeathRereadEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).DeathRereadEnabled)
	dpBool("write_time_feasibility", "store.(*DayPlanConfig).WriteTimeFeasibilityEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).WriteTimeFeasibilityEnabled)
	dpBool("geometry_reference_levels", "store.(*DayPlanConfig).GeometryRefIDsEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).GeometryRefIDsEnabled)
	dpBool("levels_fresh_by_tf", "store.(*DayPlanConfig).LevelsFreshByTFEnabled", OriginFolded, (*store.DayPlanConfig).LevelsFreshByTFEnabled)
	dpBool("evening_digest", "store.(*DayPlanConfig).EveningDigestOn", OriginFolded, (*store.DayPlanConfig).EveningDigestOn)
	dpBool("wake_on_htf_ob", "store.(*DayPlanConfig).WakeOnHTFOrderBlocks", OriginFolded, (*store.DayPlanConfig).WakeOnHTFOrderBlocks)
	add(dpPath+"wake_on_level_events", "store.(*DayPlanConfig).WakeOnLevelEventsEnabled", func(x *effCtx) effResult {
		v := x.dp().WakeOnLevelEventsEnabled()
		if !x.st.present && legacyWakeStored(x.root) {
			return effResult{value: v, origin: OriginFolded + " — legacy wake_on_* fields decide"}
		}
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	dpInt := func(path, name string, get func(*store.DayPlanConfig) int) {
		add(dpPath+path, name, func(x *effCtx) effResult {
			v := get(x.dp())
			return effResult{value: v, origin: presenceOrigin(x, v, OriginFolded)}
		})
	}
	dpInt("scenario_cap", "store.(*DayPlanConfig).ScenarioCapResolved", (*store.DayPlanConfig).ScenarioCapResolved)
	dpInt("realign_cap", "store.(*DayPlanConfig).RealignCapResolved", (*store.DayPlanConfig).RealignCapResolved)
	dpInt("wake_min_interval_min", "store.(*DayPlanConfig).WakeMinIntervalMinutes", (*store.DayPlanConfig).WakeMinIntervalMinutes)
	add(dpPath+"acceptance_rule", "store.(*DayPlanConfig).AcceptanceRuleFor", func(x *effCtx) effResult {
		return effResult{value: x.dp().AcceptanceRuleFor(x.session), origin: OriginFolded}
	})
	add(dpPath+"t1_currencies", "store.(*DayPlanConfig).T1CurrenciesFor", func(x *effCtx) effResult {
		v := x.dp().T1CurrenciesFor()
		if x.dp().T1CurrenciesSaved() {
			return effResult{value: v, origin: store.SourceSaved}
		}
		return effResult{value: v, origin: store.SourceShippedDefault}
	})
	add(dpPath+"approval_required", "trader.(*AutoTrader).approvalRequired", func(x *effCtx) effResult {
		v := x.at.approvalRequired()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add(dpPath+"fade_or_wide_k", "trader.(*AutoTrader).fadeORWideKResolved", func(x *effCtx) effResult {
		v, src := x.at.fadeORWideKResolved()
		if src == "strategy" {
			return effResult{value: v, origin: store.SourceSaved}
		}
		return effResult{value: v, origin: store.SourceShippedDefault + " (" + src + ")"}
	})
	add(dpPath+"sessions_enabled", "trader.(*AutoTrader).sessionEnabledForStrategy (per registry session)", func(x *effCtx) effResult {
		var on []string
		for _, s := range kernel.DefaultSessionRegistry().Sessions {
			if x.at.sessionEnabledForStrategy(s.Name) {
				on = append(on, s.Name)
			}
		}
		if on == nil {
			on = []string{}
		}
		return effResult{value: on, origin: presenceOrigin(x, on, store.SourceShippedDefault)}
	})
	add(dpPath+"condition_status", "kernel.ConditionStatusWithSource (= kernel.ConditionStatus) per known condition", func(x *effCtx) effResult {
		return conditionStatusRow(x)
	})
	pictureRow := func(path string, pick func(store.PictureHtfConfig) any) {
		add(dpPath+"picture_htf."+path, "trader.(*AutoTrader).pictureHtfResolvedConfig → store.PictureHtfResolved", func(x *effCtx) effResult {
			v := pick(x.at.pictureHtfResolvedConfig())
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeFutures}
		})
	}
	pictureRow("enabled", func(p store.PictureHtfConfig) any { return p.Enabled })
	pictureRow("tick_size", func(p store.PictureHtfConfig) any { return p.TickSize })
	pictureRow("pivot_window", func(p store.PictureHtfConfig) any { return p.PivotWindow })
	pictureRow("swing_lookback", func(p store.PictureHtfConfig) any { return p.SwingLookback })
	pictureRow("entry_window_sec", func(p store.PictureHtfConfig) any { return p.EntryWindowSec })
	pictureRow("freshness_sec", func(p store.PictureHtfConfig) any { return p.FreshnessSec })
	add(dpPath+"picture_htf.min_rr", "trader.(*AutoTrader).pictureMinRR (max of the knob and the strategy R:R floor)", func(x *effCtx) effResult {
		knob := x.at.pictureHtfResolvedConfig().MinRR
		v, ok := x.at.pictureMinRR(knob)
		if !ok {
			return effResult{value: "n/a — no R:R floor resolved", origin: OriginNA, scope: ScopeFutures}
		}
		if knob > 0 && v == knob {
			return effResult{value: v, origin: store.SourceSaved, scope: ScopeFutures}
		}
		return effResult{value: v, origin: "clamp (trader.pictureMinRR: min_risk_reward_ratio floor)", scope: ScopeFutures}
	})
	add(dpPath+"structural_stop.buffer_points", "store.ResolveStructuralStop(cfg, \"MNQ\")", func(x *effCtx) effResult {
		p := store.ResolveStructuralStop(x.cfg, "MNQ")
		if !p.BufferKnown {
			return effResult{value: "n/a — buffer unresolved (" + p.BufferSource + ")", origin: OriginNA, scope: ScopeFutures}
		}
		if p.BufferSource == "day_plan.structural_stop.buffer_points" {
			return effResult{value: p.BufferPoints, origin: store.SourceSaved, scope: ScopeFutures}
		}
		return effResult{value: p.BufferPoints, origin: store.SourceShippedDefault + " (" + p.BufferSource + ")", scope: ScopeFutures}
	})
	add(dpPath+"structural_stop.round_trip_cost_points", "store.ResolveStructuralStop(cfg, \"MNQ\")", func(x *effCtx) effResult {
		p := store.ResolveStructuralStop(x.cfg, "MNQ")
		if !p.CostKnown {
			return effResult{value: "n/a — cost unresolved", origin: OriginNA, scope: ScopeFutures}
		}
		return effResult{value: p.CostPoints, origin: presenceOrigin(x, p.CostPoints, store.SourceShippedDefault), scope: ScopeFutures}
	})

	// ── Day plan: per-session rows (day_plan.sessions.*) ─────────────────────
	perSession := func(path, name string, fn func(x *effCtx) effResult) {
		add(spPath+path, name, func(x *effCtx) effResult {
			if x.session == "" {
				return effResult{value: EffectiveNoSession, origin: OriginNA, scope: scopeSession("")}
			}
			return fn(x)
		})
	}
	perSession("plan_mode", "store.ResolvePlanMode", func(x *effCtx) effResult {
		v, src := store.ResolvePlanMode(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("min_scenario_quality", "store.MinScenarioQualityForWithSource (= DayPlanConfig.MinScenarioQualityFor)", func(x *effCtx) effResult {
		v, src := store.MinScenarioQualityForWithSource(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("min_grade", "store.MinGradeForWithSource (= DayPlanConfig.MinGradeFor)", func(x *effCtx) effResult {
		v, src := store.MinGradeForWithSource(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("max_trades", "store.MaxTradesForWithSource (= DayPlanConfig.MaxTradesFor)", func(x *effCtx) effResult {
		n, ok, src := store.MaxTradesForWithSource(x.dp(), x.session)
		if !ok {
			return effResult{value: "no per-session cap", origin: src}
		}
		return effResult{value: n, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("last_entry_offset_min", "store.LastEntryOffsetForWithSource (= DayPlanConfig.LastEntryOffsetFor)", func(x *effCtx) effResult {
		v, src := store.LastEntryOffsetForWithSource(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("eod_flat_offset_min", "store.EODFlatOffsetForWithSource (= DayPlanConfig.EODFlatOffsetFor)", func(x *effCtx) effResult {
		v, src := store.EODFlatOffsetForWithSource(x.dp(), x.session)
		return effResult{value: v, origin: src, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("replan_cap", "store.ResolveReplanCap (DayPlanConfig.ReplanCapFor delegates)", func(x *effCtx) effResult {
		v, src := effReplanCap(x.dp(), x.session)
		origin := explicitZeroOrigin(x, store.KnobReplanStrategy, src, src == store.SourceStrategyValue)
		return effResult{value: v, origin: origin, scope: scopeForSource(src, x.session, ScopeStrategy)}
	})
	perSession("enable", "trader.(*AutoTrader).sessionEnabledForStrategy (the admin session registry also gates: sessionRunnable)", func(x *effCtx) effResult {
		v := x.at.sessionEnabledForStrategy(x.session)
		if ov := x.dp().SessionOverride(x.session); ov != nil && ov.Enable != nil {
			return effResult{value: v, origin: store.SourceSessionOverride, scope: scopeSession(x.session)}
		}
		if x.dp() != nil && len(x.dp().SessionsEnabled) > 0 {
			return effResult{value: v, origin: store.SourceStrategyValue + " (sessions_enabled)"}
		}
		return effResult{value: v, origin: store.SourceShippedDefault + " (sessions_enabled [NY])"}
	})
	perSession("acceptance_rule", "store.(*DayPlanConfig).AcceptanceRuleFor", func(x *effCtx) effResult {
		return effResult{value: x.dp().AcceptanceRuleFor(x.session), origin: OriginFolded}
	})
	perSession("condition_status", "kernel.ConditionStatusWithSource (= kernel.ConditionStatus) per known condition", func(x *effCtx) effResult {
		return conditionStatusRow(x)
	})
	return m
}

// legacyWakeStored reports whether any of the five legacy wake switches is
// stored (the new switch being absent, they decide WakeOnLevelEventsEnabled).
func legacyWakeStored(root json.RawMessage) bool {
	for _, p := range []string{"wake_on_15m_zone", "wake_on_htf_zone", "wake_on_htf_ob", "wake_on_seated_invalidation", "wake_on_ifvg"} {
		if lookupStored(root, dpPath+p, "").present {
			return true
		}
	}
	return false
}

// conditionStatusRow resolves every known condition through the arm seam's
// chain and names, per source, which conditions it decided.
func conditionStatusRow(x *effCtx) effResult {
	var base, sess map[string]string
	if dp := x.dp(); dp != nil {
		base = dp.ConditionStatus
		if ov := dp.SessionOverride(x.session); ov != nil && ov.ConditionStatus != nil {
			sess = *ov.ConditionStatus
		}
	}
	env := kernel.ShadowConditionsEnv()
	out := map[string]string{}
	bySrc := map[string][]string{}
	for _, c := range kernel.KnownConditions() {
		st, src := kernel.ConditionStatusWithSource(c, base, sess, env)
		out[c] = st
		bySrc[src] = append(bySrc[src], c)
	}
	order := []string{store.SourceSessionOverride, store.SourceStrategyValue, kernel.ConditionSourceEnvLive, kernel.ConditionSourceEnvShadow, store.SourceShippedDefault}
	var parts []string
	scope := ScopeStrategy
	for _, src := range order {
		cs := bySrc[src]
		if len(cs) == 0 {
			continue
		}
		if len(parts) == 0 {
			scope = scopeForSource(src, x.session, ScopeStrategy)
		}
		if src == store.SourceShippedDefault && len(parts) == 0 {
			parts = append(parts, src)
			continue
		}
		if src == store.SourceShippedDefault {
			parts = append(parts, src+" (rest)")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", src, strings.Join(cs, ", ")))
	}
	return effResult{value: out, origin: strings.Join(parts, " · "), scope: scope, compound: true}
}
