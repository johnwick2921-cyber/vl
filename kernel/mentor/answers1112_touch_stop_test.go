package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// answers-1112 Q7 (REPORT-mentor-answers-1112.md, verdict corrected, medium):
// two touch candles, the entry on the SECOND one. The stop follows the first
// candle — "Thì cái stop loss của anh chị bắt buộc phải đi theo cây đầu tiên…
// chứ không phải là đặt ở cây nến 2" [Oct D5.2 @19:01-19:25] — EXCEPT when the
// second candle's wick is longer: "Ngoại trừ nếu như mà cây râu của cây nến đỏ
// nó dài hơn cây nến xanh á, thì ok, thì mình đặt ở cây nến đỏ… mà cái stop
// loss nó bự hơn cây nến xanh thì đặt bà nó cây nến xanh đi, đặt cây nến đỏ
// đi" [Oct D5.2 @19:42-20:03, .check.txt 19:58-20:03]. The stop is never
// inside the further wick.
//
// Where the bot enters on candle 2 [A, read in code]: ONLY the swing leeway
// re-arm ("em sẽ du di là 2 cây nến" [Sept D5.2 p1 @15:37-15:51]; the same
// "du di" as Oct D5.2 @18:49 "em đã du di cho anh chị là được đặt lệnh sell
// stop ở cây nến 2"). A level-touch PHL/PLH always keeps candle 1 (touch.go
// TouchTick, the Thani variant declined), so a candle-1 entry is unchanged —
// the report's candle-1 extension is [B] and not built.
//
// His chart (frames s_00875 / s_01185, level 28,154.50): candle 2 (red) O
// 28,150.50 H 28,158.25 L 28,123.50 C 28,136.50 [A, header]; candle 1 (green)
// wick ≈ 28,161 [B, pixel estimate]. There candle 1 is the further one, so
// the exception does not fire on screen. Candle 1's open/low/close and the
// exception's longer candle-2 wicks are SYNTHETIC (labelled per case); the
// level is the 4h line here (seeded warm at 28,154.50).

const q7Line = 28154.50

// q7Tape: prev below the line, candle 1 (green, first touch, closes back
// below), candle 2 (red, touches, closes back below, does NOT fill candle 1's
// sell stop — its low stays above candle 1's low).
func q7Tape(t *testing.T, c1High, c2High float64) []market.Kline {
	t.Helper()
	return []market.Kline{
		m463Bar(t, 10, 15, 9, 0, 28100, 28110, 28095, 28105), // prev: below the line
		// candle 1 (green): SYNTHETIC O/L/C, high per case (≈28,161 on screen [B])
		m463Bar(t, 10, 15, 9, 5, 28125, c1High, 28120.00, 28150.00),
		// candle 2 (red): his header O/L/C [A]; high per case
		m463Bar(t, 10, 15, 9, 10, 28150.50, c2High, 28123.50, 28136.50),
	}
}

func q7Run(t *testing.T, cfg SwingCfg, c1High, c2High float64) ([]Intent, *SwingState) {
	t.Helper()
	return m463ExampleCfg(t, cfg, 10, 15, 9, q7Line, q7Tape(t, c1High, c2High))
}

func TestAnswers1112TouchStopCoverLaterWickDefault(t *testing.T) {
	d := DefaultSwingCfg()
	if !d.CoverLaterWick {
		t.Fatal("touch_stop_cover_later_wick default must be ON [Oct D5.2 @19:42-20:03]")
	}
	if d.LaterWickStopBufferPts != DefaultConfig().PHLStopBufferPts || d.LaterWickStopBufferPts != 1.0 {
		t.Fatalf("LaterWickStopBufferPts = %v, want the course's 1-pt stop ray (PHLStopBufferPts %v) [Oct D2.3 @46:20-47:20]",
			d.LaterWickStopBufferPts, DefaultConfig().PHLStopBufferPts)
	}
	if !DefaultConfig().Swing.CoverLaterWick {
		t.Fatal("DefaultConfig().Swing.CoverLaterWick must be ON")
	}
}

func TestAnswers1112TouchStopCoverLaterWickFromTheVideo(t *testing.T) {
	lineStop := q7Line + 30 // 28,184.50 — today's swing stop
	revert := DefaultSwingCfg()
	revert.CoverLaterWick = false
	cases := []struct {
		name           string
		c1High, c2High float64
		wantDefault    float64 // candle-2 entry stop at the default (0 = candle 2 refused)
		wantRevert     float64 // touch_stop_cover_later_wick=false (today)
		refusedDefault string  // the refusal candle 2 gets at the default
	}{
		{"his screen: candle 1 is the further wick (28,161 [B] vs 28,158.25 [A]) — the exception does not fire",
			28161, 28158.25, lineStop, lineStop, ""},
		{"SYNTHETIC: candle 2's wick further (28,170) but inside the line+30 stop — already covered",
			28161, 28170, lineStop, lineStop, ""},
		{"SYNTHETIC: candle 2's wick further AND beyond the line+30 stop (28,190) — stop at 28,191",
			28161, 28190, 28191, lineStop, ""},
		{"SYNTHETIC: candle 2's wick 28,210 — covered stop 28,211 is 87.50 pts > 80: swing_max_stop still refuses it",
			28161, 28210, 0, lineStop, SwingRefusedMaxStop},
	}
	for _, c := range cases {
		es, s := q7Run(t, DefaultSwingCfg(), c.c1High, c.c2High)
		if len(es) < 1 || es[0].Price != 28120.00 || es[0].Stop != lineStop {
			t.Fatalf("%s: candle 1 must arm the sell stop 28,120.00 with the line stop %.2f (unchanged); got %+v", c.name, lineStop, es)
		}
		switch {
		case c.wantDefault == 0:
			if len(es) != 1 {
				t.Errorf("%s: default: candle 2 must be refused; got %+v", c.name, es)
			}
			if m463Refused(s, c.refusedDefault) != 1 {
				t.Errorf("%s: default: candle 2 refusal must be NAMED %s once; got %+v", c.name, c.refusedDefault, s.refused)
			}
		case len(es) != 2 || es[1].Price != 28123.50 || es[1].Stop != c.wantDefault:
			t.Errorf("%s: default: candle 2 re-arm 28,123.50 must carry stop %.2f; got %+v", c.name, c.wantDefault, es)
		}
		res, _ := q7Run(t, revert, c.c1High, c.c2High)
		if len(res) != 2 || res[1].Price != 28123.50 || res[1].Stop != c.wantRevert {
			t.Errorf("%s: revert touch_stop_cover_later_wick=false: candle 2 stop %.2f (today); got %+v", c.name, c.wantRevert, res)
		}
	}
}

// TestAnswers1112TouchStopCandleOneEntryUnchanged — a candle-1 entry keeps
// today's stop even when candle 1's own wick pokes beyond line+30 (the
// candle-1 extension is [B], not built).
func TestAnswers1112TouchStopCandleOneEntryUnchanged(t *testing.T) {
	cur := []market.Kline{
		m463Bar(t, 10, 15, 9, 0, 28100, 28110, 28095, 28105),
		m463Bar(t, 10, 15, 9, 5, 28125, 28195, 28120, 28150), // SYNTHETIC: wick 40.50 above the line
	}
	es, _ := m463ExampleCfg(t, DefaultSwingCfg(), 10, 15, 9, q7Line, cur)
	if len(es) != 1 || es[0].Stop != q7Line+30 {
		t.Fatalf("candle-1 entry: stop must stay line+30 = %.2f; got %+v", q7Line+30, es)
	}
}

// TestAnswers1112TouchStopLongMirror — the support mirror: candle 2's low
// wick further below than candle 1's and below line−30 → stop 1 pt under it.
func TestAnswers1112TouchStopLongMirror(t *testing.T) {
	line := 28154.50
	cur := []market.Kline{
		m463Bar(t, 10, 15, 9, 0, 28210, 28215, 28200, 28205),     // prev: above the line
		m463Bar(t, 10, 15, 9, 5, 28185, 28189, 28148, 28160),     // candle 1: touches, closes back above (SYNTHETIC)
		m463Bar(t, 10, 15, 9, 10, 28160, 28186.50, 28118, 28172), // candle 2: low 28,118 < line−30 = 28,124.50, high below candle 1's high
	}
	es, _ := m463ExampleCfg(t, DefaultSwingCfg(), 10, 15, 9, line, cur)
	if len(es) != 2 || es[1].Side != SideLong || es[1].Price != 28186.50 || es[1].Stop != 28117 {
		t.Fatalf("long mirror: candle 2 re-arm 28,186.50 must carry stop 28,117.00 (its low − 1); got %+v", es)
	}
	off := DefaultSwingCfg()
	off.CoverLaterWick = false
	es, _ = m463ExampleCfg(t, off, 10, 15, 9, line, cur)
	if len(es) != 2 || es[1].Stop != line-30 {
		t.Fatalf("long mirror revert: stop line−30 = %.2f; got %+v", line-30, es)
	}
}

// TestAnswers1112TouchStopReplay0709 — the ONE candle-2 swing re-arm the knob
// changes on the 90-day replay (cmd/mentor-replay, data.db read-only; bars are
// the stored MNQ 09-26 5m rows, import-shifted to their open as the replay
// reads them) [A]: Thu 9 Jul, 4h line 29,675.87 (support, approached from
// above). Candle 1 (09:15) touches and closes back above — its reject is
// 157.88 pts, refused swing_max_stop. Candle 2 (09:20) wicks 62 pts under the
// line (L 29,613.75, below candle 1's 29,674.00) and closes back above: today
// its buy stop 29,714.25 is armed with the line stop 29,645.87 — 32 pts INSIDE
// its own wick. Covered, the stop is 29,612.75 = 101.50 pts → refused
// swing_max_stop ("100 điểm quá nhiều rủi ro", the R31 skip still applies).
func TestAnswers1112TouchStopReplay0709(t *testing.T) {
	line := 29675.868722935447
	cur := []market.Kline{
		m463Bar(t, 7, 9, 9, 10, 29850.25, 29854.75, 29785.00, 29803.75),
		m463Bar(t, 7, 9, 9, 15, 29803.25, 29803.75, 29674.00, 29690.75), // candle 1
		m463Bar(t, 7, 9, 9, 20, 29691.00, 29714.25, 29613.75, 29711.25), // candle 2
	}
	off := DefaultSwingCfg()
	off.CoverLaterWick = false
	es, s := m463ExampleCfg(t, off, 7, 9, 9, line, cur)
	if len(es) != 1 || es[0].Side != SideLong || es[0].Price != 29714.25 || abs(es[0].Stop-(line-30)) > 1e-9 {
		t.Fatalf("revert (today): candle 2 buy stop 29,714.25 with the line stop %.2f; got %+v", line-30, es)
	}
	if m463Refused(s, SwingRefusedMaxStop) != 1 {
		t.Fatalf("revert: candle 1's 157.88-pt reject must be refused swing_max_stop once: %+v", s.refused)
	}
	es, s = m463ExampleCfg(t, DefaultSwingCfg(), 7, 9, 9, line, cur)
	if len(es) != 0 {
		t.Fatalf("default: candle 2's covered stop 29,612.75 (101.50 pts) must be refused; got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedMaxStop); n != 2 {
		t.Fatalf("default: candle 1 AND candle 2 refused swing_max_stop, got %d: %+v", n, s.refused)
	}
	if w := s.refused[len(s.refused)-1].Why; !strings.Contains(w, "stop 29612.75 = 101.50 pts") || !strings.Contains(w, "touch_stop_cover_later_wick ON") {
		t.Fatalf("default: the refusal must name the covered stop 29,612.75 / 101.50 pts: %q", w)
	}
}
