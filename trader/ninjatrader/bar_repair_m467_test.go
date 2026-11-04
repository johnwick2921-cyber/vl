package ninjatrader

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// m467 (release #62) - BAR REPAIR below the hole threshold, on the real 11-03
// rows (data.db, MNQ 12-26, read with sqlite3 -readonly). The 1m 17:56 is the
// one-minute gap that sat in the store under the 3-minute hole threshold; the
// 5m 17:50 / 17:55 are the live_partial bars the cross_frame pause read.

type repairWire struct {
	mu       sync.Mutex
	feedUp   bool
	sent     []time.Time
	cache    *ntwire.BarCache
	contract string
}

func (w *repairWire) IsConnected() bool { return true }
func (w *repairWire) IsFeedConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.feedUp
}
func (w *repairWire) RequestGapBackfillAt(sym string, now time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent = append(w.sent, now)
	return nil
}
func (w *repairWire) BarCache() *ntwire.BarCache { return w.cache }
func (w *repairWire) CurrentContract(string) (ntwire.ContractFact, bool) {
	return ntwire.ContractFact{Contract: w.contract}, w.contract != ""
}
func (w *repairWire) BarsBackAsked() int { return 2000 }
func (w *repairWire) sends() int         { w.mu.Lock(); defer w.mu.Unlock(); return len(w.sent) }
func (w *repairWire) setFeed(up bool)    { w.mu.Lock(); w.feedUp = up; w.mu.Unlock() }

// the real 1m rows 17:45-18:05 CT, 17:56 absent (as in data.db)
func m467Rows1m() []store.BarHistoryDB {
	type r struct {
		t          int64
		o, h, l, c float64
		v          float64
		src        string
	}
	rs := []r{
		{1793749500000, 28333.75, 28337.25, 28306.0, 28332.25, 3851, "live"},
		{1793749560000, 28333.25, 28336.25, 28322.0, 28332.75, 1499, "live"},
		{1793749620000, 28333.25, 28353.5, 28322.25, 28344.25, 2367, "live"},
		{1793749680000, 28344.25, 28356.25, 28336.5, 28338.25, 1490, "live"},
		{1793749740000, 28337.5, 28352.5, 28332.25, 28340.5, 1235, "live"},
		{1793749800000, 28340.25, 28346.0, 28338.0, 28343.0, 844, "live"},
		{1793749860000, 28342.25, 28346.0, 28331.5, 28337.75, 1078, "live"},
		{1793749920000, 28337.25, 28348.0, 28334.5, 28347.75, 730, "live"},
		{1793749980000, 28347.75, 28362.0, 28347.5, 28359.75, 1381, "live"},
		{1793750040000, 28360.25, 28368.75, 28347.5, 28357.5, 3145, "live_partial"},
		{1793750100000, 28356.25, 28362.75, 28331.25, 28337.75, 2166, "live"},
		{1793750220000, 28330.75, 28333.5, 28310.75, 28320.0, 1969, "historical"},
		{1793750280000, 28319.25, 28331.75, 28310.75, 28324.0, 1466, "historical"},
		{1793750340000, 28324.25, 28328.0, 28312.0, 28324.5, 1520, "live"},
		{1793750400000, 28324.5, 28351.25, 28317.0, 28346.75, 2484, "live"},
		{1793750460000, 28345.75, 28348.75, 28325.5, 28327.75, 1343, "live"},
		{1793750520000, 28328.25, 28357.75, 28327.75, 28350.5, 2054, "live"},
		{1793750580000, 28351.25, 28368.0, 28348.25, 28365.75, 1973, "live"},
		{1793750640000, 28365.25, 28380.75, 28360.75, 28373.0, 2570, "live"},
		{1793750700000, 28372.5, 28388.75, 28372.0, 28387.5, 1808, "live"},
	}
	out := make([]store.BarHistoryDB, len(rs))
	for i, x := range rs {
		out[i] = store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: x.t, O: x.o, H: x.h, L: x.l, C: x.c, V: x.v,
			Convention: "epoch_floor", Contract: ghContract, Source: x.src}
	}
	return out
}

func m467Store(t *testing.T) *store.BarHistoryStore {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "rep.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := bh.InsertBars(m467Rows1m()); err != nil {
		t.Fatal(err)
	}
	return bh
}

func m467Driver(t *testing.T, bh *store.BarHistoryStore, w *repairWire, clk *fakeClock) (*barRepairDriver, *lineSink) {
	t.Helper()
	sink := &lineSink{}
	d := newBarRepairDriver(func() holeWire { return w }, repairResolver(bh, func() *ntwire.BarCache { return w.cache }), 3)
	d.clock = clk.Now
	d.isOpen = func(time.Time) bool { return true }
	d.say = sink.say
	return d, sink
}

// MUTANT (RED before m467): observeLive judged the 1m store read with the
// 3-minute threshold only -> the 17:56 one-minute gap reached nothing and NO
// re-request was ever sent (the store still lacks 17:56 today).
func TestOneMinuteGapIsRepairedNotPaused(t *testing.T) {
	bh := m467Store(t)
	w := &repairWire{feedUp: true}
	clk := &fakeClock{t: time.UnixMilli(1793750760000).Add(5 * time.Second)} // 18:06:05 CT
	d, sink := m467Driver(t, bh, w, clk)
	tr := newBarHoleTracker(bh, func() *ntwire.TCPServer { return nil })
	tr.wire = func() holeWire { return w }
	tr.clock = clk.Now
	tr.isOpen = func(time.Time) bool { return true }
	tr.say = sink.say
	tr.repair = d

	before := telemetry.BarRepairSnapshot()
	tr.observeLive("MNQ", "1m", ghContract, m467Rows1m())
	if len(tr.holes) != 0 {
		t.Fatalf("a one-minute gap is NOT a hole (the pause threshold is unchanged): %+v", tr.holes)
	}
	if n := sink.count("bar repair NOTED: MNQ 1m gap 11-03 17:56"); n != 1 {
		t.Fatalf("the 17:56 gap must be noted for repair once:\n%s", sink.dump())
	}
	// +1m, inside the first 15 s of a CT minute: the re-request goes out
	clk.Set(clk.Now().Add(time.Minute))
	d.tick(clk.Now())
	if w.sends() != 1 {
		t.Fatalf("want 1 repair re-request, got %d:\n%s", w.sends(), sink.dump())
	}
	// NT8's replay lands the minute (the store upsert takes a historical row
	// where nothing was)
	if err := bh.InsertBars([]store.BarHistoryDB{{Symbol: "MNQ", TF: "1m", OpenTimeMs: 1793750160000, O: 28337.75, H: 28339.0, L: 28318.0, C: 28330.5, V: 2500,
		Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceHistorical}}); err != nil {
		t.Fatal(err)
	}
	clk.Set(clk.Now().Add(barHoleSettle))
	d.tick(clk.Now())
	if n := sink.count("✅ bar REPAIRED: MNQ 1m gap 11-03 17:56"); n != 1 {
		t.Fatalf("the repair must be said once:\n%s", sink.dump())
	}
	after := telemetry.BarRepairSnapshot()
	if after.Asked-before.Asked != 1 || after.Sent-before.Sent != 1 || after.Repaired-before.Repaired != 1 {
		t.Fatalf("counters: before %+v after %+v", before, after)
	}
	if v, _ := BarRepairSnapshot(); len(v) != 0 && barRepairs.Load() == d {
		t.Fatalf("no chain may stay pending: %+v", v)
	}
	clk.Set(clk.Now().Add(time.Hour))
	d.tick(clk.Now())
	if w.sends() != 1 {
		t.Fatalf("a repaired item is never asked again: %d sends", w.sends())
	}
}

// A stored live_partial is re-asked; NT8's replay row REPLACES it in the store;
// a later live_partial snapshot NEVER overwrites a final row.
func TestLivePartialIsReplacedByTheReplayAndNeverOverwritesAFinal(t *testing.T) {
	bh := m467Store(t)
	five := []store.BarHistoryDB{
		{Symbol: "MNQ", TF: "5m", OpenTimeMs: 1793749800000, O: 28340.25, H: 28368.75, L: 28331.5, C: 28357.5, V: 7178, Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceLivePartial},
		{Symbol: "MNQ", TF: "5m", OpenTimeMs: 1793750100000, O: 28356.25, H: 28362.75, L: 28328.5, C: 28333.25, V: 4666, Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceLivePartial},
	}
	if err := bh.InsertBars(five); err != nil {
		t.Fatal(err)
	}
	w := &repairWire{feedUp: true}
	clk := &fakeClock{t: time.UnixMilli(1793750460000).Add(5 * time.Second)} // 18:01:05
	d, sink := m467Driver(t, bh, w, clk)
	noteLivePartials(d, five)
	if n := sink.count("stored live_partial"); n != 0 {
		t.Fatalf("a partial is said only if it is still partial when its first ask is due:\n%s", sink.dump())
	}
	clk.Set(clk.Now().Add(time.Minute))
	d.tick(clk.Now())
	if n := sink.count("bar repair NOTED: MNQ 5m bar"); n != 2 {
		t.Fatalf("both partials noted at the first ask:\n%s", sink.dump())
	}
	if w.sends() != 1 {
		t.Fatalf("ONE re-request covers both (one chain per symbol), got %d", w.sends())
	}
	fixed := []store.BarHistoryDB{
		{Symbol: "MNQ", TF: "5m", OpenTimeMs: 1793749800000, O: 28340.25, H: 28368.75, L: 28331.5, C: 28357.5, V: 7178, Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceHistorical},
		{Symbol: "MNQ", TF: "5m", OpenTimeMs: 1793750100000, O: 28356.25, H: 28362.75, L: 28310.75, C: 28324.5, V: 9621, Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceHistorical},
	}
	if err := bh.InsertBars(fixed); err != nil {
		t.Fatal(err)
	}
	clk.Set(clk.Now().Add(barHoleSettle))
	d.tick(clk.Now())
	if n := sink.count("✅ bar REPAIRED: MNQ 5m bar"); n != 2 {
		t.Fatalf("both partials repaired:\n%s", sink.dump())
	}
	rows, _ := bh.BarsBetweenOn("MNQ", "5m", ghContract, 1793750100000, 1793750100001)
	if len(rows) != 1 || rows[0].L != 28310.75 || rows[0].V != 9621 {
		t.Fatalf("the store must hold NT8's bar now: %+v", rows)
	}
	// a late snapshot of the same period never displaces it
	_ = bh.InsertBars([]store.BarHistoryDB{five[1]})
	rows, _ = bh.BarsBetweenOn("MNQ", "5m", ghContract, 1793750100000, 1793750100001)
	if len(rows) != 1 || rows[0].Source != store.BarSourceHistorical || rows[0].L != 28310.75 {
		t.Fatalf("a live_partial must never overwrite a final row: %+v", rows)
	}
}

// Never while the feed is down: the attempt is DEFERRED (said once, counted)
// and goes out once the feed returns.
func TestRepairIsDeferredWhileTheFeedIsDown(t *testing.T) {
	bh := m467Store(t)
	w := &repairWire{feedUp: false}
	clk := &fakeClock{t: time.UnixMilli(1793750760000).Add(5 * time.Second)}
	d, sink := m467Driver(t, bh, w, clk)
	d.note(repairItem{Kind: repairKindGap, Symbol: "MNQ", TF: "1m", Contract: ghContract, FromMs: 1793750160000, ToMs: 1793750220000})
	for i := 1; i <= 5; i++ {
		clk.Set(clk.Now().Add(time.Minute))
		d.tick(clk.Now())
	}
	if w.sends() != 0 {
		t.Fatalf("a re-request went out while the feed was down")
	}
	if n := sink.count("bar repair re-request #1 DEFERRED for MNQ"); n != 1 {
		t.Fatalf("the deferral must be said ONCE:\n%s", sink.dump())
	}
	w.setFeed(true)
	clk.Set(clk.Now().Add(time.Minute))
	d.tick(clk.Now())
	if w.sends() != 1 {
		t.Fatalf("the deferred repair must go out once the feed is back, got %d", w.sends())
	}
}

// Still bad after the schedule (+1/+3/+10/+30 min): said ONCE as given up, the
// chain ends, nothing more is asked.
func TestRepairGivesUpAfterTheSchedule(t *testing.T) {
	bh := m467Store(t)
	w := &repairWire{feedUp: true}
	clk := &fakeClock{t: time.UnixMilli(1793750760000).Add(5 * time.Second)}
	d, sink := m467Driver(t, bh, w, clk)
	d.note(repairItem{Kind: repairKindGap, Symbol: "MNQ", TF: "1m", Contract: ghContract, FromMs: 1793750160000, ToMs: 1793750220000})
	for i := 0; i < 120; i++ {
		clk.Set(clk.Now().Add(time.Minute))
		d.tick(clk.Now())
	}
	if w.sends() != len(barHoleSchedule) {
		t.Fatalf("want %d scheduled re-requests, got %d", len(barHoleSchedule), w.sends())
	}
	if n := sink.count("bar repair GAVE UP: MNQ 1m gap"); n != 1 {
		t.Fatalf("the give-up must be said once:\n%s", sink.dump())
	}
	if !strings.Contains(sink.dump(), "still not repaired after 4 re-requests") {
		t.Fatalf("give-up line must say why:\n%s", sink.dump())
	}
	// the check keeps reporting it: a given-up item is not chained again
	d.note(repairItem{Kind: repairKindGap, Symbol: "MNQ", TF: "1m", Contract: ghContract, FromMs: 1793750160000, ToMs: 1793750220000})
	for i := 0; i < 60; i++ {
		clk.Set(clk.Now().Add(time.Minute))
		d.tick(clk.Now())
	}
	if w.sends() != len(barHoleSchedule) {
		t.Fatalf("a given-up item was asked again: %d sends", w.sends())
	}
}

// A cross_frame item is measured on the RING with the check's own rule: it is
// repaired the moment NT8's replay replaced the never-finalised 5m bar.
func TestCrossFrameItemIsRepairedWhenTheRingAgrees(t *testing.T) {
	cache := ntwire.NewBarCache(2500)
	for _, r := range m467Rows1m() {
		cache.Upsert("MNQ", "1m", []ntwire.Bar{{T: r.OpenTimeMs + 60_000, O: r.O, H: r.H, L: r.L, C: r.C, V: r.V, Final: true}})
	}
	// the 17:56 minute the ring held (synthesised inside the logged built range)
	cache.SeedHistorical("MNQ", "1m", []ntwire.Bar{{T: 1793750160000 + 60_000, O: 28337.75, H: 28339.0, L: 28318.0, C: 28330.5, V: 2500}})
	for _, b := range []ntwire.Bar{
		{T: 1793750100000, O: 28356.25, H: 28362.75, L: 28328.5, C: 28333.25, V: 4666},
		{T: 1793750400000, O: 28324.5, H: 28380.75, L: 28317.0, C: 28373.0, V: 10424},
	} {
		b.T += 300_000
		cache.Upsert("MNQ", "5m", []ntwire.Bar{b})
	}
	it := repairItem{Kind: repairKindCross, Symbol: "MNQ", TF: "5m", FromMs: 1793750100000, ToMs: 1793750400000}
	if crossFrameRepaired(cache, it) {
		t.Fatal("baseline: the 5m 17:55 snapshot disagrees with its 1m")
	}
	cache.SeedHistorical("MNQ", "5m", []ntwire.Bar{{T: 1793750400000, O: 28356.25, H: 28362.75, L: 28310.75, C: 28324.5, V: 9621}})
	if !crossFrameRepaired(cache, it) {
		t.Fatalf("after NT8's replay the bar agrees with its 1m: %+v", cache.Get("MNQ", "5m"))
	}
}

// The persister writes the cache's closed tail on every live frame: a bar
// whose final frame lands a moment after the boundary is stored live_partial
// for an instant, then overwritten live. That is never said, never counted,
// never asked of NT8.
func TestTransientPartialIsNeverSaidNorAsked(t *testing.T) {
	bh := m467Store(t)
	w := &repairWire{feedUp: true}
	clk := &fakeClock{t: time.UnixMilli(1793750460000).Add(5 * time.Second)}
	d, sink := m467Driver(t, bh, w, clk)
	row := store.BarHistoryDB{Symbol: "MNQ", TF: "3m", OpenTimeMs: 1793750220000, O: 28330.75, H: 28333.5, L: 28310.75, C: 28324.5, V: 4955, Convention: "epoch_floor", Contract: ghContract, Source: store.BarSourceLivePartial}
	_ = bh.InsertBars([]store.BarHistoryDB{row})
	before := telemetry.BarRepairSnapshot()
	noteLivePartials(d, []store.BarHistoryDB{row})
	row.Source = store.BarSourceLive // the final frame, a second later
	_ = bh.InsertBars([]store.BarHistoryDB{row})
	for i := 0; i < 10; i++ {
		clk.Set(clk.Now().Add(30 * time.Second))
		d.tick(clk.Now())
	}
	after := telemetry.BarRepairSnapshot()
	if w.sends() != 0 || len(sink.lines) != 0 || after.Asked != before.Asked || after.Repaired != before.Repaired {
		t.Fatalf("a transient partial must stay silent: sends=%d counts %+v -> %+v\n%s", w.sends(), before, after, sink.dump())
	}
}
