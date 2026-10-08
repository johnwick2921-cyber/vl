package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// VOID PARITY D3 — a read's facts are recorded whether or not the read failed.
// This is the whole point: before it, a working fix erased its own evidence.
func TestReadFactsPersistOnEveryReadAndCap(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "rf.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	rf := st.PlannerReadFacts()

	// An ACCEPTED read (no reject row anywhere) still records its facts.
	if err := rf.SaveReadFact(&PlannerReadFact{
		TraderID: "hoang", TradeDate: "2026-09-02", Session: "ASIA", PromptHash: "h1",
		VoidLevels: EncodeVoidLevels([]VoidLevelRecord{{Price: 29141.25, Short: true, ReclaimedAt: "03:34 CT"}}),
		VoidCount:  1, StopFloorPts: 27.1, ATR5m: 18.09, StopFloorMlt: 1.5,
		ScopeSinceMs: 0, ScopeBars: 2000, ScopeIntv: "1m",
	}); err != nil {
		t.Fatalf("accepted-read write: %v", err)
	}
	got, err := rf.LatestReadFact()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.VoidCount != 1 || got.StopFloorPts != 27.1 || got.ScopeBars != 2000 || got.ScopeSinceMs != 0 {
		t.Errorf("row lost its facts: %+v", got)
	}
	var recs []VoidLevelRecord
	if err := json.Unmarshal([]byte(got.VoidLevels), &recs); err != nil || len(recs) != 1 || recs[0].Price != 29141.25 {
		t.Errorf("void list must round-trip verbatim, got %q (%v)", got.VoidLevels, err)
	}

	// "computed and EMPTY" must not read as "not computed" (A24: no placeholder
	// that reads as data).
	if EncodeVoidLevels(nil) != "[]" {
		t.Errorf("an empty computed list encodes as [], got %q", EncodeVoidLevels(nil))
	}

	// Cap trims oldest, newest survive.
	for i := 0; i < PlannerReadFactsCap+25; i++ {
		if err := rf.SaveReadFact(&PlannerReadFact{TraderID: "hoang", PromptHash: "bulk", VoidLevels: "[]"}); err != nil {
			t.Fatalf("bulk write %d: %v", i, err)
		}
	}
	if n := rf.ReadFactsCount(); n > PlannerReadFactsCap {
		t.Errorf("cap not enforced: %d rows > %d", n, PlannerReadFactsCap)
	}
	last, err := rf.LatestReadFact()
	if err != nil || last.PromptHash != "bulk" {
		t.Errorf("newest row must survive the trim: %+v %v", last, err)
	}
	t.Logf("rows after cap: %d (cap %d)", rf.ReadFactsCount(), PlannerReadFactsCap)
}

// A nil store must be a no-op, never a panic on a planner read (A10).
func TestReadFactsNilStoreIsSafe(t *testing.T) {
	var rf *PlannerReadFactsStore
	if err := rf.SaveReadFact(&PlannerReadFact{}); err != nil {
		t.Errorf("nil store must no-op, got %v", err)
	}
	if n := rf.ReadFactsCount(); n != 0 {
		t.Errorf("nil store count must be 0, got %d", n)
	}
}

// PIN 4 (wave BARS HORIZON, 2026-09-09) — A LEGACY ROW READS UNKNOWN, NOT ZERO.
//
// The five new numeric columns are 0 on the 67 rows written before this wave.
// A raw-SQL reader who sees scope_gap_count = 0 on row 66 must NOT read it as
// "no gaps" — that read was served a tape with 696 open-market minutes missing.
// The discriminator is the TEXT column, the same "" vs "[]" convention this
// file already uses for VoidLevels (planner_read_facts.go:30-32).
func TestLegacyRowsReadUnknownNotZero(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "rfh.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	rf := st.PlannerReadFacts()

	// A row written the PRE-WAVE way — the exact field set of the 67 live rows.
	if err := rf.SaveReadFact(&PlannerReadFact{
		TraderID: "hoang", ScopeBars: 2000, ScopeSinceMs: 1788904800000, ScopeIntv: "1m",
	}); err != nil {
		t.Fatalf("save legacy row: %v", err)
	}
	legacy, err := rf.LatestReadFact()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if legacy.ReadHorizons != "" {
		t.Fatalf("legacy read_horizons=%q, want \"\" (not computed)", legacy.ReadHorizons)
	}
	if legacy.HorizonRecorded() {
		t.Fatalf("legacy row reports HorizonRecorded()=true, want false — its scope_gap_count=%d is UNKNOWN, not zero", legacy.ScopeGapCount)
	}

	// A POST-wave row whose gap count is genuinely 0 (a contiguous tape). The
	// two rows are byte-identical in scope_gap_count and MUST be distinguishable.
	if err := rf.SaveReadFact(&PlannerReadFact{
		TraderID: "hoang", ScopeBars: 2000, ScopeRequestedBars: 2000, ScopeSinceMs: 1788904800000,
		ScopeIntv: "1m", ScopeSpanMs: 1999 * 60000, ScopeOldestAgeMs: 2000 * 60000,
		ScopeGapCount: 0, ReadHorizons: `[{"who":"void","gaps":0}]`,
	}); err != nil {
		t.Fatalf("save computed-zero row: %v", err)
	}
	computed, err := rf.LatestReadFact()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !computed.HorizonRecorded() {
		t.Fatalf("computed-zero row reports HorizonRecorded()=false, want true")
	}
	if computed.ScopeGapCount != legacy.ScopeGapCount {
		t.Fatalf("fixture broken: the two rows must share scope_gap_count=0 (got %d vs %d)", computed.ScopeGapCount, legacy.ScopeGapCount)
	}
}

// FIX-READ-FACTS-PLAN-ID — the bind is BY EXACT ID, never "newest unbound".
// A failed read's facts row (written, no plan) must stay empty even when a
// later plan lands: only the id the read returned can bind.
func TestBindPlanToReadFact(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "bind.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	rf := st.PlannerReadFacts()

	// A FAILED read: its facts row was written, but it produced no plan.
	failed := &PlannerReadFact{TraderID: "t1", TradeDate: "2026-10-08", Session: "NY", PromptHash: "h-failed"}
	if err := rf.SaveReadFact(failed); err != nil {
		t.Fatalf("save failed read: %v", err)
	}
	failedID := failed.ID

	// A later plan lands from a path with its OWN read (a new facts row).
	good := &PlannerReadFact{TraderID: "t1", TradeDate: "2026-10-08", Session: "NY", PromptHash: "h-good"}
	if err := rf.SaveReadFact(good); err != nil {
		t.Fatalf("save good read: %v", err)
	}

	// Bind the GOOD read's id → only that row is stamped.
	if n := rf.BindPlanToReadFact(good.ID, "2026-10-08:NY:t1", 2); n != 1 {
		t.Fatalf("bind good read: rows=%d want 1", n)
	}

	// The failed read's row must still be empty — the exact-id bind never
	// reaches it. (RED for the old "newest unbound" heuristic, which WOULD have
	// bound failedID as the newest empty row.)
	var failedRow PlannerReadFact
	if err := st.gdb.Where("id = ?", failedID).First(&failedRow).Error; err != nil {
		t.Fatalf("read failed row: %v", err)
	}
	if failedRow.PlanID != "" || failedRow.Version != 0 {
		t.Fatalf("failed read's row was bound to a plan it did not produce: %q v%d", failedRow.PlanID, failedRow.Version)
	}

	// Re-binding the already-bound good row is a no-op (WHERE plan_id = '').
	if n := rf.BindPlanToReadFact(good.ID, "2026-10-08:NY:t1", 3); n != 0 {
		t.Fatalf("re-bind bound row: rows=%d want 0", n)
	}

	// Unknown id / zero id bind nothing.
	if n := rf.BindPlanToReadFact(0, "2026-10-08:NY:t1", 1); n != 0 {
		t.Fatalf("zero-id bind: rows=%d want 0", n)
	}
	if n := rf.BindPlanToReadFact(999999, "2026-10-08:NY:t1", 1); n != 0 {
		t.Fatalf("unknown-id bind: rows=%d want 0", n)
	}
}

// A nil store must be a no-op for the bind too (the write site guards on
// at.store, but the store method itself must never panic).
func TestBindPlanNilStoreIsSafe(t *testing.T) {
	var rf *PlannerReadFactsStore
	if n := rf.BindPlanToReadFact(1, "p", 1); n != 0 {
		t.Fatalf("nil store bind must no-op, got %d", n)
	}
}
