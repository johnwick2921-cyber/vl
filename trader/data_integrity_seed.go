package trader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"vl/market"
	ntTrader "vl/trader/ninjatrader"
)

// ── THE MENTOR'S SEEDED SERIES (m445 round 2, reviewer A, D1) ───────────────
//
// See trader/ninjatrader/bar_integrity_series.go for the why. The mentor seed
// (mentorSeed) and every re-seed after a history refill (mentorReseedOnRefill)
// hand the 1m series they walk to noteSeedSeries, which MEASURES it
// (ntTrader.SeriesRTHHoles) and keeps the result; dataIntegrityVerdictAt reads
// it for every mentor trader after the tracker's own answer. Nothing is
// persisted: a restart re-seeds, and re-measures.

// seedSeriesIntegrity is the last measurement of the seeded series.
type seedSeriesIntegrity struct {
	mu       sync.Mutex
	holes    []ntTrader.IntegrityHole
	judged   bool
	judgedAt time.Time
	nBars    int
}

// noteSeedSeries measures the series the evaluator is being seeded from.
func (at *AutoTrader) noteSeedSeries(series []market.Kline, now time.Time) {
	if at == nil {
		return
	}
	times := make([]int64, 0, len(series))
	for _, b := range series {
		times = append(times, b.OpenTime)
	}
	holes := ntTrader.SeriesRTHHoles(mentorSeedSymbol, "seeded 1m series", times, now)
	at.seedSeries.mu.Lock()
	changed := !sameSeedHoles(at.seedSeries.holes, holes) || !at.seedSeries.judged
	at.seedSeries.holes, at.seedSeries.judged, at.seedSeries.judgedAt, at.seedSeries.nBars = holes, true, now, len(times)
	at.seedSeries.mu.Unlock()
	if !changed {
		return
	}
	if len(holes) == 0 {
		at.logInfof("🧑‍🏫 seeded series check: %d 1m bars, no RTH hole in the last %d days — the key levels stand on a complete series", len(times), ntTrader.IntegritySeedReachDays)
		return
	}
	wins := make([]string, 0, len(holes))
	for _, h := range holes {
		wins = append(wins, fmt.Sprintf("%s (%d open min)", h.Window, h.Missing))
	}
	at.logWarnf("🧑‍🏫 seeded series check: %d 1m bars, %d hole(s) take RTH minutes in the last %d days: %s — the mentor's key levels were walked over them, so NEW mentor entries are paused until a history reload fills them (the evaluator then re-seeds) or they leave the %d-day reach",
		len(times), len(holes), ntTrader.IntegritySeedReachDays, strings.Join(wins, "; "), ntTrader.IntegritySeedReachDays)
}

func sameSeedHoles(a, b []ntTrader.IntegrityHole) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].FromMs != b[i].FromMs || a[i].ToMs != b[i].ToMs {
			return false
		}
	}
	return true
}

// seedSeriesVerdict is the key-level leg of a MENTOR verdict: blocked while the
// last measurement of the seeded series found an RTH hole that is still inside
// the reach. The reach moves with the clock (a hole leaves it at its 90th day).
func (at *AutoTrader) seedSeriesVerdict(now time.Time) dataVerdict {
	at.seedSeries.mu.Lock()
	holes := append([]ntTrader.IntegrityHole(nil), at.seedSeries.holes...)
	at.seedSeries.mu.Unlock()
	horizon := now.Add(-ntTrader.IntegritySeedReachDays * 24 * time.Hour).UnixMilli()
	live := holes[:0:0]
	for _, h := range holes {
		if h.ToMs > horizon {
			live = append(live, h)
		}
	}
	if len(live) == 0 {
		return dataVerdict{}
	}
	parts := make([]string, 0, len(live))
	wins := make([]string, 0, len(live))
	for _, h := range live {
		parts = append(parts, fmt.Sprintf("%s (%d open min)", h.Window, h.Missing))
		wins = append(wins, h.Window)
	}
	sym := mentorSeedSymbol
	return dataVerdict{
		Blocked: true, Class: ntTrader.IntegrityClassHole, Holes: live, Windows: wins,
		Reason: fmt.Sprintf("missing %s 1m bars %s in the series the mentor's key levels were seeded from — %s; the key levels were walked over those candles, so mentor entries stay paused until the stretch is filled (the evaluator re-seeds on the refill) or leaves the %d-day reach",
			sym, strings.Join(parts, "; "), ntTrader.IntegrityAction, ntTrader.IntegritySeedReachDays),
	}
}

// HealthSeedSeriesPause is the read /api/health serves: the CT windows of the
// RTH holes in the series this MENTOR trader's key levels were seeded from, while
// they pause its entries. Empty for a trader that is not in mentor mode or has
// not seeded — never a fabricated "clear" (the field is simply absent then).
func (at *AutoTrader) HealthSeedSeriesPause(now time.Time) []string {
	if at == nil || !at.integrityApplies() || !at.mentorEnabled() {
		return nil
	}
	return at.seedSeriesVerdict(now).Windows
}
