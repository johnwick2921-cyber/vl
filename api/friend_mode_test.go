package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vl/manager"
	"vl/store"
)

// W-FRIEND-HOSTED Segment 2 (DS-102) — at the PRODUCTION router (canon 53:
// NewServer → setupRoutes). A friend instance must 404 the owner-only surfaces
// (updates, maintenance, installation gate); the owner's bot keeps them.

func newFriendTestServer(t *testing.T, friend bool) *Server {
	t.Helper()
	prev := friendModeResolver
	t.Cleanup(func() { friendModeResolver = prev })
	friendModeResolver = func() bool { return friend }

	st, err := store.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0)
}

func TestFriendModeOwnerSurfaces404(t *testing.T) {
	s := newFriendTestServer(t, true)
	for _, p := range []string{"/api/updates", "/api/maintenance", "/api/installation-gate"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		s.router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("friend mode GET %s = %d, want 404 (route not registered)", p, w.Code)
		}
	}
}

func TestOwnerModeUpdateRouteStillRegistered(t *testing.T) {
	s := newFriendTestServer(t, false)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/updates", nil)
	s.router.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Errorf("owner mode GET /api/updates = 404, want the update route registered (its own gate, not 404)")
	}
}
