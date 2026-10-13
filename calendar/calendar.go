// Package calendar fetches the economic calendar (ForexFactory weekly JSON) with
// a static T1 fallback, so day-plan no-trade blackouts never silently vanish.
//
// P1.8. Pure + injectable: FetchWeek takes a FetchFunc (real HTTP by default,
// a stub in tests) and a static-fallback path. A feed outage or a parse error
// falls back to the owner-editable static T1 file and sets a Warning — it never
// returns an error and never panics. The caller stores the per-day slice with
// the trade date (replay-frozen, append-only).
package calendar

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Impact tiers. T1 (red / High) auto-writes a hard no-trade blackout; T2 (orange
// / Medium) is a caution. Low / Holiday (yellow) are ignored.
type Impact string

const (
	T1 Impact = "T1"
	T2 Impact = "T2"
)

// Source records where a week's data came from.
type Source string

const (
	SourceLive   Source = "forexfactory"
	SourceStatic Source = "static"
	SourceNone   Source = "none"
)

// ForexFactory weekly JSON URL (the standard public feed).
const ForexFactoryWeeklyURL = "https://nfs.faireconomy.media/ff_calendar_thisweek.json"

// Currencies the day-plan cares about (union of all session filters).
var interestCurrencies = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "JPY": true, "CNY": true,
}

// Event is one calendar entry (post-filter).
type Event struct {
	Time     time.Time `json:"time"`
	Currency string    `json:"currency"`
	Title    string    `json:"title"`
	Impact   Impact    `json:"impact"`
}

// Result is the fetched week grouped by CT calendar date (YYYY-MM-DD).
type Result struct {
	Days    map[string][]Event `json:"days"`
	Source  Source             `json:"source"`
	Warning string             `json:"warning,omitempty"` // non-empty on outage/fallback
}

// FetchFunc returns the raw weekly JSON bytes (or an error). Injectable for tests.
type FetchFunc func() ([]byte, error)

// StaticLoader returns the static T1 fallback events (or nil). Injectable.
type StaticLoader func() []Event

// ffEvent is the raw ForexFactory JSON shape.
type ffEvent struct {
	Title   string `json:"title"`
	Country string `json:"country"` // currency code
	Date    string `json:"date"`    // ISO-8601
	Impact  string `json:"impact"`  // High | Medium | Low | Holiday
}

// DefaultFetch GETs the ForexFactory weekly JSON with a short timeout.
func DefaultFetch() ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ForexFactoryWeeklyURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forexfactory HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20))
}

// FetchWeek fetches + filters the week. On any failure it falls back to `static`
// and sets Warning (never errors, never panics).
func FetchWeek(fetch FetchFunc, static StaticLoader) Result {
	loc := chicagoLoc()
	fallback := func(reason string) Result {
		var ev []Event
		if static != nil {
			ev = static()
		}
		src := SourceStatic
		if len(ev) == 0 {
			src = SourceNone
		}
		return Result{
			Days:    groupByCTDate(ev, loc),
			Source:  src,
			Warning: reason,
		}
	}

	if fetch == nil {
		return fallback("no calendar fetcher configured; used static T1 fallback")
	}
	data, err := fetch()
	if err != nil || len(data) == 0 {
		return fallback(fmt.Sprintf("calendar feed unavailable (%v); used static T1 fallback", err))
	}
	events, perr := parseForexFactory(data)
	if perr != nil {
		return fallback(fmt.Sprintf("calendar feed unparseable (%v); used static T1 fallback", perr))
	}
	days := groupByCTDate(events, loc)
	// CAL-EMPTY-DAY — a LIVE fetch that parsed successfully covers a whole week:
	// every trading date inside that week gets an entry, so a date with no
	// events is stored as [] ("fetched, nothing scheduled") and is never read as
	// "feed missing". Static/none keep today's behaviour (no empty rows from a
	// fallback).
	fillEmptyTradingDays(days, loc)
	return Result{Days: days, Source: SourceLive}
}

// parseForexFactory parses + filters raw FF JSON: keep High→T1 / Medium→T2 for
// currencies of interest; drop Low/Holiday (yellow) and off-list currencies.
func parseForexFactory(data []byte) ([]Event, error) {
	var raw []ffEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var out []Event
	for _, r := range raw {
		cur := strings.ToUpper(strings.TrimSpace(r.Country))
		if !interestCurrencies[cur] {
			continue
		}
		var imp Impact
		switch strings.ToLower(strings.TrimSpace(r.Impact)) {
		case "high":
			imp = T1
		case "medium":
			imp = T2
		default:
			continue // low / holiday / unknown → ignored
		}
		t, err := time.Parse(time.RFC3339, r.Date)
		if err != nil {
			// tolerate a date-only form
			if t, err = time.Parse("2006-01-02", r.Date); err != nil {
				continue
			}
		}
		out = append(out, Event{Time: t.UTC(), Currency: cur, Title: strings.TrimSpace(r.Title), Impact: imp})
	}
	return out, nil
}

// groupByCTDate buckets events by their America/Chicago calendar date and sorts
// each day by time.
func groupByCTDate(events []Event, loc *time.Location) map[string][]Event {
	days := map[string][]Event{}
	for _, e := range events {
		key := e.Time.In(loc).Format("2006-01-02")
		days[key] = append(days[key], e)
	}
	for k := range days {
		sort.SliceStable(days[k], func(i, j int) bool { return days[k][i].Time.Before(days[k][j].Time) })
	}
	return days
}

// fillEmptyTradingDays (CAL-EMPTY-DAY) gives every trading date inside the week
// the feed covers an entry in `days`: a date with no events becomes an EMPTY
// list ([]), so a fetched event-free day is stored as "[]" and is never read as
// "feed missing" (canon CLASS 49/53 — an empty computed list is [], an
// uncomputed one is absent; they must differ).
//
// Week convention: the ForexFactory "thisweek" file runs SUNDAY→SATURDAY, so a
// date's week-Monday is the NEXT day for a Sunday and the Monday on/before it
// otherwise (a Saturday stays in its own week). The covered week is derived from
// ALL event dates, not just the earliest — an earliest Sunday belongs to the
// FOLLOWING Mon–Fri:
//
//   - every event maps to the SAME week-Monday → fill that whole Mon–Fri (the FF
//     file covers the entire Sun→Sat week, so a Monday with no event was still
//     fetched);
//   - events map to DIFFERENT week-Mondays (a file spanning two weeks) → fill
//     only the Mon–Fri dates between the earliest and latest event date,
//     inclusive, never beyond, and log it once.
//
// Only Mon–Fri are ever filled — a weekend date is not a trading date and stays
// ABSENT. No events → no date to derive the week from → no fill (a bare feed
// keeps the gate's existing fail-closed behaviour). A date outside the fetched
// week is NEVER marked empty.
func fillEmptyTradingDays(days map[string][]Event, loc *time.Location) {
	if len(days) == 0 {
		return
	}
	type dated struct {
		t time.Time
	}
	dates := make([]dated, 0, len(days))
	for d := range days {
		t, err := time.ParseInLocation("2006-01-02", d, loc)
		if err != nil {
			continue
		}
		dates = append(dates, dated{t: t})
	}
	if len(dates) == 0 {
		return
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].t.Before(dates[j].t) })
	earliest, latest := dates[0].t, dates[len(dates)-1].t

	// FF week-Monday of every event date; all must agree for a whole-week fill.
	first := ffWeekMonday(earliest)
	sameWeek := true
	for _, d := range dates {
		if !ffWeekMonday(d.t).Equal(first) {
			sameWeek = false
			break
		}
	}

	fill := func(from, to time.Time) {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			key := d.Format("2006-01-02")
			if _, ok := days[key]; !ok {
				days[key] = []Event{}
			}
		}
	}

	if sameWeek {
		// Whole FF week: fill Mon–Fri of that Monday (the FF file covers all of it).
		fill(first, first.AddDate(0, 0, 6))
		return
	}
	// Two-week span: fill only Mon–Fri between earliest and latest, never beyond.
	log.Printf("calendar: feed spans two ForexFactory weeks (%s .. %s); filling only the trading dates between them",
		earliest.Format("2006-01-02"), latest.Format("2006-01-02"))
	fill(earliest, latest)
}

// ffWeekMonday returns the Monday of the ForexFactory (Sun→Sat) week containing
// d: a Sunday belongs to the FOLLOWING Mon–Fri (its next day is the Monday);
// every other weekday belongs to the Monday on/before it (a Saturday stays in
// its own week).
func ffWeekMonday(d time.Time) time.Time {
	if d.Weekday() == time.Sunday {
		return d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// SessionCurrencies returns the currency filter for a session: USD always; +JPY/
// CNY for Asia; +EUR/GBP for London; NY = USD only.
func SessionCurrencies(session string) map[string]bool {
	m := map[string]bool{"USD": true}
	switch strings.ToUpper(session) {
	case "ASIA":
		m["JPY"], m["CNY"] = true, true
	case "LONDON":
		m["EUR"], m["GBP"] = true, true
	}
	return m
}

// EventsForSession filters a day's events to a session's currency set.
func EventsForSession(day []Event, session string) []Event {
	cur := SessionCurrencies(session)
	var out []Event
	for _, e := range day {
		if cur[e.Currency] {
			out = append(out, e)
		}
	}
	return out
}

func chicagoLoc() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.UTC
	}
	return loc
}
