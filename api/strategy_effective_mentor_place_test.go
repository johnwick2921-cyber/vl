package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Studio's Mentor mode line reads the MENTOR_PLACE gate from the effective
// GET: true only when the process env turns placement on, false (present, not
// absent) otherwise.
func TestEffectiveCarriesMentorPlaceGate(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	effPutStrategy(t, st, "s-mp", effUser, `{}`)

	read := func() any {
		rec := effDo(t, s, tok, http.MethodGet, "/api/strategies/s-mp/effective", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		v, ok := raw["mentor_place"]
		if !ok {
			t.Fatalf("mentor_place missing from the effective response: %s", rec.Body.String())
		}
		return v
	}

	t.Setenv("MENTOR_PLACE", "")
	if v := read(); v != false {
		t.Fatalf("MENTOR_PLACE unset: mentor_place = %v, want false", v)
	}
	t.Setenv("MENTOR_PLACE", "1")
	if v := read(); v != true {
		t.Fatalf("MENTOR_PLACE=1: mentor_place = %v, want true", v)
	}
}

// The Studio toggle saves through the existing PUT: mentor_mode true persists,
// and an explicit false (what the OFF click sends) overrides it — the merge
// must not keep the old true.
func TestStrategyUpdateRoundTripsMentorMode(t *testing.T) {
	s, st, tok := newEffectiveServer(t)
	rec := effDo(t, s, tok, http.MethodPost, "/api/strategies", `{"name":"MM","config":{
		"strategy_type":"ai_trading","ai_config":{"risk_control":{"max_contracts_per_order":1}}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if runtimeConfig(t, st, effUser, created.ID).RiskControl.MentorMode {
		t.Fatal("a new strategy must have mentor_mode OFF")
	}
	for _, want := range []bool{true, false} {
		body := `{"config":{"ai_config":{"risk_control":{"mentor_mode":false}}}}`
		if want {
			body = `{"config":{"ai_config":{"risk_control":{"mentor_mode":true}}}}`
		}
		if rec := effDo(t, s, tok, http.MethodPut, "/api/strategies/"+created.ID, body); rec.Code != http.StatusOK {
			t.Fatalf("PUT mentor_mode=%v: %d %s", want, rec.Code, rec.Body.String())
		}
		if got := runtimeConfig(t, st, effUser, created.ID).RiskControl.MentorMode; got != want {
			t.Fatalf("after PUT mentor_mode=%v the stored value is %v", want, got)
		}
	}
}
