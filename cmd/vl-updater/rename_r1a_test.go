package main

import (
	"os"
	"path/filepath"
	"testing"
)

// R5: the worker's lock script is deploy/vl-lock.sh (the old vl-lock.sh
// branch was removed with R5).

func TestLockScriptForIsVl(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := lockScriptFor(dir); filepath.Base(got) != "vl-lock.sh" {
		t.Fatalf("lockScriptFor = %s, want deploy/vl-lock.sh", got)
	}
}
