package trader

import (
	"os"
	"strings"

	"vl/store"
)

// stopLimitEntriesEnabled reads the mentor stop-limit knob (PR B, 2026-10-03).
// Default OFF — L4: additive and fail-closed. With the knob ON, an adjudicated
// stop-entry arm routes through PlaceStopEntryWithLimit: the AddOn builds
// OrderType.StopLimit (LimitPrice == StopPrice) instead of StopMarket, so the
// entry fills at its price or misses (D1.4 p1 @24:41, p2 @00:00); the order's
// expiry (expiry_ms) is authored by the evaluator's intent (DS-102) and the
// armed pass cancels it when it lapses unfilled (N12). Go sets the frame flag
// only when the far side proves MinAddonBuildStopLimit.
func stopLimitEntriesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MENTOR_STOP_LIMIT"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// isMentorArmOrigin (REVIEW-313 F3) reports whether the arm carries the
// mentor origin. The routing reads THIS, never the expiry as a proxy for
// "mentor arm": a mentor arm that lost its expiry must be refused, and a
// non-mentor arm with a stray expiry must keep today's path.
func isMentorArmOrigin(r store.ArmedOrderDB) bool {
	return strings.EqualFold(strings.TrimSpace(r.Origin), store.ArmOriginMentor)
}

// armExpired is PURE (N12, PR B 2026-10-03): an order with a stored expiry is
// due when now >= expiry_ms and the order is UNFILLED AND LIVE — armed,
// place_pending, or working with zero filled quantity (a stop-limit resting at
// NT8 IS a working, unfilled order; it is exactly the order the expiry exists
// to cancel). A working order with a partial fill is a trade in progress and
// is never due (the position logic owns it); cancel_pending, terminal and
// expiry-less rows are never due — 0 means no expiry was ever authored, and
// this code never sweeps what no intent expired.
//
// REVIEW-313 F5: an UNKNOWN state is NEVER due. The store's own law is that
// lifecycle actions retain their refusal of unknown states (arm_state.go
// IsKnownArmState); the 6beb984a7 fall-through made any unknown, empty or
// odd-cased value due, which cancelled "WORKING" partial fills and "bogus"
// rows. Canonical predicates + single-name, case-folded comparisons — the
// arm-state lint forbids re-typed state sets outside store/.
func armExpired(r store.ArmedOrderDB, nowMs int64) bool {
	if r.ExpiryMs <= 0 || nowMs < r.ExpiryMs {
		return false
	}
	if !store.IsKnownArmState(r.State) || store.IsTerminalArmState(r.State) {
		return false
	}
	s := strings.TrimSpace(r.State)
	if strings.EqualFold(s, store.StateCancelPending) {
		return false
	}
	if strings.EqualFold(s, store.StateWorking) {
		return r.FillQuantity == 0
	}
	// armed / place_pending: the only other known non-terminal states.
	return true
}
