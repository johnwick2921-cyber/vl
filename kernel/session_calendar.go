package kernel

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ── THE CME SESSION CALENDAR (2026-09-07) ────────────────────────────────────
//
// SHORTENED SESSIONS ARE TRADING SESSIONS (owner ruling 2026-09-07).
//
// This replaced a boolean. isCMEHoliday() answered yes/no and the gate treated
// every holiday as a FULL closure — the old code said so itself: "for v1 we
// treat them as full closures and refuse to trade. Refine in Plan 3 if it
// becomes restrictive." On 2026-09-07 it became restrictive. MNQ traded 980
// bars across 153.50 points on Labor Day while the gate called the market shut,
// every cycle was skipped, no LONDON plan was ever read, and the 3-minute
// closed-market backoff logged 156 "overruns" against a 2-minute interval.
//
// A date is now one of three things, and the third is the one the boolean could
// not express: CLOSED, SHORTENED (trades until its stated close, then flat), or
// NORMAL (absent from the calendar — the weekly rules alone decide).
//
// THE CALENDAR IS DATA. It lives in session_calendar.json, embedded so it ships
// with the binary and cannot go missing at runtime, and every date cites a
// source that says whether it was verified or decided.

//go:embed session_calendar.json
var sessionCalendarJSON []byte

// SessionClass is what a date IS.
type SessionClass string

const (
	// SessionClosed — no trading at all.
	SessionClosed SessionClass = "closed"
	// SessionShortened — trades normally until CloseCT, then flat.
	SessionShortened SessionClass = "shortened"
	// SessionNormal — the weekly rules alone decide. Never stored; returned
	// for any date the calendar does not list.
	SessionNormal SessionClass = "normal"
)

// SessionDay is one calendar entry, with the provenance that lets a reader tell
// a published fact from a decision.
type SessionDay struct {
	Date    string       `json:"date"`
	Class   SessionClass `json:"class"`
	CloseCT string       `json:"close_ct,omitempty"`
	Name    string       `json:"name"`
	Source  string       `json:"source"`
	// Unestablished marks a date whose treatment nobody has sourced. Such a row
	// is CLOSED (C4: the safe side) and is NAMED on the boot line — a guessed
	// trading day is worse than a missed one (owner ruling 2026-09-07).
	Unestablished bool `json:"unestablished,omitempty"`
	// Superseded records a value this row replaced, with its provenance, so a
	// ruling can be revisited against the source instead of re-litigated from
	// memory. Never dropped silently.
	Superseded string `json:"superseded,omitempty"`
}

type sessionCalendarFile struct {
	CoveredYears []int        `json:"covered_years"`
	Dates        []SessionDay `json:"dates"`
}

var sessionCal sessionCalendarFile

func init() {
	if err := json.Unmarshal(sessionCalendarJSON, &sessionCal); err != nil {
		// The calendar is embedded, so this can only fail if someone shipped
		// invalid JSON — which the loader test catches before it ever boots.
		panic("kernel: session_calendar.json is not valid JSON: " + err.Error())
	}
}

// SessionCalendarCoversYear reports whether the calendar has been maintained
// for this year. An uncovered year is NOT a year of normal days — it is a year
// nobody has checked, and the boot line says so out loud.
func SessionCalendarCoversYear(year int) bool {
	for _, y := range sessionCal.CoveredYears {
		if y == year {
			return true
		}
	}
	return false
}

// SessionDayFor returns the calendar entry for this date, if it has one.
func SessionDayFor(t time.Time) (SessionDay, bool) {
	key := t.In(CTLocation()).Format("2006-01-02")
	for i := range sessionCal.Dates {
		if sessionCal.Dates[i].Date == key {
			return sessionCal.Dates[i], true
		}
	}
	return SessionDay{Date: key, Class: SessionNormal}, false
}

// parseCloseCT turns "12:00" into that instant on the given CT date. A malformed
// or absent time is reported, never guessed: a shortened day whose close cannot
// be read must not silently become a full trading day.
func parseCloseCT(ct time.Time, hhmm string) (time.Time, bool) {
	parts := strings.SplitN(strings.TrimSpace(hhmm), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return time.Time{}, false
	}
	loc := CTLocation()
	c := ct.In(loc)
	return time.Date(c.Year(), c.Month(), c.Day(), h, m, 0, 0, loc), true
}

// weeklyCMEOpen is the ordinary week: the rules that applied before any
// calendar existed, unchanged.
func weeklyCMEOpen(ct time.Time) bool {
	switch ct.Weekday() {
	case time.Saturday:
		return false
	case time.Sunday:
		return ct.Hour() >= 17
	case time.Friday:
		return ct.Hour() < 16
	default: // Mon-Thu
		return ct.Hour() != 16
	}
}

// SessionCalendarBootLine names TODAY's classification and, when it has one, the
// time it closes. Every field READ from the calendar the gate actually consults
// (A11) — including the source, so the owner can see whether today's answer was
// published or decided.
func SessionCalendarBootLine(now time.Time) string {
	ct := now.In(CTLocation())
	st := SessionStateAt(now)

	today := string(st.Class)
	closeTxt := "—"
	if st.Class == SessionShortened && st.HasClose {
		closeTxt = CloseClockCT(st.Close)
	}
	if st.CloseUnreadable {
		closeTxt = "UNKNOWN (unreadable close_ct — treated as closed)"
	}
	source := "default"
	if st.Listed {
		source = sourceTag(st.Source)
	}
	if st.UncoveredFallback {
		source = fmt.Sprintf("YEAR %d NOT COVERED — superseded boolean deciding", ct.Year())
	}
	if st.Unestablished {
		source = "unestablished — closed on the safe side until sourced"
	}
	return fmt.Sprintf("session calendar: today=%s close=%s source=%s · unknown-dates=%d · dates=%d covered=%v · backoff=%s (interval=%s)",
		today, closeTxt, source,
		SessionCalendarUnestablishedCount(), len(sessionCal.Dates), sessionCal.CoveredYears,
		SessionClosedBackoff(), SessionScanIntervalHint())
}

// SessionClosedBackoff and SessionScanIntervalHint let the boot line READ the
// two durations D4 turns on rather than printing literals (A11). The backoff is
// owned by the trader loop; the interval is per-trader configuration, so the
// hint says so instead of inventing a number.
func SessionClosedBackoff() string { return closedBackoffForBootLine }

// SessionScanIntervalHint reports where the interval comes from, not a value the
// calendar cannot know (A24: never a plausible number).
func SessionScanIntervalHint() string { return "per-trader scan_interval_minutes" }

// closedBackoffForBootLine is set once at boot by the trader package, which owns
// the constant. Empty until then, and it prints as UNKNOWN rather than 0.
var closedBackoffForBootLine = "UNKNOWN (not reported by the loop yet)"

// SetClosedBackoffForBootLine is how the trader package hands the calendar the
// real backoff so the boot line quotes the enforcing value (A11).
func SetClosedBackoffForBootLine(d time.Duration) {
	closedBackoffForBootLine = d.String()
}

// ── THE JOIN: calendar + weekly rules → one resolved answer ──────────────────
//
// The 233 lines above were authored by lane session-calendar-554049f5 and left
// DORMANT — production call sites 0 for every function in this file. Nothing
// below re-derives them; this is the wiring they were missing.

// SessionState is TODAY, resolved: what the calendar says, whether anyone has
// checked the year, and — for a shortened day — the instant it closes.
//
// Every field is READ (A11). Nothing here is a literal, and no date or session
// time appears in Go: the calendar is data (A24).
type SessionState struct {
	Date        string
	Class       SessionClass
	Name        string
	Source      string
	Listed      bool // the calendar has a row for this date
	YearCovered bool // someone has maintained the calendar for this year
	Close       time.Time
	HasClose    bool
	// CloseUnreadable is true when a shortened row carries a close_ct that
	// cannot be parsed. Such a day is demoted to CLOSED rather than traded to
	// a guessed time — an unreadable close is not a full trading day.
	CloseUnreadable bool
	// Unestablished — this date's treatment is not sourced; it is closed on the
	// safe side and named as such wherever the classification is shown.
	Unestablished bool
	// UncoveredFallback is true when the year is not covered and the superseded
	// boolean decided this date instead. Named, never silent.
	UncoveredFallback bool
}

// SessionStateAt resolves the classification for an instant. It is the ONE
// place the calendar and the fallback meet, so nothing downstream can consult
// half of the answer.
//
// The safe side is CLOSED in every ambiguous branch: an unreadable close time,
// an unrecognised class, and an uncovered year all refuse to trade rather than
// assume a normal day. Placing a trade is the destructive branch here.
func SessionStateAt(now time.Time) SessionState {
	ct := now.In(CTLocation())
	st := SessionState{
		Date:        ct.Format("2006-01-02"),
		YearCovered: SessionCalendarCoversYear(ct.Year()),
	}

	// YEAR NOT COVERED — nobody has checked this year. Fall back to the
	// superseded boolean, which errs closed, and say so on every surface.
	if !st.YearCovered {
		st.UncoveredFallback = true
		st.Source = "year not covered by the calendar — the superseded holiday boolean is deciding, which errs CLOSED"
		if isCMEHoliday(ct) {
			st.Class, st.Name = SessionClosed, "holiday, uncovered year"
			return st
		}
		st.Class = SessionNormal
		return st
	}

	day, listed := SessionDayFor(ct)
	st.Listed = listed
	if !listed {
		// D2: a date absent from a COVERED year is a normal date.
		st.Class = SessionNormal
		st.Source = "not listed — a normal date in a covered year"
		return st
	}
	st.Name, st.Source = day.Name, day.Source
	st.Unestablished = day.Unestablished

	switch day.Class {
	case SessionClosed:
		st.Class = SessionClosed
	case SessionShortened:
		close, ok := parseCloseCT(ct, day.CloseCT)
		if !ok {
			// D2/C4: present but unclassifiable → CLOSED, and it says so.
			st.Class, st.CloseUnreadable = SessionClosed, true
			return st
		}
		st.Class, st.Close, st.HasClose = SessionShortened, close, true
	case SessionNormal:
		// A date listed EXPLICITLY as normal — researched and found ordinary.
		// Without this arm it fell to the default below and was traded as
		// CLOSED, which is how 2026-12-31 (a sourced normal session) would have
		// stayed shut. Found by folding half_days.json, not by a test.
		st.Class = SessionNormal
	default:
		// An unrecognised class is a typo in data that ships inside the binary.
		// It must not degrade to a normal trading day (A24).
		st.Class, st.CloseUnreadable = SessionClosed, true
	}
	return st
}

// SessionEarlyCloseCT is THE ONE accessor for a day's early close, in "HH:MM" CT.
//
// ONE FACT, ONE OWNER (owner ruling 2026-09-07). Before the fold this answer
// lived in three places with two key conventions and, on three dates, two
// different times: half_days.json at the repo root, SessionRegistry.HalfDays in
// the store, and this calendar. The gate stopped trading at 12:00 on days the
// sourced file said 12:15. Now the gate, the session registry and the EOD flat
// all resolve through here.
func SessionEarlyCloseCT(now time.Time) (string, bool) {
	st := SessionStateAt(now)
	if st.Class == SessionShortened && st.HasClose {
		return CloseHHMMCT(st.Close), true
	}
	return "", false
}

// SessionEarlyCloseCTForKey answers for a CME session-day key ("YYYY-MM-DD"),
// which is the shape the session registry and the EOD-flat path already speak.
// It exists so those callers need no clock of their own (A28).
func SessionEarlyCloseCTForKey(key string) (string, bool) {
	for i := range sessionCal.Dates {
		d := sessionCal.Dates[i]
		if d.Date != key {
			continue
		}
		if d.Class != SessionShortened {
			return "", false
		}
		// Parsed rather than echoed, so a malformed close_ct cannot reach the
		// flat path as a plausible-looking string.
		anchor, err := time.ParseInLocation("2006-01-02", key, CTLocation())
		if err != nil {
			return "", false
		}
		c, ok := parseCloseCT(anchor, d.CloseCT)
		if !ok {
			return "", false
		}
		return CloseHHMMCT(c), true
	}
	return "", false
}

// SessionCalendarUnestablishedCount is how many listed dates nobody has sourced.
// The boot line prints it so an unsourced calendar cannot look like a checked one.
func SessionCalendarUnestablishedCount() int {
	n := 0
	for i := range sessionCal.Dates {
		if sessionCal.Dates[i].Unestablished {
			n++
		}
	}
	return n
}

// SessionShortenedDays lists every shortened day in the calendar, in file order.
// It exists so the half-day surfaces derive from the SAME rows the gate reads
// rather than keeping a second copy (the fold, 2026-09-07).
func SessionShortenedDays() []SessionDay {
	out := make([]SessionDay, 0, len(sessionCal.Dates))
	for i := range sessionCal.Dates {
		if sessionCal.Dates[i].Class == SessionShortened {
			out = append(out, sessionCal.Dates[i])
		}
	}
	return out
}

// SessionDayNote is the calendar's contribution to a status surface: what today
// IS, when it closes, and where that came from. It is appended to the MODE row
// and rendered on the boot line.
//
// D5 (owner ruling 2026-09-07). The MODE row read "CME CLOSED (holiday)" beside
// a feed reporting a bar seconds old, and a reader who sees that once stops
// believing the row. UNKNOWN uses the strip's established in-text shape —
// "<FACT> UNKNOWN (<why>)" — never a zero, a dash, or a silent omission.
//
// Returns "" for an ordinary unlisted day: a normal Tuesday needs no note.
func SessionDayNote(now time.Time) string {
	st := SessionStateAt(now)
	if st.UncoveredFallback {
		return fmt.Sprintf(" · calendar UNKNOWN (year %d not covered; the superseded holiday boolean is deciding, which errs closed)", now.In(CTLocation()).Year())
	}
	if !st.Listed {
		return ""
	}
	if st.Unestablished {
		return fmt.Sprintf(" · %s: treatment UNESTABLISHED (closed on the safe side until sourced — %s)", st.Name, st.Date)
	}
	switch st.Class {
	case SessionShortened:
		if !st.HasClose {
			return fmt.Sprintf(" · %s: SHORTENED but close time UNKNOWN (unreadable close_ct — treated as closed)", st.Name)
		}
		return fmt.Sprintf(" · %s: SHORTENED, closes %s [%s]", st.Name, CloseClockCT(st.Close), sourceTag(st.Source))
	case SessionClosed:
		return fmt.Sprintf(" · %s: FULL CLOSURE [%s]", st.Name, sourceTag(st.Source))
	case SessionNormal:
		return fmt.Sprintf(" · %s: listed NORMAL [%s]", st.Name, sourceTag(st.Source))
	}
	return ""
}

// sourceTag shortens a source row to its provenance CLASS, so a status line
// says whether the answer was published or decided without carrying a URL.
func sourceTag(src string) string {
	s := strings.ToLower(src)
	switch {
	case strings.HasPrefix(s, "unestablished"):
		return "unestablished"
	case strings.Contains(s, "cmegroup.com"):
		return "CME published"
	case strings.Contains(s, "owner ruling"):
		return "owner ruling"
	case src == "":
		return "source UNKNOWN"
	}
	return "unsourced"
}

// globexDailyReopenHourCT is the hour the ordinary daily break ends — the same
// boundary weeklyCMEOpen already encodes for Sunday's reopen and the Mon-Thu
// 16:00-17:00 break. Named here so the shortened-day rule reuses the weekly
// rule's own boundary instead of introducing a second session-time literal.
const globexDailyReopenHourCT = 17

// shortenedDayHalted reports whether ct falls in a shortened day's HALT: from
// its stated early close until the ordinary daily reopen.
//
// A SHORTENED DAY IS NOT A CLOSED DAY, and it is not shut until midnight either.
// CME's own wording for 2026-09-07 is "equity futures halt 12:00 CT, reopen
// 17:00 CT" — the evening session belongs to the next trading day and must run.
// Treating the whole calendar date as closed would have skipped tonight's ASIA
// session, which is the same class of loss this calendar exists to prevent, one
// layer down.
//
// Going FLAT at the early close is a different mechanism and already wired: it
// is EffectiveFlatCT / the EOD-flat path, the same discipline as 14:45 at an
// earlier time. This function governs only whether the MARKET is open.
func shortenedDayHalted(ct time.Time, close time.Time) bool {
	return !ct.Before(close) && ct.Hour() < globexDailyReopenHourCT
}
