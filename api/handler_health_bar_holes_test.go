package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	ntTrader "vl/trader/ninjatrader"
)

// GAP-BACKFILL (2026-10-14) — /api/health serves the open bar holes as READ
// values: [] when the tracker is wired and none is open, ABSENT when no tracker
// is wired (nothing is known). Pinned at the production handler.
func TestHealthServesBarHolesOnlyWhenKnown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	get := func() map[string]json.RawMessage {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		(&Server{}).handleHealth(c)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("health body not JSON: %v", err)
		}
		return body
	}

	if b := get(); b["bars_holes"] != nil || b["bars_hole_counts"] != nil || b["bars_hole_audit"] != nil {
		t.Fatalf("no tracker wired: bars_holes must be ABSENT, not fabricated, got %s / %s", b["bars_holes"], b["bars_hole_counts"])
	}

	restore := ntTrader.SetBarHolesWiredForTest()
	defer restore()
	b := get()
	if string(b["bars_holes"]) != "[]" {
		t.Fatalf("wired, none open: bars_holes must be [], got %q", b["bars_holes"])
	}
	var counts map[string]json.RawMessage
	if err := json.Unmarshal(b["bars_hole_counts"], &counts); err != nil || counts["bars_hole_detected"] == nil || counts["bars_hole_unfilled"] == nil {
		t.Fatalf("the refill counters must ride with the holes: %s (%v)", b["bars_hole_counts"], err)
	}
	// m444: the persistent gap audit's block rides with the holes — enabled,
	// 7 sessions, and the 7-day unfilled figure as a READ number (0), not absent.
	var audit struct {
		Enabled         bool `json:"enabled"`
		WindowSessions  int  `json:"window_sessions"`
		Passes          int  `json:"passes"`
		UnfilledHoles7d *int `json:"unfilled_holes_7d"`
	}
	if err := json.Unmarshal(b["bars_hole_audit"], &audit); err != nil || !audit.Enabled || audit.WindowSessions != 7 || audit.UnfilledHoles7d == nil || *audit.UnfilledHoles7d != 0 {
		t.Fatalf("bars_hole_audit must be served with the holes: %s (%v)", b["bars_hole_audit"], err)
	}
}
