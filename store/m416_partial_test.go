package store

import "testing"

// m416 A/C — store half. A partial snapshot is correctable by a final or by a
// replay; a final is never displaced by a replay or a partial.
func TestM416PartialIsCorrectable(t *testing.T) {
	bh := rollStore(t)
	const a, b, c, d = int64(1789097820000), int64(1789097880000), int64(1789097940000), int64(1789098000000)

	// partial, then live final: final wins.
	_ = bh.InsertBars([]BarHistoryDB{bsRow(a, 29774, BarSourceLivePartial)})
	_ = bh.InsertBars([]BarHistoryDB{bsRow(a, 29711, BarSourceLive)})
	if r := readOne(t, bh, a); r.C != 29711 || r.Source != BarSourceLive {
		t.Fatalf("a live final must correct a partial: %+v", r)
	}
	// partial, then historical: historical replaces the partial (C).
	_ = bh.InsertBars([]BarHistoryDB{bsRow(b, 29774, BarSourceLivePartial)})
	_ = bh.InsertBars([]BarHistoryDB{bsRow(b, 29711, BarSourceHistorical)})
	if r := readOne(t, bh, b); r.C != 29711 || r.Source != BarSourceHistorical {
		t.Fatalf("a historical row must replace a partial: %+v", r)
	}
	// ...and a later re-persist of the same partial (ring tail window) must NOT undo it.
	_ = bh.InsertBars([]BarHistoryDB{bsRow(b, 29774, BarSourceLivePartial)})
	if r := readOne(t, bh, b); r.C != 29711 || r.Source != BarSourceHistorical {
		t.Fatalf("a partial re-persist undid the correction: %+v", r)
	}
	// a live final is NEVER overwritten by historical or by a partial.
	_ = bh.InsertBars([]BarHistoryDB{bsRow(c, 29711, BarSourceLive)})
	_ = bh.InsertBars([]BarHistoryDB{bsRow(c, 1, BarSourceHistorical)})
	_ = bh.InsertBars([]BarHistoryDB{bsRow(c, 2, BarSourceLivePartial)})
	if r := readOne(t, bh, c); r.C != 29711 || r.Source != BarSourceLive {
		t.Fatalf("a live final was overwritten: %+v", r)
	}
	// a partial fills an absent key and refreshes another partial.
	_ = bh.InsertBars([]BarHistoryDB{bsRow(d, 5, BarSourceLivePartial)})
	_ = bh.InsertBars([]BarHistoryDB{bsRow(d, 6, BarSourceLivePartial)})
	if r := readOne(t, bh, d); r.C != 6 || r.Source != BarSourceLivePartial {
		t.Fatalf("partial fill/refresh: %+v", r)
	}
	if !IsReadableSource(BarSourceLivePartial) {
		t.Fatalf("a partial is live-scale tape and must stay readable")
	}
}
