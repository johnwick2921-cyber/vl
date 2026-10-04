package trader

import (
	"fmt"
	"sort"
	"strings"
	"time"

	nt "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (b) — the entry latch's production evidence ────────────
//
// The latch lives inside TCPTrader (trader/ninjatrader) and cannot import this
// package, where the ≤60 s book law (snapshotMaxAge), the one working-entry
// classifier (adjudicateAccountContract / isBracketChild) and both ledgers
// live. wireNT8EntryLatch hands it both, built from those SAME definitions —
// never a second copy (A11/A24). Scope is the trader's bound ACCOUNT and its
// WIRE symbol, so two symbols on one account (P5) do not block each other.

// wireNT8EntryLatch installs the latch's evidence on one NT8 TCP trader. It is
// the ONLY production caller of SetEntryLatchSource and NewAutoTrader calls it
// unconditionally for every *TCPTrader (both pinned). Unwired, the latch
// allows — the 🚦 boot line READS which it is.
func wireNT8EntryLatch(at *AutoTrader, nt *ntTrader.TCPTrader) {
	nt.SetEntryLatchSource(&ntTrader.EntryLatchSource{
		Book:    at.entryLatchBook,
		Ledgers: at.entryLatchLedgers,
		Now:     time.Now,
	})
}

// entryLatchBootLine READS the wiring (L7): latch=wired|UNWIRED, or n/a for a
// non-NT8 trader.
func entryLatchBootLine(at *AutoTrader) string {
	n, ok := at.trader.(*ntTrader.TCPTrader)
	if !ok || n == nil {
		return "entry latch: latch=n/a (not an NT8 TCP trader)"
	}
	state := "UNWIRED"
	if n.EntryLatchWired() {
		state = "wired"
	}
	return fmt.Sprintf("entry latch: latch=%s key=%s|%s book≤%s recent=%s",
		state, n.BoundAccount(), n.WireSymbol(), snapshotMaxAge().Round(time.Second), ntTrader.EntryLatchRecentWindow)
}

// instrumentRoot is the root of an NT8 instrument name ("MNQ 12-26" → "MNQ").
func instrumentRoot(s string) string {
	f := strings.Fields(strings.ToUpper(strings.TrimSpace(s)))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// latchScope is this trader's latch key: its bound account and wire symbol.
func (at *AutoTrader) latchScope() (account, symbol string) {
	if n, ok := at.trader.(*ntTrader.TCPTrader); ok && n != nil {
		return n.BoundAccount(), n.WireSymbol()
	}
	return "", at.futuresSymbol()
}

// entryLatchBook adjudicates the account's fresh book and positions for THIS
// trader's symbol through the one classifier the armed path uses. A stale or
// absent book, or unreadable positions, is UNVERIFIABLE — a refusal. siblings
// (DS-103 split legs) exempts those signal ids from the working-entry count.
func (at *AutoTrader) entryLatchBook(now time.Time, siblings map[string]bool) ntTrader.EntryLatchBookVerdict {
	_, sym := at.latchScope()
	root := instrumentRoot(sym)
	book, have, age := at.liveBook(now)
	var mine []nt.NT8Order
	for _, o := range book {
		// An order with no symbol (an older AddOn) cannot be proven to be on
		// another instrument — it counts.
		if strings.TrimSpace(o.Symbol) == "" || instrumentRoot(o.Symbol) == root {
			mine = append(mine, o)
		}
	}
	positions := 0
	ps, err := at.trader.GetPositions()
	if err != nil {
		return ntTrader.EntryLatchBookVerdict{Verifiable: false, Detail: "positions unreadable: " + err.Error()}
	}
	for _, p := range ps {
		ps, _ := p["symbol"].(string)
		if instrumentRoot(ps) != root {
			continue
		}
		if amt, _ := p["positionAmt"].(float64); amt != 0 {
			positions++
		}
	}
	v := adjudicateAccountContractExempt(mine, have, age, snapshotMaxAge(), 0, positions, siblings)
	switch v.Action {
	case contractUnverifiable:
		return ntTrader.EntryLatchBookVerdict{Verifiable: false, Detail: v.Why}
	case contractLive:
		return ntTrader.EntryLatchBookVerdict{Verifiable: true, Live: true,
			Detail: fmt.Sprintf("%d working entry order(s) (first %s %q) and %d position(s) on %s; book age %s",
				v.WorkingEntries, v.FirstOrderID, v.FirstName, positions, root, age.Round(time.Second))}
	}
	return ntTrader.EntryLatchBookVerdict{Verifiable: true}
}

// traderScope resolves the account and wire symbol a ledger row's trader sends
// on: this trader, then a running trader in the process registry, then the
// store's account binding (symbol unknown → treated as this symbol). ok=false:
// nothing binds it — the caller counts the row (fail-closed).
func (at *AutoTrader) traderScope(traderID string) (account, symbol string, ok bool) {
	if traderID == at.id {
		a, s := at.latchScope()
		return a, s, true
	}
	if other, found := runningTrader(traderID); found {
		a, s := other.latchScope()
		return a, s, true
	}
	if v, found := pictureHtfTraders.Load(traderID); found {
		if other, _ := v.(*AutoTrader); other != nil {
			a, s := other.latchScope()
			return a, s, true
		}
	}
	if at.store != nil {
		if tr, err := at.store.Trader().Get(traderID); err == nil && tr != nil && strings.TrimSpace(tr.Account) != "" {
			_, s := at.latchScope()
			return tr.Account, s, true
		}
	}
	return "", "", false
}

// entryLatchLedgers lists the PLACED non-terminal ledger rows on this trader's
// account and symbol: a non-terminal armed row carrying a broker signal
// (place_pending / working / cancel_pending), a Picture row working or stamped
// (a send was started). An unplaced arm and an unstamped Picture claim are not placed. A
// read error is returned — the latch refuses on it.
func (at *AutoTrader) entryLatchLedgers(siblings map[string]bool) ([]string, error) {
	if at.store == nil {
		return nil, fmt.Errorf("no store")
	}
	acct, sym := at.latchScope()
	root := instrumentRoot(sym)
	var ids []string
	rows, err := at.store.ArmedOrders().ListNonTerminalAllTraders()
	if err != nil {
		return nil, fmt.Errorf("armed ledger: %w", err)
	}
	for _, r := range rows {
		// PLACED = a non-terminal row that carries a broker signal: the stamp
		// (BeginPlacement) is what gives an arm its signal, so an unplaced
		// 'armed' row has none. The terminal set is the ONE canonical
		// predicate — never a copied list of states (arm_state guard).
		if strings.TrimSpace(r.SignalID) == "" || store.IsTerminalArmState(r.State) {
			continue
		}
		if siblings != nil && siblings[r.SignalID] {
			continue // DS-103: a sibling leg's own placed row is this entry
		}
		if a, s, ok := at.traderScope(r.TraderID); ok && (!strings.EqualFold(a, acct) || instrumentRoot(s) != root) {
			continue
		}
		ids = append(ids, latchArmedHolder(r))
	}
	prows, err := at.store.PictureHtfRecoverableAll()
	if err != nil {
		return nil, fmt.Errorf("picture ledger: %w", err)
	}
	for _, p := range prows {
		if !store.PictureSendStarted(p) {
			continue
		}
		if strings.TrimSpace(p.Account) != "" && !strings.EqualFold(p.Account, acct) {
			continue
		}
		if strings.TrimSpace(p.Symbol) != "" && instrumentRoot(p.Symbol) != root {
			continue
		}
		ids = append(ids, latchPictureHolder(p))
	}
	sort.Strings(ids)
	return ids, nil
}

// W5 D15 — THE LATCH NAMES ITS HOLDER. A refusal "ledger_open" used to list
// "picture:<opp>(<stage>)" with no row id, so a reader could not find the row
// that held the account. A Picture ledger row is now named "latched by picture
// row #<id> (<stage>)" (the picture_htf_opportunities id the 🖼 boot line prints —
// never the opportunity key, which embeds the account name, L12), and an
// armed row a Picture scenario placed says so beside its own id. A planner
// row reads exactly as before.
func latchPictureHolder(p store.PictureHtfOpportunityDB) string {
	return fmt.Sprintf("latched by picture row #%d (%s)", p.ID, p.Stage)
}

func latchArmedHolder(r store.ArmedOrderDB) string {
	if r.Source != "" {
		return fmt.Sprintf("armed#%d(%s %s) latched by %s scenario %s", r.ID, r.State, r.SignalID, r.Source, r.Scenario)
	}
	return fmt.Sprintf("armed#%d(%s %s)", r.ID, r.State, r.SignalID)
}
