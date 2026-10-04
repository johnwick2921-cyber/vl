package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// ── item 22 (invalid levels reset) — the course scopes "invalid" (ISB-only)
// to the touching candle / the session day, never the line for good
// [D5.2 p2 @20:48]. A MOVING line (EMA34 / EMA34HTF / trigger retest) clears
// its ISB-only state on a drift or a visit departure; a KEY level keeps it
// until the session day rolls or a closed 1H body deletes the level. ──────────

func TestIsMovingLineKey(t *testing.T) {
	for _, k := range []string{string(KindEMA34), string(KindEMA34HTF), string(KindTriggerRetest)} {
		if !isMovingLineKey(k) {
			t.Fatalf("%q must be a moving line", k)
		}
	}
	for _, k := range []string{"key_level:100:1", "PDH", "k1"} {
		if isMovingLineKey(k) {
			t.Fatalf("%q must NOT be a moving line", k)
		}
	}
}

// seedEmptySeeded builds a seeded evaluator with no key levels so the ONLY level
// is the trigger retest (or whatever the test appends).
func seedEmptySeeded() *Evaluator {
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = nil
	return e
}

func triggerRetestFixture() *Evaluator {
	e := seedEmptySeeded()
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 100}
	return e
}

// The trigger-retest line (a MOVING line) goes ISB-only after a wrong-way close
// at the support, then a later candle that does not touch the line ends the
// visit — the ISB-only state must clear (the course scopes "invalid" to the
// touching candle [D5.2 p2 @20:48]).
func TestMovingLineISBOnlyResetsOnDeparture(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2), // prev close above the line: support
		rthBars(1, 101, 101.2, 98, 98.5),     // touches 100, closes BELOW → wrong way
	}
	now := bars[1].CloseTime + 1
	e.Tick(bars, now)
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("precondition: the trigger retest must go ISB-only after the wrong-way close; state=%v", e.State.ISBOnly)
	}
	// Departure: a candle entirely below the band — it does not touch the line.
	bars = append(bars, rthBars(2, 95, 95.5, 93, 94))
	e.Tick(bars, bars[2].CloseTime+1)
	if e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("a moving line's ISB-only state must clear on visit departure; still set: %v", e.State.ISBOnly)
	}
}

// The trigger-retest line drifts to a new price (the trigger re-based): the old
// wrong-way classification is about a candle at the OLD price — the ISB-only
// state must clear with the drift.
func TestMovingLineISBOnlyResetsOnDrift(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 98, 98.5), // wrong-way close → ISB-only
	}
	now := bars[1].CloseTime + 1
	e.Tick(bars, now)
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("precondition: ISB-only must be set; state=%v", e.State.ISBOnly)
	}
	// The line re-bases to 110; the next tick sees the drift and clears.
	e.State.Trigger.Price = 110
	bars = append(bars, rthBars(2, 98, 99, 97.5, 98.5))
	e.Tick(bars, bars[2].CloseTime+1)
	if e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("a moving line's ISB-only state must clear when the line drifts; still set: %v", e.State.ISBOnly)
	}
}

// A KEY level's ISB-only state resets when the trading day rolls, while the
// moving lines keep theirs (they reset on drift/departure instead).
func TestKeyLevelISBOnlyResetsOnNewSessionDay(t *testing.T) {
	e := seedEmptySeeded()
	// Stale visit-day: the per-day reset fires on the first tick.
	e.State.Day.Key = "2026-09-15"
	e.State.VisitsDay = "2026-09-14"
	e.State.ISBOnly = map[string]bool{
		"key_level:100:1":         true,
		string(KindEMA34):         true,
		string(KindTriggerRetest): true,
	}
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2),
		rthBars(1, 101, 101.2, 100.4, 101.1),
	}
	now := bars[1].CloseTime + 1
	if got := tradingDayKey(time.UnixMilli(now).In(ctime())); got != "2026-09-15" {
		t.Fatalf("fixture: tick day key = %q, want 2026-09-15", got)
	}
	e.Tick(bars, now)
	if e.State.ISBOnly["key_level:100:1"] {
		t.Fatalf("a key level's ISB-only state must reset on a new session day; still set: %v", e.State.ISBOnly)
	}
	if !e.State.ISBOnly[string(KindEMA34)] || !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("moving lines must NOT reset with the session day (they reset on drift/departure): %v", e.State.ISBOnly)
	}
}
