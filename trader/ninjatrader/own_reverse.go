package ninjatrader

import (
	"fmt"
	"math"
	"strings"
	"time"

	"vl/logger"
	"vl/market"
	"vl/store"
)

// ── OWN REVERSE (look-twice, spec 2026-11-24 section 3 C) — the ledger half ──
//
// 2026-11-19: our close opened LONG 1 (row 1194, -1207.00). The reconcile's
// untracked branch found no entry of ours behind it and flagged it manual, so
// the daily-loss gate and the bot record never saw the loss. The detector
// (trader/own_reverse.go) proves it was our close and says so here; the row the
// reconcile materializes 60 s later then stays origin=bot.

// OnExitUnapplied is the package-level hook fired when a broker exit receipt
// stayed UNAPPLIED (no row to close). It must not block: it runs on the ordered
// execution worker. Nil = no-op.
var OnExitUnapplied func(traderID string, r store.NT8ExitReceipt)

func notifyExitUnapplied(traderID string, r store.NT8ExitReceipt) {
	if OnExitUnapplied != nil {
		OnExitUnapplied(traderID, r)
	}
}

// OwnReverse is one position our own close opened.
type OwnReverse struct {
	Symbol string
	Side   string // LONG | SHORT — the side NT8 holds now
	Qty    int
	Avg    float64
	SeenMs int64 // ReceiptNow when the unapplied exit receipt arrived
	Why    string
}

// ownReverseKeep bounds how long a proven reverse waits for its row; the
// materialization needs one 20 s pass plus the 60 s grace.
const ownReverseKeep = 10 * time.Minute

// ownReverseFirstSeen: the reconcile's first sighting of THIS position is at
// most this long after the receipt. A later first sighting means the account
// was flat in between, so that position is somebody else's.
const ownReverseFirstSeen = 2 * time.Minute

// ownReverseClockSlack: both instants come from ReceiptNow, but the reconcile
// may have sighted the position just before the receipt was processed.
const ownReverseClockSlack = 10 * time.Second

func (r OwnReverse) is(sym, side string, qty, avg float64) bool {
	tick := market.FuturesTickSize(sym)
	if tick <= 0 {
		tick = 0.25
	}
	return strings.EqualFold(r.Symbol, sym) && strings.EqualFold(r.Side, side) &&
		math.Abs(qty-float64(r.Qty)) < 1e-9 && math.Abs(avg-r.Avg) <= tick/2
}

// NoteOwnReverse records a proven reverse and un-flags its row if the reconcile
// has already materialized it as manual. It returns the row it corrected (0 =
// no row yet; the reconcile will keep it bot).
func (t *TCPTrader) NoteOwnReverse(st *store.Store, traderID string, r OwnReverse) (int64, error) {
	t.ownReverseMu.Lock()
	defer t.ownReverseMu.Unlock()
	known := false
	for _, have := range t.ownReverses {
		known = known || have.is(r.Symbol, r.Side, float64(r.Qty), r.Avg)
	}
	if !known { // two receipts of one reverse prove it twice
		t.ownReverses = append(t.ownReverses, r)
	}
	if st == nil || st.Position() == nil {
		return 0, nil
	}
	row, err := st.Position().GetOpenPositionByAccountSymbol(t.boundAccount, r.Symbol, r.Side)
	if err != nil || row == nil {
		return 0, err
	}
	if row.TraderID != traderID || !r.is(row.Symbol, row.Side, row.Quantity, row.EntryPrice) ||
		row.EntryTime < r.SeenMs-ownReverseClockSlack.Milliseconds() {
		return 0, nil
	}
	ok, err := st.Position().MarkOwnReverseBot(row.ID, traderID)
	if err != nil || !ok {
		return 0, err
	}
	var left []OwnReverse // this proof has its row now
	for _, have := range t.ownReverses {
		if !have.is(r.Symbol, r.Side, float64(r.Qty), r.Avg) {
			left = append(left, have)
		}
	}
	t.ownReverses = left
	logger.Warnf("🧾 origin: pos %d (%s %s qty=%.0f) was flagged manual but %s — corrected to origin=bot", row.ID, row.Symbol, row.Side, row.Quantity, r.Why)
	return row.ID, nil
}

// flagManualUnlessOwnReverse is the last step of the untracked branch: flag the
// row manual, unless the detector proved our own close opened it. One mutex
// covers the question and the write, so a proof that lands in between is never
// lost. why != "" = kept bot.
func (t *TCPTrader) flagManualUnlessOwnReverse(st *store.Store, traderID string, rowID int64, sym, side string, qty, avg float64, firstSeenMs int64) (why string) {
	t.ownReverseMu.Lock()
	defer t.ownReverseMu.Unlock()
	var kept []OwnReverse
	for _, r := range t.ownReverses {
		age := firstSeenMs - r.SeenMs
		if why == "" && r.is(sym, side, qty, avg) &&
			age >= -ownReverseClockSlack.Milliseconds() && age <= ownReverseFirstSeen.Milliseconds() {
			why = r.Why
			continue // consumed: one proof, one row
		}
		if age <= ownReverseKeep.Milliseconds() {
			kept = append(kept, r)
		}
	}
	t.ownReverses = kept
	if why == "" {
		flagAdoptedManual(st, traderID, rowID, sym, side, qty)
		return ""
	}
	keepAdoptedBot(rowID, sym, side, qty, why)
	return why
}

// EntryFillSince reports an ENTRY fill of ours on side at or after sinceMs (the
// AddOn's clock on both sides): then a normal entry explains the position. The
// AddOn sends fill frames for entry legs only.
func (t *TCPTrader) EntryFillSince(symbol, side string, sinceMs int64) (string, bool) {
	root := fillRoot(symbol)
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.recentFills) - 1; i >= 0; i-- {
		f := t.recentFills[i]
		if f.TimeMs < sinceMs || !strings.EqualFold(f.Side, side) {
			continue
		}
		if f.Symbol != "" && fillRoot(f.Symbol) != root {
			continue
		}
		if f.Account != "" && t.boundAccount != "" && !strings.EqualFold(f.Account, t.boundAccount) {
			continue
		}
		return fmt.Sprintf("entry fill %s @ %.2f", f.SignalID, f.Price), true
	}
	return "", false
}
