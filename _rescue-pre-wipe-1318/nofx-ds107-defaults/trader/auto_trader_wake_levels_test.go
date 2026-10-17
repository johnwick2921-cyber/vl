package trader

import (
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
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
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, now)
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
	if cands := collectLevelWakeCandidates(cfg, fetch, "MNQ", row, now); len(cands) != 0 {
		t.Fatalf("wake_on_level_events=false must suppress 15m candidates, got %+v", cands)
	}
	// Legacy mapping: a lone stored wake_on_15m_zone=false no longer suppresses
	// (any legacy switch ON — nil reads ON — maps the single switch ON).
	legacy := &store.DayPlanConfig{WakeOn15mZone: &off}
	if cands := collectLevelWakeCandidates(legacy, fetch, "MNQ", row, now); len(cands) != 1 {
		t.Fatalf("legacy lone wake_on_15m_zone=false maps to ON, want 1 candidate, got %+v", cands)
	}
	// Legacy mapping: all five false maps OFF.
	allOff := &store.DayPlanConfig{WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off}
	if cands := collectLevelWakeCandidates(allOff, fetch, "MNQ", row, now); len(cands) != 0 {
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
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, now)
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
	cands = collectLevelWakeCandidates(cfg, fetch, "MNQ", row, now)
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
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, now)
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
	cands := collectLevelWakeCandidates(nil, fetch, "MNQ", row, now)
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
	if cands := collectLevelWakeCandidates(cfg, fetch, "MNQ", row, now); len(cands) != 0 {
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
	performed := at.runPlannerReadWithTriggerClaimedCtx("NY", tradeDate, "level_event", "level event: x", nil, false)
	if !performed {
		t.Fatalf("the wake read must run (claimed) even though it will fail")
	}
	row, _ := st.Plan().GetLatestPlanForSession(tradeDate, "NY")
	if row == nil || row.Version != 1 || row.Lifecycle != "active" {
		t.Fatalf("a failed wake read must write NOTHING — latest row = %+v", row)
	}
}

func TestMaybeWakePlannerOnLevelEventsThrottleDedupe(t *testing.T) {
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
