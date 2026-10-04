package api

// M3 red-team H1, second layer: MACHINE tokens carry a scope claim and are
// denied by default on the credential routes, the Telegram bot's own config
// routes and /api/updates* — at the PRODUCTION router (canon 53). The email
// rule (credential_guard.go) stops the bot@internal token; the scope rule
// stops a machine token whatever email it carries, and closes the /telegram*
// routes the email rule does not cover.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// mintMapJWT signs an arbitrary claim set (so a pin can carry a claim the
// auth.Claims struct of an older build does not know).
func mintMapJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(updSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ownerClaims(email string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"user_id": updAdminID, "email": email, "iss": "vlAI",
		"iat": now.Add(-5 * time.Second).Unix(), "nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

func tokenPayload(t *testing.T, tok string) map[string]any {
	t.Helper()
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return []byte(updSecret), nil }); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return claims
}

// machineDeniedProbes: every route a machine token must be refused on, with
// a body the handler would otherwise accept.
var machineDeniedProbes = []struct{ method, path, body string }{
	{"PUT", "/api/user/password", `{"current_password":"` + updAdminPass + `","new_password":"machine-chosen-pass-1"}`},
	{"POST", "/api/reset-account", `{"confirm":"RESET-ALL-DATA"}`},
	{"GET", "/api/telegram", ""},
	{"POST", "/api/telegram", `{"bot_token":"123:abc","model_id":"m"}`},
	{"POST", "/api/telegram/model", `{"model_id":"m"}`},
	{"DELETE", "/api/telegram/binding", ""},
}

func TestBotTokenCarriesTheTelegramScope(t *testing.T) {
	e := newUpdEnv(t)
	_ = e
	p := tokenPayload(t, mustBotToken(t))
	if p["scope"] != "telegram" {
		t.Fatalf("the production bot token's scope claim = %v — want \"telegram\"", p["scope"])
	}
}

// User tokens: the login token's claim set is exactly the reviewed shape — no
// scope key at all (absent, not ""), and a jti so same-second mints can never
// share a string (token-iat-same-second, owner order 2026-10-02).
func TestLoginTokenCarriesNoScope(t *testing.T) {
	e := newUpdEnv(t)
	tok, code := credLogin(t, e, updAdminEmail, updAdminPass)
	if code != http.StatusOK {
		t.Fatalf("login = %d", code)
	}
	p := tokenPayload(t, tok)
	keys := map[string]bool{}
	for k := range p {
		keys[k] = true
	}
	want := []string{"user_id", "email", "exp", "iat", "nbf", "iss", "jti"}
	if len(keys) != len(want) {
		b, _ := json.Marshal(p)
		t.Fatalf("login token claim keys = %s — want exactly %v", b, want)
	}
	for _, k := range want {
		if !keys[k] {
			t.Fatalf("login token lacks %q", k)
		}
	}
}

// A machine-scoped token is refused on every denied route EVEN WHEN it carries
// the owner's real email (so the email rule alone would admit it); an
// unknown scope is a machine scope too. Positive controls: the same token
// still reaches an ordinary protected route; the owner's login token still
// reaches the Telegram config.
func TestMachineScopedTokenIsDeniedOnCredentialTelegramAndUpdateRoutes(t *testing.T) {
	e := newUpdEnv(t)
	t.Setenv("ALLOW_ACCOUNT_RESET", "1")
	before := e.adminRow()
	for _, scope := range []string{"telegram", "gate-jwt", "anything-else"} {
		c := ownerClaims(updAdminEmail)
		c["scope"] = scope
		tok := mintMapJWT(t, c)
		for _, p := range machineDeniedProbes {
			if w := credCall(t, e, p.method, p.path, tok, p.body); w.Code != http.StatusForbidden {
				t.Fatalf("scope %q: %s %s = %d %s — want 403", scope, p.method, p.path, w.Code, w.Body.String())
			}
		}
		e.expectAllForbidden("scope "+scope+" on /api/updates*", withToken(tok))
		if w := credCall(t, e, "GET", "/api/my-traders", tok, ""); w.Code != http.StatusOK {
			t.Fatalf("positive control: scope %q on GET /api/my-traders = %d — the agent must keep its ordinary routes", scope, w.Code)
		}
	}
	if after := e.adminRow(); after != before {
		t.Fatal("a refused machine token wrote the admin row")
	}
	if n, _ := e.st.User().Count(); n != 1 {
		t.Fatalf("users = %d after refused resets", n)
	}
	owner, _ := credLogin(t, e, updAdminEmail, updAdminPass)
	if w := credCall(t, e, "GET", "/api/telegram", owner, ""); w.Code != http.StatusOK {
		t.Fatalf("positive control: the owner's login token on GET /api/telegram = %d", w.Code)
	}
	e.expectAllAdmitted("admin")
}

// Fail closed for a bot token minted by an OLDER binary (no scope claim):
// the bot@internal email alone makes it a machine token.
func TestUnscopedBotInternalTokenIsStillAMachineToken(t *testing.T) {
	e := newUpdEnv(t)
	legacy := mintMapJWT(t, ownerClaims("bot@internal"))
	for _, tok := range map[string]string{"legacy (no scope)": legacy, "production": mustBotToken(t)} {
		for _, p := range machineDeniedProbes {
			if p.path == "/api/reset-account" {
				continue // covered above; the flag is off here
			}
			if w := credCall(t, e, p.method, p.path, tok, p.body); w.Code != http.StatusForbidden {
				t.Fatalf("bot@internal token: %s %s = %d %s — want 403", p.method, p.path, w.Code, w.Body.String())
			}
		}
		if w := credCall(t, e, "GET", "/api/my-traders", tok, ""); w.Code != http.StatusOK {
			t.Fatalf("positive control: bot@internal on GET /api/my-traders = %d", w.Code)
		}
	}
}
