package store

import (
	"path/filepath"
	"testing"
)

func TestNTPositionsLastUpsertGet(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if r, err := st.NTPositionsLast().Get("Sim101"); err != nil || r != nil {
		t.Fatalf("never stored must read nil,nil: %+v %v", r, err)
	}
	if err := st.NTPositionsLast().Upsert(" Sim101 ", 2, 1000); err != nil {
		t.Fatal(err)
	}
	if err := st.NTPositionsLast().Upsert("SIM101", 0, 2000); err != nil { // same account, canonical key
		t.Fatal(err)
	}
	r, err := st.NTPositionsLast().Get("sim101")
	if err != nil || r == nil || r.OpenCount != 0 || r.ReceivedAtMs != 2000 {
		t.Fatalf("latest frame must win under one canonical key: %+v %v", r, err)
	}
	if err := st.NTPositionsLast().Upsert("  ", 1, 3000); err != nil {
		t.Fatal(err)
	}
	var n int64
	st.GormDB().Model(&NTPositionsLast{}).Count(&n)
	if n != 1 {
		t.Fatalf("an empty account must not be filed: rows=%d", n)
	}
}
