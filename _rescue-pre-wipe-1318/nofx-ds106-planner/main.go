package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	nofxiagent "nofx/agent"
	"nofx/api"
	"nofx/auth"
	"nofx/branding"
	"nofx/config"
	"nofx/crypto"
	"nofx/expectancy"
	"nofx/kernel"
	"nofx/logger"
	"nofx/manager"
	"nofx/mcp"
	_ "nofx/mcp/payment"
	_ "nofx/mcp/provider"
	"nofx/researchsnapshot"
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
	ntwire "nofx/provider/ninjatrader"
	ntTrader "nofx/trader/ninjatrader"
)

func main() {
	// Initialize logger first so the .env outcome has somewhere to land
	// (logger.Init reads no environment variable, so config sees the same order)
	logger.Init(nil)

	// Load .env environment variables — fails open: on any error nothing is
	// set and every variable falls back to the process environment; the
	// outcome is logged once (WARN malformed / INFO absent / silent on success)
	loadDotEnv(".env")

	logger.Info("╔════════════════════════════════════════════════════════════╗")
	logger.Info("║           🚀 " + branding.ProductName() + " - AI-Powered Trading System              ║")
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
	// W-ONE-BUTTON M2 (MUST-2): resolve the installation data dir ONCE, with
	// the resolver the maintenance-hold CLI uses, BEFORE traders load (they
	// auto-start inside LoadTradersFromStore) so every entry gate reads the
	// hold file the CLI and the updater write.
	trader.SetMaintenanceDataDir(resolveMaintenanceDataDir(cfg.DBPath))
	// Ensure data directory exists (for SQLite)
	if cfg.DBType == "sqlite" {
		if dir := filepath.Dir(cfg.DBPath); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Errorf("Failed to create data directory: %v", err)
			}
		}
	}

	closeResearch := researchsnapshot.Start(cfg.DBPath+".research.db", func(line string) { logger.Infof("%s", line) }, func(line string) { logger.Warnf("%s", line) })
	defer closeResearch()
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
	// SEAM EXCLUSION (owner ruling 2026-09-03) — clears any grade a test-seam
	// row still carries and stamps the reason IN the row. This call was MISSING
	// from the 042ff360 boot: the boot line shipped and reported "excluded=3"
	// while nothing had been excluded — 573 and 574 still read D and F. The
	// pin below asserts the call exists, because a boot line without its
	// migration is a claim without a mechanism.
	st.StampSeamRowsExcluded()

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
	// Crypto K-lines come from CoinAnk; the CME futures path reads the NT8
	// BarCache only (see the 📊 market data boot line after trader load).
	// Commented out to reduce unnecessary connections:
	// go market.NewWSMonitor(150).Start(nil)
	// logger.Info("📊 WebSocket market monitor started")
	// time.Sleep(500 * time.Millisecond)
	// W-NO-BINANCE A: the "📊 Using CoinAnk API for all market data" literal that
	// stood here was false on the futures path (audit H20). The READ line
	// (trader.MarketDataBootLine) prints after the traders load, below.

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
	// W1 SETTINGS TRUTH (2026-09-23) — consecutive_loss_halt and
	// day_plan.replan_cap are presence-aware now (absent inherits, an explicit
	// 0 is 0). One line per stored strategy: what is stored, what the previous
	// binary enforced, what this one enforces. READ-ONLY — nothing is
	// rewritten; a row whose effective value changes without a Studio save
	// confirming it is REFUSED at trader load (the ⛔ line names it).
	if rep, rerr := st.Strategy().SettingsTruthBootReport(os.Getenv); rerr != nil {
		logger.Warnf("🩺 settings truth: report unavailable (%v) — the per-trader load check still applies", rerr)
	} else {
		for _, line := range rep.Lines() {
			if strings.Contains(line, "CHANGED") {
				logger.Warnf("%s", line)
			} else {
				logger.Infof("%s", line)
			}
		}
	}

	// Load all traders from database to memory (may auto-start traders with IsRunning=true)
	// F12 SINK — REGISTERED BEFORE THE TRADERS LOAD. LoadTradersFromStore builds
	// the first trader, which lazily starts the TCP server, which reads this
	// hook ONCE at start. Registered after that call (as it was on the first
	// boot) the server starts with a nil sink and every received frame is
	// cached but never persisted — the gate keeps working, so nothing looks
	// wrong, and the forensic table stays empty forever. A write on a branch
	// nothing takes.
	// F12 (2026-09-03) — the NT8 AddOn's build id and the broker's order book.
	//
	// The build id is READ from the last frame that carried one, never from our
	// own source: VL_BUILD_ID has been bumped in the repo repeatedly while NT8
	// kept running an older compile, so a line sourced from the constant would
	// report success for a change that never landed (class 6 — proof is a
	// RECEIVED frame). It says match=NO, loudly, until the owner reloads.
	//
	// The snapshot line says which source cutover leg 4 will use. Before the
	// AddOn is reloaded that is the LEDGER, and the line says so — the wave's
	// own transition state, printed rather than assumed.
	ntTrader.SetOrderSnapshotSink(func(p ntwire.OrderSnapshotPayload) {
		b, merr := json.Marshal(p.Orders)
		if merr != nil {
			logger.Warnf("📸 order_snapshot: cannot marshal orders for storage: %v", merr)
			return
		}
		if err := st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
			Account: p.Account, BuildID: p.BuildID, Reason: p.Reason,
			OrdersJSON: string(b), OrderCount: len(p.Orders),
			WorkingCount: len(p.WorkingOrders()),
			EmittedMs:    p.EmittedMs, ReceivedMs: time.Now().UnixMilli(),
		}); err != nil {
			// Forensics are worth having and never worth a frame. The cache
			// already holds the book the gate reads.
			logger.Warnf("📸 order_snapshot: store insert failed (frame still cached): %v", err)
		}
	})

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
	// W-ONE-BUTTON M2 — the installation maintenance hold, every field READ.
	// The AddOn has usually not connected yet, so addon_ack prints n/a here.
	logger.Infof("%s", trader.MaintenanceBootLine(traderManager.GetAllTraders()))
	// W-NO-BINANCE A — the market-data sources, READ (replaces the old literal).
	logger.Infof("%s", trader.MarketDataBootLine(traderManager.GetAllTraders()))
	logger.Infof("%s", researchsnapshot.CurrentBootLine())
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
		// Judged by REV since 2026-09-16 (the served bundle's GUIDE_BUILT_REV vs
		// integrity.Revision); the build time rides along as a secondary field.
		// 🗂 is its OWN line: appending the resolved dir to 🖥 would change that
		// line's bytes and break every golden reading it, while an absent knob
		// is still a fact worth stating (n/a, never an empty gap).
		logger.Infof("🗂 %s", api.ReleaseDirBootLine())
		uiLine := api.UIServingBootLine(api.ResolvedDistDir(), binAt, integrity.Revision)
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
	logger.Info(kernel.ConfirmationBootLine())
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
	logger.Infof("🧭 %s", trader.PlanLivenessBootLine(st))
	// INVALIDATION-WIRED (2026-09-03) — the arm gate's new leg and the
	// armed-under surfaces, both READ from the code that implements them.
	logger.Infof("🛡 %s", trader.ArmGateBootLine())
	// DATA-INTEGRITY (2026-09-03) — D7. Both lines READ.
	logger.Infof("🧮 %s", store.DataIntegrityBootLine(kernel.TouchOrdinalSeed != nil))
	logger.Infof("🧮 %s", st.AbConfirm().E8BootLine())
	{
		// D6 — flag-guarded recompute of the short counterfactuals. Default OFF
		// and the line above reports the state either way. Backup first: no
		// backup, no write.
		if store.E8BackfillEnabled() {
			if b, bErr := store.BackupBeforeE8Backfill(cfg.DBPath, time.Now().Format("20060102-150405")); bErr != nil {
				logger.Errorf("🧮 e8 backfill ABORTED — backup failed: %v", bErr)
			} else {
				res, rErr := st.AbConfirm().BackfillShortRows(func(planID string, version int, scenario string) (string, bool) {
					row, e := st.Plan().GetPlan(planID, version)
					if e != nil || row == nil {
						return "", false
					}
					var doc kernel.PlanDoc
					if json.Unmarshal([]byte(row.Doc), &doc) != nil {
						return "", false
					}
					for _, sc := range doc.Scenarios {
						if sc.ID == scenario {
							return sc.Direction, sc.Direction != ""
						}
					}
					return "", false
				})
				if rErr != nil {
					logger.Errorf("🧮 e8 backfill failed: %v", rErr)
				} else {
					logger.Infof("🧮 e8 short rows recomputed=%d · unrecomputable fill-bar=%d no-inputs=%d no-direction=%d · longs untouched=%d (backup %s)",
						res.Recomputed, res.BadFillBar, res.NoInputs, res.NoDirection, res.LongsUntouched, b)
				}
			}
		}
	}
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
		logger.Infof("🧪 %s", st.SeamExclusionBootLine())
		// WAVE A / D1e + D2c — THE RECORD. Flag-guarded, backup first, counted.
		// The line reports what is PENDING when the flag is off, so the counts
		// are visible without arming anything (the adherence-regrade idiom).
		{
			armed := store.WaveARecordMigrateEnabled()
			ran, backup := store.WaveACounts{}, ""
			if armed {
				pending := st.PendingWaveAWork()
				if pending.TouchRows == 0 && pending.MAEZeroed == 0 && pending.MFEZeroed == 0 {
					logger.Infof("📐 wave-A record migration armed but there is nothing to do — every row is already classified")
				} else if b, bErr := store.BackupBeforeWaveA(cfg.DBPath, time.Now().Format("20060102-150405")); bErr != nil {
					logger.Errorf("📐 wave-A record migration ABORTED — backup failed, no backup no write: %v", bErr)
				} else {
					backup = b
					if c, mErr := st.RunWaveARecordMigration(); mErr != nil {
						logger.Errorf("📐 wave-A record migration failed: %v", mErr)
					} else {
						ran = c
						logger.Warnf("📐 wave-A record migration: %d duplicate + %d legacy touch rows marked (NEVER deleted, never blessed) · mae 0→NULL on %d row(s) %v · mfe 0→NULL on %d row(s) · backup %s",
							c.TouchDuplicate, c.TouchLegacy, c.MAEZeroed, c.MAEZeroIDs, c.MFEZeroed, b)
					}
					// D2d — THE BACKFILL, three-state. It was built in wave 1A
					// and never run, which is the whole reason trade_excursions
					// reads 0: not a broken writer, an unpopulated corpus. From
					// epoch so the WHOLE history is in scope — the CLI's
					// documented `-backfill 2026-08-15` covers only rows entered
					// on or after that date and silently leaves the rest out.
					// Empty symbol and trader mean ALL — no literal to drift.
					if res, bfErr := trader.BackfillExcursions(st, "", "", time.Unix(0, 0), time.Now()); bfErr != nil {
						logger.Errorf("📐 excursion backfill failed: %v", bfErr)
					} else {
						logger.Warnf("📐 excursion backfill: scanned=%d computed=%d unrecomputable=%d (no 1m coverage — those rows keep NULLs, never zeros) levels_resolved=%d",
							res.Scanned, res.Computed, res.NoCoverage, res.LevelsFound)
					}
				}
			}
			logger.Infof("📐 %s", st.WaveARecordBootLine(armed, ran, backup))
		}
		// 1B D7 — the calibrated detector and the two tables that record it.
		logger.Infof("🔬 %s", kernel.DetectorBootLine(st.TouchOutcomes().CountOutcomes(), st.CandidatePool().CountPool()))

		// W1 EPISODE CONTRACT (2026-09-10) — the unit of opportunity, RECORDED.
		//
		// The 95f387ae boot shipped this wave with ONE of four items wired;
		// these are the other three call sites, and the A29 gate now holds all
		// of them (trader/wiring_gate_test.go). A29 in one line: built is not
		// wired, and a passing unit test cannot tell the difference.
		//
		// Empty trader means ALL, the same convention BackfillExcursions uses
		// above — a boot-time caller has no single trader in hand and must not
		// invent one.
		if to := st.TouchOutcomes(); to != nil {
			// D4 — the three-state backfill, ONE SHOT at boot. It marks what it
			// cannot recompute rather than reconstructing it; on the live
			// archive it recomputes zero, which is the research's claim
			// MEASURED and is the finding, not a failure.
			bf, bfErr := to.BackfillOpportunities("")
			if bfErr != nil {
				logger.Errorf("🎫 episode backfill failed: %v", bfErr)
			}
			// The counts the boot line prints are READ from the table just
			// touched, so the line cannot claim a number the process did not
			// compute (A: boot lines are READ, never literal).
			// The counts the boot line prints are READ from the table just
			// touched, so the line cannot claim a number the process did not
			// compute (boot lines are READ, never literal). The session-day
			// boundary is kernel.CMESessionDayStart — the SAME resolver the
			// rest of the system uses for "today", not a midnight of my own.
			sinceMs := kernel.CMESessionDayStart(time.Now()).UnixMilli()
			open, oErr := to.CountOpenOpportunities("")
			byOutcome, cErr := to.CountClosedByOutcome("", sinceMs)
			if oErr != nil || cErr != nil {
				logger.Warnf("🎫 episodes: counts unavailable at boot (open=%v closed=%v) — the line prints what it could read", oErr, cErr)
			}
			var closed int64
			for _, n := range byOutcome {
				closed += n
			}
			logger.Infof("%s", store.EpisodeBootLine(store.EpisodeBootCounts{
				Open:              open,
				ClosedToday:       closed,
				NeverReached:      byOutcome[store.OpportunityNeverReached],
				ReachedDeclined:   byOutcome[store.OpportunityReachedDeclined],
				ConfirmedNotArmed: byOutcome[store.OpportunityConfirmedNotArmed],
				ArmedNotFilled:    byOutcome[store.OpportunityArmedNotFilled],
				Filled:            byOutcome[store.OpportunityFilled],

				// BackfillRan distinguishes "ran and found nothing" from "has
				// not run" — the zero this wave expects is a MEASUREMENT, and
				// it must not render the same as an absence.
				BackfillRan:            bfErr == nil,
				BackfillRecomputed:     int64(bf.Recomputed),
				BackfillUnrecomputable: int64(totalUnrecomputable(bf)),
			}))

			// W2 FADE PERMISSION (2026-09-10) — a LABEL and a COUNTER, never a
			// gate. The backfill runs ONE SHOT here, per trader, with the clock
			// set to each episode's OPEN (never the completed session — round 11).
			// It is three-state (A30) and its counts are READ onto the boot line.
			// A fault in it must never stop the boot (A10): every branch WARNs.
			var fadeBF store.FadeBackfillResult
			for _, at := range traderManager.GetAllTraders() {
				if at == nil {
					continue
				}
				r := at.BackfillFadePermission(store.DayPlanEraStart.UnixMilli())
				fadeBF.Ran = fadeBF.Ran || r.Ran
				fadeBF.Recomputed += r.Recomputed
				fadeBF.Unrecomputable += r.Unrecomputable
				fadeBF.Untouched += r.Untouched
			}
			logger.Infof("%s", trader.FadePermissionBootLine(st, time.Now(), fadeBF))
			// 101 D2 (2026-09-16) — the chart across the roll. The flag is READ
			// (A11); the readers it gates live in api/ only and the decision
			// readers are contract-scoped and untouched (E4).
			logger.Infof("📈 chart: across-roll=%s · prior contracts fill strictly before the current contract's first live row · step never adjusted · limit max=%d · decision readers=current-contract-only",
				api.ChartAcrossRollResolved(), 20000)

			// ONE SETUP (dispatch 102, 2026-09-10) — D9's two backfills, three-state,
			// ONE SHOT per trader: verdicts at each episode's OPEN since W2's boot,
			// follow-plans since W1's boot through the roll wave's contract filter.
			// Then D8's boot line, every field READ. A fault never stops the boot.
			var osBF store.OneSetupBackfillResult
			var fpBF store.FollowBackfillResult
			var osIDs []string
			for _, at := range traderManager.GetAllTraders() {
				if at == nil {
					continue
				}
				osIDs = append(osIDs, at.GetID())
				v := at.BackfillOneSetupVerdicts(store.OneSetupVerdictEraStart.UnixMilli(), time.Now())
				osBF.Ran = osBF.Ran || v.Ran
				osBF.Recomputed += v.Recomputed
				osBF.Unrecomputable += v.Unrecomputable
				osBF.Untouched += v.Untouched
				f := at.BackfillFollowPlans(store.FollowPlanEraStart.UnixMilli(), time.Now())
				fpBF.Ran = fpBF.Ran || f.Ran
				fpBF.Recomputed += f.Recomputed
				fpBF.Unrecomputable += f.Unrecomputable
				fpBF.Untouched += f.Untouched
			}
			geometryBootNow := time.Now()
			logger.Infof("%s", trader.OneSetupBootLine(st, geometryBootNow, osIDs, osBF, fpBF))
			logger.Infof("%s", trader.StructuralGeometryBootLine(st, geometryBootNow, osIDs...))
		}
	}
	// W3 D7 (2026-09-09) — the map posture. Per-READ counts are n/a at boot (no
	// planner read has happened); `cap` is LABELLED per-trader because this
	// process serves several traders and none of their values is global.
	// pwh/pwl seatable reflects whether a DAILY bar source is actually installed
	// — the prior-week anchors come from daily bars, never from the 1m ring.
	logger.Infof("%s", kernel.MapBootLine(kernel.DefaultMaxLevels, kernel.PlanHardMaxLevels, kernel.DailySourceInstalled()))
	// W-TF D6 (2026-09-10) — which timeframes every detector runs on. The
	// detection set and the detector names are READ from their tables, so the
	// line cannot claim a pass the binary does not perform; per-tf counts are
	// n/a until a read happens and ride TFReadLine. The daily/weekly tier and
	// the htf weight are both marked [I]: round 12 establishes no timeframe
	// hierarchy and E4 measures both.
	logger.Infof("%s", kernel.TFBootLine(kernel.DefaultHTFDetectionTFs, kernel.HTFDetectorCount()))
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
	// CANCEL-CONFIRMATION (2026-09-06) — a send is not a settlement. Every
	// field READ; the reconciliation half prints n/a until a broker book exists,
	// because at process start there is none and a number here would be invented.
	logger.Infof("🧾 %s", trader.CancelBootLine(st, trader.ReconcileCounts{}, time.Now().UnixMilli()))
	// THE DESK STRIP (2026-09-06) — one read, one row per fact the owner needs.
	// The UNKNOWN count is per-request, so at boot the line says n/a instead of
	// printing a zero it has not measured.
	// 🔭 and not 🖥: the screen glyph already belongs to the UI-serving line at
	// :305 (dist staleness), and two lines under one glyph make any watcher
	// keyed on it ambiguous (A24).
	logger.Infof("🔭 %s", trader.DeskBootLine(nil))
	// ATTRIBUTION — counts READ from the table, never a literal.
	logger.Infof("🔗 %s", st.AttributionBootLine())
	logger.Infof("⚙ %s", store.KnobRegistryBootLine())
	logger.Infof("%s", trader.ArmsBootLine())
	// SESSION RISK (2026-09-09, dispatch 104 D5) — every field READ from the
	// code that enforces it, and every invented threshold carries its evidence
	// tier. The daily limit prints DECORATIVE when either toggle is off, in the
	// owner's own words, because a limit that is displayed but not enforced is
	// worse than no limit: it is a limit someone is relying on.
	logger.Infof("🛑 %s", trader.SessionRiskBootLineForBoot(st))
	// BRACKET-OCO SEPARATION (2026-09-07) — D7. Three of these fields describe
	// the C# AddOn, which this process cannot read from its own source, so they
	// are read from the broker's book and print n/a until one arrives. At boot
	// there is no book and no far-side build id, so this line is mostly n/a BY
	// DESIGN: a resolved "entry-oco=own" here would be a Go constant claiming
	// the AddOn's behaviour (A11/A24).
	logger.Infof("🧷 %s", trader.BracketsBootLine(nil, false, "", 0))
	logger.Infof("🎛 %s", kernel.EntryLawBootLedger(nil)) // P1.4 (ledger-close 2026-08-19) — clock-guard block: live host-RTC drift,
	// guard-timer freshness, last resync/check state. Log-only, best-effort.
	kernel.LogClockGuardBoot()
	// P4 (ledger-close 2026-08-19) — half-days boot line: loaded count + the
	// next upcoming early close. Fail-open on a bad file.
	trader.LogHalfDaysBoot(time.Now())

	// D6 (owner ruling 2026-09-07) — THE SESSION CALENDAR SAYS WHICH DAY IT IS,
	// once at boot. Every field is READ from the calendar the gate consults, and
	// an unsourced date is counted rather than hidden: a calendar nobody has
	// checked must not read like a checked one.
	logger.Infof("🗓 %s", kernel.SessionCalendarBootLine(time.Now()))

	// PLACE CONFIRMATION (2026-09-07) — the placement side of class 81. The
	// The reason producer is deferred to the next owner-run AddOn wave.
	// h1 omits the field; the line states that missing evidence explicitly.
	logger.Infof("📤 %s", trader.PlaceConfirmBootLine(false))

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

// totalUnrecomputable sums the three-state backfill's reason buckets. It is a
// SUM OF WHAT WAS COUNTED, never a separate tally that could disagree with the
// map it summarises (class 97: one source, both readers).
func totalUnrecomputable(r store.BackfillResult) int {
	n := 0
	for _, v := range r.Unrecomputable {
		n += v
	}
	return n
}
