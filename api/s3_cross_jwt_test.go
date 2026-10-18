package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vl/auth"
	"vl/manager"
	"vl/store"
)

// B5 S3 — a JWT minted by one instance is rejected by another. The signing
// secret is per-instance (each friend .env gets its own JWT_SECRET, and the
// owner's is separate), so a friend token never works on the owner's bot or on
// another friend's bot. Pinned at the PRODUCTION call site: the auth middleware
// on a protected route (canon 53).
func TestS3CrossInstanceJWTRejected(t *testing.T) {
	prev := auth.JWTSecret
	t.Cleanup(func() { auth.JWTSecret = prev })

	st, err := store.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	s := NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0)

	secrets := map[string]string{
		"owner":   "s3-owner-secret-0123456789abcdef0123456789abcdef",
		"friend1": "s3-friend1-secret-0123456789abcdef0123456789abcd",
		"friend2": "s3-friend2-secret-0123456789abcdef0123456789abcd",
	}

	mk := func(secret string) string {
		return mintJWT(t, "u1", "u@example.com",
			time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), secret)
	}
	hit := func(tok string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/my-traders", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		s.router.ServeHTTP(w, req)
		return w.Code
	}

	// The owner instance (secret = owner) rejects both friends' tokens.
	auth.SetJWTSecret(secrets["owner"])
	if c := hit(mk(secrets["friend1"])); c != http.StatusUnauthorized {
		t.Errorf("friend1 token on the owner instance = %d, want 401", c)
	}
	if c := hit(mk(secrets["friend2"])); c != http.StatusUnauthorized {
		t.Errorf("friend2 token on the owner instance = %d, want 401", c)
	}

	// A friend instance (secret = friend1) rejects the owner's token and the
	// other friend's token.
	auth.SetJWTSecret(secrets["friend1"])
	if c := hit(mk(secrets["owner"])); c != http.StatusUnauthorized {
		t.Errorf("owner token on friend1 instance = %d, want 401", c)
	}
	if c := hit(mk(secrets["friend2"])); c != http.StatusUnauthorized {
		t.Errorf("friend2 token on friend1 instance = %d, want 401", c)
	}
}
