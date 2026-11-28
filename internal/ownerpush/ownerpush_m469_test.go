package ownerpush

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// m469 — the P0 owner pushes grew from 9 kinds to 20+. At 3 per kind per hour
// that is far above the 30/hour all-kinds backstop, so a burst of new P0 kinds
// could have used the backstop up and held FEED DOWN back. A P0 is now neither
// refused by the backstop nor counted in it; every kind keeps its own budget.

// MUTANT (shown RED): restore `if len(p.sentAt) >= maxPerHour` without the
// !p0 leg in admitLocked -> the FEED DOWN after 30 other messages is refused.
func TestP0IsNotHeldBackByTheAllKindsBackstop(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	p.SetClockForTest(func() time.Time { return time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC) })
	// 30 non-P0 messages over 10 kinds: each kind inside its budget, the backstop full.
	for k := 0; k < 10; k++ {
		for i := 0; i < maxPerKindPerHour; i++ {
			if !p.Push(fmt.Sprintf("data-report-%d|%d", k, i), "r") {
				t.Fatalf("fixture: non-P0 %d/%d refused before the backstop filled", k, i)
			}
			p.Flush() // keep the 16-slot queue empty: only the budgets are under test
		}
	}
	if p.Push("data-report-x|1", "r") {
		t.Fatal("fixture: the backstop must refuse a 31st non-P0 message")
	}
	for _, k := range []string{"feed-down|e1", "ai-payment|t1|ai402:x", "trader_panic|t1|panic:t1:2026-11-05"} {
		if !p.Push(k, "P0") {
			t.Fatalf("P0 %q was held back by the all-kinds backstop", k)
		}
		p.Flush()
	}
	p.Flush()
}

// The other half: P0 sends do not use the backstop up, so a burst of P0s does
// not hold a daily data report back. A P0 kind is still bounded per kind - by
// the runaway backstop maxP0PerKindPerHour since m471 L3-5 (the 3/hour budget
// was shared by every trader on the box and held a 4th P0 for up to an hour).
// MUTANT (shown RED): append every admitted key to sentAt (drop the !p0 guard)
// -> the data report after 30 P0 sends is refused.
func TestP0SendsDoNotUseUpTheBackstopButKeepTheirKindBudget(t *testing.T) {
	s := &sink{}
	p := New(s.send)
	p.SetClockForTest(func() time.Time { return time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC) })
	p0Kinds := []string{"feed-down", "feed-back", "ai-payment", "ai-still-down", "ai-back", "ai-failing",
		"daily-loss", "untracked-position", "trader_panic", "goroutine_panic", "protection_unverified"}
	for _, k := range p0Kinds {
		for i := 0; i < maxP0PerKindPerHour; i++ {
			if !p.Push(fmt.Sprintf("%s|t1|%d", k, i), "p0") {
				t.Fatalf("P0 %s #%d refused inside its own budget", k, i)
			}
			p.Flush()
		}
		if p.Push(k+"|t1|over", "p0") {
			t.Fatalf("P0 kind %s got past its own %d/hour runaway backstop", k, maxP0PerKindPerHour)
		}
	}
	if !p.Push("data-report|2026-11-05|1", "report") {
		t.Fatalf("%d P0 sends used up the all-kinds backstop", len(p0Kinds)*maxP0PerKindPerHour)
	}
	p.Flush()
}

// Every m469 kind is classed P0 (never stale-discarded in an outage).
func TestM469KindsAreP0(t *testing.T) {
	for _, k := range []string{
		"ai-payment|t|ai402:2026-11-04T02:10", "ai-still-down|t|1|2", "ai-back|t|e", "ai-failing|t|e",
		"daily-loss|t|daily-loss:2026-11-05", "untracked-position|t|e", "trader_panic|t|panic:t:2026-11-05",
		"goroutine_panic|t|e", "protection_unverified|t|unverified:1",
	} {
		if !IsP0Key(k) {
			t.Errorf("%q is not classed P0", k)
		}
	}
	if IsP0Key("ai-balance|t|e") {
		t.Error("the P1 AI balance warning must not be classed P0")
	}
}

// The boot line's P0 list is the constant itself (class 45: read, never literal).
// MUTANT (shown RED): put the old literal list back in OutagePolicy -> "ai-payment" missing.
func TestOutagePolicyListsEveryP0Kind(t *testing.T) {
	got := OutagePolicy()
	for _, k := range p0KindPrefixes {
		if !strings.Contains(got, k) {
			t.Errorf("OutagePolicy() does not name P0 kind %q: %s", k, got)
		}
	}
	if !strings.Contains(got, fmt.Sprintf("exempt from the %d/hour total", maxPerHour)) {
		t.Errorf("OutagePolicy() must state the backstop exemption: %s", got)
	}
}
