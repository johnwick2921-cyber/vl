package trader

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (c) — the pre-open reconcile never flattens a position a
// ledger explains ─────────────────────────────────────────────────────────────
//
// reconcileBeforeOpenNT flattened ANY position NT8 held before an AI open,
// reading neither trader_positions nor either ledger — so an armed or Picture
// fill not yet materialized (the reconciler's untracked grace) was flattened as
// an "orphan", with its bracket, and the AI opened its own (D10). A position is
// EXPLAINED — and the AI entry refused, named, as a ⛔ gate — when on this
// account, instrument and side there is:
//
//	(i)   a placed non-terminal ledger row: an armed row carrying a broker
//	      signal, or a Picture row working / stamped (a send started)
//	(ii)  an OPEN trader_positions row whose entry_order_id is a ledger signal
//	(ii') W1b E10: an OPEN trader_positions row of ANOTHER trader — running
//	      or not — on this account, instrument and side (a WARN names the row:
//	      a stale one blocks every AI open here until it is closed)
//	(iii) a ledger FILL younger than twice the untracked grace — the
//	      reconciler has not materialized it yet — on ANY trader bound to this
//	      account, running or stopped (W1b E10: a ledger-wide read; W0b walked
//	      the running-trader registry, so a trader stopped between its fill
//	      and our open dropped out and its position read as an orphan)
//
// Every read here that FAILS explains the position (W1b E10, fail-closed): an
// unreadable ledger is never taken for "no owner", so a read error can refuse
// an AI open but can never flatten a position.
//
// An UNEXPLAINED position keeps today's owner-ruled flatten (TRACK B).

// errPositionOwned marks the refusal: the held position belongs to a ledger.
var errPositionOwned = errors.New("position owned by a ledger row")

// reconcileRefusal maps reconcileBeforeOpenNT's outcome for the open paths: a
// ledger-owned position is a ⛔ REFUSAL (counted, stamped on the record,
// returns nil — the decision is classified refused, not failed); anything
// else stays the error it was.
func (at *AutoTrader) reconcileRefusal(err error, rec *store.DecisionAction) error {
	if !errors.Is(err, errPositionOwned) {
		return err
	}
	telemetry.IncGateBlock(at.id, "reconcile_owned")
	if rec != nil {
		rec.Success = false
		rec.Error = "reconcile_owned: " + err.Error()
	}
	return nil
}

// ledgerExplainsPosition reports the ledger row that explains a held position
// on this trader's account and symbol for side ("long"/"short"), if any.
func (at *AutoTrader) ledgerExplainsPosition(symbol, side string, now time.Time) (string, bool) {
	var readErr error
	return at.ledgerExplainsPositionImpl(symbol, side, now, &readErr)
}

// ledgerExplainsPositionChecked is ledgerExplainsPosition plus the one fact the
// reconcile's own reading throws away: whether the answer is "the ledger
// explains it" or "the ledger could not be READ". Reconcile treats an unreadable
// ledger as an explanation on purpose (never flatten on a read error). A check
// that PAUSES entries must not: m447 broker_agreement reads readErr != nil as
// "cannot judge" so an unreadable ledger never CLEARS a standing block.
func (at *AutoTrader) ledgerExplainsPositionChecked(symbol, side string, now time.Time) (string, bool, error) {
	var readErr error
	reason, explained := at.ledgerExplainsPositionImpl(symbol, side, now, &readErr)
	return reason, explained, readErr
}

func (at *AutoTrader) ledgerExplainsPositionImpl(symbol, side string, now time.Time, readErr *error) (string, bool) {
	if at.store == nil {
		return "", false
	}
	acct, _ := at.latchScope()
	root := instrumentRoot(symbol)
	window := 2 * time.Duration(ntTrader.UntrackedGraceMs) * time.Millisecond
	onAccount := func(traderID string) bool {
		a, s, ok := at.traderScope(traderID)
		return !ok || (strings.EqualFold(a, acct) && instrumentRoot(s) == root)
	}
	signals := map[string]string{}
	// W1b E10: a read that fails is an EXPLANATION, never "no owner".
	unreadable := func(what string, err error) (string, bool) {
		*readErr = fmt.Errorf("%s: %w", what, err)
		return fmt.Sprintf("ledger unreadable (%s: %v) — fail-closed: never flattened on a read error", what, err), true
	}
	// W1b FOLD-11: now is captured by the caller BEFORE these reads, so a row
	// the settle pass moved working→filled after it carries UpdatedAt > now
	// (d < 0). That is fresher than fresh, never "not fresh" — reading it as
	// stale left the position unexplained and flattened it (D10). Armed and
	// Picture rows alike.
	fresh := func(ts time.Time) bool { return now.Sub(ts) < window }

	// (i) armed: non-terminal rows that carry a broker signal.
	rows, err := at.store.ArmedOrders().ListNonTerminalAllTraders()
	if err != nil {
		return unreadable("armed non-terminal", err)
	}
	for _, r := range rows {
		if strings.TrimSpace(r.SignalID) == "" || store.IsTerminalArmState(r.State) || positionSide(r.Side) != side || !onAccount(r.TraderID) {
			continue
		}
		return fmt.Sprintf("armed #%d %s %s (%s, signal %s)", r.ID, r.Scenario, side, r.State, r.SignalID), true
	}
	// (i) Picture: working, or place_pending with a submission stamp.
	recoverable, err := at.store.PictureHtfRecoverableAll()
	if err != nil {
		return unreadable("picture recoverable", err)
	}
	for _, p := range recoverable {
		if !store.PictureSendStarted(p) || positionSide(p.Direction) != side || !pictureRowOnAccount(p, acct, root) {
			continue
		}
		return fmt.Sprintf("picture %s %s (%s)", p.OppKey, side, p.Stage), true
	}
	// (iii) recent fills, not yet materialized — LEDGER-WIDE (W1b E10): every
	// trader bound to this account, running or stopped. The store widens its
	// SQL bound; the exact window is judged here on the parsed time.
	filled, err := at.store.ArmedOrders().ListFilledSinceAllTraders(now.Add(-window))
	if err != nil {
		return unreadable("armed filled", err)
	}
	for _, r := range filled {
		if positionSide(r.Side) == side && fresh(r.UpdatedAt) && onAccount(r.TraderID) {
			return fmt.Sprintf("armed #%d %s %s filled %s ago (not yet materialized)", r.ID, r.Scenario, side, now.Sub(r.UpdatedAt).Round(time.Second)), true
		}
	}
	pictureFilled, err := at.store.PictureHtfFilledSinceAll(now.Add(-window))
	if err != nil {
		return unreadable("picture filled", err)
	}
	for _, p := range pictureFilled {
		if positionSide(p.Direction) == side && fresh(p.UpdatedAt) && onAccount(p.TraderID) && pictureRowOnAccount(p, acct, root) {
			return fmt.Sprintf("picture %s %s filled %s ago (not yet materialized)", p.OppKey, side, now.Sub(p.UpdatedAt).Round(time.Second)), true
		}
	}
	// The ledger signals (ii) joins this trader's OPEN rows against — this
	// trader and every running trader (the W0b scope, unchanged).
	ids := []string{at.id}
	for _, id := range runningTraderIDs() {
		if id != at.id {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if !onAccount(id) {
			continue
		}
		armed, err := at.store.ArmedOrders().ListFilled(id, 20)
		if err != nil {
			return unreadable("armed filled of "+id, err)
		}
		for _, r := range armed {
			signals[r.SignalID] = fmt.Sprintf("armed #%d", r.ID)
		}
		pics, err := at.store.PictureHtfByTrader(id, 20)
		if err != nil {
			return unreadable("picture ledger of "+id, err)
		}
		for _, p := range pics {
			if p.SignalID != "" {
				signals[p.SignalID] = "picture " + p.OppKey
			}
		}
	}
	opens, err := at.store.Position().GetAllOpenPositions()
	if err != nil {
		return unreadable("open positions", err)
	}
	// (ii) an OPEN row of THIS trader whose entry order is a ledger signal.
	for _, op := range opens {
		if op.TraderID != at.id {
			continue
		}
		if owner, ok := signals[op.EntryOrderID]; ok && op.EntryOrderID != "" && positionSide(op.Side) == side && instrumentRoot(op.Symbol) == root {
			return fmt.Sprintf("position #%d from %s", op.ID, owner), true
		}
	}
	// (ii') W1b E10: an OPEN row of ANOTHER trader on this account, instrument
	// and side — that trader's position, running or stopped, whatever its age.
	// An account-less row counts when its trader is bound here (or bound to
	// nothing — fail-closed).
	for _, op := range opens {
		if op.TraderID == at.id || positionSide(op.Side) != side || instrumentRoot(op.Symbol) != root {
			continue
		}
		if a := strings.TrimSpace(op.Account); a != "" && !strings.EqualFold(a, acct) {
			continue
		} else if a == "" && !onAccount(op.TraderID) {
			continue
		}
		age := "age n/a"
		if op.EntryTime > 0 {
			age = "open " + now.Sub(time.UnixMilli(op.EntryTime)).Round(time.Second).String()
		}
		at.logWarnf("🧷 reconcile (ii'): OPEN row #%d of trader %s (%s %s, account %q, %s) explains the held position — if NT8 no longer holds that trader's position, row #%d is STALE and blocks every AI %s open on this account until it is closed.",
			op.ID, op.TraderID, op.Symbol, op.Side, op.Account, age, op.ID, side)
		return fmt.Sprintf("position #%d of trader %s", op.ID, op.TraderID), true
	}
	return "", false
}

func pictureRowOnAccount(p store.PictureHtfOpportunityDB, acct, root string) bool {
	if strings.TrimSpace(p.Account) != "" && !strings.EqualFold(p.Account, acct) {
		return false
	}
	return strings.TrimSpace(p.Symbol) == "" || instrumentRoot(p.Symbol) == root
}
