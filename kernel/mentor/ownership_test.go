package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// allowedSetups is the A7 set: every intraday emit site tags its entry.
func allowedSetups() map[string]bool {
	return map[string]bool{"ISB": true, "PHL": true, "PLH": true, "BOX": true, "SWING4H": true}
}

// frozenDayLatch presets a FROZEN day verdict (the §7 latch survives only when
// the key matches the trading day — a bare Verdict preset is recomputed live).
func frozenDayLatch(anchorMs int64, v DayVerdict) DayLatch {
	return DayLatch{Key: tradingDayKey(time.UnixMilli(anchorMs).In(ctime())), Verdict: v}
}

// TestTickStampsSpentDay — A5 (CTO 1791041016051, DS-102 patch): with the day
// latched DaySpent, EVERY intent Tick emits carries SpentDay (the trader's
// spent_day tier 2 and the R9 15-pt stop cap hang off it). The recorded RTH
// tape cannot reproduce a 300-pt Globex run, so the latch is preset per tick
// (frozen by its key); the run covers the full day, so whichever emit path
// produced the first intent — including the ISB missing-target early return —
// is checked.
func TestTickStampsSpentDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	loc := ctime()
	saw := 0
	for i := 2; i <= len(bars); i++ {
		now := bars[i-1].OpenTime + 59_999
		e.State.Day = frozenDayLatch(now, DaySpent)
		for _, in := range e.Tick(bars[:i], now) {
			saw++
			if !in.SpentDay {
				t.Fatalf("bar %d (%s): intent %s emitted without the spent-day stamp",
					i, time.UnixMilli(now).In(loc).Format("15:04"), in.Action)
			}
		}
	}
	if saw == 0 {
		t.Fatal("the 09-15 tape emitted no intents under a DaySpent latch — the call-site test proves nothing")
	}
}

// TestRecordedDaysCarrySetupAndGeometry — A7 (CTO 20:13:25Z item 6): over every
// recorded fixture day, every entry intent has Setup in the allowed set, and
// StopPts/TargetPts equal the geometry — and the untagged_setup counter stays
// 0 (no emit site forgets its tag). A frozen DayTrade latch stands in for the
// live measured day (an RTH-only tape is a mid-window feed → DayNotMeasured →
// zero entries, which would prove nothing).
func TestRecordedDaysCarrySetupAndGeometry(t *testing.T) {
	fixtures := []string{"mnq_1m_2026-09-15_rth", "mnq_1m_2026-09-16_rth", "mnq_1m_2026-08-28_rth", "mnq_1m_2026-09-13_boxframe"}
	totalEntries := 0
	for _, fix := range fixtures {
		bars := loadFixture(t, fix, "1m")
		cfg := DefaultConfig()
		cfg.Enabled = true
		e := New(cfg)
		// The ORB gate is default-ON: preset a drawn + escaped opening range so
		// the recorded days can emit (the synthetic fixtures do the same). Days
		// where the real 4h/1h directions refuse every ISB still exercise the
		// stamp (their entries simply never form) — the invariant is checked
		// across the whole set.
		e.State.ORB = ORB{Day: dayStartCT(bars[0].OpenTime), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		for i := 2; i <= len(bars); i++ {
			now := bars[i-1].OpenTime + 59_999
			e.State.Day = frozenDayLatch(now, DayTrade)
			for _, in := range e.Tick(bars[:i], now) {
				if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
					continue
				}
				totalEntries++
				if !allowedSetups()[in.Setup] {
					t.Fatalf("%s: entry setup %q not in the allowed set", fix, in.Setup)
				}
				if in.StopPts != abs(in.Price-in.Stop) {
					t.Fatalf("%s: StopPts %.4f != |%.4f - %.4f|", fix, in.StopPts, in.Price, in.Stop)
				}
				if in.TargetPts != abs(in.Target-in.Price) {
					t.Fatalf("%s: TargetPts %.4f != |%.4f - %.4f|", fix, in.TargetPts, in.Target, in.Price)
				}
				if in.SpentDay {
					t.Fatalf("%s: entry stamped spent on a DayTrade latch", fix)
				}
			}
		}
		if e.State.Refusals["untagged_setup"] != 0 {
			t.Fatalf("%s: untagged_setup = %d — an emit site forgot its Setup tag", fix, e.State.Refusals["untagged_setup"])
		}
	}
	if totalEntries == 0 {
		t.Fatal("no entry intents across the recorded fixtures — the invariant proves nothing")
	}
}

// TestDayGateBlocksEveryIntradaySetup — A10 (CTO 20:15:49Z): one fixture day
// run with the latch forced DayNotMeasured and then DayOff: zero intraday
// entry intents, and the refusal counters name each blocked setup. The ISB
// fixture arms on a tradeable day, so the blocked runs prove the gate.
func TestDayGateBlocksEveryIntradaySetup(t *testing.T) {
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, 9, 0, 0) + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 121, 121.5, 120.5, 120),
		mk(1, 98, 106, 97, 105),  // mother (green)
		mk(2, 103, 104, 99, 100), // ISB candle 1
	}
	now := bars[2].CloseTime + 1
	for _, tc := range []struct {
		v        DayVerdict
		wantKey  string
		notThere string
	}{
		{DayNotMeasured, "isb_day_not_measured", "isb_day_off"},
		{DayOff, "isb_day_off", "isb_day_not_measured"},
	} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		e := New(cfg)
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.Day = frozenDayLatch(now, tc.v)
		ins := e.Tick(bars, now)
		for _, in := range ins {
			if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
				t.Fatalf("verdict %v emitted an intraday entry: %+v", tc.v, ins)
			}
		}
		if e.State.Refusals[tc.wantKey] == 0 {
			t.Fatalf("verdict %v: ledger = %v, want %s counted", tc.v, e.State.Refusals, tc.wantKey)
		}
		if e.State.Refusals[tc.notThere] != 0 {
			t.Fatalf("verdict %v: ledger = %v, %s must not fire", tc.v, e.State.Refusals, tc.notThere)
		}
	}
}
