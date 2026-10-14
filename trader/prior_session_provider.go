package trader

import (
	"sync"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// N4 (release #23) — the store side of kernel.PriorSessionBarsProvider.
//
// pdVWAP and the prior-day profile (POC/VAH/VAL) need the WHOLE prior CME
// session; the 1m ring the level assembly reads holds only its tail from ~03:20
// CT on. The store holds every NT8 1m row of the current contract (the same
// door the planner reads: imported/mixed/off-scale rows excluded), so the
// kernel asks it for the session when the slice's copy is front-truncated.
// Process-wide and installed once, like the nPOC provider: the rows are
// symbol-scoped market facts, not trader state.

var priorSessionProviderOnce sync.Once

func installPriorSessionProvider(st *store.Store) {
	priorSessionProviderOnce.Do(func() {
		kernel.PriorSessionBarsProvider = priorSessionBarsFromStore(st.BarHistory())
	})
}

// priorSessionBarsFromStore reads [fromMs, toMs) of the symbol's CURRENT
// contract, NT8 rows only, as closed 1m klines.
func priorSessionBarsFromStore(bh *store.BarHistoryStore) func(symbol string, fromMs, toMs int64) ([]market.Kline, bool) {
	return func(symbol string, fromMs, toMs int64) ([]market.Kline, bool) {
		contract, ok := bh.LatestContract(symbol)
		if !ok || contract == "" {
			return nil, false
		}
		rows, err := bh.BarsBetweenFromNT8On(symbol, "1m", contract, fromMs, toMs)
		if err != nil {
			return nil, false
		}
		return storeBarsToKlines(rows, 60_000), true
	}
}
