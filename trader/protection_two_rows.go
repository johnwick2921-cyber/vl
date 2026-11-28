package trader

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── TWO ROWS OF ONE SYMBOL AND SIDE (look-twice review, 2026-11-24) ──────────
//
// A contract month is not on the wire: short MNQ 12-26 and short MNQ 03-27 both
// arrive as "MNQ short", and a stop or close the bot sends goes to the AddOn's
// own MNQ contract. So the protection net cannot tell which month a row, a stop
// in the book or its own order belongs to. That is a LIMIT, not something to
// guess around: the owner gets one P0 a session day, and the only thing changed
// is that the stop is never larger than the bot's own open row when that row can
// be tied to one of the two.

type protRow struct {
	qty int
	avg float64
}

// protRowSum is the bot's own open position there by the ledger.
type protRowSum struct{ qty, avg float64 }

// protectionTwoRows groups the positions by symbol|side.
func protectionTwoRows(positions []map[string]interface{}) map[string][]protRow {
	out := make(map[string][]protRow)
	for _, p := range positions {
		symbol, _ := p["symbol"].(string)
		side, _ := p["side"].(string)
		qtyF, _ := p["quantity"].(float64)
		if qty := int(math.Round(qtyF)); qty > 0 && symbol != "" && side != "" {
			k := symbol + "|" + side
			out[k] = append(out[k], protRow{qty: qty, avg: positionAvg(p)})
		}
	}
	return out
}

// alertTwoRowsLocked raises the P0 for every symbol and side with more than one
// row, once per CME session day. protMu held.
func (at *AutoTrader) alertTwoRowsLocked(groups map[string][]protRow, now time.Time) {
	for key, rows := range groups {
		if len(rows) < 2 {
			continue
		}
		symbol, side, _ := strings.Cut(key, "|")
		id := fmt.Sprintf("tworows:%s:%s:%s", symbol, side, kernel.CMESessionDayKey(now))
		if at.protTwoRowsAt[key] == id {
			continue
		}
		if at.protTwoRowsAt == nil {
			at.protTwoRowsAt = make(map[string]string)
		}
		at.protTwoRowsAt[key] = id
		sort.Slice(rows, func(i, j int) bool { return rows[i].avg < rows[j].avg })
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = fmt.Sprintf("×%d @ %.2f", r.qty, r.avg)
		}
		at.emitAlert("P0", alertKindProtectionTwoRows, id,
			fmt.Sprintf("NinjaTrader shows %d separate %s %s positions (two contract months?) — the bot cannot tell them apart", len(rows), side, symbol),
			fmt.Sprintf("%s %s: %s. The contract month is not in the report, so the bot cannot tell which one a stop protects, and its own orders go to its own contract only. It never places a stop larger than its own open trade when that trade can be recognised. Check the stop of each position in NinjaTrader.",
				side, symbol, strings.Join(names, " and ")))
	}
}

// ownOpenRow sums this trader's OPEN rows there that the bot opened (a position
// adopted as manual is not ours to size by). Unreadable = none = nothing capped.
func (at *AutoTrader) ownOpenRow(account, symbol, side string) (out protRowSum) {
	if at.store == nil || at.store.Position() == nil {
		return out
	}
	rows, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		return protRowSum{}
	}
	value := 0.0
	for _, r := range rows {
		if r == nil || store.IsManualOrigin(r.Origin) || !strings.EqualFold(r.Symbol, symbol) || !strings.EqualFold(r.Side, side) ||
			(r.Account != "" && !strings.EqualFold(r.Account, account)) {
			continue
		}
		out.qty += r.Quantity
		value += r.Quantity * r.EntryPrice
	}
	if out.qty > 0 {
		out.avg = value / out.qty
	}
	return out
}

// protectionOwnStopQty is the size of the stop for a row of qty: the bot's own
// size when one of the rows IS the bot's (same quantity, average within a tick)
// and qty is larger. Anything else leaves qty alone.
func protectionOwnStopQty(qty int, rows []protRow, own, ownAvg, tick float64) int {
	n := int(math.Round(own))
	if n <= 0 || math.Abs(own-float64(n)) > 1e-9 || qty <= n {
		return qty
	}
	for _, r := range rows {
		if r.qty == n && math.Abs(r.avg-ownAvg) <= tick+1e-9 {
			return n
		}
	}
	return qty
}

func protectionTick(symbol string) float64 {
	if t := market.FuturesTickSize(symbol); t > 0 {
		return t
	}
	return 0.25
}
