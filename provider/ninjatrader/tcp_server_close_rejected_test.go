// Rejected-flatten receive-path integration test.
//
// Spins a real TCPServer + MockTCPClient, has the mock write a
// position_close_rejected frame, and asserts the server decodes it and delivers
// it on CloseRejections() (NOT silently dropped as an "unknown frame type").
// Proves the END-TO-END Go receive path (read → decode → channel) for the new
// frame without NT8. Mirrors tcp_server_close_test.go.
package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func TestTCPServer_PositionCloseRejectedReceivePath(t *testing.T) {
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

	want := PositionCloseRejectedPayload{
		SignalID:     "Close",
		Symbol:       "MNQ",
		PositionSide: "long",
		Reason:       "There is no market data available to drive the simulation engine.",
		Account:      "Sim101",
		RejectTime:   "2026-06-02T03:08:03Z",
	}
	if err := writeFromMock(client, FramePositionCloseRejected, want); err != nil {
		t.Fatalf("write position_close_rejected: %v", err)
	}

	select {
	case got := <-srv.CloseRejections():
		if got != want {
			t.Errorf("CloseRejections delivered %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("position_close_rejected never delivered on CloseRejections() — frame decode missing (silent drop)")
	}
}

// TestFramePositionCloseRejected_WireString guards the wire type string so the
// Go const and the C# WriteEnvelope("position_close_rejected", …) cannot drift.
func TestFramePositionCloseRejected_WireString(t *testing.T) {
	if got := string(FramePositionCloseRejected); got != "position_close_rejected" {
		t.Errorf("FramePositionCloseRejected = %q, want %q (must match C# WriteEnvelope)", got, "position_close_rejected")
	}
}
