package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ISB-M1 one-tick tolerance (owner order 2026-11-17 "build", after the 11-16
// night replay of #72; October gives no points distance — Oct D3.3 @00:46-01:00,
// @02:40). With ISBJustBrokenTolPts = tol > 0 a candle BODY crosses a level only
// when it went MORE than tol past it on BOTH ends; tol 0 is the exact #72 read.
// The production replay of the night is pinned in
// cmd/mentor-replay/isb_just_broken_tick_test.go.

func tickHook(tol float64, side Side, jb justBrokenCtx) string {
	c := DefaultConfig()
	c.ISBJustBrokenTolPts = tol
	return (&Evaluator{Cfg: c}).isbJustBrokenRefusal(side, jb)
}

func TestISBM1Tick_Default(t *testing.T) {
	if got := DefaultConfig().ISBJustBrokenTolPts; got != 0.25 || ISBJustBrokenTolDefault != 0.25 {
		t.Fatalf("ISBJustBrokenTolPts default %v, want 0.25 (one MNQ tick, owner 2026-11-17)", got)
	}
}

// The cross predicate edge cases on a flat key level 100: a clear candle above
// it, the mother, then an inside bar sitting ON 100. The short refuses only on
// a DOWN cross by the mother.
func TestISBM1Tick_CrossPredicateEdges(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 3, 15, m, 0, 0, time.UTC) }
	lv := []Level{{Kind: KindKeyLevel, Price: 100, AtTime: at(0).UnixMilli()}}
	short := func(mo, mc float64) justBrokenCtx {
		return justBrokenCtx{levels: lv, hist: []market.Kline{
			m1Bar(at(1), 104, 105, 102, 103),                                 // clear above 100
			m1Bar(at(2), mo, math.Max(mo, mc)+0.5, math.Min(mo, mc)-0.5, mc), // mother
			m1Bar(at(3), 99.5, 100.5, 98.5, 99),                              // inside bar ON 100
		}}
	}
	long := func(mo, mc float64) justBrokenCtx {
		return justBrokenCtx{levels: lv, hist: []market.Kline{
			m1Bar(at(1), 96, 98, 95, 97), // clear under 100
			m1Bar(at(2), mo, math.Max(mo, mc)+0.5, math.Min(mo, mc)-0.5, mc),
			m1Bar(at(3), 100.5, 101.5, 99.5, 101),
		}}
	}
	const refused = "isb_just_broken_level"
	for _, c := range []struct {
		name string
		side Side
		jb   justBrokenCtx
		tol  float64
		want string
	}{
		// close side
		{"short: close exactly 0.25 through, tol 0.25 -> not a break", SideShort, short(103, 99.75), 0.25, ""},
		{"short: close 0.26 through, tol 0.25 -> a break", SideShort, short(103, 99.74), 0.25, refused},
		{"short: close 0.25 through, tol 0 (exact #72) -> a break", SideShort, short(103, 99.75), 0, refused},
		{"long: close exactly 0.25 through, tol 0.25 -> not a break", SideLong, long(97, 100.25), 0.25, ""},
		{"long: close 0.26 through, tol 0.25 -> a break", SideLong, long(97, 100.26), 0.25, refused},
		// open side
		{"short: open exactly AT the level, tol 0.25 -> not a break", SideShort, short(100, 97), 0.25, ""},
		{"short: open exactly AT the level, tol 0 (exact #72: Open == level counts) -> a break", SideShort, short(100, 97), 0, refused},
		{"short: open 0.25 above, tol 0.25 -> not a break", SideShort, short(100.25, 97), 0.25, ""},
		{"short: open 0.26 above, tol 0.25 -> a break", SideShort, short(100.26, 97), 0.25, refused},
		{"long: open exactly AT the level, tol 0.25 -> not a break", SideLong, long(100, 103), 0.25, ""},
		{"long: open exactly AT the level, tol 0 -> a break", SideLong, long(100, 103), 0, refused},
		{"long: open 0.25 below, tol 0.25 -> not a break", SideLong, long(99.75, 103), 0.25, ""},
		{"long: open 0.26 below, tol 0.25 -> a break", SideLong, long(99.74, 103), 0.25, refused},
		// a real break both ends
		{"short: open 3 above, close 1 under, tol 0.25 -> a break", SideShort, short(103, 99), 0.25, refused},
		// NaN / negative tolerances reach the kernel only by a direct Config write: the exact #72 read
		{"short: close 0.25 through, tol NaN -> the #72 read", SideShort, short(103, 99.75), math.NaN(), refused},
		{"short: close 0.25 through, tol -1 -> the #72 read", SideShort, short(103, 99.75), -1, refused},
	} {
		if got := tickHook(c.tol, c.side, c.jb); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// J-BOX-14 with a one-tick candle in between: the real break two candles back
// still refuses (a one-tick poke by the mother is "no cross", the walk goes on to
// the earlier break while price stays on the level).
func TestISBM1Tick_EarlierRealBreakStillRefuses(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 3, 15, m, 0, 0, time.UTC) }
	lv := []Level{{Kind: KindKeyLevel, Price: 100, AtTime: at(0).UnixMilli()}}
	hist := []market.Kline{
		m1Bar(at(1), 103, 104, 100.5, 101),     // clear above
		m1Bar(at(2), 101, 101.5, 98, 98.5),     // the real break: 1.00 above / 1.50 under
		m1Bar(at(3), 99.9, 100.4, 99.5, 100.2), // on the level, 0.2 back up (not a cross either way)
		m1Bar(at(4), 99.5, 100.25, 97.25, 98.75),
	}
	if got := tickHook(0.25, SideShort, justBrokenCtx{hist: hist, levels: lv}); got != "isb_just_broken_level" {
		t.Errorf("the earlier real break with price still on the level: got %q, want isb_just_broken_level (J-BOX-14)", got)
	}
}

// The 11-16 night, the tape's own bars (MNQ 12-26 1m, CST = UTC-6).
// 22:01 short (pos 819): mother 21:59 O 27,390.00 C 27,383.00 closed 0.25 under
// key 27,383.25; 21:58 (L 27,390.00) was clear above it. 05:17 long (pos 902):
// mother 05:15 O 27,255.25 C 27,261.25 opened 0.25 under key 27,255.50 and
// closed 5.75 OVER it (a one-tick OPEN, not a one-tick break: 05:13 closed 4
// under, 05:14 one tick under — the bar-seam case, owner ruling pending, see
// TestISBM1Tick_BarSeamPendingOwnerRuling); 05:13 (H 27,253.00) was clear under
// it. Default: not breaks. tol 0: refused (#72).
func TestISBM1Tick_1116NightKeyLevelOneTick(t *testing.T) {
	cst := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h+6, m, 0, 0, time.UTC) }
	formed := time.Date(2026, 11, 16, 23, 0, 0, 0, time.UTC).UnixMilli()
	k1 := []Level{{Kind: KindKeyLevel, Price: 27383.25, AtTime: formed}}
	h1 := []market.Kline{
		m1Bar(cst(16, 21, 58), 27390.50, 27397.25, 27390.00, 27390.75),
		m1Bar(cst(16, 21, 59), 27390.00, 27392.50, 27380.50, 27383.00), // mother
		m1Bar(cst(16, 22, 0), 27383.50, 27388.25, 27378.50, 27380.50),  // inside bar on 27,383.25
	}
	if got := tickHook(0.25, SideShort, justBrokenCtx{hist: h1, levels: k1}); got != "" {
		t.Errorf("22:01 short (close 0.25 through key 27,383.25), default: got %q, want none", got)
	}
	if got := tickHook(0, SideShort, justBrokenCtx{hist: h1, levels: k1}); got != "isb_just_broken_level" {
		t.Errorf("22:01 short, tol 0 (exact #72): got %q, want isb_just_broken_level", got)
	}
	k2 := []Level{{Kind: KindKeyLevel, Price: 27255.50, AtTime: formed}}
	h2 := []market.Kline{
		m1Bar(cst(17, 5, 13), 27244.75, 27253.00, 27241.00, 27251.50),
		m1Bar(cst(17, 5, 14), 27250.75, 27255.75, 27246.00, 27255.25),
		m1Bar(cst(17, 5, 15), 27255.25, 27266.50, 27251.75, 27261.25), // mother
		m1Bar(cst(17, 5, 16), 27261.75, 27270.00, 27252.25, 27265.75), // inside bar on 27,255.50
	}
	if got := tickHook(0.25, SideLong, justBrokenCtx{hist: h2, levels: k2}); got != "" {
		t.Errorf("05:17 long (open 0.25 under key 27,255.50, close 5.75 over it), default (both-ends read): got %q, want none", got)
	}
	if got := tickHook(0, SideLong, justBrokenCtx{hist: h2, levels: k2}); got != "isb_just_broken_level" {
		t.Errorf("05:17 long, tol 0 (exact #72): got %q, want isb_just_broken_level", got)
	}
	// the 19:14 long stays refused: mother 19:12 O 27,330.50 C 27,368.00 through
	// key 27,359 by 28.50 / 9.00
	k3 := []Level{{Kind: KindKeyLevel, Price: 27359.00, AtTime: formed - 6*3600_000}}
	h3 := []market.Kline{
		m1Bar(cst(16, 19, 11), 27329.00, 27335.75, 27316.25, 27329.50),
		m1Bar(cst(16, 19, 12), 27330.50, 27376.25, 27330.50, 27368.00),
		m1Bar(cst(16, 19, 13), 27361.75, 27362.75, 27346.75, 27359.00),
	}
	if got := tickHook(0.25, SideLong, justBrokenCtx{hist: h3, levels: k3}); got != "isb_just_broken_level" {
		t.Errorf("19:14 long (a 9-pt break of key 27,359), default: got %q, want isb_just_broken_level", got)
	}
}

// A sloping line the mother closed ~0.03 pt through (the 21:48 short, pos 813,
// trendline 27,434.78 [B]: the bot's line, reconstructed flat-ish here at the
// value the refusal named) is not a break by default; tol 0 refuses it.
func TestISBM1Tick_TrendlineHundredthsThrough(t *testing.T) {
	cst := func(h, m int) time.Time { return time.Date(2026, 11, 17, h+6-24, m, 0, 0, time.UTC) }
	hist := []market.Kline{
		m1Bar(cst(21, 45), 27438.00, 27445.00, 27435.50, 27441.25), // on/above the line, no cross
		m1Bar(cst(21, 46), 27441.25, 27443.00, 27421.75, 27434.75), // mother: close ~0.03 under the line
		m1Bar(cst(21, 47), 27434.00, 27436.00, 27425.50, 27430.25), // inside bar on the line
	}
	// a rising line, +0.01 pt a minute, at 27,434.78 at 21:47:00 (the mother's
	// close); anchored 30 and 60 minutes earlier so it exists (P1T) before the
	// candles read here
	t0 := cst(21, 47).UnixMilli()
	line := Trendline{Side: SideLong, P0Px: 27434.78 - 0.01*60, P0T: t0 - 60*60_000, P1Px: 27434.78 - 0.01*30, P1T: t0 - 30*60_000, ValidAt: 1}
	jb := justBrokenCtx{hist: hist, trendlines: []Trendline{line}}
	if v := line.priceAt(hist[1].CloseTime); math.Abs(v-27434.78) > 0.01 {
		t.Fatalf("line at the mother close %.4f, want ~27,434.78", v)
	}
	if got := tickHook(0.25, SideShort, jb); got != "" {
		t.Errorf("21:48 short, ~0.03 through the line, default: got %q, want none", got)
	}
	if got := tickHook(0, SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("21:48 short, tol 0 (exact #72): got %q, want isb_just_broken_level", got)
	}
}

// justBrokenBoxReverse: E7 (10-30 09:02 mother O 28,826.75 C 28,800.75 back down
// through the 28,812.50 bottom; 08:59 closed 22.75 over the 28,819.00 top; 08:57
// 39.25 under the bottom) stays refused at the default — every distance is far
// beyond one tick. A reverse whose mother poked out by ONE tick is not a reverse
// at the default, and is at tol 0.
func TestISBM1Tick_BoxReverse(t *testing.T) {
	at := func(m int) time.Time {
		return time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC).Add(time.Duration(m) * time.Minute)
	}
	box := Box{Kind: FTGH, Top: 28819, Bottom: 28812.5, FormedAtMs: time.Date(2026, 10, 30, 13, 29, 0, 0, time.UTC).UnixMilli()}
	e7 := []market.Kline{
		m1Bar(at(57), 28769.0, 28777.25, 28753.0, 28773.25),
		m1Bar(at(58), 28772.5, 28832.25, 28771.25, 28815.5),
		m1Bar(at(59), 28815.75, 28860.25, 28806.75, 28841.75),
		m1Bar(at(60), 28842.0, 28859.0, 28827.25, 28848.75),
		m1Bar(at(61), 28849.0, 28857.0, 28825.25, 28827.0),
		m1Bar(at(62), 28826.75, 28828.25, 28795.75, 28800.75),
		m1Bar(at(63), 28801.0, 28807.5, 28786.0, 28802.75),
	}
	if !justBrokenBoxReverse(SideShort, e7, box, 0.25) {
		t.Errorf("E7 at the default tolerance: not a break-and-reverse [Oct D3.3 @38:58 'Hoan toan khong duoc']")
	}
	if got := tickHook(0.25, SideShort, justBrokenCtx{hist: e7, boxes: []Box{box}}); got != "isb_just_broken_level" {
		t.Errorf("E7 hook at the default: got %q, want isb_just_broken_level", got)
	}
	if what := justBrokenAll(SideShort, justBrokenCtx{hist: e7, boxes: []Box{box}, tol: 0.25}); !strings.Contains(what, "reversed") {
		t.Errorf("E7 refused for %q, want the break-and-reverse", what)
	}
	// the mother closes only 0.25 under the bottom
	poke := append([]market.Kline(nil), e7[:5]...)
	poke = append(poke,
		m1Bar(at(62), 28826.75, 28828.25, 28811.0, 28812.25),
		m1Bar(at(63), 28812.5, 28814.0, 28811.5, 28813.0),
	)
	if justBrokenBoxReverse(SideShort, poke, box, 0.25) {
		t.Errorf("a mother 0.25 out of the box bottom is not a break at the default tolerance")
	}
	if !justBrokenBoxReverse(SideShort, poke, box, 0) {
		t.Errorf("tol 0 (exact #72): the 0.25 poke out of the box is a reverse")
	}
	// the earlier BREAK closed only 0.25 over the top: one traversal at the
	// default (no reverse), two at tol 0
	weak := []market.Kline{
		m1Bar(at(57), 28769.0, 28777.25, 28753.0, 28773.25),
		m1Bar(at(58), 28772.5, 28825.25, 28771.25, 28819.25), // 0.25 over the top
		m1Bar(at(59), 28819.0, 28822.0, 28813.0, 28816.0),
		m1Bar(at(60), 28816.0, 28817.0, 28795.75, 28800.75), // mother down out of the bottom
		m1Bar(at(61), 28801.0, 28807.5, 28786.0, 28802.75),
	}
	if justBrokenBoxReverse(SideShort, weak, box, 0.25) {
		t.Errorf("a 0.25 close over the top is not the break of a break-and-reverse at the default")
	}
	if !justBrokenBoxReverse(SideShort, weak, box, 0) {
		t.Errorf("tol 0 (exact #72): the 0.25 close over the top counts as the break")
	}
}

// #73 review P1 — OWNER RULING PENDING. This pins the rule AS BUILT (both ends),
// not a ruling. Futures bars open where the last one closed, so a move through a
// level split at a bar seam one tick from it is a break by NEITHER candle at the
// default, however far price went: A closes 0.25 through (no break: close side
// within the tolerance), B opens there and runs 20 pts on (no break: open side
// within the tolerance). Same with A closing exactly AT the level. tol 0 (#72)
// refuses both. The 11-17 05:17 long (pos 902) is this picture: 05:14 closed
// 27,255.25, one tick under key 27,255.50; the 05:15 mother opened 27,255.25 and
// closed 27,261.25, 5.75 over it.
//
// If the owner rules CLOSE-SIDE ONLY (a break = the close more than a tick
// through; the open read as #72), the two default rows below flip to refused,
// as do the 05:17 rows of TestISBM1Tick_1116NightKeyLevelOneTick, the
// open-side rows of TestISBM1Tick_CrossPredicateEdges and
// cmd/mentor-replay TestISBM1Tick_1116NightOneTickBreaksArm — and the guide.
func TestISBM1Tick_BarSeamPendingOwnerRuling(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 3, 15, m, 0, 0, time.UTC) }
	lv := []Level{{Kind: KindKeyLevel, Price: 100, AtTime: at(0).UnixMilli()}}
	for _, c := range []struct {
		name string
		hist []market.Kline
	}{
		{"A 126 -> 99.75 (0.25 under), mother B opens 99.75 and closes 80", []market.Kline{
			m1Bar(at(1), 130, 131, 125, 126),    // clear above
			m1Bar(at(2), 126, 127, 99.5, 99.75), // A
			m1Bar(at(3), 99.75, 101, 79, 80),    // mother B
			m1Bar(at(4), 85, 100.5, 81, 84),     // inside bar, wick up to the level
		}},
		{"A closes AT 100, mother B opens AT 100 and closes 80", []market.Kline{
			m1Bar(at(1), 130, 131, 125, 126),
			m1Bar(at(2), 126, 127, 99.75, 100),
			m1Bar(at(3), 100, 101, 79, 80),
			m1Bar(at(4), 85, 100.5, 81, 84),
		}},
	} {
		jb := justBrokenCtx{hist: c.hist, levels: lv}
		if got := tickHook(ISBJustBrokenTolDefault, SideShort, jb); got != "" {
			t.Errorf("%s: default (both ends, as built) got %q, want none — if the owner ruled close-side only, flip this pin", c.name, got)
		}
		if got := tickHook(0, SideShort, jb); got != "isb_just_broken_level" {
			t.Errorf("%s: tol 0 (exact #72) got %q, want isb_just_broken_level", c.name, got)
		}
	}
}

// #73 review P2 (both reviewers): at the knob's ceiling (ISBJustBrokenTolMax,
// two ticks) the closest pinned real breaks stay refused — October's D3.3 E1
// (01-Nov 21:18 "dung vo": mother 21:17 C 28,770.25 about 0.75 under the
// reconstructed line [B], TestISBM1_D33_0101_2118*) and the 11-16 19:58 short
// (pos 803 loser, 0.714 through the 27,367.08 line; production replay pin
// cmd/mentor-replay TestISBM1Tick_CeilingKeepsTheNightRealBreaks). The ceiling
// must stay under both margins.
func TestISBM1Tick_CeilingKeepsD33E1Refused(t *testing.T) {
	if ISBJustBrokenTolMax != 0.5 || ISBJustBrokenTolMax < ISBJustBrokenTolDefault {
		t.Fatalf("ISBJustBrokenTolMax = %v, want 0.5 (two ticks, under the 0.714 / ~0.75 real-break margins)", ISBJustBrokenTolMax)
	}
	at := func(m int) time.Time { return time.Date(2026, 11, 2, 3, m, 0, 0, time.UTC) } // 21:mm CST
	hist := []market.Kline{
		m1Bar(at(16), 28773.00, 28775.50, 28772.00, 28773.75),
		m1Bar(at(17), 28773.00, 28773.00, 28763.75, 28770.25), // mother
		m1Bar(at(18), 28769.75, 28771.75, 28765.00, 28765.00), // inside bar (frame 00:50)
	}
	p0 := time.Date(2026, 11, 2, 2, 26, 0, 0, time.UTC)
	line := Trendline{Side: SideLong, P0Px: 28771.00 - 0.25*52, P0T: p0.UnixMilli(), P1Px: 28771.00 - 0.25*8, P1T: at(10).UnixMilli(), ValidAt: 1}
	jb := justBrokenCtx{hist: hist, trendlines: []Trendline{line}}
	for _, tol := range []float64{0, ISBJustBrokenTolDefault, ISBJustBrokenTolMax} {
		if got := tickHook(tol, SideShort, jb); got != "isb_just_broken_level" {
			t.Errorf("D3.3 E1 21:18 short at tolerance %v: got %q, want isb_just_broken_level", tol, got)
		}
	}
	// the reconstructed close margin is 0.75: a tolerance of 0.75 releases E1 —
	// why the knob's ceiling must sit below it (the trader resolver refuses 0.75)
	if got := tickHook(0.75, SideShort, jb); got != "" {
		t.Errorf("D3.3 E1 at tolerance 0.75: got %q, want none (the reconstruction's margin is 0.75; if this moved, re-check the ceiling)", got)
	}
}
