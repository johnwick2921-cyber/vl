package trader

// CAL-EMPTY-DAY — a fetched event-free trading date is stored as "[]", so the
// gate returns no windows and fires no fail-closed alert; an ABSENT date keeps
// the P0.6 fallback + alert. Pins at the production call sites (maybeFetchCalendar
// → FetchWeek → store, and t1WindowsFor).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/store"
)

// calWeekFeed returns a live feed with one High USD event on Tue 2026-10-13 and
// one on Fri 2026-10-17 — so Mon 10-12, Wed 10-14, Thu 10-16 are event-free
// trading dates in the fetched week (10-12 Mon .. 10-18 Sun).
func calWeekFeed() []byte {
	return []byte(`[
	  {"title":"FOMC","country":"USD","date":"2026-10-13T18:00:00Z","impact":"High"},
	  {"title":"Retail Sales","country":"USD","date":"2026-10-17T18:00:00Z","impact":"High"}
	]`)
}

// (a) an event-free weekday inside the fetched week → stored "[]", no windows,
// no fail-closed alert, and the INFO line fires EXACTLY ONCE per trade date
// (P3/M2 — the firstFor guard must not be droppable without the test going RED).
func TestCalEmptyDayStoresEmptySliceAndNoFailClosed(t *testing.T) {
	at, st := f0Trader(t)
	at.calFetch = func() ([]byte, error) { return calWeekFeed(), nil }
	at.maybeFetchCalendar(nowOnCT(t, "2026-10-13"))

	slice, err := st.Calendar().GetSlice("2026-10-16")
	if err != nil || slice == nil || slice.EventsJSON != "[]" {
		t.Fatalf("event-free Thu 10-16 must be stored as [], got %+v err=%v", slice, err)
	}

	logBuf := captureTraderLog(t)
	got := at.t1WindowsFor("2026-10-16", f1Sess)
	if len(got) != 0 {
		t.Fatalf("empty slice must give no windows, got %+v", got)
	}
	_ = at.t1WindowsFor("2026-10-16", f1Sess) // second read, same trade date

	if n := strings.Count(logBuf.String(), "no events scheduled"); n != 1 {
		t.Fatalf("the 'no events scheduled' INFO line must appear exactly once per trade date, got %d\nlog:\n%s", n, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "2026-10-16 fetched — no events scheduled") {
		t.Fatalf("the INFO line must name the trade date:\n%s", logBuf.String())
	}
	for _, a := range calAlerts(st, at.id) {
		if a.Kind == "calendar-slice-missing" {
			t.Fatalf("a stored [] must NOT fire the fail-closed alert")
		}
	}
}

// (b) a date OUTSIDE the fetched span stays absent → the P0.6 fallback + alert
// still fire.
func TestCalEmptyDayOutsideSpanStaysAbsent(t *testing.T) {
	at, st := f0Trader(t)
	at.calFetch = func() ([]byte, error) { return calWeekFeed(), nil }
	at.maybeFetchCalendar(nowOnCT(t, "2026-10-13"))

	if slice, _ := st.Calendar().GetSlice("2026-10-20"); slice != nil {
		t.Fatalf("a date outside the fetched week must stay absent, got %+v", slice)
	}
	at.t1WindowsFor("2026-10-20", f1Sess)
	found := false
	for _, a := range calAlerts(st, at.id) {
		if a.Kind == "calendar-slice-missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an ABSENT slice must fire calendar-slice-missing")
	}
}

// (c) a stored "[]" upgrades when a later (same-day) fetch finds events for it.
func TestCalEmptyDayUpgradesWhenEventsAppear(t *testing.T) {
	at, st := f0Trader(t)
	now := nowOnCT(t, "2026-10-16")
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-10-16", Source: "forexfactory", EventsJSON: "[]",
		CreatedAt: now.Add(-4 * time.Hour).UnixMilli(), // stale → re-fetch
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at.calFetch = func() ([]byte, error) {
		return []byte(`[{"title":"CPI","country":"USD","date":"2026-10-16T18:00:00Z","impact":"High"}]`), nil
	}
	at.maybeFetchCalendar(now)

	slice, _ := st.Calendar().GetSlice("2026-10-16")
	if slice == nil {
		t.Fatal("slice missing")
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil || len(evs) != 1 {
		t.Fatalf("[] must upgrade when events appear, got %s", slice.EventsJSON)
	}
}

// (d) a static-source fetch never writes [] rows for event-free dates.
func TestCalEmptyDayStaticNeverFillsEmptyDays(t *testing.T) {
	at, st := f0Trader(t)
	staticPath := filepath.Join(t.TempDir(), "static_t1.json")
	os.WriteFile(staticPath, []byte(`[{"time":"2026-10-13T18:00:00Z","currency":"USD","title":"x","impact":"T1"}]`), 0o644)
	t.Setenv("VL_CALENDAR_STATIC", staticPath)
	at.calFetch = func() ([]byte, error) { return nil, errors.New("forexfactory HTTP 404") }
	at.maybeFetchCalendar(nowOnCT(t, "2026-10-13"))

	if slice, _ := st.Calendar().GetSlice("2026-10-14"); slice != nil {
		t.Fatalf("static source must never write [] rows, got %+v", slice)
	}
	if slice, _ := st.Calendar().GetSlice("2026-10-13"); slice == nil || slice.Source != "static" {
		t.Fatalf("static event date must be stored, got %+v", slice)
	}
}

// (e) a past-date "[]" stays frozen: a corrected payload for a PAST trade date
// must not overwrite it.
func TestCalEmptyDayPastDateFreezeUnchanged(t *testing.T) {
	at, st := f0Trader(t)
	now := nowOnCT(t, "2026-10-16") // today = Thu
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-10-13", Source: "forexfactory", EventsJSON: "[]",
		CreatedAt: now.Add(-4 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at.calFetch = func() ([]byte, error) {
		return []byte(`[{"title":"FOMC","country":"USD","date":"2026-10-13T18:00:00Z","impact":"High"}]`), nil
	}
	at.maybeFetchCalendar(now)

	slice, _ := st.Calendar().GetSlice("2026-10-13")
	if slice == nil || slice.EventsJSON != "[]" {
		t.Fatalf("past-date [] must stay frozen, got %+v", slice)
	}
}

func calAlerts(st *store.Store, traderID string) []*store.AlertDB {
	a, _ := st.Alert().List(traderID, 50)
	return a
}
