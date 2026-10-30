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
func (at *AutoTrader) noteSeedSeries(series []market.Kline, covered []int64, now time.Time) {
	if at == nil {
		return
	}
	times := make([]int64, 0, len(series))
	for _, b := range series {
		times = append(times, b.OpenTime)
	}
	if len(times) == 0 {
		// #55 m449: an EMPTY series (cold store, a failed read) was not measured
		// as complete - there is nothing to measure. It stays UNJUDGED, so
		// seedSeriesVerdict keeps blocking (fail-closed) and no "complete series"
		// line is printed over a read that returned nothing.
		at.logWarnf("🧑‍🏫 seeded series check: the 1m series the key levels are seeded from is EMPTY (cold store or a failed store read) - it was NOT measured, so NEW mentor entries stay paused until a seed measures a series")
		return
	}
	holes := ntTrader.SeriesRTHHoles(mentorSeedSymbol, "seeded 1m series", times, covered, now)
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
	at.logWarnf("🧑‍🏫 seeded series check: %d 1m bars, %d hole(s) take RTH minutes in the last %d days: %s — no contract in the stitch stores those minutes and the mentor's key levels were walked over them, so NEW mentor entries are paused. %s",
		len(times), len(holes), ntTrader.IntegritySeedReachDays, strings.Join(wins, "; "), ntTrader.IntegrityActionSeeded)
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
//
// #55 m449: and blocked while NO seed has been measured. The leg used to read
// at.seedSeries.holes alone, so a mentor trader whose series had never been
// measured (the seed not yet run, or an empty read) had empty holes and read
// CLEAR - an unknown that looked clean. Unknown is BLOCKED (class unknown, so
// the pause episode keeps its quiet boot grace before any owner push).
func (at *AutoTrader) seedSeriesVerdict(now time.Time) dataVerdict {
	at.seedSeries.mu.Lock()
	holes := append([]ntTrader.IntegrityHole(nil), at.seedSeries.holes...)
	judged := at.seedSeries.judged
	at.seedSeries.mu.Unlock()
	if !judged {
		return dataVerdict{Blocked: true, Class: ntTrader.IntegrityClassUnknown,
			Reason: fmt.Sprintf("missing-data check UNKNOWN for %s: the series the mentor's key levels are seeded from has not been measured yet (the seed runs at start and after every history refill) - mentor entries wait for it (fail-closed)", mentorSeedSymbol)}
	}
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
	for i := range live {
		live[i].ClearsAtMs = live[i].ToMs + ntTrader.IntegritySeedReachDays*24*60*60_000
		parts = append(parts, fmt.Sprintf("%s (%d open min, clears by itself %s)", live[i].Window, live[i].Missing, live[i].ClearsAtLabel()))
		wins = append(wins, live[i].Window)
	}
	sym := mentorSeedSymbol
	return dataVerdict{
		Blocked: true, Class: ntTrader.IntegrityClassHole, Holes: live, Windows: wins,
		Reason: fmt.Sprintf("missing %s 1m bars %s in the series the mentor's key levels were seeded from (no contract in the stitch stores those minutes) — the key levels were walked over those candles, so mentor entries stay paused: %s",
			sym, strings.Join(parts, "; "), ntTrader.IntegrityActionSeeded),
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

// HealthIntegritySymbol is the symbol this trader's entries stand on, for the
// /api/health pause surface: a pause on a symbol NO running trader trades is not
// a pause of anything (round 4, D3). "" for a trader the gate does not apply to
// (not NinjaTrader futures, or no NT8 bridge in this process).
func (at *AutoTrader) HealthIntegritySymbol() string {
	if at == nil || !at.integrityApplies() {
		return ""
	}
	return at.futuresSymbol()
}
