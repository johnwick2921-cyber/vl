package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ISB-M1 (+ J-BOX-14, TL-M10) — the inside bar at a level its mother just broke,
// every level kind (scope all, the default). The bars are the tape's own (MNQ 12-26
// 1m, data.db read-only); the production replay of 11-16 and 10-30 is pinned in
// cmd/mentor-replay/isb_just_broken_test.go. These drive the evaluator hook the
// Tick calls for both the normal ISB (eval.go isbJustBrokenRefusal, normal) and the
// reverse ISB (the _rev call).

// m1Bar is a closed 1m candle opening at the UTC instant.
func m1Bar(utc time.Time, o, h, l, c float64) market.Kline {
	t := utc.UnixMilli()
	return market.Kline{OpenTime: t, CloseTime: t + 59_999, Open: o, High: h, Low: l, Close: c, Final: true}
}

func m1Cfg(scope string) Config {
	c := DefaultConfig()
	if scope != "" {
		c.ISBJustBrokenScope = scope
	}
	return c
}

func m1Hook(t *testing.T, scope string, side Side, jb justBrokenCtx) string {
	t.Helper()
	e := &Evaluator{Cfg: m1Cfg(scope)}
	return e.isbJustBrokenRefusal(side, jb)
}

func TestISBM1_Defaults(t *testing.T) {
	if got := DefaultConfig().ISBJustBrokenScope; got != JustBrokenScopeAll {
		t.Fatalf("ISBJustBrokenScope default %q, want %q (ISB-M1, Oct D3.3 @00:46-01:00)", got, JustBrokenScopeAll)
	}
}

// The 11-16 live trade (rows 608/609): 19:12 GREEN mother O 27,330.50 C 27,368.00
// closed up through key 27,343.5 and key 27,359; the 19:13 RED inside candle
// L 27,346.75 H 27,362.75 sits ON 27,359 [Oct D3.3 @00:46-01:00 "o giua cai level
// ... dung vo"]. The long side is right (mother colour); the place is refused.
// 27,343.5 was crossed too but is under the inside candle (not "on" it); 27,383.25
// was never crossed.
func TestISBM1_1116_InsideBarOnTheKeyLevelRefused(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 17, 1, m, 0, 0, time.UTC) } // 11-16 19:mm CST
	hist := []market.Kline{
		m1Bar(at(10), 27332.25, 27336.50, 27322.25, 27328.50),
		m1Bar(at(11), 27329.00, 27335.75, 27316.25, 27329.50),
		m1Bar(at(12), 27330.50, 27376.25, 27330.50, 27368.00), // mother
		m1Bar(at(13), 27361.75, 27362.75, 27346.75, 27359.00), // inside bar
	}
	formed := time.Date(2026, 11, 16, 21, 30, 0, 0, time.UTC).UnixMilli()
	levels := []Level{
		{Key: "key_level:27383.25", Kind: KindKeyLevel, Price: 27383.25, AtTime: formed},
		{Key: "key_level:27359.00", Kind: KindKeyLevel, Price: 27359.00, AtTime: formed},
		{Key: "key_level:27343.50", Kind: KindKeyLevel, Price: 27343.50, AtTime: formed},
	}
	jb := justBrokenCtx{hist: hist, levels: levels}
	if got := m1Hook(t, "", SideLong, jb); got != "isb_just_broken_level" {
		t.Errorf("default (all): 19:13 long ISB on key 27,359 got %q, want isb_just_broken_level", got)
	}
	if what := justBrokenAll(jb); !strings.Contains(what, "27359.00") {
		t.Errorf("the refusal names %q, want key 27,359 (the level inside the inside candle)", what)
	}
	// the reverse-ISB call site reads the same location (side is not the trigger)
	if got := m1Hook(t, "", SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("default (all): the reverse-ISB read of the same pair got %q, want isb_just_broken_level", got)
	}
	// revert: the D1 build reads boxes only -> the live arm
	if got := m1Hook(t, JustBrokenScopeBox, SideLong, jb); got != "" {
		t.Errorf("scope box (revert): got %q, want none (the live 19:14 arm)", got)
	}
	// a key level formed AFTER the mother cannot have been broken by it
	late := []Level{{Kind: KindKeyLevel, Price: 27359.00, AtTime: at(13).UnixMilli()}}
	if got := m1Hook(t, "", SideLong, justBrokenCtx{hist: hist, levels: late}); got != "" {
		t.Errorf("a level that did not exist at the break refused: %q", got)
	}
}

// Oct D3.3 @00:16-01:00, frame 00:50: Sun 01 Nov 21:18 CST, the RED inside candle
// O 28,769.75 H 28,771.75 L 28,765.00 C 28,765.00 under the 21:17 RED mother
// (O 28,773.00 C 28,770.25); "the yellow ascending trendline runs through that
// candle", "no dong o duoi level ... qua gan ... o giua cai level". The hand-drawn
// line is not on the tape, so it is reconstructed [B] as the frame shows it: rising
// 0.25 pt a minute, at 28,771.00 at 21:18 - through the mother's body and inside
// the 21:18 candle. (The bot draws no 1m line there; the replay refuses that ISB on
// isb_box_blocked anyway.)
func TestISBM1_D33_0101_2118TrendlineThroughTheInsideBarRefused(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 2, 3, m, 0, 0, time.UTC) } // 21:mm CST
	hist := []market.Kline{
		m1Bar(at(16), 28773.00, 28775.50, 28772.00, 28773.75),
		m1Bar(at(17), 28773.00, 28773.00, 28763.75, 28770.25), // mother
		m1Bar(at(18), 28769.75, 28771.75, 28765.00, 28765.00), // inside bar (frame 00:50)
	}
	p0 := time.Date(2026, 11, 2, 2, 26, 0, 0, time.UTC) // 20:26 CST, "drawn from ~20:25"
	p1 := at(10)
	line := Trendline{Side: SideLong, P0Px: 28771.00 - 0.25*52, P0T: p0.UnixMilli(), P1Px: 28771.00 - 0.25*8, P1T: p1.UnixMilli(), ValidAt: 1}
	jb := justBrokenCtx{hist: hist, trendlines: []Trendline{line}}
	if got := m1Hook(t, "", SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("default (all): the 21:18 short ISB on the trendline got %q, want isb_just_broken_level", got)
	}
	if got := m1Hook(t, JustBrokenScopeBox, SideShort, jb); got != "" {
		t.Errorf("scope box (revert): got %q, want none (trendlines were not read)", got)
	}
	// a dead (5m-broken) or not-yet-valid line is not a level
	dead := line
	dead.Dead = true
	notValid := line
	notValid.ValidAt = 0
	for _, l := range []Trendline{dead, notValid} {
		if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: hist, trendlines: []Trendline{l}}); got != "" {
			t.Errorf("dead=%v valid=%d line refused: %q", l.Dead, l.ValidAt, got)
		}
	}
}

// Oct D2.2 E9 (10-30 08:34, marked): the 08:32 mother O 28,747.25 C 28,714.75 closed
// through the 28,743.9 ascending trendline; the 08:33 inside bar (H 28,735.75) is
// CLEAR of the line, and the mentor sold 28,709.5 there. "tha la anh chi du in sai
// ba o xa level" (D3.3 @01:40): allowed.
func TestISBM1_E9AllowedClearOfTheLine(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 30, 13, m, 0, 0, time.UTC) } // 08:mm CDT
	hist := []market.Kline{
		m1Bar(at(31), 28768.75, 28776.00, 28738.00, 28748.50),
		m1Bar(at(32), 28747.25, 28751.00, 28710.75, 28714.75), // mother
		m1Bar(at(33), 28714.50, 28735.75, 28711.00, 28726.00), // inside bar
	}
	line := Trendline{Side: SideLong, P0Px: 28743.9 - 0.2*20, P0T: at(12).UnixMilli(), P1Px: 28743.9 - 0.2*5, P1T: at(27).UnixMilli(), ValidAt: 1}
	if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: hist, trendlines: []Trendline{line}}); got != "" {
		t.Errorf("E9 short ISB clear of the just-broken line refused: %q", got)
	}
}

// 10-30 FTGH box 28,812.50-28,819.00 (formed 08:29). Oct D3.3 @37:00-37:51: the
// 08:59 candle broke UP out of the box (price came from under it), "doi Insaba
// long" - the 09:00 inside candle (L 28,827.25) is wholly above it: allowed (the
// mentor's buy 28,860.25, D3.3 E6 entries). The D1 box read refused it.
// E7 (@38:10-39:52): 09:02 came straight back DOWN through the box, the 09:03
// inside bar just under it - break-and-reverse, refused although clear of the edge.
// The drop-away (@40:07-40:20, 09:13-09:15, ~100 pts under the box): allowed.
func TestISBM1_1030BoxBreakVersusBreakAndReverse(t *testing.T) {
	at := func(m int) time.Time {
		return time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC).Add(time.Duration(m) * time.Minute)
	}
	tape := map[int][4]float64{
		57: {28769.0, 28777.25, 28753.0, 28773.25},
		58: {28772.5, 28832.25, 28771.25, 28815.5},
		59: {28815.75, 28860.25, 28806.75, 28841.75},
		60: {28842.0, 28859.0, 28827.25, 28848.75},
		61: {28849.0, 28857.0, 28825.25, 28827.0},
		62: {28826.75, 28828.25, 28795.75, 28800.75},
		63: {28801.0, 28807.5, 28786.0, 28802.75},
		64: {28803.0, 28830.75, 28801.0, 28815.25},
		65: {28815.5, 28821.25, 28773.25, 28787.75},
		66: {28787.75, 28817.75, 28781.25, 28815.25},
		67: {28815.25, 28823.75, 28792.0, 28799.5},
		68: {28800.0, 28800.75, 28779.0, 28785.75},
		69: {28786.0, 28786.0, 28755.5, 28757.0},
		70: {28756.75, 28758.0, 28732.75, 28734.75},
		71: {28735.0, 28753.0, 28715.5, 28717.0},
		72: {28717.25, 28722.75, 28706.25, 28714.0},
		73: {28714.5, 28717.5, 28694.0, 28705.5},
		74: {28705.75, 28710.25, 28677.75, 28679.5},
		75: {28680.0, 28702.25, 28677.25, 28690.0},
	}
	upTo := func(last int) []market.Kline {
		var out []market.Kline
		for m := 57; m <= last; m++ {
			b := tape[m]
			out = append(out, m1Bar(at(m), b[0], b[1], b[2], b[3]))
		}
		return out
	}
	box := []Box{{Kind: FTGH, Top: 28819, Bottom: 28812.5, FormedAtMs: time.Date(2026, 10, 30, 13, 29, 0, 0, time.UTC).UnixMilli()}}

	// 09:00 long ISB above the box (mother 08:59)
	jb := justBrokenCtx{hist: upTo(60), boxes: box}
	if got := m1Hook(t, "", SideLong, jb); got != "" {
		t.Errorf("default (all): the 09:00 ISB long wholly above the box refused: %q [Oct D3.3 @37:00-37:51]", got)
	}
	if got := m1Hook(t, JustBrokenScopeBox, SideLong, jb); got != "isb_just_broken_level" {
		t.Errorf("scope box (revert): the D1 refusal of 09:00 is gone: %q", got)
	}
	// E7: 09:03 short ISB under the box (mother 09:02)
	jb = justBrokenCtx{hist: upTo(63), boxes: box}
	if got := m1Hook(t, "", SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("default (all): E7 break-and-reverse got %q, want isb_just_broken_level [Oct D3.3 @38:58]", got)
	}
	if what := justBrokenAll(jb); !strings.Contains(what, "reversed") {
		t.Errorf("E7 refused for %q, want the break-and-reverse", what)
	}
	// drop-away: 09:15 short ISB (mother 09:14), the box ~100 pts above
	jb = justBrokenCtx{hist: upTo(75), boxes: box}
	if got := m1Hook(t, "", SideShort, jb); got != "" {
		t.Errorf("default (all): the drop-away ISB far under the box refused: %q [Oct D3.3 @40:07-40:20]", got)
	}
}

// J-BOX-14: the break two candles back, the mother still ON the level (no move
// away), the inside bar on it -> refused (the D1 two-candle read missed it). Once a
// candle between the break and the inside bar was clear of the level, the inside
// bar is the return to it (the retest, Oct D3.3 @01:00 / @39:20) -> allowed. The
// inside bar's own wick reaching the level is never the retest.
func TestISBM1_JBox14BreakEarlierThanTheMother(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 3, 15, m, 0, 0, time.UTC) }
	lv := []Level{{Kind: KindKeyLevel, Price: 100, AtTime: at(0).UnixMilli()}}
	stillOn := []market.Kline{
		m1Bar(at(1), 103, 104, 100.5, 101),  // above, clear of 100
		m1Bar(at(2), 101, 101.5, 98, 98.5),  // the break (body through 100)
		m1Bar(at(3), 98.5, 100.5, 97, 97.5), // mother: still on 100, no cross
		m1Bar(at(4), 98, 100.25, 97.25, 98.75),
	}
	if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: stillOn, levels: lv}); got != "isb_just_broken_level" {
		t.Errorf("break 2 candles back, still on the level: got %q, want isb_just_broken_level (J-BOX-14)", got)
	}
	movedAway := []market.Kline{
		m1Bar(at(1), 103, 104, 100.5, 101),
		m1Bar(at(2), 101, 101.5, 98, 98.5),   // the break
		m1Bar(at(3), 98.5, 99.5, 96, 96.5),   // clear under 100: moved away
		m1Bar(at(4), 96.5, 100.5, 96.25, 99), // mother: back to the level (no cross)
		m1Bar(at(5), 98.5, 100.25, 97, 98),   // inside bar on 100
	}
	if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: movedAway, levels: lv}); got != "" {
		t.Errorf("price moved away and came back (the retest): got %q, want none", got)
	}
	// old extremes are read the same way; EMA / trigger lines are not levels here
	oe := []Level{{Kind: KindOldExtreme, Price: 100, AtTime: at(0).UnixMilli(), Role: OldLow}}
	if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: stillOn, levels: oe}); got != "isb_just_broken_level" {
		t.Errorf("old extreme: got %q, want isb_just_broken_level", got)
	}
	ema := []Level{{Kind: KindEMA9, Price: 100}}
	if got := m1Hook(t, "", SideShort, justBrokenCtx{hist: stillOn, levels: ema}); got != "" {
		t.Errorf("an EMA is not a broken level here: %q", got)
	}
	// knob off reverts everything
	off := DefaultConfig()
	off.ISBJustBrokenLevel = false
	if got := (&Evaluator{Cfg: off}).isbJustBrokenRefusal(SideShort, justBrokenCtx{hist: stillOn, levels: lv}); got != "" {
		t.Errorf("mentor_isb_just_broken_level=false: got %q, want none", got)
	}
}
