package main

import (
	"fmt"
	"os"
)

// cmd/vl-friend — the friend's Windows app (S8). It is cross-built with
// GOOS=windows GOARCH=amd64 -trimpath -ldflags="-s -w". The install/update flow
// is split into small, unit-tested pieces (installcode.go, verify.go, ssig
// port, flatwait.go, flow.go); the Windows side effects (Tailscale, connect.json,
// the desktop shortcut, NinjaTrader) are wired in run_windows.go and proven
// end-to-end on a real Windows PC in B8.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vl-friend:", err)
		os.Exit(1)
	}
}
