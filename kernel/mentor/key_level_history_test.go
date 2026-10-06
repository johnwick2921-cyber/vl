package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// KEYLEVEL-FULL-HISTORY (release #10) tests — post REL10-438-FIXES. Fixtures
// are real-UTC epoch ms through America/Chicago (EPOCH RULING 2026-10-03).

// rthHour1m builds the 60 1m bars of one RTH hour (hh:30 anchor) with constant
// open/close — a GREEN candle when c>o, RED when c<o.
func rthHour1m(y int, mo time.Month, d, hh int, o, c float64) []market.Kline {
	start := auditMs(y, mo, d, hh, 30, 0)
	bars := make([]market.Kline, 60)
	for i := 0; i < 60; i++ {
		ot := start + int64(i)*60_000
		hi, lo := o, c
		if c < o {
			hi, lo = o, c
		}
		bars[i] = market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: lo, Close: c}
	}
	return bars
}

// native1h builds one whole-hour-aligned native 1h bar (the store's 1h is
// epoch-hour aligned, NOT 08:30 RTH — KEY DB FINDING) for the roll-gap input.
func native1h(y int, mo time.Month, d, hh int, o, c float64) market.Kline {
	ot := auditMs(y, mo, d, hh, 0, 0)
	hi, lo := o, c
	if c < o {
		hi, lo = o, c
	}
	return market.Kline{OpenTime: ot, CloseTime: ot + 3600_000 - 1, Open: o, High: hi, Low: lo, Close: c}
}

// native1hSession builds one native 1h bar per hour across a Globex session
// (23 hours, 17:00 CT → next day 16:00 CT), so a session has 23 overlapping
// native 1h bars.
func native1hSession(y int, mo time.Month, d, hh int, o, c float64) []market.Kline {
	return native1hN(y, mo, d, hh, 23, o, c)
}

// native1hN builds n hourly native 1h bars starting at (y,mo,d) hh:00 CT.
func native1hN(y int, mo time.Month, d, hh, n int, o, c float64) []market.Kline {
	out := make([]market.Kline, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, native1h(y, mo, d, hh+i, o, c))
	}
	return out
}

// denseSession1m builds n consecutive 1m bars starting at (y,mo,d) 17:00 CT
// (the Globex session open). A full session is 23*60 = 1380 bars; sessionKey
// of those bars is (y,mo,d+1).
func denseSession1m(y int, mo time.Month, d int, n int, o, c float64) []market.Kline {
	start := auditMs(y, mo, d, 17, 0, 0)
	bars := make([]market.Kline, n)
	for i := 0; i < n; i++ {
		ot := start + int64(i)*60_000
		hi, lo := o, c
		if c < o {
			hi, lo = o, c
		}
		bars[i] = market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: lo, Close: c}
	}
	return bars
}

// rthRange1m builds consecutive 1m bars from (y,mo,d) hh:mm:00 CT to
// (y,mo,d) toHH:toMM:00 CT (exclusive), constant OHLC.
func rthRange1m(y int, mo time.Month, d, fromHH, fromMM, toHH, toMM int, o, c float64) []market.Kline {
	start := auditMs(y, mo, d, fromHH, fromMM, 0)
	end := auditMs(y, mo, d, toHH, toMM, 0)
	var bars []market.Kline
	for ot := start; ot < end; ot += 60_000 {
		hi, lo := o, c
		if c < o {
			hi, lo = o, c
		}
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: lo, Close: c})
	}
	return bars
}

// TestRollDayKeyFindsFirstDenseSessionDay — the roll is the FIRST session day
// (17:00 CT flip) with >= denseFront1mMin 1m bars; sparse snapshots before it
// never count.
func TestRollDayKeyFindsFirstDenseSessionDay(t *testing.T) {
	// One sparse snapshot on session 09-11, dense from session 09-15.
	bars := append(
		[]market.Kline{{OpenTime: auditMs(2026, 9, 10, 17, 0, 0), Open: 1, High: 1, Low: 1, Close: 1}},
		denseSession1m(2026, 9, 14, denseFront1mMin, 100, 101)...)
	day, ok := rollDayKey(bars)
	if !ok || day != "2026-09-15" {
		t.Fatalf("rollDayKey = (%q, %v), want (2026-09-15, true)", day, ok)
	}
}

// TestMeasureRollGapLastFullSessionBeforeRoll — the gap is the median over the
// LAST FULL session day before the roll (>=10 overlapping native 1h bars), NOT
// the whole overlap. Session 09-14 has 9 bars (<10) and must be skipped in
// favour of 09-11 (23 bars).
func TestMeasureRollGapLastFullSessionBeforeRoll(t *testing.T) {
	// Session 09-11: 23 overlapping native 1h bars. Session 09-14: only 9
	// (17:00→01:00, the live MNQ 09-26 tail shape) — below the 10-bar floor.
	newer1h := append(
		native1hSession(2026, 9, 10, 17, 110, 110),
		native1hN(2026, 9, 13, 17, 9, 110, 110)...)
	older1h := append(
		native1hSession(2026, 9, 10, 17, 100, 100),
		native1hN(2026, 9, 13, 17, 9, 100, 100)...)
	rollDay := "2026-09-15"

	got := lastFullOverlapBefore(newer1h, older1h, rollDay)
	if got != "2026-09-11" {
		t.Fatalf("lastFullOverlapBefore = %q, want 2026-09-11 (09-14 has <10 bars)", got)
	}
	gap, ok := measureRollGap(newer1h, older1h, got)
	if !ok || gap.N != 23 || gap.Gap != 10 {
		t.Fatalf("measureRollGap = %+v, %v; want N=23 gap=10", gap, ok)
	}
	if gap.Min != 10 || gap.Max != 10 {
		t.Fatalf("spread = [%.2f..%.2f], want [10..10]", gap.Min, gap.Max)
	}
	// The 9-bar day must NOT produce a measured gap.
	if _, ok := measureRollGap(newer1h, older1h, "2026-09-14"); ok {
		t.Fatal("measureRollGap on the 9-bar day must be ok=false")
	}
}

// TestRollStitcherNoHoleFromSparseSnapshots (FIX 2) — the OLD cut kept older
// bars only before the newer contract's FIRST 1m bar, so the newer's sparse
// pre-roll snapshots dropped ~4 real sessions of the older contract. The new
// cut keeps every older bar BEFORE the roll day and every newer bar FROM it:
// the older contract's real sessions survive (no hole).
func TestRollStitcherNoHoleFromSparseSnapshots(t *testing.T) {
	// Older 09-26: real full sessions on 09-11 and 09-14.
	olderBars := append(
		denseSession1m(2026, 9, 10, 1380, 100, 101),
		denseSession1m(2026, 9, 13, 1380, 101, 102)...)
	older1h := append(
		native1hSession(2026, 9, 10, 17, 100, 100),
		native1hSession(2026, 9, 13, 17, 100, 100)...)
	// Newer 12-26: ONE sparse 1m snapshot on session 09-11 (the old cut's
	// "first bar"), dense from session 09-15 (roll day). Its native 1h is
	// dense on 09-11 (the measurement day), like the live next-contract 1h.
	newerBars := append(
		[]market.Kline{{OpenTime: auditMs(2026, 9, 10, 17, 0, 0), Open: 1, High: 1, Low: 1, Close: 1}},
		denseSession1m(2026, 9, 14, 1380, 110, 111)...)
	newer1h := append(
		native1hSession(2026, 9, 10, 17, 110, 110),
		native1hSession(2026, 9, 14, 17, 110, 110)...)

	stitched, _, gaps, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 09-26", Bars: olderBars, Bars1H: older1h},
		{Contract: "MNQ 12-26", Bars: newerBars, Bars1H: newer1h},
	})
	if stopped != "" {
		t.Fatalf("stitch stopped at %q, want full", stopped)
	}
	if len(gaps) != 1 || gaps[0].Gap != 10 || gaps[0].Day != "2026-09-11" {
		t.Fatalf("gaps = %+v, want one gap 10 on 2026-09-11", gaps)
	}
	// No hole: the older contract's REAL sessions (09-11 AND 09-14) survive
	// the cut, back-adjusted onto the newest scale (+10).
	seen := map[string]bool{}
	for _, b := range stitched {
		seen[sessionKeyCT(b.OpenTime)] = true
	}
	if !seen["2026-09-11"] || !seen["2026-09-14"] || !seen["2026-09-15"] {
		t.Fatalf("stitched sessions = %v, want 09-11, 09-14 (older) and 09-15 (newer) all present — no hole", seen)
	}
	// Back-adjustment: an older 09-14 bar at open 101 lands at 111.
	adj := false
	for _, b := range stitched {
		if sessionKeyCT(b.OpenTime) == "2026-09-14" && b.Open == 111 {
			adj = true
		}
	}
	if !adj {
		t.Fatal("older 09-14 bars must be back-adjusted by +10 (open 101 → 111)")
	}
}

// TestRollStitcherSameDayContinuation (REL10-STITCH-CUT) — the older contract
// ends MID-SESSION at 09-14 10:33 CT (where the store stopped recording it) and
// the newer contract already has bars that day from 09:56. The cut is at the
// older's LAST STORED BAR: the stitched series is continuous across 10:33 (no
// gap, no duplicate minute) and 09-14 has its full 08:30–15:00 RTH candles. The
// OLD session-key cut dropped the newer's 09-14 bars (key 09-14 < roll day
// 09-15) and left a ~6.5 h hole.
func TestRollStitcherSameDayContinuation(t *testing.T) {
	// Older 09-26: 09-14 RTH 08:30 → 10:33 (its last stored bar).
	older := rthRange1m(2026, 9, 14, 8, 30, 10, 34, 98, 99)
	older1h := native1hSession(2026, 9, 10, 17, 100, 100) // session 09-11 (gap day)
	// Newer 12-26: 09-14 from 09:56 → 15:00 (same-day continuation), plus the
	// dense session 09-15 (roll day). Native 1h on 09-11 for the gap.
	newer := rthRange1m(2026, 9, 14, 9, 56, 15, 0, 110, 111)
	newer = append(newer, denseSession1m(2026, 9, 14, 1380, 112, 113)...)
	newer1h := append(
		native1hSession(2026, 9, 10, 17, 110, 110),
		native1hSession(2026, 9, 14, 17, 110, 110)...)

	stitched, full1h, gaps, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 09-26", Bars: older, Bars1H: older1h},
		{Contract: "MNQ 12-26", Bars: newer, Bars1H: newer1h},
	})
	if stopped != "" {
		t.Fatalf("stitch stopped at %q, want full", stopped)
	}
	if len(gaps) != 1 || gaps[0].Gap != 10 {
		t.Fatalf("gaps = %+v, want one gap 10", gaps)
	}

	// 09-14 1m must be continuous 08:30 → 14:59: no gap > 60s, no duplicate.
	seen := map[int64]bool{}
	var prev int64
	for _, b := range stitched {
		if sessionKeyCT(b.OpenTime) != "2026-09-14" {
			continue
		}
		if seen[b.OpenTime] {
			t.Fatalf("duplicate minute %d in the stitched 09-14 series", b.OpenTime)
		}
		seen[b.OpenTime] = true
		if prev != 0 && b.OpenTime-prev != 60_000 {
			t.Fatalf("gap in the stitched 09-14 series: %d → %d (not 60s)", prev, b.OpenTime)
		}
		prev = b.OpenTime
	}
	if prev != auditMs(2026, 9, 14, 14, 59, 0) {
		t.Fatalf("stitched 09-14 series ends at %d, want 14:59 (the last RTH minute)", prev)
	}

	// 09-14 must have its complete 1H RTH candle set: 08:30, 09:30, 10:30,
	// 11:30, 12:30, 13:30, 14:30 = 7 candles.
	n0914 := 0
	for _, c := range full1h {
		if sessionKeyCT(c.OpenTime) == "2026-09-14" {
			n0914++
		}
	}
	if n0914 != 7 {
		t.Fatalf("09-14 1H RTH candle count = %d, want 7 (08:30..14:30)", n0914)
	}
}

// TestRollStitcherStopsAtNoMeasuredGap — when no session day before the roll
// has >=10 overlapping native 1h bars, the stitch STOPS and names the pair.
func TestRollStitcherStopsAtNoMeasuredGap(t *testing.T) {
	// Newer has dense 1m from session 09-15; older has 1h only on 09-01
	// (no overlap with newer's 1h before the roll).
	olderBars := denseSession1m(2026, 8, 31, 1380, 100, 101) // session 09-01
	older1h := native1hSession(2026, 8, 31, 17, 100, 100)
	newerBars := denseSession1m(2026, 9, 14, 1380, 110, 111) // session 09-15
	newer1h := native1hSession(2026, 9, 14, 17, 110, 110)

	stitched, _, gaps, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 09-26", Bars: olderBars, Bars1H: older1h},
		{Contract: "MNQ 12-26", Bars: newerBars, Bars1H: newer1h},
	})
	if stopped != "MNQ 09-26→MNQ 12-26" {
		t.Fatalf("stopped = %q, want the unmeasurable roll named", stopped)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none (the gap was never measured)", gaps)
	}
	if len(stitched) == 0 {
		t.Fatal("stitched must still hold the newest contract's bars")
	}
	line := KeyLevelHistoryLine([]Contract1M{
		{Contract: "MNQ 09-26"}, {Contract: "MNQ 12-26"},
	}, keyLevel1HBars(stitched), gaps, stopped)
	if !strings.Contains(line, "stopped at MNQ 09-26→MNQ 12-26: no measured gap") {
		t.Fatalf("boot line %q does not name the unmeasurable roll", line)
	}
}

// TestSeedFull4HEMA34FromStitchedHistory (FIX 3) — SeedFull must seed the 4h
// EMA 34 (and the depth seam) from the STITCHED, back-adjusted 1m, so a boot
// with full history reaches >=102 closed 4h candles and does NOT refuse
// SWING4H. Named RED: feed only the current contract's tail (~2 weeks) and the
// count drops below 102 (the live "95/102", "100/102" refusal).
func TestSeedFull4HEMA34FromStitchedHistory(t *testing.T) {
	// 20 sessions of dense 1m (each 1380 bars) ≈ 120 4h buckets.
	stitched := denseSession1m(2026, 8, 17, 20*1380, 100, 101)
	now := stitched[len(stitched)-1].CloseTime + 1

	cfg := enabledCfg()
	e := New(cfg)
	SeedFull(e, stitched, keyLevel1HBars(stitched), now)
	if d := e.Depths()["4h EMA34"]; d < FourHEMA34Warmup {
		t.Fatalf("stitched seed 4h EMA34 = %d, want >= %d", d, FourHEMA34Warmup)
	}
	for _, m := range e.missing {
		if strings.HasPrefix(m, "bar history depth: 4h EMA34") {
			t.Fatalf("stitched seed must not refuse SWING4H; missing=%v", e.missing)
		}
	}

	// Named RED: the current-contract tail (last ~2 weeks) is what the OLD
	// input fed the 4h EMA — it cannot reach 102.
	tail := stitched[len(stitched)-14*1380:]
	e2 := New(cfg)
	SeedFull(e2, tail, keyLevel1HBars(tail), now)
	if d := e2.Depths()["4h EMA34"]; d >= FourHEMA34Warmup {
		t.Fatalf("current-contract tail 4h EMA34 = %d, want < %d (this is the refusal the fix removes)", d, FourHEMA34Warmup)
	}
}

// TestSeedFullDeletionOverFullHistory — a level whose BODY was closed through
// TWO MONTHS before the seed must still be deleted at seed: the deletion is
// computed ONCE over the full seeded history, not just the recent window.
func TestSeedFullDeletionOverFullHistory(t *testing.T) {
	c1 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 8, 30, 0), Open: 98, Close: 99}
	c2 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 9, 30, 0), Open: 100, Close: 99}
	c3 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 10, 30, 0), Open: 99, Close: 101}
	c1.CloseTime = keyLevel1HCandleCloseTime(c1.OpenTime)
	c2.CloseTime = keyLevel1HCandleCloseTime(c2.OpenTime)
	c3.CloseTime = keyLevel1HCandleCloseTime(c3.OpenTime)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelPrunePts = 0.5
	e := New(cfg)
	now := auditMs(2026, time.September, 30, 12, 0, 0)
	SeedFull(e, nil, []market.Kline{c1, c2, c3}, now)

	var lvl *Level
	for i := range e.State.SeedLevels {
		if e.State.SeedLevels[i].Price == 100 {
			lvl = &e.State.SeedLevels[i]
		}
	}
	if lvl == nil {
		t.Fatalf("seeded levels = %+v, want a level at 100 (the c2 open)", e.State.SeedLevels)
	}
	if !e.State.DeletedLevels[lvl.Key] {
		t.Fatalf("level at 100 must be deleted at seed (body closed through 2 months ago); deleted=%v", e.State.DeletedLevels)
	}
}

// TestNative1HStoreCannotAnchorRTHCandles — the native 1h store is WHOLE-HOUR
// aligned, so it cannot build the 08:30 RTH candles; 1m wins for the key-level
// SERIES by construction; the native 1h feeds ONLY the roll-gap measurement.
func TestNative1HStoreCannotAnchorRTHCandles(t *testing.T) {
	native := []market.Kline{
		{OpenTime: auditMs(2026, 9, 15, 8, 0, 0), Open: 100, High: 101, Low: 99, Close: 100.5},
		{OpenTime: auditMs(2026, 9, 15, 9, 0, 0), Open: 100.5, High: 102, Low: 100, Close: 101},
		{OpenTime: auditMs(2026, 9, 15, 10, 0, 0), Open: 101, High: 102, Low: 100.5, Close: 101.5},
	}
	got := keyLevel1HBars(native)
	if len(got) != 3 || got[0].OpenTime != auditMs(2026, 9, 15, 8, 0, 0) || got[1].OpenTime != auditMs(2026, 9, 15, 9, 0, 0) {
		t.Fatalf("native whole-hour 1h: got %+v, want 08:00/09:00/10:00 whole-hour opens", got)
	}
	m1 := rthHour1m(2026, time.September, 15, 8, 100, 100.5)
	m1 = append(m1, rthHour1m(2026, time.September, 15, 9, 100.5, 101)...)
	m1 = append(m1, rthHour1m(2026, time.September, 15, 10, 101, 101.5)...)
	agg := keyLevel1HBars(m1)
	if len(agg) != 3 || agg[0].OpenTime != auditMs(2026, 9, 15, 8, 30, 0) || agg[1].OpenTime != auditMs(2026, 9, 15, 9, 30, 0) {
		t.Fatalf("1m aggregation: got %+v, want 08:30/09:30/10:30 RTH anchors", agg)
	}
}

// TestSeedFullDrawsLevelFromOlderContract — SeedFull must feed the 1H RTH walk
// from the given stitched series, not the current-contract 1m aggregation: a
// level that only the OLDER contract carries appears in the seeded set at its
// back-adjusted price.
func TestSeedFullDrawsLevelFromOlderContract(t *testing.T) {
	// Older 09-26: a dense session on 09-11 (its roll day), then two RTH hours
	// on 09-14 — green (08:30) then red (09:30) → a level at the 09:30 open
	// (100), back-adjusted +12 → 112 on the newest scale.
	older := denseSession1m(2026, 9, 10, 1380, 100, 101) // session 09-11, dense
	older = append(older, rthHour1m(2026, time.September, 14, 8, 98, 99)...)
	older = append(older, rthHour1m(2026, time.September, 14, 9, 100, 99)...)
	// The gap is measured on 09-11: older native 1h closes 100, newer 112.
	older1h := native1hSession(2026, 9, 10, 17, 100, 100)
	// Newer 12-26: dense from 09-15 (roll day); native 1h on 09-11 for the gap.
	newer := denseSession1m(2026, 9, 14, 1380, 112, 113)
	newer1h := native1hSession(2026, 9, 10, 17, 112, 112)

	stitched, full1h, _, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 09-26", Bars: older, Bars1H: older1h},
		{Contract: "MNQ 12-26", Bars: newer, Bars1H: newer1h},
	})
	if stopped != "" {
		t.Fatalf("stitch stopped at %q, want full", stopped)
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelPrunePts = 0.5
	e := New(cfg)
	now := auditMs(2026, time.September, 15, 9, 0, 0)
	SeedFull(e, stitched, full1h, now)
	found := false
	for _, l := range e.State.SeedLevels {
		if l.Kind == KindKeyLevel && l.Price == 112 {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded levels = %+v, want the back-adjusted older-contract level at 112", e.State.SeedLevels)
	}
}
