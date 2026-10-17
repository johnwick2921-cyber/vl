# DS-108 salvage audit — restored worktrees (2026-10-17)

READ-ONLY. Nothing deleted, no git writes in the restored trees. 684 (b) files backed up to /home/hoang/fleet-1017/salvage/<worktree>/.
worktrees scanned: 115 interesting (dirty>0 or unpushed>0)
dirty files: a=15 (same-as-github)  b=684 (newer)  c=242 (junk)
unpushed commits: 221 total, 208 already on origin/dev

## Verdicts
- KEEP-NEWER: 61
- SAFE-TO-DROP: 38
- ASK: 15
- DS-107 hands-off: 1 (vl-knobtruth-merge)

## Table
| path | branch | unpushed (on-gh) | dirty a/b/c | verdict |
|---|---|---|---|---|
| /home/hoang/nofx-101x2-red1 | (detached) | 13 (12) | 0/0/0 | ASK |
| /home/hoang/nofx-101x2-red2 | (detached) | 13 (12) | 0/0/0 | ASK |
| /home/hoang/nofx-101x2-red3 | (detached) | 26 (24) | 0/0/0 | ASK |
| /home/hoang/nofx-101x2-red4 | (detached) | 25 (24) | 0/0/0 | ASK |
| /home/hoang/nofx-103b | refs/heads/feat/owner-levels-ui | 1 (0) | 0/0/0 | ASK |
| /home/hoang/nofx-cto-208 | (detached) | 1 (0) | 0/0/0 | ASK |
| /home/hoang/nofx-cto-combo | (detached) | 1 (0) | 0/0/0 | ASK |
| /home/hoang/nofx-docsmerge | (detached) | 25 (0) | 0/0/0 | ASK |
| /home/hoang/nofx-ds103-census | refs/heads/test/census-258-clock-pins | 1 (0) | 0/0/0 | ASK |
| /home/hoang/nofx-trial | refs/heads/prep/rename-vl-trial | 4 (0) | 0/0/0 | ASK |
| /home/hoang/vl-108-rel10-check | refs/heads/scratch/rel10-check | 2 (0) | 0/0/0 | ASK |
| /home/hoang/vl-ds106-rel20dry | refs/heads/rel20dry | 4 (0) | 0/0/0 | ASK |
| /home/hoang/vl-ds108-stopverify | refs/heads/fix/stop-move-verify | 2 (0) | 0/0/0 | ASK |
| /home/hoang/vl-orb-course | refs/heads/fix/orb-course-rules | 5 (1) | 0/0/0 | ASK |
| /home/hoang/vl-rg564-head | (detached) | 5 (1) | 0/0/0 | ASK |
| /home/hoang/vl-knobtruth-merge | None | 0 (0) | 0/0/19 | DS-107 merge — hands off |
| /home/hoang/vl | refs/heads/dev | 0 (0) | 0/2/85 | KEEP-NEWER |
| /home/hoang/nofx-102 | refs/heads/fix/flip-reread | 0 (0) | 0/14/0 | KEEP-NEWER |
| /home/hoang/nofx-102-geom | refs/heads/fix/geometry-refusal | 0 (0) | 0/2/1 | KEEP-NEWER |
| /home/hoang/nofx-102-picture | refs/heads/fix/picture-htf | 0 (0) | 0/2/0 | KEEP-NEWER |
| /home/hoang/nofx-102b | refs/heads/fix/dormant-death-rearm | 0 (0) | 0/14/6 | KEEP-NEWER |
| /home/hoang/nofx-clockhold | refs/heads/fix/clock-hold | 0 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/nofx-collide | refs/heads/docs/class-number-collisions | 0 (0) | 10/47/0 | KEEP-NEWER |
| /home/hoang/nofx-ds-101-flashab | refs/heads/research/flash-vs-pro-ab | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/nofx-ds-103-red | (detached) | 0 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/nofx-ds106-guide | refs/heads/feat/mentor-guide | 0 (0) | 0/232/7 | KEEP-NEWER |
| /home/hoang/nofx-ds106-planner | refs/heads/fix/planner-arm-reach | 0 (0) | 0/125/39 | KEEP-NEWER |
| /home/hoang/nofx-ds106-w117a2 | refs/heads/fix/w117-a2-ordered-exec | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/nofx-ds107-defaults | refs/heads/fix/defaults-sane | 0 (0) | 0/130/33 | KEEP-NEWER |
| /home/hoang/nofx-ds108-c2 | refs/heads/crypto-removal-c2 | 0 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/nofx-entry | refs/heads/feat/entry-mechanics | 0 (0) | 1/3/0 | KEEP-NEWER |
| /home/hoang/nofx-flipdir | refs/heads/fix/flip-direction-validator | 0 (0) | 0/7/5 | KEEP-NEWER |
| /home/hoang/nofx-literals | refs/heads/fix/prompt-floor-bootline-literals | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/nofx-olapi | refs/heads/feat/owner-level-list-api | 0 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/nofx-rb12 | refs/heads/fix/rebrand-phase-1-2 | 0 (0) | 0/4/0 | KEEP-NEWER |
| /home/hoang/nofx-ui | refs/heads/fix/ui-serving-path-clean | 0 (0) | 0/26/26 | KEEP-NEWER |
| /home/hoang/nofx-vf | refs/heads/docs/dress-rehearsal-0830 | 0 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/vl-b11-link-idle | refs/heads/fix/b11-link-idle | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-cto-tl | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds101-audit-setups | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds104-trendline | refs/heads/fix/trendline-third-touch | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds105-n14 | refs/heads/feat/mentor-stop-after-loss | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds105-n7 | refs/heads/fix/mentor-x2-uno-reverse | 0 (0) | 0/2/0 | KEEP-NEWER |
| /home/hoang/vl-ds105-parity-run | (detached) | 0 (0) | 0/3/1 | KEEP-NEWER |
| /home/hoang/vl-ds106-boxrefs | refs/heads/fix/rel9-boxrefs | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-clock | refs/heads/test/faster-clock-waits | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-item18 | refs/heads/fix/mentor-news-print | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-kflh | refs/heads/feat/keylevel-full-history | 0 (0) | 0/3/1 | KEEP-NEWER |
| /home/hoang/vl-ds106-kicks | refs/heads/fix/live-sink-kicks-first | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-rel17 | refs/heads/release/2026-10-10-1 | 0 (0) | 0/2/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-rel21 | refs/heads/release/2026-10-14-1 | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-rel22dry | refs/heads/rel22-dry | 1 (0) | 0/3/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-roomd | refs/heads/fix/room-rule-d | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-roomrule | refs/heads/scratch/room-rule-option-b | 0 (0) | 0/4/0 | KEEP-NEWER |
| /home/hoang/vl-ds106-urfix | refs/heads/fix/ur-sliding-index | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds107-stopverify | refs/heads/f1-tmp | 0 (0) | 0/2/0 | KEEP-NEWER |
| /home/hoang/vl-ds108-govuln | refs/heads/fix/go-vuln-1026 | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-ds108-replay | refs/heads/test/full-day-replay | 0 (0) | 0/2/0 | KEEP-NEWER |
| /home/hoang/vl-exit-drive | refs/heads/fix/mentor-exit-drive-loop | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-exit-mtf | refs/heads/fix/mentor-exit-mtf | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-fullhist | (detached) | 0 (0) | 0/2/2 | KEEP-NEWER |
| /home/hoang/vl-mentor-eval | refs/heads/feat/mentor-eval | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-mentor-p3 | refs/heads/feat/mentor-p3 | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-mentor-phantom-arm | refs/heads/fix/mentor-phantom-arm | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-mentor-truth | refs/heads/feat/mentor-truth-panel | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-planner-4 | refs/heads/fix/planner-4-split | 5 (1) | 2/5/0 | KEEP-NEWER |
| /home/hoang/vl-rel10-dev | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-rel10-fix | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-reread-before | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-room-after | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-room-b | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-room-before | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-room-d | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-trend-a | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-trend-ab | (detached) | 1 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-trend-b | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/vl-trend-base | (detached) | 0 (0) | 0/1/0 | KEEP-NEWER |
| /home/hoang/nofx-101m4b-f13 | (detached) | 5 (5) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101m4b-u1 | (detached) | 6 (6) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101m4b-u1f | (detached) | 1 (9) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101m4b-u2 | (detached) | 2 (2) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101m4b-u2f | (detached) | 2 (8) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101w1-r | (detached) | 3 (3) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-docs | (detached) | 1 (1) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e12 | (detached) | 3 (3) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e12x | (detached) | 2 (2) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e1510 | (detached) | 5 (5) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e3 | (detached) | 1 (1) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e76 | (detached) | 2 (2) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x1-e913 | (detached) | 5 (5) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-auth | (detached) | 12 (12) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-cc | (detached) | 3 (14) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-fapi | (detached) | 7 (7) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-fcli | (detached) | 2 (2) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-ftg | (detached) | 2 (2) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-fweb | (detached) | 1 (1) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-ha | (detached) | 2 (13) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-hb | (detached) | 1 (12) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-hc | (detached) | 4 (4) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-101x2-wire | (detached) | 12 (12) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/nofx-cto-gate-boot3 | (detached) | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-101-plandeath | refs/heads/research/plan-death-census | 0 (0) | 0/0/2 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-102-audit0926 | refs/heads/audit/0926-knobs | 0 (0) | 0/0/2 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-102-verify0926 | refs/heads/verify/0926-system | 0 (0) | 0/0/2 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-103-deadwire | refs/heads/audit/0926-dead-wire | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-103-verify0926 | refs/heads/verify/0926-partner | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-106-audit0926 | refs/heads/audit/0926-trading-pipeline | 0 (0) | 0/0/2 | SAFE-TO-DROP |
| /home/hoang/nofx-ds-106-verify0926 | refs/heads/verify/0926-stop-entry | 0 (0) | 0/0/2 | SAFE-TO-DROP |
| /home/hoang/nofx-r001 | refs/heads/docs/round-25-seat-displacement | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/nofx-r101 | refs/heads/docs/round-23-top-down-map | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/vl-ds106-b10 | refs/heads/fix/mentor-l12-leg-budget-days | 1 (1) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/vl-ds106-b9 | refs/heads/fix/mentor-l11-box-block-whole | 1 (1) | 0/0/0 | SAFE-TO-DROP |
| /home/hoang/vl-ds108-lvlgrade | refs/heads/docs/level-grade-window-1014 | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/vl-fullhistory-replay | (detached) | 0 (0) | 0/0/1 | SAFE-TO-DROP |
| /home/hoang/vl-wake-race | refs/heads/fix/wake-clock-race | 0 (0) | 2/0/0 | SAFE-TO-DROP |

## KEEP-NEWER — (b) files (backed up)
### /home/hoang/vl
- `.cgcignore`
- `deploy/RELEASE.pre-vl.4d5382069643`

### /home/hoang/nofx-102
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `manager/trader_manager.go`
- `store/knob_registry_table.go`
- `store/strategy.go`
- `trader/auto_trader_planner.go`
- `trader/plan_liveness.go`
- `web/src/components/strategy/DayPlanEditor.test.tsx`
- `web/src/components/strategy/DayPlanEditor.tsx`
- `web/src/guide/GuidePage.test.tsx`
- `web/src/guide/content/settings.ts`
- `web/src/i18n/plan-translations.ts`
- `web/src/types/strategy.ts`
- `trader/flip_reread_boot.go`
- `trader/flip_reread_test.go`

### /home/hoang/nofx-102-geom
- `main.go`
- `trader/auto_trader_planner.go`

### /home/hoang/nofx-102-picture
- `cmd/picture_htf_replay/main.go`
- `store/knob_registry_table.go`

### /home/hoang/nofx-102b
- `api/handler_klines.go`
- `api/klines_across_roll_test.go`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `store/bar_history_across_roll.go`
- `store/bar_history_across_roll_test.go`
- `web/src/components/plan/BulkAddSheet.tsx`
- `web/src/components/plan/EditSheet.tsx`
- `web/src/components/plan/P5_door.test.tsx`
- `web/src/components/plan/PlanMiniChart.tsx`
- `web/src/components/plan/SessionPlanCard.tsx`
- `web/src/components/plan/usePlan.ts`
- `web/src/guide/content/planCard.ts`
- `web/src/i18n/plan-translations.ts`
- `web/src/lib/api/plan.ts`

### /home/hoang/nofx-clockhold
- `api/handler_plan_test.go`
- `kernel/clock_drift.go`
- `kernel/clock_hold_f6_test.go`

### /home/hoang/nofx-collide
- `api/server.go`
- `cmd/arm-state-sql/main.go`
- `deploy/canon_contract_test.go`
- `deploy/nofx-lock-test.sh`
- `deploy/nofx-lock.sh`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `docs/superpowers/CLAUDE-canon.md`
- `docs/superpowers/SYSTEM-MAP.md`
- `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q51_complete.py`
- `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/replay.py`
- `docs/superpowers/reports/2026-09-09-arm-state-predicate-data/go-test.txt`
- `docs/superpowers/reports/2026-09-09-arm-state-predicate-data/red.txt`
- `docs/superpowers/reports/2026-09-09-arm-state-predicate-data/unicode-red.txt`
- `docs/superpowers/reports/2026-09-09-arm-state-predicate-data/validation.json`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/brand-parent-red.txt`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/final-validation.json`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/followup-mutations.json`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/lock-tests.txt`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/lunch-parent-red.txt`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/source-freshness.json`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover-data/tz-parent-red.txt`
- `docs/superpowers/reports/2026-09-10-arm-state-cutover.md`
- `docs/superpowers/reports/2026-09-10-lock-three-defects.md`
- `kernel/candle_disclosure.go`
- `store/arm_state.go`
- `store/arm_state_source_guard_test.go`
- `store/arm_state_test.go`
- `store/armed_orders.go`
- `store/armed_placement_dup_test.go`
- `store/wave_a_migration.go`
- `trader/arm_cancel_safety.go`
- `trader/arm_state_contract_test.go`
- `trader/arm_sweep_settlement_test.go`
- `trader/armed_executor.go`
- `trader/armed_row_hygiene_test.go`
- `trader/class33_boot_sweep.go`
- `trader/class33_cutover_gate.go`
- `trader/f12_leg4.go`
- `trader/ninjatrader/bar_horizon_warn.go`
- `trader/one_contract.go`
- `trader/regime_input_window.go`
- `trader/types/interface.go`
- `trader/zerob_exit_sanity_test.go`
- `web/src/brand-scope.test.ts`
- `web/src/guide/content/guards.ts`
- `web/src/guide/content/status.ts`
- `web/src/test/brand-scope-baseline.json`

### /home/hoang/nofx-ds-101-flashab
- `trader/research_export_ab.go`

### /home/hoang/nofx-ds-103-red
- `web/src/App.tsx`
- `web/src/brand-scope.test.ts`
- `web/src/test/brand-scope-baseline.json`

### /home/hoang/nofx-ds106-guide
- `.env.example`
- `.github/workflows/docker-build.yml`
- `.github/workflows/release.yml`
- `.gitignore`
- `Makefile`
- `agent/backend_logs_test.go`
- `agent/brand_visible_test.go`
- `agent/config_visibility_test.go`
- `agent/memory_test.go`
- `agent/prompt_persona.go`
- `agent/tools.go`
- `api/agent_routes.go`
- `api/handler_exchange_test.go`
- `api/handler_klines.go`
- `api/handler_klines_depth_test.go`
- `api/handler_klines_r1a_test.go`
- `api/handler_updates.go`
- `api/handler_updates_knob_test.go`
- `api/handler_updates_mac_purpose_test.go`
- `api/handler_updates_starter_test.go`
- `api/handler_updates_test.go`
- `api/handler_updates_verifier_test.go`
- `api/handler_user.go`
- `api/machine_scope_test.go`
- `api/release_dir.go`
- `api/release_dir_test.go`
- `api/server.go`
- `api/strategy_effective.go`
- `api/token_clock_window_test.go`
- `branding/census_test.go`
- `branding/scope_test.go`
- `cmd/levelstats-backfill/main.go`
- `cmd/nq_smoke/smoke_roundtrip.go`
- `cmd/vl-activate/main.go`
- `cmd/vl-activate/main_test.go`
- `cmd/vl-updater/main.go`
- `cmd/vl-updater/main_test.go`
- `cmd/vl-updater/rename_r1a_test.go`
- `deploy/bars-key-rollback.sh`
- `deploy/canon_contract_test.go`
- `deploy/cutover.sh`
- `deploy/cutover_test.sh`
- `deploy/install-autostart.sh`
- `deploy/install-updater-worker.sh`
- `deploy/nofx-claim.sh`
- `deploy/nofx-clock-guard.sh`
- `deploy/nofx-db-backup.sh`
- `deploy/nofx-lock.sh`
- `deploy/planner-ab-report.sh`
- `deploy/planner_ab_report_fixture_test.sh`
- `deploy/postboot-check.sh`
- `deploy/release/README.md`
- `deploy/release/package.sh`
- `deploy/release_contract_test.go`
- `deploy/release_manifest_addon_pin_test.go`
- `deploy/release_manifest_caller_test.go`
- `deploy/release_manifest_versions_test.go`
- `deploy/rename_r1a_contract_test.go`
- `deploy/unit_templates_test.go`
- `deploy/updater_worker_install_test.go`
- `deploy/vl-claim.sh`
- `deploy/vl-clock-guard.sh`
- `deploy/vl-db-backup.sh`
- `deploy/vl-lock-test.sh`
- `deploy/vl-lock.sh`
- `deploy/vl_db_backup_test.go`
- `deploy/vl_twin_contract_test.go`
- `docker/Dockerfile.backend`
- `docs/internal/external-reviews/2026-05-28-end-to-end-architecture.md`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `docs/superpowers/CLAUDE-canon.md`
- `docs/superpowers/SYSTEM-MAP.md`
- `docs/superpowers/VL-TRADING-RULEBOOK-v1.md`
- `docs/superpowers/plans/2026-08-29-news-hygiene-micro-wave.md`
- `docs/superpowers/plans/2026-08-29-sunday-shield-wave.md`
- `docs/superpowers/reports/2026-08-18-timegate-audit-ai-timeout.md`
- `docs/superpowers/reports/2026-08-19-final-bundle.md`
- `docs/superpowers/reports/2026-08-19-ledger-close-FINAL.md`
- `docs/superpowers/reports/2026-08-19-zerotrade-forensics.md`
- `docs/superpowers/reports/2026-08-20-brand-census-docs-brand-census.md`
- `docs/superpowers/reports/2026-08-20-brand-census.md`
- `docs/superpowers/reports/2026-08-25-1h-wave-r2-r4-implementation.md`
- `docs/superpowers/reports/2026-08-26-bar-persistence.md`
- `docs/superpowers/reports/2026-08-26-fvg-entry-model.md`
- `docs/superpowers/reports/2026-08-26-packb-volume-levels.md`
- `docs/superpowers/reports/2026-08-26-s-fix-wave.md`
- `docs/superpowers/reports/2026-08-27-guide-page.md`
- `docs/superpowers/reports/2026-08-27-plan-mode-layering-ux.md`
- `docs/superpowers/reports/2026-08-28-bar-truth-wave.md`
- `docs/superpowers/reports/2026-08-28-forensics-hygiene-wave.md`
- `docs/superpowers/reports/2026-08-28-missed-200pt.md`
- `docs/superpowers/reports/2026-08-28-waterfall-class-wave.md`
- `docs/superpowers/reports/2026-08-29-f1-dependency-vuln-scan.md`
- `docs/superpowers/reports/2026-08-29-total-audit-15.md`
- `docs/superpowers/reports/2026-08-30-weekly-bias-wave.md`
- `docs/superpowers/reports/2026-09-01-class35-replan-budget.md`
- `docs/superpowers/reports/2026-09-01-full-system-audit.md`
- `docs/superpowers/reports/2026-09-01-planner-api-failure.md`
- `docs/superpowers/reports/2026-09-02-bar-source-audit.md`
- `docs/superpowers/reports/2026-09-02-deepseek-e2e-audit.md`
- `docs/superpowers/reports/2026-09-02-detector-redesign.md`
- `docs/superpowers/reports/2026-09-03-rebrand-census.md`
- `docs/superpowers/reports/2026-09-06-v5-precheck.md`
- `docs/superpowers/reports/2026-09-08-brand-visible.md`
- `docs/superpowers/reports/2026-09-08-plan-liveness.md`
- `docs/superpowers/reports/2026-09-08-scenario-economics.md`
- `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/basis-receipts.json`
- `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/basis_receipts.py`
- `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/remote-ci-setup-failures.txt`
- `docs/superpowers/reports/2026-09-12-structural-stop.md`
- `docs/superpowers/reports/2026-09-12-structural-stop/evidence/github-checks.json`
- `docs/superpowers/reports/2026-09-13-repo-understanding/CORE-TRACE.md`
- `docs/superpowers/reports/2026-09-13-repo-understanding/CTO-REPORT.md`
- `docs/superpowers/reports/2026-09-13-repo-understanding/FULL-AUDIT.md`
- `docs/superpowers/reports/2026-09-13-repo-understanding/tools/build-core-trace.py`
- `docs/superpowers/reports/2026-09-13-repository-repairs/dependency-alerts-at-review.jsonl`
- `docs/superpowers/reports/2026-09-13-structural-stop-daily-loss-evidence/github-checks.json`
- `docs/superpowers/research/2026-09-08-trading-policy/README.md`
- `docs/superpowers/research/2026-09-09-structure-candle-targets-staleness/README.md`
- `docs/superpowers/research/2026-09-12-stop-target-geometry/README.md`
- `docs/superpowers/research/2026-09-16-s2-replay/harness/main.go`
- `docs/superpowers/runbooks/2026-09-04-partner-update.md`
- `docs/superpowers/runbooks/2026-09-08-clean-machine.md`
- `internal/activation/activation.go`
- `internal/activation/rename_r1a_test.go`
- `internal/activation/stage_test.go`
- `internal/activation/steps.go`
- `internal/activation/steps_test.go`
- `internal/activation/system.go`
- `internal/envcompat/envcompat.go`
- `internal/envcompat/envcompat_test.go`
- `internal/installpath/installpath.go`
- `internal/installpath/releasedir.go`
- `internal/installpath/releasedir_test.go`
- `internal/updateauth/census_directives_test.go`
- `internal/updaterbootstrap/workertoken.go`
- `internal/updaterjob/consistency_test.go`
- `internal/updaterjob/golden_test.go`
- `internal/updaterjob/job_test.go`
- `internal/updaterjob/testdata/job_full.golden.json`
- `internal/updaterjob/testdata/legacy-jobs/e1dcc173751ccbfe0b2a7268fd40451a.json`
- `internal/updaterjob/verdict.go`
- `internal/updaterjob/verdict_read_test.go`
- `internal/updaterjob/view_test.go`
- `internal/updatersource/source.go`
- `internal/updaterwire/paths_test.go`
- `internal/updaterworker/activate_reprove_test.go`
- `internal/updaterworker/app_http.go`
- `internal/updaterworker/containment_test.go`
- `internal/updaterworker/deps.go`
- `internal/updaterworker/harness_test.go`
- `internal/updaterworker/hold_census_test.go`
- `internal/updaterworker/host_os.go`
- `internal/updaterworker/host_os_test.go`
- `internal/updaterworker/library_activation_test.go`
- `internal/updaterworker/nt8_test.go`
- `internal/updaterworker/orphans_test.go`
- `internal/updaterworker/preflight_test.go`
- `internal/updaterworker/recovery.go`
- `internal/updaterworker/release.go`
- `internal/updaterworker/release_test.go`
- `internal/updaterworker/releasecheck.go`
- `internal/updaterworker/releasefixture/releasefixture.go`
- `internal/updaterworker/releaseroot.go`
- `internal/updaterworker/releaseroot_unit_test.go`
- `internal/updaterworker/rename_r1a_test.go`
- `internal/updaterworker/reverifier_test.go`
- `internal/updaterworker/rollback_test.go`
- `internal/updaterworker/runner.go`
- `internal/updaterworker/snapshot.go`
- `internal/updaterworker/sqldriverpin/sqldriverpin_test.go`
- `internal/updaterworker/sshsig_test.go`
- `internal/updaterworker/steps.go`
- `internal/updaterworker/target.go`
- `internal/updaterworker/worker_self_update.go`
- `kernel/boot_integrity.go`
- `kernel/boot_integrity_test.go`
- `kernel/clock_health.go`
- `kernel/clock_health_test.go`
- `kernel/funding_suppress_test.go`
- `logger/log_prune.go`
- `logger/log_prune_r1a_test.go`
- `logger/log_prune_test.go`
- `main.go`
- `ninjascript/VLTraderTCPClient.cs`
- `ninjascript/vltrader_tcp_PROTOCOL.md`
- `provider/ninjatrader/bar_source.go`
- `provider/ninjatrader/bar_source_r1a_test.go`
- `provider/ninjatrader/order_snapshot.go`
- `provider/ninjatrader/tcp_framing.go`
- `provider/ninjatrader/tcp_server.go`
- `scripts/sandbox-down.sh`
- `scripts/sandbox-up.sh`
- `start.sh`
- `store/ab_confirm.go`
- `store/adherence_regrade.go`
- `store/armed_orders.go`
- `store/armed_orders_test.go`
- `store/bar_contract_key.go`
- `store/maintenance_census_mint_test.go`
- `store/sqlitedriver/one_registration_census_test.go`
- `store/strategy_schema_test.go`
- `store/testdata/settings_truth_rows.json`
- `store/wave_a_migration.go`
- `telegram/agent/apicall.go`
- `telegram/bot_token_same_second_test.go`
- `trader/armed_executor.go`
- `trader/auto_trader_calendar.go`
- `trader/cancel_budget_bootline_test.go`
- `trader/cancel_confirm.go`
- `trader/cancel_confirm_test.go`
- `trader/f0_calendar_ignition_test.go`
- `trader/ninjatrader/bar_source_wire_test.go`
- `trader/ninjatrader/tcp_trader.go`
- `trader/ninjatrader/trader.go`
- `trader/stop_entry_sibling_latch_test.go`
- `trader/t1_news_hygiene_test.go`
- `trader/testdata/w2-write-truth/plan_row_452_subset.json`
- `trader/testdata/w2-write-truth/plan_row_455_subset.json`
- `trader/wave_b_placement_test.go`
- `web/src/bootstrapOrder.test.ts`
- `web/src/brand-scope.test.ts`
- `web/src/components/faq/FAQContent.tsx`
- `web/src/guide/GuidePage.test.tsx`
- `web/src/guide/content/mentor.ts`
- `web/src/guide/content/settings.ts`
- `web/src/guide/content/status.ts`
- `web/src/i18n/translations.ts`
- `web/src/lib/storageMigration.test.ts`
- `web/src/lib/storageMigration.ts`
- `web/src/main.tsx`
- `web/src/test/brand-scope-baseline.json`

### /home/hoang/nofx-ds106-planner
- `agent/agent.go`
- `agent/onboard.go`
- `agent/web.go`
- `api/handler_plan.go`
- `api/strategy_effective_test.go`
- `clock-seams-owed.list`
- `clock-seams.list`
- `cmd/planner_ab/main.go`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `expectancy/expectancy_test.go`
- `go.mod`
- `kernel/class38_guard_test.go`
- `kernel/class45_feeds_forward.go`
- `kernel/class45_feeds_forward_test.go`
- `kernel/class45_pin_test.go`
- `kernel/entry_policy_prompt.go`
- `kernel/entry_policy_prompt_test.go`
- `kernel/knob_prune_pin_test.go`
- `kernel/levels_swing.go`
- `kernel/map_candidates.go`
- `kernel/plan_doc.go`
- `kernel/plan_machine.go`
- `kernel/plan_machine_test.go`
- `kernel/plan_overlay.go`
- `kernel/planner_prompt.go`
- `kernel/prompt_contract.go`
- `kernel/scenario_write_truth.go`
- `kernel/testdata/identity_legacy_output.json`
- `kernel/testdata/knob_prune/executor_keylevels.txt`
- `kernel/testdata/knob_prune/planner_prompt_defaults.txt`
- `kernel/testdata/knob_prune/planner_prompt_legacy_policy.txt`
- `main.go`
- `provider/ninjatrader/bar_frame_age_test.go`
- `provider/ninjatrader/bar_live_sink.go`
- `provider/ninjatrader/bar_persist.go`
- `provider/ninjatrader/closed_cache_tail_test.go`
- `provider/ninjatrader/order_snapshot.go`
- `provider/ninjatrader/order_snapshot_test.go`
- `provider/ninjatrader/tcp_server.go`
- `store/armed_orders.go`
- `store/armed_zone_test.go`
- `store/cancel_lifecycle_test.go`
- `store/knob_registry_table.go`
- `store/strategy.go`
- `trader/arm_cancel_safety.go`
- `trader/armed_executor.go`
- `trader/auto_trader.go`
- `trader/auto_trader_clock.go`
- `trader/auto_trader_flip_breach.go`
- `trader/auto_trader_matched_random.go`
- `trader/auto_trader_no_trade_map_test.go`
- `trader/auto_trader_planner.go`
- `trader/auto_trader_planner_test.go`
- `trader/auto_trader_reread.go`
- `trader/auto_trader_transition.go`
- `trader/auto_trader_wake_levels.go`
- `trader/auto_trader_wake_levels_test.go`
- `trader/auto_trader_weekly.go`
- `trader/auto_trader_weekly_test.go`
- `trader/cancel_confirm.go`
- `trader/cancel_settlement_test.go`
- `trader/class160_signal_keyed_test.go`
- `trader/class32_wallclock_test.go`
- `trader/class35_replan_budget_test.go`
- `trader/class36_preflight_test.go`
- `trader/death_reread.go`
- `trader/death_reread_wave_test.go`
- `trader/effective_settings.go`
- `trader/entry_duplicate_sequences_test.go`
- `trader/entry_gate.go`
- `trader/flip_breach_test.go`
- `trader/flip_direction_warn_test.go`
- `trader/flip_reread_cto_test.go`
- `trader/flip_reread_test.go`
- `trader/follow_plan_wiring.go`
- `trader/gate_parity_matrix_test.go`
- `trader/geometry_refusal_wave_test.go`
- `trader/identity_wiring_test.go`
- `trader/knob_prune_pin_test.go`
- `trader/maintenance_planner_test.go`
- `trader/maintenance_wiring.go`
- `trader/ninjatrader/account_binding_test.go`
- `trader/ninjatrader/level_stats_nightly_test.go`
- `trader/ninjatrader/level_stats_wire.go`
- `trader/ninjatrader/reconcile.go`
- `trader/ninjatrader/tcp_trader.go`
- `trader/ninjatrader/trader.go`
- `trader/ninjatrader/trader_test.go`
- `trader/one_setup_boot.go`
- `trader/one_setup_boot_test.go`
- `trader/picture_htf_evaluator.go`
- `trader/picture_htf_evaluator_test.go`
- `trader/picture_htf_live.go`
- `trader/picture_scenario_executor_test.go`
- `trader/plan_born_check_writesite_test.go`
- `trader/plan_liveness_test.go`
- `trader/protection_reconciler.go`
- `trader/reread_drain_test.go`
- `trader/research_snapshot_test.go`
- `trader/scenario_level_identity.go`
- `trader/scenario_level_identity_test.go`
- `trader/shadow_demotion_test.go`
- `trader/strict_nudge.go`
- `trader/structural_geometry.go`
- `trader/write_time_feasibility.go`
- `trader/write_time_feasibility_test.go`
- `web/package-lock.json`
- `web/src/brand-scope.test.ts`
- `web/src/components/charts/AdvancedChart.tsx`
- `web/src/components/charts/ChartTabs.test.tsx`
- `web/src/components/charts/ChartTabs.tsx`
- `web/src/components/modals/TwoStageKeyModal.tsx`
- `web/src/components/plan/EditSheet.tsx`
- `web/src/components/plan/P5_door.test.tsx`
- `web/src/components/plan/SessionPlanCard.tsx`
- `web/src/components/strategy/__fixtures__/strategyEffective.fixture.json`
- `web/src/guide/GuidePage.test.tsx`
- `web/src/guide/content/guards.ts`
- `web/src/guide/content/planCard.ts`
- `web/src/guide/content/settings.ts`
- `web/src/guide/content/status.ts`
- `web/src/lib/api/plan.ts`
- `web/src/pages/AgentChatPage.tsx`
- `web/src/pages/TraderDashboardPage.tsx`
- `web/src/test/brand-scope-baseline.json`

### /home/hoang/nofx-ds106-w117a2
- `provider/ninjatrader/ordered_exec.go`

### /home/hoang/nofx-ds107-defaults
- `.github/workflows/release.yml`
- `CONTRIBUTING.md`
- `INSTALL.md`
- `Makefile`
- `agent/agent.go`
- `agent/onboard.go`
- `agent/web.go`
- `api/handler_plan.go`
- `auth/clock_window_test.go`
- `auth/mint_census_test.go`
- `clock-seams-owed.list`
- `clock-seams.list`
- `cmd/planner_ab/main.go`
- `deploy/cutover.sh`
- `deploy/guide_rev_producers_test.go`
- `deploy/release/README.md`
- `deploy/release/db-compat.sh`
- `deploy/release/manifest.sh`
- `deploy/release/secret-scan.sh`
- `deploy/release_contract_test.go`
- `deploy/release_manifest_caller_test.go`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `docs/superpowers/SYSTEM-MAP.md`
- `docs/superpowers/runbooks/2026-09-04-partner-update.md`
- `expectancy/expectancy_test.go`
- `go.mod`
- `internal/activation/steps_test.go`
- `internal/updateauth/census_mint_test.go`
- `internal/updateauth/census_test.go`
- `internal/updateauth/census_walk_test.go`
- `kernel/class38_guard_test.go`
- `kernel/entry_policy_prompt.go`
- `kernel/entry_policy_prompt_test.go`
- `kernel/knob_prune_pin_test.go`
- `kernel/levels_swing.go`
- `kernel/plan_doc.go`
- `kernel/plan_machine.go`
- `kernel/plan_machine_test.go`
- `kernel/plan_overlay.go`
- `kernel/planner_prompt.go`
- `kernel/prompt_contract.go`
- `kernel/scenario_write_truth.go`
- `kernel/testdata/knob_prune/planner_prompt_defaults.txt`
- `kernel/testdata/knob_prune/planner_prompt_legacy_policy.txt`
- `main.go`
- `provider/ninjatrader/bar_frame_age_test.go`
- `provider/ninjatrader/bar_live_sink.go`
- `provider/ninjatrader/bar_live_sink_burst_test.go`
- `provider/ninjatrader/bar_persist.go`
- `provider/ninjatrader/closed_cache_tail_test.go`
- `provider/ninjatrader/tcp_server.go`
- `store/armed_orders.go`
- `store/armed_zone_test.go`
- `store/knob_registry_table.go`
- `store/picture_defaults_test.go`
- `store/strategy.go`
- `store/users_writer_census_test.go`
- `trader/auto_trader.go`
- `trader/auto_trader_clock.go`
- `trader/auto_trader_flip_breach.go`
- `trader/auto_trader_matched_random.go`
- `trader/auto_trader_no_trade_map_test.go`
- `trader/auto_trader_planner.go`
- `trader/auto_trader_planner_test.go`
- `trader/auto_trader_reread.go`
- `trader/auto_trader_transition.go`
- `trader/auto_trader_wake_levels.go`
- `trader/auto_trader_wake_levels_test.go`
- `trader/auto_trader_weekly.go`
- `trader/auto_trader_weekly_test.go`
- `trader/class160_signal_keyed_test.go`
- `trader/class32_wallclock_test.go`
- `trader/class35_replan_budget_test.go`
- `trader/class36_preflight_test.go`
- `trader/death_reread.go`
- `trader/death_reread_wave_test.go`
- `trader/entry_duplicate_sequences_test.go`
- `trader/flip_breach_test.go`
- `trader/flip_direction_warn_test.go`
- `trader/flip_reread_cto_test.go`
- `trader/flip_reread_test.go`
- `trader/follow_plan_wiring.go`
- `trader/gate_parity_matrix_test.go`
- `trader/knob_prune_pin_test.go`
- `trader/maintenance_planner_test.go`
- `trader/maintenance_wiring.go`
- `trader/ninjatrader/account_binding_test.go`
- `trader/ninjatrader/level_stats_nightly_test.go`
- `trader/ninjatrader/level_stats_wire.go`
- `trader/ninjatrader/reconcile.go`
- `trader/ninjatrader/tcp_trader.go`
- `trader/ninjatrader/trader.go`
- `trader/ninjatrader/trader_test.go`
- `trader/one_setup_boot.go`
- `trader/one_setup_boot_test.go`
- `trader/picture_scenario_executor_test.go`
- `trader/plan_born_check_writesite_test.go`
- `trader/plan_liveness_test.go`
- `trader/reread_drain_test.go`
- `trader/research_snapshot_test.go`
- `trader/scenario_level_identity.go`
- `trader/scenario_level_identity_test.go`
- `trader/shadow_demotion_test.go`
- `trader/strict_nudge.go`
- `trader/write_time_feasibility_test.go`
- `web/package-lock.json`
- `web/scripts/stamp-guide-rev.sh`
- `web/src/brand-scope.test.ts`
- `web/src/components/charts/AdvancedChart.tsx`
- `web/src/components/charts/ChartTabs.test.tsx`
- `web/src/components/charts/ChartTabs.tsx`
- `web/src/components/modals/TwoStageKeyModal.tsx`
- `web/src/components/plan/EditSheet.tsx`
- `web/src/components/plan/P5_door.test.tsx`
- `web/src/components/plan/SessionPlanCard.tsx`
- `web/src/components/updates/UpdateBadge.test.tsx`
- `web/src/components/updates/UpdateBadge.tsx`
- `web/src/guide/content/guards.ts`
- `web/src/guide/content/planCard.ts`
- `web/src/guide/content/settings.ts`
- `web/src/guide/content/status.ts`
- `web/src/i18n/updates-translations.ts`
- `web/src/lib/api/plan.ts`
- `web/src/lib/api/updates.test.ts`
- `web/src/lib/api/updates.ts`
- `web/src/pages/AgentChatPage.tsx`
- `web/src/pages/TraderDashboardPage.tsx`
- `web/src/pages/UpdatesPage.test.tsx`
- `web/src/pages/UpdatesPage.tsx`
- `web/src/test/brand-scope-baseline.json`

### /home/hoang/nofx-ds108-c2
- `agent/brain.go`
- `docs/ONBOARDING.md`
- `docs/operations/STARTUP.md`

### /home/hoang/nofx-entry
- `kernel/entry_law_test.go`
- `kernel/plan_confirm_test.go`
- `trader/split_entry_test.go`

### /home/hoang/nofx-flipdir
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `kernel/plan_lifecycle.go`
- `store/plan.go`
- `trader/auto_trader_dayplan.go`
- `trader/auto_trader_planner.go`
- `trader/flip_direction_warn_test.go`
- `web/src/guide/content/planCard.ts`

### /home/hoang/nofx-literals
- `trader/armed_executor.go`

### /home/hoang/nofx-olapi
- `api/handler_plan.go`
- `api/server.go`
- `api/handler_plan_ownerlevel_test.go`

### /home/hoang/nofx-rb12
- `agent/onboard.go`
- `main.go`
- `trader/lighter/trader.go`
- `trader/lighter/trading.go`

### /home/hoang/nofx-ui
- `deploy/RELEASE`
- `docs/superpowers/AUDIT-CHECKLIST.md`
- `docs/superpowers/research/INDEX.md`
- `kernel/level_stats_calc.go`
- `kernel/shadow_ab.go`
- `kernel/touch_telemetry.go`
- `main.go`
- `store/ab_confirm.go`
- `store/adherence_regrade_test.go`
- `store/attribution.go`
- `store/attribution_test.go`
- `store/plan.go`
- `store/position.go`
- `store/store.go`
- `store/touch_episode.go`
- `trader/auto_trader.go`
- `trader/auto_trader_clock.go`
- `trader/plan_lifecycle_wave_test.go`
- `web/src/components/plan/C_discipline.test.tsx`
- `web/src/components/plan/DisciplinePanel.tsx`
- `web/src/components/plan/ExpectancyPanel.tsx`
- `web/src/components/plan/PlanCard.tsx`
- `web/src/guide/content/expectancy.ts`
- `web/src/guide/content/guards.ts`
- `web/src/guide/content/status.ts`
- `web/src/guide/types.ts`

### /home/hoang/nofx-vf
- `cmd/vfverify/d1.go`
- `cmd/vfverify/d2.go`
- `cmd/vfverify/p3.go`

### /home/hoang/vl-b11-link-idle
- `provider/ninjatrader/link_idle.go`

### /home/hoang/vl-cto-tl
- `kernel/mentor/zz_cto_tl_probe_test.go`

### /home/hoang/vl-ds101-audit-setups
- `kernel/mentor/zz_audit_probe_test.go`

### /home/hoang/vl-ds104-trendline
- `kernel/mentor/trendline.go`

### /home/hoang/vl-ds105-n14
- `trader/mentor_b3_day_stop_sweep_test.go`

### /home/hoang/vl-ds105-n7
- `kernel/mentor/box.go`
- `kernel/mentor/eval.go`

### /home/hoang/vl-ds105-parity-run
- `kernel/mentor/fixture_test.go`
- `kernel/mentor/parity_tapegen_test.go`
- `kernel/mentor/parity_test.go`

### /home/hoang/vl-ds106-boxrefs
- `kernel/mentor/rel9_boxrefs_test.go`

### /home/hoang/vl-ds106-clock
- `mcp/config_usage_test.go`

### /home/hoang/vl-ds106-item18
- `trader/auto_trader.go`

### /home/hoang/vl-ds106-kflh
- `kernel/mentor/eval.go`
- `kernel/mentor/key_level_history_test.go`
- `kflh_prune.go`

### /home/hoang/vl-ds106-kicks
- `trader/live_sink_kicks_test.go`

### /home/hoang/vl-ds106-rel17
- `trader/auto_trader.go`
- `trader/mentor_exit_drive.go`

### /home/hoang/vl-ds106-rel21
- `.pr-body-21.md`

### /home/hoang/vl-ds106-rel22dry
- `trader/mentor_level_order_test.go`
- `trader/mentor_stall_alarm_test.go`
- `trader/mentor_tick.go`

### /home/hoang/vl-ds106-roomd
- `kernel/mentor/eval.go`

### /home/hoang/vl-ds106-roomrule
- `kernel/mentor/b9_isb_cap_test.go`
- `kernel/mentor/b9_obey_pins_test.go`
- `kernel/mentor/eval.go`
- `kernel/mentor/isb_trigger_side_test.go`

### /home/hoang/vl-ds106-urfix
- `kernel/mentor/ur_sliding_index_test.go`

### /home/hoang/vl-ds107-stopverify
- `trader/mentor_exit_drive.go`
- `trader/mentor_stop_move_verify_test.go`

### /home/hoang/vl-ds108-govuln
- `PR-BODY-GOVULN.md`

### /home/hoang/vl-ds108-replay
- `trader/full_day_replay_test.go`
- `trader/mentor_tick.go`

### /home/hoang/vl-exit-drive
- `trader/auto_trader.go`

### /home/hoang/vl-exit-mtf
- `kernel/mentor/mtf.go`

### /home/hoang/vl-fullhist
- `kernel/mentor/zz_replay_faithful_test.go`
- `kernel/mentor/zz_replay_fullhist_test.go`

### /home/hoang/vl-mentor-eval
- `kernel/mentor/replay_test.go`

### /home/hoang/vl-mentor-p3
- `trader/mentor_mode_test.go`

### /home/hoang/vl-mentor-phantom-arm
- `.pr-body.md`

### /home/hoang/vl-mentor-truth
- `api/handler_mentor_truth.go`

### /home/hoang/vl-planner-4
- `store/knob_registry_table.go`
- `trader/armed_executor.go`
- `web/src/guide/GuidePage.test.tsx`
- `web/src/guide/content/settings.ts`
- `web/src/i18n/strategy-translations.ts`

### /home/hoang/vl-rel10-dev
- `kernel/mentor/zz_replay_stack_test.go`

### /home/hoang/vl-rel10-fix
- `kernel/mentor/zz_replay_stack_test.go`

### /home/hoang/vl-reread-before
- `kernel/mentor/zz_replay_stack_test.go`

### /home/hoang/vl-room-after
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-room-b
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-room-before
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-room-d
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-trend-a
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-trend-ab
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-trend-b
- `kernel/mentor/zz_replay_room_test.go`

### /home/hoang/vl-trend-base
- `kernel/mentor/zz_replay_room_test.go`

## ASK — unpushed commits not confirmed on origin/dev
- `/home/hoang/nofx-101x2-red1` — unpushed 13 (on-gh 12)
- `/home/hoang/nofx-101x2-red2` — unpushed 13 (on-gh 12)
- `/home/hoang/nofx-101x2-red3` — unpushed 26 (on-gh 24)
- `/home/hoang/nofx-101x2-red4` — unpushed 25 (on-gh 24)
- `/home/hoang/nofx-103b` — unpushed 1 (on-gh 0)
- `/home/hoang/nofx-cto-208` — unpushed 1 (on-gh 0)
- `/home/hoang/nofx-cto-combo` — unpushed 1 (on-gh 0)
- `/home/hoang/nofx-docsmerge` — unpushed 25 (on-gh 0)
- `/home/hoang/nofx-ds103-census` — unpushed 1 (on-gh 0)
- `/home/hoang/nofx-trial` — unpushed 4 (on-gh 0)
- `/home/hoang/vl-108-rel10-check` — unpushed 2 (on-gh 0)
- `/home/hoang/vl-ds106-rel20dry` — unpushed 4 (on-gh 0)
- `/home/hoang/vl-ds108-stopverify` — unpushed 2 (on-gh 0)
- `/home/hoang/vl-orb-course` — unpushed 5 (on-gh 1)
- `/home/hoang/vl-rg564-head` — unpushed 5 (on-gh 1)