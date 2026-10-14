package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/store"
)

// OPS NEW-8 (10-14) — the mentor stale-data / clock-step block logged its
// START edge only; nothing said when NEW mentor entries resumed (3 holds = 6
// min of RTH mentor holds on 10-14, none with an end line). The false edge is
// now logged once, with the hold's length on the gate's own clock (the seam's
// now; time.Now() in production) and the leg that started it.
func TestMentorStaleBlockLogsItsEndEdgeWithTheHoldLength(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	now := mentorStaleBlockClock(9, 0)
	t.Cleanup(kernel.ClockSampleForTest(kernel.NewSteppedClock(now).Sample)) // no box clock step can leak in
	hook := &infoCaptureHook{}
	logger.Log.AddHook(hook)

	mentorStaleBlockFeed(t, now, 3*time.Minute) // stale on the wall rule
	if stale, _ := at.mentorStaleDataBlockedWhy(now); !stale {
		t.Fatal("fixture: a 3-minute-old 1m bar must start a stale episode")
	}
	if n := hook.count("mentor stale-data block ENDED"); n != 0 {
		t.Fatalf("no end line while the episode is open, got %d", n)
	}
	later := now.Add(3 * time.Minute)
	mentorStaleBlockFeed(t, later, 30*time.Second) // the feed is back three minutes later
	if stale, _ := at.mentorStaleDataBlockedWhy(later); stale {
		t.Fatal("fixture: a 30 s-old 1m bar is fresh")
	}
	at.mentorStaleDataBlockedWhy(later) // a second fresh read is not a second edge
	if n := hook.count("🧑‍🏫 mentor stale-data block ENDED after "); n != 1 {
		t.Fatalf("the end edge must be logged exactly once, got %d", n)
	}
	if n := hook.count("ENDED after 3m0s — NEW mentor arms allowed again; it began on: bar_age:"); n != 1 {
		t.Fatalf("the end line must carry the hold's length on the seam clock (3m0s) and the leg that started it, got %d", n)
	}
}
