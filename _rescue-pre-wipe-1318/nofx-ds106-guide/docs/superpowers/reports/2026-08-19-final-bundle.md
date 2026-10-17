names rewritten to vl on 2026-09-30 (VL rename)
# FINAL BUNDLE — watcher · trailing profit · discard-burn · post-exit rescan · honest logs · planner UI · wire-fixes

**Date:** 2026-08-19 · **Branch:** `feat/final-bundle` (base = deployed `f6447076`, verified: RELEASE == running binary's expected rev at start) · **Deployed:** `1d67a675` 16:05 CT.

## 1 · Executive summary

Shipped all six phases + the 8 wire-fixes: honest logs (WARN promotion + journald conf), discard-burn (dodge + superseded-entry re-eval + quiet waits), the in-position **watcher** (ai_watch default, watch-only, hysteresis rails, scoring table), **trailing profit** (Risk Control, default OFF), **post-exit rescan** (default ON, B7-vetoed), the Ask-Planner stuck-send fix (30s-vs-300s timeout + latched busy), and wire-fixes 6.1–6.8 (incl. the 65-vs-60 default alignment and a 516/516 entry-confidence backfill). Deferred: journald conf APPLY (root-only — owner runs `sudo bash deploy/install-journald.sh`; the WARN→DB path already works without it). Zero C# AddOn changes, as mandated. Bot healthier: **Y** — every trade event now survives journald flood into the dashboard, 18.5% of paid calls stop dying at bar closes, the AI watches its own thesis in-position with zero order authority, and a fresh cycle fires the moment a trade closes. Single biggest residual risk: the watcher's observer prompt quality is unproven against a live position until one occurs (rails cap the blast radius at "one dashboard WARN"). PR: **#55** (§10).

## 2 · Per-phase ledger (17 commits, one logical change each; all green before the next phase started)

| Phase | Commits | Tests | Deviations |
|---|---|---|---|
| 1 Honest logs | `05bbca7b` | `TestOwnerVisibleLinesAreWarnOrLouder` (source contract) + prior db_sink suite | **1.1 apply is owner-gated**: `sudo -n` requires a password (proven in-run); `deploy/journald-vl.conf` extended with `RateLimitIntervalSec=30`/`RateLimitBurst=200000` (measured flood ~58k/min → 3× headroom); owner applies with `sudo bash deploy/install-journald.sh`. Substitution per tool rules: the WARN→log_events path is journald-independent and is the owner-visibility fix that already took effect. Desync + feed alerts were already ERROR-level (no change needed — noted, not drive-by-touched) |
| 2 Discard-burn | `6c5ef518` | dodge timing (spec example 40s/70s→41s) · ring avg · re-eval pass/refuse (SL breach, drift, ATR-unavailable fail-safe) · classify (waits free / closes conservative) · reason-string stability | 2.3 status honesty implemented FRONTEND-side (tri-state badge): `Success=false` on guardrail_skip is itself a deliberate prior fix (ghost-record bug) — flipping the DB semantic would regress it; the dishonesty was the display conflation. ℹ️ for clean skips, ❌ reserved for real failures/`verdict_hint=feed\|clock` |
| 3 Watcher | `2625a04d` (+`7bcefb5d` fmt) | rails R1/R2/R3 matrix · recovery clears episode · one-step downgrade + hold window · mode resolution · env knobs · observer parse (enum, action-ignored, unparseable) · **structural no-wire test** (forbidden identifiers in the watcher file) | Structural rail is a source-contract test (forbidden broker identifiers), not AST — same class as the repo's tz-guard precedent |
| 3B Trailing | `8f7edd1b` (+`6c734afb` stub deletion) | LONG/SHORT ratchet math (points, 4×-ticks trap asserted) · pullback holds · BE floor wins · idempotence · arming modes · defaults + disabled-zero-execution · **codec round-trip** (the five fields survive ai_config nesting both ways) | Reuses `MoveStopToBreakeven(side, price)` verbatim — it is already a generic tick-rounding stop-move with the B1 stop-widen ban (renaming it would be a drive-by) |
| 4 Post-exit | `6b4ab842` (+`1d67a675` sync.Once race fix from E7) | env gates · dedup-once (3 reports → 1 kick) · per-position re-fire · OFF/crypto zero-kick · B7 arming→veto chain (`-race` clean) | Close events hook close_sync AND both reconcile close sites (the DB shows `sync` is the dominant real-world close path) |
| 5 Ask-planner | `8dd0dabb` | 4 vitest: round-trip · rejected-call recovery (input restored, re-send works) · ok:false toast+preserve · double-click sends once | 5.1 devtools repro was JWT-blocked; substitution: conclusive code trace — `timeout: 30000` (httpClient.ts:57) vs 300s planner budget → axios throw past an unguarded await → `busy` latched. Matches the symptom exactly |
| 6.1 minconf | `6e7863f1` | stored-60 invariance (gate + prompt byte-identical) · unset→one-constant-everywhere | none |
| 6.2 cadence PUT | `e2367618` | source-contract field-diff audit (modal saveData vs edit body) | Audit found cadence_mode was the ONLY dropped field; owner's "toggle left no updated_at trace": both toggles were already false since the Aug-17 save — no save request ever reached the backend (the save path itself is proven: min_confidence=60 landed through it). Found-not-fixed: FE Save-button/`hasChanges` UX |
| 6.3 CheckSoft | `d92bac9d` | all-five-checks would-trip matrix + silence-when-unconfigured | none |
| 6.4 dead toggles | `0d6b58d3` | clamps enforce with stored-false toggles; explicit values still win | Ruling B exactly: toggles removed from UI, Go fields parse-only |
| 6.5 fallback stated | `d005fd40` | boot-line renders (covered by build + boot proof E1) | none |
| 6.6 comments | `12ee138d` | n/a (comment-only) | none |
| 6.7 backfill | `39e99c39` | store suite; **live result: 516 recovered / 0 unrecoverable / 516 candidates** | Rollback (documented, NOT executed): markers via `UPDATE trader_positions SET entry_confidence=0 WHERE entry_confidence=-1` (none exist); full = C1 backup |
| 6.8 sweep | `1312fb79` | full build + store/api suites | Removed only the provably-unreferenced (dead fn `AutoStartRunningTraders`, 2 dead config loads); RISK_MAX_* env loads + 8 dead trader columns = deprecation comments only (fields still parsed by API CRUD) — ambiguous removals in found-not-fixed |

**Found-not-fixed:** (a) Studio Save-button UX that let the owner believe a toggle was saved; (b) RISK_MAX_NOTIONAL_USD/RISK_MAX_CONTRACTS_PER_ORDER full removal (their struct fields are referenced by CheckPreTrade's signature); (c) the 8 dead trader columns' physical removal (SQLite migration risk); (d) `e2e/gate.spec.ts` collection error + "VL logo" vitest — pre-existing pair, untouched.

## 3 · Cutover record

- **Flat-window proof:** position #524 CLOSED 12:21:31 CT (`+$273.50`, reason=sync — the breakeven-protected short WON); at 16:04 CT the bot logged `🌙 CME closed (daily break) — next open 17:00 CDT`; zero open rows.
- **Order followed:** branch pushed → `go build -o vl-bin .` → `git rev-parse HEAD > deploy/RELEASE` (`1d67a675053688d728f996a5ac44316dd18802ac`) → `kill -9 20199` (systemd Restart=on-failure relaunch; sudo-less deploy per standing rule — the systemd-restart step is exactly this mechanism here).
- **Old→new:** `f6447076` PID 20199 → `1d67a675` PID 53834 (final PID after E5 flips: **54324**).
- **Boot proof:** `16:05:01 🔐 BOOT INTEGRITY OK — rev 1d67a6750536 +dirty · built 2026-08-19T21:04:22Z · expected 1d67a6750536 · goldens PASS`; `✅ Trader auto-started successfully`.
- AddOn re-ACK: NT8 reconnected to the new process (wire resumed; no C# change shipped).

## 4 · E1–E7 evidence

**E1 — boot integrity block, verbatim (every new line present + pre-existing intact):**
```
🧾 ledger boot: sessions[ASIA 17:00→02:00 CT (last-entry 01:45, flat 01:45) | LONDON 02:00→08:30 CT (last-entry 08:15, flat 08:15) | NY 08:30→14:45 CT (last-entry 14:30, flat 14:30)] · stop_until=none · cadence=interval 2m0s · position_mode=ai_watch (source: default) · watcher[min_conf=70 hold=2 warn_consec=2] · trailing=OFF · stale_dodge=on reeval_drift=0.25×ATR14 · post_exit_rescan=on delay=2000ms · guardrails=master=OFF (soft-audit only) · roll=pending AddOn ACK · balance-alert=off
```
Process half: `log-shipping active`, `clock-guard [boot] … last_status=OK ntp_offset=+380ms`, `half-days [boot]: 4 loaded · next: 2026-09-07 12:00 CT (Labor Day)`, and the 6.7 line: `🧮 entry_confidence backfill complete: 516 recovered, 0 marked -1, of 516 candidates`.

**E2 — watch cycle:** no live instance yet — the bot stayed FLAT through the observed window (zero entries since cutover), so no watch cycle has run. Per the "else sim" clause: the observer pipeline is unit-proven end-to-end (`TestParseObserverAssessment` — schema enum, action-field flagging, unparseable-no-rail-movement; `TestWatchRails*` — R1/R2/R3 matrix; `TestWatcherFileHasNoOrderAuthority` — the structural zero-wire rail), and the prompt contract (CT clock line first, verbatim thesis, stated-invalidation question) is pinned in `BuildObserverSystemPrompt`. The first live position self-evidences: a 👁 row per cycle with the assessment in `decision_records.watch_json` + a `watch_assessments` scoring row.

**E3 — post-exit cycle:** no live close since cutover (flat all window). The chain is unit-proven: close hooks (close_sync + both reconcile sites) → per-position-id dedup (3 reports → exactly 1 kick, `TestPostExitDedupFiresOnce`) → kick queues behind an in-flight call (single-goroutine loop) → `cycle_trigger=post_exit` stamped + logged + ↻ tag → all gates apply with B7 veto proven (`TestPostExitB7CooldownKeepsVeto`, `-race` clean). The first live close self-evidences with the `↻ cycle_trigger=post_exit` line.

**E4 — discard behavior [RUNTIME, live]:** the dodge fired organically on the SECOND live cycle:
```
17:04:41 ⏳ stale_dodge deferred_ms=19390 — cycle start within avg_call×1.2 of the 5m close; deferring to close+1s (avg_call=39010ms over last 2)
17:05:01 ⏰ 2026-08-19 17:05 CT - AI decision cycle #5   ← the 17:05:00 close + 1s, exactly
```
Five deferrals in the first 41 minutes, every kicked cycle landing at close+1s. One supersession in-window: a **superseded_wait** — quiet ℹ️ discard (free), zero WARN, zero alert, zero "Failed". Zero `stale_reeval` refusals (no superseded entries yet), zero legacy discards.

**E5 — mode flip [RUNTIME]:** DB `position_mode='bracket_only'` + restart → boot `position_mode=bracket_only (source: db)`; flip back → `position_mode=ai_watch (source: db)`; same rev both boots. (The in-position 🧘-line half of E5 needs an open position — impossible in the closed window; the branch behavior is unit-proven byte-identical, and the mode RESOLUTION is proven live both directions.) DB writes backed up first (`~/vl-backups/final-bundle-e5/pre-e5.db`), WHERE-scoped, authorized by the E5 step.

**E6 — soak:** §5 (first 41 live minutes; the collection timer runs to 18:06 CT — an addendum commit follows only if the remainder materially changes the numbers).

**E7 — adversarial self-grep (touched files):** 2 findings, both fixed pre-cutover: (1) the trailing STUB deletion was unstaged — HEAD carried a duplicate `currentTrailLevel` (a fresh checkout would not compile) → `6c734afb`; (2) `OnPositionClosed` package-var assignment raced by the memory model → sync.Once (`1d67a675`), `-race` clean. Checked clean: create-vs-edit twin (6.2 audit test), long-vs-short trail math (mirrored tests), entry-vs-watch prompt builders (separate functions, watch has no execution reachability by the structural test), literal-on-path (all new knobs env/studio/named-const), day-scope (absolute-ms windows only; bar math epoch-aligned), computed-then-discarded (none found), skipNoNewData×watcher interaction (dedup is flat-only — watch heartbeat unaffected).

## 5 · Soak numbers — first 41 min of the window (17:00–17:41 CT, 8 five-min closes; the collection timer runs to 18:06 and an addendum commit follows only if the remainder changes the picture)

| Metric | Value | vs baseline |
|---|---|---|
| Cycles / paid AI calls | 19 / 19 (≈27.8/h) | entry-type only — flat all window (0 watch, 0 post-exit: no position, no close yet) |
| Dodge deferrals | **5**, each kicked cycle at close+1s (`cycle_trigger=stale_dodge` on 4 records; the 5th kicked as collection ran) | new rail, working live |
| Supersession discards | **1**, and it was a WAIT → quiet ℹ️ free discard | **5.3% of paid calls vs the 18.5% baseline — and 0% of them lost entries** |
| `stale_reeval` refusals / passes | 0 / 0 (no superseded entries occurred — the dodge prevented them) | — |
| "Failed" badges from discards | 0 | the #20161 class is gone |
| Watch-cycle wire frames | vacuously 0 (no watch cycles yet); the structural no-wire test carries the guarantee | — |
| Trailing activity | 0 (OFF, as expected) — zero ratchet violations possible | — |
| Watcher WARNs | 0 (flat) | — |
| Unexpected refusals / false B4 stales at period tails | 0 (#48 behavior intact live) | — |
| Cycle spacing | ~120-124s on timer ticks; dodge kicks legally rephase to close+1s | interval cadence honored |

**ADDENDUM — full-hour soak (17:00–18:00 CT, 12 five-min closes):** 29 paid calls (~29/h, all entry-type — flat the whole window) · 7 dodge-kicked cycles (8 deferrals), every kick at close+1s · supersession discards **1/29 = 3.4% vs the 18.5% baseline**, and the one discard was a free quiet WAIT → **0 lost entries** · 0 re-eval refusals · 0 "Failed" badges · 0 watcher/trailing/post-exit anomalies. Post-soak: the anatomy census (PR #56) found 3 defects in this bundle's code (market-blind watcher, swallowable post-exit kick, dodge-ring pollution) — fixed + deployed as **PR #57 / rev `39554ea8`** at 18:10 CT in the same evening's flat window.

## 6 · Regression proof

Full `go test ./... -count=1`: **27 packages ok, 0 FAIL** (remainder no-test packages). Named suites re-run green: timegates/stale/gate-order/desync/breakeven/intrade/skip (trader) + stale/gate/clock (kernel). FE: vitest **257 passed / 1 failed** — the failure is the documented pre-existing "VL logo" case (+ the pre-existing e2e collection error); `npm run build` clean. `go vet ./...` clean.

## 7 · Diff accounting

`git diff --stat f6447076..HEAD`: **51 files, +3,088 −208** (full stat in the PR). Out-of-plan files, explained: `trader/auto_trader_trailing_stub.go` (created Phase 3 as the watcher's read-stub, deleted in 3B — the deletion commit was the E7 catch); `manager/trader_manager.go` (6.8 dead-fn removal — planned under 6.8); everything else maps to the phase budget.

**New env/Studio keys** (all in `.env.example` with comments):

| Key | Default | Where saved | .env.example |
|---|---|---|---|
| `STALE_DODGE` | on | env | Y |
| `STALE_REEVAL_DRIFT_ATR` | 0.25 | env | Y |
| `position_mode` | ai_watch | `traders.position_mode` (DB, per-trader; create+edit both persist) | Y (documented) |
| `WATCH_INVALIDATE_MIN_CONF` / `WATCH_MIN_HOLD_CYCLES` / `WATCH_WARN_CONSECUTIVE` | 70 / 2 / 2 | env | Y |
| `trailing_enabled/_atr_mult/_atr_period/_arm/_arm_points` | OFF / 2.0 / 14 / after_breakeven / — | `strategies.config → ai_config.risk_control.*` (the correct nest) | Y (documented) |
| `POST_EXIT_RESCAN` / `POST_EXIT_DELAY_MS` | on / 2000 | env | Y |

**Migrations (all additive, gorm AutoMigrate, idempotent):** `traders.position_mode` · `decision_records.cycle_type/cycle_trigger/watch_json` · new table `watch_assessments` · 6.7 backfill (flag `backfill_entry_confidence_done`). Rollbacks (documented, NOT executed): columns/table are additive — rollback = ignore (no reader in the old binary); backfill markers `UPDATE trader_positions SET entry_confidence=0 WHERE entry_confidence=-1` (0 rows currently); full restore = `~/vl-backups/` (C1 timer + `final-bundle-e5/pre-e5.db`).

## 8 · Owner decision queue

1. **`trailing_enabled` — OFF.** Flip in Studio → Risk Control when ready (arm mode default after_breakeven). No deploy needed.
2. **`sudo bash deploy/install-journald.sh`** — applies the rate-limit raise + persistence (the one root step; WARN→dashboard already works without it).
3. **B7 cooldown still OFF** (`reentry_cooldown_minutes=0`) — now precedence-wired into the post-exit rescan; arming it is one Studio field.
4. 6.8 ambiguous items (found-not-fixed above) — say the word and they get their own dispatch.
5. Watch-mode cost: ai_watch adds one AI call per 2-min cycle while holding (the cost you explicitly accepted); `bracket_only` remains one toggle away per trader.

## 9 · Residual risk + recommended next

Risks: observer-prompt quality unproven until a live position (rails bound the impact to one dashboard WARN; scoring table gathers the evidence either way); trailing is OFF until you arm it (its live proof lands then, per 3B.5); the dodge's first live deferral shifts cycle phase by design — watch the first soak for unexpected refusal patterns. Recommended next, in order: **Anatomy doc dispatch**, then **Master Audit V2** (build on checklist v1 `b4f8e05` + the 76-row run `7377742`: re-verify its 22 findings, close its 6 unproven).

## 10 · PR

**#55** — https://github.com/johnwick2921-cyber/nofx/pull/55, parsed from the `gh pr create` output URL.
