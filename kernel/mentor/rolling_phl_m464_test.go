package mentor

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m464 (release #59) — the D2.2 rolling PHL/PLH ladder re-arms on EVERY closed
// candle, and a refused rung never ends the ladder.
//
// Course [Ngày 2.2 PHL PLH, final notes, R7/R8/R9/R10 + WORKED EXAMPLE 1]:
//
//	D2.2-R7 [p1 @02:26–03:27]: "mình ra một lệnh buy stop trên đỉnh của từng
//	cây nến đã đóng xong… Cây nến này đóng xong nên mình ra tiếp." Screen
//	ladder 29,437.00 / 29,439.25 → 29,434.50 → 29,413.00 → 29,403.50 →
//	29,396.25 (last rung firm).
//	D2.2-R8 [p1 @03:27–04:16]: the candle that goes through the previous
//	candle's high fills the buy stop; the 10:15 candle ran ~+43 pts.
//	D2.2-R9 [p1 @04:19–05:43, 07:15–07:31]: stop = the LOW of the overtaken
//	candle — the small GREEN signal candle O 29,390.50 H 29,395.75
//	L 29,386.50 C 29,394.00; stop ray 29,386.00.
//	D2.2-R10 [p1 @04:14–05:43, screen]: entry ray = signal high + 0.50,
//	stop ray = signal low − 0.50 (never said aloud).
//	WORKED EXAMPLES §6/§7 [p2 @10:06–11:22, 21:03–23:30]: the 10:13 red
//	candle O 29,401.25 H 29,403.25 L 29,375.25 C 29,390.50.
//
// The bug (codecheck D2.2.verify R7/R9, P1): the 10:13 red rung (entry
// 29,404.25, stop 29,375.25, risk 29) hits the 25-pt StopCeilingPts, the old
// code cancelled AND ended the ladder, and the 10:14 small green candle — the
// mentor's own signal bar — could not restart it (with-trend colour), so the
// worked buy at 29,396.25 never armed.
//
// The bot's numbers (default knobs, STATED): entry = the signal high +
// PHLEntryBufferPts (default 1.00 → 29,396.75; the course draws +0.50 →
// 29,396.25, pinned below with the buffer at 0.50); stop = the signal LOW with
// no buffer (29,386.50; the course ray sits 0.50 under it at 29,386.00 — phl.go
// has no stop buffer); target = the old high 29,433.25 − PHLTargetShyPts 5 =
// 29,428.25 (the mentor drew 29,425.75, "gần đỉnh cũ").

// m464Base is Fri 11 Sep 2026 09:40 CT — the D2.2 replay day.
func m464Base() int64 {
	return time.Date(2026, time.September, 11, 9, 40, 0, 0, ctime()).UnixMilli()
}

// m464CourseTape is the D2.2 worked-buy tape, 1m, 09:40 → 10:15 CT. The
// anchored candles are the course's own OHLC: the 09:48 low 29,335, the 10:06
// old high 29,433.25 (line 29,433.25, WORKED EXAMPLE 5), the 10:09 red candle
// high 29,424.25 (quiz ray, R31), the 10:12 price 29,400.75 (EXAMPLE 5), the
// 10:13 red and 10:14 green candles (exact), and the 10:15 breakout to
// ~29,445 (R8). The candles between are interpolated to make the shape the
// lesson describes (up-leg, red pullback); they are not course prices. The
// up-leg is drawn WITHOUT the ~29,350 higher low Winnie reads [p3 @00:39–06:09]:
// two interpolated lows make the bot draw its own 1m trendline through them,
// and the broken line (29,413 at 10:14) becomes the first-obstacle target
// (TrendlineIsTarget) — a property of the interpolation, not of the lesson
// (D2.2-R75: an untested trendline is not evidence).
func m464CourseTape() []market.Kline {
	t0 := m464Base()
	raw := [][4]float64{
		{29362, 29364, 29355, 29357},             // 09:40
		{29357, 29359, 29350, 29352},             // 09:41
		{29352, 29355, 29346, 29348},             // 09:42
		{29348, 29351, 29342, 29345},             // 09:43
		{29345, 29349, 29340, 29343},             // 09:44
		{29343, 29346, 29338, 29341},             // 09:45
		{29341, 29345, 29337, 29344},             // 09:46
		{29344, 29346, 29339, 29341},             // 09:47
		{29341, 29344, 29335, 29342},             // 09:48 the low 29,335 [p1 @00:37–00:47]
		{29342, 29352, 29340, 29350},             // 09:49
		{29350, 29360, 29348, 29358},             // 09:50
		{29358, 29366, 29355, 29364},             // 09:51
		{29364, 29370, 29362, 29368},             // 09:52
		{29368, 29372, 29365, 29370},             // 09:53
		{29370, 29374, 29367, 29372},             // 09:54
		{29372, 29378, 29370, 29376},             // 09:55
		{29376, 29381, 29374, 29379},             // 09:56
		{29379, 29388, 29376, 29386},             // 09:57
		{29386, 29393, 29383, 29391},             // 09:58
		{29391, 29399, 29388, 29397},             // 09:59
		{29397, 29405, 29394, 29403},             // 10:00
		{29403, 29411, 29400, 29409},             // 10:01
		{29409, 29416, 29405, 29414},             // 10:02
		{29414, 29421, 29410, 29419},             // 10:03
		{29419, 29426, 29415, 29424},             // 10:04
		{29424, 29430, 29420, 29428},             // 10:05
		{29428, 29433.25, 29424, 29431},          // 10:06 the old high 29,433.25
		{29431, 29432, 29421, 29423},             // 10:07 red — ladder starts
		{29423, 29425.5, 29414, 29416},           // 10:08 red
		{29416, 29424.25, 29410, 29412},          // 10:09 red, high 29,424.25 (quiz yellow ray)
		{29412, 29413.75, 29402, 29404},          // 10:10 red
		{29404, 29412.5, 29398, 29400},           // 10:11 red
		{29402, 29404.5, 29396, 29400.75},        // 10:12 red, price 29,400.75
		{29401.25, 29403.25, 29375.25, 29390.50}, // 10:13 red — the 29-pt rung (ceiling)
		{29390.50, 29395.75, 29386.50, 29394.00}, // 10:14 GREEN signal candle (R9)
		{29394.00, 29445.00, 29392.50, 29439.00}, // 10:15 the breakout (R8)
	}
	out := make([]market.Kline, len(raw))
	for i, r := range raw {
		out[i] = market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000,
			Open: r[0], High: r[1], Low: r[2], Close: r[3]}
	}
	return out
}

// m464Idx returns the tape index of the HH:MM CT candle (09:40 = 0).
func m464Idx(hh, mm int) int { return (hh*60 + mm) - (9*60 + 40) }

// m464Mirror reflects a tape through price axis a (O' = a−O, H' = a−L,
// L' = a−H, C' = a−C): an uptrend pullback becomes a downtrend bounce, a red
// candle becomes green — the D2.2 sell mirror [R17/R18, p1 @11:05–12:13].
func m464Mirror(bars []market.Kline, a float64) []market.Kline {
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		out[i] = market.Kline{OpenTime: b.OpenTime, CloseTime: b.CloseTime,
			Open: a - b.Open, High: a - b.Low, Low: a - b.High, Close: a - b.Close}
	}
	return out
}

// m464Evaluator is the seeded evaluator at the PRODUCTION defaults for every
// rolling-ladder knob (PHLEntryBufferPts 1.00, PHLMinCandlesFromExtreme 3,
// PHLTargetShyPts 5, StopCeilingPts 25, RoomMultiple 2, the near-box and
// location bans ON), with only the scaffolding a 36-candle synthetic tape
// cannot supply honestly:
//   - the old extreme the leg targets (seeded, as the live seed does), the 4h
//     trend side, the day verdict and a drawn/escaped ORB;
//   - Exec2mAfter30m OFF — D2.2 is drawn on the 1m chart (the 2m schedule is
//     the later X5 routine);
//   - the EMA 9/34 location lines OFF — on 36 candles the EMA 34 is not the
//     real day's line, and its synthetic touch would OWN the entry (m216);
//   - the 5m trigger line HELD on the trend side at the leg's start (the 09:48
//     low 29,335 — a buy line under the up-leg; mirror: a sell line over the
//     down-leg). The lesson's chart has no 5m trigger (X8 is a later lesson);
//     left to the tape, the pullback breaks the 10:05 5m bucket low 29,410,
//     the line flips short, and that line becomes both a trigger-side refusal
//     (LocTriggerFilter, default ON — kept ON here) and the first obstacle the
//     long's target stops at. TestM464CourseLadderTapeTriggerIsNamed pins what
//     the production read does with the tape's own trigger.
func m464Evaluator(side Side, oldExtreme Level, at int64) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{oldExtreme}
	e.State.ORB = ORB{Day: dayStartCT(at), High: 29300, Low: 29290, Drawn: true, Escaped: side}
	if side == SideShort {
		e.State.ORB = ORB{Day: dayStartCT(at), High: 29510, Low: 29500, Drawn: true, Escaped: side}
	}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: side, Price: 29000}}
	if side == SideShort {
		e.State.HTF = HTF{FourH: TriggerLine{Dir: side, Price: 29800}}
	}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(at).In(ctime())), Verdict: DayTrade}
	// LastBucket = MaxInt64: TriggerTick skips every bucket, the line holds.
	e.State.Trigger = TriggerLine{Dir: side, Price: 29335, LastBucket: math.MaxInt64}
	if side == SideShort {
		e.State.Trigger = TriggerLine{Dir: side, Price: 58800 - 29335, LastBucket: math.MaxInt64}
	}
	return e
}

// m464Rolling returns the rolling entries in ins.
func m464Rolling(ins []Intent) []Intent {
	var out []Intent
	for _, in := range ins {
		if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL/PLH rolling pullback") {
			out = append(out, in)
		}
	}
	return out
}

type m464Step struct {
	at      string
	places  []Intent
	cancels []Intent
}

// m464Run ticks the tape from index from through to, one closed candle at a
// time at its close instant (the production clock), and records each tick's
// rolling placements and cancels.
func m464Run(e *Evaluator, bars []market.Kline, from, to int) []m464Step {
	var steps []m464Step
	for i := from; i <= to; i++ {
		ins := e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
		s := m464Step{at: time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04"), places: m464Rolling(ins)}
		for _, in := range ins {
			if in.Action == CancelArm && strings.HasPrefix(in.ArmID, "lvl-") {
				s.cancels = append(s.cancels, in)
			}
		}
		steps = append(steps, s)
	}
	return steps
}

func m464Approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestM464CourseWorkedBuyLadder — the D2.2 worked buy, through e.Tick (the
// production call site), at the production defaults. The ladder starts on the
// 10:07 red candle; the 10:13 rung is refused at the 25-pt ceiling (named,
// counted) and the resting 10:12 rung is cancelled because the ladder moved
// past it — but the ladder CONTINUES, and the 10:14 small GREEN candle (the
// mentor's signal bar) becomes the last rung: buy stop 29,396.75 (= 29,395.75
// + the 1.00 default buffer; the course draws 29,396.25 = +0.50), stop
// 29,386.50 (the signal low; course ray 29,386.00). The 10:15 candle fills it
// and the ladder ends.
// Mutant A: restore `withTrend && st.ArmID == "" → return nil` (colour gates
// every rung) → no 10:14 rung → RED.
// Mutant B: make a refused rung end the ladder (rollingCancel) → no 10:14 rung → RED.
func TestM464CourseWorkedBuyLadder(t *testing.T) {
	bars := m464CourseTape()
	e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))

	last := steps[len(steps)-1]
	if last.at != "10:14" || len(last.places) != 1 {
		t.Fatalf("the 10:14 green signal candle must be the last rung (exactly one buy stop), steps=%+v", steps)
	}
	in := last.places[0]
	if in.Side != SideLong || in.Setup != "PHL" || !m464Approx(in.Price, 29396.75) || !m464Approx(in.Stop, 29386.50) {
		t.Fatalf("the last rung = %s %s entry %.2f stop %.2f, want long PHL 29,396.75 / 29,386.50 [D2.2-R7/R9 p1 @02:26–05:43: 29,395.75 + 1.00 default buffer, stop = the signal low]", in.Side, in.Setup, in.Price, in.Stop)
	}
	if !m464Approx(in.Target, 29428.25) {
		t.Fatalf("target %.2f, want 29,428.25 (old high 29,433.25 − shy 5; the course drew 29,425.75 'gần đỉnh cũ')", in.Target)
	}
	if e.State.RollingPHL.ArmID != in.ArmID || !e.State.RollingPHL.Ladder {
		t.Fatalf("the 10:14 rung must rest as the ladder's order, state=%+v", e.State.RollingPHL)
	}

	// The 10:13 red rung: risk 29 > 25 — refused WITH its name, counted once.
	if got := e.State.Refusals["rolling_phl_stop_ceiling"]; got != 1 {
		t.Fatalf("the 10:13 rung must be refused and counted once as rolling_phl_stop_ceiling, ledger=%v", e.State.Refusals)
	}
	var s1013 m464Step
	for _, s := range steps {
		if s.at == "10:13" {
			s1013 = s
		}
	}
	if len(s1013.places) != 0 {
		t.Fatalf("the 10:13 rung is over the ceiling and must not arm: %+v", s1013.places)
	}
	// The 10:12 rung (entry 29,405.50, risk 9.5) armed; at 10:13 the mentor's
	// ladder moved to 29,403.50, so the stale 29,405.50 order is cancelled.
	if len(s1013.cancels) != 1 || !strings.Contains(s1013.cancels[0].Reason, "ladder continues") {
		t.Fatalf("the resting 10:12 rung must be cancelled (ladder continues) at the refused 10:13 rung, got %+v", s1013.cancels)
	}

	// 10:15 breaks 29,396.75 → filled, the ladder is over: no new rung, no cancel.
	ins := e.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	if p := m464Rolling(ins); len(p) != 0 {
		t.Fatalf("the 10:15 breakout fills the last rung; no new rung may arm: %+v", p)
	}
	for _, x := range ins {
		if x.Action == CancelArm && x.ArmID == in.ArmID {
			t.Fatalf("a FILLED rung is never cancelled: %+v", x)
		}
	}
	if e.State.RollingPHL.Ladder || e.State.RollingPHL.ArmID != "" {
		t.Fatalf("the filled ladder must be cleared, state=%+v", e.State.RollingPHL)
	}
	// Outcome from the bars [R8]: the 10:15 candle reaches the target.
	if bars[len(bars)-1].High < in.Target {
		t.Fatalf("course outcome: the 10:15 candle (H 29,445) reaches the target %.2f", in.Target)
	}
}

// TestM464CourseWorkedBuyLadderDrawnBuffer — the same tape with the entry
// buffer at the course's drawn +0.50 [D2.2-R10 p1 @04:14–05:43]: the last rung is EXACTLY the
// mentor's 29,396.25. The stop stays the signal low 29,386.50 (phl.go has no
// stop buffer; the course ray 29,386.00 is 0.50 under it).
func TestM464CourseWorkedBuyLadderDrawnBuffer(t *testing.T) {
	bars := m464CourseTape()
	e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	e.Cfg.PHLEntryBufferPts = 0.5
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))
	last := steps[len(steps)-1]
	if len(last.places) != 1 || !m464Approx(last.places[0].Price, 29396.25) || !m464Approx(last.places[0].Stop, 29386.50) {
		t.Fatalf("with the drawn 0.50 buffer the last rung must be the mentor's 29,396.25 / stop 29,386.50, got %+v", last.places)
	}
}

// TestM464CourseWorkedSellMirror — the PLH mirror of the worked buy [D2.2
// R17/R18 p1 @11:05–12:13: "Tôi rà một lệnh sell stop… Stop loss cũng như
// vậy, đặt ở đỉnh cây nến bị vượt qua"]: the buy tape reflected through
// 58,800. The 29-pt GREEN rung (mirror of 10:13) is refused at the ceiling,
// the ladder continues, and the small RED candle (mirror of the 10:14 signal
// bar — with-trend for a short) becomes the last rung: sell stop
// 58,800 − 29,395.75 − 1.00 = 29,403.25, stop its high 29,413.50.
// Mutant: as TestM464CourseWorkedBuyLadder (both are the same code path).
func TestM464CourseWorkedSellMirror(t *testing.T) {
	const a = 58800.0
	bars := m464Mirror(m464CourseTape(), a)
	e := m464Evaluator(SideShort, Level{Key: "old-low:29366.75", Kind: KindOldExtreme, Role: OldLow, Price: a - 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))
	last := steps[len(steps)-1]
	if len(last.places) != 1 {
		t.Fatalf("the mirrored 10:14 red candle must be the last sell rung, steps=%+v", steps)
	}
	in := last.places[0]
	if in.Side != SideShort || in.Setup != "PLH" || !m464Approx(in.Price, 29403.25) || !m464Approx(in.Stop, 29413.50) {
		t.Fatalf("mirror rung = %s %s %.2f / %.2f, want short PLH 29,403.25 / 29,413.50", in.Side, in.Setup, in.Price, in.Stop)
	}
	if got := e.State.Refusals["rolling_phl_stop_ceiling"]; got != 1 {
		t.Fatalf("the mirrored 29-pt rung must be counted once as rolling_phl_stop_ceiling, ledger=%v", e.State.Refusals)
	}
	ins := e.Tick(bars, BarCloseInstant(bars[len(bars)-1]))
	if len(m464Rolling(ins)) != 0 || e.State.RollingPHL.Ladder {
		t.Fatalf("the mirrored breakout fills the sell rung and ends the ladder, state=%+v", e.State.RollingPHL)
	}
}

// TestM464RefusedRungThenReactionEndsLadder — no chase: when a rung was
// REFUSED (nothing resting) and the next candle breaks it — the reaction the
// rung was for [D2.2-R8 p1 @03:27–04:16] — the ladder is over; the following with-trend candle
// does NOT arm (that would chase the impulse), and the reaction is counted.
// Mutant: drop the unarmed-reaction end → the green impulse candle arms → RED.
func TestM464RefusedRungThenReactionEndsLadder(t *testing.T) {
	bars := m464CourseTape()[:m464Idx(10, 14)] // through 10:13 (the refused rung)
	t0 := m464Base()
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		i := m464Idx(hh, mm)
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// 10:14 breaks the refused 10:13 rung (29,404.25) — a reaction with no order.
	bars = append(bars, mk(10, 14, 29390.5, 29406, 29388, 29404))
	// 10:15 a green candle with a LOWER high: with-trend, after the reaction.
	bars = append(bars, mk(10, 15, 29401, 29405, 29399, 29404.5))
	e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 15))
	for _, s := range steps {
		if (s.at == "10:14" || s.at == "10:15") && len(s.places) != 0 {
			t.Fatalf("%s: the ladder reacted on a refused rung — no rung may arm after it (no chase): %+v", s.at, s.places)
		}
	}
	if e.State.RollingPHL.Ladder {
		t.Fatalf("the ladder must end on the reaction, state=%+v", e.State.RollingPHL)
	}
	if e.State.Counters["rolling_ladder_reacted_no_order"] != 1 {
		t.Fatalf("the unarmed reaction must be counted once, counters=%v", e.State.Counters)
	}
}

// TestM464DroppedRungKeepsLadder — a rung whose entry a LATER filter drops
// (unregisterArm, m431's exit pass / the trader's DropArm) loses only that
// rung; the ladder keeps re-arming on the next closed candle.
// Mutant: restore the full `RollingPHL{}` reset in unregisterArm → the next
// green candle cannot arm → RED.
func TestM464DroppedRungKeepsLadder(t *testing.T) {
	e, bars, now := rollingFixture()
	if len(rollingPlaceEntries(e.Tick(bars, now))) != 1 {
		t.Fatal("setup: the first roll must arm")
	}
	e.DropArm(e.State.RollingPHL.ArmID)
	if e.State.RollingPHL.ArmID != "" || !e.State.RollingPHL.Ladder {
		t.Fatalf("a dropped rung clears the order but keeps the ladder, state=%+v", e.State.RollingPHL)
	}
	t0 := bars[0].OpenTime
	// a GREEN candle with a lower high (117 < the dropped rung 119): next rung.
	next := append(append([]market.Kline{}, bars...), rollingKline(t0, 6, 112.5, 116.5, 112, 116))
	places := rollingPlaceEntries(e.Tick(next, next[6].OpenTime+59_999))
	if len(places) != 1 || places[0].Price != 117.5 || places[0].Stop != 112 {
		t.Fatalf("the ladder must re-arm on the next closed candle at 117.5 / 112, got %+v", places)
	}
}

// TestM464CourseLadderTapeTriggerIsNamed — the same worked-buy tape with the
// 5m trigger line left to the tape (the production read, LocTriggerFilter
// default ON). The pullback breaks the 10:05 5m bucket low 29,410 and the line
// flips SHORT there; the 10:14 rung is then refused — and the refusal is NAMED
// and counted, the ladder keeps running (m464: never a silent end). This pins
// a course gap the fix does NOT close: with the X8 5m trigger the bot still
// sits the mentor's D2.2 buy out (reported, not changed — X8 "lúc nào mình
// cũng phải nhìn khung 5 phút" [X8 part1 @06:25] is its own rule).
func TestM464CourseLadderTapeTriggerIsNamed(t *testing.T) {
	bars := m464CourseTape()
	e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
	e.State.Trigger = TriggerLine{} // the tape's own 5m trigger
	steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))
	if e.State.Trigger.Dir != SideShort || e.State.Trigger.Price != 29410 {
		t.Fatalf("the tape's 5m trigger must flip short at 29,410, got %+v", e.State.Trigger)
	}
	if last := steps[len(steps)-1]; len(last.places) != 0 {
		t.Fatalf("with the short 5m trigger the 10:14 rung is refused, got %+v", last.places)
	}
	k := func(name string) string {
		return rollingLevelKey + "@" + itoa64(bars[m464Idx(10, 14)].CloseTime) + "|" + name
	}
	if !e.State.PHLRefusedVisit[k("rolling_room")] {
		t.Fatalf("the 10:14 rung must be refused and NAMED rolling_room (target = the 29,410 trigger line, reward 13.25 < 2 × 10.25), marks=%v", e.State.PHLRefusedVisit)
	}
	if !e.State.RollingPHL.Ladder {
		t.Fatalf("a refused rung never ends the ladder, state=%+v", e.State.RollingPHL)
	}
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// TestM464BreakingCandleNeverRestartsTheLadder — the candle that breaks the
// newest rung (the fill, or the reaction through a refused rung) is the ENTRY
// candle [D2.2-R8 p1 @03:27–04:16], never the start of a new ladder. On a 2m/5m
// exec frame the next 1m tick re-reads that same closed candle; found by the
// m434 tape diff (r22 10-12 22:31 exec2m: the reaction ended the ladder on the
// close tick and the re-read restarted it a minute later). Pinned by ticking the
// same closed candle twice.
// Mutant: drop the `execCur.CloseTime <= st.ReactedAt` start guard → the red
// breaking candle (a pullback colour) starts a new ladder on the re-read → RED.
func TestM464BreakingCandleNeverRestartsTheLadder(t *testing.T) {
	e, bars, now := rollingFixture()
	if len(rollingPlaceEntries(e.Tick(bars, now))) != 1 {
		t.Fatal("setup: the first roll must arm (entry 119)")
	}
	t0 := bars[0].OpenTime
	// bar 6: RED, high 120 ≥ 119 → fills the rung; red = a long's pullback colour.
	next := append(append([]market.Kline{}, bars...), rollingKline(t0, 6, 119.5, 120, 113, 114))
	at := next[6].OpenTime + 59_999
	if p := rollingPlaceEntries(e.Tick(next, at)); len(p) != 0 {
		t.Fatalf("the filling candle must not arm: %+v", p)
	}
	// the re-read of the same closed candle (the odd-minute 1m tick of a 2m frame).
	if p := rollingPlaceEntries(e.Tick(next, at+1)); len(p) != 0 {
		t.Fatalf("the breaking candle restarted the ladder on a re-read: %+v", p)
	}
	if e.State.RollingPHL.Ladder || e.State.RollingPHL.ArmID != "" {
		t.Fatalf("no ladder may run after the break, state=%+v", e.State.RollingPHL)
	}
	// the NEXT pullback candle starts a new ladder (its rung is then read by
	// every gate; on this tape the box ban drops it — the ladder still runs).
	next = append(next, rollingKline(t0, 7, 114, 116, 111, 112))
	_ = e.Tick(next, next[7].OpenTime+59_999)
	if st := e.State.RollingPHL; !st.Ladder || st.RungAt != next[7].CloseTime {
		t.Fatalf("a later red pullback candle starts a new ladder, state=%+v", st)
	}
}
