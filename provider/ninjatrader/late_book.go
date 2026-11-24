package ninjatrader

import (
	"net"
	"time"
)

// ── LATE ORDER BOOKS (look-twice, third review of 2026-11-24) ────────────────
//
// The book's twin of late_frame.go. The AddOn builds an order_snapshot on several
// threads (the order-update event, the 30 s loop), stamps it (emitted_at_ms) and
// then waits for the socket's write lock, so a book stamped BEFORE another can be
// written AFTER it. The cache keeps the last one written: a stop that is working
// is then missing from it until the next book, up to 30 s later.
//
// A book stamped OLDER than the newest one of the same account on the same
// connection is LATE: the protection net may not place, close or alert on it. It
// is still the cached book for every other reader.
//
// LATE IS NOT FALSE, and the stamp is the Windows clock, which can be set back:
// then every book looks late. A late book is late by a write, not by seconds, so
// one that is newer than the late books before it and was RECEIVED
// positionRegrowProof after the first of them (Go's clock) is the newest book: the
// clock moved. The 30 s resend, or at worst the one after it, always ends it, and
// the hold's 35 s P0 does not depend on the book at all.
//
// No stamp (0), or a connection that is not known: nothing to compare, not late.

type bookOrder struct {
	link    net.Conn  // the connection the stamps below were read on
	newest  int64     // the newest stamp there, ms
	late    bool      // THIS book was stamped before it
	lateAt  time.Time // the first book of the late run was received
	lateMax int64     // its newest stamp
}

// next is the order state of a book stamped emitted and received at on link.
func (o bookOrder) next(emitted int64, at time.Time, link net.Conn) bookOrder {
	if link == nil || o.link != link {
		return bookOrder{link: link, newest: emitted}
	}
	switch {
	case emitted <= 0: // no stamp: not late, and no news of the order either
		return bookOrder{link: link, newest: o.newest}
	case emitted >= o.newest:
		return bookOrder{link: link, newest: emitted}
	case !o.late:
		return bookOrder{link: link, newest: o.newest, late: true, lateAt: at, lateMax: emitted}
	case emitted < o.lateMax: // late even among the late ones, or the clock went back again: a new run
		return bookOrder{link: link, newest: o.newest, late: true, lateAt: at, lateMax: emitted}
	case emitted > o.lateMax && at.Sub(o.lateAt) >= positionRegrowProof: // the Windows clock was set back
		return bookOrder{link: link, newest: emitted}
	}
	return bookOrder{link: link, newest: o.newest, late: true, lateAt: o.lateAt, lateMax: emitted}
}

// LatestOrder is LatestReceived with whether that book was written late.
func (c *OrderSnapshotCache) LatestOrder(account string) (p OrderSnapshotPayload, receivedAt time.Time, late, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byKey[snapKey(account)]
	return s.Payload, s.ReceivedAt, s.order.late, ok
}
