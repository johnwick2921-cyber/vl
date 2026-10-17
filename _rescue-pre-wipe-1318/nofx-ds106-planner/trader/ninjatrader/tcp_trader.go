// Package ninjatrader — alternative Trader implementation that emits signals
// through the Plan 1.5 TCP bridge instead of the Plan 1 CSV bridge.
//
// SEPARATE TYPE from the existing CSV `Trader` (per ADR-007: additive, not a
// modification). Implements the same 19-method `trader/types.Trader`
// interface. Selection is via NT_TRANSPORT env var (transport.go).
//
// Wire-protocol contract: provider/ninjatrader/tcp_framing.go + tcp_server.go.
// Tick rounding: shared with the CSV trader via in-package call to
// RoundToTick / InstrumentTickSize (tick_rounding.go).
package ninjatrader

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"nofx/config"
	"nofx/discipline"
	"nofx/logger"
	"nofx/market"
	"nofx/provider/databento"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
	"nofx/trader/types"
) // reflect used in GetBalance to notify parent AutoTrader

// TCPTrader satisfies trader/types.Trader using the TCP bridge.
type TCPTrader struct {
	server *ntwire.TCPServer
	symbol string // for tick rounding selection
	// boundAccount (P5.4): the NT8 sub-account this trader trades on
	// (store.Trader.Account). Empty = the AddOn's active account (legacy).
	// When set, signals carry it and the AddOn's Phase-3 routing submits there.
	boundAccount string

	// traderID (A2/G1): the OWNING trader's id, stamped on every order/modify/cancel
	// frame so the AddOn can echo it back for identity verification. Set at
	// StartCloseSync (which already receives it). Empty until then.
	traderID string

	mu       sync.Mutex
	stopLoss map[string]float64 // key: "<symbol>:<side>"
	takePrft map[string]float64
	guard    *orderGuard // B3: dupe-drop + rate breaker at the order-submission chokepoint
	// openOrdersSrc (class 33) — the ledger-backed working-order source for
	// GetOpenOrders (flat-gate leg 4). nil = unwired = the leg FAILS.
	openOrdersSrc func(symbol string) ([]types.OpenOrder, error)
	rejectSink    func(signalID, brokerReason string)
	lastFill      ntwire.FillPayload
	hasFill       bool

	// recentFills is a bounded ring of the last confirmed fills (class 27,
	// 2026-08-31). A NETTING close emits no position_close frame — the only
	// evidence of the real exit is the opposite-side fill that flattened the
	// account (the NT8 Add→Flat pair). The reconcile orphan-close consults this
	// ring to reconstruct the real exit price instead of fabricating exit=entry.
	// Guarded by mu. Reconcile goroutine reads via takeNettingExit.
	recentFills []recentFill
	// recentRejects is the bounded ring of broker rejections by signal
	// (W-EXEC-TRUTH W0 (d), CLASS 160). Guarded by mu.
	recentRejects []recentReject
	// latchSource is the W0 (b) entry latch's evidence (nil = UNWIRED = allow;
	// production wiring is wireNT8EntryLatch, pinned). Guarded by mu.
	latchSource *EntryLatchSource

	// closedAt records the wall-clock (ms) of the most recent FILL-CONFIRMED close
	// (position_close frame) per "SYMBOL|SIDE" for THIS trader's bound account. The
	// reconcile-before-open gate awaits this event — which arrives ~instantly on the
	// flatten fill for the bound account REGARDLESS of which account is the streamed
	// "active" one — instead of the positions snapshot, whose non-active-account
	// refresh is only the 30s heartbeat (the 6s-vs-30s starvation). Guarded by mu.
	closedAt map[string]int64

	// lastEntrySignalID is the signal_id of the most recent ENTRY (open). It lets
	// GetOrderStatus report ONLY the fill that belongs to the current entry (the
	// fill frame echoes signal_id), so recordAndConfirmOrder records the real
	// NT8 AverageFillPrice as entry_price instead of the stale 5m-mark reference.
	// Cleared on close so a close-path poll never matches the entry fill.
	lastEntrySignalID string

	// st is the store handle wired at StartPositionReconcile (GAR-F1). It lets
	// MoveStopToBreakeven resolve a MATERIALIZED position's persisted entry
	// order identity when no in-process entry signal exists — the #566 class
	// where every move_stop send failed "no open entry to move the stop".
	st *store.Store

	// entryOrderID caches SYMBOL|SIDE → entry order/signal identity learned at
	// reconcile materialization (GAR-F1) so move_stop/trailing can address
	// positions that were never placed through a Go-side signal. Guarded by mu.
	entryOrderID map[string]string

	// pending tracks signal_id → side so we can correlate fills back to
	// position state. Optional: not strictly needed for the 19-method
	// interface, but cheap insurance.
	pendingMu sync.Mutex
	pending   map[string]string // signal_id -> side
	// C8 (2026-08-25) — pendingAt stamps each pending entry's submit time (Unix
	// ms) so the reconcile sweep can drop entries NT8 never confirmed (rejected
	// silently / AddOn drop) instead of letting them linger as would-be
	// positions. Same pendingMu.
	pendingAt map[string]int64

	// closeSyncOnce guards StartCloseSync so a re-entrant AutoTrader.Run never
	// spawns a second consumer racing on the single ClosedPositions() channel.
	closeSyncOnce sync.Once

	// reconcileOnce guards StartPositionReconcile (mirrors closeSyncOnce) so a
	// re-entrant AutoTrader.Run never spawns a second reconcile goroutine.
	reconcileOnce sync.Once

	// flatSince tracks, per open-position row id, the first time the reconcile loop
	// observed it NT8-flat-but-DB-open (Unix ms). It implements the flat-grace
	// window: reconcile defers orphan-closing a freshly-flat row so close-sync's
	// position_close frame can arrive and record the real ×pv P&L first. Accessed
	// ONLY from the single reconcile goroutine (reconcilePositions) → no lock needed.
	flatSince map[int64]int64

	// divergeSince (A4/G4) tracks, per open-position row id, the first time reconcile
	// observed a qty divergence (NT8 held ≠ DB belief). It debounces the belief≠broker
	// freeze so an in-flight fill doesn't false-freeze. Reconcile goroutine only → no lock.
	divergeSince map[int64]int64

	// untrackedSince tracks, per "SYMBOL|SIDE" key, the first time reconcile
	// observed NT8 HOLDING a position for which this trader has NO open row
	// (a manual NT8 entry, or an entry whose fill was never recorded). It
	// debounces materializing an OPEN row (Source="reconcile") so a bot-opened
	// row — which lands within seconds — is never double-created, while a
	// genuinely untracked position becomes trackable (and its later close
	// records real P&L instead of being dropped). Reconcile goroutine only.
	untrackedSince map[string]int64

	// Plan 4 Stage 4 — reference to the parent AutoTrader (optional).
	// Used to notify the AutoTrader when the first account_balance frame arrives.
	// Set by transport.go after creating the trader.
	parentAutoTrader interface{} // *AutoTrader (avoid circular import)

	// entryPermit (W-ONE-BUTTON M2 site 4) — the installation-wide maintenance
	// permit, taken by the FOUR entry functions only, after the SIM rail and
	// before the B3 guard, and held across the wire write. nil = allow (a
	// standalone TCPTrader, every pre-existing fixture); the AutoTrader wires
	// it at construction (NewAutoTrader) — pinned by a wiring test.
	entryPermit func() (func(), bool)
}

// ErrMaintenanceHold marks an entry refused because the installation is under
// maintenance (W-ONE-BUTTON). Callers classify it with errors.Is — like
// ntwire.ErrAddonBuildTooOld, it is a policy refusal, not a broker failure.
var ErrMaintenanceHold = errors.New("maintenance hold: new entries are refused while the installation is being updated")

// IsMaintenanceHold reports whether err is a maintenance-hold refusal: the
// permit refused the entry (ErrMaintenanceHold) or the queue dropped it
// because the hold landed before the flush (ntwire.ErrEntryHeld). Either way
// the entry did NOT reach NT8.
func IsMaintenanceHold(err error) bool {
	return errors.Is(err, ErrMaintenanceHold) || errors.Is(err, ntwire.ErrEntryHeld)
}

// SetEntryHoldCheck forwards the maintenance predicate to this trader's TCP
// server, whose queue drops held entries on flush (gap U2).
func (t *TCPTrader) SetEntryHoldCheck(fn func() bool) {
	if t.server != nil {
		t.server.SetEntryHoldCheck(fn)
	}
}

// SetMaintenanceSource forwards the installation hold to this trader's TCP
// server, which pushes it to the AddOn as the maintenance frame (M2 site 7).
func (t *TCPTrader) SetMaintenanceSource(fn func() (bool, string)) {
	if t.server != nil {
		t.server.SetMaintenanceSource(fn)
	}
}

// MaintenanceView is what the installation gate reads from this trader's
// wire: the current connection's record, whether it is still the connected
// client, and the queued-signal depth. ok=false without a server.
func (t *TCPTrader) MaintenanceView() (rec ntwire.ConnectionRecord, connected bool, queued int, ok bool) {
	if t.server == nil {
		return ntwire.ConnectionRecord{}, false, 0, false
	}
	rec, connected = t.server.ConnectionRecord()
	return rec, connected, t.server.PendingSignalCount(), true
}

// SetDroppedEntrySink registers, under owner (the AutoTrader id), this trader's handler for ITS OWN queued
// entries the maintenance hold dropped (W-ONE-BUTTON M2, M-2). Ownership is
// exact: every entry function registers its signal in t.pending before the
// send. A never-attempted drop provably never reached NT8, so the trader
// forgets it (no fill can come, and a stale lastEntrySignalID must not claim a
// later fill); an ATTEMPTED one may have reached NT8, so it stays tracked.
func (t *TCPTrader) SetDroppedEntrySink(owner string, fn func(ntwire.DroppedEntry)) {
	if t.server == nil {
		return
	}
	// Keyed by the OWNING TRADER's id (M2.1): a reloaded trader replaces its
	// old sink rather than leaving it registered for the life of the process.
	key := "trader:" + strings.TrimSpace(owner)
	if strings.TrimSpace(owner) == "" {
		key = fmt.Sprintf("tcptrader:%p", t) // no owner (standalone): per instance
	}
	t.server.AddDroppedEntrySink(key, func(d ntwire.DroppedEntry) {
		if !t.HasPendingEntry(d.SignalID) {
			return
		}
		if !d.Attempted {
			t.forgetUnsentEntry(d.SignalID)
		}
		if fn != nil {
			fn(d)
		}
	})
}

// HasPendingEntry reports whether this trader still tracks signalID as an
// entry awaiting its fill.
func (t *TCPTrader) HasPendingEntry(signalID string) bool {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	_, ok := t.pending[signalID]
	return ok
}

func (t *TCPTrader) forgetUnsentEntry(signalID string) {
	t.pendingMu.Lock()
	delete(t.pending, signalID)
	delete(t.pendingAt, signalID)
	t.pendingMu.Unlock()
	t.mu.Lock()
	if t.lastEntrySignalID == signalID {
		t.lastEntrySignalID = ""
	}
	t.mu.Unlock()
}

// SetEntryPermit installs the maintenance permit. Only entry sends take it:
// PlaceProtectiveStop, CancelOrder, ModifyBracket, MoveStopToBreakeven and the
// close paths never do (CTO correction C1).
func (t *TCPTrader) SetEntryPermit(fn func() (func(), bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entryPermit = fn
}

// acquireEntryPermit returns the release to defer across the send, or an
// ErrMaintenanceHold refusal. Unwired = allow.
func (t *TCPTrader) acquireEntryPermit(what string) (func(), error) {
	t.mu.Lock()
	fn := t.entryPermit
	t.mu.Unlock()
	if fn == nil {
		return func() {}, nil
	}
	release, ok := fn()
	if !ok {
		return nil, fmt.Errorf("ninjatrader/tcp: refusing %s: %w", what, ErrMaintenanceHold)
	}
	if release == nil {
		release = func() {}
	}
	return release, nil
}

// Compile-time interface guard (ADR-007 pattern — mirrors CSV Trader).
var _ types.Trader = (*TCPTrader)(nil)

// NewTCPTrader wraps an already-Started TCPServer with the Trader interface.
// The server's lifecycle is owned by the caller (transport.go).
func NewTCPTrader(server *ntwire.TCPServer, symbol string, account ...string) *TCPTrader {
	t := &TCPTrader{
		server:    server,
		symbol:    symbol,
		stopLoss:  map[string]float64{},
		takePrft:  map[string]float64{},
		guard:     newOrderGuard(),
		pending:   map[string]string{},
		pendingAt: map[string]int64{},
	}
	if len(account) > 0 {
		t.boundAccount = strings.TrimSpace(account[0])
	}
	if t.boundAccount != "" {
		logger.Infof("🔗 ninjatrader/tcp: trader %s BOUND to account %s (P5.4 per-(symbol,account) routing)", symbol, t.boundAccount)
		// A3 (G2) — declare this bound account to the server's allowlist so the AddOn
		// enforces execution against an owner-declared set (re-sent to the AddOn on
		// connect + on this bind), independent of any signal payload.
		server.RegisterBoundAccount(t.boundAccount)
	}
	// Phase 2 — drive the bar subscription from THIS trader's symbol instead of
	// the hardwired default "MNQ". On the next (re)connect the AddOn subscribes to
	// this root and NT8 resolves the qualified front-month (VLContractResolver,
	// quarterly families). MNQ is unchanged (it is just the symbol here too).
	server.SetBarsSubscribeSymbol(symbol)
	// P5.2 — extra bar-subscription roots (bars-only until P5.4) are wired by
	// transport.go AFTER this constructor: the config symbols-list REPLACES the
	// extras (source of truth per (re)load), then the NT_EXTRA_SYMBOLS testing
	// override appends. Both dedup against the real primary set above.
	// Subscribe to inbound fills — update lastFill cache (mirrors CSV Trader).
	// Install ownership before returning a trader that can send entries.
	fills := server.SubscribeFillsFor(symbol, t.boundAccount)
	go func() {
		// P5.4 — router-fed per-symbol stream (no cross-trader racing). The
		// channel CLOSES when a reloaded trader re-subscribes, ending this
		// goroutine (fixes the pre-P5.4 reload leak).
		for fill := range fills {
			// P5.2 split-brain defense: a symbol-tagged fill for a DIFFERENT
			// instrument must never be attributed to this trader. Empty symbol
			// = legacy (pre-P5.2) AddOn → assumed primary (back-compat).
			if fill.Symbol != "" && !equalSymbol(fill.Symbol, symbol) {
				logger.Warnf("⚠️ ninjatrader/tcp: REJECTED fill for symbol %q (this trader trades %q) — signal_id=%s (split-brain defense)",
					fill.Symbol, symbol, fill.SignalID)
				continue
			}
			// H3 account guard (defense in depth on top of the (symbol,account)
			// routing): with two same-symbol traders, NEVER cache another account's
			// fill as this trader's position. Empty account = legacy AddOn → trust
			// the symbol routing.
			if fill.Account != "" && t.boundAccount != "" && !strings.EqualFold(fill.Account, t.boundAccount) {
				// A4 (G4) — a fill on an account this trader is NOT bound to reached it:
				// the (symbol,account) routing should make this impossible, so a fire is
				// a real cross-account event. Reject the fill AND FREEZE the trader (new
				// entries blocked until the owner clears; open-position management goes on).
				t.mu.Lock()
				tid := t.traderID
				t.mu.Unlock()
				if discipline.FreezeTrader(tid, "post-fill account mismatch: fill account "+fill.Account+" ≠ bound "+t.boundAccount, time.Now().UnixMilli()) {
					logger.Errorf("🚨 A4 FREEZE: fill for account %q reached trader bound to %q — signal_id=%s (post-fill account guard). Trader FROZEN.",
						fill.Account, t.boundAccount, fill.SignalID)
				}
				continue
			}
			// C8 (2026-08-25) — a REJECTED entry must never become the
			// fill-derived position (the phantom-position class). NT8 truth: NO
			// position exists. Drop the pending marker, clear any cached fill for
			// that signal, and alarm.
			if strings.EqualFold(fill.Status, "rejected") {
				t.pendingMu.Lock()
				if fill.SignalID != "" {
					delete(t.pending, fill.SignalID)
					delete(t.pendingAt, fill.SignalID)
				}
				t.pendingMu.Unlock()
				t.mu.Lock()
				if t.lastFill.SignalID == fill.SignalID {
					t.lastFill = ntwire.FillPayload{}
					t.hasFill = false
				}
				if t.lastEntrySignalID == fill.SignalID {
					t.lastEntrySignalID = ""
				}
				t.recordRecentReject(fill.SignalID, fill.Reason)
				tid := t.traderID
				t.mu.Unlock()
				t.notifyReject(fill.SignalID, fill.Reason)
				reason := fill.Reason
				if strings.TrimSpace(reason) == "" {
					reason = store.PlacementReasonUnavailable
				}
				logger.Errorf("🚨 C8 ENTRY REJECTED by NT8: %s %s qty=%d signal_id=%s reason=%q — no position exists; pending entry dropped (no phantom).",
					fill.Symbol, fill.Side, fill.Quantity, fill.SignalID, reason)
				telemetry.RecordError(tid, "nt_entry_rejected", fmt.Sprintf("%s %s rejected (signal %s)", fill.Symbol, fill.Side, fill.SignalID), telemetry.CostNone)
				continue
			}
			// A CONFIRMED fill resolves its pending entry marker.
			t.pendingMu.Lock()
			if fill.SignalID != "" {
				delete(t.pending, fill.SignalID)
				delete(t.pendingAt, fill.SignalID)
			}
			t.pendingMu.Unlock()
			t.mu.Lock()
			t.lastFill = fill
			t.hasFill = true
			// Class 27 (2026-08-31): retain confirmed fills in the netting ring
			// so reconcile can reconstruct a netting-close's real exit price.
			if strings.EqualFold(fill.Status, "filled") || strings.EqualFold(fill.Status, "partial") {
				t.recordRecentFill(fill)
			}
			t.mu.Unlock()
		}
	}()
	return t
}

// --- Trader interface methods (19 total) ---

func (t *TCPTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.placeEntry(symbol, "long", quantity)
}

func (t *TCPTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.placeEntry(symbol, "short", quantity)
}

// activeAccountName is the NT8 account the connection will trade (the streamed
// current account). "" before the first account frame.
func (t *TCPTrader) activeAccountName() string {
	if a := t.server.CurrentAccount(); a != nil {
		return *a
	}
	return ""
}

// FarSideProves reports whether the AddOn on the wire has proven a capability
// floor (FarSideProven over the build id reported on the heartbeat). Capability
// is proven by RECEIPT, never assumed — no build id means no.
func (t *TCPTrader) FarSideProves(minBuild string) bool {
	if t == nil || t.server == nil {
		return false
	}
	return ntwire.FarSideProven(t.server.FarSideBuildID(), minBuild)
}

// OrderSnapshotLookup returns the broker's own book entry for a signal id from
// the latest RECEIVED order snapshot (entry legs carry the plain signal id;
// protective legs carry -sl/-tp suffixes). WIRE SEMANTICS (picture-htf round,
// 2026-09-20): the AddOn EXCLUDES terminal orders from the snapshot
// (VLTraderTCPClient.cs SendOrderSnapshot skips Filled/Cancelled/Rejected/
// Expired), so this lookup can prove an order is WORKING but can never prove
// a terminal outcome, and NT8Order carries no fill price (limit_price is the
// order's limit). Terminal recovery must use received execution/order
// history — never this book.
func (t *TCPTrader) OrderSnapshotLookup(signalID string) (ntwire.NT8Order, bool) {
	var zero ntwire.NT8Order
	if t == nil || t.server == nil || signalID == "" {
		return zero, false
	}
	snap, ok := t.server.OrderSnapshots().Latest(t.boundAccount)
	if !ok {
		return zero, false
	}
	for _, o := range snap.Orders {
		if o.Name == signalID {
			return o, true
		}
	}
	return zero, false
}

// feedNowUTC is the latest market bar close, falling back to wall time when
// absent. It is a market fact, never the creation timestamp of an entry command.
func (t *TCPTrader) feedNowUTC(symbol string) time.Time {
	if t.server != nil {
		for _, tf := range []string{"1m", "5m"} {
			bars := t.server.BarCache().Get(symbol, tf)
			if len(bars) > 0 {
				open := time.UnixMilli(bars[len(bars)-1].T).UTC()
				return open.Add(time.Duration(timeframeDurationMs(tf)) * time.Millisecond)
			}
		}
	}
	return time.Now().UTC()
}

// isAccountTradeable reports whether an order may be sent for `name`: it must be a
// SIM account (per the C#-reported accounts list, which uses Account.Simulation) AND,
// if NT_ALLOWED_ACCOUNTS is configured, on that allow-list. This is the hard
// live/funded-account block (Stage-2 Phase-1). Fail-safe: unknown account → false.
func (t *TCPTrader) isAccountTradeable(name string) bool {
	if name == "" {
		return false
	}
	accts, _ := t.server.GetAccountsList()
	isSim := false
	found := false
	for _, a := range accts {
		if a.Name == name {
			isSim = a.IsSim
			found = true
			break
		}
	}
	if !found || !isSim {
		return false // unknown or non-SIM (incl. the live account) → never tradeable
	}
	if allow := config.Get().AllowedNTAccounts; len(allow) > 0 {
		for _, a := range allow {
			if a == name {
				return true
			}
		}
		return false // an allow-list is set and this account isn't on it
	}
	return true
}

// entryBracket is a market entry's OWN stop and target, carried INTO the send
// (W1b FOLD-3) instead of through the shared (symbol, side) maps.
type entryBracket struct{ stop, target float64 }

// OpenWithBracket places a market entry that carries its OWN bracket (W1b
// FOLD-3): the same send as OpenLong/OpenShort (every rail, the one rounding),
// except the stop and target ride in as arguments, and the shared (symbol,
// side) SL/TP maps — which MoveStopToBreakeven's widen ban reads as the live
// stop — learn them ONLY once the entry may be on the wire (sendAttempted, the
// rule B3 and the latch record by). A refusal before the send (bound account,
// SIM, permit, latch, B3, the bracket itself) and the hold's provably-unsent
// drop leave the maps byte-identical. An ambiguous send failure keeps the
// bracket (fail-closed: the entry may be live carrying exactly that stop).
// Not a Trader interface method; the NT8 open path reaches it by assertion.
func (t *TCPTrader) OpenWithBracket(symbol, side string, quantity, stop, target float64) (map[string]interface{}, error) {
	return t.placeEntryWith(symbol, side, quantity, &entryBracket{stop: stop, target: target})
}

// placeEntry is the legacy market entry: its bracket is whatever
// SetStopLoss/SetTakeProfit last wrote to the maps.
func (t *TCPTrader) placeEntry(symbol, side string, quantity float64) (map[string]interface{}, error) {
	return t.placeEntryWith(symbol, side, quantity, nil)
}

func (t *TCPTrader) placeEntryWith(symbol, side string, quantity float64, own *entryBracket) (map[string]interface{}, error) {
	// SAFETY RAIL (Stage-2 Phase-1, defense-in-depth): never SEND an entry for an
	// account that isn't tradeable (SIM + allow-listed). The C# AddOn enforces this
	// again right before submit; this refuses in Go before the frame is even sent.
	// The LIVE/funded account is never tradeable.
	// MULTI-ACCOUNT SAFETY: an UNBOUND trader (empty boundAccount) must NOT borrow
	// the connection's shared active account — that would execute on whatever account
	// was last globally selected (i.e. ANOTHER trader's account). Refuse outright,
	// replacing the old activeAccountName() fallback.
	tradeAcct := t.boundAccount
	if tradeAcct == "" {
		return nil, fmt.Errorf("ninjatrader/tcp: refusing %s entry on %s — trader has no bound account (select an account first); NOT falling back to the shared active account", side, symbol)
	}
	if !t.isAccountTradeable(tradeAcct) {
		return nil, fmt.Errorf("ninjatrader/tcp: refusing %s entry — account %q is not tradeable (not on allow-list / not SIM)", side, tradeAcct)
	}
	// W-ONE-BUTTON M2 site 4 — maintenance permit, before B3 (a refusal must
	// not consume the dedupe slot) and before any pending/lastEntrySignalID
	// write; held until SendSignal returns.
	releasePermit, err := t.acquireEntryPermit(side + " entry on " + symbol)
	if err != nil {
		return nil, err
	}
	defer releasePermit()
	// W-EXEC-TRUTH W0 (b) — the ONE entry latch: after the permit, BEFORE B3
	// (a latch refusal never consumes the dedupe slot), held across the
	// ledger stamp and the send.
	latchDone, lerr := t.acquireEntryLatch("market entry " + side + " on " + symbol)
	if lerr != nil {
		return nil, lerr
	}
	latchSent := false
	defer func() { latchDone(latchSent) }()

	// B3 — dupe guard + rate limiter at the order-submission chokepoint: a
	// replayed / double-fired entry (same account|side|symbol|qty within a bar) is
	// DROPPED, and an order runaway trips the per-minute breaker.
	b3Done, reason, ok := t.b3Reserve(fmt.Sprintf("%s|%s|%s|%.0f", tradeAcct, upperSideStr(side), symbol, quantity))
	if !ok {
		return nil, fmt.Errorf("ninjatrader/tcp: entry not admitted — %s", reason)
	}
	b3Sent := false // W1b E12(b): the slot is recorded ONLY by an attempted send
	defer func() { b3Done(b3Sent) }()

	// Expiry warning — defense in depth (mirrors CSV Trader).
	if days := databento.DaysUntilExpiry(symbol, time.Now()); days >= 0 && days <= 5 {
		logger.Warnf("ninjatrader/tcp: placing %s entry on %s within %d days of expiry — verify contract roll", side, symbol, days)
	}

	// CSV Trader keys SL/TP by "LONG"/"SHORT" uppercase; mirror that here.
	upperSide := upperSideStr(side)
	t.mu.Lock()
	sl := t.stopLoss[keyFor(symbol, upperSide)]
	tp := t.takePrft[keyFor(symbol, upperSide)]
	if own != nil { // W1b FOLD-3: the entry's own bracket, never the maps'
		sl, tp = own.stop, own.target
	}
	tid := t.traderID // A2 (G1) — captured under lock (set-once at StartCloseSync)
	t.mu.Unlock()
	if sl == 0 || tp == 0 {
		return nil, fmt.Errorf("ninjatrader/tcp: SetStopLoss and SetTakeProfit must be called before %s", side)
	}

	// Mirror CSV Trader: entry price for the wire is a reference value
	// computed as SL/TP midpoint. NT places market orders; entry on the
	// wire is for the AddOn's logging + protective bracket reference.
	entryRef := (sl + tp) / 2.0

	// W1b E12(a) — WireBracket: entry/target nearest tick, the stop AWAY from
	// the entry (never inside the floor the gate approved). The gate judges
	// the SAME function's output. An unknown side refuses the send.
	tick := InstrumentTickSize(t.symbol)
	entry, sl, tp, rerr := WireBracket(side, entryRef, sl, tp, tick)
	if rerr != nil {
		return nil, fmt.Errorf("ninjatrader/tcp: refusing %s entry on %s: %w", side, symbol, rerr)
	}

	signalID := uuid.NewString()
	payload := ntwire.SignalPayload{
		Symbol:     t.symbol,
		Account:    t.boundAccount, // P5.4 — empty = legacy (AddOn's active account)
		TraderID:   tid,            // A2 (G1) — identity stamp; server assigns Seq
		Side:       side,           // lowercase per spec L4390
		Quantity:   int(quantity),
		Entry:      entry,
		StopLoss:   sl,
		TakeProfit: tp,
		SignalID:   signalID,
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
	}

	// A1 (G3) — PRE-SUBMIT IDENTITY INVARIANT: the account on the outbound order MUST
	// equal this trader's bound account. It is equal by construction above, but assert
	// it at the send chokepoint so no present or future code path can ever emit an
	// order for another trader's account. A mismatch is REFUSED loudly, never sent.
	if err := assertBoundAccount("entry", symbol, payload.Account, t.boundAccount); err != nil {
		logger.Errorf("🚨 %v — REFUSING to submit", err)
		return nil, err
	}

	t.pendingMu.Lock()
	t.pending[signalID] = upperSide
	t.pendingAt[signalID] = time.Now().UTC().UnixMilli()
	t.pendingMu.Unlock()

	// Mark this as the current entry so GetOrderStatus reports its fill (and only
	// its fill) — the real NT8 AverageFillPrice becomes the recorded entry_price.
	t.mu.Lock()
	t.lastEntrySignalID = signalID
	t.mu.Unlock()

	serr := t.server.SendSignal(payload)
	latchSent = sendAttempted(serr)
	b3Sent = latchSent
	if own != nil && latchSent {
		// W1b FOLD-3 — the maps learn the bracket only now: the entry may be on
		// the wire carrying it (the AUTHORED values, as SetStopLoss stored them).
		t.mu.Lock()
		t.stopLoss[keyFor(symbol, upperSide)] = own.stop
		t.takePrft[keyFor(symbol, upperSide)] = own.target
		t.mu.Unlock()
	}
	if err := serr; err != nil {
		return nil, fmt.Errorf("ninjatrader/tcp: send signal: %w", err)
	}
	return map[string]interface{}{
		"status":    "submitted",
		"symbol":    symbol,
		"side":      side,
		"quantity":  quantity,
		"signal_id": signalID,
	}, nil
}

// PlaceLimitEntry (PHASE 2 armed orders) places a RESTING limit entry with its
// bracket prices — the armed-order engine's wire call. Same safety rails as
// placeEntry (bound account + SIM + B3 guard); the AddOn submits OrderType.Limit
// and defers SL/TP to SubmitBracketOnEntryFill, identical to market entries.
func (t *TCPTrader) PlaceLimitEntry(symbol, side string, quantity float64, limitPx, sl, tp float64, beforeSend ...func(string) error) (string, error) {
	tradeAcct := t.boundAccount
	if tradeAcct == "" {
		return "", fmt.Errorf("ninjatrader/tcp: refusing armed %s entry on %s — trader has no bound account", side, symbol)
	}
	if !t.isAccountTradeable(tradeAcct) {
		return "", fmt.Errorf("ninjatrader/tcp: refusing armed %s entry — account %q is not tradeable (not on allow-list / not SIM)", side, tradeAcct)
	}
	// W-ONE-BUTTON M2 site 4 — maintenance permit, before B3 and before
	// beforeSend (ledger.BeginPlacement); held across SendSignal.
	releasePermit, err := t.acquireEntryPermit("armed " + side + " limit entry on " + symbol)
	if err != nil {
		return "", err
	}
	defer releasePermit()
	// W-EXEC-TRUTH W0 (b) — the ONE entry latch: after the permit, BEFORE B3
	// (a latch refusal never consumes the dedupe slot), held across the
	// ledger stamp and the send.
	latchDone, lerr := t.acquireEntryLatch("armed limit " + side + " on " + symbol)
	if lerr != nil {
		return "", lerr
	}
	latchSent := false
	defer func() { latchDone(latchSent) }()
	b3Done, reason, ok := t.b3Reserve(fmt.Sprintf("armed|%s|%s|%s|%.0f", tradeAcct, upperSideStr(side), symbol, quantity))
	if !ok {
		return "", fmt.Errorf("ninjatrader/tcp: armed entry not admitted — B3 dupe/rate guard: %s", reason)
	}
	b3Sent := false // W1b E12(b): the slot is recorded ONLY by an attempted send
	defer func() { b3Done(b3Sent) }()
	tick := InstrumentTickSize(t.symbol)
	entry, sl, tp, rerr := WireBracket(side, limitPx, sl, tp, tick) // W1b E12(a): stop AWAY from entry
	if rerr != nil {
		return "", fmt.Errorf("ninjatrader/tcp: refusing armed %s entry on %s: %w", side, symbol, rerr)
	}
	tid := t.traderID
	signalID := uuid.NewString()
	payload := ntwire.SignalPayload{
		Symbol:     t.symbol,
		Account:    t.boundAccount,
		TraderID:   tid,
		Side:       side,
		Quantity:   int(quantity),
		Entry:      entry,
		StopLoss:   sl,
		TakeProfit: tp,
		SignalID:   signalID,
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
		OrderType:  "limit",
		LimitPrice: entry,
	}
	if err := assertBoundAccount("armed-entry", symbol, payload.Account, t.boundAccount); err != nil {
		logger.Errorf("🚨 %v — REFUSING to submit armed entry", err)
		return "", err
	}
	for _, register := range beforeSend {
		if err := register(signalID); err != nil {
			return "", fmt.Errorf("ninjatrader/tcp: register placement: %w", err)
		}
	}
	t.pendingMu.Lock()
	t.pending[signalID] = upperSideStr(side)
	t.pendingAt[signalID] = time.Now().UTC().UnixMilli()
	t.pendingMu.Unlock()
	t.mu.Lock()
	t.lastEntrySignalID = signalID
	t.mu.Unlock()
	serr := t.server.SendSignal(payload)
	latchSent = sendAttempted(serr)
	b3Sent = latchSent
	if err := serr; err != nil {
		return "", fmt.Errorf("ninjatrader/tcp: send armed signal: %w", err)
	}
	return signalID, nil
}

// PlaceStopEntry (E7, entry-mechanics 2026-08-30) places a STOP-MARKET entry
// (the breakout-retest fallback / breakdown immediate alternative) with the
// same bracket-on-fill contract as limits. stopPx is the TRIGGER price (the
// tick offset is applied by the caller). Back-compat law: the frame is
// additive JSON — only send it when the far-side AddOn has proven it.
func (t *TCPTrader) PlaceStopEntry(symbol, side string, quantity float64, stopPx, sl, tp float64, beforeSend ...func(string) error) (string, error) {
	// CAPABILITY HANDSHAKE — the far-side AddOn must PROVE, by a build_id that
	// arrived on the wire, that it will BUILD this order correctly. Two distinct
	// failures live behind this one gate:
	//   2026-08-30 — a pre-E7 AddOn executed the unknown frame as MARKET (the
	//     22:32 test filled at 29346.25 instead of resting at 28700).
	//   2026-09-05 (WAVE B / D1) — the E7..f12 AddOns parsed the frame and built
	//     an OrderType.StopMarket, but passed the trigger in CreateOrder's
	//     limitPrice argument with a literal 0 in stopPrice. NT8 accepted a
	//     zero-trigger stop and simply never worked it: 22 of 22 lifetime
	//     submissions, 0 fills, no reject, no error.
	// So the floor is MinAddonBuildStopSlot, not FarSideBuildE7. An AddOn that
	// only proves the parse is refused — never sent a frame it will mis-execute.
	if bid := t.server.FarSideBuildID(); !ntwire.FarSideProven(bid, ntwire.MinAddonBuildStopSlot) {
		return "", fmt.Errorf("ninjatrader/tcp: refusing stop-entry %s %s trigger=%.2f qty=%.0f [guard=far_side_build] — addon build predates the stop-slot fix (build_id=%s, need ≥ %s): does not prove stop_entry support; F5-compile + restart the new AddOn: %w",
			side, symbol, stopPx, quantity, ntwire.BuildIDForLog(bid), ntwire.MinAddonBuildStopSlot, ntwire.ErrAddonBuildTooOld)
	}
	tradeAcct := t.boundAccount
	if tradeAcct == "" {
		return "", fmt.Errorf("ninjatrader/tcp: refusing stop-entry %s on %s — trader has no bound account", side, symbol)
	}
	if !t.isAccountTradeable(tradeAcct) {
		return "", fmt.Errorf("ninjatrader/tcp: refusing stop-entry %s — account %q is not tradeable (not on allow-list / not SIM)", side, tradeAcct)
	}
	// W-ONE-BUTTON M2 site 4 — maintenance permit, before B3 and before
	// beforeSend; held across SendSignal.
	releasePermit, err := t.acquireEntryPermit("stop-entry " + side + " on " + symbol)
	if err != nil {
		return "", err
	}
	defer releasePermit()
	// W-EXEC-TRUTH W0 (b) — the ONE entry latch: after the permit, BEFORE B3
	// (a latch refusal never consumes the dedupe slot), held across the
	// ledger stamp and the send.
	latchDone, lerr := t.acquireEntryLatch("stop-entry " + side + " on " + symbol)
	if lerr != nil {
		return "", lerr
	}
	latchSent := false
	defer func() { latchDone(latchSent) }()
	b3Done, reason, ok := t.b3Reserve(fmt.Sprintf("stopentry|%s|%s|%s|%.0f", tradeAcct, upperSideStr(side), symbol, quantity))
	if !ok {
		return "", fmt.Errorf("ninjatrader/tcp: stop-entry not admitted — B3 dupe/rate guard: %s", reason)
	}
	b3Sent := false // W1b E12(b): the slot is recorded ONLY by an attempted send
	defer func() { b3Done(b3Sent) }()
	tick := InstrumentTickSize(t.symbol)
	entry, sl, tp, rerr := WireBracket(side, stopPx, sl, tp, tick) // W1b E12(a): stop AWAY from entry
	if rerr != nil {
		return "", fmt.Errorf("ninjatrader/tcp: refusing stop-entry %s on %s: %w", side, symbol, rerr)
	}
	tid := t.traderID
	signalID := uuid.NewString()
	payload := ntwire.SignalPayload{
		Symbol:     t.symbol,
		Account:    t.boundAccount,
		TraderID:   tid,
		Side:       side,
		Quantity:   int(quantity),
		Entry:      entry,
		StopLoss:   sl,
		TakeProfit: tp,
		SignalID:   signalID,
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
		OrderType:  "stop_entry",
		StopPrice:  entry,
	}
	if err := assertBoundAccount("stop-entry", symbol, payload.Account, t.boundAccount); err != nil {
		logger.Errorf("🚨 %v — REFUSING to submit stop-entry", err)
		return "", err
	}
	for _, register := range beforeSend {
		if err := register(signalID); err != nil {
			return "", fmt.Errorf("ninjatrader/tcp: register placement: %w", err)
		}
	}
	t.pendingMu.Lock()
	t.pending[signalID] = upperSideStr(side)
	t.pendingAt[signalID] = time.Now().UTC().UnixMilli()
	t.pendingMu.Unlock()
	t.mu.Lock()
	t.lastEntrySignalID = signalID
	t.mu.Unlock()
	serr := t.server.SendSignal(payload)
	latchSent = sendAttempted(serr)
	b3Sent = latchSent
	if err := serr; err != nil {
		return "", fmt.Errorf("ninjatrader/tcp: send stop-entry signal: %w", err)
	}
	return signalID, nil
}

// CancelOrder REQUESTS a cancel. ITS RETURN MEANS SENT, NOT CONFIRMED.
//
// D5 (cancel-confirmation, 2026-09-06). This is a one-line pass-through to
// SendCancelOrder: a nil error means a frame reached the socket, and NOTHING
// MORE. It does not mean the AddOn acted on it, and it certainly does not mean
// the order left the broker's book — the AddOn removes its own bookkeeping
// entry BEFORE calling Account.Cancel and acks regardless of whether the cancel
// threw (VLTraderTCPClient.cs:1665-1707), so a nil here is compatible with an
// order that is still resting.
//
// A cancel is proven ONLY by the order's absence from a FRESH broker snapshot
// (A20, class 6). Callers must move the ledger row to cancel_pending via
// store.RequestCancel and let the settlement pass confirm it; writing a
// terminal state on this return is the defect that let one arm slot hold nine
// live orders while the ledger read 'cancelled' (nt8_order_snapshots id 1664).
//
// TestNoCallSiteTreatsCancelReturnAsConfirmation enforces this in the tree.
//
// (PHASE 2 armed orders) cancels a working resting limit entry
// and/or its bracket legs on the AddOn side.
func (t *TCPTrader) CancelOrder(signalID string) error {
	return t.server.SendCancelOrder(ntwire.CancelOrderPayload{
		Symbol: t.symbol, SignalID: signalID, Account: t.boundAccount, TraderID: t.traderID,
	})
}

// ModifyBracket (PHASE 2 armed orders) modifies the live bracket SL/TP in place.
func (t *TCPTrader) ModifyBracket(signalID string, newSL, newTP float64) error {
	return t.server.SendModifyBracket(ntwire.ModifyBracketPayload{
		Symbol: t.symbol, SignalID: signalID, NewStopLoss: newSL, NewTakeProfit: newTP,
		Account: t.boundAccount, TraderID: t.traderID,
	})
}

// OrderUpdates returns THIS trader's order_update stream (per symbol+account).
func (t *TCPTrader) OrderUpdates() <-chan ntwire.OrderUpdatePayload {
	return t.server.SubscribeOrderUpdatesFor(t.symbol, t.boundAccount)
}

// OrderUpdatesListen returns a coordinated fan-out listener for this trader's
// order_update stream plus its unregister func. Unlike OrderUpdates (single
// subscriber, last-subscribe-wins — a second direct subscribe CLOSES the first
// consumer's channel), listeners coexist: the armed executor and the picture
// broker consumer both receive every frame, and neither can evict the other.
// The channel closes only when the underlying subscription dies; consumers
// keep their self-heal contract and re-listen.
func (t *TCPTrader) OrderUpdatesListen() (<-chan ntwire.OrderUpdatePayload, func()) {
	return t.server.ListenOrderUpdates(t.symbol, t.boundAccount)
}

// RecentFillFor returns the LATEST received fill for a signal id from the
// netting-fill ring — real execution evidence (fill frames carry the actual
// average fill price, never a limit price). ok=false means no fill for that
// signal was received since this process booted.
func (t *TCPTrader) RecentFillFor(signalID string) (price, quantity float64, ok bool) {
	if strings.TrimSpace(signalID) == "" {
		return 0, 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var best *recentFill
	for i := range t.recentFills {
		f := &t.recentFills[i]
		if f.SignalID != signalID || f.Price <= 0 {
			continue
		}
		if best == nil || f.TimeMs >= best.TimeMs {
			best = f
		}
	}
	if best == nil {
		return 0, 0, false
	}
	return best.Price, best.Quantity, true
}

// rememberEntryOrderID caches the entry order/signal identity for a
// reconcile-materialized position (GAR-F1). Called from the materialization
// path; empty ids are ignored.
func (t *TCPTrader) rememberEntryOrderID(symbol, side, signalID string) {
	if signalID == "" {
		return
	}
	key := keyFor(symbol, upperSideStr(side))
	t.mu.Lock()
	if t.entryOrderID == nil {
		t.entryOrderID = make(map[string]string)
	}
	t.entryOrderID[key] = signalID
	t.mu.Unlock()
}

// resolveEntrySignalID picks the order identity for move_stop/trailing (GAR-F1):
//  1. the in-process last entry signal (normal Go-side entries),
//  2. the materialization cache (positions materialized this process), then
//  3. the persisted row's entry_order_id (covers a restart AFTER the repair
//     pass stamped it) — the #566 dead-cell fallback chain.
//
// Empty = no usable identity (the caller reports the failure).
func (t *TCPTrader) resolveEntrySignalID(symbol, side string) string {
	key := keyFor(symbol, upperSideStr(side))
	t.mu.Lock()
	sid := t.lastEntrySignalID
	if sid == "" {
		sid = t.entryOrderID[key]
	}
	st := t.st
	t.mu.Unlock()
	if sid == "" && st != nil {
		if p, err := st.Position().GetOpenPositionByAccountSymbol(t.boundAccount, symbol, upperSideStr(side)); err == nil && p != nil && p.EntryOrderID != "" {
			sid = p.EntryOrderID
			t.rememberEntryOrderID(symbol, side, sid)
		}
	}
	return sid
}

// MoveStopToBreakeven asks the AddOn to move the resting stop for THIS trader's
// current open position (the last entry's signal_id) to newStop, tick-rounded —
// WITHOUT closing the position. Errors (no-op) if there is no tracked open entry.
// Part 2 auto-breakeven. An OLD AddOn ignores the move_stop frame, so the
// original protective stop keeps guarding the trade until the paired redeploy.
func (t *TCPTrader) MoveStopToBreakeven(side string, newStop float64) error {
	key := keyFor(t.symbol, upperSideStr(side))
	sid := t.resolveEntrySignalID(t.symbol, side)
	t.mu.Lock()
	cur := t.stopLoss[key]
	tid := t.traderID // A2 (G1)
	t.mu.Unlock()
	if sid == "" {
		return fmt.Errorf("ninjatrader/tcp: no open entry to move the stop for %s", t.symbol)
	}
	newStop = RoundToTick(newStop, InstrumentTickSize(t.symbol))
	// B1 STOP-WIDEN BAN: a stop may only TIGHTEN (reduce risk), never widen. A widen
	// attempt is a bug or bad input — REFUSE it and never send it to the broker.
	if cur > 0 && stopWouldWiden(side, cur, newStop) {
		logger.Warnf("⛔ stop-widen REFUSED: %s %s new stop %.2f would WIDEN from %.2f (risk-increasing) — NOT sent.",
			t.symbol, side, newStop, cur)
		return fmt.Errorf("ninjatrader/tcp: stop-widen ban — %s %s new stop %.2f widens from %.2f", t.symbol, side, newStop, cur)
	}
	if err := t.server.SendMoveStop(ntwire.MoveStopPayload{
		Symbol:      t.symbol,
		SignalID:    sid,
		NewStopLoss: newStop,
		// WALL CLOCK, not the tape (2026-09-07). This was feedNowUTC — the last
		// BAR's close — which is a market fact, not the moment a command was
		// created. The AddOn ages a received timestamp against DateTime.UtcNow
		// (VLTraderTCPClient.cs:814), so a bar-derived stamp reads as stale by
		// exactly the length of any feed gap.
		//
		// It is INERT today: the AddOn parses "timestamp" in one place only,
		// HandleSignal, and HandleMoveStop reads just signal_id and
		// new_stop_loss. Fixed anyway for two reasons. A freshness guard on
		// move_stop — the obvious hardening after this wave — would inherit the
		// bug fully formed, and auto-breakeven would then fail during feed gaps,
		// which is when a runner most needs its stop moved. And the internal
		// inconsistency teaches the wrong convention: the sibling
		// PlaceProtectiveStopPayload two hundred lines below already stamps
		// time.Now().UTC(), so one file said two different things about what
		// "timestamp" means.
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Account:   t.boundAccount, // A2 (G1) — identity stamp
		TraderID:  tid,
	}); err != nil {
		return err
	}
	// Track the new stop so a subsequent move is checked against it.
	t.mu.Lock()
	t.stopLoss[key] = newStop
	t.mu.Unlock()
	return nil
}

// assertBoundAccount (A1/G3) is the pre-submit identity invariant: the account an
// order/modify/cancel is being submitted on MUST equal the emitting trader's bound
// account. Returns an error (the caller REFUSES + 🚨) on any mismatch — the
// defense-in-depth chokepoint that makes cross-account emission structurally
// impossible. An empty bound account is itself a mismatch unless the frame is also
// account-less (legacy), which the entry path already refuses upstream.
func assertBoundAccount(op, symbol, frameAccount, boundAccount string) error {
	if frameAccount != boundAccount {
		return fmt.Errorf("A1 pre-submit invariant: %s %s frame account %q ≠ bound account %q (cross-account emission blocked)", op, symbol, frameAccount, boundAccount)
	}
	return nil
}

// stopWouldWiden reports whether moving a stop from cur→next WIDENS it (increases
// risk) for the given side. Long: widen = next below cur (stop moves further from
// price). Short: widen = next above cur. Equal or tighter → false. (B1 stop-widen
// ban — the one authority for the tighten-only invariant.)
func stopWouldWiden(side string, cur, next float64) bool {
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		return next > cur
	}
	return next < cur // long (default)
}

func (t *TCPTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.sendClose("long", quantity)
}

func (t *TCPTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.sendClose("short", quantity)
}

// sendClose asks the AddOn to flatten the symbol's position (close at market +
// cancel the protective bracket). The resulting market-exit fill returns as a
// position_close frame, which the close-sync records to history.
func (t *TCPTrader) sendClose(side string, quantity float64) (map[string]interface{}, error) {
	return t.sendCloseAt(side, quantity, 0)
}

// CloseWithLimit (4.3) submits a LIMIT exit at limitPrice instead of an
// immediate market flatten. The market fallback is a follow-up sendClose
// (limit 0) from the caller after its own budget elapses. LimitPrice <= 0 is
// identical to sendClose.
func (t *TCPTrader) CloseWithLimit(symbol string, side string, quantity float64, limitPrice float64) (map[string]interface{}, error) {
	return t.sendCloseAt(side, quantity, limitPrice)
}

func (t *TCPTrader) sendCloseAt(side string, quantity float64, limitPrice float64) (map[string]interface{}, error) {
	// Clear the entry correlation so the close-path order poll does NOT match the
	// lingering entry fill (which would record exit≈entry, PnL≈0). The real exit
	// is recorded by close-sync off the position_close frame.
	t.mu.Lock()
	t.lastEntrySignalID = ""
	tid := t.traderID // A2 (G1)
	t.mu.Unlock()

	payload := ntwire.ClosePositionPayload{
		Symbol:     t.symbol,
		Side:       side,
		Quantity:   int(quantity),
		LimitPrice: limitPrice,
		SignalID:   uuid.NewString(),
		Account:    t.boundAccount, // A2 (G1) — identity stamp
		TraderID:   tid,
	}
	if err := t.server.SendClosePosition(payload); err != nil {
		return nil, fmt.Errorf("ninjatrader/tcp: send close: %w", err)
	}
	return map[string]interface{}{
		"status": "close_submitted",
		"symbol": t.symbol,
		"side":   side,
	}, nil
}

func (t *TCPTrader) SetLeverage(symbol string, leverage int) error {
	return nil // futures leverage is set at the broker, not per-order
}

func (t *TCPTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	return nil // n/a for futures
}

// PlaceProtectiveStop places a STANDALONE protective stop for a position the
// broker is holding with no protection (D5, 2026-09-07).
//
// NOTE FOR THE NEXT READER: SetStopLoss below is NOT this. It writes a price
// into a local map that a subsequent placeEntry reads — it puts nothing on the
// wire and creates no order. Before today there was no way for this process to
// place a protective stop for an already-open position at all, which is part of
// why position 592 stayed naked for 8h19m: nothing was watching, and nothing
// could have fixed it if it had been.
func (t *TCPTrader) PlaceProtectiveStop(symbol, positionSide string, quantity int, stopPrice float64, signalID, reason string) error {
	// CAPABILITY HANDSHAKE. An older AddOn has no handler for this frame. Rather
	// than send into the dark and log a placement that never happened (class 81),
	// refuse and name the build.
	if bid := t.server.FarSideBuildID(); !ntwire.FarSideProven(bid, ntwire.MinAddonBuildProtectiveStop) {
		return fmt.Errorf("ninjatrader/tcp: refusing protective-stop %s %s stop=%.2f qty=%d [guard=far_side_build] — addon build cannot place a standalone protective stop (build_id=%s, need ≥ %s); the position stays UNPROTECTED and this is reported, not silently retried: %w",
			positionSide, symbol, stopPrice, quantity, ntwire.BuildIDForLog(bid), ntwire.MinAddonBuildProtectiveStop, ntwire.ErrAddonBuildTooOld)
	}
	tradeAcct := t.boundAccount
	if tradeAcct == "" {
		return fmt.Errorf("ninjatrader/tcp: refusing protective-stop %s on %s — trader has no bound account", positionSide, symbol)
	}
	if !t.isAccountTradeable(tradeAcct) {
		return fmt.Errorf("ninjatrader/tcp: refusing protective-stop %s — account %q is not tradeable (not on allow-list / not SIM)", positionSide, tradeAcct)
	}
	if quantity <= 0 || stopPrice <= 0 {
		return fmt.Errorf("ninjatrader/tcp: refusing protective-stop %s %s — qty=%d stop=%.2f; a protective order is never sized or priced from a zero", positionSide, symbol, quantity, stopPrice)
	}
	tick := InstrumentTickSize(t.symbol)
	return t.server.SendPlaceProtectiveStop(ntwire.PlaceProtectiveStopPayload{
		Symbol:       symbol,
		SignalID:     signalID,
		PositionSide: upperSideStr(positionSide),
		Quantity:     quantity,
		StopPrice:    RoundToTick(stopPrice, tick),
		Reason:       reason,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Account:      tradeAcct,
		TraderID:     t.traderID,
	})
}

func (t *TCPTrader) SetStopLoss(symbol, positionSide string, quantity, stopPrice float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLoss[keyFor(symbol, upperSideStr(positionSide))] = stopPrice
	return nil
}

func (t *TCPTrader) SetTakeProfit(symbol, positionSide string, quantity, takeProfitPrice float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.takePrft[keyFor(symbol, upperSideStr(positionSide))] = takeProfitPrice
	return nil
}

// EntryBracketMapsForTest copies the shared (symbol, side) SL/TP maps — what a
// legacy OpenLong/OpenShort sends and what MoveStopToBreakeven's widen ban
// reads as the live stop (W1b FOLD-3). Read-only; a test hook like the
// server's SeedPositionsForTest.
func (t *TCPTrader) EntryBracketMapsForTest() (stops, targets map[string]float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stops = make(map[string]float64, len(t.stopLoss))
	for k, v := range t.stopLoss {
		stops[k] = v
	}
	targets = make(map[string]float64, len(t.takePrft))
	for k, v := range t.takePrft {
		targets[k] = v
	}
	return stops, targets
}

func (t *TCPTrader) CancelAllOrders(symbol string) error {
	return fmt.Errorf("ninjatrader/tcp: CancelAllOrders not supported")
}

func (t *TCPTrader) GetBalance() (map[string]interface{}, error) {
	// Plan 4.11 — serve the REAL NT SIM account from the latest account_balance
	// frame (C# AddOn → tcp_server). No more $50k mock. Until the first frame
	// arrives (AddOn connecting), report zeros so the dashboard shows "no data"
	// rather than a fabricated balance; the C# emits on connect + periodically.
	//
	// Read THIS trader's OWN bound account (P5.4), mirroring GetPositions'
	// decouple (cb00347d) — GetBalance was the missed twin. The shared NT8
	// connection streams ONE "current" account; using AccountState() (current)
	// here showed a trader the WRONG account's equity when another trader/panel
	// owned the connection (a display AND a risk-sizing bug). Prefer the bound
	// account's own snapshot; fall back to the streamed `current` ONLY when the
	// bound account has no snapshot yet (AddOn hasn't streamed it), so this never
	// regresses to zeros vs today's behavior. The returned "account" field names
	// which NT account these numbers actually reflect.
	acct, ok := t.server.AccountStateFor(t.boundAccount)
	if !ok || t.boundAccount == "" {
		acct, ok = t.server.AccountState()
	}
	if ok {
		// Plan 4 Stage 4 — notify parent AutoTrader that balance has arrived
		// (used by defer-until-balance guard in runCycle).
		// Use reflection to avoid circular import (trader/ninjatrader → trader).
		if t.parentAutoTrader != nil {
			if method := reflect.ValueOf(t.parentAutoTrader).MethodByName("SetHasReceivedBalance"); method.IsValid() {
				method.Call([]reflect.Value{reflect.ValueOf(true)})
			}
		}

		equity := acct.NetLiquidation
		if equity == 0 {
			equity = acct.CashValue + acct.UnrealizedPnL
		}
		avail := acct.BuyingPower
		if avail == 0 {
			avail = acct.CashValue
		}
		return map[string]interface{}{
			"totalEquity":           equity,
			"availableBalance":      avail,
			"totalWalletBalance":    acct.CashValue,
			"totalUnrealizedProfit": acct.UnrealizedPnL,
			// Which NT account these numbers actually reflect (bound account when
			// its snapshot exists, else the streamed current — see the decouple
			// above). Lets the dashboard label/guard the balance accurately.
			"account": acct.Account,
			// Issue 2B — NT reports its own realized/unrealized P&L per account.
			// brokerNativePnL signals GetAccountInfo to use realized+unrealized as
			// the displayed P&L instead of (equity - global InitialBalance), which
			// otherwise produces a fake % on any account whose real starting equity
			// differs from the trader's single 100000 baseline.
			"totalRealizedProfit": acct.RealizedPnL,
			"brokerNativePnL":     true,
		}, nil
	}
	return map[string]interface{}{
		"totalEquity":      0.0,
		"availableBalance": 0.0,
	}, nil
}

// IsFeedConnected reports whether the NT8 price feed is usable (delegates to the
// TCP server's latest feed_status; default-allow until the first frame arrives).
func (t *TCPTrader) IsFeedConnected() bool { return t.server.IsFeedConnected() }

// FeedStatus returns the latest NT8 price-feed status string ("" until the first
// feed_status frame).
func (t *TCPTrader) FeedStatus() string { return t.server.FeedStatus() }

// IsConnected reports whether the NT8 C# AddOn's TCP socket is currently connected
// — the RAW link, independent of feed/market-data status. The dead-man watchdog
// (B5) gates NEW entries on this: a dropped link blocks entries until a clean
// positions/orders reconciliation.
func (t *TCPTrader) IsConnected() bool { return t.server.IsConnected() }

func (t *TCPTrader) GetPositions() ([]map[string]interface{}, error) {
	// Read positions for THIS trader's OWN bound account — NOT the shared connection
	// CurrentAccount() (which the dashboard can switch for DISPLAY). Decoupling keeps
	// each running trader's monitoring — drawdown (auto_trader_risk.go), decision
	// context (auto_trader_loop.go) — reading its own account even while another
	// account is being VIEWED. Empty boundAccount does NOT fall back to current (that
	// would re-open the ef550df7 hole): PositionsFor("") returns !ok and we use the
	// fill-derived cache below (an unbound trader refuses to trade anyway). Still
	// reflects positions opened MANUALLY in NT8 (the AddOn emits a `positions`
	// snapshot on select / connect / PositionUpdate).
	acct := t.boundAccount
	if snap, ok := t.server.PositionsFor(acct); ok {
		// NT8-truth uPnL: the account_balance frame carries the account's LIVE
		// unrealized P&L. When exactly ONE position is open, that total IS this
		// position's uPnL — use it (and derive the mark) so the displayed P&L
		// matches NT8, instead of marking off a stale 5m bar close. With multiple
		// positions the account total can't be split per-position, so those fall
		// back to the mark calc.
		nonFlat := 0
		for _, p := range snap {
			if p.Quantity != 0 {
				nonFlat++
			}
		}
		var uPnLOverride *float64
		if nonFlat == 1 {
			if bal, ok := t.server.AccountStateFor(acct); ok {
				v := bal.UnrealizedPnL
				uPnLOverride = &v
			}
		}
		out := make([]map[string]interface{}, 0, nonFlat)
		for _, p := range snap {
			if p.Quantity == 0 {
				continue // flat — should not appear in a snapshot, but be safe
			}
			out = append(out, t.positionMap(p.Symbol, p.Side, float64(p.Quantity), p.AvgPrice, uPnLOverride))
		}
		return out, nil
	}

	// Fallback (no NT8 snapshot yet): the single fill-derived position.
	t.mu.Lock()
	if !t.hasFill {
		t.mu.Unlock()
		return []map[string]interface{}{}, nil
	}
	fill := t.lastFill
	t.mu.Unlock()
	return []map[string]interface{}{
		t.positionMap(t.symbol, fill.Side, float64(fill.Quantity), fill.FillPrice, nil),
	}, nil
}

// positionMap builds the UI/decision-context record for one open position, with
// uPnL marked off the latest cached bar and the futures point value. Shared by
// the NT8-snapshot path (truth) and the fill-derived fallback so both produce
// the identical shape.
func (t *TCPTrader) positionMap(symbol, side string, qty, entry float64, uPnLOverride *float64) map[string]interface{} {
	sd := upperSideStr(side)

	// Futures: contract point value (MNQ=$2/pt). positionAmt is signed (short < 0)
	// per the Binance convention GetAccountInfo expects.
	pv := market.FuturesPointValue(symbol)
	if pv <= 0 {
		pv = 1
	}
	dir := 1.0
	signedQty := qty
	if sd == "SHORT" {
		dir = -1.0
		signedQty = -qty
	}

	var mark, uPnL, uPnLPct float64
	if uPnLOverride != nil {
		// NT8-truth uPnL (account_balance UnrealizedPnL) — already correctly signed
		// by NT8. Derive the mark so the displayed "Current" is consistent with it:
		// long uPnL=(mark-entry)*qty*pv → mark=entry+uPnL/(qty*pv); short →
		// mark=entry-uPnL/(qty*pv); i.e. mark = entry + dir*uPnL/(qty*pv).
		uPnL = *uPnLOverride
		mark = entry
		if qty > 0 {
			mark = entry + dir*uPnL/(qty*pv)
		}
		if entry > 0 {
			uPnLPct = (mark - entry) / entry * 100 * dir
		}
	} else {
		// Fallback (multi-position / no account snapshot yet): mark off the latest
		// 5m BarCache close. Less precise than NT8's live uPnL but self-contained.
		mark = entry
		if bars := t.server.BarCache().Get(symbol, "5m"); len(bars) > 0 {
			mark = bars[len(bars)-1].C
		}
		uPnL = (mark - entry) * dir * qty * pv
		if entry > 0 {
			uPnLPct = (mark - entry) / entry * 100 * dir
		}
	}

	return map[string]interface{}{
		// snake_case — read by the UI Position type (web/src/types/trading.ts)
		"symbol":             symbol,
		"side":               sd,
		"entry_price":        entry,
		"mark_price":         mark,
		"quantity":           qty,
		"leverage":           1.0, // futures: margin is per-contract, not crypto leverage
		"unrealized_pnl":     uPnL,
		"unrealized_pnl_pct": uPnLPct,
		"liquidation_price":  0.0,
		"margin_used":        0.0,
		// camelCase — read by AutoTrader.GetAccountInfo (Binance-style margin calc)
		"positionAmt":      signedQty,
		"entryPrice":       entry,
		"markPrice":        mark,
		"unRealizedProfit": uPnL,
	}
}

// DebugPlaceTestTrade places a deterministic 1-contract bracket order on the
// resolved SIM account for end-to-end proof (Plan 4.11 dashboard: signal →
// NT8 SIM fill → position). It BYPASSES the AI + risk gate — a debug harness,
// NOT a trading path — and is reachable only via the SIM-gated
// /api/debug/nt-test-trade endpoint. SL/TP are priced off the latest cached
// MNQ bar (20pt stop / 40pt target → ~2:1). side = "short" else long.
// DebugUnsubscribeBars drops an EXTRA bar subscription at runtime (P5.1
// dispose-one proof / P5.3 building block): removes the root from the
// auto-subscribe list and sends bars_unsubscribe so the AddOn disposes that
// root's BarsRequests. The primary (trading) symbol is refused server-side.
func (t *TCPTrader) DebugUnsubscribeBars(symbol string) error {
	return t.server.UnsubscribeBarsSymbol(symbol)
}

// P5.3 — runtime symbol management for the owner API (flag-gated upstream).
// Bars-only: extras never touch the trading/gate/order paths (P5.4 lifts that).

// RuntimeSubscribeBars adds an extra bar-subscription root NOW (no restart).
func (t *TCPTrader) RuntimeSubscribeBars(symbol string) error {
	return t.server.SubscribeBarsSymbol(symbol)
}

// RuntimeUnsubscribeBars removes an extra root NOW (primary refused) + purges
// its cached bars.
func (t *TCPTrader) RuntimeUnsubscribeBars(symbol string) error {
	return t.server.UnsubscribeBarsSymbol(symbol)
}

// RequestDeepBarsBackfill (BAR-TRUTH WAVE 2026-08-28) sends a one-shot deep
// bars_subscribe on the live connection (arbiter/backfill). The auto-subscribe
// state is untouched.
func (t *TCPTrader) RequestDeepBarsBackfill(symbol, timeframe string, barsBack int) error {
	return t.server.RequestDeepBarsBackfill(symbol, timeframe, barsBack)
}

// BarTruthEndCapture returns the captured NT8-truth replay batches (empty when
// no capture window ran) for the three-way diff.
func (t *TCPTrader) BarTruthEndCapture() map[string]ntwire.BarTruthBatch {
	return t.server.EndBarTruthCapture()
}

// BarTruthDrops returns the BAR-TRUTH drop counters (ingest + persist queue).
func (t *TCPTrader) BarTruthDrops() (int64, int64, int64, int64) {
	return t.server.BarTruthDrops()
}

// BarTruthCache returns the kernel cache snapshot for (symbol, tf) — nil when
// the pair has no bars.
func (t *TCPTrader) BarTruthCache(symbol, tf string) []ntwire.Bar {
	return t.server.BarCache().Get(symbol, tf)
}

// BarsSubscriptionStates exposes the per-symbol subscription lifecycle
// (pending/subscribed/error/unsubscribed, with contract/reason).
func (t *TCPTrader) BarsSubscriptionStates() map[string]ntwire.SymbolSubState {
	return t.server.BarsSubscriptionStates()
}

// BarsPrimarySymbol is the trading symbol (the root that can never be
// unsubscribed at runtime).
func (t *TCPTrader) BarsPrimarySymbol() string {
	return t.server.BarsSubscribeSymbol()
}

func (t *TCPTrader) DebugPlaceTestTrade(side string) (map[string]interface{}, error) {
	bars := t.server.BarCache().Get(t.symbol, "5m")
	if len(bars) == 0 {
		return nil, fmt.Errorf("ninjatrader/tcp: no %s bars cached; cannot price a test trade (recompile/connect NT8 first)", t.symbol)
	}
	price := bars[len(bars)-1].C
	tick := InstrumentTickSize(t.symbol)
	// W1b FOLD-3: the test trade carries its own bracket into the send, so a
	// refused one never leaves it in the maps the widen ban reads.
	if side == "short" {
		return t.OpenWithBracket(t.symbol, "short", 1, RoundToTick(price+20, tick), RoundToTick(price-40, tick))
	}
	return t.OpenWithBracket(t.symbol, "long", 1, RoundToTick(price-20, tick), RoundToTick(price+40, tick))
}

// BarCache exposes the live NT8 bar cache for the Stage 4 SSE chart relay.
// Read-only access; the relay polls Get(symbol, timeframe) for snapshots +
// incremental updates.
func (t *TCPTrader) BarCache() *ntwire.BarCache { return t.server.BarCache() }

// GetServer exposes the underlying TCP server for API handlers (e.g., account selection).
// Used by the /api/accounts and /api/account/select handlers to interact with the NT AddOn.
func (t *TCPTrader) GetServer() *ntwire.TCPServer { return t.server }

// BoundAccount returns the NT sub-account THIS trader is bound to (P5.4), or ""
// if unbound. Unlike server.CurrentAccount() (the shared, streamed/display
// account that flaps to whichever account's frame arrived last), this is the
// trader's OWN account — so record/stat/equity attribution stamps the correct
// owner even while another account is being streamed/viewed. It is the missed
// twin of the GetBalance/GetPositions/reconcile decouples.
func (t *TCPTrader) BoundAccount() string { return t.boundAccount }

// WireSymbol is the instrument this trader sends entries on (the payload
// symbol and the entry latch key) — never the raw, possibly comma-listed config.
func (t *TCPTrader) WireSymbol() string { return t.symbol }

// flattenKey is the "SYMBOL|SIDE" key for closedAt (upper-cased, trimmed).
func flattenKey(symbol, side string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side))
}

// MarkCloseConfirmed records that a FILL-CONFIRMED close (position_close frame)
// arrived for (symbol, side) on this trader's bound account. Called from close-sync
// on every position_close. This is the fast, account-correct flat signal the
// reconcile-before-open gate awaits (the frame arrives on the flatten fill even for
// a non-active account, unlike the positions snapshot which lags to the 30s
// heartbeat).
func (t *TCPTrader) MarkCloseConfirmed(symbol, side string) {
	t.mu.Lock()
	if t.closedAt == nil {
		t.closedAt = make(map[string]int64)
	}
	t.closedAt[flattenKey(symbol, side)] = time.Now().UnixMilli()
	t.mu.Unlock()
}

// CloseConfirmedSince reports whether a fill-confirmed close for (symbol, side)
// arrived at or after sinceMs — the frame-path flat confirmation for reconcile.
func (t *TCPTrader) CloseConfirmedSince(symbol, side string, sinceMs int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closedAt != nil && t.closedAt[flattenKey(symbol, side)] >= sinceMs
}

// ResetAccountState clears cached fill/position state when switching accounts.
// Called by the /api/account/select handler to ensure GetPositions() fetches fresh
// data from the newly selected account (not stale cached fills from the old account).
func (t *TCPTrader) ResetAccountState() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hasFill = false
	t.lastFill = ntwire.FillPayload{}
	t.stopLoss = map[string]float64{}
	t.takePrft = map[string]float64{}
	t.pending = map[string]string{}
	t.pendingAt = map[string]int64{}
}

// SetParentAutoTrader sets the parent AutoTrader reference (Plan 4 Stage 4).
// Called by transport.go after creating the TCPTrader. Used to notify when
// the first account_balance frame arrives (defer-until-balance guard).
func (t *TCPTrader) SetParentAutoTrader(parent interface{}) {
	t.parentAutoTrader = parent
}

func (t *TCPTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.0f", quantity), nil
}

func (t *TCPTrader) GetOrderStatus(symbol, orderID string) (map[string]interface{}, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Report a fill ONLY when it belongs to the current entry (correlate by the
	// echoed signal_id). This guards against a stale fill from a prior trade and
	// against the entry fill leaking into a close-path poll (lastEntrySignalID is
	// cleared on close). Until the matching fill arrives → "pending".
	if !t.hasFill || t.lastEntrySignalID == "" || t.lastFill.SignalID != t.lastEntrySignalID {
		return map[string]interface{}{"status": "pending"}, nil
	}
	// Canonical shape expected by recordAndConfirmOrder (auto_trader_decision.go)
	// AND pollAndUpdateOrderStatus (handler_trader_status.go): status "FILLED"
	// (uppercase) + key "avgPrice". The AddOn reports the real broker fill
	// (AverageFillPrice) as lastFill.FillPrice — THIS is the NT8-truth entry that
	// replaces the stale marketData.CurrentPrice (5m-mark) reference. Previously
	// this returned "filled"/"price", which neither poller matched, so the entry
	// was never upgraded from the frozen mark.
	status := "FILLED"
	if t.lastFill.Status == "rejected" {
		status = "REJECTED"
	}
	return map[string]interface{}{
		"status":      status,
		"avgPrice":    t.lastFill.FillPrice,
		"executedQty": float64(t.lastFill.Quantity),
		"price":       t.lastFill.FillPrice, // back-compat (prior key)
		"side":        upperSideStr(t.lastFill.Side),
	}, nil
}

func (t *TCPTrader) GetClosedPnL(start time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	return nil, nil
}

func (t *TCPTrader) GetMarketPrice(symbol string) (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasFill {
		return 0, fmt.Errorf("ninjatrader/tcp: no fill yet, market price unavailable; use Databento client directly")
	}
	return t.lastFill.FillPrice, nil
}

func (t *TCPTrader) CancelStopLossOrders(symbol string) error {
	return fmt.Errorf("ninjatrader/tcp: CancelStopLossOrders not supported — SL is set at entry")
}

func (t *TCPTrader) CancelTakeProfitOrders(symbol string) error {
	return fmt.Errorf("ninjatrader/tcp: CancelTakeProfitOrders not supported — TP is set at entry")
}

func (t *TCPTrader) CancelStopOrders(symbol string) error {
	return fmt.Errorf("ninjatrader/tcp: CancelStopOrders not supported")
}

// SetOpenOrdersSource (CLASS 33, 2026-09-02) wires the working-order source
// for GetOpenOrders. NT8 emits NO working-order frame (audit F12), so the only
// truth about resting entries is the armed_orders ledger — the AutoTrader owns
// the store and injects it here at start.
func (t *TCPTrader) SetOpenOrdersSource(fn func(symbol string) ([]types.OpenOrder, error)) {
	t.mu.Lock()
	t.openOrdersSrc = fn
	t.mu.Unlock()
}

// GetOpenOrders answers flat-gate leg 4. Until 2026-09-02 this was
// `return []types.OpenOrder{}, nil` — a gate that could not fail, which passed
// VACUOUSLY at every cutover 35 → 41 (class 33) including 09-02 00:16 CT with
// two arms resting at the broker. It now reports the armed_orders ledger's
// non-terminal rows, each stamped with its Source. A source that errors FAILS
// the leg; an UNWIRED source fails it too — never a silent empty (A24).
func (t *TCPTrader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	t.mu.Lock()
	fn := t.openOrdersSrc
	t.mu.Unlock()
	if fn == nil {
		return nil, fmt.Errorf("open-orders source not wired (class 33): NT8 has no working-order frame, so leg 4 has no truth to report — refusing to answer empty")
	}
	rows, err := fn(symbol)
	if err != nil {
		return nil, fmt.Errorf("open-orders (armed_orders ledger): %w", err)
	}
	return rows, nil
}

// b3Reserve runs B3 (dupe guard + rate breaker) for one entry at the
// submission chokepoint and returns the reservation's done (W1b E12(b)): the
// caller defers done(sent) with sent = sendAttempted(serr), so the dedupe slot
// and the rate count are recorded ONLY by a send, never by a refusal after B3.
// A refusal is logged and counted HERE for every entry path — the armed paths
// used to discard the reason and count nothing, so the Gate-blocks panel's
// "Duplicate order dropped" never showed an armed refusal. B6: B3 fires at the
// shared chokepoint (bound to an account, not a trader), so it tallies under
// the process-wide "" trader bucket. A nil guard admits (done is a no-op).
func (t *TCPTrader) b3Reserve(key string) (done func(sent bool), reason string, ok bool) {
	if t.guard == nil {
		return func(bool) {}, "", true
	}
	done, reason, ok = t.guard.reserve(key, time.Now().UnixMilli())
	if ok {
		return done, "", true
	}
	gate := "b3_order_dedup"
	if strings.Contains(reason, "breaker") {
		gate = "b3_rate_breaker"
		logger.Warnf("🚨 B3 %s", reason)
	} else {
		logger.Warnf("⛔ B3 %s", reason)
	}
	telemetry.IncGateBlock("", gate)
	return nil, reason, false
}

// upperSideStr normalises "long"/"LONG"/"Long" → "LONG" for SL/TP map keys.
// CSV Trader keys are uppercase, so we mirror that to keep behaviour parallel.
func upperSideStr(side string) string {
	switch side {
	case "long", "LONG", "Long":
		return "LONG"
	case "short", "SHORT", "Short":
		return "SHORT"
	default:
		return side
	}
}

// SetRejectSink installs the owning AutoTrader's receipt sink. The fallback is
// the same store transition for standalone TCPTrader users; never invoke both.
func (t *TCPTrader) SetRejectSink(fn func(signalID, brokerReason string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rejectSink = fn
}
func (t *TCPTrader) notifyReject(signalID, brokerReason string) {
	if strings.TrimSpace(signalID) == "" {
		return
	}
	t.mu.Lock()
	sink, st, tid := t.rejectSink, t.st, t.traderID
	t.mu.Unlock()
	if sink != nil {
		sink(signalID, brokerReason)
		return
	}
	if st != nil {
		if err := st.ArmedOrders().ApplyPlacementReceipt(tid, signalID, store.StateRejected, brokerReason); err != nil {
			logger.Errorf("persist entry rejection signal=%s: %v", signalID, err)
		}
	}
}
