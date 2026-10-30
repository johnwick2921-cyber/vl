package ownerpush

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type sink struct {
	mu   sync.Mutex
	msgs []string
	gate chan struct{} // when non-nil the sender blocks until it is closed
}

func (s *sink) send(text string) {
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	s.msgs = append(s.msgs, text)
	s.mu.Unlock()
}
func (s *sink) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

func TestPushDedupesAnEpisodeKey(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	if !p.Push("feed-down|ep1", "down") {
		t.Fatal("first push refused")
	}
	if p.Push("feed-down|ep1", "down") {
		t.Fatal("the same episode key must not push twice")
	}
	if !p.Push("feed-back|ep1", "back") {
		t.Fatal("a different key must push")
	}
	p.Flush()
	if n := len(s.got()); n != 2 {
		t.Fatalf("want 2 sends, got %d", n)
	}
}

// The all-kinds backstop: unknown-shaped keys (no "|") share ONE bucket, so the
// per-kind budget binds first and only maxPerKindPerHour of them go out.
func TestPushUnknownKeysShareOneBudget(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	clk := newTestClock(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
	p.SetClockForTest(clk.Now)
	sent := 0
	for i := 0; i < 20; i++ {
		if p.Push("k"+string(rune('a'+i)), "m") {
			sent++
		}
	}
	p.Flush()
	if sent != maxPerKindPerHour || len(s.got()) != maxPerKindPerHour {
		t.Fatalf("budget: accepted %d / delivered %d, want %d", sent, len(s.got()), maxPerKindPerHour)
	}
	if p.Dropped() != 20-maxPerKindPerHour {
		t.Fatalf("dropped = %d", p.Dropped())
	}
	clk.Advance(61 * time.Minute) // the window rolls
	if !p.Push("later", "m") {
		t.Fatal("after an hour the budget must reopen")
	}
	p.Flush()
}

// m438: one flooding kind must never use up what FEED DOWN / trader-down /
// protection pushes need. MUTANT (shown RED): make kindOf return one constant
// bucket (the old shared budget) -> the later feed-down push is refused -> RED.
func TestOneKindCannotStarveAnother(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	p.SetClockForTest(func() time.Time { return time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC) })
	accepted := 0
	for i := 0; i < 50; i++ { // a naked-position loop minting a new id per pass
		if p.Push(fmt.Sprintf("unprotected_position|t1|unprotected:MNQ:Long:%d", i), "naked") {
			accepted++
		}
	}
	if accepted != maxPerKindPerHour {
		t.Fatalf("the flooding kind got %d pushes, want exactly its budget %d", accepted, maxPerKindPerHour)
	}
	for _, k := range []string{"feed-down|feeddown:e1", "trader_down_no_evaluator|t1|e", "protection_partial|t1|e", "feed-back|e1"} {
		if !p.Push(k, "important") {
			t.Fatalf("%s was starved by an unrelated kind", k)
		}
	}
	p.Flush()
}

// m438: a dropped message stays ELIGIBLE. The alert row is written once, so
// without the hold the P0 was lost for good. MUTANT (shown RED): drop the
// p.holdLocked call in Push -> Retry has nothing to re-offer -> RED.
func TestDroppedPushIsRetriedWhenTheBudgetReopens(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	clk := newTestClock(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
	p.SetClockForTest(clk.Now)
	for i := 0; i < maxPerKindPerHour; i++ {
		p.Push(fmt.Sprintf("unprotected_position|t1|e%d", i), "naked")
	}
	if p.Push("unprotected_position|t1|late", "LATE-P0") {
		t.Fatal("over budget must be refused")
	}
	if p.Held() != 1 {
		t.Fatalf("held = %d, want the dropped P0 held", p.Held())
	}
	if n := p.Retry(); n != 0 {
		t.Fatalf("no budget yet, retry accepted %d", n)
	}
	clk.Advance(61 * time.Minute)
	if n := p.Retry(); n != 1 {
		t.Fatalf("the budget reopened: retry accepted %d, want 1", n)
	}
	p.Flush()
	got := s.got()
	if len(got) != maxPerKindPerHour+1 || got[len(got)-1] != "LATE-P0" {
		t.Fatalf("the held P0 was not delivered: %q", got)
	}
	if p.Held() != 0 || p.Retry() != 0 {
		t.Fatal("a delivered message must leave the hold (no double send)")
	}
}

// A hold older than holdTTL is stale news and is discarded, never delivered.
func TestHeldPushExpires(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	clk := newTestClock(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
	p.SetClockForTest(clk.Now)
	for i := 0; i < maxPerKindPerHour; i++ {
		p.Push(fmt.Sprintf("feed-down|e%d", i), "d")
	}
	p.Push("feed-down|old", "OLD")
	clk.Advance(holdTTL + time.Minute)
	if n := p.Retry(); n != 0 || p.Held() != 0 {
		t.Fatalf("stale hold must be discarded, accepted %d held %d", n, p.Held())
	}
}

// Cancel withdraws a held FEED DOWN once the feed is back.
func TestCancelWithdrawsAHeldMessage(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	clk := newTestClock(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
	p.SetClockForTest(clk.Now)
	for i := 0; i < maxPerKindPerHour; i++ {
		p.Push(fmt.Sprintf("feed-down|e%d", i), "d")
	}
	p.Push("feed-down|ep", "DOWN")
	p.Cancel("feed-down|ep")
	clk.Advance(61 * time.Minute)
	if p.Retry() != 0 || p.Push("feed-down|ep", "DOWN") {
		t.Fatal("a cancelled message must never be delivered late")
	}
}

// m438: a sender that never returns must not wedge the single worker. MUTANT
// (shown RED): call p.send inline in deliver (no deadline) -> the second message
// never arrives -> RED.
func TestStuckSenderCannotHangTheWorker(t *testing.T) {
	var mu sync.Mutex
	var got []string
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	p := New(func(text string) {
		if text == "hang" {
			<-block
		}
		mu.Lock()
		got = append(got, text)
		mu.Unlock()
	})
	p.SetSendTimeoutForTest(100 * time.Millisecond)
	p.SetClockForTest(func() time.Time { return time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC) })
	p.Push("feed-down|a", "hang")
	p.Push("feed-back|a", "next")
	done := make(chan struct{})
	go func() { p.Flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker is wedged on a send that never returns")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "next" {
		t.Fatalf("the message after the stuck one must go out, got %q", got)
	}
}

func TestPushNeverBlocksOnASlowSender(t *testing.T) {
	s := &sink{gate: make(chan struct{})}
	p := New(s.send)
	p.SetClockForTest(func() time.Time { return time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC) })
	done := make(chan struct{})
	go func() {
		for i := 0; i < 3*queueCap; i++ {
			p.Push("k"+string(rune('A'+i)), "m")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Push blocked on a stuck sender")
	}
	close(s.gate)
	p.Flush()
}

func TestPushNoSenderAndFriendAreNoOps(t *testing.T) {
	if New(nil).Push("k", "m") {
		t.Fatal("no sender must be a no-op")
	}
	s := &sink{}
	p := New(s.send)
	p.SetEnabledForTest(func() bool { return false })
	if p.Push("k", "m") {
		t.Fatal("a friend instance must never push")
	}
	p.Flush()
	if len(s.got()) != 0 {
		t.Fatal("friend instance sent")
	}
	var nilp *Pusher
	if nilp.Push("k", "m") {
		t.Fatal("nil pusher must be a no-op")
	}
}

func TestFriendInstanceEnvRefusesViaDefaultEnabled(t *testing.T) {
	t.Setenv("VL_FRIEND_INSTANCE", "1")
	s := &sink{}
	p := New(s.send)
	if p.Push("k", "m") {
		t.Fatal("VL_FRIEND_INSTANCE=1 must refuse the push (production default check)")
	}
}

func TestScrubRedactsCredentialsAndCaps(t *testing.T) {
	in := "bot token=123456:ABCdefGHIjklMNOpqrSTUvwxYZ0123456789abc and api_key: sk-live-xyz and " + strings.Repeat("a", 40)
	out := Scrub(in)
	for _, bad := range []string{"123456:ABC", "sk-live-xyz", strings.Repeat("a", 33)} {
		if strings.Contains(out, bad) {
			t.Fatalf("secret %q survived: %s", bad, out)
		}
	}
	if long := Scrub(strings.Repeat("ab ", 1000)); len([]rune(long)) > maxTextRunes+1 {
		t.Fatalf("not capped: %d", len([]rune(long)))
	}
}

func TestDefaultPushNoOpWhenNotStarted(t *testing.T) {
	restore := Use(nil)
	defer restore()
	if Push("k", "m") {
		t.Fatal("no default pusher = no-op")
	}
}

// testClock is the fake clock the budget/hold tests hand to SetClockForTest.
// The pusher's worker goroutine reads it (onSent -> now) while the test moves
// it forward, so it must be synchronised: a plain captured time.Time variable
// was a data race the CI -race run caught on TestHeldPushExpires (PR #616).
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock(t time.Time) *testClock { return &testClock{t: t} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}
