package kernel

import (
	"testing"
	"time"

	"vl/market"
)

// TestClassifyFeedFreshnessPrecedence pins the CTO precedence contract:
// delayed > stale/missing > fresh; UNKNOWN broker mode is never a reject reason
// on its own, and unsupported modes normalize to UNKNOWN (never realtime).
func TestClassifyFeedFreshnessPrecedence(t *testing.T) {
	nowMs := int64(1_700_000_000_000)
	freshBar := nowMs - 1_000   // 1s old — current
	staleBar := nowMs - 600_000 // 10 min old — stale

	cases := []struct {
		name        string
		mode        string
		newestBarMs int64
		want        FeedFreshness
	}{
		{"delayed+fresh", "delayed", freshBar, FeedFreshDelayed},
		{"delayed+stale", "delayed", staleBar, FeedFreshDelayed},
		{"realtime+stale", "realtime", staleBar, FeedFreshStale},
		{"realtime+fresh", "realtime", freshBar, FeedFreshFresh},
		{"unknown+fresh", "", freshBar, FeedFreshFresh},
		{"unknown+missing", "", 0, FeedFreshStale},
		{"unsupported+stale", "weird", staleBar, FeedFreshStale},
		{"unsupported+fresh", "weird", freshBar, FeedFreshFresh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyFeedFreshness(tc.mode, tc.newestBarMs, 60_000, nowMs)
			if got != tc.want {
				t.Fatalf("ClassifyFeedFreshness(%q, %d, 60s, now) = %v, want %v",
					tc.mode, tc.newestBarMs, got, tc.want)
			}
		})
	}
}

// TestFeedModeReadsWiredProvider — feedMode() returns "" (UNKNOWN) when no
// provider is wired, and the wired value otherwise. The reconnect-invalidation
// itself is pinned in provider/ninjatrader (TestFeedModeInvalidatedOnDisconnect).
func TestFeedModeReadsWiredProvider(t *testing.T) {
	if got := feedMode(); got != "" {
		t.Fatalf("feedMode() with no wired provider = %q, want \"\"", got)
	}

	prev := market.FuturesFeedMode
	t.Cleanup(func() { market.FuturesFeedMode = prev })
	market.FuturesFeedMode = func() string { return "delayed" }
	if got := feedMode(); got != "delayed" {
		t.Fatalf("feedMode() with wired provider = %q, want \"delayed\"", got)
	}
}

// TestEntryFeedStaleDelayed pins the production call site: the mentor /
// entry-admission entry verdict (EntryFeedStale) refuses a NEW entry when the
// broker mode is explicitly delayed, even with a fresh bar; an unknown mode +
// fresh bar is not refused by mode.
func TestEntryFeedStaleDelayed(t *testing.T) {
	prev := market.FuturesFeedMode
	t.Cleanup(func() { market.FuturesFeedMode = prev })

	now := time.Now()
	market.FuturesFeedMode = func() string { return "delayed" }
	stale, why := EntryFeedStale("MNQ", now.UnixMilli(), now)
	if !stale || why != "broker feed mode is delayed" {
		t.Fatalf("delayed mode + fresh bar: EntryFeedStale = (%v, %q), want (true, \"broker feed mode is delayed\")", stale, why)
	}

	market.FuturesFeedMode = func() string { return "" }
	if stale, _ := EntryFeedStale("MNQ", now.UnixMilli(), now); stale {
		t.Fatalf("unknown mode + fresh bar: EntryFeedStale = stale, want not stale")
	}
}
