package branding

// Census guard (Z17, plan v7 FINAL item 10): every tracked file's count
// of the old brand token (assembled at runtime below) must EQUAL its
// allowed count in the transition table. This file and the table never
// contain the old name in any casing; wrapper paths use the {OLD}
// placeholder, expanded at runtime. Entries and the Ceiling may only
// go DOWN; the CTO rejects any PR that raises either. This table is
// PROVISIONAL (generated at the D2-DEAD+WEB merge head); the final
// re-pin lands after the ops + agent/addon parts merge.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type censusEntry struct {
	count  int
	phase  string
	reason string
}

var censusTable = map[string][]censusEntry{
	"deploy/postboot-check.sh": {
		{count: 41, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/VL-TRADING-RULEBOOK-v1.md": {
		{count: 40, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-repo-understanding/CORE-TRACE.md": {
		{count: 35, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-repo-understanding/CTO-REPORT.md": {
		{count: 35, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-repo-understanding/FULL-AUDIT.md": {
		{count: 35, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-structural-stop-daily-loss-evidence/github-checks.json": {
		{count: 33, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"web/src/lib/storageMigration.test.ts": {
		{count: 32, phase: "final", reason: "wallet-key migration row removed with the wallet family (integration)"},
	},
	"docs/superpowers/reports/2026-09-12-structural-stop/evidence/github-checks.json": {
		{count: 26, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"internal/envcompat/envcompat_test.go": {
		{count: 26, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/rename_r1a_test.go": {
		{count: 22, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/golden_test.go": {
		{count: 22, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/testdata/job_full.golden.json": {
		{count: 22, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/testdata/legacy-jobs/e1dcc173751ccbfe0b2a7268fd40451a.json": {
		{count: 11, phase: "final", reason: "2026-10-02 #307: real pre-rename job file kept verbatim as a canon-53 fixture (paths are historical data, not code)"},
	},
	"internal/updaterworker/rename_r1a_test.go": {
		{count: 22, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/cutover.sh": {
		{count: 21, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"web/src/brand-scope.test.ts": {
		{count: 21, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/rename_r1a_contract_test.go": {
		{count: 18, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl-db-backup.sh": {
		{count: 18, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-10-scenario-level-identity-data/remote-ci-setup-failures.txt": {
		{count: 16, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/research/2026-09-08-trading-policy/README.md": {
		{count: 16, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"deploy/updater_worker_install_test.go": {
		{count: 15, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl-lock-test.sh": {
		{count: 15, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release_contract_test.go": {
		{count: 14, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/install-updater-worker.sh": {
		{count: 13, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/planner-ab-report.sh": {
		{count: 13, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/steps_test.go": {
		{count: 13, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"logger/log_prune_test.go": {
		{count: 13, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/research/2026-09-12-stop-target-geometry/README.md": {
		{count: 12, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"internal/envcompat/envcompat.go": {
		{count: 12, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/harness_test.go": {
		{count: 12, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	// ONE-BUTTON P-A/P-E additions (integration, 2026-10-02): the VL_ env
	// prefix and its legacy fallback, and the ReleaseRepo constant — the
	// census counts the token in every tracked file. (The reasons below
	// spell nothing out so this file itself stays at zero.)
	"internal/updaterbootstrap/workertoken.go": {
		{count: 2, phase: "final", reason: "P-E worker-token env reads (VL_ prefix + legacy fallback)"},
	},
	"internal/updatersource/source.go": {
		{count: 1, phase: "final", reason: "P-A ReleaseRepo constant value"},
	},
	"api/release_dir_test.go": {
		{count: 11, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/verdict_read_test.go": {
		{count: 11, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/vl-updater/rename_r1a_test.go": {
		{count: 10, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/cutover_test.sh": {
		{count: 10, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/release_test.go": {
		{count: 10, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"main.go": {
		{count: 10, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"ninjascript/VLTraderTCPClient.cs": {
		{count: 10, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/bars-key-rollback.sh": {
		{count: 9, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-20-brand-census-docs-brand-census.md": {
		{count: 9, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-20-brand-census.md": {
		{count: 9, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"store/strategy_schema_test.go": {
		{count: 9, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/vl-updater/main_test.go": {
		{count: 8, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release/package.sh": {
		{count: 8, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl_twin_contract_test.go": {
		{count: 8, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/library_activation_test.go": {
		{count: 8, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/releaseroot.go": {
		{count: 8, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"web/src/components/faq/FAQContent.tsx": {
		{count: 8, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"web/src/i18n/translations.ts": {
		{count: 8, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"cmd/vl-updater/main.go": {
		{count: 7, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/unit_templates_test.go": {
		{count: 7, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/research/2026-09-09-structure-candle-targets-staleness/README.md": {
		{count: 7, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"branding/scope_test.go": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/vl-activate/main.go": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl-lock.sh": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/system.go": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/view_test.go": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/reverifier_test.go": {
		{count: 6, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"web/src/lib/storageMigration.ts": {
		{count: 5, phase: "final", reason: "wallet-key migration row removed with the wallet family (integration)"},
	},
	"deploy/vl-claim.sh": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/internal/external-reviews/2026-05-28-end-to-end-architecture.md": {
		{count: 5, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-03-rebrand-census.md": {
		{count: 5, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-10-scenario-level-identity-data/basis-receipts.json": {
		{count: 5, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-repository-repairs/dependency-alerts-at-review.jsonl": {
		{count: 5, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/consistency_test.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/job_test.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/release.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"kernel/boot_integrity_test.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"logger/log_prune_r1a_test.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"provider/ninjatrader/bar_source.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"provider/ninjatrader/bar_source_r1a_test.go": {
		{count: 5, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	".github/workflows/docker-build.yml": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/activation.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/steps.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/installpath/releasedir.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/installpath/releasedir_test.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/host_os.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/host_os_test.go": {
		{count: 3, phase: "final", reason: "2026-10-02: MainTreeLockHeld pinning test removed (P3c, worker-lock-cancel) — the token lived only in that deleted test's fake lock script name"},
	},
	"internal/updaterworker/preflight_test.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/releasefixture/releasefixture.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/releaseroot_unit_test.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/runner.go": {
		{count: 4, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	".env.example": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/backend_logs_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/prompt_persona.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/tools.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_klines_r1a_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates_verifier_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl-clock-guard.sh": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/hold_census_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/orphans_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/recovery.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/rollback_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/sshsig_test.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"kernel/boot_integrity.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"logger/log_prune.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"start.sh": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/lighter/trading.go": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"web/src/test/brand-scope-baseline.json": {
		{count: 3, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	".gitignore": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"Makefile": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/memory_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_exchange_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_klines.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates_knob_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/release_dir.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/levelstats-backfill/main.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/vl-activate/main_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/{OLD}-lock.sh": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/planner_ab_report_fixture_test.sh": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/AUDIT-CHECKLIST.md": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-01-planner-api-failure.md": {
		{count: 2, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-08-scenario-economics.md": {
		{count: 2, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-12-structural-stop.md": {
		{count: 2, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterwire/paths_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/app_http.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/deps.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/target.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"kernel/funding_suppress_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/ab_confirm.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/adherence_regrade.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/bar_contract_key.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/sqlitedriver/one_registration_census_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/wave_a_migration.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"telegram/bot_token_same_second_test.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/lighter/trader.go": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"web/src/guide/content/status.ts": {
		{count: 2, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	".github/workflows/release.yml": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/brand_visible_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"agent/config_visibility_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/agent_routes.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_klines_depth_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates_mac_purpose_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_updates_starter_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/handler_user.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/machine_scope_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/server.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/strategy_effective.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"api/token_clock_window_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"cmd/nq_smoke/smoke_roundtrip.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/canon_contract_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/install-autostart.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/{OLD}-claim.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/{OLD}-clock-guard.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/{OLD}-db-backup.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release/README.md": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release_manifest_addon_pin_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release_manifest_caller_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/release_manifest_versions_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"deploy/vl_db_backup_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docker/Dockerfile.backend": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/CLAUDE-canon.md": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/plans/2026-08-29-news-hygiene-micro-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/plans/2026-08-29-sunday-shield-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-18-timegate-audit-ai-timeout.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-19-final-bundle.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-19-ledger-close-FINAL.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-19-zerotrade-forensics.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-25-1h-wave-r2-r4-implementation.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-26-bar-persistence.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-26-fvg-entry-model.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-26-packb-volume-levels.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-26-s-fix-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-27-guide-page.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-27-plan-mode-layering-ux.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-28-bar-truth-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-28-forensics-hygiene-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-28-missed-200pt.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-28-waterfall-class-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-29-f1-dependency-vuln-scan.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-29-total-audit-15.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-08-30-weekly-bias-wave.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-01-class35-replan-budget.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-01-full-system-audit.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-02-bar-source-audit.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-02-deepseek-e2e-audit.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-02-detector-redesign.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-06-v5-precheck.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-08-brand-visible.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-08-plan-liveness.md": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-10-scenario-level-identity-data/basis_receipts.py": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/reports/2026-09-13-repo-understanding/tools/build-core-trace.py": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/research/2026-09-16-s2-replay/harness/main.go": {
		{count: 1, phase: "R4", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/runbooks/2026-09-04-partner-update.md": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"docs/superpowers/runbooks/2026-09-08-clean-machine.md": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/activation/stage_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updateauth/census_directives_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterjob/verdict.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/activate_reprove_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/containment_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/nt8_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/snapshot.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/sqldriverpin/sqldriverpin_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"internal/updaterworker/steps.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"kernel/clock_health_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"manager/trader_manager.go": {
		{count: 0, phase: "final", reason: "DS-103 cut landed — zero residual hits"},
	},
	"scripts/sandbox-down.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"scripts/sandbox-up.sh": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/maintenance_census_mint_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"store/strategy_futures_indicators_test.go": {
		{count: 0, phase: "final", reason: "fixture cut landed — zero residual hits"},
	},
	"store/testdata/settings_truth_rows.json": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"telegram/agent/apicall.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/auto_trader_calendar.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/f0_calendar_ignition_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/ninjatrader/trader.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/t1_news_hygiene_test.go": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/testdata/w2-write-truth/plan_row_452_subset.json": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
	"trader/testdata/w2-write-truth/plan_row_455_subset.json": {
		{count: 1, phase: "R5", reason: "transitional — re-pinned at final"},
	},
}

// Ceiling = sum of allowed counts at the R1b merge (1160) + 11 for the
// e1dcc173 legacy job fixture (2026-10-02 #307).
const censusCeiling = 1171

func TestCensusGuard(t *testing.T) {
	tok := "no" + "fx" // runtime assembly — never the literal
	// expand() the table keys once: table keys hold the {OLD}
	// placeholder, real paths hold the literal.
	expandedTable := make(map[string][]censusEntry, len(censusTable))
	for k, es := range censusTable {
		expandedTable[expand(k)] = es
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files failed (never skip): %v", err)
	}
	files := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	scanned := 0
	var mismatches []string
	for _, f := range files {
		if f == "" {
			continue
		}
		scanned++
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		actual := strings.Count(strings.ToLower(string(b)), tok) +
			strings.Count(strings.ToLower(f), tok)
		allowed := 0
		if es, ok := expandedTable[f]; ok {
			for _, e := range es {
				allowed += e.count
			}
		}
		if actual != allowed {
			mismatches = append(mismatches, fmt.Sprintf("%s: actual %d, allowed %d — context: %s", f, actual, allowed, context(b, tok)))
		}
	}
	if scanned != len(files) || scanned == 0 {
		t.Fatalf("scanned %d of %d files (must be equal and > 0)", scanned, len(files))
	}
	if len(mismatches) > 0 {
		t.Fatalf("census mismatches (%d):\n%s", len(mismatches), strings.Join(mismatches, "\n"))
	}
	sum := 0
	for _, es := range censusTable {
		for _, e := range es {
			sum += e.count
		}
	}
	if sum > censusCeiling {
		t.Fatalf("allowed sum %d exceeds Ceiling %d — entries only go DOWN", sum, censusCeiling)
	}
}

// expand resolves the {OLD} path placeholder at runtime.
func expand(path string) string {
	return strings.ReplaceAll(path, "{OLD}", "no"+"fx")
}

// context prints up to two matched lines from the file on a mismatch.
func context(b []byte, tok string) string {
	lt := strings.ToLower(tok)
	var found []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.Contains(strings.ToLower(ln), lt) {
			ln = strings.TrimSpace(ln)
			if len(ln) > 120 {
				ln = ln[:120] + "..."
			}
			found = append(found, ln)
			if len(found) == 2 {
				break
			}
		}
	}
	return strings.Join(found, " | ")
}
