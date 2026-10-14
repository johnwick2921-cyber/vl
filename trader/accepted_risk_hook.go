package trader

import (
	"strconv"
	"strings"
	"time"

	nt "vl/provider/ninjatrader"
	"vl/store"
)

// ── WAVE A / D4 — RECORDING WHAT THE BROKER ACCEPTED ─────────────────────────
//
// NT8 sends an order_update the moment it accepts an order, and the AddOn fires
// an order_snapshot on the SAME state change (VLTraderTCPClient.cs:1600), so the
// broker's own prices are in hand at exactly the instant we need them. Until
// now the acceptance event was received and dropped: onArmedOrderUpdate handled
// filled / partfilled / rejected / cancelled and had NO case for "accepted".
//
// CLASS 23 / A10 GOVERNS THIS FILE. It is telemetry. It writes one row and
// decides nothing — a panic, a stale book or a locked database WARNs and the
// trading loop continues.

// recordAcceptedRisk appends the immutable record of an accepted order. It is
// called on every live entry state, and RECORDS ONCE PER SIGNAL (N-F, release
// #22 rescan): the Accepted and the Working frame both reached here and wrote
// a pair per acceptance (rows 224/225, 226/227, 228/229). A re-authorization
// is a new signal, so it still appends ANOTHER row rather than editing this
// one, and ledger drift stays a query.
func (at *AutoTrader) recordAcceptedRisk(r store.ArmedOrderDB, u nt.OrderUpdatePayload) {
	if at == nil || at.store == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			at.logWarnf("🧾 accepted-risk recording panicked and was contained: %v", rec)
		}
	}()
	if prior, err := at.store.AcceptedRisk().ForSignal(u.SignalID); err == nil {
		for i := range prior {
			if prior[i].TraderID == at.id {
				return // already recorded on this signal's first live state
			}
		}
	}

	now := time.Now().UTC()
	row := &store.AcceptedRisk{
		TraderID: at.id, SignalID: u.SignalID, OrderName: u.OrderName,
		Symbol: u.Symbol, Account: u.Account, Side: r.Side,
		// N-F: u.Quantity is the CUMULATIVE FILLED count (e.Filled) — 0 on
		// every live state, which is why 229 of 229 rows read quantity=0. The
		// ORDER's quantity comes from the broker book below; without one, the
		// ledger's signed contracts; never the filled count.
		Quantity: ledgerContracts(r),
		// The ledger's terms AT THIS INSTANT — recorded so the divergence from
		// the broker is measurable instead of reconstructed from a log file.
		LedgerEntryPx: r.EntryPx, LedgerStopPx: r.StopPx, LedgerTargetPx: r.TargetPx,
		AcceptedAtMs: now.UnixMilli(),
		BookSource:   "none",
	}

	// THE BROKER'S OWN PRICES, from the same F12 book cutover leg 4 answers
	// from. Absent or stale leaves them NULL: an unknown accepted price is
	// never 0, and never the ledger's number wearing the broker's name.
	//
	// W117 F2 (R4) — the book is only read when it is not proven PRE-change:
	// the AddOn sends order_update THEN order_snapshot, and the ordered
	// worker that proved the snapshot never arrived stamps BookGateNotFresh.
	// A not-fresh book is suppressed entirely — never read stale.
	if u.BookGate != nt.BookGateNotFresh {
		if cache, account, _ := at.brokerBook(); cache != nil {
			if age, ok := cache.AgeAt(account, now); ok {
				row.BookAgeMs = age.Milliseconds()
			}
			if snap, ok := cache.Latest(account); ok {
				row.BookSource = "f12"
				applyBrokerTerms(row, snap.WorkingOrders(), u.SignalID)
			}
		}
	} else {
		at.logWarnf("🧾 accepted-risk: book suppressed for %s — no post-update snapshot proven (never read the pre-change book)", u.SignalID)
	}

	if err := at.store.AcceptedRisk().Append(row); err != nil {
		at.logWarnf("🧾 accepted-risk write failed (signal %s): %v", u.SignalID, err)
		return
	}
	at.logInfof("🧾 accepted risk recorded: signal=%s order=%s entry=%s stop=%s target=%s (ledger stop %.4f) · book=%s age=%dms",
		u.SignalID, u.OrderName, fmtPx(row.AcceptedEntryPx), fmtPx(row.AcceptedStopPx),
		fmtPx(row.AcceptedTargetPx), row.LedgerStopPx, row.BookSource, row.BookAgeMs)
}

// applyBrokerTerms fills the accepted prices from the broker's book. NT8 names
// the bracket children "<signal>-sl" and "<signal>-tp", so the leg each order
// IS comes from its own name rather than from a guess about its price.
func applyBrokerTerms(row *store.AcceptedRisk, orders []nt.NT8Order, signalID string) {
	if signalID == "" {
		return
	}
	for i := range orders {
		o := orders[i]
		if !strings.HasPrefix(o.Name, signalID) {
			continue
		}
		// FIX 4 (2026-09-07) — A DYING ORDER IS NOT AN ACCEPTED ONE.
		//
		// CancelPending and CancelSubmitted are non-terminal (they still count
		// as WORKING for the slot guard, correctly — a cancel in flight may
		// still fail). But they are on their way OUT, and recording their
		// prices as "what the broker accepted" writes a protection that is
		// already being withdrawn.
		//
		// accepted_risk ids 9 and 10 are exactly that: they recorded
		// stop=29554 / target=29623 at 23:37:02 from snapshot 8209/8212, where
		// the -sl read CancelPending and the -tp read CancelSubmitted. The row
		// said position 592 was protected at 29554; the book was already
		// cancelling that protection, and 8214 held nothing at all.
		// D3 (2026-09-07) — ONLY WHAT IS STANDING AT THE EXCHANGE COUNTS.
		//
		// This was `if isCancelInFlight(o.State) { continue }`: it caught the
		// two dying states and let everything else through. Two other classes
		// slipped past it.
		//
		//   TriggerPending — NT8 holds the order on THIS PC. It is not at the
		//   exchange and will not fire if the machine is down, so its price is
		//   not a protection the broker has agreed to. The state appeared
		//   nowhere in this tree before today.
		//
		//   Unreadable — a state we could not parse is not evidence. Owner
		//   ruling 2026-09-07: UNKNOWN takes no branch.
		//
		// So the test is now positive: the order must be LIVE AT THE EXCHANGE.
		// One classifier, in provider/ninjatrader/order_state.go.
		if !o.IsLiveAtExchange() {
			continue
		}
		switch {
		case strings.HasSuffix(strings.ToLower(o.Name), "-sl"):
			if o.StopPrice > 0 {
				v := o.StopPrice
				row.AcceptedStopPx = &v
			}
		case strings.HasSuffix(strings.ToLower(o.Name), "-tp"):
			if o.LimitPrice > 0 {
				v := o.LimitPrice
				row.AcceptedTargetPx = &v
			}
		default:
			// the entry itself
			if row.OrderType == "" {
				row.OrderType = o.Type
			}
			if o.Name == signalID && o.Quantity > 0 {
				row.Quantity = o.Quantity // N-F: the broker's order quantity
			}
			if p := entryPriceOf(o); p > 0 {
				v := p
				row.AcceptedEntryPx = &v
			}
		}
	}
}

// ledgerContracts is the ledger's signed contract count for an arm row (nil or
// non-positive → 0: unknown, never invented).
func ledgerContracts(r store.ArmedOrderDB) int {
	if r.Contracts != nil && *r.Contracts > 0 {
		return *r.Contracts
	}
	return 0
}

// isCancelInFlight reports whether the broker is already withdrawing this
// order. It now delegates to the one shared vocabulary rather than carrying its
// own copy of the four spellings (A24: no fixture — and no caller — holds its
// own copy of a constant).
func isCancelInFlight(state string) bool {
	return nt.ClassifyOrderState(state) == nt.LivenessDying
}

// entryPriceOf is the price the entry order rests at: a stop's trigger,
// otherwise its limit. Same rule orderPrice uses for leg 4 — one definition.
func entryPriceOf(o nt.NT8Order) float64 {
	if o.StopPrice > 0 {
		return o.StopPrice
	}
	return o.LimitPrice
}

// fmtPx renders a nullable price so a log line cannot print 0.00 for "unknown".
func fmtPx(p *float64) string {
	if p == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}
