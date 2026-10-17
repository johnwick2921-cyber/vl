// W-OWNERLEVEL-API (2026-09-17) — GET /api/plan/owner-levels + the owner_level_id
// on OWNER level facts. Exercised at the production call sites: the handler
// through a gin router with the C1 ownership middleware in front of it (the
// same chain server.go mounts), and the level_facts OWNER merge through
// ownerLevelIndex → planLevelFacts, which is exactly what handlePlanToday runs.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"

	"github.com/gin-gonic/gin"
)

func newOwnerLevelTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "ownerlevels.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	for _, tr := range []*store.Trader{
		{ID: "a_trader", UserID: "user-a", Name: "A", AIModelID: "m", ExchangeID: "e"},
		{ID: "b_trader", UserID: "user-b", Name: "B", AIModelID: "m", ExchangeID: "e"},
	} {
		if err := st.Trader().Create(tr); err != nil {
			t.Fatalf("create trader %s: %v", tr.ID, err)
		}
	}
	return &Server{store: st}, st
}

func mustSaveOwnerLevel(t *testing.T, st *store.Store, l *store.OwnerLevelDB) int64 {
	t.Helper()
	if err := st.OwnerLevel().Save(l); err != nil {
		t.Fatalf("save owner level: %v", err)
	}
	return l.ID
}

// ownerLevelsRouter mounts the GET route behind the same C1 ownership middleware
// the protected group uses; as_user stands in for the JWT user.
func ownerLevelsRouter(s *Server) *gin.Engine {
	r := gin.New()
	r.GET("/api/plan/owner-levels",
		func(c *gin.Context) { c.Set("user_id", c.Query("as_user")); c.Next() },
		s.planTraderOwnership(),
		s.handlePlanOwnerLevels)
	return r
}

type ownerLevelsResp struct {
	Levels []struct {
		ID          int64   `json:"id"`
		Symbol      string  `json:"symbol"`
		Price       float64 `json:"price"`
		Label       string  `json:"label"`
		Note        string  `json:"note"`
		ScenarioTag string  `json:"scenario_tag"`
		CreatedAt   int64   `json:"created_at"`
		Consumed    bool    `json:"consumed"`
	} `json:"levels"`
	AsOfMs int64 `json:"as_of_ms"`
}

func getOwnerLevels(t *testing.T, r *gin.Engine, url string) (int, string, ownerLevelsResp) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	var out ownerLevelsResp
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
	}
	return rec.Code, rec.Body.String(), out
}

// (a) the caller's rows only, price ASC, consumed rows and other symbols
// excluded, as_of_ms present.
func TestHandlePlanOwnerLevelsListsOwnRowsSortedByPrice(t *testing.T) {
	s, st := newOwnerLevelTestServer(t)
	hi := mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30200, Label: "PDH", Note: "fade it", ScenarioTag: "S1", UserID: "user-a", CreatedAt: 1700000000})
	lo := mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30100, Label: "ONL", UserID: "user-a", CreatedAt: 1700000001})
	mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30150, Label: "USED", UserID: "user-a", Consumed: true, CreatedAt: 1700000002})
	mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "ES", Price: 5000, Label: "OTHER-SYM", UserID: "user-a", CreatedAt: 1700000003})
	mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30050, Label: "THEIRS", UserID: "user-b", CreatedAt: 1700000004})

	before := time.Now().UnixMilli()
	code, body, out := getOwnerLevels(t, ownerLevelsRouter(s), "/api/plan/owner-levels?as_user=user-a&trader_id=a_trader")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", code, body)
	}
	if len(out.Levels) != 2 {
		t.Fatalf("expected exactly the 2 pending MNQ rows of user-a, got %d body=%s", len(out.Levels), body)
	}
	if out.Levels[0].ID != lo || out.Levels[1].ID != hi {
		t.Fatalf("expected price ASC order ids [%d %d], got [%d %d]", lo, hi, out.Levels[0].ID, out.Levels[1].ID)
	}
	if out.Levels[0].Price != 30100 || out.Levels[1].Price != 30200 {
		t.Fatalf("expected prices [30100 30200], got [%v %v]", out.Levels[0].Price, out.Levels[1].Price)
	}
	top := out.Levels[1]
	if top.Symbol != "MNQ" || top.Label != "PDH" || top.Note != "fade it" || top.ScenarioTag != "S1" || top.CreatedAt != 1700000000 || top.Consumed {
		t.Fatalf("row fields not carried: %+v", top)
	}
	if out.AsOfMs < before || out.AsOfMs > time.Now().UnixMilli() {
		t.Fatalf("as_of_ms %d outside [%d, now]", out.AsOfMs, before)
	}
	for _, l := range out.Levels {
		if l.Label == "THEIRS" || l.Label == "USED" || l.Label == "OTHER-SYM" {
			t.Fatalf("leaked row %q into user-a's MNQ list", l.Label)
		}
	}
}

// (b) another user's trader → 404 and no rows, both through the middleware and
// through the handler's own guard.
func TestHandlePlanOwnerLevelsCrossUserIs404(t *testing.T) {
	s, st := newOwnerLevelTestServer(t)
	mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30200, Label: "PDH", UserID: "user-a", CreatedAt: 1})

	code, body, _ := getOwnerLevels(t, ownerLevelsRouter(s), "/api/plan/owner-levels?as_user=user-b&trader_id=a_trader")
	if code != http.StatusNotFound {
		t.Fatalf("middleware: expected 404, got %d body=%s", code, body)
	}
	if json.Valid([]byte(body)) {
		var probe map[string]any
		_ = json.Unmarshal([]byte(body), &probe)
		if _, has := probe["levels"]; has {
			t.Fatalf("404 body must carry no rows: %s", body)
		}
	}

	// Handler-only chain (no middleware): the in-handler guard must hold alone.
	bare := gin.New()
	bare.GET("/api/plan/owner-levels",
		func(c *gin.Context) { c.Set("user_id", "user-b"); c.Next() },
		s.handlePlanOwnerLevels)
	code, body, _ = getOwnerLevels(t, bare, "/api/plan/owner-levels?trader_id=a_trader")
	if code != http.StatusNotFound {
		t.Fatalf("handler guard: expected 404, got %d body=%s", code, body)
	}

	// Missing trader_id is a 400, not a listing of anything.
	code, body, _ = getOwnerLevels(t, ownerLevelsRouter(s), "/api/plan/owner-levels?as_user=user-a")
	if code != http.StatusBadRequest {
		t.Fatalf("missing trader_id: expected 400, got %d body=%s", code, body)
	}
}

// (c) empty → {"levels":[]} — a computed empty list is [], never absent/null.
func TestHandlePlanOwnerLevelsEmptyIsEmptyArray(t *testing.T) {
	s, _ := newOwnerLevelTestServer(t)
	code, body, out := getOwnerLevels(t, ownerLevelsRouter(s), "/api/plan/owner-levels?as_user=user-a&trader_id=a_trader")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", code, body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if string(raw["levels"]) != "[]" {
		t.Fatalf(`expected "levels":[] literally, got %s`, string(raw["levels"]))
	}
	if out.AsOfMs == 0 {
		t.Fatalf("as_of_ms must be present on an empty list: %s", body)
	}
}

// (d) the level_facts OWNER row carries owner_level_id equal to the store row
// id; the AI row carries none. Built through ownerLevelIndex (the handler's
// builder) and planLevelFacts (the handler's emitter).
func TestPlanLevelFactsOwnerRowCarriesOwnerLevelID(t *testing.T) {
	s, st := newOwnerLevelTestServer(t)
	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()

	now := time.Date(2026, 9, 17, 14, 30, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		bars := make([]market.Kline, 0, 30)
		for i := 0; i < 30; i++ {
			t0 := start.Add(time.Duration(i) * time.Minute)
			bars = append(bars, market.Kline{OpenTime: t0.UnixMilli(), Open: 30150, High: 30152, Low: 30148, Close: 30150, Volume: 1, CloseTime: t0.Add(time.Minute).UnixMilli() - 1})
		}
		return bars
	}

	// Owner level entered as 30200 (integer) — the plan stores 30200.00; the
	// price-match logic (roundKey) is unchanged by this wave.
	ownerID := mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30200, Label: "PDH", Note: "fade", ScenarioTag: "S2", UserID: "user-a", CreatedAt: 1})
	// A consumed row at the AI level's price must NOT turn it into an owner fact.
	mustSaveOwnerLevel(t, st, &store.OwnerLevelDB{Symbol: "MNQ", Price: 30100, Label: "GONE", UserID: "user-a", Consumed: true, CreatedAt: 2})

	doc := kernel.PlanDoc{Levels: []kernel.PlanLevel{
		{Price: 30200.00, Label: "PDH", Grade: "A", Instruction: "fade"},
		{Price: 30100.00, Label: "ONL", Grade: "B", Instruction: "reclaim-long"},
	}}
	owners := s.ownerLevelIndex("user-a", "MNQ")
	if len(owners) != 1 {
		t.Fatalf("ownerLevelIndex: expected 1 active owner row, got %d", len(owners))
	}
	facts, _, _ := planLevelFacts("a_trader", "MNQ", doc, now, "touch", owners, start.UnixMilli())
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	byLabel := map[string]gin.H{}
	for _, f := range facts {
		byLabel[f["label"].(string)] = f
	}
	own := byLabel["PDH"]
	if own["origin"] != "OWNER" {
		t.Fatalf("PDH fact must be OWNER, got %v", own["origin"])
	}
	gotID, ok := own["owner_level_id"].(int64)
	if !ok || gotID != ownerID {
		t.Fatalf("owner_level_id: want store id %d, got %v (%T)", ownerID, own["owner_level_id"], own["owner_level_id"])
	}
	if own["note"] != "fade" || own["scenario_id"] != "S2" {
		t.Fatalf("owner note/tag not carried: note=%v scenario_id=%v", own["note"], own["scenario_id"])
	}
	ai := byLabel["ONL"]
	if ai["origin"] != "AI" {
		t.Fatalf("ONL fact must be AI, got %v", ai["origin"])
	}
	if _, has := ai["owner_level_id"]; has {
		t.Fatalf("AI fact must not carry owner_level_id: %v", ai["owner_level_id"])
	}

	// Serialized shape: the id survives JSON as an integer on the OWNER row only.
	raw, _ := json.Marshal(facts)
	var rows []map[string]any
	_ = json.Unmarshal(raw, &rows)
	seen := 0
	for _, r := range rows {
		if v, has := r["owner_level_id"]; has {
			seen++
			if int64(v.(float64)) != ownerID {
				t.Fatalf("serialized owner_level_id %v != %d", v, ownerID)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 fact with owner_level_id, got %d", seen)
	}
}
