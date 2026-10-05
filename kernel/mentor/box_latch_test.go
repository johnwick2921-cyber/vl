package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestBoxLatchNeverRedrawsOnHigherHigh pins D4.1-22 — never redraw the box
// on a higher high ("Nếu nó tạo ĐỈNH CAO HƠN thì có nên VẼ LẠI BOX không?
// KHÔNG… Mình KHÔNG CÓ VẼ CÁI BOX KHÁC… Y NGUYÊN ĐÓ TỚI CUỐI NGÀY"
// [D4.1 p2 @02:39–02:57]). The first FTGH drawn per trading day is latched in
// State and never replaced by a later higher confirmed swing high. Mutant:
// let the later extreme rebuild the box (drop the latch) → the control says
// the scan alone rebuilds ftgh:103.50:100.00, but the latched box must stay
// ftgh:102.00:100.00 → RED.
func TestBoxLatchNeverRedrawsOnHigherHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// First FTGH: confirmed swing high 102 @2, partner (later, confirmed,
	// non-breaking) high 101.5 @5. A HIGHER confirmed high 103.5 @9 appears
	// later the same day, with its own later partner 102.5 @12 — so the pure
	// scan, after that higher high, WOULD rebuild the box on the new extreme.
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 99.5, 99, 100),
		mk(2, 100, 102, 99, 101),
		mk(3, 101, 101, 100, 100.5),
		mk(4, 100, 100, 99, 99.5),
		mk(5, 99.5, 101.5, 99, 100),
		mk(6, 100, 101, 99, 100),
		mk(7, 100, 101, 99, 100),
		mk(8, 100, 100.5, 99, 100),
		mk(9, 100, 103.5, 99, 101),
		mk(10, 101, 102, 100, 100.5),
		mk(11, 100, 101, 99, 100),
		mk(12, 100, 102.5, 99, 101),
		mk(13, 101, 101.5, 100, 100.5),
	}
	e := New(cfg)
	e.Tick(bars[:7], bars[6].OpenTime+59_999)
	if got := e.State.LatchedBoxes["ftgh"]; got.Key != "ftgh:102.00:100.00" || got.Top != 102 || got.Bottom != 100 {
		t.Fatalf("first FTGH box = %+v, want key ftgh:102.00:100.00 top 102 bottom 100", got)
	}

	// CONTROL: the pure scan, after the higher high closes, WOULD rebuild the
	// box with the new extreme — the latch (not the scan) is what keeps the
	// first box. Without this control the pin would be vacuous (a tape that
	// never redraws cannot prove the latch stops a redraw).
	var rebuiltKey string
	for _, b := range BoxesBuild(bars, cfg.Box, time.UnixMilli(bars[13].OpenTime+59_999)) {
		if b.Kind == FTGH {
			rebuiltKey = b.Key
		}
	}
	if rebuiltKey != "ftgh:103.50:101.00" {
		t.Fatalf("control: the scan after the higher high should rebuild ftgh:103.50:101.00, got %q — the tape does not exercise the redraw", rebuiltKey)
	}

	// The latch must survive the higher high: same box, same key, to day end.
	e.Tick(bars[:14], bars[13].OpenTime+59_999)
	got := e.State.LatchedBoxes["ftgh"]
	if got.Key != "ftgh:102.00:100.00" || got.Top != 102 || got.Bottom != 100 {
		t.Fatalf("after the higher high the box was redrawn: %+v, want the first box ftgh:102.00:100.00", got)
	}
}
