package ninjatrader

import (
	"strings"
	"time"
)

// ── OWN CLOSE LOG (look-twice, spec 2026-11-24 section 3 C) ──────────────────
//
// Which close_position frames did WE write, and when. The broker's order name
// proves nothing: NT8 names every Flatten "Close", the owner's own Chart Trader
// close included. A resting limit exit is named after our own signal id, so it
// is matched by id; a market close is matched by send instant. In memory only:
// a restart forgets it, and an exit nobody can attribute is never called ours.

// ownCloseLogCap bounds the log per (symbol, account).
const ownCloseLogCap = 16

// OwnClose is one close_position frame this process wrote.
type OwnClose struct {
	SignalID string
	Market   bool      // no limit price: the AddOn flattens ("Close")
	SentAt   time.Time // ReceiptNow, just before the write
}

// noteOwnClose records a close before its write; the returned func forgets it
// again (the write failed: that close never reached NT8 whole).
func (s *TCPServer) noteOwnClose(p ClosePositionPayload) (forget func()) {
	if strings.TrimSpace(p.Symbol) == "" || strings.TrimSpace(p.Account) == "" {
		return func() {}
	}
	key := subKey(p.Symbol, p.Account)
	c := OwnClose{SignalID: p.SignalID, Market: p.LimitPrice <= 0, SentAt: s.ReceiptNow()}
	s.acctMu.Lock()
	defer s.acctMu.Unlock()
	if s.ownCloses == nil {
		s.ownCloses = make(map[string][]OwnClose)
	}
	l := append(s.ownCloses[key], c)
	if len(l) > ownCloseLogCap {
		l = l[len(l)-ownCloseLogCap:]
	}
	s.ownCloses[key] = l
	return func() {
		s.acctMu.Lock()
		defer s.acctMu.Unlock()
		var kept []OwnClose
		for _, have := range s.ownCloses[key] {
			if have != c {
				kept = append(kept, have)
			}
		}
		s.ownCloses[key] = kept
	}
}

// OwnCloses returns the closes this process sent on (account, symbol), oldest
// first.
func (s *TCPServer) OwnCloses(account, symbol string) []OwnClose {
	s.acctMu.RLock()
	defer s.acctMu.RUnlock()
	return append([]OwnClose(nil), s.ownCloses[subKey(symbol, account)]...)
}
