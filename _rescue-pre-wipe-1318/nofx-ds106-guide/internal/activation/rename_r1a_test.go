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

// R1a dual readers: MainPID (vl first, nofx fallback), NewestLogPath (both
// prefixes), Resolve (exactly-one binary).

func TestReadMainPIDUnitFallback(t *testing.T) {
	absent := func(string) (int, error) { return 0, errors.New("unit not found") }
	stopped := func(string) (int, error) { return 0, nil }
	perUnit := func(m map[string]int) func(string) (int, error) {
		return func(u string) (int, error) { return m[u], nil }
	}
	for name, c := range map[string]struct {
		fn   func(string) (int, error)
		want int
		err  bool
	}{
		"vl live wins":                 {perUnit(map[string]int{"vl": 4242, "nofx": 99}), 4242, false},
		"vl absent falls to nofx":      {perUnit(map[string]int{"nofx": 99}), 99, false},
		"vl stopped (0) falls to nofx": {perUnit(map[string]int{"vl": 0, "nofx": 99}), 99, false},
		"vl at 1 falls to nofx":        {perUnit(map[string]int{"vl": 1, "nofx": 99}), 99, false},
		"vl errors falls to nofx": {func(u string) (int, error) {
			if u == "vl" {
				return 0, errors.New("no unit")
			}
			return 99, nil
		}, 99, false},
		"both gone refuses":      {absent, 0, true},
		"both stopped refuses":   {stopped, 0, true},
		"vl live with nofx gone": {perUnit(map[string]int{"vl": 4242}), 4242, false},
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

func TestNewestLogPathScansBothPrefixes(t *testing.T) {
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
	t.Run("vl newest wins", func(t *testing.T) {
		write("nofx_2026-09-23.log", 2*time.Hour)
		write("vl_2026-09-24.log", time.Minute)
		got, err := NewestLogPath(dir)
		if err != nil || filepath.Base(got) != "vl_2026-09-24.log" {
			t.Fatalf("NewestLogPath = %q/%v, want the vl file", got, err)
		}
	})
	t.Run("nofx only still works", func(t *testing.T) {
		dir2 := t.TempDir()
		p := filepath.Join(dir2, "nofx_2026-09-23.log")
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := NewestLogPath(dir2)
		if err != nil || filepath.Base(got) != "nofx_2026-09-23.log" {
			t.Fatalf("NewestLogPath = %q/%v, want the nofx file", got, err)
		}
	})
	t.Run("neither refuses with both names", func(t *testing.T) {
		_, err := NewestLogPath(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "vl_*.log") {
			t.Fatalf("NewestLogPath on empty dir = %v, want the dual-name refusal", err)
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

func TestResolveReadsEitherBinaryName(t *testing.T) {
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
	t.Run("nofx-bin release still resolves", func(t *testing.T) {
		dir := t.TempDir()
		manifestFor(t, dir, sha)
		if err := os.WriteFile(filepath.Join(dir, "nofx-bin"), []byte("\x7fELF"), 0o755); err != nil {
			t.Fatal(err)
		}
		rel, err := Resolve(dir)
		if err != nil || filepath.Base(rel.Binary) != "nofx-bin" {
			t.Fatalf("Resolve = %+v/%v, want a nofx-bin release", rel, err)
		}
	})
	t.Run("both present is refused", func(t *testing.T) {
		dir := t.TempDir()
		manifestFor(t, dir, sha)
		for _, b := range []string{"vl-bin", "nofx-bin"} {
			if err := os.WriteFile(filepath.Join(dir, b), []byte("\x7fELF"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Resolve(dir); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("Resolve on a both-binaries dir = %v, want the exactly-one refusal", err)
		}
	})
	t.Run("neither present keeps the old lenient reading", func(t *testing.T) {
		dir := t.TempDir()
		manifestFor(t, dir, sha)
		rel, err := Resolve(dir)
		if err != nil || filepath.Base(rel.Binary) != "nofx-bin" {
			t.Fatalf("Resolve on a binaryless dir = %+v/%v, want the lenient nofx-bin reading", rel, err)
		}
	})
}
