package mentor

import "vl/market"

type SmallRange struct {
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
	At int64   `json:"at"`
}

func smallRangeWindow(bars []market.Kline) (lo, hi float64, ok bool) { return 0, 0, false }

func (e *Evaluator) advanceSmallRange(bars []market.Kline) {}
