package testhome

import (
	"os"
	"path/filepath"
	"testing"
)

// ROUND 3 (DS-106): listing is depth-1 of the listed dirs. A new file written
// at DEPTH 2 under a listed dir — inside a subdirectory that already exists —
// changes no depth-1 name and is not fingerprinted, so the guard returns 0.
// This test pre-creates ~/.cache/mozilla (a normal existing subdir) and writes
// ~/.cache/mozilla/newfile during Run; it FAILS today (Run returns 0).
func TestGuardDetectsANestedFileUnderAListedDir(t *testing.T) {
	// RESIDUAL by CTO decision 2026-10-17: a full-depth fingerprint of the real home
	// (~11M files) is not affordable per package, and dir-mtime watching false-alarms
	// on concurrent writers (gopls, gh, the pinned GOCACHE). The backstop is the
	// PREVENTION half — HOME/XDG point at a throwaway folder for every test, and the
	// lint forbids os/user — so a nested write needs a literal real-home path.
	// Kept as a documented, skipped probe so the gap stays visible.
	t.Skip("documented residual: depth-2 writes under a listed dir (see comment)")
	fakeHome := t.TempDir()
	sub := filepath.Join(fakeHome, ".cache", "mozilla")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	old := realHomeFn
	realHomeFn = func() string { return fakeHome }
	t.Cleanup(func() { realHomeFn = old })

	code := Run(func() int {
		if err := os.WriteFile(filepath.Join(sub, "newfile.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code == 0 {
		t.Fatal("HOLE: the guard returned 0 while a NEW file was written at depth 2 under a listed dir (~/.cache/mozilla/newfile)")
	}
}
