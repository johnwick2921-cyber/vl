package trader

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
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

// TestOneSetupBackfillFoldsOverlay (WAVE 1a-plan P2) — the backfill's scenario
// match reads the plan through the ONE fold: an owner overlay that adds an
// armed scenario at a NEW level must be seen when recomputing that level's
// verdict. RED on the base-only reader: the 29450 candidate is
// unrecomputable:no_scenario_at_level. GREEN: recomputed. (The doc carries a
// Reasoning so the folded re-validation passes — a stored doc that cannot
// re-validate falls back to the base by design, which is the correct read.)
func TestOneSetupBackfillFoldsOverlay(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	ts := st.TouchOutcomes()
	sym := at.futuresSymbol()
	era := time.Date(2026, 9, 10, 18, 47, 7, 0, kernel.CTLocation())
	open := era.Add(30 * time.Minute)
	pid := "2026-09-11:NY:" + at.id

	base := kernel.PlanDoc{
		Reasoning:      "wave-1a-p2-onesetup",
		Bias:           kernel.PlanBias{Direction: "long", Conviction: "low"},
		DeathCondition: "n/a",
		Levels: []kernel.PlanLevel{
			{Price: 29490, Label: "PDL", Grade: "A", Instruction: "fade"},
		},
		Scenarios: []kernel.PlanScenario{
			{ID: "S1", Trigger: "reject at 29490", Condition: "reject", Direction: "long", Quality: "A",
				Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 29490, Side: "above"},
				Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 29490, Stop: 29480, Target: 29530}},
		},
	}
	blob, _ := json.Marshal(base)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-11", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: era}); err != nil {
		t.Fatal(err)
	}
	// The owner adds S9, armed at 29450 — a level the base has no scenario at.
	// CreatedAt is stated: the overlay lands BEFORE the episode opens (era),
	// so the backfill's as-of fold sees it (skeptic F8).
	if _, err := st.Plan().AppendOverlay(&store.PlanOverlayDB{
		PlanID: pid, PlanVersion: 1, OverlayID: "owner-add-s9", Origin: "owner",
		CreatedAt: era,
		Patch:     `[{"op":"add","path":"/scenarios/-","value":{"id":"S9","trigger":"reject at 29450","condition":"reject","direction":"long","quality":"A","arm":{"enabled":true,"entry":29450,"stop":29440,"target":29500}}}]`,
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.CandidatePool().SavePool([]store.CandidatePoolRow{
		{TraderID: at.id, Symbol: sym, PlanID: pid, PlanVersion: 1, Session: "NY", ReadAtMs: open.Add(-10 * time.Minute).UnixMilli(), LevelPrice: 29450, LevelKind: "ONL", Label: "ONL", Rank: 1, Seated: true, Score: 0.8, Grade: "B"},
	}); err != nil {
		t.Fatal(err)
	}
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
	yes := true
	r := &store.TouchOutcomeRow{TraderID: at.id, Symbol: sym, LevelPrice: 29450, LevelKind: "X", CandidateSeated: true, PlanID: pid, PlanVersion: 1, Session: "NY", Ordinal: 1,
		OpenedAtMs: open.UnixMilli(), ClosedAtMs: open.Add(time.Minute).UnixMilli(), Outcome: "hold", Validity: store.ValidityValid, EntrySide: "above", FadePermitted: &yes}
	if err := ts.SaveOutcome(r); err != nil {
		t.Fatal(err)
	}

	res := at.BackfillOneSetupVerdicts(era.UnixMilli(), open.Add(time.Hour))
	if res.Recomputed != 1 || res.Unrecomputable != 0 {
		t.Fatalf("the folded S9 must recompute the 29450 candidate: %+v", res)
	}
	var got store.TouchOutcomeRow
	if err := st.GormDB().First(&got, r.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.OneSetupBackfill == nil || *got.OneSetupBackfill != "recomputed" {
		t.Fatalf("the 29450 row must be recomputed through the folded S9, got backfill=%v", got.OneSetupBackfill)
	}
}

// ── Skeptic F8 (2026-09-24): the backfill matches on the SAME predicate the
// live stamper runs — plannerScenariosOnly (D8: one_setup governs PLANNER
// plays only) — and folds only overlays written AT OR BEFORE the episode's
// open (a later overlay never rewrites a closed episode's attribution).

// f8Seed mirrors TestOneSetupBackfillFoldsOverlay's seeding: one active plan
// with a 29490 planner scenario, a pool read, 1m bars, and one episode at
// levelPrice opened at `open`.
func f8Seed(t *testing.T, at *AutoTrader, st *store.Store, pid string, open time.Time, levelPrice float64) {
	t.Helper()
	ts := st.TouchOutcomes()
	sym := at.futuresSymbol()
	if err := st.CandidatePool().SavePool([]store.CandidatePoolRow{
		{TraderID: at.id, Symbol: sym, PlanID: pid, PlanVersion: 1, Session: "NY", ReadAtMs: open.Add(-10 * time.Minute).UnixMilli(), LevelPrice: levelPrice, LevelKind: "ONL", Label: "ONL", Rank: 1, Seated: true, Score: 0.8, Grade: "B"},
	}); err != nil {
		t.Fatal(err)
	}
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
	yes := true
	r := &store.TouchOutcomeRow{TraderID: at.id, Symbol: sym, LevelPrice: levelPrice, LevelKind: "X", CandidateSeated: true, PlanID: pid, PlanVersion: 1, Session: "NY", Ordinal: 1,
		OpenedAtMs: open.UnixMilli(), ClosedAtMs: open.Add(time.Minute).UnixMilli(), Outcome: "hold", Validity: store.ValidityValid, EntrySide: "above", FadePermitted: &yes}
	if err := ts.SaveOutcome(r); err != nil {
		t.Fatal(err)
	}
}

// (F8a) a machine (Picture) overlay armed at the episode's level must NOT
// match — the live predicate excludes machine scenarios, and a one_setup
// verdict must never be stamped against a Picture play. RED = drop
// plannerScenariosOnly → the row recomputes with Scenario=P1.
func TestOneSetupBackfillExcludesMachineScenarios(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	era := time.Date(2026, 9, 10, 18, 47, 7, 0, kernel.CTLocation())
	open := era.Add(30 * time.Minute)
	pid := "2026-09-11:NY:" + at.id
	base := kernel.PlanDoc{
		Reasoning: "f8 machine exclusion", Bias: kernel.PlanBias{Direction: "long", Conviction: "low"},
		DeathCondition: "n/a",
		Levels:         []kernel.PlanLevel{{Price: 29490, Label: "PDL", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{
			{ID: "S1", Trigger: "reject at 29490", Condition: "reject", Direction: "long", Quality: "A",
				Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 29490, Side: "above"},
				Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 29490, Stop: 29480, Target: 29530}},
		},
	}
	blob, _ := json.Marshal(base)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-11", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: era}); err != nil {
		t.Fatal(err)
	}
	psc := kernel.PlanScenario{
		ID: "P1", Trigger: "H1 close beyond 29450", Condition: "reject", Direction: "long", Quality: "B",
		TargetChain: []float64{29500}, Invalid: "window closed",
		Arm:    &kernel.PlanArmSpec{Enabled: true, Entry: 29450, Stop: 29440, Target: 29500},
		Source: kernel.ScenarioSourcePicture,
		Machine: &kernel.PlanMachineSource{Rule: kernel.MachineRulePictureH1CloseBreak, RuleVer: 1, Ref: "opp-f8",
			EligibleFromMs: open.Add(-time.Minute).UnixMilli(), EligibleUntilMs: open.Add(time.Minute).UnixMilli()},
	}
	patch, err := kernel.MachineOverlayPatch(psc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Plan().AppendOverlay(&store.PlanOverlayDB{PlanID: pid, PlanVersion: 1, Origin: kernel.MachineOverlayOriginPicture, CreatedAt: era, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	f8Seed(t, at, st, pid, open, 29450)
	res := at.BackfillOneSetupVerdicts(era.UnixMilli(), open.Add(time.Hour))
	if res.Recomputed != 0 || res.Unrecomputable != 1 {
		t.Fatalf("a machine scenario must not match the one_setup backfill: %+v", res)
	}
	var got store.TouchOutcomeRow
	if err := st.GormDB().First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.OneSetupBackfill == nil || *got.OneSetupBackfill != "unrecomputable:no_scenario_at_level" {
		t.Fatalf("the episode must read no_scenario_at_level (P1 excluded by the live predicate), got %v", got.OneSetupBackfill)
	}
}

// (F8b) an overlay written AFTER the episode opened must not fold into the
// backfill — the recompute serves what the executor saw at the OPEN, not what
// the owner added later. RED = drop the as-of filter → recomputed with S9.
func TestOneSetupBackfillIgnoresOverlaysAfterTheOpen(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	era := time.Date(2026, 9, 10, 18, 47, 7, 0, kernel.CTLocation())
	open := era.Add(30 * time.Minute)
	pid := "2026-09-11:NY:" + at.id
	base := kernel.PlanDoc{
		Reasoning: "f8 time gate", Bias: kernel.PlanBias{Direction: "long", Conviction: "low"},
		DeathCondition: "n/a",
		Levels:         []kernel.PlanLevel{{Price: 29490, Label: "PDL", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{
			{ID: "S1", Trigger: "reject at 29490", Condition: "reject", Direction: "long", Quality: "A",
				Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 29490, Side: "above"},
				Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 29490, Stop: 29480, Target: 29530}},
		},
	}
	blob, _ := json.Marshal(base)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-11", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: era}); err != nil {
		t.Fatal(err)
	}
	// S9 added at 29450 — but a full HOUR after the episode opened.
	if _, err := st.Plan().AppendOverlay(&store.PlanOverlayDB{
		PlanID: pid, PlanVersion: 1, OverlayID: "owner-add-s9-late", Origin: "owner",
		CreatedAt: open.Add(time.Hour),
		Patch:     `[{"op":"add","path":"/scenarios/-","value":{"id":"S9","trigger":"reject at 29450","condition":"reject","direction":"long","quality":"A","arm":{"enabled":true,"entry":29450,"stop":29440,"target":29500}}}]`,
	}); err != nil {
		t.Fatal(err)
	}
	f8Seed(t, at, st, pid, open, 29450)
	res := at.BackfillOneSetupVerdicts(era.UnixMilli(), open.Add(time.Hour))
	if res.Recomputed != 0 || res.Unrecomputable != 1 {
		t.Fatalf("a late overlay must not rewrite the open's attribution: %+v", res)
	}
	var got store.TouchOutcomeRow
	if err := st.GormDB().First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.OneSetupBackfill == nil || *got.OneSetupBackfill != "unrecomputable:no_scenario_at_level" {
		t.Fatalf("the episode must read no_scenario_at_level (the late S9 is invisible at the open), got %v", got.OneSetupBackfill)
	}
}

// RT-6 (release #24, T25) — THE LIVE SHAPE: two traders, each bound to its
// own strategy, both saved one_setup_enabled=false (DB a5b7662e / 7a853ea3).
// The runtime reads each trader's own strategy (armed_executor.go
// GetStrategyConfig → oneSetupVerdictsAt → oneSetupConfig →
// store.ResolveOneSetup), so it runs OFF. The boot line used to print
// "ON[O] … (source: shipped default (no bound strategy readable))" because
// it asked for ONE bound strategy and fell back to the shipped default when
// there were two. It now reads each trader's own, names the trader, and
// never falls back to the shipped default for a trader whose strategy is
// readable. MUTANT: resolve through the old single-strategy fallback → the
// line reads ON / shipped default → RED.
func TestOneSetupBootLineReadsEachTradersOwnStrategy(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 22, 51, 0, kernel.CTLocation())
	st, err := store.New(filepath.Join(t.TempDir(), "boot2.db"))
	if err != nil {
		t.Fatal(err)
	}
	g := st.GormDB()
	for _, s := range [][2]string{{"a5b7662e", `{"day_plan":{"plan_enabled":true,"one_setup_enabled":false}}`}, {"7a853ea3", `{"day_plan":{"plan_enabled":true,"one_setup_enabled":false}}`}} {
		if err := g.Exec(`INSERT INTO strategies (id, user_id, name, config) VALUES (?,?,?,?)`, s[0], "u1", "MNQ "+s[0], s[1]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range [][3]string{{"t-hoang", "hoang", "a5b7662e"}, {"t-12345", "12345", "7a853ea3"}} {
		if err := g.Exec(`INSERT INTO traders (id, user_id, name, strategy_id, ai_model_id, exchange_id, initial_balance) VALUES (?,?,?,?,?,?,?)`,
			tr[0], "u1", tr[1], tr[2], "m1", "e1", 50000.0).Error; err != nil {
			t.Fatal(err)
		}
	}
	line := OneSetupBootLine(st, now, []string{"t-hoang", "t-12345"}, store.OneSetupBackfillResult{}, store.FollowBackfillResult{})
	for _, want := range []string{"🎯 one setup: OFF[O]", "[hoang] OFF (saved value)", "[12345] OFF (saved value)"} {
		if !strings.Contains(line, want) {
			t.Fatalf("two bound strategies, both saved OFF: the line must read %q, got:\n%s", want, line)
		}
	}
	for _, bad := range []string{"one setup: ON", "no bound strategy readable"} {
		if strings.Contains(line, bad) {
			t.Fatalf("the line must not print %q when every trader's strategy is readable:\n%s", bad, line)
		}
	}
	// Mixed: one trader saved ON → MIXED, each named with its own value.
	if err := g.Exec(`UPDATE strategies SET config = ? WHERE id = ?`, `{"day_plan":{"plan_enabled":true,"one_setup_enabled":true}}`, "7a853ea3").Error; err != nil {
		t.Fatal(err)
	}
	line = OneSetupBootLine(st, now, []string{"t-hoang", "t-12345"}, store.OneSetupBackfillResult{}, store.FollowBackfillResult{})
	for _, want := range []string{"🎯 one setup: MIXED[O]", "[hoang] OFF (saved value)", "[12345] ON (saved value)"} {
		if !strings.Contains(line, want) {
			t.Fatalf("mixed switches must read %q, got:\n%s", want, line)
		}
	}
	// No trader resolvable → n/a with the reason, never the shipped default.
	line = OneSetupBootLine(st, now, []string{"t-ghost"}, store.OneSetupBackfillResult{}, store.FollowBackfillResult{})
	if !strings.Contains(line, "🎯 one setup: n/a[O]") || !strings.Contains(line, "[t-ghost] n/a (") {
		t.Fatalf("an unresolvable trader must read n/a with its reason, got:\n%s", line)
	}
}
