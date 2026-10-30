package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
	ntTrader "vl/trader/ninjatrader"
)

// ── #55 m449 — AN UNMEASURED SEED IS BLOCKED, NOT CLEAR ─────────────────────
//
// seedSeriesVerdict read only at.seedSeries.holes, so a mentor trader whose
// seeded series had never been measured (the seed not yet run, or an empty store
// read) had no holes and read CLEAR: an unknown that looked clean. It is now
// BLOCKED (class unknown, fail-closed) until a seed measures a non-empty series.
//
// MUTANTS (shown RED in the report):
//   U1  seedSeriesVerdict ignores `judged`                     -> before-seed pin RED
//   U2  noteSeedSeries marks an EMPTY series judged            -> empty-series pin RED

func TestUnmeasuredSeedSeriesBlocksMentorEntries(t *testing.T) {
	at := seedStateTrader(t)
	at.exchange = "ninjatrader"
	armIntegrityGate(t, &intFake{blockedFor: func(int) bool { return false }}) // the tracker says clear: only the seed leg is under test
	now := r4CT(time.September, 16, 10, 0)

	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, false)
	if !v.Blocked || v.Class != ntTrader.IntegrityClassUnknown || !strings.Contains(v.Reason, "has not been measured") {
		t.Fatalf("a mentor trader whose seed has not been measured must be BLOCKED (unknown), got %+v", v)
	}
	if reason, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "k"}); !refused || !strings.HasPrefix(reason, "data_integrity: ") {
		t.Fatalf("the production admit chain must refuse a mentor arm on an unmeasured seed, got refused=%v %q", refused, reason)
	}
	if got := at.HealthSeedSeriesPause(now); len(got) != 0 {
		t.Fatalf("health must not fabricate a window for an unmeasured series: %v", got)
	}

	// an EMPTY series (cold store / failed read) is still not a measurement
	at.noteSeedSeries(nil, nil, now)
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, false); !v.Blocked {
		t.Fatalf("an empty seed series must not read as a measured, complete one: %+v", v)
	}

	// a real, hole-free series measures clear
	var series []market.Kline
	for m := now.Add(-3 * time.Hour); m.Before(now); m = m.Add(time.Minute) {
		series = append(series, market.Kline{OpenTime: m.UnixMilli()})
	}
	at.noteSeedSeries(series, nil, now)
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, false); v.Blocked {
		t.Fatalf("a measured, complete series must resume: %+v", v)
	}
}
