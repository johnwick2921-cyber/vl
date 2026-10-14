package mentor

import (
	"testing"

	"vl/market"
)

// ── r22 "ONE SOURCE": the chart/panel DRAW set carries exactly the old
//    extremes the trading code reads — the nearest unbroken old high and old
//    low — never folded into a key level, never pruned. ────────────────────

// TestDisplayLevelsDrawsOldExtremesOneForOne — DisplayLevels passes the
// trading old extremes through unchanged (key, price, role), even an old high
// 1.5 pts from a key level (the pre-r22 display folded it into the key, so the
// chart and the trader disagreed). Mutant (restore the fold) → RED.
func TestDisplayLevelsDrawsOldExtremesOneForOne(t *testing.T) {
	trading := []Level{
		{Kind: KindKeyLevel, Price: 100, Key: "key:100"},
		{Kind: KindOldExtreme, Price: 101.5, Key: "old_extreme:101.50", AtTime: 10, Role: OldHigh},
		{Kind: KindOldExtreme, Price: 80, Key: "old_extreme:80.00", AtTime: 9, Role: OldLow},
		{Kind: KindEMA34, Price: 90, Key: "ema34"},
	}
	drawn := DisplayLevels(trading, 20)
	var old []Level
	for _, l := range drawn {
		if len(l.MergedFrom) != 0 {
			t.Fatalf("drawn line %s carries merged_from %v — nothing is folded any more", l.Key, l.MergedFrom)
		}
		if l.Kind == KindOldExtreme {
			old = append(old, l)
		}
	}
	if len(drawn) != len(trading) || len(old) != 2 {
		t.Fatalf("drawn %d lines (%d old extremes), want %d lines with both old extremes: %+v", len(drawn), len(old), len(trading), drawn)
	}
	for i, w := range []Level{trading[1], trading[2]} {
		if old[i].Key != w.Key || old[i].Price != w.Price || old[i].Role != w.Role {
			t.Fatalf("drawn old extreme %d = %+v, want the trading line %+v", i, old[i], w)
		}
	}
}

// TestTradingLocationReadsOldExtremeAtKeyLevel — LocationVerdict reads the
// trading set: an old extreme within ±2 of a key is the location
// "old_extreme@key_level" (an old high alone is not a location).
func TestTradingLocationReadsOldExtremeAtKeyLevel(t *testing.T) {
	levels := []Level{
		{Kind: KindKeyLevel, Price: 100, Key: "key:100"},
		{Kind: KindOldExtreme, Price: 101.5, Key: "old_extreme:101.50", AtTime: 10, Role: OldHigh},
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cur := market.Kline{High: 102, Low: 100.5, Close: 101}
	ok, where := LocationVerdict(cur, levels, TriggerLine{}, nil, cfg)
	if !ok || where != "old_extreme@key_level" {
		t.Fatalf("location = %v %q, want old_extreme@key_level", ok, where)
	}
}

// oldExtremeTape5m is ten 5m candles with pivot HIGHS 110 (bucket 2) and 108
// (bucket 6) and a pivot LOW 95 (bucket 4); no later candle trades beyond any
// of them. extra candles are appended after it.
func oldExtremeTape5m(extra ...market.Kline) []market.Kline {
	bars := []market.Kline{
		mk5(0, 100, 101, 99, 100),
		mk5(1, 100, 103, 99.5, 102),
		mk5(2, 102, 110, 101, 104), // pivot high 110
		mk5(3, 104, 105, 100, 101),
		mk5(4, 101, 102, 95, 96), // pivot low 95
		mk5(5, 96, 104, 96.5, 103),
		mk5(6, 103, 108, 102, 107), // pivot high 108 (a lower high)
		mk5(7, 107, 107.5, 100, 101),
		mk5(8, 101, 102, 97, 98),
		mk5(9, 98, 99, 97.5, 98.5),
	}
	return append(bars, extra...)
}

func oldExtremesOf(t *testing.T, bars []market.Kline) map[string]float64 {
	t.Helper()
	now := bars[len(bars)-1].CloseTime + 1
	got := map[string]float64{}
	for _, l := range Levels(bars, DefaultConfig(), now) {
		if l.Kind != KindOldExtreme {
			continue
		}
		if _, dup := got[l.Role]; dup || l.Role != OldHigh && l.Role != OldLow {
			t.Fatalf("old extreme %+v: want exactly one line per role, each with its role", l)
		}
		got[l.Role] = l.Price
	}
	return got
}

// TestLevelsOldExtremesAreTheNearestUnbrokenPair — r22 P1, the unit pin at the
// trading level source (Levels → swingOldExtremeLevels). Of the pivots 110 /
// 108 (highs) and 95 (low), none broken, the set is the NEAREST: old high 108,
// old low 95 — one line per role, the role stamped. The pre-r22 source returned
// every pivot (110, 108, 95). Mutant (return every pivot) → RED.
func TestLevelsOldExtremesAreTheNearestUnbrokenPair(t *testing.T) {
	got := oldExtremesOf(t, oldExtremeTape5m())
	if len(got) != 2 || got[OldHigh] != 108 || got[OldLow] != 95 {
		t.Fatalf("old extremes = %v, want {high: 108, low: 95} — the nearest unbroken pair", got)
	}
}

// TestLevelsOldExtremeDropsOnceTradedThrough — a candle trading above the old
// high 108 breaks it (a wick is enough — law 2, the candle need not close):
// the nearest UNBROKEN old high is 110 again. The break is read on the candles
// BEFORE the newest one, so the candle being judged meets the old extremes
// that stood before it.
func TestLevelsOldExtremeDropsOnceTradedThrough(t *testing.T) {
	broke := mk5(10, 98.5, 108.5, 98, 99) // wick to 108.5 > 108, closes far below
	judged := mk5(11, 99, 100, 98.5, 99.5)
	got := oldExtremesOf(t, oldExtremeTape5m(broke, judged))
	if got[OldHigh] != 110 {
		t.Fatalf("old high = %v, want 110 — 108 was traded through by a later wick", got)
	}
	// The SAME wick as the newest candle (the one being judged) does not yet
	// break 108: it is measured against it.
	got = oldExtremesOf(t, oldExtremeTape5m(broke))
	if got[OldHigh] != 108 {
		t.Fatalf("old high = %v, want 108 — the judged candle meets the old high that stood before it", got)
	}
}

// mk5 builds a 5m bar (OpenTime in 5m buckets) for the swing detector.
func mk5(i int64, o, h, l, c float64) market.Kline {
	ot := i * 5 * 60_000
	return market.Kline{OpenTime: ot, Open: o, High: h, Low: l, Close: c, CloseTime: ot + 5*60_000 - 1}
}
