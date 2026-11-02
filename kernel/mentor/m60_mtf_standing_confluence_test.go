package mentor

import (
	"testing"

	"vl/market"
)

// m465 P1 (release #60) — D4.2-R17: mode C holds while the 15m AND the 5m both
// "say so" — "tại sao hồi sáng em lại không move stop loss mà em đặt stop loss
// ở dưới để cho nó chạy, tại vì đây nó cộng hưởng quá nhiều thứ... Khung 15
// phút nó cũng kêu mình làm vậy, khung 5 phút cũng kêu mình làm vậy" [D4.2 p1
// @14:57–15:37].
//
// WE-2 (Mon 14 Sep, D4.2 p1 @10:34–15:17): the 15m box 29,079.25 / 28,985.00
// (the 10:00 15m inside the 09:45 15m, long), the 5m ISB = the red 10:10 candle
// 29,048.50 / 29,017.25 (inside the green 10:05 → long), the 1m entry
// ~29,040, ran ~+100. Before #60 MTFAligned read only the LATEST closed 5m and
// 15m pairs, so the confluence was gone one 5m candle after the 5m ISB even
// though both boxes still stood.

// m60p1WE2 is the WE-2 morning through the 10:15 5m candle (which closes 29,050
// — just above the 5m box top, so (10:10, 10:15) is NOT an inside bar, while
// no 1m body leaves the box), then the 1m ISB 10:20 green / 10:21 inside.
func m60p1WE2() []market.Kline {
	bars := m60p1Tape([][6]float64{
		{9, 0, 28_900, 28_915, 28_895, 28_912},
		{9, 5, 28_912, 28_928, 28_908, 28_925},
		{9, 10, 28_925, 28_945, 28_920, 28_940},
		{9, 15, 28_950, 29_000, 28_945, 28_995},
		{9, 20, 28_995, 29_050, 28_990, 29_045},
		{9, 25, 29_045, 29_085, 29_040, 29_082},
		{9, 30, 29_086, 29_088, 29_030, 29_035},
		{9, 35, 29_035, 29_040, 28_980, 28_985},
		{9, 40, 28_985, 28_990, 28_945, 28_950},
		// the 09:45 15m mother: O28,941 H29,060 L28,938 C29,050 (green)
		{9, 45, 28_941, 29_000, 28_938, 28_995},
		{9, 50, 28_995, 29_030, 28_990, 29_025},
		{9, 55, 29_025, 29_060, 29_020, 29_050},
		// the 10:00 15m inside bar: O29,000 H29,079.25 L28,985 C29,025 → the WE-2 box
		{10, 0, 29_000, 29_030, 28_985, 29_025},
		{10, 5, 29_025, 29_079.25, 29_015, 29_045},
		{10, 10, 29_040, 29_048.50, 29_017.25, 29_025}, // the 5m ISB (red, inside the green 10:05)
		{10, 15, 29_030, 29_052, 29_028, 29_050},       // NOT an inside bar of 10:10
	})
	return append(bars,
		m60p1K(m60p1At(10, 20), 29_036, 29_046, 29_030, 29_044), // green mother
		m60p1K(m60p1At(10, 21), 29_041, 29_045, 29_034, 29_039), // inside → 1m ISB long
	)
}

func m60p1WE2Levels() []Level {
	return []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 28_850},
		{Key: "K-above", Kind: KindKeyLevel, Price: 29_300},
	}
}

// TestM60ConfluenceSurvivesNextFiveMinCandle — D4.2-R17 / WE-2: at 10:21 the
// standing 15m box (long, 28,985–29,079.25) and the standing 5m box (long,
// 29,017.25–29,048.50) both agree with a 1m LONG ISB → mode C
// (intent.Confluence). The latest closed 5m pair (10:10, 10:15) is not an
// inside bar, so the pre-#60 read (mtf_confluence_source = last_pair) says
// false — the mutant.
func TestM60ConfluenceSurvivesNextFiveMinCandle(t *testing.T) {
	run := func(source string) (*Evaluator, *Intent) {
		e := m60p1Eval(t, SideLong, 28_950, SideLong, SideLong, m60p1WE2Levels())
		e.Cfg.MTFConfluenceSource = source
		ins := m60p1Run(e, m60p1WE2())
		return e, m60p1ISB(ins, SideLong)
	}

	e, in := run(MTFConfluenceStandingBoxes)
	b15, b5 := e.State.ISBBox15m, e.State.ISBBox
	if b15 == nil || b15.Provisional || b15.Dir != SideLong || b15.High != 29_079.25 || b15.Low != 28_985 || b15.AtTime != m60p1At(10, 0) {
		t.Fatalf("precondition: the WE-2 15m box (long 28,985–29,079.25 @10:00, closed) must stand, got %+v", b15)
	}
	if b5 == nil || b5.Dir != SideLong || b5.High != 29_048.50 || b5.Low != 29_017.25 || b5.AtTime != m60p1At(10, 10) {
		t.Fatalf("precondition: the WE-2 5m box (long 29,017.25–29,048.50 @10:10) must stand, got %+v", b5)
	}
	if in == nil {
		t.Fatalf("the WE-2 1m long ISB must emit; refusals %v", e.State.Refusals)
	}
	if !in.Confluence {
		t.Fatalf("D4.2-R17: both standing boxes say long → mode C (Confluence) must hold, got %+v", *in)
	}

	e, in = run(MTFConfluenceLastPair)
	if in == nil {
		t.Fatalf("last_pair: the ISB itself must still emit; refusals %v", e.State.Refusals)
	}
	if in.Confluence {
		t.Fatal("last_pair mutant: the latest 5m pair (10:10, 10:15) is no inside bar → no confluence (the pre-#60 read)")
	}
}

// TestM60MTFConfluenceStandingUnit — the predicate on its own.
func TestM60MTFConfluenceStandingUnit(t *testing.T) {
	b15 := &ISBBox{High: 29_079.25, Low: 28_985, Dir: SideLong, AtTime: 1_000}
	b5 := &ISBBox{High: 29_048.50, Low: 29_017.25, Dir: SideLong, AtTime: 2_000}
	cases := []struct {
		name                string
		side                Side
		box5, box15         *ISBBox
		at                  int64
		lastDir, lastEscape Side
		want                bool
	}{
		{"both standing, same side", SideLong, b5, b15, 2_000, SideLong, "", true},
		{"entry against both", SideShort, b5, b15, 2_000, SideLong, "", false},
		{"no 15m box", SideLong, b5, nil, 2_000, SideLong, "", false},
		{"provisional 15m never counts", SideLong, b5, &ISBBox{Dir: SideLong, AtTime: 1_000, Provisional: true}, 2_000, SideLong, "", false},
		{"5m box opposite", SideLong, &ISBBox{Dir: SideShort, AtTime: 2_000}, b15, 2_000, SideShort, "", false},
		{"5m escaped the 15m way after the 15m box", SideLong, nil, b15, 2_000, SideLong, SideLong, true},
		{"5m escaped against", SideLong, nil, b15, 2_000, SideLong, SideShort, false},
		{"5m box older than the 15m box", SideLong, nil, b15, 500, SideLong, SideLong, false},
		{"no 5m read at all", SideLong, nil, b15, 0, "", "", false},
	}
	for _, c := range cases {
		if got := MTFConfluenceStanding(c.side, c.box5, c.box15, c.at, c.lastDir, c.lastEscape); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
