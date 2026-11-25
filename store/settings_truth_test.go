package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── W1 SETTINGS TRUTH — the resolvers, the conversion report, the record ─────

func envMap(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// The breaker's whole precedence in one table: saved (incl. 0) → env → 8.
func TestResolveBreakerHaltPrecedence(t *testing.T) {
	withHalt := func(v *int) *StrategyConfig {
		c := &StrategyConfig{}
		c.RiskControl.ConsecutiveLossHalt = v
		return c
	}
	mentorHalt := func(v *int) *StrategyConfig {
		c := withHalt(v)
		c.RiskControl.MentorMode = true
		return c
	}
	cases := []struct {
		name   string
		cfg    *StrategyConfig
		env    map[string]string
		want   int
		origin string
		srcHas string
	}{
		{"nil config, no env → shipped 8", nil, nil, 8, "[I]", SourceShippedDefault},
		{"absent, no env → shipped 8 (inherit is ON)", withHalt(nil), nil, 8, "[I]", SourceShippedDefault},
		// [Oct D4.1 @ 14:40] OctD4.1-R28: mentor-mode default is 4; saved/env still win.
		{"M472 A4 mentor, absent, no env -> 4 [I]", mentorHalt(nil), nil, 4, "[I]", SourceShippedDefault},
		{"M472 A4 mentor + saved 6 -> 6 [O]", mentorHalt(IntPtr(6)), nil, 6, "[O]", SourceSaved},
		{"M472 A4 mentor + env 6 -> 6 [E]", mentorHalt(nil), map[string]string{"BREAKER_HALT_N": "6"}, 6, "[E]", "env BREAKER_HALT_N"},
		{"saved 3", withHalt(IntPtr(3)), nil, 3, "[O]", SourceSaved},
		{"saved 0 is OFF", withHalt(IntPtr(0)), nil, 0, "[O]", SourceSaved},
		{"absent + env 3 → 3 [E]", withHalt(nil), map[string]string{"BREAKER_HALT_N": "3"}, 3, "[E]", "env BREAKER_HALT_N"},
		{"absent + env 0 → off [E]", withHalt(nil), map[string]string{"BREAKER_HALT_N": "0"}, 0, "[E]", "env BREAKER_HALT_N"},
		{"saved 5 beats env 3", withHalt(IntPtr(5)), map[string]string{"BREAKER_HALT_N": "3"}, 5, "[O]", SourceSaved},
		{"saved 0 beats env 3", withHalt(IntPtr(0)), map[string]string{"BREAKER_HALT_N": "3"}, 0, "[O]", SourceSaved},
		{"absent + env invalid → 8, and the source SAYS invalid", withHalt(nil), map[string]string{"BREAKER_HALT_N": "abc"}, 8, "[I]", "invalid"},
		{"absent + env negative → 8, invalid", withHalt(nil), map[string]string{"BREAKER_HALT_N": "-2"}, 8, "[I]", "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, src := ResolveBreakerHaltEnv(c.cfg, envMap(c.env))
			if n != c.want {
				t.Fatalf("N=%d want %d (source %q)", n, c.want, src)
			}
			if got := OriginLetter(src); got != c.origin {
				t.Fatalf("origin %s want %s (source %q)", got, c.origin, src)
			}
			if !strings.Contains(src, c.srcHas) {
				t.Fatalf("source %q does not name %q", src, c.srcHas)
			}
		})
	}
}

// The replan cap: session → strategy → 2, and 0 is honoured at BOTH levels.
func TestResolveReplanCapPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		dp      *DayPlanConfig
		session string
		want    int
		origin  string
	}{
		{"nil block → 2 [I]", nil, "NY", 2, "[I]"},
		{"absent strategy → 2 [I]", &DayPlanConfig{}, "NY", 2, "[I]"},
		{"strategy 4", &DayPlanConfig{ReplanCap: IntPtr(4)}, "NY", 4, "[O]"},
		{"strategy 0 is 0 (was read as 2)", &DayPlanConfig{ReplanCap: IntPtr(0)}, "NY", 0, "[O]"},
		{"strategy 0 at the strategy level", &DayPlanConfig{ReplanCap: IntPtr(0)}, "", 0, "[O]"},
		{"session 3 beats strategy 0", &DayPlanConfig{ReplanCap: IntPtr(0), Sessions: []DayPlanSessionOverride{{Session: "NY", ReplanCap: IntPtr(3)}}}, "NY", 3, "[O]"},
		{"session 0 beats strategy 4", &DayPlanConfig{ReplanCap: IntPtr(4), Sessions: []DayPlanSessionOverride{{Session: "ASIA", ReplanCap: IntPtr(0)}}}, "ASIA", 0, "[O]"},
		{"session case-folds (class 28)", &DayPlanConfig{Sessions: []DayPlanSessionOverride{{Session: "london", ReplanCap: IntPtr(1)}}}, "LONDON", 1, "[O]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, src := ResolveReplanCap(c.dp, c.session)
			if n != c.want || OriginLetter(src) != c.origin {
				t.Fatalf("got %d%s (%q), want %d%s", n, OriginLetter(src), src, c.want, c.origin)
			}
			// ReplanCapFor is the production entry point every gate calls.
			if got := c.dp.ReplanCapFor(c.session); got != c.want {
				t.Fatalf("ReplanCapFor=%d, resolver=%d — two rules", got, c.want)
			}
		})
	}
}

func TestOriginLetterIsOneMapping(t *testing.T) {
	for src, want := range map[string]string{
		SourceSaved:                     "[O]",
		SourceStrategyValue:             "[O]",
		SourceSessionOverride:           "[O]",
		"env BREAKER_HALT_N":            "[E]",
		SourceShippedDefault:            "[I]",
		SourceSchemaDefault:             "[I]",
		SourceShippedDefault + " (env)": "[I]",
		"something new":                 "[?]",
	} {
		if got := OriginLetter(src); got != want {
			t.Errorf("OriginLetter(%q)=%s want %s", src, got, want)
		}
	}
}

// ── THE REPORT ──────────────────────────────────────────────────────────────

type truthFixtureRow struct {
	ID     string          `json:"id"`
	Bound  int             `json:"bound"`
	Record json.RawMessage `json:"record"` // the RAW stored marker; absent = none
	Config json.RawMessage `json:"config"`
}

// rawRecord is the system_config value the fixture row stores: a JSON string
// verbatim, a JSON object as its compact text; found=false when none.
func (r truthFixtureRow) rawRecord(t *testing.T) (string, bool) {
	t.Helper()
	if len(r.Record) == 0 {
		return "", false
	}
	var str string
	if err := json.Unmarshal(r.Record, &str); err == nil {
		return str, true
	}
	return string(r.Record), true
}

func loadTruthFixture(t *testing.T) []truthFixtureRow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "settings_truth_rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Rows []truthFixtureRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Rows
}

// truthInputs builds the pure report's input; each record goes through the
// production parser, exactly as ExplicitZeroRecordOf reads the stored value.
func truthInputs(t *testing.T, rows []truthFixtureRow) []SettingsTruthInput {
	t.Helper()
	in := make([]SettingsTruthInput, 0, len(rows))
	for _, r := range rows {
		rec := ExplicitZeroRecord{Fields: map[string]bool{}}
		if raw, ok := r.rawRecord(t); ok {
			rec, _ = parseExplicitZeroMarker(raw)
		}
		in = append(in, SettingsTruthInput{ID: r.ID, Config: string(r.Config), Bound: r.Bound, Record: rec})
	}
	return in
}

const truthGolden = "testdata/settings_truth_report.golden"

func checkTruthGolden(t *testing.T, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(truthGolden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(truthGolden)
	if err != nil {
		t.Fatalf("read golden (capture with UPDATE_GOLDEN=1): %v", err)
	}
	if got != string(want) {
		t.Fatalf("settings-truth report drifted from the golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The migration report IS a fixture: the nine research-copy shapes plus the
// synthetic zeros, env unset. Pinned byte-for-byte.
func TestSettingsTruthReportGolden(t *testing.T) {
	res := SettingsTruthReport(truthInputs(t, loadTruthFixture(t)), envMap(nil))
	checkTruthGolden(t, strings.Join(res.Lines(), "\n")+"\n")
}

// The expected live result: the nine research-copy rows change NOTHING, in
// every environment the bot could boot with. Sample ids: the nine below.
func TestSettingsTruthResearchRowsChangeNothing(t *testing.T) {
	var research []SettingsTruthInput
	for _, in := range truthInputs(t, loadTruthFixture(t)) {
		if !strings.HasPrefix(in.ID, "syn-") {
			research = append(research, in)
		}
	}
	if len(research) != 9 {
		t.Fatalf("fixture carries %d research rows, want the 9 of the 09-16 copy", len(research))
	}
	for _, env := range []map[string]string{nil, {"BREAKER_HALT_N": "3"}, {"BREAKER_HALT_N": "0"}, {"BREAKER_HALT_N": "junk"}} {
		res := SettingsTruthReport(research, envMap(env))
		if res.ChangedRows != 0 || res.RefusedRows != 0 {
			t.Fatalf("env %v: %d research rows change and %d are refused — the conversion must change none:\n%s",
				env, res.ChangedRows, res.RefusedRows, strings.Join(res.Lines(), "\n"))
		}
	}
}

// The refusal: a stored 0 that no Studio save confirmed refuses its bound
// trader(s); a confirmed one loads; BREAKER_HALT_N=0 makes the breaker-0 rows
// UNCHANGED (off then, off now) so nothing is refused for the breaker.
func TestSettingsTruthRefusesOnlyUnconfirmedChanges(t *testing.T) {
	byID := func(res SettingsTruthResult) map[string]SettingsTruthRow {
		m := map[string]SettingsTruthRow{}
		for _, r := range res.Rows {
			m[r.ID] = r
		}
		return m
	}
	rows := byID(SettingsTruthReport(truthInputs(t, loadTruthFixture(t)), envMap(nil)))
	for id, wantRefuse := range map[string]bool{
		"syn-breaker-0-flat":                   true,
		"syn-breaker-0-nested":                 true,
		"syn-breaker-0-nested-confirmed":       false,
		"syn-breaker-0-legacy-record":          false, // the first W1 record shape still confirms
		"syn-breaker-0-bad-record":             true,  // a record that does not parse confirms NOTHING
		"syn-breaker-null":                     false,
		"syn-replan-0-strategy":                true,
		"syn-replan-0-strategy-confirmed":      false,
		"syn-replan-0-session-only":            false, // a session 0 always meant 0
		"a5b7662e-7bf7-49bb-9f09-7efa48f95ac8": false,
	} {
		r, ok := rows[id]
		if !ok {
			t.Fatalf("row %s missing", id)
		}
		if (r.Refuse != "") != wantRefuse {
			t.Errorf("%s: refuse=%q, want refuse=%v", id, r.Refuse, wantRefuse)
		}
	}
	// CTO ruling (msg 1790173735176): the refusal names the STRATEGY ID, the
	// EXACT FIELD, the stored value, and the before/after meaning.
	for id, wants := range map[string][]string{
		"syn-breaker-0-nested": {"strategy syn-breaker-0-nested", "field ai_config.risk_control.consecutive_loss_halt stores 0",
			"before W1 that meant 'inherit' (breaker 8)", "after W1 it means OFF (breaker off[O])"},
		"syn-breaker-0-flat": {"strategy syn-breaker-0-flat", "field risk_control.consecutive_loss_halt stores 0"},
		"syn-replan-0-strategy": {"strategy syn-replan-0-strategy", "field day_plan.replan_cap stores 0",
			"before W1 that meant 'the shipped default 2' (strategy/NY/ASIA/LONDON 2/3/2/2)", "after W1 it means 0 re-plans (0[O]/3[O]/0[O]/0[O])"},
	} {
		for _, w := range wants {
			if !strings.Contains(rows[id].Refuse, w) {
				t.Errorf("%s refusal lacks %q:\n  %s", id, w, rows[id].Refuse)
			}
		}
	}
	// CTO ruling msg 1790176346377 R2: every explicit 0 says which applies —
	// confirmed by a Studio save (and WHEN, in CT), or UNCONFIRMED.
	for id, want := range map[string]string{
		"syn-breaker-0-nested-confirmed":  "OFF — confirmed by Studio save 2026-09-23 10:04 CT", // 15:04Z = CDT
		"syn-replan-0-strategy-confirmed": "0 — confirmed by Studio save 2026-01-15 14:30 CT",   // 20:30Z = CST
		"syn-breaker-0-legacy-record":     "OFF — confirmed by Studio save n/a — the record predates saved_at",
		"syn-breaker-0-nested":            "explicit 0 UNCONFIRMED — re-save in Studio",
		"syn-breaker-0-flat":              "explicit 0 UNCONFIRMED — re-save in Studio",
		"syn-breaker-0-bad-record":        "explicit 0 UNCONFIRMED — re-save in Studio",
		"syn-replan-0-strategy":           "explicit 0 UNCONFIRMED — re-save in Studio",
	} {
		r := rows[id]
		got := r.Breaker.Zero + r.Replan.Zero // exactly one knob holds the 0 in each row
		if got != want {
			t.Errorf("%s: explicit-0 verdict %q, want %q", id, got, want)
		}
		if !strings.Contains(r.Line(), "("+want+")") {
			t.Errorf("%s: the 🩺 line does not print %q:\n  %s", id, want, r.Line())
		}
	}
	for _, id := range []string{"syn-breaker-null", "syn-replan-0-session-only", "70695b25-01a3-4c38-9917-261260b49550"} {
		if r := rows[id]; r.Breaker.Zero != "" || r.Replan.Zero != "" {
			t.Errorf("%s holds no strategy-level explicit 0 — no verdict, got %q / %q", id, r.Breaker.Zero, r.Replan.Zero)
		}
	}

	withEnvOff := byID(SettingsTruthReport(truthInputs(t, loadTruthFixture(t)), envMap(map[string]string{"BREAKER_HALT_N": "0"})))
	if r := withEnvOff["syn-breaker-0-nested"]; r.Breaker.Changed || r.Refuse != "" {
		t.Errorf("BREAKER_HALT_N=0: a stored 0 was off before and is off now — nothing changed, nothing refused; got %+v", r)
	}
}

// ── THE STORE SEAT — the same rows through the real tables ──────────────────

func seedTruthStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "truth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	g := st.GormDB()
	// store.New seeds its own default strategy rows; the report must speak
	// for exactly the fixture's rows, so start from an empty table.
	if err := g.Exec(`DELETE FROM strategies`).Error; err != nil {
		t.Fatal(err)
	}
	for _, r := range loadTruthFixture(t) {
		if err := g.Exec(`INSERT INTO strategies (id, user_id, name, config) VALUES (?,?,?,?)`,
			r.ID, "u1", "s", string(r.Config)).Error; err != nil {
			t.Fatal(err)
		}
		for b := 0; b < r.Bound; b++ {
			if err := g.Exec(`INSERT INTO traders (id, user_id, name, strategy_id, ai_model_id, exchange_id, initial_balance) VALUES (?,?,?,?,?,?,?)`,
				fmt.Sprintf("t-%s-%d", r.ID, b), "u1", "trader", r.ID, "m1", "e1", 50000.0).Error; err != nil {
				t.Fatal(err)
			}
		}
		if raw, ok := r.rawRecord(t); ok {
			// The record's RAW bytes, as a Studio save (or an older binary, or
			// a damaged restore) left them — the boot seat must parse them.
			if err := g.Exec(`INSERT INTO system_config (key, value) VALUES (?, ?)`, explicitZeroKey(r.ID), raw).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	return st
}

// The boot seat reads the tables and prints EXACTLY the pure report's golden.
func TestSettingsTruthBootReportReadsTheStore(t *testing.T) {
	st := seedTruthStore(t)
	res, err := st.Strategy().SettingsTruthBootReport(envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	checkTruthGolden(t, strings.Join(res.Lines(), "\n")+"\n")
}

// A Studio save REPLACES the record: turning the breaker back to inherit
// drops it, so a later raw 0 is not covered by a stale confirmation. Driven
// through the production writer (UpdateWithExplicitZeros).
func TestUpdateWithExplicitZerosReplacesTheRecordPerSave(t *testing.T) {
	st := seedTruthStore(t)
	s := st.Strategy()
	id := "syn-breaker-0-nested-confirmed"
	if !s.ExplicitZerosConfirmed(id)[KnobBreaker] {
		t.Fatal("seeded confirmation missing")
	}
	save := func(cfg StrategyConfig) {
		t.Helper()
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateWithExplicitZeros(&Strategy{ID: id, UserID: "u1", Name: "s", Config: string(b)}, &cfg); err != nil {
			t.Fatal(err)
		}
	}
	save(StrategyConfig{}) // breaker nil = inherit
	if s.ExplicitZerosConfirmed(id)[KnobBreaker] {
		t.Fatal("a save that holds no explicit 0 must drop the confirmation")
	}
	if _, found, _ := readExplicitZeroMarker(st.GormDB(), id); found {
		t.Fatal("a save with no explicit 0 DELETES the record row, it does not leave an empty one")
	}
	both := StrategyConfig{DayPlan: &DayPlanConfig{ReplanCap: IntPtr(0)}}
	both.RiskControl.ConsecutiveLossHalt = IntPtr(0)
	before := time.Now().UTC().Add(-time.Second)
	save(both)
	rec := s.ExplicitZeroRecordOf(id)
	if !rec.Fields[KnobBreaker] || !rec.Fields[KnobReplanStrategy] {
		t.Fatalf("both explicit zeros must be recorded, got %v", rec.Fields)
	}
	// saved_at is the save's instant, and it is the row's updated_at.
	if rec.SavedAt.Before(before) || rec.SavedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("saved_at %v is not this save's time", rec.SavedAt)
	}
	row, err := s.Get("u1", id)
	if err != nil {
		t.Fatal(err)
	}
	if !row.UpdatedAt.Truncate(time.Second).Equal(rec.SavedAt) {
		t.Fatalf("row updated_at %v and record saved_at %v must be one instant", row.UpdatedAt, rec.SavedAt)
	}
}

// The record parser: the JSON shape needs saved_at; the first W1 shape (a bare
// comma list) still confirms, with no time; anything else confirms NOTHING.
func TestParseExplicitZeroMarker(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		ok     bool
		fields []string
		at     string // RFC3339, "" = zero
	}{
		{"json", `{"fields":["risk_control.consecutive_loss_halt","day_plan.replan_cap"],"saved_at":"2026-09-23T15:04:05Z"}`, true,
			[]string{KnobBreaker, KnobReplanStrategy}, "2026-09-23T15:04:05Z"},
		{"json, no fields", `{"fields":[],"saved_at":"2026-09-23T15:04:05Z"}`, true, nil, "2026-09-23T15:04:05Z"},
		{"legacy list", "day_plan.replan_cap,risk_control.consecutive_loss_halt", true, []string{KnobBreaker, KnobReplanStrategy}, ""},
		{"legacy empty (the old no-zero save)", "", true, nil, ""},
		{"json without saved_at", `{"fields":["risk_control.consecutive_loss_halt"]}`, false, nil, ""},
		{"json with a bad saved_at", `{"fields":["risk_control.consecutive_loss_halt"],"saved_at":"yesterday"}`, false, nil, ""},
		{"truncated json", `{"fields":["risk_control.consecutive_loss_halt"]`, false, nil, ""},
		{"json naming an unknown knob", `{"fields":["risk_control.max_positions"],"saved_at":"2026-09-23T15:04:05Z"}`, false, nil, ""},
		{"legacy naming an unknown knob", "risk_control.consecutive_loss_halt,bogus", false, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, ok := parseExplicitZeroMarker(c.raw)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v", ok, c.ok)
			}
			if len(rec.Fields) != len(c.fields) {
				t.Fatalf("fields %v want %v", rec.Fields, c.fields)
			}
			for _, f := range c.fields {
				if !rec.Fields[f] {
					t.Fatalf("fields %v lack %s", rec.Fields, f)
				}
			}
			if c.at == "" && !rec.SavedAt.IsZero() || c.at != "" && rec.SavedAt.Format(time.RFC3339) != c.at {
				t.Fatalf("saved_at %v want %q", rec.SavedAt, c.at)
			}
		})
	}
}

// ── R1: THE ROW AND ITS RECORD ARE ONE TRANSACTION ───────────────────────────

// failRecordWrites makes every write of a settings_truth_zero:* row fail
// inside SQLite itself (a trigger, no seam in production code), so the
// production save methods run exactly as they do live.
func failRecordWrites(t *testing.T, st *Store) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TRIGGER t_fail_zero_ins BEFORE INSERT ON system_config WHEN NEW.key LIKE 'settings_truth_zero:%' BEGIN SELECT RAISE(ABORT, 'test: record write refused'); END`,
		`CREATE TRIGGER t_fail_zero_upd BEFORE UPDATE ON system_config WHEN NEW.key LIKE 'settings_truth_zero:%' BEGIN SELECT RAISE(ABORT, 'test: record write refused'); END`,
		`CREATE TRIGGER t_fail_zero_del BEFORE DELETE ON system_config WHEN OLD.key LIKE 'settings_truth_zero:%' BEGIN SELECT RAISE(ABORT, 'test: record write refused'); END`,
	} {
		if err := st.GormDB().Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSaveRollsBackTheRowWhenTheRecordFails(t *testing.T) {
	st := seedTruthStore(t)
	s := st.Strategy()
	failRecordWrites(t, st)

	off := StrategyConfig{}
	off.RiskControl.ConsecutiveLossHalt = IntPtr(0)
	offJSON, _ := json.Marshal(off)

	// UPDATE: the row must be byte-identical afterwards.
	id := "syn-breaker-null"
	before, err := s.Get("u1", id)
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateWithExplicitZeros(&Strategy{ID: id, UserID: "u1", Name: "renamed", Config: string(offJSON)}, &off)
	if err == nil || !strings.Contains(err.Error(), "record write refused") {
		t.Fatalf("update must fail with the record's error, got %v", err)
	}
	after, _ := s.Get("u1", id)
	if after.Config != before.Config || after.Name != before.Name || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("the row was saved without its record:\n before %s %q\n after  %s %q", before.Config, before.Name, after.Config, after.Name)
	}

	// UPDATE to inherit (the record's DELETE fails) — the confirmed row keeps
	// both its bytes and its record.
	conf := "syn-breaker-0-nested-confirmed"
	cb, _ := s.Get("u1", conf)
	var inherit StrategyConfig
	if err := s.UpdateWithExplicitZeros(&Strategy{ID: conf, UserID: "u1", Name: "s", Config: "{}"}, &inherit); err == nil {
		t.Fatal("update must fail when the record delete fails")
	}
	if ca, _ := s.Get("u1", conf); ca.Config != cb.Config {
		t.Fatalf("row changed without its record: %s → %s", cb.Config, ca.Config)
	}

	// CREATE: no row at all.
	err = s.CreateWithExplicitZeros(&Strategy{ID: "new-off", UserID: "u1", Name: "n", Config: string(offJSON)}, &off)
	if err == nil {
		t.Fatal("create must fail when the record fails")
	}
	if _, gerr := s.Get("u1", "new-off"); gerr == nil {
		t.Fatal("the created row survived its failed record")
	}

	// DUPLICATE of a confirmed source: no copy at all.
	if err := s.Duplicate("u1", conf, "dup-x", "copy"); err == nil {
		t.Fatal("duplicate must fail when the copied record fails")
	}
	if _, gerr := s.Get("u1", "dup-x"); gerr == nil {
		t.Fatal("the duplicate survived its failed record")
	}
}

// Duplicate copies the bytes, so it carries the source's confirmation —
// verbatim, saved_at included (the source's save confirmed the copy's 0).
func TestDuplicateCarriesTheExplicitZeroRecord(t *testing.T) {
	st := seedTruthStore(t)
	if err := st.Strategy().Duplicate("u1", "syn-breaker-0-nested-confirmed", "dup-1", "copy"); err != nil {
		t.Fatal(err)
	}
	dup, err := st.Strategy().Get("u1", "dup-1")
	if err != nil {
		t.Fatal(err)
	}
	if r := st.Strategy().SettingsTruthRefusal(dup, envMap(nil)); r != "" {
		t.Fatalf("a copy of a confirmed OFF breaker must load, got refusal %q", r)
	}
	src, _, _ := readExplicitZeroMarker(st.GormDB(), "syn-breaker-0-nested-confirmed")
	cp, found, _ := readExplicitZeroMarker(st.GormDB(), "dup-1")
	if !found || cp != src {
		t.Fatalf("the copy's record %q (found=%v) is not the source's %q", cp, found, src)
	}
	if err := st.Strategy().Duplicate("u1", "syn-breaker-0-nested", "dup-2", "copy"); err != nil {
		t.Fatal(err)
	}
	dup2, _ := st.Strategy().Get("u1", "dup-2")
	if r := st.Strategy().SettingsTruthRefusal(dup2, envMap(nil)); r == "" {
		t.Fatal("a copy of an UNCONFIRMED legacy 0 must stay refused")
	}
}
