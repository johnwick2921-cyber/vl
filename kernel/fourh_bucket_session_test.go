package kernel

import (
	"testing"
	"time"

	"vl/market"
)

func sessionBarAt(openMs int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, Open: o, High: h, Low: l, Close: c, Volume: 1}
}

// TestAggregateSessionAnchored4HUses1700CTAnchor pins #42 (m220): the chart's
// coarse-TF backfill must bucket on the 17:00 CT anchor, NOT the Unix epoch.
// The native 4h ring opens at 22/02/06/10/14 UTC (17/21/01/05/09/13 CT in CDT);
// an epoch-floor would emit 00/04/08/12/16/20 UTC and overlap the first native
// bar at the seam.
func TestAggregateSessionAnchored4HUses1700CTAnchor(t *testing.T) {
	// 1h bars at 06:00, 07:00, 08:00, 09:00 UTC on 2026-10-20 (= 01:00..04:00 CDT).
	// Epoch-floor 4h buckets: 04:00 and 08:00 UTC. 17:00-CT-anchored 4h buckets:
	// 01:00 CDT (= 06:00 UTC) — the 01:00..04:00 CDT bars all fall in the
	// 01:00-05:00 CDT bucket, so a single bucket opens at 01:00 CDT = 06:00 UTC.
	start := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC)
	var bars []market.Kline
	for i := 0; i < 4; i++ {
		bars = append(bars, sessionBarAt(start.Add(time.Duration(i)*time.Hour).UnixMilli(), 100, 101, 99, 100.5))
	}
	got := AggregateSessionAnchored(bars, "4h")
	if len(got) != 1 {
		t.Fatalf("got %d buckets, want 1 (all four hours share one 17:00-CT-anchored 4h bucket)", len(got))
	}
	// 01:00 CDT on 2026-10-20 = 06:00 UTC.
	want := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC).UnixMilli()
	if got[0].OpenTime != want {
		t.Fatalf("bucket open = %s, want 06:00 UTC (01:00 CDT)", time.UnixMilli(got[0].OpenTime).UTC())
	}
	if got[0].High != 101 || got[0].Low != 99 {
		t.Fatalf("OHLC aggregation wrong: %+v", got[0])
	}
}

// TestAggregateSessionAnchored1DUsesCTMidnight pins the 1d half of #42: the
// native 1d ring is open-stamped (D-1) 00:00 CT, so a derived 1d bucket must
// floor to CT midnight — not the epoch and not 17:00 CT.
func TestAggregateSessionAnchored1DUsesCTMidnight(t *testing.T) {
	// 1h bars across 2026-10-20 00:00 .. 23:00 CT. In CDT (UTC-5) these are
	// 05:00 UTC .. 04:00 UTC next day. The CT-midnight bucket is 00:00 CT.
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, CTLocation())
	var bars []market.Kline
	for i := 0; i < 24; i++ {
		bars = append(bars, sessionBarAt(start.Add(time.Duration(i)*time.Hour).UnixMilli(), 100, 101, 99, 100.5))
	}
	got := AggregateSessionAnchored(bars, "1d")
	if len(got) != 1 {
		t.Fatalf("got %d buckets, want 1 (one CT-midnight-to-midnight day)", len(got))
	}
	if got[0].OpenTime != start.UnixMilli() {
		t.Fatalf("1d bucket open = %s, want 00:00 CT", time.UnixMilli(got[0].OpenTime).In(CTLocation()))
	}
}
