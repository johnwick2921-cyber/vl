package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// m386 — the Friends page is OFF unless VL_FRIENDS_PAGE=1. At the PRODUCTION
// router (canon 53), the owner friends API 404s when the flag is unset and is
// reachable (its own owner gate, not 404) when the flag is on.

func TestFriendsPageOffOwnerFriends404(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	t.Setenv("VL_FRIENDS_PAGE", "")

	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/owner/friends", ""},
		{"POST", "/api/owner/friends", `{"name":"tom"}`},
	} {
		if w := friendCall(t, e, c.method, c.path, c.body, e.tok); w.Code != http.StatusNotFound {
			t.Errorf("friends page off: %s %s = %d (%s), want 404", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

func TestFriendsPageOnOwnerFriendsReachable(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	t.Setenv("VL_FRIENDS_PAGE", "1")

	if w := friendCall(t, e, "GET", "/api/owner/friends", "", e.tok); w.Code != http.StatusOK {
		t.Fatalf("friends page on: GET /api/owner/friends = %d (%s), want 200 (empty registry)", w.Code, w.Body.String())
	}
}

// TestFriendsPage404Gate is the direct pin of the gate itself: friendsPage404
// aborts with 404 when the flag is off and returns false (no abort) when on.
func TestFriendsPage404Gate(t *testing.T) {
	t.Setenv("VL_FRIENDS_PAGE", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/owner/friends", nil)
	if !friendsPage404(c) {
		t.Fatal("flag off: friendsPage404 must abort true")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("flag off: status = %d, want 404", w.Code)
	}

	t.Setenv("VL_FRIENDS_PAGE", "1")
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/owner/friends", nil)
	if friendsPage404(c2) {
		t.Fatal("flag on: friendsPage404 must not abort")
	}
}
