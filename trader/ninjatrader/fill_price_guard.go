package ninjatrader

import (
	"fmt"
	"strings"
	"time"

	"vl/internal/datatruth/checks"
	"vl/logger"
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
//	armed entry fill      the SAME fill frame: the AddOn sends an order_update AND
//	                      a fill frame for one entry fill (OnOrderUpdate), and the
//	                      fill frame carries the fill time. The armed executor does
//	                      NOT judge the order_update twin (it counted one fill twice
//	                      under a different id and stamped time.Now() on a late or
//	                      catch-up frame) - pinned by
//	                      TestArmedEntryOneFillFromBothFramesIsCountedOnce.
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
	t.noteFill(kind, id, symbol, side, price, atMs, orderPx, nil, "", "")
}

// noteFill hands one fill to the guard. account is the NT8 account it printed on
// (the strike is PER ACCOUNT) and event names the real-world fill the frame
// belongs to (the legs of one split bracket share it, so they are ONE strike);
// "" for either keeps the guard's fail-closed defaults (everyone / one event per
// id). A price <= 0 or an empty id is NOT dropped here: the guard counts a
// fill it cannot judge as a far fill and logs it (m451).
func (t *TCPTrader) noteFill(kind, id, symbol, side string, price float64, atMs int64, orderPx float64, slipTicks *float64, account, event string) {
	if t == nil {
		return
	}
	// noteFill runs on the exit-RECORDING path (recordClose, recordCloseOrdered): a
	// panic in the check (the alert callback, the ring read, the log) must never
	// cost the exit record (m447 round 3 note; the reconcile observer recovers the
	// same way).
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("fill_price: noteFill panicked (%v) - the fill is recorded, the check skipped it [fill id %s]", r, id)
		}
	}()
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		sym = t.symbol
	}
	o := checks.FillObs{ID: id, Symbol: sym, Kind: kind, Side: side, Price: price, AtMs: atMs,
		Tick: market.FuturesTickSize(sym), OrderPx: orderPx, Account: account, Event: event}
	if slipTicks != nil {
		o.SlipTicks, o.HasSlip = *slipTicks, true
	}
	checks.DefaultFills().Record(o, t.fillRing(), time.Now())
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

// entryFillID identifies one entry fill by what the broker reported (account,
// symbol, signal, price, cumulative quantity) and NEVER by the clock: a replay of
// the same frame, a late frame and a frame with no fill time all give the same
// id (m447 review defect 1 - the id used to carry fillTimeMs, which falls back to
// time.Now(), so it was not replay-stable).
func entryFillID(f ntwire.FillPayload) string {
	return fmt.Sprintf("entry|%s|%s|%s|%.4f|%d", f.Account, f.Symbol, f.SignalID, f.FillPrice, f.Quantity)
}

// exitFillID is likewise clock-free: the frame's own exit time STRING (empty
// stays empty) and its seq, never a time.Now() fallback.
func exitFillID(account, symbol, side, signal string, seq uint64, exitTime string) string {
	return fmt.Sprintf("exit|%s|%s|%s|%s|%d|%s", account, symbol, side, signal, seq, exitTime)
}

// entryFillEvent / exitFillEvent are the fill EVENT keys (m451): what makes two
// frames the same print. They carry the account, so mirrored traders on two
// accounts keep separate strikes (the strike is per account), and carry NEITHER
// the leg, the exit time nor the cumulative quantity/average price, which all
// differ between frames of one print: a split bracket sends one position_close
// frame per leg (same signal and seq, exit_time stamped a few ms apart), and an
// entry sends a fill frame per partial (cumulative average price and quantity).
//
// A frame with no signal id cannot be tied to an order, so it names no event ("")
// and the guard treats each of its ids as its own: never collapse unrelated fills.
func entryFillEvent(f ntwire.FillPayload) string {
	if strings.TrimSpace(f.SignalID) == "" {
		return ""
	}
	return fmt.Sprintf("entry|%s|%s|%s", f.Account, f.Symbol, f.SignalID)
}

func exitFillEvent(account, symbol, side, signal string, seq uint64) string {
	if strings.TrimSpace(signal) == "" {
		return ""
	}
	return fmt.Sprintf("exit|%s|%s|%s|%s|%d", account, symbol, side, signal, seq)
}
