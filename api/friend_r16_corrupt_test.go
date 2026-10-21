package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// R16 — a corrupt registry must surface as 500, not a false 404 "friend not
// found".

func TestR16CorruptRegistryReturns500Not404(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)

	// Corrupt JSON (not a valid array).
	p := filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "friends.json")
	if err := os.WriteFile(p, []byte(`{"name":`), 0o644); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/pause", "", e.tok)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt registry action = %d, want 500 (%s)", w.Code, w.Body.String())
	}
}
