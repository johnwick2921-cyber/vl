package ninjatrader

import (
	"path/filepath"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── W-BARS-CONTRACT-KEY — the persister's call site, two contracts, one minute ─
//
// At a roll the AddOn's replay serves the NEW contract's history for minutes
// the OLD contract already holds. Through the persister's own mapping
// (barRowsForPersist — the function the worker invokes) and the store's
// InsertBars, both rows must land, each under its label, and the ring
// rehydrate's reader (LastNBarsOn per contract) must see exactly its own.
func TestPersisterStoresTwoContractsForTheSameMinute(t *testing.T) {
	t.Setenv("BARS_KEY_BACKUP_DIR", t.TempDir())
	st, err := store.New(filepath.Join(t.TempDir(), "roll.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	if !store.BarsKeyHasContract() {
		t.Fatalf("a fresh store must be on the contract key")
	}
	base := int64(1_789_000_000_000)
	// the September minute as it traded (a live frame), then December's replay
	// of the SAME minute after the re-subscribe ACKed "MNQ 12-26"
	sep := []ntwire.Bar{{T: base, O: 29000, H: 29010, L: 28990, C: 29005, V: 1, Final: true, Source: ntwire.BarSourceLive}}
	dec := []ntwire.Bar{{T: base, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Source: ntwire.BarSourceHistorical}}
	if err := bh.InsertBars(barRowsForPersist("MNQ", "1m", "MNQ 09-26", false, sep)); err != nil {
		t.Fatalf("sep: %v", err)
	}
	if err := bh.InsertBars(barRowsForPersist("MNQ", "1m", "MNQ 12-26", true, dec)); err != nil {
		t.Fatalf("dec: %v", err)
	}
	n, _ := bh.Count()
	if n != 2 {
		t.Fatalf("rows = %d, want 2 — the old key dropped December's overlap here", n)
	}
	s, err := bh.LastNBarsOn("MNQ", "1m", "MNQ 09-26", 10)
	if err != nil || len(s) != 1 || s[0].C != 29005 || s[0].Source != store.BarSourceLive {
		t.Fatalf("sep reader: %+v err=%v", s, err)
	}
	d, err := bh.LastNBarsOn("MNQ", "1m", "MNQ 12-26", 10)
	if err != nil || len(d) != 1 || d[0].C != 29295 || d[0].Source != store.BarSourceHistorical {
		t.Fatalf("dec reader: %+v err=%v", d, err)
	}
	// the store fallback for "which contract" prefers the row that TRADED
	if c, ok := bh.LatestContract("MNQ"); !ok || c != "MNQ 09-26" {
		t.Fatalf("LatestContract on the shared minute = %q %v, want the live September row", c, ok)
	}
	// the nightly integrity line: not a duplicate, one roll overlap
	dups, _, total, err := bh.BarsIntegrity()
	ov, _ := bh.RollOverlaps()
	if err != nil || dups != 0 || total != 2 || ov != 1 {
		t.Fatalf("integrity dups=%d total=%d overlaps=%d err=%v", dups, total, ov, err)
	}
}
