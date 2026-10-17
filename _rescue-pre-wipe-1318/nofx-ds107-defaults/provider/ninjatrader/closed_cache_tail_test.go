package ninjatrader

import "testing"

// ClosedCacheTail: the live path reads final CLOSED bars from the cache tail
// (the frame only ever carries the forming bar) — forming tail bars are
// excluded, the window trims, and ascending order is preserved.
func TestClosedCacheTail(t *testing.T) {
	now := int64(1_800_000_000_000)
	cache := map[string][]Bar{
		"1m": {
			{T: now - 3*60_000},        // closed (3 min ago)
			{T: now - 2*60_000},        // closed
			{T: now - 60_000},          // closed
			{T: now - 10_000, C: 99.5}, // forming — must be excluded
		},
	}
	get := func(symbol, tf string) []Bar { return cache[tf] }

	got := ClosedCacheTail(get, "MNQ", "1m", now, 8)
	if len(got) != 3 {
		t.Fatalf("ClosedCacheTail = %d bars, want 3 (forming excluded)", len(got))
	}
	if got[0].T != now-3*60_000 || got[2].T != now-60_000 {
		t.Fatalf("ClosedCacheTail not ascending by T: %+v", got)
	}

	// Window trims to the most-recent closed bars.
	got = ClosedCacheTail(get, "MNQ", "1m", now, 2)
	if len(got) != 2 || got[0].T != now-2*60_000 {
		t.Fatalf("ClosedCacheTail window trim failed: %+v", got)
	}

	// Empty cache → no bars, no panic.
	if got := ClosedCacheTail(func(string, string) []Bar { return nil }, "MNQ", "1m", now, 8); len(got) != 0 {
		t.Fatalf("ClosedCacheTail empty cache = %+v want nil", got)
	}
}
