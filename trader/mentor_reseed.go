package trader

import (
	"encoding/json"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── MENTOR-SEED-STATE (release #23) ─────────────────────────────────────────
//
// N1 (P1) — the boot seed raced the history refill, and nothing re-seeded.
// mentorSeedAtStart reads the store at trader construction. The AddOn's
// subscribe-time history is HELD by the replay hold and reaches the store only
// on the next live frame (10-14: seed 08:22:40, "1999 row(s) released to the
// store" 08:23:00), and a bar hole can be refilled at any time. The evaluator's
// history-derived state never re-reads a bar it already folded in, so the 10-14
// boot traded on a 1h trigger seeded from a 12-minute 07:00 bucket (short
// @30790.75; truth short @30716.00), a 1m EMA 34 +30.7 pts off, and refused 4
// ISB candidates on the false 1h flip.
//
// Now every refill bumps a generation (trader/ninjatrader/history_refill.go);
// the mentor tick compares it with the generation its state was seeded at and,
// when it moved, re-runs the STATE-ONLY half of the seed from the store
// (mentor.ReseedState: trigger lines, swing line, 1m EMAs, key-level deletion)
// before it ticks — latches, positions, visit counters, arms and legs kept.
// Every re-seed is logged with its before → after and counted.
//
// N6 (P2) — the seed now honours the print windows of every day it replays.

// mentorSeedSymbol is the instrument the mentor seeds and ticks on.
const mentorSeedSymbol = "MNQ"

// mentorRefillGeneration reads the MNQ 1m history-refill generation.
func mentorRefillGeneration() (uint64, string) {
	return ntTrader.HistoryRefillGeneration(mentorSeedSymbol)
}

// mentorReseedOnRefill runs under mentorEvalMu, before the tick, at the tick's
// own clock (BarCloseInstant of the newest closed bar). A no-op until a refill
// moves the generation past the one the state was seeded at.
func (at *AutoTrader) mentorReseedOnRefill(nowMs int64) {
	if at == nil || at.mentorEval == nil || at.store == nil {
		return
	}
	gen, why := mentorRefillGeneration()
	if gen == at.mentorSeedHistGen {
		return
	}
	// Claim the generation FIRST: a refill landing while this re-seed reads the
	// store bumps it again and the next tick re-seeds once more (idempotent).
	at.mentorSeedHistGen = gen
	start := time.Now()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	stitched1m, full1HRTH, _, _, _, seamCovered := at.mentorFullKeyLevelHistory(bh)
	bars1m := stitched1m
	if len(full1HRTH) == 0 {
		rows, err := bh.LastNBarsCurrentContract(mentorSeedSymbol, "1m", mentorSeedBars1mN)
		if err != nil {
			at.mentorCount("reseed_refused")
			at.logWarnf("🧑‍🏫 mentor re-seed after a history refill (%s) REFUSED — 1m store read failed (%v); state kept as it was", why, err)
			return
		}
		bars1m, full1HRTH, seamCovered = storeBarsToKlines(rows, 60_000), nil, nil
	}
	at.noteSeedSeries(bars1m, seamCovered, time.UnixMilli(nowMs)) // m445 round 2: the refilled series is measured again — a filled hole resumes the gate
	var rep mentor.ReseedReport
	at.mentorWithHistoryPrintWindows(bars1m, func() {
		rep = mentor.ReseedState(at.mentorEval, bars1m, full1HRTH, nowMs)
	})
	ms := time.Since(start).Milliseconds()
	switch {
	case rep.Refused != "":
		at.mentorCount("reseed_refused")
		at.logWarnf("🧑‍🏫 mentor re-seed after a history refill (%s) at %s: %s [%d ms]", why, kernel.ClockCTSeconds(time.UnixMilli(nowMs)), rep.Line(), ms)
	case rep.Changed():
		at.mentorCount("reseed_after_refill")
		at.mentorCount("reseed_changed")
		at.logWarnf("🧑‍🏫 mentor re-seed after a history refill (%s) at %s: %s [%d ms]", why, kernel.ClockCTSeconds(time.UnixMilli(nowMs)), rep.Line(), ms)
	default:
		at.mentorCount("reseed_after_refill")
		at.logInfof("🧑‍🏫 mentor re-seed after a history refill (%s) at %s: %s [%d ms]", why, kernel.ClockCTSeconds(time.UnixMilli(nowMs)), rep.Line(), ms)
	}
	at.mentorRefreshDepths()
}

// mentorRefreshChartSeedOnClosed (m427/2) rebuilds the boot DISPLAY seed
// (mentor.SeedChartDisplay, via mentorSeedChartDisplay) when the history-refill
// generation moved after it was built, on a CLOSED market where no Tick has run
// since boot (ChartSeededAt != 0). Without it a restart in the 16:00-17:00 break
// or over a weekend showed the pre-release store's levels / EMA / lines, marked
// "as of last close", until the first bar at the reopen — the replay hold is
// released ~30 s after boot, and the only re-seed reader is the Tick.
//
// DISPLAY ONLY, same contract as m422: it writes through SeedChartDisplay alone
// (ChartLevels/ChartTrendlines/ChartEMA/ChartSeededAt), reads the same current-
// contract store window, and tracks its OWN generation (mentorChartSeedGen) so
// it never claims the one the first real Tick's state re-seed waits on. A no-op
// when the market is open (the Tick path owns it), after any Tick, or when the
// generation has not moved.
func (at *AutoTrader) mentorRefreshChartSeedOnClosed(now time.Time) {
	if at == nil || at.store == nil || kernel.IsCMEOpen(now) {
		return
	}
	at.mentorEvalMu.Lock()
	defer at.mentorEvalMu.Unlock()
	if at.mentorEval == nil || at.mentorEval.State.ChartSeededAt == 0 {
		return // no evaluator, or a real Tick already replaced the display seed
	}
	gen, _ := mentorRefillGeneration()
	if gen == at.mentorChartSeedGen {
		return
	}
	at.mentorChartSeedGen = gen
	bh := store.NewBarHistoryStore(at.store.GormDB())
	rows, err := bh.LastNBarsCurrentContract(mentorSeedSymbol, "1m", mentorSeedBars1mN)
	if err != nil {
		at.mentorCount("chart_reseed_closed_refused")
		at.logWarnf("🧑‍🏫 chart display re-seed after a history refill REFUSED — 1m store read failed (%v); display kept as it was", err)
		return
	}
	at.mentorSeedChartDisplay(storeBarsToKlines(rows, 60_000))
	at.mentorCount("chart_reseed_closed")
	at.logInfof("🧑‍🏫 chart display re-seeded on the closed market after a history refill (display only)")
}

// mentorDayEventsOnForTest is the per-date calendar seam (tests; nil → the
// stored slice for that date).
var mentorDayEventsOnForTest func(date string) ([]calendar.Event, bool)

// mentorDayEventsOn returns one CT calendar date's stored events (the SAME
// slice mentorDayEvents reads for today).
func (at *AutoTrader) mentorDayEventsOn(date string) ([]calendar.Event, bool) {
	if mentorDayEventsOnForTest != nil {
		return mentorDayEventsOnForTest(date)
	}
	if at == nil || at.store == nil {
		return nil, false
	}
	slice, err := at.store.Calendar().GetSlice(date)
	if err != nil || slice == nil {
		return nil, false
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil {
		return nil, false
	}
	return evs, true
}

// mentorHistoryPrintWindows (N6) is the union of the red-folder 07:30 print
// windows of EVERY CT date the bars cover a 07:00-hour bar on — the windows a
// running bot applied to each of those days' ticks (mentorNewsPrintWindows, by
// that day's date). A date with no readable slice contributes none, exactly as
// the tick path does on that day.
func (at *AutoTrader) mentorHistoryPrintWindows(bars []market.Kline) []mentor.PrintWindow {
	ct := kernel.CTLocation()
	seen := map[string]bool{}
	var out []mentor.PrintWindow
	for _, b := range bars {
		t := time.UnixMilli(b.OpenTime).In(ct)
		if t.Hour() != 7 {
			continue
		}
		d := t.Format("2006-01-02")
		if seen[d] {
			continue
		}
		seen[d] = true
		if evs, ok := at.mentorDayEventsOn(d); ok {
			out = append(out, mentorNewsPrintWindowsFromEvents(evs)...)
		}
	}
	return out
}

// mentorWithHistoryPrintWindows runs a seed with the history's print windows
// in the evaluator config, then restores the tick's own (today's) windows.
func (at *AutoTrader) mentorWithHistoryPrintWindows(bars []market.Kline, seed func()) {
	prev := at.mentorEval.Cfg.PrintWindows
	at.mentorEval.Cfg.PrintWindows = at.mentorHistoryPrintWindows(bars)
	defer func() { at.mentorEval.Cfg.PrintWindows = prev }()
	seed()
}
