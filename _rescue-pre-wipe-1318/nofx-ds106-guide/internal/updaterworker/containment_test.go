package updaterworker

// PIN (U4G defects 3 and 4): PathWithin's fail-closed defaults, as a table —
// the mutants that survive in the wild are exactly the defaults this pins:
//   - Y7: a non-ENOENT Lstat error (ENOTDIR, EACCES) is NOT "not yet created"
//     — it refuses;
//   - Y8: the ".." check cannot be dropped — the parent is outside;
//   - Y12: a relative path refuses (the IsAbs guard cannot be dropped);
//   - Y11: callers must not ignore the error (ReleaseRoot refuses a
//     non-existent install — the containment error is never "outside").
//
// The accepted/refused rows are the U4G probe G3 table.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathWithinTable(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(base, "nofx")
	mkdirRRFixture(t, install)
	dangle := filepath.Join(base, "dangle")
	if err := os.Symlink(filepath.Join(base, "nowhere"), dangle); err != nil {
		t.Fatal(err)
	}
	blockingFile := filepath.Join(base, "plain-file")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(base, "mode-000")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	installLink := filepath.Join(base, "install-link")
	if err := os.Symlink(install, installLink); err != nil {
		t.Fatal(err)
	}
	insideViaLink := filepath.Join(install, "a")

	for _, tc := range []struct {
		name    string
		p, dir  string
		inside  bool // when err == nil
		wantErr string
	}{
		{"<install>/..x is inside", filepath.Join(install, "..x"), install, true, ""},
		{"an install named through a symlink: a real child is inside", insideViaLink, installLink, true, ""},
		{"the install itself is inside", install, install, true, ""},
		{"the install's parent is outside", base, install, false, ""},
		{"a dangling symlink element refuses", filepath.Join(dangle, "x"), install, false, "does not resolve"},
		{"a file as a path element refuses (ENOTDIR)", filepath.Join(blockingFile, "x"), install, false, "cannot be checked"},
		{"an unreadable element refuses (not 'not yet created')", filepath.Join(locked, "x"), install, false, "cannot be checked"},
		{"a relative path refuses", "rel/x", install, false, "must both be absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inside, err := PathWithin(tc.p, tc.dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("PathWithin(%q, %q) = %v, %v; want the refusal %q", tc.p, tc.dir, inside, err, tc.wantErr)
				}
				return
			}
			if err != nil || inside != tc.inside {
				t.Fatalf("PathWithin(%q, %q) = %v, %v; want inside=%v", tc.p, tc.dir, inside, err, tc.inside)
			}
		})
	}
}

// d4 + Y11: ReleaseRoot refuses an install that does not exist (or cannot
// resolve) — U4G made it return ok; a containment against an install that is
// not there is unknown, never "outside".
func TestReleaseRootRefusesANonExistentInstall(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "releases")
	mkdirRRFixture(t, root)
	setReleaseRoot(t, root)
	for _, installDir := range []string{
		filepath.Join(base, "no", "such", "install"),
		filepath.Join(base, "dangling"), // created as a symlink below
	} {
		if installDir == filepath.Join(base, "dangling") {
			if err := os.Symlink(filepath.Join(base, "nowhere"), installDir); err != nil {
				t.Fatal(err)
			}
		}
		t.Run(filepath.Base(installDir), func(t *testing.T) {
			got, err := ReleaseRoot(installDir)
			if !errors.Is(err, ErrReleaseRoot) || got != "" || !strings.Contains(err.Error(), "cannot be resolved") {
				t.Fatalf("ReleaseRoot(%s) = %q, %v; want refused with 'cannot be resolved'", installDir, got, err)
			}
		})
	}
}
