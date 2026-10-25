package ninjatrader

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

const m424Contract = "MNQ 12-26"

func m424Store(t *testing.T) *store.BarHistoryStore {
	t.Helper()
	t.Setenv("BARS_KEY_BACKUP_DIR", t.TempDir())
	st, err := store.New(filepath.Join(t.TempDir(), "m424.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	return bh
}

func ct1023(h, m, s int) time.Time {
	return time.Date(2026, 10, 23, h, m, s, 0, kernel.CTLocation())
}

func m424Row(tf string, at time.Time, c float64, src string) store.BarHistoryDB {
	return store.BarHistoryDB{Symbol: "MNQ", TF: tf, OpenTimeMs: at.UnixMilli(), O: c, H: c + 2, L: c - 2, C: c, V: 10,
		Convention: "open", Contract: m424Contract, Source: src}
}

// m424Flusher wires the REAL persister closure (newBarPersister) to a real
// store and a real ring, with a settable clock — the production composition.
func m424Flusher(t *testing.T, bh *store.BarHistoryStore, ring *ntwire.BarCache, now *time.Time) *breakFlusher {
	t.Helper()
	rep := &persistReport{}
	f := newBreakFlusher(func() *ntwire.BarCache { return ring }, newBarPersister(bh, func() *ntwire.TCPServer { return nil }, rep), bh, rep)
	f.hold = newReplayHold(0)
	f.clock = func() time.Time { return *now }
	return f
}

func m424Get(t *testing.T, bh *store.BarHistoryStore, tf string, at time.Time) (store.BarHistoryDB, bool) {
	t.Helper()
	rows, err := bh.BarsBetweenOn("MNQ", tf, m424Contract, at.UnixMilli(), at.UnixMilli()+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		return store.BarHistoryDB{}, false
	}
	return rows[0], true
}

// 10-23 fixture: the last live 1m frame is 15:58 with 15:59 forming, then no
// frames at all. Past 16:00 the 15:59 1m and the 15:55 5m must land in the DB
// without any new frame, as live_partial (the AddOn never said final).
func TestM424SessionFinalBarLandsAtTheBreakWithoutAFrame(t *testing.T) {
	bh := m424Store(t)
	if err := bh.InsertBars([]store.BarHistoryDB{
		m424Row("1m", ct1023(15, 57, 0), 29000, store.BarSourceLive),
		m424Row("1m", ct1023(15, 58, 0), 29001, store.BarSourceLive),
		m424Row("5m", ct1023(15, 50, 0), 29000, store.BarSourceLive),
	}); err != nil {
		t.Fatal(err)
	}
	ring := ntwire.NewBarCache(500)
	// frames are CLOSE-stamped on the wire; the ring converts to OPEN-stamp.
	mk := func(at time.Time, dur time.Duration, c float64, final bool) ntwire.Bar {
		return ntwire.Bar{T: at.Add(dur).UnixMilli(), O: c, H: c + 2, L: c - 2, C: c, V: 10, Final: final}
	}
	ring.Upsert("MNQ", "1m", []ntwire.Bar{mk(ct1023(15, 57, 0), time.Minute, 29000, true), mk(ct1023(15, 58, 0), time.Minute, 29001, true), mk(ct1023(15, 59, 0), time.Minute, 29003, false)})
	ring.Upsert("MNQ", "5m", []ntwire.Bar{mk(ct1023(15, 50, 0), 5*time.Minute, 29000, true), mk(ct1023(15, 55, 0), 5*time.Minute, 29003, false)})

	now := ct1023(15, 58, 30) // market open: live frames own persistence
	f := m424Flusher(t, bh, ring, &now)
	f.tick()
	if _, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0)); ok {
		t.Fatal("15:59 persisted while the market was still open and the bar still forming")
	}

	now = ct1023(16, 0, 30) // past the break, no frame has arrived
	f.tick()
	r, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0))
	if !ok {
		t.Fatal("10-23: the session's last 1m bar (15:59) never reached the DB at the break")
	}
	if r.Source != store.BarSourceLivePartial || r.C != 29003 {
		t.Fatalf("15:59 1m = %+v, want live_partial C=29003 (ring bar was not Final)", r)
	}
	r5, ok := m424Get(t, bh, "5m", ct1023(15, 55, 0))
	if !ok || r5.Source != store.BarSourceLivePartial {
		t.Fatalf("15:55 5m = %+v ok=%v, want live_partial", r5, ok)
	}
}

// Never overwrite a better row, and a Final ring bar is stored live.
func TestM424BreakFlushKeepsBetterRowsAndStoresFinalAsLive(t *testing.T) {
	bh := m424Store(t)
	if err := bh.InsertBars([]store.BarHistoryDB{
		m424Row("1m", ct1023(15, 58, 0), 29001, store.BarSourceLive),
		m424Row("1m", ct1023(15, 59, 0), 29010, store.BarSourceLive), // the real final is already stored
	}); err != nil {
		t.Fatal(err)
	}
	ring := ntwire.NewBarCache(500)
	ring.Upsert("MNQ", "1m", []ntwire.Bar{
		{T: ct1023(15, 59, 0).UnixMilli(), O: 1, H: 3, L: 0, C: 29001.5, V: 10, Final: true},
		{T: ct1023(16, 0, 0).UnixMilli(), O: 1, H: 3, L: 0, C: 29003, V: 10}, // stale partial snapshot
	})
	now := ct1023(16, 1, 0)
	f := m424Flusher(t, bh, ring, &now)
	f.tick()
	r, _ := m424Get(t, bh, "1m", ct1023(15, 59, 0))
	if r.Source != store.BarSourceLive || r.C != 29010 {
		t.Fatalf("a partial snapshot displaced a stored final: %+v", r)
	}
	r58, _ := m424Get(t, bh, "1m", ct1023(15, 58, 0))
	if r58.Source != store.BarSourceLive {
		t.Fatalf("Final ring bar stored as %q, want live", r58.Source)
	}
}

// A replay-labelled ring bar is never pushed through the live door.
func TestM424BreakFlushNeverOffersReplayBars(t *testing.T) {
	bh := m424Store(t)
	if err := bh.InsertBars([]store.BarHistoryDB{m424Row("1m", ct1023(15, 50, 0), 29000, store.BarSourceLive)}); err != nil {
		t.Fatal(err)
	}
	ring := ntwire.NewBarCache(500)
	// replay frames arrive close-stamped; SeedHistorical stamps Source=historical
	ring.SeedHistorical("MNQ", "1m", []ntwire.Bar{{T: ct1023(16, 0, 0).UnixMilli(), O: 1, H: 3, L: 0, C: 29500, V: 10}})
	now := ct1023(16, 5, 0)
	f := m424Flusher(t, bh, ring, &now)
	f.tick()
	rows, _ := bh.BarsBetweenOn("MNQ", "1m", m424Contract, ct1023(15, 51, 0).UnixMilli(), ct1023(16, 5, 0).UnixMilli())
	if len(rows) != 0 {
		t.Fatalf("replay bar leaked through the live door: %+v", rows)
	}
}

func m424HeldReplay(f *breakFlusher, closeOffset float64) {
	// a replay of 15:56..15:59 1m; the store's live rows cover 15:56..15:58
	var rows []store.BarHistoryDB
	for i, m := range []int{56, 57, 58, 59} {
		rows = append(rows, m424Row("1m", ct1023(15, m, 0), 29000+float64(i)+closeOffset, store.BarSourceHistorical))
	}
	f.hold.add("MNQ", "1m", rows)
}

func m424LiveRows(t *testing.T, bh *store.BarHistoryStore) {
	t.Helper()
	if err := bh.InsertBars([]store.BarHistoryDB{
		m424Row("1m", ct1023(15, 56, 0), 29000, store.BarSourceLive),
		m424Row("1m", ct1023(15, 57, 0), 29001, store.BarSourceLive),
		m424Row("1m", ct1023(15, 58, 0), 29002, store.BarSourceLive),
	}); err != nil {
		t.Fatal(err)
	}
}

// Restart during the break: the replay is in the hold, no live frame can ever
// release it. On the live scale it is released (only the minute live never
// wrote is filled; the live rows are kept).
func TestM424RestartInBreakReleasesOnScaleReplay(t *testing.T) {
	bh := m424Store(t)
	m424LiveRows(t, bh)
	f := m424Flusher(t, bh, ntwire.NewBarCache(500), new(time.Time))
	now := ct1023(16, 10, 0)
	f.clock = func() time.Time { return now }
	m424HeldReplay(f, 0)
	f.tick()
	if held := f.hold.peek("MNQ", "1m"); len(held) != 0 {
		t.Fatalf("hold still has %d row(s) after a closed-market settle", len(held))
	}
	r, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0))
	if !ok || r.Source != store.BarSourceHistorical {
		t.Fatalf("15:59 after release = %+v ok=%v, want historical", r, ok)
	}
	r58, _ := m424Get(t, bh, "1m", ct1023(15, 58, 0))
	if r58.Source != store.BarSourceLive {
		t.Fatalf("replay overwrote a live row: %+v", r58)
	}
}

// An off-scale replay is discarded, never written; with no overlap it is held;
// with the market open nothing is judged here.
func TestM424RestartInBreakDiscardsOffScaleHoldsWithoutEvidenceAndWaitsWhenOpen(t *testing.T) {
	bh := m424Store(t)
	m424LiveRows(t, bh)
	f := m424Flusher(t, bh, ntwire.NewBarCache(500), new(time.Time))
	now := ct1023(16, 10, 0)
	f.clock = func() time.Time { return now }
	m424HeldReplay(f, 290) // the 09-10 shape: ~290 pts off
	f.tick()
	if _, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0)); ok {
		t.Fatal("an off-scale replay row reached the store")
	}
	if n := len(f.hold.peek("MNQ", "1m")); n != 0 {
		t.Fatalf("off-scale hold not discarded: %d left", n)
	}

	// no live overlap -> no evidence -> kept
	f.hold.add("MNQ", "1m", []store.BarHistoryDB{m424Row("1m", ct1023(14, 0, 0), 29000, store.BarSourceHistorical)})
	f.tick()
	if n := len(f.hold.peek("MNQ", "1m")); n != 1 {
		t.Fatalf("hold with no evidence = %d row(s), want 1 kept", n)
	}

	// market open -> never acts
	m424HeldReplay(f, 0)
	now = ct1023(10, 0, 0)
	f.tick()
	if n := len(f.hold.peek("MNQ", "1m")); n == 0 {
		t.Fatal("hold settled while the market was open")
	}
}

// m426 (review of m424): the first write fails (no contract known: the store
// holds none and no AddOn ACK) — `seen` must NOT be set, so the next tick,
// once a contract exists, lands the session-final bar. Mutant: set seen before
// the persist (the m424 original) → the second tick skips and the bar is lost.
func TestM426FailedBreakFlushIsRetriedNextTick(t *testing.T) {
	bh := m424Store(t) // empty: contractFor finds nothing
	ring := ntwire.NewBarCache(500)
	ring.Upsert("MNQ", "1m", []ntwire.Bar{
		{T: ct1023(15, 59, 0).Add(time.Minute).UnixMilli(), O: 1, H: 3, L: 0, C: 29003, V: 10, Final: true},
	})
	now := ct1023(16, 0, 30)
	f := m424Flusher(t, bh, ring, &now)
	f.tick()
	if _, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0)); ok {
		t.Fatal("bar written with no contract known")
	}
	if len(f.seen) != 0 || f.flushed != 0 {
		t.Fatalf("a failed write was recorded as flushed: seen=%v flushed=%d", f.seen, f.flushed)
	}
	// a contract appears (the store now holds a row for it)
	if err := bh.InsertBars([]store.BarHistoryDB{m424Row("1m", ct1023(15, 40, 0), 29000, store.BarSourceLive)}); err != nil {
		t.Fatal(err)
	}
	now = ct1023(16, 1, 0)
	f.tick()
	if r, ok := m424Get(t, bh, "1m", ct1023(15, 59, 0)); !ok || r.C != 29003 {
		t.Fatalf("the retry never landed the session-final bar: %+v ok=%v", r, ok)
	}
}

// m427/7 — the `seen` dedupe. Two ticks on an UNCHANGED ring tail while the
// market stays closed persist once and log the 🔔 line once (a weekend would
// otherwise rewrite the same tail and repeat the line every 30 s). A changed
// tail (a final landing later) is handed over again.
// Mutant: `if f.seen[key] == fp { continue }` -> `if false {` -> RED.
func TestM427BreakFlushDedupesAnUnchangedTail(t *testing.T) {
	bh := m424Store(t)
	if err := bh.InsertBars([]store.BarHistoryDB{m424Row("1m", ct1023(15, 40, 0), 29000, store.BarSourceLive)}); err != nil {
		t.Fatal(err)
	}
	ring := ntwire.NewBarCache(500)
	ring.Upsert("MNQ", "1m", []ntwire.Bar{
		{T: ct1023(16, 0, 0).UnixMilli(), O: 1, H: 3, L: 0, C: 29003, V: 10},
	})
	var buf bytes.Buffer
	prev := logger.Log.Out
	logger.Log.SetOutput(&buf)
	t.Cleanup(func() { logger.Log.SetOutput(prev) })

	now := ct1023(16, 0, 30)
	f := m424Flusher(t, bh, ring, &now)
	calls := 0
	inner := f.persist
	f.persist = func(h bool, sym, tf string, bars []ntwire.Bar) { calls++; inner(h, sym, tf, bars) }

	f.tick()
	now = ct1023(16, 1, 0)
	f.tick()
	if calls != 1 {
		t.Fatalf("persist called %d times over two ticks on an unchanged tail, want 1", calls)
	}
	if n := strings.Count(buf.String(), "persisted the session's last"); n != 1 {
		t.Fatalf("the 🔔 line was logged %d times, want 1; log: %q", n, buf.String())
	}

	// the tail changes (a final lands): handed over again, logged again
	ring.Upsert("MNQ", "1m", []ntwire.Bar{
		{T: ct1023(16, 0, 0).UnixMilli(), O: 1, H: 3, L: 0, C: 29003, V: 10, Final: true},
	})
	now = ct1023(16, 2, 0)
	f.tick()
	if calls != 2 {
		t.Fatalf("a changed tail must be handed over again: calls=%d, want 2", calls)
	}
}

// m427/7 — settleHold counts only LIVE store rows as evidence. A store holding
// only a HISTORICAL row for the same minutes (a prior release, or a replay of
// another feed) is not a live pair: with a 290-pt-off held replay the verdict
// must be "no pair -> hold kept", not "disagree -> discarded".
// Mutant: `r.Source == store.BarSourceLive` -> `true` -> the hold is discarded -> RED.
func TestM427SettleHoldIgnoresHistoricalStoreRowsAsEvidence(t *testing.T) {
	bh := m424Store(t)
	if err := bh.InsertBars([]store.BarHistoryDB{
		m424Row("1m", ct1023(15, 56, 0), 29000, store.BarSourceHistorical),
		m424Row("1m", ct1023(15, 57, 0), 29001, store.BarSourceHistorical),
	}); err != nil {
		t.Fatal(err)
	}
	f := m424Flusher(t, bh, ntwire.NewBarCache(500), new(time.Time))
	now := ct1023(16, 10, 0)
	f.clock = func() time.Time { return now }
	m424HeldReplay(f, 290)
	f.tick()
	if n := len(f.hold.peek("MNQ", "1m")); n != 4 {
		t.Fatalf("hold = %d row(s), want all 4 kept: a historical store row is not a live pair", n)
	}
}
