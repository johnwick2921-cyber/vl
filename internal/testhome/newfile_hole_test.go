package testhome

import (
	"os"
	"path/filepath"
	"testing"
)

// HOLE: the guard's DETECT pass only flags (a) a VANISHED top-level entry and
// (b) a changed/deleted file under the two watched paths (~/.config/vl-updater,
// ~/bin/vl-updater). A test that WRITES A NEW FILE anywhere else in the real
// home — a literal path, a child process with a cleared env whose
// os.UserHomeDir reads the passwd, or a GOENV write — is invisible: new
// top-level names are deliberately ignored, and new files under un-watched
// directories are never stamped.
//
// This test points the realHomeFn seam at a throwaway folder that stands in
// for the real home and asserts the guard MUST return non-zero when a test
// writes a new file there. It FAILS today: Run returns 0 and the file stays.
func TestGuardDetectsANewFileWrittenToTheRealHome(t *testing.T) {
	fakeHome := t.TempDir()
	old := realHomeFn
	realHomeFn = func() string { return fakeHome }
	t.Cleanup(func() { realHomeFn = old })

	code := Run(func() int {
		p := filepath.Join(fakeHome, "evil-new-file.txt")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code == 0 {
		t.Fatal("HOLE: the guard returned 0 while a NEW file was written to the real home (outside the watched paths)")
	}
}
