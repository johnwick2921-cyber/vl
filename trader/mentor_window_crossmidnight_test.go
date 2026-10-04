package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// The window start is the most recent occurrence of the start time at or before
// now, so a window that crosses midnight is active after 00:00. Mutant: build
// `open` on TODAY's date only (the old code) → the 00:30 pin goes RED.
func TestMentorWindowCrossesMidnight(t *testing.T) {
	ct := kernel.CTLocation()
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, ct) }

	cases := []struct {
		name    string
		start   string
		minutes int
		now     time.Time
		active  bool
	}{
		{"23:00+120 active at 23:30 same evening", "23:00", 120, at(2, 23, 30), true},
		{"23:00+120 active at 00:30 next day", "23:00", 120, at(3, 0, 30), true},
		{"23:00+120 active at 00:59 next day", "23:00", 120, at(3, 0, 59), true},
		{"23:00+120 inactive at 01:00 (exclusive end)", "23:00", 120, at(3, 1, 0), false},
		{"23:00+120 inactive at 12:00", "23:00", 120, at(3, 12, 0), false},
		{"23:00+120 inactive at 22:59", "23:00", 120, at(3, 22, 59), false},
		{"08:30+60 unchanged: active at 08:30", "08:30", 60, at(2, 8, 30), true},
		{"08:30+60 unchanged: active at 09:29", "08:30", 60, at(2, 9, 29), true},
		{"08:30+60 unchanged: inactive at 09:30", "08:30", 60, at(2, 9, 30), false},
		{"08:30+60 unchanged: inactive at 08:29", "08:30", 60, at(2, 8, 29), false},
		{"0 minutes = always active", "08:30", 0, at(2, 3, 0), true},
		{"-1 (stored 'no window') = always active", "08:30", -1, at(2, 3, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			active, why := mentorWindowActive(tc.start, tc.minutes, tc.now)
			if active != tc.active {
				t.Fatalf("active=%v (why %q), want %v", active, why, tc.active)
			}
			if !active && why == "" {
				t.Fatal("an inactive window must say why")
			}
		})
	}
	if active, why := mentorWindowActive("bogus", 60, at(3, 0, 30)); active || why == "" {
		t.Fatalf("a bad start must refuse: active=%v why=%q", active, why)
	}
}

// Production call site: a saved "no window" (-1) lets an entry through at 10:00
// CT, while the unset default (0 → 60) still refuses it; SWING stays exempt.
func TestMentorWindowGateNoWindowStoredValue(t *testing.T) {
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 10, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "ISB"}

	unset := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if refuse, _ := unset.mentorWindowGate(in); !refuse {
		t.Fatal("unset window (default 08:30+60) must refuse an entry at 10:00 CT")
	}
	none := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowMinutes: -1})
	if refuse, why := none.mentorWindowGate(in); refuse {
		t.Fatalf("a saved no-window (-1) must allow 10:00 CT: %s", why)
	}
	overnight := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "23:00", MentorWindowMinutes: 120})
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 3, 0, 30, 0, 0, ct) }
	if refuse, why := overnight.mentorWindowGate(in); refuse {
		t.Fatalf("23:00+120 must allow 00:30 CT: %s", why)
	}
}
