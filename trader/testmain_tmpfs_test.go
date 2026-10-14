package trader

import (
	"os"
	"testing"
	"time"

	"vl/internal/testtmpfs"
	"vl/kernel"
)

// TestMain keeps t.TempDir() on tmpfs: the per-test SQLite migrations fsync a
// file on disk (~1.2 s each) and cost ~0.02 s on /dev/shm (internal/testtmpfs).
//
// It also gives the process-wide clock-step guard a STEADY clock (OPS-FOLLOWUPS
// 10-14): the guard reads the real wall clock, and this box's WSL clock steps
// (+1032 ms at 15:42:38 on 10-14, after the 15:01 reboot). A real step during a
// run held NEW entries for 120 s and failed ~50 placement tests at once —
// mentor and planner-arm paths alike — none of which was about clocks. Here the
// wall reading is derived from the monotonic one, so it never steps; a test
// that exercises the guard injects its own clock (kernel.ClockSampleForTest)
// and its restore returns to this one. Production never calls it.
func TestMain(m *testing.M) {
	start := time.Now()
	restore := kernel.ClockSampleForTest(func() (int64, time.Duration) {
		mono := time.Since(start)
		return start.UnixNano() + int64(mono), mono
	})
	code := testtmpfs.Run(m.Run)
	restore()
	os.Exit(code)
}
