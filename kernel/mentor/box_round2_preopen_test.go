package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D3-G1 + reread D3.3a-F1 / D3.3b-F1 / D3.3b-F2 — the course's own FTGH
// box of Fri 30 Oct is built from two PRE-OPEN peaks (peak 1 ~08:00, peak 2
// ~08:29), neither the session extreme: "Bên tay trái mình có thấy một cái đỉnh,
// bên tay phải ... 1 cái đỉnh ... song song với nhau. Mình vẽ được 1 cái box"
// [Oct D3.3 @ 17:13-17:25] (OctD3.3-R23/R24, E3 frames s_00866/s_00977/s_01286);
// "Là Market Open, 8 giờ rưỡi sáng. Bôm, mở cửa ra ... bên đây mình đã vẽ được
// rồi thì nó sẽ valid cho tới sáng ngày mai" [@ 41:28-41:36] (R51); "trước khi
// Market Open tầm 10 phút thì em coi có ... nếu mà nó có thì em vẽ" [@ 42:00]
// (R52). Real stored bars: testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv
// (MNQ 12-26, extracted read-only from data.db).

func round2Oct30Tick(t *testing.T, cfg Config, until time.Time) *Evaluator {
	t.Helper()
	bars := m413Load(t, "testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv")
	e := New(cfg)
	for i := range bars {
		if bars[i].CloseTime+1 > until.UnixMilli() {
			break
		}
		e.Tick(append([]market.Kline(nil), bars[:i+1]...), bars[i].CloseTime+1)
	}
	return e
}

func round2Live(e *Evaluator, k BoxKind) (Box, bool) {
	for _, b := range liveBoxes(e.State.LatchedBoxes, e.State.PostEscapeBoxes) {
		if b.Kind == k {
			return b, true
		}
	}
	return Box{}, false
}

func TestRound2D3G1PreOpenPeaksFormTheMorningFTGH(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	e := round2Oct30Tick(t, cfg, time.Date(2026, 10, 30, 8, 40, 0, 0, ctime()))
	b, ok := round2Live(e, FTGH)
	if !ok {
		t.Fatal("no live FTGH at 08:40")
	}
	// course box ~28,812.50-28,818.25: peak 1 wick 08:00 H 28,818.25, peak 2
	// body top 08:29 O 28,812.50 (the 08:29 candle is red, body top = open).
	if b.Top != 28818.25 || b.Bottom != 28812.50 {
		t.Fatalf("live FTGH = %.2f-%.2f (formed %s), want the pre-open pair 28812.50-28818.25", b.Bottom, b.Top, time.UnixMilli(b.FormedAtMs).In(ctime()).Format("15:04"))
	}
}

// knob OFF reproduces today's behaviour: the evening/overnight box stands.
func TestRound2D3G1KnobOffKeepsTheOvernightBox(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	cfg.BoxPreOpenLookbackMin = 0
	e := round2Oct30Tick(t, cfg, time.Date(2026, 10, 30, 8, 40, 0, 0, ctime()))
	b, ok := round2Live(e, FTGH)
	if !ok {
		t.Fatal("no live FTGH at 08:40")
	}
	if b.Top < 29000 {
		t.Fatalf("knob off: live FTGH = %.2f-%.2f, want the overnight 29,17x box", b.Bottom, b.Top)
	}
}
