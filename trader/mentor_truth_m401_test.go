package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
)

// TestM401TruthEMASeriesCarriesFrame (display contract, M401): ema_series is
// [{ms, ema9, ema34, tf_min}] and carries the EMA the engine actually used —
// tf_min 1 inside 08:30 <= t < 09:00 CT, then one CLOSED 2m bar per point
// (tf_min 2, ms = the 2m bar's open) — sorted by ms. Drives the production
// path: a real Evaluator.Tick, then MentorTruthSnapshot. MUTANT: build the
// curve on 1m only → no tf_min 2 point → RED.
func TestM401TruthEMASeriesCarriesFrame(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = true
	at.mentorEval = mentor.New(cfg)

	loc := kernel.CTLocation()
	t0 := time.Date(2026, time.October, 21, 3, 0, 0, 0, loc).UnixMilli()
	bars := make([]market.Kline, 7*60+1) // 03:00 .. 10:00 CT inclusive
	for i := range bars {
		c := 29900.0 + float64(i%11) + float64(i)/50
		bars[i] = market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_999, Open: c - 1, High: c + 2, Low: c - 2, Close: c}
	}
	now := mentor.BarCloseInstant(bars[len(bars)-1])
	at.mentorEval.Tick(bars, now)

	p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
	if !ok || p.EMASeries == nil {
		t.Fatalf("snapshot ok=%v ema_series=%v", ok, p.EMASeries)
	}
	pts := *p.EMASeries
	nine := time.Date(2026, time.October, 21, 9, 0, 0, 0, loc).UnixMilli()
	open0830 := time.Date(2026, time.October, 21, 8, 30, 0, 0, loc).UnixMilli()
	var one, two int
	var prev int64
	for _, pt := range pts {
		if pt.Ms <= prev {
			t.Fatalf("ema_series not sorted by ms: %d after %d", pt.Ms, prev)
		}
		prev = pt.Ms
		if pt.Ms >= open0830 && pt.Ms < nine {
			if pt.TFMin != 1 {
				t.Fatalf("08:30–09:00 point %+v must be tf_min 1", pt)
			}
			one++
		} else {
			if pt.TFMin != 2 {
				t.Fatalf("2m-period point %+v must be tf_min 2", pt)
			}
			two++
		}
	}
	if one == 0 || two == 0 {
		t.Fatalf("want both frames across 09:00, one=%d two=%d", one, two)
	}
	b, _ := json.Marshal(pts[len(pts)-1])
	if !strings.Contains(string(b), `"tf_min":2`) {
		t.Fatalf("wire lacks tf_min: %s", b)
	}
}
