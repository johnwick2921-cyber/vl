package main
package ninjatrader

import (
	"errors"
	"strings"
	"sync"
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
//   - The read loop only ENQUEUES — non-blocking, never drops. The queue is an
//     UNBOUNDED mutex-guarded slice + a coalesced wake channel (say which: a
//     slice, so a stuck consumer grows memory instead of losing a fill; the
//     wake channel is cap 1 so a burst of frames still wakes the worker once).
//     No SQLite work ever runs on the read goroutine.
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
type OrderedExecutionHandlers struct {
	Order func(OrderUpdatePayload)
	Fill  func(FillPayload)
	Close func(PositionClosePayload)
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
	wake     chan struct{} // cap 1, coalesced
	handlers OrderedExecutionHandlers
	closed   bool
}

func newOrderedOwner(h OrderedExecutionHandlers) *orderedOwner {
	return &orderedOwner{handlers: h, wake: make(chan struct{}, 1)}
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
			// Wake the worker so it observes closed and drains out.
			select {
			case owner.wake <- struct{}{}:
			default:
			}
		})
	}, nil
}

// enqueueOrdered appends one frame to the owner's queue (non-blocking, never
// drops) and returns whether an owner consumed it. Callers must be the read
// loop only.
func (s *TCPServer) enqueueOrdered(key string, it orderedItem) bool {
	s.orderedMu.Lock()
	owner := s.orderedOwners[key]
	s.orderedMu.Unlock()
	if owner == nil {
		return false
	}
	owner.queueMu.Lock()
	if owner.closed {
		owner.queueMu.Unlock()
		return false
	}
	owner.queue = append(owner.queue, it)
	owner.queueMu.Unlock()
	select {
	case owner.wake <- struct{}{}:
	default:
	}
	return true
}

// orderedSnapWait bounds how long a worker waits for the post-update
// order_snapshot watermark before applying with the book suppressed.
const orderedSnapWait = 2 * time.Second

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

// snapSeqFor returns the order_snapshot watermark for one account (0 when none
// has ever been seen). PER-ACCOUNT: a snapshot for account B must never satisfy
// account A's post-update wait (the R4 review finding — one global counter let
// another account's snapshot certify A's pre-change book).
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

// waitForPostUpdateSnapshot blocks (bounded) until an order_snapshot for the
// UPDATE'S OWN account has been applied AFTER this order_update was received. The AddOn sends
// order_update then order_snapshot on the same state change; applying the
// update before the snapshot would let recordAcceptedRisk read the PRE-change
// broker book. On timeout the frame is applied with BookGate=bookNotFresh so
// the book is suppressed rather than read stale.
func (s *TCPServer) waitForPostUpdateSnapshot(it *orderedItem) {
	at := it.snapAt
	deadline := time.Now().Add(orderedSnapWait)
	for s.snapSeqFor(it.order.Account) <= at {
		if time.Now().After(deadline) {
			s.logger.Warn("tcp_server: ordered order_update applied WITHOUT a post-update snapshot (book suppressed — never read stale)", "wait", orderedSnapWait, "account", it.order.Account)
			it.order.BookGate = BookGateNotFresh
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	it.order.BookGate = BookGateFresh
}

// BookGate marks whether the broker book is proven post-change at application
// time. BookGateUnspecified = legacy channel path (today's behavior);
// BookGateFresh = the worker observed a post-update snapshot;
// BookGateNotFresh = it timed out and the book must NOT be read.
const (
	BookGateUnspecified int8 = 0
	BookGateFresh       int8 = 1
	BookGateNotFresh    int8 = 2
)
