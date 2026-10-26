package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// C52-W10 B20 — the X11 5m pullback warning is DIRECTION-AWARE and has its
// mirror [Go over lớp học part1.srt @00:13:21–00:15:19, quote @00:14:50–
// 00:15:02]: in a 5m UPTREND (swing structure HH+HL) a candle that breaks the
// prior candle's LOW warns a LONG; in a 5m DOWNTREND a candle that breaks the
// prior candle's HIGH warns a SHORT. A SHORT taken in the uptrend's own
// pullback is the trade the warning predicts, so it is NOT cut. Before the fix
// the warning was "prev 5m candle green + low broken" and cut any ISB side.

// pwCandle is one 5m candle spec for the 1m builder below.
type pwCandle struct {
	hi, lo float64
	bull   bool
}

// pwBase is 10:00 CT on 2026-09-15: past the 08:30-09:30 open window, so the
// 4h/1h gate is off and the ISB side is not refused against the history's own
// 4h read.
func pwBase() int64 { return auditMs(2026, 9, 15, 10, 0, 0) }

// pwBar is rthBars on the 10:00 base.
func pwBar(i int, o, h, l, c float64) market.Kline {
	ot := pwBase() + int64(i)*60_000
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
}

// pwOneMinute expands 5m candle specs into 1m bars that end exactly at 10:00 CT,
// so the 5m buckets close on :00/:05 and aggregate back to the specs (open at
// one extreme, close at the other).
func pwOneMinute(specs []pwCandle) []market.Kline {
	start := pwBase() - int64(len(specs))*5*60_000
	var out []market.Kline
	mk := func(i int, o, h, l, c float64) {
		ot := start + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c})
	}
	for n, sp := range specs {
		first, last := sp.lo, sp.hi
		if !sp.bull {
			first, last = sp.hi, sp.lo
		}
		i := n * 5
		mk(i, first, first, first, first)
		mk(i+1, first, sp.hi, sp.lo, last)
		mk(i+2, last, sp.hi, sp.lo, first)
		mk(i+3, first, sp.hi, sp.lo, last)
		mk(i+4, last, last, last, last)
	}
	return out
}

// pwSeries shifts the CE-8 fixture highs/lows (course_entries_test.go) by
// `shift` points. The ISB fixture sits at 100 and needs an EMA 34 target
// BEYOND the entry, so the 5m history is shifted above it for a long ISB and
// below it for a short one (the EMA 34 follows the history).
func pwSeries(hl [][2]float64, bull bool, shift float64) []pwCandle {
	var out []pwCandle
	for _, x := range hl {
		out = append(out, pwCandle{hi: x[0] + shift, lo: x[1] + shift, bull: bull})
	}
	return out
}

var (
	// HH+HL; the last candle (13,8) breaks the prior candle's (16,11) low.
	pwUp = [][2]float64{{10, 5}, {12, 7}, {8, 3}, {9, 4}, {14, 9}, {11, 6}, {10, 5}, {16, 11}, {13, 8}}
	// LH+LL; the last candle (15,9) breaks the prior candle's (14,8) high.
	pwDown = [][2]float64{{20, 15}, {18, 12}, {22, 17}, {21, 16}, {16, 10}, {17, 11}, {19, 14}, {14, 8}, {15, 9}}
)

func TestPullbackWarning5mDirectionAware(t *testing.T) {
	k := func(hl [][2]float64, bull bool) []market.Kline {
		var out []market.Kline
		for i, x := range hl {
			o := int64(i) * 300_000
			op, cl := x[1], x[0]
			if !bull {
				op, cl = x[0], x[1]
			}
			out = append(out, market.Kline{OpenTime: o, CloseTime: o + 299_999, High: x[0], Low: x[1], Open: op, Close: cl})
		}
		return out
	}
	if got := pullbackWarning5m(k(pwUp, true)); got != SideLong {
		t.Fatalf("5m uptrend + prior low broken must warn the LONG, got %q", got)
	}
	if got := pullbackWarning5m(k(pwDown, false)); got != SideShort {
		t.Fatalf("5m downtrend + prior high broken must warn the SHORT (the mirror), got %q", got)
	}
	// Uptrend structure, latest candle holds above the prior low → no warning.
	held := append(append([][2]float64{}, pwUp[:8]...), [2]float64{18, 12})
	if got := pullbackWarning5m(k(held, true)); got != "" {
		t.Fatalf("a candle holding the prior low must not warn, got %q", got)
	}
	// Downtrend structure with the latest candle breaking the prior LOW (not the
	// high) is no warning for either side.
	if got := pullbackWarning5m(k(append(append([][2]float64{}, pwDown[:8]...), [2]float64{13, 7}), false)); got != "" {
		t.Fatalf("a downtrend candle that does not break the prior high must not warn, got %q", got)
	}
	// "5m uptrend" is the swing structure, not "the previous candle is green":
	// green candles with no HH+HL read as no trend → no warning.
	mixed := [][2]float64{{10, 5}, {12, 7}, {8, 3}, {9, 4}, {14, 9}}
	if got := pullbackWarning5m(k(mixed, true)); got != "" {
		t.Fatalf("no confirmed structure must not warn, got %q", got)
	}
	if got := pullbackWarning5m(k(pwUp[:1], true)); got != "" {
		t.Fatalf("one candle must not warn, got %q", got)
	}
}

// pwTick runs the PRODUCTION Tick on the 5m prefix + a 3-bar 1m ISB fixture
// (the isb_in_range_test.go fixture, mirrored for the short) and returns the
// emitted ISB intent.
func pwTick(t *testing.T, hl [][2]float64, bull bool, side Side) (Intent, *Evaluator) {
	t.Helper()
	shift := 120.0
	if side == SideShort {
		shift = 55.0
	}
	specs := pwSeries(hl, bull, shift)
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0
	cfg.Exec2mAfter30m = false // read the 1m ISB pair; the 5m warning read is frame-independent
	cfg.FifteenMinIBRule = ""  // the synthetic history draws its own 15m box; this fixture pins the size flag, not the CE-1 gate
	var head, prev, cur market.Kline
	// No standing 5m ISB box: the Tick builds the 5m trigger line from the
	// history, and that line governs the ISB side (a candle that broke the prior
	// 5m low IS a sell trigger, so after the warning candle only the SHORT ISB
	// survives the trigger-side filter, the buy side is refused upstream).
	if side == SideLong {
		head = pwBar(0, 121, 121.5, 120.5, 120)
		prev = pwBar(1, 98, 102.9, 97.5, 99)
		cur = pwBar(2, 99.5, 101, 98, 100)
	} else {
		head = pwBar(0, 79, 79.5, 78.5, 80)
		prev = pwBar(1, 102, 102.5, 97.1, 101)
		cur = pwBar(2, 100.5, 102, 99, 100)
	}
	if !IsISB(prev, cur) || ISBDirection(prev) != side {
		t.Fatalf("fixture: %s ISB", side)
	}
	bars := append(pwOneMinute(specs), head, prev, cur)
	e := New(cfg)
	orb := ORB{Day: dayStartCT(pwBase()), High: 90, Low: 85, Drawn: true, Escaped: side}
	if side == SideShort {
		orb.High, orb.Low = 115, 110 // price escaped BELOW the opening range
	}
	e.State.ORB = orb
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(pwBase()).In(ctime())), Verdict: DayTrade}
	// The synthetic history happens to end in a 15m/30m inside pair; that box is
	// not what this fixture pins, so mark those pairs as already seen (the Tick
	// builds one box per pair and never rebuilds a seen one).
	now := cur.CloseTime + 1
	for _, tf := range []int{15, 30} {
		cb := e.closedBucketsTFMemo(bars, tf, now)
		if len(cb) >= 2 {
			if bx, ok := ISBBoxFrom5m(cb[len(cb)-2], cb[len(cb)-1]); ok {
				if tf == 15 {
					e.State.LastISBBox15mAt = bx.AtTime
				} else {
					e.State.LastISBBox30mAt = bx.AtTime
				}
			}
		}
	}
	// Pin the 5m trigger line to the side under test: TriggerTick never re-reads
	// a bucket at or below LastBucket, so the history cannot overwrite it. (The
	// live line is a function of 5m closes; this fixture pins the SIZE FLAG, not
	// the trigger.) 4h/1h: gate is off at 10:00 CT.
	trigPx := 90.0
	if side == SideShort {
		trigPx = 110
	}
	e.State.Trigger = TriggerLine{Dir: side, Price: trigPx, LastBucket: now}
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if in.Action == PlaceStopLimitEntry && in.Setup == "ISB" {
			return in, e
		}
	}
	t.Fatalf("the %s ISB must emit; got %+v (refusals %v) trigger %+v", side, ins, e.State.Refusals, e.State.Trigger)
	return Intent{}, e
}

// TestPullbackWarnOnISBTick pins the flag through the production emit site
// (`chosen.Flag = e.isbFlagsFor(execCur, chosen.Side, …)`). MUTANT: pass the
// wrong side / drop the side compare in isbFlagsFor → the short in the uptrend
// carries the flag (RED), or the mirror short is not flagged (RED).
func TestPullbackWarnOnISBTick(t *testing.T) {
	// 5m uptrend, last candle broke the prior low: the Tick's own sell trigger
	// lets the SHORT ISB through. It is the very trade the pullback predicts, so
	// it must NOT be cut (the side-blind warning cut it before the fix).
	in, e := pwTick(t, pwUp, true, SideShort)
	if HasFlag(in.Flag, FlagPullbackWarn) {
		t.Fatalf("a SHORT ISB taken in the 5m uptrend's pullback must NOT carry pullback_warn, got %q", in.Flag)
	}
	// The warning the production Tick computed is for the LONG side.
	probe := market.Kline{High: 103, Low: 99, Close: 100}
	if got := e.isbFlagsFor(probe, SideLong, nil, nil); !HasFlag(got, FlagPullbackWarn) {
		t.Fatalf("the Tick must warn a LONG in a 5m uptrend whose last candle broke the prior low, got %q", got)
	}
	// Mirror: 5m downtrend, last candle broke the prior HIGH: the LONG ISB (buy
	// trigger) is the pullback trade and is not cut; the warning is for a SHORT.
	in, e = pwTick(t, pwDown, false, SideLong)
	if HasFlag(in.Flag, FlagPullbackWarn) {
		t.Fatalf("mirror: a LONG ISB in the 5m downtrend's pullback must NOT carry pullback_warn, got %q", in.Flag)
	}
	if got := e.isbFlagsFor(probe, SideShort, nil, nil); !HasFlag(got, FlagPullbackWarn) {
		t.Fatalf("mirror: the Tick must warn a SHORT in a 5m downtrend whose last candle broke the prior high, got %q", got)
	}
}

// pwScaled maps the pw fixture highs/lows onto the reverse-ISB fixture's price
// band (~99-104, around its 101 tape) keeping the swing ORDER, so the 1m EMA 9
// of the tape still sits inside the ISB candle.
func pwScaled(hl [][2]float64, base float64, bull bool) []pwCandle {
	var out []pwCandle
	for _, x := range hl {
		out = append(out, pwCandle{hi: base + 0.3*x[0], lo: base + 0.3*x[1], bull: bull})
	}
	return out
}

// pwReverseTick runs the PRODUCTION Tick on isbReverseFixture (a LONG reverse
// ISB at the EMA 9: SHORT-pointing pair, trigger LONG) with the 40 flat 1m bars
// replaced by a synthetic 5m history, and returns the emitted reverse intent.
func pwReverseTick(t *testing.T, hl [][2]float64, bull bool, base float64) (Intent, *Evaluator) {
	t.Helper()
	e, bars, now := isbReverseFixture(t)
	isbPair := bars[len(bars)-2:]
	t0 := isbPair[0].OpenTime - int64(len(hl))*5*60_000
	var hist []market.Kline
	for n, sp := range pwScaled(hl, base, bull) {
		first, last := sp.lo, sp.hi
		if !sp.bull {
			first, last = sp.hi, sp.lo
		}
		mk := func(i int, o, h, l, c float64) {
			ot := t0 + int64(n*5+i)*60_000
			hist = append(hist, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c})
		}
		mk(0, first, first, first, first)
		mk(1, first, sp.hi, sp.lo, last)
		mk(2, last, sp.hi, sp.lo, first)
		mk(3, first, sp.hi, sp.lo, last)
		mk(4, last, last, last, last)
	}
	bars = append(hist, isbPair...)
	// Pin the 5m trigger LONG (the reverse ISB's side): a broken 5m low would
	// flip a live trigger to sell; this fixture pins the SIZE FLAG, not the trigger.
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 101, LastBucket: math.MaxInt64}
	// As in pwTick: the synthetic history draws its own 15m/30m inside pairs;
	// mark them already seen (this fixture pins the size flag, not the box gates).
	for _, tf := range []int{15, 30} {
		cb := e.closedBucketsTFMemo(bars, tf, now)
		if len(cb) >= 2 {
			if bx, ok := ISBBoxFrom5m(cb[len(cb)-2], cb[len(cb)-1]); ok {
				if tf == 15 {
					e.State.LastISBBox15mAt = bx.AtTime
				} else {
					e.State.LastISBBox30mAt = bx.AtTime
				}
			}
		}
	}
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && (in.Setup == "ISB" || in.Setup == "PHL") && in.Side == SideLong {
			return in, e
		}
	}
	t.Fatalf("the reverse LONG ISB must emit; got %+v (refusals %v)", ins, e.State.Refusals)
	return Intent{}, e
}

// TestPullbackWarnOnReverseISBTick pins the REVERSE ISB emit site
// (`in.Flag = e.isbFlagsFor(execCur, in.Side, levels, boxes)`) through the
// production Tick. A reverse LONG ISB in a 5m uptrend whose last candle broke
// the prior low is the warned side (carries pullback_warn); the same reverse
// LONG in a 5m downtrend whose last candle broke the prior high is the
// UNwarned side (the warning is for the SHORT), so it must not carry it.
// MUTANT: flip the side argument at the reverse site (in.Side -> opposite) →
// the warned case loses the flag and the unwarned case gains it (RED).
func TestPullbackWarnOnReverseISBTick(t *testing.T) {
	in, e := pwReverseTick(t, pwUp, true, 99)
	if e.pullbackWarn5m != SideLong {
		t.Fatalf("fixture: the 5m uptrend must warn the LONG, got %q", e.pullbackWarn5m)
	}
	if !HasFlag(in.Flag, FlagPullbackWarn) {
		t.Fatalf("a reverse LONG ISB in a 5m uptrend whose last candle broke the prior low must carry pullback_warn, got %q (%+v)", in.Flag, in)
	}
	in, e = pwReverseTick(t, pwDown, false, 97)
	if e.pullbackWarn5m != SideShort {
		t.Fatalf("fixture: the 5m downtrend must warn the SHORT, got %q", e.pullbackWarn5m)
	}
	if HasFlag(in.Flag, FlagPullbackWarn) {
		t.Fatalf("a reverse LONG ISB must NOT carry pullback_warn when the warning is for the SHORT, got %q", in.Flag)
	}
}
