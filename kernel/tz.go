package kernel

import (
	"sync"
	"time"
)

// CanonicalZone is THE timezone for every rendered time in the system:
// America/Chicago (CT). CME futures session windows, lunch no-trade,
// last-entry/flat times and killzones are all CT wall-clock values, so
// every prompt, card, digest and log line must render CT with an explicit
// label. Host-local time is never used for anything the model or owner
// sees (owner standing rule 2026-08-19).
const CanonicalZone = "America/Chicago"

var (
	ctLocOnce sync.Once
	ctLoc     *time.Location
)

// CTLocation returns the cached canonical America/Chicago location.
// It falls back to UTC ONLY when the zone database is missing on the host
// — the sole degradation path, never local time.
func CTLocation() *time.Location {
	ctLocOnce.Do(func() {
		loc, err := time.LoadLocation(CanonicalZone)
		if err != nil {
			loc = time.UTC
		}
		ctLoc = loc
	})
	return ctLoc
}

// NowCT returns the current time IN the canonical zone.
func NowCT() time.Time { return time.Now().In(CTLocation()) }

// FormatCT renders t as "2006-01-02 15:04 CT" in the canonical zone.
func FormatCT(t time.Time) string {
	return t.In(CTLocation()).Format("2006-01-02 15:04") + " CT"
}

// ClockCT renders t as "15:04 CT" in the canonical zone.
func ClockCT(t time.Time) string {
	return t.In(CTLocation()).Format("15:04") + " CT"
}

// WeekdayClockCT renders t as "Mon 15:04 CT" — a weekday-prefixed clock for the
// session open/close lines (m150): "CLOSED (Friday close); reopens Sun 17:00 CT".
// The "Mon 15:04" layout carries an explicit weekday prefix + CT suffix, so it is
// not a bare clock layout (the TZ guard's single-source rule still holds).
func WeekdayClockCT(t time.Time) string {
	return t.In(CTLocation()).Format("Mon 15:04") + " CT"
}

// ClockCTAndUTC renders the single labelled dual read the prompts carry:
// "07:06 CT (12:06 UTC)" — one labelled zone per view, no ambiguity.
func ClockCTAndUTC(t time.Time) string {
	return ClockCT(t) + " (" + t.UTC().Format("15:04") + " UTC)"
}

// ClockCTSeconds renders t at SECONDS resolution, "15:04:05 CT" — added for
// the P7 prompt Snapshot line (ledger-close 2026-08-19) so every stored prompt
// self-documents the exact instant its data was assembled. New layouts live
// ONLY here (the TZ-guard test enforces it).
func ClockCTSeconds(t time.Time) string {
	return t.In(CTLocation()).Format("15:04:05") + " CT"
}

// TableTimeCT renders a bar timestamp for the labelled "Time(CT)" table
// rows: "01-02 15:04" in the canonical zone. The zone label lives in the
// table header; the conversion still routes through CTLocation.
func TableTimeCT(t time.Time) string {
	return t.In(CTLocation()).Format("01-02 15:04")
}

// CloseClockCT renders a session close time for DISPLAY: "12:00 CT".
// CloseHHMMCT renders the same instant as bare "12:00" for DATA — the shape the
// session registry and the EOD-flat comparison speak.
//
// Both live here because the tz guard (TestTZGuardSingleTimeSource) refuses a
// bare "15:04" layout anywhere outside this file, and the session calendar had
// five of them. One timezone source; no component does its own clock math.
//
// The two are deliberately separate: the first draft routed everything through
// ClockCT, which appends " CT", and that fed "12:00 CT" into a minutes parser
// that expects "12:00" — the flat comparison silently stopped matching.
func CloseClockCT(t time.Time) string {
	return ClockCT(t)
}

// CloseHHMMCT is the bare wire/comparison form.
func CloseHHMMCT(t time.Time) string {
	return t.In(CTLocation()).Format("15:04")
}
