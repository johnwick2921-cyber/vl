package trader

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"vl/market"
	nt "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── OWN REVERSE (spec look-twice 2026-11-24, section 3 C) ────────────────────
//
// 2026-11-19 06:47:30: our close reached NT8 after the stop had already filled,
// the Flatten bought 1 more and opened LONG 1 with no stop. Nothing on the box
// said so: the exit receipt stayed unapplied (position_id 0), the reconcile
// called the long "manual", and it ran 3 h 06 m to -1207.00.
//
// This asks, 3 s after an exit receipt stays unapplied on the bound account:
// does NT8 now hold the OPPOSITE side, in exactly the quantity the receipts
// over-closed (Σ receipts − entry_quantity), at their price within half a tick?
// Then one P0 says it, and — only with a close of OURS on record — the row is
// kept origin=bot, so the daily-loss gate sees the money.
//
// IT NEVER SENDS AN ORDER. OWN_REVERSE=close is refused in this release: the
// send record is in memory only, and an automatic close of a position a hand
// may be working needs a durable one. "close" (and any bad value) reads alert.

const (
	ownReverseSetting = "OWN_REVERSE"
	ownReverseAlert   = "alert"
	ownReverseOff     = "off"
	ownReverseClose   = "close"

	// ownReverseGiveUp: the AddOn resends every position on its 30 s heartbeat;
	// after two of them with no usable frame nothing can be proven.
	ownReverseGiveUp = 70 * time.Second
	// ownReverseGroup: unapplied receipts this close together are one over-close
	// (10-30: two limit exits 15 ms apart, 10-16: two stops 6 ms apart).
	ownReverseGroup = 10 * time.Second
	// ownReverseSendWindow: how old a MARKET close of ours may be when the fill
	// it could explain is read. NT8 names every Flatten "Close", so only the
	// send instant separates ours from a hand's (11-19: 0.138 s; the read loop
	// can stall 5 s on the SQLite busy timeout).
	ownReverseSendWindow = 10 * time.Second
	// ownReverseEntrySlack: an entry fill of ours may print just before the exit.
	ownReverseEntrySlack = 5 * time.Second
)

// ownReverseDelay: worst false window 0.424 s + worst wire lag 1.153 s. A var only
// so the tests that do not pin the 3 s need not wait them out.
var ownReverseDelay = ownReverseDelayDefault

const ownReverseDelayDefault = 3 * time.Second

// ownReverseMode is the OWN_REVERSE setting in force: alert unless exactly off.
func ownReverseMode() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(ownReverseSetting)), ownReverseOff) {
		return ownReverseOff
	}
	return ownReverseAlert
}

// OwnReverseBootLine — READ from the setting the detector obeys (A11).
func OwnReverseBootLine() string {
	raw, set := os.LookupEnv(ownReverseSetting)
	if !set {
		raw = "unset"
	}
	mode := ownReverseMode()
	switch v := strings.ToLower(strings.TrimSpace(raw)); {
	case v == ownReverseClose:
		mode += " (close is REFUSED in this release: it never sends an order)"
	case set && v != "" && v != ownReverseAlert && v != ownReverseOff:
		mode += " (bad value)"
	}
	return fmt.Sprintf("own-reverse: %s (%s=%s)", mode, ownReverseSetting, raw)
}

// dispatchExitUnapplied is ntTrader.OnExitUnapplied: to the OWNING running trader.
func dispatchExitUnapplied(traderID string, r store.NT8ExitReceipt) {
	if at, ok := runningTrader(traderID); ok {
		at.watchOwnReverse(r)
	}
}

// watchOwnReverse starts the delayed check. It never blocks (ordered worker).
// The goroutine is joined by monitorWg: the Add is under isRunningMutex with
// isRunning true, which Stop clears there before it waits.
func (at *AutoTrader) watchOwnReverse(r store.NT8ExitReceipt) {
	if ownReverseMode() == ownReverseOff {
		return
	}
	tcp, srv := at.armedTrader(), at.protectionServer()
	if tcp == nil || srv == nil || at.store == nil || !strings.EqualFold(r.Account, tcp.BoundAccount()) {
		return
	}
	run := at.protectionRunCtx()
	if run == nil || run.Err() != nil {
		return
	}
	// The attribution is decided NOW, against the instant the read loop received
	// the fill — the worker may have waited on the database since.
	seen := srv.ReceiptNow()
	readAt := seen
	if ev := srv.ProtectionEvidenceFor(r.Account, r.Symbol); !ev.Exit.IsZero() && ev.Exit.Before(seen) {
		readAt = ev.Exit
	}
	ours := ownCloseOnRecord(srv.OwnCloses(r.Account, r.Symbol), r, readAt)

	at.isRunningMutex.Lock()
	if !at.isRunning {
		at.isRunningMutex.Unlock()
		return
	}
	at.monitorWg.Add(1)
	at.isRunningMutex.Unlock()
	telemetry.IncTraderEvent(at.id, "own_reverse_watch")
	at.goNetted("own-reverse", func() {
		defer at.monitorWg.Done()
		at.ownReverseLoop(run, srv, tcp, r, seen, ours)
	})
}

// ownCloseOnRecord returns how our own close explains receipt r, or "".
func ownCloseOnRecord(sent []nt.OwnClose, r store.NT8ExitReceipt, readAt time.Time) string {
	for i := len(sent) - 1; i >= 0; i-- {
		c := sent[i]
		if !c.Market && c.SignalID != "" && c.SignalID == r.SignalID {
			return "the bot's own limit exit " + shortID(c.SignalID)
		}
		age := readAt.Sub(c.SentAt)
		if c.Market && strings.EqualFold(r.SignalID, "Close") && age >= 0 && age <= ownReverseSendWindow {
			return fmt.Sprintf("the bot's own market close, sent %s before that fill", age.Round(time.Millisecond))
		}
	}
	return ""
}

func (at *AutoTrader) ownReverseLoop(run context.Context, srv *nt.TCPServer, tcp *ntTrader.TCPTrader, r store.NT8ExitReceipt, seen time.Time, ours string) {
	for {
		timer := time.NewTimer(ownReverseDelay)
		select {
		case <-timer.C:
		case <-run.Done():
			timer.Stop()
			return
		}
		done, why := at.ownReverseCheck(srv, tcp, r, seen, ours)
		if done {
			if why != "" {
				at.logInfof("🔁 own-reverse check: exit receipt %s (%s %s ×%.0f @ %.2f) is not a proven reverse — %s",
					shortID(r.SignalID), r.Side, r.Symbol, r.Quantity, r.Price, why)
			}
			return
		}
		if waited := srv.ReceiptNow().Sub(seen); waited > ownReverseGiveUp {
			telemetry.IncTraderEvent(at.id, "own_reverse_unresolved")
			at.logWarnf("🔁 own-reverse check: exit receipt %s (%s %s ×%.0f @ %.2f) stayed unapplied and nothing could be proven after %s — %s",
				shortID(r.SignalID), r.Side, r.Symbol, r.Quantity, r.Price, waited.Round(time.Second), why)
			return
		}
	}
}

// ownReverseCheck is one look. done=false: the evidence is not readable yet.
func (at *AutoTrader) ownReverseCheck(srv *nt.TCPServer, tcp *ntTrader.TCPTrader, r store.NT8ExitReceipt, seen time.Time, ours string) (done bool, why string) {
	if ownReverseMode() == ownReverseOff {
		return true, ""
	}
	cur, err := at.store.Position().ExitReceipt(r.ID)
	switch {
	case err != nil:
		return false, "the exit receipt could not be read: " + err.Error()
	case cur == nil:
		return true, "the receipt is not stored"
	case cur.Applied:
		return true, "" // it only beat its row
	}
	ev := srv.ProtectionEvidenceFor(r.Account, r.Symbol)
	if !ev.Known || !ev.OnLink || !ev.PositionsAfterFills() {
		return false, "no positions frame newer than the fill has been received"
	}
	side := "LONG"
	if strings.EqualFold(r.Side, "LONG") {
		side = "SHORT"
	}
	var held *nt.OpenPosition
	for i := range ev.Snapshot {
		if p := &ev.Snapshot[i]; instrumentRoot(p.Symbol) == instrumentRoot(r.Symbol) && strings.EqualFold(p.Side, side) && p.Quantity > 0 {
			held = p
		}
	}
	if held == nil {
		return true, "NinjaTrader holds no " + side
	}
	oc, err := at.store.Position().OverCloseFor(at.id, *cur, ownReverseGroup.Milliseconds())
	if err != nil {
		return false, "the ledger could not be read: " + err.Error()
	}
	if oc.Row == nil {
		return true, "no row of this trader to count from"
	}
	if oc.Excess <= 0 || math.Abs(oc.Excess-float64(held.Quantity)) > 1e-9 {
		return true, fmt.Sprintf("receipts %.0f + %.0f unapplied − entry %.0f = %.0f, but NinjaTrader holds %s ×%d",
			oc.Applied, oc.Pending, oc.Entry, oc.Excess, side, held.Quantity)
	}
	tick := market.FuturesTickSize(r.Symbol)
	if tick <= 0 {
		return true, "no tick size for " + r.Symbol
	}
	if math.Abs(held.AvgPrice-oc.AvgPx) > tick/2 {
		return true, fmt.Sprintf("NinjaTrader's average %.4f is not the exit price %.4f", held.AvgPrice, oc.AvgPx)
	}
	if what, ok := tcp.EntryFillSince(r.Symbol, side, r.ExitMs-ownReverseEntrySlack.Milliseconds()); ok {
		return true, "a normal entry of the bot explains it: " + what
	}
	if row, rerr := at.store.Position().GetOpenPositionByAccountSymbol(r.Account, r.Symbol, side); rerr != nil {
		return false, "the open row could not be read: " + rerr.Error()
	} else if row != nil && row.EntryOrderID != "" {
		return true, fmt.Sprintf("row %d there has an entry order of its own", row.ID)
	}
	at.flagOwnReverse(srv, tcp, r, ev, oc, side, held, seen, ours)
	return true, ""
}

// flagOwnReverse raises the one P0 and, with a send of ours on record, keeps
// the row in the bot record. It sends nothing.
func (at *AutoTrader) flagOwnReverse(srv *nt.TCPServer, tcp *ntTrader.TCPTrader, r store.NT8ExitReceipt, ev nt.ProtectionEvidence, oc store.OverClose, side string, held *nt.OpenPosition, seen time.Time, ours string) {
	telemetry.IncTraderEvent(at.id, "own_reverse_flagged")
	stop := at.ownReverseStopText(r.Symbol, side, srv.ReceiptNow())
	who, cause := "An exit filled more contracts than were open and opened a reverse", "No close order of the bot is on record for that fill."
	if ours != "" {
		who, cause = "The bot's close opened a reverse", "That fill is "+ours+"."
	}
	at.emitAlert("P0", alertKindOwnReverse,
		fmt.Sprintf("own-reverse:%s:%s:%s:%.0f", r.Symbol, side, oc.FirstID, oc.Excess),
		fmt.Sprintf("%s, %s — check NinjaTrader now", who, stop),
		fmt.Sprintf("NinjaTrader holds %s %s ×%d @ %.2f. Row %d was %s ×%.0f, but its exits filled %.0f. %s Nothing was sent to close it: close it, or protect it, by hand.",
			side, r.Symbol, held.Quantity, held.AvgPrice, oc.Row.ID, strings.ToUpper(oc.Row.Side), oc.Entry, oc.Applied+oc.Pending, cause))
	at.logErrorf("🔁 OWN REVERSE: NT8 holds %s %s ×%d @ %.2f after exits of row %d filled %.0f of %.0f (receipt %s unapplied, exit price %.2f); %s; %s; positions frame %s after the receipt",
		side, r.Symbol, held.Quantity, held.AvgPrice, oc.Row.ID, oc.Applied+oc.Pending, oc.Entry, shortID(r.ID), oc.AvgPx, stop, cause,
		ev.Positions.Sub(seen).Round(time.Millisecond))
	if ours == "" {
		return // never call a fill ours without the send
	}
	if _, err := tcp.NoteOwnReverse(at.store, at.id, ntTrader.OwnReverse{Symbol: r.Symbol, Side: side, Qty: held.Quantity,
		Avg: held.AvgPrice, SeenMs: seen.UnixMilli(),
		Why: fmt.Sprintf("our own close opened it (row %d over-closed by %.0f; %s)", oc.Row.ID, oc.Excess, ours)}); err != nil {
		telemetry.IncTraderEvent(at.id, "own_reverse_tag_failed")
		at.logErrorf("🔁 OWN REVERSE: the row of %s %s could not be kept origin=bot: %v — it may read manual and stay outside the daily-loss gate", side, r.Symbol, err)
	}
}

// ownReverseStopText says what the newest book shows — never "no stop" for a
// book that cannot be read.
func (at *AutoTrader) ownReverseStopText(symbol, side string, now time.Time) string {
	book, have, age, _, _, _ := at.protectionBook(now)
	if !have || (snapshotMaxAge() > 0 && age > snapshotMaxAge()) {
		return "its stop is UNKNOWN"
	}
	for _, o := range book {
		if o.IsWorking() && instrumentRoot(o.Symbol) == instrumentRoot(symbol) && isProtectiveStopFor(o, side) {
			return "a stop order is on the book"
		}
	}
	return "no stop"
}
