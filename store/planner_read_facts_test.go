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

// FIX-READ-FACTS-PLAN-ID — the bind stamps only the newest UNBOUND row for the
// (trader, date, session) key, and never re-binds a row that already has a plan.
func TestBindPlanToLatestReadFact(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "bind.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	rf := st.PlannerReadFacts()

	// Read 1 → plan v1: the newest unbound row is read 1's.
	if err := rf.SaveReadFact(&PlannerReadFact{TraderID: "t1", TradeDate: "2026-10-08", Session: "NY", PromptHash: "h1"}); err != nil {
		t.Fatalf("save read 1: %v", err)
	}
	if n := rf.BindPlanToLatestReadFact("t1", "2026-10-08", "NY", "2026-10-08:NY:t1", 1); n != 1 {
		t.Fatalf("bind read 1: rows=%d want 1", n)
	}

	// Read 2 → plan v2: read 1 is already bound, so the newest unbound is read 2.
	if err := rf.SaveReadFact(&PlannerReadFact{TraderID: "t1", TradeDate: "2026-10-08", Session: "NY", PromptHash: "h2"}); err != nil {
		t.Fatalf("save read 2: %v", err)
	}
	if n := rf.BindPlanToLatestReadFact("t1", "2026-10-08", "NY", "2026-10-08:NY:t1", 2); n != 1 {
		t.Fatalf("bind read 2: rows=%d want 1", n)
	}

	// No unbound row remains → a third bind is a no-op (idempotent).
	if n := rf.BindPlanToLatestReadFact("t1", "2026-10-08", "NY", "2026-10-08:NY:t1", 3); n != 0 {
		t.Fatalf("bind after all bound: rows=%d want 0", n)
	}

	// Read back by id: v1 ↔ h1, v2 ↔ h2.
	var rows []PlannerReadFact
	if err := st.gdb.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(rows) != 2 || rows[0].PromptHash != "h1" || rows[0].Version != 1 || rows[0].PlanID != "2026-10-08:NY:t1" {
		t.Fatalf("read 1 row wrong: %+v", rows)
	}
	if rows[1].PromptHash != "h2" || rows[1].Version != 2 || rows[1].PlanID != "2026-10-08:NY:t1" {
		t.Fatalf("read 2 row wrong: %+v", rows)
	}
}

// A nil store must be a no-op for the bind too (the write site guards on
// at.store, but the store method itself must never panic).
func TestBindPlanNilStoreIsSafe(t *testing.T) {
	var rf *PlannerReadFactsStore
	if n := rf.BindPlanToLatestReadFact("t", "d", "NY", "p", 1); n != 0 {
		t.Fatalf("nil store bind must no-op, got %d", n)
	}
}
