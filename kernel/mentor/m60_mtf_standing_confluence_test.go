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

// m60p1WE2 is the WE-2 morning through the 10:15 5m candle (which OPENS 29,050
// — just above the 5m box top, so (10:10, 10:15) is NOT an inside bar, while
// every 1m CLOSE stays inside the box: m466 G20, release #61, the box escape is
// a 1m close outside it), then the 1m ISB 10:20 green / 10:21 inside.
func m60p1WE2() []market.Kline {
	bars := m60p1Tape([][6]float64{
		// a steady rise into the 09:45 15m (no earlier top above the entry)
		{9, 0, 28_800, 28_818, 28_796, 28_815},
		{9, 5, 28_815, 28_834, 28_812, 28_830},
		{9, 10, 28_830, 28_850, 28_826, 28_846},
		{9, 15, 28_846, 28_866, 28_842, 28_862},
		{9, 20, 28_862, 28_882, 28_858, 28_878},
		{9, 25, 28_878, 28_900, 28_874, 28_896},
		{9, 30, 28_896, 28_915, 28_892, 28_912},
		{9, 35, 28_912, 28_930, 28_908, 28_926},
		{9, 40, 28_926, 28_945, 28_922, 28_940},
		// the 09:45 15m mother: O28,941 H29,060 L28,938 C29,050 (green)
		{9, 45, 28_941, 29_000, 28_938, 28_995},
		{9, 50, 28_995, 29_030, 28_990, 29_025},
		{9, 55, 29_025, 29_060, 29_020, 29_050},
		// the 10:00 15m inside bar: O29,000 H29,079.25 L28,985 C29,025 → the WE-2 box
		{10, 0, 29_000, 29_030, 28_985, 29_025},
		{10, 5, 29_025, 29_079.25, 29_015, 29_045},
		{10, 10, 29_040, 29_048.50, 29_017.25, 29_025}, // the 5m ISB (red, inside the green 10:05)
		{10, 15, 29_050, 29_052, 29_038, 29_040},       // NOT an inside bar of 10:10 (opens above it); every 1m close inside the box
	})
	return append(bars,
		m60p1K(m60p1At(10, 20), 29_036, 29_046, 29_030, 29_044), // green mother
		m60p1K(m60p1At(10, 21), 29_041, 29_045, 29_034, 29_039), // inside → 1m ISB long
	)
}

// m60p1WE2Levels: key levels far from the coil.
func m60p1WE2Levels() []Level {
	return []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 28_850},
		{Key: "K-above", Kind: KindKeyLevel, Price: 29_300},
	}
}

// TestM60ConfluenceSurvivesNextFiveMinCandle — D4.2-R17 / WE-2. Part 1, the
// real tick path on the WE-2 tape: at 10:21 the evaluator has built the 15m
// box (long 28,985–29,079.25, the 10:00 15m inside the 09:45, CLOSED) and the
// 5m box (long 29,017.25–29,048.50, the red 10:10 inside the green 10:05),
// both still standing, while the latest closed 5m pair (10:10, 10:15) is no
// inside bar. Mode C's read for a LONG entry is TRUE with the standing boxes
// and FALSE with the pre-#60 last_pair read (the mutant). Part 2, the emit
// path: a 1m long ISB with both boxes standing goes out with
// intent.Confluence; last_pair → no confluence on the same ISB.
//
// (The WE-2 coil itself sits inside the FTGH the tape draws — the bot never
// trades inside a box [D3.2 p1 @06:59] — so the emit half runs on the flat
// mtfFixture, where nothing but the MTF read decides the flag.)
func TestM60ConfluenceSurvivesNextFiveMinCandle(t *testing.T) {
	e := m60p1Eval(t, SideLong, 28_950, SideLong, SideLong, m60p1WE2Levels())
	bars := m60p1WE2()
	m60p1Run(e, bars)
	b15, b5 := e.State.ISBBox15m, e.State.ISBBox
	if b15 == nil || b15.Provisional || b15.Dir != SideLong || b15.High != 29_079.25 || b15.Low != 28_985 || b15.AtTime != m60p1At(10, 0) {
		t.Fatalf("the WE-2 15m box (long 28,985–29,079.25 @10:00, closed) must stand at 10:21, got %+v", b15)
	}
	// ISB5-F1 (merge default, OctD4.3-R59 [Oct D4.3 @ 58:00]): the 10:15 candle
	// closed inside the 10:10 box, so it merged in and the top grew to its high
	// 29,052 (newest: 29,048.50, the 10:10 candle alone).
	if b5 == nil || b5.Dir != SideLong || b5.High != 29_052 || b5.Low != 29_017.25 || b5.AtTime != m60p1At(10, 10) || b5.MergedTo != m60p1At(10, 15) {
		t.Fatalf("the WE-2 5m box (long 29,017.25–29,052 @10:10, merged with 10:15) must stand at 10:21, got %+v", b5)
	}
	now := BarCloseInstant(bars[len(bars)-1])
	cb5, cb15 := closedBuckets(bars, now, e.Cfg), closedBucketsTF(bars, 15, now)
	if p, c := cb5[len(cb5)-2], cb5[len(cb5)-1]; IsISB(p, c) || c.OpenTime != m60p1At(10, 15) {
		t.Fatalf("precondition: the latest closed 5m pair must be (10:10, 10:15) and NOT an inside bar, got %+v / %+v", p, c)
	}
	if !e.mtfConfluence(SideLong, cb5, cb15) {
		t.Fatal("D4.2-R17: both standing boxes say long → mode C must hold one 5m candle after the 5m ISB")
	}
	if e.mtfConfluence(SideShort, cb5, cb15) {
		t.Fatal("a short entry against both boxes is no confluence")
	}
	e.Cfg.MTFConfluenceSource = MTFConfluenceLastPair
	if e.mtfConfluence(SideLong, cb5, cb15) {
		t.Fatal("last_pair mutant: the latest 5m pair is no inside bar → the pre-#60 read says no confluence")
	}

	// Part 2 — the emit path stamps the flag.
	emit := func(source string) *Intent {
		f, fb, now := mtfFixture(t, false) // a long 1m ISB, flat 5m/15m candles
		f.Cfg.MTFConfluenceSource = source
		f.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: fb[0].OpenTime}
		f.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: fb[0].OpenTime}
		f.State.LastISBBoxAt, f.State.LastISBBoxDir = fb[0].OpenTime, SideLong
		in := m60p1ISB(f.Tick(fb, now), SideLong)
		if in == nil {
			t.Fatalf("%s: the long ISB must emit; refusals %v", source, f.State.Refusals)
		}
		return in
	}
	if in := emit(MTFConfluenceStandingBoxes); !in.Confluence {
		t.Fatalf("standing_boxes: the emitted ISB must carry Confluence (mode C), got %+v", *in)
	}
	if in := emit(MTFConfluenceLastPair); in.Confluence {
		t.Fatalf("last_pair: no closed 5m/15m ISB pair → no Confluence, got %+v", *in)
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
