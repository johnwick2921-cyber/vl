package ninjatrader

import (
	"fmt"
	"strings"
	"time"

	"vl/internal/datatruth/checks"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
)

// ── m447 (7) — the fill-price data-truth hook ────────────────────────────────
//
// Every entry and exit fill this trader records is handed to the process-wide
// checks.DefaultFills guard, which judges it against the stored 1m bars of the
// fill minute (internal/datatruth/checks/fillprice.go has the rule and the
// reasoning). The hook is one call at each place a fill price enters the bot:
//
//	entry fill frame      handleFillInbound (market / AI-path entries)
//	armed entry fill      TCPTrader.NoteFill, called by the armed executor's
//	                      materializeArmedEntry (the order_update fill)
//	exit                  recordClose / recordCloseOrdered (the position_close frame)
//
// It reads, never writes: a fill is recorded exactly as before whatever the
// guard thinks, and an exit is never delayed or refused by it.

// fillRing reads this trader's server's 1m ring in the checks' bar shape.
func (t *TCPTrader) fillRing() checks.RingFn {
	if t == nil || t.server == nil {
		return nil
	}
	cache := t.server.BarCache()
	return func(symbol string) ([]checks.Bar, error) {
		return (&ntwire.DataTruthSources{Cache: cache}).Bars(symbol, 1)
	}
}

// NoteFill hands one fill to the data-truth guard. id must be stable for a
// replay of the same frame; orderPx 0 = the order's price is not known here.
func (t *TCPTrader) NoteFill(kind, id, symbol, side string, price float64, atMs int64, orderPx float64) {
	t.noteFill(kind, id, symbol, side, price, atMs, orderPx, nil)
}

func (t *TCPTrader) noteFill(kind, id, symbol, side string, price float64, atMs int64, orderPx float64, slipTicks *float64) {
	if t == nil || price <= 0 || id == "" {
		return
	}
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		sym = t.symbol
	}
	o := checks.FillObs{ID: id, Symbol: sym, Kind: kind, Side: side, Price: price, AtMs: atMs,
		Tick: market.FuturesTickSize(sym), OrderPx: orderPx}
	if slipTicks != nil {
		o.SlipTicks, o.HasSlip = *slipTicks, true
	}
	checks.DefaultFills.Record(o, t.fillRing(), time.Now())
}

// fillTimeMs parses a frame's RFC3339 fill/exit time, falling back to now.
func fillTimeMs(rfc string) int64 {
	if rfc != "" {
		if ts, err := time.Parse(time.RFC3339, rfc); err == nil {
			return ts.UTC().UnixMilli()
		}
	}
	return time.Now().UTC().UnixMilli()
}

func entryFillID(f ntwire.FillPayload) string {
	return fmt.Sprintf("entry|%s|%s|%s|%.4f|%d|%d", f.Account, f.Symbol, f.SignalID, f.FillPrice, f.Quantity, fillTimeMs(f.FillTime))
}

func exitFillID(account, symbol, side, signal string, seq uint64, exitMs int64) string {
	return fmt.Sprintf("exit|%s|%s|%s|%s|%d|%d", account, symbol, side, signal, seq, exitMs)
}
