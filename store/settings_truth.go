package store

// ── W1 SETTINGS TRUTH (2026-09-23) — THE CONVERSION REPORT ──────────────────
//
// consecutive_loss_halt and day_plan.replan_cap became presence-aware (*int):
// absent inherits, an explicit 0 is 0. Before W1 both were ints whose 0 meant
// "unset" to the runtime (breaker → env/8, strategy replan → 2) while the UI
// and the struct comment called the breaker's 0 OFF.
//
// So the conversion changes an effective value for EXACTLY ONE stored shape: an
// explicit 0 (breaker, or the strategy-level replan cap). No pre-W1 writer
// could store one — every writer re-marshalled the struct and omitempty dropped
// the 0 — so the expected population is empty [B]. The report proves that at
// every boot instead of assuming it, one line per stored strategy.
//
// FAIL-CLOSED, NO DB WRITE AT BOOT. A row whose effective value WOULD change
// is not rewritten: the trader(s) bound to it are REFUSED at load with a named
// reason until the owner re-saves the knob through a W1 writer. A breaker that
// was inherited (ON) is never silently turned OFF by the conversion.
//
// WHICH ZEROS ARE THE OWNER'S. After W1 the Studio's OFF writes an explicit 0 —
// the new meaning — so a stored 0 alone cannot say whether it predates W1.
// The Studio's two writers (POST/PUT /api/strategies — the surface where the
// owner SEES the knob's state) therefore record which knobs each save holds as
// an explicit 0 (one system_config key per strategy, written at SAVE time by
// the owner's action, never at boot). A 0 the record covers is the owner's and
// loads; a 0 it does not cover is refused. Raw copies (the acceptance-rule
// migration, an agent update without a config) and the agent's config writes
// never confirm — the agent does not show the owner the breaker — so an
// agent-written 0 loads only after a Studio save. Duplicate carries the
// source's record to the copy, since it copies the same bytes.
//
// ONE TRANSACTION (CTO ruling msg 1790176346377, R1). The row and its record
// are written together (CreateWithExplicitZeros / UpdateWithExplicitZeros /
// Duplicate, each one gorm Transaction) — a save that persists the row without
// its record does not exist; if the record cannot be written the row is rolled
// back and the handler answers an error. The record carries the save's time
// (saved_at), so the boot report and the effective endpoint can say WHICH save
// confirmed the 0 (R2). A record that does not parse reads as UNCONFIRMED —
// fail-closed.

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// The knob keys the report and the confirmation record use.
const (
	KnobBreaker        = "risk_control.consecutive_loss_halt"
	KnobReplanStrategy = "day_plan.replan_cap"
)

// settingsTruthSessions — the sessions whose effective replan cap is compared;
// "" is the strategy level (outside every session window).
var settingsTruthSessions = []string{"", "NY", "ASIA", "LONDON"}

// ── THE PRE-W1 RULES, FROZEN ────────────────────────────────────────────────
// Used ONLY by the report to say what the previous binary enforced. Nothing on
// a live path may call these: they are the defect W1 corrects.

// legacyBreakerHaltN is breakerHaltN as it stood before W1: a knob > 0 wins,
// anything else (absent, 0) fell to env BREAKER_HALT_N, else 8.
func legacyBreakerHaltN(stored *int, getenv func(string) string) int {
	if stored != nil && *stored > 0 {
		return *stored
	}
	if n, st := BreakerHaltEnvState(getenv); st == EnvValid {
		return n
	}
	return BreakerHaltDefault
}

// legacyReplanCapFor is ReplanCapFor as it stood before W1: the strategy
// level counted only when > 0; a session override counted at ≥ 0.
func legacyReplanCapFor(c *DayPlanConfig, session string) int {
	n := ReplanCapDefault
	if c != nil && c.ReplanCap != nil && *c.ReplanCap > 0 {
		n = *c.ReplanCap
	}
	if ov := c.SessionOverride(session); ov != nil && ov.ReplanCap != nil && *ov.ReplanCap >= 0 {
		n = *ov.ReplanCap
	}
	return n
}

// ── THE REPORT ──────────────────────────────────────────────────────────────

// SettingsTruthInput is one stored strategy row as the report reads it.
type SettingsTruthInput struct {
	ID     string
	Config string // the RAW stored JSON — presence is read from it
	Bound  int    // traders bound to this strategy
	// Record — the save-time confirmation marker (which knobs a Studio save
	// held as an explicit 0, and when).
	Record ExplicitZeroRecord
}

// SettingsTruthKnob is one knob of one row: what is stored, what the previous
// binary enforced, what this one enforces.
type SettingsTruthKnob struct {
	Knob    string // the confirmation key (location-agnostic)
	Path    string // the EXACT stored JSON field, e.g. ai_config.risk_control.consecutive_loss_halt
	Note    string // a location fact worth printing (a legacy flat key, an ignored duplicate)
	Stored  string // absent | null | <n> | unreadable
	// Zero — for an explicit 0 only: whether a Studio save confirmed it, and
	// when (ExplicitZeroVerdict). "" when the knob is not an explicit 0.
	Zero    string
	Before  string
	After   string
	Changed bool
}

// SettingsTruthRow is one stored strategy.
type SettingsTruthRow struct {
	ID      string
	Bound   int
	Error   string // the config does not parse — nothing resolved
	Breaker SettingsTruthKnob
	Replan  SettingsTruthKnob
	Changed bool
	// Unconfirmed — changed knobs no W1 save has confirmed. Non-empty ⇒ Refuse.
	Unconfirmed []string
	Refuse      string
}

// SettingsTruthResult is the whole report.
type SettingsTruthResult struct {
	EnvState    string // BREAKER_HALT_N: unset | <n> | invalid
	Rows        []SettingsTruthRow
	ChangedRows int
	RefusedRows int
}

// SettingsTruthReport is PURE: rows in, report out. getenv is the environment
// the report evaluates (os.Getenv at boot; a fixed map in tests).
func SettingsTruthReport(rows []SettingsTruthInput, getenv func(string) string) SettingsTruthResult {
	res := SettingsTruthResult{}
	switch n, st := BreakerHaltEnvState(getenv); st {
	case EnvValid:
		res.EnvState = strconv.Itoa(n)
	default:
		res.EnvState = st
	}
	sorted := append([]SettingsTruthInput(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, in := range sorted {
		row := settingsTruthRow(in, getenv)
		if row.Changed {
			res.ChangedRows++
		}
		if row.Refuse != "" {
			res.RefusedRows++
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

func settingsTruthRow(in SettingsTruthInput, getenv func(string) string) SettingsTruthRow {
	row := SettingsTruthRow{ID: in.ID, Bound: in.Bound}
	var raw map[string]any
	if err := json.Unmarshal([]byte(in.Config), &raw); err != nil {
		row.Error = "config is not a JSON object"
		return row
	}
	// The SAME parse the runtime uses (StrategyConfig.UnmarshalJSON, nested
	// ai_config or the legacy flat keys) — the report resolves what the trader
	// will load, not a second reading of the JSON.
	var cfg StrategyConfig
	if err := json.Unmarshal([]byte(in.Config), &cfg); err != nil {
		row.Error = "config does not parse (" + err.Error() + ")"
		return row
	}

	zeros := map[string]bool{}
	for _, k := range ExplicitZeroKnobs(&cfg) {
		zeros[k] = true
	}
	zeroVerdict := func(knob string) string {
		if !zeros[knob] {
			return ""
		}
		return ExplicitZeroVerdict(knob, in.Record)
	}

	// Breaker.
	shape, path, note := breakerRawShape(raw)
	before := legacyBreakerHaltN(cfg.RiskControl.ConsecutiveLossHalt, getenv)
	after, src := ResolveBreakerHaltEnv(&cfg, getenv)
	row.Breaker = SettingsTruthKnob{
		Knob: KnobBreaker, Path: path, Note: note, Stored: shape, Zero: zeroVerdict(KnobBreaker),
		Before: breakerWord(before), After: breakerWord(after) + OriginLetter(src),
		Changed: before != after,
	}

	// Replan cap — the strategy level and each session.
	var dpRaw map[string]any
	if dp, ok := raw["day_plan"].(map[string]any); ok {
		dpRaw = dp
	}
	rp := SettingsTruthKnob{Knob: KnobReplanStrategy, Path: "day_plan.replan_cap", Stored: storedIntShape(dpRaw, "replan_cap"),
		Zero: zeroVerdict(KnobReplanStrategy)}
	var b, a []string
	for _, s := range settingsTruthSessions {
		old := legacyReplanCapFor(cfg.DayPlan, s)
		n, src := ResolveReplanCap(cfg.DayPlan, s)
		b = append(b, strconv.Itoa(old))
		a = append(a, strconv.Itoa(n)+OriginLetter(src))
		if old != n {
			rp.Changed = true
		}
	}
	rp.Before, rp.After = strings.Join(b, "/"), strings.Join(a, "/")
	row.Replan = rp

	row.Changed = row.Breaker.Changed || row.Replan.Changed
	for _, k := range []SettingsTruthKnob{row.Breaker, row.Replan} {
		if k.Changed && !in.Record.Fields[k.Knob] {
			row.Unconfirmed = append(row.Unconfirmed, k.Knob)
		}
	}
	if len(row.Unconfirmed) > 0 {
		row.Refuse = settingsTruthRefusal(in.ID, row)
	}
	return row
}

// settingsTruthRefusal names — per the CTO ruling (msg 1790173735176) — the
// STRATEGY ID, the EXACT stored field, the stored value, and what that value
// meant before W1 and means after it.
func settingsTruthRefusal(id string, row SettingsTruthRow) string {
	var parts []string
	for _, k := range row.Unconfirmed {
		switch k {
		case KnobBreaker:
			parts = append(parts, fmt.Sprintf(
				"field %s stores %s: before W1 that meant 'inherit' (breaker %s), after W1 it means OFF (breaker %s) — re-save the strategy in the Studio (its breaker shows OFF there) to confirm OFF, or turn the breaker ON to inherit",
				row.Breaker.Path, row.Breaker.Stored, row.Breaker.Before, row.Breaker.After))
		case KnobReplanStrategy:
			parts = append(parts, fmt.Sprintf(
				"field %s stores %s: before W1 that meant 'the shipped default 2' (strategy/NY/ASIA/LONDON %s), after W1 it means 0 re-plans (%s) — re-save the strategy in the Studio to confirm 0, or clear the cap to inherit 2",
				row.Replan.Path, row.Replan.Stored, row.Replan.Before, row.Replan.After))
		}
	}
	return fmt.Sprintf("refused: strategy %s — %s", id, strings.Join(parts, "; and "))
}

// breakerWord renders a breaker N: off for 0.
func breakerWord(n int) string {
	if n <= 0 {
		return "off"
	}
	return strconv.Itoa(n)
}

// breakerRawShape reads the stored breaker the way UnmarshalJSON does: nested
// under ai_config when ai_config is present and non-null, else the legacy flat
// risk_control key. It returns the EXACT field path the value was read from.
func breakerRawShape(raw map[string]any) (shape, path, note string) {
	if ac, present := raw["ai_config"]; present && ac != nil {
		acm, _ := ac.(map[string]any)
		rc, _ := acm["risk_control"].(map[string]any)
		shape, path = storedIntShape(rc, "consecutive_loss_halt"), "ai_config.risk_control.consecutive_loss_halt"
		if flat, ok := raw["risk_control"].(map[string]any); ok {
			if _, has := flat["consecutive_loss_halt"]; has {
				note = "a flat risk_control.consecutive_loss_halt is also stored — ignored, ai_config wins"
			}
		}
		return shape, path, note
	}
	rc, _ := raw["risk_control"].(map[string]any)
	return storedIntShape(rc, "consecutive_loss_halt"), "risk_control.consecutive_loss_halt", "legacy flat key"
}

// storedIntShape says what a JSON object holds at key: absent, null, an
// integer, or unreadable.
func storedIntShape(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return "absent"
	}
	if v == nil {
		return "null"
	}
	if f, isNum := v.(float64); isNum && f == math.Trunc(f) && !math.IsInf(f, 0) {
		return strconv.FormatInt(int64(f), 10)
	}
	return "unreadable"
}

// Lines renders the report: one line per strategy, then the summary.
func (r SettingsTruthResult) Lines() []string {
	out := make([]string, 0, len(r.Rows)+1)
	for _, row := range r.Rows {
		out = append(out, row.Line())
	}
	out = append(out, fmt.Sprintf(
		"🩺 settings truth (W1): %d strateg%s · BREAKER_HALT_N=%s · rows changed=%d · refused=%d",
		len(r.Rows), map[bool]string{true: "y", false: "ies"}[len(r.Rows) == 1], r.EnvState, r.ChangedRows, r.RefusedRows))
	return out
}

// Line renders one strategy row.
func (row SettingsTruthRow) Line() string {
	id := row.ID
	if id == "" {
		id = "(empty id)" // a real row: the research copy holds one
	}
	if row.Error != "" {
		return fmt.Sprintf("🩺 settings truth [%s] bound=%d · n/a (%s)", id, row.Bound, row.Error)
	}
	breakerStored := row.Breaker.Stored
	if row.Breaker.Note != "" {
		breakerStored += " [" + row.Breaker.Path + ", " + row.Breaker.Note + "]"
	}
	if row.Breaker.Zero != "" {
		breakerStored += " (" + row.Breaker.Zero + ")"
	}
	replanStored := row.Replan.Stored
	if row.Replan.Zero != "" {
		replanStored += " (" + row.Replan.Zero + ")"
	}
	verdict := "UNCHANGED"
	switch {
	case row.Refuse != "":
		verdict = "CHANGED — " + row.Refuse
	case row.Changed:
		verdict = "CHANGED — confirmed by a W1 save"
	}
	return fmt.Sprintf(
		"🩺 settings truth [%s] bound=%d · consecutive_loss_halt stored=%s before=%s after=%s · replan_cap stored=%s before=%s after=%s (strategy/NY/ASIA/LONDON) · %s",
		id, row.Bound,
		breakerStored, row.Breaker.Before, row.Breaker.After,
		replanStored, row.Replan.Before, row.Replan.After,
		verdict)
}

// ── THE CONFIRMATION RECORD ─────────────────────────────────────────────────

// explicitZeroKey is the system_config key holding one strategy's record.
func explicitZeroKey(strategyID string) string { return "settings_truth_zero:" + strategyID }

// ExplicitZeroKnobs lists the W1 knobs cfg holds as an explicit 0, sorted.
func ExplicitZeroKnobs(cfg *StrategyConfig) []string {
	var out []string
	if cfg == nil {
		return out
	}
	if v := cfg.RiskControl.ConsecutiveLossHalt; v != nil && *v == 0 {
		out = append(out, KnobBreaker)
	}
	if cfg.DayPlan != nil && cfg.DayPlan.ReplanCap != nil && *cfg.DayPlan.ReplanCap == 0 {
		out = append(out, KnobReplanStrategy)
	}
	sort.Strings(out)
	return out
}

// ExplicitZeroRecord is one strategy's confirmation record as READ: the knobs
// a Studio save held as an explicit 0, and when that save happened.
type ExplicitZeroRecord struct {
	Fields map[string]bool
	// SavedAt is the confirming save's instant (UTC). Zero when the record
	// predates saved_at (the first W1 shape: a bare comma list).
	SavedAt time.Time
}

// explicitZeroMarker is the stored shape of a record (system_config value).
type explicitZeroMarker struct {
	Fields  []string `json:"fields"`
	SavedAt string   `json:"saved_at"`
}

// knownExplicitZeroKnob — a record naming anything else is not one this binary
// wrote, so it confirms nothing (fail-closed).
func knownExplicitZeroKnob(k string) bool { return k == KnobBreaker || k == KnobReplanStrategy }

// parseExplicitZeroMarker reads a stored record. ok=false means the value is
// not a record this binary can vouch for — the caller reads it as UNCONFIRMED.
// Two shapes are accepted: the JSON {"fields":[…],"saved_at":"<RFC3339>"}
// (saved_at REQUIRED), and the first W1 shape, a bare comma list of knob keys
// (no time: SavedAt stays zero and prints n/a).
func parseExplicitZeroMarker(v string) (ExplicitZeroRecord, bool) {
	rec := ExplicitZeroRecord{Fields: map[string]bool{}}
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "{") {
		var m explicitZeroMarker
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return ExplicitZeroRecord{Fields: map[string]bool{}}, false
		}
		at, err := time.Parse(time.RFC3339Nano, m.SavedAt)
		if err != nil {
			return ExplicitZeroRecord{Fields: map[string]bool{}}, false
		}
		for _, k := range m.Fields {
			if !knownExplicitZeroKnob(k) {
				return ExplicitZeroRecord{Fields: map[string]bool{}}, false
			}
			rec.Fields[k] = true
		}
		rec.SavedAt = at.UTC()
		return rec, true
	}
	for _, k := range strings.Split(v, ",") {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if !knownExplicitZeroKnob(k) {
			return ExplicitZeroRecord{Fields: map[string]bool{}}, false
		}
		rec.Fields[k] = true
	}
	return rec, true
}

// encodeExplicitZeroMarker renders the stored shape. fields must be sorted.
func encodeExplicitZeroMarker(fields []string, savedAt time.Time) (string, error) {
	b, err := json.Marshal(explicitZeroMarker{Fields: fields, SavedAt: savedAt.UTC().Format(time.RFC3339)})
	return string(b), err
}

// writeExplicitZeroMarker writes exactly the record cfg's save implies, on the
// caller's transaction: an upsert when the save holds an explicit 0, a delete
// when it holds none (a knob turned back to inherit drops out). Never called
// outside a transaction that also writes the strategy row.
func writeExplicitZeroMarker(tx *gorm.DB, strategyID string, cfg *StrategyConfig, savedAt time.Time) error {
	fields := ExplicitZeroKnobs(cfg)
	if len(fields) == 0 {
		return tx.Exec(`DELETE FROM system_config WHERE key = ?`, explicitZeroKey(strategyID)).Error
	}
	val, err := encodeExplicitZeroMarker(fields, savedAt)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO system_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		explicitZeroKey(strategyID), val).Error
}

// readExplicitZeroMarker reads one strategy's raw record on db (a transaction
// or the store); found=false when there is none.
func readExplicitZeroMarker(db *gorm.DB, strategyID string) (val string, found bool, err error) {
	var vals []string
	if err := db.Raw(`SELECT value FROM system_config WHERE key = ?`, explicitZeroKey(strategyID)).
		Scan(&vals).Error; err != nil {
		return "", false, err
	}
	if len(vals) == 0 {
		return "", false, nil
	}
	return vals[0], true, nil
}

// copyExplicitZeroMarker carries a source strategy's record to a duplicate
// VERBATIM, saved_at included — the copy's 0 was confirmed by the source's
// save — on the caller's transaction. No source record → the copy has none.
func copyExplicitZeroMarker(tx *gorm.DB, fromID, toID string) error {
	val, found, err := readExplicitZeroMarker(tx, fromID)
	if err != nil {
		return err
	}
	if !found {
		return tx.Exec(`DELETE FROM system_config WHERE key = ?`, explicitZeroKey(toID)).Error
	}
	return tx.Exec(`INSERT INTO system_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		explicitZeroKey(toID), val).Error
}

// ExplicitZeroRecordOf reads one strategy's record. None, a read error, or a
// value that does not parse all read as an EMPTY record — nothing confirmed.
func (s *StrategyStore) ExplicitZeroRecordOf(strategyID string) ExplicitZeroRecord {
	if s == nil || s.db == nil {
		return ExplicitZeroRecord{Fields: map[string]bool{}}
	}
	val, found, err := readExplicitZeroMarker(s.db, strategyID)
	if err != nil || !found {
		return ExplicitZeroRecord{Fields: map[string]bool{}}
	}
	rec, _ := parseExplicitZeroMarker(val)
	return rec
}

// ExplicitZerosConfirmed is the record's knob set (empty when none).
func (s *StrategyStore) ExplicitZerosConfirmed(strategyID string) map[string]bool {
	return s.ExplicitZeroRecordOf(strategyID).Fields
}

// CreateWithExplicitZeros is the Studio's create (POST /api/strategies): the
// strategy row AND its confirmation record in ONE transaction.
func (s *StrategyStore) CreateWithExplicitZeros(strategy *Strategy, cfg *StrategyConfig) error {
	if err := guardStrategyModes(strategy.Config); err != nil {
		return err
	}
	savedAt := time.Now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(strategy).Error; err != nil {
			return err
		}
		return writeExplicitZeroMarker(tx, strategy.ID, cfg, savedAt)
	})
}

// UpdateWithExplicitZeros is the Studio's update (PUT /api/strategies/:id):
// the strategy row AND its confirmation record in ONE transaction. The row's
// updated_at and the record's saved_at are the same instant. An update that
// matches no row writes no record.
func (s *StrategyStore) UpdateWithExplicitZeros(strategy *Strategy, cfg *StrategyConfig) error {
	if err := guardStrategyModes(strategy.Config); err != nil {
		return err
	}
	savedAt := time.Now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := updateStrategyRow(tx, strategy, savedAt)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("strategy %s: no row updated", strategy.ID)
		}
		return writeExplicitZeroMarker(tx, strategy.ID, cfg, savedAt)
	})
}

// ExplicitZeroVerdict is what an explicit 0 at knob means to the owner, in the
// words the boot report and the effective endpoint both print (R2):
//
//	OFF — confirmed by Studio save <time CT>   (the replan cap: 0 — …)
//	explicit 0 UNCONFIRMED — re-save in Studio
func ExplicitZeroVerdict(knob string, rec ExplicitZeroRecord) string {
	if !rec.Fields[knob] {
		return "explicit 0 UNCONFIRMED — re-save in Studio"
	}
	word := "OFF"
	if knob == KnobReplanStrategy {
		word = "0"
	}
	return word + " — confirmed by Studio save " + savedAtCT(rec.SavedAt)
}

var (
	settingsTruthLocOnce sync.Once
	settingsTruthLoc     *time.Location
)

// savedAtCT renders a save instant the way kernel.FormatCT renders operator
// times ("2006-01-02 15:04 CT", America/Chicago; store cannot import kernel).
// A record with no time prints n/a; a host without the zone database prints
// UTC and SAYS UTC — never a UTC clock labelled CT.
func savedAtCT(t time.Time) string {
	if t.IsZero() {
		return "n/a — the record predates saved_at"
	}
	settingsTruthLocOnce.Do(func() {
		if loc, err := time.LoadLocation("America/Chicago"); err == nil {
			settingsTruthLoc = loc
		}
	})
	if settingsTruthLoc == nil {
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}
	return t.In(settingsTruthLoc).Format("2006-01-02 15:04 CT")
}

// ── THE BOOT AND LOAD SEATS ─────────────────────────────────────────────────

// SettingsTruthBootReport reads every stored strategy (READ-ONLY) and returns
// the report the boot block prints — before LoadTradersFromStore, so the
// refusal each trader load applies has already been announced.
func (s *StrategyStore) SettingsTruthBootReport(getenv func(string) string) (SettingsTruthResult, error) {
	if s == nil || s.db == nil {
		return SettingsTruthResult{}, fmt.Errorf("store required")
	}
	var all []Strategy
	if err := s.db.Order("id").Find(&all).Error; err != nil {
		return SettingsTruthResult{}, err
	}
	type boundRow struct {
		StrategyID string
		N          int
	}
	var bound []boundRow
	_ = s.db.Raw(`SELECT strategy_id, COUNT(*) AS n FROM traders WHERE strategy_id IS NOT NULL AND strategy_id <> '' GROUP BY strategy_id`).
		Scan(&bound).Error
	boundBy := map[string]int{}
	for _, b := range bound {
		boundBy[b.StrategyID] = b.N
	}
	in := make([]SettingsTruthInput, 0, len(all))
	for _, st := range all {
		in = append(in, SettingsTruthInput{
			ID: st.ID, Config: st.Config, Bound: boundBy[st.ID],
			Record: s.ExplicitZeroRecordOf(st.ID),
		})
	}
	return SettingsTruthReport(in, getenv), nil
}

// SettingsTruthRefusal is the load-time check for ONE strategy: "" when its
// trader may load, else the named reason it may not.
func (s *StrategyStore) SettingsTruthRefusal(st *Strategy, getenv func(string) string) string {
	if st == nil {
		return ""
	}
	row := settingsTruthRow(SettingsTruthInput{
		ID: st.ID, Config: st.Config, Record: s.ExplicitZeroRecordOf(st.ID),
	}, getenv)
	return row.Refuse
}
