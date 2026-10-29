package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/store"
)

// ── #55 m449 — THE AUDIT'S AGE-OUT IS NOT THE GATE'S RELEASE ────────────────
//
// Review #55 (reproduced): the gap audit reconciles the holes it tracks against
// the store each pass, and a hole wholly before ITS window (auditDays CME
// sessions, default 7) was removed from the tracker outright. A hole that was
// RETIRED (beyond NT8's replay) or UNFILLED was therefore forgotten days before
// it left the windows a decision reads (deep ~8.7 sessions, swing4h ~18, the
// mentor RTH key-level leg 90 days), and the m445 verdict read CLEAR while the
// minutes were still gone - with a "clears by itself <window instant>" promise
// on the owner's screen that was false.
//
// These pins drive the PRODUCTION pass (auditPass, the function nt8-bar-gap-audit
// runs) and ask the function the trader package asks (IntegrityVerdictFor /
// IntegrityVerdictForMentor).
//
// MUTANTS (shown RED in the report):
//   A1  the age-out branch calls removeLocked without retainRetiredLocked
//       -> TestAuditAgeOutKeepsAnUnfilledHoleInTheGate RED
//   A2  retainRetiredLocked is a no-op -> same pin RED

func auditAgeFixture(t *testing.T) (*ghFixture, time.Time) {
	t.Helper()
	armIntegrity(t)
	f := newGHFixture(t)
	f.tr.auditDays = 7
	f.stream(ct(2026, 10, 6, 10, 0), ct(2026, 10, 6, 10, 5))
	f.stream(ct(2026, 10, 6, 10, 20), ct(2026, 10, 6, 10, 21)) // hole A: 10-06 10:06-10:19, never refilled
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49)) // hole B, live
	waitFor(t, "both holes open", func() bool { return len(f.holes()) >= 2 })
	return f, f.clk.Now()
}

func holeWindows(v IntegrityVerdict) string {
	var w []string
	for _, h := range v.Holes {
		w = append(w, h.Window)
	}
	return strings.Join(w, " | ")
}

func TestAuditAgeOutKeepsAnUnfilledHoleInTheGate(t *testing.T) {
	f, det := auditAgeFixture(t)
	f.tr.tick(det.Add(61 * time.Second))
	now := ct(2026, 10, 15, 7, 50)

	// precondition: hole A (10-06 10:06) is inside the deep window (12000 open
	// min back from 10-15 07:50 starts 10-02) and the audit window (7 sessions)
	// starts 10-06 17:00, i.e. AFTER hole A ended.
	winFrom := auditWindowStart(now, f.tr.auditDays, f.tr.isOpen)
	if !winFrom.After(ct(2026, 10, 6, 10, 20)) {
		t.Fatalf("fixture: the audit window %v must start after hole A", winFrom)
	}
	before := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now)
	if !strings.Contains(holeWindows(before), "10-06 10:06–10:20") {
		t.Fatalf("precondition: hole A is in the deep-window verdict: %+v", before.Holes)
	}

	f.tr.auditPass(now) // PRODUCTION pass: reconciles and ages out

	for _, tc := range []struct {
		name   string
		mentor bool
		open   int
	}{
		{"deep", false, IntegrityDeepOpenMin},
		{"swing4h", false, IntegritySwingOpenMin},
		{"mentor deep", true, IntegrityDeepOpenMin},
		{"mentor swing4h", true, IntegritySwingOpenMin},
	} {
		var v IntegrityVerdict
		if tc.mentor {
			v = IntegrityVerdictForMentor("MNQ", tc.open, now)
		} else {
			v = IntegrityVerdictFor("MNQ", tc.open, now)
		}
		if !v.Blocked || !strings.Contains(holeWindows(v), "10-06 10:06–10:20") {
			t.Fatalf("%s: an UNFILLED hole still inside the window must keep the gate blocked after the audit ages it out of ITS window; got blocked=%v holes=%q", tc.name, v.Blocked, holeWindows(v))
		}
	}
	if un := IntegrityUnfilledHoles("MNQ"); !strings.Contains(func() string {
		var s []string
		for _, h := range un {
			s = append(s, h.Window)
		}
		return strings.Join(s, "|")
	}(), "10-06 10:06–10:20") {
		t.Fatalf("the resume message must still list the unfilled hole: %+v", un)
	}
}

// The hole is released ONLY by a measurement (the store holds the minutes) or by
// leaving every window - never by the audit's clock.
func TestAuditAgedOutHoleIsReleasedByAMeasuredFill(t *testing.T) {
	f, det := auditAgeFixture(t)
	f.tr.tick(det.Add(61 * time.Second))
	now := ct(2026, 10, 15, 7, 50)
	f.tr.auditPass(now)
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now); !strings.Contains(holeWindows(v), "10-06 10:06–10:20") {
		t.Fatalf("precondition: aged-out hole still gates: %+v", v.Holes)
	}
	// NT8 history reload carries the minutes: the store now holds them
	f.putOpen("1m", ct(2026, 10, 6, 10, 6), ct(2026, 10, 6, 10, 20), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 6, 10, 22), ct(2026, 10, 14, 7, 48), store.BarSourceHistorical)
	f.tr.remeasureRetired(now.Add(2 * time.Minute)) // the production re-measure (tick calls it)
	if v := IntegrityVerdictFor("MNQ", IntegrityDeepOpenMin, now); strings.Contains(holeWindows(v), "10-06 10:06–10:20") {
		t.Fatalf("a measured fill must release the hole: %+v", v.Holes)
	}
}

// Outside EVERY window it may finally go: >100 days (past the 90-day mentor
// key-level reach) the re-measure drops it, and the verdict (which ignores a
// hole outside the window anyway) is clear. 89 days: still kept.
func TestRetiredHoleKeptWithinReachAndDroppedOutsideEveryWindow(t *testing.T) {
	f, det := auditAgeFixture(t)
	f.tr.tick(det.Add(61 * time.Second))
	f.tr.auditPass(ct(2026, 10, 15, 7, 50))
	f.tr.mu.Lock()
	n := len(f.tr.retired)
	f.tr.mu.Unlock()
	if n == 0 {
		t.Fatal("precondition: the aged-out hole is retained")
	}
	f.tr.remeasureRetired(ct(2027, 1, 3, 10, 0)) // 89 days after 10-06 -> inside the 90-day reach
	f.tr.mu.Lock()
	kept := len(f.tr.retired)
	f.tr.mu.Unlock()
	if kept == 0 {
		t.Fatal("an unfilled hole within the 90-day key-level reach must stay retained")
	}
	f.tr.remeasureRetired(ct(2027, 2, 1, 10, 0)) // 118 days: outside every window
	f.tr.mu.Lock()
	left := len(f.tr.retired)
	f.tr.mu.Unlock()
	if left != 0 {
		t.Fatalf("outside every window the hole may go, still %d retained", left)
	}
}
