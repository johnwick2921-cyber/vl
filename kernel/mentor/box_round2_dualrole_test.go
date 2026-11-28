package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D3-G2 — a bar that is BOTH a 3-bar swing high and a 3-bar swing low
// (a big outside bar) is recorded in both roles. Thu 29 Oct 09:05 (O 29,619 H
// 29,629.25 L 29,586 C 29,588.50) is the second peak of the course's red FTGH
// 29,621.25-29,632.75 (E11, frame s_02696) "lấy cái râu cao nhất của cái đỉnh
// 1, kéo xuống cái thân gần nhất của cái đỉnh 2" [Oct D3.3 @ 15:46] (OctD3.3-R25):
// before the fix the switch kept it as a low only, so the pair fell to the 09:19
// high and the box formed 14 minutes later, 15.5 pts lower. Real stored bars:
// testdata/r2_mnq_1m_20261028_1700_to_20261029_1200ct.csv.

func round2Oct29Tick(t *testing.T, cfg Config, until time.Time) *Evaluator {
	t.Helper()
	bars := m413Load(t, "testdata/r2_mnq_1m_20261028_1700_to_20261029_1200ct.csv")
	e := New(cfg)
	for i := range bars {
		if bars[i].CloseTime+1 > until.UnixMilli() {
			break
		}
		e.Tick(append([]market.Kline(nil), bars[:i+1]...), bars[i].CloseTime+1)
	}
	return e
}

func TestRound2D3G2DualRoleBarIsBothSwings(t *testing.T) {
	withFlatNear(t)
	e := round2Oct29Tick(t, m473Cfg(), time.Date(2026, 10, 29, 9, 12, 0, 0, ctime()))
	b, ok := round2Live(e, FTGH)
	if !ok {
		t.Fatal("no live FTGH at 09:12")
	}
	if b.Top != 29629.75 || b.Bottom != 29619.00 {
		t.Fatalf("live FTGH = %.2f-%.2f (formed %s), want 29619.00-29629.75 (peak 1 wick 09:02, peak 2 body top 09:05)", b.Bottom, b.Top, time.UnixMilli(b.FormedAtMs).In(ctime()).Format("15:04"))
	}
}

func TestRound2D3G2KnobOffKeepsTheSingleRoleRead(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	cfg.BoxSwingBothRoles = false
	e := round2Oct29Tick(t, cfg, time.Date(2026, 10, 29, 9, 22, 0, 0, ctime()))
	b, ok := round2Live(e, FTGH)
	if !ok {
		t.Fatal("no live FTGH at 09:22")
	}
	if b.Bottom != 29605.75 {
		t.Fatalf("knob off: live FTGH = %.2f-%.2f, want today's 29,605.75-29,629.75 (09:19 pair)", b.Bottom, b.Top)
	}
}
