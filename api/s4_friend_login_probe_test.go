package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vl/auth"
)

// TestS4FriendLoginCannotReachOwnerSurfaces — R6 ruling (CTO 2026-10-18): a
// friend LOGIN must be refused every owner-only surface (updates, maintenance,
// installer, telegram, debug, admin, armed test, bar arbiter). These routes are
// not registered on a friend bot, so they 404 even with a VALID friend JWT —
// the probe proves the 404 is route-gating, not a missing token, by also
// exercising one enabled authenticated route with the SAME token.
func TestS4FriendLoginCannotReachOwnerSurfaces(t *testing.T) {
	prev := auth.JWTSecret
	auth.SetJWTSecret("s4-probe-test-secret")
	t.Cleanup(func() { auth.JWTSecret = prev })

	s := newFriendTestServer(t, true)

	tok := mintJWT(t, "friend-user-1", "friend@example.com",
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour), "s4-probe-test-secret")

	disabled := []string{
		"/api/updates",
		"/api/maintenance",
		"/api/installation-gate",
		"/api/telegram",
		"/api/telegram/model",
		"/api/telegram/binding",
		"/api/telegram/bind-code",
		"/api/debug/nt-test-trade",
		"/api/debug/nt-bars-unsubscribe",
		"/api/nt/bar-arbiter",
		"/api/armed/test-arm",
		"/api/admin/bars/import",
	}
	for _, p := range disabled {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		s.router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("friend login GET %s = %d, want 404 (owner surface must not exist on a friend bot)", p, w.Code)
		}
	}

	// The SAME token reaches an enabled authenticated surface — proving the 404s
	// above are route-gating, not a dead/refused token.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/resolved", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	s.router.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatalf("friend login GET /api/config/resolved = 404 — the probe token does not reach an enabled authenticated route, so the 404s above prove nothing")
	}
}
