package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D3-G4 — a carried box stays live. "Cái box này hôm thứ sáu ... vẫn còn
// valid trên chart Chủ nhật" [Oct D3.3 @ 17:00] (OctD3.3-R27; E9: Sun 1 Nov ~21:53
// the retest of the Friday FTGH 28,808.25 -> -17.50 pts, frames s_02558/s_02567)
// and "valid cho tới sáng ngày mai" [@ 41:28] (R51). m473 filled only a role the
// live set LACKED, so the Sunday-evening FTGH (latched 17:13) hid Friday's box
// and the bot went LONG from flipped evening boxes at 21:55/21:56 where the course
// shorts the Friday box. Real stored bars: Fri 30 Oct (08:30 box) + Sun 1 Nov
// 17:00-24:00 (testdata/r2_mnq_*.csv).

func round2FriSunTick(t *testing.T, cfg Config, until time.Time) *Evaluator {
	t.Helper()
	fri := m413Load(t, "testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv")
	sun := m413Load(t, "testdata/r2_mnq_1m_20261101_1700_to_20261102_0000ct.csv")
	e := New(cfg)
	var all []market.Kline
	for _, b := range append(append([]market.Kline(nil), fri...), sun...) {
		all = append(all, b)
		if b.CloseTime+1 > until.UnixMilli() {
			break
		}
		if b.OpenTime < time.Date(2026, 10, 30, 8, 0, 0, 0, ctime()).UnixMilli() {
			continue // only the Friday RTH ticks and the Sunday evening matter
		}
		w := all
		if len(w) > 1500 {
			w = w[len(w)-1500:]
		}
		e.Tick(append([]market.Kline(nil), w...), b.CloseTime+1)
	}
	return e
}

func TestRound2D3G4CarriedFridayBoxStaysLiveOnSunday(t *testing.T) {
	withFlatNear(t)
	e := round2FriSunTick(t, m473Cfg(), time.Date(2026, 11, 1, 21, 56, 0, 0, ctime()))
	_, carried := e.State.CarriedBoxes["ftgh"]
	if !carried {
		t.Fatalf("setup: Friday FTGH not carried: %+v", e.State.CarriedBoxes)
	}
	b := e.State.CarriedBoxes["ftgh"]
	found := false
	for _, l := range e.State.ChartLevels {
		if l.Key == "ftgh:"+fnum(b.Top)+":"+fnum(b.Bottom)+":top" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Sunday 21:56: the carried Friday FTGH %.2f-%.2f is not among the live box edges %+v", b.Bottom, b.Top, e.State.ChartLevels)
	}
}

func TestRound2D3G4KnobOffKeepsTheEveningBox(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	cfg.BoxCarriedStaysLive = false
	e := round2FriSunTick(t, cfg, time.Date(2026, 11, 1, 21, 56, 0, 0, ctime()))
	b := e.State.CarriedBoxes["ftgh"]
	for _, l := range e.State.ChartLevels {
		if l.Key == "ftgh:"+fnum(b.Top)+":"+fnum(b.Bottom)+":top" {
			t.Fatalf("knob off: the carried box must stay hidden behind the Sunday evening FTGH (today's behaviour)")
		}
	}
}
