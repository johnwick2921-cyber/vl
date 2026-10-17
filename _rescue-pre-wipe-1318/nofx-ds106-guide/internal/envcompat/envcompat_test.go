package envcompat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reset state: the package's warn bookkeeping is global; tests share it, so
// each one starts from a clean slate (no parallelism).
func reset(t *testing.T) {
	mu.Lock()
	sink, queued, warned = nil, nil, map[string]bool{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		sink, queued, warned = nil, nil, map[string]bool{}
		mu.Unlock()
	})
}

func TestEnvVLWinsSilently(t *testing.T) {
	reset(t)
	t.Setenv("VL_TEST_KEY", "vl-value")
	t.Setenv("NOFX_TEST_KEY", "nofx-value")
	var warns []string
	SetWarnSink(func(m string) { warns = append(warns, m) })
	v, src := Env("TEST_KEY")
	if v != "vl-value" || src != SourceVL {
		t.Fatalf("Env = %q/%s, want vl-value/VL", v, src)
	}
	if len(warns) != 0 {
		t.Fatalf("the VL read warned: %v", warns)
	}
}

func TestEnvNOFXWarnsExactlyOnce(t *testing.T) {
	reset(t)
	t.Setenv("NOFX_TEST_KEY", "nofx-value")
	var warns []string
	SetWarnSink(func(m string) { warns = append(warns, m) })
	for i := 0; i < 2; i++ {
		v, src := Env("TEST_KEY")
		if v != "nofx-value" || src != SourceNOFX {
			t.Fatalf("Env = %q/%s, want nofx-value/NOFX", v, src)
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "TEST_KEY") || !strings.Contains(warns[0], "NOFX_ fallback") {
		t.Fatalf("want exactly one fallback WARN naming the key, got %v", warns)
	}
}

func TestEnvWarnsQueueUntilASinkLands(t *testing.T) {
	reset(t)
	t.Setenv("NOFX_EARLY_KEY", "v")
	if v, src := Env("EARLY_KEY"); v != "v" || src != SourceNOFX {
		t.Fatalf("Env = %q/%s, want v/NOFX", v, src)
	}
	var warns []string
	SetWarnSink(func(m string) { warns = append(warns, m) })
	if len(warns) != 1 || !strings.Contains(warns[0], "EARLY_KEY") {
		t.Fatalf("the queued WARN was not drained into the first sink: %v", warns)
	}
	// A second sink is ignored — one sink, ever (main.go then registers again
	// in other binaries; within one process the first wins).
	SetWarnSink(func(m string) { warns = append(warns, "second:"+m) })
	if len(warns) != 1 {
		t.Fatalf("the second sink must be ignored, got %v", warns)
	}
}

func TestEnvBothEmptyIsDefaultWithoutWarn(t *testing.T) {
	reset(t)
	var warns []string
	SetWarnSink(func(m string) { warns = append(warns, m) })
	if v, src := Env("NO_SUCH_KEY"); v != "" || src != SourceDefault {
		t.Fatalf("Env = %q/%s, want empty/default", v, src)
	}
	if len(warns) != 0 {
		t.Fatalf("a default read warned: %v", warns)
	}
}

func TestEnvEmptyVlFallsToNofx(t *testing.T) {
	reset(t)
	t.Setenv("VL_EMPTY_KEY", "")
	t.Setenv("NOFX_EMPTY_KEY", "nofx-value")
	var warns []string
	SetWarnSink(func(m string) { warns = append(warns, m) })
	v, src := Env("EMPTY_KEY")
	if v != "nofx-value" || src != SourceNOFX {
		t.Fatalf("Env = %q/%s, want nofx-value/NOFX (an EMPTY VL name is not a value)", v, src)
	}
	if len(warns) != 1 {
		t.Fatalf("want exactly one fallback WARN, got %v", warns)
	}
}

func TestBackupRootThreeWay(t *testing.T) {
	t.Run("vl-backups wins when present", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		for _, d := range []string{"vl-backups", "nofx"} {
			if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := BackupRoot(); got != filepath.Join(home, "vl-backups") {
			t.Fatalf("BackupRoot = %s, want vl-backups", got)
		}
	})
	t.Run("nofx-backups only while ~/nofx exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, "nofx"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := BackupRoot(); got != filepath.Join(home, "nofx-backups") {
			t.Fatalf("BackupRoot = %s, want nofx-backups", got)
		}
	})
	t.Run("a fresh machine never gets an old-name dir", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if got := BackupRoot(); got != filepath.Join(home, "vl-backups") {
			t.Fatalf("BackupRoot = %s, want vl-backups", got)
		}
	})
}
