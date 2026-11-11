package mentor

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// PHL-1 (phl audit, 2026-11-10) — the PHL/PLH old high / old low is the 1m
// swing the pullback started from, not a confirmed 5m/15m fractal pivot.
//
// The fixtures are the REAL MNQ 12-26 1m bars of the course's own worked
// examples, exported read-only from data.db (testdata/phl1_mnq_1m_*.csv; the
// candles quoted below are those rows):
//
//	WE6  Tue 10-27 [Oct D2.3 @ 13:37–28:33]: old high 09:36 H 29,673.50 (red
//	     line 29,672.75→29,673.25 "Tới Target ... gần đỉnh cũ" @ 26:23); buy
//	     stop dragged 29,650.75 → 29,646.25 → 29,644.75 on the 09:42/43/44 red
//	     candles; the 09:45 green candle fills it (@ 21:08–21:31).
//	WE7  Fri 10-30 [@ 30:17–41:51]: old low 13:21 L 28,288.75 (white dashed
//	     28,288.00, target 28,298.25 @ 41:23); sell stop 28,316.25 on the 13:25
//	     candle (L 28,317.25 − 1); the 13:26 red candle fills it (@ 35:46–39:09).
//	WE10 Thu 10-29 [@ 1:07:00–1:08:52]: "Rớt, bật nến xanh lên 3 cây rồi nè";
//	     sell stop 28,973 → 28,980 (19:52 L 28,981 − 1); the 19:53 red candle
//	     fills at 28,968.25; "Gần đáy cũ" = the 19:48 L 28,958.75.
//	WE9  Thu 10-29 [@ 51:29–57:47]: target green 28,830.50 near the old low
//	     20:53 L 28,825.00 — not the lower 20:47 L 28,820.25.
//
// Each example is ticked through the PRODUCTION Tick (rollingPHLTick inside
// it) with the 4h side and the 5m trigger held on the trade side, as in the
// m464 course tests (m464Evaluator). The assertion is the course's: the rung
// the mentor's order sat on, and a target near the 1m old extreme.

func phl1Tape(t *testing.T, name string) []market.Kline {
	t.Helper()
	return m413Load(t, "testdata/"+name)
}

func phl1At(t *testing.T, bars []market.Kline, y int, mo time.Month, d, hh, mm int) int {
	t.Helper()
	at := time.Date(y, mo, d, hh, mm, 0, 0, ctime()).UnixMilli()
	for i, b := range bars {
		if b.OpenTime == at {
			return i
		}
	}
	t.Fatalf("no bar at %d-%02d %02d:%02d CT", mo, d, hh, mm)
	return -1
}

// phl1Evaluator is m464Evaluator on the example's own prices: the 4h side and
// the 5m trigger held on the trade side (the trigger line at the trade's far
// side so TriggerVerdict passes every rung), no seed levels (the level set is
// what the production Levels() reads from the tape).
func phl1Evaluator(cfg Config, side Side, at int64, px float64) *Evaluator {
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.ORB = ORB{Day: dayStartCT(at), High: px - 400, Low: px - 410, Drawn: true, Escaped: side}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: side, Price: px - 500}}
	e.State.Trigger = TriggerLine{Dir: side, Price: px - 400, LastBucket: math.MaxInt64}
	if side == SideShort {
		e.State.ORB = ORB{Day: dayStartCT(at), High: px + 410, Low: px + 400, Drawn: true, Escaped: side}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: side, Price: px + 500}}
		e.State.Trigger = TriggerLine{Dir: side, Price: px + 400, LastBucket: math.MaxInt64}
	}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(at).In(ctime())), Verdict: DayTrade}
	return e
}

type phl1Rung struct {
	at     string
	places []Intent
	refuse []string
}

// phl1Run ticks bars[from..to] at each candle's close instant and records the
// rolling placements and the rolling refusal names each tick added.
func phl1Run(e *Evaluator, bars []market.Kline, from, to int) []phl1Rung {
	var out []phl1Rung
	for i := from; i <= to; i++ {
		before := map[string]int{}
		for k, v := range e.State.Refusals {
			before[k] = v
		}
		ins := e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
		r := phl1Rung{at: time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04"), places: m464Rolling(ins)}
		for k, v := range e.State.Refusals {
			if strings.HasPrefix(k, "rolling_") && v > before[k] {
				r.refuse = append(r.refuse, k)
			}
		}
		out = append(out, r)
	}
	return out
}

// phl1RunRolling calls the PRODUCTION rollingPHLTick once per closed candle,
// with the old-extreme set Tick itself computes (oldExtremeIndexes over
// Levels — the pre-PHL-1 source, read only when PHLOldExtreme1mK <= 0) and no
// drawn levels or boxes: the course's PHL/PLH lesson (Oct D2.3) on its own,
// before the later lessons' location gates (the m464 tests set the 15m box
// ban aside the same way).
func phl1RunRolling(e *Evaluator, bars []market.Kline, from, to int) []phl1Rung {
	var out []phl1Rung
	for i := from; i <= to; i++ {
		before := map[string]int{}
		for k, v := range e.State.Refusals {
			before[k] = v
		}
		w := bars[:i+1]
		now := BarCloseInstant(bars[i])
		olds := oldExtremeIndexes(Levels(w, e.Cfg, now), w)
		ins := e.rollingPHLTick(w, bars[i], bars[i], 1, olds, nil, nil, DayGate{})
		r := phl1Rung{at: time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04"), places: m464Rolling(ins)}
		for k, v := range e.State.Refusals {
			if strings.HasPrefix(k, "rolling_") && v > before[k] {
				r.refuse = append(r.refuse, k)
			}
		}
		out = append(out, r)
	}
	return out
}

func phl1Dump(steps []phl1Rung) string {
	var b strings.Builder
	for _, s := range steps {
		fmt.Fprintf(&b, "\n  %s", s.at)
		for _, p := range s.places {
			fmt.Fprintf(&b, " ARM %s %.2f stop %.2f target %.2f", p.Side, p.Price, p.Stop, p.Target)
		}
		if len(s.refuse) > 0 {
			fmt.Fprintf(&b, " refused %v", s.refuse)
		}
	}
	return b.String()
}

func phl1Find(steps []phl1Rung, at string) phl1Rung {
	for _, s := range steps {
		if s.at == at {
			return s
		}
	}
	return phl1Rung{}
}

// TestPHL1CourseWorkedExamples — the four worked PHL/PLH examples of Oct D2.3,
// at the production defaults: each one's rung arms on the mentor's candle with
// its target near the 1m old extreme the mentor drew, and the next candle (the
// mentor's fill) trades through it.
//
// Before PHL-1 the old extreme came from kernel.SwingPointLevelsAll (5m + 15m
// pivots, k = 2, the 0.25×ATR significance filter): WE7 and WE10 had NO old
// low on the target side (the ladder was cancelled silently, no intent, no
// refusal — live 10-30 13:21–13:27 and 10-29 19:48–19:53), and WE6 / WE9 read
// a far pivot instead of the mentor's.
// Mutant: read the old extreme from oldExtremes (the pre-PHL-1 source) → RED.
func TestPHL1CourseWorkedExamples(t *testing.T) {
	type want struct {
		name             string
		file             string
		y                int
		mo               time.Month
		d                int
		side             Side
		fromH, fromM     int
		rungH, rungM     int
		entry, stop, tgt float64
		oldExtreme       float64
	}
	cases := []want{
		// WE6: 09:44 R O 29,638.75 H 29,644.00 L 29,633.25 C 29,634.25 → buy stop
		// 29,645.00 (+1.00, OctD2.3-R46; the mentor's line read 29,644.75), stop
		// 29,632.25 (−1.00 — the mentor's red stop line 29,632.25 @ 25:19), target
		// 29,673.50 − 5 = 29,668.50 (the mentor's tool 29,665–29,668.75 @ 27:34).
		{"WE6 10-27 PHL", "phl1_mnq_1m_20261027_0730ct.csv", 2026, time.October, 27, SideLong, 9, 30, 9, 44, 29645.00, 29632.25, 29668.50, 29673.50},
		// WE7: 13:25 R O 28,328.75 H 28,330.75 L 28,317.25 C 28,321.25 → sell stop
		// 28,316.25 (the mentor's entry), stop 28,331.75, target 28,288.75 + 5.
		{"WE7 10-30 PLH", "phl1_mnq_1m_20261030_1120ct.csv", 2026, time.October, 30, SideShort, 13, 15, 13, 25, 28316.25, 28331.75, 28293.75, 28288.75},
		// WE10: 19:52 R O 28,983.00 H 28,988.00 L 28,981.00 C 28,981.75 → sell stop
		// 28,980.00 (the mentor's line), stop 28,989.00, target 28,958.75 + 5.
		{"WE10 10-29 PLH", "phl1_mnq_1m_20261029_1800ct.csv", 2026, time.October, 29, SideShort, 19, 40, 19, 52, 28980.00, 28989.00, 28963.75, 28958.75},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bars := phl1Tape(t, c.file)
			from := phl1At(t, bars, c.y, c.mo, c.d, c.fromH, c.fromM)
			rung := phl1At(t, bars, c.y, c.mo, c.d, c.rungH, c.rungM)
			at := fmt.Sprintf("%02d:%02d", c.rungH, c.rungM)

			// 1. PRODUCTION Tick at the production defaults: the rung candle
			// reads the 1m old extreme — whatever refuses it, it is never the
			// missing old extreme (WE7/WE10 used to have none at all).
			ep := phl1Evaluator(DefaultConfig(), c.side, bars[from].OpenTime, bars[rung].Close)
			prod := phl1Find(phl1Run(ep, bars, from, rung), at)
			for _, r := range prod.refuse {
				if r == "rolling_phl_no_old_extreme_on_side" || r == "rolling_phl_old_extreme_unconfirmed" {
					t.Fatalf("%s: production Tick refused the mentor's %s rung for its old extreme (%s)", c.name, at, r)
				}
			}

			// 2. The PHL/PLH lesson on its own (rollingPHLTick, the production
			// function; no drawn levels/boxes). cfg.RoomMultiple 1 sets aside the
			// D5.3 room rule (day 5): at its default 2 it refuses all three of
			// the mentor's Oct D2.3 trades (RR 1.7–1.8 on his own tool, @ 27:34,
			// 1:07:47) — a lesson-order question for the mentor, not PHL-1.
			cfg := DefaultConfig()
			cfg.RoomMultiple = 1
			cfg.PHLRoomMultiple = cfg.RoomMultiple // review: the PHL/PLH room follows the fixture's room setting (the decoupled knob defaults to 2)
			e := phl1Evaluator(cfg, c.side, bars[from].OpenTime, bars[rung].Close)
			steps := phl1RunRolling(e, bars, from, rung)
			got := phl1Find(steps, at)
			if len(got.places) != 1 {
				t.Fatalf("%s: the mentor's rung on the %02d:%02d candle must arm exactly one %s stop; ladder:%s\nrefusals=%v", c.name, c.rungH, c.rungM, c.side, phl1Dump(steps), e.State.Refusals)
			}
			in := got.places[0]
			if in.Side != c.side || !m464Approx(in.Price, c.entry) || !m464Approx(in.Stop, c.stop) || !m464Approx(in.Target, c.tgt) {
				t.Fatalf("%s: rung = %s %.2f stop %.2f target %.2f, want %s %.2f / %.2f → %.2f (old extreme %.2f − shy 5)", c.name, in.Side, in.Price, in.Stop, in.Target, c.side, c.entry, c.stop, c.tgt, c.oldExtreme)
			}
			if st := e.State.RollingPHL; !m464Approx(st.TargetPx, c.oldExtreme) {
				t.Fatalf("%s: the ladder's old extreme = %.2f, want the 1m swing %.2f", c.name, st.TargetPx, c.oldExtreme)
			}
			next := bars[rung+1]
			if c.side == SideLong && next.High < in.Price || c.side == SideShort && next.Low > in.Price {
				t.Fatalf("%s: the next candle (the mentor's fill) must trade through %.2f: %+v", c.name, in.Price, next)
			}
		})
	}
}

// TestPHL1WE9TargetIsTheNearestOldLow — WE9 (Thu 10-29 20:53): the mentor's
// target 28,830.50 sits near the 20:53 low 28,825.00, the NEAREST unbroken old
// low — not the lower 20:47 L 28,820.25 [OctD2.3-R55 @ 57:00]. Read through
// the production helper on the reference candle the mentor traded (21:01 G,
// the red line 28,861.50 sits under the 21:01 low 28,863.25 @ 1:04:39).
func TestPHL1WE9TargetIsTheNearestOldLow(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261029_1800ct.csv")
	r := phl1At(t, bars, 2026, time.October, 29, 21, 1)
	ex, found, pending := phlTapeOldExtreme(bars, r, bars[r].Low, SideShort, DefaultConfig().PHLOldExtreme1mK)
	if !found || pending || ex.level.Price != 28825.00 || ex.level.Role != OldLow {
		t.Fatalf("WE9 old low = %+v found=%v pending=%v, want the 20:53 L 28,825.00", ex.level, found, pending)
	}
	if got := time.UnixMilli(bars[ex.idx].OpenTime).In(ctime()).Format("15:04"); got != "20:53" {
		t.Fatalf("WE9 old low at %s, want the 20:53 candle", got)
	}
}

// TestPHL1KIsTheOnlyWidthThatReproducesTheExamples — the swing width is not
// stated by the course; the default 4 is pinned by the examples themselves:
// k = 2 or 3 reads the 10-27 09:40 lower high 29,662.00 as WE6's old high
// (target 29,657 vs a 12.75-pt risk — the 1:1 floor refuses the mentor's
// trade), and k = 5 has not confirmed the WE7 13:21 low by the 13:25 rung.
func TestPHL1KIsTheOnlyWidthThatReproducesTheExamples(t *testing.T) {
	we6 := phl1Tape(t, "phl1_mnq_1m_20261027_0730ct.csv")
	r6 := phl1At(t, we6, 2026, time.October, 27, 9, 44)
	we7 := phl1Tape(t, "phl1_mnq_1m_20261030_1120ct.csv")
	r7 := phl1At(t, we7, 2026, time.October, 30, 13, 25)
	for _, k := range []int{2, 3, 4, 5} {
		ex6, ok6, _ := phlTapeOldExtreme(we6, r6, we6[r6].High, SideLong, k)
		ex7, ok7, _ := phlTapeOldExtreme(we7, r7, we7[r7].Low, SideShort, k)
		good := ok6 && ex6.level.Price == 29673.50 && ok7 && ex7.level.Price == 28288.75
		if good != (k == 4) {
			t.Fatalf("k=%d: WE6 old high %.2f (%v), WE7 old low %.2f (%v) — only k=4 may reproduce both (29,673.50 / 28,288.75)", k, ex6.level.Price, ok6, ex7.level.Price, ok7)
		}
	}
	if DefaultConfig().PHLOldExtreme1mK != 4 {
		t.Fatalf("PHLOldExtreme1mK default = %d, want 4", DefaultConfig().PHLOldExtreme1mK)
	}
}

// TestPHL1NoStaleFarTarget — the finding's two stale-target cases, real bars:
//
//	10-23 03:15 PHL (live: entry 29,252.50, target 29,338 from the 10-22 12:00
//	old high 29,379.50, 895 bars back). The course old high is the 03:13 H
//	29,261.50 two candles earlier: unconfirmed at 03:15 → no target, no rung.
//	11-09 21:44 PLH (live: 4 rungs from 27,867.75 with target 27,825 from the
//	11:15 old low 27,820). The 21:44 low itself is the old low: no rung may
//	arm on the first bounce candles with a far target [OctD2.3-R53 @ 54:00].
func TestPHL1NoStaleFarTarget(t *testing.T) {
	for _, c := range []struct {
		name         string
		file         string
		mo           time.Month
		d            int
		side         Side
		fromH, fromM int
		toH, toM     int
		farTarget    float64
	}{
		{"10-23 03:15 PHL", "phl1_mnq_1m_20261022_0320ct.csv", time.October, 23, SideLong, 3, 10, 3, 16, 29338},
		{"11-09 21:44 PLH", "phl1_mnq_1m_20261108_2150ct.csv", time.November, 9, SideShort, 21, 40, 21, 48, 27825},
	} {
		t.Run(c.name, func(t *testing.T) {
			bars := phl1Tape(t, c.file)
			from := phl1At(t, bars, 2026, c.mo, c.d, c.fromH, c.fromM)
			to := phl1At(t, bars, 2026, c.mo, c.d, c.toH, c.toM)
			e := phl1Evaluator(DefaultConfig(), c.side, bars[from].OpenTime, bars[to].Close)
			steps := phl1RunRolling(e, bars, from, to)
			for _, s := range steps {
				for _, p := range s.places {
					if math.Abs(p.Target-c.farTarget) < 1 || math.Abs(p.Target-p.Price) > 40 {
						t.Fatalf("%s: rung at %s armed with a stale far target %.2f (entry %.2f); ladder:%s", c.name, s.at, p.Target, p.Price, phl1Dump(steps))
					}
				}
			}
		})
	}
}
