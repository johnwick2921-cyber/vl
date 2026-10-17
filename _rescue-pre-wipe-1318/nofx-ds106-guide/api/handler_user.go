package api

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"vl/auth"
	"vl/logger"
	"vl/store"
	"vl/telemetry"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// accountResetConfirmToken is the literal a caller must echo in the request body
// to run the destructive account reset — a deliberate speed bump so the endpoint
// can never be triggered by a stray click or a replayed empty POST.
const accountResetConfirmToken = "RESET-ALL-DATA"

// ── PR #200 fold F9 (CTO 1790252194343 #21) — the current_password compare ──
//
// PUT /api/user/password had no limiter on the current_password compare. The
// minimal, fail-closed fold: after a FAILED compare the answer waits a fixed
// currentPasswordFailDelay, and the refusal is counted — the B6 gate-block
// table (telemetry.IncGateBlock, the process-wide "" key: this chokepoint has
// no trader; read at GET /api/risk/gate-blocks) — beside the existing WARN
// line. A real limiter is a follow-up: a failure-only delay does not bound a
// client that runs attempts in parallel, or stops waiting once the fast
// success window has passed.
const currentPasswordFailDelay = time.Second

// currentPasswordWrongGate is the gate-block counter a failed compare bumps.
const currentPasswordWrongGate = "credential_current_password_wrong"

// currentPasswordFailSleep is the delay seam: production sleeps. The api test
// binary swaps it in TestMain (testmain_test.go), so no test ever sleeps the
// real second; TestWrongCurrentPasswordIsDelayedAndCounted pins that the
// production value is time.Sleep and the delay 1 s.
var currentPasswordFailSleep = time.Sleep

// ── P2-11 (audit 0926-system) — /login rate limit + constant-time unknown ──
//
// The unknown-email path returned a fast 401 while the known-email path ran
// bcrypt — a timing oracle for account existence. Now the unknown path burns
// the SAME bcrypt cost against a dummy hash, and both paths plus the IP get a
// failure limiter with backoff.
const (
	loginFailWindow          = 5 * time.Minute
	loginBlockAfterFails     = 5
	loginLongBlockAfterFails = 10
	loginBlockShort          = time.Minute
	loginBlockLong           = 15 * time.Minute
	loginLimiterMaxKeys      = 4096
)

// loginCheckPassword is a seam: production is auth.CheckPassword (the test
// swaps it to count calls — proving the unknown-email path burns the dummy
// hash — and to avoid real bcrypt cost where it isn't the subject).
var loginCheckPassword = auth.CheckPassword

// loginLimiterClock is a seam: production is time.Now.
var loginLimiterClock = time.Now

// loginDummyHash is a fixed bcrypt hash of a fixed string: the unknown-email
// path compares against it so both paths cost one bcrypt compare. It MUST
// exist for the constant-time path — a failed init would reopen the timing
// oracle silently, so init fails loud (B1).
var loginDummyHash = mustLoginDummyHash()

func mustLoginDummyHash() string {
	h, err := auth.HashPassword("nofx-login-dummy-constant-time-v1")
	if err != nil {
		panic(fmt.Sprintf("api: dummy login hash init failed: %v", err))
	}
	return h
}

type loginLimiterEntry struct {
	fails        int
	windowStart  time.Time
	blockedUntil time.Time
}

type loginLimiter struct {
	mu sync.Mutex
	m  map[string]*loginLimiterEntry
}

var apiLoginLimiter = &loginLimiter{m: make(map[string]*loginLimiterEntry)}

// blockedUntil reports how long the key is refused for, if at all.
func (l *loginLimiter) blocked(now time.Time, key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.m[key]; e != nil && e.blockedUntil.After(now) {
		return e.blockedUntil.Sub(now), true
	}
	return 0, false
}

// recordFail bumps the key and returns how long the key is now blocked for.
// Expired entries are pruned opportunistically so a probe with fresh keys
// cannot grow the map without bound.
func (l *loginLimiter) recordFail(now time.Time, key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > loginLimiterMaxKeys {
		for k, e := range l.m {
			if now.Sub(e.windowStart) > loginFailWindow && !e.blockedUntil.After(now) {
				delete(l.m, k)
			}
		}
	}
	e := l.m[key]
	if e == nil || now.Sub(e.windowStart) > loginFailWindow {
		e = &loginLimiterEntry{windowStart: now}
		l.m[key] = e
	}
	e.fails++
	switch {
	case e.fails >= loginLongBlockAfterFails:
		e.blockedUntil = now.Add(loginBlockLong)
	case e.fails >= loginBlockAfterFails:
		e.blockedUntil = now.Add(loginBlockShort)
	}
	if e.blockedUntil.After(now) {
		return e.blockedUntil.Sub(now)
	}
	return 0
}

// clear removes the key (a successful login resets its failure count).
func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

// keyLabel renders a limiter key for logs without printing an account name
// (L12: emails are never logged).
func keyLabel(key string) string {
	if strings.Contains(key, "@") {
		return "email(hidden)"
	}
	return key
}

// handleLogout Add current token to blacklist
func (s *Server) handleLogout(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
		return
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization format"})
		return
	}
	tokenString := parts[1]
	claims, err := auth.ValidateJWT(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}
	var exp time.Time
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	} else {
		exp = time.Now().Add(24 * time.Hour)
	}
	auth.BlacklistToken(tokenString, exp)
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// handleRegister Handle user registration request.
// handleRegister allows registration only when no users exist yet (first-time setup).
// This is a single-user system; subsequent registrations are permanently closed.
func (s *Server) handleRegister(c *gin.Context) {
	userCount, err := s.store.User().Count()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check user count"})
		return
	}

	if userCount > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "System already initialized"})
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
		Lang     string `json:"lang"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	lang := req.Lang
	if lang != "zh" && lang != "id" {
		lang = "en"
	}

	// Check if email already exists
	_, err = s.store.User().GetByEmail(req.Email)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Generate password hash
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Password processing failed"})
		return
	}

	// Create user
	userID := uuid.New().String()
	user := &store.User{
		ID:           userID,
		Email:        req.Email,
		PasswordHash: passwordHash,
	}

	err = s.store.User().Create(user)
	if err != nil {
		SafeInternalError(c, "Failed to create user", err)
		return
	}

	// SECURITY (P0 S3): registration deliberately does NOT adopt pre-existing
	// ai_models/exchanges rows. It used to call adoptOrphanRecords(userID), which
	// re-assigned every orphaned credential row — model API keys, exchange API
	// keys, private keys — to whoever registered next. Combined with the
	// then-public reset-account, that was the payoff step of an unauthenticated
	// takeover: wipe the users, register, inherit the keys.
	//
	// A fresh account now starts with only its own defaults (initUserDefaultConfigs
	// below). Recovering credentials from a previous install is an explicit,
	// operator-side action against the database, not a side effect of signup.

	// Generate JWT token
	token, err := auth.GenerateJWT(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Initialize default model and exchange configs for user
	err = s.initUserDefaultConfigs(user.ID, lang)
	if err != nil {
		logger.Infof("Failed to initialize user default configs: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"token":   token,
		"user_id": user.ID,
		"email":   user.Email,
		"message": "Registration successful",
	})
}

// handleLogin Handle user login request
func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// P2-11: per-IP AND per-account backoff. A blocked key returns 429 with
	// Retry-After BEFORE any password work runs.
	now := loginLimiterClock()
	for _, key := range []string{c.ClientIP(), req.Email} {
		if d, blocked := apiLoginLimiter.blocked(now, key); blocked {
			telemetry.IncGateBlock("", "login_rate_limited")
			logger.Warnf("🔒 login rate-limited: key=%s remaining=%s", keyLabel(key), d.Round(time.Second))
			c.Header("Retry-After", fmt.Sprintf("%.0f", math.Ceil(d.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many login attempts — try again later"})
			return
		}
	}

	// Get user information
	user, err := s.store.User().GetByEmail(req.Email)
	if err != nil {
		// P2-11: constant-time path — the unknown-email branch burns the SAME
		// bcrypt cost as the known one so the response time does not reveal
		// account existence.
		_ = loginCheckPassword(req.Password, loginDummyHash)
		apiLoginLimiter.recordFail(now, c.ClientIP())
		apiLoginLimiter.recordFail(now, req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Email or password incorrect"})
		return
	}

	// Verify password
	if !loginCheckPassword(req.Password, user.PasswordHash) {
		apiLoginLimiter.recordFail(now, c.ClientIP())
		apiLoginLimiter.recordFail(now, req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Email or password incorrect"})
		return
	}
	// Success resets both keys.
	apiLoginLimiter.clear(c.ClientIP())
	apiLoginLimiter.clear(req.Email)

	// Issue token directly after password verification.
	token, err := auth.GenerateJWT(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":   token,
		"user_id": user.ID,
		"email":   user.Email,
		"message": "Login successful",
	})
}

// handleChangePassword changes the password for the currently authenticated user.
//
// H1 (M3 red team, CTO ruling 1790231205208 item 1): only a token whose email
// is the row's own may do it — credentialActorRefusal (credential_guard.go)
// runs before anything else — AND the request must carry the account's
// CURRENT password, verified against the stored hash. A bearer token alone
// (the Telegram bot's, a stolen session's) can no longer set the password.
// Missing/empty current_password → 400; wrong → 403 "current password is
// incorrect" (the web form shows it; web/src/pages/SettingsPage.tsx sends the
// field since the same change).
func (s *Server) handleChangePassword(c *gin.Context) {
	u, why := s.credentialActorRefusal(c)
	if why != "" {
		credentialForbid(c, why)
		return
	}
	userID := u.ID
	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "current_password and new_password (min 8 chars) are required")
		return
	}
	if !auth.CheckPassword(req.CurrentPassword, u.PasswordHash) {
		logger.Warnf("🔒 [credentials] refused %s %s from %s: current password is incorrect", c.Request.Method, c.FullPath(), c.ClientIP())
		// F9: count it, then hold the answer a fixed second (see the
		// currentPasswordFailDelay block above).
		telemetry.IncGateBlock("", currentPasswordWrongGate)
		currentPasswordFailSleep(currentPasswordFailDelay)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "current password is incorrect"})
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		SafeInternalError(c, "Password processing failed", err)
		return
	}
	if err := s.store.User().UpdatePassword(userID, hash); err != nil {
		SafeInternalError(c, "Failed to update password", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password updated"})
}

// handleResetPasswordDisabled permanently refuses password resets.
//
// SECURITY (P0 S2): the previous handler took {email, new_password} on a PUBLIC
// route and reset that account immediately — no token, no old password, no mail
// verification, no rate limit. Anyone who could reach the port owned every
// account. This deployment has no mail path (repo-wide: no smtp/sendgrid/mailgun
// client), so there is no way to verify ownership out-of-band and no safe way to
// keep the endpoint. It answers 410 Gone and logs the attempt.
//
// An authenticated user changes their own password via PUT /api/user/password.
//
// PR #200 fold F5 (CTO 1790252194343): the locked-out advice carries the
// WHOLE statement. users.updated_at is the credential epoch
// (auth.CredentialEpoch): a hash-only UPDATE moves no epoch, so every
// session signed in before the reset would stay valid. Placeholders only —
// never a real email or hash. Pinned byte for byte, and EXECUTED against a
// temp SQLite store (TestResetPasswordAdviceRetiresPreResetSessionsOnSQLite).
func (s *Server) handleResetPasswordDisabled(c *gin.Context) {
	logger.Warnf("🔒 blocked POST /api/reset-password from %s — endpoint permanently disabled (P0 S2)", c.ClientIP())
	c.JSON(http.StatusGone, gin.H{"error": resetPasswordLockedOutAdvice})
}

const resetPasswordLockedOutAdvice = "Password reset by email is disabled. Sign in and use PUT /api/user/password. " +
	"Locked out? Set the new hash AND the credential epoch in ONE statement: " +
	"UPDATE users SET password_hash='NEW_BCRYPT_HASH', updated_at=CURRENT_TIMESTAMP WHERE email='YOUR_ACCOUNT_EMAIL'; " +
	"— moving updated_at is what signs out every session issued before the reset; a hash-only UPDATE leaves those sessions valid."

// handleResetAccount clears user authentication data so the system returns to
// uninitialized state for re-registration.
//
// SECURITY (P0 S2): this is the most destructive endpoint in the system — it
// deletes EVERY user, trader and strategy. It used to be public. It now requires
// all of:
//   - a valid JWT (it is registered in the `protected` group), AND
//   - that JWT's email equal to its user row's (M3 red-team H1 — never a token
//     that only carries the user_id, like the Telegram bot's), AND
//   - ALLOW_ACCOUNT_RESET=1 in the server environment (default OFF), AND
//   - an explicit {"confirm":"RESET-ALL-DATA"} body.
//
// Note also that registration no longer adopts orphaned credential rows (S3), so
// a reset no longer hands the next registrant the previous owner's keys.
func (s *Server) handleResetAccount(c *gin.Context) {
	// H1 (M3 red team): the actor must be the account itself, not a token
	// that merely carries its user_id (the Telegram bot's) — checked FIRST,
	// so a refused actor learns nothing about the env flag.
	if _, why := s.credentialActorRefusal(c); why != "" {
		credentialForbid(c, why)
		return
	}
	if os.Getenv("ALLOW_ACCOUNT_RESET") != "1" {
		logger.Warnf("🔒 blocked POST /api/reset-account from %s (user %s) — ALLOW_ACCOUNT_RESET is not enabled",
			c.ClientIP(), c.GetString("user_id"))
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Account reset is disabled. Set ALLOW_ACCOUNT_RESET=1 in the server environment and restart to enable it.",
		})
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Confirm != accountResetConfirmToken {
		logger.Warnf("🔒 blocked POST /api/reset-account from %s (user %s) — missing/incorrect confirm token",
			c.ClientIP(), c.GetString("user_id"))
		SafeBadRequest(c, `Account reset requires {"confirm":"`+accountResetConfirmToken+`"}`)
		return
	}

	logger.Warnf("⚠️  ACCOUNT RESET authorized by user %s from %s — deleting all users/traders/strategies",
		c.GetString("user_id"), c.ClientIP())

	err := s.store.Transaction(func(tx *gorm.DB) error {
		// Delete traders and strategies (config, not funds)
		tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&store.Trader{})
		tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&store.Strategy{})
		// Delete users — ai_models and exchanges are intentionally kept
		// so private keys and exchange configs survive re-registration
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&store.User{}).Error; err != nil {
			return fmt.Errorf("failed to delete users: %w", err)
		}
		return nil
	})
	if err != nil {
		SafeInternalError(c, "Failed to reset account", err)
		return
	}

	logger.Infof("✓ User accounts cleared (private keys preserved) — system reset to uninitialized")
	c.JSON(http.StatusOK, gin.H{"message": "Account reset successful, you can now register a new account"})
}

// adoptOrphanRecords was DELETED (P0 S3, 2026-08-16).
//
// It re-assigned every ai_models/exchanges row whose user_id no longer existed —
// model API keys, exchange API keys, private keys — to a newly registered
// user. That made credential inheritance an automatic side effect of signup and
// was the payoff step of the unauthenticated-takeover chain in
// docs/superpowers/reports/2026-08-16-acceptance-gate-v2.md (finding #1).
//
// Do not reintroduce it. If an operator genuinely needs to re-home credentials
// after a reset, that is a deliberate database action taken with knowledge of
// which rows are being moved and to whom.

// initUserDefaultConfigs Initialize default configs for new user
func (s *Server) initUserDefaultConfigs(userID string, lang string) error {
	if err := s.createDefaultStrategies(userID, lang); err != nil {
		logger.Warnf("Failed to create default strategies for user %s: %v", userID, err)
		// Non-fatal: user can create strategies manually
	}
	logger.Infof("✓ User %s registration completed with default strategies", userID)
	return nil
}

func (s *Server) createDefaultStrategies(userID string, lang string) error {
	type strategyI18n struct {
		name, description string
	}
	type strategyLocale struct {
		balanced, conservative, aggressive strategyI18n
	}
	locales := map[string]strategyLocale{
		"zh": {
			balanced:     strategyI18n{"均衡策略", "系统默认策略。均衡风险收益，适合大多数市场环境。5倍杠杆，最多3个仓位。"},
			conservative: strategyI18n{"稳健策略", "系统默认策略。低杠杆保守操作，优先保护本金。3倍杠杆，专注主流资产。"},
			aggressive:   strategyI18n{"积极策略", "系统默认策略。高杠杆主动交易，更广泛的币种选择，适合经验丰富的交易者。10倍杠杆，最多5个仓位。"},
		},
		"en": {
			balanced:     strategyI18n{"Balanced Strategy", "System default strategy. Balanced risk-reward, suitable for most market conditions. 5x leverage, up to 3 positions."},
			conservative: strategyI18n{"Conservative Strategy", "System default strategy. Low-leverage conservative trading, capital preservation first. 3x leverage, focused on major assets."},
			aggressive:   strategyI18n{"Aggressive Strategy", "System default strategy. High-leverage active trading, wider asset selection, for experienced traders. 10x leverage, up to 5 positions."},
		},
		"id": {
			balanced:     strategyI18n{"Strategi Seimbang", "Strategi default sistem. Risiko-reward seimbang, cocok untuk sebagian besar kondisi pasar. Leverage 5x, hingga 3 posisi."},
			conservative: strategyI18n{"Strategi Konservatif", "Strategi default sistem. Trading konservatif leverage rendah, utamakan perlindungan modal. Leverage 3x, fokus aset utama."},
			aggressive:   strategyI18n{"Strategi Agresif", "Strategi default sistem. Trading aktif leverage tinggi, pilihan aset lebih luas, untuk trader berpengalaman. Leverage 10x, hingga 5 posisi."},
		},
	}
	locale, ok := locales[lang]
	if !ok {
		locale = locales["en"]
	}

	type strategyDef struct {
		name        string
		description string
		isActive    bool
		applyConfig func(*store.StrategyConfig)
	}

	definitions := []strategyDef{
		{
			name:        locale.balanced.name,
			description: locale.balanced.description,
			isActive:    true,
			applyConfig: func(c *store.StrategyConfig) {
				// Uses default config as-is
			},
		},
		{
			name:        locale.conservative.name,
			description: locale.conservative.description,
			isActive:    false,
			applyConfig: func(c *store.StrategyConfig) {
				c.RiskControl.BTCETHMaxLeverage = 3
				c.RiskControl.AltcoinMaxLeverage = 3
				c.RiskControl.BTCETHMaxPositionValueRatio = 3.0
				c.RiskControl.AltcoinMaxPositionValueRatio = 0.5
				c.RiskControl.MinConfidence = 80
				c.RiskControl.MinRiskRewardRatio = 4.0
				c.Indicators.Klines.SelectedTimeframes = []string{"15m", "1h", "4h"}
				c.Indicators.Klines.PrimaryTimeframe = "15m"
			},
		},
		{
			name:        locale.aggressive.name,
			description: locale.aggressive.description,
			isActive:    false,
			applyConfig: func(c *store.StrategyConfig) {
				c.RiskControl.BTCETHMaxLeverage = 10
				c.RiskControl.AltcoinMaxLeverage = 7
				c.RiskControl.MaxPositions = 5
				c.RiskControl.AltcoinMaxPositionValueRatio = 2.0
				c.RiskControl.MinConfidence = 70
				c.Indicators.Klines.SelectedTimeframes = []string{"3m", "15m", "1h"}
				c.Indicators.Klines.PrimaryTimeframe = "3m"
			},
		},
	}

	// GetDefaultStrategyConfig only supports zh/en; map id -> en
	configLang := lang
	if lang == "id" {
		configLang = "en"
	}

	// Pre-build all strategy objects before opening the transaction
	var strategies []*store.Strategy
	for _, def := range definitions {
		config := store.GetDefaultStrategyConfig(configLang)
		def.applyConfig(&config)

		strategy := &store.Strategy{
			ID:          uuid.New().String(),
			UserID:      userID,
			Name:        def.name,
			Description: def.description,
			IsActive:    def.isActive,
			IsDefault:   false,
		}
		if err := strategy.SetConfig(&config); err != nil {
			return fmt.Errorf("failed to set config for strategy %q: %w", def.name, err)
		}
		strategies = append(strategies, strategy)
	}

	return s.store.Transaction(func(tx *gorm.DB) error {
		for _, strategy := range strategies {
			if err := tx.Create(strategy).Error; err != nil {
				return fmt.Errorf("failed to create strategy %q: %w", strategy.Name, err)
			}
			logger.Infof("  ✓ Created default strategy: %s (active=%v)", strategy.Name, strategy.IsActive)
		}
		return nil
	})
}
