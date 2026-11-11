package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m473 PR B round 2 — B1 validity of Globex-evening boxes, trade-gate proof of
// the carried box, anchor-in-window guard. Rule: a box is valid "until the
// market opens tomorrow … 23-24 h" [Oct D3.3 @ 40:40-41:20] (OctD3.3-R51);
// 08:30 CT open [Oct D3.3 @ 41:28-41:36] (R52).

func m473R3At(y int, m time.Month, d, h, mi int) int64 {
	return time.Date(y, m, d, h, mi, 0, 0, ctime()).UnixMilli()
}

// An evening box expires at the FIRST open after its formation, not one later.
func TestM473R3EveningBoxExpiresAtNextOpen(t *testing.T) {
	cases := []struct {
		name         string
		formed, want int64
	}{
		{"Thu 18:00 -> Fri 08:30", m473R3At(2026, 10, 29, 18, 0), m473R3At(2026, 10, 30, 8, 30)},
		{"Thu 20:00 -> Fri 08:30", m473R3At(2026, 10, 29, 20, 0), m473R3At(2026, 10, 30, 8, 30)},
		{"Sun 18:00 -> Mon 08:30", m473R3At(2026, 11, 1, 18, 0), m473R3At(2026, 11, 2, 8, 30)},
		{"Fri 08:20 (pre-open) -> Mon 08:30", m473R3At(2026, 10, 30, 8, 20), m473R3At(2026, 11, 2, 8, 30)},
		{"Thu 03:00 (overnight) -> Fri 08:30", m473R3At(2026, 10, 29, 3, 0), m473R3At(2026, 10, 30, 8, 30)},
		{"Thu 10:00 (RTH) -> Fri 08:30", m473R3At(2026, 10, 29, 10, 0), m473R3At(2026, 10, 30, 8, 30)},
	}
	for _, c := range cases {
		if got := boxExpiryMs(Box{FormedAtMs: c.formed}); got != c.want {
			t.Errorf("%s: expiry %s, want %s", c.name, time.UnixMilli(got).In(ctime()), time.UnixMilli(c.want).In(ctime()))
		}
	}
}

// A Thu-evening box with no RTH replacement must not survive to Sunday evening.
func TestM473R3EveningBoxNotCarriedToSunday(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 29, 20, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	if !m473HasBoxEdges(e) {
		t.Fatalf("setup: the evening box did not form")
	}
	m473Tick(e, bars, time.Date(2026, 11, 1, 18, 0, 0, 0, ct))
	if m473HasBoxEdges(e) {
		t.Fatalf("Thu 20:00 box must not stand on Sun 18:00 (valid to Fri 08:30): %+v", e.State.ChartLevels)
	}
	if len(e.State.CarriedBoxes) != 0 {
		t.Fatalf("carried set must be empty: %+v", e.State.CarriedBoxes)
	}
}

// A carried box whose formation bar left the bar window is dropped, never read
// through the stale index.
func TestM473R3CarriedBoxAnchorOutOfWindowDropped(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 30, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	b := e.State.LatchedBoxes["ftgh"]
	e.State.CarriedBoxes = map[string]Box{"ftgh": b}
	now := m473R3At(2026, 11, 1, 18, 0)
	if got, _ := e.carriedBoxesAt(nil, bars, now); len(got) != 1 {
		t.Fatalf("anchor in window: want the carried box, got %d", len(got))
	}
	later := []market.Kline{m473EvenBar(time.Date(2026, 11, 1, 17, 59, 0, 0, ct))} // window slid past the anchor
	if got, _ := e.carriedBoxesAt(nil, later, now); len(got) != 0 {
		t.Fatalf("anchor scrolled out of the window: carried box must be dropped, got %+v", got)
	}
}

// m473Return appends an outside bar then a reject bar of the Thu 29 Oct FTGH
// [28800, 28818] (approach from below) ending one minute before `at`.
func m473Return(bars []market.Kline, at time.Time) []market.Kline {
	mk := func(off int, h, l, c float64) market.Kline {
		ot := at.Add(time.Duration(off) * time.Minute).UnixMilli()
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 28795, High: h, Low: l, Close: c}
	}
	out := append([]market.Kline(nil), bars...)
	out = append(out, mk(-3, 28796, 28790, 28792), mk(-2, 28819, 28791, 28794), mk(-1, 28796, 28790, 28793))
	return out
}

// The carried box is honoured by the PRODUCTION return gate (BoxRefs is consumed
// only for boxes in the walked set), not just drawn on the chart.
func TestM473R3CarriedBoxIsWalkedByTheGate(t *testing.T) {
	ct := ctime()
	run := func(firstDay time.Time, at time.Time, carry bool) *Evaluator {
		old := boxCarryRTH
		boxCarryRTH = carry
		defer func() { boxCarryRTH = old }()
		bars := m473Tape(firstDay)
		e := New(m473Cfg())
		e.Tick(bars, bars[len(bars)-1].CloseTime)
		e.Tick(m473Return(bars, at), at.UnixMilli())
		return e
	}
	walked := func(e *Evaluator) bool {
		for k, v := range e.State.BoxRefs {
			if v != 0 && len(k) > 4 && k[:4] == "ftgh" {
				return true
			}
		}
		return false
	}
	thu := time.Date(2026, 10, 29, 10, 0, 0, 0, ct)
	if e := run(thu, time.Date(2026, 10, 29, 18, 0, 0, 0, ct), true); !walked(e) {
		t.Fatalf("Thu 18:00: the carried box must be walked by the gate; BoxRefs=%v", e.State.BoxRefs)
	}
	if e := run(thu, time.Date(2026, 10, 29, 18, 0, 0, 0, ct), false); walked(e) {
		t.Fatalf("control (carry off): the box must not be walked at Thu 18:00; BoxRefs=%v", e.State.BoxRefs)
	}
	fri := time.Date(2026, 10, 30, 10, 0, 0, 0, ct)
	if e := run(fri, time.Date(2026, 11, 1, 18, 0, 0, 0, ct), true); !walked(e) {
		t.Fatalf("Sun 18:00: the carried Friday box must be walked; BoxRefs=%v", e.State.BoxRefs)
	}
	// at / after the next open the carried box no longer gates.
	if e := run(thu, time.Date(2026, 10, 30, 8, 31, 0, 0, ct), true); walked(e) {
		t.Fatalf("Fri 08:31: the expired box must not gate; BoxRefs=%v", e.State.BoxRefs)
	}
	if e := run(fri, time.Date(2026, 11, 2, 8, 31, 0, 0, ct), true); walked(e) {
		t.Fatalf("Mon 08:31: the expired box must not gate; BoxRefs=%v", e.State.BoxRefs)
	}
}

// Holiday through Tick: Wed 25 Nov box stands Thu 26 Nov 18:00, gone Fri 27 Nov 08:30.
func TestM473R3HolidayBoxThroughTick(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 11, 25, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	if !m473HasBoxEdges(e) {
		t.Fatalf("setup: Wed 25 Nov box did not form")
	}
	m473Tick(e, bars, time.Date(2026, 11, 26, 18, 0, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("Thanksgiving evening: the Wed box must still stand")
	}
	m473Tick(e, bars, time.Date(2026, 11, 27, 8, 30, 0, 0, ct))
	if m473HasBoxEdges(e) {
		t.Fatalf("Fri 27 Nov 08:30: the box must be gone")
	}
}
