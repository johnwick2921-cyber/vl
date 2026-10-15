package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/kernel"
	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// TestHealthReportsRealState pins P2-6 at the production call site
// (handleHealth): time must be a real RFC3339 timestamp (never null), db must
// be probed, and a dead database must answer 503 — not 200.
func TestHealthReportsRealState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// No store: degraded (the DB leg is unavailable) but still 200 with a real
	// time and the revision cutover scripts read.
	srv := &Server{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv.handleHealth(c)

	if w.Code != http.StatusOK {
		t.Fatalf("no-store health = %d, want 200 (degraded)", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	if ts, ok := body["time"].(string); !ok || ts == "" {
		t.Fatalf("health time must be a real timestamp, got %v", body["time"])
	} else if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("health time not RFC3339: %q", ts)
	}
	if body["revision"] != kernel.RunningRevision() {
		t.Fatalf("revision must stay %q for cutover scripts, got %v", kernel.RunningRevision(), body["revision"])
	}
	if body["db"] != "unavailable" || body["status"] != "degraded" {
		t.Fatalf("no-store health must read db=unavailable status=degraded, got %+v", body)
	}
	if tr, ok := body["traders_running"].(float64); !ok || tr != 0 {
		t.Fatalf("traders_running must be 0, got %v", body["traders_running"])
	}

	// A CLOSED database must flip the endpoint to 503 — the old handler
	// answered 200 to a dead DB.
	st, err := store.New(filepath.Join(t.TempDir(), "dead.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	st.Close()
	srv2 := &Server{store: st}
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv2.handleHealth(c2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("dead-DB health = %d, want 503", w2.Code)
	}
}

// TestHealthReportsDeadManBlock — W26 CTO gate F1 supplement: the /api/health
// `nt8.<id>.dead_man` field must carry the live block at the production call
// site (handleHealth reads at.HealthDeadMan()). A running trader with a blocked
// dead-man episode must surface {blocked, since_ms, probes}; a running trader
// that is NOT blocked must omit the field.
func TestHealthReportsDeadManBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "dm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	blocked := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, true)
	blocked.SetDeadManBlockedForTest(42_000, 3)

	clear := trader.NewAutoTraderOnBrokerForTest("t2", "ninjatrader", tcpT, true)

	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", blocked)
	tm.SetTraderForTest("t2", clear)
	s := &Server{store: st, traderManager: tm}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	s.handleHealth(c)

	if w.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	nt8, ok := body["nt8"].(map[string]any)
	if !ok {
		t.Fatalf("health nt8 must be a map, got %T %v", body["nt8"], body["nt8"])
	}
	// Blocked trader: the dead_man field is present with the episode's READ values.
	t1, ok := nt8["t1"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t1 missing, got %+v", nt8)
	}
	dm, ok := t1["dead_man"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t1.dead_man must be present when blocked, got %+v", t1)
	}
	if dm["blocked"] != true {
		t.Fatalf("dead_man.blocked must be true, got %v", dm["blocked"])
	}
	if dm["since_ms"] != float64(42_000) {
		t.Fatalf("dead_man.since_ms must be 42000, got %v", dm["since_ms"])
	}
	if dm["probes"] != float64(3) {
		t.Fatalf("dead_man.probes must be 3, got %v", dm["probes"])
	}
	// Not blocked: omitempty — the field must be absent, never a fabricated false.
	t2, ok := nt8["t2"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t2 missing, got %+v", nt8)
	}
	if _, present := t2["dead_man"]; present {
		t.Fatalf("nt8.t2.dead_man must be ABSENT when not blocked, got %+v", t2)
	}
}
