package trader

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-FLIP-REREAD (2026-09-17) — a fired flip must produce a re-read in the
// FLIPPED direction (class NN), instead of only sleeping. These tests pin the
// knob gate, the once-per-fired-flip semantics, the refusal path, the death
// path, and the ASIA-v13 replay shape (built from dispatch quotes because
// /home/hoang/nofx-r101/data/db.copy.db predates the overnight ASIA rows).

// flipRereadRecorder substitutes the read-call seam so fixtures can observe the
// request without running a live planner stream. It can append a fresh plan
// before reporting success, so the async supersede tail is exercisable.
type flipRereadRecorder struct {
	mu       sync.Mutex
	calls    []string // prior lines, one per request
	appendV2 *store.PlanDB
}

func (r *flipRereadRecorder) run(at *AutoTrader, session, tradeDate, prior string, row *store.PlanDB, failClosed bool) bool {
	r.mu.Lock()
	r.calls = append(r.calls, prior)
	app := r.appendV2
	r.mu.Unlock()
	if app != nil {
		if _, err := at.store.Plan().AppendPlan(app); err != nil {
			return false
		}
	}
	return true
}

func (r *flipRereadRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *flipRereadRecorder) priors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// installFlipRecorder swaps the seam for the test duration.
func installFlipRecorder(t *testing.T, r *flipRereadRecorder) {
	t.Helper()
	orig := flipRereadRun
	flipRereadRun = func(at *AutoTrader, session, tradeDate, prior string, row *store.PlanDB, failClosed bool) bool {
		return r.run(at, session, tradeDate, prior, row, failClosed)
	}
	t.Cleanup(func() { flipRereadRun = orig })
}

func sysCfgVal(t *testing.T, st *store.Store, key string) string {
	t.Helper()
	v, err := st.GetSystemConfig(key)
	if err != nil {
		t.Fatalf("GetSystemConfig(%q): %v", key, err)
	}
	return v
}

// seedFlipBars builds the standard flip fixture: flat minutes at `flat`, then
// `drift` minutes at `driftPx`, ending endAge before the synthetic now.
func seedFlipBars(flatPx, driftPx float64, endAge time.Duration, now time.Time) {
	t0 := now.Add(-(endAge + 24*time.Minute)).UnixMilli() // 24 one-minute bars
	bars := make([]market.Kline, 0, 24)
	for i := 0; i < 14; i++ {
		ot := t0 + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: flatPx, High: flatPx, Low: flatPx, Close: flatPx})
	}
	for i := 0; i < 10; i++ { // two 5m buckets closing on the drift side
		ot := t0 + int64(14+i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: driftPx, High: driftPx, Low: driftPx, Close: driftPx})
	}
	barsAt(bars)
}

// seedActivePlan appends v1 of an active plan for the session-day.
func seedActivePlan(t *testing.T, at *AutoTrader, td, session string, birth time.Time, doc kernel.PlanDoc) *store.PlanDB {
	t.Helper()
	blob, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	pid := store.MakePlanIDForTrader(at.id, td, session)
	if _, err := at.store.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: session, StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: birth}); err != nil {
		t.Fatal(err)
	}
	row, err := at.store.Plan().GetLatestPlanForTraderSession(td, session, at.id)
	if err != nil || row == nil {
		t.Fatalf("read back seed: %+v err=%v", row, err)
	}
	return row
}

func versionLifecycle(t *testing.T, st *store.Store, td, session, id string, version int) string {
	t.Helper()
	rows, err := st.Plan().ListVersionsForTrader(td, session, id)
	if err != nil {
		t.Fatalf("ListVersionsForTrader: %v", err)
	}
	for _, r := range rows {
		if r.Version == version {
			return r.Lifecycle
		}
	}
	return ""
}

func lastLifecycleReason(t *testing.T, st *store.Store, row *store.PlanDB) string {
	t.Helper()
	events, err := st.Plan().LifecycleLog(row.PlanID, row.Version)
	if err != nil || len(events) == 0 {
		t.Fatalf("lifecycle log: %v (%d events)", err, len(events))
	}
	return events[len(events)-1].Reason
}

// dormantFlipKiller returns the killer of THIS row's dormant:flip event, which
// may no longer be the LAST event once the structure_flip read supersedes it.
func dormantFlipKiller(t *testing.T, st *store.Store, row *store.PlanDB) string {
	t.Helper()
	events, err := st.Plan().LifecycleLog(row.PlanID, row.Version)
	if err != nil {
		t.Fatalf("lifecycle log: %v", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if strings.HasPrefix(events[i].Reason, "dormant:flip:") {
			return strings.TrimPrefix(events[i].Reason, "dormant:flip:")
		}
	}
	t.Fatalf("no dormant:flip event in the log")
	return ""
}

func TestFlipRereadKnobOffStaysDormantNoRead(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	rec := &flipRereadRecorder{}
	installFlipRecorder(t, rec)
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	td := "2026-08-18"
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", FlipCondition: "flips short on 2x5m below 100"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m", FlipTo: "short"}}
	seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), doc)
	seedFlipBars(100, 96, 6*time.Minute, now)

	at.maybeRunSessionReadsAt(now)

	row, err := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if err != nil || row == nil {
		t.Fatalf("read back: %v", err)
	}
	if row.Lifecycle != "dormant" || row.Version != 1 {
		t.Fatalf("knob OFF must park dormant on the SAME version, got %s v%d", row.Lifecycle, row.Version)
	}
	if reason := lastLifecycleReason(t, st, row); !strings.HasPrefix(reason, "dormant:flip:") {
		t.Fatalf("dormant marker wrong: %q", reason)
	}
	if rec.count() != 0 {
		t.Fatalf("knob OFF must not request a re-read, got %d calls", rec.count())
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v != "" && v != "0" {
		t.Fatalf("knob OFF must not set the once-key, got %q", v)
	}
}

func TestFlipRereadOnRequestsStructureFlipOnce(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}, FlipReread: true}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	td := "2026-08-18"
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", FlipCondition: "flips short on 2x5m below 100"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m", FlipTo: "short"}}
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), doc)
	seedFlipBars(100, 96, 6*time.Minute, now)

	// The recorder appends a FLIPPED-bias v2 before reporting success, so the
	// async tail (fresh-version fetch → supersede) is exercised end to end.
	v2 := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short"}}
	blob2, _ := json.Marshal(v2)
	rec := &flipRereadRecorder{appendV2: &store.PlanDB{PlanID: row.PlanID, TradeDate: td, Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob2), CreatedAt: now.Add(5 * time.Minute)}}
	installFlipRecorder(t, rec)

	at.maybeRunSessionReadsAt(now)

	// Sync half first: the dormant marker is written before the read launches.
	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("flip must park dormant first, got %q", got)
	}
	// Capture the killer now — the supersede appends a LATER lifecycle event.
	killer := dormantFlipKiller(t, st, row)
	if !waitFor(t, 5*time.Second, func() bool {
		return versionLifecycle(t, st, td, "NY", at.id, 1) == "superseded:flip"
	}) {
		t.Fatal("v1 was not superseded by the structure_flip read")
	}
	if rec.count() != 1 {
		t.Fatalf("exactly ONE structure_flip read per fired flip, got %d", rec.count())
	}
	priors := rec.priors()
	if len(priors) != 1 {
		t.Fatalf("prior capture: %d", len(priors))
	}
	for _, want := range []string{"PRIOR PLAN v1 bias long", "bias is now expected short", "flip-condition:", "the prior plan is dormant"} {
		if !strings.Contains(priors[0], want) {
			t.Fatalf("prior must carry %q, got:\n%s", want, priors[0])
		}
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v == "" || v == "0" {
		t.Fatalf("successful read must keep the once-key set, got %q", v)
	}
	// A second fire of the SAME version must not launch a second read.
	at.maybeRereadAfterFlip(now, "NY", td, row, killer)
	if rec.count() != 1 {
		t.Fatalf("once-key must gate a second fire, got %d calls", rec.count())
	}
}

func TestFlipRereadSameBiasStillSupersedesNoLoop(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}, FlipReread: true}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	td := "2026-08-18"
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", FlipCondition: "flips short on 2x5m below 100"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m", FlipTo: "short"}}
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), doc)
	seedFlipBars(100, 96, 6*time.Minute, now)

	// The model DISAGREES with the flip: v2 keeps the old bias. The supersede
	// still stands (a fresh version landed), but no second read ever fires.
	v2 := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long"}}
	blob2, _ := json.Marshal(v2)
	rec := &flipRereadRecorder{appendV2: &store.PlanDB{PlanID: row.PlanID, TradeDate: td, Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob2), CreatedAt: now.Add(5 * time.Minute)}}
	installFlipRecorder(t, rec)

	at.maybeRunSessionReadsAt(now)
	if !waitFor(t, 5*time.Second, func() bool {
		return versionLifecycle(t, st, td, "NY", at.id, 1) == "superseded:flip"
	}) {
		t.Fatal("v1 was not superseded despite the same-bias v2")
	}
	if rec.count() != 1 {
		t.Fatalf("one read per fired flip even on disagreement, got %d", rec.count())
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v == "" || v == "0" {
		t.Fatalf("the once-key must stay consumed, got %q", v)
	}
}

func TestFlipRereadPreflightRefusalKeepsDormantPlan(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}, FlipReread: true}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	td := "2026-08-18"
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m", FlipTo: "short"}}
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), doc)
	// Bars end 15 minutes before now: old enough for the G7 flip eval to have
	// been possible in a prior cycle, but STALE for the planner preflight
	// (feedDownAfter default = 10m) — the refusal path must not consume the key.
	seedFlipBars(100, 96, 15*time.Minute, now)
	rec := &flipRereadRecorder{}
	installFlipRecorder(t, rec)

	at.maybeRereadAfterFlip(now, "NY", td, row, "flip-condition: 2x5m close below 100.00 → bias short")

	if rec.count() != 0 {
		t.Fatalf("a preflight refusal must not launch the read, got %d calls", rec.count())
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v != "" && v != "0" {
		t.Fatalf("a refused read must not consume the once-key, got %q", v)
	}
}

func TestFlipRereadDeathConditionUnchanged(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}, FlipReread: true}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	td := "2026-08-18"
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long"}, DeathStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m"}}
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), doc)
	seedFlipBars(100, 96, 6*time.Minute, now)
	rec := &flipRereadRecorder{}
	installFlipRecorder(t, rec)

	at.maybeRunSessionReadsAt(now)

	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("structured death must park dormant, got %q", got)
	}
	if reason := lastLifecycleReason(t, st, row); !strings.HasPrefix(reason, "dormant:death:") {
		t.Fatalf("death marker wrong: %q", reason)
	}
	if rec.count() != 0 {
		t.Fatalf("death-condition must NOT request a structure_flip read, got %d calls", rec.count())
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v != "" && v != "0" {
		t.Fatalf("death path must not set the flip once-key, got %q", v)
	}
}

// TestFlipRereadAsiaV13ReplayFixture replays the owner's overnight ASIA v13
// shape: plan bias SHORT with a flip above 29418.8 → bias long; price broke up
// through the line. PROVENANCE: /home/hoang/nofx-r101/data/db.copy.db predates
// the 2026-09-16 overnight ASIA rows (the copy was made 16:00 CT), so this
// fixture is built from the dispatch quotes, not from the DB.
func TestFlipRereadAsiaV13ReplayFixture(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"ASIA"}, FlipReread: true}}
	at, st := resetTrader(t, cfg)
	// Enable ASIA in the ADMIN registry the way a production edit does.
	reg := kernel.DefaultSessionRegistry()
	reg.Sessions[0].Enabled = true
	regBlob, _ := json.Marshal(reg)
	if err := st.SetSystemConfig(kernel.SessionRegistryConfigKey, string(regBlob)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 6, 0, 0, 0, time.UTC) // 01:00 CT, inside ASIA (ends 02:00 CT)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	asia := &reg.Sessions[0]
	td, ok := kernel.PlanChainTradeDate(asia, now)
	if !ok {
		t.Fatalf("PlanChainTradeDate for ASIA at %v", now)
	}
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short"}, FlipStructured: &kernel.PlanCondition{Price: 29418.8, Side: "above", Rule: "2x5m", FlipTo: "long"}}
	row := seedActivePlan(t, at, td, "ASIA", now.Add(-40*time.Minute), doc)
	seedFlipBars(29400, 29430, 6*time.Minute, now)
	rec := &flipRereadRecorder{} // no append: the read "succeeds" but authors nothing → dormant stands
	installFlipRecorder(t, rec)

	at.maybeRunSessionReadsAt(now)

	if got := versionLifecycle(t, st, td, "ASIA", at.id, 1); got != "dormant" {
		t.Fatalf("ASIA v13 flip must park dormant, got %q", got)
	}
	if !waitFor(t, 5*time.Second, func() bool { return rec.count() == 1 }) {
		t.Fatal("the structure_flip read request never landed")
	}
	priors := rec.priors()
	if len(priors) != 1 {
		t.Fatalf("prior capture: %d", len(priors))
	}
	for _, want := range []string{"PRIOR PLAN v1 bias short", "bias is now expected long", "flip-condition:", "the prior plan is dormant"} {
		if !strings.Contains(priors[0], want) {
			t.Fatalf("ASIA prior must carry %q, got:\n%s", want, priors[0])
		}
	}
	if v := sysCfgVal(t, st, flipRereadDoneKey(row)); v == "" || v == "0" {
		t.Fatalf("the once-key must be consumed by the ASIA read, got %q", v)
	}
}
