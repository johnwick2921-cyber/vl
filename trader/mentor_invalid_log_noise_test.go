package trader

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
)

// ── item 18 noise gate (DS-105 replay) — level_invalid ~240/day is
// course-correct (each new visit is a new first touch), but the per-event INFO
// line flooded the journal. The counter fires on EVERY event; the log line is
// at most once per LevelKey per 15 minutes, and the next line reports how many
// were suppressed in the window. ─────────────────────────────────────────────

// infoCaptureHook captures InfoLevel log messages for the test (logrus has no
// per-hook removal; the test filters by the message substring).
type infoCaptureHook struct {
	mu    sync.Mutex
	lines []string
}

func (h *infoCaptureHook) Levels() []logrus.Level { return []logrus.Level{logrus.InfoLevel} }
func (h *infoCaptureHook) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	h.lines = append(h.lines, e.Message)
	h.mu.Unlock()
	return nil
}

func (h *infoCaptureHook) count(sub string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// TestMentorInvalidLogDecisionPure pins the rate-limit decision: 10 events
// inside one 15-minute window → exactly one log, and the next window's line
// reports the 9 held back.
func TestMentorInvalidLogDecisionPure(t *testing.T) {
	const base = int64(1_000_000_000)
	st := mentorInvalidLogState{}
	logs := 0
	for i := 0; i < 10; i++ {
		logNow, _, next := mentorInvalidLogDecision(st, base+int64(i)) // all < 15m apart
		st = next
		if logNow {
			logs++
		}
	}
	if logs != 1 {
		t.Fatalf("log decisions = %d, want 1", logs)
	}
	_, suppressed, _ := mentorInvalidLogDecision(st, base+mentorInvalidLogWindow.Milliseconds())
	if suppressed != 9 {
		t.Fatalf("suppressed = %d, want 9", suppressed)
	}
}

// TestMentorLevelInvalidLogsOncePer15m pins the call site: 10 invalidations of
// ONE level within 15 minutes → the counter reads 10 and exactly ONE INFO line
// is emitted. The mutant that logs every event fails this.
func TestMentorLevelInvalidLogsOncePer15m(t *testing.T) {
	ResetMentorCountersForTest()
	at := &AutoTrader{id: "t-invalid"}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	hook := &infoCaptureHook{}
	logger.Log.AddHook(hook)

	for i := 0; i < 10; i++ {
		at.mentorRecordLevelInvalid(mentor.Intent{
			Action:   mentor.LevelInvalid,
			LevelKey: "lvl-1",
			Reason:   "first touch closed on the wrong side",
		})
	}

	if got := MentorCountSnapshot()["intent_level_invalid"]; got != 10 {
		t.Fatalf("counter = %d, want 10", got)
	}
	if got := hook.count("mentor level invalidated"); got != 1 {
		t.Fatalf("log lines = %d, want 1", got)
	}

	// The next window's line reports the 9 suppressed.
	mentorNowSource = func() time.Time { return now.Add(mentorInvalidLogWindow) }
	at.mentorRecordLevelInvalid(mentor.Intent{Action: mentor.LevelInvalid, LevelKey: "lvl-1", Reason: "again"})
	if got := hook.count("9 more suppressed"); got != 1 {
		t.Fatalf("the next window must report the suppressed count, lines=%v", hook.lines)
	}
}
