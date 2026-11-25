package mentor

import (
	"testing"
	"time"
	"vl/market"
)

// m473 round-2 review fixes for B1 (restart + pre-open box + display purity).
// All numbers are the [Oct D3.3 @ 17:00] prices of m473Tape; validity window
// [Oct D3.3 @ 40:40-41:20, 41:28-41:36] (OctD3.3-R27, R51, R52).

// A restart at Sunday 18:00 must still hold Friday's box (R27 frame s_02499).
func TestM473B1SeedRebuildsFridayBoxSundayRestart(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 30, 10, 0, 0, 0, ct))
	now := time.Date(2026, 11, 1, 18, 0, 0, 0, ct).UnixMilli()
	e := New(m473Cfg())
	Seed(e, bars, now)
	if _, ok := e.State.CarriedBoxes["ftgh"]; !ok {
		t.Fatalf("Seed must rebuild the Friday FTGH box as carried: %+v", e.State.CarriedBoxes)
	}
	e.SeedChartDisplay(bars, now)
	if !m473HasBoxEdges(e) {
		t.Fatalf("Sunday 18:00 chart must show the Friday box after a restart: %+v", e.State.ChartLevels)
	}
	// Monday 08:30 it is gone.
	e2 := New(m473Cfg())
	Seed(e2, bars, time.Date(2026, 11, 2, 8, 30, 0, 0, ct).UnixMilli())
	if len(e2.State.CarriedBoxes) != 0 {
		t.Fatalf("Friday box must be expired at Monday 08:30: %+v", e2.State.CarriedBoxes)
	}
}

// Thursday-evening restart (a plain weekday roll).
func TestM473B1SeedRebuildsThursdayBoxEvening(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 29, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	Seed(e, bars, time.Date(2026, 10, 29, 20, 0, 0, 0, ct).UnixMilli())
	if _, ok := e.State.CarriedBoxes["ftgh"]; !ok {
		t.Fatalf("Seed at Thu 20:00 must carry the Thu box: %+v", e.State.CarriedBoxes)
	}
}

// A box confirmed BEFORE 08:30 (08:20) is standing at the open and valid to the
// next open — it must not die at the 17:00 roll nor expire at 08:30 same day
// [Oct D3.3 @ 41:28-41:36, 42:00].
func TestM473B1PreOpenBoxCarried(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 29, 8, 13, 0, 0, ct)) // confirmed ~08:20
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	if !m473HasBoxEdges(e) {
		t.Fatalf("setup: the 08:20 box did not form")
	}
	m473Tick(e, bars, time.Date(2026, 10, 29, 18, 0, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("a pre-open (08:20) box must survive the 17:00 roll")
	}
	m473Tick(e, bars, time.Date(2026, 10, 30, 8, 29, 0, 0, ct))
	if !m473HasBoxEdges(e) {
		t.Fatalf("still valid at 08:29 next day")
	}
	m473Tick(e, bars, time.Date(2026, 10, 30, 8, 30, 0, 0, ct))
	if m473HasBoxEdges(e) {
		t.Fatalf("expired at next open 08:30")
	}
	if got := boxExpiryMs(Box{FormedAtMs: time.Date(2026, 10, 29, 8, 20, 0, 0, ct).UnixMilli()}); got != time.Date(2026, 10, 30, 8, 30, 0, 0, ct).UnixMilli() {
		t.Fatalf("08:20 box expiry must be next day 08:30, got %s", time.UnixMilli(got).In(ct))
	}
	// Holiday-eve: Wed 25 Nov 2026 box -> Thanksgiving Thu closed -> Fri 08:30
	// is the first RTH open the calendar offers (or later), never Thu 26 Nov.
	exp := time.UnixMilli(boxExpiryMs(Box{FormedAtMs: time.Date(2026, 11, 25, 10, 0, 0, 0, ct).UnixMilli()})).In(ct)
	if exp.Day() == 26 {
		t.Fatalf("expiry must skip the closed Thanksgiving open, got %s", exp)
	}
}

// SeedChartDisplay is display-only: no carried-box writes, no notes.
func TestM473B1SeedChartDisplayStaysPure(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 30, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.State.CarriedBoxes = map[string]Box{}
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	m473Tick(e, bars, time.Date(2026, 11, 1, 18, 0, 0, 0, ct)) // carried now holds the box
	before := len(e.State.CarriedBoxes)
	notes := len(e.notes)
	e.SeedChartDisplay(bars, time.Date(2026, 11, 2, 9, 0, 0, 0, ct).UnixMilli()) // box expired at 08:30
	if len(e.State.CarriedBoxes) != before || len(e.notes) != notes {
		t.Fatalf("SeedChartDisplay must not mutate carried boxes or note")
	}
}

// A box that forms in the evening vs the carried one: the newest per role wins.
func TestM473B1NewEveningBoxBeatsCarried(t *testing.T) {
	ct := ctime()
	bars := m473Tape(time.Date(2026, 10, 29, 10, 0, 0, 0, ct))
	e := New(m473Cfg())
	e.Tick(bars, bars[len(bars)-1].CloseTime)
	old := e.State.LatchedBoxes["ftgh"]
	// evening box 100 pts higher at 19:00
	eve := m473Tape(time.Date(2026, 10, 29, 19, 0, 0, 0, ct))
	for i := range eve {
		eve[i].Open += 100
		eve[i].High += 100
		eve[i].Low += 100
		eve[i].Close += 100
	}
	all := append(append([]market.Kline{}, bars...), eve...)
	e.Tick(all, all[len(all)-1].CloseTime)
	nb, ok := e.State.LatchedBoxes["ftgh"]
	if !ok || nb.Bottom == old.Bottom {
		t.Fatalf("the new evening FTGH must be the live box (newest wins), got %+v vs old %+v", nb, old)
	}
}
