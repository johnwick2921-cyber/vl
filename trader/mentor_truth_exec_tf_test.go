package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
)

// m407: the truth payload carries the intraday trendlines of the EXECUTION
// frame (knob ON): the 2m build after 09:00, and a VALID 1m line carried across
// the 09:00 frame switch.

func m407CT(h, m int) int64 {
	return time.Date(2026, time.September, 15, h, m, 0, 0, kernel.CTLocation()).UnixMilli()
}

func m407TickAll(t *testing.T, at *AutoTrader, bars []market.Kline, from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		at.mentorEval.Tick(bars[:i], mentor.BarCloseInstant(bars[i-1]))
	}
}

// a 2m tape (the kernel return tape read as 14 2m candles, each expanded to two
// 1m bars) at 10:00 CT: the truth payload must carry the VALID 2m support line
// with its 2m-bucket anchors.
func TestMentorTruthSnapshotCarries2mTrendlineAfter0900(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	if !cfg.Exec2mAfter30m {
		t.Fatal("fixture: Exec2mAfter30m must default ON")
	}
	at.mentorEval = mentor.New(cfg)
	t0 := m407CT(10, 0)
	var bars []market.Kline
	for i, c := range mentorChartTape() {
		mid := (c.Open + c.Close) / 2
		lowA := mid
		if c.Open < lowA {
			lowA = c.Open
		}
		o := t0 + int64(i)*120_000
		bars = append(bars,
			market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: c.Open, High: c.High, Low: lowA, Close: mid},
			market.Kline{OpenTime: o + 60_000, CloseTime: o + 119_999, Open: mid, High: m407Max(mid, c.Close), Low: c.Low, Close: c.Close},
		)
	}
	m407TickAll(t, at, bars, 3, len(bars))
	now := mentor.BarCloseInstant(bars[len(bars)-1])
	p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
	if !ok || p.Trendlines == nil {
		t.Fatalf("snapshot ok=%v trendlines=%v", ok, p.Trendlines)
	}
	found := false
	for _, tl := range *p.Trendlines {
		if tl.Side == "long" && tl.P0Px == 97 && tl.P1Px == 98.2 {
			found = true
			if !tl.Valid {
				t.Errorf("the 2m support line must be valid (3rd-touch return): %+v", tl)
			}
		}
	}
	if !found {
		t.Fatalf("2m support line 97→98.2 missing from the truth payload: %+v", *p.Trendlines)
	}
}

// a valid 1m line at 08:59 stays in the payload at 09:00 and 09:10 (the 2m
// frame), and leaves once a closed 5m candle closes through it.
func TestMentorTruthSnapshotKeepsValid1mLineAcrossTheSwitch(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	at.mentorEval = mentor.New(cfg)
	t0 := m407CT(8, 46)
	var bars []market.Kline
	for i, k := range mentorChartTape() {
		k.OpenTime = t0 + int64(i)*60_000
		k.CloseTime = k.OpenTime + 59_999
		bars = append(bars, k)
	}
	for i := len(bars); i < 35; i++ {
		o := t0 + int64(i)*60_000
		k := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 106, High: 107, Low: 105, Close: 106}
		if o >= m407CT(9, 15) {
			k = market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100, High: 100.5, Low: 94.5, Close: 95}
		}
		bars = append(bars, k)
	}
	idx := func(h, m int) int { return int((m407CT(h, m)-t0)/60_000) + 1 }
	has := func(upTo int) bool {
		now := mentor.BarCloseInstant(bars[upTo-1])
		p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
		if !ok || p.Trendlines == nil {
			return false
		}
		for _, tl := range *p.Trendlines {
			if tl.Side == "long" && tl.P0Px == 97 && tl.P1Px == 98.2 && tl.Valid {
				return true
			}
		}
		return false
	}
	m407TickAll(t, at, bars, 3, idx(8, 59))
	if !has(idx(8, 59)) {
		t.Fatal("fixture: the valid 1m line must be in the payload at 08:59")
	}
	prev := idx(8, 59)
	for _, hm := range [][2]int{{9, 0}, {9, 10}} {
		m407TickAll(t, at, bars, prev+1, idx(hm[0], hm[1]))
		prev = idx(hm[0], hm[1])
		if !has(prev) {
			t.Fatalf("%02d:%02d: the valid 1m line left the truth payload at the switch", hm[0], hm[1])
		}
	}
	m407TickAll(t, at, bars, prev+1, len(bars))
	if has(len(bars)) {
		t.Fatal("09:20: the line must leave the payload after a closed 5m close through it")
	}
}

func m407Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
