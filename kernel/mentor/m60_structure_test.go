package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m465 P3 (release #60) — structure fidelity. Every test here pins a gap the
// full video read found (fleet-1017/out/video-check-1101/confirmed-gaps.md),
// written from the course example FIRST and seen RED before the fix.

// ── D2.3-R7: the 3rd touch is the TEST, the entry comes on a later touch ──────
//
// "thà nó về một lần, rồi nó test, rồi nó hold rồi, là lần thứ ba đó, thì lần
// thứ tư mới dám tự tin mình vô" [D2.3 p1 @02:34–02:47, 03:03–03:16]; D3.2-R34
// "test lần thứ 3 thì mới valid" [D3.2 p2 @02:43–04:11]. On trendlineReturnTape
// the line goes VALID on bar 13 (the 3rd touch, a return). On that candle the
// line must NOT be a location (refused trendline_third_touch_is_test); two quiet
// bars later a 4th touch finds it a location.
func TestM60TrendlineThirdTouchDoesNotArm(t *testing.T) {
	bars, mk := trendlineReturnTape()
	now13 := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now13)
	if len(tls) != 1 || tls[0].ValidAt != 13 {
		t.Fatalf("fixture: want the line valid at bar 13, got %+v", tls)
	}
	boxes := BoxesBuild(bars, DefaultBoxCfg(), now13)
	got, refused := TrendlineLevels(tls, boxes, nil, bars)
	if len(got) != 0 || len(refused) != 1 || !strings.HasPrefix(refused[0], "trendline_third_touch_is_test:") {
		t.Fatalf("the 3rd touch (bar 13) is the test, not the entry: levels=%+v refused=%v — want 0 levels, 1 trendline_third_touch_is_test refusal", got, refused)
	}

	// Two bars that LEAVE the line (lows 100.5, clear of the line 99.4–99.8
	// plus its 0.5 touch band — a low of 100 would still be inside the band,
	// i.e. a touch), then the 4th touch on bar 16 (low 99.7 vs the line's
	// 99.8, closes back above).
	bars = append(bars,
		mk(14, 101, 101.5, 100.5, 101),
		mk(15, 101, 101.5, 100.5, 101),
		mk(16, 101, 101.5, 99.7, 101.2),
	)
	now16 := time.UnixMilli(bars[16].OpenTime + 59_999).In(ctime())
	// Bar 13's low becomes a confirmed swing too, so newer 2-point lines exist
	// (not valid, never locations); the 3rd-touch line is still the one valid.
	tls = TrendlinesBuild(bars, now16)
	valid := 0
	for _, x := range tls {
		if x.ValidAt != 0 && !x.Dead {
			valid++
			if x.ValidAt != 13 {
				t.Fatalf("fixture: want the one valid line at 13, got %+v", tls)
			}
		}
	}
	if valid != 1 {
		t.Fatalf("fixture: want exactly one valid line on bar 16, got %+v", tls)
	}
	boxes = BoxesBuild(bars, DefaultBoxCfg(), now16)
	got, refused = TrendlineLevels(tls, boxes, nil, bars)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("the 4th touch: the line must be a location — levels=%+v refused=%v", got, refused)
	}
}

// TestM60TrendlineAdjacentTouchIsStillTheTest — D2.3-R7 "là lần thứ ba đó, thì
// lần thứ tư mới dám tự tin mình vô" [D2.3 p1 @02:34–02:47, 03:03–03:16]: the
// 4th touch is a NEW touch — price must leave the line and come back. A candle
// right after the 3rd touch that still sits on the line (bar 14, low 99.7 vs
// the line's ~99.4–99.6, closes above) is the same 3rd-touch cluster, not the
// 4th touch: refused trendline_third_touch_is_test. Once a candle clears the
// line (+ its 0.5 band) and a later one comes back, the line is a location.
// Short mirror too.
// MUTANT: refuse only on ValidAt == lastIdx (the pre-review one-candle delay)
// → bar 14 is a location → RED.
func TestM60TrendlineAdjacentTouchIsStillTheTest(t *testing.T) {
	for _, mirror := range []bool{false, true} {
		bars, mk := trendlineReturnTape()
		bars = append(bars, mk(14, 101, 101.5, 99.7, 101.2)) // still on the line
		if mirror {
			bars = trendlineMirror(bars)
		}
		now14 := time.UnixMilli(bars[14].OpenTime + 59_999).In(ctime())
		tls := TrendlinesBuild(bars, now14)
		got, refused := TrendlineLevels(tls, BoxesBuild(bars, DefaultBoxCfg(), now14), nil, bars)
		if len(got) != 0 || len(refused) != 1 || !strings.HasPrefix(refused[0], "trendline_third_touch_is_test:") {
			t.Fatalf("mirror=%v: a candle right after the 3rd touch still on the line is the test, not the 4th touch: levels=%+v refused=%v", mirror, got, refused)
		}
		// Held, not dropped: the valid line stays on the TARGET ladder
		// (CE-11/B12) while it is refused as an entry location.
		if _, _, held := trendlineLevelsTouchHeld(tls, BoxesBuild(bars, DefaultBoxCfg(), now14), nil, bars, TrendlineEntryFourth); len(held) != 1 || len(trendlineTargets(held)) != 1 {
			t.Fatalf("mirror=%v: the held line must still mirror into a trendline target, held=%+v", mirror, held)
		}

		// Leave (bar 15 clear of the line + band), then return (bar 16): the 4th touch.
		bars, mk = trendlineReturnTape()
		bars = append(bars,
			mk(14, 101, 101.5, 99.7, 101.2),
			mk(15, 101, 101.8, 100.6, 101.5),
			mk(16, 101.5, 101.6, 99.9, 101.2),
		)
		if mirror {
			bars = trendlineMirror(bars)
		}
		now16 := time.UnixMilli(bars[16].OpenTime + 59_999).In(ctime())
		tls = TrendlinesBuild(bars, now16)
		var line *Trendline
		for i := range tls {
			if tls[i].ValidAt == 13 && !tls[i].Dead {
				line = &tls[i]
			}
		}
		if line == nil {
			t.Fatalf("mirror=%v fixture: want the line valid at 13 on bar 16, got %+v", mirror, tls)
		}
		got, refused = TrendlineLevels([]Trendline{*line}, BoxesBuild(bars, DefaultBoxCfg(), now16), nil, bars)
		if len(got) != 1 || len(refused) != 0 {
			t.Fatalf("mirror=%v: leave then return is the 4th touch — a location: levels=%+v refused=%v", mirror, got, refused)
		}
	}
}

// The knob's other value is the pre-#60 behaviour: "third" arms on the 3rd-touch
// candle itself.
func TestM60TrendlineEntryTouchThirdKnob(t *testing.T) {
	bars, _ := trendlineReturnTape()
	now13 := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now13)
	boxes := BoxesBuild(bars, DefaultBoxCfg(), now13)
	if got, refused := TrendlineLevelsTouch(tls, boxes, nil, bars, TrendlineEntryThird); len(got) != 1 || len(refused) != 0 {
		t.Fatalf("knob third: the line arms on its 3rd-touch candle — levels=%+v refused=%v", got, refused)
	}
	if got, refused := TrendlineLevelsTouch(tls, boxes, nil, bars, TrendlineEntryFourth); len(got) != 0 || len(refused) != 1 {
		t.Fatalf("knob fourth: the 3rd-touch candle is refused — levels=%+v refused=%v", got, refused)
	}
	if DefaultConfig().TrendlineEntryTouch != TrendlineEntryFourth {
		t.Fatalf("default trendline entry touch = %q, want %q (D2.3-R7)", DefaultConfig().TrendlineEntryTouch, TrendlineEntryFourth)
	}
}

// HTF lines are unchanged: a persistent 1h/4h line's ValidAt is an HTF index,
// so the 3rd-touch read never refuses it.
func TestM60TrendlineThirdTouchLeavesHTFLines(t *testing.T) {
	bars, _ := trendlineReturnTape()
	tl := Trendline{Side: SideLong, TFMin: 60, P0Px: 97, P1Px: 98.2, P0T: bars[2].OpenTime, P1T: bars[8].OpenTime, ValidAt: len(bars) - 1}
	if got, refused := TrendlineLevels([]Trendline{tl}, nil, nil, bars); len(got) != 1 || len(refused) != 0 {
		t.Fatalf("an HTF line is not touched by the 3rd-touch rule: levels=%+v refused=%v", got, refused)
	}
}

// Tick-level: the refusal is NAMED and counted in the ledger.
func TestM60TrendlineThirdTouchCountedInTick(t *testing.T) {
	bars, _ := trendlineReturnTape()
	now := bars[13].OpenTime + 59_999
	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	if len(tls) != 1 {
		t.Fatalf("fixture: %+v", tls)
	}
	e := trendlineEval(bars, now, tls[0].key(), tls[0].priceAt(bars[13].OpenTime))
	e.Tick(bars, now)
	n := 0
	for k, v := range e.State.Refusals {
		if strings.HasPrefix(k, "trendline_third_touch_is_test:") {
			n += v
		}
	}
	if n == 0 {
		t.Fatalf("the 3rd-touch refusal was not counted — refusals=%v", e.State.Refusals)
	}
}

// ── D3.2-R29/R30: near box — E6 refused, the middle case on its own room ─────
//
// E6 [D3.2 p1 @22:12–23:08]: FTGL 28,980.75–29,014.00, the short ISB candle
// ~29,015–29,025 sits on the box top: "sát box, gần box, gần supply quá, mình
// không được đánh". Short entry 29,013.50 (low − 1.5), stop 29,026.50. The FTGH
// 29,060–29,075 drawn above must NOT exempt it (box.go's old two-box rule did).
func TestM60NearBoxE6RefusesShortOnFTGLTop(t *testing.T) {
	boxes := []Box{
		{Kind: FTGL, Top: 29014.00, Bottom: 28980.75},
		{Kind: FTGH, Top: 29075.00, Bottom: 29060.00},
	}
	entry, stop := 29013.50, 29026.50
	refuse, name := nearBoxRefusal(boxes, entry, SideShort, 2, stop-entry, false)
	if !refuse || name != "near_box_inside_opposing" {
		t.Fatalf("E6 short on top of the FTGL = (%v, %q), want refused near_box_inside_opposing", refuse, name)
	}
	// Mirror: a long inside an FTGH (the box opposes a long).
	if refuse, name := nearBoxRefusal([]Box{{Kind: FTGH, Top: 110, Bottom: 100}}, 101, SideLong, 2, 3, false); !refuse || name != "near_box_inside_opposing" {
		t.Fatalf("long inside an FTGH = (%v, %q), want refused near_box_inside_opposing", refuse, name)
	}
}

// R30 [D3.2 p1 @23:14–24:41]: "cái này nó đang giữa 2 box mà… nó đâu có làm gì
// đâu" — the middle ISB (short at 29,040, risk 6) has 26 pts to the FTGL top,
// over 2 × risk: allowed on its OWN room, no exemption needed.
func TestM60NearBoxMiddleISBAllowed(t *testing.T) {
	boxes := []Box{
		{Kind: FTGL, Top: 29014.00, Bottom: 28980.75},
		{Kind: FTGH, Top: 29075.00, Bottom: 29060.00},
	}
	if refuse, name := nearBoxRefusal(boxes, 29040, SideShort, 2, 6, false); refuse {
		t.Fatalf("the middle ISB must pass on its own room, got refused %q", name)
	}
}

// The knob's other value restores the pre-#60 two-box exemption; the default is
// the course (no blanket exemption).
func TestM60NearBoxBetweenExemptKnob(t *testing.T) {
	boxes := []Box{{Kind: FTGH, Top: 110, Bottom: 100}, {Kind: FTGL, Top: 85, Bottom: 80}}
	if refuse, _ := nearBoxRefusal(boxes, 90, SideLong, 2, 6, true); refuse {
		t.Fatal("knob true: between two boxes is exempt (pre-#60)")
	}
	if refuse, name := nearBoxRefusal(boxes, 90, SideLong, 2, 6, false); !refuse || name != "near_box" {
		t.Fatalf("default: 10 pts under the FTGH with 2R = 12 → near_box, got (%v, %q)", refuse, name)
	}
	if DefaultConfig().NearBoxBetweenExempt {
		t.Fatal("default near_box_between_exempt must be false (D3.2-R29)")
	}
}

// ── D5.1-R26: the tiny-stop exception, measured 12.50 ─────────────────────────
//
// "stop loss của mình lúc đó rất nhỏ, có 12 điểm… cancel làm chi trời… Anh chị
// có một cái quyền: thấy stop loss nhỏ… cứ việc đặt lệnh, mặc dù sau 3 cây"
// [D5.1 p2 @02:24–04:41]; the on-screen stop 29,285.75 → 29,273.25 = 12.50 pts
// (50 ticks). A 12.5-pt ISB stop is held past the cancel candle; 12.75 cancels.
func TestM60ISBStackTinyStopHoldsPastCandle4(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	run := func(rng float64) []Intent {
		i1 := market.Kline{Open: 100, Close: 100 - rng + 1, High: 100 + 0.5, Low: 100 + 0.5 - rng}
		arm := &ISBArm{FirstBar: i1, TFMin: 1}
		inside := market.Kline{Open: 99, Close: 99.5, High: 99.75, Low: 98.75}
		var last []Intent
		for i := 0; i < 3; i++ {
			last = ISBStackTick(arm, inside, cfg)
			if i < 2 && len(last) != 0 {
				t.Fatalf("candle %d inside I1 must hold, got %+v", i+2, last)
			}
		}
		return last
	}
	// stop = range + 2 × 1.5 → range 9.5 = 12.5 pts.
	if got := run(9.5); len(got) != 0 {
		t.Fatalf("a 12.5-pt stop must be held past candle 4 (D5.1-R26), got %+v", got)
	}
	if got := run(9.75); len(got) != 1 || got[0].Action != CancelArm {
		t.Fatalf("a 12.75-pt stop is over the tiny-stop line → cancel at candle 4, got %+v", got)
	}
	// The body-outside-I1 cancel still applies to a tiny stop.
	i1 := market.Kline{Open: 100, Close: 91.5, High: 100.5, Low: 91}
	arm := &ISBArm{FirstBar: i1, TFMin: 1}
	if got := ISBStackTick(arm, market.Kline{Open: 99, Close: 101, High: 101.5, Low: 98}, cfg); len(got) != 1 || got[0].Action != CancelArm {
		t.Fatalf("a body outside I1 cancels at once even with a tiny stop, got %+v", got)
	}
	if DefaultConfig().ISBStackTinyStopPts != 12.5 {
		t.Fatalf("default isb_stack_tiny_stop_pts = %g, want 12.5 (D5.1-R26)", DefaultConfig().ISBStackTinyStopPts)
	}
}

// ── D2.2-R64: a flat low is an FTGL, not a PHL ───────────────────────────────
//
// "đang đi kiếm một cái lệnh có đáy cao hơn đáy trước đó; mà ngay chỗ này nó
// ngang phè" [D2.2 p3 @07:44–09:46]; "Cái chỗ này anh đang gọi nó là failure to
// go lower" [@09:46–10:15]; pink box 29,369.00–~29,380.25. The "same price"
// measure is the owner-ruled m428 R2 flatNearPts.
func TestM60PHLFlatLowRefused(t *testing.T) {
	cfg := DefaultConfig()
	ref := func(low float64) market.Kline {
		return market.Kline{Open: low + 3, Close: low + 6, High: low + 8, Low: low}
	}
	if got := phlFlatExtremeRefusal(cfg, SideLong, ref(29371.00), 29369.00, 4.5); got != "phl_flat_low_is_ftgl" {
		t.Fatalf("low 29,371.00 vs prior 29,369.00, near 4.5 → %q, want phl_flat_low_is_ftgl", got)
	}
	if got := phlFlatExtremeRefusal(cfg, SideLong, ref(29380.25), 29369.00, 4.5); got != "" {
		t.Fatalf("low 29,380.25 is 11.25 higher (> near 4.5) → a real higher low, got %q", got)
	}
	// A lower/equal low is the existing not-a-higher-low refusal, not this one.
	if got := phlFlatExtremeRefusal(cfg, SideLong, ref(29369.00), 29369.00, 4.5); got != "" {
		t.Fatalf("an equal low is phl_not_higher_low's, got %q", got)
	}
	// Mirror [D2.2 p3 @14:03–14:22]: a flat high is an FTGH, not a PLH.
	refS := market.Kline{Open: 29360, Close: 29355, High: 29367.00, Low: 29350}
	if got := phlFlatExtremeRefusal(cfg, SideShort, refS, 29369.00, 4.5); got != "plh_flat_high_is_ftgh" {
		t.Fatalf("high 29,367 vs prior high 29,369, near 4.5 → %q, want plh_flat_high_is_ftgh", got)
	}
	strict := cfg
	strict.PHLFlatLow = PHLFlatLowStrict
	if got := phlFlatExtremeRefusal(strict, SideLong, ref(29371.00), 29369.00, 4.5); got != "" {
		t.Fatalf("knob strict: the pre-#60 strict comparison lets 29,371 through, got %q", got)
	}
	if cfg.PHLFlatLow != PHLFlatLowNear {
		t.Fatalf("default phl_flat_low = %q, want %q", cfg.PHLFlatLow, PHLFlatLowNear)
	}
}

// ── D5.1-R18: the day gate's spent line is 265 ───────────────────────────────
//
// "Một cây nến daily chạy 300 điểm, mà tới đây nó chạy gần được 300 điểm… nó
// chạy gần hết cây daily luôn rồi" + "phải còn 100 điểm" [D5.1 p1 @17:56–18:14];
// the measured run was −265.00 (1,060 ticks) and the mentor called it spent.
func TestM60DayGate265IsSpent(t *testing.T) {
	for _, g := range []DayGate{DefaultDayGate(), {SpentPts: DefaultConfig().DayGateSpentPts, TargetCapPts: DefaultConfig().DayGateTargetCapPts}} {
		if v, _ := DayGateVerdict(265, true, false, g); v != DaySpent {
			t.Fatalf("gate %+v: 265 agree → %v, want DaySpent", g, v)
		}
		if v, _ := DayGateVerdict(265, true, true, g); v != DayOff {
			t.Fatalf("gate %+v: 265 conflict → %v, want DayOff", g, v)
		}
		if v, _ := DayGateVerdict(264.75, true, false, g); v != DayTrade {
			t.Fatalf("gate %+v: 264.75 → %v, want DayTrade", g, v)
		}
	}
}

// ── D5.4-R7: hovering around / overshooting the EMA 9 counts ─────────────────
//
// "nó không cần đụng, nó cứ đụng vòng vòng ngay EMA 9 là được … không quan
// trọng là nó đụng ngay tại EMA 9 hay là nó lố EMA 9" [D5.4 p1 @03:35–03:46];
// the 09:45–09:48 candles straddle the EMA 9 as a GROUP. Uptrend, red mother
// H100 L90, ISB H97 L93: an EMA 9 at 91.5 is inside the mother + ISB range
// (not the ISB candle's own) → LONG; at 88 it was never reached → refused.
func TestM60ReverseISBHoverTouch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mother := market.Kline{Open: 99, Close: 91, High: 100, Low: 90}
	isb := market.Kline{Open: 94, Close: 96, High: 97, Low: 93}
	in, ok, why := ReverseISBAtEMA9(mother, isb, 91.5, SideLong, cfg)
	if !ok || in.Side != SideLong {
		t.Fatalf("EMA 9 91.5 inside the mother+ISB group → LONG intent, got ok=%v %q %+v", ok, why, in)
	}
	if _, ok, _ := ReverseISBAtEMA9(mother, isb, 88, SideLong, cfg); ok {
		t.Fatal("EMA 9 at 88: the group never reached it → refused")
	}
	old := cfg
	old.ISBReverseTouch = ISBReverseTouchISBCandle
	if _, ok, _ := ReverseISBAtEMA9(mother, isb, 91.5, SideLong, old); ok {
		t.Fatal("knob isb_candle: the pre-#60 read needs the EMA 9 inside the ISB candle (93–97)")
	}
	if cfg.ISBReverseTouch != ISBReverseTouchPair {
		t.Fatalf("default isb_reverse_touch = %q, want %q", cfg.ISBReverseTouch, ISBReverseTouchPair)
	}
}

// m465PreValues sets every m465 P3 knob to its pre-#60 value (the knob's
// non-default side) and the two changed defaults back to their pre-#60
// numbers — for the identity tests that pin a tape as it ran before #60.
func m465PreValues(cfg *Config) {
	cfg.TrendlineEntryTouch = TrendlineEntryThird
	cfg.NearBoxBetweenExempt = true
	cfg.PHLFlatLow = PHLFlatLowStrict
	cfg.ISBReverseTouch = ISBReverseTouchISBCandle
	cfg.ISBStackTinyStopPts = 0
	cfg.DayGateSpentPts = 300
}

// TestM60PHLFlatLowRefusedAtTheTickCallSite — the production call site
// (Evaluator.Tick) of D2.2-R64: the D2-28 worked-PHL tape with the structural
// left low lifted to 29,384 — the broken candle's low 29,386 is only 2 pts
// higher, inside the day's "same price" distance (flatNearPts on this tape ≈
// 7), so the touch is a FLAT low = an FTGL, not a PHL: refused
// phl_flat_low_is_ftgl. The knob's strict side (pre-#60) emits the PHL.
// MUTANT: drop the phlFlatExtremeRefusal call in eval.go → RED.
func TestM60PHLFlatLowRefusedAtTheTickCallSite(t *testing.T) {
	withFlatNear(t)
	run := func(flat string) (*Evaluator, []Intent) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.PHLTargetShyPts = 6
		cfg.EMALocationTFMinutes = 0
		cfg.Exec2mAfter30m = false
		cfg.PHLFlatLow = flat
		e := New(cfg)
		e.seeded = true
		e.State.Seed1mWatermark = math.MaxInt64
		e.State.Seed1HWatermark = math.MaxInt64
		e.State.SeedLevels = []Level{
			{Key: "L", Kind: KindKeyLevel, Price: 29386},
			{Key: "old-low:29335", Kind: KindOldExtreme, Role: OldLow, Price: 29335},
			{Key: "old-high:29431.75", Kind: KindOldExtreme, Role: OldHigh, Price: 29431.75},
		}
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}
		bars := []market.Kline{
			rthBars(0, 29400, 29410, 29384, 29405), // the structural left low, 29,384
			rthBars(1, 29420, 29430, 29415, 29425),
			rthBars(2, 29425, 29431.75, 29420, 29428), // old high
			rthBars(3, 29410, 29415, 29400, 29408),
			rthBars(4, 29400, 29405, 29395, 29398),
			rthBars(5, 29395, 29397, 29386, 29392), // touches L=29386 — 2 pts over the left low
		}
		now := bars[5].CloseTime + 1
		e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e, e.Tick(bars, now)
	}
	isPHL := func(ints []Intent) bool {
		for _, in := range ints {
			if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong {
				return true
			}
		}
		return false
	}
	e, ints := run(PHLFlatLowNear)
	if isPHL(ints) || e.State.Refusals["phl_flat_low_is_ftgl"] == 0 {
		t.Fatalf("a low 2 pts over the left low is flat (an FTGL): want phl_flat_low_is_ftgl, no PHL; refusals=%v intents=%+v", e.State.Refusals, ints)
	}
	e, ints = run(PHLFlatLowStrict)
	if !isPHL(ints) {
		t.Fatalf("knob strict (pre-#60): 29,386 > 29,384 is a higher low — the PHL emits; refusals=%v", e.State.Refusals)
	}
}
