//go:build !windows

package main

import "fmt"

// run_other.go — the friend app is Windows-only (GOOS=windows). This stub keeps
// `go build ./...` and the Linux test suite green; the pure logic (install
// code, verify, flat-wait, flow) is tested directly on Linux.
func run(args []string) error {
	return fmt.Errorf("vl-friend runs on Windows only (build with GOOS=windows)")
}
