package safepath

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestCheckRefusesTheWipeShapes(t *testing.T) {
	tmp := t.TempDir()
	real := ""
	if u, err := user.Current(); err == nil {
		real = u.HomeDir
	}
	cases := map[string][2]string{
		"empty target":           {tmp, ""},
		"empty root":             {"", filepath.Join(tmp, "x")},
		"blank target":           {tmp, "   "},
		"relative target":        {tmp, "x"},
		"filesystem root":        {"/", "/"},
		"target is root":         {tmp, tmp},
		"outside root":           {filepath.Join(tmp, "a"), filepath.Join(tmp, "b")},
		"dot-dot escape":         {filepath.Join(tmp, "a"), filepath.Join(tmp, "a", "..", "b")},
		"real home as target":    {"/", real},
		"real home as root":      {real, filepath.Join(real, "vl")},
		"ancestor of real home":  {"/", filepath.Dir(real)},
		"HOME as target":         {filepath.Dir(os.Getenv("HOME")), os.Getenv("HOME")},
		"unexpanded var in path": {tmp, "/" + "$HOME"},
		"home from empty var":    {tmp, filepath.Join("", "/")},
	}
	for name, c := range cases {
		if err := Check(c[0], c[1]); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: Check(%q, %q) = %v, want ErrRefused", name, c[0], c[1], err)
		}
	}
}

func TestCheckRefusesASymlinkThatLeavesTheRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if err := Check(root, filepath.Join(root, "link", "victim")); !errors.Is(err, ErrRefused) {
		t.Fatalf("a symlinked parent pointing outside the root must be refused: %v", err)
	}
}

func TestRemoveAllUnderRemovesOnlyInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "staging", "deep")
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllUnder(root, filepath.Join(root, "staging")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "staging")); !os.IsNotExist(err) {
		t.Fatalf("staging must be gone: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a sibling outside the target must survive: %v", err)
	}
	// A refused call removes nothing.
	if err := RemoveAllUnder(filepath.Join(root, "staging"), root); !errors.Is(err, ErrRefused) {
		t.Fatalf("removing the parent of the root must be refused: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a refused call must not touch anything: %v", err)
	}
}

func TestRemoveAllTempOnlyUnderTheTempFolder(t *testing.T) {
	d, err := os.MkdirTemp("", "safepath-")
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllTemp(d); err != nil {
		t.Fatalf("a MkdirTemp folder must be removable: %v", err)
	}
	if err := RemoveAllTemp(os.TempDir()); !errors.Is(err, ErrRefused) {
		t.Fatalf("the temp folder itself must be refused: %v", err)
	}
}
