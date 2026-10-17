package testhome

import (
	"os"
	"path/filepath"
	"testing"
)

// The guard's own proof (owner order 2026-10-17): with the "real" home
// pointed at a throwaway folder, a run that deletes a top-level entry, an
// updater file, or the whole folder must return rc 1 even when every test
// passed; an untouched run keeps the tests' own code; and inside the run HOME
// is NOT the real home.
func fakeHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	for _, d := range []string{"vl", "nofx", ".config/vl-updater", "bin"} {
		if err := os.MkdirAll(filepath.Join(h, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(h, ".config/vl-updater/env"), []byte("TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := realHomeFn
	realHomeFn = func() string { return h }
	t.Cleanup(func() { realHomeFn = prev })
	return h
}

func TestGuardKeepsTheTestsCodeWhenTheHomeIsUntouched(t *testing.T) {
	fakeHome(t)
	if rc := Run(func() int { return 0 }); rc != 0 {
		t.Fatalf("untouched home: rc=%d, want 0", rc)
	}
	if rc := Run(func() int { return 3 }); rc != 3 {
		t.Fatalf("a failing test run must keep its own rc: got %d", rc)
	}
}

func TestGuardFailsWhenATopLevelHomeEntryIsDeleted(t *testing.T) {
	h := fakeHome(t)
	rc := Run(func() int {
		// The 10-16 shape: a recursive delete reached the real home.
		if err := os.RemoveAll(filepath.Join(h, "vl")); err != nil { //homeguard:allow — fake home under t.TempDir
			t.Fatal(err)
		}
		return 0
	})
	if rc != 1 {
		t.Fatalf("a deleted ~/vl must fail the run: rc=%d", rc)
	}
}

func TestGuardFailsWhenTheUpdaterEnvChanges(t *testing.T) {
	h := fakeHome(t)
	rc := Run(func() int {
		_ = os.WriteFile(filepath.Join(h, ".config/vl-updater/env"), []byte("TOKEN=minted-by-a-test\n"), 0o600)
		return 0
	})
	if rc != 1 {
		t.Fatalf("a rewritten updater env must fail the run: rc=%d", rc)
	}
}

func TestGuardFailsWhenTheWholeHomeIsGone(t *testing.T) {
	h := fakeHome(t)
	rc := Run(func() int {
		if err := os.RemoveAll(h); err != nil { //homeguard:allow — fake home under t.TempDir
			t.Fatal(err)
		}
		return 0
	})
	if rc != 1 {
		t.Fatalf("a wiped home must fail the run: rc=%d", rc)
	}
}

func TestGuardPointsHomeAwayFromTheRealHomeDuringTheRun(t *testing.T) {
	h := fakeHome(t)
	var during, xdg string
	Run(func() int {
		during, xdg = os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME")
		return 0
	})
	if during == "" || during == h {
		t.Fatalf("HOME during the run = %q; must be a throwaway folder, not the real home %q", during, h)
	}
	if xdg == "" || filepath.Dir(xdg) != during {
		t.Fatalf("XDG_CONFIG_HOME during the run = %q; must live inside the throwaway HOME %q", xdg, during)
	}
	if _, err := os.Stat(during); err == nil {
		t.Fatalf("the throwaway HOME %q must be removed after the run", during)
	}
}
