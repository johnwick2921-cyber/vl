package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m473 B1 — the FTGH/FTGL box lives from the RTH open (08:30 CT) to the NEXT
// RTH open (about 23-24 h), NOT to the 17:00 CT day roll. "Nó sẽ valid cho tới
// khi market open ngày mai … Có nghĩa là nó sẽ valid 24 tiếng … Market Open,
// 8 giờ rưỡi sáng … 23 tiếng 24 tiếng gì đó cũng được" [Oct D3.3 @ 40:40-41:20]
// (OctD3.3-R51); the FTGL box "vẫn sẽ valid … chờ tới sáng ngày mai"
// [Oct D3.3 @ 43:51] (R55); a Friday box still works on the live Sunday chart
// [Oct D3.3 @ 17:00] (R27). Supersedes September "mình sẽ dùng box này hoài"
// [D3.2 p2 @ 06:21]. Frame: /home/hoang/mm-course/oct/scenes/Ngày 3.3 FTGH
// FTGL/s_02464.jpg (Fri 30 Oct 1m, the FTGH box from the ~08:00 peaks
// 28,812-28,818).

func m473Cfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	return cfg
}

// m473Tape: a confirmed FTGH pair (peak 1 high 28,818 @2, peak 2 high 28,814
// @5, [Oct D3.3 @ 17:00] prices) starting at the given CT wall time.
func m473Tape(t0 time.Time) []market.Kline {
	base := 28800.0
	hl := [][2]float64{{base + 1, base - 1}, {base - 0.5, base - 1}, {base + 18, base - 1}, {base + 1, base}, {base, base - 1}, {base + 14, base - 1}, {base + 1, base - 1}}
	var out []market.Kline
	for i, p := range hl {
		ot := t0.UnixMilli() + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: base, High: p[0], Low: p[1], Close: base})
	}
	return out
}

func m473EvenBar(at time.Time) market.Kline {
	ot := at.UnixMilli()
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 28800, High: 28801, Low: 28799, Close: 28800}
}

func m473HasBoxEdges(e *Evaluator) bool {
	top, bot := false, false
	for _, l := range e.State.ChartLevels {
		if strings.HasPrefix(l.Key, "ftgh:") && strings.HasSuffix(l.Key, ":top") {
			top = true
		}
		if strings.HasPrefix(l.Key, "ftgh:") && strings.HasSuffix(l.Key, ":bottom") {
			bot = true
		}
	}
	return top && bot
}

func m473Tick(e *Evaluator, bars []market.Kline, at time.Time) {
	e.Tick(append(append([]market.Kline(nil), bars...), m473EvenBar(at.Add(-time.Minute))), at.UnixMilli())
}

// TestM473B1BoxSurvivesTheDayRoll — Thu 29 Oct 2026 RTH box is still a live
// location at 18:00 the SAME evening (past the 17:00 roll), and gone at the
// next RTH open (Fri 30 Oct 08:30 CT).
func TestM473B1BoxSurvivesTheDayRoll(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 29, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	if !m473HasBoxEdges(e) {
		t.Fatalf("setup: the RTH box did not form: %+v", e.State.ChartLevels)
	}
	// Thu 18:00 CT, same evening [Oct D3.3 @ 40:40-41:20].
	m473Tick(e, bars, time.Date(2026, 10, 29, 18, 0, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("the RTH box died at the 17:00 roll; want valid to the next 08:30 [Oct D3.3 @ 40:40-41:20]: %+v", e.State.ChartLevels)
	}
	// Fri 08:29 still valid; Fri 08:30 expired.
	m473Tick(e, bars, time.Date(2026, 10, 30, 8, 29, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("the box should still stand at 08:29 before the next open")
	}
	m473Tick(e, bars, time.Date(2026, 10, 30, 8, 30, 0, 0, ct))
	if m473HasBoxEdges(e) {
		t.Fatalf("the box must be gone at the next RTH open 08:30 [Oct D3.3 @ 41:20]: %+v", e.State.ChartLevels)
	}
}

// TestM473B1FridayBoxValidSundayEvening — Fri 30 Oct box still valid on Sun 1
// Nov 18:00 (the live Sunday chart [Oct D3.3 @ 17:00]) and expired Mon 2 Nov
// 08:30.
func TestM473B1FridayBoxValidSundayEvening(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 30, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	if !m473HasBoxEdges(e) {
		t.Fatalf("setup: the Friday box did not form")
	}
	m473Tick(e, bars, time.Date(2026, 11, 1, 18, 0, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("Friday box must be valid Sunday 18:00 [Oct D3.3 @ 17:00, 40:40]: %+v", e.State.ChartLevels)
	}
	m473Tick(e, bars, time.Date(2026, 11, 2, 8, 30, 0, 0, ct))
	if m473HasBoxEdges(e) {
		t.Fatalf("Friday box must expire at Monday 08:30")
	}
}
