package mentor

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// §4.1 box tests. Geometry and helpers are synthetic; the golden test runs
// the real builder on the course frame's 13-Sep-2026 1m tape.

// TestHeikinAshiHandWorked — the 2025-extra TF option, hand-worked:
// HAclose = (O+H+L+C)/4; HAopen = (prev HAopen + prev HAclose)/2;
// HAhigh/HAlow take the extremes with them.
func TestHeikinAshiHandWorked(t *testing.T) {
	bars := []market.Kline{
		{Open: 10, High: 12, Low: 9, Close: 11},
		{Open: 11, High: 13, Low: 10, Close: 12},
	}
	ha := HeikinAshi(bars)
	// bar 1: close (10+12+9+11)/4 = 10.5; first open (10+11)/2 = 10.5
	if ha[0].Close != 10.5 || ha[0].Open != 10.5 || ha[0].High != 12 || ha[0].Low != 9 {
		t.Fatalf("HA[0] = %+v, want o=10.5 c=10.5 h=12 l=9", ha[0])
	}
	// bar 2: close (11+13+10+12)/4 = 11.5; open (10.5+10.5)/2 = 10.5
	if ha[1].Close != 11.5 || ha[1].Open != 10.5 || ha[1].High != 13 || ha[1].Low != 10 {
		t.Fatalf("HA[1] = %+v, want o=10.5 c=11.5 h=13 l=10", ha[1])
	}
}

// TestFTGHBoxFromPairGeometry — "râu ở cái đỉnh cao nhất, và cái BODY của
// cái đỉnh GẦN NHẤT" [Cách vẽ box @ 03:29].
func TestFTGHBoxFromPairGeometry(t *testing.T) {
	bars := []market.Kline{
		{Open: 96, High: 105, Low: 95, Close: 104},  // extreme high: wick 105
		{Open: 101, High: 103, Low: 99, Close: 102}, // nearest high: upper body 102
	}
	b := boxFromPair(bars, swingPairAt{kind: kernel.KindSWGH, price: 105, idx: 0}, swingPairAt{kind: kernel.KindSWGH, price: 103, idx: 1})
	if b == nil || b.Top != 105 || b.Bottom != 102 {
		t.Fatalf("FTGH = %+v, want [102, 105]", b)
	}
}

// TestFTGLBoxFromPairGeometry — "Mình lấy cái râu thấp nhất… của cái đáy
// trước đó… kéo lên tới… cái BODY của cái đáy GẦN NHẤT" [@ 02:16–02:41].
func TestFTGLBoxFromPairGeometry(t *testing.T) {
	bars := []market.Kline{
		{Open: 99, High: 101, Low: 92, Close: 94}, // extreme low: wick 92
		{Open: 95, High: 97, Low: 93, Close: 96},  // nearest low: lower body 95
	}
	b := boxFromPair(bars, swingPairAt{kind: kernel.KindSWGL, price: 92, idx: 0}, swingPairAt{kind: kernel.KindSWGL, price: 93, idx: 1})
	if b == nil || b.Bottom != 92 || b.Top != 95 {
		t.Fatalf("FTGL = %+v, want [92, 95]", b)
	}
}

// TestEscapeDetection — "ESCAPE = the candle's BODY outside (whole candle
// better)" [D3.2 p1 @ 19:05]. The helper is pure detection: an escape
// re-arms trading and does NOT delete the box [D3.2 p1 @ 18:30–19:30];
// nothing in the builder consults it for deletion (TestBoxSurvivesEscape).
func TestEscapeDetection(t *testing.T) {
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	if escaped([]market.Kline{{Open: 103, High: 104.5, Low: 102.5, Close: 103, CloseTime: 1}}, b, -1) {
		t.Fatal("a candle inside the zone is not an escape")
	}
	if !escaped([]market.Kline{{Open: 106, High: 108, Low: 105.5, Close: 107, CloseTime: 1}}, b, -1) {
		t.Fatal("a candle whose whole body is above the FTGH must read as an escape")
	}
	if escaped([]market.Kline{{Open: 103, High: 109, Low: 102.5, Close: 104, CloseTime: 1}}, b, -1) {
		t.Fatal("a wick beyond with the body inside is not an escape")
	}
}

// TestBoxSurvivesEscape — B4 (10-03 ruling): a box is NEVER deleted intraday
// ("vẽ rồi thì để y nguyên đó tới cuối ngày" [D4.1 p2 @02:39–03:09]); an
// escaped body only re-arms trading [D3.2 p1 @ 18:30–19:30]. The fixture's
// candle 6 closes with its whole body above the FTGH [104, 106] (open 106.2,
// close 106.4) — the pre-fix code dropped the box right there. Mutant
// (re-insert the escaped() drop in BoxesBuild) -> RED.
func TestBoxSurvivesEscape(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 105, 100, 104), // swing high @2 — pairs with @4
		mk(3, 103, 104, 102, 103),
		mk(4, 102, 106, 101, 105), // swing high @4 — the extreme
		// B10 T1 ruling (20:52Z): bar 5's high was 107, above the "extreme"
		// 106 — on a real chart 106 is then not the top. 105.8 keeps bar 4 the
		// true top and still registers the SWGL (low 99).
		mk(5, 105, 105.8, 99, 100),
		mk(6, 106.2, 106.5, 105, 106.4), // ESCAPE: body fully above 106
		// The escape candle's wick 106.5 is a higher high; bar 7's high 106.6
		// stops the NEXT-bar fractal confirmation so the escape candle never
		// becomes a new swing — the box must stay [104, 106].
		mk(7, 106, 106.6, 105.5, 106.1),
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	var ftgh *Box
	for i := range boxes {
		if boxes[i].Kind == FTGH {
			b := boxes[i]
			ftgh = &b
		}
	}
	if ftgh == nil {
		t.Fatal("FTGH deleted on escape — a box is never deleted intraday [D3.2 p1 @ 18:30–19:30; D4.1 p2 @ 02:39–03:09]")
	}
	if ftgh.Top != 106 || ftgh.Bottom != 104 {
		t.Fatalf("FTGH = [%.2f, %.2f], want [104, 106]", ftgh.Bottom, ftgh.Top)
	}
}

// TestBoxThirdTouchCounting — the 3rd touch is the TRADE [D3.2 p1 @ 04:29;
// D3.4 p3 @ 07:02]; the box is valid by construction [D3.2 p2 @ 03:45].
func TestBoxThirdTouchCounting(t *testing.T) {
	cfg := DefaultBoxCfg()
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	bars := []market.Kline{
		{},
		{High: 105.1, Low: 103, Close: 103.5, CloseTime: 1},
		{High: 104, Low: 102.5, Close: 103, CloseTime: 2},
		{High: 105.2, Low: 103, Close: 104, CloseTime: 3},
		{High: 105.0, Low: 103, Close: 103.5, CloseTime: 4},
	}
	if n := countBoxTouches(bars, b, 0, cfg); n != 3 {
		t.Fatalf("touches = %d, want 3", n)
	}
}

// TestInsideAnyBoxBan — "NEVER trade inside the box — 'hoàn toàn không'"
// [D3.2 p1 @ 06:59].
func TestInsideAnyBoxBan(t *testing.T) {
	boxes := []Box{{Kind: FTGH, Top: 105, Bottom: 102}, {Kind: FTGL, Top: 95, Bottom: 92}}
	if !InsideAnyBox(boxes, 103.5) || !InsideAnyBox(boxes, 93) {
		t.Fatal("price inside either zone must be banned")
	}
	if InsideAnyBox(boxes, 100) {
		t.Fatal("price between the boxes is not inside either")
	}
}

// TestBoxEdgeLocationsExport — one location per edge for DS-103's location
// gate.
func TestBoxEdgeLocationsExport(t *testing.T) {
	loc := BoxEdgeLocations([]Box{{Kind: FTGH, Top: 105, Bottom: 102, Key: "ftgh:105:102"}})
	if len(loc) != 2 || loc[0].Kind != KindFTGHEdge || loc[1].Kind != KindFTGLEdge || loc[0].Price != 105 || loc[1].Price != 102 {
		t.Fatalf("locations = %+v, want ftgh_edge@105 + ftgl_edge@102", loc)
	}
}

// TestBoxesNoFloorOnOneWayDecline — B10 T1 (CTO 2026-10-03): a steady
// decline is a one-way tape; every bar is a LEFT-only low, but the NEXT bar
// never has a higher low, so nothing is confirmed and no FTGL may be drawn
// (mirror: a steady rise draws no FTGH). Mutant (drop the confirmation) →
// RED.
func TestBoxesNoFloorOnOneWayDecline(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	decline := make([]market.Kline, 0, 20)
	for i := 0; i < 20; i++ {
		decline = append(decline, mk(i, 220-2*float64(i), 221-2*float64(i), 218-2*float64(i), 219-2*float64(i)))
	}
	now := time.UnixMilli(decline[len(decline)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(decline, cfg, now)
	for _, b := range boxes {
		if b.Kind == FTGL {
			t.Fatalf("steady decline drew FTGL %+v — a one-way tape confirms no swing low", b)
		}
	}
	if len(boxes) != 0 {
		t.Fatalf("steady decline drew %d boxes, want 0", len(boxes))
	}

	rise := make([]market.Kline, 0, 20)
	for i := 0; i < 20; i++ {
		rise = append(rise, mk(i, 200+2*float64(i), 202+2*float64(i), 199+2*float64(i), 201+2*float64(i)))
	}
	nowR := time.UnixMilli(rise[len(rise)-1].OpenTime + 60_000).In(ctime())
	for _, b := range BoxesBuild(rise, cfg, nowR) {
		if b.Kind == FTGH {
			t.Fatalf("steady rise drew FTGH %+v — a one-way tape confirms no swing high", b)
		}
	}

	// CTO probe 20:52Z, verbatim: overlapping 1m candles — every bar makes a
	// lower high AND a lower low, but each close sits ABOVE the prior bar's
	// low, as real 1m bars do. Still a one-way tape: the next bar's low is
	// never higher, so no swing low is confirmed and no FTGL may exist.
	// (Mutant: revert the confirmation to the close test — this pin goes RED.)
	overlap := make([]market.Kline, 0, 20)
	for i := 0; i < 20; i++ {
		overlap = append(overlap, mk(i, 220-2*float64(i), 221-2*float64(i), 215-2*float64(i), 218-2*float64(i)))
	}
	nowO := time.UnixMilli(overlap[len(overlap)-1].OpenTime + 60_000).In(ctime())
	for _, b := range BoxesBuild(overlap, cfg, nowO) {
		if b.Kind == FTGL {
			t.Fatalf("overlapping decline drew FTGL %+v — the next bar never has a higher low", b)
		}
	}
}

// TestBoxesDeclineOneBounceDrawsOneFloor — B10 T1 (CTO ruling 21:00Z): a
// decline that ends with ONE bounce bar draws exactly one FTGL, with the
// bottom as the outer edge. The last decline low is confirmed by the bounce
// bar (its low is higher) and becomes the extreme; the bar before it is the
// nearest.
func TestBoxesDeclineOneBounceDrawsOneFloor(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 110, 111, 108, 109),
		mk(1, 109, 109.5, 106, 107),
		mk(2, 107, 107.5, 104, 105),     // the nearest low — left-only, no confirmation
		mk(3, 105, 105.5, 103, 104),     // the extreme low — the spike
		mk(4, 104.5, 106, 103.5, 105.5), // the ONE bounce bar: low 103.5 > 103 confirms the extreme
		mk(5, 105, 106, 104, 105),
		mk(6, 105, 105.5, 104.2, 105),
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d (%+v), want exactly one FTGL [103, 105]", len(boxes), boxes)
	}
	b := boxes[0]
	if b.Kind != FTGL || b.Bottom != 103 || b.Top != 105 {
		t.Fatalf("box = %+v, want FTGL bottom 103 (the outer edge) top 105", b)
	}
}

// TestBoxesRealFloorStillDrawn — B10 T1 (CTO 20:52Z): the fractal right side
// confirms REAL swings — lows 100 (bounce) then 101.5 (bounce) still draw
// the FTGL [100, 102.5].
func TestBoxesRealFloorStillDrawn(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 106, 107, 105, 106),
		mk(1, 105, 105.5, 101, 102),
		mk(2, 102, 103, 100, 101.5), // swing low 100 — next bar's low 102.5 confirms
		mk(3, 103, 104, 102.5, 103.5),
		mk(4, 103.5, 104, 103, 103.5),
		mk(5, 103, 103.8, 101.5, 102.5), // the nearest low — next bar's low 102 confirms
		mk(6, 102.5, 104, 102, 103.5),
		mk(7, 103, 104, 102.5, 103.5),
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d (%+v), want exactly the FTGL [100, 102.5]", len(boxes), boxes)
	}
	b := boxes[0]
	if b.Kind != FTGL || b.Bottom != 100 || b.Top != 102.5 {
		t.Fatalf("box = %+v, want FTGL bottom 100 top 102.5", b)
	}
}

// TestBoxesExtremeAmongTodayOnly — B10 T2 (CTO 2026-10-03): the slice holds
// up to 1500 1m bars (A9), so pairing against a YESTERDAY extreme and then
// dropping the box at the day check leaves a day whose low sits above
// yesterday's with no floor at all. The extreme must be chosen among TODAY's
// swings: yesterday low 100, today a clean floor at 120 → the FTGL [120, 123]
// exists. Mutant (extreme over the whole slice) → RED.
func TestBoxesExtremeAmongTodayOnly(t *testing.T) {
	cfg := DefaultBoxCfg()
	day1 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	day2 := time.Date(2026, time.September, 16, 9, 4, 0, 0, ctime()).UnixMilli()
	mk := func(t0 int64, i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(day1, 0, 104, 105, 103, 104),
		mk(day1, 1, 103, 103.5, 101, 102),
		mk(day1, 2, 102, 102.5, 100, 101), // yesterday's extreme low 100
		mk(day1, 3, 101, 102, 100.5, 101.5),
		mk(day2, 0, 127, 128, 126, 127),
		mk(day2, 1, 127, 127, 122.5, 125),
		mk(day2, 2, 125, 126, 123, 124),
		mk(day2, 3, 124, 126, 120, 123), // today's swing low 120
		mk(day2, 4, 123, 125, 122, 124),
		mk(day2, 5, 124, 125, 123, 124),
		mk(day2, 6, 124, 124.5, 121.5, 123), // today's nearest low
		mk(day2, 7, 123, 124, 122, 123.5),
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d (%+v), want exactly the today FTGL [120, 123]", len(boxes), boxes)
	}
	b := boxes[0]
	if b.Kind != FTGL || b.Bottom != 120 || b.Top != 123 {
		t.Fatalf("box = %+v, want FTGL bottom 120 top 123", b)
	}
}

// TestBoxTopTwoCandleNeverWalked — B10 T3 (CTO 2026-10-03): in a normal FTGH
// the extreme (top 1) forms FIRST and the nearest (top 2) LATER. The box
// exists only once both tops are known, so the return walk must start AFTER
// the later of the two — the top-2 candle closes back below the bottom and
// WOULD trade (reject short) if walked; it must not be, and the first return
// is the NEXT visit. Mutant (FormedAt = the extreme idx) → RED.
func TestBoxTopTwoCandleNeverWalked(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100.5),
		mk(1, 100, 103, 99, 101),
		mk(2, 104, 106, 102, 104.5),     // top 1: the extreme high
		mk(3, 104, 105, 103.5, 104),     // closes above the box bottom
		mk(4, 104, 104.5, 102.5, 103.2), // closes back below the bottom
		mk(5, 103.5, 106, 102.5, 103.2), // top 2: the nearest — wick 106, closes below bottom
		mk(6, 102.5, 103.5, 101, 102.2), // the NEXT visit: touch while the spell is open
		mk(7, 102.5, 103, 102, 102.5),   // no touch
		mk(8, 102, 103.6, 101.5, 102.6), // a later visit
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	var b *Box
	for i := range boxes {
		if boxes[i].Kind == FTGH {
			bb := boxes[i]
			b = &bb
		}
	}
	if b == nil {
		t.Fatal("no FTGH built")
	}
	if b.Top != 106 || b.Bottom != 103.5 {
		t.Fatalf("FTGH = [%.2f, %.2f], want [103.5, 106]", b.Bottom, b.Top)
	}
	if b.FormedAt != 5 {
		t.Fatalf("FormedAt = %d, want 5 (the later of the extreme and the nearest)", b.FormedAt)
	}
	// The production call site (eval.go) walks incrementally from
	// FormedAt+1 via BoxReturnBarsFrom; mirror that exact shape.
	ret := BoxReturnBarsFrom(bars, *b, b.FormedAt+1, cfg)
	if len(ret) == 0 {
		t.Fatal("no return visit at all — the next visit after the top-2 candle must be return 1")
	}
	if ret[0].RefBar != 6 {
		t.Fatalf("first return RefBar = %d (%+v), want 6 — the next visit after the top-2 candle", ret[0].RefBar, ret)
	}
	for _, r := range ret {
		if r.RefBar <= b.FormedAt {
			t.Fatalf("return RefBar %d <= FormedAt %d — a formation candle was walked", r.RefBar, b.FormedAt)
		}
	}
	// The top-2 candle is entry-eligible: walked, it would trade. With the
	// correct FormedAt the walk never reaches it.
	c := DefaultConfig()
	c.Enabled = true
	c.RoomMultiple = 0.05
	c.LocTriggerFilter = false
	levels := []Level{{Key: "k", Kind: KindKeyLevel, Price: 100}}
	out := boxEntryIntent(bars[5], *b, []Box{*b}, levels, TriggerLine{}, c)
	if len(out) != 1 || out[0].Action != PlaceStopEntry || out[0].Side != SideShort {
		t.Fatalf("top-2 candle eligibility = %+v, want one SHORT stop entry (proves the walk must exclude it)", out)
	}
}

// TestBoxesFormedAtBornOnConfirmation — B10 T1 FOLD (CTO 21:16Z): when the
// EXTREME is the LATER pairing candle, FormedAt must be extreme+1 (the box
// is born when the confirming bar closes), and the confirming bar is never
// walked as a return. Mutant M3 (FormedAt = seq[extreme].idx, drop the +1)
// → RED: FormedAt becomes 5 and the confirming bar is walked as return 1.
func TestBoxesFormedAtBornOnConfirmation(t *testing.T) {
	cfg := DefaultBoxCfg()
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 110, 111, 108, 109),
		mk(1, 109, 110, 107, 108),
		mk(2, 108, 108.5, 104, 105),
		mk(3, 105, 106, 103, 104.5),
		mk(4, 104, 104.5, 101.5, 103),   // the nearest low (left-only, unconfirmed)
		mk(5, 103.5, 104, 100, 103.6),   // the EXTREME — the later pairing candle; closes outside
		mk(6, 102, 103.2, 101.5, 102.2), // the confirming bar (higher low) — touches while outside
		mk(7, 102.5, 104, 102, 103.5),
		mk(8, 103, 104.5, 102.8, 104), // the first return visit
		mk(9, 104, 104.5, 103.2, 104.2),
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	boxes := BoxesBuild(bars, cfg, now)
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d (%+v), want exactly one FTGL [100, 103]", len(boxes), boxes)
	}
	b := boxes[0]
	if b.Kind != FTGL || b.Bottom != 100 || b.Top != 103 {
		t.Fatalf("box = %+v, want FTGL bottom 100 top 103", b)
	}
	if b.FormedAt != 6 {
		t.Fatalf("FormedAt = %d, want 6 = extreme idx 5 + 1 — born when the confirming bar closes", b.FormedAt)
	}
	ret := BoxReturnBarsFrom(bars, b, b.FormedAt+1, cfg)
	if len(ret) != 1 || ret[0].RefBar != 8 {
		t.Fatalf("returns = %+v, want exactly one return at RefBar 8", ret)
	}
	for _, r := range ret {
		if r.RefBar <= 6 {
			t.Fatalf("return RefBar %d — the confirming bar (6) was walked", r.RefBar)
		}
	}
}

// TestBoxesGolden13SepFrame — the course frame (D3.3 FTGH/FTGL
// part1_06-25.jpg, verified by the CTO): two 1m boxes on Sun 13 Sep 2026,
// FTGL ≈ 28,982 → 29,015 and FTGH ≈ 29,097 → 29,105. The builder must draw
// both (±1 pt) from the recorded db-copy tape. The 17:00 open candle is
// excluded (the frame's first candle is later).
func TestBoxesGolden13SepFrame(t *testing.T) {
	bars := loadFixture(t, "mnq_1m_2026-09-13_boxframe", "1m")
	start := bars[0].OpenTime + 3*60_000 // from 17:03 CT (rolling-3 needs 2 lead bars)
	trimmed := bars[3:]
	_ = start
	cfg := DefaultBoxCfg()
	now := time.UnixMilli(trimmed[len(trimmed)-1].OpenTime + 60_000)
	boxes := BoxesBuild(trimmed, cfg, now)
	var ftgl, ftgh *Box
	for i := range boxes {
		switch boxes[i].Kind {
		case FTGL:
			b := boxes[i]
			ftgl = &b
		case FTGH:
			b := boxes[i]
			ftgh = &b
		}
	}
	if ftgl == nil {
		t.Fatal("no FTGL box drawn on the golden frame")
	}
	if abs(ftgl.Bottom-28981) > 1 || abs(ftgl.Top-29015) > 1 {
		t.Fatalf("FTGL = [%.2f, %.2f], want ≈ [28981, 29015] ±1 (golden ~28,982 → 29,015)", ftgl.Bottom, ftgl.Top)
	}
	if ftgh == nil {
		t.Fatal("no FTGH box drawn on the golden frame")
	}
	if abs(ftgh.Top-29105) > 1 || abs(ftgh.Bottom-29097) > 1 {
		t.Fatalf("FTGH = [%.2f, %.2f], want ≈ [29097, 29105] ±1 (golden ~29,097 → 29,105)", ftgh.Bottom, ftgh.Top)
	}
	if len(boxes) > 2 {
		t.Fatalf("boxes = %d — 'Draw TWO zones, never a third' [D3.2 p2 @ 09:14]", len(boxes))
	}
}
