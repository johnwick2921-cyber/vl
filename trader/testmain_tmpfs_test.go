package trader

import (
	"os"
	"testing"
	"time"
	"vl/internal/testhome"

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
	// OCT-EXACT B7 (be_offset_ticks default 4 = 1 pt, Oct D4.2 @13:00): the ~50
	// pre-October exit-drive mechanics tests assert the break-even AT the entry,
	// so the package's test binary runs with the unset default at 0 (the stored
	// 0 revert). oct_bc_test.go pins the October default of 4 through octBEDefault.
	mentorBEOffsetDefaultTicks = 0
	// OCT-EXACT D2 (mentor_leg1_fixed_pts default 15, Oct D4.2 @12:16): the pre-October
	// split/exit tests assert leg 1 at +1R with ceil(n/2), so the binary runs with the
	// unset default at 0 (the stored 0 revert). oct_d_test.go pins the production 15.
	mentorLeg1FixedPtsDefault = 0
	start := time.Now()
	restore := kernel.ClockSampleForTest(func() (int64, time.Duration) {
		mono := time.Since(start)
		return start.UnixNano() + int64(mono), mono
	})
	code := testhome.Run(func() int { return testtmpfs.Run(m.Run) })
	restore()
	os.Exit(code)
}
