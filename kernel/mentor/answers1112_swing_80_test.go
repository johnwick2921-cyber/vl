package mentor

import (
	"testing"

	"vl/market"
)

// answers-1112 Q13 (REPORT-mentor-answers-1112.md, verdict corrected, medium):
// the largest swing (4H → 5M) stop he accepts is 70-80 pts, at 1-2 MNQ with a
// 1:1 target. mentor_swing_max_stop_pts (kernel SwingCfg.SkipStopPts) default
// 90 → 80; the revert value 90 reproduces today's behaviour exactly.
//
//   - [Sept D5.2 p2 @05:25-05:33] (large-v2 re-listen, part2.recovered2.txt;
//     the main .srt holds an invented "subscribe" line there): "có những lúc
//     mà cái nến 5 phút nó tới 7-80 điểm, 5-60 điểm cứ vô 1-2 [MNQ] cũng được
//     cho em, nhưng risk reward phải 1-1".
//   - Spoken over his own worked example, frame part2/f_000158: entry 28,689,
//     stop 28,615.75, "-71.75 (-0.25%) -287" — taken, it runs to 28,910.
//   - [Sept D5.2 p2 @19:41-19:44] "Một cái lệnh nào mà sắp lót cả 100 điểm /
//     Đừng vô"; [p2 @21:06-21:43] his own 91.75-pt stop is the one he skips.
//   - He names nothing between 80 and ~100. October gives no swing number
//     (its "don't trade the 5m, the stop is too big" [Oct D4.3 @07:58-08:27] is
//     an owner question, not built here), so September's number stands.
//
// The 60-80 pt size step (1-2 MNQ) is NOT built here — a follow-up.

// q13RejectShort builds the m463 reject-short tape whose stop (line + 30) sits
// `pts` above the entry (the touching candle's low).
func q13RejectShort(t *testing.T, stopLine, pts float64) []market.Kline {
	t.Helper()
	low := stopLine - pts
	return swingTape(t, []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, low+5, 10175, low, low+10),
	})
}

func q13Takes(t *testing.T, cfg SwingCfg, stopLine, pts float64) (taken bool, refusedMaxStop int) {
	t.Helper()
	s := &SwingState{}
	bars := q13RejectShort(t, stopLine, pts)
	es := m463Entries(SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000))
	return len(es) == 1, m463Refused(s, SwingRefusedMaxStop)
}

func TestAnswers1112SwingMaxStopDefaultIsEighty(t *testing.T) {
	if got := DefaultSwingCfg().SkipStopPts; got != 80 {
		t.Fatalf("SkipStopPts default = %v, want 80 [Sept D5.2 p2 @05:25-05:33 \"7-80 điểm\"]", got)
	}
	if got := DefaultConfig().Swing.SkipStopPts; got != 80 {
		t.Fatalf("DefaultConfig().Swing.SkipStopPts = %v, want 80", got)
	}
	// unset/0 resolves to the default (no OFF), now 80.
	if got := swingSkipStopPts(SwingCfg{}); got != 80 {
		t.Fatalf("swingSkipStopPts(0) = %v, want 80", got)
	}
}

func TestAnswers1112SwingEightyFromTheVideo(t *testing.T) {
	cfg := DefaultSwingCfg()
	line, _, ok := swingLine(swingTape(t, nil), mk5m(t, 15, 5, 5, 0, 0, 0, 0).OpenTime+60_000, cfg, ctime())
	if !ok {
		t.Fatal("line setup")
	}
	stopLine := line + 30

	cases := []struct {
		name          string
		pts           float64
		atDefault     bool // mentor_swing_max_stop_pts unset → 80
		atRevertNinty bool // mentor_swing_max_stop_pts = 90 → today
	}{
		{"f_000158 worked example 71.75 (28,689 / 28,615.75)", 71.75, true, true},
		{"80.00 exactly — the top of \"7-80\"", 80, true, true},
		{"80.25 — one tick over 80", 80.25, false, true},
		{"85 — between 80 and ~100, named by nobody", 85, false, true},
		{"90.00 — today's ceiling", 90, false, true},
		{"91.75 — his own rejected example", 91.75, false, false},
	}
	for _, c := range cases {
		taken, refused := q13Takes(t, cfg, stopLine, c.pts)
		if taken != c.atDefault {
			t.Errorf("%s: default (80) taken=%v, want %v", c.name, taken, c.atDefault)
		}
		if !c.atDefault && refused != 1 {
			t.Errorf("%s: default refusal must be NAMED swing_max_stop once, got %d", c.name, refused)
		}
		revert := cfg
		revert.SkipStopPts = 90
		taken, refused = q13Takes(t, revert, stopLine, c.pts)
		if taken != c.atRevertNinty {
			t.Errorf("%s: revert mentor_swing_max_stop_pts=90 taken=%v, want %v (today)", c.name, taken, c.atRevertNinty)
		}
		if !c.atRevertNinty && refused != 1 {
			t.Errorf("%s: revert refusal must be NAMED swing_max_stop once, got %d", c.name, refused)
		}
	}
}
