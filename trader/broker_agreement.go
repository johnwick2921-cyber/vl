package trader

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
	"vl/logger"
	"vl/market"
	nt "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m447 (release #54) — DATA-TRUTH CHECK "broker_agreement" ─────────────────
//
// OWNER PRINCIPLE (2026-10-29): "this is a financial product, not a game -
// nothing related to data loss or incorrect data can continue to trade."
//
// The bot decides from ITS OWN view of the account (trader_positions, the
// armed_orders ledger, the pending-entry map). NT8 holds the truth. When the two
// disagree about what is open, what is resting or which account this is, the bot
// is about to size, place and risk-gate against a picture that is wrong. This
// check pauses NEW ENTRIES for the symbol while they disagree, and only then.
//
// WHAT IT NEVER DOES. It is read-only. It never exits, moves a stop, trails,
// closes, protects or flattens, and the exit/management paths never consult it
// (datatruth's contract). It never closes or materializes a row - that is the
// position reconcile's job and stays there.
//
// NO SECOND RECONCILE. The disagreement classes are the ones the existing
// reconcile already names, read through the same helpers and the same numbers:
//
//	positions  NT8-held vs trader_positions OPEN - ntTrader.HeldBook (the
//	           reconcile's own "what NT8 holds"), the same "SYMBOL|SIDE" key and
//	           the same three verdicts: UNTRACKED (NT8 holds, no row - the
//	           materialize case), ORPHAN (row open, NT8 flat - the orphan-close
//	           case), QTY (held != row - the A4 divergence case); the owner lookups
//	           are reconcile's (GetOpenPositionByAccountSymbol, then
//	           unassignedOpenForTrader) and an NT8 position the ledger explains
//	           (ledgerExplainsPosition, W0 (c)) is a fill in flight, not a
//	           disagreement.
//	orders     the armed_orders ledger vs the broker's book - reaperVerdictAt
//	           (the reaper's ALIVE/GONE/UNKNOWN) and orderMatchesArm; plus the
//	           reverse: a live ENTRY order in the book that no ledger row, no
//	           position and no pending entry names (nt8_order_snapshots 153026..
//	           153203: SimAccount1, SELL limit 29458.5, 30 minutes, then it filled
//	           and the position was born from reconcile, not from the bot), and a
//	           live order for an arm the ledger already settled terminal.
//	binding    the trader's bound account must exist in NT8's account list, and
//	           every OPEN row of the trader must sit on that account.
//
// SETTLE WINDOW. A disagreement blocks only after it has been observed for
// ntTrader.ReconcileSettleMs (60 s). Why 60 s, from code timing, not taste:
//   - the reconcile's own debounce for the same three position verdicts is
//     reconcileDivergenceGraceMs = untrackedGraceMs = flatGraceMs = 60 s (3
//     reconcile passes) "a fill frame can be momentarily in flight";
//   - the AddOn's order_snapshot period is 30 s (DefaultOrderSnapshotSecs) and a
//     book older than 2 x that is called stale (OrderSnapshotInterval) - an order
//     placed or cancelled just now cannot appear in / leave the book for up to one
//     period, and the next snapshot can be one more period late.
//
// First-seen is tracked per disagreement KEY and is sampled on the reconcile
// loop (ntTrader.SetReconcileObserver, every 20 s whether or not anything tries
// to enter), so a disagreement is already old when the first entry arrives; it
// is never "first seen at the entry".
//
// SCOPE (m451). The block is FOR the trader whose view disagrees, not for the
// symbol: each trader is bound to its own NT8 account, so account A's stale book,
// unknown order or quantity mismatch says nothing about account B's picture. The
// check returns the disagreeing trader's scope token (datatruth.ScopeTrader) and
// dataTruthReasons lets the block through only to the asking trader. A trader
// that cannot judge is blocked on its own; only "no trader is registered at all"
// (nobody to read) blocks every trader on the symbol.
//
// STANDING STATE (m451). First-seen survives a closed market: the CME-closed
// ticks neither judge nor forget. A disagreement that was blocking when the market
// closed is still blocking at the reopen - the first entry attempt after the bell
// measures it fresh against its OLD clock instead of giving it a new 60 s
// to hide in - and it ends only when a measurement finds the books agree. The same
// holds for the position disagreements across the one call that cannot read the
// positions frame because an entry just landed (PosSkip).
//
// CANNOT JUDGE = BLOCK (fail closed): no bound account, no positions frame for
// the account, an unreadable store, or - while the CME is open - a broker book
// too old to read an order out of. While the CME is CLOSED nothing can open or
// fill and a quiet broker is the expected state (m433), so it is not judged.

// CheckBrokerAgreement is the registered check name.
const CheckBrokerAgreement = "broker_agreement"

// agreementSettle is the settle window (see above).
const agreementSettle = time.Duration(ntTrader.ReconcileSettleMs) * time.Millisecond

// agreementBookStale: a broker book older than this many snapshot periods is too
// old to read an order out of. The cutover gate and the reaper use 2 (60 s); this
// check pauses ENTRIES, and 30 days of persisted nt8_order_snapshots show gaps of
// 61-120 s on a live process (62, 68, 69, 71, 74, 79, 85, 90, 91, 97, 100 s ...)
// that cleared by themselves: at 2 each would open an episode and push the
// owner. 4 periods (120 s) leaves only the gaps that are real lapses.
const agreementBookStale = 4

// agreementNow is the clock of the reconcile-driven sampler (a test states its
// own instant).
var agreementNow = time.Now

// disagreement is one named disagreement. Key is stable for as long as the
// SAME condition holds (it is what first-seen is tracked by); Detail is for the
// owner and quotes both sides.
type disagreement struct {
	Key    string
	Detail string
}

// trackedD is one disagreement the tracker is clocking.
type trackedD struct {
	first  time.Time
	detail string
}

// agreementTracker holds first-seen per disagreement key.
type agreementTracker struct {
	mu    sync.Mutex
	first map[string]trackedD
}

func newAgreementTracker() *agreementTracker {
	return &agreementTracker{first: map[string]trackedD{}}
}

// apply records ds at now: a key new this call starts its settle clock, a key
// absent this call is dropped (the condition measured clear) UNLESS hold(key)
// says this call could not measure it (m451: the positions frame was unreadable
// for one call) - a held key keeps its clock, and keeps blocking if it already
// was. It returns the disagreements that have persisted at least settle (blocking)
// and the count still settling.
func (tk *agreementTracker) apply(ds []disagreement, now time.Time, settle time.Duration, hold func(key string) bool) (blocking []disagreement, settling int) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	live := make(map[string]bool, len(ds))
	for _, d := range ds {
		live[d.Key] = true
		t, ok := tk.first[d.Key]
		if !ok {
			t = trackedD{first: now}
		}
		t.detail = d.Detail
		tk.first[d.Key] = t
		if now.Sub(t.first) >= settle {
			d.Detail = fmt.Sprintf("%s [disagreeing for %ds]", d.Detail, int(now.Sub(t.first).Seconds()))
			blocking = append(blocking, d)
		} else {
			settling++
		}
	}
	keys := make([]string, 0, len(tk.first))
	for k := range tk.first {
		if !live[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if hold == nil || !hold(k) {
			delete(tk.first, k)
			continue
		}
		t := tk.first[k]
		if now.Sub(t.first) >= settle {
			blocking = append(blocking, disagreement{Key: k, Detail: fmt.Sprintf("%s [disagreeing for %ds; not re-measured on this call]", t.detail, int(now.Sub(t.first).Seconds()))})
		} else {
			settling++
		}
	}
	return blocking, settling
}

// standing returns every tracked disagreement that had already persisted settle,
// WITHOUT measuring anything: what a closed market reports (m451).
func (tk *agreementTracker) standing(now time.Time, settle time.Duration) []disagreement {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	keys := make([]string, 0, len(tk.first))
	for k := range tk.first {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []disagreement
	for _, k := range keys {
		t := tk.first[k]
		if now.Sub(t.first) >= settle {
			out = append(out, disagreement{Key: k, Detail: fmt.Sprintf("%s [disagreeing for %ds; the market is closed, so it is carried to the reopen and re-measured there]", t.detail, int(now.Sub(t.first).Seconds()))})
		}
	}
	return out
}

func (tk *agreementTracker) reset() {
	tk.mu.Lock()
	tk.first = map[string]trackedD{}
	tk.mu.Unlock()
}

// agreementRemedy is the owner-facing way out of a blocking disagreement, by the
// kind of its key. Each names what the bot will do by itself and what only the
// owner can do - the bot never edits a row quantity or cancels an NT8 order for
// this check, and the freeze clear (/api/risk/clear-freeze) does NOT lift this
// pause: it ends only when the two views agree again.
func agreementRemedy(key string) string {
	switch {
	case strings.HasPrefix(key, "pos:qty:"):
		return "REMEDY: the bot never edits a row's quantity. In NT8 close the position (or trade it to exactly the row's quantity) - the close frame closes the row and the pause lifts the moment the two quantities agree. /api/risk/clear-freeze unfreezes the trader but does NOT lift this pause."
	case strings.HasPrefix(key, "pos:orphan:"):
		return "REMEDY: none needed - the position reconcile closes the row by itself once NT8 has been flat for 60 s (an UNRESOLVED exit if no close frame came) and the pause lifts when it does. If it is still here after a few minutes, read the 'reconcile:' log lines for why it did not."
	case strings.HasPrefix(key, "pos:untracked:"):
		return "REMEDY: none needed - the position reconcile writes a row for it after 60 s and the pause lifts. If it is not yours, flatten it in NT8."
	case strings.HasPrefix(key, "ord:unknown:"), strings.HasPrefix(key, "ord:zombie:"):
		return "REMEDY: cancel that order in NT8 (Control Center > Orders tab) - the pause lifts when the next order snapshot (30 s) no longer lists it. If you want it, leave it: it blocks entries until it fills or is cancelled."
	case strings.HasPrefix(key, "ord:gone:"):
		return "REMEDY: none needed - the armed-order reaper marks the arm cancelled by itself once the arm has had no update for 15 minutes and the broker's fresh book still lacks it, and the pause lifts."
	case strings.HasPrefix(key, "bind:not_offered:"):
		return "REMEDY: open or reconnect that account in NT8 (it must appear in NT8's account list), or bind the trader to an account NT8 offers in Settings."
	case strings.HasPrefix(key, "bind:row_acct:"):
		return "REMEDY: the open row sits on another account than the trader is bound to; close that position in NT8 on the row's account, or rebind the trader to it."
	}
	return ""
}

// agreementCannotJudge is the fail-closed return: the check could not compare
// the two views, which is itself the finding.
type agreementCannotJudge struct{ why string }

func (e *agreementCannotJudge) Error() string { return e.why }

func cannotJudge(format string, a ...any) error {
	return &agreementCannotJudge{why: fmt.Sprintf(format, a...)}
}

// agreementInputs is everything the pure comparison reads: the bot's view and
// NT8's, already fetched. judgeAgreement reads no clock, no store, no network.
type agreementInputs struct {
	Symbol  string
	Account string
	Now     time.Time

	// bot side
	Rows    []*store.TraderPosition // this trader's OPEN rows
	Arms    []store.ArmedOrderDB    // this trader's non-terminal arms
	Pending map[string]int64        // entries sent, fill not yet seen (signal -> ms)
	// PendingSides is Pending's side ("LONG"/"SHORT"), signal -> side. An NT8
	// position with no row is excused only by a pending entry of ITS side.
	PendingSides map[string]string
	// ownerOf is reconcile's account-aware owner lookup for an NT8-held key
	// (sym, side) -> an OPEN row explains it. ledgerExplains is
	// ledgerExplainsPosition (W0 (c)). armFor / positionKnows resolve a book
	// order name to the ledger / a position row.
	OwnerOf        func(sym, side string) (bool, error)
	LedgerExplains func(sym, side string) (bool, error)
	ArmFor         func(signal string) (*store.ArmedOrderDB, error)
	PositionKnows  func(signal string) (bool, error)

	// broker side
	AccountOffered bool // NT8's account list is non-empty and names Account
	AccountsKnown  bool // NT8's account list is non-empty at all
	Held, HeldQty  map[string]float64
	PosJudgeable   bool // a positions frame newer than the last entry receipt
	// PosSkip: the positions frame cannot be compared on THIS call because an entry
	// just landed (the reconcile waits too). The position comparisons are skipped
	// and the tracker holds the position disagreements it was clocking (m451).
	PosSkip    bool
	Book       *nt.OrderSnapshotCache
	Interval   time.Duration
	BookAge    time.Duration
	BookKnown  bool
	BookOrders []nt.NT8Order
}

var botEntryNameRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// judgeAgreement lists every disagreement between the two views, or an error
// when it cannot compare them. It is pure.
func judgeAgreement(in agreementInputs) ([]disagreement, error) {
	var ds []disagreement
	root := instrumentRoot(in.Symbol)
	nowMs := in.Now.UnixMilli()

	// ── binding ──────────────────────────────────────────────────────────────
	if strings.TrimSpace(in.Account) == "" {
		return nil, cannotJudge("this trader has no bound NT8 account - the broker's view of it cannot be read")
	}
	if in.AccountsKnown && !in.AccountOffered {
		ds = append(ds, disagreement{"bind:not_offered:" + in.Account,
			fmt.Sprintf("the trader is bound to account %q but NT8's account list does not offer it", in.Account)})
	}
	for _, r := range in.Rows {
		if instrumentRoot(r.Symbol) != root {
			continue
		}
		if r.Account != "" && r.Account != in.Account {
			ds = append(ds, disagreement{fmt.Sprintf("bind:row_acct:%d", r.ID),
				fmt.Sprintf("open row %d %s %s is on account %q but the trader is bound to %q", r.ID, r.Symbol, r.Side, r.Account, in.Account)})
		}
	}

	// ── positions: the reconcile's three verdicts, read-only ─────────────────
	if !in.PosJudgeable {
		return nil, cannotJudge("NT8 has not reported a positions frame for %q since the last entry receipt - the held book is unknown, not flat", in.Account)
	}
	for _, r := range in.Rows {
		if in.PosSkip {
			break
		}
		if instrumentRoot(r.Symbol) != root || (r.Account != "" && r.Account != in.Account) {
			continue
		}
		key := r.Symbol + "|" + r.Side
		hq, isHeld := in.HeldQty[key]
		switch {
		case !isHeld:
			// ORPHAN: a very fresh row has not had its positions frame yet.
			if nowMs-r.EntryTime >= ntTrader.OrphanGraceMs {
				ds = append(ds, disagreement{fmt.Sprintf("pos:orphan:%d", r.ID),
					fmt.Sprintf("bot has OPEN row %d %s %s qty %.0f (entry %ds ago) but NT8 holds no %s on %s",
						r.ID, r.Symbol, r.Side, r.Quantity, (nowMs-r.EntryTime)/1000, r.Side, in.Account)})
			}
		case r.Quantity > 0 && hq != r.Quantity:
			ds = append(ds, disagreement{fmt.Sprintf("pos:qty:%d", r.ID),
				fmt.Sprintf("NT8 holds %.0f %s %s but bot row %d has %.0f", hq, r.Symbol, r.Side, r.ID, r.Quantity)})
		}
	}
	keys := make([]string, 0, len(in.Held))
	for k := range in.Held {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if in.PosSkip {
			break
		}
		sym, side := key, "LONG"
		if i := strings.LastIndex(key, "|"); i >= 0 {
			sym, side = key[:i], key[i+1:]
		}
		if instrumentRoot(sym) != root {
			continue
		}
		owned, err := in.OwnerOf(sym, side)
		if err != nil {
			return nil, cannotJudge("owner lookup for %s %s failed: %v", sym, side, err)
		}
		if owned {
			continue
		}
		explained, lerr := in.LedgerExplains(sym, side)
		if lerr != nil {
			// reconcile's "never flatten on a read error" answers (reason, true) for an
			// unreadable ledger; reusing that as "explained" would CLEAR a standing
			// block on a store error. Here an unreadable ledger is "cannot judge".
			return nil, cannotJudge("ledger read for NT8 %s %s failed: %v", sym, side, lerr)
		}
		if explained {
			continue // an armed / Picture fill in flight: the ledger explains it
		}
		if pendingEntryOnSide(in.PendingSides, side) {
			continue // an entry OF THIS SIDE was sent and its fill has not landed yet
		}
		ds = append(ds, disagreement{"pos:untracked:" + key,
			fmt.Sprintf("NT8 holds %s %s qty %.0f @ %.2f on %s but the bot has no open row and no ledger row explains it",
				sym, side, in.HeldQty[key], in.Held[key], in.Account)})
	}

	// ── orders ───────────────────────────────────────────────────────────────
	placed := make([]store.ArmedOrderDB, 0, len(in.Arms))
	for _, a := range in.Arms {
		if strings.TrimSpace(a.SignalID) == "" || store.IsTerminalArmState(a.State) {
			continue
		}
		if a.State != store.StateWorking && a.State != store.StatePlacePending {
			continue // armed (unplaced) and cancel_pending are not claims that an order rests
		}
		placed = append(placed, a)
	}
	bookNeeded := len(placed) > 0
	if !in.BookKnown || in.BookAge > agreementBookStale*in.Interval {
		if bookNeeded {
			return nil, cannotJudge("the ledger has %d placed order(s) but the broker's order book is %s - a book we have not heard about is not an empty book",
				len(placed), bookAgeWords(in.BookKnown, in.BookAge, in.Interval))
		}
		// Nothing placed: the reverse direction (an order the bot never wrote
		// down) needs a book too. A broker that has gone quiet cannot be read.
		return nil, cannotJudge("the broker's order book is %s - an order the ledger does not know about cannot be ruled out",
			bookAgeWords(in.BookKnown, in.BookAge, in.Interval))
	}
	// ledger -> broker: reaper's own verdict, GONE only.
	for _, a := range placed {
		v, why := reaperVerdictAt(in.Book, in.Account, in.Symbol, a, agreementBookStale/2*in.Interval, in.Now)
		switch v {
		case reaperGone:
			ds = append(ds, disagreement{fmt.Sprintf("ord:gone:%d", a.ID),
				fmt.Sprintf("ledger says arm %d %s %s is %s but %s", a.ID, a.Scenario, a.Side, a.State, why)})
		case reaperUnknown:
			return nil, cannotJudge("arm %d: %s", a.ID, why)
		}
	}
	// broker -> ledger.
	for _, o := range in.BookOrders {
		if !o.IsLiveAtExchange() || strings.TrimSpace(o.Name) == "" || isBracketChild(o.Name) {
			continue
		}
		if sym := strings.TrimSpace(o.Symbol); sym != "" && instrumentRoot(sym) != root {
			continue
		}
		if !botEntryNameRe.MatchString(strings.TrimSpace(o.Name)) {
			continue // an NT8 "Close"/manual order is not the bot's data to lose
		}
		if _, pend := in.Pending[o.Name]; pend {
			continue
		}
		arm, err := in.ArmFor(o.Name)
		if err != nil && !store.IsRecordNotFound(err) {
			return nil, cannotJudge("ledger lookup for broker order %s failed: %v", o.Name, err)
		}
		if arm != nil {
			if store.IsTerminalArmState(arm.State) && !strings.EqualFold(arm.State, store.StateFilled) {
				ds = append(ds, disagreement{"ord:zombie:" + o.Name,
					fmt.Sprintf("ledger arm %d is %s but NT8 still has order %s %s %s @ %.2f %s", arm.ID, arm.State, o.Name, o.Action, o.Type, orderPrice(o), o.State)})
			}
			continue
		}
		known, kerr := in.PositionKnows(o.Name)
		if kerr != nil {
			return nil, cannotJudge("position lookup for broker order %s failed: %v", o.Name, kerr)
		}
		if known {
			continue
		}
		ds = append(ds, disagreement{"ord:unknown:" + o.Name,
			fmt.Sprintf("NT8 has a live entry order %s %s %s %d @ %.2f (%s) on %s that no ledger row, position or pending entry of this trader names",
				o.Name, o.Action, o.Type, o.Quantity, orderPrice(o), o.State, in.Account)})
	}
	return ds, nil
}

// pendingEntryOnSide: some entry of the held position's side is awaiting its
// fill. A pending entry of the OTHER side (or one whose side is not a side) does
// not explain this position: it would hide an NT8 position the bot has no row
// for for as long as ANY entry rests at the broker, which for a resting limit is
// hours (m447 review N1). Unrecognised side = does not excuse (fail closed).
func pendingEntryOnSide(sides map[string]string, heldSide string) bool {
	want := positionSide(heldSide)
	if want == "" {
		return false
	}
	for _, s := range sides {
		if positionSide(s) == want {
			return true
		}
	}
	return false
}

func bookAgeWords(known bool, age, interval time.Duration) string {
	if !known {
		return "absent (no order_snapshot received for the account)"
	}
	return fmt.Sprintf("stale (%ds old, limit %ds)", int(age.Seconds()), int((agreementBookStale * interval).Seconds()))
}

// ── gathering: the production inputs ─────────────────────────────────────────

func (at *AutoTrader) brokerAgreementInputs(now time.Time) (agreementInputs, error) {
	in := agreementInputs{Now: now, Symbol: at.futuresSymbol()}
	nt8 := at.armedTrader()
	if nt8 == nil {
		return in, cannotJudge("not an NT8 trader - no broker view")
	}
	srv := nt8.GetServer()
	if srv == nil || at.store == nil || at.store.Position() == nil || at.store.ArmedOrders() == nil {
		return in, cannotJudge("no wire server or store - the two views cannot be read")
	}
	in.Account = strings.TrimSpace(nt8.BoundAccount())
	if in.Account == "" {
		return in, nil // judgeAgreement turns this into the cannot-judge
	}

	if accts, _ := srv.GetAccountsList(); len(accts) > 0 {
		in.AccountsKnown = true
		for _, a := range accts {
			if strings.EqualFold(a.Name, in.Account) {
				in.AccountOffered = true
			}
		}
	}

	// broker positions - the reconcile's own read (PositionsFor) with the W117 F1
	// rule: a frame received at-or-before the latest entry receipt predates that
	// entry and is not evidence.
	if snap, received, entryAfter, ok := srv.PositionsForExecutionReceipt(in.Account, nt8.WireSymbol()); ok {
		in.PosJudgeable = entryAfter.IsZero() || received.After(entryAfter)
		if !in.PosJudgeable && now.Sub(entryAfter) < agreementSettle {
			in.PosJudgeable = true // the entry just landed; reconcile waits too
			in.PosSkip = true
			snap = nil
			// a stale pre-entry snapshot must not be compared against the new row:
			// an empty held book plus a young row (< OrphanGraceMs) judges nothing.
		}
		in.Held, in.HeldQty = ntTrader.HeldBook(snap)
	}

	rows, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		return in, cannotJudge("open positions unreadable: %v", err)
	}
	in.Rows = rows
	arms, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		return in, cannotJudge("armed ledger unreadable: %v", err)
	}
	in.Arms = arms
	in.Pending = nt8.PendingEntries()
	in.PendingSides = nt8.PendingEntrySides()

	st := at.store
	acct := in.Account
	in.OwnerOf = func(sym, side string) (bool, error) {
		// reconcile's lookups, in its order.
		owner, err := st.Position().GetOpenPositionByAccountSymbol(acct, sym, side)
		if err != nil {
			return false, err
		}
		if owner == nil {
			owner, err = ntTrader.UnassignedOpenForTrader(st, at.id, sym, side)
			if err != nil {
				return false, err
			}
		}
		return owner != nil, nil
	}
	in.LedgerExplains = func(sym, side string) (bool, error) {
		_, explained, err := at.ledgerExplainsPositionChecked(sym, positionSide(side), now)
		return explained, err
	}
	in.ArmFor = func(signal string) (*store.ArmedOrderDB, error) { return st.ArmedOrders().FindBySignal(at.id, signal) }
	in.PositionKnows = func(signal string) (bool, error) { return st.Position().EntryOrderIDInUse(signal) }

	in.Book = srv.OrderSnapshots()
	in.Interval = OrderSnapshotInterval()
	if snap, rcv, ok := in.Book.LatestReceived(in.Account); ok && !rcv.IsZero() {
		in.BookKnown, in.BookAge, in.BookOrders = true, now.Sub(rcv), snap.Orders
	}
	return in, nil
}

// ── the per-trader verdict, the registry, the datatruth check ────────────────

// agreementTraders is every NT8 trader that has started, by id. A trader is
// removed when it stops: a stopped trader's disagreement is not a reason to
// pause a symbol another trader is trading.
var agreementTraders sync.Map // traderID -> *AutoTrader

func (at *AutoTrader) agreementTrackerFor() *agreementTracker {
	at.agreementOnce.Do(func() { at.agreementTk = newAgreementTracker() })
	return at.agreementTk
}

// brokerAgreementVerdict runs the whole comparison for this trader and returns
// the blocking disagreements. A non-nil error is "cannot judge" (a block).
func (at *AutoTrader) brokerAgreementVerdict(now time.Time) (blocking []disagreement, settling int, err error) {
	tk := at.agreementTrackerFor()
	if !kernel.IsCMEOpen(now) {
		// Market closed: nothing can open or fill and a quiet broker is expected
		// (m433), so nothing is MEASURED - but nothing is forgotten either (m451).
		// What was blocking when the bell rang is still blocking now and at the
		// reopen; it ends on a measurement that finds the books agree, never on
		// the clock.
		return tk.standing(now, agreementSettle), 0, nil
	}
	in, err := at.brokerAgreementInputs(now)
	if err != nil {
		return nil, 0, err
	}
	ds, err := judgeAgreement(in)
	if err != nil {
		return nil, 0, err
	}
	var hold func(string) bool
	if in.PosSkip {
		hold = func(k string) bool { return strings.HasPrefix(k, "pos:") }
	}
	blocking, settling = tk.apply(ds, now, agreementSettle, hold)
	for i := range blocking {
		if r := agreementRemedy(blocking[i].Key); r != "" {
			blocking[i].Detail += " - " + r
		}
	}
	return blocking, settling, nil
}

// registerBrokerAgreement puts this trader under the check and starts sampling
// on its reconcile loop. Idempotent. Called at Run, next to StartPositionReconcile.
func (at *AutoTrader) registerBrokerAgreement() {
	nt8 := at.armedTrader()
	if nt8 == nil {
		return
	}
	agreementTraders.Store(at.id, at)
	nt8.SetReconcileObserver(func() {
		now := agreementNow()
		checks.DefaultFills().Sweep(now) // m447 (7): judge fills whose bar has closed since
		if _, _, err := at.brokerAgreementVerdict(now); err != nil {
			logger.Debugf("broker_agreement: %s cannot judge: %v", at.id, err)
		}
	})
}

// unregisterBrokerAgreement is the Stop side.
//
// Compare-and-delete (m451): it removes the entry only if the entry is still THIS
// instance. A teardown that runs after a newer instance of the same trader id has
// registered (a Run's deferred and error paths both reach here) must not remove
// the newer instance's entry - the check would then find no trader while the
// CME is open and block every entry on the symbol - nor clear the newer
// instance's reconcile observer.
func (at *AutoTrader) unregisterBrokerAgreement() {
	if cur, ok := agreementTraders.Load(at.id); ok && cur != any(at) {
		return // a newer instance owns this id now
	}
	agreementTraders.CompareAndDelete(at.id, any(at))
	if nt8 := at.armedTrader(); nt8 != nil {
		nt8.SetReconcileObserver(nil)
	}
}

// RegisterBrokerAgreement registers the check with internal/datatruth. The
// integrator calls it once at boot (next to WireDataTruth) and may Require it.
func RegisterBrokerAgreement() {
	datatruth.RegisterScoped(CheckBrokerAgreement, brokerAgreementCheck)
}

// brokerAgreementCheck is the datatruth.ScopedCheckFn: it judges symbol across
// every started NT8 trader that trades it and says WHICH traders' views
// disagree - the block is for them, not for the symbol (m451).
func brokerAgreementCheck(symbol string, now time.Time) (bool, string, []string, error) {
	root := instrumentRoot(market.Normalize(symbol))
	var traders []*AutoTrader
	agreementTraders.Range(func(_, v any) bool {
		if at, ok := v.(*AutoTrader); ok && instrumentRoot(market.Normalize(at.futuresSymbol())) == root {
			traders = append(traders, at)
		}
		return true
	})
	if len(traders) == 0 {
		if !kernel.IsCMEOpen(now) {
			return false, "", nil, nil
		}
		return false, "", nil, fmt.Errorf("no started NT8 trader is registered for %s - the bot's view of the account cannot be read", symbol)
	}
	sort.Slice(traders, func(i, j int) bool { return traders[i].id < traders[j].id })
	var parts, scopes []string
	for _, at := range traders {
		blocking, _, err := at.brokerAgreementVerdict(now)
		if err != nil {
			// This trader cannot compare its two views: that is ITS finding, a block for
			// it alone (fail closed for the one whose picture is unknown).
			parts = append(parts, fmt.Sprintf("trader %s: cannot judge (fail closed): %v", at.id, err))
			scopes = append(scopes, datatruth.ScopeTrader(at.id))
			continue
		}
		for _, d := range blocking {
			parts = append(parts, fmt.Sprintf("trader %s: %s", at.id, d.Detail))
		}
		if len(blocking) > 0 {
			scopes = append(scopes, datatruth.ScopeTrader(at.id))
		}
	}
	if len(parts) == 0 {
		return false, "", nil, nil
	}
	return true, strings.Join(parts, "; "), scopes, nil
}

// brokerAgreementRegistered: this trader is under the check. A trader that is
// not (never registered, or already stopped) has a broker view nobody is
// comparing, and the scoped verdicts of the OTHER traders say nothing about it,
// so while the CME is open it cannot be admitted on the check's silence (m451).
func (at *AutoTrader) brokerAgreementRegistered() bool {
	cur, ok := agreementTraders.Load(at.id)
	return ok && cur == any(at)
}
