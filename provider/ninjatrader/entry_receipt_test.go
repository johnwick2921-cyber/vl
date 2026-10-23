// W117 F1 (cd2978b7, d7b70a90 re-derived on today's code) — entry receipt
// fences. The server must not forget which flat snapshots predate an entry:
// positive entry-execution evidence is retained across adapter replacement,
// and only growth of a cumulative observation advances the fence.

package ninjatrader

import (
	"context"
	"testing"
	"time"
)

// TestOrderUpdateFrameNotesEntryReceipt drives the REAL read loop: an
// order_update frame with positive quantity on the signal's own order name
// (partfilled or terminal) must land a server-side entry receipt even when
// no companion fill frame ever arrives. RED = remove the FrameOrderUpdate
// note in tcp_server.go → PositionsForExecutionReceipt returns a zero entry
// time.
func TestOrderUpdateFrameNotesEntryReceipt(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	// The signal's OWN order name, partfilled, positive quantity — the shape
	// d7b70a90 fences on. No fill frame follows.
	if err := writeFromMock(client, FrameOrderUpdate, OrderUpdatePayload{
		SignalID: "sig-1", OrderName: "sig-1", State: "partfilled",
		Quantity: 2, Symbol: "MNQ", Account: "Sim101",
	}); err != nil {
		t.Fatalf("write order_update: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, entry, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ"); !entry.IsZero() {
			return // the receipt landed
		}
		if time.Now().After(deadline) {
			t.Fatal("an order_update frame with positive entry evidence must land a server-side entry receipt")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEntryReceiptAdvancesOnlyOnGrowth locks the cumulative rule: only GROWTH
// of a cumulative entry observation advances the fence. Replayed old frames
// (equal or smaller quantity) must not push the receipt forward, or a stale
// broker snapshot would stay invalidated forever. RED = compare with '>='
// instead of '>' → an equal-quantity replay advances the fence and the pin
// fails.
func TestEntryReceiptAdvancesOnlyOnGrowth(t *testing.T) {
	srv := NewTCPServer(nil)

	srv.NoteEntryExecution("MNQ", "Sim101", "sig-1", 2)
	_, _, t0, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ")
	if t0.IsZero() {
		t.Fatal("the first positive note must land a receipt")
	}
	time.Sleep(2 * time.Millisecond)

	// Equal quantity: a replay of the same frame. The fence must NOT move.
	srv.NoteEntryExecution("MNQ", "Sim101", "sig-1", 2)
	_, _, again, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ")
	if !again.Equal(t0) {
		t.Fatalf("an equal-quantity replay advanced the fence: %v → %v", t0, again)
	}

	// Smaller quantity: a late frame from before the growth. Must not move.
	srv.NoteEntryExecution("MNQ", "Sim101", "sig-1", 1)
	_, _, smaller, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ")
	if !smaller.Equal(t0) {
		t.Fatalf("a smaller-quantity replay advanced the fence: %v → %v", t0, smaller)
	}

	// Growth: the fence advances.
	srv.NoteEntryExecution("MNQ", "Sim101", "sig-1", 3)
	_, _, grown, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ")
	if !grown.After(t0) {
		t.Fatalf("a grown observation must advance the fence: %v not after %v", grown, t0)
	}
}

// TestOrderUpdateProtectiveBracketDoesNotAdvanceEntryReceipt (CTO 2026-09-25,
// 09:34Z): the AddOn names brackets "<signal>-sl/-tp/-lx" and strips the suffix
// into signal_id. A PROTECTIVE (stop-loss) fill must NOT advance the entry
// fence — without the order-name filter, the flat positions frame that arrives
// just before the stop fill reads as STALE and GetPositions reports the closed
// position as open until the next snapshot. Positive control:
// TestOrderUpdateFrameNotesEntryReceipt (order_name == signal_id DOES advance).
func TestOrderUpdateProtectiveBracketDoesNotAdvanceEntryReceipt(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	// The bracket's own order name (suffix stripped into signal_id), terminal,
	// positive quantity — the shape that must be REFUSED as entry evidence.
	if err := writeFromMock(client, FrameOrderUpdate, OrderUpdatePayload{
		SignalID: "sig-1", OrderName: "sig-1-sl", State: "filled",
		Quantity: 1, Symbol: "MNQ", Account: "Sim101",
	}); err != nil {
		t.Fatalf("write order_update: %v", err)
	}

	// The fence must stay where it was (zero) for the whole observation window:
	// a protective fill is a REDUCTION of exposure, not entry execution.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, _, entry, _ := srv.PositionsForExecutionReceipt("Sim101", "MNQ"); !entry.IsZero() {
			t.Fatalf("a protective bracket fill (order_name %q ≠ signal_id %q) advanced the entry fence to %v — the pre-close flat snapshot would read stale and the closed position would read open", "sig-1-sl", "sig-1", entry)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
