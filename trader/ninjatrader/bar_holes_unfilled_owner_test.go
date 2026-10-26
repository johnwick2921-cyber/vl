package ninjatrader

import (
	"strings"
	"testing"
	"time"
	ntwire "vl/provider/ninjatrader"
)

// m432 (release #52) — the unfillable-hole -> owner read. PRODUCTION CALL SITE:
// the real fixture (server, fake AddOn, persister, SQLite, tracker) drives the
// hole through the four re-requests to UNFILLED; UnfilledHolesFeeding is what
// the trader's monitor beat reads.
//
// MUTANT (shown RED in the wave report): drop the `h.State != holeStateUnfilled`
// filter -> the "still refilling" leg reports a hole nobody has given up on.
// MUTANT 2: drop the live-receipt freshness check -> the "feed not live" leg
// reports a hole while NT8 is not delivering bars (FEED DOWN's story).
func TestUnfilledHoleIsReportedOnlyWhileNT8IsFeedingLiveBars(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	det := f.openHole()

	// 1. the hole exists but is still being re-requested: nothing to tell the owner.
	if got := f.tr.unfilledFeeding("MNQ", det); len(got) != 0 {
		t.Fatalf("a hole still in its re-request budget must not be reported: %+v", got)
	}

	steps := []struct {
		at   time.Duration
		want int
	}{
		{61 * time.Second, 1},
		{3*time.Minute + 5*time.Second, 2},
		{10*time.Minute + 5*time.Second, 3},
		{30*time.Minute + 5*time.Second, 4},
	}
	for _, s := range steps {
		f.tr.tick(det.Add(s.at))
		f.waitReRequests(s.want)
	}
	if got := f.tr.unfilledFeeding("MNQ", det.Add(31*time.Minute)); len(got) != 0 {
		t.Fatalf("not declared unfilled before the last attempt is measured: %+v", got)
	}
	unfilledAt := det.Add(32*time.Minute + 10*time.Second)
	f.tr.tick(unfilledAt) // the measure after #4 declares it

	// 2. unfilled + NT8 linked + live bars just streamed -> reported, with the
	//    window the owner needs and every field READ.
	got := f.tr.unfilledFeeding("MNQ", unfilledAt)
	if len(got) != 1 {
		t.Fatalf("want exactly one unfilled hole, got %+v\n%s", got, f.sink.dump())
	}
	h := got[0]
	if h.Symbol != "MNQ" || h.Attempts != 4 || h.Missing != 244 || h.Window != "10-14 03:44–07:48 CT" {
		t.Fatalf("hole facts wrong: %+v", h)
	}
	if h.ReplayDisagreed != 0 {
		t.Fatalf("no scale story here, ReplayDisagreed = %d", h.ReplayDisagreed)
	}

	// 3. another symbol's question never sees it.
	if got := f.tr.unfilledFeeding("ES", unfilledAt); len(got) != 0 {
		t.Fatalf("an ES trader must not be told about an MNQ hole: %+v", got)
	}
	// 4. CME closed at `now` -> nothing (the schedule is paused then too).
	if got := f.tr.unfilledFeeding("MNQ", ct(2026, 10, 17, 12, 0)); len(got) != 0 {
		t.Fatalf("CME closed: %+v", got)
	}
	// 5. NT8 is NOT delivering live bars (newest live receipt older than the
	//    freshness window): that is a feed outage, not missing history.
	f.tr.receiptClock = func() time.Time { return time.Now().Add(liveBarFreshness + time.Minute) }
	if got := f.tr.unfilledFeeding("MNQ", unfilledAt); len(got) != 0 {
		t.Fatalf("a stalled feed must not be reported as missing history: %+v", got)
	}
	// the package-level read goes through the installed tracker (production path)
	f.tr.receiptClock = time.Now
	if got := UnfilledHolesFeeding("MNQ", unfilledAt); len(got) != 1 {
		t.Fatalf("UnfilledHolesFeeding must read the installed tracker: %+v", got)
	}
	if !strings.Contains(f.sink.dump(), "UNFILLED") {
		t.Fatalf("the log line still says UNFILLED:\n%s", f.sink.dump())
	}
}
