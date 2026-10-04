package ninjatrader

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
)

// ── W-EXEC-TRUTH W0 (b) — ONE ENTRY LATCH ────────────────────────────────────
//
// Three producers reach NT8 (the AI decision, the armed/planner path, Picture)
// plus three side doors (agent chat, the debug test trade, the test-arm seam),
// and before this latch nothing serialized them: each read its OWN evidence
// (store positions, the book, its own ledger) and B3's keys never collide
// across paths, so two producers could send two entries for one account and
// symbol inside the window before either fill was visible to the other.
//
// The latch sits inside the three entry functions (the Picture market entry
// was retired in W5), AFTER the maintenance permit
// and BEFORE the B3 guard, and holds ONE mutex per server per
// account|wire-symbol across the ledger stamp (beforeSend) and the send. It
// refuses when ANY of these is true:
//
//	book         the order book is stale/absent (unverifiable), or shows a
//	             working non-protective entry, or the account holds a position
//	             on the symbol (the injected Book source; one definition with
//	             the armed path's oneContractGuard)
//	ledger       either ledger holds a PLACED non-terminal row on the account
//	             and symbol (the injected Ledgers source)
//	queued       this broker has an entry sent but not yet filled/rejected
//	             (TCPTrader.pending — the AI path has no ledger)
//	recent_send  an entry was sent on this account|symbol within 60 s
//
// The caller's own row is never in a counted state when the latch runs: an arm
// is still 'armed' (its stamp is in beforeSend, after the latch) and a Picture
// claim is place_pending with no submission stamp. No exclusion by path or
// kind exists (CTO Q9 i).
//
// UNWIRED = allow (every standalone fixture), which is why the production
// wiring is pinned by test and the boot line READS latch=wired|UNWIRED (CTO Q9 ii).

// ErrEntryLatched marks a refusal by the entry latch. It is raised BEFORE the
// ledger stamp and the send, so it is provably unsent.
var ErrEntryLatched = errors.New("one entry latch")

// IsEntryLatched reports whether err is an entry-latch refusal.
func IsEntryLatched(err error) bool { return errors.Is(err, ErrEntryLatched) }

// EntryLatchRecentWindow is how long a sent entry blocks the next one on the
// same account|symbol.
const EntryLatchRecentWindow = 60 * time.Second

// EntryLatchBookVerdict is the injected book/position adjudication.
type EntryLatchBookVerdict struct {
	Verifiable bool   // false: stale/absent book or unreadable positions → refuse
	Live       bool   // a working non-protective entry, or a position on the symbol
	Detail     string // what was seen (order id / age / position count), for the refusal
}

// EntryLatchSource is the store/book-side evidence the latch cannot read from
// inside the broker package (the ≤60 s book law and both ledgers live in
// package trader, which imports this one). Book and Ledgers take the CURRENT
// entry group's sibling signal ids, so the latch treats the two legs of ONE
// split entry as one entry (DS-103 split legs). nil siblings = today's
// behaviour (no exemption).
type EntryLatchSource struct {
	Book    func(now time.Time, siblings map[string]bool) EntryLatchBookVerdict
	Ledgers func(siblings map[string]bool) (ids []string, err error)
	Now     func() time.Time
}

// latchSend is the last-send stamp per key: the time AND the entry group that
// send belonged to, so a split leg's second send is not refused as a "recent
// send" (DS-103: one acquisition covers the group).
type latchSend struct {
	at    time.Time
	group string
}

// latchState is one server's latch: a mutex and a last-send stamp per key.
type latchState struct {
	mu    sync.Mutex
	keys  map[string]*sync.Mutex
	sent  map[string]latchSend
	lastR map[string]string // last refusal reason per key (dedupes the WARN)
}

var latches sync.Map // *ntwire.TCPServer → *latchState

func latchFor(s *ntwire.TCPServer) *latchState {
	v, _ := latches.LoadOrStore(s, &latchState{keys: map[string]*sync.Mutex{}, sent: map[string]latchSend{}, lastR: map[string]string{}})
	return v.(*latchState)
}

func (ls *latchState) keyLock(key string) *sync.Mutex {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	m, ok := ls.keys[key]
	if !ok {
		m = &sync.Mutex{}
		ls.keys[key] = m
	}
	return m
}

// SetEntryLatchSource installs the latch's evidence (production: wireNT8EntryLatch).
func (t *TCPTrader) SetEntryLatchSource(src *EntryLatchSource) {
	t.mu.Lock()
	t.latchSource = src
	t.mu.Unlock()
}

// EntryLatchWired reports whether the latch has its evidence source — READ by
// the boot line, never asserted.
func (t *TCPTrader) EntryLatchWired() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latchSource != nil && t.latchSource.Book != nil && t.latchSource.Ledgers != nil
}

func (t *TCPTrader) latchKey() string {
	return strings.ToUpper(strings.TrimSpace(t.boundAccount)) + "|" + strings.ToUpper(strings.TrimSpace(t.symbol))
}

// acquireEntryLatch takes the latch for this account|symbol, or refuses. The
// returned done(sent) MUST be called exactly once; sent=true stamps the key so
// the next entry within EntryLatchRecentWindow refuses.
func (t *TCPTrader) acquireEntryLatch(what string) (done func(sent bool), err error) {
	t.mu.Lock()
	src := t.latchSource
	tid := t.traderID
	group := t.latchGroup
	siblings := t.latchSiblings
	t.mu.Unlock()
	if src == nil || src.Book == nil || src.Ledgers == nil {
		return func(bool) {}, nil // UNWIRED — the boot line says so
	}
	now := time.Now
	if src.Now != nil {
		now = src.Now
	}
	ls := latchFor(t.server)
	key := t.latchKey()
	m := ls.keyLock(key)
	m.Lock()
	refuse := func(reason, detail string) (func(bool), error) {
		m.Unlock()
		telemetry.IncGateBlock(tid, "one_entry_latch:"+reason)
		ls.mu.Lock()
		changed := ls.lastR[key] != reason+"|"+detail
		ls.lastR[key] = reason + "|" + detail
		ls.mu.Unlock()
		if changed {
			logger.Warnf("🚦 one_entry_latch: refusing %s on %s — %s: %s", what, key, reason, detail)
		}
		return nil, fmt.Errorf("ninjatrader/tcp: refusing %s: %w: one_entry_latch:%s — %s", what, ErrEntryLatched, reason, detail)
	}
	at := now()
	bv := src.Book(at, siblings)
	if !bv.Verifiable {
		return refuse("book_unverifiable", bv.Detail)
	}
	if bv.Live {
		return refuse("working_entry_or_position", bv.Detail)
	}
	ids, lerr := src.Ledgers(siblings)
	if lerr != nil {
		return refuse("ledger_unreadable", lerr.Error())
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		return refuse("ledger_open", "placed non-terminal row(s) "+strings.Join(ids, ", "))
	}
	t.pendingMu.Lock()
	var queued []string
	for sid := range t.pending {
		if siblings != nil && siblings[sid] {
			continue // DS-103: a sibling leg's own pending entry is this entry
		}
		queued = append(queued, sid)
	}
	t.pendingMu.Unlock()
	if len(queued) > 0 {
		sort.Strings(queued)
		return refuse("queued_entry", "entry sent, not yet filled or rejected: "+strings.Join(queued, ", "))
	}
	ls.mu.Lock()
	last, seen := ls.sent[key]
	ls.mu.Unlock()
	if seen && at.Sub(last.at) < EntryLatchRecentWindow && at.Sub(last.at) >= 0 &&
		!(group != "" && last.group == group) {
		return refuse("recent_send", fmt.Sprintf("an entry was sent on %s %s ago (window %s)", key, at.Sub(last.at).Round(time.Second), EntryLatchRecentWindow))
	}
	ls.mu.Lock()
	delete(ls.lastR, key)
	ls.mu.Unlock()
	return func(sent bool) {
		if sent {
			ls.mu.Lock()
			ls.sent[key] = latchSend{at: now(), group: group}
			ls.mu.Unlock()
		}
		m.Unlock()
	}, nil
}

// SetLatchGroup marks the NEXT latch acquisition as one leg of an entry group
// (DS-103 split legs): group is the shared entry-group id and siblings are the
// OTHER legs' signal ids. The latch then exempts those siblings from the
// queued / ledger / book / recent-send checks, so the two legs of ONE mentor
// intent read as ONE entry. ClearLatchGroup restores today's behaviour.
func (t *TCPTrader) SetLatchGroup(group string, siblings map[string]bool) {
	t.mu.Lock()
	t.latchGroup = group
	t.latchSiblings = siblings
	t.mu.Unlock()
}

// ClearLatchGroup clears the entry-group hint set by SetLatchGroup.
func (t *TCPTrader) ClearLatchGroup() {
	t.mu.Lock()
	t.latchGroup = ""
	t.latchSiblings = nil
	t.mu.Unlock()
}

// sendAttempted reports whether a SendSignal outcome may have put the entry on
// the wire: a success, a queued send, or an ambiguous failure all count; only
// the hold's provably-unsent drop does not.
func sendAttempted(err error) bool {
	return err == nil || !errors.Is(err, ntwire.ErrEntryHeld)
}
