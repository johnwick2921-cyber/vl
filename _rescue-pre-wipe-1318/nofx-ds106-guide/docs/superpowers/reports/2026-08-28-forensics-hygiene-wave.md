names rewritten to vl on 2026-09-30 (VL rename)
# FORENSICS-HYGIENE WAVE — STRICT-TRUTH + S-1..S-4 + T2/T4

**2026-08-27, cutover 18:14:40 CT · branch `fix/forensics-hygiene` off dev ·
rev `b38150996225` · boot `🔐 BOOT INTEGRITY OK — rev b38150996225 +dirty ·
built 2026-08-27T23:06:10Z · expected b3815099 · goldens PASS` (PID 3170202,
flat gate: positions `[]` + DB OPEN=0 before kill -9, owner "go").** All times
CT.

## 0. S-4 — merge fix/bar-truth → dev (done first)

`git merge --ff-only fix/bar-truth` on dev → pushed `43bb60cb..8d3fce21`.
`git rev-list dev..405e1323` = 0 → dev now contains every commit the running
rev contains. Branch census: every other branch ahead of dev is report-only
(`docs/*`) or superseded WIP (feat/bar-persistence, feat/volume-levels — their
content shipped via dev merges). No deployed code lives on a side branch.

## 1. STRICT ARBITRATION — **STRICT IS INTACT; the final-verify sweep read the WRONG strategy**

- The hoang trader binds `a5b7662e-7bf7` (`traders.strategy_id`). Fresh raw
  read of its `day_plan` JSON (this run, 17:56 CT):
  `day_plan.plan_mode = "strict"` **and** `sessions[NY/ASIA/LONDON].plan_mode
  = "strict"` ×3.
- The final-verify sweep queried `WHERE is_active=1 LIMIT 1` → strategy
  `4104ca0a` (均衡策略, `plan_mode:"advisory"`) — a strategy **no trader
  binds**. That was the sweep's error; nothing reverted strict.
- Audit trail: `api/strategy.go:412` at 14:44:43 CT — "🔄 Strategy
  a5b7662e-7bf7 saved — removing trader … from memory to reload with the new
  config" (owner's Strategy Studio save; GIN GET /api/strategies +
  preview-prompt POSTs at 14:43-14:44 from 127.0.0.1). The saved config IS
  strict everywhere. **No owner re-click needed; no config written by this
  dispatch.**
- Consequence for the P4 register: strict-era entries still awaited (the
  first strict-gated open_* with a cited scenario — strict became effective
  at/after the owner's saves, the last at 14:44:43 CT).

## 2. S-1 — order_update log flood

`trader/armed_executor.go:322` logged EVERY `order_update` frame at INFO
(25k lines/s during the 13:35-14:05 CT storm → 1.08GB of a 1.48GB flood
hour). Now: per-frame content at **DEBUG, sampled 1-in-500**
(`ARMED_ORDER_UPDATE_LOG_SAMPLE`, T8 pattern) + a **1-line/min INFO summary**
with per-state counts (`📡 armed order_update summary (1-line/min): frames=N
working=x cancelled=y …`). The receive path stays provable.

Second flood class already dead: the 14:05-14:16 `received frame type=bar`
INFO flood came from the PRE-T8 binary (boot 13:50:56, before the 14:29
level-truth cutover); T8 sampling covers `bar_update` since 14:29.

**Retention E-proof (reopen-hour rate):** measured 17:00-18:00 CT on the
current binary — the hour INCLUDING the 30k-frames/min Globex reopen flood —
= **1.09 MB** → projection **81.9 days** at the 2G cap ≥ 7d ✓. (The
13:35-14:35 storm classes are eliminated by this deploy's DEBUG+sampling and
by the already-deployed T8.) Post-cutover re-measurement at the next
08:30/17:00 boundary stays the follow-up gate.

## 3. S-2 — honest counters + the malformed summary line

`ingestDropSummary` (`provider/ninjatrader/bar_persist.go:191`) now uses the
formatted logger and honest labels:
```
bars: ingest drop summary: intrabar_dropped=%d current_dropped=%d historical_dropped=%d peak_depth=%d/%d
(1-line/min; intra-bar drops self-heal on the next tick — closed bars are NEVER dropped on this path)
bars: persist queue summary: queue_drops=%d closes_dropped=%d flushed=%d (closes_dropped must be 0 …)
```
- Ingest evictions are labeled **intrabar** (the channel carries forming
  updates only; closes are re-derived from the cache tail — 0 by
  construction).
- `persistDroppedCloses` counts CLOSED bars lost on persist-queue-full drops
  separately — the number that must stay 0.
- Honest-through-flood proof: awaits tomorrow's 17:00 CT flood (labels are
  live now); today's flood behavior is recorded in the bar-truth report.

## 4. S-3 — INGEST_QUEUE_CAP + peak depth

`INGEST_QUEUE_CAP` env (default **1024**, was fixed 256) +
`sampleIngestDepth` tracks the session-scoped high-water mark (reset at the
17:00 CT CME roll) and the summary prints `peak_depth=N/1024`.

## 5. T2 — stamp regression root-cause + fix

The 2/12 unstamped rows in the newest plan were the **(HTF)-carried rows**
(`Demand·1h (HTF)` 29541.12, `iFVG(bull)·1h (HTF)` 29670.62). Root cause:
the model carries 3dp prices **truncated to 2dp** into the doc while
`CarryMachineGrades`/`StampMachineGrades` key on `math.Round(p*100)/100`
(half-up) — `29541.125 → 29541.12` misses key `29541.13`. Fix: ±0.011
tolerance fallback in both functions (real levels sit ≥0.25 apart — no
collisions). Live-row demonstration (both real rows stamp under the fix) +
unit tests `TestT2StampMachineGradesFromMap` (now 4/4 incl. truncation) and
new `TestT2CarryMachineGradesTolerance` — green. Golden re-check at the next
plan write that carries .125-price rows.

## 6. T4 — consumed-no-touch: growth was a measurement artifact; invariant repaired

- Root cause of the "8→56 growth": **not growth** — `EnsureLevel` upserts
  bump `updated_at` on every planner read, re-dating legacy rows to today.
  By `created_at`: 0 consumed rows with `times_tested=0` were created after
  the level-truth cutover — the fix was working.
- The 56 legacy rows still violated the invariant, so
  `RepairConsumedWithoutTouch` (idempotent) stamps them at boot.
- Post-cutover: `🩹 level-state repair: 56 legacy consumed rows stamped with
  their consuming touch (T4 invariant)` (18:16:40) → live query
  `consumed=1 AND times_tested=0` = **0** ✓. Growth stopped by construction:
  the only consumed writers are `MarkConsumed` (stamps the touch) and
  `DecrementFreshness` (reachable only via `RecordPlay`, which increments
  first).

## Tests

`go test ./...` 27 packages ok · goldens PASS · new T2/T4 tests green ·
`go vet` clean on touched packages.

## Follow-ups (noted, not this wave)

- Boot text "proximity=… retuned 0.3" is a stale S-wave label; live config
  `proximity_filter_atr = 1.5` (matches the ±457pt band = 1.5 × ~305pt
  DailyRangeProxy).
- Re-measure retention at the next reopen boundary; counters through the next
  17:00 flood; strict-gate first cited-scenario quote at the next entry.

Pinned: https://github.com/johnwick2921-cyber/nofx/blob/8b55822fa6e6ba3dd5332e0eedd4a516e6a17571/docs/superpowers/reports/2026-08-28-forensics-hygiene-wave.md
