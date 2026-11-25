package datareport

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/ownerpush"
)

// sink collects what a real ownerpush.Pusher hands to its sender.
type sink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *sink) send(text string) { s.mu.Lock(); s.msgs = append(s.msgs, text); s.mu.Unlock() }
func (s *sink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

// installPusher installs a REAL ownerpush pusher as the process default, so the
// scheduler's default Push (ownerpush.Push) is the production call site.
func installPusher(t *testing.T, enabled bool) (*sink, *ownerpush.Pusher) {
	t.Helper()
	sk := &sink{}
	p := ownerpush.New(sk.send)
	p.SetEnabledForTest(func() bool { return enabled })
	restore := ownerpush.Use(p)
	t.Cleanup(restore)
	return sk, p
}

func schedFixture(t *testing.T, now *time.Time, markerDir string) *Scheduler {
	t.Helper()
	st, db := newDB(t)
	sess := SessionFor(normalNow)
	putBars(t, st, "MNQ", sess, func(int) bool { return false })
	return NewScheduler(Config{
		Sources:   Sources{DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m},
		MarkerDir: markerDir,
		Now:       func() time.Time { return *now },
	})
}

func TestTickSendsOncePerSessionDayInsideTheWindowThroughOwnerPush(t *testing.T) {
	sk, p := installPusher(t, true)
	now := ct(2026, 10, 28, 15, 59)
	s := schedFixture(t, &now, t.TempDir())

	for _, c := range []struct {
		at   time.Time
		want bool
		why  string
	}{
		{ct(2026, 10, 28, 15, 59), false, "before the close"},
		{ct(2026, 10, 28, 16, 9), false, "the close has not settled yet"},
		{ct(2026, 10, 28, 17, 0), false, "after the 17:00 roll the gate table is the next day's"},
		{ct(2026, 10, 28, 16, 10), true, "16:10 is the first send minute"},
		{ct(2026, 10, 28, 16, 11), false, "already sent for this session day"},
		{ct(2026, 10, 28, 16, 55), false, "already sent for this session day"},
	} {
		now = c.at
		if got := s.Tick(); got != c.want {
			t.Fatalf("Tick at %v = %v, want %v (%s)", c.at, got, c.want, c.why)
		}
	}
	p.Flush()
	msgs := sk.all()
	if len(msgs) == 0 {
		t.Fatal("nothing reached the owner-push sender")
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "vl data report — session 2026-10-27") {
		t.Fatalf("report text missing: %v", msgs)
	}
}

func TestRestartDoesNotResendTheSameSessionDay(t *testing.T) {
	sk, p := installPusher(t, true)
	now := ct(2026, 10, 28, 16, 12)
	marker := t.TempDir()
	if !schedFixture(t, &now, marker).Tick() {
		t.Fatal("first run must send")
	}
	p.Flush()
	first := len(sk.all())
	// The bot restarts at 16:20: a NEW scheduler, a NEW in-memory set, the same
	// marker dir (data/datareport on the box).
	now = ct(2026, 10, 28, 16, 20)
	if schedFixture(t, &now, marker).Tick() {
		t.Fatal("a restarted bot re-sent the report for a session day already sent")
	}
	p.Flush()
	if len(sk.all()) != first {
		t.Fatalf("restart produced %d new messages", len(sk.all())-first)
	}
	if _, err := os.Stat(filepath.Join(marker, "2026-10-27.sent")); err != nil {
		t.Fatalf("marker not written: %v", err)
	}
}

func TestFriendInstanceNeverSendsTheReport(t *testing.T) {
	sk, p := installPusher(t, false) // the friend-instance check refuses
	now := ct(2026, 10, 28, 16, 12)
	schedFixture(t, &now, t.TempDir()).Tick()
	p.Flush()
	if got := sk.all(); len(got) != 0 {
		t.Fatalf("a friend instance pushed: %v", got)
	}
}

func TestAClosedDayIsNotReportedAndClaimsNothing(t *testing.T) {
	sk, p := installPusher(t, true)
	now := ct(2026, 4, 3, 16, 12) // Good Friday: the CME never traded
	marker := t.TempDir()
	_, db := newDB(t)
	s := NewScheduler(Config{Sources: Sources{DB: db, FeedDownAfter: feed10m}, MarkerDir: marker, Now: func() time.Time { return now }})
	if s.Tick() {
		t.Fatal("reported a day the CME never traded")
	}
	p.Flush()
	if len(sk.all()) != 0 {
		t.Fatalf("pushed: %v", sk.all())
	}
	if ents, _ := os.ReadDir(marker); len(ents) != 0 {
		t.Fatalf("a skipped day wrote a marker: %v", ents)
	}
}

func TestSendNowIsOnDemandAndNotSwallowedByTheDailyDedupe(t *testing.T) {
	sk, p := installPusher(t, true)
	st, db := newDB(t)
	putBars(t, st, "MNQ", SessionFor(normalNow), func(int) bool { return false })
	src := Sources{DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m}
	rep, acc := SendNow(src, nil, normalNow)
	if len(rep.Symbols) != 1 || len(acc) == 0 || !acc[0] {
		t.Fatalf("on-demand report not accepted: %+v %v", rep.Symbols, acc)
	}
	// Same session, a minute later: a different key, so it is not a duplicate.
	if _, acc2 := SendNow(src, nil, normalNow.Add(time.Minute)); len(acc2) == 0 || !acc2[0] {
		t.Fatalf("second on-demand report swallowed: %v", acc2)
	}
	p.Flush()
	if len(sk.all()) < 2 {
		t.Fatalf("expected 2+ messages, got %v", sk.all())
	}
}

// N1: an EMPTY store on an open day is the day the data failed: the report goes
// out (with n/a) instead of being skipped as "nothing to report".
func TestAnEmptyStoreOnAnOpenCalendarDayStillSendsTheReport(t *testing.T) {
	sk, p := installPusher(t, true)
	now := ct(2026, 10, 28, 16, 12)
	_, db := newDB(t) // no bars, no trades
	s := NewScheduler(Config{Sources: Sources{DB: db, FeedDownAfter: feed10m}, MarkerDir: t.TempDir(), Now: func() time.Time { return now }})
	if !s.Tick() {
		t.Fatal("no 1m bars stored on an open CME day was skipped as a closed day")
	}
	p.Flush()
	if !strings.Contains(strings.Join(sk.all(), "\n"), "1m bars: n/a (no 1m bars stored in the last 7 days)") {
		t.Fatalf("report text: %v", sk.all())
	}
}

// N2: a panic inside one tick must not end the loop for good.
func TestStartKeepsTickingAfterATickPanics(t *testing.T) {
	calls := make(chan struct{}, 8)
	stop := Start(Config{
		Sources: Sources{DB: nil},
		Now: func() time.Time {
			select {
			case calls <- struct{}{}:
			default:
			}
			panic("boom")
		},
		TickEvery: 5 * time.Millisecond,
	})
	defer stop()
	for i := 0; i < 3; i++ {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("the loop died after %d ticks", i)
		}
	}
}
