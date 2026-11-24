package ninjatrader

import (
	"strings"
	"time"
)

// ── LATE POSITIONS FRAMES (look-twice, review of 2026-11-24) ────────────────
//
// The AddOn reads the account's positions and writes the frame in two steps, on
// several threads (NT8's PositionUpdate event, the 30 s resend). A frame BUILT
// before an exit filled can therefore be WRITTEN after the Flat frame. It is the
// newest frame Go has, newer than every fill, and it shows the dead position
// until the next one, up to 30 s later. No fence and no link check sees that:
// the frame really is the last thing the AddOn wrote.
//
// What contradicts it is a frame that came BEFORE it: the position had already
// been reported flat (or smaller), and no entry fill has been received since. A
// position cannot grow without one - except by a hand order, which the AddOn may
// not report at all (NT8 gives it no name). So such a growth is not called false,
// it is UNPROVEN: a later frame that still shows it proves it. A late frame is
// late by a write, not by seconds; the truth comes with the next 30 s resend.
//
// Kept per account, symbol and side (rows of two contract months summed), on the
// ONE read loop, in Go receive time. It survives a reconnect. Evidence only:
// GetPositions does not read it.

// positionRegrowProof is how much later a second frame must show the same size.
// Above the 5 s the read loop can stall on the order-snapshot insert (frames
// waiting in the socket then look seconds apart), below the 30 s resend - so the
// next resend, or at worst the one after it, always settles it.
const positionRegrowProof = 6 * time.Second

type positionLevel struct {
	qty     int       // in the newest frame
	floor   int       // the size nothing contradicts
	floorAt time.Time // when the newest frame at that size was received
	grownAt time.Time // the first frame of the current unproven size (qty > floor)
}

// notePositionLevelsLocked takes one positions frame. Called before the account's
// receive instant is replaced. acctMu held.
func (s *TCPServer) notePositionLevelsLocked(account string, ps []OpenPosition, now time.Time) {
	prevAt, known := s.acctPositionsReceived[account]
	sizes := make(map[string]int)
	symbols := make(map[string]string)
	for _, p := range ps {
		if p.Quantity > 0 {
			k := positionLevelKey(p.Symbol, p.Side)
			sizes[k] += p.Quantity
			symbols[k] = p.Symbol
		}
	}
	prev := s.positionLevels[account]
	next := make(map[string]positionLevel, len(sizes)) // a side the frame does not show is flat: forgotten
	for k, qty := range sizes {
		l, ok := prev[k]
		switch {
		case ok:
		case known:
			l = positionLevel{floorAt: prevAt} // the frame before this one showed none
		default:
			l = positionLevel{floor: qty} // the account's first frame: nothing contradicts it
		}
		var opened time.Time
		if r := s.entryReceipts[subKey(symbols[k], account)]; r != nil {
			opened = r.opened
		}
		switch {
		case qty <= l.floor, opened.After(l.floorAt):
			l.floor, l.floorAt, l.grownAt = qty, now, time.Time{}
		case qty != l.qty:
			l.grownAt = now
		case now.Sub(l.grownAt) >= positionRegrowProof:
			l.floor, l.floorAt, l.grownAt = qty, now, time.Time{}
		}
		l.qty = qty
		next[k] = l
	}
	if s.positionLevels == nil {
		s.positionLevels = make(map[string]map[string]positionLevel)
	}
	s.positionLevels[account] = next
}

func positionLevelKey(symbol, side string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side))
}

// Unproven: the newest frame shows MORE of this side than a frame before it, no
// entry fill has been received since that smaller frame, and no later frame has
// repeated it yet. It may be a frame written late; nothing may be sent on it.
func (e ProtectionEvidence) Unproven(symbol, side string) bool {
	l, ok := e.levels[positionLevelKey(symbol, side)]
	return ok && l.qty > l.floor && !e.EntryOpened.After(l.floorAt)
}
