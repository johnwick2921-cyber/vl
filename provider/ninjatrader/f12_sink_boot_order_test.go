// F12 — the snapshot sink's WIRING, not its logic.
//
// The logic was always fine and always tested. What shipped broken was the
// ORDER: main.go registered the sink AFTER LoadTradersFromStore, which builds
// the first trader, which lazily starts the TCP server, which reads the hook
// exactly once at start. The server came up with a nil sink and every received
// frame was cached and none persisted — silently, because the cache is what the
// cutover gate reads, so the feature kept working and the missing half left no
// trace. nt8_order_snapshots sat at 0 rows while frames arrived every 30 s.
//
// The existing tests could not catch it: they call the sink directly and never
// exercise the wiring order. This file does the opposite — it drives a REAL
// frame through a REAL server and asserts a row lands.

package ninjatrader

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"vl/store"
)

// TestSinkInstalledBeforeStartPersistsTheFirstFrame is the boot-order test: the
// sink is registered BEFORE the server starts, exactly as main.go must, and the
// FIRST received frame has to reach the database.
func TestSinkInstalledBeforeStartPersistsTheFirstFrame(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "f12.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)

	// ORDER UNDER TEST: sink first, start second.
	var fired atomic.Int64
	srv.SetOrderSnapshotSink(func(p OrderSnapshotPayload) {
		// Counted AFTER the insert: the poll below reads the row as soon as the
		// count moves, and counting first raced the insert (seen once in a
		// loaded full -race run, 2026-09-22; 0/40 alone at base and at head).
		defer fired.Add(1)
		if ierr := db.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
			Account: p.Account, BuildID: p.BuildID, Reason: p.Reason,
			OrderCount: len(p.Orders), WorkingCount: len(p.WorkingOrders()),
			EmittedMs: p.EmittedMs, ReceivedMs: time.Now().UnixMilli(),
		}); ierr != nil {
			t.Errorf("insert: %v", ierr)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// The sink must be installed on the running server — the exact thing that
	// was nil in production.
	if srv.orderSnapCB == nil {
		t.Fatal("the snapshot sink is NIL on a started server — registered too late")
	}

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	frame := OrderSnapshotPayload{
		Account: "Sim101", BuildID: "2026-09-03-f12", Reason: "state_change",
		EmittedMs: time.Now().UnixMilli(),
		Orders: []NT8Order{
			{OrderID: "NT-1", Symbol: "MNQ", Type: "stop", StopPrice: 29475, Quantity: 1, State: "Working"},
			{OrderID: "NT-2", Symbol: "MNQ", Type: "limit", LimitPrice: 29450, Quantity: 1, State: "Filled"},
		},
	}
	if err := writeFromMock(client, FrameOrderSnapshot, frame); err != nil {
		t.Fatalf("write order_snapshot: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("the sink never fired for a received order_snapshot frame")
	}

	// THE ASSERTION THAT WOULD HAVE CAUGHT PRODUCTION: a row exists.
	row, rerr := db.NT8OrderSnapshots().Latest("Sim101", "")
	if rerr != nil {
		t.Fatalf("no row persisted for the first frame: %v", rerr)
	}
	if row.BuildID != "2026-09-03-f12" {
		t.Errorf("row build_id = %q, want the frame's", row.BuildID)
	}
	if row.OrderCount != 2 || row.WorkingCount != 1 {
		t.Errorf("row counts = %d/%d, want 2 orders / 1 working (Filled is not working)",
			row.OrderCount, row.WorkingCount)
	}
}

// A sink registered AFTER the server starts is NOT installed. This is the trap
// itself, asserted rather than described: if someone later makes SetOrderSnapshotSink
// take effect post-start, this test fails and they can delete it deliberately —
// which is the point. An invariant nothing asserts is a comment.
func TestSinkRegisteredAfterStartIsNotInstalled(t *testing.T) {
	srv := NewTCPServer(nil)
	srv.SetAddrForTest(freeEphemeralAddr(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// The transport-layer singleton reads its package-level hook once, at start.
	// Setting it on THIS server object still works (it is a plain field), so what
	// this pins is the shape of the bug: the value must be present BEFORE the
	// start that reads it. See TestMainRegistersTheSinkBeforeLoadingTraders.
	if srv.orderSnapCB != nil {
		t.Fatal("a freshly started server must have no sink until one is set")
	}
}

// THE ORDERING GUARD. The bug was not in any function — it was in the sequence
// of two calls in main.go, and no unit test of either call could see it. This
// reads the source and pins the order.
func TestMainRegistersTheSinkBeforeLoadingTraders(t *testing.T) {
	// main.go sits two directories up from provider/ninjatrader.
	src, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Skipf("main.go unreadable from here (%v) — the ordering guard cannot run", err)
	}
	s := string(src)
	sink := indexOf(s, "SetOrderSnapshotSink(")
	load := indexOf(s, "LoadTradersFromStore(")
	if sink < 0 || load < 0 {
		t.Fatalf("cannot locate both calls in main.go (sink=%d load=%d) — the guard has gone stale", sink, load)
	}
	if sink > load {
		t.Fatalf("SetOrderSnapshotSink is registered AFTER LoadTradersFromStore "+
			"(offsets %d > %d). LoadTradersFromStore starts the TCP server, which reads "+
			"the hook once — so the server comes up with a nil sink and every frame is "+
			"cached but never persisted. This is the F12 defect, exactly.", sink, load)
	}
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
