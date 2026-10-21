package homeguard

import "testing"

// ROUND 2 (DS-106, test/home-guard-holes-2): the AST lint tracks os.RemoveAll
// and raw unlinks but deliberately blesses a single os.Remove. A tree delete
// built from filepath.Walk + one os.Remove per entry is therefore invisible —
// the same AST shape as the allowed single-file remove. This test asserts the
// walk+remove tree delete MUST be flagged; it FAILS today (zero findings).
func TestLintCatchesWalkRemoveTree(t *testing.T) {
	src := []byte(`package p
import "os"
import "path/filepath"
func wipe(root string) error {
	return filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Remove(p) // deletes every file AND then the dirs, bottom-up
	})
}`)
	lines, why, err := deleteFindings("walk.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("HOLE: filepath.Walk + os.Remove deletes a whole tree and the lint reports nothing")
	}
	_ = why
}

// ROUND 2 (DS-106): the lint knows only os / syscall / x-sys-unix. Any other
// package with a delete entry point — afero's Fs.RemoveAll, an io/fs wrapper,
// a future dep — is invisible. This test asserts such a delete MUST be
// flagged; it FAILS today.
func TestLintCatchesAferoRemoveAll(t *testing.T) {
	src := []byte(`package p
import "github.com/spf13/afero"
func wipe(fs afero.Fs, p string) error { return fs.RemoveAll(p) }`)
	lines, _, err := deleteFindings("afero.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("HOLE: afero.Fs.RemoveAll (or any non-listed package's tree delete) is invisible to the lint")
	}
}
