package trader

import (
	"time"

	"vl/market"
	"vl/store"
)

// NewAutoTraderOnBrokerForTest builds a minimal AutoTrader over broker for
// ANOTHER package's call-site test (W1b FOLD-5 repair, canon 53): the agent's
// chat-door resolver reads the manager's roster of *AutoTrader through its
// production adapter (managedTradeCandidate → GetStatus / GetExchange /
// GetUnderlyingTrader), and a test there cannot build one any other way
// without NewAutoTrader's process-wide side effects (the NT8 listener, the AI
// client, the bar/level sinks). It carries no store, no AI client and no
// loop; running only sets the flag GetStatus reports. Never called in
// production.
func NewAutoTraderOnBrokerForTest(id, exchange string, broker Trader, running bool) *AutoTrader {
	at := &AutoTrader{id: id, name: id, exchange: exchange, trader: broker}
	at.config.ID, at.config.Name, at.config.Exchange = id, id, exchange
	at.isRunning = running
	return at
}

// SetDeadManBlockedForTest forces the dead-man watchdog into a blocked episode
// with the given start and failed-probe count, so another package's call-site
// test (/api/health `dead_man`) can exercise HealthDeadMan without spinning the
// real TCP link. Test-only; never called in production.
func (at *AutoTrader) SetDeadManBlockedForTest(sinceMs int64, probes int) {
	at.deadMan.mu.Lock()
	defer at.deadMan.mu.Unlock()
	at.deadMan.blockedSinceMs = sinceMs
	at.deadMan.failedProbes = probes
	at.deadMan.stuckAlerted = false
	at.deadMan.set(dmAwaitingReconcile)
}

// NoteSeedSeriesForTest puts the trader in (or out of) mentor mode and measures
// the given 1m open-time series as its seeded key-level series, so another
// package's call-site test (/api/health trading_paused.mentor_seed) can exercise
// HealthSeedSeriesPause without running a seed. Test-only; never called in
// production.
func (at *AutoTrader) NoteSeedSeriesForTest(mentorMode bool, openTimesMs []int64, now time.Time) {
	at.config.StrategyConfig = &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: mentorMode}}
	series := make([]market.Kline, 0, len(openTimesMs))
	for _, ms := range openTimesMs {
		series = append(series, market.Kline{OpenTime: ms})
	}
	at.noteSeedSeries(series, nil, now)
}
