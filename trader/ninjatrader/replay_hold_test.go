package ninjatrader

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── fixture ────────────────────────────────────────────────────────────────
//
// The live situation of 2026-09-10 22:39 CT, in miniature. A prior process
// wrote minutes 0–9 LIVE at ~29350. The process restarts; the AddOn replays
// minutes 5–14 at ~29060 (the same contract, ~290 pts below — facts 16516009
// vs 16518205). Minutes 10–14 exist ONLY in the replay: the restart gap.

const (
	rhLiveScale   = 29350.0
	rhReplayScale = 29060.0
	rhContract    = "MNQ 12-26"
	minuteMs      = int64(60_000)
)

type rhFixture struct {
	bh     *store.BarHistoryStore
	server *ntwire.TCPServer
	t0     int64
}

func newRHFixture(t *testing.T) rhFixture {
	t.Helper()
	barReplayHold = newReplayHold(0)
	st, err := store.New(filepath.Join(t.TempDir(), "rh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-2 * time.Hour).Truncate(time.Minute).UnixMilli()
	live := make([]store.BarHistoryDB, 0, 10)
	for i := int64(0); i < 10; i++ {
		live = append(live, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: t0 + i*minuteMs,
			O: rhLiveScale, H: rhLiveScale + 2, L: rhLiveScale - 2, C: rhLiveScale + 1, V: 10,
			Convention: "open", Contract: rhContract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(live); err != nil {
		t.Fatal(err)
	}
	server := ntwire.NewTCPServer(nil) // unstarted; the ring and the store fallback are all this needs
	replay := make([]ntwire.Bar, 0, 10)
	for i := int64(5); i < 15; i++ {
		// close-stamped, as the AddOn sends them; SeedHistorical open-stamps
		replay = append(replay, ntwire.Bar{T: t0 + (i+1)*minuteMs, O: rhReplayScale, H: rhReplayScale + 2, L: rhReplayScale - 2, C: rhReplayScale + 1, V: 10})
	}
	server.BarCache().SeedHistorical("MNQ", "1m", replay)
	return rhFixture{bh: bh, server: server, t0: t0}
}

func (f rhFixture) census(t *testing.T) map[string]int64 {
	t.Helper()
	c, err := f.bh.SourceCensus("MNQ")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f rhFixture) gapRows(t *testing.T) []store.BarHistoryDB {
	t.Helper()
	rows, err := f.bh.BarsBetweenOn("MNQ", "1m", rhContract, f.t0+10*minuteMs, f.t0+15*minuteMs)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (f rhFixture) liveBar(i int64, scale float64) []ntwire.Bar {
	return []ntwire.Bar{{T: f.t0 + i*minuteMs, O: scale, H: scale + 2, L: scale - 2, C: scale + 1, V: 10}}
}

// resolveAsThePersisterWould is exactly the two lines the persister runs on a
// live frame (pinned below at the call site).
func (f rhFixture) resolveAsThePersisterWould() {
	checked, offScale := f.server.BarCache().SeedVerdict("MNQ", "1m")
	barReplayHold.resolve("MNQ", "1m", checked, offScale, f.bh.InsertBars)
}

// ── pins ───────────────────────────────────────────────────────────────────

// THE BOOT BACKFILL WRITES NO REPLAY ROW. Before this wave it flushed the
// whole seed into the store the moment it landed — on the wrong scale, into
// every minute live never wrote.
func TestBootBackfillHoldsTheReplayOutOfTheStore(t *testing.T) {
	f := newRHFixture(t)
	n := backfillBars(f.bh, f.server)
	if n == 0 {
		t.Fatal("backfill must report the held replay as landed, or the boot loop retries for five minutes")
	}
	c := f.census(t)
	if c[store.BarSourceHistorical] != 0 {
		t.Fatalf("the store received %d replay row(s) before any live bar judged the replay's scale", c[store.BarSourceHistorical])
	}
	if held, _, _, _ := barReplayHold.heldCount("MNQ"); held != 10 {
		t.Fatalf("the hold should carry the 10 replay minutes, has %d", held)
	}
	if got := f.gapRows(t); len(got) != 0 {
		t.Fatalf("the restart gap must stay EMPTY until the replay is judged, got %d row(s)", len(got))
	}
}

// AN OFF-SCALE REPLAY NEVER REACHES THE STORE. The first live bar arrives ~290
// above the replay; the ring says off-scale; the hold discards. The gap stays
// empty — an empty minute reads as a gap, a wrong-scale minute reads as the
// largest move of the day.
func TestOffScaleReplayIsDiscardedOnTheRingsVerdict(t *testing.T) {
	f := newRHFixture(t)
	backfillBars(f.bh, f.server)
	f.server.BarCache().Upsert("MNQ", "1m", f.liveBar(15, rhLiveScale))
	if checked, off := f.server.BarCache().SeedVerdict("MNQ", "1m"); !checked || !off {
		t.Fatalf("the ring must have judged this seed off-scale, got checked=%v offScale=%v", checked, off)
	}
	f.resolveAsThePersisterWould()
	c := f.census(t)
	if c[store.BarSourceHistorical] != 0 {
		t.Fatalf("an off-scale replay reached the store: %d historical row(s)", c[store.BarSourceHistorical])
	}
	if got := f.gapRows(t); len(got) != 0 {
		t.Fatalf("the restart gap must stay EMPTY after an off-scale replay, got %d row(s) — the next boot's rehydrate would carry them into the ring", len(got))
	}
	held, written, discarded, _ := barReplayHold.heldCount("MNQ")
	if held != 0 || written != 0 || discarded != 10 {
		t.Fatalf("hold after discard: held=%d written=%d discarded=%d, want 0/0/10", held, written, discarded)
	}
	// and the live rows the replay collided with are untouched
	rows, _ := f.bh.LastNBarsOn("MNQ", "1m", rhContract, 100)
	for _, r := range rows {
		if r.C != rhLiveScale+1 {
			t.Fatalf("live row @%d changed to %.2f", r.OpenTimeMs, r.C)
		}
	}
}

// A REPLAY ON THE LIVE SCALE IS RELEASED, as 'historical', and only into the
// minutes live never wrote. The upsert rule keeps every live row.
func TestOnScaleReplayIsReleasedAsHistoricalIntoTheGapOnly(t *testing.T) {
	f := newRHFixture(t)
	// this replay is on the live scale
	onScale := make([]ntwire.Bar, 0, 10)
	for i := int64(5); i < 15; i++ {
		onScale = append(onScale, ntwire.Bar{T: f.t0 + (i+1)*minuteMs, O: rhLiveScale, H: rhLiveScale + 2, L: rhLiveScale - 2, C: rhLiveScale - 1, V: 10})
	}
	f.server = ntwire.NewTCPServer(nil)
	f.server.BarCache().SeedHistorical("MNQ", "1m", onScale)
	backfillBars(f.bh, f.server)
	f.server.BarCache().Upsert("MNQ", "1m", f.liveBar(15, rhLiveScale))
	f.resolveAsThePersisterWould()
	c := f.census(t)
	if c[store.BarSourceHistorical] != 5 {
		t.Fatalf("the replay should fill exactly the 5 gap minutes as historical, got %d", c[store.BarSourceHistorical])
	}
	if c[store.BarSourceLive] != 10 {
		t.Fatalf("the 10 live rows must all still be live, got %d", c[store.BarSourceLive])
	}
	rows, _ := f.bh.LastNBarsOn("MNQ", "1m", rhContract, 100)
	for _, r := range rows {
		if r.OpenTimeMs < f.t0+10*minuteMs && r.C != rhLiveScale+1 {
			t.Fatalf("live row @%d overwritten by the released replay (close %.2f)", r.OpenTimeMs, r.C)
		}
	}
	if _, written, discarded, _ := barReplayHold.heldCount("MNQ"); written != 10 || discarded != 0 {
		t.Fatalf("hold after release: written=%d discarded=%d, want 10/0", written, discarded)
	}
}

// NO VERDICT, NO WRITE. A live frame before the ring has compared anything
// (cold key — the replay lands after) leaves the hold holding.
func TestReplayHoldWaitsForAVerdict(t *testing.T) {
	barReplayHold = newReplayHold(0)
	calls := 0
	rows := []store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", OpenTimeMs: 1, Source: store.BarSourceHistorical}}
	barReplayHold.add("MNQ", "1m", rows)
	barReplayHold.resolve("MNQ", "1m", false, false, func([]store.BarHistoryDB) error { calls++; return nil })
	if calls != 0 {
		t.Fatal("an unjudged replay was written")
	}
	if held, _, _, _ := barReplayHold.heldCount("MNQ"); held != 1 {
		t.Fatalf("still held: want 1 got %d", held)
	}
	barReplayHold.resolve("MNQ", "1m", true, false, func([]store.BarHistoryDB) error { calls++; return nil })
	if calls != 1 {
		t.Fatal("a judged, on-scale replay must be written exactly once")
	}
}

// THE HOLD IS ONE ROW PER MINUTE AND KEEPS THE NEWEST TAIL. The replay frame
// and the boot backfill both hand the same seed over; a full hold drops the
// OLDEST, never the minutes that abut the live feed.
func TestReplayHoldDedupesAndCapsToTheNewestTail(t *testing.T) {
	h := newReplayHold(3)
	mk := func(ts ...int64) []store.BarHistoryDB {
		out := make([]store.BarHistoryDB, 0, len(ts))
		for _, t := range ts {
			out = append(out, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: t, C: float64(t)})
		}
		return out
	}
	h.add("MNQ", "1m", mk(1, 2, 3))
	h.add("MNQ", "1m", mk(2, 3, 4, 5)) // 2,3 again + two newer
	held, _, _, overflow := h.heldCount("MNQ")
	if held != 3 || overflow != 2 {
		t.Fatalf("held=%d overflow=%d, want 3/2 (minutes 1 and 2 dropped as oldest)", held, overflow)
	}
	var got []int64
	h.resolve("MNQ", "1m", true, false, func(rows []store.BarHistoryDB) error {
		for _, r := range rows {
			got = append(got, r.OpenTimeMs)
		}
		return nil
	})
	if len(got) != 3 || got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("released %v, want [3 4 5]", got)
	}
}

// THE HOLD IS WIRED, not merely built (A29). Text-level pin: the persister
// closure holds every historical frame and resolves on every live frame; the
// backfill routes the ring's historical bars to the hold.
func TestReplayHoldIsWiredIntoThePersisterAndTheBackfill(t *testing.T) {
	src, err := os.ReadFile("bar_persist_wire.go")
	if err != nil {
		t.Fatal(err)
	}
	// GAP-BACKFILL (2026-10-14): the closure was lifted into newBarPersister so
	// a production-call-site test can drive it; WireBarPersistence installs it.
	closure := regexp.MustCompile(`(?s)func newBarPersister\(.*?\n\}\n`).Find(src)
	if closure == nil {
		t.Fatal("could not locate the persister closure (newBarPersister)")
	}
	if !regexp.MustCompile(`ntwire\.SetBarPersister\(newBarPersister\(bh, serverFn\)\)`).Match(src) {
		t.Fatal("WireBarPersistence no longer installs newBarPersister — the persister closure is unwired")
	}
	for _, want := range []string{
		`barReplayHold.resolve(symbol, tf, checked, offScale, bh.InsertBars)`,
		`barReplayHold.add(symbol, tf, rows)`,
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(closure) {
			t.Fatalf("the persister closure no longer contains %q — the hold is unwired", want)
		}
	}
	backfill := regexp.MustCompile(`(?s)func backfillBars\(.*?\n\}\n`).Find(src)
	if backfill == nil || !regexp.MustCompile(`barReplayHold\.add\(pair\[0\], pair\[1\], hold\)`).Match(backfill) {
		t.Fatal("backfillBars no longer routes historical ring bars to the hold")
	}
}
