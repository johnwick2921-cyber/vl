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
// touch on bar 13) shifted onto the 28,5xx MNQ price level of the frame.
// Through the production path (cfg := DefaultConfig(); the knob value the live
// eval hands TrendlineLevelsTouch) the return candle arms the line as a
// location; before m472 the default "fourth" refused it (third_touch_is_test)
// and waited for a fourth touch.
func TestM472TrendlineTwoBottomsReturnArmsOnDefault(t *testing.T) {
	bars, _ := trendlineReturnTape()
	const shift = 28500.0 // frame prices: bottoms ~28,600 / ~28,620
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
