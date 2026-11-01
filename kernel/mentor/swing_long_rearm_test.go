package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestSwing4hOneSetupPerApproachLong — the LONG mirror of
// TestSwing4hOneSetupPerApproach (m463, D5.2-R19/R22/R24): the support's first
// touch arms and fills; the 05:15 bar CLOSES BELOW the line → the level is
// INVALID for the 4h candle; 05:20 closes back above (candle 1) and 05:25 is
// its inside bar → the LONG ISB, stop AT the line. The later above-line
// reject-shaped touch at 05:40 is refused (swing_level_invalid) — never a
// plain re-arm.
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
		mk(15, 5, 10, 10165, 10175, 10155, 10175), // candle 2 of the window: fills the long
		mk(15, 5, 15, 10060, 10100, 10050, 10060), // closes BELOW the line (no touch) → level INVALID
		mk(15, 5, 20, 10060, 10220, 10050, 10200), // closes back above (candle 1)
		mk(15, 5, 25, 10195, 10205, 10195, 10205), // its inside bar → LONG ISB @10205
		mk(15, 5, 30, 10195, 10205, 10195, 10205), // (the ISB is spent)
		mk(15, 5, 35, 10195, 10210, 10190, 10205), // back above without touching
		mk(15, 5, 40, 10165, 10175, 10155, 10175), // above-line touch → REFUSED (invalid level)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, cur[8].OpenTime+60_000)
	var entries []Intent
	cancels := 0
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry:
			entries = append(entries, in)
		case CancelArm:
			cancels++
		}
	}
	if len(entries) != 2 || entries[0].Side != SideLong || entries[1].Side != SideLong || entries[1].Price != 10205 || abs(entries[1].Stop-swingLineGolden) > 0.01 {
		t.Fatalf("want the long reject + the LONG inside bar (stop AT the line) only; got %+v", entries)
	}
	if cancels != 0 {
		t.Fatalf("cancels = %d, want 0 (the reject filled; nothing rests); got %+v", cancels, out)
	}
	if m463Refused(s, SwingRefusedLevelInvalid) == 0 {
		t.Fatalf("the 05:40 reject-shaped touch must be NAMED swing_level_invalid: %+v", s.refused)
	}
}
