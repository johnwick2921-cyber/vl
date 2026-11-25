package ninjatrader

import (
	"fmt"
	"testing"
	"time"
)

// P2-14 RED→GREEN — the bounded queue NEVER drops: flood one owner past
// orderedQueueCap while its handler is blocked; the overflow path must (a) go
// loud (counter ≥ 1, ERROR log), (b) block the read loop, and (c) deliver EVERY
// frame in receive order once the handler drains — exactly once. Driven through
// a REAL TCPServer and a REAL wire client (startedServer + dialWire), so the
// production read loop does the enqueueing — no re-built inputs (canon 53).
func TestOrderedQueueOverflowNeverDrops(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})
	rec := &recHandlers{}
	block := make(chan struct{})
	unreg, err := s.RegisterOrderedExecutionsFor("MNQ", "Sim101", OrderedExecutionHandlers{
		Fill: func(f FillPayload) {
			rec.fill(f)
			<-block // the durable consumer is stalled (simulates SQLite-bound handlers)
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer unreg()

	// One frame is parked INSIDE the worker (the blocked handler), so the queue
	// can hold cap more; flooding cap+2 frames forces the last one through the
	// overflow path.
	const total = orderedQueueCap + 2
	writerDone := make(chan error, 1)
	go func() {
		for i := 0; i < total; i++ {
			if err := WriteFrame(w.conn, FrameFill, FillPayload{
				SignalID: fmt.Sprintf("sig-%03d", i), Symbol: "MNQ", Account: "Sim101",
				Side: "long", Quantity: 1, Status: "filled",
			}); err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()

	// The read loop must hit the bound and go loud — never silently grow memory.
	owner := func() *orderedOwner {
		s.orderedMu.Lock()
		defer s.orderedMu.Unlock()
		return s.orderedOwners[subKey("MNQ", "Sim101")]
	}()
	deadline := time.Now().Add(30 * time.Second)
	for owner.overflowCount.Load() == 0 {
		if time.Now().After(deadline) {
			select {
			case e := <-writerDone:
				t.Fatalf("overflow counter never moved — the bound did not engage (writer err: %v; handler ran %d, queue %d)", e,
					rec.snapshotLen(), func() int { owner.queueMu.Lock(); defer owner.queueMu.Unlock(); return len(owner.queue) }())
			default:
				t.Fatal("overflow counter never moved — the bound did not engage (writer still blocked)")
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	if q := func() int { owner.queueMu.Lock(); defer owner.queueMu.Unlock(); return len(owner.queue) }(); q != orderedQueueCap {
		t.Fatalf("queue length = %d at overflow, want exactly cap %d", q, orderedQueueCap)
	}

	// Release the handler: every queued frame (including the blocked one) must
	// arrive, in order, exactly once — nothing dropped, nothing duplicated.
	close(block)
	ev := waitEvents(t, rec, total, 30*time.Second)
	for i := 0; i < total; i++ {
		if want := fmt.Sprintf("fill:sig-%03d", i); ev[i] != want {
			t.Fatalf("frame %d out of order: got %q want %q", i, ev[i], want)
		}
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("wire writer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wire writer never finished — a frame was stranded at the TCP layer")
	}
	// Settle, then pin exactly-once.
	time.Sleep(100 * time.Millisecond)
	if got := rec.snapshotLen(); got != total {
		t.Fatalf("delivered %d fills, want exactly %d", got, total)
	}
}

// P2-14 RED→GREEN — a blocked overflow enqueuer must not be stranded when the
// owner unregisters mid-wait: the enqueue returns false (the frame falls back
// to the advisory fanout with OrderedOwned=false) and the frames already queued
// still drain to the worker, exactly once.
func TestOrderedOverflowEnqueuerReleasedByUnregister(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})
	rec := &recHandlers{}
	block := make(chan struct{})
	unreg, err := s.RegisterOrderedExecutionsFor("MNQ", "Sim101", OrderedExecutionHandlers{
		Fill: func(f FillPayload) {
			rec.fill(f)
			<-block
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer unreg()

	const total = orderedQueueCap + 2
	writeFill := func(i int) error {
		return WriteFrame(w.conn, FrameFill, FillPayload{
			SignalID: fmt.Sprintf("sig-%03d", i), Symbol: "MNQ", Account: "Sim101",
			Side: "long", Quantity: 1, Status: "filled",
		})
	}

	// PRECONDITION, established not assumed: sig-000 is dequeued by the worker
	// and parked inside the blocked handler BEFORE the flood. Without this the
	// worker goroutine can be scheduled late (`go runOrderedOwner` is only
	// woken, never awaited), the read loop then fills the queue to cap with
	// sig-000..sig-255 and sig-256 trips overflowCount while nothing was
	// dequeued; the unreg() below then closes the owner and only cap (256)
	// frames ever reach the worker, never cap+1 (257): a 30 s waitEvents
	// timeout under load (m456).
	if err := writeFill(0); err != nil {
		t.Fatalf("wire write sig-000: %v", err)
	}
	parkDeadline := time.Now().Add(30 * time.Second)
	for rec.snapshotLen() < 1 {
		if time.Now().After(parkDeadline) {
			t.Fatal("sig-000 never reached the handler: worker not parked")
		}
		time.Sleep(time.Millisecond)
	}

	writerDone := make(chan error, 1)
	go func() {
		for i := 1; i < total; i++ {
			if err := writeFill(i); err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()

	owner := func() *orderedOwner {
		s.orderedMu.Lock()
		defer s.orderedMu.Unlock()
		return s.orderedOwners[subKey("MNQ", "Sim101")]
	}()
	deadline := time.Now().Add(30 * time.Second)
	for owner.overflowCount.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("overflow never engaged — cannot test the blocked-enqueuer path")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The owner unregisters while the (cap+2)-th enqueue is blocked in its wait.
	unreg()

	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("wire writer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the blocked enqueuer was stranded by the unregister — enqueueOrdered never returned false")
	}

	// The worker drains what was queued (cap in the queue + 1 parked in the
	// handler — the (cap+2)-th went to the advisory fanout as
	// OrderedOwned=false), exactly once.
	close(block)
	const drained = orderedQueueCap + 1
	ev := waitEvents(t, rec, drained, 30*time.Second)
	if len(ev) != drained {
		t.Fatalf("drained %d, want %d", len(ev), drained)
	}
	for i := 0; i < drained; i++ {
		if want := fmt.Sprintf("fill:sig-%03d", i); ev[i] != want {
			t.Fatalf("frame %d out of order: got %q want %q", i, ev[i], want)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if got := rec.snapshotLen(); got != drained {
		t.Fatalf("delivered %d after unregister, want exactly %d", got, drained)
	}
}
