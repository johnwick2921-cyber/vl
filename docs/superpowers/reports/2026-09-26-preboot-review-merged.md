# PRE-BOOT SKEPTIC REVIEW — merged range vs live

- **Task:** DS-104, CTO dispatch 2026-09-26 06:38 CT (owner order, all 8 lanes). Read-only.
- **Range:** `04ae1c2f` (live boot-3, RELEASE `6cac1b896bdb`) → `35a53d29` (dev tip) = #246 sys-robustness + #249 leaks-lifecycle + #248 planner-killers + #250 test-srctree-race + #247 FIX-OPS.
  - `git log -1 -- dev`: `35a53d29 Merge pull request #247 from johnwick2921-cyber/fix/ops-observability`
  - `git log -1 -- 04ae1c2f`: live RELEASE `6cac1b896bdb…`, booted 19:33:22 CT 2026-09-25.
- **Method:** per-PR file maps, then interaction reads at the MERGED head (single-PR gates can't see cross-PR composition). Build sanity at the merged head: `go build ./...` **OK**.
- **Verdict: NO P0, NO P1.** One P2 (partial panic net). P3 notes below. Boot can proceed on this tree from this review's perspective.

## Evidence tiers

**[A]** read the exact merged lines · **[B]** inferred from strong evidence · **[C]** speculation.

## P2 — #246's panic net does not cover two goroutines #249 keeps/adds

**[A]** `trader/auto_trader.go:1125,1140` wrap the run-loop beats and
`auto_trader_risk.go:38` wraps the drawdown-monitor beat in `runBeatSafely` →
`recoverPanic` (freeze + P0 alert). But two goroutines in the merged tree have
**no recover at all**:

- `trader/ninjatrader/level_stats_wire.go:70` — the nightly level-stats loop (24h-idling; #249 made it stoppable).
- `trader/picture_htf_broker.go:401` — the order-update consumer (#249's P2-15 restart-safe consumer).

A panic in either still **kills the whole process** (Go runtime behaviour for
unrecovered goroutine panics). The net's own comment scopes its claim to "loop
or monitor" so nothing printed lies — but the merged tree's implied story
("a trader panic can never kill the process") is partial. Recommendation
(post-boot wave): route both bodies through the same recover pattern (freeze
the trader, log + alert, exit the goroutine). Not a boot blocker: the exposure
is no worse than live (live had no net anywhere).

## P3 notes

1. **Panic-beat aftermath noise [A].** `tickStart`/`closedSkip` are set *inside*
   the safe closure (`auto_trader.go:1140-1153`). After a recovered panic
   `tickStart` is the zero `time.Time`, so the very next line logs a bogus
   "⏱ cycle overran the scan interval" WARN; and `emitAlert("P0", …)` fires on
   **every** recurring beat panic (`FreezeTrader` dedupes, the alert does not)
   → P0 alert flood on a hot panic loop.
2. **Freeze is an in-memory latch [A].** `discipline/freeze.go` keeps
   `frozen map[string]freezeInfo`; a process restart silently clears it and
   entries resume on the same state that panicked. The freeze reason says
   "reconcile from NT8, then clear the freeze" — but nothing surfaces a past
   freeze in a boot line after restart. Consider a boot line when a trader
   freezes, and document restart-unfreezes in the P1-F story.
3. **`busyTimeoutDSN` conditional coverage [B].** `sqlitedriver.go` skips DSNs
   containing `_pragma` (or `_busy_timeout`) on the assumption the DSN already
   carries busy_timeout. A DSN with any *other* `_pragma=` param and no
   `busy_timeout` silently gets no guarantee. Production passes a bare path
   (`data/data.db` → `file:<abs>?_busy_timeout=5000`), so today is covered;
   the guard would be tighter checking for `busy_timeout` specifically.
4. **Retention boot line counts 4 tables every boot [A].** `main.go` runs
   `retention.RunDaily` + `BootLine(rc)` at boot; the boot line READS live
   counts via 4 COUNT(*)s **even with every knob OFF** (the live
   `decision_records` is ~905 MB). Knobs default OFF so nothing is deleted —
   this is boot-time work, not a safety issue; the daily ticker repeats it.
5. **`/api/health` 503 shape [A].** Dead DB → 503 but the JSON body still
   carries `revision` (`c.JSON(503, …)`), so cutover scripts that parse the
   body keep working; a script that aborts on the status code before parsing
   refuses cutover exactly when it should (DB down). Note, not a defect.

## Confirmed clean (the checks that came back empty)

- **No new error discards.** `grep '_ ='` over every non-test file changed in
  the range: all hits are pre-existing (`store/armed_orders.go:883/894`,
  `store/log_event.go:110` are **not** new — diff shows no added `_ =` there).
  The one new ignore line (`ch, _ := listen(tcp)`) is a moved pre-existing
  shape, same error class as the line it replaced.
- **No dangling telemetry.** Removed `IncBreakdownGapNoted` /
  `nofx_fill_latency_seconds` leave zero callers; remaining `fill_latency_ms`
  is the DB column/API doc, not the deleted metric.
- **Knob defaults all safe.** `RETENTION_*_DAYS` = 0/OFF · `LOG_RETENTION_DAYS`
  = 0/OFF · `NOFX_BACKUP_RESEARCH` = 0 · `NOFX_BACKUP_MIN_FREE_GB` = 50 ·
  `death_reread_retry_min` = nil → `wake_min_interval_min` (today's value) ·
  busy_timeout 5000 per connection. None changes shipped behaviour unset.
- **Boot lines read live state.** Retention boot line renders resolved knobs +
  live row counts; log-prune line renders the resolved knob + pruned count;
  transport WARN reads the env. WARNs carry trader/account/plan ids (B1).
- **#248 salvage × #247 retention: no interaction [A].** `born_dead_dropped`
  writes liveness events — not among the 4 retention-prunable tables; `plans`
  is pinned protected by `TestRunDailyPrunesOnlyTheFourTables`.
- **#249 stop × #246 freeze: compose correctly [A].** `cancelStopMonitor` is
  idempotent + nil-safe, the grid-init error path cancels, the ordered worker
  drains on Stop, and the frozen admission leg (`entry_admission.go:215`) is
  entry-only — closes/exits stay live while frozen, as designed.
- **#250 is test-only** (+ checklist entry); no product code.
- **Merged head builds clean** (`go build ./...` OK at `35a53d29`).

## What this review did NOT do

- No product-code edits, no DB writes, no deploy, no merge.
- No `-race` re-run (CI 25/25 at merge covers it; timebox).
