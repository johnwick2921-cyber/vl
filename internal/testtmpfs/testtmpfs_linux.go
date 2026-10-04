//go:build linux

package testtmpfs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	base    = "/dev/shm"
	prefix  = "vlt-"
	minFree = uint64(1) << 30 // below 1 GiB free the default TMPDIR is kept
)

func run(fn func() int) int {
	if os.Getenv("TMPDIR") != "" || os.Getenv("VL_TEST_NO_TMPFS") != "" {
		return fn() // an explicit TMPDIR (or the opt-out) always wins
	}
	dir, why := prepare()
	if dir == "" {
		fmt.Fprintf(os.Stderr, "testtmpfs: %s — tests keep the default TMPDIR\n", why)
		return fn()
	}
	defer os.RemoveAll(dir)
	os.Setenv("TMPDIR", dir)
	defer os.Unsetenv("TMPDIR")
	return fn()
}

// prepare returns the private tmpfs dir for this process, or "" and the reason
// it could not be used. The dir is exactly vlt-<pid> so the unix-socket paths
// the updater tests build under t.TempDir() stay short.
func prepare() (dir, why string) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(base, &st); err != nil {
		return "", base + " is not available (" + err.Error() + ")"
	}
	if free := st.Bavail * uint64(st.Bsize); free < minFree {
		return "", fmt.Sprintf("%s has %d MiB free, under 1 GiB", base, free>>20)
	}
	sweepStale()
	dir = fmt.Sprintf("%s/%s%d", base, prefix, os.Getpid())
	_ = os.RemoveAll(dir) // a dead run that owned this pid
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", base + " is not writable (" + err.Error() + ")"
	}
	return dir, ""
}

// sweepStale removes vlt-<pid> dirs left by test runs that were killed: the
// owning pid no longer exists. A live pid (or one we may not signal) is left.
func sweepStale() {
	ents, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(e.Name(), prefix))
		if err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			_ = os.RemoveAll(base + "/" + e.Name())
		}
	}
}
