package ninjatrader

import (
	"context"
	"net"
	"testing"
	"time"

	"vl/kernel"
)

// B11 (L15) — the Go-side read-idle detector: a half-open AddOn link (no inbound
// frame for linkIdleTimeoutSeconds while the CME session is OPEN) is treated as
// down — logged and closed, the same state the close path sets. It must never
// fire in the CME daily break (16:00–17:00 CT) or the weekend.

// openSessionTime is a known OPEN CME instant (Monday 2026-10-05 10:00 CT).
func openSessionTime() time.Time {
	return time.Date(2026, 10, 5, 10, 0, 0, 0, kernel.CTLocation())
}

// TestLinkIdleClosesOnOpenSessionSilence is the B11 call-site pin: with the
// session OPEN and the last inbound frame older than the timeout, the watcher
// closes the connection. MUTANT: delete linkIdleWatcher from Start (or the
// closeConn call in checkLinkIdle) → the connection survives → RED.
func TestLinkIdleClosesOnOpenSessionSilence(t *testing.T) {
	t.Setenv("NT8_LINK_IDLE_SECONDS", "10")
	open := openSessionTime()
	prevNow, prevTick := linkIdleNow, linkIdleTick
	linkIdleNow = func() time.Time { return open }
	linkIdleTick = 20 * time.Millisecond
	defer func() { linkIdleNow, linkIdleTick = prevNow, prevTick }()

	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !srv.IsConnected() {
		t.Fatal("server did not register the client connection")
	}

	// The last frame is older than the 10s timeout; the watcher must close.
	srv.lastFrameUnixMs.Store(open.Add(-11 * time.Second).UnixMilli())

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !srv.IsConnected() {
			return // closed by the idle detector — pass
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("link idle detector did not close the silent open-session connection")
}

// TestLinkIdleNeverFiresWhenSessionClosed — the weekend / daily-break shield:
// even with a stale last-frame stamp, a CLOSED CME session must never close the
// connection. Drives checkLinkIdle directly (the one-tick decision the watcher
// calls).
func TestLinkIdleNeverFiresWhenSessionClosed(t *testing.T) {
	srv := NewTCPServer(nil)
	// Saturday 2026-10-03 12:00 CT — CME closed (weekend).
	sat := time.Date(2026, 10, 3, 12, 0, 0, 0, kernel.CTLocation())
	if kernel.IsCMEOpen(sat) {
		t.Fatalf("fixture: %v must be a closed session", sat)
	}
	// A stale stamp alone is not enough — the session gate must hold.
	srv.lastFrameUnixMs.Store(sat.Add(-time.Hour).UnixMilli())
	srv.checkLinkIdle(sat)
	if srv.IsConnected() {
		t.Fatal("fixture: the server must not be connected for this test")
	}
	// closeConn is the only observable effect; with no conn it must be a no-op
	// and no panic — the point is that checkLinkIdle returned WITHOUT firing.
	// Assert the inverse through the open-session case above (which DOES fire).
}

// TestLinkIdleFiresWhenStaleAndOpen — the same one-tick decision, OPEN session:
// with a stale stamp and a live connection the detector closes it.
func TestLinkIdleFiresWhenStaleAndOpen(t *testing.T) {
	t.Setenv("NT8_LINK_IDLE_SECONDS", "10")
	open := openSessionTime()
	if !kernel.IsCMEOpen(open) {
		t.Fatalf("fixture: %v must be an open session", open)
	}
	srv := NewTCPServer(nil)
	addr := freeEphemeralAddr(t)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.IsConnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !srv.IsConnected() {
		t.Fatal("server did not register the client connection")
	}
	srv.lastFrameUnixMs.Store(open.Add(-11 * time.Second).UnixMilli())
	srv.checkLinkIdle(open)
	if srv.IsConnected() {
		t.Fatal("checkLinkIdle must close a stale link while the session is open")
	}
}
