package trader

import (
	"path/filepath"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/mcp"
	"vl/store"
)

// wakeBars builds TF bars (open,high,low,close rows) spaced `tfMin` minutes,
// ending before `endMs`.
func wakeBars(tfMin int64, endMs int64, rows [][4]float64) []market.Kline {
	step := tfMin * 60_000
	bars := make([]market.Kline, len(rows))
	// first bar's OpenTime is endMs - len*step (all bars closed before endMs).
	first := endMs - int64(len(rows))*step
	for i, r := range rows {
		open := first + int64(i)*step
		bars[i] = market.Kline{
			OpenTime:  open,
			Open:      r[0],
			High:      r[1],
			Low:       r[2],
			Close:     r[3],
			CloseTime: open + step - 1,
		}
	}
	return bars
}

// zonePattern15m returns 15m bars with a reversal demand zone: bar0 is the down
// leg, bars1–6 the flat base (bodies 0), bar7 the +5.0 departure. With ~2.4
// range bars ATR14≈2.4 → smallBody≈1.2, departure≈3.6. Departure bar = index 7.
func zonePattern15m(endMs int64) []market.Kline {
	rows := [][4]float64{
		{101.5, 102.0, 99.5, 100.0}, // down leg (body −1.5 > smallBody)
		{100.0, 101.0, 99.0, 100.0}, // base 1..6
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 105.5, 99.8, 105.0}, // departure up (+5.0 ≥ 1.5×ATR)
	}
	// filler keeps ATR14 defined (range 2, body 0 → no extra zones).
	for i := 0; i < 12; i++ {
		rows = append(rows, [4]float64{100.0, 101.0, 99.0, 100.0})
	}
	return wakeBars(15, endMs, rows)
}

func TestCollectWakeCandidates15mReversalZone(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	bars := zonePattern15m(now.UnixMilli())
	row := &store.PlanDB{PlanID: "p1", Version: 1,
		CreatedAt: now.Add(-24 * time.Hour)} // older than the whole bar series
	fetch := func(tf string, count int) []market.Kline {
		if tf == "15m" {
			return bars
		}
		return nil
	}
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, nil, now)
	if len(cands) != 1 {
		t.Fatalf("expected exactly one 15m reversal-zone candidate, got %d: %+v", len(cands), cands)
	}
	c := cands[0]
	if c.kind != "zone" || c.tier != "15m" || c.prio != wakePrio15mZone {
		t.Fatalf("candidate = kind %q tier %q prio %d, want zone/15m/%d", c.kind, c.tier, c.prio, wakePrio15mZone)
	}
	if c.birthMs != bars[7].OpenTime {
		t.Fatalf("birth = %d want %d (departure bar)", c.birthMs, bars[7].OpenTime)
	}
}

func TestCollectWakeCandidatesKnobOff15m(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-24 * time.Hour)}
	off := false
	fetch := func(tf string, count int) []market.Kline {
		if tf == "15m" {
			return zonePattern15m(now.UnixMilli())
		}
		return nil
	}
	// W-KNOB-PRUNE: the single switch off suppresses every class.
	cfg := &store.DayPlanConfig{WakeOnLevelEvents: &off}
	if cands := collectLevelWakeCandidates(cfg, fetch, "MNQ", row, nil, now); len(cands) != 0 {
		t.Fatalf("wake_on_level_events=false must suppress 15m candidates, got %+v", cands)
	}
	// Legacy mapping: a lone stored wake_on_15m_zone=false no longer suppresses
	// (any legacy switch ON — nil reads ON — maps the single switch ON).
	legacy := &store.DayPlanConfig{WakeOn15mZone: &off}
	if cands := collectLevelWakeCandidates(legacy, fetch, "MNQ", row, nil, now); len(cands) != 1 {
		t.Fatalf("legacy lone wake_on_15m_zone=false maps to ON, want 1 candidate, got %+v", cands)
	}
	// Legacy mapping: all five false maps OFF.
	allOff := &store.DayPlanConfig{WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off}
	if cands := collectLevelWakeCandidates(allOff, fetch, "MNQ", row, nil, now); len(cands) != 0 {
		t.Fatalf("all five legacy switches false must map to OFF, got %+v", cands)
	}
}

func TestCollectWakeCandidatesHTFZoneAndOB(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-24 * time.Hour)}
	// Same pattern on 1h → an HTF S/D zone (any pattern counts) + OBs.
	bars1h := wakeBars(60, now.UnixMilli(), func() [][4]float64 {
		rows := [][4]float64{
			{101.5, 102.0, 99.5, 100.0},
			{100.0, 101.0, 99.0, 100.0}, {100.0, 101.0, 99.0, 100.0},
			{100.0, 101.0, 99.0, 100.0}, {100.0, 101.0, 99.0, 100.0},
			{100.0, 101.0, 99.0, 100.0}, {100.0, 101.0, 99.0, 100.0},
			{100.0, 105.5, 99.8, 105.0},
		}
		for i := 0; i < 12; i++ {
			rows = append(rows, [4]float64{100.0, 101.0, 99.0, 100.0})
		}
		return rows
	}())
	fetch := func(tf string, count int) []market.Kline {
		if tf == "1h" {
			return bars1h
		}
		return nil
	}
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, nil, now)
	foundZone := false
	for _, c := range cands {
		if c.kind == "zone" && c.tier == "1h" && c.prio == wakePrioHTFZone {
			foundZone = true
		}
		if c.kind == "ob" {
			t.Fatalf("wake_on_htf_ob=false (default) must suppress OB candidates, got %+v", c)
		}
	}
	if !foundZone {
		t.Fatalf("expected an HTF zone candidate, got %+v", cands)
	}

	// Enable the OB knob → the displacement bar yields OB candidates.
	on := true
	cfg := &store.DayPlanConfig{WakeOnHTFOB: true, WakeOn15mZone: &on}
	cands = collectLevelWakeCandidates(cfg, fetch, "MNQ", row, nil, now)
	foundOB := false
	for _, c := range cands {
		if c.kind == "ob" && c.prio == wakePrioHTFOB {
			foundOB = true
		}
	}
	if !foundOB {
		t.Fatalf("wake_on_htf_ob=true must yield OB candidates, got %+v", cands)
	}
}

func TestCollectWakeCandidatesIFVG(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-24 * time.Hour)}
	// Bullish gap [100,103] on 15m, then a later close below 100 → iFVG(bear).
	bars := wakeBars(15, now.UnixMilli(), [][4]float64{
		{99.0, 99.5, 98.0, 99.0},
		{101.0, 104.0, 100.5, 103.5},
		{104.0, 105.0, 103.0, 104.5},
		{103.0, 103.5, 97.0, 99.0}, // close 99 < 100 → inversion
		{99.0, 100.0, 98.0, 99.5},
	})
	fetch := func(tf string, count int) []market.Kline {
		if tf == "15m" {
			return bars
		}
		return nil
	}
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, nil, now)
	found := false
	for _, c := range cands {
		if c.kind == "ifvg" && c.label == "iFVG(bear)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an iFVG candidate, got %+v", cands)
	}
}

func TestCollectWakeCandidatesSeatedInvalidation(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	// 15m bars: last closed bar closes 95.0 — far below a seated Demand 100.
	bars := wakeBars(15, now.UnixMilli(), [][4]float64{
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{99.0, 99.5, 94.5, 95.0}, // close 95 << 100 − noise
	})
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-24 * time.Hour),
		Doc: `{"bias":{"direction":"neutral"},"levels":[{"price":100,"label":"Demand·15m","grade":"B"},{"price":103,"label":"EQH·1h","grade":"B"}]}`}
	fetch := func(tf string, count int) []market.Kline {
		if tf == "15m" {
			return bars
		}
		return nil
	}
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, nil, now)
	found := false
	for _, c := range cands {
		if c.kind == "invalidation" {
			found = true
			if c.prio != wakePrioInvalidation || c.tier != "15m" {
				t.Fatalf("invalidation candidate = prio %d tier %q, want %d/15m", c.prio, c.tier, wakePrioInvalidation)
			}
		}
	}
	if !found {
		t.Fatalf("expected a seated-invalidation candidate, got %+v", cands)
	}

	// EQH must never invalidate (not a zone kind) — and with the single switch
	// off, nothing fires at all (W-KNOB-PRUNE).
	off := false
	cfg := &store.DayPlanConfig{WakeOnLevelEvents: &off}
	if cands := collectLevelWakeCandidates(cfg, fetch, "MNQ", row, nil, now); len(cands) != 0 {
		t.Fatalf("wake_on_level_events=false must suppress candidates, got %+v", cands)
	}
}

// TestWakeReadFailureKeepsActivePlan is the W6-C regression (2026-08-25 live
// bug): a wake re-read that fails every retry must NOT fail-close the session —
// the still-active plan keeps trading and no row is written.
func TestWakeReadFailureKeepsActivePlan(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	at.mcpClient = &errorDecisionClient{} // every planner call fails
	tradeDate := "2026-08-25"
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: "trader-1",
		TradeDate: tradeDate, Session: "NY", Lifecycle: "active",
		Doc: `{"bias":{"direction":"neutral"},"levels":[]}`,
	}); err != nil {
		t.Fatal(err)
	}
	performed := at.runPlannerReadWithTriggerClaimedCtx(time.Now(), "NY", tradeDate, "level_event", "level event: x", nil, false)
	if !performed {
		t.Fatalf("the wake read must run (claimed) even though it will fail")
	}
	row, _ := st.Plan().GetLatestPlanForSession(tradeDate, "NY")
	if row == nil || row.Version != 1 || row.Lifecycle != "active" {
		t.Fatalf("a failed wake read must write NOTHING — latest row = %+v", row)
	}
}

func TestMaybeWakePlannerOnLevelEventsThrottleDedupe(t *testing.T) {
	defer drainReReads(t) // T2: the first wake fires an async planner read; join before the seam resets.
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	bars := zonePattern15m(now.UnixMilli())
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		if tf == "15m" {
			return bars
		}
		return nil
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })

	on := true
	at := &AutoTrader{
		id:       "t1",
		exchange: "ninjatrader",
		store:    st,
		config: AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig:    &store.StrategyConfig{DayPlan: &store.DayPlanConfig{WakeOn15mZone: &on, WakeMinIntervalMin: 10}},
		},
	}
	row := &store.PlanDB{PlanID: "p1", Version: 1,
		CreatedAt: now.Add(-24 * time.Hour),
		Doc:       `{"bias":{"direction":"neutral"},"levels":[]}`}
	// CLOCK SEAM (2026-09-03): this test used to call the wall-clock entry
	// point with a fixture anchored at a fixed `now`. That was harmless until
	// the class-47 cutoffs began ENFORCING — the last-window cutoff asks how
	// many minutes remain to the session flat, so the test passed in the
	// morning and failed after ~14:20 CT. It states its own clock now.
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", "2026-08-25", row)
	if at.lastLevelWakeKey == "" || at.lastPlannerWakeAt.IsZero() {
		t.Fatalf("first wake must record key + clock (key=%q)", at.lastLevelWakeKey)
	}
	key1, t1 := at.lastLevelWakeKey, at.lastPlannerWakeAt
	// Same cycle: dedupe must hold the key and clock unchanged.
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", "2026-08-25", row)
	if at.lastLevelWakeKey != key1 || !at.lastPlannerWakeAt.Equal(t1) {
		t.Fatalf("dedupe must leave wake state unchanged (key %q→%q)", key1, at.lastLevelWakeKey)
	}
	// A DIFFERENT event inside the min-interval window must be throttled.
	at.lastLevelWakeKey = ""
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", "2026-08-25", row)
	if at.lastLevelWakeKey != "" {
		t.Fatalf("min-interval throttle must suppress the second wake inside the window, got key %q", at.lastLevelWakeKey)
	}
}

// TestMaybeWakePlannerFoldsOverlaySeatedLevel (WAVE 1a-plan P2) — the seated-
// level invalidation reads the plan through the ONE fold: an owner overlay
// adding a seated Demand level must be able to wake the planner. RED on the
// base-only reader: lastLevelWakeKey stays "" (no candidates). GREEN: the wake
// fires.
func TestMaybeWakePlannerFoldsOverlaySeatedLevel(t *testing.T) {
	defer drainReReads(t) // T2: a fired wake spawns an async planner read; join before the seam resets.
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, WakeMinIntervalMin: 10}})
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	// Skeptic F10: seam the trader clock to the fixture — the read's preflight
	// freshness (traderNow) must judge the fixture tape, not the wall clock
	// (30 days later, every fixture bar stale).
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	// 15m bars: last closed bar closes 95.0 — far below a seated Demand 100.
	bars := wakeBars(15, now.UnixMilli(), [][4]float64{
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{100.0, 101.0, 99.0, 100.0},
		{99.0, 99.5, 94.5, 95.0},
	})
	prev := market.FuturesBarsProvider
	// 1m serves the read's preflight freshness — feedNewestBarAge assumes 1m
	// spacing (newestStamp = last OpenTime + 60s), so the 1m tape must be
	// 1m-spaced and end at now, or the read refuses stale before it claims.
	oneMin := wakeBars(1, now.UnixMilli(), make([][4]float64, 130))
	for i := range oneMin {
		oneMin[i].Open, oneMin[i].High, oneMin[i].Low, oneMin[i].Close = 100, 100, 100, 100
	}
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		if tf == "15m" || tf == "5m" {
			return bars
		}
		if tf == "1m" {
			return oneMin
		}
		return nil
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })

	pid := "2026-08-25:NY"
	base := `{"reasoning":"wave-1a-p2-wake","bias":{"direction":"neutral"},"death_condition":"flat","levels":[],"scenarios":[{"id":"S1","condition":"reject","direction":"long","quality":"A"}]}`
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, StrategyID: at.id, TradeDate: "2026-08-25", Session: "NY", Lifecycle: "active", Doc: base, CreatedAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// The owner adds a seated Demand level at 100 through an overlay.
	if _, err := st.Plan().AppendOverlay(&store.PlanOverlayDB{PlanID: pid, PlanVersion: 1, OverlayID: "owner-add-demand", Origin: "owner",
		Patch: `[{"op":"add","path":"/levels/-","value":{"price":100,"label":"Demand·15m","grade":"B"}}]`}); err != nil {
		t.Fatal(err)
	}
	row, err := st.Plan().GetLatestPlanForTraderSession("2026-08-25", "NY", at.id)
	if err != nil || row == nil {
		t.Fatalf("plan row: %v", err)
	}
	// T2 join (CTO 2026-09-25, root cause of the 06:07 CI fail): the claim is a
	// TRANSIENT state — on the fast runner the read opened and released it
	// between two waitFor polls, and the observation failed on a read that DID
	// run (the CI log shows all three planner attempts completing in 0.0s).
	// Durable fix: the fake planner BLOCKS inside the claim until the test has
	// OBSERVED the claim open, so the observation cannot race the read; the
	// release then lets the read finish fail-closed and the deferred
	// drainReReads joins it.
	bp := &blockingPlannerClient{entered: make(chan struct{}, 1), release: make(chan struct{})}
	at.mcpClient = bp
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", "2026-08-25", row)
	if at.lastLevelWakeKey == "" {
		t.Fatal("the owner overlay's seated Demand level must wake the planner through the fold")
	}
	if !waitFor(t, 10*time.Second, func() bool {
		_, open := anyPlannerStreamOpen()
		return open
	}) {
		t.Fatal("the fired wake's read never started — no planner stream claim appeared (a wall-clock gate refused it: check session/clock/seam)")
	}
	close(bp.release) // the claim is observed open; let the read run to its fail-closed end
}

// blockingPlannerClient (CTO 2026-09-25) is the wake test's planner: it BLOCKS
// inside the claimed read until released, so the claim stays open while the
// test observes it. It returns no JSON (fail-closed), exactly like the CI
// runner's fake.
type blockingPlannerClient struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingPlannerClient) SetAPIKey(string, string, string) {}
func (b *blockingPlannerClient) SetTimeout(time.Duration)         {}
func (b *blockingPlannerClient) ResolvedModel() string            { return "test-model" }
func (b *blockingPlannerClient) CallWithMessages(_, _ string) (string, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return "", nil
}
func (b *blockingPlannerClient) CallWithRequest(*mcp.Request) (string, error) { return "", nil }
func (b *blockingPlannerClient) CallWithRequestStream(*mcp.Request, func(string)) (string, error) {
	return "", nil
}
func (b *blockingPlannerClient) CallWithRequestFull(*mcp.Request) (*mcp.LLMResponse, error) {
	return nil, nil
}

// TestCollectWakeCandidatesStaleZones (W26 planner-stale-zones) — the stale-zones
// candidate fires exactly when EVERY armed market_in_zone leg is farther than
// planner_stale_zones_max_atr × ATR5m from the last closed 1m close, and OFF (nil)
// emits nothing — today's behaviour byte-identical.
func TestCollectWakeCandidatesStaleZones(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	row := &store.PlanDB{PlanID: "p1", Version: 1, CreatedAt: now.Add(-24 * time.Hour)}
	// 1m bars: last close = 100. 5m bars: range 2 → ATR14 ≈ 2.0.
	ones := wakeBars(1, now.UnixMilli(), [][4]float64{{100, 101, 99, 100}, {100, 101, 99, 100}, {100, 101, 99, 100}})
	fiveRows := make([][4]float64, 20)
	for i := range fiveRows {
		fiveRows[i] = [4]float64{100, 101, 99, 100}
	}
	fives := wakeBars(5, now.UnixMilli(), fiveRows)
	fetch := func(tf string, count int) []market.Kline {
		switch tf {
		case "1m":
			return ones
		case "5m":
			return fives
		}
		return nil
	}
	resolved := &kernel.PlanDoc{Scenarios: []kernel.PlanScenario{{
		ID:  "S1",
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 110, Stop: 108, Target: 114, Policy: kernel.EntryPolicyMarketInZone},
	}}}
	// OFF (nil) = today's behaviour: no stale-zones candidate.
	if cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, resolved, now); len(cands) != 0 {
		t.Fatalf("OFF must emit no stale-zones candidate, got %+v", cands)
	}
	// A threshold above the arm's distance (10 pts ≈ 5×ATR) does not fire.
	hi := 6.0
	if cands := collectLevelWakeCandidates(&store.DayPlanConfig{PlannerStaleZonesMaxATR: &hi}, fetch, "MNQ", row, resolved, now); len(cands) != 0 {
		t.Fatalf("6×ATR threshold must not fire for a ~5×ATR arm, got %+v", cands)
	}
	// A threshold below the arm's distance fires one stale_zones candidate.
	lo := 3.0
	cands := collectLevelWakeCandidates(&store.DayPlanConfig{PlannerStaleZonesMaxATR: &lo}, fetch, "MNQ", row, resolved, now)
	if len(cands) != 1 || cands[0].kind != "stale_zones" {
		t.Fatalf("3×ATR threshold must emit one stale_zones candidate, got %+v", cands)
	}
	if cands[0].prio != wakePrioStaleZones {
		t.Fatalf("stale-zones prio = %d, want %d", cands[0].prio, wakePrioStaleZones)
	}
	// A split arm with BOTH legs far still fires; one near leg does not.
	split := &kernel.PlanDoc{Scenarios: []kernel.PlanScenario{{
		ID: "S1",
		Arm: &kernel.PlanArmSpec{Enabled: true, Policy: kernel.EntryPolicyMarketInZone, Legs: []kernel.PlanArmLeg{
			{Entry: 110, Stop: 108, Target: 114},
			{Entry: 112, Stop: 110, Target: 116},
		}},
	}}}
	if cands := collectLevelWakeCandidates(&store.DayPlanConfig{PlannerStaleZonesMaxATR: &lo}, fetch, "MNQ", row, split, now); len(cands) != 1 {
		t.Fatalf("split arm with both legs far must emit one candidate, got %+v", cands)
	}
	splitNear := &kernel.PlanDoc{Scenarios: []kernel.PlanScenario{{
		ID: "S1",
		Arm: &kernel.PlanArmSpec{Enabled: true, Policy: kernel.EntryPolicyMarketInZone, Legs: []kernel.PlanArmLeg{
			{Entry: 110, Stop: 108, Target: 114},
			{Entry: 102, Stop: 100, Target: 106},
		}},
	}}}
	if cands := collectLevelWakeCandidates(&store.DayPlanConfig{PlannerStaleZonesMaxATR: &lo}, fetch, "MNQ", row, splitNear, now); len(cands) != 0 {
		t.Fatalf("split arm with one near leg must not fire, got %+v", cands)
	}
}

// TestWakeDeferredByStreamRefiresOnClose — W26 F-1: a wake deferred by "stream
// open" re-fires the moment the stream closes (instead of waiting for the next
// monitor cycle). The 10-14 wake sat parked behind another trader's 2h stream.
func TestWakeDeferredByStreamRefiresOnClose(t *testing.T) {
	defer drainReReads(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, WakeMinIntervalMin: 10}})
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, kernel.CTLocation())
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	// 15m bars with a last close 95 — below a seated Demand 100.
	bars := wakeBars(15, now.UnixMilli(), [][4]float64{
		{100, 101, 99, 100}, {100, 101, 99, 100}, {100, 101, 99, 100},
		{100, 101, 99, 100}, {100, 101, 99, 100}, {100, 101, 99, 100},
		{99, 99.5, 94.5, 95},
	})
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		if tf == "15m" || tf == "5m" {
			return bars
		}
		return nil
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	pid := "2026-08-25:ASIA"
	base := `{"bias":{"direction":"neutral"},"levels":[{"price":100,"label":"Demand·15m","grade":"B"}],"scenarios":[{"id":"S1","condition":"reject","direction":"long","quality":"A"}]}`
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, StrategyID: at.id, TradeDate: "2026-08-25", Session: "ASIA", Lifecycle: "active", Doc: base, CreatedAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	row, err := st.Plan().GetLatestPlanForTraderSession("2026-08-25", "ASIA", at.id)
	if err != nil || row == nil {
		t.Fatalf("plan row: %v", err)
	}
	// Open ANOTHER trader's stream — the wake must defer and register a re-fire.
	claimPlannerRead("other:2026-08-25:ASIA")
	at.maybeWakePlannerOnLevelEventsAt(now, "ASIA", "2026-08-25", row)
	if at.lastLevelWakeKey != "" {
		t.Fatalf("wake must defer while a stream is open, got key %q", at.lastLevelWakeKey)
	}
	// Close the stream — the deferred wake re-fires (async).
	releasePlannerRead("other:2026-08-25:ASIA")
	deadline := time.Now().Add(2 * time.Second)
	for at.lastLevelWakeKey == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if at.lastLevelWakeKey == "" {
		t.Fatal("the deferred wake must re-fire once the stream closes")
	}
}
