package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestM472TrendlineTwoBottomsReturnArmsOnDefault — the D3.2 two-bottom worked
// example. [Oct D3.2 @ 31:00-31:17] "Tầm 30 độ thì 2 điểm đụng là được rồi ...
// Em chỉ cần 2 touch là được mà" (OctD3.2-R44); [Oct D3.2 @ 41:20-41:27] "bên
// đây mình có một cái đáy ... thì nó đã được cấu tạo với 2 đáy rồi thì mình mới
// về được cái trendline ... xíu nữa nến nó chạy về đụng trendline" (OctD3.2-R59).
// Frame: /home/hoang/mm-course/oct/scenes/Ngày 3.2 Trendline - Việc đóng nến và
// trade tại levels/s_02485.jpg — two circled bottoms (~28,600 and ~28,620) and
// the line extended right (28,595.75 -> ~28,645.50); price returns to the line.
// The tape is trendlineReturnTape (two rising lows, a leave, then the RETURN
// touch on bar 13) shifted onto the 28,5xx level: the SHAPE of the D3.2 example
// with SYNTHETIC prices, not the frame's circled bottoms; only ValidAt==13 and
// the arming are asserted (the 28,595.75 -> ~28,645.50 line is not). A literal
// "2 touches define the line" is not implemented (ValidAt stays 3, as ordered).
// Through the production path (cfg := DefaultConfig(); the knob value the live
// eval hands TrendlineLevelsTouch) the return candle arms the line as a
// location; before m472 the default "fourth" refused it (third_touch_is_test)
// and waited for a fourth touch.
func TestM472TrendlineTwoBottomsReturnArmsOnDefault(t *testing.T) {
	bars, _ := trendlineReturnTape()
	const shift = 28500.0 // synthetic level near the frame's 28,6xx
	shifted := make([]market.Kline, len(bars))
	for i, b := range bars {
		b.Open += shift
		b.High += shift
		b.Low += shift
		b.Close += shift
		shifted[i] = b
	}
	bars = shifted
	now13 := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now13)
	if len(tls) != 1 || tls[0].ValidAt != 13 {
		t.Fatalf("fixture: two bottoms + the return must make one valid line at bar 13, got %+v", tls)
	}
	cfg := DefaultConfig()
	boxes := BoxesBuild(bars, DefaultBoxCfg(), now13)
	got, refused := TrendlineLevelsTouch(tls, boxes, nil, bars, cfg.TrendlineEntryTouch)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("default config (TrendlineEntryTouch=%q): the return to the 2-bottom line must arm the entry [Oct D3.2 @ 31:17, 41:27]; levels=%+v refused=%v", cfg.TrendlineEntryTouch, got, refused)
	}
}

// TestM472TrendlineFrameNumbersReturnArms — review round 2: the same lesson on the
// FRAME's own numbers [Oct D3.2 @ 41:20-41:27] OctD3.2-R59, frames
// /home/hoang/mm-course/oct/scenes/Ngày 3.2 Trendline - Việc đóng nến và trade tại levels/s_02462.jpg,
// s_02465.jpg, s_02469.jpg, s_02485.jpg: Sun 1 Nov 1m, bottoms at 17:10 (28,600) and
// 17:30 (28,620), the line extended right, price returns to it. The bot's line
// runs through the two bottoms (the on-screen 17:04 28,595.75 -> 18:17 ~28,645.50
// line is hand-drawn and approximate), so the return touch is at 17:50 (28,640).
// Caveat [B]: the lecture ties "2 points are enough" to a ~30 degree line
// (OctD3.2-R44), and whether he now drops the 3rd-touch validity is an OPEN
// question for the mentor (notes OctD3.2 OPEN QUESTIONS); ValidAt stays 3.
func TestM472TrendlineFrameNumbersReturnArms(t *testing.T) {
	t0 := time.Date(2026, time.November, 1, 17, 4, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)
	line := func(i int) float64 { return 28600 + float64(i-6) } // 17:10 -> 28,600, 17:30 -> 28,620
	var bars []market.Kline
	for i := 0; i <= 46; i++ {
		l := line(i)
		switch {
		case i == 6 || i == 26:
			bars = append(bars, mk(i, l+3, l+9, l, l+4)) // the circled bottoms
		case i == 40:
			bars = append(bars, mk(i, l+8, l+16, l+6, l+12)) // swing HIGH: the leave
		case i == 46:
			bars = append(bars, mk(i, l+4, l+9, l-0.25, l+6)) // the RETURN touches the line
		default:
			bars = append(bars, mk(i, l+7, l+10, l+5, l+8))
		}
	}
	now := time.UnixMilli(bars[46].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].ValidAt != 46 {
		t.Fatalf("two bottoms (28,600 / 28,620) + the return at 17:50 must make one valid line at bar 46, got %+v", tls)
	}
	cfg := DefaultConfig()
	boxes := BoxesBuild(bars, DefaultBoxCfg(), now)
	got, refused := TrendlineLevelsTouch(tls, boxes, nil, bars, cfg.TrendlineEntryTouch)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("default (TrendlineEntryTouch=%q): the return must arm; levels=%+v refused=%v", cfg.TrendlineEntryTouch, got, refused)
	}
}
