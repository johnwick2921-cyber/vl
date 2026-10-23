package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"vl/config"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
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

	// deadManHealthInfo is the per-trader /api/health `dead_man` field (W26).
	type deadManHealthInfo struct {
		Blocked bool  `json:"blocked"`
		SinceMs int64 `json:"since_ms"`
		Probes  int   `json:"probes"`
	}
	type nt8Info struct {
		Link       string `json:"link"`
		FeedStatus string `json:"feed_status"`
		// FeedMode is the broker market-data mode from the feed_status frame:
		// "realtime" | "delayed" | "unknown". "" is projected to "unknown" —
		// a missing mode is NEVER reported as realtime (P0 feed freshness).
		FeedMode     string `json:"feed_mode"`
		LastBarAgeMs *int64 `json:"last_bar_age_ms"` // absent = no bar available
		// W26 deadman-escalation: entries blocked by the dead-man reconcile gate
		// and the current blocked episode. READ values; since_ms = 0 when not
		// blocked; probes = consecutive failed reconcile probes in this episode.
		DeadMan *deadManHealthInfo `json:"dead_man,omitempty"`
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
			// P0 feed freshness — broker mode, never fabricated realtime.
			info.FeedMode = "unknown"
			if mode := at.HealthFeedMode(); mode == "realtime" || mode == "delayed" {
				info.FeedMode = mode
			}
			if ageMs, ok := at.HealthLastBarAgeMs(now); ok {
				info.LastBarAgeMs = &ageMs
			}
			if blocked, sinceMs, probes := at.HealthDeadMan(); blocked {
				info.DeadMan = &deadManHealthInfo{Blocked: true, SinceMs: sinceMs, Probes: probes}
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

	body := gin.H{
		"status":          status,
		"time":            now.UTC().Format(time.RFC3339),
		"revision":        kernel.RunningRevision(),
		"db":              dbStatus,
		"nt8":             nt8,
		"traders_running": running,
		// m386: the Friends page flag for the web (nav + route). READ value; the
		// web hides the Friends surface when false. The owner API 404s the same way.
		"friends_page": config.FriendsPageEnabled(),
		// Release #22 OPS-SAFETY: the bot's own wall-vs-monotonic clock-step
		// counters (backward/forward, largest, last, whether NEW entries are
		// held right now). Read values; last_* absent until a step is seen.
		"clock": kernel.ClockStepSnapshot(),
	}
	// GAP-BACKFILL (2026-10-14): the open 1m bar holes the bot is refilling
	// from NT8 by itself, and the refill counters. READ values: [] = the
	// tracker is wired and no hole is open; ABSENT = no tracker (no store), so
	// nothing is known — never a fabricated empty list (A24).
	if holes, ok := ntTrader.BarHolesSnapshot(); ok {
		body["bars_holes"] = holes
		body["bars_hole_counts"] = telemetry.BarsHoleCountsSnapshot()
	}
	// WAVE 26 — the stale-print guard's lifetime count, beside the hole counts:
	// how many live bars were quarantined (labelled mixed) after a reconnect
	// because their close was impossible against the neighbouring minutes.
	body["bars_stale_prints"] = ntwire.StalePrintQuarantined()
	// K1/K7 — the connect-key refusal counter, named as the protocol doc names
	// it (nt_connect_key_refused). 0 when no key is required or no server runs.
	body["nt_connect_key_refused"] = ntTrader.ConnectKeyRefusedCount()
	c.JSON(code, body)
}
