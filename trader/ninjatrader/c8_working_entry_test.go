package ninjatrader

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── N-B (release #22 rescan) — C8 must not drop a WORKING resting entry ──────
//
// Rows 366 / 367: NT8 Working at 08:56:00.143 / 09:55:00.129 (snapshots
// 112370 / 112623); the C8 sweep dropped their markers at 08:56:54 / 09:55:48
// with "NT8 never confirmed"; both filled seconds later.

func c8Capture(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	logger.AttachDBSink(func(_ int64, level, _, _, message, _ string) {
		mu.Lock()
		lines = append(lines, level+" "+message)
		mu.Unlock()
	})
	t.Cleanup(func() { logger.AttachDBSink(func(int64, string, string, string, string, string) {}) })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

func c8Lines(lines []string, sub string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func TestC8KeepsAnEntryTheBrokerBookShowsWorking(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "c8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := ntwire.NewTCPServer(nil)
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	get := c8Capture(t)

	now := time.Now().UTC()
	sent := now.UnixMilli() - entryConfirmGraceMs - 9_000 // row 366: 54 s after the send
	kept0, _, _ := C8SweepCounts()
	tr.pendingMu.Lock()
	tr.pending["sig-366"] = "LONG"
	tr.pendingAt["sig-366"] = sent
	tr.pending["sig-dead"] = "LONG" // NT8 never had it
	tr.pendingAt["sig-dead"] = sent
	tr.pendingMu.Unlock()
	tr.mu.Lock()
	tr.lastEntrySignalID = "sig-366"
	tr.mu.Unlock()
	// The broker book received AFTER the send: 366 Working, nothing for sig-dead.
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "o366", Symbol: "MNQ", Name: "sig-366", Action: "buy", Type: "limit", LimitPrice: 30517, Quantity: 1, State: "Working"},
	}}, now)

	tr.reconcilePositions("trader-c8", "nt", "ninjatrader", st)

	if !tr.HasPendingEntry("sig-366") {
		t.Fatal("N-B: a WORKING resting entry must stay pending — its fill must still match")
	}
	tr.mu.Lock()
	last := tr.lastEntrySignalID
	tr.mu.Unlock()
	if last != "sig-366" {
		t.Fatalf("N-B: the sweep cleared lastEntrySignalID of a working entry (now %q)", last)
	}
	if tr.HasPendingEntry("sig-dead") {
		t.Fatal("an entry the fresh book provably lacks is still swept")
	}
	lines := get()
	if c8Lines(lines, "never confirmed") != 1 || !strings.Contains(strings.Join(lines, "\n"), "[sig-dead]") {
		t.Fatalf("N-B: exactly one 'never confirmed' line, naming only sig-dead; got %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "never confirmed") && strings.Contains(l, "sig-366") {
			t.Fatalf("N-B: the working entry was reported 'never confirmed': %s", l)
		}
	}
	if kept, _, _ := C8SweepCounts(); kept-kept0 != 1 {
		t.Fatalf("N-B: the kept entry is counted once; kept delta %d", kept-kept0)
	}
	// A second pass (20 s later) keeps it and does NOT re-count / re-log.
	tr.reconcilePositions("trader-c8", "nt", "ninjatrader", st)
	if kept, _, _ := C8SweepCounts(); !tr.HasPendingEntry("sig-366") || kept-kept0 != 1 {
		t.Fatalf("N-B: second pass must keep silently; kept delta %d", kept-kept0)
	}
}

func TestC8KeepsAnEntryWhoseArmedRowIsWorking(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "c8b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	led := st.ArmedOrders()
	r := &store.ArmedOrderDB{TraderID: "trader-c8b", PlanID: "p", Scenario: "S1", Version: 1, State: "armed", Side: "SHORT", EntryPx: 30456.5, StopPx: 30551.25, TargetPx: 30257.5}
	if err := led.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	if err := led.BeginPlacement(r.ID, "sig-367"); err != nil {
		t.Fatal(err)
	}
	if err := led.ApplyPlacementReceipt("trader-c8b", "sig-367", store.StateWorking, "order_update Working"); err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil) // NO book at all
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	get := c8Capture(t)
	tr.pendingMu.Lock()
	tr.pending["sig-367"] = "SHORT"
	tr.pendingAt["sig-367"] = time.Now().UTC().UnixMilli() - entryConfirmGraceMs - 3_000
	tr.pendingMu.Unlock()

	tr.reconcilePositions("trader-c8b", "nt", "ninjatrader", st)
	if !tr.HasPendingEntry("sig-367") {
		t.Fatal("N-B: an entry whose armed row is WORKING must stay pending")
	}
	if c8Lines(get(), "never confirmed") != 0 {
		t.Fatalf("N-B: no 'never confirmed' for a working row; got %v", get())
	}
}

// With no book and no working row the drop still happens, but the line says
// UNKNOWN — never "never confirmed" without a book to prove it.
func TestC8NoBookSaysUnknownNotNeverConfirmed(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "c8c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := ntwire.NewTCPServer(nil)
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	get := c8Capture(t)
	tr.pendingMu.Lock()
	tr.pending["sig-x"] = "LONG"
	tr.pendingAt["sig-x"] = time.Now().UTC().UnixMilli() - entryConfirmGraceMs - 3_000
	tr.pendingMu.Unlock()
	tr.reconcilePositions("trader-c8c", "nt", "ninjatrader", st)
	if tr.HasPendingEntry("sig-x") {
		t.Fatal("no evidence of a working order: still swept")
	}
	if c8Lines(get(), "never confirmed") != 0 || c8Lines(get(), "confirmation UNKNOWN") != 1 {
		t.Fatalf("no book: the line must say UNKNOWN, not 'never confirmed'; got %v", get())
	}
}
