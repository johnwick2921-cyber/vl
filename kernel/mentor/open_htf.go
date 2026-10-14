package mentor

import (
	"time"

	"vl/market"
)

// SeedOpenHTF — MENTOR-NY item 6 (S2). A seed that runs AFTER today's 08:30
// (a boot mid-session) re-latches the day gate on the 4h/1h of THAT moment.
// This replays the SAME closed 1m history the seed used, cut at the 08:30 read
// (bars that closed by 08:30), through the same TriggerReplay1m and print-window
// feed, from the HTF state the seed started from (prior), to READ what the 08:30
// decision saw. The day-gate line then shows both values. Called by the trader
// right after Seed/SeedFull with the same bars; a seed before 08:30 (or after
// the 17:00 roll) records nothing. It never changes the live HTF state.
//
// Kept out of seed.go on purpose (that file belongs to the seed-state lane).
func SeedOpenHTF(e *Evaluator, bars1m []market.Kline, prior HTF, now int64) {
	if e == nil {
		return
	}
	key := tradingDayKey(time.UnixMilli(now).In(ctime()))
	open := (DayGateReport{Key: key}).OpenAt()
	if open <= 0 || now < open+60_000 {
		return
	}
	htfBars := htfFeedBars(closedOnly(bars1m, now), e.Cfg.PrintWindows)
	cut := 0
	for cut < len(htfBars) && htfBars[cut].OpenTime < open {
		cut++
	}
	four := TriggerReplay1m(prior.FourH, htfBars[:cut], 240, e.Cfg)
	one := TriggerReplay1m(prior.OneH, htfBars[:cut], 60, e.Cfg)
	e.State.OpenHTF = OpenHTFRead{Key: key, FourHDir: four.Dir, OneHDir: one.Dir}
}
