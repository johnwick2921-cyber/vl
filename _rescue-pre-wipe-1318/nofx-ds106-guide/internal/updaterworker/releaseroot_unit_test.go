package updaterworker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// PIN (CTO 1790303901586): ReleaseRoot's own rules, at the unit, so a second
// caller inherits them — not only cmd/vl-updater's fetch refusals. Each row
// is one rule: containment compares path ELEMENTS ("<install>/..rel" is
// inside), a trusted root is its own resolved path, and the install side is
// resolved too. The two controls prove the rules do not refuse a real
// outside root, including one whose NAME shares the install's prefix.
func TestReleaseRootRules(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(base, "nofx")
	mkdirRRFixture(t, install)
	outsideRoot := filepath.Join(base, "releases")
	mkdirRRFixture(t, outsideRoot)
	prefixSibling := filepath.Join(base, "nofx-releases")
	mkdirRRFixture(t, prefixSibling)
	dotdotInside := filepath.Join(install, "..rel")
	mkdirRRFixture(t, dotdotInside)
	linkToOutside := filepath.Join(base, "releases-link")
	if err := os.Symlink(outsideRoot, linkToOutside); err != nil {
		t.Fatal(err)
	}
	insideReal := filepath.Join(install, "rel")
	mkdirRRFixture(t, insideReal)
	installLink := filepath.Join(base, "install-link")
	if err := os.Symlink(install, installLink); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, installDir, root string
		want                   string // "" = accepted and returned as-is
	}{
		{"<install>/..rel is INSIDE the install", install, dotdotInside, "refuse"},
		{"a symlinked root is refused (not its own resolved path)", install, linkToOutside, "refuse"},
		{"an install named through a symlink is resolved: a root inside the real install is refused", installLink, insideReal, "refuse"},
		{"control: a sibling root outside the install is accepted", install, outsideRoot, ""},
		{"control: a sibling whose NAME shares the install's prefix is outside", install, prefixSibling, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setReleaseRoot(t, tc.root)
			got, err := ReleaseRoot(tc.installDir)
			if tc.want == "refuse" {
				if !errors.Is(err, ErrReleaseRoot) || got != "" {
					t.Fatalf("ReleaseRoot(%s) with NOFX_RELEASE_DIR=%s = %q, %v; want refused with ErrReleaseRoot", tc.installDir, tc.root, got, err)
				}
				return
			}
			if err != nil || got != tc.root {
				t.Fatalf("ReleaseRoot(%s) with NOFX_RELEASE_DIR=%s = %q, %v; want %q accepted", tc.installDir, tc.root, got, err, tc.root)
			}
		})
	}
}

func mkdirRRFixture(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}
