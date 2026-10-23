// M11 pin (REVIEW-312 r3, P2-1): the N9 read-loop drop. With the regime OFF a
// cancel-report echo must be dropped AT THE READ LOOP, before any consumer
// (NoteEntryExecution, the ordered worker, the picture-HTF subscriber) can see
// it. Remove the drop in tcp_server.go and the subscriber below receives the
// echo → RED. The ON control proves the same frame passes when the regime is
// on.

package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func TestCancelReportEchoDroppedAtTheReadLoopWhenRegimeOFF(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()
	sub := srv.SubscribeOrderUpdatesFor("MNQ", "Sim101")

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	if err := writeFromMock(client, FrameOrderUpdate, OrderUpdatePayload{
		SignalID: "sig-echo", OrderName: "sig-echo", State: "cancelled",
		CancelReport: true, Symbol: "MNQ", Account: "Sim101",
	}); err != nil {
		t.Fatalf("write echo: %v", err)
	}
	select {
	case u := <-sub:
		t.Fatalf("the regime-OFF read loop must drop the cancel-report echo, got %+v", u)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestCancelReportEchoPassesWhenRegimeON(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "1")
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()
	sub := srv.SubscribeOrderUpdatesFor("MNQ", "Sim101")

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	if err := writeFromMock(client, FrameOrderUpdate, OrderUpdatePayload{
		SignalID: "sig-echo", OrderName: "sig-echo", State: "cancelled",
		CancelReport: true, Symbol: "MNQ", Account: "Sim101",
	}); err != nil {
		t.Fatalf("write echo: %v", err)
	}
	select {
	case u := <-sub:
		if !u.CancelReport {
			t.Fatalf("the regime-ON echo must pass with cancel_report set, got %+v", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the regime-ON echo must reach the subscriber")
	}
}
