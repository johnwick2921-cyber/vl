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
