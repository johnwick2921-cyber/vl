package mentor

// CTO read-only probe (never committed): run the production TrendlinesBuild
// over a real day's bars at every closed 1m bar and record each distinct line.
import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"vl/market"
)

func TestCTOTrendlineProbe(t *testing.T) {
	in := os.Getenv("TL_BARS")
	out := os.Getenv("TL_OUT")
	if in == "" || out == "" {
		t.Skip("probe only")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ T int64; O, H, L, C, V float64 }
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	bars := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		bars = append(bars, market.Kline{OpenTime: r.T, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V, CloseTime: r.T + 59_999})
	}
	type rec struct {
		Key                    string
		Side                   string
		P0T, P1T               int64
		P0, P1                 float64
		FirstSeenT             int64
		ValidT                 int64 // open time of the 3rd-touch bar (0 = never)
		ValidBarsAfterP1       int
		DeadT                  int64
		SlopePtsPerMin         float64
	}
	seen := map[string]*rec{}
	order := []string{}
	for i := 10; i < len(bars); i++ {
		now := time.UnixMilli(bars[i].CloseTime + 1)
		for _, tl := range TrendlinesBuild(bars[:i+1], now) {
			k := tl.key()
			r, ok := seen[k]
			if !ok {
				r = &rec{Key: k, Side: string(tl.Side), P0T: tl.P0T, P1T: tl.P1T, P0: tl.P0Px, P1: tl.P1Px, FirstSeenT: bars[i].OpenTime,
					SlopePtsPerMin: (tl.P1Px - tl.P0Px) / (float64(tl.P1T-tl.P0T) / 60000)}
				seen[k] = r
				order = append(order, k)
			}
			if tl.ValidAt > 0 && r.ValidT == 0 {
				r.ValidT = bars[tl.ValidAt].OpenTime
				r.ValidBarsAfterP1 = tl.ValidAt - tl.P1Idx
			}
			if tl.Dead && r.DeadT == 0 {
				r.DeadT = bars[i].OpenTime
			}
		}
	}
	res := []rec{}
	for _, k := range order {
		res = append(res, *seen[k])
	}
	b, _ := json.MarshalIndent(res, "", " ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("distinct lines: %d", len(res))
}
