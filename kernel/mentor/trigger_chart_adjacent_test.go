package mentor

import (
	"testing"
	"time"

	"vl/market"
)

var chartCT = func() *time.Location {
	l, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}
	return l
}()

// gapBar builds a bucket of tfMin minutes opening at the given Chicago time.
func gapBar(tfMin int, open time.Time, high, low, close float64) market.Kline {
	ms := open.UnixMilli()
	return market.Kline{OpenTime: ms, CloseTime: ms + int64(tfMin)*60_000 - 1, High: high, Low: low, Close: close}
}

func ctTime(day, hour, min int) time.Time {
	return time.Date(2026, time.June, day, hour, min, 0, 0, chartCT)
}

func chartTrigger(t *testing.T, tf int, bars []market.Kline) TriggerLine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	switch tf {
	case 5:
		return TriggerTick(TriggerLine{}, bars, tf, cfg)
	case 60:
		return HTFAdvance(HTF{}, nil, bars, cfg).OneH
	case 240:
		return HTFAdvance(HTF{}, bars, nil, cfg).FourH
	}
	t.Fatalf("tf %d", tf)
	return TriggerLine{}
}

// The first candle after the 16:00–17:00 CT halt is compared with the candle
// before the halt (his chart's "previous candle"), not skipped for being 65
// minutes after it.
func TestTriggerComparesAcrossDailyHalt(t *testing.T) {
	bars := []market.Kline{
		gapBar(5, ctTime(10, 15, 55), 100, 96, 98),
		gapBar(5, ctTime(10, 17, 0), 103, 97, 102), // breaks 15:55's high
		gapBar(5, ctTime(10, 17, 5), 104, 101, 103),
	}
	got := chartTrigger(t, 5, bars)
	if got.Dir != SideLong || got.Price != 100 {
		t.Fatalf("17:00 candle after the halt: %+v, want long @ 100", got)
	}
}

// Friday's last 1h (and 4h) candle vs the Sunday-open candle.
func TestTriggerComparesAcrossWeekend(t *testing.T) {
	for _, tf := range []int{60, 240} {
		bars := []market.Kline{
			gapBar(tf, ctTime(5, 14, 0), 100, 96, 98), // Friday
			gapBar(tf, ctTime(7, 17, 0), 99, 94, 95),  // Sunday open breaks the low
			gapBar(tf, ctTime(7, 18, 0), 96, 93, 94),
		}
		got := chartTrigger(t, tf, bars)
		if got.Dir != SideShort || got.Price != 96 {
			t.Fatalf("tf=%d Sunday open: %+v, want short @ 96", tf, got)
		}
	}
}

// Time-adjacent buckets behave exactly as before.
func TestTriggerAdjacentBucketsUnchanged(t *testing.T) {
	bars := []market.Kline{
		gapBar(5, ctTime(10, 9, 0), 100, 96, 98),
		gapBar(5, ctTime(10, 9, 5), 103, 99, 102), // breaks high → long @ 100
		gapBar(5, ctTime(10, 9, 10), 101, 94, 95), // breaks low → reversal short @ 99
		gapBar(5, ctTime(10, 9, 15), 100, 90, 91), // same-direction re-break: line stays
	}
	got := chartTrigger(t, 5, bars)
	if got.Dir != SideShort || got.Price != 99 {
		t.Fatalf("adjacent chain: %+v, want short @ 99", got)
	}
}
