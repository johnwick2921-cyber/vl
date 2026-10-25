package mentor

import (
	"sort"
	"time"

	"vl/market"
)

// SeedChartDisplay (release #50, m422 / C5) fills the DISPLAY-ONLY chart set
// (State.ChartLevels, ChartTrendlines, ChartEMA) once at boot, so a restart
// while the market is closed still draws the last close's levels, EMA, boxes
// and trendlines. Without it Tick never runs (mentorEvalOnce fires on a new
// bar), and the chart stays blank all weekend.
//
// bars is the CURRENT-CONTRACT tick window (the same ~1500 closed 1m bars Tick
// reads), NOT the stitched back-adjusted seed series; now is the evaluator
// clock (BarCloseInstant of the newest bar).
//
// DISPLAY ONLY — the contract is pinned by TestSeedChartDisplayNeverAuthors:
//   - writes ONLY ChartLevels, ChartTrendlines, ChartEMA and ChartSeededAt;
//   - State.Levels stays empty (limits.go R1 reads empty as "no Tick yet");
//   - never calls seededLevels (it advances the watermarks), carryIntradayLines,
//     htFTrendlines, trackTrendlines, latchBoxes, e.note or e.refuse, nor
//     anything that touches arm, touch, latch or watermark state;
//   - the first real Tick overwrites the set and clears ChartSeededAt.
func (e *Evaluator) SeedChartDisplay(bars []market.Kline, now int64) {
	if e == nil || !e.Cfg.Enabled || len(bars) < 2 {
		return
	}
	var levels []Level

	// 1H RTH key levels, minus the ones a closed 1H body already deleted.
	levels = append(levels, withoutDeleted(e.State.SeedLevels, e.State.DeletedLevels)...)

	// EMA 34 / EMA 9 on the exec frame, READ from the seeded state (no fold).
	ema34, ema9 := e.State.EMA34, e.State.EMA9
	if e.Cfg.Exec2mAfter30m && e.Cfg.EMATFMinutes <= 1 &&
		emaScheduleTF(e.Cfg, e.Cfg.EMATFMinutes, now) == 2 && e.State.EMA2mWarmed {
		ema34, ema9 = e.State.EMA34x2, e.State.EMA9x2
	}
	if e.State.EMA34Warmed && ema34 > 0 {
		levels = append(levels, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: ema34})
	}
	if e.State.EMA34Warmed && ema9 > 0 {
		levels = append(levels, Level{Key: string(KindEMA9), Kind: KindEMA9, Price: ema9})
	}

	// HTF trigger lines (target-only levels, drawn as lines).
	if e.State.HTF.FourH.Dir != "" && e.State.HTF.FourH.Price != 0 {
		levels = append(levels, Level{Key: "htf_4h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.FourH.Price})
	}
	if e.State.HTF.OneH.Dir != "" && e.State.HTF.OneH.Price != 0 {
		levels = append(levels, Level{Key: "htf_1h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.OneH.Price})
	}

	// The seeded latched boxes' edges — only while the latch still belongs to
	// the trading day `now` is in (Tick clears a stale latch; we just skip it).
	var boxes []Box
	if e.State.LatchedBoxesDay == boxLatchDayKey(now, e.Cfg) {
		keys := make([]string, 0, len(e.State.LatchedBoxes))
		for k := range e.State.LatchedBoxes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			boxes = append(boxes, e.State.LatchedBoxes[k])
		}
	}
	levels = append(levels, BoxEdgeLocations(boxes)...)

	// Trendlines: the pure builder only (no carry, no HTF tracking, no refuse).
	exec2m := e.Cfg.Exec2mAfter30m && exec2mReads2m(now)
	trendlines, _ := trendlinesBuildExec(bars, time.UnixMilli(now), exec2m)
	tlLevels, _ := TrendlineLevels(trendlines, boxes, levels, bars)
	levels = append(levels, tlLevels...)

	e.State.ChartLevels = levels
	e.State.ChartTrendlines = trendlines
	e.State.ChartEMA = emaSeriesChartSched(bars, e.Cfg)
	e.State.ChartSeededAt = now
}
