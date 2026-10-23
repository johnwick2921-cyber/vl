package ninjatrader

import (
	"path/filepath"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// m416 A — the persister's row mapping (the function the worker invokes) stores
// a wall-clock-closed but non-final live bar as live_partial, and a later final
// corrects it through the real store.
func TestM416NonFinalClosedBarIsStoredPartialAndCorrectable(t *testing.T) {
	t.Setenv("BARS_KEY_BACKUP_DIR", t.TempDir())
	st, err := store.New(filepath.Join(t.TempDir(), "m416.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	base := int64(1_789_000_020_000)
	snap := []ntwire.Bar{{T: base, O: 29702, H: 29781, L: 29698, C: 29774.5, V: 2907, Source: ntwire.BarSourceLive}} // 10-19 08:58 shape
	rows := barRowsForPersist("MNQ", "1m", "MNQ 12-26", false, snap)
	if rows[0].Source != store.BarSourceLivePartial {
		t.Fatalf("non-final live bar source = %q, want %q", rows[0].Source, store.BarSourceLivePartial)
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	fin := []ntwire.Bar{{T: base, O: 29702, H: 29781, L: 29698, C: 29705, V: 11000, Final: true, Source: ntwire.BarSourceLive}}
	frows := barRowsForPersist("MNQ", "1m", "MNQ 12-26", false, fin)
	if frows[0].Source != store.BarSourceLive {
		t.Fatalf("final live bar source = %q, want live", frows[0].Source)
	}
	if err := bh.InsertBars(frows); err != nil {
		t.Fatal(err)
	}
	got, err := bh.LastNBarsOn("MNQ", "1m", "MNQ 12-26", 5)
	if err != nil || len(got) != 1 || got[0].C != 29705 || got[0].V != 11000 || got[0].Source != store.BarSourceLive {
		t.Fatalf("after the final: %+v err=%v", got, err)
	}
}
