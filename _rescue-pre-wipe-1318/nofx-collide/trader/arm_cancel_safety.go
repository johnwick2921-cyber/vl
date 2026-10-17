package trader

import (
	"strings"
	"time"

	nt "nofx/provider/ninjatrader"
	"nofx/store"
)

// ── NEVER CANCEL A FILLED ARM (2026-09-07) ───────────────────────────────────
//
// WHAT HAPPENED. 2026-09-06 23:35:03 an S1 long limit was placed at 29576 and
// filled at ~23:35:22. The reconciler saw the untracked position and
// materialized it at 23:36:42. One cycle later, at 23:37:02, the
// one-open-position guard observed "a position is open" and cancelled the arm
// that had CREATED it — because the LEDGER still read `working`: the fill was
// drained at the END of the cycle, after every guard had already decided.
//
// NT8 cancels by signal id, so that cancel took the bracket with it. Snapshot
// 8208, seven seconds before the guard fired, is the whole story:
//
//     aa07e583-…-sl   Accepted   stop 29554
//     aa07e583-…-tp   Working    limit 29623
//
// TWO CHILDREN AND NO ENTRY. The entry order was already gone — filled — and
// the only things left to cancel were the protections. Position 592 then ran
// naked for 8h18m, through the Monday open.
//
// THE RULE. A cancel targets the ENTRY. If the entry is no longer resting at
// the broker, there is nothing to cancel and the children are not ours to
// touch. The ledger's word is not evidence — it is a memory, and at 23:37:02 it
// was a minute out of date.

// armCancelVerdict is the whole adjudication as one value, so a caller cannot
// act on half of it.
type armCancelVerdict struct {
	Allow bool
	Why   string
}

// entryIsResting reports whether the book holds a WORKING order that IS this
// signal's entry — a bare name, not a bracket child.
//
// NT8 names the entry after the signal and its children "<signal>-sl" /
// "<signal>-tp", so the suffix is what separates "the order we placed" from
// "the protections it grew".
func entryIsResting(book []nt.NT8Order, signalID string) (found bool, childrenSeen bool) {
	sig := strings.ToLower(strings.TrimSpace(signalID))
	if sig == "" {
		return false, false
	}
	for i := range book {
		o := book[i]
		if !o.IsWorking() {
			continue
		}
		n := strings.ToLower(strings.TrimSpace(o.Name))
		switch {
		case n == sig:
			found = true
		case strings.HasPrefix(n, sig+"-"):
			childrenSeen = true
		}
	}
	return found, childrenSeen
}

// adjudicateArmCancel decides whether this arm's signal may be cancelled AT THE
// BROKER. PURE: the caller supplies the ledger state and the book.
//
// It refuses on ignorance. Cancelling is the destructive branch here — it can
// remove a live protective stop — so an unreadable book is a refusal, not a
// permission (A24: UNKNOWN never takes the destructive branch).
// positionContext carries the ONE fact that tells the two children-without-entry
// shapes apart. Its zero value means "we do not know", which is deliberately the
// conservative reading: an unknown position is treated as OPEN, so ignorance
// never reaches the destructive branch (A24).
type positionContext struct {
	Known bool
	Open  bool
}

// adjudicateArmCancel is the conservative entry point: no position context, so
// the children case is always refused. Every caller that has not established
// what the broker holds uses this.
func adjudicateArmCancel(ledgerState, signalID string, book []nt.NT8Order, haveBook bool) armCancelVerdict {
	return adjudicateArmCancelWith(ledgerState, signalID, book, haveBook, positionContext{})
}

// adjudicateArmCancelWith is the same adjudication with the broker's position
// truth supplied. PURE.
func adjudicateArmCancelWith(ledgerState, signalID string, book []nt.NT8Order, haveBook bool, pos positionContext) armCancelVerdict {
	if strings.TrimSpace(signalID) == "" {
		return armCancelVerdict{false, "the arm has no signal id — nothing was ever placed under it"}
	}
	// A CONFIRMED-FLAT ACCOUNT HAS NOTHING TO PROTECT.
	//
	// This sits ABOVE the ledger and book checks on purpose. Every refusal
	// below exists for one reason: cancelling might remove protection from a
	// LIVE position. When the caller has read the broker's own positions and
	// found none, that reason is gone — and the opposite risk takes over. Class
	// 27, 2026-08-31: a netting close left an arm's stop resting and it fired 26
	// minutes later, opening a naked short. Refusing here to protect a position
	// that does not exist is how that order stays alive.
	//
	// Only a caller that has actually established flatness sets this; the zero
	// value is "unknown", and unknown keeps every refusal below.
	if pos.Known && !pos.Open {
		return armCancelVerdict{true, "the broker reports FLAT — no position exists for these orders to be protecting, and a resting orphan stop can fire later and open a naked one (class 27)"}
	}
	// THE LEDGER'S OWN WORD, first and cheapest. A filled arm is never cancelled.
	normalizedState := strings.ToLower(strings.TrimSpace(ledgerState))
	if normalizedState == store.StateFilled {
		return armCancelVerdict{false, "the arm is FILLED — its entry is a position, and the only orders left under this signal are its protections"}
	}
	if store.IsTerminalArmState(ledgerState) || !store.IsKnownArmState(ledgerState) || normalizedState == store.StateCancelPending {
		return armCancelVerdict{false, "the arm is " + ledgerState + " — not a live order"}
	}
	// THE BOOK DECIDES. The ledger is a memory; at 23:37:02 it was a minute out
	// of date and that minute cost a stop.
	if !haveBook {
		return armCancelVerdict{false, "no broker book — the entry cannot be shown to be resting, and cancelling blind can take a live protective stop with it"}
	}
	resting, children := entryIsResting(book, signalID)
	if resting {
		return armCancelVerdict{true, "the entry is still resting at the broker"}
	}
	if children {
		// TWO OPPOSITE MEANINGS, ONE SHAPE (2026-09-07).
		//
		// Children with no entry means the entry is gone. Whether that is a
		// disaster or a mess depends entirely on whether a position exists:
		//
		//   OPEN → these ARE the protection. Cancelling them is 2026-09-06
		//          23:37:02: accepted stop 29554 and target 29623 withdrawn,
		//          position 592 naked for 8h19m.
		//   FLAT → these are ORPHANS. Class 27, 2026-08-31: a netting close
		//          left an arm's stop resting and it fired 26 minutes later,
		//          opening a NAKED SHORT. Leaving them alive is the bug.
		//
		// Refusing both cases would re-open class 27; allowing both is the
		// naked stop. So the caller that KNOWS is asked, and the caller that
		// does not know gets the non-destructive answer.
		if pos.Known && !pos.Open {
			return armCancelVerdict{true, "the entry is gone and the broker reports FLAT — these are ORPHAN protective orders with no position behind them; leaving them resting is class 27 (a stop that fires later and opens a naked position)"}
		}
		return armCancelVerdict{false, "the entry has FILLED — only its bracket children remain, and a cancel would take the protection with it (2026-09-06 23:37:02)"}
	}
	// NOTHING AT ALL under this signal. This is NOT the dangerous case and must
	// not be refused: there is no protection to lose, and refusing would strand
	// the ledger row `working` forever with no way to retire it. The guard
	// exists to protect a FILLED arm's children — not to block every cancel
	// whose target has already gone. (Caught by
	// TestShadowedRestingOrderCancelledAtBoot, which retires exactly such a row.)
	return armCancelVerdict{true, "nothing under this signal is working at the broker — the cancel is a harmless no-op and lets the ledger row retire"}
}

// cancelSignalIfSafe is the SIGNAL-level gate, for the paths that hold only a
// signal id (the stale reaper, the re-request pass). The ledger state is
// unknown there, so the BOOK alone decides — and the same rule holds: if the
// entry is not resting, the only things under this signal are protections.
//
// Returns true when the cancel was actually sent.
func (at *AutoTrader) cancelSignalIfSafe(send func(string) error, signalID, who string, now time.Time) bool {
	return at.cancelSignalIfSafeWith(send, signalID, who, now, positionContext{})
}

// cancelSignalIfSafeWith is the same gate for a caller that has already
// established what the broker holds — the class-27 desync sweep, which reaches
// its cancel loop only after reading the broker FLAT for every row it is about
// to sweep.
func (at *AutoTrader) cancelSignalIfSafeWith(send func(string) error, signalID, who string, now time.Time, pos positionContext) bool {
	book, have, _ := at.liveBook(now)
	v := adjudicateArmCancelWith(store.StateWorking, signalID, book, have, pos)
	if !v.Allow {
		at.logWarnf("🛟 cancel REFUSED (%s) signal=%s — %s", who, shortID(signalID), v.Why)
		return false
	}
	if err := send(signalID); err != nil {
		at.logWarnf("✕ cancel SEND failed (%s) signal=%s: %v", who, shortID(signalID), err)
	}
	return true
}

// cancelSafetyFor is the production entry point: it reads THIS trader's live
// book and adjudicates one row. now is the caller's clock (A28).
func (at *AutoTrader) cancelSafetyFor(r store.ArmedOrderDB, now time.Time) armCancelVerdict {
	book, have, _ := at.liveBook(now)
	return adjudicateArmCancel(r.State, r.SignalID, book, have)
}
