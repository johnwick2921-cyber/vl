package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// KEYLEVEL-FULL-HISTORY (release #10) tests. Fixtures are real-UTC epoch ms
// through America/Chicago (EPOCH RULING 2026-10-03): two contracts overlap on
// a roll DAY (same OpenTimes, different price scale = the basis), and the
// older contract's PRE-roll history exists only on its own scale.

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

func TestRollGapMedianAndBackAdjust(t *testing.T) {
	newer := []market.Kline{
		{OpenTime: 1000, Close: 110},
		{OpenTime: 2000, Close: 111},
		{OpenTime: 3000, Close: 112},
	}
	older := []market.Kline{
		{OpenTime: 2000, Open: 100, Close: 101}, // diff 10
		{OpenTime: 3000, Open: 101, Close: 102}, // diff 10
		{OpenTime: 4000, Open: 99, Close: 100},  // no overlap
	}
	gap, n, ok := rollGap(newer, older)
	if !ok || n != 2 || gap != 10 {
		t.Fatalf("rollGap = (%v, %d, %v), want (10, 2, true)", gap, n, ok)
	}
	adj := backAdjust(older, gap)
	if adj[0].Close != 111 || adj[1].Close != 112 || adj[0].Open != 110 {
		t.Fatalf("backAdjust = %+v, want closes 111/112 and open 110", adj)
	}
}

func TestRollGapNoOverlapNotOk(t *testing.T) {
	newer := []market.Kline{{OpenTime: 1000, Close: 110}}
	older := []market.Kline{{OpenTime: 999, Close: 100}}
	if _, _, ok := rollGap(newer, older); ok {
		t.Fatal("rollGap with no shared OpenTime must be ok=false — never guess a gap")
	}
}

// TestStitchKeyLevelHistoryBackAdjustsOlderContract — the named RED: a level
// drawn on the OLDER contract's pre-roll day appears in the stitched series at
// the BACK-ADJUSTED price. Reverting the trader to LastNBarsCurrentContract
// (50000) drops the older day entirely, so this level is gone — the test fails.
func TestStitchKeyLevelHistoryBackAdjustsOlderContract(t *testing.T) {
	// Roll day 2026-09-15: both contracts have 08:30 RTH 1m bars.
	// Older: 08:30 red (102→101). Newer: 08:30 green (112→113). Basis = +12.
	// Older pre-roll day 2026-09-14: 08:30 green (98→99), 09:30 red (100→99)
	// → a green→red colour change draws a level at the 09:30 open = 100,
	// back-adjusted to 112.
	older := append(
		rthHour1m(2026, time.September, 14, 8, 98, 99),   // green
		rthHour1m(2026, time.September, 14, 9, 100, 99)..., // red → level at 100
	)
	older = append(older, rthHour1m(2026, time.September, 15, 8, 102, 101)...) // overlap day
	newer := rthHour1m(2026, time.September, 15, 8, 112, 113)                  // overlap day, +12 basis

	stitched, gaps, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 06-26", Bars: older, Bars1H: []market.Kline{
			native1h(2026, time.September, 14, 8, 98, 99),
			native1h(2026, time.September, 14, 9, 100, 99),
			native1h(2026, time.September, 15, 8, 102, 101), // overlap hour
		}},
		{Contract: "MNQ 09-26", Bars: newer, Bars1H: []market.Kline{
			native1h(2026, time.September, 15, 8, 112, 113), // overlap hour, +12 basis
		}},
	})
	if stopped != "" {
		t.Fatalf("stitch stopped at %q, want full", stopped)
	}
	if len(gaps) != 1 || gaps[0] != 12 {
		t.Fatalf("gaps = %v, want [12]", gaps)
	}
	// The stitched 1H RTH series must contain a candle whose OPEN is the
	// back-adjusted 09:30 level (112) — the older contract's pre-roll colour
	// change, now on the newest scale.
	found := false
	for _, c := range stitched {
		if c.OpenTime == auditMs(2026, time.September, 14, 9, 30, 0) && c.Open == 112 {
			found = true
		}
	}
	if !found {
		t.Fatalf("stitched series missing the back-adjusted older-contract level at 112: %+v", stitched)
	}
}

// TestStitchKeyLevelHistoryStopsAtNoOverlap — the first roll with no shared
// timestamp must STOP the stitch (never guess a gap) and the boot line names it.
func TestStitchKeyLevelHistoryStopsAtNoOverlap(t *testing.T) {
	older := rthHour1m(2026, time.September, 14, 8, 98, 99)  // day 1 only
	newer := rthHour1m(2026, time.September, 15, 8, 112, 113) // day 2 only, no overlap
	stitched, gaps, stopped := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 06-26", Bars: older, Bars1H: []market.Kline{native1h(2026, time.September, 14, 8, 98, 99)}},
		{Contract: "MNQ 09-26", Bars: newer, Bars1H: []market.Kline{native1h(2026, time.September, 15, 8, 112, 113)}},
	})
	if stopped != "MNQ 06-26→MNQ 09-26" {
		t.Fatalf("stopped = %q, want the no-overlap roll named", stopped)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps = %v, want none (the gap was never measured)", gaps)
	}
	if len(stitched) != 1 {
		t.Fatalf("stitched = %d candles, want only the newest contract's 1 candle", len(stitched))
	}
	line := KeyLevelHistoryLine([]Contract1M{
		{Contract: "MNQ 06-26"}, {Contract: "MNQ 09-26"},
	}, stitched, gaps, stopped)
	if !strings.Contains(line, "stopped at MNQ 06-26→MNQ 09-26: no overlap") {
		t.Fatalf("boot line %q does not name the no-overlap roll", line)
	}
}

// TestSeedFullDeletionOverFullHistory — a level whose BODY was closed through
// TWO MONTHS before the seed must still be deleted at seed: the deletion is
// computed ONCE over the full seeded history, not just the recent window.
// (The old path — last 50000 1m bars ≈ 3.5 weeks — would miss this and the
// dead level would reappear.)
func TestSeedFullDeletionOverFullHistory(t *testing.T) {
	// Three 1H RTH candles, July 1 2026, all closed; now is 2 months later.
	c1 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 8, 30, 0), Open: 98, Close: 99}   // green
	c2 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 9, 30, 0), Open: 100, Close: 99}  // red → level at 100
	c3 := market.Kline{OpenTime: auditMs(2026, time.July, 1, 10, 30, 0), Open: 99, Close: 101} // green, body crosses 100
	c1.CloseTime = keyLevel1HCandleCloseTime(c1.OpenTime)
	c2.CloseTime = keyLevel1HCandleCloseTime(c2.OpenTime)
	c3.CloseTime = keyLevel1HCandleCloseTime(c3.OpenTime)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelPrunePts = 0.5
	e := New(cfg)
	now := auditMs(2026, time.September, 30, 12, 0, 0) // two months later
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

// TestNative1HStoreCannotAnchorRTHCandles — the parity finding: the native 1h
// store is WHOLE-HOUR aligned, so its candles open at 08:00/09:00/10:00 — NOT
// the 08:30/09:30/10:30 RTH anchors the 1m aggregation produces. The level walk
// draws a line at the candle OPEN, so whole-hour candles would draw levels at
// the wrong prices. Hence 1m wins for the key-level SERIES by construction; the
// native 1h feeds ONLY the roll-gap measurement.
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
	older := append(
		rthHour1m(2026, time.September, 14, 8, 98, 99),    // green
		rthHour1m(2026, time.September, 14, 9, 100, 99)..., // red → level at 100
	)
	older = append(older, rthHour1m(2026, time.September, 15, 8, 102, 101)...)
	newer := rthHour1m(2026, time.September, 15, 8, 112, 113)
	stitched, _, _ := StitchKeyLevelHistory([]Contract1M{
		{Contract: "MNQ 06-26", Bars: older, Bars1H: []market.Kline{
			native1h(2026, time.September, 14, 8, 98, 99),
			native1h(2026, time.September, 14, 9, 100, 99),
			native1h(2026, time.September, 15, 8, 102, 101),
		}},
		{Contract: "MNQ 09-26", Bars: newer, Bars1H: []market.Kline{
			native1h(2026, time.September, 15, 8, 112, 113),
		}},
	})

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelPrunePts = 0.5
	e := New(cfg)
	now := auditMs(2026, time.September, 15, 9, 0, 0) // after all fixtures closed
	SeedFull(e, newer, stitched, now)                 // missing sources refuse entries, not levels
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
