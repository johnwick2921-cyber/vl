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

// ── m438 (release #53) ──────────────────────────────────────────────────────

// offScaleReplay answers a re-request with a replay of [from, to] on ANOTHER
// price scale: it overlaps live minutes at 30700 and disagrees with them.
func (f *ghFixture) offScaleReplay(from, to time.Time) {
	f.t.Helper()
	var bars []ntwire.Bar
	for m := from; !m.After(to); m = m.Add(time.Minute) {
		bars = append(bars, wireBar(m, 30400))
	}
	f.addon.send(ntwire.FrameBarsHistorical, ntwire.BarsHistoricalPayload{Symbol: "MNQ", Timeframe: "1m", Contract: ghContract, Bars: bars})
}

func (f *ghFixture) disagreed() int64 {
	_, d := f.srv.BarCache().OverlapVerdictCounts()
	return d
}

// ReplayDisagreed is PER HOLE: a disagreeing replay that happened BEFORE the hole
// was detected (any earlier gap-backfill anywhere in the process) must not turn
// this hole's owner message into a "price scale" story; one that happens WHILE the
// hole is open must.
//
// MUTANT (shown RED): ReplayDisagreed: int(disagreedNow) (the old process-lifetime
// copy) -> the first leg reads 1 -> RED.
func TestUnfilledHoleReplayDisagreedIsPerHole(t *testing.T) {
	f := newGHFixture(t)
	f.stream(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))

	// an EARLIER disagreeing gap-backfill, before any hole exists (a different
	// session day so the per-day re-request budget is untouched).
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) {
		f.offScaleReplay(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))
	})
	if err := f.srv.RequestGapBackfillAt("MNQ", ct(2026, 10, 13, 10, 0)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the earlier disagreeing verdict", func() bool { return f.disagreed() == 1 })
	f.addon.mu.Lock()
	f.addon.baseline = len(f.addon.subs) // that request is not the tracker's
	f.addon.mu.Unlock()

	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	waitFor(t, "the tracker to open the hole", func() bool { return len(f.holes()) == 1 })
	det := f.clk.Now()
	for i, at := range []time.Duration{61 * time.Second, 3*time.Minute + 5*time.Second, 10*time.Minute + 5*time.Second, 30*time.Minute + 5*time.Second} {
		f.tr.tick(det.Add(at))
		f.waitReRequests(i + 1)
	}
	unfilledAt := det.Add(32*time.Minute + 10*time.Second)
	f.tr.tick(unfilledAt)

	got := f.tr.unfilledFeeding("MNQ", unfilledAt)
	if len(got) != 1 {
		t.Fatalf("want one unfilled hole: %+v\n%s", got, f.sink.dump())
	}
	if got[0].ReplayDisagreed != 0 {
		t.Fatalf("a disagreement from BEFORE this hole was detected leaked into it: ReplayDisagreed = %d", got[0].ReplayDisagreed)
	}

	// a disagreeing replay WHILE the hole is open does count.
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) {
		f.offScaleReplay(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))
	})
	if err := f.srv.RequestGapBackfillAt("MNQ", ct(2026, 10, 15, 10, 0)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the in-hole disagreeing verdict", func() bool { return f.disagreed() == 2 })
	got = f.tr.unfilledFeeding("MNQ", unfilledAt)
	if len(got) != 1 || got[0].ReplayDisagreed != 1 {
		t.Fatalf("a disagreement while the hole is open must count once: %+v", got)
	}
}

// SlowSpent is READ: false while slow re-requests remain, true once the cap is
// spent and the hole is still open; the cadence and horizon come off the constants.
// MUTANT (shown RED): SlowSpent: false -> the final leg -> RED.
func TestUnfilledHoleSaysWhenTheSlowRetriesAreSpent(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	det := f.openHole()
	for i, at := range []time.Duration{61 * time.Second, 3*time.Minute + 5*time.Second, 10*time.Minute + 5*time.Second, 30*time.Minute + 5*time.Second} {
		f.tr.tick(det.Add(at))
		f.waitReRequests(i + 1)
	}
	unfilledAt := det.Add(32*time.Minute + 10*time.Second)
	f.tr.tick(unfilledAt)
	first := f.tr.unfilledFeeding("MNQ", unfilledAt)
	if len(first) != 1 || first[0].SlowSpent {
		t.Fatalf("fresh unfilled hole must not read as spent: %+v", first)
	}
	if first[0].ReaskEvery != 15*time.Minute || first[0].ReaskHorizon != 2*time.Hour {
		t.Fatalf("cadence/horizon = %v / %v, want 15m / 2h", first[0].ReaskEvery, first[0].ReaskHorizon)
	}
	sent := 4
	for k := 1; k <= 9; k++ {
		f.tr.tick(unfilledAt.Add(time.Duration(k) * 15 * time.Minute))
		if k <= 8 {
			sent++
			f.waitReRequests(sent)
		}
	}
	last := f.tr.unfilledFeeding("MNQ", unfilledAt.Add(9*15*time.Minute))
	if len(last) != 1 || !last[0].SlowSpent {
		t.Fatalf("after the cap the hole must read SlowSpent: %+v\n%s", last, f.sink.dump())
	}
}
