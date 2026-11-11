package mentor

import (
	"math"
	"time"

	"vl/market"
)

// D2-G10 — Knock Knock counts tests per price ZONE, pre-market included.
// "người ta sẽ không còn muốn vô ở những lần sau cái lần thứ 3"; "lần thứ 4 là ...
// có khả năng break nhiều nhất"; "resistor mở lên nó test 3 lần, 4 lần ... đừng vô
// short ngay chỗ đó nữa" [Oct D2.1 @ 21:18-21:36, 22:24-22:39, 23:24-23:43]
// (OctD2.1-R22/R23/R25/R27/R28/R31/R34/R38). The line 28,816.25 is drawn "from
// the far left" (07:37) and the pre-market tests count (frame s_01351). The level
// visit counter (Visits, B23) is keyed by level identity and reset when the day
// gate latches at 08:30: a horizontal S/R re-keyed at each slightly different
// price (flat_resistance:<price>) never builds a count, and the 08:30 reset wipes
// the pre-market tests.

// ZoneVisit is one price zone's test count for a trading day.
type ZoneVisit struct {
	Price   float64 `json:"price"`    // the zone's anchor: the first level price that opened it
	Side    Side    `json:"side"`     // the side price approached from at the tests
	N       int     `json:"n"`        // distinct tests (reject visits) so far
	LastBar int64   `json:"last_bar"` // CloseTime of the last test's candle (one candle is one test)
}

// zoneNear is the width of a zone: the day's "near" distance (m428 R2 — the same
// horizontal-line tolerance the flat pair uses).
func zoneNear(bars []market.Kline) float64 {
	if len(bars) == 0 {
		return 0
	}
	return flatNearPts(bars, len(bars)-1)
}

// zoneCountsLevel is the levels Knock Knock is about: HORIZONTAL lines "tested
// again and again" — key levels, old highs/lows (incl. the flat S/R pair) and
// gap edges. A moving line (EMA 9/34, the trigger retest, a trendline, a wick
// zone) is never a zone: its touches are every few bars by construction.
func zoneCountsLevel(lvl Level) bool {
	switch lvl.Kind {
	case KindKeyLevel, KindOldExtreme, KindGapEdge:
		return true
	}
	return false
}

func (e *Evaluator) zoneIndex(price float64, side Side, near float64) int {
	best, bd := -1, math.MaxFloat64
	for i, z := range e.State.ZoneVisits {
		if z.Side != side {
			continue
		}
		if d := math.Abs(z.Price - price); d <= near && d < bd {
			best, bd = i, d
		}
	}
	return best
}

// noteZoneVisit records one reject visit of a level into its zone. The zone
// table belongs to the TRADING day (the 17:00 CT roll), not the 08:30 day-gate
// key, so the pre-market tests of the same CME session count.
func (e *Evaluator) noteZoneVisit(lvl Level, tr Touch, bars []market.Kline, now int64) {
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))
	if e.State.ZoneVisitsDay != day {
		e.State.ZoneVisits = nil
		e.State.ZoneVisitsDay = day
	}
	near := zoneNear(bars)
	i := e.zoneIndex(lvl.Price, tr.ApproachedFrom, near)
	if i < 0 {
		e.State.ZoneVisits = append(e.State.ZoneVisits, ZoneVisit{Price: lvl.Price, Side: tr.ApproachedFrom, N: 1, LastBar: tr.RefBar.CloseTime})
		return
	}
	z := &e.State.ZoneVisits[i]
	if z.LastBar == tr.RefBar.CloseTime {
		return // two levels of one zone touched by the SAME candle are one test
	}
	z.N++
	z.LastBar = tr.RefBar.CloseTime
}

// zoneVisitsOver reports whether the level's zone has been tested more than
// LevelMaxVisits times this trading day ("after the 3rd" — the 4th test refuses).
func (e *Evaluator) zoneVisitsOver(lvl Level, tr Touch, bars []market.Kline, now int64) bool {
	if !e.Cfg.KnockKnockZone || e.Cfg.LevelMaxVisits <= 0 || !zoneCountsLevel(lvl) {
		return false
	}
	if e.State.ZoneVisitsDay != tradingDayKey(time.UnixMilli(now).In(ctime())) {
		return false
	}
	i := e.zoneIndex(lvl.Price, tr.ApproachedFrom, zoneNear(bars))
	return i >= 0 && e.State.ZoneVisits[i].N > e.Cfg.LevelMaxVisits
}
