package trader

import (
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// RED stub (m465 P4) — replaced by the implementation in the next commit.
func mentorResonanceRawISBOn(rc *store.RiskControlConfig) bool { return false }
func mentor2mReduceSizeOn(rc *store.RiskControlConfig) bool    { return false }
func mentorResonanceRawPair(bars []market.Kline, tf int) (prev, cur market.Kline, ok bool) {
	return
}
func (at *AutoTrader) mentorResonanceRawTick(nt *ntTrader.TCPTrader, p *mentorLivePos, prev, cur market.Kline) bool {
	return false
}
