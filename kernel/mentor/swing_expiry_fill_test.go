package mentor

import (
	"testing"

	"vl/market"
)

// m437 (release #53, recheck lens l8) — the swing resting order expires at the
// bucket end - 1ms, and the tick that finalises the bucket's LAST 5m bar runs at
// now = that bar's CloseTime+1 = the bucket end. The expiry used to drop Pending
// at the top of SwingTick, before the bar walk reached that last bar, so a real
// fill on it (the NT8 order is live until its expiry) was never tracked: no BE
// at +1R, no hold-close. Production clock convention: CloseTime = open + 5m - 1,
// now = CloseTime + 1. Short rejection at the 10057.14 line (closed buckets
// 17:00 -> 10000, 21:00 -> 11000), armed off the 04:45 candle of the 01:00
// bucket (entry 10038).

func expiryBars(t *testing.T, ks ...market.Kline) []market.Kline {
	t.Helper()
	bars := []market.Kline{
		mk5m(t, 14, 17, 5, 9999, 10000, 9998, 10000),
		mk5m(t, 14, 21, 5, 10999, 11000, 10998, 11000),
		mk5m(t, 15, 1, 0, 10000, 10001, 9999, 10000),
		mk5m(t, 15, 4, 40, 10020, 10022, 10018, 10020),
		mk5m(t, 15, 4, 45, 10040, 10060, 10038, 10045), // touches the line, closes back: sell stop @10038
	}
	bars = append(bars, ks...)
	for i := range bars {
		bars[i].CloseTime = bars[i].OpenTime + swingBarMs - 1
	}
	return bars
}

// tickAtClose runs SwingTick the way production does: now = the newest closed
// bar's CloseTime + 1.
func tickAtClose(s *SwingState, bars []market.Kline) []Intent {
	return SwingTick(s, bars, DefaultSwingCfg(), bars[len(bars)-1].CloseTime+1)
}

func armExpiryOrder(t *testing.T) (*SwingState, []market.Kline) {
	t.Helper()
	bars := expiryBars(t)
	s := &SwingState{}
	out := tickAtClose(s, bars)
	if len(out) != 1 || out[0].Action != PlaceStopEntry || out[0].Side != SideShort {
		t.Fatalf("setup: want one SHORT stop entry, got %+v", out)
	}
	if s.Pending == nil || s.Pending.ExpiryMs <= 0 {
		t.Fatalf("setup: pending/expiry not set: %+v", s.Pending)
	}
	return s, bars
}

// A fill on the bucket's LAST 5m bar is tracked, then BE at +1R and the
// hold-close both run.
func TestSwingExpiryFillOnLastBarOfBucketIsTracked(t *testing.T) {
	s, bars := armExpiryOrder(t)
	bars = expiryBars(t,
		mk5m(t, 15, 4, 50, 10045, 10050, 10040, 10042), // no fill (low 10040 > 10038)
		mk5m(t, 15, 4, 55, 10042, 10046, 10030, 10032), // LAST bar of the bucket: low 10030 <= 10038 fills
	)
	tickAtClose(s, bars)
	if s.Pos == nil {
		t.Fatalf("filled order on the last 5m bar of the bucket not tracked: Pos=nil pending=%+v", s.Pending)
	}
	if s.Pending != nil {
		t.Fatalf("a filled order must not stay pending: %+v", s.Pending)
	}
	// BE at +1R: a closed bar at/through entry - Risk.
	beClose := s.Pos.Entry - s.Pos.Risk - 1
	bars = append(bars, mk5m(t, 15, 5, 0, 10000, 10001, beClose-1, beClose))
	bars[len(bars)-1].CloseTime = bars[len(bars)-1].OpenTime + swingBarMs - 1
	out := tickAtClose(s, bars)
	var be bool
	for _, in := range out {
		if in.Action == ActionMoveStopBE {
			be = true
		}
	}
	if !be {
		t.Fatalf("no BE at +1R for the last-bar fill; got %+v", out)
	}
	// hold-close: entry bucket 01:00 holds one 4h candle -> closes on a bar at/after 09:00.
	late := mk5m(t, 15, 9, 5, beClose, beClose, beClose, beClose)
	late.CloseTime = late.OpenTime + swingBarMs - 1
	bars = append(bars, late)
	out = tickAtClose(s, bars)
	var closed bool
	for _, in := range out {
		if in.Action == ActionClosePosition {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("no hold-close for the last-bar fill; got %+v", out)
	}
}

// No fill on any bar: the order expires cleanly at the bucket end, no position,
// and no manage intents.
func TestSwingExpiryUnfilledOrderExpiresCleanly(t *testing.T) {
	s, _ := armExpiryOrder(t)
	bars := expiryBars(t,
		mk5m(t, 15, 4, 50, 10045, 10050, 10040, 10042),
		mk5m(t, 15, 4, 55, 10042, 10046, 10039, 10040), // low 10039 > 10038: no fill
	)
	out := tickAtClose(s, bars)
	if s.Pending != nil {
		t.Fatalf("an unfilled order must be dropped once its final bar was walked: %+v", s.Pending)
	}
	if s.Pos != nil {
		t.Fatalf("no fill -> no position, got %+v", s.Pos)
	}
	for _, in := range out {
		if in.Action == ActionMoveStopBE || in.Action == ActionClosePosition {
			t.Fatalf("an unfilled order must send no manage intent: %+v", out)
		}
	}
}

// A bar that OPENS after the expiry (delivered late, in the same walk as the
// final bar) never fills the expired order.
func TestSwingExpiryBarAfterExpiryNeverFills(t *testing.T) {
	s, _ := armExpiryOrder(t)
	bars := expiryBars(t,
		mk5m(t, 15, 4, 50, 10045, 10050, 10040, 10042),
		mk5m(t, 15, 4, 55, 10042, 10046, 10039, 10040), // final bar: no fill
		mk5m(t, 15, 5, 0, 10040, 10041, 10000, 10005),  // next bucket: would trade through 10038
	)
	tickAtClose(s, bars)
	if s.Pos != nil {
		t.Fatalf("a bar opening after the expiry must not fill the expired order: %+v", s.Pos)
	}
	if s.Pending != nil {
		t.Fatalf("expired order still pending: %+v", s.Pending)
	}
}
