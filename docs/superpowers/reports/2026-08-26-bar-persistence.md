> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# 2026-08-26 · Bar Persistence — The Unblock (dispatch report)

**PR:** [#77](https://github.com/johnwick2921-cyber/vl/pull/77) · **Branch:** `feat/bar-persistence`
**Deployed revs (cutover sequence):** `3f5c6b24` (step 0, PR #76 base) → `fb066e6b` (bar persistence) → `85f6badb` (live-persist fix)

## 1. Schema

`store/bar_history.go` — additive, idempotent:

```
bars(symbol TEXT, tf TEXT, open_time_ms INTEGER, o REAL, h REAL, l REAL, c REAL, v REAL,
     PRIMARY KEY (symbol, tf, open_time_ms))
```

- Index `idx_bars_sym_tf_time` on `(symbol, tf, open_time_ms DESC)`.
- `INSERT OR IGNORE` batch writes (dedup on restart/backfill — idempotent by construction).
- Gorm sub-store on the root Store (`Store.BarHistory()`), same SQLite file `data/data.db`.

## 2. Writer

- Every closed-bar event the BarCache receives (all 28 subscribed symbol×tf pairs, incl. ES) is fanned out AFTER the cache write, in its own goroutine with panic-recover — a slow or failing DB can never block the drain loop or the socket read loop.
- Forming bars are never written (`ClosedBarsOnly`: open T + tf duration ≤ now).
- Failures log `WARN`, never block the trade loop.

### Live-path bug found during the 30-min proof (fixed in `dfdea28a`)

The first cutover (`fb066e6b`) persisted the boot backfill but **zero live bars** —
the table froze at the replay watermark for 10+ minutes while the chart advanced.
Root cause: NT8's `bar_update` frames carry **only the forming bar**; the just-closed
bar is never re-emitted at the minute boundary, so `ClosedBarsOnly(frame)` filtered
every live frame. Fix: live candidates now come from the **cache tail** (which always
holds the final closed bars — the chart proves it): `ClosedCacheTail(cache.Get, symbol,
tf, now, window=8)`, while historical replays keep persisting from the frame batch.
Test: `provider/ninjatrader/closed_cache_tail_test.go` (`TestClosedCacheTail`).

## 3. Boot backfill

`trader/ninjatrader/bar_persist_wire.go` — at startup, flush every closed bar the
in-memory cache holds (~33 h) with a retry-while-empty loop (the AddOn's
`bars_historical` replay lands a few seconds after restart).

## 4. Retention

`BAR_RETENTION_DAYS` (env, default 90). Prune runs at boot + daily:

```
🧹 bars: pruned 33047 rows older than 2026-05-28
```

## 5. ATR companion

- `structure_json.atr` verified landing every cycle — 3 consecutive decisions:
  `06:52:35 → 15m 31.24 / 1h 76.97 / 5m 18.02`, `06:54:32 → 18.02`, `06:55:42 → 18.62`.
- One-line replay helper added: `kernel.SimpleATR14(highs, lows, closes)` — join
  bars+atr by timestamp via `store.BarHistoryStore.BarsBetween`, recompute the same
  Wilder ATR(14) the live engine uses.

## 6. Boot lines

Step 0 (PR #76 base, `3f5c6b24`):
```
🔐 BOOT INTEGRITY OK — rev 3f5c6b24d154 +dirty · expected 3f5c6b24 · goldens PASS
```
Cutover (`fb066e6b`) and fix (`85f6badb`):
```
🔐 BOOT INTEGRITY OK — rev 85f6badb9d1a +dirty · built 2026-08-26T06:57:07Z · expected 85f6badb · goldens PASS
📦 bars: persisting 28 symbol×tf retention=90d rows=60989 (backfilled 44882)
```
(Spec boot line format `bars: persisting Nsym×Ntf retention=90d rows=X` — matches.)

## 7. Tests

- closed-bar insert ✓ `TestClosedBarsOnly` / `TestClosedCacheTail`
- forming bar never written ✓ (both tests assert forming excluded)
- dedup on restart ✓ `TestBarInsertAndDedup` (`INSERT OR IGNORE`)
- prune ✓ `TestBarPrune` + `TestBarRetentionEnv`
- backfill idempotent ✓ (IGNORE + retry loop; second boot backfilled the same set cleanly)
- loop never blocks on DB error ✓ `TestFanOutBarPersistRecoversPanic` / `...Noop` (injected panic recovered, warn sink fires)

`go test ./...` EXIT=0 at every gate.

## 8. Cutover proof

**Flat-window check before each cutover:** 0 OPEN positions ✓

**Live growth after the fix** (table max advanced minute-by-minute with wall clock):
`06:56 → 06:57 → 06:58 …` — every closed bar lands ~1 s after its boundary.

**30-min window growth** (rows with `open_time_ms` in the last 30 min):
| tf | rows/30min |
|----|-----------|
| 1m | 58 (MNQ+ES, ~2/min) |
| 3m | 18 |
| 5m | 10 |
| 15m | 2 |

≈ 88 rows/30 min ≈ **4,224 rows/day** across TFs.

**3-row live-chart match** — `GET /api/klines?symbol=MNQ&interval=1m&limit=4&exchange=ninjatrader`
(chart = the live NT8 BarCache) vs `SELECT … FROM bars` — **OHLCV identical, all 3 rows**:

| minute (UTC) | chart | table |
|---|---|---|
| 06:57 | O 29252.25 H 29257.00 L 29248.50 C 29255.25 V 716 | identical ✓ |
| 06:58 | O 29255.00 H 29256.50 L 29248.75 C 29251.00 V 435 | identical ✓ |
| 06:59 | O 29250.50 H 29255.75 L 29250.50 C 29251.75 V 333 | identical ✓ |

## 9. Disk math

- Row ≈ 55 B (8+text+5×REAL+index). ~4,224 rows/day ≈ **~230 KB/day**.
- 90-day retention ≈ **~21 MB** — trivial vs the 535 MB DB.
- `bars` table after prune: 34,061 rows (90-day window; the prune correctly removed
  the deep 1w/3d replay history).

## 10. What this unblocks

Level-system deep verification (queued dispatch): Part 1 detector recomputation
from stored bars and Part 4 reaction reality now have a timestamped OHLCV substrate
(1m MNQ back to 2026-05-28, 90d rolling) plus a replay-join ATR helper.

**REFUSAL AUTOPSY (owner scope add 2026-08-26, runs inside the Sep 3 deep
verification):** for every gate refusal since Aug 26 (`sl_too_tight`, HTF veto,
`min_scenario_quality`, stale) replay what the market did next — would the
refused entry have hit its stated TP before its SL? Output table:
`refusals · would-have-won · would-have-lost · Σ hypothetical`, with a
per-gate verdict: **SAVING MONEY** or **COSTING MONEY**.

**B1 RESOLUTION RULE (planner-contract wave 2026-08-26, MANDATORY for all
Sep-3 replay + refusal-autopsy tooling):** every hypothetical stop/target hit
test MUST resolve on **1m bars** — never on the 2-min decision/confirm bar.
The MPM look-ahead trap measured a 73%→50% edge degradation when resolution
was coarsened to the 2-min bar (a TP/SL inside a 2-min bar gets judged by
whichever extreme the aggregator orders first). Fixture:
`scripts/mpm_resolution_fixture.py` proves the difference on a synthetic
series (1m: TP first; 2-min aggregated: SL first).
