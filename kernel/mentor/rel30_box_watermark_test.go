package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// rel30Cfg is the production evaluator config with the non-box machinery turned
// down so the synthetic tapes exercise only the target path.
func rel30Cfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.OrbGateEnabled = false
	cfg.LocTriggerFilter = false
	return cfg
}

// TestBoxReturnConsumedWhenDayGateRefuses — release #30. A box return refused by
// the day gate (box_day_off) is CONSUMED: the BoxRefs watermark advances, the
// refusal counts exactly once, and the stale return is NOT re-walked as a fresh
// entry when the day lifts. RED on today's code: the watermark advanced AFTER
// the day-gate check, so the same return re-fired box_day_off every tick (10-15:
// 798 refusals from 24 returns) and was walked as fresh once the ban lifted.
func TestBoxReturnConsumedWhenDayGateRefuses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false // m410: this fixture pins the 1m box geometry (boxes now draw on the traded 2m frame after 09:00)
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// FTGL [95, 96] (extreme low 95 @2, partner low 95.5 @5), one reject return
	// @7 (high 97.1 touches the top, closes above) — the ONE tradable return.
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96), // bottom 1: extreme low 95
		mk(3, 96.5, 97, 96, 96.8),
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),   // bottom 2: later confirmed higher low
		mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2 → FTGL [95, 96]
		mk(7, 96.5, 97.1, 95.9, 97), // the ONE reject return
	}
	boxKey := "ftgl:96.00:95.00"
	now := bars[7].OpenTime + 59_999
	newE := func() *Evaluator {
		e := New(cfg)
		// Seed a far target so the box entry has a target ladder (non-vacuous).
		e.seeded = true
		e.State.Seed1mWatermark = maxInt64
		e.State.Seed1HWatermark = maxInt64
		e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
		return e
	}

	// Control: with a normal day the return WOULD emit a box entry — the tape
	// is a real tradable return, so the day-off pin is not vacuous.
	ctl := newE()
	ctl.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	ctlOut := ctl.Tick(bars[:8], now)
	boxEntries := 0
	for _, in := range ctlOut {
		if strings.HasPrefix(in.Reason, "box edge return") {
			boxEntries++
		}
	}
	if boxEntries != 1 {
		t.Fatalf("control: the day-trade run emitted %d box entries, want 1 (the return must be tradable)", boxEntries)
	}

	// Main: day OFF refuses the return ONCE, the watermark advances, and the
	// stale return is not re-walked after the ban lifts.
	e := newE()
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayOff}
	e.Tick(bars[:8], now)
	if got := e.State.Refusals["box_day_off"]; got != 1 {
		t.Fatalf("tick 1: box_day_off = %d, want 1", got)
	}
	if got := e.State.BoxRefs[boxKey]; got != bars[7].OpenTime {
		t.Fatalf("tick 1: watermark did not advance — BoxRefs[%s] = %d, want %d", boxKey, got, bars[7].OpenTime)
	}
	// Same bars re-ticked while the day stays off: the return is consumed.
	e.Tick(bars[:8], now)
	if got := e.State.Refusals["box_day_off"]; got != 1 {
		t.Fatalf("re-tick: box_day_off = %d, want still 1 (the return is consumed once)", got)
	}
	// Lift the day: the stale return must NOT be walked as a fresh entry.
	e.State.Day.Verdict = DayTrade
	out := e.Tick(bars[:8], now)
	for _, in := range out {
		if strings.HasPrefix(in.Reason, "box edge return") {
			t.Fatalf("the stale day-off-refused return was traded after the ban lifted: %+v", in)
		}
	}
}

// TestPhlTfConflictCountsEveryRejectedLevel — release #30: during a standing
// 15m/5m conflict phl_tf_conflict counts EVERY rejected level in the tick, not
// only the first (the old `break` counted 1). Mutant: re-insert the `break`
// after the first `e.refuse("phl_tf_conflict")` → the counter stays 1 → RED.
func TestPhlTfConflictCountsEveryRejectedLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.Exec2mAfter30m = false // the conflict counter pin reads the 1m touch
	cfg.LocTriggerFilter = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// Three key levels (99.5 / 100.2 / 101.0) all touched and rejected by the
	// same candle (bar 5: low 99.5, close 102.9 above all three).
	bars := []market.Kline{
		mk(0, 101, 101.5, 100.8, 101.3),
		mk(1, 101, 101.4, 99.7, 100.5),
		mk(2, 99, 115, 98, 114.8),
		mk(3, 102, 118, 100.2, 117.8),
		mk(4, 102, 102.4, 100.2, 102.2),
		mk(5, 102, 103, 99.5, 102.9),
	}
	now := bars[5].OpenTime + 59_999
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = maxInt64
	e.State.Seed1HWatermark = maxInt64
	e.State.SeedLevels = []Level{
		{Key: "L1", Kind: KindKeyLevel, Price: 99.5},
		{Key: "L2", Kind: KindKeyLevel, Price: 100.2},
		{Key: "L3", Kind: KindKeyLevel, Price: 101.0},
	}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	// A standing conflict (5m box short vs 15m box long): both boxes sit ABOVE
	// the close, so their inside-bans do not fire — the conflict is the ONLY
	// suppressor of the level path.
	e.State.ISBBox = &ISBBox{High: 110, Low: 102.9, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 102.9, Dir: SideLong, AtTime: bars[0].OpenTime}
	e.Tick(bars[:6], now)
	if got := e.State.Refusals["phl_tf_conflict"]; got != 3 {
		t.Fatalf("phl_tf_conflict = %d, want 3 (one per rejected level)", got)
	}
}

// TestIsbrevCountersOnlyRealReverseCandidates — release #30: isbrev_tf_conflict
// and isbrev_day_off count only a REAL reverse-ISB candidate (an ISB at the EMA
// 9 pointing against the trend), not every ISB. This tape's last pair is an ISB
// whose price never reaches the EMA 9 (ema ≈ 208.5 vs the ISB wick 99.5–104.5),
// so it is NOT a reverse candidate. Mutant: re-gate on IsISB instead of
// revCandidate → isbrev_tf_conflict becomes 1 → RED.
func TestIsbrevCountersOnlyRealReverseCandidates(t *testing.T) {
	cfg := rel30Cfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// Pads close ~200 so the EMA 9 sits far above; the last pair is an ISB
	// (trend 106→100, inside bar high 104.5 / low 99.5).
	bars := []market.Kline{
		mk(0, 200, 201, 199, 200.5),
		mk(1, 200, 201, 199, 200.5),
		mk(2, 200, 201, 199, 200.5),
		mk(3, 106, 106.5, 99.5, 100),
		mk(4, 102, 104.5, 99.5, 104),
	}
	now := bars[4].OpenTime + 59_999
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))

	// (1) conflict on: a non-reverse ISB must NOT count in isbrev_tf_conflict.
	e := New(cfg)
	e.State.Day = DayLatch{Key: day, Verdict: DayTrade}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 95}
	// The conflict boxes sit AROUND the close (Low 102 ≤ close 104 ≤ High 110)
	// so neither escapes at the current candle and the conflict holds.
	e.State.ISBBox = &ISBBox{High: 110, Low: 102, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 102, Dir: SideLong, AtTime: bars[0].OpenTime}
	e.Tick(bars[:5], now)
	if got := e.State.Refusals["isbrev_tf_conflict"]; got != 0 {
		t.Fatalf("isbrev_tf_conflict = %d, want 0 (the ISB is not a reverse candidate)", got)
	}

	// (2) day off, no conflict: a non-reverse ISB must NOT count in
	// isbrev_day_off either.
	e2 := New(cfg)
	e2.State.Day = DayLatch{Key: day, Verdict: DayOff}
	e2.State.Trigger = TriggerLine{Dir: SideLong, Price: 95}
	e2.Tick(bars[:5], now)
	if got := e2.State.Refusals["isbrev_day_off"]; got != 0 {
		t.Fatalf("isbrev_day_off = %d, want 0 (the ISB is not a reverse candidate)", got)
	}
}
