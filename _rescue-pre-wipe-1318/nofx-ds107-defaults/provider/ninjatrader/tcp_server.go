// Package ninjatrader — TCP listener for Plan 1.5 pre-trigger build.
//
// Listens on 127.0.0.1:36974 (NOT NT's ATI port 36973). Accepts a single
// concurrent NT AddOn client; rejects (closes) any second simultaneous dial.
// Routes signal frames out, fill/heartbeat/ack frames in. Spec ref:
// docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md L4359 + L4374-4416.
//
// CSV bridge (Plan 1) remains the SIM-validated production path. This file
// is purely additive — Plan 1 critical files (csv_writer.go, csv_tailer.go,
// types.go, mock_nt.go) stay byte-identical per ADR-007.
package ninjatrader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Wire-protocol constants per spec L4359 + L4408 + L4415 + L4414 + L4376.
const (
	// TCPListenAddr — loopback only; NT runs on the same Windows host (or
	// reaches WSL2 via mirrored networking). NOT 36973 (NT's ATI port).
	TCPListenAddr = "127.0.0.1:36974"

	// TCPHeartbeatInterval — server pings every 30s per spec L4408.
	TCPHeartbeatInterval = 30 * time.Second

	// TCPHeartbeatAckTimeout — server closes the connection after this long
	// without a heartbeat ack per spec L4415.
	TCPHeartbeatAckTimeout = 60 * time.Second

	// TCPStaleSignalAge — signals older than this are dropped silently from
	// the pending queue on reconnect per spec L4414 (C# AddOn may reject
	// stale; we proactively drop on the Go side to avoid wasted bandwidth).
	TCPStaleSignalAge = 60 * time.Second

	// TCPMaxFrameBytes — oversized frames are an error per spec L4376.
	TCPMaxFrameBytes = 1 << 20 // 1 MB

	// fillChannelBuffer — bounded so a stuck consumer can't OOM the server.
	fillChannelBuffer = 32

	// acceptLoopPollInterval — listener deadline so the accept loop can
	// observe ctx cancellation without blocking indefinitely on Accept().
	acceptLoopPollInterval = 250 * time.Millisecond
)

// TCPServer is the Go-side endpoint of the Plan 1.5 TCP bridge. It owns the
// listener, exactly one connected client at a time, the pending-signal queue
// flushed on (re)connect, and the inbound fill channel that TCPTrader reads.
type TCPServer struct {
	histAtSub  historyAtSubscribe // 101 E1
	histReplay historyReplayState // 101 D1'(3a)
	addr       string
	listener   net.Listener

	// U1 wire-liveness (stale-bar dispatch 2026-08-19): every inbound frame
	// bumps these; a 60s reporter line answers "feed or math" in one glance.
	framesTotal     atomic.Int64
	lastFrameUnixMs atomic.Int64
	// T8 (2026-08-27) — bar_update frame-log sampler (INFO spam ate the
	// journald cap in <1 day; the frame is DEBUG-sampled 1-in-N now).
	frameLogN atomic.Int64

	// Pending signals to flush on (re)connect (spec L4414).
	pendingMu sync.Mutex
	pending   []timedSignal
	// entryHoldCheck (W-ONE-BUTTON M2, gap U2): func() bool — true while the
	// installation is under maintenance. flushPending drops queued entries
	// while it holds; nothing re-queues them.
	entryHoldCheck atomic.Value
	// maint (W-ONE-BUTTON M2 site 7): the hold source, what each connection was
	// told, and the per-connection record (hello epoch + maintenance_ack).
	maint maintenanceWire

	// E7 capability handshake (2026-08-30): the far-side AddOn's build id,
	// reported on every heartbeat. Empty until the first heartbeat carries it.
	// Frame types are gated on it (FarSideBuildE7) — an old AddOn that would
	// mis-execute an unknown frame must never receive it (the 22:32 stop_entry
	// test executed as MARKET on the pre-E7 AddOn).
	farSideBuild atomic.Value // string

	// F12 — the broker's working-order book, latest per account+symbol. Read by
	// cutover leg 4 and the override guard; nil-safe because an older AddOn
	// sends no snapshots at all and leg 4 must then say so out loud rather than
	// silently falling back.
	orderSnaps *OrderSnapshotCache
	// orderSnapCB is the optional persistence hook (the store writes one row
	// per snapshot). Optional so the provider package keeps no store import and
	// the parse path stays testable without a database.
	orderSnapCB func(OrderSnapshotPayload)

	// Inbound fills — TCPTrader subscribes via Fills().
	fillCh     chan FillPayload
	closeCh    chan PositionClosePayload
	orderUpdCh chan OrderUpdatePayload
	rejectCh   chan PositionCloseRejectedPayload
	instrCh    chan InstrumentInfoPayload

	// Coordinated order_update fan-out (picture-htf round, 2026-09-20):
	// subscribeFor REPLACES the (symbol, account) channel, so two in-process
	// consumers (armed executor + picture broker consumer) subscribing directly
	// would close each other's channel and one would silently stop receiving.
	// ListenOrderUpdates owns THE one direct subscription per key and copies
	// every received frame to each listener.
	ouFanMu   sync.Mutex
	ouFanNext int
	ouFanouts map[string]*orderUpdateFanout

	// Latest NT8 price-feed status (from the feed_status frame). Empty until the
	// first frame; consumers DEFAULT-ALLOW on empty (never false-halt at startup).
	feedMu     sync.RWMutex
	feedStatus string
	// lastBarNano is the wall-clock (UnixNano) of the most recent LIVE bar_update.
	// The feed_status frame is edge-triggered (sent only on CHANGE), so a momentary
	// flap can latch a stale "Disconnected" that never self-heals. A recent
	// bar_update proves the feed is actually delivering data — IsFeedConnected uses
	// it to override a stale non-"Connected" status. 0 until the first live bar.
	lastBarNano atomic.Int64

	// Plan 4.4 Stage 2 — bar ingest. Bar frames from the C# AddOn
	// (bars_historical, bar_update) decode in the read loop and post to
	// barIngestCh via a NON-BLOCKING drop-oldest send (for bar_update) or
	// a bounded BLOCKING send (for bars_historical). The drainBarIngest
	// goroutine consumes from this channel and writes into barCache.
	// Backpressure invariant: a slow cache writer must NOT stall the
	// socket read (which would block heartbeats → spurious reconnect).
	barCache      *BarCache
	barIngestCh   chan barIngestMsg
	barsSubscribe BarsSubscribePayload // sent on each (re)connect after flushPending
	barsSubMu     sync.RWMutex         // protects barsSubscribe + extraBarsSymbols
	// P5.1 — EXTRA symbol roots auto-subscribed alongside the primary on each
	// (re)connect, with the SAME timeframes/bars-back as the primary (one
	// bars_subscribe frame per root). Empty by default → exactly one frame, the
	// pre-P5 single-symbol behavior byte-identical. The C# manager registry is
	// keyed SYMBOL|TF, so extra roots stream concurrently and independently;
	// its reconnect ResubscribeAll + watchdog already iterate ALL entries.
	extraBarsSymbols []string
	// P5.4 — the TRADING-symbol registry. Every TCPTrader REGISTERS its symbol
	// (SetBarsSubscribeSymbol no longer overwrites): the FIRST registration
	// claims the legacy primary slot (template for timeframes/bars-back);
	// subsequent different roots append. All registered roots are subscribed,
	// refused by UnsubscribeBarsSymbol, and used by the fan-out router.
	tradingSymbols    []string
	primaryRegistered bool
	// P5.4 — config extras keyed by the OWNING trader's primary symbol, so one
	// trader's (re)load REPLACES only ITS OWN extras (a second trader's load no
	// longer wipes the first's). Key "" = the legacy unowned slot.
	extrasByOwner map[string][]string

	// P5.3 — per-symbol subscription state, fed by the AddOn's ack frames
	// (subscribed / unsubscribed / subscribe_error). "pending" until an ack
	// arrives (a pre-P5.3 AddOn never acks → stays pending; bars still flow).
	subStateMu sync.RWMutex
	// ROLL WAVE — the last contract each symbol's ACK named, and observed rolls.
	rollMu    sync.RWMutex
	lastNamed map[string]string
	rolls     map[string]rollEvent
	subStates map[string]SymbolSubState

	// HISTORY IMPORT (wave 101) — request-id-keyed streams for the
	// bars_history_data / bars_history_error frames. Nothing here feeds the
	// live bar ingest; the importer (trader/historical_import.go) is the ONLY
	// consumer, and the server only fans frames out. Rows are written by the
	// importer via store.ImportBars — never by this server.
	histSubMu       sync.RWMutex
	historyDataSubs map[string]chan BarsHistoryDataPayload
	historyErrSubs  map[string]chan BarsHistoryErrorPayload

	// Plan 4.11 — latest real account snapshot from the C# AddOn
	// (account_balance frame). Replaces the $50k mock in
	// TCPTrader.GetBalance once the first frame arrives.
	acctMu       sync.RWMutex
	acctBalances map[string]AccountBalancePayload // per-account snapshots keyed by account name (Issue 2A)
	// Open-position read-back (positions frame) — per-account CURRENT open
	// positions reported by the C# AddOn. Replaces the fill-only inference so
	// GetPositions reflects NT8 truth across account switch-back + manual trades.
	// Guarded by acctMu. Each frame REPLACES the account's slice (full snapshot).
	acctPositions map[string][]OpenPosition

	// Plan 4 Stage 4 — available accounts discovered by the C# AddOn
	// (accounts_list frame). Emitted on connect and on account change.
	// Thread-safe, read by the /api/accounts handler, written by readLoop.
	accountsListMu sync.RWMutex
	accountsList   []AccountInfo // immutable copy, slice
	currentAccount string        // currently selected account name

	// P5.4 — symbol-routed fan-out. ONE router goroutine per source channel
	// (fills/closes/rejects/instrument_info) consumes it and dispatches by the
	// payload's symbol to per-symbol subscriber channels, so multiple traders
	// never race on the shared channels (the audit's message-loss/cross-
	// contamination bug). Legacy EMPTY-symbol payloads route to the PRIMARY
	// trading symbol. Re-subscribing a symbol CLOSES the prior channel, which
	// terminates a dead (reloaded-away) trader instance's consumer goroutine.
	subsMu       sync.Mutex
	fillSubs     map[string]chan FillPayload
	closeSubs    map[string]chan PositionClosePayload
	rejectSubs   map[string]chan PositionCloseRejectedPayload
	instrSubs    map[string]chan InstrumentInfoPayload
	orderUpdSubs map[string]chan OrderUpdatePayload

	// Connection state — single concurrent client (spec L4359).
	connMu        sync.Mutex
	conn          net.Conn
	lastAckTime   time.Time
	staleOverride time.Duration // test hook: 0 means use TCPStaleSignalAge

	// writeMu serializes all writes to a connected client's net.Conn.
	// Both heartbeatLoop and readLoop (handling client heartbeats) write
	// to the same conn; without serialization, concurrent writes could
	// interleave frame bytes and corrupt the wire protocol. The C# side
	// already uses lock(writeLock) for the same reason. Added in
	// Plan 1.5.6 alongside the FrameHeartbeat ack-write deadline fix.
	writeMu sync.Mutex

	// Lifecycle.
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	logger     *slog.Logger
	runCtx     context.Context // set in Start; the lazy router goroutine binds to it
	routerOnce sync.Once

	// A2 (G1) — echo-verify registry. Each outbound order/modify/cancel is assigned
	// a monotonic seq and registered here by (trader_id, account, signal_id); the
	// AddOn echoes seq on the paired ack/fill/close/reject, which the read loop
	// verifies via checkEcho. Indexed by seq AND by signal_id (fills echo signal_id;
	// a v3 AddOn also echoes seq). Bounded by pendingOpsCap (drop-oldest).
	seqCounter   atomic.Uint64
	pendingMu2   sync.Mutex
	pendingOps   map[uint64]pendingOp
	pendingBySig map[string]uint64

	// A3 (G2) — the bound-account allowlist this session's traders declare. Sent to
	// the AddOn (account_register) on every connect BEFORE queued signals flush, and
	// again whenever a new trader binds, so the AddOn enforces against an
	// owner-declared set (never signal-self-registered). Guarded by boundAcctMu.
	boundAcctMu   sync.Mutex
	boundAccounts map[string]bool
}

// pendingOpsCap bounds the echo-verify registry so a run of un-retired ops can never
// grow it without limit (order volume is low; this is a backstop).
const pendingOpsCap = 4096

// ============================================================================
// P5.4 — symbol-routed fan-out (fills / closes / rejects / instrument_info)
// ============================================================================

// ensureRouters lazily starts the single router goroutine the first time any
// per-symbol subscription is created. Legacy direct consumers of Fills() etc.
// (tests) keep working as long as nothing subscribes — once a subscriber
// exists, ALL production consumption goes through the router.
func (s *TCPServer) ensureRouters() {
	s.routerOnce.Do(func() {
		// Map pointers must be stable before runRouters evaluates its dispatch
		// arguments. Locking only inside dispatch was too late for lazy init.
		s.subsMu.Lock()
		s.fillSubs = make(map[string]chan FillPayload)
		s.closeSubs = make(map[string]chan PositionClosePayload)
		s.rejectSubs = make(map[string]chan PositionCloseRejectedPayload)
		s.instrSubs = make(map[string]chan InstrumentInfoPayload)
		s.orderUpdSubs = make(map[string]chan OrderUpdatePayload)
		s.subsMu.Unlock()
		ctx := s.runCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go s.runRouters(ctx)
	})
}

func (s *TCPServer) runRouters(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-s.fillCh:
			dispatchToOwner(s, s.fillSubs, p.Symbol, p.Account, p, "fill")
		case p := <-s.closeCh:
			dispatchToOwner(s, s.closeSubs, p.Symbol, p.Account, p, "position_close")
		case p := <-s.rejectCh:
			dispatchToOwner(s, s.rejectSubs, p.Symbol, p.Account, p, "close_rejected")
		case p := <-s.instrCh:
			// instrument_info carries no account (spec cross-check) → every
			// subscriber of the symbol gets it (both same-symbol traders).
			broadcastToSymbol(s, s.instrSubs, p.Symbol, p, "instrument_info")
		case p := <-s.orderUpdCh:
			// PHASE 2 armed orders — order state changes route per (symbol,account)
			// like fills, so each trader sees only its own working orders.
			dispatchToOwner(s, s.orderUpdSubs, p.Symbol, p.Account, p, "order_update")
		}
	}
}

// subKey composes the fan-out routing key. Two traders on the SAME symbol but
// DIFFERENT accounts get DISTINCT keys, so re-subscription never evicts the other
// trader's channel and each receives only its OWN account's fills/closes/rejects
// (the H3 fix). account "" is the legacy/unbound key.
func subKey(symbol, account string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "\x00" + strings.TrimSpace(account)
}

// trySendLocked delivers one payload (subsMu held; mutually exclusive with the
// close-on-resubscribe so a send on a closed channel is impossible).
func trySendLocked[T any](s *TCPServer, ch chan T, p T, kind, symbol string) {
	select {
	case ch <- p:
	default:
		s.logger.Warn("tcp_server: "+kind+" subscriber full — dropped", "symbol", symbol)
	}
}

// broadcastLocked sends to EVERY subscriber of the symbol regardless of account
// (subsMu held). Used for account-less frames (instrument_info) and the legacy
// no-account fallback. Both same-symbol traders receive the payload.
func broadcastLocked[T any](s *TCPServer, subs map[string]chan T, sym string, p T, kind, symbol string) {
	prefix := sym + "\x00"
	sent := false
	for key, ch := range subs {
		if strings.HasPrefix(key, prefix) {
			trySendLocked(s, ch, p, kind, symbol)
			sent = true
		}
	}
	if sent {
		return
	}
	if kind == "instrument_info" {
		s.logger.Info("tcp_server: instrument_info with no trader subscriber (data-only symbol)", "symbol", symbol)
	} else {
		s.logger.Warn("tcp_server: "+kind+" with NO subscriber — dropped", "symbol", symbol, "payload", fmt.Sprintf("%+v", p))
	}
}

// dispatchToOwner routes an account-tagged payload (fill / position_close /
// close_rejected) to the subscriber that owns (symbol, account). With two
// same-symbol traders this delivers each account's frames to its OWN trader.
// EMPTY symbol (legacy AddOn) → primary trading symbol. EMPTY account (legacy
// AddOn) → every subscriber of the symbol (single-trader back-compat). A specific
// account with no subscriber is DROPPED — an account-tagged order frame is never
// cross-delivered to a different account's trader.
func dispatchToOwner[T any](s *TCPServer, subs map[string]chan T, symbol, account string, p T, kind string) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		sym = strings.ToUpper(strings.TrimSpace(s.BarsSubscribeSymbol()))
	}
	acct := strings.TrimSpace(account)
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if acct != "" {
		if ch := subs[sym+"\x00"+acct]; ch != nil {
			trySendLocked(s, ch, p, kind, symbol)
			return
		}
		s.logger.Warn("tcp_server: "+kind+" for account with NO subscriber — dropped (not cross-delivered)", "symbol", symbol, "account", acct)
		return
	}
	broadcastLocked(s, subs, sym, p, kind, symbol)
}

// broadcastToSymbol routes an account-less payload (instrument_info) to every
// subscriber of the symbol. EMPTY symbol → primary trading symbol.
func broadcastToSymbol[T any](s *TCPServer, subs map[string]chan T, symbol string, p T, kind string) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		sym = strings.ToUpper(strings.TrimSpace(s.BarsSubscribeSymbol()))
	}
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	broadcastLocked(s, subs, sym, p, kind, symbol)
}

// subscribeFor installs (or REPLACES) the (symbol, account) subscriber channel.
// Re-subscribing the SAME (symbol, account) closes the previous channel —
// terminating a reloaded-away trader instance's consumer goroutine (the pre-P5.4
// reload-leak fix) — but a DIFFERENT account on the same symbol gets its own key
// and is never evicted (the H3 same-symbol collision fix).
func subscribeFor[T any](s *TCPServer, subs *map[string]chan T, symbol, account string) <-chan T {
	s.ensureRouters()
	key := subKey(symbol, account)
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if *subs == nil {
		*subs = make(map[string]chan T)
	}
	if old, ok := (*subs)[key]; ok {
		close(old)
	}
	ch := make(chan T, fillChannelBuffer)
	(*subs)[key] = ch
	return ch
}

// SubscribeFillsFor returns the fill stream for (symbol, account) — router-fed.
func (s *TCPServer) SubscribeFillsFor(symbol, account string) <-chan FillPayload {
	return subscribeFor(s, &s.fillSubs, symbol, account)
}

// SubscribeOrderUpdatesFor returns the order-state stream for (symbol, account)
// — PHASE 2 armed orders.
func (s *TCPServer) SubscribeOrderUpdatesFor(symbol, account string) <-chan OrderUpdatePayload {
	return subscribeFor(s, &s.orderUpdSubs, symbol, account)
}

// ── Coordinated order_update fan-out ─────────────────────────────────────────
//
// subscribeFor installs-or-REPLACES the one channel per (symbol, account).
// The armed executor and the picture broker consumer both want order_update
// frames for the SAME trader; each subscribing directly means the second
// subscribe closes the first consumer's channel and it silently stops
// receiving (the 2026-08-27 class, now reproducible between two consumers).
//
// The fan-out owns THE single direct subscription per key and copies every
// received frame to each registered listener. A listener's channel closes
// only when the underlying subscription dies (server teardown, or a direct
// SubscribeOrderUpdatesFor from code that has not migrated); consumers keep
// their existing self-heal contract and simply re-listen.

// orderUpdateFanout is one key's shared subscription + its listeners.
type orderUpdateFanout struct {
	src       <-chan OrderUpdatePayload
	listeners map[int]chan OrderUpdatePayload
}

// ListenOrderUpdates registers a fan-out listener for (symbol, account) and
// returns its channel plus an unregister func. The FIRST listener creates the
// single direct subscription; later listeners share it — no listener ever
// evicts another. remove() unregisters and closes THIS listener only.
func (s *TCPServer) ListenOrderUpdates(symbol, account string) (<-chan OrderUpdatePayload, func()) {
	key := subKey(symbol, account)
	s.ouFanMu.Lock()
	defer s.ouFanMu.Unlock()
	if s.ouFanouts == nil {
		s.ouFanouts = map[string]*orderUpdateFanout{}
	}
	f := s.ouFanouts[key]
	if f == nil {
		f = &orderUpdateFanout{
			src:       s.SubscribeOrderUpdatesFor(symbol, account),
			listeners: map[int]chan OrderUpdatePayload{},
		}
		s.ouFanouts[key] = f
		go s.runOrderUpdateFanout(key, f)
	}
	id := s.ouFanNext
	s.ouFanNext++
	ch := make(chan OrderUpdatePayload, fillChannelBuffer)
	f.listeners[id] = ch
	return ch, func() {
		s.ouFanMu.Lock()
		defer s.ouFanMu.Unlock()
		if cur, ok := s.ouFanouts[key]; ok && cur == f {
			if lch, exists := cur.listeners[id]; exists {
				delete(cur.listeners, id)
				close(lch)
			}
		}
	}
}

// runOrderUpdateFanout copies every frame the direct subscription receives to
// every current listener. When the direct subscription dies, every listener's
// channel is closed (the consumers' self-heal signal) and the key is forgotten
// so the next listener re-establishes the subscription.
func (s *TCPServer) runOrderUpdateFanout(key string, f *orderUpdateFanout) {
	for {
		u, open := <-f.src
		if !open {
			break
		}
		s.ouFanMu.Lock()
		for id, lch := range f.listeners {
			select {
			case lch <- u:
			default:
				s.logger.Warn("tcp_server: order_update listener full — dropped", "listener", id, "signal", u.SignalID)
			}
		}
		s.ouFanMu.Unlock()
	}
	s.ouFanMu.Lock()
	if s.ouFanouts[key] == f {
		delete(s.ouFanouts, key)
	}
	for id, lch := range f.listeners {
		delete(f.listeners, id)
		close(lch)
	}
	s.ouFanMu.Unlock()
}

// FeedOrderUpdateForTest routes one order_update payload into the router (the
// same channel the wire reader feeds in production) so cross-package tests
// can drive the REAL subscription → router → fan-out → consumer path.
func (s *TCPServer) FeedOrderUpdateForTest(p OrderUpdatePayload) {
	s.orderUpdCh <- p
}

// FeedFillForTest routes one fill payload into the fill router — the same
// channel the wire reader feeds — so cross-package tests can exercise the
// real fill subscription and the trader's received-fill retention.
func (s *TCPServer) FeedFillForTest(p FillPayload) {
	s.fillCh <- p
}

// SubscribeClosesFor returns the position_close stream for (symbol, account).
func (s *TCPServer) SubscribeClosesFor(symbol, account string) <-chan PositionClosePayload {
	return subscribeFor(s, &s.closeSubs, symbol, account)
}

// SubscribeRejectsFor returns the close-rejection stream for (symbol, account).
func (s *TCPServer) SubscribeRejectsFor(symbol, account string) <-chan PositionCloseRejectedPayload {
	return subscribeFor(s, &s.rejectSubs, symbol, account)
}

// SubscribeInstrumentInfoFor returns the instrument_info stream for (symbol,
// account). instrument_info is broadcast to all accounts of the symbol, but the
// subscription is still per-(symbol,account) so each trader has its own channel.
func (s *TCPServer) SubscribeInstrumentInfoFor(symbol, account string) <-chan InstrumentInfoPayload {
	return subscribeFor(s, &s.instrSubs, symbol, account)
}

// SubscribeBarsHistoryFor returns the data and error streams for one named-
// contract pull, keyed by the caller's request id. HISTORY IMPORT (wave 101):
// the importer installs a subscription BEFORE sending the request frame. Both
// channels are buffered so the C# side's chunk stream never stalls the read
// loop.
func (s *TCPServer) SubscribeBarsHistoryFor(requestID string) (<-chan BarsHistoryDataPayload, <-chan BarsHistoryErrorPayload) {
	s.ensureRouters()
	key := strings.TrimSpace(requestID)
	s.histSubMu.Lock()
	defer s.histSubMu.Unlock()
	if s.historyDataSubs == nil {
		s.historyDataSubs = make(map[string]chan BarsHistoryDataPayload)
		s.historyErrSubs = make(map[string]chan BarsHistoryErrorPayload)
	}
	if old, ok := s.historyDataSubs[key]; ok {
		close(old)
	}
	if old, ok := s.historyErrSubs[key]; ok {
		close(old)
	}
	dataCh := make(chan BarsHistoryDataPayload, 32)
	errCh := make(chan BarsHistoryErrorPayload, 4)
	s.historyDataSubs[key] = dataCh
	s.historyErrSubs[key] = errCh
	return dataCh, errCh
}

// UnsubscribeBarsHistoryFor removes one pull's channels (importer teardown).
func (s *TCPServer) UnsubscribeBarsHistoryFor(requestID string) {
	key := strings.TrimSpace(requestID)
	s.histSubMu.Lock()
	defer s.histSubMu.Unlock()
	if ch, ok := s.historyDataSubs[key]; ok {
		close(ch)
		delete(s.historyDataSubs, key)
	}
	if ch, ok := s.historyErrSubs[key]; ok {
		close(ch)
		delete(s.historyErrSubs, key)
	}
}

// SendBarsHistoryRequest asks the AddOn to pull one named contract's history
// over [from, to). Immediate command: errors if no NT client is connected, so
// the importer can state `unavailable` instead of hanging (three-state honesty).
func (s *TCPServer) SendBarsHistoryRequest(payload BarsHistoryRequestPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameBarsHistoryRequest, payload)
	s.writeMu.Unlock()
	return err
}

// barIngestMsg is the internal envelope passed from the socket read loop
// to the drain goroutine that writes into barCache. The historical flag
// distinguishes the one-shot seed batch from streaming updates so the
// drainer can apply the correct cache method.
type barIngestMsg struct {
	historical bool
	symbol     string
	timeframe  string
	// contract: the front month the AddOn named on this frame ("" = unknown).
	contract string
	bars     []Bar
}

// barIngestChannelBuffer caps the bar ingest channel. FORENSICS HYGIENE
// (2026-08-28): the 2026-08-27 17:00 CT Globex reopen flooded ~507 frames/s
// and overran the old fixed 256 — 1399 drop-oldest evictions in ~4 minutes
// (zero closes lost, but the margin was structurally thin). The cap is
// INGEST_QUEUE_CAP. S-LIST CLOSER (2026-08-27): the default was raised
// 1024 → 4096 after the 2026-08-27 21:42 flood touched 1024/1024 (1
// intrabar drop); the live high-water mark stays surfaced in the 1-line/min
// ingest summary (peak_depth).
func ingestQueueCap() int {
	if v := os.Getenv("INGEST_QUEUE_CAP"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 4096
}

// Plan 4.4 Stage 2 — default auto-subscribe parameters. The auto-subscribed set
// is the CHART display set (the dashboard MNQ chart offers these timeframe
// buttons), NOT the kernel's decision timeframes — the kernel reads the
// strategy's SelectedTimeframes (store/strategy.go; currently [5m,15m,1h]) and
// is unaffected by adding more chart subscriptions here. The C# AddOn
// (VLBarsSubscriptionManager.MapTimeframe) subscribes whatever timeframes this
// envelope lists; all 7 below are native NT8 BarsPeriodType (Minute 1/3/5/15/30,
// Minute*60 = 1h, Day = 1d) so no AddOn change is needed. bars_back stays a
// single value (the frozen wire carries one per envelope); 500 is the
// spec-recommended depth across all timeframes.
var (
	defaultAutoBarsSymbol = "MNQ"
	// Phase 4b — the 14-timeframe chart set (was 7). Every entry is already
	// supported by the AddOn's MapTimeframe + the Go normalizer; the running AddOn
	// subscribes them on the bot's next reconnect (no NT8 redeploy needed for 4b).
	defaultAutoBarsTimeframes = []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
	// 2000 (was 500): the default 500 1m-bars only reach ~8h of trading time —
	// shorter than an overnight+ NT8 outage, so a re-seed never even requested
	// the missing window and a feed-down gap could never self-heal. 2000 1m-bars
	// span ~33h of trading, enough for a fresh BarsRequest to pull the gap window
	// from the provider's (Tradovate) historical server and backfill holes left
	// by NT8 downtime. Providers cap deep requests on coarse timeframes, so this
	// is safe across the 14-tf set.
	defaultAutoBarsBack = 2000
)

// timedSignal retains enqueue metadata across retries. Freshness is proved
// exclusively by payload.Timestamp, never by the queue timestamp.
type timedSignal struct {
	payload   SignalPayload
	timestamp time.Time
	// attempted is true once a write of this signal's frame was STARTED on ANY
	// connection. WriteFrame can fail after some bytes left the socket, so an
	// attempted signal may have reached the AddOn; only a signal with zero bytes
	// ever written provably never left this process (W-ONE-BUTTON M2, CTO
	// condition 1 — the maintenance-hold drop settles only those).
	attempted bool
}

// ListenAddr returns the NT8 bridge listen address: TCPListenAddr unless
// NT_TCP_LISTEN_ADDR overrides it. The listener is a process-singleton, so a
// SECOND backend on the same host (demo preview, sandbox, integration test)
// could never create a NinjaTrader trader — "bind: address already in use"
// failed trader creation, which then 404s every /api/plan/* call for it. The
// override lets a second instance bind elsewhere; unset = byte-identical
// behavior for the live bot.
func ListenAddr() string {
	if v := strings.TrimSpace(os.Getenv("NT_TCP_LISTEN_ADDR")); v != "" {
		return v
	}
	return TCPListenAddr
}

// NewTCPServer constructs a server bound to ListenAddr(). The listener is
// not opened until Start. Pass nil to use the default slog logger.

// barUpdateLogSample — T8 (2026-08-27): the per-frame bar_update log sampler
// (INFO spam ate the journald cap; DEBUG + 1-in-N now). Zero literals.
func barUpdateLogSample() int64 {
	if v := os.Getenv("BAR_UPDATE_LOG_SAMPLE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 500
}

func NewTCPServer(logger *slog.Logger) *TCPServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPServer{
		addr:       ListenAddr(),
		orderSnaps: NewOrderSnapshotCache(),
		fillCh:     make(chan FillPayload, fillChannelBuffer),
		orderUpdCh: make(chan OrderUpdatePayload, fillChannelBuffer), closeCh: make(chan PositionClosePayload, fillChannelBuffer),
		rejectCh:      make(chan PositionCloseRejectedPayload, fillChannelBuffer),
		instrCh:       make(chan InstrumentInfoPayload, fillChannelBuffer),
		barCache:      NewBarCache(0),
		barIngestCh:   make(chan barIngestMsg, ingestQueueCap()),
		acctBalances:  make(map[string]AccountBalancePayload),
		acctPositions: make(map[string][]OpenPosition),
		barsSubscribe: BarsSubscribePayload{
			Symbol:     defaultAutoBarsSymbol,
			Timeframes: append([]string(nil), defaultAutoBarsTimeframes...),
			BarsBack:   defaultAutoBarsBack,
		},
		logger: logger,
	}
}

// BarCache exposes the bar cache for the kernel (Stage 3) and chart
// relay (Stage 4). The cache is goroutine-safe; readers receive snapshot
// copies via Get and never block writers for long.
func (s *TCPServer) BarCache() *BarCache { return s.barCache }

// FarSideBuildID returns the far-side AddOn's reported build id ("" until the
// first heartbeat carries it). E7 capability handshake (2026-08-30).
func (s *TCPServer) FarSideBuildID() string {
	v, _ := s.farSideBuild.Load().(string)
	return v
}

// AccountState returns the latest account_balance snapshot received from the
// C# AddOn (Plan 4.11) and whether one has arrived yet. TCPTrader.GetBalance
// uses it to serve the real SIM account instead of the $50k mock; ok=false
// means no frame yet (caller falls back to a documented placeholder).
func (s *TCPServer) AccountState() (AccountBalancePayload, bool) {
	// Return the snapshot for the currently selected account. Balances are keyed
	// per-account (Issue 2A) so switching accounts no longer clobbers one slot.
	s.accountsListMu.RLock()
	current := s.currentAccount
	s.accountsListMu.RUnlock()
	return s.AccountStateFor(current)
}

// AccountStateFor returns the latest account_balance snapshot for a specific
// account name and whether one has arrived yet. ok=false for an unknown/empty
// account (caller falls back to a documented placeholder).
func (s *TCPServer) AccountStateFor(account string) (AccountBalancePayload, bool) {
	s.acctMu.RLock()
	defer s.acctMu.RUnlock()
	if account == "" || s.acctBalances == nil {
		return AccountBalancePayload{}, false
	}
	p, ok := s.acctBalances[account]
	return p, ok
}

// PositionsFor returns the latest open-position snapshot for an account and
// whether a positions frame has arrived for it yet. ok=false means no snapshot
// (caller falls back to the fill-derived cache). A non-nil empty slice means
// the account is known-flat. Returns a defensive copy.
func (s *TCPServer) PositionsFor(account string) ([]OpenPosition, bool) {
	s.acctMu.RLock()
	defer s.acctMu.RUnlock()
	if account == "" || s.acctPositions == nil {
		return nil, false
	}
	v, ok := s.acctPositions[account]
	if !ok {
		return nil, false
	}
	out := make([]OpenPosition, len(v))
	copy(out, v)
	return out, true
}

// GetAccountsList returns the list of available NT accounts discovered by the
// C# AddOn (Plan 4 Stage 4). Returns a copy of the account list and the current
// account name. If no accounts_list frame has been received yet, returns nil + "".
func (s *TCPServer) GetAccountsList() ([]AccountInfo, string) {
	s.accountsListMu.RLock()
	defer s.accountsListMu.RUnlock()
	// Defensive copy so caller can't modify our internal list
	var accounts []AccountInfo
	if len(s.accountsList) > 0 {
		accounts = make([]AccountInfo, len(s.accountsList))
		copy(accounts, s.accountsList)
	}
	return accounts, s.currentAccount
}

// AccountsList returns the latest accounts_list payload received from the C# AddOn
// and a boolean indicating if one has arrived yet. This is the test-facing version.
// Production code calls GetAccountsList(). ok=false means no frame yet.
func (s *TCPServer) AccountsList() (AccountsListPayload, bool) {
	s.accountsListMu.RLock()
	defer s.accountsListMu.RUnlock()
	if len(s.accountsList) == 0 {
		return AccountsListPayload{}, false
	}
	return AccountsListPayload{Accounts: s.accountsList}, true
}

// CurrentAccount returns the currently selected account name (nil/empty if none selected).
// Test-facing accessor. Returns a copy of the account name, not a pointer to internal state.
func (s *TCPServer) CurrentAccount() *string {
	s.accountsListMu.RLock()
	defer s.accountsListMu.RUnlock()
	if s.currentAccount == "" {
		return nil
	}
	// Return a pointer to a copy, not to internal state
	acct := s.currentAccount
	return &acct
}

// SetAccountsList updates the account list (called by readLoop on accounts_list frame)
// and optionally sets the current account.
func (s *TCPServer) SetAccountsList(accounts []AccountInfo, current string) {
	s.accountsListMu.Lock()
	defer s.accountsListMu.Unlock()
	// Defensive copy of the slice
	s.accountsList = make([]AccountInfo, len(accounts))
	copy(s.accountsList, accounts)
	s.currentAccount = current
}

// SetBarsSubscribe overrides the auto-subscribe parameters sent on each
// (re)connect. Stage 3 will call this with the active strategy's
// SelectedTimeframes; until then the server uses Balanced Strategy
// defaults (MNQ at 5m/15m/1h, 500 bars back).
func (s *TCPServer) SetBarsSubscribe(p BarsSubscribePayload) {
	s.barsSubMu.Lock()
	defer s.barsSubMu.Unlock()
	// Defensive copy of the timeframes slice to detach from caller storage.
	p.Timeframes = append([]string(nil), p.Timeframes...)
	s.barsSubscribe = p
}

// SetBarsSubscribeSymbol overrides ONLY the auto-subscribe symbol (preserving the
// configured timeframes + bars-back) so the bar subscription tracks the ACTIVE
// trader's symbol instead of the hardwired default. Phase 2 — the ninjatrader
// trader calls this with its symbol at creation; on the next (re)connect the
// AddOn subscribes to that root and NT8 resolves the qualified front-month
// contract (VLContractResolver). Empty symbol is ignored (keeps the default).
// P5.4: this is now a REGISTRATION, not an overwrite — the FIRST registered
// trader claims the legacy primary slot (replacing the hardwired default);
// subsequent DIFFERENT roots are appended to the trading registry so a second
// trader (e.g. ES) can never silently kill the first trader's (MNQ) bar feed.
func (s *TCPServer) SetBarsSubscribeSymbol(symbol string) {
	if symbol == "" {
		return
	}
	s.barsSubMu.Lock()
	defer s.barsSubMu.Unlock()
	if !s.primaryRegistered {
		// First trader registration — claim the primary slot (legacy behavior:
		// the default "MNQ" template symbol is replaced by the real trader's).
		s.barsSubscribe.Symbol = symbol
		s.primaryRegistered = true
		s.tradingSymbols = []string{symbol}
		return
	}
	for _, t := range s.tradingSymbols {
		if strings.EqualFold(t, symbol) {
			return // idempotent re-registration (trader reload)
		}
	}
	s.tradingSymbols = append(s.tradingSymbols, symbol)
	s.logger.Info("tcp_server: registered ADDITIONAL trading symbol (P5.4 multi-trader)",
		"symbol", symbol, "primary", s.barsSubscribe.Symbol)
}

// TradingSymbols returns the registered trading roots (primary first).
func (s *TCPServer) TradingSymbols() []string {
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	if len(s.tradingSymbols) == 0 {
		return []string{s.barsSubscribe.Symbol}
	}
	return append([]string(nil), s.tradingSymbols...)
}

// BarsSubscribeSymbol returns the current auto-subscribe symbol (the root the
// AddOn is told to stream). Exported for wiring/tests.
func (s *TCPServer) BarsSubscribeSymbol() string {
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	return s.barsSubscribe.Symbol
}

func (s *TCPServer) currentBarsSubscribe() BarsSubscribePayload {
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	return BarsSubscribePayload{
		Symbol:     s.barsSubscribe.Symbol,
		Timeframes: append([]string(nil), s.barsSubscribe.Timeframes...),
		BarsBack:   s.barsSubscribe.BarsBack,
	}
}

// AddBarsSubscribeSymbols registers EXTRA symbol roots (e.g. "ES", "NQ") to be
// auto-subscribed alongside the primary on each (re)connect, sharing the
// primary's timeframes + bars-back. Blank entries, duplicates, and the primary
// itself are skipped (case-insensitive). P5.1 — with no extras registered the
// server's behavior is byte-identical to the single-symbol code.
func (s *TCPServer) AddBarsSubscribeSymbols(symbols ...string) {
	s.barsSubMu.Lock()
	defer s.barsSubMu.Unlock()
	for _, sym := range symbols {
		sym = strings.TrimSpace(sym)
		if sym == "" || strings.EqualFold(sym, s.barsSubscribe.Symbol) {
			continue
		}
		dup := false
		for _, existing := range s.extraBarsSymbols {
			if strings.EqualFold(existing, sym) {
				dup = true
				break
			}
		}
		if !dup {
			s.extraBarsSymbols = append(s.extraBarsSymbols, sym)
		}
	}
}

// SymbolSubState is one symbol's subscription lifecycle as seen Go-side (P5.3).
// State: "pending" (subscribe sent, no ack yet — also a pre-P5.3 AddOn that
// never acks), "subscribed" (+Contract), "error" (+Reason), "unsubscribed".
type SymbolSubState struct {
	State     string    `json:"state"`
	Contract  string    `json:"contract,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *TCPServer) setSubState(symbol, state, contract, reason string) {
	if symbol == "" {
		return
	}
	s.subStateMu.Lock()
	if s.subStates == nil {
		s.subStates = make(map[string]SymbolSubState)
	}
	s.subStates[strings.ToUpper(symbol)] = SymbolSubState{
		State: state, Contract: contract, Reason: reason, UpdatedAt: time.Now(),
	}
	s.subStateMu.Unlock()
}

// BarsSubscriptionStates snapshots the per-symbol subscription lifecycle for
// the owner API: every currently-configured root (primary + extras) plus any
// symbol that still has a recorded state (e.g. just-unsubscribed).
func (s *TCPServer) BarsSubscriptionStates() map[string]SymbolSubState {
	out := make(map[string]SymbolSubState)
	s.subStateMu.RLock()
	for k, v := range s.subStates {
		out[k] = v
	}
	s.subStateMu.RUnlock()
	// Configured roots with no ack yet → pending placeholder.
	for _, p := range s.barsSubscribePayloads() {
		k := strings.ToUpper(p.Symbol)
		if _, ok := out[k]; !ok && p.Symbol != "" {
			out[k] = SymbolSubState{State: "pending"}
		}
	}
	return out
}

// SubscribeBarsSymbol ADDS an extra root at runtime (P5.3): registers it (the
// next reconnect re-subscribes it too) and — if the AddOn is connected — sends
// the bars_subscribe frame NOW, cloning the primary's timeframes/bars-back.
// The C# side acks with subscribed{resolved_contract} or subscribe_error.
func (s *TCPServer) SubscribeBarsSymbol(symbol string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return fmt.Errorf("tcp_server: subscribe: empty symbol")
	}
	s.barsSubMu.RLock()
	isTrading := s.isTradingSymbolLocked(symbol)
	payload := BarsSubscribePayload{
		Symbol:     symbol,
		Timeframes: append([]string(nil), s.barsSubscribe.Timeframes...),
		BarsBack:   s.barsSubscribe.BarsBack,
	}
	s.barsSubMu.RUnlock()
	if isTrading {
		return fmt.Errorf("tcp_server: %q is already a TRADING symbol", symbol)
	}
	s.AddBarsSubscribeSymbols(symbol) // idempotent (dedup)
	s.setSubState(symbol, "pending", "", "")

	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("tcp_server: subscribe %s: not connected (registered; will subscribe on reconnect)", symbol)
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameBarsSubscribe, payload)
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("tcp_server: send bars_subscribe %s: %w", symbol, err)
	}
	s.logger.Info("tcp_server: sent runtime bars_subscribe", "symbol", symbol,
		"timeframes", payload.Timeframes, "bars_back", payload.BarsBack)
	return nil
}

// SetExtraBarsSymbolsFor REPLACES one OWNER's config-extras list (P5.4: keyed
// by the owning trader's primary symbol, so trader B's (re)load replaces only
// ITS extras — trader A's survive). The Exchange row stays the source of truth
// per trader; a symbol removed from a row doesn't linger for that owner.
func (s *TCPServer) SetExtraBarsSymbolsFor(owner string, symbols ...string) {
	s.barsSubMu.Lock()
	defer s.barsSubMu.Unlock()
	if s.extrasByOwner == nil {
		s.extrasByOwner = make(map[string][]string)
	}
	clean := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		if sym = strings.TrimSpace(sym); sym != "" {
			clean = append(clean, sym)
		}
	}
	s.extrasByOwner[strings.ToUpper(owner)] = clean
}

// SetExtraBarsSymbols is the legacy unowned form (owner "") — kept for tests
// and single-trader back-compat.
func (s *TCPServer) SetExtraBarsSymbols(symbols ...string) {
	s.SetExtraBarsSymbolsFor("", symbols...)
}

// UnsubscribeBarsSymbol removes an EXTRA root from the auto-subscribe list and
// sends a bars_unsubscribe frame so the AddOn disposes that root's BarsRequests
// (its updates stop; other roots keep streaming — the P5.1 dispose-one proof,
// and the P5.3 runtime-remove building block). The PRIMARY symbol (the live
// trader's instrument) is refused — never tear down the trading feed. Removal
// from the list happens even if the send fails (not connected), so a later
// reconnect won't re-subscribe the dropped root.
func (s *TCPServer) UnsubscribeBarsSymbol(symbol string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return fmt.Errorf("tcp_server: unsubscribe: empty symbol")
	}
	s.barsSubMu.Lock()
	if s.isTradingSymbolLocked(symbol) {
		s.barsSubMu.Unlock()
		return fmt.Errorf("tcp_server: refusing to unsubscribe TRADING symbol %q (a live trader's feed)", symbol)
	}
	kept := s.extraBarsSymbols[:0]
	for _, existing := range s.extraBarsSymbols {
		if !strings.EqualFold(existing, symbol) {
			kept = append(kept, existing)
		}
	}
	s.extraBarsSymbols = kept
	// Also drop it from every owner's config-extras list (an explicit runtime
	// removal overrides config until that owner's next reload re-asserts it).
	for owner, extras := range s.extrasByOwner {
		keptO := extras[:0]
		for _, e := range extras {
			if !strings.EqualFold(e, symbol) {
				keptO = append(keptO, e)
			}
		}
		s.extrasByOwner[owner] = keptO
	}
	s.barsSubMu.Unlock()

	// P5.3 clean teardown: drop the symbol's cached bars (write-to-own-cache's
	// counterpart — removed symbol leaves no stale data) + record the state.
	// Safe: the PRIMARY was refused above, so the trading cache is untouchable.
	s.barCache.PurgeSymbol(symbol)
	s.setSubState(symbol, "unsubscribed", "", "")

	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("tcp_server: unsubscribe %s: not connected (removed from auto-subscribe list)", symbol)
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameBarsUnsubscribe, BarsUnsubscribePayload{Symbol: symbol})
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("tcp_server: send bars_unsubscribe %s: %w", symbol, err)
	}
	s.logger.Info("tcp_server: sent bars_unsubscribe", "symbol", symbol)
	return nil
}

// isSubscribedBarsSymbol reports whether a symbol is in the subscribed set
// (the primary or an extra root, case-insensitive). The bar ingest path uses
// it to REJECT mistagged/unsubscribed bars loudly — the P5.2 split-brain
// defense: a bar tagged symbol X may only ever reach cache[X], and only if X
// was actually subscribed.
func (s *TCPServer) isSubscribedBarsSymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	for _, root := range s.subscribedRootsLocked() {
		if strings.EqualFold(root, symbol) {
			return true
		}
	}
	return false
}

// isTradingSymbolLocked: symbol is a REGISTERED trading root (or the primary
// slot) — never tear these down at runtime. Caller holds barsSubMu.
func (s *TCPServer) isTradingSymbolLocked(symbol string) bool {
	if strings.EqualFold(symbol, s.barsSubscribe.Symbol) {
		return true
	}
	for _, t := range s.tradingSymbols {
		if strings.EqualFold(t, symbol) {
			return true
		}
	}
	return false
}

// subscribedRootsLocked returns the dedup'd union of every root to subscribe,
// primary FIRST: trading registry, then per-owner config extras, then the
// unowned (env/runtime) extras. Caller holds barsSubMu (read).
func (s *TCPServer) subscribedRootsLocked() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	add := func(sym string) {
		if sym == "" || seen[strings.ToUpper(sym)] {
			return
		}
		seen[strings.ToUpper(sym)] = true
		out = append(out, sym)
	}
	add(s.barsSubscribe.Symbol)
	for _, t := range s.tradingSymbols {
		add(t)
	}
	for _, extras := range s.extrasByOwner {
		for _, e := range extras {
			add(e)
		}
	}
	for _, e := range s.extraBarsSymbols {
		add(e)
	}
	return out
}

// barsSubscribePayloads returns the ordered auto-subscribe frames: the primary
// first (the exact pre-P5 payload), then one clone per additional root with
// only the symbol swapped. Single trader + no extras → exactly [primary],
// byte-identical to the old single-frame behavior.
func (s *TCPServer) barsSubscribePayloads() []BarsSubscribePayload {
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	roots := s.subscribedRootsLocked()
	out := make([]BarsSubscribePayload, 0, len(roots))
	for _, sym := range roots {
		out = append(out, BarsSubscribePayload{
			Symbol:     sym,
			Timeframes: append([]string(nil), s.barsSubscribe.Timeframes...),
			BarsBack:   s.barsSubscribe.BarsBack,
		})
	}
	return out
}

// Start binds the listener and spins up the accept loop. The returned error
// is non-nil only if the bind fails. Stop releases the port.
func (s *TCPServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("tcp_server: listen %s: %w", s.addr, err)
	}
	s.listener = ln
	cctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.runCtx = cctx // P5.4 — the lazy fan-out router binds to the server lifetime
	s.wg.Add(2)
	go s.acceptLoop(cctx)
	// Plan 4.4 Stage 2 — bar ingest drain goroutine, decouples cache
	// writes from the socket read loop.
	go s.drainBarIngest(cctx)
	// U1 3.3 — wire-liveness line every 60s (not wg-tracked: exits with ctx).
	go s.livenessReporter(cctx)
	// W-ONE-BUTTON M2 site 7 — push the installation hold (silent when unheld).
	s.maint.mu.Lock()
	s.maint.tick, s.maint.resend = maintenanceTick, maintenanceResend
	s.maint.mu.Unlock()
	go s.maintenanceLoop(cctx.Done(), maintenanceTick)
	s.logger.Info("tcp_server: listening", "addr", s.addr)
	return nil
}

// livenessReporter (U1 3.3, stale-bar dispatch 2026-08-19) emits one line per
// minute answering "feed or math" at a glance: how long since ANY frame, the
// frame rate, and each TF's newest bar age (open-stamp + interval = NT8's own
// stamp of the bar). Silent only when the server has never seen a frame AND
// holds no bars (pre-first-connect boot).
func (s *TCPServer) livenessReporter(ctx context.Context) {
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	var prevTotal int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			total := s.framesTotal.Load()
			perMin := total - prevTotal
			prevTotal = total
			last := s.lastFrameUnixMs.Load()
			nowMs := time.Now().UnixMilli()
			lastAge := "never"
			if last > 0 {
				lastAge = fmt.Sprintf("%ds", (nowMs-last)/1000)
			}
			tfAges := ""
			for _, tf := range []string{"1m", "5m", "15m"} {
				bars := s.BarCache().Get("MNQ", tf)
				if len(bars) == 0 {
					tfAges += " " + tf + "=none"
					continue
				}
				// Age of the newest bar's OPEN: 0..period while the forming bar
				// is live — always non-negative and readable at a glance. (The
				// close-stamp variant went negative for forming bars.)
				age := (nowMs - bars[len(bars)-1].T) / 1000
				tfAges += fmt.Sprintf(" %s=%ds", tf, age)
			}
			if last == 0 && tfAges == " 1m=none 5m=none 15m=none" {
				continue // pre-first-connect boot — nothing to report yet
			}
			s.logger.Info("wire_liveness",
				"last_frame_age", lastAge, "frames_per_min", perMin, "bar_age", strings.TrimSpace(tfAges))
		}
	}
}

// Stop cancels the accept loop, closes the listener, drops any active
// connection, and waits for all goroutines to exit. Safe to call once.
func (s *TCPServer) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.closeConn()
	s.wg.Wait()
	return nil
}

// SendSignal queues a signal for delivery to the connected client. If no
// client is currently connected, the signal is buffered and flushed on the
// next successful accept (subject to TCPStaleSignalAge).
func (s *TCPServer) SendSignal(payload SignalPayload) error {
	if err := s.checkSignalAge(payload, time.Now()); err != nil {
		return err
	}
	// A2 (G1) — stamp the monotonic seq + register (trader_id, account, signal_id)
	// so the AddOn's echo on the paired ack/fill/close can be verified.
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID)
	s.pendingMu.Lock()
	s.pending = append(s.pending, timedSignal{payload: payload, timestamp: time.Now()})
	s.pendingMu.Unlock()
	heldDropped, err := s.flushPendingReportFor(payload.SignalID)
	if err == nil {
		err = ownDropError(payload.SignalID, heldDropped)
	}
	recordResearchSignal(payload, err)
	return err
}

// SendClosePosition tells the connected AddOn to flatten the symbol's position.
// Unlike SendSignal it is NOT queued — a close is an immediate command; if no
// client is connected it errors so the caller can report it.
func (s *TCPServer) SendClosePosition(payload ClosePositionPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID) // A2 (G1)
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameClosePosition, payload)
	s.writeMu.Unlock()
	return err
}

// SendMoveStop asks the AddOn to move a resting stop to a new price without
// closing the position (auto-breakeven). Immediate command like
// SendClosePosition; errors if no client is connected so the caller can report it.
func (s *TCPServer) SendMoveStop(payload MoveStopPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID) // A2 (G1)
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameMoveStop, payload)
	s.writeMu.Unlock()
	return err
}

// SendCancelOrder (PHASE 2 armed orders) asks the AddOn to cancel a working
// resting limit entry and/or its bracket legs. Immediate command.
// SendPlaceProtectiveStop puts a standalone protective stop on the wire (D5).
func (s *TCPServer) SendPlaceProtectiveStop(payload PlaceProtectiveStopPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID)
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FramePlaceProtectiveStop, payload)
	s.writeMu.Unlock()
	return err
}

func (s *TCPServer) SendCancelOrder(payload CancelOrderPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID)
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameCancelOrder, payload)
	s.writeMu.Unlock()
	return err
}

// SendModifyBracket (PHASE 2 armed orders) asks the AddOn to modify the live
// bracket SL/TP in place (the safe Change pattern). Immediate command.
func (s *TCPServer) SendModifyBracket(payload ModifyBracketPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID)
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameModifyBracket, payload)
	s.writeMu.Unlock()
	return err
}

// SendAccountSelect tells the connected AddOn to switch to a different account.
// Like SendClosePosition, this is an immediate command (not queued); if no
// client is connected it errors so the caller can report it.
func (s *TCPServer) SendAccountSelect(payload AccountSelectPayload) error {
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameAccountSelect, payload)
	s.writeMu.Unlock()
	if err == nil {
		s.logger.Info("tcp_server: account select sent", "account", payload.Account)
	}
	return err
}

// Fills returns the inbound fill channel. TCPTrader subscribes here.
func (s *TCPServer) Fills() <-chan FillPayload { return s.fillCh }

// ClosedPositions returns the channel of position_close frames (SL/TP exits).
// Consumed by the NT close-sync to mark trader_positions CLOSED.
func (s *TCPServer) ClosedPositions() <-chan PositionClosePayload { return s.closeCh }

// CloseRejections returns the channel of position_close_rejected frames — an
// exit/flatten the SIM/broker rejected (e.g. "no market data" with the feed down).
// The position is STILL OPEN in NT8; consumers must alarm, not record a close.
func (s *TCPServer) CloseRejections() <-chan PositionCloseRejectedPayload { return s.rejectCh }

// FeedStatus returns the latest NT8 price-feed status ("" until the first
// feed_status frame). Prefer IsFeedConnected for gating.
func (s *TCPServer) FeedStatus() string {
	s.feedMu.RLock()
	defer s.feedMu.RUnlock()
	return s.feedStatus
}

// feedFreshWindow: a live bar_update within this window proves the NT8 feed is
// actually delivering data, so IsFeedConnected treats it as connected even if the
// last edge-triggered feed_status frame latched a stale "Disconnected".
const feedFreshWindow = 90 * time.Second

// IsFeedConnected reports whether the NT8 price feed is usable. DEFAULT-ALLOW:
// before any feed_status frame is received (startup) it returns true so a healthy
// bot is never false-halted on mere absence of the signal. On an explicit
// non-"Connected" status it returns false — UNLESS a live bar_update arrived
// within feedFreshWindow, which proves the feed is up regardless of a stale
// edge-triggered status (feed_status is sent only on change, so a momentary flap
// can latch a "Disconnected" that never self-heals while bars keep flowing).
func (s *TCPServer) IsFeedConnected() bool {
	s.feedMu.RLock()
	status := s.feedStatus
	s.feedMu.RUnlock()
	if status == "" || status == "Connected" {
		return true
	}
	// Stale-status override: trust live bars over an out-of-date status flag.
	if last := s.lastBarNano.Load(); last > 0 && time.Since(time.Unix(0, last)) < feedFreshWindow {
		return true
	}
	return false
}

// InstrumentInfo returns the channel of instrument_info frames — the resolved
// NT8 instrument's real specs (point value, tick). Consumers cross-check the
// hardcoded FuturesPointValue/FuturesTickSize tables and surface any drift.
func (s *TCPServer) InstrumentInfo() <-chan InstrumentInfoPayload { return s.instrCh }

// IsConnected reports whether a client is currently connected.
func (s *TCPServer) IsConnected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil
}

// Addr returns the actual listening address (useful for tests using :0).
func (s *TCPServer) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

// SetStaleSignalAgeForTest overrides the stale-signal cutoff for the
// TestTCPServer_StaleSignalDropped test. Production code never calls this.
func (s *TCPServer) SetStaleSignalAgeForTest(d time.Duration) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.staleOverride = d
}

// SetAddrForTest overrides the bind address before Start (used by tests that
// need an ephemeral port via "127.0.0.1:0"). Production callers leave the
// constructor default in place.
func (s *TCPServer) SetAddrForTest(addr string) { s.addr = addr }

// ListenAddrForTest returns the bound listen address (valid after Start) so
// in-process fixtures can dial the real accept loop and observe sent frames.
func (s *TCPServer) ListenAddrForTest() net.Addr {
	return s.listener.Addr()
}

// SeedPositionsForTest injects a per-account open-position snapshot without a wire
// frame (mirrors the FramePositions receive path). Tests only; production is fed by
// the C# AddOn's `positions` frames.
func (s *TCPServer) SeedPositionsForTest(account string, ps []OpenPosition) {
	s.acctMu.Lock()
	if s.acctPositions == nil {
		s.acctPositions = make(map[string][]OpenPosition)
	}
	s.acctPositions[account] = ps
	s.acctMu.Unlock()
}

// SeedAccountBalanceForTest injects a per-account balance snapshot (normally set
// by account_balance frames). Tests only.
func (s *TCPServer) SeedAccountBalanceForTest(account string, p AccountBalancePayload) {
	s.acctMu.Lock()
	if s.acctBalances == nil {
		s.acctBalances = make(map[string]AccountBalancePayload)
	}
	s.acctBalances[account] = p
	s.acctMu.Unlock()
}

// SetCurrentAccountForTest sets the shared streamed "current"/display account
// (normally set by account_balance / account_select frames). Tests only.
func (s *TCPServer) SetCurrentAccountForTest(account string) {
	s.accountsListMu.Lock()
	s.currentAccount = account
	s.accountsListMu.Unlock()
}

func (s *TCPServer) staleAge() time.Duration {
	s.connMu.Lock()
	override := s.staleOverride
	s.connMu.Unlock()
	if override > 0 {
		return override
	}
	return TCPStaleSignalAge
}

func (s *TCPServer) acceptLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if t, ok := s.listener.(*net.TCPListener); ok {
			_ = t.SetDeadline(time.Now().Add(acceptLoopPollInterval))
		}
		c, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			s.logger.Warn("tcp_server: accept error", "err", err)
			continue
		}

		// Single concurrent client (spec L4359). Any second simultaneous
		// dial is closed immediately.
		s.connMu.Lock()
		if s.conn != nil {
			s.logger.Warn("tcp_server: rejecting concurrent client", "addr", c.RemoteAddr())
			_ = c.Close()
			s.connMu.Unlock()
			continue
		}
		s.conn = c
		s.lastAckTime = time.Now()
		s.connMu.Unlock()
		s.beginConnectionRecord(c, time.Now()) // W-ONE-BUTTON M2 site 7

		s.logger.Info("tcp_server: client connected", "addr", c.RemoteAddr())
		s.wg.Add(2)
		go s.readLoop(ctx, c)
		go s.heartbeatLoop(ctx, c)
		// A3 (G2) — declare the bound-account allowlist BEFORE flushing any queued
		// signals, so the AddOn's allowlist is established before it can execute one.
		s.sendAccountAllowlist(c)
		// W-ONE-BUTTON M2 site 7 — a held installation tells the AddOn BEFORE
		// the flush (which itself drops queued entries while held).
		s.pushMaintenance(c, time.Now())
		// Flush any signals queued during the disconnect window.
		_ = s.flushPending()
		// Plan 4.4 Stage 2 — auto-subscribe to bars on every accept so
		// the C# AddOn starts emitting bars_historical + bar_update
		// without operator action. Idempotent on the C# side per the
		// protocol contract (re-subscribing returns immediately).
		s.sendAutoBarsSubscribe(c)
	}
}

// ── BAR-TRUTH WAVE (2026-08-28) — deep backfill + three-way arbiter ────────
// S-1 demanded an arbiter: NT8 truth (the deep replay frames) vs the kernel
// cache vs the persisted bars table. The replay capture records count + FNV
// hash per (symbol,tf) during a capture window; the API then diffs the three.

type barTruthCapture struct {
	mu      sync.Mutex
	active  bool
	batches map[string]BarTruthBatch
}

type BarTruthBatch struct {
	Count  int    `json:"count"`
	Hash   uint64 `json:"hash"` // FNV-1a over T,O,H,L,C,V
	LastT  int64  `json:"last_t"`
	Symbol string `json:"symbol"`
	TF     string `json:"tf"`
}

// fnvAddBar folds one bar into an FNV-1a running hash (bit-exact over the
// 8-byte fields) so two bar sets with identical content always hash equal.
func fnvAddBar(h uint64, b Bar) uint64 {
	if h == 0 {
		h = 14695981039346656037
	}
	write := func(v float64) uint64 {
		u := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			h ^= (u >> (8 * i)) & 0xFF
			h *= 1099511628211
		}
		return h
	}
	h = write(float64(b.T))
	h = write(b.O)
	h = write(b.H)
	h = write(b.L)
	h = write(b.C)
	h = write(b.V)
	return h
}

var truthCapture barTruthCapture

func (s *TCPServer) beginBarTruthCapture(symbol, tf string) {
	truthCapture.mu.Lock()
	defer truthCapture.mu.Unlock()
	truthCapture.active = true
	truthCapture.batches = map[string]BarTruthBatch{}
	_ = symbol
	_ = tf
}

func (s *TCPServer) captureBarTruthReplay(symbol, tf string, bars []Bar) {
	truthCapture.mu.Lock()
	defer truthCapture.mu.Unlock()
	if !truthCapture.active || len(bars) == 0 {
		return
	}
	key := symbol + "|" + tf
	b := truthCapture.batches[key]
	b.Symbol, b.TF = symbol, tf
	b.Count += len(bars)
	for _, x := range bars {
		b.Hash = fnvAddBar(b.Hash, x)
		if x.T > b.LastT {
			b.LastT = x.T
		}
	}
	truthCapture.batches[key] = b
}

func (s *TCPServer) endBarTruthCapture() map[string]BarTruthBatch {
	truthCapture.mu.Lock()
	defer truthCapture.mu.Unlock()
	truthCapture.active = false
	out := truthCapture.batches
	truthCapture.batches = nil
	return out
}

// EndBarTruthCapture (exported) returns the captured replay batches and closes
// the window. Empty when no capture was active.
func (s *TCPServer) EndBarTruthCapture() map[string]BarTruthBatch {
	return s.endBarTruthCapture()
}

// BarTruthDrops returns the BAR-TRUTH drop counters (ingest oldest/current/
// historical + persist queue) for the E-proof drop-zero check.
func (s *TCPServer) BarTruthDrops() (oldest, current, historical, persist int64) {
	return ingestDropOld.Load(), ingestDropCur.Load(), ingestDropHist.Load(), persistDropped.Load()
}

// FNVBarSet folds a bar set into (count, FNV-1a hash) for the three-way diff.
func FNVBarSet(bars []Bar) (int, uint64) {
	h := uint64(14695981039346656037)
	for _, b := range bars {
		h = fnvAddBar(h, b)
	}
	return len(bars), h
}

// RequestDeepBarsBackfill sends a one-shot bars_subscribe on the LIVE
// connection with an enlarged BarsBack for one symbol/timeframe so the AddOn
// replays deep history (the auto-subscribe state is untouched — the next
// reconnect restores the standard back).
func (s *TCPServer) RequestDeepBarsBackfill(symbol, timeframe string, barsBack int) error {
	if barsBack <= 0 {
		barsBack = 8640 // 6 days of 1m — covers the 08-24+ retention window
	}
	s.beginBarTruthCapture(symbol, timeframe)
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("no live NT8 connection")
	}
	payload := BarsSubscribePayload{Symbol: symbol, Timeframes: []string{timeframe}, BarsBack: barsBack}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second)) // the conn's last deadline may be stale/expired
	err := WriteFrame(c, FrameBarsSubscribe, payload)
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("bars_subscribe write: %w", err)
	}
	s.logger.Info("tcp_server: sent DEEP bars_subscribe (bar-truth arbiter)",
		"symbol", symbol, "timeframe", timeframe, "bars_back", barsBack)
	return nil
}

// freshly-accepted connection — the primary symbol first, then one frame per
// extra root (P5.1). Failure here is non-fatal — the C# side will simply not
// start streaming bars; the next reconnect retries. We log a warn so the
// operator sees the failure.
func (s *TCPServer) sendAutoBarsSubscribe(c net.Conn) {
	for _, payload := range s.barsSubscribePayloads() {
		if payload.Symbol == "" || len(payload.Timeframes) == 0 {
			// No subscription configured — Stage 2 default always populates
			// this, but defend against an explicit empty override.
			continue
		}
		s.writeMu.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := WriteFrame(c, FrameBarsSubscribe, payload)
		s.writeMu.Unlock()
		if err != nil {
			s.logger.Warn("tcp_server: send bars_subscribe", "err", err,
				"symbol", payload.Symbol, "timeframes", payload.Timeframes)
			continue
		}
		s.setSubState(payload.Symbol, "pending", "", "") // P5.3 — awaiting the AddOn's ack
		s.logger.Info("tcp_server: sent bars_subscribe",
			"symbol", payload.Symbol,
			"timeframes", payload.Timeframes,
			"bars_back", payload.BarsBack)
	}
}

// drainBarIngest consumes the bar ingest channel and applies updates to
// the cache. Runs as a background goroutine for the server's lifetime.
// Decoupling the cache writes from the socket read loop guarantees the
// read loop never stalls on cache lock contention — which would block
// heartbeat reads and trigger a spurious reconnect.
func (s *TCPServer) drainBarIngest(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.barIngestCh:
			if msg.historical {
				s.barCache.SeedHistorical(msg.symbol, msg.timeframe, msg.bars)
			} else {
				s.barCache.Upsert(msg.symbol, msg.timeframe, msg.bars)
				// LIVE-only fan-out to deterministic evaluators (two-picture
				// mode). Historical replays must NOT mint opportunities.
				//
				// W4/D24: neither may a frame that reached us long after it was
				// emitted. It is real data and the cache write above keeps it,
				// but a backlogged queue draining all at once must not present
				// old prices as news.
				if liveFrameTooOld(msg.bars, time.Now().UnixMilli()) {
					staleLiveFrames.Add(1)
					if n := staleLiveFrames.Load(); n%100 == 1 {
						s.logger.Warn("picture-htf: bar_update frame refused as a live entry event — too old; cached, not traded",
							"max_age_ms", liveFrameMaxAgeMs, "refused_total", n, "symbol", msg.symbol, "timeframe", msg.timeframe)
					}
				} else {
					fanOutLiveBars(msg.symbol, msg.timeframe, msg.contract, msg.bars)
				}
			}
			// Bar persistence (2026-08-26) — fan-out AFTER the cache write, in
			// its own goroutine: a slow/failing DB must never stall the drain
			// (backpressure invariant) or the socket read loop.
			//
			// Live bar_update frames carry ONLY the forming bar (NT8 does not
			// re-emit the just-closed bar at the boundary), so live candidates
			// come from the cache tail — the cache always holds the final
			// closed bars. Historical replays persist from the frame batch.
			var persistBars []Bar
			if msg.historical {
				persistBars = ClosedBarsOnly(msg.bars, msg.timeframe, time.Now().UnixMilli())
			} else {
				persistBars = ClosedCacheTail(s.barCache.Get, msg.symbol, msg.timeframe, time.Now().UnixMilli(), 8)
			}
			fanOutBarPersist(s.logger.Warn, msg.historical, msg.symbol, msg.timeframe, persistBars)
		}
	}
}

// enqueueBarUpdate posts a bar_update to the ingest channel using a
// drop-oldest, non-blocking send. If the channel is full (slow drain
// goroutine), we discard the OLDEST pending update to make room, log
// the drop once, and retry. If the channel is still full after the
// drop, we discard THIS update too — under sustained pressure the
// freshest in-flight update will still arrive on the next tick.
//
// This path MUST NOT block the socket read loop. Blocking the read
// loop stalls heartbeat receive → 60s ack timeout → server closes the
// conn → spurious reconnect cycle.
// ingestPeakDepth is the high-water mark of the ingest channel (session-
// scoped: reset at the 17:00 CT CME session roll). ingestPeakDay is the
// session-day the peak belongs to.
var (
	ingestPeakDepth atomic.Int64
	ingestPeakDay   atomic.Int64 // yyyymmdd at last reset
)

// sampleIngestDepth updates the peak-depth high-water mark and resets it at
// the CME 17:00 CT session roll (per-session peak, FORENSICS HYGIENE).
func sampleIngestDepth(depth int) {
	now := time.Now()
	day := int64(now.Year()*10000 + int(now.Month())*100 + now.Day())
	if now.Hour() == 17 {
		if ingestPeakDay.CompareAndSwap(ingestPeakDay.Load(), day) && ingestPeakDay.Load() == day {
			ingestPeakDepth.Store(0)
		}
	}
	for {
		cur := ingestPeakDepth.Load()
		if int64(depth) <= cur || ingestPeakDepth.CompareAndSwap(cur, int64(depth)) {
			return
		}
	}
}

func (s *TCPServer) enqueueBarUpdate(symbol, timeframe, contract string, bars []Bar) {
	// Stamp the live-feed freshness signal (IsFeedConnected uses it to override a
	// stale edge-triggered feed_status). Cheap atomic store on the hot path.
	s.lastBarNano.Store(time.Now().UnixNano())
	msg := barIngestMsg{historical: false, symbol: symbol, timeframe: timeframe, contract: contract, bars: bars}
	select {
	case s.barIngestCh <- msg:
		sampleIngestDepth(len(s.barIngestCh))
		// S-LIST CLOSER: the 1-line/min summary also fires on the CLEAN path
		// (rate-limited inside), so peak_depth is observable after a reopen
		// even when zero drops occurred — the E-proof line for FIX3.
		ingestDropSummary()
		return
	default:
	}
	// Channel full — drop oldest (counted, summarized 1-line/min), retry.
	sampleIngestDepth(len(s.barIngestCh))
	select {
	case <-s.barIngestCh:
		ingestDropOld.Add(1)
		ingestDropSummary()
	default:
	}
	select {
	case s.barIngestCh <- msg:
	default:
		ingestDropCur.Add(1)
		ingestDropSummary()
	}
}

// enqueueBarHistorical posts a bars_historical message to the ingest
// channel. Historical batches are one-shot per (symbol, timeframe) and
// carry the full warmup window the kernel needs for indicator
// computation — they MUST NOT be dropped. We use a bounded blocking
// send guarded by a short timeout: if the drain goroutine is stuck
// longer than 2s, log + drop (better to lose this batch than deadlock
// the read loop forever).
func (s *TCPServer) enqueueBarHistorical(symbol, timeframe string, bars []Bar) {
	s.histAtSub.note(symbol, timeframe, len(bars)) // 101 E1: what arrived, per tf
	msg := barIngestMsg{historical: true, symbol: symbol, timeframe: timeframe, bars: bars}
	select {
	case s.barIngestCh <- msg:
		return
	case <-time.After(2 * time.Second):
		ingestDropHist.Add(1)
		ingestDropSummary()
	}
}

// helloProcessPair renders the hello identity for the log: n/a when absent,
// never 0/"" — an unread value must not read as a datum (L7, runbook
// 2026-09-23-addon-m21-f5.md C5).
func helloProcessPair(pid int, mvid string) (string, string) {
	p, m := "n/a", "n/a"
	if pid != 0 {
		p = strconv.Itoa(pid)
	}
	if mvid != "" {
		m = mvid
	}
	return p, m
}

func (s *TCPServer) readLoop(ctx context.Context, c net.Conn) {
	researchWire := newResearchWire()
	defer s.wg.Done()
	defer s.closeConn()

	// Plan 1.5.7 — per-connection ctx-cancel watcher unblocks the blocked
	// ReadFrame on server shutdown by closing this specific conn. Replaces
	// the previous SetReadDeadline(2s) + continue-on-timeout pattern that
	// caused frame desync: when a frame arrived near the 2s deadline,
	// io.ReadFull would consume partial bytes (the consumed bytes are gone
	// from the socket) then return a timeout; the `continue` then re-entered
	// ReadFrame which read 4 fresh bytes starting at the WRONG offset (mid-
	// header or mid-body), decoded as a garbage big-endian length usually
	// > 1 MB, and triggered ErrFrameTooLarge → spurious "oversized frame,
	// closing connection" warnings (~49 over 16h on quiet traffic).
	//
	// connCtx is derived from the server ctx so it cancels on shutdown.
	// defer connCancel() runs on normal disconnect, releasing the watcher
	// goroutine without a leak. c.Close() is idempotent.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go func() {
		<-connCtx.Done()
		_ = c.Close()
	}()

	// P5.2 handshake state (per connection). helloSeen flips on a valid hello;
	// a data frame arriving without one marks a LEGACY (pre-P5.2) AddOn —
	// tolerated with a single warning so the lockstep deploy window (new Go,
	// not-yet-F5'd C#) cannot brick the bar feed. An explicit version MISMATCH
	// is refused in the FrameHello case below.
	helloSeen := false
	legacyWarned := false

	for {
		// No SetReadDeadline — ReadFrame blocks until a frame arrives OR
		// the connection is closed (by the watcher above on shutdown, or
		// by the peer). The watcher provides the shutdown-responsiveness
		// the old 2s polling deadline used to.
		env, err := ReadFrame(c)
		if err == nil {
			researchWire.observe(env.Type, env.Payload)
			s.framesTotal.Add(1)
			s.lastFrameUnixMs.Store(time.Now().UnixMilli())
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				s.logger.Info("tcp_server: client disconnected")
				return
			}
			if errors.Is(err, ErrFrameTooLarge) {
				s.logger.Warn("tcp_server: oversized frame, closing connection")
				return
			}
			s.logger.Warn("tcp_server: read frame error", "err", err)
			return
		}

		// T8 (2026-08-27) — per-frame INFO spam (7.5M bar_update lines/day)
		// consumed the journald 2G cap in <24h, destroying forensics. bar_update
		// now logs at DEBUG, sampled 1-in-N (BAR_UPDATE_LOG_SAMPLE, default 500
		// ≈ one line per ~2 min at 4 bars/s); every other frame type keeps its
		// unconditional INFO.
		if env.Type == FrameBarUpdate {
			n := s.frameLogN.Add(1)
			if n%barUpdateLogSample() == 0 {
				s.logger.Debug("tcp_server: received frame (sampled)", "type", env.Type, "sample_every", barUpdateLogSample())
			}
		} else {
			s.logger.Info("tcp_server: received frame", "type", env.Type)
		}

		// P5.2 — legacy detection: a data frame before any hello means a
		// pre-P5.2 AddOn. Tolerated (warn once per connection) so the lockstep
		// deploy window can't kill the feed; an explicit version mismatch is
		// still refused in the FrameHello case.
		if !helloSeen && !legacyWarned && env.Type != FrameHello && env.Type != FrameHeartbeat && env.Type != FrameAck {
			legacyWarned = true
			s.logger.Warn("tcp_server: data frame before hello — LEGACY (pre-P5.2) AddOn assumed",
				"first_frame", env.Type,
				"action", "redeploy the C# AddOn (cp → F5 → NT8 restart) to enable the protocol handshake")
		}

		switch env.Type {
		case FrameHello:
			// P5.2 handshake. Version MISMATCH → refuse the connection loudly
			// (close; never silently misparse a different protocol generation).
			// Matching hello → reply with ours so the AddOn can verify us too.
			var p HelloPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad hello payload", "err", err)
				continue
			}
			if p.ProtocolVersion != ProtocolVersion {
				s.logger.Error("tcp_server: PROTOCOL VERSION MISMATCH — refusing connection",
					"peer_version", p.ProtocolVersion, "our_version", ProtocolVersion,
					"source", p.Source,
					"action", "redeploy the C# AddOn + Go binary in lockstep (cp → F5 → NT8 restart)")
				return // closes the connection via the deferred cleanup
			}
			helloSeen = true
			// F12 — the handshake carries build_id, so the running DLL is
			// identifiable from the FIRST frame instead of only after a
			// heartbeat or a snapshot.
			if p.BuildID != "" {
				if prev, _ := s.farSideBuild.Load().(string); prev != p.BuildID {
					s.farSideBuild.Store(p.BuildID)
				}
			}
			s.recordHello(c, p, time.Now()) // W-ONE-BUTTON M2 (Q3): the epoch, per connection
			pid, mvid := helloProcessPair(p.NT8PID, p.AssemblyMVID)
			s.logger.Info("tcp_server: hello handshake OK",
				"protocol_version", p.ProtocolVersion, "source", p.Source, "build_id", p.BuildID,
				"nt8_pid", pid, "assembly_mvid", mvid)
			s.writeMu.Lock()
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := WriteFrame(c, FrameHello, HelloPayload{ProtocolVersion: ProtocolVersion, Source: "nofx-go"})
			s.writeMu.Unlock()
			if err != nil {
				s.logger.Warn("tcp_server: write hello reply", "err", err)
				return
			}

		case FrameMaintenanceAck:
			// W-ONE-BUTTON M2 site 7 — recorded on THIS connection's record only.
			var p MaintenanceAckPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad maintenance_ack payload", "err", err)
				continue
			}
			if s.recordMaintenanceAck(c, p, time.Now()) {
				s.logger.Info("tcp_server: 🔒 maintenance_ack",
					"held", p.Held, "job_id", p.JobID, "queued_commands", p.QueuedCommands,
					"connections", len(p.Connections), "accounts", len(p.Accounts),
					"census_error", p.CensusError, "build_id", p.BuildID)
			}

		case FrameFill:
			var fill FillPayload
			if err := json.Unmarshal(env.Payload, &fill); err != nil {
				s.logger.Warn("tcp_server: bad fill payload", "err", err)
				continue
			} // A2 (G1) — verify the echoed identity against the pending op. A mismatch
			// (forged/cross-wired fill) is NOT processed; the owning trader is frozen.
			// W3 — pass the frame status: a rejected fill may echo account "" (the C#
			// guard-reject path); the account leg alone is then tolerated.
			if !s.verifyInbound("fill", fill.Seq, fill.TraderID, fill.Account, fill.SignalID, fill.Status) {
				continue
			}
			// Retire ONLY a rejected fill (terminal — no position opened). A filled
			// entry keeps its pending op alive so the later position_close (keyed by
			// the SAME entry signal_id) still verifies against it before retiring.
			if fill.Status == "rejected" {
				s.retirePending(fill.Seq, fill.SignalID)
			}
			select {
			case s.fillCh <- fill:
			default:
				s.logger.Warn("tcp_server: fill channel full, dropping", "signal_id", fill.SignalID)
			}

		case FrameOrderUpdate:
			// PHASE 2 armed orders — informational state-change frame; routes
			// to per-(symbol,account) subscribers like fills. No strict echo
			// verify (advisory event; the terminal fill/close frames keep it).
			var oup OrderUpdatePayload
			if err := json.Unmarshal(env.Payload, &oup); err != nil {
				s.logger.Warn("tcp_server: bad order_update payload", "err", err)
				continue
			}
			select {
			case s.orderUpdCh <- oup:
			default:
				s.logger.Warn("tcp_server: order_update channel full, dropping", "signal_id", oup.SignalID)
			}

		case FrameOrderSnapshot:
			// F12 — the BROKER's book. A malformed frame is logged and dropped
			// (A10): the previous snapshot stays, and leg 4 will age it out
			// rather than trusting a half-parsed one.
			p, perr := ParseOrderSnapshot(env.Payload)
			if perr != nil {
				s.logger.Warn("tcp_server: bad order_snapshot payload", "err", perr)
				continue
			}
			if s.orderSnaps != nil {
				s.orderSnaps.PutAt(p, time.Now())
			}
			// The snapshot's build_id feeds the SAME far-side field the E7
			// heartbeat handshake owns — one received value, one source.
			if p.BuildID != "" {
				if prev, _ := s.farSideBuild.Load().(string); prev != p.BuildID {
					s.farSideBuild.Store(p.BuildID)
					s.logger.Info("tcp_server: far-side AddOn build_id=" + p.BuildID + " (from order_snapshot)")
				}
			}
			if s.orderSnapCB != nil {
				s.orderSnapCB(p)
			}

		case FrameAck:
			s.connMu.Lock()
			s.lastAckTime = time.Now()
			s.connMu.Unlock()
			// A2 (G1) — an ORDER ack (seq>0) echoes the identity; verify it (a
			// heartbeat ack has seq==0 → tolerated). Non-terminal → do not retire.
			var ack AckPayload
			if err := json.Unmarshal(env.Payload, &ack); err == nil && ack.Seq != 0 {
				_ = s.verifyInbound("ack", ack.Seq, ack.TraderID, ack.Account, ack.SignalID, "")
			}

		case FrameHeartbeat:
			// E7 capability handshake — the AddOn's build id (see farSideBuild).
			var hb HeartbeatPayload
			if err := json.Unmarshal(env.Payload, &hb); err == nil && hb.BuildID != "" {
				if prev, _ := s.farSideBuild.Load().(string); prev != hb.BuildID {
					s.farSideBuild.Store(hb.BuildID)
					s.logger.Info("tcp_server: far-side AddOn build_id=" + hb.BuildID)
				}
			}
			// Respond to peer heartbeat with an ack (spec L4410). Set our
			// own write deadline — Go's net.Conn deadlines are PERSISTENT
			// until reset, so without this the ack inherits whatever
			// deadline heartbeatLoop most recently set. Once that stale
			// deadline expires (5s after the last scheduled server
			// heartbeat), every subsequent write fails with i/o timeout
			// and the connection dies. (Plan 1.5.6, fixes NEW-1.)
			s.writeMu.Lock()
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := WriteFrame(c, FrameAck, AckPayload{Acks: "heartbeat"})
			s.writeMu.Unlock()
			if err != nil {
				s.logger.Warn("tcp_server: write heartbeat ack", "err", err)
				return
			}

		case FrameSignal:
			// Clients don't send signals; ignore but log.
			s.logger.Warn("tcp_server: unexpected signal frame from client")

		case FrameSubscribed:
			// P5.3 — the AddOn confirmed a bars_subscribe with the resolved
			// front-month contract.
			var p SubscribedPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad subscribed payload", "err", err)
				continue
			}
			s.logger.Info("tcp_server: subscription ACK", "symbol", p.Symbol, "contract", p.ResolvedContract)
			// ROLL WAVE — this ACK is the ONE frame that names the contract.
			// observeContract records the subscription state AND detects a
			// roll (a different name than last time → purge, record, notify).
			s.observeContract(p.Symbol, p.ResolvedContract, time.Now())

		case FrameUnsubscribed:
			var p UnsubscribedPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad unsubscribed payload", "err", err)
				continue
			}
			s.setSubState(p.Symbol, "unsubscribed", "", "")
			s.logger.Info("tcp_server: unsubscribe ACK", "symbol", p.Symbol, "removed", p.Removed)

		case FrameSubscribeError:
			// P5.3 — a FAILED subscribe (e.g. instrument not in NT8's DB)
			// surfaces Go-side instead of dying silently in the NT8 Output.
			var p SubscribeErrorPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad subscribe_error payload", "err", err)
				continue
			}
			s.setSubState(p.Symbol, "error", "", p.Reason)
			s.logger.Warn("tcp_server: SUBSCRIBE FAILED", "symbol", p.Symbol, "reason", p.Reason)

		case FrameBarsHistorical:
			// Plan 4.4 Stage 2 — one-shot per (symbol, timeframe) after
			// the initial BarsRequest load. Seeds the cache.
			var p BarsHistoricalPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad bars_historical payload", "err", err)
				continue
			}
			// P5.2 split-brain defense: a bar for a symbol we never subscribed
			// is REJECTED loudly — write-to-own-cache only.
			if !s.isSubscribedBarsSymbol(p.Symbol) {
				s.logger.Warn("tcp_server: REJECTED bars_historical for unsubscribed symbol (split-brain defense)",
					"symbol", p.Symbol, "timeframe", p.Timeframe, "bars", len(p.Bars))
				continue
			}
			s.enqueueBarHistorical(p.Symbol, p.Timeframe, p.Bars)

		case FrameBarsHistoryData:
			// HISTORY IMPORT (wave 101) — fan one chunk out to the importer
			// that owns the request id. No live-bar path reads these frames.
			var p BarsHistoryDataPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad bars_history_data payload", "err", err)
				continue
			}
			s.histSubMu.RLock()
			ch, ok := s.historyDataSubs[strings.TrimSpace(p.RequestID)]
			s.histSubMu.RUnlock()
			if !ok {
				s.logger.Warn("tcp_server: bars_history_data for unknown request id — dropped",
					"request_id", p.RequestID, "contract", p.Contract, "bars", len(p.Bars))
				continue
			}
			select {
			case ch <- p:
			default:
				s.logger.Warn("tcp_server: bars_history_data channel full — chunk dropped (importer too slow)",
					"request_id", p.RequestID, "contract", p.Contract)
			}

		case FrameBarsHistoryError:
			var p BarsHistoryErrorPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad bars_history_error payload", "err", err)
				continue
			}
			s.histSubMu.RLock()
			ch, ok := s.historyErrSubs[strings.TrimSpace(p.RequestID)]
			s.histSubMu.RUnlock()
			if !ok {
				s.logger.Warn("tcp_server: bars_history_error for unknown request id — dropped",
					"request_id", p.RequestID, "contract", p.Contract, "reason", p.Reason)
				continue
			}
			select {
			case ch <- p:
			default:
			}

		case FrameBarUpdate:
			// Plan 4.4 Stage 2 — streaming updates. The bars array may
			// carry multiple bar indices per the NT8 multi-bar gotcha
			// (a single tick can update MinIndex..MaxIndex); the cache's
			// Upsert handles each in order via dedup-by-t.
			var p BarUpdatePayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad bar_update payload", "err", err)
				continue
			}
			// P5.2 split-brain defense (mirrors bars_historical above). Note:
			// bar_update for a just-unsubscribed symbol can race in-flight —
			// rejected here, which is exactly the desired teardown semantics.
			if !s.isSubscribedBarsSymbol(p.Symbol) {
				s.logger.Warn("tcp_server: REJECTED bar_update for unsubscribed symbol (split-brain defense)",
					"symbol", p.Symbol, "timeframe", p.Timeframe, "bars", len(p.Bars))
				continue
			}
			s.enqueueBarUpdate(p.Symbol, p.Timeframe, p.Contract, p.Bars)

		case FrameAccountBalance:
			// Plan 4.11 — real NT account snapshot. Store the latest;
			// TCPTrader.GetBalance serves it instead of the $50k mock.
			var p AccountBalancePayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad account_balance payload", "err", err)
				continue
			}
			s.acctMu.Lock()
			if s.acctBalances == nil {
				s.acctBalances = make(map[string]AccountBalancePayload)
			}
			s.acctBalances[p.Account] = p
			s.acctMu.Unlock()
			// Update current account if it changed
			s.accountsListMu.Lock()
			s.currentAccount = p.Account
			s.accountsListMu.Unlock()

		case FrameAccountsList:
			// Plan 4 Stage 4 — discover available accounts from the C# AddOn.
			// The AddOn emits this unsolicited on connect and on account list change.
			// Stores the list for dashboard UI and server-side account validation.
			var p AccountsListPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad accounts_list payload", "err", err)
				continue
			}
			s.logger.Info("tcp_server: received accounts_list frame", "count", len(p.Accounts))
			if len(p.Accounts) > 0 {
				// Extract the current account from the first account_balance frame
				// (the AddOn emits accounts_list then account_balance immediately after).
				// For now, store the list; the current account is synced via account_balance.
				s.accountsListMu.Lock()
				s.accountsList = make([]AccountInfo, len(p.Accounts))
				copy(s.accountsList, p.Accounts)
				s.accountsListMu.Unlock()
				s.logger.Info("tcp_server: stored accounts",
					"count", len(p.Accounts),
					"accounts", func() []string {
						var names []string
						for _, a := range p.Accounts {
							names = append(names, a.Name)
						}
						return names
					}())
			} else {
				s.logger.Warn("tcp_server: accounts_list frame received but empty")
			}

		case FrameAccountSelect:
			// Plan 4 Stage 4 — account selection received from the client
			// (via the API handler). Validate that the account is in the list
			// and is a SIM account. Reject live accounts.
			var p AccountSelectPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad account_select payload", "err", err)
				continue
			}
			s.handleAccountSelect(p)

		case FramePositionClose:
			// Position-history fix — an OCO exit leg filled and the position
			// went flat. Hand to the close-sync to record the close.
			var p PositionClosePayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad position_close payload", "err", err)
				continue
			}
			// A2 (G1) — verify the echoed identity; a mismatch freezes the trader and
			// the close is NOT recorded (never record a close for the wrong originator).
			if !s.verifyInbound("position_close", p.Seq, p.TraderID, p.Account, p.SignalID, "") {
				continue
			}
			s.retirePending(p.Seq, p.SignalID)
			select {
			case s.closeCh <- p:
			default:
				s.logger.Warn("tcp_server: close channel full, dropping", "signal_id", p.SignalID)
			}

		case FramePositions:
			// Open-position read-back — the C# AddOn's CURRENT open-position
			// snapshot for an account (emitted on account_select, connect, and
			// any PositionUpdate, incl. manual trades). REPLACE the per-account
			// cache: an empty list means the account is flat. This is what lets
			// GetPositions reflect NT8 truth after a switch-back / manual open.
			var p PositionsPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad positions payload", "err", err)
				continue
			}
			s.acctMu.Lock()
			if s.acctPositions == nil {
				s.acctPositions = make(map[string][]OpenPosition)
			}
			s.acctPositions[p.Account] = p.Positions
			s.acctMu.Unlock()
			s.logger.Info("tcp_server: positions snapshot", "account", p.Account, "count", len(p.Positions))

		case FramePositionCloseRejected:
			// An exit/flatten was REJECTED (e.g. SIM "no market data" while the
			// feed is down). The position is STILL OPEN in NT8 — hand to the
			// consumer to alarm; never record a close off a rejected flatten.
			var p PositionCloseRejectedPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad position_close_rejected payload", "err", err)
				continue
			}
			// A2 (G1) — verify the echoed identity before alarming on the reject. Do
			// NOT retire: a rejected close means the position is STILL OPEN, so the
			// entry op stays pending for the eventual real position_close.
			if !s.verifyInbound("position_close_rejected", p.Seq, p.TraderID, p.Account, p.SignalID, "") {
				continue
			}
			select {
			case s.rejectCh <- p:
			default:
				s.logger.Warn("tcp_server: reject channel full, dropping", "signal_id", p.SignalID)
			}

		case FrameFeedStatus:
			// NT8 price-feed status. Store the latest; the AutoTrader gates
			// opens/closes when it is not "Connected" (the SIM rejects orders
			// with "no market data" while the feed is down).
			var p FeedStatusPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad feed_status payload", "err", err)
				continue
			}
			s.feedMu.Lock()
			s.feedStatus = p.PriceStatus
			s.feedMu.Unlock()
			s.logger.Info("tcp_server: feed status", "price_status", p.PriceStatus)

		case FrameInstrumentInfo:
			// Resolved NT8 instrument specs (point value, tick) — ground truth to
			// cross-check the hardcoded tables. Hand to the consumer; non-blocking.
			var p InstrumentInfoPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				s.logger.Warn("tcp_server: bad instrument_info payload", "err", err)
				continue
			}
			select {
			case s.instrCh <- p:
			default:
				s.logger.Warn("tcp_server: instrument channel full, dropping", "symbol", p.Symbol)
			}

		default:
			s.logger.Warn("tcp_server: unknown frame type", "type", env.Type)
		}
	}
}

func (s *TCPServer) heartbeatLoop(ctx context.Context, c net.Conn) {
	defer s.wg.Done()
	ticker := time.NewTicker(TCPHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.connMu.Lock()
			if s.conn != c {
				s.connMu.Unlock()
				return
			}
			since := time.Since(s.lastAckTime)
			s.connMu.Unlock()
			if since > TCPHeartbeatAckTimeout {
				s.logger.Warn("tcp_server: heartbeat ack timeout, closing", "since", since)
				s.closeConn()
				return
			}
			s.writeMu.Lock()
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := WriteFrame(c, FrameHeartbeat, HeartbeatPayload{})
			s.writeMu.Unlock()
			if err != nil {
				s.logger.Warn("tcp_server: write heartbeat", "err", err)
				s.closeConn()
				return
			}
		}
	}
}

// handleAccountSelect validates an account_select request from the AddOn.
// Only SIM accounts are permitted. Rejects live accounts and logs the result.
// Updates CurrentAccount if the select succeeds.
func (s *TCPServer) handleAccountSelect(p AccountSelectPayload) {
	s.accountsListMu.RLock()
	var isSim bool
	found := false
	for _, a := range s.accountsList {
		if a.Name == p.Account {
			isSim = a.IsSim
			found = true
			break
		}
	}
	s.accountsListMu.RUnlock()

	if !found {
		s.logger.Warn("tcp_server: account_select for unknown account", "account", p.Account)
		return
	}

	if !isSim {
		s.logger.Warn("tcp_server: rejected account_select for live account", "account", p.Account)
		return
	}

	// Update current account
	s.accountsListMu.Lock()
	s.currentAccount = p.Account
	s.accountsListMu.Unlock()
	s.logger.Info("tcp_server: account selected", "account", p.Account)
}

// checkSignalAge enforces h1's 60s payload-age ceiling on our side. Row and
// queue timestamps cannot prove freshness. Missing/malformed clocks are refused.
func (s *TCPServer) checkSignalAge(sig SignalPayload, now time.Time) error {
	created, err := time.Parse(time.RFC3339Nano, sig.Timestamp)
	if err != nil {
		s.logger.Warn("tcp_server: refusing signal: payload age unavailable", "signal_id", sig.SignalID, "timestamp", sig.Timestamp)
		return fmt.Errorf("signal %s refused: payload age unavailable (invalid timestamp)", sig.SignalID)
	}
	age := now.Sub(created)
	if age > s.staleAge() || age < 0 {
		s.logger.Warn("tcp_server: refusing stale/future signal", "signal_id", sig.SignalID, "payload_age_ms", age.Milliseconds(), "threshold_ms", s.staleAge().Milliseconds())
		return fmt.Errorf("signal %s refused: payload age %s outside [0,%s]", sig.SignalID, age, s.staleAge())
	}
	return nil
}

// flushPending writes any non-stale queued signals to the connected client.
// No-op if disconnected. Stale entries (>TCPStaleSignalAge) are dropped, and
// so is every queued entry while the installation is under maintenance.
func (s *TCPServer) flushPending() error {
	_, err := s.flushPendingReport()
	return err
}

// flushPendingReport is flushPending plus what it DROPPED because of the
// maintenance hold, as signal id → attempted (so SendSignal can tell its own
// entry "never sent" from "may have reached NT8" — ownDropError).
func (s *TCPServer) flushPendingReport() (map[string]bool, error) { return s.flushPendingReportFor("") }

// flushPendingReportFor is flushPendingReport inside the SendSignal call of
// signal own: that entry's drop is reported Own (its caller learns of it from
// the returned error, not from the sink — M2.1, review 2 N2).
func (s *TCPServer) flushPendingReportFor(own string) (map[string]bool, error) {
	// Held: the whole queue is dropped and reported, CONNECTED OR NOT (the
	// queue holds entries only; nothing in it may reach the wire while held).
	if s.entryHeld() {
		return s.dropQueuedWhileHeldFor(own), nil
	}
	var heldDropped map[string]bool
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return nil, nil
	}

	s.pendingMu.Lock()
	toSend := append([]timedSignal(nil), s.pending...)
	s.pending = s.pending[:0]
	s.pendingMu.Unlock()

	for i, queued := range toSend {
		sig := queued.payload
		if s.entryHeld() {
			// W-ONE-BUTTON M2 (gap U2): the hold landed mid-flush — the rest of
			// this batch is dropped and reported, never re-queued.
			heldDropped = s.reportDrops(toSend[i:], own)
			return heldDropped, nil
		}
		s.writeMu.Lock()
		// W-ONE-BUTTON M2.1 (review F1): re-check the hold WITH the writer held.
		// Waiting for writeMu (the hello reply, a heartbeat) must not let a
		// hold that landed meanwhile be written past.
		if s.entryHeld() {
			s.writeMu.Unlock()
			heldDropped = s.reportDrops(toSend[i:], own)
			return heldDropped, nil
		}
		// Check after waiting for the writer, using the command's original
		// timestamp. Neither enqueue nor retry is allowed to renew its lease.
		if err := s.checkSignalAge(sig, time.Now()); err != nil {
			s.writeMu.Unlock()
			continue
		}
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := WriteFrame(c, FrameSignal, sig)
		s.writeMu.Unlock()
		if err != nil {
			// The write was STARTED: bytes may have reached the AddOn.
			toSend[i].attempted = true
			// W2 (SUNDAY-SHIELD) — re-queue this signal AND the remaining
			// unsent tail. The old code re-queued only the current one and
			// DROPPED every later queued signal on a mid-flush conn death —
			// a reconnect right after could lose armed entries silently.
			s.pendingMu.Lock()
			s.pending = append(s.pending, toSend[i:]...)
			s.pendingMu.Unlock()
			s.logger.Warn("tcp_server: flush signal failed", "err", err, "signal_id", sig.SignalID, "requeued", len(toSend)-i)
			s.closeConn()
			return heldDropped, err
		}
	}
	return heldDropped, nil
}

// ErrEntryHeld is returned by SendSignal when its OWN entry was dropped from
// the queue because the installation went under maintenance between the
// caller's permit and the flush (W-ONE-BUTTON M2, site-map gap U2). The
// caller must record the entry as NOT placed.
var ErrEntryHeld = errors.New("entry dropped: the installation is under maintenance (hold landed before the flush)")

// SetEntryHoldCheck installs the maintenance predicate the queue consults
// before writing each queued entry. nil = never held (today's behaviour).
func (s *TCPServer) SetEntryHoldCheck(fn func() bool) {
	if fn == nil {
		s.entryHoldCheck.Store(func() bool { return false })
		return
	}
	s.entryHoldCheck.Store(fn)
}

func (s *TCPServer) entryHeld() bool {
	if fn, ok := s.entryHoldCheck.Load().(func() bool); ok && fn != nil {
		return fn()
	}
	return false
}

// PendingSignalCount is the number of entry signals queued for the next
// flush — an installation-gate input: a queued entry is an entry in flight.
func (s *TCPServer) PendingSignalCount() int {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return len(s.pending)
}

func (s *TCPServer) closeConn() {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}
