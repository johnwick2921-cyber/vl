package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// urFixSlidingMk builds 1m bars from t0.
func urFixSlidingMk(t0 int64) func(i int, o, h, l, c float64) market.Kline {
	return func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
}

// TestUrFixLatchedBoxFlipAndTouchesAcrossSlidingWindow — UR-FIX U1. The live
// window SLIDES (the provider returns the last 2000 bars), so a latched box's
// FormedAt index shifts by 1 each new bar. On dev the latched box keeps its
// latch-time Flipped (the D14 Uno-Reverse never fires live) and countBoxTouches
// walks from a stale index (the touches drift). The fix anchors FormedAtMs and
// re-resolves + recomputes Flipped and Touches every tick. RED on dev (Flipped
// stuck false, touches 0); GREEN on the fix.
func TestUrFixLatchedBoxFlipAndTouchesAcrossSlidingWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := urFixSlidingMk(t0)
	bars := []market.Kline{
		mk(0, 99, 100, 98.5, 99.5),
		mk(1, 99, 100, 98.5, 99.5),
		mk(2, 99, 100, 98.5, 99.5),
		mk(3, 100, 101, 99, 100),
		mk(4, 100, 99.5, 99, 100),
		mk(5, 100, 102, 99, 101),    // extreme high 102 (confirmed by bar 6)
		mk(6, 101, 101, 100, 100.5), // confirms bar 5
		mk(7, 100, 100, 99, 99.5),
		mk(8, 99.5, 101.5, 99, 100), // partner high 101.5 (later, lower)
		mk(9, 100, 101, 99, 100),    // confirms partner → box born (FormedAt 9)
		mk(10, 101, 102.1, 100.5, 101), // wick touches the top 102 → Touches 1
		mk(11, 103, 105, 102.5, 104),   // BODY escape: open/close above the top
	}
	e := New(cfg)

	// Tick 1: the box latches BEFORE the escape — Flipped false.
	e.Tick(bars[:11], bars[10].OpenTime+59_999)
	latched := e.State.LatchedBoxes["ftgh"]
	if latched.Key != "ftgh:102.00:100.00" {
		t.Fatalf("first FTGH = %+v, want ftgh:102.00:100.00", latched)
	}
	if latched.Flipped {
		t.Fatal("pre-escape the box must not be flipped")
	}

	// Tick 2: SLIDE the window (drop the oldest, append the escape bar).
	e.Tick(bars[1:12], bars[11].OpenTime+59_999)
	latched = e.State.LatchedBoxes["ftgh"]
	if !latched.Flipped {
		t.Fatal("UR-FIX U1: the D14 flip must fire after a body escape (dev froze it at latch)")
	}
	if latched.FormedAt != 8 {
		t.Fatalf("UR-FIX U1: FormedAt = %d, want 8 (the formation bar's current index; dev kept the stale 9)", latched.FormedAt)
	}
	if latched.Touches != 1 {
		t.Fatalf("UR-FIX U1: touches = %d, want 1 (the touch bar is missed by the stale index; dev returns 0)", latched.Touches)
	}
}

// TestUrFixTrendlineKeyStableAcrossSlidingWindow — UR-FIX U2. The trendline
// level key embedded P0Idx/P1Idx, so when the window slid the key changed every
// tick and per-level state (Visits, LevelArms, ISBOnly, VisitCapRefused) never
// carried over. The fix keys on P0T/P1T (OpenTimes). RED on dev (the key
// changes after the slide); GREEN on the fix.
func TestUrFixTrendlineKeyStableAcrossSlidingWindow(t *testing.T) {
	bars, mk := trendlineBars() // P0@2 (97), P1@8 (98.2)
	// Pad 3 leading bars so the swings stay inside swings3's window after the
	// slide (P0 → index 5, P1 → index 11), then append one neutral bar.
	prepend := []market.Kline{
		mk(-3, 100, 101, 99.5, 100.5),
		mk(-2, 100, 101, 99.5, 100.5),
		mk(-1, 100, 101, 99.5, 100.5),
	}
	bars = append(prepend, bars...)
	bars = append(bars, mk(10, 100, 101, 99.5, 100)) // neutral: no new swing, no touch

	now := time.UnixMilli(bars[12].OpenTime + 59_999).In(ctime())
	k1 := trendlineKeyOf(t, bars[0:13], now)
	// Slide the window: drop the oldest bar, append the neutral one.
	now2 := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	k2 := trendlineKeyOf(t, bars[1:14], now2)

	if k1 == "" || k2 == "" {
		t.Fatalf("trendline keys must both build: %q / %q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("UR-FIX U2: trendline key changed across the slide: %q → %q (dev embeds indices)", k1, k2)
	}
}

func trendlineKeyOf(t *testing.T, bars []market.Kline, now time.Time) string {
	t.Helper()
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 {
		return ""
	}
	return tls[0].key()
}
