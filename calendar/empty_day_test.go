package calendar

import (
	"errors"
	"testing"
	"time"
)

// CAL-EMPTY-DAY — a LIVE fetch covers a whole week; every trading date (Mon–Fri)
// inside it gets an entry, an event-free date gets an EMPTY list ([]). A date
// outside the fetched week stays ABSENT, and the static/none paths never fill.

// weekFeed returns a live feed with High USD events on two weekdays of the
// 2026-10-12 (Mon) .. 2026-10-18 (Sun) week: Tue 10-13 and Thu 10-15.
func weekFeed() []byte {
	return []byte(`[
	  {"title":"FOMC","country":"USD","date":"2026-10-13T18:00:00Z","impact":"High"},
	  {"title":"CPI","country":"USD","date":"2026-10-15T18:00:00Z","impact":"High"}
	]`)
}

func TestFetchWeekFillsEmptyTradingDays(t *testing.T) {
	res := FetchWeek(func() ([]byte, error) { return weekFeed(), nil }, nil)
	if res.Source != SourceLive || res.Warning != "" {
		t.Fatalf("live fetch source/warn wrong: %s / %q", res.Source, res.Warning)
	}
	// Mon 10-12, Wed 10-14, Fri 10-16 are event-free trading dates → empty [].
	for _, d := range []string{"2026-10-12", "2026-10-14", "2026-10-16"} {
		evs, ok := res.Days[d]
		if !ok {
			t.Fatalf("trading date %s missing — must be stored as [] (fetched, nothing scheduled)", d)
		}
		if len(evs) != 0 {
			t.Fatalf("trading date %s should be empty, got %d events", d, len(evs))
		}
	}
	// The event dates keep their events.
	if len(res.Days["2026-10-13"]) != 1 || len(res.Days["2026-10-15"]) != 1 {
		t.Fatalf("event dates wrong: %v", res.Days)
	}
	// Weekend dates stay ABSENT (not trading dates).
	for _, d := range []string{"2026-10-17", "2026-10-18"} {
		if _, ok := res.Days[d]; ok {
			t.Fatalf("weekend %s must stay absent, got an entry", d)
		}
	}
}

func TestFetchWeekStaticDoesNotFillEmptyDays(t *testing.T) {
	static := func() []Event {
		return []Event{{Time: time.Date(2026, 10, 13, 18, 0, 0, 0, time.UTC), Currency: "USD", Title: "x", Impact: T1}}
	}
	res := FetchWeek(func() ([]byte, error) { return nil, errors.New("down") }, static)
	if res.Source != SourceStatic {
		t.Fatalf("outage should be static, got %s", res.Source)
	}
	if _, ok := res.Days["2026-10-14"]; ok {
		t.Fatalf("static fallback must NOT fill empty trading days")
	}
	if len(res.Days) != 1 {
		t.Fatalf("static should only carry the event date, got %d days: %v", len(res.Days), res.Days)
	}
}

func TestFetchWeekAllFilteredDoesNotInventRows(t *testing.T) {
	// A live parse that keeps ZERO events (everything Low / off-list) has no
	// event to derive the covered week from → no fill, no invented [] rows.
	res := FetchWeek(func() ([]byte, error) {
		return []byte(`[{"title":"Minor","country":"USD","date":"2026-10-13T15:00:00Z","impact":"Low"}]`), nil
	}, nil)
	if res.Source != SourceLive {
		t.Fatalf("a parsed feed is live, got %s", res.Source)
	}
	if len(res.Days) != 0 {
		t.Fatalf("an all-filtered week must not invent empty rows, got %v", res.Days)
	}
}

// CTO probe (fold on PR #487): the FF week runs SUNDAY→SATURDAY, so an earliest
// SUNDAY event belongs to the FOLLOWING Mon–Fri, not the ISO week's PREVIOUS
// Monday. CNY Medium at 2026-10-11T23:30Z is Sun 18:30 CT; USD High on Tue
// 10-13 and Thu 10-15. The fill must cover THIS week (Mon 10-12 … Fri 10-16)
// and must NOT touch the previous week (Fri 10-09).
func TestFetchWeekSundayEarliestFillsThisWeek(t *testing.T) {
	feed := []byte(`[
	  {"title":"CNY Medium","country":"CNY","date":"2026-10-11T23:30:00Z","impact":"Medium"},
	  {"title":"USD High 1","country":"USD","date":"2026-10-13T18:00:00Z","impact":"High"},
	  {"title":"USD High 2","country":"USD","date":"2026-10-15T18:00:00Z","impact":"High"}
	]`)
	res := FetchWeek(func() ([]byte, error) { return feed, nil }, nil)
	if res.Source != SourceLive {
		t.Fatalf("live source expected, got %s", res.Source)
	}
	// This week's Monday is filled (the exact bug: it used to be ABSENT).
	if evs, ok := res.Days["2026-10-12"]; !ok {
		t.Fatalf("Mon 2026-10-12 (this week) must be filled as [], got absent")
	} else if len(evs) != 0 {
		t.Fatalf("Mon 2026-10-12 must be [], got %d events", len(evs))
	}
	// The previous week's Friday is NOT filled (never fetched).
	if _, ok := res.Days["2026-10-09"]; ok {
		t.Fatalf("Fri 2026-10-09 (previous week) must stay absent")
	}
	// The rest of this week's Mon–Fri are filled; the event dates keep events.
	for _, d := range []string{"2026-10-14", "2026-10-16"} {
		if evs, ok := res.Days[d]; !ok || len(evs) != 0 {
			t.Fatalf("trading date %s must be [], got %v", d, evs)
		}
	}
	if len(res.Days["2026-10-13"]) != 1 || len(res.Days["2026-10-15"]) != 1 {
		t.Fatalf("event dates must keep their events: %v", res.Days)
	}
	// The Sunday event date itself stays (it is not a trading date and is NOT
	// filled, but the CNY event lives there).
	if evs, ok := res.Days["2026-10-11"]; !ok || len(evs) != 1 {
		t.Fatalf("the Sunday CNY event date must stay with its event: %v", res.Days)
	}
}

// A file whose events map to TWO different FF weeks (Thu 10-08 → Mon 10-05,
// Tue 10-13 → Mon 10-12) fills only the Mon–Fri dates BETWEEN the earliest and
// latest event, never beyond — so 10-05 (before) and 10-16 (after) stay absent.
func TestFetchWeekTwoWeekSpanFillsOnlyBetween(t *testing.T) {
	feed := []byte(`[
	  {"title":"USD A","country":"USD","date":"2026-10-08T18:00:00Z","impact":"High"},
	  {"title":"USD B","country":"USD","date":"2026-10-13T18:00:00Z","impact":"High"}
	]`)
	res := FetchWeek(func() ([]byte, error) { return feed, nil }, nil)
	if res.Source != SourceLive {
		t.Fatalf("live source expected, got %s", res.Source)
	}
	// Between: Fri 10-09 and Mon 10-12 are trading dates in the span → [].
	for _, d := range []string{"2026-10-09", "2026-10-12"} {
		if evs, ok := res.Days[d]; !ok || len(evs) != 0 {
			t.Fatalf("in-span trading date %s must be [], got %v", d, evs)
		}
	}
	// Never beyond: the first week's Monday (10-05) and the second week's Friday
	// (10-16) are OUTSIDE [earliest, latest] and must stay absent.
	for _, d := range []string{"2026-10-05", "2026-10-16"} {
		if _, ok := res.Days[d]; ok {
			t.Fatalf("out-of-span date %s must stay absent", d)
		}
	}
	// The event dates keep their events.
	if len(res.Days["2026-10-08"]) != 1 || len(res.Days["2026-10-13"]) != 1 {
		t.Fatalf("event dates must keep their events: %v", res.Days)
	}
}
