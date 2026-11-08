package ownerpush

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// m471 L3-5 — a P0 kind is not held back by the 3/hour per-kind budget. The kind
// is the key text before the first "|", and per-trader P0 kinds put the trader
// AFTER it ("unprotected_position|<trader>|<event>"), so every trader on the
// box shared ONE 3/hour bucket: three traders' "no stop - one is being placed"
// used the hour and trader 1's "naked, stop behind the market - FLATTENING"
// waited up to 60 minutes.
//
// MUTANT (shown RED): apply maxPerKindPerHour to P0 keys again -> "the 4th
// unprotected_position P0 was refused by the shared per-kind budget" -> RED.
func TestFourthP0OfOneKindIsSentAtOnce(t *testing.T) {
	var mu sync.Mutex
	var got []string
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	p := NewE(func(s string) error { mu.Lock(); got = append(got, s); mu.Unlock(); return nil })
	p.SetEnabledForTest(func() bool { return true })
	p.SetClockForTest(func() time.Time { return now })

	for _, tr := range []string{"t1", "t2", "t3"} {
		if !p.Push("unprotected_position|"+tr+"|reconnect:1", "vl P0 - "+tr+" position has NO stop - one is being placed") {
			t.Fatalf("first pushes should be accepted (%s)", tr)
		}
	}
	p.Flush()
	if !p.Push("unprotected_position|t1|flatten:2", "vl P0 - t1 naked and the stop is already behind the market - FLATTENING") {
		t.Fatalf("the 4th unprotected_position P0 (trader t1 flatten) was refused by the shared per-kind budget; held=%d", p.Held())
	}
	p.Flush()
	mu.Lock()
	delivered := false
	for _, s := range got {
		if strings.Contains(s, "FLATTENING") {
			delivered = true
		}
	}
	mu.Unlock()
	if !delivered || p.Held() != 0 {
		t.Fatalf("the flatten P0 must be delivered at once (held=%d)", p.Held())
	}
}

// The exemption is bounded: a P0 kind has a runaway backstop (a loop minting a
// new episode id per pass), and a non-P0 kind keeps its 3/hour budget.
func TestP0KindRunawayBackstopAndNonP0BudgetStillHold(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	p := NewE(func(string) error { return nil })
	p.SetEnabledForTest(func() bool { return true })
	p.SetClockForTest(func() time.Time { return now })

	for i := 0; i < maxP0PerKindPerHour; i++ {
		if !p.Push(fmt.Sprintf("stop-move-not-applied|t1|e%d", i), "stop move not applied") {
			t.Fatalf("P0 #%d of one kind refused below the runaway backstop (%d)", i+1, maxP0PerKindPerHour)
		}
		p.Flush()
	}
	if p.Push("stop-move-not-applied|t1|runaway", "stop move not applied") {
		t.Fatalf("P0 #%d of one kind in an hour must be held by the runaway backstop", maxP0PerKindPerHour+1)
	}
	if p.Held() != 1 {
		t.Fatalf("the refused P0 must be HELD, not lost: held=%d", p.Held())
	}

	for i := 0; i < maxPerKindPerHour; i++ {
		if !p.Push(fmt.Sprintf("data-report|d%d", i), "report") {
			t.Fatalf("non-P0 push #%d refused inside its budget", i+1)
		}
		p.Flush()
	}
	if p.Push("data-report|d-over", "report") {
		t.Fatalf("a non-P0 kind must keep its %d/hour budget", maxPerKindPerHour)
	}
}

// m471 L3-3 — at boot, restored P0s that belong to an episode the PREVIOUS
// process was tracking (feed down, a data / trading pause, an AI outage, their
// reminders and scope changes) are withdrawn and named in the boot message, not
// re-sent as current; money / position P0s and episode ENDS are still sent, and
// the withdrawn keys are NOT marked told, so a condition still true after the
// restart can be raised again. Start (the production boot) runs RestoreAtBoot.
//
// MUTANT (shown RED): supersededLabel returns "" for every key -> "episode-bound
// restored P0 delivered as current" -> RED. MUTANT 2: Start calls EnableState
// instead of RestoreAtBoot -> "Start must supersede" -> RED.
func TestRestoreAtBootSupersedesEpisodeP0sAndKeepsMoneyP0s(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC) // 02:00 CT
	enableState(t, p, path)
	episode := []string{
		"feed-down|feeddown:2026-11-03:02:00 CT", "feed-still-down|1762156800|1",
		"data-paused-t1|1762156800", "data-paused-t1|1762156800|widen1", "data-still-paused-t1|1762156800|1",
		"data_truth_bar_gap|MNQ|1762156800000", "data_truth_still_bar_gap|MNQ|1762156800000|1",
		"ai-payment|t1|ai402:2026-11-03T08:00", "ai-still-down|t1|1", "ai-failing|t1|ai-failing:x",
	}
	kept := []string{
		"daily-loss|t1|daily-loss:2026-11-03", "untracked-position|t1|untracked:MNQ:LONG:1",
		"unprotected_position|t1|flatten:MNQ:LONG:1", "goroutine_panic|t1|panic:x",
		"feed-back|feeddown:2026-11-02:23:00 CT", "data-resumed-t1|1762100000", "data_truth_bar_gap|MNQ|1762100000000|clear",
	}
	for _, k := range append(append([]string{}, episode...), kept...) {
		p.Push(k, "MSG "+k)
		p.Flush()
	}
	waitState(t, path, func(d *stateDisk) bool { return len(d.Held) == len(episode)+len(kept) })

	q, c2 := newClocked(f.send)
	c2.t = c.now().Add(5 * time.Hour)
	info := q.RestoreAtBoot(path)
	t.Cleanup(q.CloseState)
	if info.Restored != len(kept) || q.Held() != len(kept) {
		t.Fatalf("want %d money / end P0s kept, got restored=%d held=%d", len(kept), info.Restored, q.Held())
	}
	_, text := BootMessage("abc123", info, false, c2.now())
	for _, want := range []string{"FEED DOWN 11-03 02:00 CT", "superseded by this restart", "and ", "more", "tells you again if one is still true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("boot message lacks %q: %q", want, text)
		}
	}
	if r := []rune(text); len(r) > maxTextRunes {
		t.Fatalf("boot message is %d runes, Scrub would cut it at %d: %q", len(r), maxTextRunes, text)
	}
	f.set(nil)
	drive(q, c2, 2*time.Minute)
	sent := strings.Join(f.sent(), "\n")
	for _, k := range episode {
		if strings.Contains(sent, "MSG "+k+"\n") || strings.HasSuffix(sent, "MSG "+k) {
			t.Errorf("episode-bound restored P0 delivered as current: %q", k)
		}
	}
	for _, k := range kept {
		if !strings.Contains(sent, "MSG "+k) {
			t.Errorf("a money / episode-end P0 restored at boot was not delivered: %q", k)
		}
	}
	// withdrawn, not told: the new process can raise a still-current one again
	if !q.Push("feed-down|feeddown:2026-11-03:02:00 CT", "FEED DOWN (still)") {
		t.Fatal("a superseded key must not be marked told - a feed still down must be able to reach the owner")
	}
}

func TestStartRunsTheBootSupersede(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errDNS}
	p, _ := newClocked(f.send)
	enableState(t, p, path)
	p.Push("feed-down|feeddown:x", "FEED DOWN")
	p.Flush()
	waitState(t, path, func(d *stateDisk) bool { return len(d.Held) == 1 })

	restore := Use(nil)
	defer restore()
	info := Start(func(string) error { return errDNS }, path)
	defMu.RLock()
	started := def
	defMu.RUnlock()
	if started != nil {
		t.Cleanup(started.CloseState)
	}
	if len(info.Superseded) != 1 || info.Restored != 0 {
		t.Fatalf("Start must supersede the restored FEED DOWN at boot: %+v", info)
	}
}
