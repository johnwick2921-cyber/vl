package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/manager"
	"vl/store"
	"vl/trader"
)

func pepTruthServer(t *testing.T, cfg *store.StrategyConfig) (*Server, *gin.Engine, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "pt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u1", "t1", "pep-trader", "Sim101")
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", nil, false)
	at.SetStrategyConfigForTest(cfg)
	at.SetStoreForTest(st)
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := &Server{store: st, traderManager: tm}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", c.GetHeader("X-User")); c.Next() })
	r.GET("/api/traders/:id/pep-truth", s.handlePepTruth)
	return s, r, st
}

func getPepTruth(r *gin.Engine, user, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/traders/"+id+"/pep-truth", nil)
	req.Header.Set("X-User", user)
	r.ServeHTTP(rec, req)
	return rec
}

// The dashboard card's endpoint: the resolved style/contracts/flat time, the
// durable day count and the mode, read through the production handler.
// MUTANT: handlePepTruth answering from a typed copy of the defaults (not the
// resolver) -> contracts 3, not 6 -> RED.
func TestPepTruthEndpointReadsTheRunningSettings(t *testing.T) {
	on, six := true, 6
	_, r, st := pepTruthServer(t, &store.StrategyConfig{Pep: &store.PepConfig{Enabled: &on, Style: "E", Contracts: &six}})
	if _, err := st.PepDay().ClaimEntry(store.PepDayDB{TraderID: "t1", Day: store.PepDayOf(time.Now()), Style: "E", Side: "long", Symbol: "MNQ", Contracts: 6}, 1, 1); err != nil {
		t.Fatal(err)
	}
	rec := getPepTruth(r, "u1", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != true || got["mode"] != "pep" || got["style"] != "E" || got["contracts"] != float64(6) || got["flat_time_ct"] != "15:00" {
		t.Fatalf("truth %v", got)
	}
	if got["trades_today"] != float64(1) {
		t.Fatalf("trades_today = %v, want the durable count 1: %v", got["trades_today"], got)
	}
}

// pep OFF: the card hides itself and reports the mode; a conflict is told by
// name and the trader is NOT painted as pep. Another user's trader is refused.
func TestPepTruthEndpointOffConflictAndOwnership(t *testing.T) {
	_, r, _ := pepTruthServer(t, &store.StrategyConfig{})
	rec := getPepTruth(r, "u1", "t1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) || !strings.Contains(rec.Body.String(), `"mode":"ai"`) {
		t.Fatalf("off: %d %s", rec.Code, rec.Body.String())
	}
	if rec := getPepTruth(r, "someone-else", "t1"); rec.Code == http.StatusOK {
		t.Fatalf("another user must not read the trader's card: %d", rec.Code)
	}

	on := true
	cfg := &store.StrategyConfig{Pep: &store.PepConfig{Enabled: &on}}
	cfg.RiskControl.MentorMode = true
	_, r2, _ := pepTruthServer(t, cfg)
	rec = getPepTruth(r2, "u1", "t1")
	var got struct {
		Enabled  bool   `json:"enabled"`
		Mode     string `json:"mode"`
		Conflict *struct {
			Code string `json:"code"`
		} `json:"conflict"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Enabled || got.Mode != "conflict" || got.Conflict == nil || got.Conflict.Code != store.PepConflictMentor {
		t.Fatalf("conflict card: %s", rec.Body.String())
	}
}

// /api/my-traders and /api/traders/:id/config carry the mode for the dashboard
// chip (additive fields).
func TestTradingModeFieldsForTheDashboardChip(t *testing.T) {
	on := true
	mode, style := tradingModeFields(&store.StrategyConfig{Pep: &store.PepConfig{Enabled: &on, Style: "e"}})
	if mode != "pep" || style != "E" {
		t.Fatalf("pep chip = %q %q, want pep / E (canonical casing)", mode, style)
	}
	m := &store.StrategyConfig{}
	m.RiskControl.MentorMode = true
	if mode, style = tradingModeFields(m); mode != "mentor" || style != "" {
		t.Fatalf("mentor chip = %q %q", mode, style)
	}
	if mode, style = tradingModeFields(&store.StrategyConfig{}); mode != "ai" || style != "" {
		t.Fatalf("ai chip = %q %q", mode, style)
	}
}
