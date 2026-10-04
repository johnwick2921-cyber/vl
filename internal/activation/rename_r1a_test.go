package activation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// R5 single readers: MainPID (the vl unit only), NewestLogPath (the vl_
// prefix only), Resolve (the vl-bin binary only).

func TestReadMainPIDReadsTheVlUnit(t *testing.T) {
	absent := func(string) (int, error) { return 0, errors.New("unit not found") }
	stopped := func(string) (int, error) { return 0, nil }
	live := func(string) (int, error) { return 4242, nil }
	one := func(string) (int, error) { return 1, nil }
	for name, c := range map[string]struct {
		fn   func(string) (int, error)
		want int
		err  bool
	}{
		"vl live":       {live, 4242, false},
		"unit absent":   {absent, 0, true},
		"unit stopped":  {stopped, 0, true},
		"pid below 2":   {one, 0, true},
	} {
		got, err := readMainPID(c.fn)
		if c.err {
			if err == nil {
				t.Errorf("%s: readMainPID = %d, want an error", name, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: readMainPID = %d/%v, want %d", name, got, err, c.want)
		}
	}
}

func TestNewestLogPathFindsTheVlLog(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-age)
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("newest vl log wins", func(t *testing.T) {
		write("vl_2026-09-23.log", 2*time.Hour)
		write("vl_2026-09-24.log", time.Minute)
		got, err := NewestLogPath(dir)
		if err != nil || filepath.Base(got) != "vl_2026-09-24.log" {
			t.Fatalf("NewestLogPath = %q/%v, want the vl file", got, err)
		}
	})
	t.Run("empty dir refuses with the vl name", func(t *testing.T) {
		_, err := NewestLogPath(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "vl_*.log") {
			t.Fatalf("NewestLogPath on empty dir = %v, want the vl-only refusal", err)
		}
	})
}

// manifestFor writes a minimal resolvable manifest.
func manifestFor(t *testing.T, dir, sha string) {
	t.Helper()
	m := fmt.Sprintf(`{"source_sha":%q,"binary_md5":"","signature_verdict":"sshsig:release:SHA256:fake"}`, sha)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRequiresVlBin(t *testing.T) {
	sha := strings.Repeat("a", 40)
	t.Run("vl-bin release resolves to vl-bin", func(t *testing.T) {
		dir := t.TempDir()
		manifestFor(t, dir, sha)
		if err := os.WriteFile(filepath.Join(dir, "vl-bin"), []byte("\x7fELF"), 0o755); err != nil {
			t.Fatal(err)
		}
		rel, err := Resolve(dir)
		if err != nil || filepath.Base(rel.Binary) != "vl-bin" {
			t.Fatalf("Resolve = %+v/%v, want a vl-bin release", rel, err)
		}
	})
	t.Run("missing keeps the old lenient reading", func(t *testing.T) {
		dir := t.TempDir()
		manifestFor(t, dir, sha)
		rel, err := Resolve(dir)
		if err != nil || filepath.Base(rel.Binary) != "vl-bin" {
			t.Fatalf("Resolve on a binaryless dir = %+v/%v, want the lenient vl-bin reading", rel, err)
		}
	})
}
