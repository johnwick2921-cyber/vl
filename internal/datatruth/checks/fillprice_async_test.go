//go:build !windows

package checks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// m55r3-h2: the state write is OFF the Record path, coalesced, ordered, and
// crash-safe once it lands.
//
// MUTANT: persist() writes inline -> the Record in the stalled leg blocks, RED.
func TestFillGuardRecordNeverBlocksOnAStalledStateWriteAndTheLatestSnapshotLands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fill.json")
	g, _ := newTestGuard(nil)
	if _, err := g.EnableState(path); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".tmp"
	if err := syscall.Mkfifo(tmp, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := now.UnixMilli() / minuteMs * minuteMs

	done := make(chan struct{})
	go func() {
		defer close(done)
		// a malformed fill (price 0) is counted as a far fill: it persists
		g.Record(FillObs{ID: "bad1", Symbol: "MNQ", Kind: "exit", Account: "Sim101", Price: 0, AtMs: m + 5_000}, nil, now)
		g.Record(FillObs{ID: "bad2", Symbol: "MNQ", Kind: "exit", Account: "Sim101", Price: 0, AtMs: m + 6_000}, nil, now)
		g.Record(FillObs{ID: "bad3", Symbol: "MNQ", Kind: "exit", Account: "Sim101", Price: 0, AtMs: m + 7_000}, nil, now)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record is blocked on the state-file write")
	}
	if b, _, _ := g.Check()("MNQ", now); !b {
		t.Fatal("the in-memory pause is immediate, whatever the disk does")
	}

	// the disk recovers: a reader appears, the stalled write ends (a FIFO cannot be
	// fsynced, so that write fails loudly and writes nothing), and the writer goes idle.
	r, err := os.OpenFile(tmp, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Flush(5 * time.Second)
	r.Close()
	// the temp name is a regular path again; a fresh persist lands the NEWEST
	// snapshot (all three strikes) through the normal temp + rename.
	if err := os.Rename(tmp, tmp+".stalled"); err != nil {
		t.Fatal(err)
	}
	g.persist()
	if !g.Flush(5 * time.Second) {
		t.Fatal("the writer did not go idle")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d fillStateDisk
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("the landed file is complete JSON: %v", err)
	}
	var ids int
	for _, st := range d.Strikes {
		ids += len(st.IDs)
	}
	if ids != 3 {
		t.Fatalf("the newest snapshot (3 strikes) is what lands: %d (%+v)", ids, d)
	}
}
