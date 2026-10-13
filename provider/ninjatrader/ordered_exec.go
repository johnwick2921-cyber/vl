package ninjatrader

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// W117 slice A — F2 rebuilt: TCP RECEIVE-ORDER EXECUTION.
//
// The earlier #117 / PR-#218 design ran the owning durable consumers ON the
// TCP read goroutine (before the advisory fanout). That failed review: a slow
// durable consumer (SQLite) stalls frame reception, and a callback under the
// execution lock can wedge registration. This design instead gives each
// (symbol, account) owner its OWN worker goroutine:
//
//   - The read loop only ENQUEUES. The queue is a BOUNDED mutex-guarded slice
//     (orderedQueueCap) + a coalesced wake channel (cap 1, so a burst of frames
//     still wakes the worker once). A frame is NEVER dropped: a full queue
//     blocks the enqueue (P2-14) with a per-pass timeout that logs ERROR and
//     bumps a counter, then keeps waiting for room — a stuck durable consumer
//     stalls the read loop LOUDLY instead of growing memory unbounded; the
//     server-side heartbeat-receive stall bounds that state at 60s and forces a
//     reconnect (the AddOn re-syncs its book on reconnect). No SQLite work ever
//     runs on the read goroutine.
//   - The worker drains FIFO and calls each handler OUTSIDE any registry lock
//     (the owner struct is immutable once registered; re-registration replaces
//     the map entry, the old worker drains and exits).
//   - Enqueue stamps each order_update with the snapshot watermark (snapSeq);
//     the worker waits for a post-update order_snapshot before applying, so
//     recordAcceptedRisk never reads the PRE-change broker book (the AddOn
//     sends order_update THEN order_snapshot — VLTraderTCPClient.cs ~1948
//     then ~1955). Bounded wait: on timeout the frame is applied with
//     BookGate=bookNotFresh (the book is suppressed, never read stale).
//   - Frames owned by a registered owner carry OrderedOwned=true on the
//     advisory channel copies, so the legacy consumers skip them (no
//     double-application). No owner registered → flags false → today's
//     behavior byte-identical.

var ErrOrderedOwnerExists = errors.New("an ordered-execution owner for this (symbol, account) already exists")

// OrderedExecutionHandlers are the owning trader's durable consumers. Each is
// called on the owner's worker goroutine, in receive order, exactly once per
// frame. A nil handler means that frame kind is not consumed by this owner.
//
// TraderID identifies the owning trader so a refused duplicate registration
// can name the trader that already holds the (symbol, account) owner slot
// (REL18-C O2: the P0 alert names BOTH traders). Empty is allowed — legacy
// test registrations carry no trader.
type OrderedExecutionHandlers struct {
	TraderID string
	Order    func(OrderUpdatePayload)
	Fill     func(FillPayload)
	Close    func(PositionClosePayload)
}

type orderedKind uint8

const (
	orderedOrder orderedKind = iota + 1
	orderedFill
	orderedClose
)

// orderedItem is one queued frame. snapAt is the order_snapshot watermark
// captured at enqueue (order items only; 0 otherwise).
type orderedItem struct {
	kind   orderedKind
	order  OrderUpdatePayload
	fill   FillPayload
	close_ PositionClosePayload
	snapAt int64
}

// orderedOwner is one per (symbol, account). Its worker owns it exclusively;
// the enqueuer only touches queueMu. The handlers are immutable after
// registration — replaced owners get a NEW struct, the old one drains out.
type orderedOwner struct {
	queueMu  sync.Mutex
	queue    []orderedItem
	wake     chan struct{} // cap 1, coalesced — new work
	space    chan struct{} // cap 1, coalesced — room dequeued
	handlers OrderedExecutionHandlers
	traderID string // owning trader ("" = unknown) — named on a refused duplicate
	closed   bool

	// overflowCount counts frames that found the queue full (P2-14); the
	// ERROR log is rate-limited but every overflowed frame is counted.
	overflowCount  atomic.Int64
	lastOverflowAt atomic.Int64 // unix-nano of the last full-queue ERROR log
}

func newOrderedOwner(h OrderedExecutionHandlers) *orderedOwner {
	return &orderedOwner{handlers: h, wake: make(chan struct{}, 1), space: make(chan struct{}, 1)}
}

// RegisterOrderedExecutionsFor installs (or refuses to replace) the durable
// execution consumers for exactly one (symbol, account) owner. The returned
// closure unregisters them; it is idempotent and MUST be called on trader
// Stop. A second LIVE registration for the same (symbol, account) is refused
// loudly (ErrOrderedOwnerExists) — never silently evicted: two traders on one
// (symbol, account) would double-apply the same fills.
func (s *TCPServer) RegisterOrderedExecutionsFor(symbol, account string, h OrderedExecutionHandlers) (func(), error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("ordered execution owner requires a non-empty symbol")
	}
	key := subKey(symbol, account)
	owner := newOrderedOwner(h)
	owner.traderID = h.TraderID
	s.orderedMu.Lock()
	if s.orderedOwners == nil {
		s.orderedOwners = make(map[string]*orderedOwner)
	}
	if existing := s.orderedOwners[key]; existing != nil && !existing.closed {
		s.orderedMu.Unlock()
		return nil, ErrOrderedOwnerExists
	}
	s.orderedOwners[key] = owner
	s.orderedMu.Unlock()
	go s.runOrderedOwner(key, owner)
	var once sync.Once
	return func() {
		once.Do(func() {
			owner.queueMu.Lock()
			owner.closed = true
			owner.queueMu.Unlock()
			s.orderedMu.Lock()
			if s.orderedOwners[key] == owner {
				delete(s.orderedOwners, key)
			}
			s.orderedMu.Unlock()
			// Wake the worker so it observes closed and drains out, and any
			// overflow-blocked enqueuer so it observes closed and falls back
			// to the advisory fanout (returns false) instead of waiting on a
			// worker that has exited.
			select {
			case owner.wake <- struct{}{}:
			default:
			}
			select {
			case owner.space <- struct{}{}:
			default:
			}
		})
	}, nil
}

// OrderedOwnerTraderID returns the trader id that CURRENTLY owns the ordered
// execution worker for (symbol, account), or ("", false) when the slot is
// free/unknown. Used by REL18-C O2 to name the conflicting trader in the P0.
func (s *TCPServer) OrderedOwnerTraderID(symbol, account string) (string, bool) {
	s.orderedMu.Lock()
	defer s.orderedMu.Unlock()
	if s.orderedOwners == nil {
		return "", false
	}
	o := s.orderedOwners[subKey(symbol, account)]
	if o == nil || o.closed {
		return "", false
	}
	return o.traderID, o.traderID != ""
}

// enqueueOrdered appends one frame to the owner's queue and returns whether an
// owner consumed it. Callers must be the read loop only.
//
// P2-14: the queue is bounded. A full queue NEVER drops the frame — the
// enqueue blocks (bounded passes of orderedEnqueueWait, each timing out into an
// ERROR + a counter) until the worker drains room or the owner closes. A frame
// arriving while the owner is closed/absent returns false and flows to the
// advisory fanout exactly as before this bound existed.
func (s *TCPServer) enqueueOrdered(key string, it orderedItem) bool {
	s.orderedMu.Lock()
	owner := s.orderedOwners[key]
	s.orderedMu.Unlock()
	if owner == nil {
		return false
	}
	for first := true; ; first = false {
		owner.queueMu.Lock()
		if owner.closed {
			owner.queueMu.Unlock()
			return false
		}
		if len(owner.queue) < orderedQueueCap {
			owner.queue = append(owner.queue, it)
			owner.queueMu.Unlock()
			select {
			case owner.wake <- struct{}{}:
			default:
			}
			return true
		}
		qlen := len(owner.queue)
		owner.queueMu.Unlock()
		if first {
			owner.overflowCount.Add(1)
		}
		if now := time.Now(); now.UnixNano()-owner.lastOverflowAt.Load() >= orderedOverflowLogInterval.Nanoseconds() {
			owner.lastOverflowAt.Store(now.UnixNano())
			s.logger.Error("tcp_server: ordered-exec queue FULL — read loop blocking; frame is NOT dropped",
				"key", key, "cap", orderedQueueCap, "len", qlen,
				"overflow_total", owner.overflowCount.Load())
		}
		select {
		case <-owner.space:
		case <-time.After(orderedEnqueueWait):
			// timed out loud; loop re-checks (and re-counts only a NEW frame)
		}
	}
}

// orderedSnapWait bounds how long a worker waits for the post-update
// order_snapshot watermark before applying with the book suppressed.
const orderedSnapWait = 2 * time.Second

// orderedQueueCap bounds one owner's FIFO (P2-14). The worker drains at
// handler speed (SQLite-bound), so 256 frames is minutes of headroom for any
// real burst; the bound exists so a stalled worker costs LATENCY + loud
// counters, never unbounded memory.
const orderedQueueCap = 256

// orderedEnqueueWait is one bounded pass of the never-drop overflow wait. Each
// elapsed pass logs ERROR and counts; the enqueue then waits again — the frame
// is applied late, never lost.
const orderedEnqueueWait = 5 * time.Second

// orderedOverflowLogInterval rate-limits the full-queue ERROR so a stalled
// worker is loud, not log-flooding.
const orderedOverflowLogInterval = 2 * time.Second

// runOrderedOwner drains the FIFO until closed-and-empty, calling handlers on
// THIS goroutine with NO lock held (R3: a handler that re-registers or takes
// its time can never wedge registration or the read loop).
func (s *TCPServer) runOrderedOwner(key string, owner *orderedOwner) {
	for {
		owner.queueMu.Lock()
		for len(owner.queue) == 0 && !owner.closed {
			owner.queueMu.Unlock()
			<-owner.wake
			owner.queueMu.Lock()
		}
		if len(owner.queue) == 0 {
			owner.queueMu.Unlock()
			return // closed and drained — nothing is lost, nothing is applied late
		}
		it := owner.queue[0]
		owner.queue = owner.queue[1:]
		// Copy the handler set locally; the callback runs with NO lock held.
		h := owner.handlers
		owner.queueMu.Unlock()
		// P2-14: wake one overflow-blocked enqueuer per dequeued frame
		// (coalesced cap-1 — several waiters retry on the lock in turn).
		select {
		case owner.space <- struct{}{}:
		default:
		}

		switch it.kind {
		case orderedOrder:
			if h.Order != nil {
				s.waitForPostUpdateSnapshot(&it)
				h.Order(it.order)
			}
		case orderedFill:
			if h.Fill != nil {
				h.Fill(it.fill)
			}
		case orderedClose:
			if h.Close != nil {
				h.Close(it.close_)
			}
		}
	}
}

// waitForPostUpdateSnapshot blocks (bounded) until an order_snapshot has been
// applied AFTER this order_update was received. The AddOn sends
// order_update then order_snapshot on the same state change; applying the
// update before the snapshot would let recordAcceptedRisk read the PRE-change
// broker book. On timeout the frame is applied with BookGate=bookNotFresh so
// the book is suppressed rather than read stale.
// snapSeqFor returns the order_snapshot watermark for one account (0 when none
// has ever been seen). PER-ACCOUNT: a snapshot for account B must never satisfy
// account A's post-update wait (the R4 review finding F-B — one global counter
// let another account's snapshot certify A's pre-change book).
func (s *TCPServer) snapSeqFor(account string) int64 {
	s.snapSeqMu.Lock()
	defer s.snapSeqMu.Unlock()
	return s.snapSeq[account]
}

// snapSeqBump advances the account's watermark by one (called by the read loop
// on every VALID order_snapshot for that account).
func (s *TCPServer) snapSeqBump(account string) int64 {
	s.snapSeqMu.Lock()
	defer s.snapSeqMu.Unlock()
	if s.snapSeq == nil {
		s.snapSeq = make(map[string]int64)
	}
	s.snapSeq[account]++
	return s.snapSeq[account]
}

func (s *TCPServer) waitForPostUpdateSnapshot(it *orderedItem) {
	at := it.snapAt
	deadline := time.Now().Add(orderedSnapWait)
	for s.snapSeqFor(it.order.Account) <= at {
		if time.Now().After(deadline) {
			s.logger.Warn("tcp_server: ordered order_update applied WITHOUT a post-update snapshot (book suppressed — never read stale)", "wait", orderedSnapWait)
			it.order.BookGate = BookGateNotFresh
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	it.order.BookGate = BookGateFresh
}

// BookGate marks whether the broker book is proven post-change at application
// time. BookGateUnspecified = legacy channel path (today's behavior);
// BookGateFresh = the worker observed a post-update snapshot; BookGateNotFresh
// the book must NOT be read.
const (
	BookGateUnspecified int8 = 0
	BookGateFresh       int8 = 1
	BookGateNotFresh    int8 = 2
)
