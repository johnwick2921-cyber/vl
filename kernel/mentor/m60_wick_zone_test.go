package mentor

// m465 P5 — release #60: the D4.3 wick microscalp, the WHOLE setup (5m wick-run
// zone → 1m ISB/PHL at the zone → 5m-wick targets, short mirror, safe-play
// sub-knob) behind knob wick_microscalp (default OFF). Every rule cites the
// lesson [Ngày 4.3 "Follow trend 5, kiếm lệnh 1"], timestamps = part 1.

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m60c5 is one CLOSED 5m candle (Mon 14 Sep 2026 from 08:00 CT + i×5m).
func m60c5(i int, o, h, l, c float64) market.Kline { return wick5m(i, o, h, l, c) }

// ── R1 ──────────────────────────────────────────────────────────────────────

// TestM60WickRunTwoIsEnough — D4.3-R1: "từ 2 cây 5 phút là đẹp, mới nói là at
// least là phải 2 cây, tới 3 cây thì 2 cây là ok" [@06:38–06:42]; "là tới cây
// nến 3 thôi" [@13:29]. Two lower-wick 5m candles in an uptrend draw a zone;
// one is wick_run_too_short. (Knob OFF byte-identity is
// TestM434TickOutputIsByteIdentical + TestM60WickKnobOffIsInert.)
func TestM60WickRunTwoIsEnough(t *testing.T) {
	plain := m60c5(0, 100, 106, 100, 106) // no wick at all
	two := []market.Kline{
		plain,
		m60c5(1, 106, 107, 99, 106.5), // lower 7, upper 0.5
		m60c5(2, 106, 107, 100, 106),  // lower 6, upper 1
	}
	z, why := detectWickRun(two, SideLong)
	if why != "" || z.Side != SideLong {
		t.Fatalf("2 lower-wick candles in an uptrend = (%+v, %q), want a LONG zone [D4.3 @06:38–06:42]", z, why)
	}
	one := []market.Kline{plain, m60c5(1, 106, 110, 106, 110), m60c5(2, 106, 107, 100, 106)}
	if z, why := detectWickRun(one, SideLong); z.Side != "" || why != wickRefuseRunTooShort {
		t.Fatalf("1 lower-wick candle = (%+v, %q), want wick_run_too_short", z, why)
	}
	// Fewer than two closed 5m candles: no candidate yet (nothing to refuse).
	if z, why := detectWickRun(two[2:], SideLong); z.Side != "" || why != "" {
		t.Fatalf("a single closed 5m candle = (%+v, %q), want no candidate", z, why)
	}
}

// ── R2 / R3 ─────────────────────────────────────────────────────────────────

// TestM60WickRunMixedRefused — D4.3-R2: "cây này nó rút ở dưới lên, cây này nó
// rút trên xuống, cây này nó rút dưới lên thì không được" [@01:27–01:49]; "rút
// ngược chiều nhau nó khó lắm" [@03:12–03:18]. lower 4 / upper 8 / lower 4: the
// wicks pull both ways with equal force → wick_mixed_direction.
func TestM60WickRunMixedRefused(t *testing.T) {
	w := []market.Kline{
		m60c5(0, 104, 104, 100, 104), // lower 4
		m60c5(1, 104, 112, 104, 104), // upper 8
		m60c5(2, 104, 104, 100, 104), // lower 4
	}
	for _, trend := range []Side{SideLong, ""} {
		if z, why := detectWickRun(w, trend); z.Side != "" || why != wickRefuseMixed {
			t.Fatalf("trend %q: lower/upper/lower with equal summed wicks = (%+v, %q), want wick_mixed_direction", trend, z, why)
		}
	}
}

// TestM60WickRunOneOddTolerated — D4.3-R3: "Mặc dù là cây này nó rút râu ở trên
// xuống, nhưng nhìn… technically nhìn ra nó vẫn có sự rút râu từ dưới rút lên"
// [@01:55–02:02]. Pinned: ≤1 odd candle in the 3-candle window AND the summed
// with-trend wick beats the summed against wick. lower 6 / upper 1 / lower 5 →
// zone; lower / upper / upper → refused.
func TestM60WickRunOneOddTolerated(t *testing.T) {
	ok := []market.Kline{
		m60c5(0, 106, 106, 100, 106), // lower 6
		m60c5(1, 106, 107, 106, 106), // upper 1 (the odd one)
		m60c5(2, 106, 106, 101, 106), // lower 5
	}
	z, why := detectWickRun(ok, SideLong)
	if why != "" || z.Side != SideLong {
		t.Fatalf("lower 6 / upper 1 / lower 5 = (%+v, %q), want a LONG zone", z, why)
	}
	if z.Bottom != 100 || z.Top != 105 {
		t.Fatalf("the odd candle must not draw the box: %.2f–%.2f, want 100.00–105.00", z.Bottom, z.Top)
	}
	bad := []market.Kline{
		m60c5(0, 106, 106, 100, 106), // lower 6
		m60c5(1, 106, 112, 106, 106), // upper 6
		m60c5(2, 106, 112, 106, 106), // upper 6
	}
	if z, why := detectWickRun(bad, SideLong); z.Side != "" || why == "" {
		t.Fatalf("lower / upper / upper in an uptrend = (%+v, %q), want refused", z, why)
	}
}

// ── R5 / R10 ────────────────────────────────────────────────────────────────

// TestM60WickZoneEx1Bounds — D4.3-R5 "Mọi người phải nhìn được là nó rút râu ở
// đâu nhiều hơn" [@08:35, 11:29]. Zone = [lowest, highest] wick extreme of the
// run, widened to 5.00 (Ex.1 28,880.50–28,885.50 [screen @03:35–03:46]) and
// capped at 14.25 (Ex.4 29,261.00–29,275.25 [@15:25–15:27]) from the far edge.
func TestM60WickZoneEx1Bounds(t *testing.T) {
	ex1 := []market.Kline{
		m60c5(6, 28906, 28933.75, 28880.50, 28905.50), // 08:30 [screen header @07:13]
		m60c5(7, 28905, 28906, 28882.00, 28890),       // 08:35 red, long lower wick
	}
	z, why := detectWickRun(ex1, SideLong)
	if why != "" || z.Bottom != 28880.50 || z.Top != 28885.50 {
		t.Fatalf("Ex.1 zone = %.2f–%.2f (%q), want 28,880.50–28,885.50 (5-pt floor)", z.Bottom, z.Top, why)
	}
	ex4 := []market.Kline{
		m60c5(14, 29250, 29275.25, 29245, 29248), // upper wick to 29,275.25
		m60c5(15, 29248, 29261.00, 29236, 29240), // upper wick to 29,261.00
	}
	z, why = detectWickRun(ex4, SideShort)
	if why != "" || z.Bottom != 29261.00 || z.Top != 29275.25 {
		t.Fatalf("Ex.4 zone = %.2f–%.2f (%q), want 29,261.00–29,275.25", z.Bottom, z.Top, why)
	}
	wide := []market.Kline{
		m60c5(0, 29280, 29300, 29270, 29275), // upper 20
		m60c5(1, 29275, 29280, 29265, 29268), // upper 5 (high 20 under the first)
	}
	if z, _ := detectWickRun(wide, SideShort); z.Top != 29300 || z.Bottom != 29300-14.25 {
		t.Fatalf("a 20-pt wick cluster caps at 14.25 from the extreme: %.2f–%.2f", z.Bottom, z.Top)
	}
}

// TestM60WickBaseThreeCandles — D4.3-R10: "trend đang tăng thì mình kiếm những
// khúc nào nó rút râu, nó tạo base, nó tạo cái support ở chỗ này nè… 3 cái
// nến" [@09:17–09:26]. Ex.2 red / green / red with lows ~29,237 → zone
// 29,237.00–29,242.00 (the 5-pt floor; he drew 29,237.75–29,247.50).
func TestM60WickBaseThreeCandles(t *testing.T) {
	base := []market.Kline{
		m60c5(47, 29248, 29250, 29237.00, 29243), // red, lower 6
		m60c5(48, 29242, 29247, 29237.25, 29245), // green, lower 4.75
		m60c5(49, 29246, 29248, 29237.00, 29240), // red, lower 3
	}
	z, why := detectWickRun(base, SideLong)
	if why != "" || z.Bottom != 29237.00 || z.Top != 29242.00 {
		t.Fatalf("Ex.2 base = %.2f–%.2f (%q), want 29,237.00–29,242.00", z.Bottom, z.Top, why)
	}
	if z.FormedMs != base[0].OpenTime || z.FormedAt != base[2].CloseTime {
		t.Fatalf("the box spans the base: formed %d..%d, want %d..%d", z.FormedMs, z.FormedAt, base[0].OpenTime, base[2].CloseTime)
	}
}

// ── R8 ──────────────────────────────────────────────────────────────────────

// m60Uptrend5m is a closed 5m tape whose confirmed swing structure reads UP
// (swing lows 85 → 89 → 96, swing highs 110 → 115) and whose last three
// candles carry the given wicks.
func m60Uptrend5m(last3 [3][4]float64) []market.Kline {
	c := []market.Kline{
		m60c5(0, 95, 100, 90, 96),
		m60c5(1, 96, 105, 95, 100),
		m60c5(2, 97, 98, 85, 96), // swing low 85
		m60c5(3, 96, 110, 92, 108),
		m60c5(4, 104, 104, 94, 100), // confirms the swing high 110
		m60c5(5, 100, 103, 89, 101), // swing low 89 (higher low)
		m60c5(6, 101, 115, 97, 112), // swing high 115 (higher high)
	}
	for i, x := range last3 {
		c = append(c, m60c5(7+i, x[0], x[1], x[2], x[3]))
	}
	return c
}

// TestM60WickTrendFromStructure — D4.3-R8: "Rút trên nha, tại vì đang xu hướng
// giảm mà… đang xu hướng giảm mà rút dưới là tôi không biết đường tôi đánh"
// [@13:32–13:44]; R6 the 5m trend read left to right, not the 1m
// [@14:43–15:03]. The trend is the 5m STRUCTURE (structureTrend5m), not the
// trigger line: structure up with the trigger short → a LONG zone; upper
// wicks in the uptrend → wick_against_trend. Ranging ("") → the run's side.
func TestM60WickTrendFromStructure(t *testing.T) {
	lower := m60Uptrend5m([3][4]float64{{108, 112, 100, 110}, {104, 108, 96, 106}, {105, 109, 98, 107}})
	e := New(DefaultConfig())
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 120}
	if got := structureTrend5m(lower); got != SideLong {
		t.Fatalf("fixture: the 5m structure must read long, got %q", got)
	}
	trend := e.wickTrend(lower)
	if trend != SideLong {
		t.Fatalf("d43_trend_source=structure (default): trend %q, want long (the trigger says short)", trend)
	}
	z, why := detectWickRun(lower, trend)
	if why != "" || z.Side != SideLong || z.Bottom != 96 || z.Top != 101 {
		t.Fatalf("structure up + lower wicks = (%+v, %q), want LONG 96.00–101.00", z, why)
	}
	upper := m60Uptrend5m([3][4]float64{{104, 112, 102, 103}, {104, 113, 101, 102}, {103, 111, 101.5, 102}})
	if got := structureTrend5m(upper); got != SideLong {
		t.Fatalf("fixture: the 5m structure must still read long, got %q", got)
	}
	if z, why := detectWickRun(upper, e.wickTrend(upper)); z.Side != "" || why != wickRefuseAgainstTrend {
		t.Fatalf("upper wicks in an uptrend = (%+v, %q), want wick_against_trend", z, why)
	}
	// d43_trend_source=trigger_line reads the trigger instead.
	e.Cfg.D43TrendSource = D43TrendTriggerLine
	if got := e.wickTrend(lower); got != SideShort {
		t.Fatalf("d43_trend_source=trigger_line: trend %q, want the trigger's short", got)
	}
	// R13: ranging → the run's own side.
	if z, why := detectWickRun(lower[len(lower)-3:], ""); why != "" || z.Side != SideLong {
		t.Fatalf("ranging + lower wicks = (%+v, %q), want a LONG zone", z, why)
	}
}

// ── R15 ─────────────────────────────────────────────────────────────────────

// TestM60WickZoneIsLocation — D4.3-R15: "Vẫn được: potential high low khung giờ
// nhỏ hơn, hay là insert bar cho lệnh long khung giờ nhỏ hơn" [@06:13–06:29].
// The zone edge is a location; the 5m-wick target stays target-only; neither
// makes a mid-range.
func TestM60WickZoneIsLocation(t *testing.T) {
	edge := Level{Key: "wick_zone:long:1:top", Kind: KindWickZone, Price: 28885.5}
	if !levelIsLocation(edge, nil) {
		t.Fatal("the wick zone edge must be a PHL/PLH location")
	}
	if levelIsLocation(Level{Key: "wick_microscalp:long:28919.75", Kind: KindWickMicroscalp, Price: 28919.75}, nil) {
		t.Fatal("the 5m-wick target must stay target-only")
	}
	cfg := DefaultConfig()
	cfg.RangeGapPts = 50
	lv := []Level{edge, {Key: "K", Kind: KindKeyLevel, Price: 28920}}
	if MidRange(lv, 28900, cfg) {
		t.Fatal("a wick zone edge is never a mid-range boundary [D4.3 @04:44–04:55]")
	}
	if got := LocationOf(edge.Key); got != "wick_zone long" {
		t.Fatalf("LocationOf(%q) = %q, want \"wick_zone long\"", edge.Key, got)
	}
	if !isMovingLineKey(edge.Key) {
		t.Fatal("a close inside the box is not its death: the wrong-way mark is per visit")
	}
}

// ── Tick fixtures ───────────────────────────────────────────────────────────

// m60Eval is a seeded evaluator with the knob ON, a 1m read, the trigger /
// 4h / ORB / day all on side, and only the given seed levels.
func m60Eval(side Side, trig float64, seed []Level, now int64) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.LocTriggerFilter = false
	cfg.WickMicroscalpEnabled = true
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34, e.State.EMA9 = 0, 0
	e.State.SeedLevels = seed
	// LastBucket frozen: the fixture's trigger stands (TriggerTick re-reads
	// only buckets newer than LastBucket).
	e.State.Trigger = TriggerLine{Dir: side, Price: trig, LastBucket: math.MaxInt64}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: side, Price: trig}}
	if side == SideLong {
		e.State.ORB = ORB{Day: dayStartCT(now), High: trig, Low: trig - 50, Drawn: true, Escaped: SideLong}
	} else {
		e.State.ORB = ORB{Day: dayStartCT(now), High: trig + 50, Low: trig, Drawn: true, Escaped: SideShort}
	}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// m60Bucket expands one 5m candle into five 1m bars (path o → h → l → c)
// starting at rthBars index i0.
func m60Bucket(i0 int, o, h, l, c float64) []market.Kline {
	return []market.Kline{
		rthBars(i0, o, h, math.Min(o, h), h),
		rthBars(i0+1, h, h, l, l),
		rthBars(i0+2, l, math.Max(l, c), l, c),
		rthBars(i0+3, c, c, c, c),
		rthBars(i0+4, c, c, c, c),
	}
}

// m60InjectZone puts a standing zone in the state and freezes the run reader
// (the zone-to-entry path is tested apart from the detector).
func m60InjectZone(e *Evaluator, z WickZone, now int64) {
	z.Key = string(KindWickZone) + ":" + string(z.Side) + ":1"
	z.FormedMs, z.FormedAt = 1, 1
	z.Day = tradingDayKey(time.UnixMilli(now).In(ctime()))
	zz := z
	e.State.Wick = &WickState{Last5mClose: math.MaxInt64}
	*e.State.Wick.slot(z.Side) = &zz
}

func m60FindISB(ins []Intent, side Side) *Intent {
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == side {
			return in
		}
	}
	return nil
}

// m60Ex1Tape: five closed 5m candles whose upper wicks top at 28,919.75 and
// 28,933.50 (Ex.1's two rays [@07:40–07:44]), price back to the box, then the
// Ex.1 08:38 green mother (O28,888.75 H28,908 L28,883.50 C28,893.25 [screen
// @06:08–06:16]) and its inside bar.
func m60Ex1Tape() []market.Kline {
	var bars []market.Kline
	bars = append(bars, m60Bucket(0, 28900, 28919.75, 28895, 28915)...) // upper wick to 28,919.75
	bars = append(bars, m60Bucket(5, 28915, 28933.50, 28910, 28930)...) // upper wick to 28,933.50
	bars = append(bars, m60Bucket(10, 28930, 28950, 28925, 28950)...)   // no upper wick
	bars = append(bars, m60Bucket(15, 28950, 28950, 28920, 28922)...)   // no upper wick
	bars = append(bars, m60Bucket(20, 28922, 28922, 28890, 28892)...)   // no upper wick
	bars = append(bars,
		rthBars(25, 28888.75, 28908, 28883.50, 28893.25), // the green mother, its low in the box
		rthBars(26, 28894, 28896, 28890, 28895),          // inside bar (body inside the mother)
	)
	return bars
}

// ── R13 / R11 / R17 ─────────────────────────────────────────────────────────

// TestM60WickZoneArmsOneMinISB — D4.3-R13 "mọi người có quyền xem đây là một vùng
// supply [= demand] zone, và mọi người bắt ngược từ đây" [@04:44–04:55]; R14
// "Vô market là nó đâm cho lủng bụng" [@05:26–05:46]; R15 "Insert bar cho lệnh
// long nè" [@06:08–06:16]. Ex.1: the 08:38 1m ISB touching 28,880.50–28,885.50
// arms LONG and its target is the nearest 5m wick, 28,919.75 (R17); knob OFF
// the same ISB targets something else. Price in the zone with no setup → no
// entry, and the visit is named wick_zone_no_setup.
func TestM60WickZoneArmsOneMinISB(t *testing.T) {
	bars := m60Ex1Tape()
	now := bars[len(bars)-1].CloseTime
	seed := []Level{{Key: "K-below", Kind: KindKeyLevel, Price: 28800}, {Key: "K-above", Kind: KindKeyLevel, Price: 29000}}

	off := m60Eval(SideLong, 28850, seed, now)
	off.Cfg.WickMicroscalpEnabled = false
	offISB := m60FindISB(off.Tick(bars, now), SideLong)
	if offISB == nil {
		t.Fatalf("fixture: the knob-OFF long ISB must fire; refusals %v", off.State.Refusals)
	}
	if off.State.Wick != nil {
		t.Fatalf("knob OFF: no wick state, got %+v", off.State.Wick)
	}

	e := m60Eval(SideLong, 28850, seed, now)
	m60InjectZone(e, WickZone{Side: SideLong, Bottom: 28880.50, Top: 28885.50}, now)
	ins := e.Tick(bars, now)
	isb := m60FindISB(ins, SideLong)
	if isb == nil {
		t.Fatalf("Ex.1: the 1m ISB at the wick zone must arm LONG; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 28919.75 {
		t.Fatalf("Ex.1 target = %.2f, want 28,919.75 — the nearest 5m wick [D4.3 @09:39–09:46] (knob OFF: %.2f)", isb.Target, offISB.Target)
	}
	if offISB.Target == 28919.75 {
		t.Fatalf("the test must discriminate: knob OFF also targeted 28,919.75")
	}
	if !strings.Contains(isb.Reason, "wick zone") || e.State.Counters["wick_zone_entry"] != 1 {
		t.Fatalf("the entry must name the wick zone: reason %q counters %v", isb.Reason, e.State.Counters)
	}
	if e.State.Refusals[wickRefuseTargetNoRoom] != 0 {
		t.Fatalf("28,919.75 is 22.25 pts over a 9-pt risk: no wick_target_no_room, got %v", e.State.Refusals)
	}
	for _, in := range ins {
		if in.Action == ActionClosePosition {
			t.Fatalf("never a market order at the zone [D4.3 @05:26–05:46]: %+v", in)
		}
	}

	// Price in the zone with no setup: a red bar into the box (no ISB pair),
	// then bars back above it — no entry, and the visit is named once.
	e2 := m60Eval(SideLong, 28850, seed, now)
	tape := append([]market.Kline{}, bars[:25]...)
	tape = append(tape, rthBars(25, 28892, 28893, 28883, 28884)) // into the box, closes inside
	m60InjectZone(e2, WickZone{Side: SideLong, Bottom: 28880.50, Top: 28885.50}, tape[len(tape)-1].CloseTime)
	for i := 25; i < 25+1+wickAtZoneBars+1; i++ {
		if i > 25 {
			tape = append(tape, rthBars(i, 28890+float64(i-25), 28892+float64(i-25)*2, 28889+float64(i-25), 28891+float64(i-25)*2))
		}
		for _, in := range e2.Tick(tape, tape[len(tape)-1].CloseTime) {
			if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && strings.Contains(in.Reason, "wick zone") {
				t.Fatalf("no setup → no zone entry: %+v", in)
			}
		}
	}
	if got := e2.State.Refusals[wickRefuseNoSetup]; got != 1 {
		t.Fatalf("a visit with no setup is named once: wick_zone_no_setup = %d (refusals %v)", got, e2.State.Refusals)
	}
}

// TestM60WickZonePHLAtTheZone — D4.3-R11: "anh chị chuyển qua khung 1 phút… nó
// đang tạo một cái failure to go lower khá là nhỏ ở dưới này" [@03:22–03:44];
// Ex.2 "đụng đây, đánh [potential high low] luôn, đánh lên" [@09:27–09:40]. A
// 1m candle whose low is IN the box and which closes back above it is a PHL
// at the zone (the near edge is a location, KindWickZone); the same candle
// with the zone gone is no PHL.
func TestM60WickZonePHLAtTheZone(t *testing.T) {
	bars := []market.Kline{
		rthBars(-1, 28840, 28920, 28835, 28915),   // the low the leg to the old high started from
		rthBars(0, 28920, 28931.75, 28910, 28925), // the old high
		rthBars(1, 28905, 28908, 28895, 28902),
		rthBars(2, 28892, 28902, 28887, 28896),
		rthBars(3, 28890, 28892, 28883, 28891), // low 28,883 in the box, closes back above 28,885
	}
	now := bars[len(bars)-1].CloseTime + 1
	seed := []Level{{Key: "old-high:28931.75", Kind: KindOldExtreme, Role: OldHigh, Price: 28931.75}}
	find := func(ins []Intent) *Intent {
		for i := range ins {
			if ins[i].Action == PlaceStopEntry && ins[i].Setup == "PHL" {
				return &ins[i]
			}
		}
		return nil
	}

	e := m60Eval(SideLong, 28800, seed, now)
	e.Cfg.PHLTargetShyPts = 6
	m60InjectZone(e, WickZone{Side: SideLong, Bottom: 28880, Top: 28885}, now)
	ins := e.Tick(bars, now)
	phl := find(ins)
	if phl == nil || phl.Side != SideLong || !strings.HasPrefix(phl.AnchorKey, "wick_zone:long:") {
		t.Fatalf("the 1m reject at the wick zone must arm a LONG PHL anchored on the zone; got %+v (refusals %v)", phl, e.State.Refusals)
	}
	if phl.Price != 28892.5 {
		t.Fatalf("PHL entry = %.2f, want 28,892.50 (the reference high + 0.50, D2.2-R10)", phl.Price)
	}

	// The same tape with no zone: 28,885 is no level → no PHL.
	e2 := m60Eval(SideLong, 28800, seed, now)
	e2.Cfg.PHLTargetShyPts = 6
	if p := find(e2.Tick(bars, now)); p != nil {
		t.Fatalf("no zone → no location → no PHL, got %+v", p)
	}
}

// TestM60WickZoneWrongSideDropped — the zone trades only its own side: a PLH
// short read off the demand box's top from INSIDE the box is dropped and named.
func TestM60WickZoneWrongSideDropped(t *testing.T) {
	e := New(DefaultConfig())
	e.Cfg.Enabled, e.Cfg.WickMicroscalpEnabled = true, true
	m60InjectZone(e, WickZone{Side: SideLong, Bottom: 28880, Top: 28885}, rthBars(0, 0, 0, 0, 0).CloseTime)
	in := Intent{Action: PlaceStopEntry, Setup: "PLH", Side: SideShort, Price: 28881, Stop: 28886, AnchorKey: e.State.Wick.Long.Key + ":top", ArmID: "lvl-1"}
	if out := e.wickZoneLeave([]Intent{in}, nil, 0); len(out) != 0 {
		t.Fatalf("a short off a demand zone must be dropped, got %+v", out)
	}
	if e.State.Refusals[wickRefuseWrongSide] != 1 {
		t.Fatalf("named once: %v", e.State.Refusals)
	}
}

// TestM60WickTargetsAre5mWicks — D4.3-R17 "Target thì là những cái râu của khung
// 5 phút" [@09:39–09:46], never "100-200 điểm" [@07:30–07:49]: Ex.1 →
// 28,919.75 then 28,933.50. A 5m wick closer than 1R is named
// wick_target_no_room.
func TestM60WickTargetsAre5mWicks(t *testing.T) {
	bars := m60Ex1Tape()
	c5 := closedBucketsTF(bars, 5, bars[len(bars)-1].CloseTime)
	z := WickZone{Side: SideLong, Bottom: 28880.50, Top: 28885.50}
	got := wickTargetLadder(c5, z)
	if len(got) < 2 || got[0] != 28919.75 || got[1] != 28933.50 {
		t.Fatalf("Ex.1 ladder = %v, want 28,919.75 then 28,933.50 [D4.3 @07:40–07:44]", got)
	}
	// Room: an entry 28,912 with a 9-pt risk sees 28,919.75 under 1R.
	e := New(DefaultConfig())
	e.Cfg.Enabled, e.Cfg.WickMicroscalpEnabled = true, true
	m60InjectZone(e, z, bars[len(bars)-1].CloseTime)
	in := Intent{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideLong, Price: 28912, Stop: 28903, RefBarMs: bars[len(bars)-1].CloseTime}
	if out := e.wickZoneLeave([]Intent{in}, bars, bars[len(bars)-1].CloseTime); len(out) != 1 {
		t.Fatalf("the room note never drops the entry: %+v", out)
	}
	if e.State.Refusals[wickRefuseTargetNoRoom] != 1 {
		t.Fatalf("28,919.75 is 7.75 pts over a 9-pt risk: want wick_target_no_room, got %v", e.State.Refusals)
	}
}

// ── R12 ─────────────────────────────────────────────────────────────────────

// TestM60WickZoneExtendsUntilBroken — D4.3-R12 "Thì có phải là mọi người kéo dài
// nó qua, nó vẫn có setup để anh chị vô lệnh mà" [@06:03–06:07, 09:34, 13:50,
// 15:34]. The run reader draws the box from the 1m tape, the box survives 40
// 1m bars, then a 1m close under its bottom kills it: wick_zone_broken.
func TestM60WickZoneExtendsUntilBroken(t *testing.T) {
	var bars []market.Kline
	bars = append(bars,
		rthBars(0, 100, 101, 95, 100.5), rthBars(1, 100.5, 102, 99, 101), rthBars(2, 101, 103, 100, 102),
		rthBars(3, 102, 103, 101, 102.5), rthBars(4, 102.5, 103, 101.5, 102), // 5m O100 H103 L95 C102: lower 5
		rthBars(5, 102, 103, 96, 102.5), rthBars(6, 102.5, 104, 101, 103), rthBars(7, 103, 104, 102, 103.5),
		rthBars(8, 103.5, 104, 102.5, 103), rthBars(9, 103, 104, 102.5, 103.5), // 5m O102 H104 L96 C103.5: lower 6
	)
	e := m60Eval(SideLong, 90, nil, bars[len(bars)-1].CloseTime)
	for i := 1; i < len(bars); i++ {
		e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
	}
	z := e.State.Wick.Long
	if z == nil || z.Bottom != 95 || z.Top != 100 {
		t.Fatalf("two lower-wick 5m candles must draw the LONG box 95.00–100.00, got %+v (refusals %v)", z, e.State.Refusals)
	}
	key := z.Key
	for i := 10; i < 50; i++ { // 40 1m bars above the box
		bars = append(bars, rthBars(i, 104, 106, 104, 106))
		e.Tick(bars, BarCloseInstant(bars[i]))
		if e.State.Wick.Long == nil || e.State.Wick.Long.Key != key {
			t.Fatalf("bar %d: the box is extended right while nothing closes under it, got %+v", i, e.State.Wick.Long)
		}
	}
	if !m60HasLevel(e.State.ChartLevels, key+":top") || !m60HasLevel(e.State.ChartLevels, key+":bottom") {
		t.Fatalf("the chart carries both edges (one rectangle): %+v", e.State.ChartLevels)
	}
	bars = append(bars, rthBars(50, 105, 105, 93.5, 94)) // a 1m close under 95
	e.Tick(bars, BarCloseInstant(bars[50]))
	if e.State.Wick.Long != nil {
		t.Fatalf("a 1m close under the bottom breaks the box, still %+v", e.State.Wick.Long)
	}
	if e.State.Refusals[wickRefuseZoneBroken] != 1 {
		t.Fatalf("named once: %v", e.State.Refusals)
	}
}

func m60HasLevel(lv []Level, key string) bool {
	for _, l := range lv {
		if l.Key == key && l.Kind == KindWickZone {
			return true
		}
	}
	return false
}

// ── R19 / R27 / R20 ─────────────────────────────────────────────────────────

// m60Ex3Tape: closed 5m candles whose LOWER wicks bottom at 29,606 and 29,600
// (the short target ladder), price back up into the 29,628–29,642 supply box,
// then a red mother touching the box and its inside bar (closing at insideC).
func m60Ex3Tape(insideO, insideC float64) []market.Kline {
	var bars []market.Kline
	bars = append(bars, m60Bucket(0, 29612, 29620, 29606, 29615)...) // lower wick to 29,606
	bars = append(bars, m60Bucket(5, 29610, 29615, 29600, 29612)...) // lower wick to 29,600
	bars = append(bars, m60Bucket(10, 29612, 29625, 29612, 29625)...)
	bars = append(bars,
		rthBars(15, 29632, 29641, 29620, 29622),     // red mother, high in the box
		rthBars(16, insideO, 29630, 29623, insideC), // inside bar
	)
	return bars
}

// TestM60WickShortMirrorEx3 — D4.3-R19 "Có phải lên đụng cái box này em scalp
// ngược xuống. Em sẽ nhìn theo xu hướng của khung 5 phút" [@15:38–15:47]; R27 the
// long procedure mirrored [@13:22–15:47]. The Ex.3 run (upper wicks topping
// 29,642 / 29,640 / 29,628 in the downtrend) draws 29,628.00–29,642.00; a 1m
// short ISB touching it arms SHORT with the nearest 5m wick LOW as target.
func TestM60WickShortMirrorEx3(t *testing.T) {
	run := []market.Kline{
		m60c5(48, 29630, 29642, 29626, 29628), // upper 12
		m60c5(49, 29628, 29640, 29618, 29620), // upper 12
		m60c5(50, 29620, 29628, 29610, 29612), // upper 8
	}
	z, why := detectWickRun(run, SideShort)
	if why != "" || z.Side != SideShort || z.Bottom != 29628 || z.Top != 29642 {
		t.Fatalf("Ex.3 zone = (%+v, %q), want SHORT 29,628.00–29,642.00 [screen @13:27, @13:45–14:00]", z, why)
	}
	if z, why := detectWickRun(run, SideLong); z.Side != "" || why != wickRefuseAgainstTrend {
		t.Fatalf("the same upper wicks in an uptrend = (%+v, %q), want wick_against_trend", z, why)
	}

	bars := m60Ex3Tape(29624, 29625)
	now := bars[len(bars)-1].CloseTime
	seed := []Level{{Key: "K-above", Kind: KindKeyLevel, Price: 29700}, {Key: "K-below", Kind: KindKeyLevel, Price: 29500}}
	e := m60Eval(SideShort, 29660, seed, now)
	m60InjectZone(e, z, now)
	ins := e.Tick(bars, now)
	isb := m60FindISB(ins, SideShort)
	if isb == nil {
		t.Fatalf("Ex.3: the 1m short ISB at the supply box must arm SHORT; intents %+v refusals %v", ins, e.State.Refusals)
	}
	// D4.3-R17 + D1.4 "at least 1:1": a wick target needs 1R of room, not the
	// 2R the other targets keep. Entry 29,621.50, risk 10: the 29,606 wick low
	// is 15.50 pts (1.55R) away — the target; the pre-fix head skipped it for
	// 29,600 (2.15R) [D4.3 @09:39–09:46, @13:45–14:00 "last 29,606.00"].
	if isb.Target != 29606 {
		t.Fatalf("short target = %.2f, want 29,606 — the nearest 5m wick low ≥ 1R under the entry %.2f (risk %.2f)", isb.Target, isb.Price, isb.Stop-isb.Price)
	}
	if e.State.Refusals["room"] != 0 {
		t.Fatalf("a 1.55R wick target must pass the room rule (1R for a wick target): refusals %v", e.State.Refusals)
	}
	if !strings.Contains(isb.Reason, "wick zone short") {
		t.Fatalf("the entry must name the supply zone: %q", isb.Reason)
	}
}

// TestM60WickSafePlayWaits — D4.3-R20 "mọi người đợi nó nằm ở dưới cái box. Mọi
// người vẫn có thể trade bình thường, là thêm một sự confirm" [@14:07–14:27].
// Sub-knob wick_safe_play (default OFF): ON, a short at the supply box whose
// reference candle closed INSIDE the box waits (wick_safe_play_wait, the arm
// is un-registered); one that closed under the box trades.
func TestM60WickSafePlayWaits(t *testing.T) {
	z := WickZone{Side: SideShort, Bottom: 29628, Top: 29642}
	seed := []Level{{Key: "K-above", Kind: KindKeyLevel, Price: 29700}, {Key: "K-below", Kind: KindKeyLevel, Price: 29500}}
	run := func(insideO, insideC float64, safe bool) (*Intent, *Evaluator) {
		bars := m60Ex3Tape(insideO, insideC)
		now := bars[len(bars)-1].CloseTime
		e := m60Eval(SideShort, 29660, seed, now)
		e.Cfg.WickSafePlay = safe
		m60InjectZone(e, z, now)
		return m60FindISB(e.Tick(bars, now), SideShort), e
	}
	if d := DefaultConfig(); d.WickSafePlay || d.WickMicroscalpEnabled {
		t.Fatalf("both knobs default OFF: wick_microscalp=%v wick_safe_play=%v", d.WickMicroscalpEnabled, d.WickSafePlay)
	}
	// Inside bar closing 29,629 — inside the box.
	if isb, _ := run(29628.5, 29629, false); isb == nil {
		t.Fatal("safe play OFF: the ISB closing inside the box still trades")
	}
	isb, e := run(29628.5, 29629, true)
	if isb != nil {
		t.Fatalf("safe play ON: a reference candle closing inside the box must wait, got %+v", isb)
	}
	if e.State.Refusals[wickRefuseSafePlayWait] != 1 || len(e.State.ISBArms) != 0 {
		t.Fatalf("named once and the arm un-registered: refusals %v arms %v", e.State.Refusals, e.State.ISBArms)
	}
	// Inside bar closing 29,625 — under the box: the confirm is there.
	if isb, e := run(29624, 29625, true); isb == nil {
		t.Fatalf("safe play ON: a reference candle closed under the box trades; refusals %v", e.State.Refusals)
	}
}

// ── knob OFF ────────────────────────────────────────────────────────────────

// TestM60WickKnobOffIsInert — knob OFF (the default) builds no wick state, no
// wick level, no wick refusal over a real recorded day (10-23); the m434 tick
// golden (TestM434TickOutputIsByteIdentical) pins the bytes.
func TestM60WickKnobOffIsInert(t *testing.T) {
	bars := m423Bars(t)
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	for i := 150; i < len(bars); i += 7 {
		lo := i + 1 - 1500
		if lo < 0 {
			lo = 0
		}
		e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
		if e.State.Wick != nil {
			t.Fatalf("tick %d: knob OFF built wick state %+v", i, e.State.Wick)
		}
		for _, l := range e.State.ChartLevels {
			if l.Kind == KindWickZone || l.Kind == KindWickMicroscalp {
				t.Fatalf("tick %d: knob OFF drew %+v", i, l)
			}
		}
	}
	for k := range e.State.Refusals {
		if strings.HasPrefix(k, "wick_") {
			t.Fatalf("knob OFF counted %s", k)
		}
	}
}

// TestM60WickTargetRoomIsOneR — D4.3-R17 "Target thì là những cái râu của khung
// 5 phút" [@09:39–09:46] + D1.4 "at least 1:1": the room for a 5m-wick target
// (KindWickMicroscalp) is 1R, not RoomMultiple (2R). The HTF trigger keeps 2R;
// a wick under 1R still falls through. The room rule at every call site reads
// the chosen target's kind (targetRoomMultiple), so the 1R target is not then
// refused "room" at 2R.
func TestM60WickTargetRoomIsOneR(t *testing.T) {
	entry, stop := 100.0, 90.0                                    // long, risk 10
	wick := Level{Key: "w", Kind: KindWickMicroscalp, Price: 115} // 1.5R
	far := Level{Key: "k", Kind: KindKeyLevel, Price: 140}        // 4R
	if got := nextLevelBeyondRoom([]Level{wick, far}, entry, stop, SideLong, 2); got != 115 {
		t.Fatalf("a 1.5R wick target must be chosen at RoomMultiple 2, got %.2f", got)
	}
	if m := targetRoomMultiple([]Level{wick, far}, 115, 2); m != 1 {
		t.Fatalf("room multiple for a wick target = %.2f, want 1", m)
	}
	if refuse, why := roomRefusal(entry, stop, 115, false, targetRoomMultiple([]Level{wick, far}, 115, 2)); refuse {
		t.Fatalf("a 1.5R wick target must pass the room rule: %s", why)
	}
	if m := targetRoomMultiple([]Level{wick, far}, 140, 2); m != 2 {
		t.Fatalf("a key-level target keeps RoomMultiple 2, got %.2f", m)
	}
	if m := targetRoomMultiple([]Level{wick}, 115, 0); m != 0 {
		t.Fatalf("RoomMultiple 0 (room rule off) stays off for a wick target, got %.2f", m)
	}
	htf := Level{Key: "h", Kind: KindHTFTrigger, Price: 115}
	if got := nextLevelBeyondRoom([]Level{htf, far}, entry, stop, SideLong, 2); got != 140 {
		t.Fatalf("the HTF trigger keeps the 2R skip, got %.2f", got)
	}
	near := Level{Key: "w2", Kind: KindWickMicroscalp, Price: 108} // 0.8R
	if got := nextLevelBeyondRoom([]Level{near, far}, entry, stop, SideLong, 2); got != 140 {
		t.Fatalf("a wick under 1R falls through, got %.2f", got)
	}
	// Short mirror.
	ws := Level{Key: "ws", Kind: KindWickMicroscalp, Price: 85}
	if got := nextLevelBeyondRoom([]Level{ws, {Key: "k2", Kind: KindKeyLevel, Price: 60}}, 100, 110, SideShort, 2); got != 85 {
		t.Fatalf("short: a 1.5R wick target must be chosen, got %.2f", got)
	}
}
