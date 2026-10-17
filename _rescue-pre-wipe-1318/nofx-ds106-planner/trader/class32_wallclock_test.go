package trader

import (
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// CLASS 32 (owner ruling 2026-08-31) — scheduled reads must fire on wall-clock.
// Tonight's evidence: every cycle 16:26→16:38 logged cycle_skip=no_new_data
// because CME is halted 16:00-17:00, so the 16:30 ASIA read was skipped WITH
// the data work and fired ~17:00:03 — 30 minutes late, no error, no alarm, no
// plan at the open.
//
// The fixtures below pin the fix at the tick level: the session read is
// evaluated BEFORE the data-gated skips, fires exactly once, authors from the
// last stored bars, and the data pipeline still idles when the tape is frozen.

// class32FrozenBars returns a byte-identical bar slice on every call — the
// no-new-data signature never changes, exactly like the CME halt tape. Bars are
// anchored near `now` so the planner preflight sees fresh data.
func class32FrozenBars(now time.Time) func(string, string, int) []market.Kline {
	base := now.Add(-390 * time.Minute).UnixMilli()
	bars := make([]market.Kline, 0, 390)
	for i := 0; i < 390; i++ {
		o := base + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: o, High: 15650 + float64(i%10), Low: 15550 + float64(i%10), Close: 15600 + float64(i%10), CloseTime: o + 59_000})
	}
	return func(string, string, int) []market.Kline { return bars }
}

// These session/data-cycle fixtures start with the weekly read already stored.
// Otherwise tickOnce also spawns an unrelated weekly backfill worker that can
// outlive the test and race restoration of the global bar provider.
func class32ClockTrader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	at, st := asiaClockTrader(t)
	monday := "2026-08-17"
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID:     st.Plan().ResolvePlanID(monday, "WEEKLY", at.id),
		StrategyID: at.id, TradeDate: monday, Session: "WEEKLY",
		TriggerReason: "test_weekly", Lifecycle: "active", Doc: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	return at, st
}

// 6.1 REGRESSION PIN — the whole wave. On the pre-class-32 code tickOnce
// returned at the no-new-data skip before the read was ever evaluated, so this
// test FAILS there (waitPlan gets nil) and passes on the fix.
func TestClass32AsiaReadFiresAt1630WithFrozenBars(t *testing.T) {
	at, st := class32ClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 8, 18, 16, 30) // Tuesday, inside the 16:00-17:00 halt
	market.FuturesBarsProvider = class32FrozenBars(now)

	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })

	if kernel.IsCMEOpen(now) {
		t.Fatal("fixture: 16:30 must be inside the CME maintenance halt (IsCMEOpen=false)")
	}
	// Prime the no-new-data signature: the first sighting runs, the identical
	// second sighting skips (the exact state during the halt).
	at.skipNoNewData(now)
	if !at.skipNoNewData(now) {
		t.Fatal("fixture: frozen bars must produce the no-new-data skip")
	}

	at.tickOnce(false) // 16:30 — old code returns at the skip; the fix fires the read first

	row := waitPlan(t, st, "2026-08-18", "ASIA", "t1")
	if row == nil {
		t.Fatal("ASIA read did NOT fire at 16:30 with no new bars (class 32 regression)")
	}
	if row.Lifecycle != "active" {
		t.Fatalf("plan lifecycle = %q, want active", row.Lifecycle)
	}
}

// 6.4 IDEMPOTENCE — with no new bars for 20 minutes after the scheduled time,
// the read fires ONCE, not on every tick. The plan-store dedupe + the
// in-flight claim ("already in flight — skipping duplicate call") make any
// second tick a no-op.
func TestClass32ReadFiresOnceWithFrozenTape(t *testing.T) {
	at, st := class32ClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 8, 18, 16, 30)
	market.FuturesBarsProvider = class32FrozenBars(now)

	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })

	at.skipNoNewData(now)
	at.tickOnce(false)
	if row := waitPlan(t, st, "2026-08-18", "ASIA", "t1"); row == nil {
		t.Fatal("the first 16:30 tick must fire the ASIA read")
	}

	// 16:31 → 16:50, still frozen, still inside the read window.
	for _, mm := range []int{31, 33, 36, 40, 50} {
		testNow = func() time.Time { return ctTime(t, 2026, 8, 18, 16, mm) }
		at.tickOnce(false)
	}
	time.Sleep(500 * time.Millisecond) // let any erroneous duplicate surface

	rows, err := st.Plan().ListVersionsForTrader("2026-08-18", "ASIA", "t1")
	if err != nil {
		t.Fatal(err)
	}
	scheduled := 0
	for _, r := range rows {
		if r.TriggerReason == "ASIA_scheduled_read" {
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatalf("the scheduled ASIA read must fire exactly ONCE, got %d (rows=%d)", scheduled, len(rows))
	}
}

// 6.5 — the data-skip path still skips DATA work when no new bars arrive (no
// busy-looping the bar pipeline). At 15:00 no read window is active, the tape
// is frozen, so tickOnce must not enter runCycle at all.
func TestClass32FrozenTapeStillSkipsDataWork(t *testing.T) {
	at, _ := class32ClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 8, 18, 15, 0) // CME open, no session read window active
	market.FuturesBarsProvider = class32FrozenBars(now)

	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })

	at.skipNoNewData(now)
	at.tickOnce(false)
	if at.callCount != 0 {
		t.Fatalf("frozen tape must skip the data cycle, runCycle ran %d time(s)", at.callCount)
	}
}

// 6.3 — the halt-fired line: content and age math, with and without stored bars.
func TestClass32HaltReadLogLine(t *testing.T) {
	now := ctTime(t, 2026, 8, 18, 16, 30)
	line := haltSessionReadLine("ASIA", "5m", now, "2026-08-18 16:00:00 CT", 30, true)
	for _, want := range []string{
		"session read fired during halt",
		"(ASIA)",
		"authoring from last stored bars",
		"newest 5m 2026-08-18 16:00:00 CT",
		"age 30m",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("halt line %q missing %q", line, want)
		}
	}
	empty := haltSessionReadLine("ASIA", "5m", now, "", 0, false)
	if !strings.Contains(empty, "NO stored bars") {
		t.Errorf("empty-cache line %q must say NO stored bars", empty)
	}
}

// 4.3 helper — newestStoredBarInfo resolves the newest stored bar and its age.
func TestClass32NewestStoredBarInfo(t *testing.T) {
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 8, 18, 16, 30)
	market.FuturesBarsProvider = class32FrozenBars(now)

	newestCT, ageMin, have := newestStoredBarInfo("MNQ", "5m", now)
	if !have {
		t.Fatal("frozen provider must yield a newest bar")
	}
	if newestCT == "" || ageMin <= 0 {
		t.Fatalf("newestCT=%q ageMin=%d — want non-empty and positive", newestCT, ageMin)
	}
	market.FuturesBarsProvider = nil
	if _, _, have := newestStoredBarInfo("MNQ", "5m", now); have {
		t.Fatal("nil provider must yield have=false")
	}
}
