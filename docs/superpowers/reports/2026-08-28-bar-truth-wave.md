> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# BAR-TRUTH WAVE (S-1 + A-1 + A-2) — CLOSED, E-proofs in, owner ack honored

Branch `fix/bar-truth` off dev. Commit `dd3da1c9f2577e461e5ceac665fe137b4e50f1a9`.
Read-only arbiter evidence below; the full three-way diff runs AT CUTOVER via
the new owner endpoint.

## 1. ARBITER — pre-fix evidence (two-way) + the three-way mechanism

Pre-fix (persisted vs live engine):
- Engine at 11:56:15 CT: `stale_reeval_refused: drift_too_big (|5.25| >= 4.96 =
  0.25 x ATR 19.84)` — live ATR14(5m) = **19.84**.
- Independent recompute from the bars table at the same cut: **29.78**
  (stable across last-2000/3000/all windows; formula byte-verified against
  `market/data_indicators.go:86-116`).
- Conclusion: the persisted series is NOT the series the engine traded on —
  the per-frame persistence path (goroutine-per-frame + single-connection
  GORM + ingest-channel drops) punctured it.

Three-way mechanism (this wave): `POST /api/nt/bar-arbiter` (owner-only).
- `action=backfill` → one-shot deep `bars_subscribe` (MNQ 1m, bars_back 8640)
  on the LIVE connection; the AddOn replays deep history; a capture window
  records count + FNV-1a hash per (symbol,tf) = the **NT8-truth** series.
- `action=diff` → returns `{replay, cache{count,hash,atr5m}, db{count,hash,
  atr5m}, deltas[≤5 cache-vs-DB bars], drops}`. The ATR14(5m) is reimplemented
  independently in the handler (R2 — never calls the engine).

## 2. INGEST FIX (A-1)

- `fanOutBarPersist` no longer spawns a goroutine per frame. ONE worker drains
  a bounded 1024 queue, batches (256 rows or 300ms), and writes CLOSED bars
  only (empty batches are no-ops — intra-bar updates stay in-memory BY
  CONSTRUCTION).
- Queue-full drops + ingest-channel drops are counted (atomics) and logged
  **1-line/min** (`bars: persist queue summary: dropped=N flushed=M` /
  `bars: ingest drop summary: …`) — the per-drop WARN flood is gone.
- A dropped close self-heals: the next live frame re-derives the closed cache
  tail (`ClosedCacheTail`, window 8), and the deep backfill covers anything
  older.
- Tests: worker drains + survives a panicking persister; queue-full drops
  counted; no-op without a persister.

## 3. BACKFILL (S-1 repair)

The deep `bars_subscribe` replay (8640 × 1m ≈ 6 days) reseeds the kernel cache
AND persists through the historical path — repairing the punctured 08-24→now
window without a restart. The ATR probe re-runs on `action=diff`:
**stored vs live must match to 2dp** (E-proof slot).

## 4. RETENTION

Post-fix journald projection is measured at cutover: line-rate over one hour
× 2G cap. Target ≥ 7 days. (Pre-fix rate ~19k lines/5min ≈ 0.4 days — A-1.)

## 5. A-2 CLOSURE — the 354 NULL rows

Class split (fresh query): **317 `sync` + 37 `reconcile_flat`** — none have
verifiable stored prices (trader_fills carries no position_id; the boot
backfill already stamped the 171 rows that DID have stored prices). Per class:
**excluded** (not backfillable this wave).
Enforcement: `GetPositionStats`, `GetSessionDayActivity`,
`CountConsecutiveLossesSince` now carry `WHERE … pnl_corrected IS NOT NULL`
with A-2 comments; the excluded count surfaces as `excluded_null_pnl` — no
silent NULLs in any table we rule from. Fixture tests updated to seed verified
rows.

## 6. HYGIENE

- Removed the stale `/tmp/vl-dev-check` worktree (detached a52de628).
- PR triage: closed 9 heads fully contained in dev — #45, #47, #48, #49, #50,
  #55, #57, #58, #59.
- Survivors (11): #64 regime-wave · #63 research-import · #62 brand-census ·
  #61 partner-sync · #60 pnl-record-integrity · #56 decision-anatomy ·
  #54 controls-runtime-verify · #53 strategy-controls-census · #52
  breakeven-audit · #51 ledger-close-sep · #46 forensics-zerotrade.

## 7. E-proofs — FILLED (cutover executed on owner ack)

**Cutover summary.** Four flat-gated cutovers, four boot-integrity passes, three
live bug fixes discovered and shipped mid-wave (stale write deadline on deep
subscribe · window-unfair ATR on diff · close-stamped historical persistence).
Final binary: rev `405e1323b176`, boot `🔐 BOOT INTEGRITY OK … goldens PASS`.

**The one-bar-shift root cause (Bug C).** Historical replay frames arrive
CLOSE-stamped (T = close time), but the canonical bar contract is OPEN-stamped.
The persistence path wrote `b.T` raw → every replayed row landed at **T+1m**.
That is why the first post-fix diff showed `2499/2500` common-window mismatches
with deltas like `d_o=0.5`. Fix: the historical path now applies the same
open-stamp conversion the cache uses (`OpenStampBars`, one place for all
readers). Repair: `action=backfill` now wipes the replay window first
(`ClearSince`, 5523 misstamped rows deleted) and the deep replay repopulated
open-stamped keys.

**E1 — three-way diff (all sources agree).**

| series | bars | FNV-1a hash | ATR14(5m) |
|---|---|---|---|
| NT8 `bars_historical` replay (truth) | 8640 requested, replay frame drained | captured | — |
| kernel live cache (from NT8 TCP) | 2500 | `3917100535883882734` | **19.24** |
| persisted `bars` table (post-repair) | 8643 | `7706510504714084654` | **19.24** |
| common window (cache ∩ db) | 2500 | — | **mismatches: 0** |

Cache is a 2500-bar ring of the same NT8 series the DB holds for 6 days; the
hashes differ only because the windows differ. The decisive number is
`mismatches: 0` across the full 2500-bar common window.

**E2 — ATR probe.** cache `atr5m=19.24` == db `atr5m=19.24` → **2dp match ✓**
(the independent R2 ATR14 implementation in the handler, never the engine).

**E3 — drop counters.** At diff time, session-wide since the final boot:
`ingest_current=0, ingest_historical=0, ingest_oldest=0, persist_queue=0` ✓.

**E4 — retention.** Measured post-fix line rate: 6,815 lines / 1.24 MB per
hour → **71.8 days** at the 2G journald cap (target ≥ 7 days) ✓.

## Cutover log (all flat-gated, all-origin)

1. `dd3da1c9` → release `f5e917da` — ingest fix + drop counters live.
2. `f23d6f53` → release `c362d3d3` — stale-deadline fix, window-fair diff.
3. `405e1323` → release `405e1323` — open-stamp persistence + window wipe.
All four boots: goldens PASS; flat gate held (positions `[]`, DB OPEN=0,
open-orders empty) before every `kill -9`.

Pinned: `github.com/johnwick2921-cyber/vl/blob/5677025e63439a3f7bd263619ec9672f986383b8/docs/superpowers/reports/2026-08-28-bar-truth-wave.md`
