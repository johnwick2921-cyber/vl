package trader

// R22 TIME-WEEKLY — P1-A, pinned at the production call site.
//
// The live WEEKLY doc for week 2026-10-12 (plans rows trade_date=2026-10-12
// session=WEEKLY, both traders, facts_hash e89ad8bae4a8…) said
// PWH 31616.50 · PWL 30760.75 · PWC 30969.50 and "No unfilled NWOG edges or
// computed IPDA extremes". The log line at 10-11 16:30:17 read
// "17 bar(s) from nt8_agg via 1d → 17 completed week(s)": the reader was fed
// WEEKLY aggregates bucketed on Thursday 00:00 UTC (AggregateToTF floors to
// the Unix epoch, a Thursday), not daily bars.
//
// The truth, from the native 1d rows the cache serves (MNQ 12-26, rows stamped
// Sun 10-04 … Thu 10-08 = trading days Mon 10-05 … Fri 10-09):
// PWH 31616.50 · PWL 30792.00 · PWC 31108.75.
//
// This test serves the REAL persisted native 1d rows (data.db, read-only
// export) through market.FuturesBarsProvider — exactly what the BarCache hands
// the resolver — and calls the production facts path runWeeklyRead uses.

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"vl/market"
)

// loadNative1dFixture reads the exported native 1d rows: [open_ms,o,h,l,c,v].
func loadNative1dFixture(t *testing.T) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile("../kernel/testdata/mnq1226_native_1d_through_20261013.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][6]float64
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{OpenTime: int64(r[0]), CloseTime: int64(r[0]) + 86_400_000 - 1,
			Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	return out
}

// nativeDailyAt serves the rows the cache would hold at `now`: a native daily
// row exists once its session has STARTED. NT8 open-stamps a daily row at
// 00:00 CT of the calendar day before its trading day, and that session opens
// at 17:00 CT the same calendar day.
func nativeDailyAt(rows []market.Kline, now time.Time) func(symbol, tf string, count int) []market.Kline {
	return func(_, tf string, count int) []market.Kline {
		if tf != "1d" {
			return nil
		}
		var out []market.Kline
		for _, b := range rows {
			st := time.UnixMilli(b.OpenTime).In(chicagoLoc())
			sessOpen := time.Date(st.Year(), st.Month(), st.Day(), 17, 0, 0, 0, chicagoLoc())
			if !sessOpen.After(now) {
				out = append(out, b)
			}
		}
		if count > 0 && len(out) > count {
			out = out[len(out)-count:]
		}
		return out
	}
}

func withFuturesBars(t *testing.T, fn func(symbol, tf string, count int) []market.Kline) {
	t.Helper()
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = fn
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
}

func TestWeeklyReadUsesCMEWeeks_Live1012(t *testing.T) {
	now := time.Date(2026, 10, 11, 16, 30, 17, 0, chicagoLoc()) // the live read instant
	withFuturesBars(t, nativeDailyAt(loadNative1dFixture(t), now))
	at := &AutoTrader{id: "weekly-1012"}

	facts, bars, src := at.weeklyReadFacts(now)
	if len(bars) == 0 {
		t.Fatalf("no bars from the weekly source (%s)", src)
	}
	t.Logf("source=%s bars=%d refs=%+v weeks=%d nwogs=%d ipda=%+v",
		src, len(bars), facts.Refs, len(facts.Weeks), len(facts.NWOGs), facts.IPDA)

	if !facts.RefsOK {
		t.Fatal("no prior-week refs from a cache holding ~4 months of daily bars")
	}
	if facts.Refs.PWH != 31616.50 || facts.Refs.PWL != 30792.00 || facts.Refs.PWC != 31108.75 {
		t.Errorf("week 2026-10-12 refs = PWH %.2f PWL %.2f PWC %.2f; want the CME week Mon 10-05 → Fri 10-09: "+
			"PWH 31616.50 PWL 30792.00 PWC 31108.75 (live doc said PWL 30760.75 PWC 30969.50 — the Thursday-UTC bucket)",
			facts.Refs.PWH, facts.Refs.PWL, facts.Refs.PWC)
	}
	if n := len(facts.Weeks); n == 0 || facts.Weeks[n-1].WeekStart != "2026-10-05" {
		t.Errorf("latest completed week must be Monday 2026-10-05, got %+v", facts.Weeks)
	}
	for _, r := range facts.IPDA {
		if r.PosPct < 0 {
			t.Errorf("IPDA %dd is 'insufficient history' with ~85 daily bars in the cache — it was fed weekly bars", r.Days)
		}
	}
	if len(facts.NWOGs) == 0 {
		t.Error("no NWOG computed from ~17 weeks of daily bars — the gap detector never saw a Friday close or a Sunday open")
	}
}
