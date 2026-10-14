package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// OPS-SECURITY / UI-GUIDE-TRUTH §4 — GET /api/equity-history was PUBLIC
// (server.go: the public competition group): 10,000 equity points for a
// trader were readable with no token, and CORS * let any page in the owner's
// browser read them. It now lives behind authMiddleware like every other
// account figure. The web client's httpClient always attaches the Bearer
// token, so the dashboard keeps working. Driven through the real router.
func TestEquityHistoryRequiresAuth(t *testing.T) {
	e := newUpdEnv(t)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/equity-history?trader_id=t1", ""},
		{"POST", "/api/equity-history-batch", `{"trader_ids":["t1"]}`},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		e.s.router.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", tc.method, tc.path, w.Code)
		}
		// With the owner's token the route is reachable (not 401).
		w = httptest.NewRecorder()
		r = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Authorization", "Bearer "+e.tok)
		e.s.router.ServeHTTP(w, r)
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s %s WITH the owner's token = 401 (%s) — the dashboard would break", tc.method, tc.path, w.Body.String())
		}
	}
}
