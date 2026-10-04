//go:build linux

package testtmpfs

import (
	"os"
	"strings"
	"testing"
)

func TestRunPutsTMPDIROnTmpfsAndRemovesIt(t *testing.T) {
	t.Setenv("TMPDIR", "")
	t.Setenv("VL_TEST_NO_TMPFS", "")
	var seen string
	code := Run(func() int { seen = os.Getenv("TMPDIR"); return 7 })
	if code != 7 {
		t.Fatalf("Run must return fn's exit code, got %d", code)
	}
	if !strings.HasPrefix(seen, "/dev/shm/vlt-") {
		t.Skipf("no usable /dev/shm here (TMPDIR inside fn = %q)", seen)
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Fatalf("the private dir %s must be removed after the run (err=%v)", seen, err)
	}
	if got := os.Getenv("TMPDIR"); got != "" {
		t.Fatalf("TMPDIR must be restored after the run, got %q", got)
	}
}

func TestExplicitTMPDIRAndOptOutWin(t *testing.T) {
	t.Setenv("TMPDIR", "/var/tmp")
	var seen string
	Run(func() int { seen = os.Getenv("TMPDIR"); return 0 })
	if seen != "/var/tmp" {
		t.Fatalf("an explicit TMPDIR must win, got %q", seen)
	}
	t.Setenv("TMPDIR", "")
	t.Setenv("VL_TEST_NO_TMPFS", "1")
	Run(func() int { seen = os.Getenv("TMPDIR"); return 0 })
	if seen != "" {
		t.Fatalf("VL_TEST_NO_TMPFS must keep the default TMPDIR, got %q", seen)
	}
}

func TestStaleDirsOfDeadRunsAreSweptAndLiveOnesKept(t *testing.T) {
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		t.Skip("no /dev/shm")
	}
	dead := base + "/" + prefix + "2147483646" // a pid that cannot exist
	live := base + "/" + prefix + "1"          // init: alive, owned by root
	for _, d := range []string{dead, live} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Skipf("cannot create %s: %v", d, err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(live) })
	sweepStale()
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatalf("a dir of a dead pid must be swept (err=%v)", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a dir of a live pid must be kept: %v", err)
	}
}
