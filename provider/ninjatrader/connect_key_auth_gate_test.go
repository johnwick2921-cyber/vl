package ninjatrader

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// K1 — connect-key AUTH GATE. With NT_CONNECT_KEY set, nothing may reach the
// wire (signals, closes, bar subscriptions, heartbeats, post-accept sends) and
// nothing may be PROCESSED from the peer until the peer sends a hello carrying
// the matching key. A peer that never says hello is closed after
// firstHelloTimeout so it cannot squat the single client slot.
//
// The owner's box (no key) is byte-identical to today: the legacy tolerance
// (data before hello → warn once, keep going) is untouched by these tests.

// rawDial connects WITHOUT sending a hello, so the peer sits in the
// pre-auth (pending) state that K1/GATE 3 must fence off. It does NOT wait for
// IsConnected(): a pending conn is deliberately never "connected".
func rawDial(t *testing.T, s *TCPServer) net.Conn {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 100; i++ {
		c, err = net.Dial("tcp", s.ListenAddrForTest().String())
		if err == nil {
			break
		}
		if c != nil {
			_ = c.Close()
			c = nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("rawDial: never connected")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// expectClosed asserts the server CLOSED the connection (EOF / reset) rather
// than leaving it open (which would surface as a read timeout).
func expectClosed(t *testing.T, c net.Conn, d time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	if _, err := ReadFrame(c); err == nil {
		t.Fatal("expected the server to close the connection, but a frame arrived")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("expected the connection to be CLOSED, but it is still open (read timed out after %s)", d)
	}
}

// expectSilent asserts the peer received NOTHING within d (open but silent).
func expectSilent(t *testing.T, c net.Conn, d time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	if _, err := ReadFrame(c); err == nil {
		t.Fatal("unauthenticated peer received a frame; want nothing on the wire")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		// open but silent — exactly what K1 requires before auth.
	} else {
		t.Fatalf("expected an open, silent connection (read timeout), got %v", err)
	}
}

func shrinkFirstHelloTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := firstHelloTimeout
	firstHelloTimeout = d
	t.Cleanup(func() { firstHelloTimeout = old })
}

// TestConnectKeyNoHelloBarUpdateClosedAndNotProcessed — a bar_update before any
// hello is BOTH closed and not processed (the freshness stamp must not move).
//
// MUTANT: drop the pre-auth close in readLoop (or let the legacy warn path run
// in keyed mode) → the bar reaches enqueueBarUpdate → lastBarNano moves → RED.
func TestConnectKeyNoHelloBarUpdateClosedAndNotProcessed(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)

	// Register the symbol so a processed bar_update WOULD pass the split-brain
	// defense and reach enqueueBarUpdate. The write side fails (unauthed), which
	// is fine — registration is the side effect we need.
	if err := s.SubscribeBarsSymbol("MNQ"); err == nil {
		t.Fatal("subscribe on an unauthenticated peer should fail, got nil")
	}

	if err := WriteFrame(c, FrameBarUpdate, BarUpdatePayload{
		Symbol:    "MNQ",
		Timeframe: "1m",
		Contract:  "MNQ 06-26",
		Bars:      []Bar{{T: time.Now().UnixMilli(), O: 1, H: 2, L: 0.5, C: 1.5}},
	}); err != nil {
		t.Fatalf("write bar_update: %v", err)
	}

	// Closed, not just warned.
	expectClosed(t, c, 3*time.Second)

	// Not processed: the live-feed freshness stamp must still be zero.
	if got := s.lastBarNano.Load(); got != 0 {
		t.Fatalf("bar_update before hello was PROCESSED (lastBarNano=%d), want 0", got)
	}
}

// TestConnectKeyNoHelloSendSignalNothingWritten — SendSignal on an unauthenticated
// peer writes NOTHING to the socket (the signal may queue, but no frame may
// reach a peer that never proved the key).
//
// MUTANT: let a writer read s.conn directly instead of connectedAuthed() → a
// FrameSignal reaches the peer → RED.
func TestConnectKeyNoHelloSendSignalNothingWritten(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)

	err := s.SendSignal(SignalPayload{
		Symbol:     "MNQ",
		Side:       "long",
		Quantity:   1,
		Entry:      100,
		StopLoss:   99,
		TakeProfit: 101,
		SignalID:   "k1-test-signal",
		Timestamp:  time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("SendSignal returned %v, want nil (queued, not flushed)", err)
	}

	expectSilent(t, c, 700*time.Millisecond)
}

// TestConnectKeySilentPeerClosedThenRealAddOnConnects — a peer that connects and
// says nothing is closed at firstHelloTimeout, freeing the single client slot so
// the real AddOn can connect immediately after.
//
// MUTANT: remove the first-hello read deadline (or the timeout close) → the
// silent peer holds the slot, the real AddOn is rejected as a concurrent client
// → RED.
func TestConnectKeySilentPeerClosedThenRealAddOnConnects(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	shrinkFirstHelloTimeout(t, 150*time.Millisecond)
	s := startedServer(t, nil)

	silent := rawDial(t, s)
	expectClosed(t, silent, 3*time.Second)

	// The slot is now free: the real AddOn (with the right key) connects.
	w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w.next(FrameHello, 2*time.Second); !ok {
		t.Fatal("real AddOn did not get a hello reply after the silent peer was closed")
	}
	if got := s.ConnectKeyRefused(); got != 0 {
		t.Fatalf("silent peer was counted as a key refusal (%d), want 0 (it never sent a hello)", got)
	}
}

// sendWrongHelloAndWaitRefusal dials, sends a wrong-key hello, and blocks until
// the refusal counter moves past `before` (the readLoop handles it async).
func sendWrongHelloAndWaitRefusal(t *testing.T, s *TCPServer, before int64) {
	t.Helper()
	c := rawDial(t, s)
	if err := WriteFrame(c, FrameHello, HelloPayload{ProtocolVersion: ProtocolVersion, Source: "vltrader-addon", ConnectKey: "wrong-key"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectKeyRefused() > before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("refusal was never counted")
}

// TestConnectKeyRefusedWarnRateLimited — U21: the "connect_key refused" WARN is
// logged at most once per peer per minute, while the counter still records every
// refusal. A wrong-key AddOn reconnecting every 5 s increments the counter 12×
// but writes ONE warn line.
//
// MUTANT: log unconditionally (drop logPreAuthRefused) → the warn fires twice
// here → RED.
func TestConnectKeyRefusedWarnRateLimited(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	var buf syncBuffer
	s := NewTCPServer(slog.New(slog.NewTextHandler(&buf, nil)))
	s.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })

	sendWrongHelloAndWaitRefusal(t, s, 0)
	sendWrongHelloAndWaitRefusal(t, s, 1)

	if got := s.ConnectKeyRefused(); got != 2 {
		t.Fatalf("refusal counter = %d, want 2 (counts every refusal)", got)
	}
	if n := strings.Count(buf.String(), "connect_key refused"); n != 1 {
		t.Fatalf("warn logged %d times, want 1 (rate-limited to once per peer per minute)", n)
	}
}

// ── GATE 3 root-fix tests: a pre-auth (pending) conn is never s.conn ─────────

func shrinkHeartbeatInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := TCPHeartbeatInterval
	TCPHeartbeatInterval = d
	t.Cleanup(func() { TCPHeartbeatInterval = old })
}

// syncBuffer is a mutex-guarded bytes.Buffer for capturing slog output: the
// server's logger writes from several goroutines while the test reads, and a
// plain bytes.Buffer races under -race.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestConnectKeyPendingPeerIsNotConnectedAndRecordUnchanged — a pending (pre-auth)
// peer must read IsConnected()==false, and the connection record must not have
// started (AcceptSeq 0): no beginConnectionRecord before auth.
//
// MUTANT: publish the pending conn as s.conn at accept → IsConnected()==true →
// RED.
func TestConnectKeyPendingPeerIsNotConnectedAndRecordUnchanged(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)
	// Give the accept loop a moment to register the pending conn.
	time.Sleep(50 * time.Millisecond)

	if s.IsConnected() {
		t.Fatal("a pre-auth pending peer must read IsConnected()==false")
	}
	rec, ok := s.ConnectionRecord()
	if ok {
		t.Fatal("ConnectionRecord ok=true for a pending peer; want false (no live authed conn)")
	}
	if rec.AcceptSeq != 0 {
		t.Fatalf("AcceptSeq = %d for a pending peer; want 0 (record starts only at auth)", rec.AcceptSeq)
	}
	_ = c
}

// TestConnectKeyPendingPeerNoMaintenanceFrame — an active maintenance hold + a
// pending peer must send NO maintenance frame (the hold is only pushed to an
// authed conn).
func TestConnectKeyPendingPeerNoMaintenanceFrame(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	fastMaintenanceTimers(t)
	src := &holdSrc{}
	src.set(true, "job-gate3")
	s := startedServer(t, src)

	c := rawDial(t, s)
	// The recheck timer fires several times while the peer stays pending.
	expectSilent(t, c, 400*time.Millisecond)
}

// TestConnectKeyPendingPeerNoAllowlist — RegisterBoundAccount with a pending peer
// must send no allowlist frame.
func TestConnectKeyPendingPeerNoAllowlist(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)
	s.RegisterBoundAccount("Sim101")
	expectSilent(t, c, 400*time.Millisecond)
}

// TestConnectKeyPendingPeerNoBarsResubscribe — resendBarsSubscribe with a pending
// peer must send nothing and report not-connected.
func TestConnectKeyPendingPeerNoBarsResubscribe(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)
	if _, err := s.resendBarsSubscribe("MNQ"); err == nil {
		t.Fatal("resendBarsSubscribe on a pending peer must fail (not connected)")
	}
	expectSilent(t, c, 400*time.Millisecond)
}

// TestConnectKeyLoopingKeylessRedialRealAddOnConnects — a looping keyless dial
// keeps replacing the pending slot, but the real AddOn (right key) still connects
// because an authed incumbent is never preempted.
func TestConnectKeyLoopingKeylessRedialRealAddOnConnects(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	// A keyless peer keeps dialing (each new dial preempts the previous pending).
	for i := 0; i < 5; i++ {
		c := rawDial(t, s)
		_ = c
		time.Sleep(20 * time.Millisecond)
	}

	w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w.next(FrameHello, 2*time.Second); !ok {
		t.Fatal("real AddOn did not connect after keyless redials")
	}
}

// TestConnectKeyPreemptedPendingConnIsClosed — when a new dial preempts a waiting
// pending conn, the OLD pending conn is actually CLOSED (fd/goroutine hygiene):
// its readLoop unblocks and exits, so a looping keyless peer cannot accumulate
// goroutines. DS-107's minor note.
//
// MUTANT: stop closing the old pending conn on preemption → the first conn stays
// open → expectClosed times out → RED.
func TestConnectKeyPreemptedPendingConnIsClosed(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	first := rawDial(t, s)
	time.Sleep(30 * time.Millisecond) // let the accept loop register the first pending conn
	second := rawDial(t, s)           // this preempts the first pending conn

	// The preempted conn must have been CLOSED by the server.
	expectClosed(t, first, 3*time.Second)

	// The replacement is still the pending conn (not yet authed).
	if s.IsConnected() {
		t.Fatal("the replacement pending peer must still read IsConnected()==false")
	}
	_ = second
}

// TestConnectKeySignalQueuedBeforeAuthDeliveredAfterAuth — a signal queued while
// the peer is pending IS delivered once the peer authenticates (flush runs at
// auth, not before).
func TestConnectKeySignalQueuedBeforeAuthDeliveredAfterAuth(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	c := rawDial(t, s)
	if err := s.SendSignal(SignalPayload{
		Symbol:     "MNQ",
		Side:       "long",
		Quantity:   1,
		Entry:      100,
		StopLoss:   99,
		TakeProfit: 101,
		SignalID:   "k1-pre-auth-signal",
		Timestamp:  time.Now().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SendSignal (pre-auth) returned %v, want nil (queued)", err)
	}

	// Authenticate the SAME conn.
	if err := WriteFrame(c, FrameHello, HelloPayload{ProtocolVersion: ProtocolVersion, Source: "vltrader-addon", ConnectKey: testConnectKey}); err != nil {
		t.Fatal(err)
	}

	// The flush at auth delivers the queued signal.
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		env, err := ReadFrame(c)
		if err != nil {
			t.Fatalf("read after auth: %v (signal was not delivered)", err)
		}
		if env.Type == FrameSignal {
			return
		}
	}
}

// TestConnectKeyNoHeartbeatBeforeAuth — a pending peer gets no heartbeat (the
// heartbeat loop starts only at auth), even after several heartbeat intervals.
func TestConnectKeyNoHeartbeatBeforeAuth(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	shrinkHeartbeatInterval(t, 40*time.Millisecond)
	s := startedServer(t, nil)

	c := rawDial(t, s)
	expectSilent(t, c, 250*time.Millisecond)
}

// TestConnectKeyCheckBeforeVersion — a hello with a wrong key AND a wrong version
// is refused as a KEY refusal (counter increments, no version-mismatch log), i.e.
// the key check runs BEFORE the protocol-version check.
func TestConnectKeyCheckBeforeVersion(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	var buf syncBuffer
	s := NewTCPServer(slog.New(slog.NewTextHandler(&buf, nil)))
	s.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })

	c := rawDial(t, s)
	if err := WriteFrame(c, FrameHello, HelloPayload{ProtocolVersion: 999, Source: "vltrader-addon", ConnectKey: "wrong-key"}); err != nil {
		t.Fatal(err)
	}

	// Wait for the refusal to be counted.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && s.ConnectKeyRefused() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.ConnectKeyRefused(); got != 1 {
		t.Fatalf("ConnectKeyRefused = %d, want 1 (key check runs before version)", got)
	}
	if strings.Contains(buf.String(), "PROTOCOL VERSION MISMATCH") {
		t.Fatalf("version mismatch logged before the key check; the key must be checked FIRST:\n%s", buf.String())
	}
}
