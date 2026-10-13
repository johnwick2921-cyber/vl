package ninjatrader

import (
	"errors"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// W117 slice A (F2) — the owning trader's durable execution consumers ride the
// server's per-(symbol,account) ordered worker, in TCP receive order. The
// worker calls exactly these three closures; the legacy advisory consumers
// skip frames the worker owns (OrderedOwned), so nothing is double-applied.
//
// Lifecycle (R5): InstallOrderedExecutions is called from AutoTrader.Run —
// never from NewTCPTrader — and the returned closure MUST be called on Stop.
// A second LIVE registration for the same (symbol, account) is refused loudly
// by the server (two traders on one account would double-apply the same
// fills); the refusal is an error here, not a silent eviction.

// InstallOrderedExecutions wires this trader's durable consumers into the
// server's ordered-execution worker for its (symbol, account) owner. The
// returned func unregisters them (idempotent; call on Stop).
func (t *TCPTrader) InstallOrderedExecutions(traderID, exchangeID, exchangeType string, st *store.Store, orderFn func(ntwire.OrderUpdatePayload)) (func(), error) {
	if t == nil || t.server == nil {
		return nil, errors.New("install ordered executions: nil trader or server")
	}
	if st == nil {
		return nil, errors.New("install ordered executions: nil store")
	}
	// The durable close consumer is NOT the legacy recordClose the advisory
	// close-sync path calls — it is recordCloseOrdered → store.ApplyNT8Exit,
	// with different semantics: one transaction reduces the exact owned
	// residual, writes the deduped exit fill, flips the receipt, and parks the
	// receipt (RETAINED as pending) plus the broker price (putPricedClose) when
	// the row is missing or incomplete — instead of legacy's ProcessTrade
	// attribution. Both are one funnel per path, but they are different funnels.
	unreg, err := t.server.RegisterOrderedExecutionsFor(t.symbol, t.boundAccount, ntwire.OrderedExecutionHandlers{
		TraderID: traderID,
		Order:    orderFn,
		Fill:     t.handleFillInbound,
		Close: func(p ntwire.PositionClosePayload) {
			// R6 — durable apply-or-park ON the worker: the exit is retained
			// when its cumulative entry update has not landed yet.
			t.recordCloseOrdered(traderID, exchangeID, exchangeType, st, p)
		},
	})
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.traderID = traderID
	t.st = st
	t.mu.Unlock()
	return unreg, nil
}

// OrderedExecutionOwnerID reports the trader id that CURRENTLY owns the
// ordered-execution worker for this trader's (symbol, account), or
// ("", false) when the slot is free/unknown. REL18-C O2 names the conflicting
// trader when a duplicate registration is refused.
func (t *TCPTrader) OrderedExecutionOwnerID() (string, bool) {
	if t == nil || t.server == nil {
		return "", false
	}
	return t.server.OrderedOwnerTraderID(t.symbol, t.boundAccount)
}
