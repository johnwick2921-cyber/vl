package mentor

import "vl/market"

type MidRangeReject struct {
	Side   Side   `json:"side"`
	At     int64  `json:"at"`
	BoxKey string `json:"box_key"`
}

func (e *Evaluator) advanceMidRangeReject(cur market.Kline, boxes []Box) {}
