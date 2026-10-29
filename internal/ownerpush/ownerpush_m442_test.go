package ownerpush

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// m442 - the owner alerts must be TRUE and must REACH the owner.

type flaky struct {
	mu   sync.Mutex
	fail error // returned by every send while non-nil
	got  []string
	n    int
}

func (f *flaky) send(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, text)
	return nil
}
func (f *flaky) set(err error) { f.mu.Lock(); f.fail = err; f.mu.Unlock() }
func (f *flaky) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newClocked(send func(string) error) (*Pusher, *clock) {
	c := &clock{t: time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)}
	p := NewE(send)
	p.SetClockForTest(c.now)
	return p, c
}

// N4. MUTANT (shown RED): in onFailed drop the hold (p.holdMsgLocked) -> the
// failed P0 is never re-offered -> RED. MUTANT 2: leave seen[key] set -> a
// re-Push of the same key is refused as a duplicate and Retry skips it -> RED.
func TestFailedSendIsHeldUnseenAndRetriedWithBackoff(t *testing.T) {
	f := &flaky{fail: errors.New("telegram send: connection reset")}
	p, c := newClocked(f.send)
	if !p.Push("unprotected_position|t1|e1", "NAKED") {
		t.Fatal("first push refused")
	}
	p.Flush()
	st := p.Stats()
	if st.Failed != 1 || st.Sent != 0 || st.Held != 1 {
		t.Fatalf("after a failed send want failed=1 sent=0 held=1, got %+v", st)
	}
	if !strings.Contains(st.LastError, "connection reset") {
		t.Fatalf("last error not recorded: %+v", st)
	}
	// backoff: not before 30 s
	if n := p.Retry(); n != 0 {
		t.Fatalf("retry before the backoff elapsed accepted %d", n)
	}
	f.set(nil) // Telegram is back
	c.add(31 * time.Second)
	if n := p.Retry(); n != 1 {
		t.Fatalf("retry after the backoff accepted %d, want 1", n)
	}
	p.Flush()
	if got := f.sent(); len(got) != 1 || got[0] != "NAKED" {
		t.Fatalf("the failed P0 was not delivered on retry: %q", got)
	}
	st = p.Stats()
	if st.Sent != 1 || st.Failed != 1 || st.Held != 0 {
		t.Fatalf("after the retry want sent=1 failed=1 held=0, got %+v", st)
	}
	if p.Retry() != 0 {
		t.Fatal("a delivered message must leave the hold (no double send)")
	}
}

// A failed send must not spend the hourly budget: three failures of one kind
// followed by success must still be deliverable inside the same hour.
func TestFailedSendRefundsTheBudgetSlot(t *testing.T) {
	f := &flaky{fail: errors.New("boom")}
	p, c := newClocked(f.send)
	for i := 0; i < maxPerKindPerHour; i++ {
		p.Push(fmt.Sprintf("feed-down|ep%d", i), "down")
	}
	p.Flush()
	f.set(nil)
	c.add(time.Minute)
	if !p.Push("feed-down|fresh", "FRESH") {
		t.Fatalf("failed sends spent the budget: a fresh message in the same hour was refused (held=%d)", p.Held())
	}
	p.Flush()
	found := false
	for _, m := range f.sent() {
		found = found || m == "FRESH"
	}
	if !found {
		t.Fatalf("fresh message not delivered: %q", f.sent())
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, w := range want {
		if got := backoffFor(i + 1); got != w {
			t.Errorf("backoffFor(%d) = %s, want %s", i+1, got, w)
		}
	}
}

// A sender that PANICS or never returns is a failure, not a delivery.
func TestPanickingAndStuckSendersAreFailuresNotDeliveries(t *testing.T) {
	p, _ := newClocked(func(string) error { panic("kaboom") })
	p.Push("trader_down_no_evaluator|t|e", "DOWN")
	p.Flush()
	if st := p.Stats(); st.Failed != 1 || st.Held != 1 || st.Sent != 0 {
		t.Fatalf("a panicking sender must count as failed+held, got %+v", st)
	}
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	p2, _ := newClocked(func(string) error { <-stuck; return nil })
	p2.SetSendTimeoutForTest(50 * time.Millisecond)
	p2.Push("feed-down|e", "DOWN")
	p2.Flush()
	if st := p2.Stats(); st.Failed != 1 || st.Held != 1 || st.Sent != 0 {
		t.Fatalf("a send past its deadline must count as failed+held, got %+v", st)
	}
}

// ErrNotBound is a SKIP: no retry, no failure, the key stays spoken for.
func TestUnboundChatIsASkipNotAFailure(t *testing.T) {
	f := &flaky{fail: ErrNotBound}
	p, c := newClocked(f.send)
	p.Push("feed-down|e", "DOWN")
	p.Flush()
	st := p.Stats()
	if st.Skipped != 1 || st.Failed != 0 || st.Held != 0 {
		t.Fatalf("unbound must be skipped (not failed, not held), got %+v", st)
	}
	c.add(time.Hour)
	if p.Retry() != 0 || f.n != 1 {
		t.Fatalf("unbound must never be retried (sends=%d)", f.n)
	}
}

// Cancel must win over a failing in-flight copy: a FEED DOWN whose BACK went out
// is never re-held by a late failure.
func TestCancelBeatsALateFailure(t *testing.T) {
	gate := make(chan struct{})
	p, c := newClocked(func(string) error { <-gate; return errors.New("late failure") })
	p.Push("feed-down|e1", "DOWN")
	p.Cancel("feed-down|e1")
	close(gate)
	p.Flush()
	if p.Held() != 0 {
		t.Fatalf("a cancelled message was re-held after its send failed: held=%d", p.Held())
	}
	c.add(time.Hour)
	if p.Retry() != 0 {
		t.Fatal("a cancelled message must never be retried")
	}
}

// #27: the numbers the boot line prints come from the constants.
func TestLimitsTextIsReadFromTheConstants(t *testing.T) {
	got := Limits()
	if !strings.Contains(got, fmt.Sprintf("%d/hour per kind", maxPerKindPerHour)) || !strings.Contains(got, fmt.Sprintf("%d/hour in total", maxPerHour)) {
		t.Fatalf("Limits() = %q does not state the constants (%d per kind, %d total)", got, maxPerKindPerHour, maxPerHour)
	}
	if strings.Contains(got, "8/hour") {
		t.Fatalf("stale 8/hour figure: %q", got)
	}
}

// #24. MUTANT (shown RED): make persistLocked a no-op -> the second process
// finds no file and nothing is restored -> RED.
func TestHeldAndSeenSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ownerpush.json")
	f := &flaky{fail: errors.New("telegram down")}
	p, _ := newClocked(f.send)
	info := p.EnableState(path)
	if info.PrevExit != PrevExitNone {
		t.Fatalf("first boot PrevExit = %q", info.PrevExit)
	}
	p.Push("unprotected_position|t1|e1", "NAKED LONG, NO STOP") // fails -> held
	p.Flush()
	f.set(nil)
	p.Push("feed-down|e9", "FEED DOWN") // delivered -> seen
	p.Flush()
	p.FlushState()

	// "restart": a new process, same file, Telegram healthy now
	g := &flaky{}
	q, c2 := newClocked(g.send)
	q.SetClockForTest(c2.now)
	info = q.EnableState(path)
	if info.PrevExit != PrevExitUnclean {
		t.Fatalf("a file left in 'running' must read as not-clean, got %q", info.PrevExit)
	}
	if info.Restored != 1 || q.Held() != 1 {
		t.Fatalf("restored=%d held=%d, want the held P0 back", info.Restored, q.Held())
	}
	if q.Push("feed-down|e9", "FEED DOWN again") {
		t.Fatal("a delivered episode key must stay deduped across a restart")
	}
	c2.add(time.Second)
	if n := q.Retry(); n != 1 {
		t.Fatalf("the restored held P0 was not re-offered (accepted %d)", n)
	}
	q.Flush()
	if got := g.sent(); len(got) != 1 || got[0] != "NAKED LONG, NO STOP" {
		t.Fatalf("restored P0 not delivered after the restart: %q", got)
	}
}

// An in-flight (admitted, never confirmed) message is persisted as held: a kill
// between enqueue and send must not lose it.
func TestInFlightMessageIsPersistedAsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	p, _ := newClocked(func(string) error { <-gate; return nil })
	p.EnableState(path)
	p.Push("protection_partial|t|e", "PARTIAL")
	p.FlushState()
	d, err := readState(path)
	if err != nil || d == nil || len(d.Held) != 1 || d.Held[0].Key != "protection_partial|t|e" {
		t.Fatalf("in-flight message not in the state file: %+v err=%v", d, err)
	}
}

func TestStaleHeldIsNotRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errors.New("down")}
	p, c := newClocked(f.send)
	p.EnableState(path)
	p.Push("feed-down|e", "OLD NEWS")
	p.Flush()
	p.FlushState()
	q, c2 := newClocked((&flaky{}).send)
	c2.add(holdTTL + time.Minute)
	_ = c
	if info := q.EnableState(path); info.Restored != 0 || q.Held() != 0 {
		t.Fatalf("a held message older than holdTTL must not be restored: %+v held=%d", info, q.Held())
	}
}

func TestCleanShutdownIsRecordedAndReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	p, _ := newClocked((&flaky{}).send)
	p.EnableState(path)
	restore := Use(p)
	MarkCleanShutdown()
	restore()
	q, _ := newClocked((&flaky{}).send)
	info := q.EnableState(path)
	if info.PrevExit != PrevExitClean {
		t.Fatalf("PrevExit = %q, want clean", info.PrevExit)
	}
}

func TestUnreadableStateStartsEmptyAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ := newClocked((&flaky{}).send)
	if info := p.EnableState(path); info.PrevExit != PrevExitUnknown {
		t.Fatalf("PrevExit = %q, want %q", info.PrevExit, PrevExitUnknown)
	}
}

func TestStateFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	p, _ := newClocked((&flaky{}).send)
	p.EnableState(path)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state file mode %v leaks alert text to other users", fi.Mode().Perm())
	}
}

// #28: the boot message names the revision and the reason.
func TestBootMessageNamesRevAndHowTheLastProcessEnded(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	key, text := BootMessage("e8dfc898382d", BootInfo{PrevExit: PrevExitUnclean, Restored: 2}, false, now)
	if !strings.HasPrefix(key, "boot|") || kindOf(key) != "boot" {
		t.Fatalf("boot key %q must be its own kind", key)
	}
	for _, want := range []string{"e8dfc898382d", "did NOT shut down cleanly", "2 held alert(s) restored"} {
		if !strings.Contains(text, want) {
			t.Errorf("boot text %q lacks %q", text, want)
		}
	}
	_, clean := BootMessage("abc", BootInfo{PrevExit: PrevExitClean}, false, now)
	if !strings.Contains(clean, "stopped cleanly") {
		t.Errorf("clean-exit text %q", clean)
	}
	_, refused := BootMessage("abc", BootInfo{PrevExit: PrevExitClean}, true, now)
	if !strings.Contains(refused, "TRADING IS REFUSED") {
		t.Errorf("a refused boot must say so: %q", refused)
	}
	if _, nr := BootMessage("", BootInfo{}, false, now); !strings.Contains(nr, "n/a") {
		t.Errorf("an unknown rev must print n/a, got %q", nr)
	}
}

// Friend instances are refused for boot / storm pushes exactly as for the rest.
func TestFriendInstanceRefusesBootAndStormPushes(t *testing.T) {
	f := &flaky{}
	p, _ := newClocked(f.send)
	p.SetEnabledForTest(func() bool { return false })
	k, txt := BootMessage("r", BootInfo{}, false, time.Now())
	if p.Push(k, txt) {
		t.Fatal("friend instance pushed the boot message")
	}
	for i := 0; i < 5; i++ {
		if p.NoteRefusal(Refusal{Class: ClassStopWiden, TraderID: "t", Subject: "MNQ long"}) {
			t.Fatal("friend instance pushed a storm")
		}
	}
	p.Flush()
	if len(f.sent()) != 0 {
		t.Fatalf("friend instance sent %q", f.sent())
	}
}

// ── item 6: refusal storm ────────────────────────────────────────────────────

// MUTANT (shown RED): raise StormThreshold's comparison to > (n > Threshold) ->
// the third refusal does not fire -> RED. MUTANT 2: drop the `st.active` latch
// -> a 4th refusal fires a second push in the same episode -> RED.
func TestStormFiresOncePerEpisodeAtTheThreshold(t *testing.T) {
	f := &flaky{}
	p, c := newClocked(f.send)
	r := Refusal{Class: ClassStopWiden, TraderID: "trader-1", Subject: "MNQ long sig-1", Detail: "new stop 29850.00 would widen from 29900.00"}
	for i := 1; i < StormThreshold; i++ {
		if p.NoteRefusal(r) {
			t.Fatalf("refusal %d fired below the threshold %d", i, StormThreshold)
		}
		c.add(time.Minute)
	}
	if !p.NoteRefusal(r) {
		t.Fatalf("refusal %d (the threshold) did not fire", StormThreshold)
	}
	for i := 0; i < 5; i++ { // the same episode keeps repeating
		c.add(time.Minute)
		if p.NoteRefusal(r) {
			t.Fatal("a second push inside one episode")
		}
	}
	p.Flush()
	got := f.sent()
	if len(got) != 1 {
		t.Fatalf("want exactly one storm push, got %d: %q", len(got), got)
	}
	for _, want := range []string{"REFUSAL STORM", "stop-widen REFUSED", "3 times in 10 min", "trader-1", "MNQ long sig-1", "29850.00"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("storm text %q lacks %q", got[0], want)
		}
	}
}

func TestStormNeedsTheThresholdInsideTheWindow(t *testing.T) {
	f := &flaky{}
	p, c := newClocked(f.send)
	r := Refusal{Class: ClassSwingBE, TraderID: "t", Subject: "arm-1"}
	for i := 0; i < 6; i++ { // one every 6 min: never 3 inside 10
		if p.NoteRefusal(r) {
			t.Fatalf("a slow trickle (every 6 min) fired at refusal %d", i+1)
		}
		c.add(6 * time.Minute)
	}
}

// Different positions / traders / classes never add up into one storm.
func TestStormScopesAreIndependent(t *testing.T) {
	f := &flaky{}
	p, _ := newClocked(f.send)
	refs := []Refusal{
		{Class: ClassStopWiden, TraderID: "A", Subject: "p1"},
		{Class: ClassStopWiden, TraderID: "B", Subject: "p1"},
		{Class: ClassStopWiden, TraderID: "A", Subject: "p2"},
		{Class: ClassSwingBE, TraderID: "A", Subject: "p1"},
	}
	for round := 0; round < StormThreshold-1; round++ {
		for _, r := range refs {
			if p.NoteRefusal(r) {
				t.Fatalf("2 refusals in each of 4 scopes fired: %+v", r)
			}
		}
	}
}

// After a full quiet window the episode is over; the next storm is a NEW push.
func TestStormNewEpisodeAfterAQuietWindow(t *testing.T) {
	f := &flaky{}
	p, c := newClocked(f.send)
	r := Refusal{Class: ClassCloseRejected, TraderID: "t", Subject: "MNQ long"}
	fire := func() bool {
		var fired bool
		for i := 0; i < StormThreshold; i++ {
			fired = p.NoteRefusal(r) || fired
			c.add(10 * time.Second)
		}
		return fired
	}
	if !fire() {
		t.Fatal("first storm did not fire")
	}
	c.add(StormWindow + time.Minute)
	if !fire() {
		t.Fatal("a storm after a quiet window must be a new episode with its own push")
	}
	p.Flush()
	if n := len(f.sent()); n != 2 {
		t.Fatalf("want 2 pushes (one per episode), got %d", n)
	}
}

// A storm of one class has its own budget: it cannot starve FEED DOWN.
func TestStormClassDoesNotStarveFeedDown(t *testing.T) {
	f := &flaky{}
	p, c := newClocked(f.send)
	for ep := 0; ep < maxPerKindPerHour+2; ep++ {
		for i := 0; i < StormThreshold; i++ {
			p.NoteRefusal(Refusal{Class: ClassStopWiden, TraderID: "t", Subject: "p"})
		}
		c.add(StormWindow + time.Minute)
	}
	if !p.Push("feed-down|e1", "FEED DOWN") {
		t.Fatal("a refusal storm used up the FEED DOWN budget")
	}
}

// Long ids are shortened, never printed as [redacted].
func TestStormTextNamesTheTraderAndKeepsLongIdsReadable(t *testing.T) {
	f := &flaky{}
	p, _ := newClocked(f.send)
	SetTraderLabeler(func(id string) string { return "Mess" })
	t.Cleanup(func() { SetTraderLabeler(nil) })
	long := strings.Repeat("a1b2c3d4-", 8) + "xyz" // 75 chars, token-shaped
	r := Refusal{Class: ClassStopNotApplied, TraderID: long, Subject: "mentor-isb-" + long + " leg 2"}
	for i := 0; i < StormThreshold; i++ {
		p.NoteRefusal(r)
	}
	p.Flush()
	got := f.sent()
	if len(got) != 1 {
		t.Fatalf("want 1 push, got %q", got)
	}
	if !strings.Contains(got[0], "trader Mess") {
		t.Errorf("must name the trader by display name: %q", got[0])
	}
	if strings.Contains(got[0], "[redacted]") {
		t.Errorf("a long id was redacted instead of shortened: %q", got[0])
	}
}

func TestStormMemoryIsBounded(t *testing.T) {
	p, c := newClocked((&flaky{}).send)
	for i := 0; i < stormMaxScopes*3; i++ {
		p.NoteRefusal(Refusal{Class: ClassStopWiden, TraderID: "t", Subject: fmt.Sprintf("p%d", i)})
		c.add(StormWindow + time.Second)
	}
	p.mu.Lock()
	n := len(p.storms)
	p.mu.Unlock()
	if n > stormMaxScopes+1 {
		t.Fatalf("storm table grew to %d scopes", n)
	}
}
