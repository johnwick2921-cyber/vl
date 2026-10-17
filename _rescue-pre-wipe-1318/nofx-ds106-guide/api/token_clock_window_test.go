package api

// M3 verifier defect 4 — CTO ruling 1790243040753 "future-iat survives H2:
// TIGHTEN, fail-closed". auth.ValidateJWT parses with jwt.WithIssuedAt() and
// jwt.WithLeeway(60 s, auth.ClockLeeway): a token whose iat is more than
// 60 s ahead of the server's clock is refused EVERYWHERE (401 on the protected
// group, the uniform 403 on /api/updates). Before, iat was never compared with
// now, so a token stamped in the future — minted before a password change —
// carried an iat AFTER the change's epoch and survived H2 (the verifier's
// probe: GET /api/my-traders = 200).
//
// jwt v5 applies the ONE leeway to iat, nbf AND exp (validator.go
// verifyIssuedAt / verifyNotBefore / verifyExpiresAt), so the ruling also
// widens exp and nbf by 60 s. That widening is pinned here BOUNDED (30 s in:
// admitted; 2 min out: refused) and on the record — and the logout blacklist
// holds an entry through the same 60 s past exp, or a logged-out token would
// come back to life for the last minute of the window the parser admits.
// Driven at the PRODUCTION router (NewServer, real middleware — canon 53).

import (
	"net/http"
	"testing"
	"time"

	"vl/auth"

	"github.com/golang-jwt/jwt/v5"
)

// mintAt signs the admin's auth.Claims with EXACTLY the given times; a zero
// time leaves that claim out. mintJWT couples nbf to iat-1min, which would
// let the nbf rule hide what the iat rule does.
func mintAt(t *testing.T, iat, nbf, exp time.Time) string {
	t.Helper()
	rc := jwt.RegisteredClaims{Issuer: "nofxAI"}
	if !iat.IsZero() {
		rc.IssuedAt = jwt.NewNumericDate(iat)
	}
	if !nbf.IsZero() {
		rc.NotBefore = jwt.NewNumericDate(nbf)
	}
	if !exp.IsZero() {
		rc.ExpiresAt = jwt.NewNumericDate(exp)
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{UserID: updAdminID, Email: updAdminEmail, RegisteredClaims: rc}).SignedString([]byte(updSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// expectRefused: 401 on a protected non-credential route AND the uniform 403
// on every /api/updates route.
func expectRefused(t *testing.T, e *updEnv, label, tok string) {
	t.Helper()
	if w := credCall(t, e, "GET", "/api/my-traders", tok, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("%s: GET /api/my-traders = %d %s — want 401", label, w.Code, w.Body.String())
	}
	e.expectAllForbidden(label, withToken(tok))
}

// expectAdmitted: 200 on the protected route AND every /api/updates route
// reaches its handler (the admin's positive-control statuses).
func expectAdmitted(t *testing.T, e *updEnv, label, tok string) {
	t.Helper()
	if w := credCall(t, e, "GET", "/api/my-traders", tok, ""); w.Code != http.StatusOK {
		t.Errorf("%s: GET /api/my-traders = %d %s — want 200", label, w.Code, w.Body.String())
	}
	e.expectAllAdmitted(label, withToken(tok))
}

func TestFutureIatTokenIsRefusedEverywhere(t *testing.T) {
	e := newUpdEnv(t)
	now := time.Now()
	past := now.Add(-time.Minute)

	// The ruling's pins. nbf is in the PAST, so only the iat rule can refuse
	// (the verifier's probe shape).
	expectRefused(t, e, "iat = now+2min (nbf past)", mintAt(t, now.Add(2*time.Minute), past, now.Add(time.Hour)))
	expectAdmitted(t, e, "iat = now+30s (nbf past) — inside the 60 s leeway", mintAt(t, now.Add(30*time.Second), past, now.Add(time.Hour)))

	// The shape the server itself mints (auth.signToken: iat == nbf == now):
	// stamped 2 min ahead, it is refused — by nbf as well as iat.
	expectRefused(t, e, "iat = nbf = now+2min (the server's own shape)", mintAt(t, now.Add(2*time.Minute), now.Add(2*time.Minute), now.Add(time.Hour)))

	// Defect 4 end to end: a token stamped 10 min ahead, minted BEFORE the
	// owner's password change, is refused after it (its iat is after the
	// change's epoch, so only the future-iat rule can catch it).
	ahead := mintAt(t, now.Add(10*time.Minute), past, now.Add(time.Hour))
	owner, code := credLogin(t, e, updAdminEmail, updAdminPass)
	if code != http.StatusOK {
		t.Fatalf("owner login = %d", code)
	}
	if w := credCall(t, e, "PUT", "/api/user/password", owner, `{"current_password":"`+updAdminPass+`","new_password":"owner-rotated-pass-9"}`); w.Code != http.StatusOK {
		t.Fatalf("the owner's rotation = %d %s", w.Code, w.Body.String())
	}
	if w := credCall(t, e, "GET", "/api/my-traders", ahead, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("defect 4: a future-iat token minted before the password change: GET /api/my-traders after it = %d %s — want 401", w.Code, w.Body.String())
	}
	// Positive control: a session opened after the change works.
	untilNextSecond()
	fresh, code := credLogin(t, e, updAdminEmail, "owner-rotated-pass-9")
	if code != http.StatusOK {
		t.Fatalf("login with the rotated password = %d", code)
	}
	if w := credCall(t, e, "GET", "/api/my-traders", fresh, ""); w.Code != http.StatusOK {
		t.Fatalf("positive control: a post-change session = %d", w.Code)
	}
}

// The ruling's side effect, BOUNDED: the one 60 s leeway also applies to exp
// and nbf. exp 30 s past and nbf 30 s ahead are admitted; 2 min either way is
// refused.
func TestClockLeewayOnExpAndNbfIsBoundedAtSixtySeconds(t *testing.T) {
	e := newUpdEnv(t)
	now := time.Now()
	iat := now.Add(-10 * time.Minute) // after the admin row's updated_at (1 h ago): Q8 passes

	expectAdmitted(t, e, "exp = now-30s — inside the 60 s leeway", mintAt(t, iat, iat, now.Add(-30*time.Second)))
	expectRefused(t, e, "exp = now-2min", mintAt(t, iat, iat, now.Add(-2*time.Minute)))
	expectAdmitted(t, e, "nbf = now+30s — inside the 60 s leeway", mintAt(t, iat, now.Add(30*time.Second), now.Add(time.Hour)))
	expectRefused(t, e, "nbf = now+2min", mintAt(t, iat, now.Add(2*time.Minute), now.Add(time.Hour)))
}

// The logout blacklist is an in-memory map whose entry lives until the
// token's exp. With the 60 s exp leeway the parser admits a token up to 60 s
// PAST exp, so the entry must live that long too: a token logged out inside
// that window stays refused.
func TestLoggedOutTokenStaysRevokedThroughTheExpiryLeeway(t *testing.T) {
	e := newUpdEnv(t)
	now := time.Now()
	iat := now.Add(-10 * time.Minute)
	tok := mintAt(t, iat, iat, now.Add(-30*time.Second))
	if w := credCall(t, e, "GET", "/api/my-traders", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("control: a token 30 s past exp (inside the leeway) = %d %s — want 200", w.Code, w.Body.String())
	}
	if w := credCall(t, e, "POST", "/api/logout", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("logout = %d %s", w.Code, w.Body.String())
	}
	expectRefused(t, e, "a token logged out 30 s past its exp", tok)

	// The same through a fresh token: logged out, then refused.
	live := mintAt(t, iat, iat, now.Add(time.Hour))
	if w := credCall(t, e, "POST", "/api/logout", live, ""); w.Code != http.StatusOK {
		t.Fatalf("logout (live token) = %d %s", w.Code, w.Body.String())
	}
	expectRefused(t, e, "a logged-out live token", live)
}
