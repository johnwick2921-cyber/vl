package ninjatrader

import "strings"

// ── ONE ORDER-STATE VOCABULARY (2026-09-07, D3) ──────────────────────────────
//
// Before this file the tree classified broker order states in four unrelated
// places, each answering a narrower question than its callers assumed:
//
//   provider/ninjatrader/order_snapshot.go  a terminal SET, "not terminal" = working
//   trader/accepted_risk_hook.go            a dying SET, four spellings, inline
//   trader/armed_executor.go                a switch on two live spellings
//   ninjascript/VLTraderTCPClient.cs        `st != Working && st != Accepted`
//
// Two of them were wrong, and wrong in the direction that costs money.
//
//   (a) TriggerPending appeared NOWHERE in the tree — 0 occurrences across
//       every .go and .cs file, measured 2026-09-07. NinjaTrader holds such an
//       order on the LOCAL PC; it is not at the exchange and will not fire if
//       the machine is down. Falling through the terminal set, it read as an
//       ordinary live order, so a stop that existed only on this desktop
//       counted as protection.
//
//   (b) `unknown` sat in the terminal set beside `filled` and `cancelled`, so
//       an order whose state we could not READ was classified as an order that
//       no longer EXISTS. Owner ruling, 2026-09-07: "an unreadable state is not
//       history. UNKNOWN is non-terminal and takes no destructive branch; a
//       cancel or a reconciliation that meets it does nothing and logs."
//
// The distinction the rest of the tree needs is not two-valued. "Still on the
// books" (what a flat gate counts) and "actually protecting a position at the
// exchange" (what a reconciler needs) are different questions, and a single
// IsWorking() bool cannot answer both. Hence a graded classification with one
// chokepoint — ClassifyOrderState — and named predicates over it, so a caller
// states WHICH question it is asking.

// OrderLiveness grades how alive an order is. The zero value is LivenessUnknown
// on purpose: a state we failed to parse must never default into a confident
// answer.
type OrderLiveness int

const (
	// LivenessUnknown — the state is unreadable, unrecognised or absent. NOT
	// terminal: the order may well be standing. No destructive branch may be
	// taken on it (A24).
	LivenessUnknown OrderLiveness = iota

	// LivenessPending — created or sent, not yet confirmed by the broker.
	// Counts as standing (a fill can still arrive), but nothing here has been
	// AGREED to, so these terms are not an accepted risk.
	LivenessPending

	// LivenessLive — confirmed and standing AT THE EXCHANGE. `Accepted` is the
	// normal resting state of a stop-market; treating "not Working" as "not
	// live" was error (a) of the four above.
	LivenessLive

	// LivenessLocal — held on this PC, not at the exchange (TriggerPending).
	// Counts as standing, and NEVER as protection.
	LivenessLocal

	// LivenessDying — a cancel is in flight. Still standing (the cancel may
	// fail, and the order can still fill in that window), but on its way out,
	// so its prices are never recorded as accepted.
	LivenessDying

	// LivenessTerminal — history. The only class on which an order may be
	// treated as gone.
	LivenessTerminal
)

func (l OrderLiveness) String() string {
	switch l {
	case LivenessPending:
		return "pending"
	case LivenessLive:
		return "live"
	case LivenessLocal:
		return "local-only"
	case LivenessDying:
		return "dying"
	case LivenessTerminal:
		return "terminal"
	}
	return "unknown"
}

// orderStateLiveness maps every NinjaTrader OrderState we have observed or that
// NT8 documents. Keys are NORMALISED (see normalizeOrderState): lower-cased with
// separators stripped, so "CancelPending", "cancel_pending" and "cancel pending"
// are one key rather than three chances to miss one — which is how the dying
// set came to carry four spellings inline.
var orderStateLiveness = map[string]OrderLiveness{
	// created / in flight to the broker
	"initialized":     LivenessPending,
	"submitted":       LivenessPending,
	"changepending":   LivenessPending,
	"changesubmitted": LivenessPending,

	// confirmed and standing at the exchange
	"accepted":   LivenessLive,
	"working":    LivenessLive,
	"suspended":  LivenessLive,
	"partfilled": LivenessLive,
	"partial":    LivenessLive, // N3 P0: the AddOn emits "partial" (VLTraderTCPClient.cs:1545)

	// held on this PC — NOT at the exchange
	"triggerpending": LivenessLocal,

	// a cancel is in flight; can still fill
	"cancelpending":   LivenessDying,
	"cancelsubmitted": LivenessDying,

	// history
	"filled":         LivenessTerminal,
	"cancelled":      LivenessTerminal,
	"canceled":       LivenessTerminal,
	"rejected":       LivenessTerminal,
	"expired":        LivenessTerminal,
	"partfilleddone": LivenessTerminal,

	// explicitly NOT terminal — owner ruling 2026-09-07
	"unknown": LivenessUnknown,
}

// normalizeOrderState is the single canonicalizer for a broker state string,
// applied where the value ENTERS a classification (checklist 28).
func normalizeOrderState(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// ClassifyOrderState is the ONE place a broker order state becomes a judgement.
// An unrecognised string is LivenessUnknown — never a confident answer.
func ClassifyOrderState(raw string) OrderLiveness {
	if l, ok := orderStateLiveness[normalizeOrderState(raw)]; ok {
		return l
	}
	return LivenessUnknown
}

// Liveness classifies this order's state.
func (o NT8Order) Liveness() OrderLiveness { return ClassifyOrderState(o.State) }

// IsWorking reports whether the order still stands at the broker — the
// conservative question a flat gate asks. Everything that is not provably
// history counts, INCLUDING an unreadable state.
func (o NT8Order) IsWorking() bool { return o.Liveness() != LivenessTerminal }

// IsLiveAtExchange reports whether this order is confirmed and standing AT THE
// EXCHANGE — the question "is this position actually protected" asks. A
// TriggerPending stop held on the PC is not, a dying order is not, and an
// unreadable one is not (it is not a NO either; callers must check
// IsStateReadable before treating a false as "no protection exists").
func (o NT8Order) IsLiveAtExchange() bool { return o.Liveness() == LivenessLive }

// IsCancelInFlight reports whether the broker is already withdrawing this
// order. Measured across the live book: CancelSubmitted and CancelPending are
// the two states NT8 uses (130 and 22 occurrences across 360 frames).
func (o NT8Order) IsCancelInFlight() bool { return o.Liveness() == LivenessDying }

// IsHeldLocally reports whether NT8 is holding this order on this PC rather
// than at the exchange.
func (o NT8Order) IsHeldLocally() bool { return o.Liveness() == LivenessLocal }

// IsStateReadable reports whether we understood the state at all. A false here
// is the A24 case: log, count, and take no destructive branch.
func (o NT8Order) IsStateReadable() bool { return o.Liveness() != LivenessUnknown }

// Terminal broker-order OUTCOMES (N7 part 5, CTO release #4). One table in the
// broker-state package: a terminal order either FILLED (a position exists) or
// left UNFILLED (cancelled / rejected / expired). Callers ask the order, never
// re-type the state names.
const (
	OrderOutcomeFilled   = "filled"
	OrderOutcomeUnfilled = "unfilled"
)

var orderTerminalOutcome = map[string]string{
	"filled":         OrderOutcomeFilled,
	"partfilleddone": OrderOutcomeFilled,
	"cancelled":      OrderOutcomeUnfilled,
	"canceled":       OrderOutcomeUnfilled,
	"rejected":       OrderOutcomeUnfilled,
	"expired":        OrderOutcomeUnfilled,
}

// TerminalOutcome is "filled", "unfilled", or "" for a non-terminal/unknown state.
func (o NT8Order) TerminalOutcome() string { return orderTerminalOutcome[normalizeOrderState(o.State)] }
