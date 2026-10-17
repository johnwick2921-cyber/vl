package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// PIN (probed 2026-10-17, DS-106): Check resolves only the LONGEST EXISTING
// prefix, so a target whose intermediate component does not exist yet passes a
// standalone Check. The attack — plant a symlink AFTER that Check and then
// delete — does NOT escape the root, because RemoveAllUnder runs Check again
// immediately before os.RemoveAll, and that second Check resolves the now
// existing symlink and refuses. This pins the invariant end-to-end.
func TestSymlinkPlantedAfterCheckCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	target := filepath.Join(link, "victim")

	// A standalone Check before the symlink exists sees the target lexically
	// inside root and returns nil — that is expected; the re-check is what
	// closes the gap.
	if err := Check(root, target); err != nil {
		t.Fatalf("precondition: Check before the symlink exists must pass, got %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	// RemoveAllUnder re-checks against the now-existing symlink and refuses.
	if err := RemoveAllUnder(root, target); !errors.Is(err, ErrRefused) {
		t.Fatalf("RemoveAllUnder must refuse the symlinked target, got %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the victim OUTSIDE root must survive, got err=%v", err)
	}
}
