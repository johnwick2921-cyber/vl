package mentor

import "vl/market"

type BreakLock struct {
	Side Side    `json:"side"`
	Lo   float64 `json:"lo"`
	Hi   float64 `json:"hi"`
	At   int64   `json:"at"`
	Src  string  `json:"src"`
	Day  string  `json:"day"`
}

type breakEvent struct {
	Side   Side
	Lo, Hi float64
	Src    string
}

func boxBreakEvents(bars []market.Kline, boxes []Box, baseCfg BoxCfg, cur market.Kline) []breakEvent {
	return nil
}

func (e *Evaluator) advanceBreakLock(cur market.Kline, now int64, events []breakEvent) {}

func (e *Evaluator) courseRangeFilter(out []Intent, boxes []Box) []Intent { return out }
