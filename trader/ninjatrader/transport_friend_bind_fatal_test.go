package ninjatrader

import (
	"net"
	"os"
	"sync"
	"testing"
)

// U6 — in friend mode a cached NT bind error is FATAL (Restart=on-failure
// retries); the owner's box keeps the cached-error behavior. Driven at the
// production call site getOrStartTCPServer with a real listener collision.

func resetTCPServerSingleton() {
	tcpServerOnce = sync.Once{}
	tcpServerInst.Store(nil)
	tcpServerErr = nil
}

func TestU6FriendModeBindErrorIsFatal(t *testing.T) {
	// Hold a real address so Start's net.Listen fails deterministically.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold addr: %v", err)
	}
	defer ln.Close()

	prevAddr := os.Getenv("NT_TCP_LISTEN_ADDR")
	t.Setenv("NT_TCP_LISTEN_ADDR", ln.Addr().String())
	t.Cleanup(func() {
		os.Setenv("NT_TCP_LISTEN_ADDR", prevAddr)
		resetTCPServerSingleton()
	})

	prevFriend := friendModeEnabled
	prevFatal := bindErrorFatal
	t.Cleanup(func() { friendModeEnabled = prevFriend; bindErrorFatal = prevFatal })

	// Friend mode: bind error must route to the fatal seam.
	var fatalCalls int
	friendModeEnabled = func() bool { return true }
	bindErrorFatal = func(error) { fatalCalls++ }

	resetTCPServerSingleton()
	if _, err := getOrStartTCPServer(); err == nil {
		t.Fatalf("friend mode: expected a bind error")
	}
	if fatalCalls != 1 {
		t.Fatalf("friend mode: bindErrorFatal calls = %d, want 1", fatalCalls)
	}

	// Owner mode: bind error is CACHED, not fatal.
	fatalCalls = 0
	friendModeEnabled = func() bool { return false }

	resetTCPServerSingleton()
	if _, err := getOrStartTCPServer(); err == nil {
		t.Fatalf("owner mode: expected a bind error")
	}
	if fatalCalls != 0 {
		t.Fatalf("owner mode: bindErrorFatal calls = %d, want 0", fatalCalls)
	}
}
