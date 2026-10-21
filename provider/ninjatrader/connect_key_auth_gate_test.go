package ninjatrader

import (
	"net"
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
// pre-auth state that K1 must fence off.
func rawDial(t *testing.T, s *TCPServer) net.Conn {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 100; i++ {
		c, err = net.Dial("tcp", s.ListenAddrForTest().String())
		if err == nil && waitConnected(s, 20*time.Millisecond) {
			break
		}
		if c != nil {
			_ = c.Close()
			c = nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("rawDial: never became the connected client")
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
