package api

import (
	"net/http"
	"testing"
)

// R15 — pause/rotate/revoke WARN (409) when /api/friend/flat is not flat;
// force=1 overrides. A definitive flat answer is the only refusal.

func TestR15NonFlatRefusesActionsUnlessForce(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)

	prev := friendFlatProbe
	t.Cleanup(func() { friendFlatProbe = prev })

	// Not flat, definitive.
	friendFlatProbe = func(*friendRecord) (bool, bool) { return false, true }
	for _, act := range []string{"pause", "rotate", "revoke"} {
		writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z"}]`)
		w := friendCall(t, e, "POST", "/api/owner/friends/tom/"+act, "", e.tok)
		if w.Code != http.StatusConflict {
			t.Errorf("%s on a non-flat friend = %d, want 409 (%s)", act, w.Code, w.Body.String())
		}
	}

	// force=1 overrides.
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z"}]`)
	w := friendCall(t, e, "POST", "/api/owner/friends/tom/pause?force=1", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("pause?force=1 on a non-flat friend = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	// Flat, definitive: no refusal.
	friendFlatProbe = func(*friendRecord) (bool, bool) { return true, true }
	resetFriendRateLimiterForTest()
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z"}]`)
	w = friendCall(t, e, "POST", "/api/owner/friends/tom/pause", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("pause on a flat friend = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestR15UnreachableFriendProceeds(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z"}]`)

	prev := friendFlatProbe
	friendFlatProbe = func(*friendRecord) (bool, bool) { return false, false } // unknown
	t.Cleanup(func() { friendFlatProbe = prev })

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/pause", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("pause on an unreachable friend = %d, want 200 (must stay revocable) (%s)", w.Code, w.Body.String())
	}
}
