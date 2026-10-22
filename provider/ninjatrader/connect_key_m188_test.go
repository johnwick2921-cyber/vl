package ninjatrader

import (
	"testing"
	"time"
)

// m188 B1/B1b — a keyed (friend-bot) connection must outlive the pre-auth
// firstHelloTimeout, and a client close must free the slot at once so the
// AddOn's reconnect is accepted. Before the fix the first-hello read deadline
// was never cleared after auth (6c6b0d418 removed the clear), so every keyed
// session died firstHelloTimeout after accept; and `defer s.connGone(c,
// pending)` captured pending==true, so the dead conn stayed in s.conn and the
// reconnect was rejected as a concurrent client.

// MUTANT: drop the SetReadDeadline(time.Time{}) after auth → RED (the authed
// conn stops receiving heartbeats past the timeout).
func TestM188KeyedConnOutlivesFirstHelloTimeout(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	shrinkFirstHelloTimeout(t, 400*time.Millisecond)
	shrinkHeartbeatInterval(t, 300*time.Millisecond)
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w.next(FrameHello, 3*time.Second); !ok {
		t.Fatal("no server hello for a matching connect key")
	}
	time.Sleep(1200 * time.Millisecond) // three times the pre-auth timeout
	if !s.IsConnected() {
		t.Fatal("the authenticated keyed conn died after firstHelloTimeout — the first-hello deadline was not cleared")
	}
	if _, ok := w.next(FrameHeartbeat, 2*time.Second); !ok {
		t.Fatal("the authenticated keyed conn stopped receiving frames after firstHelloTimeout")
	}
}

// MUTANT: restore `defer s.connGone(c, pending)` (value capture) → RED (the
// slot stays held by the dead conn and the reconnect is refused).
func TestM188KeyedReconnectAcceptedAfterClientClose(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	shrinkFirstHelloTimeout(t, 400*time.Millisecond)
	shrinkHeartbeatInterval(t, 5*time.Second) // no heartbeat write may free the slot for us
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w.next(FrameHello, 3*time.Second); !ok {
		t.Fatal("no server hello for a matching connect key")
	}
	_ = w.conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.IsConnected() {
		t.Fatal("a client close did not free the slot — the dead keyed conn is still s.conn")
	}
	w2 := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w2.next(FrameHello, 3*time.Second); !ok {
		t.Fatal("the reconnect after a client close was refused")
	}
}
