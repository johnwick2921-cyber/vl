package api

// W-FRIEND-HOSTED Segment 6 (DS-106): the owner Friends page API through the
// production router. Each test pins one B2 / S-item contract:
//
//	TestOwnerFriends404InFriendMode           B2 — the owner API is 404 on a friend bot
//	TestOwnerFriendsOwnerOnly                 B2 — owner JWT only (403 for a second account)
//	TestOwnerFriendsListEmpty                 canonical [] when no registry
//	TestOwnerFriendsAddCallsProvisionByArgv   fixed argv (no shell); install code once
//	TestOwnerFriendsRevokeSurfacesTailscale   remove.sh + Tailscale "api_key_missing" (no silent skip)
//	TestOwnerFriendsRotateReKeysOnce          S5/S10 — new key lands in .env, only SHA-256 in the registry
//	TestOwnerFriendsActionRateLimited         S10 — Add / rotate rate limited
//	TestOwnerFriendsListProbesLiveStatus      live fields from the friend's own /api/health

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// testSystemctlCalls records every (verb unit) pair the S22 recording fake saw.
var testSystemctlCalls []string

func friendCall(t *testing.T, e *updEnv, method, path, body, tok string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr, r.Host = "127.0.0.1:52000", "127.0.0.1:8080"
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.s.router.ServeHTTP(w, r)
	return w
}

func friendTestEnv(t *testing.T, e *updEnv) {
	t.Helper()
	resetFriendRateLimiterForTest()
	t.Cleanup(resetFriendRateLimiterForTest)
	resetFriendAlertStateForTest()
	t.Cleanup(resetFriendAlertStateForTest)
	t.Setenv("VL_FRIENDS_ROOT", t.TempDir())
	t.Setenv("VL_FRIENDS_SCRIPTS_DIR", t.TempDir())
	t.Setenv("VL_FRIEND_MONITOR_OFF", "1")
	t.Setenv("TUNNEL_IP", "")
	t.Setenv("TAILSCALE_API_KEY", "")
	// m386: the Friends page is opt-in. Every friends-page test opts IN here;
	// the dedicated off-test sets VL_FRIENDS_PAGE="" itself.
	t.Setenv("VL_FRIENDS_PAGE", "1")
	// S22: a friend test never touches the real service manager — install a
	// recording fake (the production default is restored on cleanup).
	prevSystemctl := systemctlUser
	systemctlUser = func(verb, unit string) error {
		testSystemctlCalls = append(testSystemctlCalls, verb+" "+unit)
		return nil
	}
	t.Cleanup(func() { systemctlUser = prevSystemctl; testSystemctlCalls = nil })
	// U12: pin TUNNEL_IP as valid without a real tunnel interface.
	prevTunnel := tunnelIPOK
	tunnelIPOK = func() (string, bool) { return "100.64.0.7", true }
	t.Cleanup(func() { tunnelIPOK = prevTunnel })
}

func writeFriendScript(t *testing.T, e *updEnv, name, content string) string {
	t.Helper()
	p := filepath.Join(os.Getenv("VL_FRIENDS_SCRIPTS_DIR"), name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return p
}

func writeRegistry(t *testing.T, rows string) {
	t.Helper()
	p := filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "friends.json")
	if err := os.WriteFile(p, []byte(rows), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func TestOwnerFriends404InFriendMode(t *testing.T) {
	e := newUpdEnv(t)
	t.Setenv("VL_FRIEND_INSTANCE", "1")
	t.Setenv("VL_FRIENDS_ROOT", t.TempDir())
	t.Setenv("VL_FRIEND_MONITOR_OFF", "1")

	if w := friendCall(t, e, "GET", "/api/owner/friends", "", e.tok); w.Code != http.StatusNotFound {
		t.Fatalf("friend mode: want 404, got %d (%s)", w.Code, w.Body.String())
	}
	if w := friendCall(t, e, "POST", "/api/owner/friends", `{"name":"tom"}`, e.tok); w.Code != http.StatusNotFound {
		t.Fatalf("friend mode add: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestOwnerFriendsOwnerOnly(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	secondTok := seedSecondUser(t, e)

	if w := friendCall(t, e, "GET", "/api/owner/friends", "", secondTok); w.Code != http.StatusForbidden {
		t.Fatalf("non-owner list: want 403, got %d (%s)", w.Code, w.Body.String())
	}
	if w := friendCall(t, e, "POST", "/api/owner/friends", `{"name":"tom"}`, secondTok); w.Code != http.StatusForbidden {
		t.Fatalf("non-owner add: want 403, got %d (%s)", w.Code, w.Body.String())
	}
	if w := friendCall(t, e, "GET", "/api/owner/friends", "", e.tok); w.Code != http.StatusOK {
		t.Fatalf("owner list: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestOwnerFriendsListEmpty(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	w := friendCall(t, e, "GET", "/api/owner/friends", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Friends         []json.RawMessage `json:"friends"`
		TailscaleKeySet bool              `json:"tailscale_key_set"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v (%s)", err, w.Body.String())
	}
	if resp.Friends == nil || len(resp.Friends) != 0 {
		t.Fatalf("want friends == [], got %v", resp.Friends)
	}
	if resp.TailscaleKeySet {
		t.Fatalf("tailscale_key_set must be false when TAILSCALE_API_KEY is unset")
	}
}

// TestOwnerFriendsAddCallsProvisionByArgv proves the add handler invokes
// provision.sh with a FIXED argv (name as a single argument, no shell string)
// and returns the install code once.
func TestOwnerFriendsAddCallsProvisionByArgv(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)

	writeFriendScript(t, e, "provision.sh", `#!/bin/bash
set -euo pipefail
# Record the exact argv so the test can prove no shell string building happened.
printf '%s|%s|%s' "$#" "$1" "${2:-}" > "$VL_FRIENDS_ROOT/argv.txt"
# Write a registry row for the new friend.
cat > "$VL_FRIENDS_ROOT/friends.json" <<JSON
[{"name":"$1","web_port":18081,"nt_port":36981,"key_sha256":"abc","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]
JSON
echo "install-code-for-$1"
`)

	w := friendCall(t, e, "POST", "/api/owner/friends", `{"name":"tom"}`, e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("add: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Friend struct {
			Name string `json:"name"`
		} `json:"friend"`
		InstallCode string `json:"install_code"`
		ShownOnce   bool   `json:"shown_once"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if resp.Friend.Name != "tom" {
		t.Fatalf("friend name = %q, want tom", resp.Friend.Name)
	}
	if resp.InstallCode != "install-code-for-tom" || !resp.ShownOnce {
		t.Fatalf("install code = %q (shown_once=%v)", resp.InstallCode, resp.ShownOnce)
	}

	argv, err := os.ReadFile(filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "argv.txt"))
	if err != nil {
		t.Fatalf("argv marker: %v", err)
	}
	if string(argv) != "1|tom|" {
		t.Fatalf("provision.sh argv = %q, want exactly one arg 'tom' (no shell splitting)", string(argv))
	}
}

func TestOwnerFriendsAddRejectsEmptyName(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	if w := friendCall(t, e, "POST", "/api/owner/friends", `{"name":"  "}`, e.tok); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name: want 400, got %d", w.Code)
	}
}

// TestOwnerFriendsRevokeSurfacesTailscale pins S11/B2: revoke runs remove.sh and
// then reports the Tailscale step — "api_key_missing" when TAILSCALE_API_KEY is
// unset (never a silent skip).
func TestOwnerFriendsRevokeSurfacesTailscale(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"abc","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":"dev123"}]`)
	if err := os.MkdirAll(filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFriendScript(t, e, "remove.sh", `#!/bin/bash
set -euo pipefail
mkdir -p "$VL_FRIENDS_ROOT/trash"
mv "$VL_FRIENDS_ROOT/$1" "$VL_FRIENDS_ROOT/trash/$1"
echo "moved"
`)

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/revoke", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Name      string `json:"name"`
		State     string `json:"state"`
		Tailscale string `json:"tailscale"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if resp.Name != "tom" || resp.State != "revoked" {
		t.Fatalf("revoke result = %+v", resp)
	}
	if resp.Tailscale != "api_key_missing" {
		t.Fatalf("tailscale = %q, want api_key_missing (surfaced, never silent)", resp.Tailscale)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("VL_FRIENDS_ROOT"), "trash", "tom")); err != nil {
		t.Fatalf("remove.sh must MOVE the folder to trash: %v", err)
	}
}

// TestOwnerFriendsRotateReKeysOnce pins S5/S10: the new key is written ONLY to
// the friend's .env, the registry stores only its SHA-256, and the returned
// install code carries the new key exactly once.
func TestOwnerFriendsRotateReKeysOnce(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	root := os.Getenv("VL_FRIENDS_ROOT")
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"oldsha","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)
	if err := os.MkdirAll(filepath.Join(root, "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(root, "tom", ".env")
	if err := os.WriteFile(envPath, []byte("JWT_SECRET=x\nNT_CONNECT_KEY=oldkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		InstallCode string `json:"install_code"`
		ShownOnce   bool   `json:"shown_once"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if !resp.ShownOnce || resp.InstallCode == "" {
		t.Fatalf("rotate must return the install code once")
	}

	// The .env carries the new key; the old line is gone.
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envBytes), "oldkey") {
		t.Fatalf(".env still carries the old key: %q", string(envBytes))
	}
	if !strings.Contains(string(envBytes), "NT_CONNECT_KEY=") {
		t.Fatalf(".env lost the connect key line: %q", string(envBytes))
	}
	newKey := strings.TrimPrefix(mustEnvConnectKey(t, string(envBytes)), "NT_CONNECT_KEY=")

	// The registry stores only the SHA-256 of the new key.
	rows, err := loadFriends()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(newKey))
	if rows[0].KeySHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("registry key_sha256 = %q, want sha256(%q)", rows[0].KeySHA256, newKey)
	}

	// The install code (base64url JSON) carries the same new key.
	raw, err := base64RawURLDecode(resp.InstallCode)
	if err != nil {
		t.Fatalf("install code not base64url: %v", err)
	}
	var ic map[string]interface{}
	if err := json.Unmarshal(raw, &ic); err != nil {
		t.Fatalf("install code not JSON: %v", err)
	}
	if ic["connect_key"] != newKey {
		t.Fatalf("install code connect_key = %v, want %q", ic["connect_key"], newKey)
	}
}

// TestOwnerFriendsRotateRestartsUnit — S22: after a successful rotate, the owner
// API restarts the friend unit so the rotated NT_CONNECT_KEY is re-read and the
// OLD key is refused on the next hello. The restart is the DS-102-scoped half;
// the key check itself is the connect-key server's (DS-104).
func TestOwnerFriendsRotateRestartsUnit(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	root := os.Getenv("VL_FRIENDS_ROOT")
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"oldsha","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)
	if err := os.MkdirAll(filepath.Join(root, "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tom", ".env"), []byte("JWT_SECRET=x\nNT_CONNECT_KEY=oldkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	testSystemctlCalls = nil
	w := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	got := append([]string(nil), testSystemctlCalls...)
	if len(got) != 1 || got[0] != "restart "+friendUnit("tom") {
		t.Fatalf("systemctl calls = %v, want exactly [%q]", got, "restart "+friendUnit("tom"))
	}
}

// TestOwnerFriendsPauseDisablesResumeEnables — U3: pause must survive a reboot
// (stop + disable), resume is enable --now (enable + start).
func TestOwnerFriendsPauseDisablesResumeEnables(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"x","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)

	testSystemctlCalls = nil
	if w := friendCall(t, e, "POST", "/api/owner/friends/tom/pause", "", e.tok); w.Code != http.StatusOK {
		t.Fatalf("pause: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := append([]string(nil), testSystemctlCalls...)
	if len(got) != 2 || got[0] != "stop "+friendUnit("tom") || got[1] != "disable "+friendUnit("tom") {
		t.Fatalf("pause systemctl calls = %v, want [stop, disable] for %s", got, friendUnit("tom"))
	}

	testSystemctlCalls = nil
	if w := friendCall(t, e, "POST", "/api/owner/friends/tom/resume", "", e.tok); w.Code != http.StatusOK {
		t.Fatalf("resume: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got = append([]string(nil), testSystemctlCalls...)
	if len(got) != 2 || got[0] != "enable "+friendUnit("tom") || got[1] != "start "+friendUnit("tom") {
		t.Fatalf("resume systemctl calls = %v, want [enable, start] for %s", got, friendUnit("tom"))
	}
}

func mustEnvConnectKey(t *testing.T, env string) string {
	t.Helper()
	for _, ln := range strings.Split(env, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "NT_CONNECT_KEY=") {
			return strings.TrimSpace(ln)
		}
	}
	t.Fatalf("no NT_CONNECT_KEY line in %q", env)
	return ""
}

func TestOwnerFriendsActionRateLimited(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)
	root := os.Getenv("VL_FRIENDS_ROOT")
	writeRegistry(t, `[{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"oldsha","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)
	if err := os.MkdirAll(filepath.Join(root, "tom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tom", ".env"), []byte("NT_CONNECT_KEY=k\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	first := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if first.Code != http.StatusOK {
		t.Fatalf("first rotate: want 200, got %d (%s)", first.Code, wBody(first))
	}
	second := friendCall(t, e, "POST", "/api/owner/friends/tom/rotate", "", e.tok)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second rotate within the gap: want 429, got %d (%s)", second.Code, wBody(second))
	}
}

// TestOwnerFriendsListProbesLiveStatus pins that the live fields come from the
// friend's OWN /api/health, not from anywhere in the owner's process.
func TestOwnerFriendsListProbesLiveStatus(t *testing.T) {
	e := newUpdEnv(t)
	friendTestEnv(t, e)

	// A fake friend bot serving /api/health on an ephemeral port.
	friendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"degraded","revision":"v-friend-1","nt8":{"t1":{"link":"up","last_bar_age_ms":1200}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer friendSrv.Close()

	u, err := url.Parse(friendSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUNNEL_IP", host)
	writeRegistry(t, `[{"name":"tom","web_port":`+strconv.Itoa(port)+`,"nt_port":36981,"key_sha256":"abc","state":"active","created_at":"2026-10-18T00:00:00Z","tailscale_device_id":""}]`)

	w := friendCall(t, e, "GET", "/api/owner/friends", "", e.tok)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Friends []struct {
			Name         string `json:"name"`
			BotUp        bool   `json:"bot_up"`
			NTConnected  bool   `json:"nt_connected"`
			Revision     string `json:"revision"`
			LastBarAgeMs *int64 `json:"last_bar_age_ms"`
		} `json:"friends"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v (%s)", err, w.Body.String())
	}
	if len(resp.Friends) != 1 {
		t.Fatalf("want 1 friend, got %d", len(resp.Friends))
	}
	f := resp.Friends[0]
	if !f.BotUp || !f.NTConnected || f.Revision != "v-friend-1" {
		t.Fatalf("live fields not probed: %+v", f)
	}
	if f.LastBarAgeMs == nil || *f.LastBarAgeMs != 1200 {
		t.Fatalf("last_bar_age_ms = %v, want 1200", f.LastBarAgeMs)
	}
}

func base64RawURLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func wBody(w *httptest.ResponseRecorder) string { return w.Body.String() }
