package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vl/manager"
	"vl/store"
)

// W-FRIEND-HOSTED (S14): GET /api/friend/identity answers the SHA-256 of the
// instance's NT_CONNECT_KEY (never the key) in friend mode only.

func newIdentityServer(t *testing.T, friend bool) *Server {
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

func getIdentity(t *testing.T, s *Server) (int, map[string]interface{}, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/friend/identity", nil)
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w.Code, nil, w.Body.String()
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, w.Body.String())
	}
	return w.Code, body, w.Body.String()
}

func TestFriendIdentityOwner404(t *testing.T) {
	s := newIdentityServer(t, false)
	code, _, _ := getIdentity(t, s)
	if code != http.StatusNotFound {
		t.Fatalf("owner mode = %d, want 404", code)
	}
}

func TestFriendIdentityReturnsKeySHA256(t *testing.T) {
	const key = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	t.Setenv("NT_CONNECT_KEY", key)
	s := newIdentityServer(t, true)

	code, body, raw := getIdentity(t, s)
	if code != http.StatusOK {
		t.Fatalf("friend mode = %d, want 200", code)
	}
	sum := sha256.Sum256([]byte(key))
	if body["key_sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("key_sha256 = %v, want sha256(key)", body["key_sha256"])
	}
	// The key itself must never be returned (S14).
	if raw == "" || contains(raw, key) {
		t.Fatalf("response must not carry the connect key")
	}
}

func TestFriendIdentityEmptyKey(t *testing.T) {
	t.Setenv("NT_CONNECT_KEY", "")
	s := newIdentityServer(t, true)
	_, body, _ := getIdentity(t, s)
	// sha256("") is a real digest, but the key is always set on a provisioned
	// friend; pin that the field is present regardless.
	if _, ok := body["key_sha256"]; !ok {
		t.Fatalf("key_sha256 must be present")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
