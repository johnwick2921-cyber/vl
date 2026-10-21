package api

import (
	"net/http"
	"testing"
)

// R7 — rotate on a paused friend must not restart (start) it: 409 "resume
// first", and no systemctl restart is issued.

func TestR7RotateOnPausedFriendRefused(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"paused","created_at":"2026-10-18T00:00:00Z"}]`)

	testSystemctlCalls = nil
	w := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if w.Code != http.StatusConflict {
		t.Fatalf("rotate on paused = %d, want 409 (%s)", w.Code, w.Body.String())
	}
	for _, c := range testSystemctlCalls {
		if c == "restart vl-friend@tom.service" {
			t.Fatalf("rotate on a paused friend must NOT restart the unit (saw %q)", c)
		}
	}
}
