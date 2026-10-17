package kernel

import (
	"strings"
	"testing"
)

// F6 (2026-08-30) — clock-hold decision + news-window widening fixtures.
// Pure, no clock involved: the drift is injected.

func TestClockHoldDecision(t *testing.T) {
	const warn, tol = int64(30_000), int64(60_000)
	cases := []struct {
		name      string
		drift     int64
		have      bool
		wantDefer bool
		wantWiden int64
	}{
		{"no measurement fails open", 61_000, false, false, 0},
		{"below warn", 29_999, true, false, 0},
		{"at warn widens, authoring proceeds", 30_000, true, false, 30_000},
		{"warn band widens", 41_000, true, false, 41_000},
		{"at tolerance still authors, widens", 60_000, true, false, 60_000},
		// Positive drift is ambiguous (closed-market bars look identical) —
		// authoring proceeds, windows widen. Only NEGATIVE drift (feed in the
		// future — provably broken local clock) defers.
		{"positive breach authors, widens", 61_000, true, false, 61_000},
		{"negative breach defers (clock BEHIND)", -61_000, true, true, 61_000},
		{"tiny drift", 500, true, false, 0},
	}
	for _, c := range cases {
		deferA, widen := ClockHoldDecision(c.drift, c.have, warn, tol)
		if deferA != c.wantDefer || widen != c.wantWiden {
			t.Errorf("%s: got (defer=%v widen=%d), want (defer=%v widen=%d)",
				c.name, deferA, widen, c.wantDefer, c.wantWiden)
		}
	}
}

func TestWidenCTWindows(t *testing.T) {
	base := []CTWindow{{Start: 780, End: 810, Label: "FOMC 13:00 CT ±15m"}}

	if w := WidenCTWindows(base, 0); w[0].Start != 780 || w[0].End != 810 {
		t.Fatalf("zero drift must not touch windows: %+v", w[0])
	}
	if w := WidenCTWindows(base, 41_000); w[0].Start != 779 || w[0].End != 811 {
		t.Fatalf("41s drift must widen by 1m each side: %+v", w[0])
	}
	if w := WidenCTWindows(base, 61_000); w[0].Start != 778 || w[0].End != 812 {
		t.Fatalf("61s drift must widen by 2m each side: %+v", w[0])
	}
	if w := WidenCTWindows(base, -90_000); w[0].Start != 778 || w[0].End != 812 {
		t.Fatalf("negative 90s drift must widen by 2m each side: %+v", w[0])
	}

	// Midnight wrap: start must wrap forward, end must not wrap spuriously.
	wrap := []CTWindow{{Start: 2, End: 5, Label: "00:02 CT event"}}
	if w := WidenCTWindows(wrap, 3*60_000); w[0].Start != 1439 || w[0].End != 8 {
		t.Fatalf("wrap case: got start=%d end=%d, want 1439/8", w[0].Start, w[0].End)
	}
	// Original slice must be untouched (pure).
	if base[0].Start != 780 || base[0].End != 810 {
		t.Fatal("WidenCTWindows mutated its input")
	}
}

func TestT1NoTradeLinesDriftCarriesWidening(t *testing.T) {
	evs := []PlannerCalendarEvent{{TimeCT: "13:00", Impact: "T1", Title: "FOMC"}}
	plain := T1NoTradeLines(evs)
	wide := T1NoTradeLinesDrift(evs, 61_000)
	if len(plain) != 1 || len(wide) != 1 {
		t.Fatalf("want 1 line each, got %d/%d", len(plain), len(wide))
	}
	if !strings.Contains(wide[0], "(clock drift)") {
		t.Fatalf("drift line must carry the widening marker: %q", wide[0])
	}
	if strings.Contains(plain[0], "(clock drift)") {
		t.Fatalf("plain line must not carry drift: %q", plain[0])
	}
}
