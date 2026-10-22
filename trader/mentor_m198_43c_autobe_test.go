package trader

import (
	"errors"
	"testing"

	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── m198 #43-C pin: auto-breakeven's through-market close FAILURE must reset
// breakevenDone=false so BE is retried next cycle (the sibling moveStopWire
// failure branch already does this). Mutant: leave breakevenDone=true on error
// → this test goes RED.

func TestM198AutoBERetryAfterCloseFailure(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	on := true
	at := &AutoTrader{
		id:       "t1",
		exchange: "ninjatrader",
		trader:   &nttrader.TCPTrader{},
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{
			BreakevenEnabled:       &on,
			BreakevenTriggerPoints: 1,
		}}},
	}
	oldPrice := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return 9.5, true } // below entry 10 → through the market
	t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })
	oldClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error {
		return errors.New("no client")
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })

	at.maybeMoveStopToBreakeven("MNQ", "long", 10, 11) // +1pt ≥ 1pt trigger → the through-market close branch

	key := "MNQ_long"
	at.breakevenMu.Lock()
	done := at.breakevenDone[key]
	at.breakevenMu.Unlock()
	if done {
		t.Fatalf("after a FAILED through-market close, breakevenDone must be false (retry next cycle), got true")
	}
}
