package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/envcompat"
)

// R1a: the worker's lock script prefers deploy/vl-lock.sh, and the updater's
// env fallbacks WARN on its stderr sink.

func TestLockScriptForPrefersVl(t *testing.T) {
	t.Run("vl-lock.sh wins when present", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "deploy"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"vl-lock.sh", "nofx-lock.sh"} {
			if err := os.WriteFile(filepath.Join(dir, "deploy", n), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := lockScriptFor(dir); filepath.Base(got) != "vl-lock.sh" {
			t.Fatalf("lockScriptFor = %s, want deploy/vl-lock.sh", got)
		}
	})
	t.Run("nofx-lock.sh when vl is absent", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "deploy"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "deploy", "nofx-lock.sh"), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := lockScriptFor(dir); filepath.Base(got) != "nofx-lock.sh" {
			t.Fatalf("lockScriptFor = %s, want deploy/nofx-lock.sh", got)
		}
	})
}

// The updater never inits the logger: its envcompat sink is stderr, and a
// NOFX_-only read WARNs there (fresh process state, so the warning must fire
// exactly once).
func TestUpdaterWarnsOnStderrForTheNofxFallback(t *testing.T) {
	envcompat.ResetForTest()
	t.Cleanup(envcompat.ResetForTest)
	t.Setenv("NOFX_RELEASE_INBOX", t.TempDir())
	var stdout, stderr bytes.Buffer
	_ = run([]string{"fetch", strings.Repeat("a", 40)}, bytes.NewReader(nil), &stdout, &stderr)
	out := stderr.String()
	if !strings.Contains(out, "env RELEASE_INBOX read through the NOFX_ fallback") {
		t.Fatalf("the fetch's NOFX_ fallback did not WARN on stderr; got:\n%s", out)
	}
}
