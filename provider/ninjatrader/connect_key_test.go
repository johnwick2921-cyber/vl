package ninjatrader

import (
	"strings"
	"testing"
	"time"
)

// W-FRIEND-HOSTED S4 — the bot checks the connect key on the hello, before any
// signal, allowlist or bar subscription is sent to the peer.

// testConnectKey is a 64-hex connect key (16 hex chars repeated 4×), built at
// runtime so the source carries no 64-char hex literal for a secret scanner to
// flag.
var testConnectKey = strings.Repeat("0123456789abcdef", 4)

// drainAll reads every frame the peer received until the connection closes (or
// d elapses). A refused connection must yield NOTHING.
func drainAll(w *wireClient, d time.Duration) int {
	n := 0
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-w.frames:
			if !ok {
				return n
			}
			n++
		case <-deadline:
			return n
		}
	}
}

// TestConnectKeyWrongAndMissingRefused — a missing or wrong key is refused:
// the connection is closed, the refusal is counted, and not a single frame
// (no hello reply, no allowlist, no queued signal, no bars subscribe) is sent.
//
// MUTANT: skip the key check (or run the post-accept sends in the accept loop
// even when a key is set) → a frame arrives → this test goes RED.
func TestConnectKeyWrongAndMissingRefused(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	w := dialWireNoWait(t, s, HelloPayload{})
	if n := drainAll(w, 700*time.Millisecond); n != 0 {
		t.Fatalf("missing key: the server sent %d frame(s), want 0 (nothing before the key check)", n)
	}
	if got := s.ConnectKeyRefused(); got != 1 {
		t.Fatalf("missing key: ConnectKeyRefused = %d, want 1", got)
	}

	w2 := dialWireNoWait(t, s, HelloPayload{ConnectKey: "wrong-key"})
	if n := drainAll(w2, 700*time.Millisecond); n != 0 {
		t.Fatalf("wrong key: the server sent %d frame(s), want 0", n)
	}
	if got := s.ConnectKeyRefused(); got != 2 {
		t.Fatalf("wrong key: ConnectKeyRefused = %d, want 2", got)
	}
}

// TestConnectKeyAccepted — the right key connects and is not refused.
func TestConnectKeyAccepted(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", testConnectKey)
	s := startedServer(t, nil)

	w := dialWire(t, s, HelloPayload{ConnectKey: testConnectKey})
	if _, ok := w.next(FrameHello, 2*time.Second); !ok {
		t.Fatal("right key: no hello reply, want connected")
	}
	if got := s.ConnectKeyRefused(); got != 0 {
		t.Fatalf("right key: ConnectKeyRefused = %d, want 0", got)
	}
}

// TestConnectKeyUnsetIsToday — no key set = the owner's box: today's behaviour,
// byte-identical (the hello reply arrives, nothing is refused or counted).
func TestConnectKeyUnsetIsToday(t *testing.T) {
	s := startedServer(t, nil)

	w := dialWire(t, s, HelloPayload{})
	if _, ok := w.next(FrameHello, 2*time.Second); !ok {
		t.Fatal("unset key: no hello reply, want today's behaviour")
	}
	if got := s.ConnectKeyRefused(); got != 0 {
		t.Fatalf("unset key: ConnectKeyRefused = %d, want 0", got)
	}
}
