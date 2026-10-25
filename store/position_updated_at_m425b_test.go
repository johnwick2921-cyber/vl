package store

import (
	"path/filepath"
	"testing"
)

// m425b E3: trader_positions.updated_at is MILLISECONDS for every writer. GORM
// auto-stamps an int64 field named UpdatedAt in SECONDS on any Updates() that
// does not set it (UpdateExcursion, the adherence/attribution stampers), which
// wrote seconds into rows 691-697 next to ms rows. MUTANT: drop
// `autoUpdateTime:milli` from the UpdatedAt tag → the stamp is ~1.7e9 → RED.
func TestPositionUpdatedAtIsMillisecondsOnAutoStamp(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pos := &TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 100, Quantity: 1, EntryTime: 1792706946521}
	if err := st.Position().Create(pos); err != nil {
		t.Fatal(err)
	}
	if pos.CreatedAt < 100_000_000_000 {
		t.Fatalf("auto-created created_at = %d is seconds, want ms", pos.CreatedAt)
	}
	// a writer that does NOT set updated_at itself: the GORM auto-stamp decides.
	if err := st.Position().UpdateExcursion(pos.ID, 1, 2); err != nil {
		t.Fatal(err)
	}
	var got TraderPosition
	if err := st.GormDB().First(&got, pos.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAt < 100_000_000_000 {
		t.Fatalf("updated_at = %d after an auto-stamped update is SECONDS; every writer must write ms", got.UpdatedAt)
	}
}

// Old second-valued rows (691-697) still read right; ms and 0 pass through.
func TestPositionEpochMsToleratesSecondsAndMs(t *testing.T) {
	for _, c := range []struct{ in, want int64 }{
		{1792708302, 1792708302000},
		{1792711588703, 1792711588703},
		{0, 0},
	} {
		if got := PositionEpochMs(c.in); got != c.want {
			t.Errorf("PositionEpochMs(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if (&TraderPosition{UpdatedAt: 1792708302}).UpdatedAtMs() != 1792708302000 {
		t.Error("UpdatedAtMs must normalize a legacy seconds row")
	}
}

// The history ordering mixes both units: a seconds-valued OPEN partial must
// sort by its true time, not before every ms row. MUTANT: revert the ORDER BY.
func TestHistoryOrderToleratesLegacySecondsUpdatedAt(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mk := func(entry int64) *TraderPosition {
		p := &TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 100, Quantity: 1, EntryTime: entry, RealizedPnL: 5}
		if err := st.Position().Create(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	older, newer := mk(1), mk(2)
	// two OPEN partials (realized != 0, exit_time 0): one legacy seconds stamp
	// that is the NEWER activity, one ms stamp that is older.
	st.GormDB().Exec("UPDATE trader_positions SET updated_at = ? WHERE id = ?", int64(1792708302000)-60_000, older.ID)
	st.GormDB().Exec("UPDATE trader_positions SET updated_at = ? WHERE id = ?", int64(1792708302), newer.ID)
	rows, err := st.Position().GetHistoryPositions("t1", 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("history: %v len=%d", err, len(rows))
	}
	if rows[0].ID != newer.ID {
		t.Fatalf("the legacy-seconds row (newer activity) must sort first, got id %d", rows[0].ID)
	}
}
