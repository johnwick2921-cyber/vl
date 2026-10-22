package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newLS(t *testing.T) *LevelStateStore {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.LevelState()
}

func seeded(t *testing.T, ls *LevelStateStore, key string, consumed bool, fresh string, age time.Duration) *LevelStateDB {
	t.Helper()
	now := time.Now()
	bin := 0
	fmt.Sscanf(key, "MNQ|equal-H/L||%d", &bin)
	if err := ls.EnsureLevel(&LevelStateDB{
		Symbol: "MNQ", LevelType: "equal-H/L", BinIndex: bin, Price: 30000,
		Consumed: consumed, Freshness: fresh,
		CreatedAt: now.Add(-age), UpdatedAt: now.Add(-age),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cur, _ := ls.Get(key)
	if cur == nil {
		t.Fatalf("seed: no row for %s", key)
	}
	return cur
}

// cmeDayStartLocal mirrors the 17:00 America/Chicago session-day boundary so
// the aging test can pick instants that are UNAMBIGUOUSLY n session-days old
// regardless of the wall-clock time the test runs at.
func cmeDayStartLocal(t time.Time) time.Time {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		chicago = time.UTC
	}
	ct := t.In(chicago)
	boundary := time.Date(ct.Year(), ct.Month(), ct.Day(), 17, 0, 0, 0, chicago)
	if ct.Hour() < 17 {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary
}

// P1d — a consumed level role-flips for its session-day, then the scar HEALS:
// next 1-2 session-days → C (tested×2), from the 3rd session-day → B (tested).
func TestP1DAgedFreshness(t *testing.T) {
	now := time.Now()
	start := cmeDayStartLocal(now)
	sameDay := start.Add(time.Minute)
	if sameDay.After(now) {
		sameDay = now // first minute of the session-day edge
	}
	tests := []struct {
		name     string
		consumed bool
		fresh    string
		last     time.Time
		want     string
	}{
		{"fresh A untouched", false, "A", now.Add(-time.Hour), "A"},
		{"tested B untouched", false, "B", now.Add(-10 * time.Hour), "B"},
		{"burned this session-day", true, "done", sameDay, "done"},
		{"burned yesterday", true, "done", start.Add(-time.Minute), "C"},
		{"burned 2 session-days ago", true, "done", start.AddDate(0, 0, -1).Add(-time.Minute), "C"},
		{"burned 3 session-days ago", true, "done", start.AddDate(0, 0, -2).Add(-time.Minute), "B"},
		{"burned a week ago", true, "done", start.AddDate(0, 0, -6).Add(-time.Minute), "B"},
	}
	for _, tc := range tests {
		cur := &LevelStateDB{Consumed: tc.consumed, Freshness: tc.fresh,
			CreatedAt: tc.last, UpdatedAt: tc.last}
		if got := AgedFreshness(cur, now); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

// TestP1DAgedFreshnessUsesConsumptionTime — bug-hunt #53: EnsureLevel refreshes
// updated_at every cycle for every seated level, which pinned a burned level at
// "done" forever. AgedFreshness must age from the consuming touch
// (last_play_ms), not updated_at. Mutant (age from UpdatedAt) → RED.
func TestP1DAgedFreshnessUsesConsumptionTime(t *testing.T) {
	now := time.Now()
	start := cmeDayStartLocal(now)
	burned := start.AddDate(0, 0, -3).Add(-time.Minute) // 3+ session-days ago
	cur := &LevelStateDB{Consumed: true, Freshness: FreshnessDone,
		LastPlayMs: burned.UnixMilli(), CreatedAt: burned, UpdatedAt: now}
	if got := AgedFreshness(cur, now); got != FreshnessB {
		t.Fatalf("consumed 3 session-days ago with a fresh updated_at must heal to B, got %q", got)
	}
}

// P1b — ResetBurns clears burned rows (to C, never back to fresh) and the
// cutoff limits the blast radius to pre-fix rows.
func TestP1BResetBurns(t *testing.T) {
	ls := newLS(t)
	oldKey := MakeLevelKey("", "MNQ", "equal-H/L", "", 1)
	newKey := MakeLevelKey("", "MNQ", "equal-H/L", "", 2)
	_ = seeded(t, ls, oldKey, true, "done", 30*time.Hour) // burned "pre-fix"
	_ = seeded(t, ls, newKey, true, "done", time.Minute)  // burned "post-fix"

	cutoff := time.Now().Add(-time.Hour).UnixMilli()
	n, err := ls.ResetBurns(cutoff)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if n != 1 {
		t.Fatalf("cutoff reset must touch exactly the pre-fix row, got %d", n)
	}
	old, _ := ls.Get(oldKey)
	if old == nil || old.Consumed || old.Freshness != FreshnessC {
		t.Fatalf("pre-fix row must reset to C/unconsumed, got %+v", old)
	}
	neu, _ := ls.Get(newKey)
	if neu == nil || !neu.Consumed {
		t.Fatalf("post-fix row must stay burned, got %+v", neu)
	}

	// No cutoff → everything burned resets (repair of ALL false burns).
	n2, err := ls.ResetBurns(0)
	if err != nil || n2 != 1 {
		t.Fatalf("full reset must touch the remaining burned row, got n=%d err=%v", n2, err)
	}
}
