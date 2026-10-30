package ninjatrader

import (
	"strings"
	"sync"
	"time"
)

// ── ROLL WAVE (2026-09-10) — THE CURRENT CONTRACT IS ONE VALUE FROM ONE SOURCE ─
//
// The AddOn resolves the front month by a UTC date rule (VLContractResolver.cs
// :80, expiry minus eight days) and re-subscribes on reconnect. On 2026-09-10 the
// rule flipped at 19:00 CT; the wire changed at the 21:15:03 CT reconnect, when
// the subscription ACK named "MNQ 12-26" where the previous one had named
// "MNQ 09-26". Nothing on the Go side noticed: the ring kept ~2,000 September
// bars and appended December ones, and a ~292-point step presented to every
// reader as a move.
//
// The rule now: the contract is whatever the AddOn's MOST RECENT RECEIVED frame
// named. That frame is the `subscribed` ACK — the hello carries only build_id,
// and bar frames carry no instrument at all (C4). A date rule is exactly the
// defect this replaces and is never consulted (A24).

// ContractFact is the current contract with its provenance: the frame that
// named it and when that frame was received (the four-clock rule — a fact
// carries its observation and its receipt time, and names its source).
type ContractFact struct {
	Contract   string    // e.g. "MNQ 12-26"
	Source     string    // "subscribed" — the only frame that names it today
	ReceivedAt time.Time // when that ACK arrived
	Previous   string    // the contract before the last roll ("" if never rolled this process)
	RolledAt   time.Time // zero if no roll observed this process
}

// CurrentContract returns the contract the AddOn most recently named for a
// symbol. ok=false means NO frame has named one this process — the caller
// prints n/a and never guesses (A24).
func (s *TCPServer) CurrentContract(symbol string) (ContractFact, bool) {
	if s == nil {
		return ContractFact{}, false
	}
	key := strings.ToUpper(strings.TrimSpace(symbol))
	s.subStateMu.RLock()
	st, ok := s.subStates[key]
	s.subStateMu.RUnlock()
	if !ok || st.State != "subscribed" || strings.TrimSpace(st.Contract) == "" {
		return ContractFact{}, false
	}
	f := ContractFact{Contract: st.Contract, Source: "subscribed", ReceivedAt: st.UpdatedAt}
	s.rollMu.RLock()
	if r, ok := s.rolls[key]; ok {
		f.Previous, f.RolledAt = r.from, r.at
	}
	s.rollMu.RUnlock()
	return f, true
}

// rollEvent is one observed contract change for a symbol, kept so the boot
// line and the desk strip can say "since <ts>" from the record.
type rollEvent struct {
	from, to string
	at       time.Time
}

// RollListener is called ONCE per observed roll, after the ring for that
// symbol has been purged. It runs on the reader goroutine — do the work
// elsewhere. The persist layer uses it to reseed from the store for the new
// contract only and to raise the owner-facing P0.
type RollListener func(symbol, from, to string, at time.Time)

var (
	rollListenersMu sync.RWMutex
	rollListeners   []RollListener
)

// OnContractRoll registers a listener. Registration is additive and the
// listener is never removed; the persist wire registers exactly one.
func OnContractRoll(fn RollListener) {
	if fn == nil {
		return
	}
	rollListenersMu.Lock()
	rollListeners = append(rollListeners, fn)
	rollListenersMu.Unlock()
}

// observeContract is called from the `subscribed` ACK handler with the
// contract the AddOn just named. If it differs from the one previously named
// for this symbol, that is a ROLL:
//
//   - the ring for the symbol is PURGED — every timeframe, because every
//     timeframe was on the old scale. A ring that keeps the retired contract's
//     bars and appends the new one's is the 292-point tape this wave exists to
//     end. The AddOn's post-subscribe historical replay refills it on the new
//     contract; the store reseed (listener) adds what the replay does not carry.
//   - the event is recorded with both symbols and the timestamp, logged, and
//     handed to the listeners exactly once.
//
// A repeated ACK naming the SAME contract — a reconnect without a roll — does
// nothing. The first ACK of a process is not a roll either: there is no
// previous value to have rolled from.
func (s *TCPServer) observeContract(symbol, contract string, at time.Time) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	contract = strings.TrimSpace(contract)
	if key == "" || contract == "" {
		return
	}
	// ONE entry records the ACK. The subscription state (what CurrentContract
	// reads) and the roll record are written together so no caller — handler
	// or fixture — can update one and forget the other. The first draft had the
	// handler call setSubState and then this; the pin called only this, and
	// CurrentContract answered "nothing named" after a roll it had just seen.
	s.setSubState(key, "subscribed", contract, "")
	s.rollMu.Lock()
	if s.lastNamed == nil {
		s.lastNamed = make(map[string]string)
	}
	if s.rolls == nil {
		s.rolls = make(map[string]rollEvent)
	}
	prev := s.lastNamed[key]
	s.lastNamed[key] = contract
	if prev == "" || prev == contract {
		s.rollMu.Unlock()
		return
	}
	s.rolls[key] = rollEvent{from: prev, to: contract, at: at}
	s.rollMu.Unlock()

	purged := 0
	if s.barCache != nil {
		purged = s.barCache.PurgeSymbol(key)
	}
	liveContracts.reset(key) // m446: the retired contract left the ring with the purge
	histContracts.reset(key) // ... and so did the seed it was built from
	// A9 — loud, with both symbols and the timestamp, every time.
	s.logger.Warn("📜 CONTRACT ROLLED — ring purged; the retired contract's bars stay in the store as history and are filtered, never deleted",
		"symbol", key, "from", prev, "to", contract, "at", at.Format(time.RFC3339), "ring_bars_purged", purged)

	rollListenersMu.RLock()
	ls := append([]RollListener(nil), rollListeners...)
	rollListenersMu.RUnlock()
	for _, fn := range ls {
		fn(key, prev, contract, at)
	}
}
