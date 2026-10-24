package mentor

import (
	"math"
	"math/rand"
	"testing"

	"vl/market"
)

// M401: the EMA 9/34 levels and the EMA34 location line follow the execution-
// frame schedule (1m 08:30 <= t < 09:00 CT, 2m otherwise) when Exec2mAfter30m
// is ON. Course: "Em lúc nào cũng prefer trade khung 2 phút … Trong 30 phút đầu
// giờ em vẫn sẽ trade khung 1 phút … sau 30 phút em sẽ chuyển qua khung 2
// phút" [extra Routine trước khi trade part1 @00:00:34–00:01:14]; the entry is
// read against the EMA34 on the 2m candles [extra Cách né trap & Setup gồng lời
// part1 @00:02:05–00:02:48]; "anh TRADE KHUNG NÀO thì anh BẬT KHUNG ĐÓ" [D5.4
// @08:15]. The pins drive Seed + Tick (the live path) and Tick on a cold
// evaluator (Levels), never a copy of the schedule.

// m401Tape is a contiguous random 1m tape 2026-10-21 03:00 → 10:30 CT.
func m401Tape() []market.Kline {
	rng := rand.New(rand.NewSource(401))
	start := auditMs(2026, 10, 21, 3, 0, 0)
	n := 7*60 + 30
	out := make([]market.Kline, n)
	px := 29900.0
	for i := range out {
		c := px + (rng.Float64()-0.5)*10
		out[i] = market.Kline{
			OpenTime: start + int64(i)*60_000, CloseTime: start + int64(i)*60_000 + 59_999,
			Open: px, High: math.Max(px, c) + rng.Float64()*3, Low: math.Min(px, c) - rng.Float64()*3, Close: c,
		}
		px = c
	}
	return out
}

// m401Indep computes the EMA independently of the production aggregation:
// plain pairing of 1m closes on even-minute boundaries, seeded with the first
// close, over everything closed up to and including bars[:upto].
func m401Indep(bars []market.Kline, upto, tf, period int) float64 {
	k := 2.0 / float64(period+1)
	var ema float64
	have := false
	if tf == 1 {
		for _, b := range bars[:upto] {
			if !have {
				ema, have = b.Close, true
			} else {
				ema += k * (b.Close - ema)
			}
		}
		return ema
	}
	for i := 0; i+1 < upto; i++ {
		if (bars[i].OpenTime/60_000)%2 != 0 {
			continue
		}
		c := bars[i+1].Close // the 2m bucket's last 1m close
		if !have {
			ema, have = c, true
		} else {
			ema += k * (c - ema)
		}
	}
	return ema
}

func m401Cfg(knob bool) Config {
	c := DefaultConfig()
	c.Enabled = true
	c.Exec2mAfter30m = knob
	return c
}

func m401Level(t *testing.T, e *Evaluator, k LevelKind) float64 {
	t.Helper()
	for _, l := range e.State.ChartLevels {
		if l.Kind == k {
			return l.Price
		}
	}
	t.Fatalf("level %s not in ChartLevels at the tick", k)
	return 0
}

// m401Run seeds on the tape up to the 08:00 CT close and ticks bar by bar until
// the bar OPENING at (hh:mm) has closed; returns the evaluator and the count of
// bars consumed.
func m401Run(t *testing.T, cfg Config, seeded bool, hh, mm int) (*Evaluator, []market.Kline, int) {
	t.Helper()
	tape := m401Tape()
	want := auditMs(2026, 10, 21, hh, mm, 0)
	upto := 0
	for i, b := range tape {
		if b.OpenTime == want {
			upto = i + 1
		}
	}
	if upto == 0 {
		t.Fatalf("no bar at %02d:%02d", hh, mm)
	}
	e := New(cfg)
	first := 0
	if seeded {
		first = 5 * 60 // seed through 08:00 CT
		Seed(e, tape[:first], tape[first-1].CloseTime+1)
	} else {
		first = upto
	}
	for n := first; n <= upto; n++ {
		if n < 2 {
			continue
		}
		e.Tick(tape[:n], BarCloseInstant(tape[n-1]))
	}
	return e, tape, upto
}

func TestM401EMALevelsFollowExecFrame(t *testing.T) {
	for _, seeded := range []bool{true, false} {
		mode := map[bool]string{true: "seeded", false: "cold"}[seeded]
		// (a) 08:45 CT bar closed -> the 1m EMA.
		e, tape, upto := m401Run(t, m401Cfg(true), seeded, 8, 45)
		for _, c := range []struct {
			k      LevelKind
			period int
		}{{KindEMA34, 34}, {KindEMA9, 9}, {KindEMA34HTF, 34}} {
			if got, want := m401Level(t, e, c.k), m401Indep(tape, upto, 1, c.period); math.Abs(got-want) > 1e-6 {
				t.Errorf("%s 08:45 %s = %.6f, want 1m EMA %.6f", mode, c.k, got, want)
			}
		}
		// (b) 10:00 CT: the 2m EMA computed independently; and NOT the 1m one.
		e, tape, upto = m401Run(t, m401Cfg(true), seeded, 10, 0)
		for _, c := range []struct {
			k      LevelKind
			period int
		}{{KindEMA34, 34}, {KindEMA9, 9}, {KindEMA34HTF, 34}} {
			got := m401Level(t, e, c.k)
			want, one := m401Indep(tape, upto, 2, c.period), m401Indep(tape, upto, 1, c.period)
			if math.Abs(got-want) > 1e-6 {
				t.Errorf("%s 10:00 %s = %.6f, want 2m EMA %.6f (1m is %.6f)", mode, c.k, got, want, one)
			}
			if c.period == 34 && math.Abs(want-one) < 1e-3 {
				t.Fatalf("fixture degenerate: 1m and 2m EMA coincide")
			}
		}
		// (c) knob OFF -> the 1m EMA all day, 10:00 included.
		e, tape, upto = m401Run(t, m401Cfg(false), seeded, 10, 0)
		for _, c := range []struct {
			k      LevelKind
			period int
		}{{KindEMA34, 34}, {KindEMA9, 9}, {KindEMA34HTF, 34}} {
			if got, want := m401Level(t, e, c.k), m401Indep(tape, upto, 1, c.period); math.Abs(got-want) > 1e-6 {
				t.Errorf("%s knob OFF 10:00 %s = %.6f, want 1m EMA %.6f", mode, c.k, got, want)
			}
		}
	}
}

// The curve: tf_min 1 up to the 08:59 bar, then one CLOSED 2m bar per point at
// its open ms with the 2m EMA, sorted by ms; knob OFF is 1m only.
func TestM401ChartSeriesCarriesFrame(t *testing.T) {
	tape := m401Tape()
	// 10:00 bar closed -> last closed 2m bucket opens 09:58.
	cut := 0
	for i, b := range tape {
		if b.OpenTime == auditMs(2026, 10, 21, 9, 59, 0) {
			cut = i + 1
		}
	}
	bars := tape[:cut]
	cfg := m401Cfg(true)
	cfg.EMAPeriod9, cfg.EMAPeriod34 = 9, 34
	s := emaSeriesChartSched(bars, cfg)
	if len(s) == 0 {
		t.Fatal("empty series")
	}
	sawOne, sawTwo := false, false
	nine := auditMs(2026, 10, 21, 9, 0, 0)
	var prev int64
	for _, p := range s {
		if p.Ms <= prev {
			t.Fatalf("series not strictly sorted by ms at %d", p.Ms)
		}
		prev = p.Ms
		switch {
		case p.Ms < auditMs(2026, 10, 21, 8, 30, 0) || p.Ms >= nine:
			if p.TFMin != 2 || (p.Ms/60_000)%2 != 0 {
				t.Fatalf("2m period point %+v must be tf 2 on an even-minute bar open", p)
			}
			sawTwo = true
		default:
			if p.TFMin != 1 {
				t.Fatalf("1m period point %+v must be tf 1", p)
			}
			sawOne = true
		}
	}
	if !sawOne || !sawTwo {
		t.Fatalf("expected both frames, one=%v two=%v", sawOne, sawTwo)
	}
	last := s[len(s)-1]
	if last.Ms != auditMs(2026, 10, 21, 9, 58, 0) {
		t.Fatalf("last point ms = %d, want the 09:58 closed 2m bar", last.Ms)
	}
	if want := m401Indep(bars, len(bars), 2, 34); math.Abs(last.Ema34-want) > 0.01 {
		t.Fatalf("2m tail ema34 = %.4f, want %.4f", last.Ema34, want)
	}
	for _, p := range emaSeriesChartSched(bars, m401Cfg(false)) {
		if p.TFMin != 1 {
			t.Fatalf("knob OFF point %+v must be tf 1", p)
		}
	}
}
