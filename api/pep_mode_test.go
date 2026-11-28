package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"vl/store"
)

// PEP — through the production handlers (create POST + edit PUT), the pep block
// survives a save and an unrelated edit, a trader is exactly one of
// {AI/planner, mentor, pep}, and a clamped number is returned as a warning.

type pepAPIErr struct {
	Error    string `json:"error"`
	ErrorKey string `json:"error_key"`
}

func pepReadCfg(t *testing.T, s *Server, id string) (store.StrategyConfig, string) {
	t.Helper()
	got, err := s.store.Strategy().Get("u1", id)
	if err != nil || got == nil {
		t.Fatalf("read strategy: %v", err)
	}
	var cfg store.StrategyConfig
	if err := json.Unmarshal([]byte(got.Config), &cfg); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	return cfg, got.Config
}

func TestPepBlockSurvivesCreateAndEditPut(t *testing.T) {
	s, r := planModeStrategyServer(t)
	rec := doStrategyRequest(r, http.MethodPost, "/api/strategies", `{"name":"pep","config":{"pep":{
		"pep_enabled":true,"pep_style":"E","pep_contracts":6,
		"style_a":{"pep_a_box_minutes":7,"pep_a_sl_fraction":0.9},
		"style_e":{"pep_e_ema_period":12}}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	cfg, _ := pepReadCfg(t, s, out.ID)
	if !cfg.PepEnabled() || cfg.Pep.Style != "E" || *cfg.Pep.Contracts != 6 || *cfg.Pep.StyleA.BoxMinutes != 7 || *cfg.Pep.StyleE.EMAPeriod != 12 {
		t.Fatalf("create dropped the block: %+v", cfg.Pep)
	}

	// SWITCHING STYLE never loses the other style's numbers (the PUT merge keeps
	// every key it was not sent)
	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+out.ID, `{"config":{"pep":{"pep_style":"A"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ = pepReadCfg(t, s, out.ID)
	if cfg.Pep.Style != "A" || *cfg.Pep.StyleA.BoxMinutes != 7 || *cfg.Pep.StyleA.SLFraction != 0.9 || *cfg.Pep.StyleE.EMAPeriod != 12 || *cfg.Pep.Contracts != 6 {
		t.Fatalf("a style switch lost numbers: %+v A=%+v E=%+v", cfg.Pep, cfg.Pep.StyleA, cfg.Pep.StyleE)
	}

	// an unrelated edit leaves the whole block alone
	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+out.ID, `{"config":{"day_plan":{"scenario_cap":4}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unrelated edit: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ = pepReadCfg(t, s, out.ID)
	if !cfg.PepEnabled() || cfg.Pep.Style != "A" {
		t.Fatalf("an unrelated edit changed pep: %+v", cfg.Pep)
	}
}

func TestPepWithMentorOrDayPlanIsRefusedByName(t *testing.T) {
	s, r := planModeStrategyServer(t)
	post := func(body string) (int, pepAPIErr) {
		rec := doStrategyRequest(r, http.MethodPost, "/api/strategies", body)
		var e pepAPIErr
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		return rec.Code, e
	}

	// CREATE with both on
	code, e := post(`{"name":"x","config":{"pep":{"pep_enabled":true},"ai_config":{"risk_control":{"mentor_mode":true}}}}`)
	if code != http.StatusBadRequest || e.ErrorKey != store.PepConflictMentor || !strings.Contains(e.Error, "mentor") || !strings.Contains(e.Error, "pep") {
		t.Fatalf("create pep+mentor: %d %+v", code, e)
	}
	code, e = post(`{"name":"x","config":{"pep":{"pep_enabled":true},"day_plan":{"plan_enabled":true}}}`)
	if code != http.StatusBadRequest || e.ErrorKey != store.PepConflictDayPlan {
		t.Fatalf("create pep+day plan: %d %+v", code, e)
	}
	list, _ := s.store.Strategy().List("u1")
	for _, st := range list {
		if st.Name == "x" {
			t.Fatal("a refused create left a row")
		}
	}

	// EDIT: a mentor strategy cannot turn pep on; a pep strategy cannot turn mentor on
	rec := doStrategyRequest(r, http.MethodPost, "/api/strategies", `{"name":"m","config":{"ai_config":{"risk_control":{"mentor_mode":true}}}}`)
	var mm struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mm)
	_, before := pepReadCfg(t, s, mm.ID)
	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+mm.ID, `{"config":{"pep":{"pep_enabled":true}}}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusBadRequest || e.ErrorKey != store.PepConflictMentor {
		t.Fatalf("edit mentor strategy to pep: %d %s", rec.Code, rec.Body.String())
	}
	if _, after := pepReadCfg(t, s, mm.ID); after != before {
		t.Fatal("a refused edit changed the stored config")
	}

	rec = doStrategyRequest(r, http.MethodPost, "/api/strategies", `{"name":"p","config":{"pep":{"pep_enabled":true}}}`)
	var pp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pp)
	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+pp.ID, `{"config":{"ai_config":{"risk_control":{"mentor_mode":true}}}}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusBadRequest || e.ErrorKey != store.PepConflictMentor {
		t.Fatalf("edit pep strategy to mentor: %d %s", rec.Code, rec.Body.String())
	}

	// pep OFF with mentor ON is fine, and turning pep OFF in the same save frees mentor
	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+pp.ID, `{"config":{"pep":{"pep_enabled":false},"ai_config":{"risk_control":{"mentor_mode":true}}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pep off + mentor on in one save must be legal: %d %s", rec.Code, rec.Body.String())
	}
}

// A clamped number is never silent: the save answers a warning that names the
// leaf, and the SAVED value is left as the owner wrote it (the bot runs the
// clamp; the effective row says so).
func TestPepClampIsReturnedAsAWarning(t *testing.T) {
	s, r := planModeStrategyServer(t)
	rec := doStrategyRequest(r, http.MethodPost, "/api/strategies", `{"name":"c","config":{"pep":{"pep_enabled":true,"pep_contracts":15}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID       string   `json:"id"`
		Warnings []string `json:"warnings"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "pep.pep_contracts") && strings.Contains(w, "10") {
			found = true
		}
	}
	if !found {
		t.Fatalf("create warnings do not name the clamp: %v", out.Warnings)
	}
	cfg, _ := pepReadCfg(t, s, out.ID)
	if *cfg.Pep.Contracts != 15 {
		t.Fatalf("the saved value must stay as written, got %d", *cfg.Pep.Contracts)
	}
	if store.ResolvePep(&cfg).Contracts != 10 {
		t.Fatal("the running value must be the ceiling")
	}

	rec = doStrategyRequest(r, http.MethodPut, "/api/strategies/"+out.ID, `{"config":{"pep":{"pep_contracts":12}}}`)
	out.Warnings = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || len(out.Warnings) == 0 || !strings.Contains(strings.Join(out.Warnings, "|"), "pep.pep_contracts") {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
}
