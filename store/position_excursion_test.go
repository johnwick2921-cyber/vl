package store

import (
	"path/filepath"
	"testing"
)

func TestPositionExcursionColumns(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ps := st.Position()

	pos := &TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryPrice: 15600,
		Status: "OPEN", EntryTime: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := ps.Create(pos); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ps.SetEntryConfidence(pos.ID, 72); err != nil {
		t.Fatalf("set confidence: %v", err)
	}
	if err := ps.UpdateExcursion(pos.ID, 25.5, 40.0); err != nil {
		t.Fatalf("update excursion: %v", err)
	}

	got, err := ps.GetOpenPositions("t1")
	if err != nil || len(got) != 1 {
		t.Fatalf("get: %v n=%d", err, len(got))
	}
	p := got[0]
	// E4 (wave 1A): mae/mfe are nullable now — nil means never computed.
	if p.EntryConfidence == nil || *p.EntryConfidence != 72 {
		t.Fatalf("entry confidence not persisted: %v", p.EntryConfidence)
	}
	if p.MAE == nil || p.MFE == nil {
		t.Fatalf("excursion columns not persisted: mae=%v mfe=%v", p.MAE, p.MFE)
	}
	if *p.MAE != 25.5 || *p.MFE != 40.0 {
		t.Fatalf("excursion columns wrong: mae=%v mfe=%v", *p.MAE, *p.MFE)
	}
}

// E4 (wave 1A) — a NEW position must carry NULL excursions, not the column
// DEFAULT 0. The DDL default has to stay (removing it makes GORM rebuild
// trader_positions, and the position_plan_join VIEW fails mid-rebuild — that
// took store initialization down when it was tried), so this pins that the
// entry writer still lands a NULL rather than letting the default apply.
func TestNewPositionHasNullExcursions(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ps := st.Position()

	pos := &TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryPrice: 15600,
		EntryTime: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := ps.Create(pos); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := ps.GetOpenPositions("t1")
	if err != nil || len(got) != 1 {
		t.Fatalf("get: %v n=%d", err, len(got))
	}
	if got[0].MAE != nil || got[0].MFE != nil {
		t.Fatalf("a fresh position must have NULL excursions, got mae=%v mfe=%v — the DEFAULT 0 leaked back in and 'never measured' is unreadable again",
			deref(got[0].MAE), deref(got[0].MFE))
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
