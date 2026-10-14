package ninjatrader

import (
	"fmt"
	"sync"
)

// B3 — duplicate-order guard + rate limiter. Two structural protections at the
// order-submission chokepoint:
//   - DUPE GUARD: an identical order action (idempotence key: account|side|symbol|
//     qty) repeated within ~one bar is DROPPED — a replayed / double-fired signal
//     can never place a second order in the same bar.
//   - RATE LIMITER: if more than orderRateMax order-actions fire within
//     orderRateWindow, a breaker trips and halts new actions (🚨) until the window
//     drains — a runaway loop can't machine-gun the broker.

const (
	orderDedupWindowMs = 55_000 // drop an identical order within ~1 (1-minute) bar
	orderRateWindowMs  = 60_000 // rate-limit sliding window
	orderRateMax       = 10     // max order-actions per window before the breaker trips
)

type orderGuard struct {
	mu          sync.Mutex
	lastSeen    map[string]int64 // idempotence key → ms of its last SENT action
	actionTimes []int64          // sent-action timestamps within the rate window
	inFlight    map[string]bool  // keys reserved and not yet sent or released
}

func newOrderGuard() *orderGuard {
	return &orderGuard{lastSeen: make(map[string]int64), inFlight: make(map[string]bool)}
}

// reserve is B3's CHECK, split from its COMMIT (W1b E12(b)). It reports whether
// an order action with the given idempotence key may proceed at nowMs; ok==false
// → the action MUST be dropped, and reason says why (duplicate or rate breaker).
//
// On ok the key is only RESERVED: the caller MUST call done exactly once (a
// deferred call is the pattern). done(true) — the send was attempted — records
// the key and the action, and the dedupe window and the rate breaker count it;
// done(false) — refused or aborted before the wire — releases the reservation
// and records NOTHING. Before this split the key was recorded at admit, so a
// refusal after B3 (the ledger registration, a missing bracket, the maintenance
// hold at the flush) burned the 55 s window and the legitimate retry of an
// order that never reached the broker was dropped as its "duplicate".
//
// Fail-closed while reserved: the same key is refused as a duplicate until done
// runs, and a reservation counts toward the rate breaker, so a concurrent
// caller cannot slip a second identical order through the check/commit gap.
// Consequence named for the record: the breaker now counts SENT actions (plus
// reservations in flight), not admitted ones.
func (g *orderGuard) reserve(key string, nowMs int64) (done func(sent bool), reason string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if last, seen := g.lastSeen[key]; seen && nowMs-last < orderDedupWindowMs {
		return nil, fmt.Sprintf("duplicate order dropped — same key within %dms [%s]", nowMs-last, key), false
	}
	if g.inFlight[key] {
		return nil, fmt.Sprintf("duplicate order dropped — same key already in flight [%s]", key), false
	}

	// Prune the rate window, then count (sent actions + reservations in flight).
	cut := nowMs - orderRateWindowMs
	kept := g.actionTimes[:0]
	for _, ts := range g.actionTimes {
		if ts >= cut {
			kept = append(kept, ts)
		}
	}
	g.actionTimes = kept
	if n := len(g.actionTimes) + len(g.inFlight); n >= orderRateMax {
		return nil, fmt.Sprintf("rate breaker TRIPPED — %d order-actions within %ds; halting new actions", n, orderRateWindowMs/1000), false
	}

	g.inFlight[key] = true
	var once sync.Once
	return func(sent bool) {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.inFlight, key)
			if sent {
				g.lastSeen[key] = nowMs
				g.actionTimes = append(g.actionTimes, nowMs)
			}
		})
	}, "", true
}

// admit is reserve + done(true): check and record in one step, for a caller
// whose action is sent unconditionally once admitted.
func (g *orderGuard) admit(key string, nowMs int64) (reason string, ok bool) {
	done, reason, ok := g.reserve(key, nowMs)
	if ok {
		done(true)
	}
	return reason, ok
}

// ageForTest moves every recorded send d into the past — a fixture's way of
// saying "more than one bar has passed" without sleeping through the 55 s
// dedupe window. Test seam only; never called by production code.
func (g *orderGuard) ageForTest(d int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, v := range g.lastSeen {
		g.lastSeen[k] = v - d
	}
	for i := range g.actionTimes {
		g.actionTimes[i] -= d
	}
}
