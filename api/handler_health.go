package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"vl/kernel"
)

// handleHealth (P2-6) reports REAL state, not a liveness constant: DB ping,
// NT8 feed status + last-bar age per running trader, running-trader count, and
// a non-null time. 200 while the API can answer; 503 only when the DATABASE is
// dead (the API cannot serve anything real without it). NT8 down is 200 with
// status "degraded" — cutover scripts read `revision` from this endpoint and
// must keep working while the AddOn is restarting.
func (s *Server) handleHealth(c *gin.Context) {
	now := time.Now()

	dbStatus := "ok"
	if s.store == nil || s.store.DB() == nil {
		dbStatus = "unavailable"
	} else if err := s.store.DB().Ping(); err != nil {
		dbStatus = "dead"
	}

	type nt8Info struct {
		Link         string `json:"link"`
		FeedStatus   string `json:"feed_status"`
		LastBarAgeMs *int64 `json:"last_bar_age_ms"` // absent = no bar available
	}
	nt8 := map[string]nt8Info{}
	running := 0
	if s.traderManager != nil {
		for id, at := range s.traderManager.GetAllTraders() {
			if !at.HealthIsRunning() {
				continue
			}
			running++
			info := nt8Info{Link: "unknown", FeedStatus: "UNKNOWN (no feed_status received)"}
			// P-D stale link latch: the link field follows the REAL TCP socket
			// (HealthLinkConnected), never the latched edge-triggered
			// feed_status — the last feed_status frame can outlive the socket
			// by hours and must not read as "up". The latched string is still
			// surfaced as the last-known feed status.
			if connected, ok := at.HealthLinkConnected(); ok {
				if connected {
					info.Link = "up"
				} else {
					info.Link = "down"
				}
			}
			if st := at.HealthFeedStatus(); st != "" {
				info.FeedStatus = st
			}
			if ageMs, ok := at.HealthLastBarAgeMs(now); ok {
				info.LastBarAgeMs = &ageMs
			}
			nt8[id] = info
		}
	}

	status := "ok"
	code := http.StatusOK
	switch dbStatus {
	case "dead":
		status = "error"
		code = http.StatusServiceUnavailable
	case "unavailable":
		status = "degraded"
	}
	if code == http.StatusOK {
		for _, info := range nt8 {
			if info.Link != "up" {
				status = "degraded"
				break
			}
		}
	}

	c.JSON(code, gin.H{
		"status":          status,
		"time":            now.UTC().Format(time.RFC3339),
		"revision":        kernel.RunningRevision(),
		"db":              dbStatus,
		"nt8":             nt8,
		"traders_running": running,
		// Release #22 OPS-SAFETY: the bot's own wall-vs-monotonic clock-step
		// counters (backward/forward, largest, last, whether NEW entries are
		// held right now). Read values; last_* absent until a step is seen.
		"clock": kernel.ClockStepSnapshot(),
	})
}
