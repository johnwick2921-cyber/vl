package main

// THE ONE RULE guard (owner order 2026-10-17, after the 2026-10-16 wipe of
// /home/hoang): this package's tests run with HOME pointed at a throwaway
// folder, and the run FAILS if anything in the real home folder was deleted
// or an updater path changed. internal/homeguard fails the build when a test
// package is missing this TestMain.

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

func TestMain(m *testing.M) {
	os.Exit(testhome.Guard(m))
}
