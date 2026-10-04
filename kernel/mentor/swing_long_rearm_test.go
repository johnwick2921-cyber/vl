package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestSwing4hOneSetupPerApproachLong — the LONG-side re-arm guard
// (swing4h.go:219, audit row W9 long side): one long setup per approach, no
// new one until price has left the line by at least the stop distance and
// comes back. The mirror of the short-side test. R32 [D5.2 p2 @20:48]: the
// through-cross makes the level INVALID, so the later above-line touch is
// refused — an invalid level trades only an inside bar.
func TestSwing4hOneSetupPerApproachLong(t *testing.T) {
	cfg := DefaultSwingCfg()
	loc := time.FixedZone("CT", -5*3600)
	mk := func(day, hour, minute int, o, h, l, c float64) market.Kline {
		ot := time.Date(2026, 9, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: ot.UnixMilli(), Open: o, High: h, Low: l, Close: c, CloseTime: ot.UnixMilli() + 4*60_000}
	}
	cur := []market.Kline{
		mk(15, 5, 0, 10200, 10210, 10195, 10205),  // prev above the line
		mk(15, 5, 5, 10165, 10175, 10155, 10175),  // first setup (long)
		mk(15, 5, 10, 10165, 10175, 10155, 10175), // same approach again — blocked
		mk(15, 5, 15, 10060, 10100, 10050, 10060), // leaves the line by ≥ 30 (entirely below)
		mk(15, 5, 20, 10060, 10220, 10050, 10200), // crosses back through → cancel, level INVALID
		mk(15, 5, 25, 10195, 10205, 10195, 10205), // above the line — no ISB close-back
		mk(15, 5, 30, 10195, 10205, 10195, 10205), // above the line — no ISB close-back
		mk(15, 5, 35, 10195, 10210, 10190, 10205), // back above without touching
		mk(15, 5, 40, 10165, 10175, 10155, 10175), // above-line touch → REFUSED (invalid level)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[8].OpenTime+60_000)
	entries, cancels, longs := 0, 0, 0
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry:
			entries++
			if in.Side == SideLong {
				longs++
			}
		case CancelArm:
			cancels++
		}
	}
	if entries != 1 || longs != 1 {
		t.Fatalf("long stop entries = %d (longs=%d), want 1 (the invalid level refuses the later touch); got %+v", entries, longs, out)
	}
	if cancels != 1 {
		t.Fatalf("cancels = %d, want 1 (the through-cross back); got %+v", cancels, out)
	}
}
