package ninjatrader

import (
	"testing"
	"time"
)

// TestIsFeedConnected_FreshBarOverridesStaleStatus locks the stale-status fix:
// IsFeedConnected default-allows before any status, honors "Connected", returns
// false on a non-"Connected" status with no recent bars, but OVERRIDES a stale
// "Disconnected" when a live bar_update arrived within feedFreshWindow (the bar
// stream is the real proof the feed is up; feed_status is edge-triggered and can
// latch on a momentary flap).
func TestIsFeedConnected_FreshBarOverridesStaleStatus(t *testing.T) {
	s := &TCPServer{}

	// 1. No status yet → DEFAULT-ALLOW (never false-halt at startup).
	if !s.IsFeedConnected() {
		t.Fatal("empty feed status must default-allow (true)")
	}
	// 2. Explicit "Connected" → true.
	s.feedStatus = "Connected"
	if !s.IsFeedConnected() {
		t.Fatal("Connected status must be true")
	}
	// 3. "Disconnected" + no bars → false (the legacy behavior, preserved).
	s.feedStatus = "Disconnected"
	if s.IsFeedConnected() {
		t.Fatal("Disconnected with no recent bars must be false")
	}
	// 4. "Disconnected" + a FRESH bar → override → true (the fix).
	s.lastBarNano.Store(time.Now().UnixNano())
	if !s.IsFeedConnected() {
		t.Fatal("a fresh bar_update must override a stale Disconnected (true)")
	}
	// 5. "Disconnected" + a STALE bar (older than the window) → false.
	s.lastBarNano.Store(time.Now().Add(-2 * feedFreshWindow).UnixNano())
	if s.IsFeedConnected() {
		t.Fatal("a stale bar (older than feedFreshWindow) must NOT override Disconnected")
	}
	// 6. Transient states (Connecting/Disconnecting) also honor the override.
	s.feedStatus = "Connecting"
	s.lastBarNano.Store(time.Now().UnixNano())
	if !s.IsFeedConnected() {
		t.Fatal("a fresh bar must override a non-Connected transient status too")
	}
}

// P0 feed freshness — the feed_status frame's additive "mode" is decoded and
// cached on the production path; an omitted mode stays "" (UNKNOWN) and is
// never fabricated as realtime.
func TestFeedStatusModeDecoded(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})

	send := func(mode string) {
		t.Helper()
		if err := WriteFrame(w.conn, FrameFeedStatus, FeedStatusPayload{
			PriceStatus: "Connected",
			Time:        time.Now().UTC().Format(time.RFC3339),
			Mode:        mode,
		}); err != nil {
			t.Fatal(err)
		}
	}

	send("delayed")
	waitFeedMode(t, s, "delayed")
	if got := s.FeedStatus(); got != "Connected" {
		t.Fatalf("FeedStatus = %q, want Connected", got)
	}

	send("realtime")
	waitFeedMode(t, s, "realtime")
}

// TestFeedStatusModeOmittedStaysUnknown — a legacy AddOn that omits "mode"
// leaves FeedMode() == "" (UNKNOWN); the code must not fabricate realtime.
func TestFeedStatusModeOmittedStaysUnknown(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})

	if err := WriteFrame(w.conn, FrameFeedStatus, FeedStatusPayload{
		PriceStatus: "Connected",
		Time:        time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.FeedStatus() == "Connected" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.FeedMode(); got != "" {
		t.Fatalf("FeedMode = %q, want \"\" (UNKNOWN) for an omitted mode", got)
	}
}

func waitFeedMode(t *testing.T, s *TCPServer, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.FeedMode() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("FeedMode = %q, want %q", s.FeedMode(), want)
}

// TestFeedModeInvalidatedOnDisconnect — the broker mode is session-scoped:
// a disconnect retires the old session's "delayed" evidence, and a replacement
// that omits mode reads UNKNOWN (no false recovery from the old session's
// receipt).
func TestFeedModeInvalidatedOnDisconnect(t *testing.T) {
	s := startedServer(t, nil)

	// Session A reports delayed.
	w := dialWire(t, s, HelloPayload{})
	if err := WriteFrame(w.conn, FrameFeedStatus, FeedStatusPayload{
		PriceStatus: "Connected",
		Time:        time.Now().UTC().Format(time.RFC3339),
		Mode:        "delayed",
	}); err != nil {
		t.Fatal(err)
	}
	waitFeedMode(t, s, "delayed")

	// Disconnect session A.
	_ = w.conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.FeedMode() == "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.FeedMode(); got != "" {
		t.Fatalf("after disconnect: FeedMode = %q, want \"\" (session-scoped)", got)
	}

	// Session B connects and omits mode → still UNKNOWN.
	w2 := dialWire(t, s, HelloPayload{})
	if err := WriteFrame(w2.conn, FrameFeedStatus, FeedStatusPayload{
		PriceStatus: "Connected",
		Time:        time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.FeedStatus() == "Connected" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.FeedMode(); got != "" {
		t.Fatalf("replacement omits mode: FeedMode = %q, want \"\" (no false recovery)", got)
	}
}
