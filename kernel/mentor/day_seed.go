package mentor

import (
	"time"

	"vl/market"
)

// ── DAY-LATCH SEED RESTORE (B1, release #30) ────────────────────────────────
//
// A restart must NOT re-decide the trading day. Before this, a boot after 08:30
// re-latched the §7 day gate on the BOOT's 4h/1h lines (the first Tick latched
// an empty State.Day), so 10-15 went SPENT at 08:30 → DAY OFF after the 11:32
// and 13:43 restarts (165 RTH minutes) while a never-restarted bot stayed
// SPENT. The latch is frozen AT 08:30; the seed restores that decision.

// SeedDayLatch (B1) restores the §7 day latch when `now` is past the trading
// day's 08:30 CT open: the 08:30 4h/1h read (replayed from the bars closed by
// 08:30 through the same freeze/drop the live path uses) + the frozen Globex
// run, exactly as the running bot froze it at 08:30. Then the one-way DayOff
// recheck is replayed over the 1h closes since, so a clear that happened before
// the restart stays cleared. Before 08:30, or when the latch is already frozen
// (a re-seed must not re-decide), nothing changes.
func SeedDayLatch(e *Evaluator, bars1m []market.Kline, now int64) {
	if e == nil || e.State.Day.Key != "" {
		return // already latched, or nothing to restore
	}
	key := tradingDayKey(time.UnixMilli(now).In(ctime()))
	open := (DayGateReport{Key: key}).OpenAt()
	if open <= 0 || now < open+60_000 {
		return // at/before the 08:30 read: the first tick latches live, unchanged
	}
	four, one := openHTFLines(e, bars1m, open)
	m := GlobexMeasure(bars1m, open, ctime())
	dg := DayGate{SpentPts: e.Cfg.DayGateSpentPts, TargetCapPts: e.Cfg.DayGateTargetCapPts}
	conflict := HTFConflict(HTF{FourH: four, OneH: one})
	v, _ := DayGateVerdict(m.Run, m.OK, conflict, dg)
	e.State.Day = DayLatch{Key: key, Verdict: v}
	e.State.DayGateReport = DayGateReport{
		Key:       key,
		Verdict:   v,
		RunPts:    m.Run,
		HaveRun:   m.OK,
		HiPx:      m.Hi,
		HiAt:      m.HiAt,
		LoPx:      m.Lo,
		LoAt:      m.LoAt,
		FourHDir:  four.Dir,
		OneHDir:   one.Dir,
		Conflict:  conflict,
		SpentPts:  dg.SpentPts,
		ReadAt:    open,
		Restored:  true,
	}
	// Replay the one-way DayOff recheck over the 1h closes since 08:30: a clear
	// that happened before the restart stays cleared.
	if v == DayOff && e.Cfg.DayOffRecheck {
		e.State.Day = replayDayOffRecheck(e, key, open, four, one, bars1m, now)
	}
}

// openHTFLines replays the 4h/1h trigger lines as they stood at `open` (the
// 08:30 CT read) over the bars closed by `open`, through the SAME freeze/drop
// the live path uses. Empty prior — a fresh boot starts from empty lines.
func openHTFLines(e *Evaluator, bars1m []market.Kline, open int64) (four, one TriggerLine) {
	closed := closedOnly(bars1m, open)
	if e.Cfg.NewsBarsExcluded {
		frozen := func(openMs int64) bool { return newsBarFrozen(openMs, e.Cfg) }
		return TriggerReplay1mFrozen(TriggerLine{}, closed, 240, e.Cfg, frozen),
			TriggerReplay1mFrozen(TriggerLine{}, closed, 60, e.Cfg, frozen)
	}
	feed := htfFeedBars(closed, e.Cfg.PrintWindows)
	return TriggerReplay1m(TriggerLine{}, feed, 240, e.Cfg),
		TriggerReplay1m(TriggerLine{}, feed, 60, e.Cfg)
}

// replayDayOffRecheck replays the one-way DayOff recheck from the 08:30 read
// forward: at each 1h close since 08:30 the 4h/1h lines are advanced and the
// latch clears the moment they agree (HTFAgreement) — the same one-way rule the
// live tick runs. Returns the (possibly cleared) latch.
func replayDayOffRecheck(e *Evaluator, key string, open int64, four, one TriggerLine, bars1m []market.Kline, now int64) DayLatch {
	latch := DayLatch{Key: key, Verdict: DayOff}
	rc := DayRecheck{Key: key}
	var since []market.Kline
	for _, b := range closedOnly(bars1m, now) {
		if b.OpenTime >= open {
			since = append(since, b)
		}
	}
	if len(since) == 0 {
		e.State.DayRecheck = rc
		return latch
	}
	advance := func(prefix []market.Kline) HTF {
		if e.Cfg.NewsBarsExcluded {
			frozen := func(openMs int64) bool { return newsBarFrozen(openMs, e.Cfg) }
			return HTF{
				FourH: TriggerReplay1mFrozen(four, prefix, 240, e.Cfg, frozen),
				OneH:  TriggerReplay1mFrozen(one, prefix, 60, e.Cfg, frozen),
			}
		}
		feed := htfFeedBars(prefix, e.Cfg.PrintWindows)
		return HTF{
			FourH: TriggerReplay1m(four, feed, 240, e.Cfg),
			OneH:  TriggerReplay1m(one, feed, 60, e.Cfg),
		}
	}
	for _, b1h := range barsTF(since, 60) {
		if b1h.CloseTime > now {
			break // the still-forming bucket
		}
		var prefix []market.Kline
		for _, b := range since {
			if b.CloseTime <= b1h.CloseTime {
				prefix = append(prefix, b)
			}
		}
		if len(prefix) == 0 {
			continue
		}
		rc.Last1HClose = b1h.CloseTime
		if ok, dir := HTFAgreement(advance(prefix)); ok {
			rc.ClearedDir = dir
			rc.ClearedAt = b1h.CloseTime + 1 // the close boundary, like BarCloseInstant
			rc.Clears = 1
			latch = DayLatch{Key: key, Verdict: DaySpent}
			break
		}
	}
	e.State.DayRecheck = rc
	return latch
}

// SeedTrendlines (B3, release #30) marks the trendline pairs already crossed
// between their anchors before now, and the lines already valid, so the first
// tick does not recount them (the count is a live counter; the seed restores
// the state, never counts). The window subtlety: trendlinesBuild runs over the
// seed's full closed history, which covers today's swings; the live tick builds
// over its sliding window — the today filter keeps both on the same day.
func SeedTrendlines(e *Evaluator, bars1m []market.Kline, now int64) {
	if e == nil {
		return
	}
	lines, crossed := trendlinesBuild(closedOnly(bars1m, now), time.UnixMilli(now))
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))
	e.tlDay = day
	e.tlCrossed = map[string]bool{}
	for _, k := range crossed {
		e.tlCrossed[k] = true
	}
	e.tlValid = map[string]bool{}
	e.tlTF = map[string]int{}
	for _, tl := range lines {
		if tl.ValidAt > 0 && !tl.Dead {
			e.tlValid[tl.key()] = true
			e.tlTF[tl.key()] = tl.TFMin
		}
	}
}
