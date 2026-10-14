// Package testnet is the shared test-isolation guard that keeps tests OFF the
// live bot's API port. A package installs it in its TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testnet.Guard(m)) }
//
// Guard wraps http.DefaultTransport's dialer for the run: a dial to the LIVE
// bot port (LiveBotPort, 8080) on any loopback host is REFUSED (the request
// fails with a named error) and RECORDED, and the run fails after m.Run even
// when the code under test swallowed the error — the Telegram agent turns an
// HTTP error into a reply string, which is exactly how TestAgentDirectReply
// sent four real requests to the production bot on every `go test` (release
// #22 OPS-SAFETY; 236 "invalid number of segments" 401 lines in
// vl_2026-10-13.log, bursts of 4, the last at 02:03:27 10-14).
//
// Pair it with the repo census (TestNoTestNamesTheLiveBotPort in this
// package), which catches a test that builds its own transport.
package testnet

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
)

// LiveBotPort is the production bot's API port (config.APIServerPort default).
const LiveBotPort = 8080

var (
	mu       sync.Mutex
	attempts []string
)

// IsLiveBotAddr reports whether addr ("host:port") is the live bot's API on a
// loopback host.
func IsLiveBotAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if p, err := strconv.Atoi(port); err != nil || p != LiveBotPort {
		return false
	}
	if host == "localhost" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// Attempts returns the refused dials recorded so far (tests of the guard).
func Attempts() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), attempts...)
}

// Install wraps http.DefaultTransport for the current process and returns the
// restore func. Guard calls it; a guard test calls it directly.
func Install() (restore func()) {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return func() {}
	}
	prev := tr.DialContext
	base := prev
	if base == nil {
		base = (&net.Dialer{}).DialContext
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if IsLiveBotAddr(addr) {
			mu.Lock()
			attempts = append(attempts, network+" "+addr)
			mu.Unlock()
			return nil, fmt.Errorf("testnet: a TEST dialed the LIVE bot API %s — point it at an httptest server", addr)
		}
		return base(ctx, network, addr)
	}
	return func() {
		tr.DialContext = prev
		mu.Lock()
		attempts = nil
		mu.Unlock()
	}
}

// Guard runs the package's tests with the live-port dial refused, and fails
// the run (exit 1) if any test tried it.
func Guard(m *testing.M) int {
	restore := Install()
	defer restore()
	code := m.Run()
	if got := Attempts(); len(got) > 0 {
		fmt.Fprintf(os.Stderr, "testnet: FAIL — %d dial(s) to the LIVE bot port %d from this package's tests: %v\n", len(got), LiveBotPort, got)
		if code == 0 {
			code = 1
		}
	}
	return code
}
