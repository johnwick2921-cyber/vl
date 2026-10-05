package trader

// F0 — calendar ignition test matrix: fetch-ok / 404-fallback-static / skip-fresh
// (+ the F0.3 static→live upgrade). All paths must STORE correctly; no network.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vl/calendar"
	"vl/store"
)

// f0Trader builds a minimal day-plan-enabled NT8 AutoTrader over a temp store.
func f0Trader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	at := &AutoTrader{
		id:       t.Name(), // unique per test so the package-level fetch throttle never collides
		exchange: "ninjatrader",
		store:    st,
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true},
		}},
	}
	return at, st
}

// ffFixture returns one High (T1) + one Medium (T2) USD event on the given CT date.
func ffFixture(dateCT string) []byte {
	return []byte(`[
	  {"title":"FOMC Meeting Minutes","country":"USD","date":"` + dateCT + `T19:00:00Z","impact":"High"},
	  {"title":"Crude Oil Inventories","country":"USD","date":"` + dateCT + `T15:30:00Z","impact":"Medium"}]`)
}

// nowOnCT returns a time whose CT calendar date equals dateCT (12:00 CT = 17:00Z).
func nowOnCT(t *testing.T, dateCT string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, dateCT+"T17:00:00Z")
	if err != nil {
		t.Fatalf("bad date: %v", err)
	}
	return ts
}

func TestF0FetchOKStores(t *testing.T) {
	at, st := f0Trader(t)
	at.calFetch = func() ([]byte, error) { return ffFixture("2026-08-19"), nil }

	at.maybeFetchCalendar(nowOnCT(t, "2026-08-19"))

	slice, err := st.Calendar().GetSlice("2026-08-19")
	if err != nil || slice == nil {
		t.Fatalf("live slice not stored: %v %v", slice, err)
	}
	if slice.Source != "forexfactory" {
		t.Fatalf("source = %q, want forexfactory", slice.Source)
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil || len(evs) != 2 {
		t.Fatalf("stored events wrong: %s", slice.EventsJSON)
	}
}

func TestF0Fallback404StoresStatic(t *testing.T) {
	at, st := f0Trader(t)
	// static T1 file (owner-editable) — the 404 case must still store blackout rows.
	staticPath := filepath.Join(t.TempDir(), "static_t1.json")
	os.WriteFile(staticPath, []byte(`[
	  {"time":"2026-08-19T18:00:00Z","currency":"USD","title":"FOMC Meeting Minutes (static)","impact":"T1"}]`), 0o644)
	t.Setenv("VL_CALENDAR_STATIC", staticPath)
	at.calFetch = func() ([]byte, error) { return nil, errors.New("forexfactory HTTP 404") }

	at.maybeFetchCalendar(nowOnCT(t, "2026-08-19"))

	slice, err := st.Calendar().GetSlice("2026-08-19")
	if err != nil || slice == nil {
		t.Fatalf("static slice not stored on 404: %v %v", slice, err)
	}
	if slice.Source != "static" {
		t.Fatalf("source = %q, want static", slice.Source)
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil || len(evs) != 1 || evs[0].Impact != calendar.T1 {
		t.Fatalf("static T1 event wrong: %s", slice.EventsJSON)
	}
}

func TestF0SkipFreshNoRefetch(t *testing.T) {
	at, st := f0Trader(t)
	now := nowOnCT(t, "2026-08-19")
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-08-19", Source: "forexfactory", EventsJSON: `[]`,
		CreatedAt: now.Add(-time.Hour).UnixMilli(), // F0.4: fresh (<3h) → skip
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at.calFetch = func() ([]byte, error) {
		t.Fatal("skip-fresh must NOT hit the network")
		return nil, nil
	}

	at.maybeFetchCalendar(now)
	// and the once-per-date skip log dedupe latch is set
	if at.lastCalSkipDate != "2026-08-19" {
		t.Fatalf("skip-fresh latch not set: %q", at.lastCalSkipDate)
	}
}

// F0.4 — a LIVE slice older than the 3h freshness window re-fetches, and a
// CHANGED payload updates it in place (same-day corrections) while an unchanged
// payload leaves it byte-identical (replay integrity).
func TestF0StaleLiveSliceRefreshesWhenChanged(t *testing.T) {
	at, st := f0Trader(t)
	now := nowOnCT(t, "2026-08-19")
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-08-19", Source: "forexfactory",
		EventsJSON: `[{"time":"2026-08-19T19:00:00Z","currency":"USD","title":"old title","impact":"T1"}]`,
		CreatedAt:  now.Add(-4 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at.calFetch = func() ([]byte, error) { return ffFixture("2026-08-19"), nil }

	at.maybeFetchCalendar(now)

	slice, _ := st.Calendar().GetSlice("2026-08-19")
	if slice == nil {
		t.Fatal("slice missing")
	}
	var evs []calendar.Event
	if err := json.Unmarshal([]byte(slice.EventsJSON), &evs); err != nil || len(evs) != 2 {
		t.Fatalf("stale live slice not refreshed with the changed feed: %s", slice.EventsJSON)
	}

	// unchanged second fetch (same fixture) must NOT rewrite the row.
	resetCalFetchThrottleForTest(at.id) // clear the persisted throttle
	at.calFetch = func() ([]byte, error) { return ffFixture("2026-08-19"), nil }
	before := slice.EventsJSON
	at.maybeFetchCalendar(now)
	after, _ := st.Calendar().GetSlice("2026-08-19")
	if after == nil || after.EventsJSON != before {
		t.Fatal("unchanged feed must not rewrite the live slice")
	}
}

func TestF0StaticUpgradesToLive(t *testing.T) {
	at, st := f0Trader(t)
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-08-19", Source: "static",
		EventsJSON: `[{"time":"2026-08-19T18:00:00Z","currency":"USD","title":"static guess","impact":"T1"}]`,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at.calFetch = func() ([]byte, error) { return ffFixture("2026-08-19"), nil }

	// static slice = stale → re-fetch fires and upgrades it in place.
	at.maybeFetchCalendar(nowOnCT(t, "2026-08-19"))

	slice, _ := st.Calendar().GetSlice("2026-08-19")
	if slice == nil || slice.Source != "forexfactory" {
		t.Fatalf("static slice not upgraded to live: %+v", slice)
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil || len(evs) != 2 {
		t.Fatalf("upgraded events wrong: %s", slice.EventsJSON)
	}

	// but a LIVE row is frozen: a second fetch with different content must not rewrite it.
	resetCalFetchThrottleForTest(at.id) // clear the persisted throttle
	at.lastCalSkipDate = ""       // clear latch
	at.calFetch = func() ([]byte, error) {
		t.Fatal("live slice is fresh — must skip, not refetch")
		return nil, nil
	}
	at.maybeFetchCalendar(nowOnCT(t, "2026-08-19"))
}
