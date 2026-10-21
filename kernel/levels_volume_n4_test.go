package kernel

import (
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vl/market"
)

// N4 (release #23) — pdVWAP and the prior-day profile read the WHOLE prior
// session. Real bars: mentor/testdata/mnq_1m_seed_state_2026-10-14.json.gz
// (MNQ 12-26 1m, 10-11 17:00 → 10-14 11:00 CT, extracted read-only). At 10-13
// 08:30 the 2,000-bar ring holds 1,070 of the prior session's (10-11 17:00 →
// 10-12 16:00) 1,378 bars. Independent oracle (stdlib Python,
// ~/deck-fix-1005/rel22/fixwave/mentor-seed-state/py/pdvwap.py, the same
// typical-price VWAP and 120-bin close profile): whole session pdVWAP
// 30999.9259, POC/VAH/VAL 30896.4823/31058.4292/30846.2229; the ring's tail
// gives 30983.7006 (Δ−16.23), −29.57 at 11:00 and −54.21 at 14:30 — exactly the
// MATH-LEVELS-TIME N4 table, so the oracle is the rescan's.

func n4Bars(t *testing.T) []market.Kline {
	t.Helper()
	f, err := os.Open(filepath.Join("mentor", "testdata", "mnq_1m_seed_state_2026-10-14.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Bars [][]float64 `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(doc.Bars))
	for _, r := range doc.Bars {
		ot := int64(r[0])
		out = append(out, market.Kline{OpenTime: ot, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5], CloseTime: ot + 59_999})
	}
	return out
}

// n4Ring is the slice the level assembly reads: the last 2,000 closed bars.
func n4Ring(all []market.Kline, now time.Time) []market.Kline {
	var cb []market.Kline
	for _, b := range all {
		if b.CloseTime < now.UnixMilli() {
			cb = append(cb, b)
		}
	}
	if len(cb) > 2000 {
		cb = cb[len(cb)-2000:]
	}
	return cb
}

func n4Kinds(levels []DetectedLevel) map[LevelKind][]float64 {
	out := map[LevelKind][]float64{}
	for _, l := range levels {
		out[l.Kind] = append(out[l.Kind], l.Price)
	}
	return out
}

func TestN4PriorSessionReadWholeFromTheStore(t *testing.T) {
	all := n4Bars(t)
	prev := PriorSessionBarsProvider
	t.Cleanup(func() { PriorSessionBarsProvider = prev })
	calls := 0
	PriorSessionBarsProvider = func(symbol string, fromMs, toMs int64) ([]market.Kline, bool) {
		calls++
		var out []market.Kline
		for _, b := range all {
			if b.OpenTime >= fromMs && b.OpenTime < toMs {
				out = append(out, b)
			}
		}
		return out, true
	}
	for _, hm := range [][2]int{{8, 30}, {11, 0}, {14, 30}} {
		now := time.Date(2026, 10, 13, hm[0], hm[1], 0, 0, CTLocation())
		got := n4Kinds(VolumeLevelsFor("MNQ-N4-STORE", n4Ring(all, now), now))
		if v := got[KindPDVWAP]; len(v) != 1 || math.Abs(v[0]-30999.9259) > 0.0001 {
			t.Fatalf("%02d:%02d pdVWAP = %v, want the whole session's [30999.9259] (the ring's tail gives 30983.70 at 08:30)", hm[0], hm[1], v)
		}
		want := map[LevelKind]float64{KindPOC: 30891.875, KindVAH: 31060, KindVAL: 30848.75}
		for k, w := range want {
			if v := got[k]; len(v) != 1 || math.Abs(v[0]-w) > 0.0001 {
				t.Fatalf("%02d:%02d prior-day %s = %v, want [%.4f] (whole session, m239 SVP 1.25-pt rows)", hm[0], hm[1], k, v, w)
			}
		}
	}
	if calls != 1 {
		t.Fatalf("the whole session must be read from the store ONCE per symbol and session, read %d times", calls)
	}
}

func TestN4FrontTruncatedPriorSessionIsRefused(t *testing.T) {
	all := n4Bars(t)
	prev := PriorSessionBarsProvider
	t.Cleanup(func() { PriorSessionBarsProvider = prev })
	PriorSessionBarsProvider = nil // no store: the ring is the only source
	now := time.Date(2026, 10, 13, 8, 30, 0, 0, CTLocation())
	before := PriorSessionReadCounts()["prior_session_refused_truncated"]
	got := n4Kinds(VolumeLevelsFor("MNQ-N4-NOSTORE", n4Ring(all, now), now))
	if len(got[KindPDVWAP]) != 0 || len(got[KindPOC]) != 0 || len(got[KindVAH]) != 0 || len(got[KindVAL]) != 0 {
		t.Fatalf("a front-truncated prior session must be refused, drew pdVWAP %v POC %v VAH %v VAL %v", got[KindPDVWAP], got[KindPOC], got[KindVAH], got[KindVAL])
	}
	if len(got[KindSETT]) != 0 {
		t.Fatalf("m239: SETT is the native 1d close; with no daily source it must be absent (never the last 1m print): %v", got[KindSETT])
	}
	if PriorSessionReadCounts()["prior_session_refused_truncated"] <= before {
		t.Fatal("the refusal must be counted")
	}
	// A WHOLE prior session in the slice (03:00, before the ring cuts it) is
	// read straight from the slice — the provider is never needed.
	early := time.Date(2026, 10, 13, 3, 0, 0, 0, CTLocation())
	if v := n4Kinds(VolumeLevelsFor("MNQ-N4-NOSTORE-EARLY", n4Ring(all, early), early))[KindPDVWAP]; len(v) != 1 || math.Abs(v[0]-30999.9259) > 0.0001 {
		t.Fatalf("03:00 pdVWAP = %v, want the whole session's [30999.9259] from the slice", v)
	}
}
