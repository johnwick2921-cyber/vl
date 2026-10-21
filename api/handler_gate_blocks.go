// B6 — gate-block counters endpoint.
//
// Exposes the per-trader, per-CME-session-day tally of how many times each named
// risk/safety gate blocked an entry or decision cycle today (feed_down, dead_man,
// consecutive_loss, price_sanity, stale_data, the Strategy-Studio guardrails, the
// B3 order guard, …). This is the queryable companion to the global Prometheus
// RiskGateTrips counter and the source of the daily journal summary line.
//
//	GET /api/risk/gate-blocks?trader_id=<id>
//	Returns: {"session_day_utc":"<RFC3339>","summary":"<one-line>",
//	          "by_trader":{"<trader_id>":{"<gate>":<count>}}}
//
// The empty-string trader key ("") holds process-wide gates that fire at a shared
// chokepoint with no per-trader identity (the B3 order guard).
//
// C5 (README §9) — ?trader_id= scopes the response to that trader plus the
// process-wide "" key, so the panel no longer ships every trader's tally.

package api

import (
	"net/http"
	"time"

	"vl/telemetry"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleGateBlocks(c *gin.Context) {
	dayMs, table := telemetry.GateBlockSnapshot()
	sessionDayUTC := ""
	if dayMs > 0 {
		sessionDayUTC = time.UnixMilli(dayMs).UTC().Format(time.RFC3339)
	}
	if table == nil {
		table = map[string]map[string]int{}
	}
	if tid := c.Query("trader_id"); tid != "" {
		scoped := map[string]map[string]int{}
		if v, ok := table[tid]; ok {
			scoped[tid] = v
		}
		if v, ok := table[""]; ok {
			scoped[""] = v
		}
		table = scoped
	}
	c.JSON(http.StatusOK, gin.H{
		"session_day_utc": sessionDayUTC,
		"summary":         telemetry.GateBlockDailySummary(),
		"by_trader":       table,
		// STALLED-SETUP ALARM (release #22): the per-trader per-mentor-setup-kind
		// seen-vs-placed tally (emitted vs reached the ledger), same session-day
		// rollover. Read-only — the alert is the writer, never the API.
		"setup_stalls": telemetrySetupStallTable(c),
	})
}

// telemetrySetupStallTable renders the stalled-setup snapshot scoped the same
// way as the gate-block table: full when no trader_id is given, else that
// trader only. An empty table is [] (computed empty, never nil).
func telemetrySetupStallTable(c *gin.Context) map[string]map[string]telemetry.SetupStallCount {
	_, table := telemetry.SetupStallSnapshot()
	if table == nil {
		table = map[string]map[string]telemetry.SetupStallCount{}
	}
	if tid := c.Query("trader_id"); tid != "" {
		scoped := map[string]map[string]telemetry.SetupStallCount{}
		if v, ok := table[tid]; ok {
			scoped[tid] = v
		}
		table = scoped
	}
	return table
}

// P0-cleanup (2026-08-19) — the ONE error surface: structured error events
// (type / cause / cost) per trader per session-day, plus the daily summary
// line. Mirrors the gate-blocks endpoint.
//
//	GET /api/risk/errors
//	Returns: {"rows":[{trader,type,cause,cost,count,decisions_lost,trades_lost}],
//	          "summary":"errors today: N (types: …), decisions lost: N"}
func (s *Server) handleErrors(c *gin.Context) {
	trader := c.Query("trader_id")
	rows := telemetry.ErrorSummary(trader)
	if rows == nil {
		rows = []telemetry.ErrorEventRow{}
	}
	c.JSON(http.StatusOK, gin.H{
		"rows":    rows,
		"summary": telemetry.ErrorDigestLine(trader),
	})
}
