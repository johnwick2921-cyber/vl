package trader

import (
	"sync"
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	nttrader "vl/trader/ninjatrader"
)

// x8RampBars builds n CLOSED 1m bars (5m-aligned start) rising 1 pt/min from
// 100, the last close overridden to lastClose.
func x8RampBars(n int, lastClose float64) []market.Kline {
	base := int64(120_000_000_000)
	bars := make([]market.Kline, 0, n)
	for i := 0; i < n; i++ {
		c := 100 + float64(i)
		if i == n-1 {
			c = lastClose
		}
		o := base + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: c, High: c + 1, Low: c - 1, Close: c, Final: true})
	}
	return bars
}

// x8PickClose finds a last close C with C > EMA34(tf) for every tf in above and
// C <= EMA34(tf) for every tf in below. On the rising ramp EMA1m > EMA2m >
// EMA5m, so C between two of them splits the frames.
func x8PickClose(t *testing.T, n, period int, above, below []int) float64 {
	t.Helper()
	for c := 80.0; c < 200; c += 0.25 {
		bars := x8RampBars(n, c)
		ok := true
		for _, tf := range above {
			if !(c > mentor.EMAOnTF(bars, tf, period)) {
				ok = false
			}
		}
		for _, tf := range below {
			if !(c <= mentor.EMAOnTF(bars, tf, period)) {
				ok = false
			}
		}
		if ok {
			return c
		}
	}
	t.Fatalf("no close splits frames above=%v below=%v", above, below)
	return 0
}

// TestMentorX8TrapDodgeReadsEntryFrame pins m409 on the production hook
// (mentorX8TrapDodgeExit): X8 reads the candle and EMA34 on the position's
// ENTRY frame (TrailTFMin 1/2/5; 0 = unknown -> 2m, byte-identical). The close
// is chosen so the frames' EMA34 straddle it. MUTANT: hard-code the frame back
// to 2 (x8FrameMin returns 2) -> the 1m and 5m cases go RED.
func TestMentorX8TrapDodgeReadsEntryFrame(t *testing.T) {
	const n, period = 120, 34
	cA := x8PickClose(t, n, period, []int{2, 5}, []int{1}) // 1m no-fire; 2m,5m fire
	cB := x8PickClose(t, n, period, []int{5}, []int{1, 2}) // only 5m fires
	cases := []struct {
		name  string
		frame int
		c     float64
		want  bool
	}{
		{"1m entry reads the 1m EMA34 (no exit where the 2m would)", 1, cA, false},
		{"2m entry unchanged", 2, cA, true},
		{"5m entry reads the 5m EMA34 (exit where the 2m would not)", 5, cB, true},
		{"2m entry, 5m-only close stays", 2, cB, false},
		{"missing frame falls back to the 2m", 0, cA, true},
		{"missing frame, 2m no-fire close stays", 0, cB, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)
			cfg := mentor.DefaultConfig()
			cfg.Enabled = true
			e := mentor.New(cfg)
			e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideShort, Price: 99}
			at.mentorEval = e
			p := bPos("B", "long", 100, 98, 104, 1)
			p.TrailTFMin = tc.frame
			var mu sync.Mutex
			closes := 0
			old := mentorCloseLegAtMarketWire
			mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, _ int) error {
				mu.Lock()
				closes++
				mu.Unlock()
				return nil
			}
			t.Cleanup(func() { mentorCloseLegAtMarketWire = old })
			got := at.mentorX8TrapDodgeExit(nil, p, x8RampBars(n, tc.c))
			if got != tc.want {
				t.Fatalf("frame %d close %.2f: fired=%v want %v", tc.frame, tc.c, got, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if (closes == 1) != tc.want {
				t.Fatalf("closes=%d want fired=%v", closes, tc.want)
			}
		})
	}
}
