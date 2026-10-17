package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F35 — the persisted last accounts_list frame. Pins the UPSERT semantics the
// installation gate's sim_accounts fallback rests on:
//   - a fresh frame UPSERTs by name (no duplicates);
//   - a name that leaves the frame keeps its last row (never deleted);
//   - a non-SIM row stays non-SIM; missing data reads fail-closed at the gate
//     (the store itself simply returns nil, and the gate treats nil as absent);
//   - LatestReceivedAt is the newest stamp across rows; LastSeenCT formats it.
func TestNTAccountLastUpsertListGetLatest(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := st.NTAccountsLast()
	if s == nil {
		t.Fatal("NTAccountsLast() nil")
	}

	rows, err := s.List()
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("fresh table not empty: %+v", rows)
	}
	if _, ok := s.LatestReceivedAt(); ok {
		t.Fatal("LatestReceivedAt ok=true on an empty table")
	}

	// Frame 1: two accounts.
	t1 := time.Date(2026, 10, 17, 14, 5, 0, 0, time.UTC).UnixMilli()
	if err := s.Upsert([]NTAccountLast{
		{Name: "Sim101", IsSim: true, Connection: "SimulationConnection", ReceivedAtMs: t1},
		{Name: "Sim102", IsSim: true, Connection: "PlaybackConnection", ReceivedAtMs: t1},
	}); err != nil {
		t.Fatalf("Upsert frame 1: %v", err)
	}
	rows, err = s.List()
	if err != nil {
		t.Fatalf("List after frame 1: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(rows), rows)
	}

	// Frame 2: Sim102 flips non-SIM, Sim101 drops off the frame entirely.
	// The dropped name must SURVIVE at its last state (never deleted), and the
	// flip must be reflected.
	t2 := t1 + 60_000
	if err := s.Upsert([]NTAccountLast{
		{Name: "Sim102", IsSim: false, Connection: "LiveConnection", ReceivedAtMs: t2},
	}); err != nil {
		t.Fatalf("Upsert frame 2: %v", err)
	}
	rows, err = s.List()
	if err != nil {
		t.Fatalf("List after frame 2: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("drop-off must keep the row: want 2, got %d: %+v", len(rows), rows)
	}
	byName := map[string]NTAccountLast{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	sim101, ok := byName["Sim101"]
	if !ok || !sim101.IsSim || sim101.ReceivedAtMs != t1 {
		t.Fatalf("Sim101 dropped row wrong: %+v", sim101)
	}
	sim102, ok := byName["Sim102"]
	if !ok || sim102.IsSim {
		t.Fatalf("Sim102 flip not persisted (must stay non-SIM): %+v", sim102)
	}
	if sim102.Connection != "LiveConnection" {
		t.Fatalf("Sim102 connection not updated: %q", sim102.Connection)
	}

	got, err := s.Get("Sim101")
	if err != nil || got == nil || !got.IsSim {
		t.Fatalf("Get Sim101: %+v, %v", got, err)
	}
	if got, err := s.Get("Unknown"); err != nil || got != nil {
		t.Fatalf("Get Unknown must be nil,nil: %+v, %v", got, err)
	}

	ms, ok := s.LatestReceivedAt()
	if !ok || ms != t2 {
		t.Fatalf("LatestReceivedAt = %d,%v; want %d", ms, ok, t2)
	}

	if l := LastSeenCT(t2); !strings.HasSuffix(l, " CT") || len(l) == 0 {
		t.Fatalf("LastSeenCT(%d) = %q", t2, l)
	}
	if l := LastSeenCT(0); l != "never" {
		t.Fatalf("LastSeenCT(0) = %q, want never", l)
	}
}
