package testhome

import (
	"os"
	"path/filepath"
	"testing"
)

// ROUND 2 (DS-106, test/home-guard-holes-2): the new-name detection lists only
// ~, ~/.config, ~/bin and ~/.ssh. A test that writes a NEW file under ~/.local
// or ~/.cache (XDG_DATA_HOME / XDG_CACHE_HOME defaults — dirs that already
// exist, so no new top-level name) is invisible: those dirs are neither listed
// nor fingerprinted. This test points the realHomeFn seam at a throwaway home
// and asserts the guard MUST return non-zero; it FAILS today (Run returns 0).
func TestGuardDetectsANewFileUnderLocalOrCache(t *testing.T) {
	fakeHome := t.TempDir()
	for _, d := range []string{".local/share", ".cache"} {
		if err := os.MkdirAll(filepath.Join(fakeHome, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := realHomeFn
	realHomeFn = func() string { return fakeHome }
	t.Cleanup(func() { realHomeFn = old })

	code := Run(func() int {
		p := filepath.Join(fakeHome, ".local", "share", "evil.txt")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code == 0 {
		t.Fatal("HOLE: the guard returned 0 while a NEW file was written under ~/.local (an unlisted, unfingerprinted dir)")
	}
}
