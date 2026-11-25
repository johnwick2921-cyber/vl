package ninjatrader

import (
	"strconv"
	"strings"
	"time"
)

// ── EXIT FENCE (look-twice, spec 2026-11-24 section 3 A) ────────────────────
//
// entry_receipt.go answers "is this positions snapshot newer than the last
// ENTRY fill". Nothing answered the twin question, and on 2026-11-19 06:47:30
// that cost -1207: the stop had filled, its leg was gone from the book, and the
// newest positions frame still said Short 1 because it was RECEIVED before the
// exit fill.
//
// This is a RECORD only. GetPositions, PositionsFor and
// PositionsForExecutionReceipt do not read it and do not change: an exit-fenced
// GetPositions would fall to the fill-derived cache, which still says "open".
//
// Protected by TCPServer.acctMu, lives on the SERVER (survives reconnect and
// adapter replacement), and is stamped on the ONE read loop, so it is ordered
// against the positions / order_snapshot stamps of the same loop. Compare only
// Go receive times (ReceiptNow); exit_time is the AddOn's own clock and is used
// here as an identity string, never as an instant.

// exitPairWindow bounds how long a position_close may be read as the SECOND
// frame of a fill its order_update already stamped. The AddOn writes both from
// one OnOrderUpdate call; the read loop can stall up to the 5 s SQLite busy
// timeout between them (order-snapshot sink). Past the window the close stamps
// again — a fence that is too NEW only holds longer, one that is too OLD acts
// on a dead position.
const exitPairWindow = 10 * time.Second

// exitLegState is what is already counted for one exit leg of one signal.
type exitLegState struct {
	qty      int  // cumulative quantity counted for the CURRENT order of this name
	terminal bool // that order's full fill was counted (either frame)

	// unpaired: an order_update "filled" was counted and its position_close has
	// not arrived yet. One close of the same quantity pairs with it.
	unpaired   bool
	unpairedAt time.Time

	closes map[string]struct{} // position_close identities already seen
}

type exitReceiptState struct {
	received time.Time
	legs     map[string]*exitLegState
}

// exitLegSuffixes maps an AddOn order-name suffix to the canonical leg key.
// Longest first: "-sl2" does not end in "-sl", but the order documents intent.
var exitLegSuffixes = []struct{ suffix, reason, leg string }{
	{"-sl3", "sl", "3"}, {"-tp3", "tp", "3"},
	{"-sl2", "sl", "2"}, {"-tp2", "tp", "2"},
	{"-sl", "sl", "1"}, {"-tp", "tp", "1"},
	{"-lx", "limit", "1"}, // the AddOn reports a -lx fill as reason "limit"
}

// exitKeyFromOrderName is the canonical exit key of an order_update, or
// ok=false when the name is not one of the AddOn's exit legs ("Close" and hand
// orders are fenced from position_close only: the AddOn de-dupes order_update
// by NAME, and "Close" is one name for every account).
func exitKeyFromOrderName(orderName string) (string, bool) {
	name := strings.TrimSpace(orderName)
	for _, s := range exitLegSuffixes {
		if base, ok := strings.CutSuffix(name, s.suffix); ok && base != "" {
			return base + "|" + s.reason + "|" + s.leg, true
		}
	}
	return "", false
}

// exitKeyFromClose is the SAME canonical key built from a position_close. A
// single bracket's -sl/-tp reports leg 0 and a split bracket's reports leg 1;
// both are the order named "<signal>-sl"/"-tp", so 0 and 1 are one key.
func exitKeyFromClose(p PositionClosePayload) string {
	leg := p.Leg
	if leg <= 1 {
		leg = 1
	}
	return strings.TrimSpace(p.SignalID) + "|" + strings.ToLower(strings.TrimSpace(p.ExitReason)) + "|" + strconv.Itoa(leg)
}

// exitLegLocked returns the fence and the leg state (acctMu held).
func (s *TCPServer) exitLegLocked(symbol, account, key string) (*exitReceiptState, *exitLegState) {
	k := subKey(symbol, account)
	if s.exitReceipts == nil {
		s.exitReceipts = make(map[string]*exitReceiptState)
	}
	r := s.exitReceipts[k]
	if r == nil {
		r = &exitReceiptState{legs: make(map[string]*exitLegState)}
		s.exitReceipts[k] = r
	}
	l := r.legs[key]
	if l == nil {
		l = &exitLegState{closes: make(map[string]struct{})}
		r.legs[key] = l
	}
	return r, l
}

// noteExitOrderUpdate stamps the exit fence from an order_update of one of the
// AddOn's own exit legs. Only a NEW fill moves it: the quantity is cumulative,
// so an equal or smaller repeat is a replay.
//
// Names are REUSED (place_protective_stop names its order "<signal>-sl", like
// leg 1 of the bracket). A standing order with NOTHING filled, under a name
// whose fill was already counted, can only be another order: the count restarts.
func (s *TCPServer) noteExitOrderUpdate(p OrderUpdatePayload) {
	if p.CancelReport || strings.TrimSpace(p.Symbol) == "" || strings.TrimSpace(p.Account) == "" {
		return // a cancel-report echo repeats history, it is not a fill event
	}
	key, ok := exitKeyFromOrderName(p.OrderName)
	if !ok {
		return
	}
	state := normalizeOrderState(p.State)
	filled := state == "filled"
	partial := state == "partfilled" || state == "partial"
	live := ClassifyOrderState(p.State)
	standing := live == LivenessPending || live == LivenessLive || live == LivenessLocal
	ended := live == LivenessTerminal && !filled // cancelled/rejected/expired: no fill by itself
	if !filled && !partial && !ended && !(standing && p.Quantity == 0) {
		return
	}

	s.acctMu.Lock()
	defer s.acctMu.Unlock()
	r, l := s.exitLegLocked(p.Symbol, p.Account, key)
	if ended {
		l.terminal = l.terminal || l.qty > 0 // part-filled, then cancelled: that order is over
		return
	}
	if p.Quantity <= 0 {
		if standing && l.terminal {
			l.qty, l.terminal, l.unpaired = 0, false, false
		}
		return
	}
	if p.Quantity <= l.qty {
		return
	}
	now := s.ReceiptNow()
	l.qty = p.Quantity
	r.received = now
	if filled {
		l.terminal = true
		l.unpaired, l.unpairedAt = true, now
	}
}

// noteExitClose stamps the exit fence from a position_close. Called BEFORE
// verifyInbound: the identity check decides whether the close may be RECORDED,
// but the fill itself is a broker fact. The close of a fill already counted
// from its order_update does not move the fence (one fill, one stamp); a close
// with a new seq+exit_time that pairs with nothing is a new fill even on a
// reused name.
func (s *TCPServer) noteExitClose(p PositionClosePayload) {
	if strings.TrimSpace(p.Symbol) == "" || strings.TrimSpace(p.Account) == "" || p.Quantity <= 0 {
		return
	}
	id := strconv.Itoa(p.Leg) + "|" + strconv.FormatUint(p.Seq, 10) + "|" + strings.TrimSpace(p.ExitTime)

	s.acctMu.Lock()
	defer s.acctMu.Unlock()
	r, l := s.exitLegLocked(p.Symbol, p.Account, exitKeyFromClose(p))
	if _, seen := l.closes[id]; seen {
		return // the same frame again
	}
	l.closes[id] = struct{}{}
	now := s.ReceiptNow()
	if l.unpaired && p.Quantity == l.qty && now.Sub(l.unpairedAt) <= exitPairWindow {
		l.unpaired = false
		return
	}
	l.unpaired = false
	l.terminal = true
	if p.Quantity > l.qty {
		l.qty = p.Quantity // a late order_update of this same fill is not a second one
	}
	r.received = now
}

// ExecutionReceipts is one consistent view of the Go RECEIVE instants that
// decide whether a positions snapshot can be trusted after a fill. A zero time
// means "never seen in this process" — unknown, not "long ago".
type ExecutionReceipts struct {
	Positions time.Time // the account's newest positions frame
	Entry     time.Time // newest entry fill on (symbol, account)
	Exit      time.Time // newest exit fill on (symbol, account)
}

// PositionsAfterFills reports whether the newest positions frame was received
// strictly AFTER both fences. No positions frame at all is false.
func (e ExecutionReceipts) PositionsAfterFills() bool {
	return !e.Positions.IsZero() && e.Positions.After(e.Entry) && e.Positions.After(e.Exit)
}

// ExecutionReceiptsFor reads the three instants under one lock.
func (s *TCPServer) ExecutionReceiptsFor(account, symbol string) ExecutionReceipts {
	s.acctMu.RLock()
	defer s.acctMu.RUnlock()
	var out ExecutionReceipts
	if strings.TrimSpace(account) == "" {
		return out
	}
	out.Positions = s.acctPositionsReceived[account]
	key := subKey(symbol, account)
	if r := s.entryReceipts[key]; r != nil {
		out.Entry = r.received
	}
	if r := s.exitReceipts[key]; r != nil {
		out.Exit = r.received
	}
	return out
}
