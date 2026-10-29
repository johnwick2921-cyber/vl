package ninjatrader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── CONTRACT ROLL: the pre-recording stretch is not a hole (review round 2) ──
//
// A contract that has just rolled in is stored sparsely BEFORE the bot records
// it: on the live store 12-26 holds one `historical_import` row a day for
// 09-07..09-10 and then dense data from 09-14 09:34 — and the boot scan of the
// new contract read those stretches as ~1400-minute holes, pausing the intraday
// window for a day and the deep / swing windows for 6 / 17 trading days after
// every roll (replayed on the real rows: 09-15 08:00 boot, all three windows
// BLOCKED). The bot was not on that contract yet; the stitched history takes
// those minutes from the older contract.
//
// MUTANT R1  trimPreRecording is a no-op  -> TestRollInSparseImportsAreNotHoles RED

func TestRollInSparseImportsAreNotHoles(t *testing.T) {
	armIntegrity(t)
	st := &memHoleStore{imports: map[int64]bool{}}
	// the pre-recording snapshots: one import row a day
	for _, d := range []int{19, 20, 21} {
		m := ct(2026, 10, d, 12, 0)
		st.add(m.UnixMilli())
		st.imports[m.UnixMilli()] = true
	}
	// recorded from 10-22 09:34 on, with ONE real hole 13:00–13:19 that is the
	// bot's own loss (it was on the contract)
	holeFrom, holeTo := ct(2026, 10, 22, 13, 0), ct(2026, 10, 22, 13, 20)
	boot := ct(2026, 10, 23, 6, 0)
	for m := ct(2026, 10, 22, 9, 34); m.Before(boot); m = m.Add(time.Minute) {
		if barHoleExpected(m) && (m.Before(holeFrom) || !m.Before(holeTo)) {
			st.add(m.UnixMilli())
		}
	}
	tr := newBarHoleTracker(st, func() *ntwire.TCPServer { return nil })
	tr.say = (&lineSink{}).say
	tr.clock = func() time.Time { return boot }
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })
	st.add(boot.UnixMilli())
	tr.observeLive("MNQ", "1m", ghContract, []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: boot.UnixMilli()}})
	now := boot.Add(2 * time.Minute)

	for _, win := range []int{IntegrityIntradayOpenMin, IntegrityDeepOpenMin, IntegritySwingOpenMin} {
		v := IntegrityVerdictFor("MNQ", win, now)
		if !v.Blocked || len(v.Holes) != 1 || v.Holes[0].Window != "10-22 13:00–13:20 CT" {
			t.Fatalf("window %d: only the bot's own 13:00 hole may stand — the pre-recording import stretch is not a hole: %+v", win, v)
		}
	}

	// control: an import row AFTER the first recorded bar is data like any other,
	// and a store with no recorded bar at all is judged whole
	if got := trimPreRecording(nil); got != nil {
		t.Fatalf("nil in, nil out: %v", got)
	}
	allImport := []store.BarHistoryDB{{OpenTimeMs: 1, Source: store.BarSourceHistoricalImport}, {OpenTimeMs: 2, Source: store.BarSourceHistoricalImport}}
	if got := trimPreRecording(allImport); len(got) != 2 {
		t.Fatalf("a store with no recorded bar is judged whole: %v", got)
	}
	mixed := []store.BarHistoryDB{{OpenTimeMs: 1, Source: store.BarSourceHistoricalImport}, {OpenTimeMs: 2, Source: store.BarSourceLive}, {OpenTimeMs: 3, Source: store.BarSourceHistoricalImport}}
	if got := trimPreRecording(mixed); len(got) != 2 || got[0].OpenTimeMs != 2 {
		t.Fatalf("only LEADING imports go: %v", got)
	}
}
