package ownerpush

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// m467 (release #62) - THE 11-03 OUTAGE. The box lost its internet at ~02:00 CT
// ("dial tcp: lookup api.telegram.org"). FEED DOWN "feed-down|feeddown:2026-11-02:02:00 CT"
// and two data-truth pauses failed 10 sends each (last 03:31:39) and were
// DISCARDED as stale at 03:31-03:41 (holdTTL 90 min) - the network came back
// before the 16:10 data report, and nobody re-sent them. The owner never
// learned the bot was blind for 15 hours. These pins replay that shape.

var errDNS = errors.New(`telegram bot api: Post "https://api.telegram.org/bot[redacted]/getMe": dial tcp: lookup api.telegram.org: no such host`)

// drive advances the clock in steps, running the production retry loop at its
// own cadence (retryLoopEvery), flushing the worker after each pass.
func drive(p *Pusher, c *clock, d time.Duration) {
	for el := time.Duration(0); el < d; el += retryLoopEvery {
		c.add(retryLoopEvery)
		p.Retry()
		p.Flush()
	}
}

// MUTANT (RED before m467): Retry discards every held message older than
// holdTTL whatever its kind -> the FEED DOWN of 02:11 is gone by 03:41 and is
// never delivered when Telegram answers again at 16:10.
func TestP0AlertSurvivesAFourteenHourOutage(t *testing.T) {
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 8, 11, 2, 0, time.UTC) // 02:11:02 CT
	p.Push("feed-down|feeddown:2026-11-02:02:00 CT", "FEED DOWN since 02:00 CT")
	p.Push("data_truth_indicator_lookback|MNQ|1793692917970", "DATA TRUTH PAUSED")
	p.Flush()
	drive(p, c, 14*time.Hour) // -> ~16:11 CT, still failing
	if st := p.Stats(); st.Stale != 0 || st.Held != 2 {
		t.Fatalf("a P0 held through an outage must never go stale: %+v", st)
	}
	f.set(nil) // the network is back
	drive(p, c, 6*time.Minute)
	got := strings.Join(f.sent(), "\n---\n")
	for _, want := range []string{"FEED DOWN since 02:00 CT", "DATA TRUTH PAUSED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("held P0 %q not delivered after the outage; sent:\n%s", want, got)
		}
	}
}

// The recovery SUMMARY: the first delivery after an outage of >= outageSummaryMin
// tells the owner how long alerts could not reach him and how many failed.
func TestOutageRecoverySummaryIsPushed(t *testing.T) {
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 8, 2, 9, 0, time.UTC) // 02:02:09 CT
	p.Push("data_truth_indicator_lookback|MNQ|1", "A")
	p.Push("feed-down|ep", "B")
	p.Push("data_truth_indicator_lookback_session|MNQ|2", "C")
	p.Flush()
	drive(p, c, 2*time.Hour)
	p.Cancel("feed-down|ep") // the episode ended while the network was down
	drive(p, c, 12*time.Hour)
	f.set(nil)
	drive(p, c, 6*time.Minute)
	var summary string
	for _, m := range f.sent() {
		if strings.Contains(m, "could not reach you") {
			summary = m
		}
	}
	if summary == "" {
		t.Fatalf("no outage summary after recovery; sent %q", f.sent())
	}
	for _, want := range []string{"from 11-03 02:02 CT", "3 alert(s) failed to send", "1 withdrawn"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q lacks %q", summary, want)
		}
	}
	if strings.Contains(summary, "api.telegram.org") || strings.Contains(summary, "bot") && strings.Contains(summary, "getMe") {
		t.Fatalf("the summary must not carry the transport error (a URL that once held the token): %q", summary)
	}
	if st := p.Stats(); st.Outages != 1 {
		t.Fatalf("outage counter: %+v", st)
	}
}

// A short blip (one failed send, success 30 s later) is not an outage worth a
// message.
func TestShortBlipHasNoSummary(t *testing.T) {
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	p.Push("feed-down|ep", "B")
	p.Flush()
	f.set(nil)
	drive(p, c, 2*time.Minute)
	for _, m := range f.sent() {
		if strings.Contains(m, "could not reach you") {
			t.Fatalf("a %s blip pushed an outage summary: %q", 2*time.Minute, m)
		}
	}
}

// Recovery is IMMEDIATE: once any send succeeds, every held message's backoff
// is lifted, so a P0 whose next attempt was minutes away goes out now.
func TestSuccessLiftsEveryBackoff(t *testing.T) {
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	p.Push("feed-down|ep", "FEED")
	p.Flush()
	drive(p, c, 3*time.Hour) // backoff now at its cap
	f.set(nil)
	p.Push("boot|1", "UP") // a fresh message goes out first
	p.Flush()
	c.add(time.Second)
	p.Retry()
	p.Flush()
	found := false
	for _, m := range f.sent() {
		found = found || m == "FEED"
	}
	if !found {
		t.Fatalf("the held P0 waited out its backoff after a send had succeeded: %q", f.sent())
	}
}

// P0 retries back off to at most p0RetryBackoffMax (5 min), so a returning
// network is noticed within minutes; a non-P0 keeps the 15 min cap.
func TestP0BackoffCap(t *testing.T) {
	if got := backoffForKey("feed-down|x", 20); got != p0RetryBackoffMax {
		t.Fatalf("P0 backoff cap %s, want %s", got, p0RetryBackoffMax)
	}
	if got := backoffForKey("data-report|x", 20); got != retryBackoffMax {
		t.Fatalf("non-P0 backoff cap %s, want %s", got, retryBackoffMax)
	}
}

// Every production P0 kind is classed P0 (a kind that is not would be
// discarded after holdTTL again).
func TestP0KindsClassified(t *testing.T) {
	for _, k := range []string{
		"feed-down|feeddown:2026-11-02:02:00 CT", "feed-still-down|e|2", "feed-back|e",
		"data_truth_cross_frame|MNQ|1", "data_truth_still|cross_frame|MNQ|1|1",
		"data-paused-abc|1", "data-still-paused-abc|1|2", "data-resumed-abc|1",
		"trader_down_no_evaluator|t|e", "dead_man_reconcile_stuck|t|e", "unprotected_position|t|e",
		"protection_partial|t|e", "protection_pending_stuck|t|e", "protection_over_covered|t|e",
		"position_close_rejected|t|e", "stop-move-not-applied|t|e", "boot|1", "push-outage|1",
	} {
		if !IsP0Key(k) {
			t.Errorf("%q is not classed P0", k)
		}
	}
	for _, k := range []string{"data-report|2026-11-02|1", "bar-hole-unfilled|x", "k"} {
		if IsP0Key(k) {
			t.Errorf("%q must not be P0", k)
		}
	}
}

// The hold cap evicts a non-P0 before any P0.
func TestHoldCapEvictsNonP0First(t *testing.T) {
	f := &flaky{fail: errDNS}
	p, _ := newClocked(f.send)
	p.mu.Lock()
	p.holdMsgLocked(heldMsg{key: "data-report|x|1", text: "R", at: p.now()})
	for i := 0; i < holdCap; i++ {
		p.holdMsgLocked(heldMsg{key: fmt.Sprintf("feed-down|e%d", i), text: "F", at: p.now()})
	}
	keys := map[string]bool{}
	for _, h := range p.held {
		keys[h.key] = true
	}
	p.mu.Unlock()
	if keys["data-report|x|1"] || !keys["feed-down|e0"] {
		t.Fatalf("the hold cap must evict the non-P0 first: %v", keys)
	}
}

// Across a restart: a P0 held for longer than holdTTL is still restored (the
// 11-03 outage outlived the old 90-minute window), and the outage's start
// survives, so the summary after the restart still reads from 02:02.
func TestP0HeldIsRestoredAfterHoldTTLAndOutageSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 8, 2, 9, 0, time.UTC)
	enableState(t, p, path)
	p.Push("feed-down|e", "FEED DOWN")
	p.Flush()
	waitState(t, path, func(d *stateDisk) bool { return len(d.Held) == 1 && d.OutageSince > 0 })
	q, c2 := newClocked(f.send)
	c2.t = c.now().Add(3 * time.Hour)
	if info := enableState(t, q, path); info.Restored != 1 || q.Held() != 1 {
		t.Fatalf("a P0 older than holdTTL must be restored: %+v held=%d", info, q.Held())
	}
	f.set(nil)
	drive(q, c2, 2*time.Minute)
	var summary string
	for _, m := range f.sent() {
		if strings.Contains(m, "could not reach you") {
			summary = m
		}
	}
	if !strings.Contains(summary, "from 11-03 02:02 CT") {
		t.Fatalf("outage start lost across the restart: %q (sent %q)", summary, f.sent())
	}
}

// The boot line's outage policy reads the constants (class 45: boot lines are
// read, never literal).
func TestOutagePolicyReadsTheConstants(t *testing.T) {
	got := OutagePolicy()
	for _, want := range []string{p0RetryBackoffMax.String(), outageSummaryMin.String(), holdTTL.String(), "NEVER dropped"} {
		if !strings.Contains(got, want) {
			t.Fatalf("OutagePolicy() %q lacks %q", got, want)
		}
	}
}
