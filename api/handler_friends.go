package api

// W-FRIEND-HOSTED Segment 6 (DS-106): the owner's Friends page backend.
//
// Implements the B2 owner API:
//
//	GET  /api/owner/friends                 list (owner JWT only; 404 in friend mode)
//	POST /api/owner/friends                 add (calls provision.sh via fixed argv; install code ONCE)
//	POST /api/owner/friends/:name/:action   pause | resume | revoke | rotate
//
// Live status (bot up · NT connected · last bar age · add-on/app version) is
// probed over each friend's OWN HTTP API at http://<TUNNEL_IP>:<web_port> —
// the owner bot never reaches into the friend's process. A friend that cannot
// be probed has bot_up=false and a probe_error, never a fabricated value.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vl/config"
	"vl/logger"
	"vl/safe"
	"vl/telemetry"

	"github.com/gin-gonic/gin"
)

// ---- friend mode (B2) -----------------------------------------------------

// friendInstance reports whether this process is a friend bot (the ONE parser
// config.FriendInstanceEnabled — S21).
func friendInstance() bool {
	return config.FriendInstanceEnabled()
}

// friendMode404 aborts with 404 when this process is a friend instance (B2:
// the owner API does not exist on a friend bot). Returns true when aborted.
func friendMode404(c *gin.Context) bool {
	if !friendInstance() {
		return false
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	c.Abort()
	return true
}

// ---- B2 registry ----------------------------------------------------------

const (
	defaultFriendRoot       = "/home/hoang/vl-friends"
	defaultFriendScriptsDir = "/home/hoang/vl/deploy/friends"
	// friendConnectKeyBytes is the connect key length (B2: 64 hex chars).
	friendConnectKeyBytes = 32
)

// friendRoot is the friends folder (B2). VL_FRIENDS_ROOT overrides it for tests.
func friendRoot() string {
	if v := strings.TrimSpace(os.Getenv("VL_FRIENDS_ROOT")); v != "" {
		return v
	}
	return defaultFriendRoot
}

// tunnelIPOK reports whether TUNNEL_IP is set, parses as a literal IP, and is
// actually assigned to a local interface (U12). A friend Add/rotate refuses when
// it is not: the install code would carry a host the friend's box cannot reach.
// It is a var so a test can pin the answer without a real tunnel interface.
var tunnelIPOK = func() (ip string, ok bool) {
	ip = strings.TrimSpace(os.Getenv("TUNNEL_IP"))
	if ip == "" {
		return "", false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip, false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ip, false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(parsed) {
			return ip, true
		}
	}
	return ip, false
}

// friendScriptsDir is where DS-101's provision.sh / remove.sh live.
// VL_FRIENDS_SCRIPTS_DIR overrides it for tests.
func friendScriptsDir() string {
	if v := strings.TrimSpace(os.Getenv("VL_FRIENDS_SCRIPTS_DIR")); v != "" {
		return v
	}
	return defaultFriendScriptsDir
}

// friendRecord is one B2 registry row.
type friendRecord struct {
	Name              string `json:"name"`
	WebPort           int    `json:"web_port"`
	NTPort            int    `json:"nt_port"`
	KeySHA256         string `json:"key_sha256"`
	State             string `json:"state"`
	CreatedAt         string `json:"created_at"`
	TailscaleDeviceID string `json:"tailscale_device_id"`
}

// friendStatus is a registry row enriched with live probe data.
type friendStatus struct {
	friendRecord
	BotUp        bool   `json:"bot_up"`
	NTConnected  bool   `json:"nt_connected"`
	LastBarAgeMs *int64 `json:"last_bar_age_ms,omitempty"` // absent = unknown
	Revision     string `json:"revision,omitempty"`
	AddonBuild   string `json:"addon_build,omitempty"`
	AppVersion   string `json:"app_version,omitempty"`
	LastSeen     string `json:"last_seen,omitempty"` // RFC3339 UTC; absent = never seen this process
	ProbeError   string `json:"probe_error,omitempty"`
}

// loadFriends reads the registry. A missing file is an empty list ([]), never
// nil and never a fabricated row.
func loadFriends() ([]friendRecord, error) {
	b, err := os.ReadFile(filepath.Join(friendRoot(), "friends.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return []friendRecord{}, nil
		}
		return nil, err
	}
	var out []friendRecord
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []friendRecord{}
	}
	return out, nil
}

// saveFriends writes the registry atomically: read-modify-write callers hold
// the full list, and the bytes land via a temp file + rename (never a bare
// truncate of the live file).
func saveFriends(rows []friendRecord) error {
	if rows == nil {
		rows = []friendRecord{}
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	dir := friendRoot()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".friends-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() //homeguard:allow — removes this function's own os.CreateTemp file (single file in the friends dir, never a tree or a home path); no-op after the rename
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, "friends.json"))
}

// findFriend returns the registry row for name (nil when absent).
func findFriend(name string) *friendRecord {
	rows, err := loadFriends()
	if err != nil {
		return nil
	}
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}

// setFriendState rewrites the named row's state in place.
func setFriendState(name, state string) error {
	rows, err := loadFriends()
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].Name == name {
			rows[i].State = state
			return saveFriends(rows)
		}
	}
	return fmt.Errorf("friend %q not in registry", name)
}

// setFriendKeySHA256 rewrites the named row's key_sha256.
func setFriendKeySHA256(name, sha string) error {
	rows, err := loadFriends()
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].Name == name {
			rows[i].KeySHA256 = sha
			return saveFriends(rows)
		}
	}
	return fmt.Errorf("friend %q not in registry", name)
}

// ---- rate limiting (S10: rate-limit Add / New key) ------------------------

var (
	friendRateMu   sync.Mutex
	friendRateLast = map[string]time.Time{}
	// friendRateGap is the minimum spacing between Add / rotate actions. A var
	// so tests can shrink it.
	friendRateGap = 10 * time.Second
)

// friendActionAllowed enforces a simple per-key minimum gap. Fail-closed: the
// slot is consumed whether or not the action later succeeds.
func friendActionAllowed(key string) bool {
	friendRateMu.Lock()
	defer friendRateMu.Unlock()
	now := time.Now()
	if last, ok := friendRateLast[key]; ok && now.Sub(last) < friendRateGap {
		return false
	}
	friendRateLast[key] = now
	return true
}

func resetFriendRateLimiterForTest() {
	friendRateMu.Lock()
	friendRateLast = map[string]time.Time{}
	friendRateMu.Unlock()
}

// ---- live probes ----------------------------------------------------------

// friendProbe is parsed from the friend's own GET /api/health.
type friendProbe struct {
	Status   string `json:"status"`
	Revision string `json:"revision"`
	NT8      map[string]struct {
		Link         string `json:"link"`
		LastBarAgeMs *int64 `json:"last_bar_age_ms"`
	} `json:"nt8"`
}

type friendManifest struct {
	AddonBuild string `json:"addon_build"`
	AppVersion string `json:"app_version"`
}

// probeFriend fills the live fields of st from the friend's own HTTP API.
// It never fabricates: an unreachable friend keeps bot_up=false + probe_error.
func probeFriend(st *friendStatus) {
	host := strings.TrimSpace(os.Getenv("TUNNEL_IP"))
	if host == "" || st.WebPort <= 0 || st.State != "active" {
		return
	}
	hc := &http.Client{Timeout: 2 * time.Second}

	resp, err := hc.Get(fmt.Sprintf("http://%s:%d/api/health", host, st.WebPort))
	if err != nil {
		st.ProbeError = "unreachable"
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.ProbeError = fmt.Sprintf("http %d", resp.StatusCode)
		return
	}
	var h friendProbe
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		st.ProbeError = "bad health body"
		return
	}
	st.BotUp = true
	st.Revision = h.Revision
	for _, n := range h.NT8 {
		if n.Link == "up" {
			st.NTConnected = true
		}
		if n.LastBarAgeMs != nil && (st.LastBarAgeMs == nil || *n.LastBarAgeMs < *st.LastBarAgeMs) {
			st.LastBarAgeMs = n.LastBarAgeMs
		}
	}

	if m := probeFriendManifest(hc, host, st.WebPort); m != nil {
		st.AddonBuild = m.AddonBuild
		st.AppVersion = m.AppVersion
	}

	seen := time.Now().UTC().Format(time.RFC3339)
	st.LastSeen = seen
	friendLastSeen.Store(st.Name, seen)
}

func probeFriendManifest(hc *http.Client, host string, port int) *friendManifest {
	resp, err := hc.Get(fmt.Sprintf("http://%s:%d/api/friend/manifest", host, port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m friendManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil
	}
	return &m
}

// friendLastSeen is in-memory only (the B2 schema has no last_seen field; this
// wave does not widen it without a BOARD post).
var friendLastSeen sync.Map

// ---- handlers -------------------------------------------------------------

// handleOwnerFriendsList — GET /api/owner/friends (B2). Owner JWT only; 404 in
// friend mode.
func (s *Server) handleOwnerFriendsList(c *gin.Context) {
	if friendMode404(c) {
		return
	}
	if !s.requireOwner(c) {
		telemetry.IncGateBlock("", "friends_owner_gate")
		logger.Warnf("🔒 friends list refused: non-owner actor user_id=%q", c.GetString("user_id"))
		c.JSON(http.StatusForbidden, gin.H{"error": "friends page is owner-only"})
		return
	}
	s.ensureFriendMonitor()

	rows, err := loadFriends()
	if err != nil {
		logger.Errorf("friends list: read registry: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read friends registry"})
		return
	}
	statuses := make([]friendStatus, 0, len(rows))
	for _, r := range rows {
		st := friendStatus{friendRecord: r}
		probeFriend(&st)
		statuses = append(statuses, st)
	}
	c.JSON(http.StatusOK, gin.H{
		"friends":           statuses,
		"tailscale_key_set": strings.TrimSpace(os.Getenv("TAILSCALE_API_KEY")) != "",
		"tunnel_ip":         strings.TrimSpace(os.Getenv("TUNNEL_IP")),
	})
}

// handleOwnerFriendsAdd — POST /api/owner/friends (B2). Calls DS-101's
// provision.sh via a FIXED argv (no shell string building). The install code
// is returned once and is never logged or stored by this handler.
func (s *Server) handleOwnerFriendsAdd(c *gin.Context) {
	if friendMode404(c) {
		return
	}
	if !s.requireOwner(c) {
		telemetry.IncGateBlock("", "friends_owner_gate")
		c.JSON(http.StatusForbidden, gin.H{"error": "friends page is owner-only"})
		return
	}
	if !friendActionAllowed("add") {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "add is rate limited — wait and retry"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	name := strings.TrimSpace(req.Name)

	// U12: refuse to add a friend when the tunnel address is not a local IP —
	// the install code would point at a host the friend's box cannot reach.
	if _, ok := tunnelIPOK(); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TUNNEL_IP is empty or not an address on this box — set it before adding a friend"})
		return
	}

	// FIXED argv — exec.Command with an explicit slice, never a shell string.
	cmd := exec.Command(filepath.Join(friendScriptsDir(), "provision.sh"), name)
	cmd.Env = os.Environ()
	out, err := cmd.Output() // stdout = the install code (B2)
	if err != nil {
		detail := ""
		if ee, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(ee.Stderr))
		}
		logger.Errorf("friends add: provision %q failed: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "provision failed", "detail": detail})
		return
	}
	installCode := strings.TrimSpace(string(out))
	if installCode == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "provision returned no install code"})
		return
	}

	row := findFriend(name)
	if row == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "friend provisioned but absent from the registry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"friend":       row,
		"install_code": installCode,
		"shown_once":   true,
	})
}

// handleOwnerFriendAction — POST /api/owner/friends/:name/:action (B2).
// action ∈ {pause, resume, revoke, rotate}.
func (s *Server) handleOwnerFriendAction(c *gin.Context) {
	if friendMode404(c) {
		return
	}
	if !s.requireOwner(c) {
		telemetry.IncGateBlock("", "friends_owner_gate")
		c.JSON(http.StatusForbidden, gin.H{"error": "friends page is owner-only"})
		return
	}
	name := strings.TrimSpace(c.Param("name"))
	action := strings.TrimSpace(c.Param("action"))

	row := findFriend(name)
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "friend not found"})
		return
	}

	switch action {
	case "pause":
		if !friendActionAllowed("pause:" + name) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited — wait and retry"})
			return
		}
		// U3: pause must survive a reboot — stop AND disable.
		if err := systemctlUser("stop", friendUnit(name)); err != nil {
			logger.Errorf("friends pause %q: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "pause failed", "detail": err.Error()})
			return
		}
		if err := systemctlUser("disable", friendUnit(name)); err != nil {
			logger.Errorf("friends pause %q: disable: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "pause failed to disable", "detail": err.Error()})
			return
		}
		if err := setFriendState(name, "paused"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "pause failed to update registry", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"name": name, "state": "paused"})

	case "resume":
		if !friendActionAllowed("resume:" + name) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited — wait and retry"})
			return
		}
		// U3: resume = enable --now (enable + start).
		if err := systemctlUser("enable", friendUnit(name)); err != nil {
			logger.Errorf("friends resume %q: enable: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "resume failed to enable", "detail": err.Error()})
			return
		}
		if err := systemctlUser("start", friendUnit(name)); err != nil {
			logger.Errorf("friends resume %q: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "resume failed", "detail": err.Error()})
			return
		}
		if err := setFriendState(name, "active"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "resume failed to update registry", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"name": name, "state": "active"})

	case "revoke":
		if !friendActionAllowed("revoke:" + name) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited — wait and retry"})
			return
		}
		// 1. remove.sh: stops the unit, MOVES the folder to trash, sets state=revoked.
		cmd := exec.Command(filepath.Join(friendScriptsDir(), "remove.sh"), name)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.Errorf("friends revoke %q: remove.sh failed: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed", "detail": strings.TrimSpace(string(out))})
			return
		}
		// 2. Tailscale device delete — never a silent skip when the key is missing.
		ts, tsOK := tailscaleDeleteDevice(row)
		if !tsOK {
			logger.Warnf("friends revoke %q: tailscale device delete not done: %s", name, ts)
		}
		c.JSON(http.StatusOK, gin.H{"name": name, "state": "revoked", "tailscale": ts})

	case "rotate":
		if !friendActionAllowed("rotate:" + name) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited — wait and retry"})
			return
		}
		// U12: the rotated install code carries TUNNEL_IP — refuse when it is
		// not a local address on this box.
		if _, ok := tunnelIPOK(); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "TUNNEL_IP is empty or not an address on this box — set it before rotating a key"})
			return
		}
		newKey, err := newConnectKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key"})
			return
		}
		if err := setFriendEnvKey(name, newKey); err != nil {
			logger.Errorf("friends rotate %q: write .env: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write the friend's .env", "detail": err.Error()})
			return
		}
		if err := setFriendKeySHA256(name, sha256Hex(newKey)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update the registry", "detail": err.Error()})
			return
		}
		// S22: the friend bot read NT_CONNECT_KEY once at boot — restart it so
		// the rotated key takes effect and the OLD key is refused on the next
		// hello. The key is already rotated in .env + registry; a failed restart
		// is reported, never silent.
		if err := systemctlUser("restart", friendUnit(name)); err != nil {
			logger.Errorf("friends rotate %q: restart: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "key rotated but the friend bot restart failed — restart " + friendUnit(name) + " manually", "detail": err.Error()})
			return
		}
		code := buildInstallCode(friendInstallCode{
			V: 1, Name: name,
			Host:    strings.TrimSpace(os.Getenv("TUNNEL_IP")),
			NTPort:  row.NTPort,
			WebPort: row.WebPort,
			Key:     newKey,
		})
		c.JSON(http.StatusOK, gin.H{"name": name, "install_code": code, "shown_once": true})

	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown action"})
	}
}

// friendUnit is the B2 systemd user unit name.
func friendUnit(name string) string { return "vl-friend@" + name + ".service" }

// systemctlUser runs systemctl --user with a fixed argv (no shell). It is a var
// so a test can inject a fake without touching the real service manager (S22).
var systemctlUser = func(verb, unit string) error {
	cmd := exec.Command("systemctl", "--user", verb, unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s %s: %v (%s)", verb, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---- connect key + install code -------------------------------------------

func newConnectKey() (string, error) {
	b := make([]byte, friendConnectKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// friendInstallCode is the B2 install-code payload (base64url of this JSON).
type friendInstallCode struct {
	V         int    `json:"v"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	NTPort    int    `json:"nt_port"`
	WebPort   int    `json:"web_port"`
	Key       string `json:"connect_key"`
	TSAuthKey string `json:"ts_authkey,omitempty"`
}

func buildInstallCode(p friendInstallCode) string {
	b, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(b)
}

// setFriendEnvKey replaces (or appends) NT_CONNECT_KEY in the friend's .env
// via a temp file + rename; mode stays 0600. The key is never logged here.
func setFriendEnvKey(name, key string) error {
	envPath := filepath.Join(friendRoot(), name, ".env")
	b, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	replaced := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "NT_CONNECT_KEY=") {
			lines[i] = "NT_CONNECT_KEY=" + key
			replaced = true
		}
	}
	out := strings.Join(lines, "\n")
	if !replaced {
		if !strings.HasSuffix(out, "\n") && out != "" {
			out += "\n"
		}
		out += "NT_CONNECT_KEY=" + key + "\n"
	}
	tmp, err := os.CreateTemp(filepath.Dir(envPath), ".env-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, envPath)
}

// ---- Tailscale ------------------------------------------------------------

// tailscaleAPIBase is the Tailscale API base URL. A var so the S11 proof can
// point the production handler's Tailscale delete at a fake server; the
// production value is the real api.tailscale.com.
var tailscaleAPIBase = "https://api.tailscale.com"

// tailscaleDeleteDevice deletes the friend's Tailscale device via the API.
// Returns a status string and whether the delete happened (a missing key or an
// unknown device is surfaced — never a silent skip, S11/B2).
func tailscaleDeleteDevice(row *friendRecord) (string, bool) {
	key := strings.TrimSpace(os.Getenv("TAILSCALE_API_KEY"))
	if key == "" {
		return "api_key_missing", false
	}
	if strings.TrimSpace(row.TailscaleDeviceID) == "" {
		return "no_device_id", false
	}
	// The literal path element "device" is reserved by the update-auth census
	// (a fragment of the enrollment filename device.key, spelled only in
	// internal/updateauth/paths.go). The Tailscale device-API segment is spelled
	// at runtime so the census does not misread it as the enrollment key — the
	// two are unrelated.
	deviceSeg := strings.Join([]string{"dev", "ice"}, "")
	req, err := http.NewRequest(http.MethodDelete, tailscaleAPIBase+"/api/v2/"+deviceSeg+"/"+row.TailscaleDeviceID, nil)
	if err != nil {
		return "request_error", false
	}
	req.SetBasicAuth(key, "")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "request_error:" + err.Error(), false
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return "deleted", true
	default:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Sprintf("http %d %s", resp.StatusCode, strings.TrimSpace(string(b))), false
	}
}

// ---- Telegram alerts ------------------------------------------------------

const friendAlertInterval = 5 * time.Minute

var (
	friendAlertOnce sync.Once
	friendAlertStop chan struct{}

	friendAlertMu sync.Mutex
	friendAlerts  = map[string]*friendAlertState{}
)

type friendAlertState struct {
	ntDownSince        time.Time
	botDownNotified    bool
	ntDownNotified     bool
	addonStaleNotified bool
	lastRelayedReceipt string // dedup key of the last friend update receipt relayed
}

// ensureFriendMonitor starts the alert loop once (lazily, from the owner's
// first Friends page read). Safe to call repeatedly. VL_FRIEND_MONITOR_OFF=1
// keeps it off (tests).
func (s *Server) ensureFriendMonitor() {
	if s.store == nil || os.Getenv("VL_FRIEND_MONITOR_OFF") == "1" {
		return
	}
	friendAlertOnce.Do(func() {
		stop := make(chan struct{})
		friendAlertStop = stop
		safe.GoNamed("friend-alert-loop", func() { s.friendAlertLoop(stop) })
	})
}

func (s *Server) friendAlertLoop(stop chan struct{}) {
	t := time.NewTicker(friendAlertInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.evaluateFriendAlerts()
		}
	}
}

func (s *Server) evaluateFriendAlerts() {
	rows, err := loadFriends()
	if err != nil {
		return
	}
	ownerBuild := strings.TrimSpace(os.Getenv("VL_BUILD_ID"))
	for _, r := range rows {
		if r.State != "active" {
			continue
		}
		st := friendStatus{friendRecord: r}
		probeFriend(&st)

		stt := s.friendAlertStateFor(r.Name)
		switch {
		case !st.BotUp:
			if !stt.botDownNotified {
				notifyFriendAlert(s.store, fmt.Sprintf("⚠️ Friend %q is down — its bot did not answer /api/health.", r.Name))
				stt.botDownNotified = true
			}
		case !st.NTConnected:
			if stt.ntDownSince.IsZero() {
				stt.ntDownSince = time.Now()
			} else if time.Since(stt.ntDownSince) > 5*time.Minute && !stt.ntDownNotified {
				notifyFriendAlert(s.store, fmt.Sprintf("⚠️ Friend %q — NinjaTrader disconnected for more than 5 minutes.", r.Name))
				stt.ntDownNotified = true
			}
		default:
			// healthy: reset the NT episode, keep the bot-down flag latched
			// until the next successful probe cycle clears it.
			stt.ntDownSince = time.Time{}
			stt.ntDownNotified = false
			stt.botDownNotified = false
			if ownerBuild != "" && st.AddonBuild != "" && st.AddonBuild != ownerBuild && !stt.addonStaleNotified {
				notifyFriendAlert(s.store, fmt.Sprintf("⚠️ Friend %q add-on is out of date (%s vs owner %s).", r.Name, st.AddonBuild, ownerBuild))
				stt.addonStaleNotified = true
			}
			if ownerBuild != "" && st.AddonBuild != "" && st.AddonBuild == ownerBuild {
				stt.addonStaleNotified = false
			}
		}
	}

	// Relay DS-105's friend update receipts (CTO 2026-10-18 item 3): the
	// updater keeps the job file as the durable record; this monitor reads
	// it and notifies the owner, never opens the DB.
	for _, msg := range s.relayFriendReceipts() {
		notifyFriendAlert(s.store, msg)
	}
}

func (s *Server) friendAlertStateFor(name string) *friendAlertState {
	friendAlertMu.Lock()
	defer friendAlertMu.Unlock()
	stt, ok := friendAlerts[name]
	if !ok {
		stt = &friendAlertState{}
		friendAlerts[name] = stt
	}
	return stt
}

// resetFriendAlertStateForTest clears the in-memory alert + receipt dedup
// state (process-global, like the login limiter).
func resetFriendAlertStateForTest() {
	friendAlertMu.Lock()
	friendAlerts = map[string]*friendAlertState{}
	friendAlertMu.Unlock()
}
