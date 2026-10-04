package updaterbootstrap

// P0 test-isolation GUARD: the package's tests write the worker env and the
// last-authz files under throwaway homes (attended(t) isolates HOME + the
// userHomeDir seam). The shared internal/testhome guard snapshots the PROCESS
// home's ~/.config/vl-updater and ~/bin/vl-updater before the tests and fails
// the run if ANY file in them changed (created, deleted, content, or mtime) —
// the live box's worker credential and binary must survive every test run
// byte-identically. A test that forgets its isolation trips this guard
// (mutant-pinned: remove the Setenv in attended and the guard fails).

import (
	"os"
	"testing"

	"vl/internal/testhome"
	"vl/internal/testtmpfs"
)

func TestMain(m *testing.M) {
	// t.TempDir() on tmpfs (internal/testtmpfs); the guard still wraps m.Run.
	os.Exit(testtmpfs.Run(func() int { return testhome.Guard(m) }))
}
