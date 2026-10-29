package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth/checks"

	"github.com/gin-gonic/gin"
)

// m447 (7): POST /api/risk/ack-fill-price lifts a standing fill_price pause for
// the symbol and reports whether one was standing.
func TestHandleAckFillPrice(t *testing.T) {
	saved := checks.DefaultFills
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	checks.DefaultFills = g
	defer func() { checks.DefaultFills = saved }()

	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / 60_000 * 60_000
	ring := func(string) ([]checks.Bar, error) {
		return []checks.Bar{{T: m, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true}, {T: m + 60_000, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true}}, nil
	}
	for _, id := range []string{"a", "b"} {
		g.Record(checks.FillObs{ID: id, Symbol: "MNQ", Kind: "exit", Price: 29100, AtMs: m + 5_000, Tick: 0.25}, ring, day)
	}
	blocked, _, _ := g.Check()("MNQ", day)
	if !blocked {
		t.Fatal("two far fills must pause before the ack")
	}

	s := &Server{}
	router := gin.New()
	router.POST("/api/risk/ack-fill-price", s.handleAckFillPrice)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/risk/ack-fill-price", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no symbol must be a 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/risk/ack-fill-price?symbol=mnq", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"acknowledged":true`) {
		t.Fatalf("ack must lift the pause: %d %s", rec.Code, rec.Body.String())
	}
	if blocked, _, _ := g.Check()("MNQ", day); blocked {
		t.Fatal("entries must resume after the ack")
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/risk/ack-fill-price?symbol=MNQ", nil))
	if !strings.Contains(rec.Body.String(), `"acknowledged":false`) {
		t.Fatalf("nothing left to acknowledge: %s", rec.Body.String())
	}
}
