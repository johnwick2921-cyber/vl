package trader

// F1 — calendar throttle P2 pin (CTO 2026-10-05): the "re-fetching" log moved
// AFTER the 1h throttle, so it fires only when the fetch ACTUALLY runs — and the
// throttle itself persists across trader reconstruction (package-level map keyed
// by trader id), not a struct field that zeroes on rebuild.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"vl/kernel"

	"vl/calendar"
	"vl/logger"
	"vl/store"
)

// f1SeedStaleLive stores a forexfactory slice whose EventsJSON is EXACTLY what
// the producer will re-marshal for the same feed (so the first fetch sees
// changed=false and the slice STAYS stale, forcing the throttle path on call 2).
func f1SeedStaleLive(t *testing.T, st *store.Store, dateCT string) {
	t.Helper()
	res := calendar.FetchWeek(func() ([]byte, error) { return ffFixture(dateCT), nil }, nil)
	evs := res.Days[dateCT]
	js, err := json.Marshal(evs)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	ok, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate:  dateCT,
		Source:     "forexfactory",
		EventsJSON: string(js),
		CreatedAt:  time.Now().Add(-4 * time.Hour).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("seed stale slice: wrote=%v err=%v", ok, err)
	}
}

// TestF1RefetchLogFiresOnlyWhenFetchRuns pins the log-ordering half of P2. The
// "drop the throttle move" mutant (log-before-throttle) fires "re-fetching" on
// EVERY stale-live cycle even while the throttle suppresses the fetch → RED.
func TestF1RefetchLogFiresOnlyWhenFetchRuns(t *testing.T) {
	at, st := f0Trader(t)
	at.id = "f1-refetch-log" // unique id so the package throttle never collides
	f1SeedStaleLive(t, st, "2026-10-05")

	var buf bytes.Buffer
	prev := logger.Log.Out
	logger.Log.SetOutput(&buf)
	defer logger.Log.SetOutput(prev)

	calls := 0
	at.calFetch = func() ([]byte, error) { calls++; return ffFixture("2026-10-05"), nil }

	at.maybeFetchCalendar(nowOnCT(t, "2026-10-05"))
	at.maybeFetchCalendar(nowOnCT(t, "2026-10-05"))

	if calls != 1 {
		t.Fatalf("fetch ran %d times, want 1 (1h throttle must suppress the second)", calls)
	}
	if n := strings.Count(buf.String(), "re-fetching"); n != 1 {
		t.Fatalf("'re-fetching' logged %d times, want 1 (log must move after the throttle):\n%s", n, buf.String())
	}
}

// TestF1CalFetchThrottlePersistsAcrossReconstruction pins the persistence half of
// P2: a rebuilt AutoTrader with the SAME trader id must inherit the throttle.
func TestF1CalFetchThrottlePersistsAcrossReconstruction(t *testing.T) {
	const tid = "f1-throttle-persist"
	resetCalFetchThrottleForTest(tid) // start clean

	// Instance 1 fetches (the seeded slice stays stale, so this is a REAL fetch).
	at1, st := f0Trader(t)
	at1.id = tid
	f1SeedStaleLive(t, st, "2026-10-05")
	calls := 0
	fetch := func() ([]byte, error) { calls++; return ffFixture("2026-10-05"), nil }
	at1.calFetch = fetch
	at1.maybeFetchCalendar(nowOnCT(t, "2026-10-05"))
	if calls != 1 {
		t.Fatalf("first instance fetch ran %d times, want 1", calls)
	}

	// Instance 2 — a "rebuilt" trader (fresh struct, same id, same store) — must
	// be throttled: no second fetch inside the 1h window.
	at2, _ := f0Trader(t)
	at2.id = tid
	at2.calFetch = fetch
	at2.maybeFetchCalendar(nowOnCT(t, "2026-10-05"))
	if calls != 1 {
		t.Fatalf("rebuilt trader re-fetched (calls=%d) — throttle did not persist across reconstruction", calls)
	}
}

// TestF1CalFetchThrottleIsPerTradeDate — DS-102 #409 review (d): the throttle is
// scoped to (trader, trade date). An attempt at 23:50 CT on 10-05 must NOT delay
// the first fetch of 10-06 at 00:05 CT, 15 minutes later. Mutant: key the
// throttle by trader id only → the 10-06 fetch is suppressed → RED.
func TestF1CalFetchThrottleIsPerTradeDate(t *testing.T) {
	const tid = "f1-throttle-per-date"
	resetCalFetchThrottleForTest(tid)
	t.Cleanup(func() { resetCalFetchThrottleForTest(tid) })

	at, st := f0Trader(t)
	at.id = tid
	f1SeedStaleLive(t, st, "2026-10-05")
	calls := 0
	at.calFetch = func() ([]byte, error) { calls++; return nil, fmt.Errorf("feed down") }

	loc := kernel.CTLocation()
	at.maybeFetchCalendar(time.Date(2026, 10, 5, 23, 50, 0, 0, loc))
	if calls != 1 {
		t.Fatalf("10-05 23:50 attempt ran %d times, want 1", calls)
	}
	at.maybeFetchCalendar(time.Date(2026, 10, 6, 0, 5, 0, 0, loc))
	if calls != 2 {
		t.Fatalf("the first fetch of the NEW trade date was throttled by the previous date's attempt (calls=%d, want 2)", calls)
	}
	// Same date, inside the hour → still throttled.
	at.maybeFetchCalendar(time.Date(2026, 10, 6, 0, 20, 0, 0, loc))
	if calls != 2 {
		t.Fatalf("same-date retry inside 1h was not throttled (calls=%d, want 2)", calls)
	}
}
