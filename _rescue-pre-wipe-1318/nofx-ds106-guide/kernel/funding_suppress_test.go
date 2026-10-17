// B8 (audit F4): the "funding rate" prompt content is a crypto-perp concept and
// must be suppressed on CME futures even when the toggle is ON — the futures
// instrument block states there is none, so emitting it was self-contradictory.
package kernel

import (
	"strings"
	"testing"

	"vl/store"
)

func TestFundingRateSuppressedOnFutures(t *testing.T) {
	mk := func(symbol string) *StrategyEngine {
		cfg := &store.StrategyConfig{}
		cfg.CoinSource.StaticCoins = []string{symbol}
		cfg.Indicators.EnableFundingRate = true // toggle ON in both cases
		return NewStrategyEngine(cfg)
	}

	// Futures (MNQ): funding rate must NOT appear in the Available-Data list.
	fut := mk("MNQ")
	if !fut.isFuturesInstrument() {
		t.Fatal("MNQ must be recognized as a futures instrument")
	}
	var fb strings.Builder
	fut.writeAvailableIndicators(&fb)
	if strings.Contains(fb.String(), "Funding rate") {
		t.Errorf("futures: funding rate must be suppressed, got:\n%s", fb.String())
	}

	// Crypto (BTCUSDT): funding rate must REMAIN (behavior unchanged — real there).
	cry := mk("BTCUSDT")
	if cry.isFuturesInstrument() {
		t.Fatal("BTCUSDT must NOT be a futures instrument")
	}
	var cb strings.Builder
	cry.writeAvailableIndicators(&cb)
	if !strings.Contains(cb.String(), "Funding rate") {
		t.Errorf("crypto: funding rate must remain listed, got:\n%s", cb.String())
	}
}

// TestAI500OITopRemovedWithNofxOSProvider (F11a, D2-DEAD item 12): the AI500 /
// OI_Top filter tags rode the deleted legacy provider, so they must not be
// advertised anywhere — futures never had them (single static symbol), and the
// crypto branch loses them with the provider.
func TestAI500OITopRemovedWithNofxOSProvider(t *testing.T) {
	mk := func(symbol string) *StrategyEngine {
		cfg := &store.StrategyConfig{}
		cfg.CoinSource.StaticCoins = []string{symbol}
		return NewStrategyEngine(cfg)
	}

	var fb strings.Builder
	mk("MNQ").writeAvailableIndicators(&fb)
	if strings.Contains(fb.String(), "AI500 / OI_Top") {
		t.Errorf("futures: AI500/OI_Top filter tags must be suppressed, got:\n%s", fb.String())
	}

	var cb strings.Builder
	mk("BTCUSDT").writeAvailableIndicators(&cb)
	if strings.Contains(cb.String(), "AI500 / OI_Top") {
		t.Errorf("crypto: AI500/OI_Top filter tags must be absent after the provider deletion, got:\n%s", cb.String())
	}
}
