package addonbuild

// THE ONE RULE guard: every test package mounts the shared home guard, which
// snapshots the process home before the tests and fails the run if anything
// under it changed — the live box's NT8 folder and credentials must survive
// every test run byte-identically. This package's tests write only to
// worker-made temp dirs and a fake Run; the guard enforces it.

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

func TestMain(m *testing.M) {
	os.Exit(testhome.Guard(m))
}
