package mentor

import (
	"testing"

	"vl/market"
)

func tfBar(tfMin, idx int, high, low, close float64) market.Kline {
	ms := int64(idx+1000) * int64(tfMin) * 60_000
	return market.Kline{OpenTime: ms, CloseTime: ms + int64(tfMin)*60_000 - 1, High: high, Low: low, Close: close}
}

// An outside bar (breaks BOTH extremes) that closes INSIDE the prior range is
// beyond neither extreme: it moves nothing; the next bar decides (CTO parity
// ruling 2026-10-04 Q2).
func TestOutsideBarInsideCloseMovesNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	for _, tf := range []int{5, 60, 240} {
		prior := tfBar(tf, 0, 100, 96, 98)
		outsideInside := tfBar(tf, 1, 103, 94, 98)
		run := func(start TriggerLine) TriggerLine {
			bars := []market.Kline{prior, outsideInside}
			if tf == 5 {
				return TriggerTick(start, bars, tf, cfg)
			}
			h := HTF{}
			if tf == 240 {
				h.FourH = start
				return HTFAdvance(h, bars, nil, cfg).FourH
			}
			h.OneH = start
			return HTFAdvance(h, nil, bars, cfg).OneH
		}
		for _, start := range []TriggerLine{{}, {Dir: SideLong, Price: 90}, {Dir: SideShort, Price: 110}} {
			got := run(start)
			if got.Dir != start.Dir || got.Price != start.Price {
				t.Fatalf("tf=%d start=%+v: inside-close outside bar moved the line to %+v", tf, start, got)
			}
		}
	}
}

// The beyond-close cases still pick the side price is NOW beyond.
func TestOutsideBarBeyondCloseStillPicksSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	up := TriggerTick(TriggerLine{}, []market.Kline{tfBar(5, 0, 100, 96, 98), tfBar(5, 1, 103, 94, 101)}, 5, cfg)
	if up.Dir != SideLong || up.Price != 100 {
		t.Fatalf("close above prior high: %+v, want long @ 100", up)
	}
	dn := TriggerTick(TriggerLine{}, []market.Kline{tfBar(5, 0, 100, 96, 98), tfBar(5, 1, 103, 94, 95)}, 5, cfg)
	if dn.Dir != SideShort || dn.Price != 96 {
		t.Fatalf("close below prior low: %+v, want short @ 96", dn)
	}
}
