package main

import (
	"fmt"
	"log/slog"
	nofxiagent "nofx/agent"
	"nofx/api"
	"nofx/auth"
	"nofx/config"
	"nofx/crypto"
	"nofx/expectancy"
	"nofx/kernel"
	"nofx/logger"
	"nofx/manager"
	"nofx/mcp"
	_ "nofx/mcp/payment"
	_ "nofx/mcp/provider"
	"nofx/store"
	"nofx/telegram"
	"nofx/telemetry"
	"nofx/trader"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env environment variables
	_ = godotenv.Load()

	// Initialize logger
	logger.Init(nil)

	logger.Info("╔════════════════════════════════════════════════════════════╗")
	logger.Info("║           🚀 NOFX - AI-Powered Trading System              ║")
	logger.Info("╚════════════════════════════════════════════════════════════╝")

	// Initialize global configuration (loaded from .env)
	config.Init()
	cfg := config.Get()
	logger.Info("✅ Configuration loaded")

	// Initialize encryption service BEFORE database (so EncryptedString can decrypt on read)
	logger.Info("🔐 Initializing encryption service...")
	cryptoService, err := crypto.NewCryptoService()
	if err != nil {
		logger.Fatalf("❌ Failed to initialize encryption service: %v", err)
	}
	crypto.SetGlobalCryptoService(cryptoService)
	logger.Info("✅ Encryption service initialized successfully")

	// Initialize database from configuration
	// For backward compatibility: command line arg overrides config (SQLite only)
	if len(os.Args) > 1 {
		cfg.DBPath = os.Args[1]
	}
	// Ensure data directory exists (for SQLite)
	if cfg.DBType == "sqlite" {
		if dir := filepath.Dir(cfg.DBPath); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Errorf("Failed to create data directory: %v", err)
			}
		}
	}

	logger.Infof("📋 Initializing database (%s)...", cfg.DBType)
	dbType := store.DBTypeSQLite
	if cfg.DBType == "postgres" {
		dbType = store.DBTypePostgres
	}
	st, err := store.NewWithConfig(store.DBConfig{
		Type:     dbType,
		Path:     cfg.DBPath,
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		SSLMode:  cfg.DBSSLMode,
	})
	if err != nil {
		logger.Fatalf("❌ Failed to initialize database: %v", err)
	}
	defer st.Close()

	// P6 (ledger-close 2026-08-19) — WARN+ERROR→DB log shipping. Attached
	// AFTER the store exists (the logger boots first); non-blocking by the
	// LogEventStore contract (select-default drop + single writer + daily
	// LOG_DB_RETENTION_DAYS prune).
	logger.AttachDBSink(func(tsMs int64, level, component, traderID, message, fieldsJSON string) {
		st.LogEvent().Enqueue(store.LogEventDB{
			TsUTC: tsMs, Level: level, Component: component,
			TraderID: traderID, Message: message, FieldsJSON: fieldsJSON,
		})
	})
	logger.Infof("🧾 log-shipping active: WARN+ → log_events (retention %s days; async, drop-on-overload)",
		func() string {
			if v := os.Getenv("LOG_DB_RETENTION_DAYS"); v != "" {
				return v
			}
			return "30"
		}())

	// 6.7 (final-bundle) — one-time entry_confidence backfill from decision
	// records (flag-guarded, WHERE-scoped, additive; feeds the watcher scoring
	// table's history).
	st.BackfillEntryConfidence()

	// P0 pnl-record-integrity (2026-08-20) — one-time additive correction of
	// the 37 wrong recorded-PnL rows (originals preserved; readers COALESCE).
	st.CorrectHistoricalPnL()
	// ATTRIBUTION (2026-09-02) — one sentinel: day-plan-era CLOSED rows carrying
	// "" become UNRESOLVABLE. Idempotent, WHERE-scoped, pre-era history untouched.
	st.ConvergePlanLinkSentinel()

	// T7 (2026-08-27) — stamp pnl_corrected on EVERY reconstructable closed MNQ
	// row (the column must be complete, not just the disagreements).
	st.BackfillPnlCorrectedAll()

	// Initialize installation ID for experience improvement (anonymous statistics)
	initInstallationID(st)

	// Set JWT secret
	auth.SetJWTSecret(cfg.JWTSecret)
	logger.Info("🔑 JWT secret configured")

	// P0 timezone — CT is canonical for EVERY rendered time (owner rule
	// 2026-08-19). The host's local zone is ignored by every renderer.
	logger.Infof("🕐 Timezone pinned: %s (CT) — all prompts, cards, digests and logs render CT, host TZ ignored", kernel.CanonicalZone)

	// P0 2026-08-19 — print every effective AI parameter at startup so a silent
	// default can never hide again (the max_tokens=2000 disease). Any knob the
	// operator did NOT set explicitly is called out as a WARNING.
	ai := mcp.EffectiveAIParamsSnapshot(mcp.DefaultDeepSeekModel)
	logger.Infof("🧠 AI params in force: model=%s client_max_tokens=%d planner_max_tokens=%d temperature=%.2f top_p=%s timeout=%ds (HTTP ceiling; non-stream paths) planner_stream_idle=%ds planner_stream_total=%ds retries=%d backoff=%ds · truncated-responses=%d",
		ai.Model, ai.MaxTokens, trader.PlannerMaxTokens(), ai.Temperature, formatTopP(ai.TopP), ai.TimeoutSeconds, kernel.PlannerStreamIdleSeconds(), kernel.PlannerStreamTotalSeconds(), ai.MaxRetries, ai.RetryBackoffSeconds, mcp.TruncatedResponses.Load())
	unset := []string{}
	if !ai.MaxTokensSet {
		unset = append(unset, "AI_MAX_TOKENS")
	}
	if !ai.TemperatureSet {
		unset = append(unset, "AI_TEMPERATURE")
	}
	if !ai.TopPSet {
		unset = append(unset, "AI_TOP_P")
	}
	if !ai.TimeoutSet {
		unset = append(unset, "AI_TIMEOUT_SECONDS")
	}
	if !ai.MaxRetriesSet {
		unset = append(unset, "AI_MAX_RETRIES")
	}
	if !ai.RetryBackoffSet {
		unset = append(unset, "AI_RETRY_BACKOFF_SECONDS")
	}
	// P0 2026-08-19 — agent sub-call token caps are AI parameters too; audit
	// them the same way.
	ac := nofxiagent.AITokenCapsSnapshot()
	logger.Infof("🤖 agent sub-call caps: taskstate_summary=%d taskstate_incremental=%d replanner=%d",
		ac.TaskStateSummary, ac.TaskStateIncremental, ac.Replanner)
	if !ac.SummarySet {
		unset = append(unset, "AI_TASKSTATE_SUMMARY_MAX_TOKENS")
	}
	if !ac.IncrementalSet {
		unset = append(unset, "AI_TASKSTATE_INCREMENTAL_MAX_TOKENS")
	}
	if !ac.ReplannerSet {
		unset = append(unset, "AI_REPLANNER_MAX_TOKENS")
	}
	if len(unset) > 0 {
		logger.Warnf("⚠️ AI params at UNSET defaults (nobody chose these explicitly): %v — set them in .env if the defaults are not what you intend", unset)
	}

	// WebSocket market monitor is NO LONGER USED
	// All K-line data now comes from CoinAnk API instead of Binance WebSocket cache
	// Commented out to reduce unnecessary connections:
	// go market.NewWSMonitor(150).Start(nil)
	// logger.Info("📊 WebSocket market monitor started")
	// time.Sleep(500 * time.Millisecond)
	logger.Info("📊 Using CoinAnk API for all market data (WebSocket cache disabled)")

	// Create TraderManager
	traderManager := manager.NewTraderManager()

	// ENTRY-MECHANICS ADDENDUM (2026-08-30) — align stored acceptance rules
	// with the new per-condition entry law BEFORE traders load their config
	// (the old "2x5m" string would contradict the validator → reject loops).
	if n1, n2, merr := st.Strategy().RepairAcceptanceRuleMigration(); merr != nil {
		logger.Warnf("⚠️ acceptance-rule migration FAILED: %v (resolver self-heals at read; fix the DB row)", merr)
	} else if n1+n2 > 0 {
		logger.Infof("🩹 acceptance-rule migration: strategy-level=%d session=%d (2x5m → 5m_close)", n1, n2)
	}

	// Load all traders from database to memory (may auto-start traders with IsRunning=true)
	if err := traderManager.LoadTradersFromStore(st); err != nil {
		logger.Fatalf("❌ Failed to load traders: %v", err)
	}

	// Display loaded trader information
	traders, err := st.Trader().List("default")
	if err != nil {
		logger.Fatalf("❌ Failed to get trader list: %v", err)
	}

	logger.Info("🤖 AI Trader Configurations in Database:")
	if len(traders) == 0 {
		logger.Info("  (No trader configurations, please create via Web interface)")
	} else {
		for _, t := range traders {
			status := "❌ Stopped"
			if t.IsRunning {
				status = "✅ Running"
			}
			idShort := t.ID
			if len(idShort) > 8 {
				idShort = idShort[:8]
			}
			logger.Infof("  • %s [%s] %s - AI Model: %s, Exchange: %s",
				t.Name, idShort, status, t.AIModelID, t.ExchangeID)
		}
	}

	// Plan 4 Task 23 — risk + audit endpoints are registered inside
	// api.NewServer / setupRoutes (gin router); see api/server.go for
	// POST /api/risk/force-flat, GET /api/risk/status, GET /api/audit/decisions.

	// Plan 4 Task 25 — Prometheus metrics endpoint (T25 owns this marker; T23 leaves space below).

	// Start API server
	// P1 — BOOT INTEGRITY ASSERTION. Runs before any trader cycles. A mismatch
	// with the intended release, or a drifted prompt golden, REFUSES TRADING for
	// this process (entries blocked; everything else stays read-only usable).
	integrity := kernel.AssertBootIntegrity()
	if integrity.Refused {
		logger.Errorf("%s", integrity.Line())
		logger.Errorf("🔐 TRADING REFUSED — %s", integrity.Reason)
		logger.Errorf("🔐 No new positions will be opened until this is fixed and the bot is restarted.")
	} else {
		logger.Infof("%s", integrity.Line())
	}
	// UI SERVING PATH (owner ruling 2026-09-03). Printed right after the boot
	// integrity line because it answers the same question about a different
	// artifact: is what is being SERVED the thing that was just BUILT. The
	// binary's build time comes from the integrity result above — one reader of
	// debug.ReadBuildInfo in the process, not two that can disagree.
	{
		binAt, perr := time.Parse(time.RFC3339, integrity.BuildTime)
		if perr != nil {
			binAt = time.Time{} // unknown → the staleness comparison is skipped, not guessed
		}
		uiLine := api.UIServingBootLineAt(api.UIDistDir, binAt)
		if strings.Contains(uiLine, "STALE") || strings.Contains(uiLine, "served-by=none") {
			logger.Warnf("🖥 %s", uiLine)
		} else {
			logger.Infof("🖥 %s", uiLine)
		}
	}
	// PHASE 3.5 — clock health at boot (log-only; repeated at each session roll
	// by the trader loop). At boot the NT8 wire may not be up yet — the line
	// says "none" honestly rather than waiting.
	kernel.LogClockHealth("boot", "MNQ")
	// REGIME WAVE (Cutover 2, 2026-08-21) — one boot line per regime knob:
	// value + source, so the boot block self-documents the wave's enforcement.
	kernel.LogRegimeBootLedger()
	// PACK B (2026-08-26) — volume wave boot line: one line per shipped knob so
	// the boot block self-documents the wave (dispatch: boot adds the knobs).
	kernel.ApplyRoleMapOverrides(os.Getenv("LEVEL_ROLE_MAP"))
	kernel.LogVolumeWaveBoot() // PRE-SUNDAY F5 (2026-08-28) — scenario schema ledger: the full
	// condition vocabulary in the boot block, so a schema change can never
	// ship silently again (the 8th-condition parse-reject class).
	logger.Infof("📜 %s", kernel.ScenarioSchemaLedger())
	// ENTRY-MECHANICS (E1-E9, 2026-08-30) — the confirm-rule enum + the entry
	// knobs in the boot block: the 5-rule vocabulary and the seam state must be
	// verifiable from the boot line alone (the owner's cutover checklist).
	logger.Infof("🔐 %s", kernel.ConfirmRuleLedger())
	// CLASS 35 (2026-09-01) — the re-plan budget accounting mode in the boot
	// block: which trigger classes spend, which are free, and the counter key.
	logger.Infof("🧮 %s", store.ReplanBudgetBootLine())
	// CLASS 36 (2026-09-01) — planner preflight scope in the boot block.
	logger.Infof("🗓 %s", trader.PreflightBootLine())
	// 0B (2026-09-02) — the exit posture in one line: the stop composition, the
	// suspended mechanisms, the Stage-A size and the boot-sweep re-arm.
	logger.Infof("🛑 %s", trader.ExitPolicyBootLineLive(kernel.MinSLATRMult()))
	// CLASS 45 (2026-09-02) — what the prompt now feeds forward. Boot ORDER by
	// name (owner ruling): 📜 prompt feeds forward → ⏱ wakes → 🚫 no-chase.
	logger.Infof("📜 %s", kernel.PromptFeedsForwardBootLine(-1, 0, kernel.MinSLATRMult()))
	// CLASS 47 (2026-09-02) — wake cadence: every field READ from its resolver.
	logger.Infof("⏱ %s", trader.WakeCadenceBootLine())
	// INVALIDATION-WIRED (2026-09-03) — the arm gate's new leg and the
	// armed-under surfaces, both READ from the code that implements them.
	logger.Infof("🛡 %s", trader.ArmGateBootLine())
	// ADHERENCE REGRADE (owner ruling 2026-09-03) — flag-guarded. Default OFF,
	// and the line reports what is PENDING either way, so the count is visible
	// without arming anything. Backup first: no backup, no write.
	{
		pending, _ := st.Position().StuckAdherenceRows()
		regraded, backup := 0, ""
		if store.AdherenceRegradeEnabled() && len(pending) > 0 {
			before, _ := st.Position().AdherenceDistribution()
			if b, bErr := store.BackupBeforeRegrade(cfg.DBPath, time.Now().Format("20060102-150405")); bErr != nil {
				logger.Errorf("🩹 adherence regrade ABORTED — backup failed: %v", bErr)
			} else {
				backup = b
				if n, rErr := st.Position().RegradeStuckAdherence(); rErr != nil {
					logger.Errorf("🩹 adherence regrade failed: %v", rErr)
				} else {
					regraded = n
					after, _ := st.Position().AdherenceDistribution()
					logger.Infof("🩹 adherence distribution before %v → after %v (cleared rows are ungraded until W5 recomputes them)", before, after)
				}
			}
		}
		logger.Infof("🩹 %s", store.AdherenceRegradeBootLine(len(pending), regraded, store.AdherenceRegradeEnabled(), backup))
	}
	// VOID PARITY (2026-09-02) — the ONE scope the prompt's VOID list and the
	// write-site validator both read. Every field READ from its resolver.
	logger.Infof("📜 %s", kernel.VoidScopeBootLine())
	// NO-TRADE BAND (2026-09-02) — the windows the gate, the grader and the
	// card all read, and where each one comes from. Every field resolved.
	logger.Infof("🗓 %s", kernel.NoTradeBandBootLine())
	// TRADE EXCURSIONS (wave 1A, 2026-09-02) — how many positions have a path
	// recorded, how many were rebuilt from the tape, and how many the tape
	// does not reach. Every number READ from the table.
	logger.Infof("📐 %s", st.TradeExcursions().ExcursionBootLine())
	// EXPECTANCY (wave 1D, 2026-09-03) — the per-condition table, READ. Every
	// number comes from the table just built, so the line cannot claim a cell
	// count the process did not compute. A failure here WARNs and boots on:
	// this is a read model and it may never stop the loop (class 23 / A10).
	if xt, xerr := expectancy.LoadAndBuildAt(st.GormDB(), time.Now()); xerr != nil {
		logger.Warnf("📊 expectancy: table unavailable at boot: %v", xerr)
	} else {
		logger.Infof("%s", xt.BootLine())
	}
	// P&L-TRUTH WAVE (2026-09-01) — corrected-column guard in the boot block.
	logger.Infof("🧾 %s", store.PnLSurfacesBootLine())
	// ATTRIBUTION — counts READ from the table, never a literal.
	logger.Infof("🔗 %s", st.AttributionBootLine())
	logger.Infof("🎛 %s", kernel.EntryLawBootLedger()) // P1.4 (ledger-close 2026-08-19) — clock-guard block: live host-RTC drift,
	// guard-timer freshness, last resync/check state. Log-only, best-effort.
	kernel.LogClockGuardBoot()
	// P4 (ledger-close 2026-08-19) — half-days boot line: loaded count + the
	// next upcoming early close. Fail-open on a bad file.
	trader.LogHalfDaysBoot(time.Now())

	// SANDBOX: a demo instance has no NT8 wire, so install a deterministic
	// synthetic bar feed — without it level_facts/price/chart/armor are all empty.
	if cfg.SandboxMode {
		api.InstallSandboxBars("MNQ", 30231.5)
		logger.Warnf("🧪 SANDBOX MODE — synthetic bars installed, canned planner replies, no live trading")
	}

	server := api.NewServer(traderManager, st, cryptoService, cfg.APIServerHost, cfg.APIServerPort)

	// Create hot-reload channel for Telegram bot; wire it to the API server
	// so that POST /api/telegram can trigger a bot restart when the token changes.
	telegramReloadCh := make(chan struct{}, 1)
	server.SetTelegramReloadCh(telegramReloadCh)

	// Start the NOFXi web agent on top of the current dev branch services.
	nofxiAgent := nofxiagent.New(traderManager, st, nil, slog.Default())
	agentWeb := nofxiagent.NewWebHandler(nofxiAgent, slog.Default())
	server.RegisterAgentHandler(agentWeb)
	nofxiAgent.Start()
	defer nofxiAgent.Stop()

	go func() {
		if err := server.Start(); err != nil {
			logger.Fatalf("❌ Failed to start API server: %v", err)
		}
	}()

	// Start Telegram bot (if TELEGRAM_BOT_TOKEN is configured)
	go telegram.Start(cfg, st, telegramReloadCh)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ System started successfully, waiting for trading commands...")
	logger.Info("📌 Tip: Use Ctrl+C to stop the system")

	<-quit
	logger.Info("📴 Shutdown signal received, closing system...")

	if err := server.Shutdown(); err != nil {
		logger.Warnf("⚠️ HTTP server shutdown error: %v", err)
	}
	logger.Info("✅ HTTP server stopped")

	// nofxiAgent.Stop() is handled by defer above

	// Stop all traders
	traderManager.StopAll()
	logger.Info("✅ System shut down safely")
}

// initInstallationID initializes the anonymous installation ID for experience improvement
// This ID is persisted in database and used for anonymous usage statistics
func initInstallationID(st *store.Store) {
	const key = "installation_id"

	// Try to load from database
	installationID, err := st.GetSystemConfig(key)
	if err != nil {
		logger.Warnf("⚠️ Failed to load installation ID: %v", err)
	}

	// Generate new ID if not exists
	if installationID == "" {
		installationID = uuid.New().String()
		if err := st.SetSystemConfig(key, installationID); err != nil {
			logger.Warnf("⚠️ Failed to save installation ID: %v", err)
		}
		logger.Infof("📊 Generated new installation ID: %s", installationID[:8]+"...")
	}

	// Set installation ID in experience module
	telemetry.SetInstallationID(installationID)
}

// formatTopP renders the top_p value for the startup log (0 = omitted).
func formatTopP(v float64) string {
	if v <= 0 {
		return "omitted"
	}
	return fmt.Sprintf("%.2f", v)
}
