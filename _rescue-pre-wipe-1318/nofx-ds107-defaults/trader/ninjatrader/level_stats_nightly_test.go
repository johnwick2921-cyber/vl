package ninjatrader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"
)

// ═══════════════════════════════════════════════════════════════════════════
// S-LIST CLOSER FIX2 — nightly level_stats fixtures (2026-08-27).
//
// The bug: the nightly writer evaluated 0 rows on every production run. The
// root cause was NOT the plan lookup — ListVersionsForTrader matches
// (trade_date, session, strategy_id) and the hoang trader's rows exist. It was
// the WIRING: a process-global sync.Once, so whichever trader constructed
// first owned the nightly job — at boot that is the non-running "15m" trader
// (constructed before hoang), whose strategy_id has no plan rows. The fix keys
// idempotency per trader ID.
//
// Fixtures: (1) the per-trader wiring decision; (2) a hermetic end-to-end
// runLevelStatsDayOnce that seeds bars + plan rows and must evaluate >0 rows;
// (3) env-gated proof harness that replays the REAL nightly path against a DB
// COPY (LEVELSTATS_PROOF_DB) with a pinned clock — the worktree proof the
// dispatch requires (no live-DB writes).
// ═══════════════════════════════════════════════════════════════════════════

func TestLevelStatsNightlyPerTraderWiring(t *testing.T) {
	if !wireLevelStatsForTrader("trader-A") {
		t.Fatal("first wiring for trader-A must start the job")
	}
	if wireLevelStatsForTrader("trader-A") {
		t.Fatal("second wiring for trader-A must be a no-op")
	}
	if !wireLevelStatsForTrader("trader-B") {
		t.Fatal("a DIFFERENT trader must get its own job (the pre-fix global once denied this)")
	}
	if wireLevelStatsForTrader("trader-B") {
		t.Fatal("second wiring for trader-B must be a no-op")
	}
}

// TestLevelStatsNightlyEvaluatesSeatedRows — hermetic end-to-end: bars + plan
// rows for one session-day, then runLevelStatsDayOnce must evaluate the
// seated levels (>0 rows) with the right identity fields.
func TestLevelStatsNightlyEvaluatesSeatedRows(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "ls.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ls := st.LevelStats()
	if err := ls.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}

	loc := kernel.CTLocation()
	start := time.Date(2026, 8, 26, 17, 0, 0, 0, loc)
	rows := make([]store.BarHistoryDB, 0, 120)
	for i := 0; i < 120; i++ {
		ms := start.Add(time.Duration(i) * time.Minute).UnixMilli()
		px := 100.0 + float64(i)*0.1
		rows = append(rows, store.BarHistoryDB{Contract: "MNQ 09-26", Source: store.BarSourceLive, Symbol: "MNQ", TF: "1m", OpenTimeMs: ms, O: px, H: px + 1, L: px - 1, C: px, V: 10})
	}
	if err := st.BarHistory().InsertBars(rows); err != nil {
		t.Fatal(err)
	}

	doc := kernel.PlanDoc{
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
	}
	blob, _ := json.Marshal(doc)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: "2026-08-26:NY:trader-1", TradeDate: "2026-08-26", Session: "NY",
		StrategyID: "trader-1", Lifecycle: "active", Doc: string(blob),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	n, err := runLevelStatsDayOnce(st, ls, "trader-1", "2026-08-26",
		start.UnixMilli(), start.Add(24*time.Hour).UnixMilli(), time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("nightly evaluation: %v", err)
	}
	if n != 1 {
		t.Fatalf("evaluated rows = %d, want 1 (the seated PDH level)", n)
	}
	total, _ := ls.Count()
	if total != 1 {
		t.Fatalf("level_stats rows = %d, want 1", total)
	}
}

// TestLevelStatsNightlyProofDB — the worktree proof: replays the REAL nightly
// path (runLevelStatsDayAt) against a DB COPY with a pinned clock. Env-gated:
//
//	LEVELSTATS_PROOF_DB     path to the sqlite COPY (writable, NOT the live DB)
//	LEVELSTATS_PROOF_TRADER trader id (strategy_id) to evaluate
//	LEVELSTATS_PROOF_NOW    pinned "now" in CT (RFC3339); day evaluated = the
//	                        previous CME session-day, same arithmetic as prod
func TestLevelStatsNightlyProofDB(t *testing.T) {
	path := os.Getenv("LEVELSTATS_PROOF_DB")
	traderID := os.Getenv("LEVELSTATS_PROOF_TRADER")
	if path == "" || traderID == "" {
		t.Skip("proof env (LEVELSTATS_PROOF_DB/TRADER) not set — hermetic fixtures cover the path")
	}
	nowStr := os.Getenv("LEVELSTATS_PROOF_NOW")
	if nowStr == "" {
		nowStr = "2026-08-27T22:00:00"
	}
	now, err := time.ParseInLocation("2006-01-02T15:04:05", nowStr, kernel.CTLocation())
	if err != nil {
		t.Fatalf("LEVELSTATS_PROOF_NOW: %v", err)
	}
	st, err := store.New(path)
	if err != nil {
		t.Fatalf("open proof DB: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ls := st.LevelStats()
	if err := ls.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cur := kernel.CMESessionDayStart(now)
	dayKey := cur.AddDate(0, 0, -1).In(kernel.CTLocation()).Format("2006-01-02")

	// The COPY is disposable — clear the target day's rows so the WRITE this
	// replay performs is what's measured (the live 74 rows were backfill
	// writes; a nightly replay on top of them would upsert idempotently and
	// the total count would not move).
	if _, err := st.DB().Exec("DELETE FROM level_stats WHERE session_day = ? AND trader_id = ?", dayKey, traderID); err != nil {
		t.Fatalf("clear copy day rows: %v", err)
	}
	before, _ := ls.Count()
	n, err := runLevelStatsDayAt(st, ls, traderID, now)
	if err != nil {
		t.Fatalf("nightly replay: %v", err)
	}
	after, _ := ls.Count()

	for _, sess := range []string{"NY", "ASIA", "LONDON"} {
		vers, _ := st.Plan().ListVersionsForTrader(dayKey, sess, traderID)
		t.Logf("PROOF plan lookup: %s/%s → %d version(s) (pre-fix nightly saw 0)", dayKey, sess, len(vers))
	}
	t.Logf("PROOF nightly replay: pinned now=%s day=%s trader=%s evaluated=%d rows before=%d after=%d",
		nowStr, dayKey, traderID, n, before, after)
	if n <= 0 {
		t.Fatalf("nightly replay evaluated 0 rows for %s — the T1 saga is NOT closed", dayKey)
	}
	if after <= before {
		t.Fatalf("nightly replay wrote nothing for %s (before=%d after=%d)", dayKey, before, after)
	}
}
