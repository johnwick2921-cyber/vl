package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// sparseHeadWindow is the production seed-window shape for the MNQ 12-26
// contract: two sparse `historical_import` snapshots (2026-09-07 12:00 and
// 2026-09-08 16:00 CT, more than an hour apart) followed by contiguous 1m
// bars for three RTH sessions. The colour flips every hour so the level walk
// has levels to draw.
func sparseHeadWindow() []market.Kline {
	at := func(d, h, m int) int64 { return auditMs(2026, time.September, d, h, m, 0) }
	snap := func(ms int64, px float64) market.Kline {
		return market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: px, High: px + 1, Low: px - 1, Close: px}
	}
	w := []market.Kline{snap(at(7, 12, 0), 100), snap(at(8, 16, 0), 100)}
	for _, day := range []int{14, 15, 16} {
		for h := 8; h <= 14; h++ {
			for m := 0; m < 60; m++ {
				if h == 8 && m < 30 {
					continue
				}
				ms := at(day, h, m)
				base := 100 + float64(day-14)*10
				o := base + float64(h)
				c := o + 1
				if h%2 == 1 { // alternate the hour's colour: red on odd hours
					c = o - 1
				}
				w = append(w, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: o, High: o + 2, Low: o - 2, Close: c})
			}
		}
	}
	return w
}

// A 1m series that starts with two sparse snapshots must be aggregated into
// anchored 1H candles — never read as pre-bucketed 1H input (the old
// first-two-bars rule made every 1m bar in 08:00–14:59 a "candle").
func TestKeyLevel1HBarsSparseSnapshotHeadIsStillOneMinute(t *testing.T) {
	w := sparseHeadWindow()
	got := keyLevel1HBars(w)
	want := keyLevel1HBars(w[2:]) // the same 1m tape without the snapshots

	if len(got) > len(want)+1 {
		t.Fatalf("%d candles from %d bars — the 1m series was read as pre-bucketed 1H input (want %d 1m-derived, +1 for the 09-07 snapshot)",
			len(got), len(w), len(want))
	}
	if len(want) != 3*7 {
		t.Fatalf("control: the 1m tape aggregates to %d candles, want 21 (3 sessions × 7 anchored hours)", len(want))
	}
	for _, c := range got {
		if m := rthMinuteOf(c.OpenTime); m%60 != 30 {
			t.Fatalf("candle opens at minute-of-day %d — not on the 08:30 anchor (a raw 1m bar leaked in)", m)
		}
	}
	tail := got[len(got)-len(want):]
	for i := range want {
		if tail[i] != want[i] {
			t.Fatalf("candle %d differs from the 1m-only aggregation: got %+v want %+v", i, tail[i], want[i])
		}
	}
}

// Production call site: Seed() over the sparse-head window builds the same
// 1H RTH series and level set from the 1m tape as it does without the head.
func TestSeedSparseSnapshotHeadBuildsOneMinuteLevels(t *testing.T) {
	w := sparseHeadWindow()
	now := w[len(w)-1].CloseTime + 1
	e1 := New(DefaultConfig())
	e1.Cfg.Enabled = true
	Seed(e1, w, now)
	e2 := New(DefaultConfig())
	e2.Cfg.Enabled = true
	Seed(e2, w[2:], now)

	if n1, n2 := len(e1.State.Seed1HBars), len(e2.State.Seed1HBars); n1 > n2+1 {
		t.Fatalf("seeded %d 1H candles with the sparse head, %d without: the head turned the 1m tape into garbage candles", n1, n2)
	}
	if len(e2.State.SeedLevels) == 0 {
		t.Fatal("control: the 1m-only seed drew no levels — the fixture cannot discriminate")
	}
	if n1, n2 := len(e1.State.SeedLevels), len(e2.State.SeedLevels); n1 < n2 || n1 > n2+1 {
		t.Fatalf("seed levels with the sparse head = %d, without = %d", n1, n2)
	}
}

// The pre-bucketed fixture path still works: a series of ready-made 1H
// candles (gaps >= 60 min) is taken as candles.
func TestKeyLevel1HBarsStillAcceptsPreBucketed1H(t *testing.T) {
	var in []market.Kline
	for h := 8; h <= 14; h++ {
		ms := auditMs(2026, time.September, 15, h, 30, 0)
		o := 100 + float64(h)
		in = append(in, market.Kline{OpenTime: ms, CloseTime: ms + 3_599_999, Open: o, High: o + 3, Low: o - 3, Close: o + 1})
	}
	got := keyLevel1HBars(in)
	if len(got) != len(in) {
		t.Fatalf("pre-bucketed 1H input: %d candles out of %d — each bar must stay a candle", len(got), len(in))
	}
	for i := range in {
		if got[i].Open != in[i].Open || got[i].Close != in[i].Close {
			t.Fatalf("pre-bucketed candle %d was re-aggregated: %+v vs %+v", i, got[i], in[i])
		}
	}
}
