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

	"vl/kernel"
	"vl/store"
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
// An explicit set: external data-source headers and URLs (a URL commonly carries
// a key in its query string), and any leaf whose name reads as a credential.
// Redacted rows never carry the stored value or an effective one.

var secretLeafRe = regexp.MustCompile(`(?i)(api_?key|secret|token|password|passphrase|private_?key|credential)`)

func isSecretPath(path string) bool {
	if strings.Contains(path, "external_data_sources.headers") || strings.Contains(path, "external_data_sources.url") {
		return true
	}
	leaf := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		leaf = path[i+1:]
	}
	return secretLeafRe.MatchString(leaf)
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

// exitMechSuspendedRow is the effective row while the 0B wire seam is suspended
// (exitMechsSuspended() — the SAME function the wire gate reads; the message is
// never a copy). The sentence is the EFFECTIVE VALUE, so the chip never prints
// "eff on"/"eff off" for a mechanism that cannot move a stop.
func exitMechSuspendedRow() effResult {
	reason := "env EXIT_MECHS_SUSPENDED unset"
	if v := strings.TrimSpace(os.Getenv("EXIT_MECHS_SUSPENDED")); v != "" {
		reason = "env EXIT_MECHS_SUSPENDED=" + v
	}
	return effResult{
		value:  "SUSPENDED — " + reason + " (no stop is ever moved)",
		origin: "env EXIT_MECHS_SUSPENDED (0B suspension)",
		scope:  ScopeProcessEnv,
	}
}

// mentorNotUsedSizingRow is the effective value for a knob mentor mode overrides:
// mentor sizing governs (mentor_max_contracts = N, resolved from the production
// reader — never a literal), so the inert saved value is not the running number.
func mentorNotUsedSizingRow(x *effCtx) effResult {
	mx, _ := x.at.mentorMaxContracts()
	return effResult{
		value:  fmt.Sprintf("not used in mentor mode — mentor sizing governs (mentor_max_contracts = %d)", mx),
		origin: "mentor_mode on",
		scope:  ScopeStrategy,
	}
}

// pepNotUsedSizingRow is mentorNotUsedSizingRow's pep twin: a pep trader sizes
// every entry from pep_contracts (resolveMaxContracts returns pepMaxContracts),
// never from the AI Risk Control cap, so the inert saved value is not the
// running number. The count is the production reader's (never a literal).
func pepNotUsedSizingRow(x *effCtx) effResult {
	n, _ := x.at.pepMaxContracts()
	return effResult{
		value:  fmt.Sprintf("not used in pep mode — pep sizing governs (pep_contracts = %d)", n),
		origin: pepModeOnOrigin,
		scope:  ScopeStrategy,
	}
}

// pepModeOnOrigin is the origin marker the Studio reads (RiskControlEditor
// mentorNotAppliedCaveat) to render a "not applied" caveat for pep, the twin of
// the mentor marker "mentor_mode on".
const pepModeOnOrigin = "pep_enabled on"

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
		// release #32 (B2 item b) — mentor mode owns entries; the AI confidence
		// floor never gates a mentor entry, so the saved 60 is not the running
		// number on a mentor strategy.
		if x.at != nil && x.at.mentorEnabled() {
			return effResult{
				value:  "not used in mentor mode — mentor entries come from the course rules and carry no AI confidence",
				origin: "mentor_mode on",
				scope:  ScopeStrategy,
			}
		}
		// PEP (rereview m461 P2-1): pep entries are the PDF rules and carry no AI
		// confidence either; the AI makes no entries at all.
		if x.at != nil && x.at.pepEnabled() {
			return effResult{
				value:  "not used in pep mode — pep entries come from the PDF rules and carry no AI confidence",
				origin: pepModeOnOrigin,
				scope:  ScopeStrategy,
			}
		}
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
	add(rcPath+"max_contracts_per_order", "trader.(*AutoTrader).resolveMaxContracts", func(x *effCtx) effResult {
		// MENTOR-SIZE-TRUTH (DS-105 R2 P2): in mentor mode resolveMaxContracts
		// returns mentor_max_contracts (default 20), NOT this knob — the 0B
		// 2-contract clamp does not apply to a mentor-mode trader. release #32
		// (B2 item b): the message names the mentor's governor with its resolved
		// value, taken from the production reader (never a literal).
		if x.at != nil && x.at.mentorEnabled() {
			return mentorNotUsedSizingRow(x)
		}
		// PEP (rereview m461 P2-1): resolveMaxContracts returns pep_contracts for
		// a pep trader; the 0B 2-contract clamp and this knob do not apply.
		if x.at != nil && x.at.pepEnabled() {
			return pepNotUsedSizingRow(x)
		}
		n, src := kernel.ResolveMaxContractsWithSource(x.rc().MaxContractsPerOrder, int(maxFuturesContracts))
		scope := ScopeFutures
		if strings.HasPrefix(src, "env ") || strings.HasPrefix(src, "clamp") {
			scope = ScopeProcessEnv
		}
		return effResult{value: n, origin: src, scope: scope}
	})
	add(rcPath+"mentor_max_contracts", "trader.(*AutoTrader).mentorMaxContracts (the mentor-mode ceiling resolveMaxContracts actually returns)", func(x *effCtx) effResult {
		if x.at != nil && x.at.pepEnabled() {
			return effResult{value: "— (pep mode; not applied)", origin: "mentor_mode off", scope: ScopeStrategy}
		}
		if x.at == nil || !x.at.mentorEnabled() {
			return effResult{value: "— (AI mode; not applied)", origin: "mentor_mode off", scope: ScopeStrategy}
		}
		mx, _ := x.at.mentorMaxContracts()
		return effResult{value: mx, origin: presenceOrigin(x, mx, store.SourceShippedDefault), scope: ScopeStrategy}
	})

	// ── Mentor mode: every risk_control.mentor_* / mentor_tuning.* path ──────
	// F-13 (W32 mentor-knob-truth): the owner must see the value the bot RUNS.
	// Every resolver calls the production resolver the runtime uses (no copied
	// literals); origin is "saved value" when the stored row holds that exact
	// value, else "shipped default". mentor_done_after_win + its day_start are
	// DS-104's rows — deliberately left unresolved here.

	add(rcPath+"mentor_mode", "trader.(*AutoTrader).mentorEnabled", func(x *effCtx) effResult {
		v := x.at.mentorEnabled()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	mentorSizeRow := func(path, name string, pick func(b, c, big, r, s4, sp, mx int) int) {
		add(rcPath+path, name, func(x *effCtx) effResult {
			b, c, big, r, s4, sp, mx := x.at.mentorKnobs()
			v := pick(b, c, big, r, s4, sp, mx)
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
		})
	}
	mentorSizeRow("mentor_base_contracts", "trader.(*AutoTrader).mentorKnobs", func(b, _, _, _, _, _, _ int) int { return b })
	mentorSizeRow("mentor_confluence_contracts", "trader.(*AutoTrader).mentorKnobs", func(_, c, _, _, _, _, _ int) int { return c })
	mentorSizeRow("mentor_big_contracts", "trader.(*AutoTrader).mentorKnobs", func(_, _, big, _, _, _, _ int) int { return big })
	mentorSizeRow("mentor_reduced_contracts", "trader.(*AutoTrader).mentorKnobs", func(_, _, _, r, _, _, _ int) int { return r })
	mentorSizeRow("mentor_swing4h_contracts", "trader.(*AutoTrader).mentorKnobs", func(_, _, _, _, s4, _, _ int) int { return s4 })
	add(rcPath+"mentor_spent_day_contracts", "DEAD — mentorContractsFor discards it; the spent-day runner cap is the constant mentorSpentDayRunnerCap=2 (F-7)", func(x *effCtx) effResult {
		return effResult{value: "not read (dead — the spent-day runner cap is the constant 2)", origin: "ineffective", scope: ScopeStrategy}
	})
	add(rcPath+"mentor_trail_mode", "trader.(*AutoTrader).mentorTrailMode", func(x *effCtx) effResult {
		v := x.at.mentorTrailMode()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	add(rcPath+"mentor_trail_tf", "trader.(*AutoTrader).mentorTrailTF", func(x *effCtx) effResult {
		v := x.at.mentorTrailTF()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	add(rcPath+"mentor_window_start", "trader.(*AutoTrader).mentorWindowKnobs", func(x *effCtx) effResult {
		start, _ := x.at.mentorWindowKnobs()
		return effResult{value: start, origin: presenceOrigin(x, start, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	add(rcPath+"mentor_window_minutes", "trader.(*AutoTrader).mentorWindowKnobs", func(x *effCtx) effResult {
		_, min := x.at.mentorWindowKnobs()
		return effResult{value: min, origin: presenceOrigin(x, min, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	mentorRCRow := func(path, name string, get func(store.RiskControlConfig) any) {
		add(rcPath+path, name, func(x *effCtx) effResult {
			v := get(x.rc())
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
		})
	}
	mentorRCRow("mentor_stale_data_block", "mentorStaleDataBlockEnabled (nil → ON)", func(rc store.RiskControlConfig) any { return mentorStaleDataBlockEnabled(&rc) })
	mentorRCRow("mentor_stop_after_loss", "mentor_tick.go reads v==nil||!*v (nil → OFF)", func(rc store.RiskControlConfig) any {
		if rc.MentorStopAfterLoss == nil {
			return false
		}
		return *rc.MentorStopAfterLoss
	})
	mentorRCRow("mentor_day_off_recheck", "mentorDayOffRecheck (nil → OFF)", func(rc store.RiskControlConfig) any { return mentorDayOffRecheck(&rc) })
	mentorRCRow("mentor_leg_budget_enabled", "mentorLegBudgetEnabled (nil → ON)", func(rc store.RiskControlConfig) any { return mentorLegBudgetEnabled(&rc) })
	add(rcPath+"mentor_leg_reset_on", "mentorLegResetOn (default \"close\")", func(x *effCtx) effResult {
		v := mentorLegResetOn(x.at, &x.cfg.RiskControl)
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
	})
	mentorRCRow("mentor_lvl_revisit_min_pts", "mentorLvlRevisitMinPts (default 0)", func(rc store.RiskControlConfig) any { return mentorLvlRevisitMinPts(&rc) })
	mentorRCRow("mentor_ema_max_cross_30m", "mentorEmaMaxCross30m (default 0 — removed; the mentor gives no count)", func(rc store.RiskControlConfig) any { return mentorEmaMaxCross30m(&rc) })
	mentorRCRow("mentor_loc_trigger_filter", "mentorLocationTriggerFilter (nil → ON; inert under school 1 unless level_entries_need_trigger_side)", func(rc store.RiskControlConfig) any { return mentorLocationTriggerFilter(&rc) })
	mentorRCRow("mentor_loss_departure_pts", "mentorLossDeparturePts (default 0)", func(rc store.RiskControlConfig) any { return mentorLossDeparturePts(&rc) })

	// course-exits (release #25): each through mentorCourseExitsResolve.
	mentorCXRow := func(path, name string, get func(mentorCourseExitsResolved) any) {
		add(rcPath+path, name, func(x *effCtx) effResult {
			v := get(mentorCourseExitsResolve(x.at, &x.cfg.RiskControl))
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
		})
	}
	mentorCXRow("mentor_swing_ema34_is_first_target", "trader.mentorCourseExitsResolve (CX-1, nil → ON)", func(c mentorCourseExitsResolved) any { return c.SwingEMA34FirstTarget })
	mentorCXRow("mentor_swing_runner", "trader.mentorCourseExitsResolve (CX-2, nil → ON)", func(c mentorCourseExitsResolved) any { return c.SwingRunner })
	mentorCXRow("mentor_resonance_runner_tp_old_high", "trader.mentorCourseExitsResolve (CX-3, nil → OFF = runner uncapped)", func(c mentorCourseExitsResolved) any { return c.ResonanceRunnerTP })
	mentorCXRow("mentor_be_before_partial", "trader.mentorCourseExitsResolve (CX-4, nil → ON)", func(c mentorCourseExitsResolved) any { return c.BEBeforePartial })
	mentorCXRow("mentor_isb_c3_partial_any_profit", "trader.mentorCourseExitsResolve (CX-5, nil → ON)", func(c mentorCourseExitsResolved) any { return c.ISBC3AnyProfit })
	mentorCXRow("mentor_reverse_isb_as_phl", "trader.mentorCourseExitsResolve (CX-6, nil → ON)", func(c mentorCourseExitsResolved) any { return c.ReverseISBAsPHL })
	mentorCXRow("mentor_ema9_visit_cap", "trader.mentorCourseExitsResolve (CX-8)", func(c mentorCourseExitsResolved) any { return c.EMA9VisitCap })
	mentorCXRow("mentor_isb_max_pts", "trader.mentorCourseExitsResolve", func(c mentorCourseExitsResolved) any { return c.ISBMaxPts })
	mentorCXRow("mentor_keypair_candle_ratio", "trader.mentorCourseExitsResolve", func(c mentorCourseExitsResolved) any { return c.KeypairRatio })
	mentorCXRow("mentor_box_fresh_rth_pair", "trader.mentorCourseExitsResolve", func(c mentorCourseExitsResolved) any { return c.BoxFreshRTHPair })
	mentorCXRow("mentor_box_max_height_pts", "trader.mentorCourseExitsResolve", func(c mentorCourseExitsResolved) any { return c.BoxMaxHeightPts })

	// mentor_tuning.* (K3): each through mentorTuningResolve (the ONE resolver).
	mentorTuningRow := func(path, name string, get func(mentorTuned) any) {
		add(rcPath+"mentor_tuning."+path, name, func(x *effCtx) effResult {
			v := get(mentorTuningResolve(x.at, &x.cfg.RiskControl))
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeStrategy}
		})
	}
	mentorTuningRow("trigger_school", "trader.mentorTuningResolve → kernel DefaultConfig", func(t mentorTuned) any { return t.TriggerSchool })
	mentorTuningRow("ping_pong_min_gap_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.PingPongMinGapPts })
	mentorTuningRow("ping_pong_candle_max_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.PingPongCandleMaxPts })
	mentorTuningRow("ping_pong_candle_lookback", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.PingPongCandleLookback })
	mentorTuningRow("level_max_visits", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.LevelMaxVisits })
	mentorTuningRow("orb_gate_enabled", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.OrbGateEnabled })
	mentorTuningRow("isb_reverse_ema9_enabled", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBReverseEMA9Enabled })
	mentorTuningRow("htf_gate_news_only", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.HTFGateNewsOnly })
	mentorTuningRow("exec_2m_after_30m", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.Exec2mAfter30m })
	mentorTuningRow("day_gate_spent_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.DayGateSpentPts })
	mentorTuningRow("day_gate_target_cap_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.DayGateTargetCapPts })
	mentorTuningRow("swing_max_stop_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.SwingMaxStopPts })
	mentorTuningRow("swing_min_stop_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.SwingMinStopPts })
	mentorTuningRow("15m_ib_requires_5m_isb", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.FifteenMinIBRule })
	mentorTuningRow("tf_conflict_end", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.TFConflictEnd })
	mentorTuningRow("news_wait_both_lines", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.NewsWaitBothLines })
	mentorTuningRow("news_bars_excluded", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.NewsBarsExcluded })
	mentorTuningRow("isb_box_escape", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBBoxEscapeMode })
	mentorTuningRow("ema34_loss_relock", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.EmaLossRelock })
	mentorTuningRow("orb_edge_veto_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.OrbEdgeVetoPts })
	mentorTuningRow("orb_isb_fraction_outside", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.OrbISBFractionOutside })
	mentorTuningRow("d43_trend_source", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.D43TrendSource })
	mentorTuningRow("level_entries_need_trigger_side", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.LevelEntriesNeedTriggerSide })
	mentorTuningRow("trendline_is_target", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.TrendlineIsTarget })
	mentorTuningRow("trendline_between_boxes", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.TrendlineBetweenBoxes })
	mentorTuningRow("leg_reset_on_wave_break", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.LegResetOnWaveBreak })
	mentorTuningRow("flag_wall_target", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.FlagWallTarget })
	mentorTuningRow("box_new_zone_after_escape", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.BoxNewZoneAfterEscape })
	mentorTuningRow("orb_escape_completely", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.OrbEscapeCompletely })
	mentorTuningRow("isb_stack_cancel_candle_2m", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBStackCancelCandle2m })
	mentorTuningRow("isb_stack_tiny_stop_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBStackTinyStopPts })
	mentorTuningRow("evening_slow_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.EveningSlowPts })
	mentorTuningRow("evening_slow_from_hour", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.EveningSlowFromHour })
	mentorTuningRow("evening_slow_window_min", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.EveningSlowWindowMin })
	mentorTuningRow("near_box_room_multiple", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.NearBoxRoomMultiple })
	mentorTuningRow("near_box_room_phl", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.NearBoxRoomPHL })
	mentorTuningRow("phl_target_shy_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.PHLTargetShyPts })
	mentorTuningRow("isb_old_extreme_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBOldExtremePts })
	mentorTuningRow("isb_in_range_gap_pts", "trader.mentorTuningResolve", func(t mentorTuned) any { return t.ISBInRangeGapPts })

	add(rcPath+"planner_contracts", "trader.plannerContracts (dispatch-31), clamped to resolveMaxContracts — the size the authoring stamp actually writes", func(x *effCtx) effResult {
		// The EFFECTIVE size: min(plannerContracts(rc), resolveMaxContracts()).
		// A knob of 4 with the always-on clamp at 1 stamps 1 — the effective
		// line must say so, with the binding source as the origin (fold-564-b
		// D4: the old line showed 4 while the wire sent 1).
		rc := x.rc()
		v := plannerContracts(&rc)
		origin := store.SourceShippedDefault
		if rc.PlannerContracts > 0 {
			origin = store.SourceSaved
		}
		if rc.PlannerContracts > plannerContractsCeiling {
			origin = "clamp (trader.plannerContractsCeiling = 10)"
		}
		scope := ScopeStrategy
		if x.at != nil && x.at.pepEnabled() {
			// PEP: the AI planner makes no entries, so this stamp size is never used.
			return effResult{value: "not used in pep mode — the AI planner makes no entries", origin: pepModeOnOrigin, scope: ScopeStrategy}
		}
		if x.at != nil && !x.at.mentorEnabled() {
			max, maxSrc := kernel.ResolveMaxContractsWithSource(rc.MaxContractsPerOrder, int(maxFuturesContracts))
			if v > max {
				v = max
				origin = maxSrc
				scope = ScopeProcessEnv
			}
		}
		return effResult{value: v, origin: origin, scope: scope}
	})
	// MENTOR-DONE-AFTER-WIN eff (release #32): the switch was UI-BLIND — the
	// panel said "n/a — no resolver registered" while the gate read it. The
	// value is on/off from the ONE resolver the gate and the sweep use; the
	// origin names the course's nil → ON default when the key is absent.
	add(rcPath+"mentor_done_after_win", "trader.(*AutoTrader).mentorDoneAfterWinEnabled", func(x *effCtx) effResult {
		on := x.at != nil && x.at.mentorDoneAfterWinEnabled()
		v := "off"
		if on {
			v = "on"
		}
		origin := store.SourceShippedDefault + " (course default: ON)"
		if x.st.present {
			origin = store.SourceSaved
		}
		return effResult{value: v, origin: origin}
	})
	add(rcPath+"mentor_done_after_win_day_start", "trader.(*AutoTrader).mentorDoneAfterWinDayStart", func(x *effCtx) effResult {
		if x.at == nil {
			return effResult{value: mentorDoneAfterWinDayStartDefault, origin: store.SourceShippedDefault}
		}
		v := x.at.mentorDoneAfterWinDayStart()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
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
			// release #32 (B2 item a) — the 0B wire seam is checked FIRST, on
			// BOTH traders: while suspended no stop is ever moved, mentor or not.
			if exitMechsSuspended() {
				return exitMechSuspendedRow()
			}
			// FIX-AUTOBE-OFF-IN-MENTOR (owner ruling 2026-10-09): with mentor
			// mode ON, the AI-era stop movers do not apply to mentor trades —
			// the mentor's exits own the stop.
			if x.at != nil && x.at.mentorEnabled() {
				return effResult{value: "not applied while mentor mode is ON (owner ruling 2026-10-09)", origin: "mentor_mode on", scope: ScopeStrategy}
			}
			// PEP (rereview m461 P2-1): modeOwnsExits() keeps the AI-era stop movers
			// off a pep position; pep has no break-even and no trailing (PDF rule).
			if x.at != nil && x.at.pepEnabled() {
				return effResult{value: "not applied while pep mode is ON (pep's fixed stop and target own the exit)", origin: pepModeOnOrigin, scope: ScopeStrategy}
			}
			v := on(x.rc())
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
		if exitMechsSuspended() {
			return exitMechSuspendedRow()
		}
		v := breakevenTriggerPoints(x.rc())
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault), scope: ScopeFutures}
	})
	add(rcPath+"breakeven_trigger_r", "store.ResolveBreakevenTriggerR (trader.breakevenTriggerFor)", func(x *effCtx) effResult {
		if exitMechsSuspended() {
			return exitMechSuspendedRow()
		}
		v, src := store.ResolveBreakevenTriggerR(x.rc())
		return effResult{value: v, origin: src, scope: ScopeFutures}
	})
	trailing := func(path string, pick func(mult float64, period int, arm string, armPts float64) any) {
		add(rcPath+path, "trader.trailingConfig", func(x *effCtx) effResult {
			if exitMechsSuspended() {
				return exitMechSuspendedRow()
			}
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

	// ── Pep (owner order 2026-10-29): every leaf of the root pep block resolves
	// through store.ResolvePep — the ONE resolver kernel/pep.FromSettings and the
	// boot line read. A clamped number names itself in its origin; a legal saved
	// value (including a legal 0) reads "saved value".
	pepRow := func(path, leaf string, get func(r store.PepResolved) any) {
		add(path, "store.ResolvePep (kernel/pep.FromSettings reads the same)", func(x *effCtx) effResult {
			r := store.ResolvePep(x.cfg)
			v := get(r)
			// m461: A / E are the PDF. A rule leaf under A or E runs the PDF's value
			// whatever is saved, and says so (the stored number stays in the DB).
			if r.Locked && store.PepLockedLeaves[leaf] {
				return effResult{value: v, origin: "locked to the PDF (style " + r.Style + "): the saved value is ignored; switch to Custom to edit"}
			}
			if note, clamped := r.Clamps[leaf]; clamped {
				return effResult{value: v, origin: "clamp (store.ResolvePep): " + note}
			}
			if strings.HasPrefix(leaf, "pep_c_") && r.Style != store.PepStyleCustom {
				return effResult{value: v, origin: "not used: style is " + r.Style + " (Custom only)"}
			}
			if x.st.present {
				return effResult{value: v, origin: store.SourceSaved}
			}
			return effResult{value: v, origin: store.SourceShippedDefault}
		})
	}
	pepRow("pep.pep_enabled", "pep_enabled", func(r store.PepResolved) any { return r.Enabled })
	pepRow("pep.pep_style", "pep_style", func(r store.PepResolved) any { return r.Style })
	pepRow("pep.pep_contracts", "pep_contracts", func(r store.PepResolved) any { return r.Contracts })
	pepRow("pep.pep_box_start_ct", "pep_box_start_ct", func(r store.PepResolved) any { return r.BoxStartCT })
	pepRow("pep.pep_flat_time_ct", "pep_flat_time_ct", func(r store.PepResolved) any { return r.FlatTimeCT })
	pepRow("pep.pep_tick_size", "pep_tick_size", func(r store.PepResolved) any { return r.TickSize })
	pepRow("pep.pep_sl_rounding", "pep_sl_rounding", func(r store.PepResolved) any { return r.SLRounding })
	pepRow("pep.pep_max_trades_per_day", "pep_max_trades_per_day", func(r store.PepResolved) any { return r.MaxTradesPerDay })
	pepRow("pep.pep_fee_per_contract_usd", "pep_fee_per_contract_usd", func(r store.PepResolved) any { return r.FeePerContractRT })
	pepRow("pep.pep_max_box_points", "pep_max_box_points", func(r store.PepResolved) any { return r.MaxBoxPoints })
	pepRow("pep.pep_signal_max_age_min", "pep_signal_max_age_min", func(r store.PepResolved) any { return r.SignalMaxAgeMin })
	pepRow("pep.style_a.pep_a_box_minutes", "pep_a_box_minutes", func(r store.PepResolved) any { return r.A.BoxMinutes })
	pepRow("pep.style_a.pep_a_sl_fraction", "pep_a_sl_fraction", func(r store.PepResolved) any { return r.A.SLFraction })
	pepRow("pep.style_a.pep_a_tp_fraction", "pep_a_tp_fraction", func(r store.PepResolved) any { return r.A.TPFraction })
	pepRow("pep.style_a.pep_a_both_edges", "pep_a_both_edges", func(r store.PepResolved) any { return r.A.BothEdges })
	pepRow("pep.style_e.pep_e_box_minutes", "pep_e_box_minutes", func(r store.PepResolved) any { return r.E.BoxMinutes })
	pepRow("pep.style_e.pep_e_sl_fraction", "pep_e_sl_fraction", func(r store.PepResolved) any { return r.E.SLFraction })
	pepRow("pep.style_e.pep_e_tp_fraction", "pep_e_tp_fraction", func(r store.PepResolved) any { return r.E.TPFraction })
	pepRow("pep.style_e.pep_e_ema_period", "pep_e_ema_period", func(r store.PepResolved) any { return r.E.EMAPeriod })
	pepRow("pep.style_e.pep_e_ema_tf_min", "pep_e_ema_tf_min", func(r store.PepResolved) any { return r.E.EMATFMin })
	pepRow("pep.style_e.pep_e_signal_source", "pep_e_signal_source", func(r store.PepResolved) any { return r.E.SignalSource })
	pepRow("pep.style_e.pep_e_warmup_bars", "pep_e_warmup_bars", func(r store.PepResolved) any { return r.E.WarmupBars })
	pepRow("pep.style_custom.pep_c_entry_type", "pep_c_entry_type", func(r store.PepResolved) any { return r.Custom.EntryType })
	pepRow("pep.style_custom.pep_c_box_minutes", "pep_c_box_minutes", func(r store.PepResolved) any { return r.Custom.BoxMinutes })
	pepRow("pep.style_custom.pep_c_sl_fraction", "pep_c_sl_fraction", func(r store.PepResolved) any { return r.Custom.SLFraction })
	pepRow("pep.style_custom.pep_c_tp_fraction", "pep_c_tp_fraction", func(r store.PepResolved) any { return r.Custom.TPFraction })
	pepRow("pep.style_custom.pep_c_both_edges", "pep_c_both_edges", func(r store.PepResolved) any { return r.Custom.BothEdges })
	pepRow("pep.style_custom.pep_c_ema_period", "pep_c_ema_period", func(r store.PepResolved) any { return r.Custom.EMAPeriod })
	pepRow("pep.style_custom.pep_c_ema_tf_min", "pep_c_ema_tf_min", func(r store.PepResolved) any { return r.Custom.EMATFMin })
	pepRow("pep.style_custom.pep_c_signal_source", "pep_c_signal_source", func(r store.PepResolved) any { return r.Custom.SignalSource })
	pepRow("pep.style_custom.pep_c_warmup_bars", "pep_c_warmup_bars", func(r store.PepResolved) any { return r.Custom.WarmupBars })

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
	// Release #25 planner-knobs — the STRICT-12345 loss-fix switches, each
	// READ from the resolver its consumer calls (all default OFF).
	add(dpPath+"planner_room_gate", "store.ResolvePlannerRoomGate", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerRoomGate(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_room_min_r", "store.ResolvePlannerRoomMinR", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerRoomMinR(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_exit_on_thesis_death", "store.ResolvePlannerExitOnThesisDeath", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerExitOnThesisDeath(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_no_chase_enforce", "store.ResolvePlannerNoChaseEnforce", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerNoChaseEnforce(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_momentum_into_zone_max_atr", "store.ResolvePlannerMomentumIntoZoneMaxATR", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerMomentumIntoZoneMaxATR(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_atr_floor_cap_at_invalidation", "store.ResolvePlannerATRFloorCap", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerATRFloorCap(x.dp())
		return effResult{value: v, origin: src}
	})
	add(dpPath+"planner_stale_zones_max_atr", "store.ResolvePlannerStaleZonesMaxATR", func(x *effCtx) effResult {
		v, src := store.ResolvePlannerStaleZonesMaxATR(x.dp())
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
	// S3 (release #32) — planner_model was live but had no resolver (UI-BLIND).
	// Empty binding falls back to the primary model, exactly like
	// resolvePlannerModelID / resolvePlannerClient.
	add(dpPath+"planner_model", "trader.resolvePlannerModelID (empty → primary)", func(x *effCtx) effResult {
		primary := x.at.aiModel
		if strings.TrimSpace(primary) == "" {
			primary = "deepseek"
		}
		pm := ""
		if dp := x.dp(); dp != nil {
			pm = dp.PlannerModel
		}
		modelID, usePrimary := resolvePlannerModelID(pm, primary)
		if usePrimary {
			return effResult{value: modelID, origin: "empty binding → primary model"}
		}
		return effResult{value: modelID, origin: presenceOrigin(x, modelID, store.SourceSaved)}
	})
	// F3 (part-A) — planner_contract was UI-BLIND (no resolver). nil = ON.
	add(dpPath+"planner_contract", "store.(*DayPlanConfig).PlannerContractOn", func(x *effCtx) effResult {
		v := x.dp().PlannerContractOn()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	// F10 (part-A) — the four legacy wake_on_* rows printed "n/a — no resolver
	// registered". They are read ONLY for the wake_on_level_events mapping
	// (nil reads ON, store.legacyWakeOn); resolving them shows that.
	legacyWake := func(path string, get func(*store.DayPlanConfig) *bool) {
		add(dpPath+path, "legacy: read for the wake_on_level_events mapping (nil reads ON)", func(x *effCtx) effResult {
			var p *bool
			if dp := x.dp(); dp != nil {
				p = get(dp)
			}
			v := true // nil pointer reads ON (store.legacyWakeOn)
			if p != nil {
				v = *p
			}
			return effResult{value: v, origin: presenceOrigin(x, v, OriginFolded)}
		})
	}
	legacyWake("wake_on_15m_zone", func(dp *store.DayPlanConfig) *bool { return dp.WakeOn15mZone })
	legacyWake("wake_on_htf_zone", func(dp *store.DayPlanConfig) *bool { return dp.WakeOnHTFZone })
	legacyWake("wake_on_seated_invalidation", func(dp *store.DayPlanConfig) *bool { return dp.WakeOnSeatedInvalidation })
	legacyWake("wake_on_ifvg", func(dp *store.DayPlanConfig) *bool { return dp.WakeOnIFVG })
	dpBool := func(path, name, def string, get func(*store.DayPlanConfig) bool) {
		add(dpPath+path, name, func(x *effCtx) effResult {
			v := get(x.dp())
			return effResult{value: v, origin: presenceOrigin(x, v, def)}
		})
	}
	dpBool("flip_reread", "store.(*DayPlanConfig).FlipRereadEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).FlipRereadEnabled)
	dpBool("planner_fresh_tape", "store.(*DayPlanConfig).PlannerFreshTapeEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).PlannerFreshTapeEnabled)
	dpBool("death_reread", "store.(*DayPlanConfig).DeathRereadEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).DeathRereadEnabled)
	dpBool("write_time_feasibility", "store.(*DayPlanConfig).WriteTimeFeasibilityEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).WriteTimeFeasibilityEnabled)
	dpBool("geometry_reference_levels", "store.(*DayPlanConfig).GeometryRefIDsEnabled", store.SourceShippedDefault, (*store.DayPlanConfig).GeometryRefIDsEnabled)
	dpBool("levels_fresh_by_tf", "store.(*DayPlanConfig).LevelsFreshByTFEnabled", OriginFolded, (*store.DayPlanConfig).LevelsFreshByTFEnabled)
	dpBool("evening_digest", "store.(*DayPlanConfig).EveningDigestOn", OriginFolded, (*store.DayPlanConfig).EveningDigestOn)
	dpBool("wake_on_htf_ob", "store.(*DayPlanConfig).WakeOnHTFOrderBlocks", OriginFolded, (*store.DayPlanConfig).WakeOnHTFOrderBlocks)
	add(dpPath+"wake_on_level_events", "store.(*DayPlanConfig).WakeOnLevelEventsEnabled", func(x *effCtx) effResult {
		v := x.dp().WakeOnLevelEventsEnabled()
		if !x.st.present && legacyWakeStored(x.root) {
			// F4 (part-A): name WHAT inherited the ON — the owner's stored
			// legacy wake_on_htf_ob=true, or the legacy fields generally.
			if ob := lookupStored(x.root, dpPath+"wake_on_htf_ob", ""); ob.present {
				return effResult{value: v, origin: "inherited from stored legacy wake_on_htf_ob=" + string(ob.value) + "; save this toggle to own it"}
			}
			return effResult{value: v, origin: "inherited from stored legacy wake_on_* fields; save this toggle to own it"}
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
	// FIX-PLANNER (2026-09-26): a LIVE knob — origin is shipped-default/saved
	// (the dpInt helper hardcodes the folded origin, which would lie for this
	// row on the UI/API).
	add(dpPath+"death_reread_retry_min", "store.(*DayPlanConfig).DeathRereadRetryMinutes", func(x *effCtx) effResult {
		v := x.dp().DeathRereadRetryMinutes()
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
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
		// S1 (release #32): the value reads "runs: yes/no (via)" so nobody
		// reads a session that an explicit override switches ON as OFF. The
		// bool that decides is still sessionEnabledForStrategy; only the
		// displayed wording changes.
		if ov := x.dp().SessionOverride(x.session); ov != nil && ov.Enable != nil {
			return effResult{value: sessionRunsLabel(v, "override"), origin: store.SourceSessionOverride, scope: scopeSession(x.session)}
		}
		if x.dp() != nil && len(x.dp().SessionsEnabled) > 0 {
			return effResult{value: sessionRunsLabel(v, "sessions_enabled"), origin: store.SourceStrategyValue + " (sessions_enabled)"}
		}
		return effResult{value: sessionRunsLabel(v, "default [NY]"), origin: store.SourceShippedDefault + " (sessions_enabled [NY])"}
	})
	perSession("acceptance_rule", "store.(*DayPlanConfig).AcceptanceRuleFor", func(x *effCtx) effResult {
		return effResult{value: x.dp().AcceptanceRuleFor(x.session), origin: OriginFolded}
	})
	perSession("condition_status", "kernel.ConditionStatusWithSource (= kernel.ConditionStatus) per known condition", func(x *effCtx) effResult {
		return conditionStatusRow(x)
	})

	// ── release #32 (B2 item d) — resolvers for the UI-BLIND rows ─────────────
	// The prompt-section boxes and the indicator periods are LIVE on the futures
	// path (kernel.buildFuturesPrompt / engine_analysis.go read them); the
	// crypto/grid/coin-source rows are crypto-path-only and say so on futures.

	// Prompt sections — the SAME fields buildFuturesPrompt reads.
	add("ai_config.prompt_sections.role_definition", "kernel.buildFuturesPrompt (ps.RoleDefinition)", func(x *effCtx) effResult {
		v := x.cfg.PromptSections.RoleDefinition
		return effResult{value: v, origin: presenceOrigin(x, v, "empty box → prompt builds the fixed CME role")}
	})
	add("ai_config.prompt_sections.trading_frequency", "kernel.buildFuturesPrompt (ps.TradingFrequency)", func(x *effCtx) effResult {
		v := x.cfg.PromptSections.TradingFrequency
		return effResult{value: v, origin: presenceOrigin(x, v, "empty box → omitted from the prompt")}
	})
	add("ai_config.prompt_sections.entry_standards", "kernel.buildFuturesPrompt (ps.EntryStandards)", func(x *effCtx) effResult {
		v := x.cfg.PromptSections.EntryStandards
		return effResult{value: v, origin: presenceOrigin(x, v, "empty box → omitted from the prompt")}
	})
	add("ai_config.prompt_sections.decision_process", "kernel.buildFuturesPrompt (ps.DecisionProcess)", func(x *effCtx) effResult {
		v := x.cfg.PromptSections.DecisionProcess
		return effResult{value: v, origin: presenceOrigin(x, v, "empty box → omitted from the prompt")}
	})
	add("prompt_variant", "trader.(*AutoTrader) prompt variant (auto_trader_loop.go)", func(x *effCtx) effResult {
		v := x.cfg.PromptVariant
		return effResult{value: v, origin: presenceOrigin(x, v, "unset → balanced (no Mode block)")}
	})
	add("strategy_type", "store.(*StrategyConfig).StrategyType (normalizeStrategyType)", func(x *effCtx) effResult {
		v := x.cfg.StrategyType
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})

	// Indicator periods — the clamped/backfilled values the prompt's indicator
	// table (engine_analysis.go) reads.
	add("ai_config.indicators.rsi_periods", "store.(*Strategy).ParseConfig → applyMissingDefaults (engine_analysis.go indicator table)", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.Indicators.RSIPeriods },
			func(c *store.StrategyConfig) bool { return len(c.Indicators.RSIPeriods) == 0 })
	})
	add("ai_config.indicators.klines.longer_timeframe", "store.(*Strategy).ParseConfig → normalizeTimeframe (engine_analysis.go longer-TF)", func(x *effCtx) effResult {
		return clampRow(x, func(c *store.StrategyConfig) any { return c.Indicators.Klines.LongerTimeframe },
			func(c *store.StrategyConfig) bool { return c.Indicators.Klines.LongerTimeframe == "" })
	})

	// Crypto-path-only rows: on a futures strategy the honest value is the
	// marker, never the stored number (CLASS 151 — the stored value stays
	// honoured on the crypto path).
	cryptoOnly := func(path, detail string, get func(*store.StrategyConfig) any) {
		add(path, "crypto path only (CLASS 151)", func(x *effCtx) effResult {
			if x.venue == "ninjatrader" {
				return effResult{value: detail, origin: "venue " + x.venue + " (CLASS 151 — stored value stays honoured on the crypto path)", scope: "venue:" + x.venue}
			}
			v := get(x.clamped)
			return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
		})
	}
	cryptoOnly("ai_config.coin_source.static_coins", "crypto coin list — inert on futures (the futures symbol comes from the NT8 binding)", func(c *store.StrategyConfig) any { return c.CoinSource.StaticCoins })
	cryptoOnly("grid_config.stop_loss_pct", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.StopLossPct
	})
	cryptoOnly("grid_config.symbol", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.Symbol
	})
	cryptoOnly("grid_config.total_investment", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.TotalInvestment
	})
	cryptoOnly("grid_config.lower_price", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.LowerPrice
	})
	cryptoOnly("grid_config.upper_price", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.UpperPrice
	})
	cryptoOnly("grid_config.max_drawdown_pct", "crypto grid path only; inert on futures", func(c *store.StrategyConfig) any {
		if c.GridConfig == nil {
			return nil
		}
		return c.GridConfig.MaxDrawdownPct
	})

	// No-consumer knobs (KnobIneffective): the marker is the truth on every
	// venue, never a borrowed default.
	noConsumer := func(path, detail string) {
		add(path, "no consumer (KnobIneffective)", func(x *effCtx) effResult {
			return effResult{value: detail, origin: "no consumer (KnobIneffective)", scope: ScopeStrategy}
		})
	}
	noConsumer("ai_config.risk_control.max_margin_usage", "prompt text only (advisory) — no gate reads it; inert on futures")
	noConsumer("ai_config.risk_control.min_position_size", "crypto path hardcodes 12/60 — the stored value is never read; inert on futures")

	// publish_config — read by api/strategy.go on every path (not crypto-only).
	add("publish_config.is_public", "api/strategy.go (attachPublishConfig)", func(x *effCtx) effResult {
		var v any = false
		if x.cfg.PublishConfig != nil {
			v = x.cfg.PublishConfig.IsPublic
		}
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})
	add("publish_config.config_visible", "api/strategy.go (attachPublishConfig)", func(x *effCtx) effResult {
		var v any = true
		if x.cfg.PublishConfig != nil {
			v = x.cfg.PublishConfig.ConfigVisible
		}
		return effResult{value: v, origin: presenceOrigin(x, v, store.SourceShippedDefault)}
	})

	return m
}

// sessionRunsLabel is the S1 (release #32) display form of the per-session
// enable bool: "runs: yes (via)" / "runs: no (via)" so the owner can never
// read a session that an explicit override switches ON as OFF. The bool that
// decides entries is unchanged; only the rendered value changes.
func sessionRunsLabel(runs bool, via string) string {
	if runs {
		return "runs: yes (" + via + ")"
	}
	return "runs: no (" + via + ")"
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
