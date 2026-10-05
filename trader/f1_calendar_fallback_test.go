package trader

// F1 — calendar fallback P1 pins (CTO 2026-10-05): the frozen static file must
// NEVER apply another week's HH:MM schedule to a date it does not cover. Pins at
// the production call site t1WindowsFor (slice-missing → calendarFallbackWindows).
//
// Static fixture: two T1 events on DIFFERENT dates at DIFFERENT times, so the old
// buggy behavior (apply ALL static events to any date) is distinguishable from
// the new date-aware behavior.

import (
	"os"
	"path/filepath"
	"testing"

	"vl/kernel"
)

// f1StaticFile writes the two-event fixture and points VL_CALENDAR_STATIC at it.
// 09-03T18:00Z → 13:00 CT (m=780) → [765, 795]; 09-10T20:00Z → 15:00 CT (m=900) → [885, 915].
func f1StaticFile(t *testing.T) {
	t.Helper()
	staticPath := filepath.Join(t.TempDir(), "static_t1.json")
	os.WriteFile(staticPath, []byte(`[
	  {"time":"2026-09-03T18:00:00Z","currency":"USD","title":"ISM Services PMI","impact":"T1"},
	  {"time":"2026-09-10T20:00:00Z","currency":"USD","title":"ECB Presser (fake static)","impact":"T1"}
	]`), 0o644)
	t.Setenv("VL_CALENDAR_STATIC", staticPath)
}

var f1Sess = &kernel.SessionDef{Name: "NY"}

// TestF1FallbackUncoveredSundayHasNoWindows — 2026-10-04 is a Sunday (non-trading
// day), and the static file does NOT cover it: the OLD code applied September's
// two windows; the NEW code must return ZERO windows.
func TestF1FallbackUncoveredSundayHasNoWindows(t *testing.T) {
	at, _ := f0Trader(t)
	f1StaticFile(t)
	if got := at.t1WindowsFor("2026-10-04", f1Sess); len(got) != 0 {
		t.Fatalf("uncovered Sunday got %d windows %+v, want 0 (no September schedule leaked)", len(got), got)
	}
}

// TestF1FallbackUncoveredWeekdayHoldsStandardWindow — 2026-10-05 is an uncovered
// trading Monday: exactly ONE conservative window, the standard 07:30 CT print
// [07:20, 07:35) = {Start:440, End:455}.
func TestF1FallbackUncoveredWeekdayHoldsStandardWindow(t *testing.T) {
	at, _ := f0Trader(t)
	f1StaticFile(t)
	got := at.t1WindowsFor("2026-10-05", f1Sess)
	if len(got) != 1 {
		t.Fatalf("uncovered weekday got %d windows %+v, want exactly 1", len(got), got)
	}
	if got[0].Start != 7*60+20 || got[0].End != 7*60+35 {
		t.Fatalf("uncovered weekday window = %+v, want {Start:%d End:%d} (07:20-07:35 CT)",
			got[0], 7*60+20, 7*60+35)
	}
}

// TestF1FallbackCoveredDateUsesOwnStaticEvents — 2026-09-03 IS covered by the
// static file: only that date's event (13:00 CT → [765, 795]) must gate, NOT the
// 09-10 event (15:00 CT → [885, 915]).
func TestF1FallbackCoveredDateUsesOwnStaticEvents(t *testing.T) {
	at, _ := f0Trader(t)
	f1StaticFile(t)
	got := at.t1WindowsFor("2026-09-03", f1Sess)
	want := []kernel.CTWindow{{Start: 13*60 - 15, End: 13*60 + 15}}
	if len(got) != len(want) {
		t.Fatalf("covered date got %d windows %+v, want %d %+v (only that date's static event)",
			len(got), got, len(want), want)
	}
	for i := range want {
		if got[i].Start != want[i].Start || got[i].End != want[i].End {
			t.Fatalf("covered date window[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
