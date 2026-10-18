package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── RELEASE #25 COURSE-EXITS (lane mentor-course-exits) — kernel call-site pins ──

// cx1SwingTape is the swing reject short of TestSwingRejectRoomRule with 40
// flat 5m candles at 10100 BEFORE the touch, inside the 05:00 4h bucket — so
// the warmed 5m EMA 34 sits ON the trade side (below a short's entry) and
// closer than 2R: the swing's first target, which R68 measured as an obstacle.
func cx1SwingTape(t *testing.T) ([]market.Kline, int64) {
	t.Helper()
	var cur []market.Kline
	for i := 0; i < 40; i++ {
		m := 5 * i
		cur = append(cur, mk5m(t, 15, 5+m/60, m%60, 10100, 10102, 10098, 10100))
	}
	// 05:00 + 40×5m = 08:20 → the prev at 08:20, the touch at 08:25.
	cur = append(cur,
		mk5m(t, 15, 8, 20, 10100, 10110, 10095, 10105), // prev: below the line
		mk5m(t, 15, 8, 25, 10160, 10175, 10155, 10160), // touches the placed line, closes back below → reject
	)
	bars := swingTape(t, cur)
	return bars, cur[len(cur)-1].OpenTime + 5*60_000
}

func cx1Tick(t *testing.T, firstTarget bool) ([]Intent, *Evaluator) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CourseExits.SwingEMA34IsFirstTarget = firstTarget
	bars, now := cx1SwingTape(t)
	e := New(cfg)
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	seededWith(e, fullDepth, now)
	return e.Tick(bars, now), e
}

// TestCX1SwingEMA34IsTheFirstTargetNotAnObstacle — D5.2 p2 @01:40–01:48
// "Target đầu tiên anh chị, Hold thẳng về email 34 05 phút luôn cho em": the 5m
// EMA 34 is the swing's FIRST TARGET. Knob OFF (R68): the on-side EMA sits
// closer than 2R → refused "room". Knob ON (the default): the swing emits with
// its target AT the 5m EMA 34 (further than 1R). Mutant: swingRoomFilterRefuses
// ignores the knob → the ON leg goes RED.
func TestCX1SwingEMA34IsTheFirstTargetNotAnObstacle(t *testing.T) {
	if !DefaultConfig().CourseExits.SwingEMA34IsFirstTarget {
		t.Fatal("CX-1 default must be ON (case a)")
	}
	off, eOff := cx1Tick(t, false)
	if entriesOf(off, "SWING4H") != 0 || eOff.State.Refusals["room"] == 0 {
		t.Fatalf("knob OFF (R68) must refuse the swing on the room rule; intents %+v refusals %v", off, eOff.State.Refusals)
	}
	on, eOn := cx1Tick(t, true)
	if entriesOf(on, "SWING4H") != 1 {
		t.Fatalf("knob ON: the swing must trade with the 5m EMA 34 as its first target; intents %+v refusals %v", on, eOn.State.Refusals)
	}
	var in Intent
	for _, x := range on {
		if x.Setup == "SWING4H" && x.Action == PlaceStopEntry {
			in = x
		}
	}
	risk := in.Stop - in.Price
	if in.Side != SideShort || risk <= 0 {
		t.Fatalf("want a short swing with a positive risk: %+v", in)
	}
	oneR := in.Price - risk
	if !(in.Target < oneR) || in.Price-in.Target >= 2*risk {
		t.Fatalf("target %.2f must be the on-side 5m EMA 34: beyond 1R (%.2f) yet inside 2R (%.2f); intent %+v", in.Target, oneR, in.Price-2*risk, in)
	}
}

// TestCX6ReverseISBTaggedPHLWhenOn — D5.4 @03:52–03:56 "Nó là bộ tên solo 2,
// chứ nó không phải là Insa 3": knob ON (the course-defaults-30 default) tags
// the reverse ISB PHL (long) and KEEPS the ISB location size flags (P2-2); OFF
// keeps "ISB". Mutant: the retag call removed from eval.go → ON RED.
func TestCX6ReverseISBTaggedPHLWhenOn(t *testing.T) {
	if !DefaultConfig().CourseExits.ReverseISBAsPHL {
		t.Fatal("CX-6 default must be ON (course-defaults-30)")
	}
	for _, on := range []bool{false, true} {
		e, bars, now := isbReverseFixture(t)
		e.Cfg.CourseExits.ReverseISBAsPHL = on
		ins := e.Tick(bars, now)
		var got *Intent
		for i := range ins {
			x := &ins[i]
			if x.Action == PlaceStopEntry && x.Side == SideLong && strings.Contains(x.Reason, "reverse ISB") {
				got = x
			}
		}
		if got == nil {
			t.Fatalf("on=%v: the reverse ISB must fire; intents %+v refusals %v", on, ins, e.State.Refusals)
		}
		want := "ISB"
		if on {
			want = "PHL"
		}
		if got.Setup != want {
			t.Fatalf("on=%v: reverse ISB tagged %q, want %q", on, got.Setup, want)
		}
	}
}

// TestCX6ReverseISBRetagKeepsLocationFlags — P2-2: the retag renames the setup
// (ISB → PHL/PLH) but must NOT clear the ISB location size flags, so a reverse
// ISB inside a range or at an old extreme still sizes 3, never base 5. Mutant:
// `in.Flag = ""` in reverseISBRetag → RED.
func TestCX6ReverseISBRetagKeepsLocationFlags(t *testing.T) {
	cfg := DefaultConfig()
	both := FlagISBAtOldExtreme + "|" + FlagISBInRange
	for _, side := range []Side{SideLong, SideShort} {
		in := Intent{Setup: "ISB", Side: side, Flag: both, Reason: "reverse ISB"}
		out := reverseISBRetag(in, cfg)
		want := "PHL"
		if side == SideShort {
			want = "PLH"
		}
		if out.Setup != want {
			t.Fatalf("side=%v: retagged setup %q, want %q", side, out.Setup, want)
		}
		if !HasFlag(out.Flag, FlagISBAtOldExtreme) || !HasFlag(out.Flag, FlagISBInRange) {
			t.Fatalf("side=%v: the retag must KEEP the ISB location flags, got %q", side, out.Flag)
		}
	}
}

// cx8EMA9Visits runs the B23 visit tape with the touched level being an EMA 9
// line (Kind ema9) under the given mode.
func cx8EMA9Visits(t *testing.T, mode string) *Evaluator {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.KeyLevelTFMinutes = 1
	cfg.LocTriggerFilter = false
	cfg.EMALocationTFMinutes = 0
	cfg.LevelMaxVisits = 3
	cfg.Exec2mAfter30m = false // the B23 visit-count pins read the 1m touch
	cfg.CourseExits.EMA9VisitCap = mode
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 98, 99, 98, 98.5), mk(1, 99, 101, 99, 99.5), mk(2, 99, 99.6, 99, 99.5),
		mk(3, 99, 101, 99, 99.5), mk(4, 99, 99.6, 99, 99.5), mk(5, 99, 101, 99, 99.5),
		mk(6, 99, 99.6, 99, 99.5), mk(7, 99, 101, 99, 99.5),
	}
	now := bars[7].OpenTime + 59_999
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	// the seeded level set carries the EMA 9 line from State.EMA9 (eval.go
	// seededLevels) — the line sits at 100 for the whole tape.
	e.State.EMA9 = 100
	e.State.ORB = ORB{Day: dayStartCT(now), High: 130, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 200}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	for i := 2; i <= len(bars); i++ {
		e.Tick(bars[:i], bars[i-1].CloseTime+1)
	}
	return e
}

// TestCX8EMA9VisitCapModes — the EMA 9 "knock knock" budget: shared (today,
// the default) refuses the 4th visit "level_visit_cap"; exempt never caps the
// EMA 9 (like EMA 34); per_level keys each EMA 9 price as its own door.
// Mutant: visitCapAppliesFor ignores exempt → the exempt leg RED.
func TestCX8EMA9VisitCapModes(t *testing.T) {
	if DefaultConfig().CourseExits.EMA9VisitCap != EMA9VisitShared {
		t.Fatal("CX-8 default must be shared (case b, the course states no EMA 9 visit rule)")
	}
	shared := cx8EMA9Visits(t, EMA9VisitShared)
	if shared.State.Refusals["level_visit_cap"] != 1 {
		t.Fatalf("shared: the 4th EMA 9 visit must be capped once; refusals %v visits %v", shared.State.Refusals, shared.State.Visits)
	}
	exempt := cx8EMA9Visits(t, EMA9VisitExempt)
	if exempt.State.Refusals["level_visit_cap"] != 0 {
		t.Fatalf("exempt: the EMA 9 must never be visit-capped; refusals %v", exempt.State.Refusals)
	}
	per := cx8EMA9Visits(t, EMA9VisitPerLevel)
	if per.State.Visits["ema9@100.00"] != 4 || per.State.Visits["ema9"] != 0 {
		t.Fatalf("per_level: visits must key at the EMA 9 price; visits %v", per.State.Visits)
	}
	if k := visitKeyFor(Level{Key: "ema9", Kind: KindEMA9, Price: 101.25}, EMA9VisitPerLevel); k != "ema9@101.25" {
		t.Fatalf("per_level key = %q", k)
	}
	if mode, ok := NormalizeEMA9VisitCap("bogus"); ok || mode != EMA9VisitShared {
		t.Fatalf("a bad value must fail closed to shared (ok=false): %q %v", mode, ok)
	}
}

// TestCX9ISBMaxStopRefusedAndNamed — knob mentor_isb_max_pts (default 30 —
// course-defaults-30: stop < 30 taken, ≥ 30 refused): with the knob at 1 the
// standard long ISB (stop > 1) is refused and counted "isb_stop_over_max",
// never folded into "isb_stop_twenties". Mutant: drop the isbOverMaxStop check
// in ISBStopVerdict → RED.
func TestCX9ISBMaxStopRefusedAndNamed(t *testing.T) {
	if DefaultConfig().CourseExits.ISBMaxStopPts != 30 {
		t.Fatal("CX-9 default must be 30 (course-defaults-30)")
	}
	ins, e := scopeISBTick(fullDepth)
	if entriesOf(ins, "ISB") != 1 {
		t.Fatalf("control: the ISB must emit with the default 30 (its stop 5 < 30); %+v %v", ins, e.State.Refusals)
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.CourseExits.ISBMaxStopPts = 1
	e2 := newISBEval(cfg)
	bars := isbFixture()
	now := bars[len(bars)-1].CloseTime + 1
	seededWith(e2, fullDepth, now)
	ins2 := e2.Tick(bars, now)
	if entriesOf(ins2, "ISB") != 0 {
		t.Fatalf("knob 1 pt: the ISB must be refused; %+v", ins2)
	}
	if e2.State.Refusals["isb_stop_over_max"] != 1 || e2.State.Refusals["isb_stop_twenties"] != 0 {
		t.Fatalf("the refusal must be named isb_stop_over_max: %v", e2.State.Refusals)
	}
}

// TestCX10KeypairRatio — X4 @06:31 keypair rule with the CX-10 ratio: the thin
// tape that trades at ratio 1 (today) sits out once the ratio scales the
// candle past the level gap. Mutant: drop the ratio factor → RED.
func TestCX10KeypairRatio(t *testing.T) {
	if DefaultConfig().CourseExits.KeypairCandleRatio != 1 {
		t.Fatal("CX-10 default must be 1 (today)")
	}
	e, bars, now := b21PairFixture(false)
	e.Cfg.CourseExits.KeypairCandleRatio = 50
	e.Tick(bars, now)
	if e.State.Refusals["keypair_candle_too_big"] == 0 {
		t.Fatalf("ratio 50: the thin pair must sit out; refusals %v", e.State.Refusals)
	}
}

func cx11Mk(t0 int64) func(i int, o, h, l, c float64) market.Kline {
	return func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
}

// cx11FTGH is the TestBoxLatchNeverRedrawsOnHigherHigh first-FTGH shape (top
// 102 / body 100) shifted by off.
func cx11FTGH(mk func(i int, o, h, l, c float64) market.Kline, base int, off float64) []market.Kline {
	return []market.Kline{
		mk(base+0, 100+off, 101+off, 99+off, 100+off),
		mk(base+1, 100+off, 99.5+off, 99+off, 100+off),
		mk(base+2, 100+off, 102+off, 99+off, 101+off),
		mk(base+3, 101+off, 101+off, 100+off, 100.5+off),
		mk(base+4, 100+off, 100+off, 99+off, 99.5+off),
		mk(base+5, 99.5+off, 101.5+off, 99+off, 100+off),
		mk(base+6, 100+off, 101+off, 99+off, 100+off),
	}
}

// TestCX11BoxMaxHeight — a built FTGH taller than mentor_box_max_height_pts is
// not a box: never latched, counted "box_too_tall". OFF (0): latched as today.
// Mutant: buildBoxes skips filterTallBoxes → RED.
func TestCX11BoxMaxHeight(t *testing.T) {
	for _, maxH := range []float64{0, 1.5} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.KeyLevelTFMinutes = 1
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.CourseExits.BoxMaxHeightPts = maxH
		t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
		bars := cx11FTGH(cx11Mk(t0), 0, 0)
		e := New(cfg)
		e.Tick(bars, bars[6].OpenTime+59_999)
		_, latched := e.State.LatchedBoxes["ftgh"]
		if maxH == 0 && !latched {
			t.Fatalf("off: the 2-pt FTGH must latch as today; %+v", e.State.LatchedBoxes)
		}
		if maxH > 0 && (latched || e.State.Refusals["box_too_tall"] == 0) {
			t.Fatalf("max %.1f: the 2-pt FTGH must not latch and must count box_too_tall; latched %+v refusals %v", maxH, e.State.LatchedBoxes, e.State.Refusals)
		}
	}
}

// TestCX11FreshRTHPair — knob OFF (today): the overnight FTGH latched at 08:0x
// stands through RTH. ON: at 08:30 CT the latch clears once and the RTH pair is
// drawn from RTH candles only. Mutant: boxLatchDayKey ignores the knob → RED.
func TestCX11FreshRTHPair(t *testing.T) {
	for _, on := range []bool{false, true} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.KeyLevelTFMinutes = 1
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.CourseExits.BoxFreshRTHPair = on
		pre := time.Date(2026, time.September, 15, 8, 0, 0, 0, ctime()).UnixMilli()
		rth := time.Date(2026, time.September, 15, 8, 30, 0, 0, ctime()).UnixMilli()
		bars := cx11FTGH(cx11Mk(pre), 0, 0)
		bars = append(bars, cx11FTGH(cx11Mk(rth), 0, 10)...)
		e := New(cfg)
		e.Tick(bars[:7], bars[6].OpenTime+59_999)
		if got := e.State.LatchedBoxes["ftgh"]; got.Top != 102 {
			t.Fatalf("on=%v: the overnight FTGH must latch first: %+v", on, got)
		}
		e.Tick(bars, bars[len(bars)-1].OpenTime+59_999)
		got := e.State.LatchedBoxes["ftgh"]
		if !on && got.Top != 102 {
			t.Fatalf("off: the overnight FTGH must stand through RTH (today): %+v", got)
		}
		if on && got.Top != 112 {
			t.Fatalf("on: the RTH pair must be drawn fresh at 08:30 (top 112): %+v", got)
		}
	}
}
