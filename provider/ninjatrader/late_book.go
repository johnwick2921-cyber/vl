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
// then every book looks late. Two rules end that, both in Go's receive time:
//
//  1. ONE STEP. A late book that is newer than the late books before it, was
//     RECEIVED positionRegrowProof after the first of them and is STAMPED
//     lateBookStepProof after it is the newest book: the clock moved. The Go time
//     alone is not enough: two books really written late, with two 5 s stalls of
//     the read loop between them, look 13 s apart - and their stamps do not. The
//     30 s resend, or at worst the one after it, ends a single step.
//  2. A CLOCK THAT KEEPS STEPPING BACK starts a new run with every step, so rule 1
//     may never hold. After lateBookCap of ONLY late books the newest one received
//     is evidence again. A book is late by a write: three resends in a row that
//     are all late are not, and the alternative is worse - a really naked position
//     that the bot never protects for as long as the clock keeps moving.
//
// A hold's 35 s P0 runs on through a late book, whether that book shows a stop or
// not (a book that cannot be read is UNKNOWN, and its P0 comes after 10 minutes).
//
// No stamp (0), or a connection that is not known: nothing to compare, not late.

const (
	// lateBookStepProof is how far the STAMPS must move over a late run before the
	// clock is taken to have been set back. The run lasts at least
	// positionRegrowProof (12 s) in Go time; a real step keeps the Windows clock
	// running, so its stamps move as far, less a late first read.
	lateBookStepProof = 10 * time.Second
	// lateBookCap: three 30 s resends.
	lateBookCap = 90 * time.Second
)

type bookOrder struct {
	link      net.Conn  // the connection the stamps below were read on
	newest    int64     // the newest stamp there, ms
	late      bool      // THIS book was stamped before it
	lateSince time.Time // every book since then was late
	lateAt    time.Time // the first book of the late run was received
	lateFirst int64     // its stamp
	lateMax   int64     // the run's newest stamp
}

// next is the order state of a book stamped emitted and received at on link.
func (o bookOrder) next(emitted int64, at time.Time, link net.Conn) bookOrder {
	if link == nil || o.link != link {
		return bookOrder{link: link, newest: emitted}
	}
	since := at
	if o.late {
		since = o.lateSince
	}
	run := bookOrder{link: link, newest: o.newest, late: true, lateSince: since, lateAt: at, lateFirst: emitted, lateMax: emitted}
	switch {
	case emitted <= 0: // no stamp: not late, and no news of the order either
		return bookOrder{link: link, newest: o.newest}
	case emitted >= o.newest:
		return bookOrder{link: link, newest: emitted}
	case at.Sub(since) >= lateBookCap: // nothing but late books all that time: the clock keeps moving
		return bookOrder{link: link, newest: emitted}
	case !o.late, emitted < o.lateMax: // late even among the late ones, or the clock went back again: a new run
		return run
	case emitted > o.lateMax && at.Sub(o.lateAt) >= positionRegrowProof &&
		emitted-o.lateFirst >= lateBookStepProof.Milliseconds(): // the Windows clock was set back, once
		return bookOrder{link: link, newest: emitted}
	}
	run.lateAt, run.lateFirst = o.lateAt, o.lateFirst
	return run
}

// LatestOrder is LatestReceived with whether that book was written late.
func (c *OrderSnapshotCache) LatestOrder(account string) (p OrderSnapshotPayload, receivedAt time.Time, late, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byKey[snapKey(account)]
	return s.Payload, s.ReceivedAt, s.order.late, ok
}
