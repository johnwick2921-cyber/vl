package trader

import (
	"strings"

	"vl/store"
)

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

// mentorFillIsFull (N3 P0, 2026-10-04) reports whether an order_update is a FULL
// fill — terminal — given the row's signed contract count and the CUMULATIVE
// fill quantity (e.Filled). The AddOn emits "partial" for a part-fill
// (VLTraderTCPClient.cs:1545) and "filled" only when complete; a
// "partfilled"/"partial" state with quantity >= total is treated full
// (defensive — it can never be wider than the signed count).
func mentorFillIsFull(state string, quantity, total int) bool {
	isPart := strings.EqualFold(state, "partfilled") || strings.EqualFold(state, "partial")
	if !isPart {
		return true
	}
	return quantity >= total
}

// mentorRemainderToCancel (N3 P1, 2026-10-04) returns the unfilled remainder of
// a partially filled working row — the contracts to cancel at expiry — or 0 when
// there is nothing to cancel (no signed count, no fill, or already full).
func mentorRemainderToCancel(r store.ArmedOrderDB) int {
	if r.Contracts == nil || *r.Contracts <= 0 {
		return 0
	}
	if r.FillQuantity <= 0 || r.FillQuantity >= *r.Contracts {
		return 0
	}
	return *r.Contracts - r.FillQuantity
}
