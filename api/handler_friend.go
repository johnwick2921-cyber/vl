package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// friendSnapshotMaxAge is the R9c staleness bound for the NT8 position + order
// snapshots (120 s — there is no older snapshot-age knob to reuse).
const friendSnapshotMaxAge = 120 * time.Second

// friendStateKnown (W-FRIEND-HOSTED R9c) is implemented by the NT TCPTrader: the
// NT8 link is connected and the position + order snapshots are recent. A trader
// that does not implement it (non-NT) reads KNOWN — the same DEFAULT-ALLOW the
// dead-man watchdog uses, and irrelevant here because every friend trader is NT.
type friendStateKnown interface {
	FriendStateKnown(now time.Time, maxAge time.Duration) bool
}

// W-FRIEND-HOSTED (CTO ruling 2026-10-18 07:36 + R9 07:42 + F1 09:03):
// GET /api/friend/flat is the ONE friend-facing probe DS-105's updater may use
// to decide a restart. flat=true ONLY when the instance has at least one trader
// AND every trader has (a) no open position, (b) no working/pending entry or
// resting bracket order, and (c) a KNOWN + FRESH position state (an NT trader
// that answers FriendStateKnown=true). Anything else — zero traders, no NT8, a
// position, an order, an error, disconnected, stale, UNKNOWN — reads false.
func (s *Server) handleFriendFlat(c *gin.Context) {
	traders := s.traderManager.GetAllTraders()
	// F1: an empty instance (no traders, no NT8) is NOT flat — never a
	// fabricated all-clear that would let the updater restart a half-built bot.
	flat := len(traders) > 0
	now := time.Now()
	for _, at := range traders {
		positions, err := at.GetPositions()
		if err != nil {
			// UNKNOWN / stale / disconnected position state = NOT flat
			// (GetPositions errors when there is no fresh NT8 snapshot and no
			// confirmed entry fill — never a fabricated flat book).
			flat = false
			break
		}
		if !flatFromPositions(positions) {
			flat = false
			break
		}
		orders, err := at.GetOpenOrders("")
		if err != nil {
			// An unreadable order book is also UNKNOWN → not flat.
			flat = false
			break
		}
		if len(orders) > 0 {
			// A working/pending entry or resting bracket → not flat (R9 b).
			flat = false
			break
		}
		k, ok := at.GetUnderlyingTrader().(friendStateKnown)
		if !ok || !k.FriendStateKnown(now, friendSnapshotMaxAge) {
			// NT disconnected / snapshots stale / a trader that cannot answer
			// → not flat (R9 c). Every trader must be known+fresh.
			flat = false
			break
		}
	}
	// R11: the recorded trader count lets DS-105 restart a zero-trader instance
	// (safe — nothing manages a position) without touching "flat" itself.
	c.JSON(http.StatusOK, gin.H{"flat": flat, "traders": len(traders), "at": now.UnixMilli()})
}

// flatFromPositions reports flat when no position carries a non-zero quantity.
// A position map missing "quantity" cannot prove non-flat, so it reads flat —
// the fail-closed side is the GetPositions error, handled by the caller.
func flatFromPositions(positions []map[string]interface{}) bool {
	for _, p := range positions {
		if qty, ok := p["quantity"].(float64); ok && qty != 0 {
			return false
		}
	}
	return true
}
