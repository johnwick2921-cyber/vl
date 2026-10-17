package trader

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"
)

// ── ONE SETUP D8 — the boot line is READ, never literal ──────────────────────

func osBootStore(t *testing.T, dayPlanJSON string) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "boot.db"))
	if err != nil {
		t.Fatal(err)
	}
	g := st.GormDB()
	if err := g.Exec(`INSERT INTO strategies (id, user_id, name, config) VALUES (?,?,?,?)`,
		"99-bound", "u1", "MNQ", `{"day_plan":`+dayPlanJSON+`}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := g.Exec(`INSERT INTO traders (id, user_id, name, strategy_id, ai_model_id, exchange_id, initial_balance) VALUES (?,?,?,?,?,?,?)`,
		"trader-1", "u1", "hoang", "99-bound", "m1", "e1", 50000.0).Error; err != nil {
		t.Fatal(err)
	}
	return st
}

// The counters are keyed by the plan's trade date: at 01:45 CT on 09-11 the
// in-force ASIA plan is 2026-09-10's, and the boot line must ask for THAT key.
func TestOneSetupBootTradeDateIsThePlanChainDate(t *testing.T) {
	asia := time.Date(2026, 9, 11, 1, 45, 0, 0, kernel.CTLocation())
	if got := oneSetupBootTradeDate(asia); got != "2026-09-10" {
		t.Fatalf("01:45 CT 09-11 is the ASIA instance that opened 17:00 09-10 → trade date 2026-09-10, got %s", got)
	}
	ny := time.Date(2026, 9, 11, 9, 0, 0, 0, kernel.CTLocation())
	if got := oneSetupBootTradeDate(ny); got != "2026-09-11" {
		t.Fatalf("09:00 CT 09-11 → 2026-09-11, got %s", got)
	}
}

func TestOneSetupBootLineReadsTheBoundStrategy(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, kernel.CTLocation())
	// Absent knobs → ON [O] and B [O], shipped defaults named as the source.
	st := osBootStore(t, `{"plan_enabled":true}`)
	line := OneSetupBootLine(st, now, []string{"trader-1"}, store.OneSetupBackfillResult{}, store.FollowBackfillResult{})
	for _, want := range []string{"🎯 one setup: ON[O]", "level=best-near-price(min-grade B)[O]", "play=reject", "target=first-distinct-eligible-zone",
		"permission-required=yes", "map=untouched", "today armable=0 declined=0 (level=0 play=0 day=0 not-evaluated=0 waiting=0)",
		"follow-plan=RECORDED-ONLY[T] breaks=0 retests=0 role-reversed=0/0", "bias-flip=recorded-only", "off-switch=one_setup_enabled", "shipped default", "backfill=not-run"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line lacks %q:\n%s", want, line)
		}
	}
	// Saved OFF and grade A → the line reads them from the bound row.
	st2 := osBootStore(t, `{"plan_enabled":true,"one_setup_enabled":false,"one_setup_min_grade":"A"}`)
	line2 := OneSetupBootLine(st2, now, []string{"trader-1"}, store.OneSetupBackfillResult{Ran: true, Recomputed: 3, Unrecomputable: 2, Untouched: 1}, store.FollowBackfillResult{Ran: true, Recomputed: 4})
	for _, want := range []string{"🎯 one setup: OFF[O]", "min-grade A", "saved value", "backfill verdicts recomputed=3 unrecomputable=2 untouched=1 · follow recomputed=4 unrecomputable=0 untouched=0"} {
		if !strings.Contains(line2, want) {
			t.Fatalf("boot line lacks %q:\n%s", want, line2)
		}
	}
	// Counters are READ: record two declines and one armable for today's trade date.
	td := oneSetupBootTradeDate(now)
	for _, c := range []string{store.OneSetupClassLevel, store.OneSetupClassLevel, store.OneSetupClassArmable, store.OneSetupClassObstacleFloor} {
		if _, err := store.IncArmRefusal(st, "trader-1", td, "NY", c); err != nil {
			t.Fatal(err)
		}
	}
	line3 := OneSetupBootLine(st, now, []string{"trader-1"}, store.OneSetupBackfillResult{}, store.FollowBackfillResult{})
	if !strings.Contains(line3, "today armable=1 declined=2 (level=2 play=0 day=0 not-evaluated=0 waiting=0) · obstacle-below-floor=1") {
		t.Fatalf("counters not read onto the line:\n%s", line3)
	}
}

// D9 — the verdict backfill is three-state and never guesses: a row with no
// scenario at its level, no pool read, or no plan is unrecomputable:<which>;
// rows before the boundary are untouched; a row with everything recorded is
// recomputed at its OPEN with W2's permission stamp as the permission leg.
func TestOneSetupBackfillThreeState(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	ts := st.TouchOutcomes()
	sym := at.futuresSymbol()
	era := time.Date(2026, 9, 10, 18, 47, 7, 0, kernel.CTLocation())
	open := era.Add(30 * time.Minute)
	pid := "2026-09-11:NY:" + at.id
	// The plan: S1 reject long at 29490 (arm enabled).
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-11", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: oneSetupFixtureDoc(), CreatedAt: era}); err != nil {
		t.Fatal(err)
	}
	// A pool read before the open: 29490 grade A (best), 29500 grade B.
	if err := st.CandidatePool().SavePool([]store.CandidatePoolRow{
		{TraderID: at.id, Symbol: sym, PlanID: pid, PlanVersion: 1, Session: "NY", ReadAtMs: open.Add(-10 * time.Minute).UnixMilli(), LevelPrice: 29490, LevelKind: "PDL", Label: "PDL", Rank: 1, Seated: true, Score: 0.9, Grade: "A"},
		{TraderID: at.id, Symbol: sym, PlanID: pid, PlanVersion: 1, Session: "NY", ReadAtMs: open.Add(-10 * time.Minute).UnixMilli(), LevelPrice: 29500, LevelKind: "ONH", Label: "ONH", Rank: 2, Seated: true, Score: 0.7, Grade: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	// Bars at the open, stamped with a contract (the roll wave's rule).
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}
	var bars []store.BarHistoryDB
	for i := -6; i <= 1; i++ {
		o := open.Add(time.Duration(i) * time.Minute).UnixMilli()
		bars = append(bars, store.BarHistoryDB{Symbol: sym, TF: "1m", OpenTimeMs: o, O: 29495, H: 29496, L: 29494, C: 29495, V: 1, Contract: "MNQ 09-26", Source: store.BarSourceLive})
	}
	if err := st.BarHistory().InsertBars(bars); err != nil {
		t.Fatal(err)
	}
	mk := func(level float64, openedAt time.Time, permitted *bool) uint {
		r := &store.TouchOutcomeRow{TraderID: at.id, Symbol: sym, LevelPrice: level, LevelKind: "X", CandidateSeated: true, PlanID: pid, PlanVersion: 1, Session: "NY", Ordinal: 1,
			OpenedAtMs: openedAt.UnixMilli(), ClosedAtMs: openedAt.Add(time.Minute).UnixMilli(), Outcome: "hold", Validity: store.ValidityValid, EntrySide: "above", FadePermitted: permitted}
		if err := ts.SaveOutcome(r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	yes := true
	recomputable := mk(29490, open, &yes)             // S1's level, permitted → recomputed, allowed
	noScenario := mk(29450, open, &yes)               // no scenario at 29450 → unrecomputable:no_scenario_at_level
	nullPerm := mk(29490, open.Add(time.Minute), nil) // permission NULL → recomputed, declined not_evaluated
	before := mk(29490, era.Add(-time.Hour), &yes)    // before the boundary → untouched

	res := at.BackfillOneSetupVerdicts(era.UnixMilli(), open.Add(time.Hour))
	if !res.Ran || res.Recomputed != 2 || res.Unrecomputable != 1 || res.Untouched != 1 {
		t.Fatalf("three-state counts wrong: %+v", res)
	}
	read := func(id uint) store.TouchOutcomeRow {
		var r store.TouchOutcomeRow
		if err := st.GormDB().First(&r, id).Error; err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := read(recomputable)
	if r.OneSetupVerdicts == nil || *r.OneSetupVerdicts != "level=ok play=ok permission=ok" || r.OneSetupBackfill == nil || *r.OneSetupBackfill != "recomputed" || r.OneSetupEvaluatedMs == nil || *r.OneSetupEvaluatedMs != open.UnixMilli() {
		t.Fatalf("recomputable row wrong: verdicts=%v backfill=%v at=%v", r.OneSetupVerdicts, r.OneSetupBackfill, r.OneSetupEvaluatedMs)
	}
	if r := read(noScenario); r.OneSetupVerdicts != nil || r.OneSetupBackfill == nil || *r.OneSetupBackfill != "unrecomputable:no_scenario_at_level" {
		t.Fatalf("no-scenario row must be unrecomputable:no_scenario_at_level: %v %v", r.OneSetupVerdicts, r.OneSetupBackfill)
	}
	if r := read(nullPerm); r.OneSetupVerdicts == nil || !strings.Contains(*r.OneSetupVerdicts, "permission=not_evaluated") {
		t.Fatalf("NULL permission must recompute as not_evaluated, never permitted: %v", r.OneSetupVerdicts)
	}
	if r := read(before); r.OneSetupVerdicts != nil || r.OneSetupBackfill != nil {
		t.Fatal("a row before the boundary must be untouched")
	}
	// Idempotent: a second run changes nothing.
	res2 := at.BackfillOneSetupVerdicts(era.UnixMilli(), open.Add(time.Hour))
	if res2.Recomputed != 0 || res2.Unrecomputable != 0 {
		t.Fatalf("second run must find nothing to do: %+v", res2)
	}
}
