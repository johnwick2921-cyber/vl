package mentor

import "vl/market"

// governingTrigger — STUB (RED step): always the 5m.
func governingTrigger(cfg Config, t5, t15, t30 TriggerLine, cb5, cb15 []market.Kline) (TriggerLine, int) {
	return t5, 5
}
