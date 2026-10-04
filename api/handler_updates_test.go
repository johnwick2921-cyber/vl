package api

// W-ONE-BUTTON M3 — update authorization, driven at the PRODUCTION call site
// (canon 53): the router built by NewServer with the real global middleware
// (gin.Default + corsMiddleware) and the real setupRoutes, never a bare
// gin.New(). Every refusal test carries a positive control: the same request
// with the one defect removed reaches the next stage (the stub verifier's
// 422), so the refusal is caused by the defect and not by a broken harness.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"vl/auth"
	"vl/config"
	"vl/internal/updateauth"
	"vl/logger"
	"vl/manager"
	"vl/store"
	"vl/telegram/agent"
	"vl/trader"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	updAdminID    = "aaaaaaaa-1111-2222-3333-444444444444"
	updAdminEmail = "owner@example.test"
	updAdminPass  = "correct-horse-battery"
	updOtherID    = "bbbbbbbb-1111-2222-3333-444444444444"
	updOtherEmail = "someone@example.test"
	updSecret     = "m3-test-secret-not-the-default-32b+" // ≥ 32 bytes: /api/updates* refuse a shorter secret (red-team M1)
	updRelease    = "v2026.09.24-1"
)

const forbiddenBody = `{"error":"forbidden"}`

type updEnv struct {
	t       *testing.T
	s       *Server
	st      *store.Store
	dataDir string
	tok     string
}

// newUpdEnv: a temp installation, an admin user row created well in the past
// (so a JWT issued a few seconds ago passes Q8), the admin enrolled by the
// CLI's own writer, and the production server.
func newUpdEnv(t *testing.T) *updEnv {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(dataDir, "data.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Plan().Close(); _ = st.Close() })

	prevDir := trader.MaintenanceDataDir()
	trader.SetMaintenanceDataDir(dataDir)
	t.Cleanup(func() { trader.SetMaintenanceDataDir(prevDir) })
	prevSecret := auth.JWTSecret
	auth.SetJWTSecret(updSecret)
	t.Cleanup(func() { auth.JWTSecret = prevSecret })

	hash, err := auth.HashPassword(updAdminPass)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UTC()
	if err := st.User().Create(&store.User{ID: updAdminID, Email: updAdminEmail, PasswordHash: hash, CreatedAt: past, UpdatedAt: past}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if err := updateauth.Enroll(dataDir, updAdminID, updAdminEmail, hash, time.Now(), false); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	e := &updEnv{t: t, st: st, dataDir: dataDir}
	e.s = NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0)
	e.tok = mintJWT(t, updAdminID, updAdminEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), updSecret)
	// P2-11: the login limiter is process-global; every env starts clean so
	// one test's failed logins cannot block another's (the IP key is shared).
	resetLoginLimiter()
	// install-with-password lockout limiter: same process-global hygiene.
	apiInstallPasswordLimiter.mu.Lock()
	apiInstallPasswordLimiter.m = make(map[string]*loginLimiterEntry)
	apiInstallPasswordLimiter.mu.Unlock()
	return e
}

// mintJWT signs auth.Claims with an explicit iat/exp (auth.GenerateJWT
// always uses now/now+24h).
func mintJWT(t *testing.T, userID, email string, iat, exp time.Time, secret string) string {
	t.Helper()
	c := auth.Claims{UserID: userID, Email: email, RegisteredClaims: jwt.RegisteredClaims{
		IssuedAt: jwt.NewNumericDate(iat), NotBefore: jwt.NewNumericDate(iat.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(exp), Issuer: "vlAI",
	}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// mintNoIAT: a correctly signed admin token with no iat claim (Q8 cannot be
// judged ⇒ refuse).
func mintNoIAT(t *testing.T, userID, email string, exp time.Time, secret string) string {
	t.Helper()
	c := auth.Claims{UserID: userID, Email: email, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// do sends a request that passes every transport check (loopback peer,
// loopback Host, the update header, no Origin) with the admin's token; mut
// then introduces exactly one defect.
func (e *updEnv) do(method, path, body string, mut ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:52000"
	r.Host = "127.0.0.1:8080"
	r.Header.Set(UpdateHeader, "1")
	r.Header.Set("Authorization", "Bearer "+e.tok)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mut {
		m(r)
	}
	w := httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	return w
}

// grant mints an authorization with the production minting path (the
// attended CLI's updateauth.Authorize), at the real clock.
func (e *updEnv) grant(release string) updateauth.Grant {
	e.t.Helper()
	g, err := updateauth.Authorize(e.dataDir, release, time.Now())
	if err != nil {
		e.t.Fatalf("authorize: %v", err)
	}
	return g
}

func grantBody(g updateauth.Grant) string {
	b, _ := json.Marshal(g)
	return string(b)
}

// rawMACBody builds an install body with a MAC computed directly over the
// given fields (bypassing updateauth's id validation) — for path-shaped ids.
func (e *updEnv) rawMACBody(release, job string, exp int64) string {
	e.t.Helper()
	key, err := updateauth.LoadDeviceKey(e.dataDir)
	if err != nil {
		e.t.Fatal(err)
	}
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s|%s|%s|%s|%d", updateauth.MACPurpose, updAdminID, release, job, exp)
	b, _ := json.Marshal(map[string]any{"release_id": release, "job_id": job, "expires_at": exp, "hmac": hex.EncodeToString(m.Sum(nil))})
	return string(b)
}

type updRoute struct{ method, path, body string }

func (e *updEnv) allRoutes() []updRoute {
	return []updRoute{
		{"GET", "/api/updates", ""},
		{"POST", "/api/updates/check", "{}"},
		{"POST", "/api/updates/install", grantBody(e.grant(updRelease))},
		{"GET", "/api/updates/jobs/0123456789abcdef", ""},
		{"GET", "/api/updates/jobs/0123456789abcdef/receipt", ""},
	}
}

// admittedStatus is what each route answers to the admin (positive control).
var admittedStatus = map[string]int{
	"/api/updates":                               http.StatusOK,
	"/api/updates/check":                         http.StatusOK,
	"/api/updates/install":                       http.StatusUnprocessableEntity,
	"/api/updates/jobs/0123456789abcdef":         http.StatusNotFound,
	"/api/updates/jobs/0123456789abcdef/receipt": http.StatusNotFound,
}

// expectAllForbidden: every route refuses with the byte-identical body.
func (e *updEnv) expectAllForbidden(label string, mut ...func(*http.Request)) {
	e.t.Helper()
	for _, rt := range e.allRoutes() {
		w := e.do(rt.method, rt.path, rt.body, mut...)
		if w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
			e.t.Errorf("%s: %s %s = %d %s, want 403 %s", label, rt.method, rt.path, w.Code, w.Body.String(), forbiddenBody)
		}
	}
}

// expectAllAdmitted is the positive control: the same routes without the
// defect reach their handlers.
func (e *updEnv) expectAllAdmitted(label string, mut ...func(*http.Request)) {
	e.t.Helper()
	for _, rt := range e.allRoutes() {
		w := e.do(rt.method, rt.path, rt.body, mut...)
		if w.Code != admittedStatus[rt.path] {
			e.t.Errorf("positive control %s: %s %s = %d %s, want %d", label, rt.method, rt.path, w.Code, w.Body.String(), admittedStatus[rt.path])
		}
	}
}

func withToken(tok string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

// ── positive control + response shape ────────────────────────────────────

func TestUpdatesAdminReachesEveryHandlerAndInstallStopsAtTheStub(t *testing.T) {
	e := newUpdEnv(t)
	e.expectAllAdmitted("admin")
	w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease)))
	if w.Code != http.StatusUnprocessableEntity || w.Body.String() != `{"error":"release not verified"}` {
		t.Fatalf("install = %d %s, want 422 release not verified", w.Code, w.Body.String())
	}
}

func TestUpdatesStatusNeverLeaksTheEnrollment(t *testing.T) {
	e := newUpdEnv(t)
	w := e.do("GET", "/api/updates", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "enrolled,install_enabled,manifest_verifier,worker_listening" {
		t.Fatalf("GET /api/updates keys = %v", keys)
	}
	if m["enrolled"] != true || m["install_enabled"] != false || m["manifest_verifier"] != "stub" || m["worker_listening"] != false {
		t.Fatalf("GET /api/updates = %s", w.Body.String())
	}
	key, _ := updateauth.LoadDeviceKey(e.dataDir)
	admin, _ := os.ReadFile(updateauth.AdminPath(e.dataDir))
	for _, secret := range []string{updAdminID, updAdminEmail, hex.EncodeToString(key), string(admin)} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("GET /api/updates leaks enrollment material")
		}
	}
}

// A verified manifest with no worker hand-off (M3) fails closed at 503 —
// the stage after the stub is not "install".
func TestInstallWithAVerifiedManifestButNoStarterIsUnavailable(t *testing.T) {
	e := newUpdEnv(t)
	e.s.SetUpdateVerifier(fakeVerifier{})
	w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("install = %d %s, want 503", w.Code, w.Body.String())
	}
}

type fakeVerifier struct{}

func (fakeVerifier) VerifiedManifest(id string) (updateauth.Manifest, error) {
	return updateauth.Manifest{ReleaseID: id}, nil
}

type spyVerifier struct {
	mu    sync.Mutex
	calls []string
}

func (v *spyVerifier) VerifiedManifest(id string) (updateauth.Manifest, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, id)
	return updateauth.Manifest{}, updateauth.ErrNoVerifiedManifest
}

// ── ordinary-user escalation ─────────────────────────────────────────────

func TestUpdatesRefuseAnOrdinaryUser(t *testing.T) {
	e := newUpdEnv(t)
	past := time.Now().Add(-time.Hour).UTC()
	if err := e.st.User().Create(&store.User{ID: updOtherID, Email: updOtherEmail, PasswordHash: "x", CreatedAt: past, UpdatedAt: past}); err != nil {
		t.Fatal(err)
	}
	other := mintJWT(t, updOtherID, updOtherEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), updSecret)
	e.expectAllForbidden("ordinary user", withToken(other))
	// the other user's id with the admin's email, and vice versa
	e.expectAllForbidden("admin email, other id", withToken(mintJWT(t, updOtherID, updAdminEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), updSecret)))
	e.expectAllForbidden("admin id, other email", withToken(mintJWT(t, updAdminID, updOtherEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), updSecret)))
	e.expectAllAdmitted("admin")
}

// F1: the Telegram agent's token carries the OWNER's user_id (email
// bot@internal) — minted by the production function.
func TestUpdatesRefuseTheTelegramBotToken(t *testing.T) {
	e := newUpdEnv(t)
	bot, err := agent.GenerateBotToken(updAdminID)
	if err != nil {
		t.Fatal(err)
	}
	e.expectAllForbidden("bot token", withToken(bot))
	e.expectAllAdmitted("admin")
}

// F1: the routes never enter the registry GetAPIDocs renders into the agent's
// system prompt, and no source line registers them through s.route.
func TestUpdateRoutesAreNotAdvertisedToTheAgent(t *testing.T) {
	e := newUpdEnv(t)
	docs := GetAPIDocs()
	// Positive control on an ORDINARY route: /api/reset-account (the old
	// control) is itself hidden from the agent since M3 red-team H1.
	if !strings.Contains(docs, "/api/my-traders") {
		t.Fatal("positive control: GetAPIDocs is empty — setupRoutes did not populate the registry")
	}
	if strings.Contains(docs, "/updates") {
		t.Fatal("GetAPIDocs advertises /api/updates to the Telegram agent")
	}
	found := 0
	for _, r := range e.s.router.Routes() {
		if strings.HasPrefix(r.Path, "/api/updates") {
			found++
		}
	}
	if found != 6 {
		t.Fatalf("router has %d /api/updates routes, want 6 (status, check, install, install-with-password, jobs, jobs/receipt)", found)
	}
	re := regexp.MustCompile(`s\.route(WithSchema)?\([^)]*"/updates`)
	for _, f := range []string{"server.go", "handler_updates.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if re.Match(src) {
			t.Fatalf("%s registers /updates through s.route/s.routeWithSchema", f)
		}
	}
}

// F2: while the process runs on the public default secret nothing is an
// identity — even a correctly signed admin token.
func TestUpdatesRefuseWhenTheJWTSecretIsTheInsecureDefault(t *testing.T) {
	e := newUpdEnv(t)
	e.expectAllAdmitted("real secret")
	auth.SetJWTSecret(config.InsecureDefaultJWTSecret)
	tok := mintJWT(t, updAdminID, updAdminEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), config.InsecureDefaultJWTSecret)
	e.expectAllForbidden("default secret", withToken(tok))
	auth.SetJWTSecret("")
	e.expectAllForbidden("empty secret", withToken(mintJWT(t, updAdminID, updAdminEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), "")))
}

func TestUpdatesRefuseMissingOrInvalidJWT(t *testing.T) {
	e := newUpdEnv(t)
	now := time.Now()
	cases := map[string]func(*http.Request){
		"no header":    func(r *http.Request) { r.Header.Del("Authorization") },
		"basic":        func(r *http.Request) { r.Header.Set("Authorization", "Basic "+e.tok) },
		"bare bearer":  func(r *http.Request) { r.Header.Set("Authorization", "Bearer") },
		"two headers":  func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+e.tok) },
		"garbage":      withToken("not.a.jwt"),
		"wrong secret": withToken(mintJWT(t, updAdminID, updAdminEmail, now.Add(-5*time.Second), now.Add(time.Hour), "some-other-secret")),
		"expired":      withToken(mintJWT(t, updAdminID, updAdminEmail, now.Add(-2*time.Hour), now.Add(-time.Hour), updSecret)),
		"no iat":       withToken(mintNoIAT(t, updAdminID, updAdminEmail, now.Add(time.Hour), updSecret)),
		"alg none": withToken(func() string {
			s, _ := jwt.NewWithClaims(jwt.SigningMethodNone, auth.Claims{UserID: updAdminID, Email: updAdminEmail, RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt: jwt.NewNumericDate(now.Add(-5 * time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}).SignedString(jwt.UnsafeAllowNoneSignatureType)
			return s
		}()),
	}
	for name, mut := range cases {
		e.expectAllForbidden(name, mut)
	}
	// blacklisted through the production logout route
	tok := mintJWT(t, updAdminID, updAdminEmail, now.Add(-3*time.Second), now.Add(time.Hour), updSecret)
	e.expectAllAdmitted("before logout", withToken(tok))
	r := httptest.NewRequest("POST", "/api/logout", nil)
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("logout = %d %s", w.Code, w.Body.String())
	}
	e.expectAllForbidden("blacklisted", withToken(tok))
}

// ── enrollment files: absent / unsafe ⇒ refuse, and requests never write ──

func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// SQLite's WAL index (-shm) is rewritten by READS (the gate's
		// users-store lookup); it holds no data. data.db and -wal are kept.
		if strings.HasSuffix(p, "-shm") {
			return nil
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return nil
		}
		st := fi.Sys().(*syscall.Stat_t)
		sig := fmt.Sprintf("%v|%d|%d|%d", fi.Mode(), fi.Size(), st.Ino, fi.ModTime().UnixNano())
		if fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			sum := sha256.Sum256(b)
			sig += "|" + hex.EncodeToString(sum[:])
		}
		out[p] = sig
		return nil
	})
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestUpdatesRefuseWhenNotEnrolledOrUnsafe(t *testing.T) {
	cases := map[string]func(t *testing.T, d string){
		"nothing enrolled": func(t *testing.T, d string) { _ = os.RemoveAll(updateauth.Dir(d)) },
		"admin absent":     func(t *testing.T, d string) { _ = os.Remove(updateauth.AdminPath(d)) },
		"key absent":       func(t *testing.T, d string) { _ = os.Remove(updateauth.DeviceKeyPath(d)) },
		"admin 0644":       func(t *testing.T, d string) { _ = os.Chmod(updateauth.AdminPath(d), 0o644) },
		"key 0640":         func(t *testing.T, d string) { _ = os.Chmod(updateauth.DeviceKeyPath(d), 0o640) },
		"dir 0755":         func(t *testing.T, d string) { _ = os.Chmod(updateauth.Dir(d), 0o755) },
		"key 31 bytes": func(t *testing.T, d string) {
			b, _ := os.ReadFile(updateauth.DeviceKeyPath(d))
			_ = os.WriteFile(updateauth.DeviceKeyPath(d), b[:31], 0o600)
		},
		"admin symlink": func(t *testing.T, d string) {
			b, _ := os.ReadFile(updateauth.AdminPath(d))
			cp := filepath.Join(d, "admin-copy.json")
			_ = os.WriteFile(cp, b, 0o600)
			_ = os.Remove(updateauth.AdminPath(d))
			_ = os.Symlink(cp, updateauth.AdminPath(d))
		},
		"key symlink": func(t *testing.T, d string) {
			b, _ := os.ReadFile(updateauth.DeviceKeyPath(d))
			cp := filepath.Join(d, "key-copy")
			_ = os.WriteFile(cp, b, 0o600)
			_ = os.Remove(updateauth.DeviceKeyPath(d))
			_ = os.Symlink(cp, updateauth.DeviceKeyPath(d))
		},
		"admin unknown field": func(t *testing.T, d string) {
			_ = os.WriteFile(updateauth.AdminPath(d), []byte(`{"user_id":"`+updAdminID+`","email":"`+updAdminEmail+`","enrolled_at":"2026-09-24T01:00:00Z","role":"x"}`), 0o600)
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			e := newUpdEnv(t)
			routes := e.allRoutes() // grants minted while enrolled (valid MACs)
			for _, rt := range routes {
				if w := e.do(rt.method, rt.path, rt.body); w.Code != admittedStatus[rt.path] {
					t.Fatalf("positive control %s = %d", rt.path, w.Code)
				}
			}
			mut(t, e.dataDir)
			before := snapshotTree(t, e.dataDir)
			routes = append(routes, updRoute{"POST", "/api/updates/install", routes[2].body})
			for _, rt := range routes {
				w := e.do(rt.method, rt.path, rt.body)
				if w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
					t.Errorf("%s %s = %d %s, want 403", rt.method, rt.path, w.Code, w.Body.String())
				}
			}
			if !sameTree(before, snapshotTree(t, e.dataDir)) {
				t.Error("a refused request created or modified a file under the data dir")
			}
		})
	}
}

// L4: the OFF state also covers an app whose data dir was never configured
// (trader.MaintenanceDataDir() == "") — every route refuses.
func TestUpdatesRefuseWhenTheDataDirIsUnconfigured(t *testing.T) {
	e := newUpdEnv(t)
	e.expectAllAdmitted("configured")
	trader.SetMaintenanceDataDir("")
	e.expectAllForbidden("unconfigured data dir")
	trader.SetMaintenanceDataDir("relative/data")
	e.expectAllForbidden("relative data dir")
}

// F3: the admin row must still carry the enrolled email; a row whose email
// drifted (or vanished) is not the enrolled admin, even for a token whose
// claims match admin.json.
func TestUpdatesRefuseWhenTheAdminRowNoLongerMatches(t *testing.T) {
	e := newUpdEnv(t)
	e.expectAllAdmitted("row matches")
	if err := e.st.GormDB().Exec(`UPDATE users SET email = ? WHERE id = ?`, "moved@example.test", updAdminID).Error; err != nil {
		t.Fatal(err)
	}
	e.expectAllForbidden("row email drifted")
	if err := e.st.GormDB().Exec(`UPDATE users SET email = ? WHERE id = ?`, updAdminEmail, updAdminID).Error; err != nil {
		t.Fatal(err)
	}
	e.expectAllAdmitted("row restored")
	if err := e.st.GormDB().Exec(`DELETE FROM users WHERE id = ?`, updAdminID).Error; err != nil {
		t.Fatal(err)
	}
	e.expectAllForbidden("row deleted")
}

// ── reset-account / password routes (F3, Q8) ─────────────────────────────

func TestResetAccountCannotTouchTheEnrollmentAndKillsTheAdminIdentity(t *testing.T) {
	e := newUpdEnv(t)
	t.Setenv("ALLOW_ACCOUNT_RESET", "1")
	e.expectAllAdmitted("before reset")
	before := snapshotTree(t, updateauth.Dir(e.dataDir))

	r := httptest.NewRequest("POST", "/api/reset-account", strings.NewReader(`{"confirm":"RESET-ALL-DATA"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	r.Header.Set("Authorization", "Bearer "+e.tok)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reset-account = %d %s", w.Code, w.Body.String())
	}
	adminRaw, _ := os.ReadFile(updateauth.AdminPath(e.dataDir))
	if strings.Contains(w.Body.String(), hex.EncodeToString(mustKey(t, e.dataDir))) || strings.Contains(w.Body.String(), strings.TrimSpace(string(adminRaw))) {
		t.Fatal("reset-account's response carries enrollment material")
	}
	after := snapshotTree(t, updateauth.Dir(e.dataDir))
	// the seen-job store and lock files legitimately changed from the
	// positive-control installs BEFORE the reset; compare the enrollment only
	for _, p := range []string{updateauth.AdminPath(e.dataDir), updateauth.DeviceKeyPath(e.dataDir)} {
		if before[p] == "" || before[p] != after[p] {
			t.Fatalf("reset-account changed %s", filepath.Base(p))
		}
	}
	// F3: the pre-reset admin JWT (still unexpired) no longer identifies the admin
	e.expectAllForbidden("pre-reset admin token")

	// takeover chain: register the SAME email after the reset ⇒ fresh uuid ⇒ not admin
	r = httptest.NewRequest("POST", "/api/register", strings.NewReader(`{"email":"`+updAdminEmail+`","password":"attacker-pass-1"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("register = %d %s", w.Code, w.Body.String())
	}
	var reg struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &reg)
	if reg.UserID == "" || reg.UserID == updAdminID {
		t.Fatalf("re-registered user id %q", reg.UserID)
	}
	e.expectAllForbidden("re-registered same email", withToken(reg.Token))
	final := snapshotTree(t, updateauth.Dir(e.dataDir))
	for _, p := range []string{updateauth.AdminPath(e.dataDir), updateauth.DeviceKeyPath(e.dataDir)} {
		if before[p] != final[p] {
			t.Fatalf("register changed %s", filepath.Base(p))
		}
	}
}

// Q8: a token issued before the user row last changed (password change) is
// stale for updates; a token issued after it passes — once the owner has
// re-enrolled. (Changed for the M3 red-team H1/H2 belt: the password change
// now also UN-ENROLLS, so the fresh token is refused until `enroll
// --replace`; the password routes still never touch the enrollment FILES.
// Was: TestPasswordChangeRetiresOlderTokensForUpdatesButKeepsTheEnrollment,
// which admitted the fresh token straight after the change.)
func TestPasswordChangeRetiresOlderTokensAndUnbindsTheEnrollment(t *testing.T) {
	e := newUpdEnv(t)
	e.expectAllAdmitted("old token before the change")
	before := snapshotTree(t, updateauth.Dir(e.dataDir))

	r := httptest.NewRequest("PUT", "/api/user/password", strings.NewReader(`{"current_password":"`+updAdminPass+`","new_password":"another-long-pass"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	r.Header.Set("Authorization", "Bearer "+e.tok)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("change password = %d %s", w.Code, w.Body.String())
	}
	e.expectAllForbidden("token older than the password change")

	fresh := mintJWT(t, updAdminID, updAdminEmail, time.Now().Add(2*time.Second), time.Now().Add(time.Hour), updSecret)
	e.expectAllForbidden("token issued after the change, enrollment bound to the old password", withToken(fresh))

	// the disabled public reset route: 410, enrollment untouched
	r = httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"email":"`+updAdminEmail+`","new_password":"x-x-x-x-x-x"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	if w.Code != http.StatusGone {
		t.Fatalf("reset-password = %d", w.Code)
	}
	after := snapshotTree(t, updateauth.Dir(e.dataDir))
	for _, p := range []string{updateauth.AdminPath(e.dataDir), updateauth.DeviceKeyPath(e.dataDir)} {
		if before[p] == "" || before[p] != after[p] {
			t.Fatalf("a password route changed %s", filepath.Base(p))
		}
	}

	// The owner re-enrolls (the CLI's writer, --replace, the row's current
	// hash): the fresh token is admitted, and Q8 still refuses the old one.
	u, err := e.st.User().GetByID(updAdminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := updateauth.Enroll(e.dataDir, updAdminID, updAdminEmail, u.PasswordHash, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	e.expectAllAdmitted("token issued after the change, after re-enrolling", withToken(fresh))
	e.expectAllForbidden("token older than the password change, after re-enrolling")
}

// ── CSRF / cross-origin / transport ──────────────────────────────────────

func TestUpdatesRequireTheUpdateHeader(t *testing.T) {
	e := newUpdEnv(t)
	for name, mut := range map[string]func(*http.Request){
		"missing":     func(r *http.Request) { r.Header.Del(UpdateHeader) },
		"zero":        func(r *http.Request) { r.Header.Set(UpdateHeader, "0") },
		"true":        func(r *http.Request) { r.Header.Set(UpdateHeader, "true") },
		"trailing sp": func(r *http.Request) { r.Header.Set(UpdateHeader, "1 ") },
		"two values":  func(r *http.Request) { r.Header.Add(UpdateHeader, "1") },
		"empty":       func(r *http.Request) { r.Header.Set(UpdateHeader, "") },
	} {
		e.expectAllForbidden("header "+name, mut)
	}
	e.expectAllAdmitted("header 1")
}

func TestUpdatesRefuseCrossOrigin(t *testing.T) {
	e := newUpdEnv(t)
	for _, o := range []string{"http://evil.test", "null", "http://127.0.0.1:3000", "https://127.0.0.1:8080", "http://localhost:8080", "http://127.0.0.1:8080/"} {
		o := o
		e.expectAllForbidden("origin "+o, func(r *http.Request) { r.Header.Set("Origin", o) })
	}
	e.expectAllForbidden("two origins", func(r *http.Request) {
		r.Header.Add("Origin", "http://127.0.0.1:8080")
		r.Header.Add("Origin", "http://127.0.0.1:8080")
	})
	e.expectAllForbidden("sec-fetch-site cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	e.expectAllForbidden("sec-fetch-site same-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") })
	// positive controls
	e.expectAllAdmitted("same origin", func(r *http.Request) {
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	e.expectAllAdmitted("no origin")
}

func TestUpdatesRefuseDNSRebinding(t *testing.T) {
	e := newUpdEnv(t)
	for _, h := range []string{"evil.test:8080", "evil.test", "127.0.0.1.evil.test:8080", "127.0.0.1:8080@evil.test", "0.0.0.0:8080", "192.168.1.5:8080", ""} {
		h := h
		e.expectAllForbidden("host "+h, func(r *http.Request) {
			r.Host = h
			r.Header.Set("Origin", "http://"+h) // "same-origin" by a naive Origin==Host check
		})
	}
	for _, h := range []string{"localhost:8080", "[::1]:8080", "127.0.0.1", "LOCALHOST:8080"} {
		h := h
		e.expectAllAdmitted("host "+h, func(r *http.Request) { r.Host = h })
	}
}

func TestUpdatesRefuseANonLoopbackPeerWhateverXFFSays(t *testing.T) {
	e := newUpdEnv(t)
	for _, peer := range []string{"192.168.1.5:4000", "10.0.0.2:1", "[fe80::1]:5", "garbage"} {
		peer := peer
		e.expectAllForbidden("peer "+peer, func(r *http.Request) {
			r.RemoteAddr = peer
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("X-Real-IP", "127.0.0.1")
		})
	}
	e.expectAllAdmitted("ipv6 loopback peer", func(r *http.Request) { r.RemoteAddr = "[::1]:4000" })
}

// F5: the global CORS preflight never allows the update header, so a
// cross-origin page cannot make a browser send it.
func TestUpdatePreflightNeverAllowsTheUpdateHeader(t *testing.T) {
	e := newUpdEnv(t)
	for _, p := range []string{"/api/updates", "/api/updates/install", "/api/updates/check", "/api/updates/jobs/0123456789abcdef"} {
		r := httptest.NewRequest("OPTIONS", p, nil)
		r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
		r.Header.Set("Origin", "http://evil.test")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "x-vl-update,authorization,content-type")
		w := httptest.NewRecorder()
		e.s.router.ServeHTTP(w, r)
		allow := strings.ToLower(strings.Join(w.Header().Values("Access-Control-Allow-Headers"), ","))
		if allow == "" {
			t.Fatalf("%s: positive control: the CORS middleware did not answer the preflight", p)
		}
		if strings.Contains(allow, "x-vl-update") || strings.Contains(allow, "*") {
			t.Fatalf("%s: preflight allows the update header: %q", p, allow)
		}
	}
}

// ── replay ───────────────────────────────────────────────────────────────

func TestInstallJobIDIsSingleUseAcrossRestart(t *testing.T) {
	e := newUpdEnv(t)
	body := grantBody(e.grant(updRelease))
	if w := e.do("POST", "/api/updates/install", body); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first use = %d", w.Code)
	}
	w := e.do("POST", "/api/updates/install", body)
	if w.Code != http.StatusConflict || w.Body.String() != `{"error":"job already used"}` {
		t.Fatalf("replay = %d %s, want 409", w.Code, w.Body.String())
	}
	// a restarted app over the same installation still refuses the replay
	e.s = NewServer(manager.NewTraderManager(), e.st, nil, "127.0.0.1", 0)
	if w := e.do("POST", "/api/updates/install", body); w.Code != http.StatusConflict {
		t.Fatalf("replay after restart = %d, want 409", w.Code)
	}
	// positive control: a fresh grant still reaches the stub
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("fresh grant = %d", w.Code)
	}
}

func TestConcurrentDoubleSubmitConsumesOnce(t *testing.T) {
	e := newUpdEnv(t)
	body := grantBody(e.grant(updRelease))
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = e.do("POST", "/api/updates/install", body).Code
		}(i)
	}
	wg.Wait()
	n422, n409 := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusUnprocessableEntity:
			n422++
		case http.StatusConflict:
			n409++
		}
	}
	if n422 != 1 || n409 != len(codes)-1 {
		t.Fatalf("codes %v: want exactly one 422 and the rest 409", codes)
	}
}

func TestCorruptSeenStoreFailsClosedAndIsNotReset(t *testing.T) {
	e := newUpdEnv(t)
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("positive control = %d", w.Code)
	}
	garbage := []byte("{ not json")
	g := e.grant(updRelease) // minted before the corruption: the minter refuses over a corrupt store (red-3 #5)
	if err := os.WriteFile(updateauth.SeenPath(e.dataDir), garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	w := e.do("POST", "/api/updates/install", grantBody(g))
	if w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
		t.Fatalf("corrupt store = %d %s, want 403", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(updateauth.SeenPath(e.dataDir)); !bytes.Equal(b, garbage) {
		t.Fatal("the corrupt seen store was rewritten")
	}
}

// ── expiry window ────────────────────────────────────────────────────────

func TestInstallExpiryWindow(t *testing.T) {
	e := newUpdEnv(t)
	now := time.Now()
	e.s.updatesNow = func() time.Time { return now }
	key, err := updateauth.LoadDeviceKey(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	job := 0
	body := func(exp int64) string {
		job++
		id := fmt.Sprintf("expiry-job-%06d", job)
		mac, err := updateauth.ComputeMAC(key, updAdminID, updRelease, id, exp)
		if err != nil {
			t.Fatal(err)
		}
		return grantBody(updateauth.Grant{ReleaseID: updRelease, JobID: id, ExpiresAt: exp, HMAC: mac})
	}
	n := now.Unix()
	for _, c := range []struct {
		name string
		exp  int64
		want int
	}{
		{"expired", n - 1, http.StatusForbidden},
		{"expires now", n, http.StatusForbidden},
		{"one second past the window", n + 301, http.StatusForbidden},
		{"a day out", n + 86400, http.StatusForbidden},
		{"inside", n + 1, http.StatusUnprocessableEntity},
		{"window edge (inclusive)", n + 300, http.StatusUnprocessableEntity},
	} {
		if w := e.do("POST", "/api/updates/install", body(c.exp)); w.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, w.Code, w.Body.String(), c.want)
		}
	}
	// non-canonical encodings of expires_at are malformed (400), not aliases
	mac := strings.Repeat("a", 64)
	for _, raw := range []string{`"` + fmt.Sprint(n+60) + `"`, fmt.Sprint(n+60) + ".0", "1.8e9", "-5", "0" + fmt.Sprint(n+60)} {
		b := `{"release_id":"` + updRelease + `","job_id":"expiry-raw-0001","expires_at":` + raw + `,"hmac":"` + mac + `"}`
		if w := e.do("POST", "/api/updates/install", b); w.Code != http.StatusBadRequest {
			t.Errorf("expires_at %s: %d, want 400", raw, w.Code)
		}
	}
}

// ── bad HMAC ─────────────────────────────────────────────────────────────

func TestInstallRefusesABadMACAndDoesNotSpendTheJob(t *testing.T) {
	e := newUpdEnv(t)
	g := e.grant(updRelease)
	flip := []byte(g.HMAC)
	if flip[0] == '0' {
		flip[0] = '1'
	} else {
		flip[0] = '0'
	}
	otherKey := make([]byte, 32) // non-degenerate (a degenerate key cannot mint)
	for i := range otherKey {
		otherKey[i] = byte(9 + i)
	}
	wrongKeyMAC, _ := updateauth.ComputeMAC(otherKey, updAdminID, g.ReleaseID, g.JobID, g.ExpiresAt)
	m := hmac.New(sha256.New, mustKey(t, e.dataDir))
	fmt.Fprintf(m, "%s|%s|%s|%s|%d", updateauth.MACPurpose, updAdminID, g.JobID, g.ReleaseID, g.ExpiresAt)
	reordered := hex.EncodeToString(m.Sum(nil))
	for name, mac := range map[string]string{
		"flipped":   string(flip),
		"wrong key": wrongKeyMAC,
		"uppercase": strings.ToUpper(g.HMAC),
		"truncated": g.HMAC[:63],
		"empty":     "",
		"reordered": reordered,
		"other job": func() string {
			x, _ := updateauth.ComputeMAC(mustKey(t, e.dataDir), updAdminID, g.ReleaseID, "another-job-0001", g.ExpiresAt)
			return x
		}(),
	} {
		bad := g
		bad.HMAC = mac
		w := e.do("POST", "/api/updates/install", grantBody(bad))
		if w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
			t.Errorf("%s: %d %s, want 403", name, w.Code, w.Body.String())
		}
	}
	// the job id was not spent by the refusals
	if w := e.do("POST", "/api/updates/install", grantBody(g)); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("valid MAC after refusals = %d, want 422 (job not consumed)", w.Code)
	}
}

func mustKey(t *testing.T, d string) []byte {
	t.Helper()
	k, err := updateauth.LoadDeviceKey(d)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// ── path injection ───────────────────────────────────────────────────────

func TestInstallRefusesPathShapedIDsEvenWithAValidMAC(t *testing.T) {
	e := newUpdEnv(t)
	spy := &spyVerifier{}
	e.s.SetUpdateVerifier(spy)
	exp := time.Now().Add(2 * time.Minute).Unix()
	before := snapshotTree(t, filepath.Dir(e.dataDir))
	for _, rel := range []string{"../../etc/passwd", "..", "/abs", "a/b", `a\b`, "https://x/y", "file:x", "%2e%2e", "a\x00b",
		strings.Repeat("a", 65), ".hidden", "a|b", "$(id)", "`id`", "v1;rm -rf"} {
		if w := e.do("POST", "/api/updates/install", e.rawMACBody(rel, "0123456789abcdef", exp)); w.Code != http.StatusBadRequest {
			t.Errorf("release_id %q: %d %s, want 400", rel, w.Code, w.Body.String())
		}
	}
	for _, job := range []string{"../../../x", "a/b-000000", "a|b-000000", "ABCDEF01234", strings.Repeat("a", 65)} {
		if w := e.do("POST", "/api/updates/install", e.rawMACBody(updRelease, job, exp)); w.Code != http.StatusBadRequest {
			t.Errorf("job_id %q: %d, want 400", job, w.Code)
		}
	}
	if len(spy.calls) != 0 {
		t.Fatalf("the verifier was called for a malformed id: %q", spy.calls)
	}
	if !sameTree(before, snapshotTree(t, filepath.Dir(e.dataDir))) {
		t.Fatal("a refused install created or modified a file")
	}
	// positive control: a well-formed id reaches the verifier exactly once
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("positive control = %d", w.Code)
	}
	if len(spy.calls) != 1 || spy.calls[0] != updRelease {
		t.Fatalf("verifier calls %q", spy.calls)
	}
}

func TestJobRoutesNeverServeAFile(t *testing.T) {
	e := newUpdEnv(t)
	const secret = "TOP-SECRET-SENTINEL-9f1c"
	_ = os.WriteFile(filepath.Join(updateauth.Dir(e.dataDir), "secret.json"), []byte(`{"s":"`+secret+`"}`), 0o600)
	_ = os.WriteFile(filepath.Join(e.dataDir, "secret.json"), []byte(`{"s":"`+secret+`"}`), 0o600)
	for _, p := range []string{
		"/api/updates/jobs/secret", "/api/updates/jobs/secret.json", "/api/updates/jobs/..%2fsecret.json",
		"/api/updates/jobs/..%2f..%2fsecret.json", "/api/updates/jobs/%2e%2e", "/api/updates/jobs/" + strings.Repeat("a", 300),
		"/api/updates/jobs/..%2fsecret.json/receipt", "/api/updates/jobs/device.key", "/api/updates/jobs/admin.json/receipt",
	} {
		w := e.do("GET", p, "")
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), updAdminEmail) {
			t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
		}
	}
}

// ── nothing sensitive in the logs ────────────────────────────────────────

func TestUpdatesNeverLogTheTokenMACOrKey(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	sink := writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) })
	prevGin := gin.DefaultWriter
	gin.DefaultWriter = sink // gin.Default()'s Logger binds it at NewServer
	logger.Log.SetOutput(sink)
	t.Cleanup(func() { gin.DefaultWriter = prevGin; logger.Log.SetOutput(os.Stdout) })

	e := newUpdEnv(t)
	g := e.grant(updRelease)
	bad := g
	bad.HMAC = strings.Repeat("b", 64)
	e.do("POST", "/api/updates/install", grantBody(bad))                            // 403 MAC
	e.do("POST", "/api/updates/install", grantBody(g))                              // 422
	e.do("POST", "/api/updates/install", grantBody(g))                              // 409
	e.do("GET", "/api/updates", "", withToken(mintJWT(t, updOtherID, updOtherEmail, // 403 identity
		time.Now().Add(-time.Second), time.Now().Add(time.Hour), updSecret)))
	e.do("POST", "/api/updates/install", "{bad json "+g.HMAC)

	mu.Lock()
	logs := buf.String()
	mu.Unlock()
	if !strings.Contains(logs, "[updates]") {
		t.Fatal("positive control: no update log line captured")
	}
	key := mustKey(t, e.dataDir)
	for name, s := range map[string]string{"token": e.tok, "mac": g.HMAC, "bad mac": bad.HMAC, "key": hex.EncodeToString(key), "raw key": string(key)} {
		if strings.Contains(logs, s) {
			t.Errorf("the logs contain the %s", name)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

var _ io.Writer = writerFunc(nil)
