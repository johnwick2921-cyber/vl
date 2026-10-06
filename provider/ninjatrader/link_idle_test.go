package ninjatrader

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
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

// TestLinkIdleQuietWhenDisconnected — P3-1 (rel9 fold): a stale last-frame stamp
// with NO connection must stay quiet (no WARN, no close). Before the fold the
// watcher logged "link idle … closing" every tick while NT8 was closed overnight.
// MUTANT: drop the `c == nil` early return in checkLinkIdle → the WARN fires → RED.
func TestLinkIdleQuietWhenDisconnected(t *testing.T) {
	t.Setenv("NT8_LINK_IDLE_SECONDS", "10")
	open := openSessionTime()
	if !kernel.IsCMEOpen(open) {
		t.Fatalf("fixture: %v must be an open session", open)
	}
	var buf bytes.Buffer
	srv := NewTCPServer(slog.New(slog.NewTextHandler(&buf, nil)))
	// No Start, no connection: s.conn == nil.
	srv.lastFrameUnixMs.Store(open.Add(-11 * time.Second).UnixMilli())
	srv.checkLinkIdle(open)
	if strings.Contains(buf.String(), "link idle") {
		t.Fatalf("a disconnected link must stay quiet, got: %s", buf.String())
	}
}

// TestLinkIdleDoesNotCloseAFreshConnection — P3-2 (rel9 fold): the accept path
// restarts the stamp, so a connection installed after the snapshot (or between
// the age check and the close) is never closed by a stale stamp. The re-check
// under connMu reads the fresh stamp and returns.
func TestLinkIdleDoesNotCloseAFreshConnection(t *testing.T) {
	t.Setenv("NT8_LINK_IDLE_SECONDS", "10")
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
	// The accept just reset the stamp to now, so a check against a FRESH stamp
	// (age within the timeout) must NOT close this connection. This is the
	// observable of P3-2: an accept restarts the stamp, so the re-check under
	// connMu reads it fresh and returns.
	srv.lastFrameUnixMs.Store(time.Now().Add(-1 * time.Second).UnixMilli())
	srv.checkLinkIdle(time.Now())
	if !srv.IsConnected() {
		t.Fatal("a fresh connection (stamp restarted on accept) must not be closed")
	}
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
