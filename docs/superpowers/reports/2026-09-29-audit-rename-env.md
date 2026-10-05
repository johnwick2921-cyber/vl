# AUDIT-RENAME-ENV — DS-105 (2026-09-29) — DRAFT, NOT FOR MERGE

Plan: /home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md
md5: b16759336fe0e264d11c1bc885ce5192 (verified — matches the dispatch)
Base: origin/dev 9d52f5dc6 "RELEASE 4d5382069… boot 7" (`git log -1` quoted at 20:14 CT)
Branch: audit/rename-env @77c3a6332 (push-empty claim) + this report
Slice: ENV + CONFIG (R1a item 2, R1b items 2/4). L-AUDIT honored: read-only, no code change.

Evidence tiers: [A] = read/ran the exact line at this base · [B] = strong inference · [C] = speculation.

## 1. Complete NOFX_ env census vs the plan's list

The plan (R1a) names 11 knobs ("at minimum", "full list in the identifier map"):
NOFX_UPDATER, NOFX_RELEASE_DIR, NOFX_CUTOVER_TOKEN, NOFX_EXPECTED_REVISION,
NOFX_RELEASE_INBOX, NOFX_CLOCK_STATE, NOFX_CALENDAR_STATIC, NOFX_CHART_ACROSS_ROLL,
NOFX_BAR_SCALE_MISMATCH_PCT/_MULT, NOFX_DB_PATH, NOFX_TRADER_ID.

The tree at 9d52f5dc6 reads these NOFX_ names (unique tokens, all [A] file:line):

Production Go readers (literal os.Getenv):
- api/handler_klines.go:572 NOFX_CHART_ACROSS_ROLL — PACKAGE-VAR INIT (see P0-1)
- provider/ninjatrader/bar_source.go:48,51 NOFX_BAR_SCALE_MISMATCH_PCT/_MULT — init() (see P0-1)
- kernel/boot_integrity.go:88 NOFX_EXPECTED_REVISION
- kernel/clock_health.go:146 NOFX_CLOCK_STATE (boot + session-roll calls only — not hot)
- internal/installpath/releasedir.go:38 NOFX_RELEASE_DIR (the one resolver)
- trader/auto_trader_calendar.go:117 NOFX_CALENDAR_STATIC
- cmd/levelstats-backfill/main.go:35,41 NOFX_DB_PATH, NOFX_TRADER_ID

Go readers via a named const then os.Getenv:
- internal/updaterworker/app_http.go:22 CutoverTokenEnv = "NOFX_CUTOVER_TOKEN"
- api/handler_updates.go:102 updaterKnobEnv = "NOFX_UPDATER"
- cmd/nofx-updater/main.go:77 releaseInboxEnv = "NOFX_RELEASE_INBOX"

Shell/script readers (NOFX_${…} or env names) — the plan's "shell twin" must cover EVERY one:
- deploy/cutover.sh:16,127,134 NOFX_CUTOVER_TOKEN; 102 NOFX_HEALTH_URL; 128 NOFX_GATE_URL; 36 NOFX_UNIT; 37 NOFX_INSTALL; 38,58,67 NOFX_ACTIVATE_BIN; 94 NOFX_RELEASE_DIR
- deploy/nofx-lock.sh:62 NOFX_LOCK_DIR; 63 NOFX_LEGACY_LOCK; 64 NOFX_LOCK_STALE_SECONDS; 65 NOFX_LOCK_BEAT_SECONDS; 71 NOFX_LOCK_INCOMPLETE_SECONDS
- deploy/nofx-claim.sh:24,53-55 NOFX_SESSION
- deploy/nofx-db-backup.sh:29 NOFX_DB; 30 NOFX_DB_RESEARCH; 31 NOFX_BACKUP_DIR; 32-33 NOFX_KEEP_DAILY/WEEKLY; 36-38 NOFX_BACKUP_RESEARCH, NOFX_KEEP_RESEARCH_DAILY/WEEKLY; 39 NOFX_BACKUP_MIN_FREE_GB; 14,150 NOFX_BACKUP_RESEARCH
- deploy/nofx-clock-guard.sh:25 NOFX_CLOCK_STATE
- deploy/planner-ab-report.sh:33 NOFX_REPO; 34 NOFX_DATA
- deploy/postboot-check.sh:15,16 NOFX_DATA, NOFX_REPO; 109,172,188 NOFX_ENV; 156,16 NOFX_BIN
- deploy/bars-key-rollback.sh:21 NOFX_DB
- deploy/install-updater-worker.sh:33,39 NOFX_UPDATER_BUILD_REPO; 34,39 NOFX_UPDATER_INSTALL_DIR; 52,62 NOFX_RELEASE_DIR; 53,64,66 NOFX_CUTOVER_TOKEN
- start.sh:178-284 NOFX_BACKEND_PORT/NOFX_FRONTEND_PORT; install.sh:105-106, install-stable.sh:67-68, docker-compose*.yml:11,47 (docker-only)
- .github/workflows/pr-docker-compose-healthcheck.yml:38-40 NOFX_BACKEND_PORT/FRONTEND_PORT/TIMEZONE (docker-only)
- .env.example NOFX keys: NOFX_BACKEND_PORT, NOFX_FRONTEND_PORT (marked docker-only 2026-09-26), NOFX_UPDATER (guide-documented), STOP_ENTRY_SEAM etc. are NOT NOFX-named.

**MISSED by the plan's named list** (each one silently falls back to its default if the helper/twin forgets it):
- P0/P1: NOFX_HEALTH_URL, NOFX_GATE_URL, NOFX_UNIT, NOFX_INSTALL, NOFX_ACTIVATE_BIN (cutover.sh) — a missed twin changes what cutover probes and kills. The plan's R1b item 7 changes UNIT/INSTALL DEFAULTS but never mentions these env overrides exist.
- P1: NOFX_LOCK_DIR, NOFX_LOCK_BEAT_SECONDS, NOFX_LOCK_STALE_SECONDS, NOFX_LOCK_INCOMPLETE_SECONDS, NOFX_LEGACY_LOCK (nofx-lock.sh) — the MAIN-TREE LOCK tool. If R1a skips them, the new vl-lock.sh either misses the owner's tuned values or breaks the legacy-lock hand-over; both are safety-relevant.
- P1: NOFX_DB, NOFX_DB_RESEARCH, NOFX_BACKUP_DIR, NOFX_KEEP_*, NOFX_KEEP_RESEARCH_*, NOFX_BACKUP_MIN_FREE_GB, NOFX_BACKUP_RESEARCH (backup/rollback scripts) — a missed twin silently moves or shrinks backups.
- P2: NOFX_SESSION (nofx-claim.sh), NOFX_REPO/NOFX_DATA/NOFX_ENV/NOFX_BIN (postboot-check.sh, planner-ab-report.sh), NOFX_UPDATER_BUILD_REPO/INSTALL_DIR (install-updater-worker.sh).
- P3: NOFX_HALF_DAYS is COMMENT-ONLY at trader/auto_trader_halfdays.go:27 (no Getenv in the file) — dead mention; the R1b census guard (item 10) will flag it; add to the allow-list or fix the comment.
- Test-only envs (NOFX_TEST_*, NOFX_DEMO_*, NOFX_REHEARSAL* in trader/*_test.go, main_dotenv_test.go, api/handler_updates_refusal_log_test.go) — not production; the R1b census guard must allow-list them or rename the harness.
- config/config.go reads ZERO NOFX_ names (grep [A]) — the plan's "Config structs (config/config.go)" slice expectation is empty; the env reads live in kernel/trader/api/internal, not config.

## 2. The owner's real env files — key NAMES only (never values)
/home/hoang/nofx/.env [A]: AI_HTTP_TIMEOUT_SECONDS, AI_MAX_RETRIES, AI_MAX_TOKENS, AI_PLAN_REASONING, ARMED_TEST_SEAM, CLAW402_DEFAULT_MODEL, CLAW402_WALLET_ADDRESS, CLAW402_WALLET_KEY, DATABENTO_API_KEY, DATABENTO_DATASET, DATA_ENCRYPTION_KEY, DB_PATH, DB_TYPE, EOD_FLAT_LIMIT_TICKS, EOD_FLAT_MARKET_AFTER_SEC, EXIT_MECHS_SUSPENDED, FAST_MARKET_REASONING, HISTORICAL_IMPORT_SEAM, HTF_VETO_MODE, JWT_SECRET, NINJATRADER_DATA_DIR, NOFX_BACKEND_PORT, NOFX_FRONTEND_PORT, NOFX_TIMEZONE, NOFX_UPDATER, NT_EXTRA_SYMBOLS, NT_RUNTIME_SYMBOLS, NT_TRANSPORT, RSA_PRIVATE_KEY, STOP_ENTRY_SEAM, TRADING_MODE, TRANSPORT_ENCRYPTION.
~/.config/nofx-updater/env [A]: NOFX_CUTOVER_TOKEN, NOFX_RELEASE_DIR.
So the live NOFX_ keys are: NOFX_UPDATER (covered by the plan ✓), NOFX_CUTOVER_TOKEN (✓), NOFX_RELEASE_DIR (✓), and THREE DEAD ones — NOFX_BACKEND_PORT, NOFX_FRONTEND_PORT (docker-only; the host binary ignores them), NOFX_TIMEZONE (zero readers since 2026-09-26). The optional one-line .env sed in R2 should target exactly these five names, and the three dead ones can simply be dropped.

## 3. Helper design: places the Go helper does not apply
- P0 — BEFORE LOGGING IS UP (the plan says "WARN once", but these run before any logger exists):
  - api/handler_klines.go:572: package-level `var chartAcrossRoll = …os.Getenv("NOFX_CHART_ACROSS_ROLL")…` — evaluated at package init. [A]
  - provider/ninjatrader/bar_source.go:44-53: package `init()` reads both NOFX_BAR_SCALE_* vars. [A]
  Fix to the plan text: the helper's WARN must be LAZY (deferred to the first log emit, printed once then), or these two sites must read WITHOUT the WARN and the boot line must print the SOURCE (VL_ or NOFX_) for each — the bar-source boot line already prints the values; add the source word.
- P1 — CHILD-PROCESS ENV, where the Go helper never runs:
  - cutover.sh → nofx-activate child inherits the SHELL's env: the shell twin must be in cutover.sh, and the activate CLI reads through installpath (Go, covered) [A].
  - systemd unit deploy/systemd-user/nofx-updater.service:21 `EnvironmentFile=%h/.config/nofx-updater/env` — the FILE PATH moves in R2 via the symlink (plan ✓); the keys inside are read by the worker binary through the helper [A].
  - install-updater-worker.sh WRITES the env file with NOFX_CUTOVER_TOKEN/NOFX_RELEASE_DIR today; R1b flips the writer — ordering R1a→R1b is correct, but D1 must say the script writes the NEW names and the worker's helper accepts BOTH (the plan's "Covers NOFX_UPDATER…" sentence does not name the installer script as a writer).
- NO hot-loop readers found: clock_health reads at boot + session-roll only [A]; chart/bar-scale are init-only [A]; calendar-static is per-session load [B]. The WARN-once cost is therefore negligible.

## 4. Config structs / knob registry / Guide
- config/config.go: zero NOFX_ reads [A]. The R1a helper lives in config — fine, but the dispatch's "Config structs (config/config.go)" check item overstates the surface; the reads are in kernel/trader/api/internal.
- store/knob_registry.go: zero NOFX mentions [A].
- Guide knob list: settings.ts has no NOFX_ names [A], but web/src/guide/content/updates.ts:39-48 documents NOFX_UPDATER, NOFX_RELEASE_DIR, NOFX_CUTOVER_TOKEN and `nofx-updater serve` (R1b item 9 covers updates.ts ✓), and web/src/i18n/translations.ts:827,2223,3587 tell users to edit NOFX_BACKEND_PORT/NOFX_FRONTEND_PORT in three locales — R1b item 9 says "FAQ/i18n install commands"; make these three lines explicit so a lane does not ship a guide telling users to edit dead names.

## 5. D1 item 2 wording — ambiguities and fixes
- "Route every production `os.Getenv("NOFX_…")` through it": the two init-time readers are os.Getenv but MUST NOT go through the warning form (P0 above). Fix: "…through it, EXCEPT the two package-init readers (api/handler_klines.go:572, provider/ninjatrader/bar_source.go init()) whose values the boot lines state WITH their source (VL_/NOFX_); the helper's WARN is lazy and prints once on the first log emit."
- "Shell twin in every deploy script that reads NOFX_*": the twin must also cover the env-file WRITER (install-updater-worker.sh) and the claim script (nofx-claim.sh), which D1 item 3 does not list. Fix: name the complete script list from §1.
- "list them all in the PR; at minimum …": replace the at-minimum list with the identifier map as the ONLY source, and state that the PR must quote a census diff (`grep -rhoE 'NOFX_[A-Z0-9_]+'` before/after) so a missed reader is a red diff, not a judgment call.
- NOFX_DB_PATH is read ONLY by cmd/levelstats-backfill (a tool, not the bot); if the plan wants it covered it must say "including tools", otherwise exclude it explicitly.

## Ranked findings
P0-1 init-time env readers vs the WARN-once helper (file:line + fix above).
P0-2 cutover.sh env overrides (NOFX_HEALTH_URL/GATE_URL/UNIT/INSTALL/ACTIVATE_BIN) missing from the plan — wrong health/gate targets or wrong unit after R2.
P1-1 lock-script envs (NOFX_LOCK_*) missing — tuned lock behavior can silently revert to defaults.
P1-2 backup-script envs (NOFX_DB/KEEP_*/BACKUP_*) missing — backup location/retention can silently change.
P1-3 env-file writer (install-updater-worker.sh) not named as a writer.
P2-1 claim script NOFX_SESSION missing; postboot/ab-report NOFX_REPO/DATA/ENV/BIN missing.
P2-2 i18n translations.ts port instructions (3 locales) name dead NOFX_ keys.
P3-1 NOFX_HALF_DAYS comment-only mention (census-guard trap).
P3-2 Test-only NOFX_TEST_/DEMO_/REHEARSAL_ envs need allow-listing or renaming.
P3-3 Dispatch wording fixes (§5).

What I did NOT do: no code change, no .env content read (key names only), no unit/DB/deploy action, no PR beyond this report branch.
