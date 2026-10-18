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

// W-FRIEND-HOSTED (CTO ruling 2026-10-18 07:36 + R9 07:42): GET /api/friend/flat
// is the ONE friend-facing probe DS-105's updater may use to decide a restart.
// flat=true ONLY when every trader in this friend instance has (a) no open
// position, (b) no working/pending entry or resting bracket order, and (c) a
// KNOWN + FRESH position state (NT connected, snapshots recent). Anything else —
// a position, an order, an error, disconnected, stale, UNKNOWN — reads false.
func (s *Server) handleFriendFlat(c *gin.Context) {
	flat := true
	now := time.Now()
	for _, at := range s.traderManager.GetAllTraders() {
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
		if k, ok := at.GetUnderlyingTrader().(friendStateKnown); ok && !k.FriendStateKnown(now, friendSnapshotMaxAge) {
			// NT disconnected or the snapshots are stale → not flat (R9 c).
			flat = false
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"flat": flat, "at": now.UnixMilli()})
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
