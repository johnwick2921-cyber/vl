// Position-history fix — position_close receive-path integration test.
//
// Spins a real TCPServer + MockTCPClient, has the mock write a position_close
// frame, and asserts the server delivers it on ClosedPositions(). Proves the
// END-TO-END Go receive path (read → decode → channel) without NT8. Mirrors
// tcp_server_account_test.go.
package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func TestTCPServer_PositionCloseReceivePath(t *testing.T) {
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

	want := PositionClosePayload{
		SignalID:     "abc-123",
		Symbol:       "MNQ",
		PositionSide: "long",
		ExitPrice:    30359.0,
		Quantity:     1,
		ExitReason:   "tp",
		ExitTime:     "2026-05-29T04:50:00Z",
	}
	if err := writeFromMock(client, FramePositionClose, want); err != nil {
		t.Fatalf("write position_close: %v", err)
	}

	select {
	case got := <-srv.ClosedPositions():
		if got != want {
			t.Errorf("ClosedPositions delivered %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("position_close never delivered on ClosedPositions()")
	}
}
