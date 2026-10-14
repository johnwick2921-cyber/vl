// Plan 4 Task 23 — risk handler tests.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vl/discipline"
	"vl/kernel"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockSignaler implements kernel.ForceFlatSignaler for testing.
type mockSignaler struct {
	called   bool
	traderID string
	err      error
}

func (m *mockSignaler) ForceFlat(traderID string) error {
	m.called = true
	m.traderID = traderID
	return m.err
}

// Tests that the adapter satisfies the kernel interface at compile time.
func TestForceFlatSignalAdapterSatisfiesInterface(t *testing.T) {
	var _ kernel.ForceFlatSignaler = (*forceFlatSignalAdapter)(nil)
}

// Tests that the adapter's ForceFlat increments the flattened counter.
func TestForceFlatSignalAdapterIncrementsCounter(t *testing.T) {
	a := &forceFlatSignalAdapter{writer: nil}
	if err := a.ForceFlat("trader-1"); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if a.flattened != 1 {
		t.Fatalf("expected flattened=1, got %d", a.flattened)
	}
}

// Tests that the response JSON shape matches the documented schema.
func TestForceFlatResponseShape(t *testing.T) {
	zero := 0
	resp := forceFlatResponse{
		Triggered:          true,
		TraderID:           "trader-1",
		PositionsFlattened: &zero,
		TimestampUTC:       "2026-05-26T00:00:00Z",
		LogMessage:         "ok",
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{`"triggered"`, `"trader_id"`, `"positions_flattened"`, `"timestamp_utc"`, `"log_message"`} {
		if !strings.Contains(s, key) {
			t.Errorf("response missing key %s in %s", key, s)
		}
	}
}

// Tests that the risk status response JSON shape matches the documented schema.
func TestRiskStatusResponseShape(t *testing.T) {
	resp := riskStatusResponse{
		TraderID:            "trader-1",
		DailyPnLUSD:         -100.0,
		DailyLossLimitUSD:   500.0,
		ConcurrentTrades:    1,
		MaxConcurrentTrades: 2,
		CurrentNotionalUSD:  10_000.0,
		MaxNotionalUSD:      50_000.0,
		KillSwitchArmed:     true,
		LastResetUTC:        "2026-05-26T00:00:00Z",
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{
		`"trader_id"`,
		`"daily_pnl_usd"`,
		`"daily_loss_limit_usd"`,
		`"concurrent_trades"`,
		`"max_concurrent_trades"`,
		`"current_notional_usd"`,
		`"max_notional_usd"`,
		`"kill_switch_armed"`,
		`"last_reset_utc"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("response missing key %s in %s", key, s)
		}
	}
}

// Tests that the force-flat endpoint returns 400 when trader_id is missing.
func TestHandleForceFlatRequiresTraderID(t *testing.T) {
	s := &Server{}
	router := gin.New()
	router.POST("/api/risk/force-flat", s.handleForceFlat)

	req := httptest.NewRequest(http.MethodPost, "/api/risk/force-flat", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing trader_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Tests that the risk status endpoint returns 400 when trader_id is missing.
func TestHandleRiskStatusRequiresTraderID(t *testing.T) {
	s := &Server{}
	router := gin.New()
	router.GET("/api/risk/status", s.handleRiskStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/risk/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing trader_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Tests that the mock signaler captures the call (covers ForceFlatSignaler contract).
func TestKernelMaybeForceFlatInvokesSignaler(t *testing.T) {
	m := &mockSignaler{}
	if err := kernel.MaybeForceFlat("trader-xyz", m); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !m.called {
		t.Fatal("signaler.ForceFlat was not invoked")
	}
	if m.traderID != "trader-xyz" {
		t.Fatalf("expected traderID=trader-xyz, got %q", m.traderID)
	}
}

// Tests that the audit decisions endpoint requires trader_id.
func TestHandleDecisionAuditRequiresTraderID(t *testing.T) {
	s := &Server{}
	router := gin.New()
	router.GET("/api/audit/decisions", s.handleDecisionAudit)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/decisions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing trader_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Tests that the audit decisions endpoint rejects invalid `since` parameter.
func TestHandleDecisionAuditInvalidSince(t *testing.T) {
	s := &Server{}
	router := gin.New()
	router.GET("/api/audit/decisions", s.handleDecisionAudit)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/decisions?trader_id=t1&since=garbage", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid since, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// A4 (G4) — the freeze list + owner clear endpoints: a frozen trader appears in
// GET /risk/freezes and POST /risk/clear-freeze resumes it.
func TestHandleFreezeListAndClear(t *testing.T) {
	discipline.ResetFreezeForTest()
	defer discipline.ResetFreezeForTest()
	discipline.FreezeTrader("trader-frozen", "echo mismatch on fill", 1000)

	s := &Server{}
	router := gin.New()
	router.GET("/api/risk/freezes", s.handleListFreezes)
	router.POST("/api/risk/clear-freeze", s.handleClearFreeze)

	// List shows the frozen trader.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/risk/freezes", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "trader-frozen") {
		t.Fatalf("freezes list must include the frozen trader; code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Clear without trader_id → 400.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/risk/clear-freeze", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clear without trader_id must be 400, got %d", rec.Code)
	}

	// Clear with trader_id → resumes.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/risk/clear-freeze?trader_id=trader-frozen", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cleared":true`) {
		t.Fatalf("clear must report cleared:true; code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, frozen := discipline.IsFrozen("trader-frozen"); frozen {
		t.Fatal("trader must resume (not frozen) after clear")
	}
}
