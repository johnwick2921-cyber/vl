package ninjatrader

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// ── LINK CHECK (look-twice, spec 2026-11-24 section 3 B rule 4) ──────────────
//
// Waiting 3 s proves nothing when the read loop is frozen: the order-snapshot
// sink runs a synchronous DB insert (SQLite busy timeout 5 s) right after the
// stop-less book is cached, so two looks 3 s apart can read the SAME frozen
// data while NT8's own Flat frame waits in the socket.
//
// The AddOn answers every Go heartbeat with ack "heartbeat" on the same ordered
// stream (VLTraderTCPClient.cs HandleMessage -> SendAck -> WriteEnvelope, one
// reader thread, one write lock). So once the ack of a heartbeat WE sent has
// been read, every frame the AddOn wrote before that ack has been processed.
// The ack carries no id, so it is matched by COUNT on ONE connection: TCP keeps
// the order and the AddOn never skips one (its hello, which gates its writes,
// goes out before its read loop starts).
//
// Accounting lives in heartbeatLink, one per published connection. A count from
// an older connection can therefore never satisfy a probe on a newer one.

// HeartbeatAckWait is how long a probe may wait for its ack. Past it the link
// is NOT proven caught up and the caller keeps holding.
const HeartbeatAckWait = 5 * time.Second

type heartbeatLink struct {
	mu        sync.Mutex
	sent      uint64
	acked     uint64
	positions map[string]struct{} // accounts with a positions frame on THIS link
	changed   chan struct{}       // closed and replaced on every ack
}

func newHeartbeatLink() *heartbeatLink {
	return &heartbeatLink{positions: make(map[string]struct{}), changed: make(chan struct{})}
}

func (l *heartbeatLink) noteAck() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acked >= l.sent {
		return // an ack of nothing we sent proves nothing
	}
	l.acked++
	close(l.changed)
	l.changed = make(chan struct{})
}

// linkFor returns the accounting of connection c, or nil when c is not the
// published connection.
func (s *TCPServer) linkFor(c net.Conn) *heartbeatLink {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if c == nil || s.conn != c {
		return nil
	}
	return s.hbLink
}

// writeHeartbeat writes ONE heartbeat to c and counts it on link (nil = not
// counted: a connection published by a test without an accept). Every heartbeat
// this server sends goes through here, or the count would drift.
func (s *TCPServer) writeHeartbeat(c net.Conn, link *heartbeatLink) (seq uint64, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if link != nil {
		// Counted BEFORE the write (the ack may come back at once), under writeMu
		// so the count order is the wire order.
		link.mu.Lock()
		link.sent++
		seq = link.sent
		link.mu.Unlock()
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	// On an error the count STAYS: the frame may be half written, so the stream is
	// out of step and the caller closes this connection (and its accounting).
	err = WriteFrame(c, FrameHeartbeat, HeartbeatPayload{})
	return seq, err
}

// noteHeartbeatAck counts one ack "heartbeat" read on connection c.
func (s *TCPServer) noteHeartbeatAck(c net.Conn) {
	if l := s.linkFor(c); l != nil {
		l.noteAck()
	}
}

// notePositionsOnLink records that c has delivered a positions frame for account.
func (s *TCPServer) notePositionsOnLink(c net.Conn, account string) {
	if l := s.linkFor(c); l != nil {
		l.mu.Lock()
		l.positions[account] = struct{}{}
		l.mu.Unlock()
	}
}

// LinkProbe is one heartbeat sent to ask "has Go read everything the AddOn had
// already written?".
type LinkProbe struct {
	link *heartbeatLink
	seq  uint64
}

// SendLinkProbe sends a heartbeat now. An error means there is no link to check.
// It never blocks on the ack. A failed write closes that connection, as the
// periodic heartbeat does: the next frame after a half-written one - an order -
// would be read as its tail. The AddOn redials; the look restarts there.
func (s *TCPServer) SendLinkProbe() (*LinkProbe, error) {
	s.connMu.Lock()
	c, link := s.conn, s.hbLink
	s.connMu.Unlock()
	if c == nil || link == nil {
		return nil, fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	seq, err := s.writeHeartbeat(c, link)
	if err != nil {
		s.logger.Warn("tcp_server: write link check - closing the connection", "err", err)
		s.closeConnIf(c)
		return nil, fmt.Errorf("ninjatrader/tcp: link probe: %w", err)
	}
	return &LinkProbe{link: link, seq: seq}, nil
}

// Acked reports whether the ack of THIS heartbeat has been read.
func (p *LinkProbe) Acked() bool {
	p.link.mu.Lock()
	defer p.link.mu.Unlock()
	return p.link.acked >= p.seq
}

// Wait blocks until the ack, ctx, or d - whichever comes first. The caller reads
// Acked (or the next pass does); Wait itself decides nothing.
func (p *LinkProbe) Wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		p.link.mu.Lock()
		acked, changed := p.link.acked >= p.seq, p.link.changed
		p.link.mu.Unlock()
		if acked {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// ProtectionEvidence is ONE consistent view of what the protection reconciler
// may act on (look-twice rules 1-3 and 6): the account's newest POSITIONS FRAME
// (never the fill-derived cache) with the receive instants of the fills around
// it, and the instants of our own sends. A zero time = never in this process.
type ProtectionEvidence struct {
	ExecutionReceipts
	// Snapshot is the newest positions frame; Known false = none received.
	Snapshot []OpenPosition
	Known    bool
	// OnLink: that account has sent a positions frame on the CURRENT connection.
	OnLink bool
	// EntryOpened is the newest fill of an ENTRY-named order: Entry without NT8's
	// "Close" (the order_update of a Flatten carries one name as signal and order,
	// so it moves the entry fence too, and it opens nothing).
	EntryOpened time.Time
	CloseSent   time.Time // newest close_position we sent on (symbol, account)
	StopSent    time.Time // newest place_protective_stop we sent on (symbol, account)
	AcceptSeq   uint64    // the connection's accept sequence (0 = never connected)

	levels map[string]positionLevel // late_frame.go; replaced whole by every frame, never edited
}

// LastExecution is the newest of exit fill, entry fill, our own close send and
// our own protective-stop send (rule 3). Zero = none.
func (e ProtectionEvidence) LastExecution() time.Time {
	newest := e.Entry
	for _, t := range []time.Time{e.Exit, e.CloseSent, e.StopSent} {
		if t.After(newest) {
			newest = t
		}
	}
	return newest
}

// Holds reports whether the newest positions frame shows exactly this position.
func (e ProtectionEvidence) Holds(symbol, side string, qty int, avg float64) bool {
	if !e.Known {
		return false
	}
	for _, p := range e.Snapshot {
		if strings.EqualFold(p.Symbol, symbol) && strings.EqualFold(p.Side, side) && p.Quantity == qty && p.AvgPrice == avg {
			return true
		}
	}
	return false
}

// ProtectionEvidenceFor reads the evidence for one account and symbol.
func (s *TCPServer) ProtectionEvidenceFor(account, symbol string) ProtectionEvidence {
	var out ProtectionEvidence
	if strings.TrimSpace(account) == "" {
		return out
	}
	s.connMu.Lock()
	link := s.hbLink
	if s.conn == nil {
		link = nil
	}
	s.connMu.Unlock()
	if link != nil {
		link.mu.Lock()
		_, out.OnLink = link.positions[account]
		link.mu.Unlock()
	}
	s.maint.mu.Lock()
	out.AcceptSeq = s.maint.rec.AcceptSeq
	s.maint.mu.Unlock()

	s.acctMu.RLock()
	defer s.acctMu.RUnlock()
	key := subKey(symbol, account)
	out.Positions = s.acctPositionsReceived[account]
	if r := s.entryReceipts[key]; r != nil {
		out.Entry, out.EntryOpened = r.received, r.opened
	}
	if r := s.exitReceipts[key]; r != nil {
		out.Exit = r.received
	}
	out.CloseSent, out.StopSent = s.closeSent[key], s.stopSent[key]
	out.levels = s.positionLevels[account]
	if v, ok := s.acctPositions[account]; ok {
		out.Known = true
		out.Snapshot = append([]OpenPosition(nil), v...)
	}
	return out
}

// noteOwnSend stamps one of our own order frames just BEFORE its write: the
// AddOn may act on it before the write call returns. A write that then fails
// leaves the stamp - it only makes the reconciler wait longer.
func (s *TCPServer) noteOwnSend(m *map[string]time.Time, symbol, account string) {
	if strings.TrimSpace(symbol) == "" || strings.TrimSpace(account) == "" {
		return
	}
	now := s.ReceiptNow()
	s.acctMu.Lock()
	defer s.acctMu.Unlock()
	if *m == nil {
		*m = make(map[string]time.Time)
	}
	(*m)[subKey(symbol, account)] = now
}

// ErrSendWithheld: the guard of a guarded send said no. Nothing was written.
var ErrSendWithheld = errors.New("the order was withheld at the send")

// GUARDED SENDS (look-twice). The reason to send an order is read before the
// send, and the wait in between - a database stall, another writer holding the
// socket - can outlive it. guard is asked with writeMu HELD, immediately before
// the frame is written; its error means no frame, no send stamp. It must only
// read memory (it may take connMu, acctMu, maint.mu and the order book's lock:
// nothing holds one of those while waiting for writeMu). nil guard = always.

// SendClosePositionIf is SendClosePosition behind a guard.
func (s *TCPServer) SendClosePositionIf(payload ClosePositionPayload, guard func() error) error {
	// The connection is taken BEFORE the guard: a guard that saw another connection
	// refuses, so a frame can never go out on one it did not judge.
	c := s.connectedAuthed()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID) // A2 (G1)
	s.noteOwnSend(&s.closeSent, payload.Symbol, payload.Account)                           // look-twice B rule 3
	forget := s.noteOwnClose(payload)                                                      // look-twice C
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameClosePosition, payload)
	if err != nil {
		forget()
	}
	return err
}

// SendPlaceProtectiveStopIf is SendPlaceProtectiveStop behind a guard.
func (s *TCPServer) SendPlaceProtectiveStopIf(payload PlaceProtectiveStopPayload, guard func() error) error {
	c := s.connectedAuthed()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	if c == nil {
		return fmt.Errorf("ninjatrader/tcp: no NT client connected")
	}
	payload.Seq = s.assignSeqRegister(payload.TraderID, payload.Account, payload.SignalID)
	s.noteOwnSend(&s.stopSent, payload.Symbol, payload.Account) // look-twice B rules 3 and 6
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return WriteFrame(c, FramePlaceProtectiveStop, payload)
}
