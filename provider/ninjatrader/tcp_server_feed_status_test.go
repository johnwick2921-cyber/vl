// Feed-status receive-path integration test (TRACK A).
//
// Spins a real TCPServer + MockTCPClient, has the mock write feed_status frames,
// and asserts the server decodes them (no silent drop) and that IsFeedConnected
// gates correctly: DEFAULT-ALLOW before any frame, false on ConnectionLost, true
// again on Connected. Proves the Go receive path + gate semantics without NT8.
package ninjatrader

import (
	"context"
	"testing"
	"time"
)

func waitFeedStatus(t *testing.T, srv *TCPServer, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if srv.FeedStatus() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("FeedStatus = %q, want %q within %s", srv.FeedStatus(), want, d)
}

func TestTCPServer_FeedStatusReceivePath(t *testing.T) {
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// DEFAULT-ALLOW: before any feed_status frame, trading is not gated.
	if !srv.IsFeedConnected() {
		t.Error("IsFeedConnected should default true before any feed_status frame (no false-halt at startup)")
	}

	client := NewMockTCPClient(addr, 50*time.Millisecond)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client Start: %v", err)
	}
	defer client.Stop()
	waitForConnected(t, srv, 2*time.Second)

	// ConnectionLost → gate trips.
	if err := writeFromMock(client, FrameFeedStatus, FeedStatusPayload{PriceStatus: "ConnectionLost", Time: "2026-06-02T03:06:05Z"}); err != nil {
		t.Fatalf("write feed_status ConnectionLost: %v", err)
	}
	waitFeedStatus(t, srv, "ConnectionLost", 2*time.Second)
	if srv.IsFeedConnected() {
		t.Error("IsFeedConnected should be false on ConnectionLost")
	}

	// Connected → gate clears.
	if err := writeFromMock(client, FrameFeedStatus, FeedStatusPayload{PriceStatus: "Connected", Time: "2026-06-02T03:06:06Z"}); err != nil {
		t.Fatalf("write feed_status Connected: %v", err)
	}
	waitFeedStatus(t, srv, "Connected", 2*time.Second)
	if !srv.IsFeedConnected() {
		t.Errorf("IsFeedConnected should be true on Connected; status=%q", srv.FeedStatus())
	}
}

// TestFrameFeedStatus_WireString guards the wire type string so the Go const and
// the C# WriteEnvelope("feed_status", …) cannot drift.
func TestFrameFeedStatus_WireString(t *testing.T) {
	if got := string(FrameFeedStatus); got != "feed_status" {
		t.Errorf("FrameFeedStatus = %q, want %q (must match C# WriteEnvelope)", got, "feed_status")
	}
}
