package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// cmd/vl-friend — the friend's Windows app (S8). It is cross-built with
// GOOS=windows GOARCH=amd64. The install/update flow is split into small,
// unit-tested pieces (installcode.go, verify.go, flatwait.go); the Windows
// side effects (Tailscale, connect.json, the desktop shortcut, NinjaTrader) are
// wired here and proven end-to-end on a real Windows PC in B8.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vl-friend:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_ = ctx
	_ = args
	return fmt.Errorf("vl-friend: install/update flow not yet wired (S8 in progress)")
}
