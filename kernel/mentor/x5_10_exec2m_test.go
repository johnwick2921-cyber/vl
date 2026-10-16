package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// x5_10Bars is the 4-bar fixture for the X5-10 pin (09:00–09:03 CT, RTH).
// On the 1m chart the last two bars form a long ISB (bar 2 green candle 1,
// bar 3's body inside bar 2's range); on the 2m chart candle 1 (bars 0–1)
// is a doji — open == close — so the 2m read is NO ISB at all. The head
// bars keep the EMA 34 target well above the entry (same trick as isbFixture).
func x5_10Bars() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 122, 120, 120.5),
		rthBars(1, 121, 121.5, 120, 121), // 2m candle 1: open 121 == close 121 (doji)
		rthBars(2, 99, 106, 98.5, 106),   // green candle 1 (1m)
		rthBars(3, 101, 102.1, 100.9, 102),
	}
}

// x5_10Emits counts the ISB entry intents a Tick leaves.
func x5_10Emits(e *Evaluator, bars []market.Kline, now int64) int {
	n := 0
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopLimitEntry && in.Setup == "ISB" {
			n++
		}
	}
	return n
}

// TestX5_10Exec2mAfter30mSwitchesISBReadTo2m — X5-10 (default ON since release
// #31): after the first 30 minutes of RTH (09:00 CT) the ISB entry is read on
// the 2m chart instead of the 1m ("sau 30 phút em sẽ chuyển qua khung 2 phút"
// [X5 @00:41–01:17; X11 @17:06–17:32]). The pin drives Tick at the production
// call site: the 1m read emits the long ISB, the 2m read (doji candle 1) does
// not, and the switch only applies at/after 09:00 CT.
func TestX5_10Exec2mAfter30mSwitchesISBReadTo2m(t *testing.T) {
	bars := x5_10Bars()
	now := bars[len(bars)-1].CloseTime + 1 // 09:03 CT

	// OFF (explicit): the 1m read emits the ISB.
	cfgOff := DefaultConfig()
	cfgOff.Enabled = true
	cfgOff.Exec2mAfter30m = false
	if got := x5_10Emits(newISBEval(cfgOff), bars, now); got != 1 {
		t.Fatalf("knob OFF (1m read) must emit the ISB, got %d", got)
	}

	// ON + after the first 30 min of RTH: the 2m read drops the ISB.
	cfgOn := DefaultConfig()
	cfgOn.Enabled = true
	cfgOn.Exec2mAfter30m = true
	if got := x5_10Emits(newISBEval(cfgOn), bars, now); got != 0 {
		t.Fatalf("knob ON (2m read) must drop the ISB, got %d", got)
	}

	// ON + BEFORE the first 30 min of RTH (08:50–08:53 CT): still the 1m read.
	pre := []market.Kline{
		barAt(auditMs(2026, 9, 15, 8, 50, 0), 121, 122, 120, 120.5),
		barAt(auditMs(2026, 9, 15, 8, 51, 0), 121, 121.5, 120, 121),
		barAt(auditMs(2026, 9, 15, 8, 52, 0), 99, 106, 98.5, 106),
		barAt(auditMs(2026, 9, 15, 8, 53, 0), 101, 102.1, 100.9, 102),
	}
	preNow := pre[len(pre)-1].CloseTime + 1 // 08:53 CT
	if got := x5_10Emits(newISBEval(cfgOn), pre, preNow); got != 1 {
		t.Fatalf("knob ON before 09:00 CT must keep the 1m read, got %d", got)
	}
}

func barAt(openMs int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, CloseTime: openMs + 59_999, Open: o, High: h, Low: l, Close: c}
}

// x5BarsAt is the x5_10Bars doji/ISB fixture shifted so the last bar's
// CloseTime + 1 is exactly `now`: on the 1m chart bars 2–3 form a long ISB
// (emit), on the 2m chart candle 1 (bars 0–1) is a doji (no ISB). Works at any
// even-UTC-aligned minute.
func x5BarsAt(now int64) []market.Kline {
	o := [4]float64{121, 121, 99, 101}
	h := [4]float64{122, 121.5, 106, 102.1}
	l := [4]float64{120, 120, 98.5, 100.9}
	c := [4]float64{120.5, 121, 106, 102}
	bars := make([]market.Kline, 4)
	for i := 0; i < 4; i++ {
		open := now - int64(4-i)*60_000
		bars[i] = market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: o[i], High: h[i], Low: l[i], Close: c[i]}
	}
	return bars
}

// TestX5_10Exec2mScheduleAtTick — the schedule AT THE TICK CALL SITE (canon 53):
// with the knob ON, an overnight / Europe-open ISB is read on the 2m (the doji
// candle 1 → no ISB → no isb_day_not_measured refusal), while the RTH-open
// +30 window (08:45 CT) stays on the 1m and EMITS. Knob OFF stays 1m everywhere.
// MUTANT (CTO): restore `rthMinuteOf(now) >= 9*60` AT eval.go's call site (leave
// the predicate alone) → the 01:30/03:10 rows read 1m → the ISB forms and the
// pre-open day gate refuses it (isb_day_not_measured) → RED.
func TestX5_10Exec2mScheduleAtTick(t *testing.T) {
	// Overnight + Europe open, knob ON: the 2m read finds NO ISB.
	for _, c := range []struct {
		label string
		now   int64
	}{
		{"01:30 CT (overnight)", auditMs(2026, 9, 15, 1, 30, 0)},
		{"03:10 CT (Europe open)", auditMs(2026, 9, 15, 3, 10, 0)},
	} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.Exec2mAfter30m = true
		e := newISBEval(cfg)
		e.Tick(x5BarsAt(c.now), c.now)
		if e.State.Refusals["isb_day_not_measured"] != 0 {
			t.Fatalf("%s knob ON: the 2m read must find no ISB (no isb_day_not_measured); got %v", c.label, e.State.Refusals)
		}
	}

	// Knob OFF (explicit) at 01:30: the 1m read forms the ISB → the pre-open
	// day gate refuses it (isb_day_not_measured) — proves the 1m read when OFF.
	cfgOff := DefaultConfig()
	cfgOff.Enabled = true
	cfgOff.Exec2mAfter30m = false
	eOff := newISBEval(cfgOff)
	eOff.Tick(x5BarsAt(auditMs(2026, 9, 15, 1, 30, 0)), auditMs(2026, 9, 15, 1, 30, 0))
	if eOff.State.Refusals["isb_day_not_measured"] == 0 {
		t.Fatalf("01:30 CT knob OFF: the 1m read must form the ISB (isb_day_not_measured present); got %v", eOff.State.Refusals)
	}

	// RTH open +30 window (08:45 CT), knob ON: still the 1m read → emits.
	cfgRth := DefaultConfig()
	cfgRth.Enabled = true
	cfgRth.Exec2mAfter30m = true
	if got := x5_10Emits(newISBEval(cfgRth), x5BarsAt(auditMs(2026, 9, 15, 8, 45, 0)), auditMs(2026, 9, 15, 8, 45, 0)); got != 1 {
		t.Fatalf("08:45 CT knob ON: the 1m read must emit the ISB, got %d", got)
	}
}

// TestExec2mReads2mSchedule — the X5-10 timeframe schedule (owner order
// 2026-10-13 ~23:00 CT "fix all"; sources X5 @00:07–01:17, X11 @17:06–19:35):
// with the knob ON the ISB reads the 2m at every minute EXCEPT the RTH open
// +30 minutes (08:30 ≤ t < 09:00 CT), when it stays on the 1m. Overnight and
// the Europe open (3am Texas) are 2m. The predicate is the ONE definition the
// Tick call site reads — its input is the just-closed 1m bar's CLOSE instant
// (BarCloseInstant = CloseTime+1), so each row passes the close instant of a
// bar that OPENED at the labelled minute. MUTANT (CTO): restore
// `rthMinuteOf(now) >= 9*60` → the 01:30/03:10 rows read 1m → RED; and the
// off-by-one mutant (`rthMinuteOf(now)` without the open-minute shift) →
// the 08:29 bar reads 1m and the 08:59 bar reads 2m → RED.
func TestExec2mReads2mSchedule(t *testing.T) {
	// closeMs is the CLOSE instant of a 1m bar that OPENED at hh:mm — the exact
	// value BarCloseInstant passes the Tick call site (fold #565, D6).
	closeMs := func(h, m int) int64 { return auditMs(2026, 9, 15, h, m, 0) + 60_000 }
	cases := []struct {
		label string
		now   int64
		want  bool // true = 2m read
	}{
		{"08:29 bar (pre-open — 2m)", closeMs(8, 29), true},
		{"08:30 bar (RTH open, window start)", closeMs(8, 30), false},
		{"08:45 bar (RTH open +15m)", closeMs(8, 45), false},
		{"08:59 bar (last 1m bar of the window)", closeMs(8, 59), false},
		{"09:00 bar (window end)", closeMs(9, 0), true},
		{"09:05 bar (after +30m)", closeMs(9, 5), true},
		{"01:30 bar (overnight)", closeMs(1, 30), true},
		{"03:10 bar (Europe open)", closeMs(3, 10), true},
		{"15:00 bar (RTH close)", closeMs(15, 0), true},
		{"23:59 bar (late evening)", closeMs(23, 59), true},
	}
	for _, c := range cases {
		if got := exec2mReads2m(c.now); got != c.want {
			t.Fatalf("%s: exec2mReads2m = %v, want %v (2m=%v)", c.label, got, c.want, c.want)
		}
	}
}

// TestX5_10Exec2mReadsClosedBucketsNotTheFormingOne — P1: on a 1m close that
// OPENS a 2m bucket, the read must be the previous two CLOSED 2m candles, not
// the half-formed one (barsTF would flush the partial bucket and the ISB would
// read a half-built candle). The fixture ends at 09:04 — the FIRST minute of
// the [09:04,09:05] bucket. The closed 2m read is (doji A, B) → no ISB; the
// barsTF read would be (B, half-formed 09:04) → ISB.
func TestX5_10Exec2mReadsClosedBucketsNotTheFormingOne(t *testing.T) {
	bars := []market.Kline{
		rthBars(0, 140, 141, 139, 140), // bucket A open 140
		rthBars(1, 140, 141, 139, 140), // bucket A close 140 → 2m doji
		rthBars(2, 99, 106, 98.5, 106), // bucket B (green)
		rthBars(3, 101, 105, 100, 104), // bucket B close 104
		rthBars(4, 103, 104, 102, 103), // 09:04 — first minute of the next 2m bucket
	}
	now := bars[len(bars)-1].CloseTime + 1 // 09:04 CT
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = true
	if got := x5_10Emits(newISBEval(cfg), bars, now); got != 0 {
		t.Fatalf("the 2m read must use CLOSED buckets (candle 1 is a doji), got %d ISB", got)
	}
}

// TestExec2mOnISBStackingCounts2mCandles — D1 (fold #565) at the production call
// site: with the knob ON a 2m ISB arm's stacking counts CLOSED 2m candles, not
// 1m candles. A 2m arm placed on the [09:02,09:03] ISB candle is ticked on the
// odd 1m minute only when its 2m candle has CLOSED — so "candle 4 did not fill →
// cancel" fires at the close of the 4th 2m candle (09:09), NOT after three 1m
// candles (09:06). MUTANT (D1, 1m stacking) → cancels at 09:06 → RED.
func TestExec2mOnISBStackingCounts2mCandles(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = true
	e := New(cfg)
	// bars 0-1 = 2m candle 1; bars 2-3 = 2m candle 2 (the ISB candle, I1:
	// high 106 / low 98.5). Bars 4-9 are doji 1m bars whose 2m candles stay
	// inside I1 (body 102).
	seed := []market.Kline{
		rthBars(0, 121, 121.5, 120, 120.5),
		rthBars(1, 121, 121.5, 120, 121),
		rthBars(2, 99, 106, 98.5, 106),
		rthBars(3, 101, 102, 100, 101.5),
	}
	now := seed[len(seed)-1].CloseTime + 1 // 09:03 CT
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.ISBArms = map[string]ISBArm{
		"arm-2m": {FirstBar: market.Kline{High: 106, Low: 98.5}, Inside: 0, Side: SideLong, TFMin: 2, LastTick: seed[3].CloseTime},
	}

	bars := append([]market.Kline(nil), seed...)
	step := func(i int) (cancel, extend bool, inside int) {
		bars = append(bars, rthBars(i, 102, 102.5, 101.5, 102))
		now = bars[len(bars)-1].CloseTime + 1
		for _, in := range e.Tick(bars, now) {
			if in.ArmID != "arm-2m" {
				continue
			}
			switch in.Action {
			case CancelArm:
				cancel = true
			case ExtendArm:
				extend = true
			}
		}
		return cancel, extend, e.State.ISBArms["arm-2m"].Inside
	}

	// 09:04 — the 2m candle is still forming: no tick (skip).
	if c, x, in := step(4); c || x || in != 0 {
		t.Fatalf("09:04 (2m forming) must not tick the 2m arm: cancel=%v extend=%v inside=%d", c, x, in)
	}
	// 09:05 — 2m candle 3 closes: Inside 1 (hold), extend, no cancel.
	if c, x, in := step(5); c || !x || in != 1 {
		t.Fatalf("09:05 (2m candle 3) must extend and hold: cancel=%v extend=%v inside=%d", c, x, in)
	}
	// 09:06 — 2m forming: skip.
	if c, x, in := step(6); c || x || in != 1 {
		t.Fatalf("09:06 (2m forming) must not tick: cancel=%v extend=%v inside=%d", c, x, in)
	}
	// 09:07 — 2m candle 4 closes: Inside 2 (hold).
	if c, x, in := step(7); c || !x || in != 2 {
		t.Fatalf("09:07 (2m candle 4) must extend and hold: cancel=%v extend=%v inside=%d", c, x, in)
	}
	// 09:08 — 2m forming: skip.
	if c, x, in := step(8); c || x || in != 2 {
		t.Fatalf("09:08 (2m forming) must not tick: cancel=%v extend=%v inside=%d", c, x, in)
	}
	// 09:09 — the 4th 2m candle closes: CANCEL (candle 4 did not fill); the arm
	// is then deleted from the registry.
	if c, x, _ := step(9); !c || x {
		t.Fatalf("09:09 (2m candle 5 = stack candle 4) must cancel, not extend: cancel=%v extend=%v", c, x)
	}
	if _, ok := e.State.ISBArms["arm-2m"]; ok {
		t.Fatal("the cancelled 2m arm must be deleted from ISBArms")
	}
}
