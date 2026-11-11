package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// G4 (oct round 2): OctD4.3-R5 [Oct D4.3 @ 03:10] "từ lúc Open ra... từ cái mảng
// đen nha chứ không phải tính cái mảng xanh" - the 5m trend is read from the Open
// (the RTH zone from 08:30 CT), never from the blue overnight zone. Real stored
// bars of Fri 30 Oct 2026 (the audit's [A bars]): the 08:25 Globex bucket L 28788.25,
// the 08:30 bucket L 28684.50, the 08:35 bucket L 28668.25.
//
// Today (knob trigger_from_open=false): the 08:30 bucket breaks the 08:25 low and
// draws a SELL line at 28788.25 (about 104 pts above the RTH read). October: no line
// at 08:30; the 08:35 bucket breaks the 08:30 low -> sell line 28684.50.
func g4Tape() []market.Kline {
	at := func(h, m int) int64 { return time.Date(2026, 10, 30, h, m, 0, 0, ctime()).UnixMilli() }
	var out []market.Kline
	add := func(h, m int, o, hi, l, c float64) {
		ot := at(h, m)
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: l, Close: c})
	}
	// 08:15-08:29: two flat 5m buckets (08:15, 08:20) and the 08:25 Globex bucket.
	for m := 15; m < 25; m++ {
		add(8, m, 28_800, 28_810, 28_780, 28_800)
	}
	for m := 25; m < 30; m++ {
		add(8, m, 28_800, 28_802, 28_788.25, 28_795)
	}
	// 08:30 bucket: lower low 28684.50, high below the 08:25 high.
	for m := 30; m < 35; m++ {
		l := 28_690.0
		if m == 31 {
			l = 28_684.5
		}
		add(8, m, 28_720, 28_740, l, 28_700)
	}
	// 08:35 bucket: breaks the 08:30 low (28668.25), high below it.
	for m := 35; m < 40; m++ {
		l := 28_680.0
		if m == 36 {
			l = 28_668.25
		}
		add(8, m, 28_700, 28_725, l, 28_690)
	}
	return out
}

func g4Eval(mut func(*Config)) *Evaluator {
	cfg := DefaultConfig()
	if mut != nil {
		mut(&cfg)
	}
	cfg.Enabled = true
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	return e
}

func g4Lines(e *Evaluator, bars []market.Kline) (at0830, at0835 TriggerLine) {
	for i := range bars {
		e.Tick(bars[:i+1], bars[i].CloseTime+1)
		if bars[i].OpenTime == time.Date(2026, 10, 30, 8, 34, 0, 0, ctime()).UnixMilli() {
			at0830 = e.State.Trigger
		}
	}
	return at0830, e.State.Trigger
}

func TestG4TriggerReadFromTheOpen(t *testing.T) {
	if !DefaultConfig().TriggerFromOpen {
		t.Fatal("G4: TriggerFromOpen must default ON (October: the 5m trend is read from the 08:30 open)")
	}
	bars := g4Tape()
	l30, l35 := g4Lines(g4Eval(nil), bars)
	if l30.Dir != "" {
		t.Fatalf("October: at the end of the 08:30 bucket there is no 5m line, got %+v", l30)
	}
	if l35.Dir != SideShort || l35.Price != 28_684.5 {
		t.Fatalf("October: the 08:35 bucket draws the sell line at the 08:30 low 28684.50, got %+v", l35)
	}
	// knob back = today: the overnight 08:25 bucket is the reference.
	l30, _ = g4Lines(g4Eval(func(c *Config) { c.TriggerFromOpen = false }), bars)
	if l30.Dir != SideShort || l30.Price != 28_788.25 {
		t.Fatalf("trigger_from_open=false (today): sell line 28788.25 from 08:30, got %+v", l30)
	}
}
