package ownerpush

import (
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

func TestPushRateLimited(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	clk := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	p.SetClockForTest(func() time.Time { return clk })
	sent := 0
	for i := 0; i < 20; i++ {
		if p.Push("k"+string(rune('a'+i)), "m") {
			sent++
		}
	}
	p.Flush()
	if sent != maxPerHour || len(s.got()) != maxPerHour {
		t.Fatalf("rate limit: accepted %d / delivered %d, want %d", sent, len(s.got()), maxPerHour)
	}
	if p.Dropped() != 20-maxPerHour {
		t.Fatalf("dropped = %d", p.Dropped())
	}
	clk = clk.Add(61 * time.Minute) // the window rolls
	if !p.Push("later", "m") {
		t.Fatal("after an hour the limit must reopen")
	}
	p.Flush()
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
