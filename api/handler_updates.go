package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"vl/auth"
	"vl/config"
	"vl/internal/updateauth"
	"vl/internal/updaterjob"
	"vl/internal/updatersource"
	"vl/internal/updaterwire"
	"vl/internal/updatescheck"
	"vl/logger"
	"vl/telemetry"
	"vl/trader"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── W-ONE-BUTTON M3 — update authorization ────────────────────────────────
//
// Five routes under /api/updates, mounted with their OWN gate on the bare
// /api group — deliberately NOT the protected group (whose refusals are 401s
// with distinguishing texts) and deliberately NOT through s.route /
// s.routeWithSchema, which append to routeRegistry and so to GetAPIDocs — the
// route list the Telegram LLM agent is given (CTO ruling F1). The agent holds
// a JWT whose user_id IS the owner's; F1 keeps these routes out of the route
// LIST it is handed — it hides them from the agent's map, NOT from a prober.
// Their existence is observable (PR #200 review #15): a registered
// /api/updates* method+path answers the gate's 403 to anyone — no token, any
// token — while an unregistered path answers 404, so 403-vs-404 is a
// route-existence oracle. What the uniform 403 hides is WHY a request was
// refused, never THAT the route is there (pinned by
// TestUpdatesRouteExistenceIsObservable).
//
// Every gate failure is the SAME response: 403 {"error":"forbidden"}. The
// cause is logged server-side as a category only — never the token, the MAC
// or the key.
//
// With nothing enrolled (data/updater/admin.json + device.key absent — the
// OFF state) every route refuses and nothing else in the app changes.
//
// Install = the gate (identity factor: JWT of the enrolled admin) AND an
// HMAC-SHA256 over vl-update-install/v1|admin_user_id|release_id|job_id|
// expires_at under device.key (possession factor; updateauth.Message is the
// one layout).
// Nothing on the API side can mint a MAC (CTO ruling Q1(a)): the
// owner runs the attended `vl-updater-bootstrap authorize <release_id>` on the
// box and pastes its {job_id, expires_at, hmac}.

// updatesAdminIDKey carries the enrolled admin's user_id from the gate to the
// install handler: the MAC is verified over THAT identity (red-4 #4).
const updatesAdminIDKey = "updates_admin_user_id"

// UpdateHeader is the custom header every /api/updates request must carry
// with the exact value "1". It is NOT in the CORS Access-Control-Allow-Headers
// list, so a cross-origin page can never get a browser to send it (the
// preflight fails) — pinned by TestUpdatePreflightNeverAllowsTheUpdateHeader.
const UpdateHeader = "X-VL-Update"

// maxUpdateInstallBody caps the install body (a Grant is ~200 bytes).
const maxUpdateInstallBody = 4096

// workerProbeTimeout is the dial bound on the status route's request-time
// worker probe: worker_listening is measured within 250 ms, so a status call
// can never stall on a half-dead socket (CTO ruling on #206 — a measured
// value, never inferred).
const workerProbeTimeout = 250 * time.Millisecond

// checkRelayTimeout is the relay deadline for POST /api/updates/check: the
// worker's own download is bounded at updatersource.DefaultTimeout (5 min);
// the relay adds one minute of margin (fold A1).
const checkRelayTimeout = updatersource.DefaultTimeout + time.Minute

var errForbiddenBody = gin.H{"error": "forbidden"}

// UpdateStarter hands a fully authorized, verified install to the updater
// worker (M4). In M3 no starter exists: a verified manifest (impossible with
// StubVerifier) answers 503 rather than pretending to start anything.
type UpdateStarter func(g updateauth.Grant, m updateauth.Manifest) error

// SetUpdateVerifier replaces the manifest verifier (M4 wires the real one).
// nil restores the refusing stub.
func (s *Server) SetUpdateVerifier(v updateauth.Verifier) {
	if v == nil {
		v = updateauth.StubVerifier{}
	}
	s.updateVerifier = v
}

// SetUpdateStarter installs the M4 worker hand-off. nil = none (503).
func (s *Server) SetUpdateStarter(f UpdateStarter) { s.updateStart = f }

// ── W-ONE-BUTTON M4 3b-B U5b — the updater glue knob ────────────────────
//
// VL_UPDATER=1 (exactly "1"; anything else, or unset, is OFF) wires the
// M4 worker behind the M3 routes. OFF is M3 byte for byte — the stub
// verifier, no starter, the job routes' literal 404 before any filesystem
// access, and no boot line (TestUpdatesKnobOffIsByteIdentical pins M3's
// bytes as literals).
const updaterKnobEnv = "VL_UPDATER"

// updateVerifierName is the name the boot line READS off the verifier the
// server holds (never a literal the line asserts about itself).
func updateVerifierName(v updateauth.Verifier) string {
	switch v.(type) {
	case updateauth.StubVerifier:
		return "stub"
	case verdictVerifier:
		return "verdict-file"
	case nil:
		return "n/a"
	}
	return "n/a"
}

// configureUpdater reads the knob ONCE, at NewServer (main sets the data
// dir first: main.go SetMaintenanceDataDir precedes api.NewServer). OFF:
// nothing — no field set, no line. ON: the glue is wired and ONE line is
// printed whose every value is read: the verifier's name off the verifier
// the server now holds, and "dial ok" only when the worker socket actually
// dialled (a worker is started by hand, attended — not dialling at boot is
// not knowing yet, so n/a, never "down").
func (s *Server) configureUpdater() {
	if v := os.Getenv("VL_UPDATER"); v != "1" { // R5: VL_ only
		return
	}
	s.updaterOn = true
	s.updateVerifier = verdictVerifier{}
	s.updateStart = socketStarter
	logger.Infof("📦 updater glue: on · verifier=%s · worker=%s", updateVerifierName(s.updateVerifier), probeUpdaterWorker(trader.MaintenanceDataDir()))
}

// verdictVerifier (knob ON) is the install gate's real verifier: a release is
// verified iff the worker's attended `fetch` wrote its verdict file —
// <data>/updater/verdicts/<release_id>.json, written ONCE, after every
// signature and digest check, by internal/updaterworker (which the app never
// links). The app only READS it, through updaterjob.ReadVerdict: a private
// regular file, exactly one JSON object, the id asked for, every field
// computed. The data dir is the one the maintenance hold already resolves
// (trader.MaintenanceDataDir) — no second resolver. Absent, unreadable,
// malformed or naming another release ⇒ ErrNoVerifiedManifest, M3's 422.
type verdictVerifier struct{}

func (verdictVerifier) VerifiedManifest(releaseID string) (updateauth.Manifest, error) {
	v, err := updaterjob.ReadVerdict(trader.MaintenanceDataDir(), releaseID)
	if err != nil || v.ReleaseID != releaseID {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Errorf("🔒 [updates] verdict for %q refused: %v", releaseID, err)
		}
		return updateauth.Manifest{}, updateauth.ErrNoVerifiedManifest
	}
	return updateauth.Manifest{ReleaseID: v.ReleaseID}, nil
}

// socketStarter (knob ON) hands a fully authorized, verified install to the
// worker over its unix socket — the API never runs a step itself. It sends
// exactly install{release_id, job_id} and accepts only:
//   - OK with state "requested" (the worker wrote the job file), or
//   - a re-send: OK with the job's OWN state, where the job file (read with
//     updaterjob.Read) exists, names THIS release and holds that very
//     non-terminal state.
//
// Anything else — no socket, a refusal, a different state — is an error and
// so M3's 503. The error never carries the grant (only ids and the worker's
// validated error text).
func socketStarter(g updateauth.Grant, _ updateauth.Manifest) error {
	dataDir := trader.MaintenanceDataDir()
	c, err := updaterwire.DialWorker(dataDir)
	if err != nil {
		return fmt.Errorf("updater worker unreachable (job %s): %w", g.JobID, err)
	}
	defer c.Close()
	resp, err := c.Do(updaterwire.NewInstall(g.ReleaseID, g.JobID))
	if err != nil {
		return fmt.Errorf("updater worker install (job %s): %w", g.JobID, err)
	}
	if !resp.OK {
		return fmt.Errorf("updater worker refused job %s: %s", g.JobID, resp.Error)
	}
	if resp.State == string(updaterjob.StateRequested) {
		return nil
	}
	j, err := updaterjob.Read(dataDir, g.JobID)
	if err != nil {
		return fmt.Errorf("updater worker answered state %q for job %s and its job file does not read: %w", resp.State, g.JobID, err)
	}
	if j.ReleaseID != g.ReleaseID || string(j.State) != resp.State || updaterjob.IsTerminal(j.State) {
		return fmt.Errorf("updater worker answered state %q for job %s; its job file holds %s for release %q", resp.State, g.JobID, j.State, j.ReleaseID)
	}
	return nil
}

// probeUpdaterWorker dials the worker socket and hangs up without a frame
// (the listener treats a frameless close as a clean EOF).
func probeUpdaterWorker(dataDir string) string {
	c, err := updaterwire.DialWorker(dataDir)
	if err != nil {
		return "n/a"
	}
	_ = c.Close()
	return "dial ok"
}

func (s *Server) updatesClock() time.Time {
	if s.updatesNow != nil {
		return s.updatesNow()
	}
	return time.Now()
}

// registerUpdateRoutes mounts the five update routes on the bare /api group
// with raw g.GET/g.POST — never s.route (F1: not advertised to the agent).
func (s *Server) registerUpdateRoutes(api *gin.RouterGroup) {
	upd := api.Group("/updates", s.updatesGate())
	upd.GET("", s.handleUpdatesStatus)
	upd.POST("/check", s.handleUpdatesCheck)
	upd.POST("/install", s.handleUpdatesInstall)
	upd.POST("/install-with-password", s.handleUpdatesInstallWithPassword)
	upd.GET("/jobs/:id", s.handleUpdatesJob)
	upd.GET("/jobs/:id/receipt", s.handleUpdatesJob)
}

// ── CTO fold 1790280466263 — the refusal log is not a flood ─────────────
//
// updatesRefusedTotal counts EVERY refusal by the route PATTERN (gin's
// FullPath — never the client's path or id) and a CLOSED category. A
// (route, category) that never refused has no series at all: absent, never
// a fabricated 0.
var updatesRefusedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vl_updates_refused_total",
		Help: "Refusals by the /api/updates gate and install handler, by route pattern and closed refusal category.",
	},
	[]string{"route", "category"},
)

// updatesRefusalUnmapped is the category of a reason the closed map does not
// know (TestEveryUpdatesRefusalReasonHasACategory keeps it unreachable).
const updatesRefusalUnmapped = "unmapped"

// updatesRefusalCategories is the CLOSED map reason → category. The label is
// always one of these constants, never the reason text (which may carry a
// header name or a configuration detail).
var updatesRefusalCategories = map[string]string{
	"data dir unconfigured":                "data_dir_unconfigured",
	"peer unparseable":                     "peer_unparseable",
	"peer not loopback":                    "peer_not_loopback",
	"host not a loopback name":             "host_not_loopback",
	"update header missing or wrong":       "update_header",
	"cross-origin":                         "cross_origin",
	"cross-site fetch":                     "cross_site_fetch",
	"JWT secret empty":                     "jwt_secret_unfit",
	"JWT secret is a public placeholder":   "jwt_secret_unfit",
	"JWT secret shorter than 32 bytes":     "jwt_secret_unfit",
	"authorization missing":                "authorization_missing",
	"authorization malformed":              "authorization_malformed",
	"token revoked":                        "token_revoked",
	"token invalid":                        "token_invalid",
	"machine token":                        "machine_token",
	"not enrolled":                         "not_enrolled",
	"enrollment unreadable":                "enrollment_unreadable",
	"device key unreadable":                "device_key_unreadable",
	"not the enrolled admin":               "not_enrolled_admin",
	"no user store":                        "no_user_store",
	"admin user row absent or changed":     "admin_row_changed",
	"token older than the user row":        "token_older_than_row",
	"install: outside the validity window": "install_expired",
	"install: device key unreadable":       "install_key_unreadable",
	"install: MAC mismatch":                "install_mac",
	"password changed since enrollment (re-enroll with --replace)":                                  "password_changed",
	"install: expired under the seen-store lock, or at/below its clock floor (clock stepped back?)": "install_expired_under_lock",
	"install: job-id store refused":                                                                 "job_store_refused",
	"install-with-password: no enrolled admin on the context":                                       "install_password_no_admin_context",
	"install-with-password: password locked out":                                                    "install_password_locked_out",
	"install-with-password: admin user row absent":                                                  "install_password_admin_row_absent",
	"install-with-password: password wrong":                                                         "install_password_wrong",
	"install-with-password: server-side mint refused":                                               "install_password_mint_refused",
}

// updatesRefusalCategory maps a refusal reason onto its closed category.
func updatesRefusalCategory(why string) string {
	if c, ok := updatesRefusalCategories[why]; ok {
		return c
	}
	if strings.HasPrefix(why, "forwarded request (") {
		return "forwarded"
	}
	return updatesRefusalUnmapped
}

// updatesForbid is every /api/updates refusal: the uniform 403, the count,
// and the log line — a WARN the FIRST time a (route, category) refuses in
// this process (the process builds exactly one Server: main.go api.NewServer),
// DEBUG for every repeat. An unmapped reason (none exist — pinned) would key
// its once-set on the reason too, so two different unknown reasons never
// hide behind one another.
func (s *Server) updatesForbid(c *gin.Context, why string) {
	s.updatesForbidStatus(c, why, http.StatusForbidden, errForbiddenBody)
}

// updatesForbidStatus is updatesForbid with a caller-chosen status and body:
// every refusal still counts in vl_updates_refused_total (route + closed
// category) and logs once per (route, category) — the 429 lockout refusals
// included, so they cannot hide from the counters.
func (s *Server) updatesForbidStatus(c *gin.Context, why string, status int, body gin.H) {
	route := c.FullPath()
	if route == "" {
		route = "unmatched"
	}
	cat := updatesRefusalCategory(why)
	updatesRefusedTotal.WithLabelValues(route, cat).Inc()
	key := route + "\x00" + cat
	if cat == updatesRefusalUnmapped {
		key += "\x00" + why
	}
	if _, seen := s.updatesWarned.LoadOrStore(key, struct{}{}); !seen {
		logger.Warnf("🔒 [updates] refused %s %.96q: %s — first %s refusal on %s this process; repeats log at DEBUG, all count in vl_updates_refused_total", c.Request.Method, route, why, cat, route)
	} else {
		logger.Debugf("🔒 [updates] refused %s %.96q: %s (repeat, counted as %s on %s)", c.Request.Method, route, why, cat, route)
	}
	c.AbortWithStatusJSON(status, body)
}

// updatesGate is the whole identity gate. It returns the refusal category
// ("" = admitted). Order: transport checks (no I/O — the X-VL-Update
// header among them), then the JWT, then the enrollment files, then the
// users store. A header-less request is refused before any token-derived
// work (PR #200 F8: TestUpdatesHeaderIsJudgedBeforeTheToken).
func (s *Server) updatesGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if why := s.updatesRefusal(c); why != "" {
			s.updatesForbid(c, why)
			return
		}
		c.Next()
	}
}

var loopbackHostNames = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}

// forwardingHeaders are the headers a proxy, load balancer or tunnel adds to
// say it relayed the request (lower-case, '-' separated). Any x-forwarded-*
// counts too. No browser and no local UI sends one.
var forwardingHeaders = map[string]bool{
	"forwarded": true, "x-real-ip": true, "via": true,
	"cf-connecting-ip": true, "true-client-ip": true, "x-client-ip": true,
	"x-cluster-client-ip": true, "fastly-client-ip": true, "x-original-forwarded-for": true,
}

// forwardingHeader returns the (normalized) name of the first forwarding
// header present — whatever its value, even empty — or "". Keys are
// compared case-insensitively with '_' read as '-', so a non-canonical
// spelling that some relays or CGI bridges produce cannot slip past. The
// returned name is our own constant spelling, never the client's bytes.
func forwardingHeader(h http.Header) string {
	for k := range h {
		n := strings.ReplaceAll(strings.ToLower(k), "_", "-")
		if forwardingHeaders[n] {
			return n
		}
		if strings.HasPrefix(n, "x-forwarded-") {
			return "x-forwarded-*"
		}
	}
	return ""
}

// requestHostName is the hostname part of r.Host, lower-cased, brackets
// stripped; "" when the port part is present but not all digits.
func requestHostName(hostport string) string {
	h := hostport
	if host, port, err := net.SplitHostPort(hostport); err == nil {
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return ""
			}
		}
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]"))
}

func (s *Server) updatesRefusal(c *gin.Context) string {
	r := c.Request
	dataDir := trader.MaintenanceDataDir()
	if dataDir == "" {
		return "data dir unconfigured"
	}

	// F4: loopback judged on the socket peer (RemoteAddr), never ClientIP()
	// — gin trusts X-Forwarded-For by default; NewServer turns that off
	// (PR #200 F2, SetTrustedProxies(nil)), but this gate must not depend
	// on the engine's proxy configuration either way.
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "peer unparseable"
	}
	if ip := net.ParseIP(peer); ip == nil || !ip.IsLoopback() {
		return "peer not loopback"
	}
	// F4: Host restricted to loopback names (DNS rebinding: evil.test
	// resolving to 127.0.0.1 still arrives with Host: evil.test).
	if !loopbackHostNames[requestHostName(r.Host)] {
		return "host not a loopback name"
	}
	// Red-team M3: a reverse proxy or tunnel on this box is a loopback peer
	// for EVERY client it relays (and rewrites Host to ours). A request that
	// says it was forwarded is refused. Known limit: a relay that adds none of
	// these headers is indistinguishable from a local client — updates are
	// loopback-DIRECT only (runbook).
	if h := forwardingHeader(r.Header); h != "" {
		return "forwarded request (" + h + ")"
	}
	// CSRF: the custom header, exactly one value, exactly "1".
	vs := r.Header.Values(UpdateHeader)
	if len(vs) != 1 || vs[0] != "1" {
		return "update header missing or wrong"
	}
	// Origin absent or same-origin (the server speaks plain http).
	if o := r.Header.Values("Origin"); len(o) > 1 || (len(o) == 1 && o[0] != "http://"+r.Host) {
		return "cross-origin"
	}
	if sfs := r.Header.Values("Sec-Fetch-Site"); len(sfs) > 1 || (len(sfs) == 1 && sfs[0] != "same-origin" && sfs[0] != "none") {
		return "cross-site fetch"
	}

	// F2 + red-team M1: nothing signed with a secret this public repo
	// publishes (the loader default, the .env.example placeholder, the CI
	// literal) or with one shorter than 32 bytes is an identity.
	if why := config.JWTSecretUnfitForUpdates(auth.JWTSecret); why != "" {
		return why
	}
	ah := r.Header.Values("Authorization")
	if len(ah) != 1 {
		return "authorization missing"
	}
	parts := strings.Split(ah[0], " ")
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "authorization malformed"
	}
	if auth.IsTokenBlacklisted(parts[1]) {
		return "token revoked"
	}
	claims, err := auth.ValidateJWT(parts[1])
	if err != nil || claims == nil {
		return "token invalid"
	}
	// M3 red-team H1: a machine token (any scope, or bot@internal) is never
	// an update identity, whatever email it carries.
	if claims.IsMachine() {
		return "machine token"
	}

	// The enrollment. Absent = the OFF state.
	admin, err := updateauth.LoadAdmin(dataDir)
	if err != nil {
		if errors.Is(err, updateauth.ErrNotEnrolled) {
			return "not enrolled"
		}
		return "enrollment unreadable"
	}
	key, err := updateauth.LoadDeviceKey(dataDir)
	if err != nil {
		return "device key unreadable"
	}
	defer clear(key)
	// F1: BOTH user_id and email (the Telegram bot token carries the owner's
	// user_id with email bot@internal).
	if claims.UserID != admin.UserID || claims.Email != admin.Email {
		return "not the enrolled admin"
	}
	// F3: the admin user row must still exist with the enrolled email (a
	// reset-account wipe leaves pre-reset JWTs valid for up to 24h).
	if s.store == nil {
		return "no user store"
	}
	u, err := s.store.User().GetByID(admin.UserID)
	if err != nil || u == nil || u.Email != admin.Email {
		return "admin user row absent or changed"
	}
	// H1/H2 belt: the enrollment is bound to the password the row had at
	// enrollment. Any change since — the owner's own, or one forced through a
	// machine/stolen/retired token — un-enrolls until the owner re-runs the
	// attended `vl-updater-bootstrap enroll --replace` on the box.
	if !admin.PasswordStillBound(key, u.PasswordHash) {
		return "password changed since enrollment (re-enroll with --replace)"
	}
	// Q8: a token issued before the user row last changed (password change)
	// is stale for updates. The JWT iat is whole seconds (golang-jwt
	// TimePrecision), updated_at has whatever precision its writer stored, so
	// the rule correct at every precision is iat STRICTLY after updated_at
	// truncated to the second (red-team red-1 #5): a token from the same
	// second as the change — either side of it — is refused. The comparison
	// is auth.IssuedNotAfter, the one H2 rule authMiddleware applies too; Q8
	// stays stricter than it (zero updated_at ⇒ refuse, and updated_at is
	// the epoch even on a never-changed row) — pinned by
	// TestUpdatesQ8StaysStricterThanTheSharedRetireRule: do NOT move this
	// line onto auth.RetiredBy.
	if u.UpdatedAt.IsZero() || auth.IssuedNotAfter(claims.IssuedAt, u.UpdatedAt) {
		return "token older than the user row"
	}
	c.Set(updatesAdminIDKey, admin.UserID)
	return ""
}

// handleUpdatesStatus — GET /api/updates. Only reached by the enrolled admin,
// so enrolled is true; it never returns any field or derivative of
// admin.json or device.key.
func (s *Server) handleUpdatesStatus(c *gin.Context) {
	_, stub := s.updateVerifier.(updateauth.StubVerifier)
	verifier := "configured"
	if stub {
		verifier = "stub"
	}
	enabled := !stub && s.updateStart != nil
	// worker_listening is MEASURED at request time, never inferred: a
	// bounded dial of the worker socket (the listener treats a frameless
	// close as a clean EOF). No dial when install is not enabled — a
	// configuration-only answer carries false, never a guess.
	workerListening := false
	if enabled {
		if wc, err := updaterwire.DialWorkerBounded(trader.MaintenanceDataDir(), workerProbeTimeout); err == nil {
			_ = wc.Close()
			workerListening = true
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"enrolled":          true,
		"manifest_verifier": verifier,
		"install_enabled":   enabled,
		"worker_listening":  workerListening,
	})
}

// handleUpdatesCheck — POST /api/updates/check (ONE-BUTTON P-A, fold A1).
// The bot makes NO network call: it relays the `check` verb to the worker
// over the unix socket. The worker owns every byte of release-source network
// code (updatersource). When the worker or the source is absent the answer
// keeps the M3 shape ({checked:false, reason}), so old clients degrade to
// exactly today's text.
func (s *Server) handleUpdatesCheck(c *gin.Context) {
	conn, err := updaterwire.DialWorker(trader.MaintenanceDataDir())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "updater worker unreachable"})
		return
	}
	defer conn.Close()
	// The check may download + verify (the worker bounds its own network at
	// updatersource.DefaultTimeout); the relay deadline adds a margin.
	resp, err := conn.DoWithTimeout(updaterwire.NewCheck(), checkRelayTimeout)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "updater worker unreachable"})
		return
	}
	if !resp.OK {
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "check failed"})
		return
	}
	switch resp.State {
	case "off":
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "no release source in this build"})
	case "rate_limited":
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "rate limited, try later"})
	case "up_to_date":
		d := checkDetailFrom(resp.Detail)
		c.JSON(http.StatusOK, updatescheck.UpToDate(d.detail()))
	case "verified_ready":
		d := checkDetailFrom(resp.Detail)
		c.JSON(http.StatusOK, updatescheck.VerifiedReady(d.detail()))
	case "error":
		d := checkDetailFrom(resp.Detail)
		reason := d.Reason
		if reason == "" {
			reason = "check failed"
		}
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": reason})
	default:
		c.JSON(http.StatusOK, gin.H{"checked": false, "reason": "check failed"})
	}
}

// The two check answers come from internal/updatescheck — the single
// source shared with the parity test and the fixture generator
// (cmd/gen-updates-check-fixture). Never build them inline here.
// checkDetail is the worker's CheckDetail as the relay re-reads it. The relay
// re-parses (never re-interprets) the worker's own JSON.
type checkDetail struct {
	Available       bool   `json:"available"`
	Ready           bool   `json:"ready"`
	Tag             string `json:"tag"`
	TargetCommitish string `json:"target_commitish"`
	SourceSHA       string `json:"source_sha"`
	Reason          string `json:"reason"`
}

// detail is the updatescheck view of the parsed worker answer.
func (d checkDetail) detail() updatescheck.Detail {
	return updatescheck.Detail{
		Tag:             d.Tag,
		TargetCommitish: d.TargetCommitish,
		SourceSHA:       d.SourceSHA,
	}
}

func checkDetailFrom(raw string) checkDetail {
	var d checkDetail
	_ = json.Unmarshal([]byte(raw), &d)
	return d
}

// handleUpdatesInstall — POST /api/updates/install
// {release_id, job_id, expires_at, hmac}. Order (each refusal final):
// strict parse 400 → expiry window 403 → HMAC 403 → consume job id (replay
// 409; at/below the seen store's clock floor 403) → verified manifest (M3
// stub: 422 "release not verified").
func (s *Server) handleUpdatesInstall(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUpdateInstallBody)
	g, err := updateauth.ParseInstallRequest(c.Request.Body)
	if err != nil {
		logger.Warnf("🔒 [updates] install: bad request")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	s.runInstall(c, g)
}

// runInstall is the ONE install path (owner order 10-02 07:3x: "remove the
// code step"): both the paste flow and install-with-password hand their grant
// here — expiry window → HMAC → consume job id → verified manifest → hand-off
// to the worker. The terminal authorize mints the grant off-box;
// install-with-password mints it server-side with the SAME
// updateauth.Authorize (same job id / expiry / MAC rules).
func (s *Server) runInstall(c *gin.Context, g updateauth.Grant) {
	dataDir := trader.MaintenanceDataDir()
	// the enrolled admin the gate admitted (fail closed when absent: the MAC
	// is bound to that identity and cannot verify without it)
	adminID := c.GetString(updatesAdminIDKey)
	now := s.updatesClock()
	if err := updateauth.CheckExpiry(g.ExpiresAt, now); err != nil {
		// Red-team red-3 #2: a GENUINE code refused as expired raises the
		// seen store's clock floor, so it stays expired after a clock
		// step-back. Only a key holder moves the floor (MAC first); a code
		// from the future (clock behind) is not "expired" and writes nothing.
		if g.ExpiresAt <= now.Unix() {
			if key, kerr := updateauth.LoadDeviceKey(dataDir); kerr == nil {
				genuine := adminID != "" && updateauth.VerifyMAC(key, adminID, g.ReleaseID, g.JobID, g.ExpiresAt, g.HMAC)
				clear(key)
				if genuine {
					if nerr := updateauth.NoteExpired(dataDir, now); nerr != nil {
						logger.Errorf("🔒 [updates] install: could not record the expiry clock floor: %v", nerr)
					}
				}
			}
		}
		s.updatesForbid(c, "install: outside the validity window")
		return
	}
	key, err := updateauth.LoadDeviceKey(dataDir)
	if err != nil {
		s.updatesForbid(c, "install: device key unreadable")
		return
	}
	ok := adminID != "" && updateauth.VerifyMAC(key, adminID, g.ReleaseID, g.JobID, g.ExpiresAt, g.HMAC)
	clear(key)
	if !ok {
		s.updatesForbid(c, "install: MAC mismatch")
		return
	}
	// The job id is spent from here on, whatever follows.
	// Expiry is judged again by Consume on a clock reading taken UNDER the
	// seen-store lock (red-team red-3 #3); the check above is only the early,
	// lock-free refusal.
	if err := updateauth.Consume(dataDir, g.JobID, g.ExpiresAt, s.updatesClock); err != nil {
		if errors.Is(err, updateauth.ErrReplay) {
			if errors.Is(err, updateauth.ErrPrunedReplay) {
				// M3-RT-F1: expires_at at or below the store's pruned-through
				// watermark — a consumed-and-pruned id, or a fresh grant minted
				// under a clock stepped back past a prune. Indistinguishable,
				// so it is a replay (fail closed).
				logger.Warnf("🔒 [updates] install: job id at or below the pruned-through watermark — refused as a replay (clock stepped back?)")
			} else {
				logger.Warnf("🔒 [updates] install: job id replay")
			}
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "job already used"})
			return
		}
		if errors.Is(err, updateauth.ErrExpired) {
			s.updatesForbid(c, "install: expired under the seen-store lock, or at/below its clock floor (clock stepped back?)")
			return
		}
		logger.Errorf("🔒 [updates] install: job-id store refused: %v", err)
		// #206 review fold: this refusal goes through updatesForbid too — the
		// guide says every refusal increments vl_updates_refused_total, and
		// the raw 403 used to skip the counter silently.
		s.updatesForbid(c, "install: job-id store refused")
		return
	}
	m, err := s.updateVerifier.VerifiedManifest(g.ReleaseID)
	if err != nil || m.ReleaseID != g.ReleaseID {
		logger.Warnf("🔒 [updates] install: release %q not verified", g.ReleaseID)
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": "release not verified"})
		return
	}
	if s.updateStart == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "installer unavailable"})
		return
	}
	if err := s.updateStart(g, m); err != nil {
		logger.Errorf("🔒 [updates] install: hand-off refused: %v", err)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "installer unavailable"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": g.JobID})
}

// ── owner order 10-02 07:3x — install with the owner's VL password ──────
//
// POST /api/updates/install-with-password {release_id, password}. Behind the
// SAME updates gate as /install (loopback, X-VL-Update, same-origin, machine
// tokens refused, enrolled-admin session only — updatesRefusal), so the
// caller here IS the enrolled admin with a fresh-enough session. The handler
// adds the PASSWORD proof: the account's stored hash, delayed + counted like
// the current_password compare (F9), with a lockout after 5 wrong in 15 min
// (429 + unlock time). On success it mints the grant server-side with the
// SAME updateauth.Authorize the terminal `authorize` uses, then hands it to
// runInstall — no second install path.

const (
	// installPasswordWrongGate is the gate-block counter a failed password
	// compare bumps (telemetry.IncGateBlock; read at GET /api/risk/gate-blocks).
	installPasswordWrongGate = "updates_install_password_wrong"
	// installPasswordFailWindow and thresholds: 5 wrong in 15 min locks the
	// install out for 15 min (the unlock time is in the 429 body).
	installPasswordFailWindow = 15 * time.Minute
	installPasswordBlockAfter = 5
	installPasswordBlockFor   = 15 * time.Minute
)

type installPasswordLimiter struct {
	mu sync.Mutex
	m  map[string]*loginLimiterEntry
}

var apiInstallPasswordLimiter = &installPasswordLimiter{m: make(map[string]*loginLimiterEntry)}

func (l *installPasswordLimiter) blocked(now time.Time, adminID string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.m[adminID]; e != nil && e.blockedUntil.After(now) {
		return e.blockedUntil.Sub(now), true
	}
	return 0, false
}

func (l *installPasswordLimiter) recordFail(now time.Time, adminID string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > loginLimiterMaxKeys {
		for k, e := range l.m {
			if now.Sub(e.windowStart) > installPasswordFailWindow && !e.blockedUntil.After(now) {
				delete(l.m, k)
			}
		}
	}
	e := l.m[adminID]
	if e == nil || now.Sub(e.windowStart) > installPasswordFailWindow {
		e = &loginLimiterEntry{windowStart: now}
		l.m[adminID] = e
	}
	e.fails++
	if e.fails >= installPasswordBlockAfter {
		e.blockedUntil = now.Add(installPasswordBlockFor)
		return e.blockedUntil.Sub(now)
	}
	return 0
}

func (l *installPasswordLimiter) clear(adminID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, adminID)
}

// handleUpdatesInstallWithPassword — POST /api/updates/install-with-password.
// Refusals are specific: bad body/release id 400; locked out 429 with the
// unlock time; wrong password 403 (counted in vl_updates refusals AND the
// gate-block counter, delayed like the F9 compare); the grant hand-off keeps
// every existing refusal (expiry/HMAC/replay/422) byte-identical.
func (s *Server) handleUpdatesInstallWithPassword(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUpdateInstallBody)
	var req struct {
		ReleaseID string `json:"release_id" binding:"required"`
		Password  string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "release_id and password are required"})
		return
	}
	if !updateauth.ValidReleaseID(req.ReleaseID) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "release_id is an identifier (letters, digits, . _ -)"})
		return
	}
	adminID := c.GetString(updatesAdminIDKey)
	if adminID == "" {
		// Unreachable through the gate (fail closed if it ever is).
		s.updatesForbid(c, "install-with-password: no enrolled admin on the context")
		return
	}
	now := s.updatesClock()
	if d, ok := apiInstallPasswordLimiter.blocked(now, adminID); ok {
		s.updatesForbidStatus(c, "install-with-password: password locked out", http.StatusTooManyRequests,
			gin.H{"error": "too many wrong passwords", "unlock_in_seconds": int(d.Seconds())})
		return
	}
	u, err := s.store.User().GetByID(adminID)
	if err != nil || u == nil {
		s.updatesForbid(c, "install-with-password: admin user row absent")
		return
	}
	if !auth.CheckPassword(req.Password, u.PasswordHash) {
		logger.Warnf("🔒 [updates] refused %s %s: install password wrong", c.Request.Method, c.FullPath())
		telemetry.IncGateBlock("", installPasswordWrongGate)
		if d := apiInstallPasswordLimiter.recordFail(now, adminID); d > 0 {
			s.updatesForbidStatus(c, "install-with-password: password locked out", http.StatusTooManyRequests,
				gin.H{"error": "too many wrong passwords", "unlock_in_seconds": int(d.Seconds())})
			return
		}
		currentPasswordFailSleep(currentPasswordFailDelay)
		s.updatesForbid(c, "install-with-password: password wrong")
		return
	}
	apiInstallPasswordLimiter.clear(adminID)

	g, err := updateauth.Authorize(trader.MaintenanceDataDir(), req.ReleaseID, now)
	if err != nil {
		logger.Errorf("🔒 [updates] install-with-password: server-side mint refused: %v", err)
		s.updatesForbid(c, "install-with-password: server-side mint refused")
		return
	}
	s.runInstall(c, g)
}

// handleUpdatesJob — GET /api/updates/jobs/:id and /jobs/:id/receipt.
//
// Knob OFF (M3): no jobs — every id is unknown ⇒ the literal 404, before any
// filesystem access, so no id however shaped can reach a path.
//
// Knob ON: the id must pass updaterwire.ValidJobID (the ONE id rule the wire
// and the job file share) before anything else; then the WORKER's job file
// is read with updaterjob.Read (private dirs, a safe regular file, the strict
// decoder, the file-name binding) and projected — updaterjob.View for the
// job, updaterjob.Receipts for /receipt. Every miss is the SAME 404 body:
// absent is silent; a corrupt file or an unsafe dir is logged at ERROR
// server-side and still answers 404, so the response is never a filesystem
// oracle and never carries a byte of the file.
func (s *Server) handleUpdatesJob(c *gin.Context) {
	if !s.updaterOn {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	id := c.Param("id")
	if !updaterwire.ValidJobID(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	j, err := updaterjob.Read(trader.MaintenanceDataDir(), id)
	if err != nil {
		if !errors.Is(err, updaterjob.ErrNotFound) {
			logger.Errorf("🔒 [updates] job %s unreadable — answered 404: %v", id, err)
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if strings.HasSuffix(c.FullPath(), "/receipt") {
		c.JSON(http.StatusOK, updaterjob.Receipts(j))
		return
	}
	c.JSON(http.StatusOK, updaterjob.View(j))
}
