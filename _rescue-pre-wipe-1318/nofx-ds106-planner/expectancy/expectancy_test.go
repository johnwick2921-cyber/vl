package expectancy

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"

	"gorm.io/gorm"
	"nofx/store/sqlitedriver"
)

// closeTo is the 1e-6 tolerance the dispatch names (E1).
func closeTo(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %.12f, want %.12f (Δ %.3g)", label, got, want, got-want)
	}
}

func newFixtureDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlitedriver.GormDialector(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&store.TraderPosition{}, &store.PlanDB{}, &store.ArmedOrderDB{}, &store.TradeExcursion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// ct is the zone every era boundary and session day in this system is named in.
func ct(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load CT: %v", err)
	}
	return loc
}

// seedPlan writes one plan doc whose scenarios carry the conditions under test
// and whose levels carry the labels the level-kind canonicalizer must resolve.
func seedPlan(t *testing.T, db *gorm.DB, planID string, version int, session string) {
	t.Helper()
	doc := kernel.PlanDoc{
		Levels: []kernel.PlanLevel{
			{Price: 29123.25, Label: "ONL", Grade: "A"},
			{Price: 29138.00, Label: "SWG-L·5m", Grade: "A"},
			{Price: 29085.00, Label: "OB(bull)·1h (HTF)", Grade: "A"},
		},
		Scenarios: []kernel.PlanScenario{
			{ID: "S1", Condition: "reject", Direction: "short",
				Confirm: &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 29123.25, Side: "below"}},
			{ID: "S2", Condition: "acceptance", Direction: "long",
				Confirm: &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 29138.00, Side: "above"}},
			{ID: "S3", Condition: "sweep_reclaim", Direction: "long",
				Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 29085.00, Side: "above"}},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := db.Create(&store.PlanDB{PlanID: planID, Version: version, Session: session, Doc: string(b)}).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

type seedPos struct {
	scenario string
	pnl      *float64
	entryMs  int64
	source   string
	planID   string
	closeRsn string
}

func f(v float64) *float64 { return &v }

func seedPositions(t *testing.T, db *gorm.DB, planID string, version int, session string, rows []seedPos) []int64 {
	t.Helper()
	var ids []int64
	for i, r := range rows {
		pid := r.planID
		if pid == "" {
			pid = planID
		}
		src := r.source
		if src == "" {
			src = "system"
		}
		p := &store.TraderPosition{
			TraderID: "t1", Symbol: "MNQ", Side: "SHORT", Quantity: 1,
			EntryPrice: 29100, ExitPrice: 29090,
			EntryTime: r.entryMs, ExitTime: r.entryMs + 600000,
			Status: "CLOSED", CloseReason: r.closeRsn, Source: src,
			PnlCorrected: r.pnl, RealizedPnL: -99999, // raw must never be read (A22)
			PlanID: pid, PlanVersion: version, PlanSession: session,
			CitedScenarioID: r.scenario, PlanMatched: true, PlanBand: "armed_fill",
			CreatedAt: r.entryMs, UpdatedAt: r.entryMs,
		}
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("seed position %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}
	return ids
}

// msAt is a CT wall-clock instant in epoch ms.
func msAt(t *testing.T, y int, mo time.Month, d, h, mi int) int64 {
	t.Helper()
	return time.Date(y, mo, d, h, mi, 0, 0, ct(t)).UnixMilli()
}

// ─────────────────────────────────────────────────────────────────────
// E1 — the fixture table: 40 positions, 3 conditions, every exclusion class.
// ─────────────────────────────────────────────────────────────────────

func TestE1FixtureCellsMatchHandComputedStats(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")

	base := msAt(t, 2026, time.September, 1, 9, 0) // post-era, post-0B
	var rows []seedPos

	// reject: 12 wins @ +10, 8 losses @ -10  → n=20 mean=+2
	for i := 0; i < 12; i++ {
		rows = append(rows, seedPos{scenario: "S1", pnl: f(10), entryMs: base + int64(i)*60000})
	}
	for i := 0; i < 8; i++ {
		rows = append(rows, seedPos{scenario: "S1", pnl: f(-10), entryMs: base + int64(100+i)*60000})
	}
	// acceptance: 5 @ +20, 5 @ -20 → n=10 mean=0
	for i := 0; i < 5; i++ {
		rows = append(rows, seedPos{scenario: "S2", pnl: f(20), entryMs: base + int64(200+i)*60000})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedPos{scenario: "S2", pnl: f(-20), entryMs: base + int64(300+i)*60000})
	}
	// sweep_reclaim: 3 @ +5, 1 @ -15 → n=4 mean=0
	for i := 0; i < 3; i++ {
		rows = append(rows, seedPos{scenario: "S3", pnl: f(5), entryMs: base + int64(400+i)*60000})
	}
	rows = append(rows, seedPos{scenario: "S3", pnl: f(-15), entryMs: base + 500*60000})

	// EXCLUSIONS — 2 NULL pnl_corrected, 1 test-seam, 1 sentinel, 2 crypto-era.
	rows = append(rows,
		seedPos{scenario: "S1", pnl: nil, entryMs: base + 600*60000},
		seedPos{scenario: "S1", pnl: nil, entryMs: base + 601*60000},
		seedPos{scenario: "S1", pnl: f(1000), entryMs: base + 602*60000, source: TestSeamSource, closeRsn: TestSeamSource},
		seedPos{scenario: "S1", pnl: f(1000), entryMs: base + 603*60000, planID: store.PlanUnresolvable},
	)
	cryptoMs := store.DayPlanEraStart.Add(-48 * time.Hour).UnixMilli()
	rows = append(rows,
		seedPos{scenario: "S1", pnl: f(1000), entryMs: cryptoMs},
		seedPos{scenario: "S1", pnl: f(1000), entryMs: cryptoMs + 60000},
	)
	if len(rows) != 40 {
		t.Fatalf("fixture must hold 40 positions, has %d", len(rows))
	}
	seedPositions(t, db, "2026-09-01:P", 1, "NY", rows)

	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t))
	tbl, err := LoadAndBuildAt(db, now)
	if err != nil {
		t.Fatalf("LoadAndBuildAt: %v", err)
	}

	// EXCLUSION COUNTS — each named and counted, never silently dropped.
	if got, want := tbl.Excluded.UnresolvedPnL, 2; got != want {
		t.Errorf("Excluded.UnresolvedPnL = %d, want %d", got, want)
	}
	if got, want := tbl.Excluded.TestSeam, 1; got != want {
		t.Errorf("Excluded.TestSeam = %d, want %d", got, want)
	}
	if got, want := tbl.Excluded.Unresolvable, 1; got != want {
		t.Errorf("Excluded.Unresolvable = %d, want %d", got, want)
	}
	// CRYPTO-ERA rows are ABSENT — not excluded-and-counted, never present at all.
	if tbl.Excluded.CryptoEra != 0 {
		t.Errorf("crypto-era rows must be ABSENT from the model, got Excluded.CryptoEra = %d", tbl.Excluded.CryptoEra)
	}

	rej := tbl.ByCondition("reject")
	if rej == nil {
		t.Fatalf("no reject roll-up cell")
	}
	if rej.N != 20 {
		t.Fatalf("reject N = %d, want 20", rej.N)
	}
	if rej.Wins != 12 || rej.Losses != 8 || rej.Flats != 0 {
		t.Errorf("reject W/L/F = %d/%d/%d, want 12/8/0", rej.Wins, rej.Losses, rej.Flats)
	}
	closeTo(t, "reject.SumPnL", rej.SumPnL, 40.0)
	closeTo(t, "reject.Mean", rej.Mean, 2.0)
	closeTo(t, "reject.SD", rej.SD, 10.052493799000692)
	closeTo(t, "reject.TStat", rej.TStat, 0.8897565210026094)
	closeTo(t, "reject.WilsonLo", rej.WilsonLo, 0.3865779423152061)
	closeTo(t, "reject.WilsonHi", rej.WilsonHi, 0.7811960325858074)

	acc := tbl.ByCondition("acceptance")
	if acc == nil || acc.N != 10 {
		t.Fatalf("acceptance cell missing or wrong N: %+v", acc)
	}
	closeTo(t, "acceptance.Mean", acc.Mean, 0.0)
	closeTo(t, "acceptance.SD", acc.SD, 21.081851067789195)
	closeTo(t, "acceptance.WilsonLo", acc.WilsonLo, 0.23658959361548726)
	closeTo(t, "acceptance.WilsonHi", acc.WilsonHi, 0.7634104063845127)

	sw := tbl.ByCondition("sweep_reclaim")
	if sw == nil || sw.N != 4 {
		t.Fatalf("sweep_reclaim cell missing or wrong N: %+v", sw)
	}
	closeTo(t, "sweep.WilsonLo", sw.WilsonLo, 0.3006360524426366)
	closeTo(t, "sweep.WilsonHi", sw.WilsonHi, 0.9544139373553637)

	// Level kind resolved from the plan level the scenario's confirm references.
	if got, want := rej.LevelKind, string(kernel.KindONL); rej.Key.LevelKind != "" && got != want {
		t.Errorf("reject roll-up LevelKind = %q (roll-up must not claim a kind)", got)
	}

	// MAE/MFE and hit shares are ABSENT (1A has no rows) — never a plausible zero.
	if rej.MedianMAE != nil || rej.MedianMFE != nil {
		t.Errorf("MAE/MFE must be absent with no excursion rows, got %v/%v", rej.MedianMAE, rej.MedianMFE)
	}
	if rej.StopHitShare != nil || rej.TargetHitShare != nil {
		t.Errorf("hit shares must be absent with no excursion rows, got %v/%v", rej.StopHitShare, rej.TargetHitShare)
	}

	// FRESHNESS — as-of is the last close in the model, not the wall clock.
	if tbl.AsOfMs == 0 {
		t.Errorf("AsOfMs must be the last closed position's exit time, got 0")
	}
	if tbl.AsOfMs >= now.UnixMilli() {
		t.Errorf("AsOfMs %d must precede now %d", tbl.AsOfMs, now.UnixMilli())
	}
}

// ─────────────────────────────────────────────────────────────────────
// E2 — the floor: n=29 descriptive, n=30 judged.
// ─────────────────────────────────────────────────────────────────────

func TestE2FloorAt29And30(t *testing.T) {
	mk := func(t *testing.T, wins, losses int, w, l float64) *Cell {
		t.Helper()
		db := newFixtureDB(t)
		seedPlan(t, db, "2026-09-01:P", 1, "NY")
		base := msAt(t, 2026, time.September, 1, 9, 0)
		var rows []seedPos
		for i := 0; i < wins; i++ {
			rows = append(rows, seedPos{scenario: "S1", pnl: f(w), entryMs: base + int64(i)*60000})
		}
		for i := 0; i < losses; i++ {
			rows = append(rows, seedPos{scenario: "S1", pnl: f(l), entryMs: base + int64(1000+i)*60000})
		}
		seedPositions(t, db, "2026-09-01:P", 1, "NY", rows)
		tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return tbl.ByCondition("reject")
	}

	if MinN != 30 {
		t.Fatalf("the pre-registered floor is 30, MinN = %d", MinN)
	}

	c29 := mk(t, 14, 15, 10, -10)
	if c29.N != 29 {
		t.Fatalf("N = %d, want 29", c29.N)
	}
	if !c29.Descriptive {
		t.Errorf("n=29 must be DESCRIPTIVE ONLY")
	}
	if c29.Status != StatusNotEnoughData {
		t.Errorf("n=29 Status = %q, want %q", c29.Status, StatusNotEnoughData)
	}

	c30fail := mk(t, 15, 15, 10, -10)
	if c30fail.N != 30 || c30fail.Descriptive {
		t.Fatalf("n=30 must be judged, got N=%d descriptive=%v", c30fail.N, c30fail.Descriptive)
	}
	if c30fail.Status != StatusFails {
		t.Errorf("n=30 mean 0 Status = %q, want %q", c30fail.Status, StatusFails)
	}
	closeTo(t, "fail.MeanLo", c30fail.MeanLo, -3.639628628270216)

	c30pass := mk(t, 20, 10, 30, -10)
	if c30pass.Status != StatusPasses {
		t.Errorf("n=30 mean +16.67 CI>0 Status = %q, want %q", c30pass.Status, StatusPasses)
	}
	closeTo(t, "pass.Mean", c30pass.Mean, 16.666666666666668)
	closeTo(t, "pass.MeanLo", c30pass.MeanLo, 9.803717109198502)
}

// ─────────────────────────────────────────────────────────────────────
// E3 — era split by TIMESTAMP, not session-day.
// ─────────────────────────────────────────────────────────────────────

func TestE3EraSplitByTimestampNotSessionDay(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-02:P", 1, "NY")
	pre := msAt(t, 2026, time.September, 2, 7, 41)  // before the 0B boot
	post := msAt(t, 2026, time.September, 2, 9, 41) // after it — SAME session-day
	seedPositions(t, db, "2026-09-02:P", 1, "NY", []seedPos{
		{scenario: "S1", pnl: f(10), entryMs: pre},
		{scenario: "S1", pnl: f(20), entryMs: post},
	})

	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var sawPre, sawPost bool
	for _, c := range tbl.Cells {
		switch c.Key.Era {
		case EraPre0B:
			sawPre = true
			closeTo(t, "pre-0B SumPnL", c.SumPnL, 10)
		case EraPost0B:
			sawPost = true
			closeTo(t, "post-0B SumPnL", c.SumPnL, 20)
		}
	}
	if !sawPre || !sawPost {
		t.Fatalf("both eras must appear from one session-day: pre=%v post=%v", sawPre, sawPost)
	}
}

// ─────────────────────────────────────────────────────────────────────
// E4 — the E8 counterfactual side-table never merges with realized.
// ─────────────────────────────────────────────────────────────────────

func TestE4CounterfactualSideTableFlagsShortRows(t *testing.T) {
	db := newFixtureDB(t)
	if err := db.Exec(`CREATE TABLE ab_confirm_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, trader_id TEXT, plan_id TEXT, version INTEGER,
		session TEXT, scenario TEXT, rule TEXT, condition TEXT, outcome TEXT,
		entry_px REAL DEFAULT 0, net_pnl REAL, mfe REAL, mae REAL,
		is_counterfactual INTEGER DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create ab_confirm_log: %v", err)
	}
	if err := db.Exec(`INSERT INTO ab_confirm_log (session,scenario,rule,condition,outcome,entry_px,net_pnl) VALUES
		('NY','S1','1x5m_close','reject','win',29400,10),
		('NY','S2','touch','sweep_reclaim','loss',29400,-5)`).Error; err != nil {
		t.Fatalf("seed ab: %v", err)
	}
	// The short side is the one the E8 sign bug corrupts.
	if err := db.Exec(`UPDATE ab_confirm_log SET scenario='S1'`).Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(tbl.Counterfactual) == 0 {
		t.Fatalf("E8 side-table must be populated")
	}
	for _, c := range tbl.Counterfactual {
		if !c.Counterfactual {
			t.Errorf("E8 cell %q not marked counterfactual", c.Key.Condition)
		}
	}
	// Realized cells must not absorb counterfactual rows.
	for _, c := range tbl.Cells {
		if c.N != 0 {
			t.Errorf("no realized rows were seeded, yet cell %+v has N=%d", c.Key, c.N)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// E5 — every figure is reproducible from the ids the row carries.
// ─────────────────────────────────────────────────────────────────────

func TestE5FigureIsReproducibleFromStoredRowIDs(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")
	base := msAt(t, 2026, time.September, 1, 9, 0)
	want := []float64{10, -10, 25, -5, 40}
	var rows []seedPos
	for i, v := range want {
		rows = append(rows, seedPos{scenario: "S1", pnl: f(v), entryMs: base + int64(i)*60000})
	}
	seedPositions(t, db, "2026-09-01:P", 1, "NY", rows)

	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cell := tbl.ByCondition("reject")
	if len(cell.RowIDs) != len(want) {
		t.Fatalf("cell carries %d ids, want %d", len(cell.RowIDs), len(want))
	}
	// Recompute the mean from the ids ALONE — the sample-id law made executable.
	var sum float64
	for _, id := range cell.RowIDs {
		var p store.TraderPosition
		if err := db.First(&p, id).Error; err != nil {
			t.Fatalf("row id %d not found: %v", id, err)
		}
		if p.PnlCorrected == nil {
			t.Fatalf("row id %d has NULL pnl_corrected and must not be in the cell", id)
		}
		sum += *p.PnlCorrected
	}
	closeTo(t, "mean recomputed from ids", sum/float64(len(cell.RowIDs)), cell.Mean)
}

// ─────────────────────────────────────────────────────────────────────
// E6 — clock seam (class 60): the caller owns time.Now.
// ─────────────────────────────────────────────────────────────────────

func TestE6EraBoundaryIsTheRecordedBootInstant(t *testing.T) {
	// 0B booted 2026-09-02 07:49:06 CT (commit 617faae4: "lane 2 booted at
	// 07:49:06 CT mid-wave"). Asserted as a resolved instant, never a literal
	// epoch, in the zone the era is named in.
	got := Era0BStart.In(ct(t))
	if got.Year() != 2026 || got.Month() != time.September || got.Day() != 2 ||
		got.Hour() != 7 || got.Minute() != 49 || got.Second() != 6 {
		t.Fatalf("Era0BStart = %s, want 2026-09-02 07:49:06 CT", got.Format(time.RFC3339))
	}
}

func TestE6BuildAtUsesTheCallersClock(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")
	base := msAt(t, 2026, time.September, 1, 9, 0)
	seedPositions(t, db, "2026-09-01:P", 1, "NY", []seedPos{{scenario: "S1", pnl: f(10), entryMs: base}})

	early := time.Date(2026, time.September, 1, 9, 30, 0, 0, ct(t))
	late := time.Date(2027, time.January, 1, 0, 0, 0, 0, ct(t))
	a, err := LoadAndBuildAt(db, early)
	if err != nil {
		t.Fatalf("build early: %v", err)
	}
	b, err := LoadAndBuildAt(db, late)
	if err != nil {
		t.Fatalf("build late: %v", err)
	}
	// The table's content is clock-independent; only the caller's clock differs.
	if a.AsOfMs != b.AsOfMs {
		t.Errorf("AsOfMs must come from the data, not the clock: %d vs %d", a.AsOfMs, b.AsOfMs)
	}
	if a.BuiltAtMs != early.UnixMilli() || b.BuiltAtMs != late.UnixMilli() {
		t.Errorf("BuiltAtMs must be the caller's clock: %d/%d want %d/%d",
			a.BuiltAtMs, b.BuiltAtMs, early.UnixMilli(), late.UnixMilli())
	}
}

// ─────────────────────────────────────────────────────────────────────
// Level-kind canonicalizer — one canonicalizer, the kernel enum its source.
// ─────────────────────────────────────────────────────────────────────

func TestLevelKindCanonicalizerResolvesLabels(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"ONL", string(kernel.KindONL)},
		{"SWG-L·5m", string(kernel.KindSWGL)},
		{"OB(bull)·1h (HTF)", string(kernel.KindOB)},
		{"nPOC·Tue", string(kernel.KindNPOC)},
		{"PDH", string(kernel.KindPDH)},
		{"", ""},
		{"not-a-level", ""},
	}
	for _, c := range cases {
		if got := LevelKindFromLabel(c.label); got != c.want {
			t.Errorf("LevelKindFromLabel(%q) = %q, want %q", c.label, got, c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// Projections and the era filter — a roll-up must never be a mean-of-means,
// and an era filter must re-aggregate rather than slice a computed table.
// ─────────────────────────────────────────────────────────────────────

func TestKindAndPathRollUpsArePooledNotMeansOfMeans(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")
	base := msAt(t, 2026, time.September, 1, 9, 0)
	// Two conditions, both resolving to level kind ONL is impossible (S1→ONL,
	// S2→SWG-L), so a kind roll-up must pool ACROSS conditions correctly.
	rows := []seedPos{
		{scenario: "S1", pnl: f(10), entryMs: base},
		{scenario: "S1", pnl: f(30), entryMs: base + 60000},
		{scenario: "S2", pnl: f(-20), entryMs: base + 120000},
	}
	seedPositions(t, db, "2026-09-01:P", 1, "NY", rows)

	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var onl *Cell
	for i := range tbl.Kinds {
		if tbl.Kinds[i].LevelKind == string(kernel.KindONL) {
			onl = &tbl.Kinds[i]
		}
	}
	if onl == nil {
		t.Fatalf("no ONL kind roll-up; kinds = %+v", tbl.Kinds)
	}
	if onl.N != 2 {
		t.Fatalf("ONL N = %d, want 2", onl.N)
	}
	closeTo(t, "ONL mean", onl.Mean, 20.0)
	// SD over {10,30} is 14.142... — recoverable only from the raw values, which
	// is the point: a roll-up built by averaging cell means could not produce it.
	closeTo(t, "ONL sd", onl.SD, 14.142135623730951)

	if len(tbl.Paths) == 0 {
		t.Fatalf("path roll-up missing")
	}
	for _, p := range tbl.Paths {
		if p.Path != PathDecision {
			t.Errorf("unexpected path %q (no armed_orders rows were seeded)", p.Path)
		}
	}
}

func TestFilterEraReAggregatesInsteadOfSlicing(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-02:P", 1, "NY")
	pre := msAt(t, 2026, time.September, 2, 7, 41)
	post := msAt(t, 2026, time.September, 2, 9, 41)
	seedPositions(t, db, "2026-09-02:P", 1, "NY", []seedPos{
		{scenario: "S1", pnl: f(10), entryMs: pre},
		{scenario: "S1", pnl: f(30), entryMs: pre + 60000},
		{scenario: "S1", pnl: f(-100), entryMs: post},
	})

	full, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if c := full.ByCondition("reject"); c == nil || c.N != 3 {
		t.Fatalf("unfiltered reject N = %v, want 3", c)
	}

	onlyPre := FilterEra(full, EraPre0B)
	c := onlyPre.ByCondition("reject")
	if c == nil || c.N != 2 {
		t.Fatalf("pre-0B reject N = %v, want 2", c)
	}
	// The CONDITION roll-up must be recomputed for the era, not inherited.
	closeTo(t, "pre-0B mean", c.Mean, 20.0)
	closeTo(t, "pre-0B sd", c.SD, 14.142135623730951)
	if len(c.RowIDs) != 2 {
		t.Errorf("filtered cell must carry only its own ids, got %v", c.RowIDs)
	}

	onlyPost := FilterEra(full, EraPost0B)
	if c := onlyPost.ByCondition("reject"); c == nil || c.N != 1 || c.SumPnL != -100 {
		t.Fatalf("post-0B reject = %+v, want N=1 sum=-100", c)
	}
	// An unknown era yields an EMPTY table, never the unfiltered one — a filter
	// that silently does nothing is how a scoped claim becomes a global one.
	if e := FilterEra(full, "no-such-era"); len(e.Cells) != 0 || len(e.Conditions) != 0 {
		t.Errorf("unknown era must filter to empty, got %d cells", len(e.Cells))
	}
}

func TestBootLineIsReadNotLiteral(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")
	base := msAt(t, 2026, time.September, 1, 9, 0)
	seedPositions(t, db, "2026-09-01:P", 1, "NY", []seedPos{
		{scenario: "S1", pnl: f(10), entryMs: base},
		{scenario: "S1", pnl: nil, entryMs: base + 60000},
		{scenario: "S1", pnl: f(5), entryMs: base + 120000, source: TestSeamSource},
	})
	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := tbl.BootLine()
	want := "📊 expectancy: cells=1 with_n>=30=0 judged_rollups=0 unresolved=1 excluded_test=1"
	if got != want {
		t.Errorf("BootLine()\n got %q\nwant %q", got, want)
	}
}

// The live table forced this field. On 2026-09-03 the boot line read
// "cells=41 with_n>=30=0" while the CONDITION roll-up for `reject` stood at
// n=31 and was judged FAILS. Both numbers were true and together they read as
// "nothing is judgeable yet", which was false. cells= counts the full
// five-dimensional table, where no single cell reaches the floor; the verdicts
// are made on the roll-ups, so the roll-ups get their own count.
func TestBootLineCountsJudgedRollUpsSeparatelyFromCells(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-09-01:P", 1, "NY")
	base := msAt(t, 2026, time.September, 1, 9, 0)
	var rows []seedPos
	// 30 rows split across two sessions: no single five-dimensional cell
	// reaches the floor, but the condition roll-up does.
	for i := 0; i < 30; i++ {
		sess := "NY"
		if i%2 == 0 {
			sess = "LONDON"
		}
		p := &store.TraderPosition{
			TraderID: "t1", Symbol: "MNQ", Side: "SHORT", Quantity: 1,
			EntryPrice: 29100, ExitPrice: 29090,
			EntryTime: base + int64(i)*60000, ExitTime: base + int64(i)*60000 + 1000,
			Status: "CLOSED", Source: "system", PnlCorrected: f(10), RealizedPnL: -1,
			PlanID: "2026-09-01:P", PlanVersion: 1, PlanSession: sess,
			CitedScenarioID: "S1", PlanMatched: true, PlanBand: "armed_fill",
		}
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	_ = rows
	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, c := range tbl.Cells {
		if c.N >= MinN {
			t.Fatalf("no five-dimensional cell should reach the floor here: %+v", c.Key)
		}
	}
	if c := tbl.ByCondition("reject"); c == nil || c.N != 30 || c.Descriptive {
		t.Fatalf("condition roll-up should be judged: %+v", c)
	}
	// THREE roll-ups clear the floor on this fixture, not one: condition,
	// level-kind and path each pool all 30 rows. Only the session roll-up
	// splits (15 LONDON / 15 NY) and stays descriptive. Asserting the exact
	// number pins which sets are counted — a laxer ">0" would still pass if the
	// count silently dropped a roll-up set.
	if got := tbl.BootLine(); !strings.Contains(got, "judged_rollups=3") {
		t.Errorf("boot line must count every judged roll-up, got %q", got)
	}
	for _, c := range tbl.Sessions {
		if !c.Descriptive {
			t.Errorf("session %q split 15/15 must stay descriptive, got n=%d", c.Session, c.N)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// E8 integrity — the live table forced this test.
//
// Run against the live store on 2026-09-03 the side-table produced cell means
// of −29,926 on an instrument trading at 29,900: ab_confirm_log.net_pnl is not
// a P&L for those rows, it is approximately −(price × multiplier), i.e. the
// exit was treated as zero. 40 of 188 rows are that shape and another 92 are a
// bare 0 beside a resolved outcome. A mean over either is a fabricated number
// wearing a currency symbol, so the model refuses to compute one.
// ─────────────────────────────────────────────────────────────────────

func TestE8RefusesToMeanAnUncomputableColumn(t *testing.T) {
	db := newFixtureDB(t)
	if err := db.Exec(`CREATE TABLE ab_confirm_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, trader_id TEXT, plan_id TEXT, version INTEGER,
		session TEXT, scenario TEXT, rule TEXT, condition TEXT, outcome TEXT,
		entry_px REAL DEFAULT 0, net_pnl REAL, is_counterfactual INTEGER DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create ab_confirm_log: %v", err)
	}
	// One usable row, one price-scale row, one uncomputed zero — same cell.
	if err := db.Exec(`INSERT INTO ab_confirm_log (session,scenario,rule,condition,outcome,entry_px,net_pnl) VALUES
		('NY','S1','1x5m_close','reject','win',29400,40),
		('NY','S1','1x5m_close','reject','target',29413,-117664.24),
		('NY','S1','1x5m_close','reject','stop',29200,0)`).Error; err != nil {
		t.Fatalf("seed ab: %v", err)
	}

	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(tbl.Counterfactual) != 1 {
		t.Fatalf("want 1 E8 cell, got %d", len(tbl.Counterfactual))
	}
	c := tbl.Counterfactual[0]
	if c.N != 3 {
		t.Errorf("N = %d, want 3 (the cell still reports every row it saw)", c.N)
	}
	if c.UsableN != 1 {
		t.Errorf("UsableN = %d, want 1", c.UsableN)
	}
	if c.ExcludedPriceScale != 1 {
		t.Errorf("ExcludedPriceScale = %d, want 1", c.ExcludedPriceScale)
	}
	if c.ExcludedZeroPnL != 1 {
		t.Errorf("ExcludedZeroPnL = %d, want 1", c.ExcludedZeroPnL)
	}
	if c.Mean == nil {
		t.Fatalf("one usable row must still yield a mean")
	}
	closeTo(t, "E8 mean over usable rows only", *c.Mean, 40)
}

func TestE8MeanIsAbsentWhenNoRowIsUsable(t *testing.T) {
	db := newFixtureDB(t)
	if err := db.Exec(`CREATE TABLE ab_confirm_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, trader_id TEXT, plan_id TEXT, version INTEGER,
		session TEXT, scenario TEXT, rule TEXT, condition TEXT, outcome TEXT,
		entry_px REAL DEFAULT 0, net_pnl REAL, is_counterfactual INTEGER DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Exec(`INSERT INTO ab_confirm_log (session,scenario,rule,condition,outcome,entry_px,net_pnl) VALUES
		('NY','S1','1x5m_close','reject','target',29413,-117664.24),
		('NY','S1','1x5m_close','reject','stop',29200,0)`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.September, 3, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := tbl.Counterfactual[0]
	// ABSENT, not 0. A zero here would read as "the shadow rule broke even".
	if c.Mean != nil {
		t.Errorf("Mean must be absent when no row is usable, got %v", *c.Mean)
	}
	if c.SumPnL != nil {
		t.Errorf("SumPnL must be absent when no row is usable, got %v", *c.SumPnL)
	}
	if c.UsableN != 0 || c.N != 2 {
		t.Errorf("UsableN/N = %d/%d, want 0/2", c.UsableN, c.N)
	}
	if c.Note == "" {
		t.Errorf("a cell with no usable rows must say why")
	}
}
