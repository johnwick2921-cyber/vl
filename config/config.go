package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"vl/internal/friendmode"
	"vl/internal/installpath"
	"vl/logger"
	"vl/mcp"
	"vl/telemetry"
)

// Global configuration instance
var tradingModeWarnOnce sync.Once // C2: the TRADING_MODE-ignored warning fires once per process

var global *Config

// InsecureDefaultJWTSecret is the PUBLIC fallback used when JWT_SECRET is
// unset. Anything signed with it can be forged by anyone who has read this
// file, so the update routes (api/handler_updates.go, W-ONE-BUTTON M3 F2)
// refuse every request while the process runs on it.
const InsecureDefaultJWTSecret = "default-jwt-secret-change-in-production"

// MinUpdatesJWTSecretBytes is the shortest JWT secret the update routes
// accept (W-ONE-BUTTON M3 red-team fold M1). The loader itself accepts any
// length — only /api/updates* refuse a shorter one.
const MinUpdatesJWTSecretBytes = 32

// publicJWTSecrets are the JWT secrets this PUBLIC repository publishes: the
// loader's default, the .env.example placeholder (INSTALL.md says `cp
// .env.example .env`, and only start.sh regenerates it), and the CI literal
// in .github/workflows/pr-docker-compose-healthcheck.yml. A census over the
// tracked tree (api/handler_updates_secret_test.go) fails when a literal of
// 32+ bytes lands anywhere in the repo without being listed here.
var publicJWTSecrets = []string{
	InsecureDefaultJWTSecret,
	"your-jwt-secret-change-this-in-production", // .env.example
	"test-jwt-secret-minimum-32-chars",          // CI docker-compose healthcheck
}

// JWTSecretUnfitForUpdates reports why secret cannot anchor the update
// administrator's identity ("" = fit): empty, one of the secrets this public
// repository publishes (compared after TrimSpace, as the loader trims), or
// shorter than MinUpdatesJWTSecretBytes. A token signed with such a secret
// can be forged by anyone who reads the repo or searches the short key space.
// Used ONLY by /api/updates* — the loader and the rest of the app are
// unchanged (CTO ruling: the app keeps its load-time WARN; the refusal is the
// update routes').
func JWTSecretUnfitForUpdates(secret []byte) string {
	s := strings.TrimSpace(string(secret))
	if s == "" {
		return "JWT secret empty"
	}
	for _, p := range publicJWTSecrets {
		if s == p {
			return "JWT secret is a public placeholder"
		}
	}
	if len(s) < MinUpdatesJWTSecretBytes {
		return "JWT secret shorter than 32 bytes"
	}
	return ""
}

// Config is the global configuration (loaded from .env)
// Only contains truly global config, trading related config is at trader/strategy level
type Config struct {
	// Service configuration
	APIServerHost string // interface the HTTP API binds to; default 127.0.0.1 (loopback only)
	APIServerPort int
	JWTSecret     string

	// Friend instance (W-FRIEND-HOSTED Segment 2, DS-102): VL_FRIEND_INSTANCE=1
	// turns this process into a friend's own bot copy. Trading + settings stay
	// fully functional; the owner-only surfaces (updater, installer,
	// maintenance, Telegram) are gone and both binds must be specific
	// addresses (never 0.0.0.0 / :: / empty).
	FriendInstance bool   // VL_FRIEND_INSTANCE
	FriendName     string // VL_FRIEND_NAME (lowercase a-z0-9, 2-20 chars)

	// Database configuration
	DBType string // sqlite or postgres
	DBPath string // SQLite database file path
	// SandboxMode (SANDBOX_MODE=1) turns this process into an isolated demo: the
	// trading loop never places an order, the planner uses canned replies unless
	// SANDBOX_LLM=real, and the UI paints a SANDBOX banner. Default OFF — the live
	// bot is byte-identical unless the env var is set.
	SandboxMode bool
	DBHost      string // PostgreSQL host
	DBPort      int    // PostgreSQL port
	DBUser      string // PostgreSQL user
	DBPassword  string // PostgreSQL password
	DBName      string // PostgreSQL database name
	DBSSLMode   string // PostgreSQL SSL mode

	// Security configuration
	// TransportEncryption enables browser-side encryption for API keys
	// Requires HTTPS or localhost. Set to false for HTTP access via IP.
	TransportEncryption bool

	// Experience improvement (anonymous usage statistics)
	// Helps us understand product usage and improve the experience
	// Set EXPERIENCE_IMPROVEMENT=false to disable
	ExperienceImprovement bool

	// Market data provider API keys
	AlpacaAPIKey    string // Alpaca API key for US stocks
	AlpacaSecretKey string // Alpaca secret key
	TwelveDataKey   string // TwelveData API key for forex & metals

	// Databento (NQ futures data)
	DatabentoAPIKey string

	// NinjaTrader (CSV bridge for execution)

	// AllowedNTAccounts is the allow-list of NT8 sub-account names a trader may be
	// bound to (multi-account safety rail; env NT_ALLOWED_ACCOUNTS, comma-separated).
	// Empty = no extra restriction beyond the server-side SIM-only guard. A non-empty
	// list HARD-blocks selecting any account not on it (e.g. keep a funded account off
	// the list so it can never be chosen). This is the rail Stage-2 routing will enforce.
	AllowedNTAccounts []string

	// Plan 3 Task 21 — Risk limits (hard server-side kill switches).
	// Defaults: $500 daily loss, 2 concurrent trades, $50k notional, 5 contracts/order.
	// Zero value disables the corresponding check.
	RiskMaxDailyLossUSD     float64 // RISK_MAX_DAILY_LOSS_USD
	RiskMaxConcurrentTrades int     // RISK_MAX_CONCURRENT_TRADES
	RiskMaxNotionalUSD      float64 // RISK_MAX_NOTIONAL_USD

	// 4.3 — limit-then-market EOD/T1 flatten (research v5 "slippage budgeted").
	// DORMANT by default: 0/0 = pure market flatten, byte-identical behavior.
	// LimitCloseTicks > 0 makes the session/T1 flatten FIRST place a limit exit
	// that many ticks beyond the latest bar close (favorable side) and fall back
	// to a market flatten after LimitCloseMarketAfterSec seconds.
	LimitCloseTicks        int // EOD_FLAT_LIMIT_TICKS
	LimitCloseMarketAfterS int // EOD_FLAT_MARKET_AFTER_SEC

	// Plan-lifecycle wave (hysteresis + rearm, 2026-08-27):
	// FlipATRBuffer is the deadband beyond a structured flip/death line before
	// closes count against it (FLIP_ATR_BUFFER × ATR14(5m), default 0.5).
	// FlipConfirmCloses is the minimum CONSECUTIVE decision-TF closes beyond the
	// buffered line required for invalidation (FLIP_CONFIRM_CLOSES, default 2).
	FlipATRBuffer     float64
	FlipConfirmCloses int
	// Latency routing: executor in-loop calls run cheap thinking
	// (AI_EXEC_REASONING: off|fast|low|high|max, default fast = wire effort low);
	// planner reads keep full reasoning (AI_PLAN_REASONING, default max).
	AIExecReasoning string
	AIPlanReasoning string
}

// Init initializes global configuration (from .env)
func Init() {
	cfg := &Config{
		// SECURITY: loopback by default. The API carries broker/model credentials
		// and trader control; it must never be reachable off-host unless the
		// operator opts in explicitly via API_SERVER_HOST.
		APIServerHost:         "127.0.0.1",
		APIServerPort:         8080,
		ExperienceImprovement: true, // Default: enabled to help improve the product
		// Database defaults
		DBType:    "sqlite",
		DBPath:    "data/data.db",
		DBHost:    "localhost",
		DBPort:    5432,
		DBUser:    "postgres",
		DBName:    "vl",
		DBSSLMode: "disable",
	}

	// Load from environment variables
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = strings.TrimSpace(v)
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = InsecureDefaultJWTSecret
		logger.Warnf("⚠️  JWT_SECRET env var not set; using INSECURE default. " +
			"Acceptable for localhost-only paper trading. " +
			"Set JWT_SECRET in .env (e.g. `openssl rand -base64 64`) before any network-exposed deploy.")
	}

	// W-FRIEND-HOSTED Segment 2: friend-instance flags. VL_FRIEND_INSTANCE is the
	// ONE parser (FriendInstanceEnabled: "1"/"true", trimmed). The name is
	// lowercased + trimmed here (the B2 contract: lowercase a-z0-9, 2-20 chars);
	// an invalid name is NOT refused at load — main.go validates it before boot
	// (a friend copy with a bad name must not silently run as the owner).
	if FriendInstanceEnabled() {
		cfg.FriendInstance = true
		cfg.FriendName = strings.ToLower(strings.TrimSpace(os.Getenv("VL_FRIEND_NAME")))
	}

	if v := os.Getenv("API_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.APIServerPort = port
		}
	}

	// API_SERVER_HOST overrides the bind interface. "0.0.0.0" (or any non-loopback
	// address) exposes the API to the network — warn loudly, since the surface
	// includes trader control and credential-bearing config endpoints.
	if v := strings.TrimSpace(os.Getenv("API_SERVER_HOST")); v != "" {
		cfg.APIServerHost = v
		if v != "127.0.0.1" && v != "localhost" && v != "::1" {
			logger.Warnf("⚠️  API_SERVER_HOST=%q — the API is bound OFF-LOOPBACK and reachable from the network. "+
				"Ensure JWT_SECRET is strong and a firewall/reverse proxy fronts it.", v)
		}
	}

	// Transport encryption: default false for easier deployment
	// Set TRANSPORT_ENCRYPTION=true to enable (requires HTTPS or localhost)
	if v := os.Getenv("TRANSPORT_ENCRYPTION"); v != "" {
		cfg.TransportEncryption = strings.ToLower(v) == "true"
	}

	// Experience improvement: anonymous usage statistics
	// Default enabled, set EXPERIENCE_IMPROVEMENT=false to disable
	if v := os.Getenv("EXPERIENCE_IMPROVEMENT"); v != "" {
		cfg.ExperienceImprovement = strings.ToLower(v) != "false"
	}

	// Market data provider API keys
	cfg.AlpacaAPIKey = os.Getenv("ALPACA_API_KEY")
	cfg.AlpacaSecretKey = os.Getenv("ALPACA_SECRET_KEY")
	cfg.TwelveDataKey = os.Getenv("TWELVEDATA_API_KEY")

	// Databento + NinjaTrader (NQ futures path)
	cfg.DatabentoAPIKey = os.Getenv("DATABENTO_API_KEY")
	// (6.8) DATABENTO_DATASET + NINJATRADER_DATA_DIR loads removed — zero live
	// readers [A, PR #54]: the databento client hardcodes its dataset and the
	// NT8 data dir comes from the exchange row; only cmd/nq_smoke reads the env
	// directly.
	for _, a := range strings.Split(os.Getenv("NT_ALLOWED_ACCOUNTS"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			cfg.AllowedNTAccounts = append(cfg.AllowedNTAccounts, a)
		}
	}
	// C2 — futures-only build: TRADING_MODE is accepted only as "futures" (or
	// unset); any other value is IGNORED with exactly ONE boot warning, never a
	// refusal. (The env line survives in partner .env files without breaking the
	// parse.)
	if v := strings.TrimSpace(os.Getenv("TRADING_MODE")); v != "" && !strings.EqualFold(v, "futures") {
		tradingModeWarnOnce.Do(func() {
			logger.Warnf("TRADING_MODE=%s ignored — futures-only build", v)
		})
	}

	// Plan 3 Task 21 — Risk limits
	cfg.RiskMaxDailyLossUSD = getEnvFloat("RISK_MAX_DAILY_LOSS_USD", 500)
	cfg.RiskMaxConcurrentTrades = getEnvInt("RISK_MAX_CONCURRENT_TRADES", 2)
	// 4.3 — limit-then-market flatten (dormant by default).
	cfg.LimitCloseTicks = getEnvInt("EOD_FLAT_LIMIT_TICKS", 0)
	cfg.LimitCloseMarketAfterS = getEnvInt("EOD_FLAT_MARKET_AFTER_SEC", 0)
	// Plan-lifecycle wave (hysteresis + rearm, 2026-08-27).
	cfg.FlipATRBuffer = getEnvFloat("FLIP_ATR_BUFFER", 0.5)
	cfg.FlipConfirmCloses = getEnvInt("FLIP_CONFIRM_CLOSES", 2)
	cfg.AIExecReasoning = getEnvOrDefault("AI_EXEC_REASONING", "fast")
	cfg.AIPlanReasoning = getEnvOrDefault("AI_PLAN_REASONING", "max")
	// Deprecated-in-practice (6.8): loaded but enforced nowhere — the only
	// CheckPreTrade caller passes 0 notional by design; futures notional is
	// capped by strategy max_notional_leverage instead. Kept for struct compat.
	cfg.RiskMaxNotionalUSD = getEnvFloat("RISK_MAX_NOTIONAL_USD", 50_000)
	// (E2, fail-register wave): RISK_MAX_CONTRACTS_PER_ORDER removed — zero
	// readers ever (the live clamp is strategy max_contracts_per_order else the
	// researched 2).

	// Database configuration
	if v := os.Getenv("DB_TYPE"); v != "" {
		cfg.DBType = strings.ToLower(v)
	}
	if v := os.Getenv("SANDBOX_MODE"); v == "1" || strings.EqualFold(v, "true") {
		cfg.SandboxMode = true
	}
	// W-ONE-BUTTON M2 (MUST-2): the ONE DB-path resolver, shared with the
	// maintenance-hold CLI — same default ("data/data.db"), same DB_PATH rule.
	cfg.DBPath = installpath.DBPath(os.Getenv)
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.DBHost = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.DBPort = port
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.DBUser = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.DBPassword = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DBName = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		cfg.DBSSLMode = v
	}

	global = cfg

	// Initialize experience improvement (installation ID will be set after database init)
	telemetry.Init(cfg.ExperienceImprovement, "")

	// Set up AI token usage tracking callback
	mcp.TokenUsageCallback = func(usage mcp.TokenUsage) {
		telemetry.TrackAIUsage(telemetry.AIUsageEvent{
			ModelProvider: usage.Provider,
			ModelName:     usage.Model,
			Channel:       usage.Channel(),
			InputTokens:   usage.PromptTokens,
			OutputTokens:  usage.CompletionTokens,
		})
	}

	// P0-cleanup (2026-08-19) — a NEW error class announces itself the same day.
	telemetry.ErrorAnnounceFunc = func(trader, typ, cause string, cost telemetry.ErrorCost) {
		logger.Warnf("🚨 NEW ERROR CLASS %q (trader %s): %s [cost %s]", typ, trader, cause, cost)
	}
}

// Get returns the global configuration
func Get() *Config {
	if global == nil {
		Init()
	}
	return global
}

// IsFriendInstance reports whether this process is a friend's bot copy
// (W-FRIEND-HOSTED Segment 2). A nil receiver reads false — the owner's bot
// and every test that never calls Init() stay byte-identical.
func (c *Config) IsFriendInstance() bool {
	return c != nil && c.FriendInstance
}

// ValidFriendName enforces the B2 contract for VL_FRIEND_NAME: lowercase a-z0-9,
// 2-20 characters. It is the ONE canonicalizer for friend names — the
// provisioner (DS-101), the boot check (main.go) and the owner API (DS-106) all
// call it rather than re-deriving the regex.
func ValidFriendName(name string) bool {
	if len(name) < 2 || len(name) > 20 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// FriendInstanceEnabled is the ONE parser for VL_FRIEND_INSTANCE (S21/R3): "1"
// or "true" (trimmed, case-insensitive for "true"), anything else false. The
// canonicalizer lives in internal/friendmode so security can read it without an
// import cycle (config → mcp → security); this is the config-package facade.
func FriendInstanceEnabled() bool {
	return friendmode.Enabled()
}

// JWTSecretIsDefault reports whether the effective JWT secret is the insecure
// hard-coded fallback (P2-13, audit 0926-system): the boot line must print
// the READ state — INSECURE DEFAULT vs custom — instead of an unconditional
// "configured".
func (c *Config) JWTSecretIsDefault() bool {
	if c == nil {
		return true
	}
	return strings.TrimSpace(c.JWTSecret) == "" || strings.TrimSpace(c.JWTSecret) == InsecureDefaultJWTSecret
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}
