names rewritten to vl on 2026-09-30 (VL rename)
# 2026-09-02 — Bar-source audit: which consumers are starved of history the system already has

**Dispatch:** BAR-SOURCE AUDIT — READ-ONLY · no lock · no engine code · no config. Owner hoang, 2026-09-02.
**Live rev audited:** `0465a10bfa4b865a8406a1d684501ec4673febc7` (`deploy/RELEASE:1` = running binary [A]).
**Audit tree:** worktree `~/vl-barsource`, branch `docs/bar-source-audit-0902`, pinned at 0465a10b. Main tree untouched, porcelain-clean, no lock taken.
**Evidence tiers:** [A] directly verified (ran it / read the exact line / queried the DB) · [B] inferred · [C] speculation.
**Evidence classes:** [RUNTIME] journal · [DB] read-only `data/data.db` · [CODE] file:line · [CONFIG] `.env` / resolver.

---

## Verdict (one paragraph)

**Exactly ONE consumer is genuinely starved: the weekly reader (C1), and with it the whole weekly chip including IPDA.** It computes weekly OHLC / PWH / PWL / NWOG / IPDA / 3-week structure from the SQLite `bars` table (B1), which holds 1m-only data since 2026-08-19 (MNQ) / 08-24 (ES) — giving **2 completed weeks** today against a guard that demands ≥4 → `ThinHistory=true` → the "WEEKLY thin · low" card. Meanwhile the NT8 BarCache (B2) already holds native `1w` and `1d` series (2000 bars requested per TF on subscribe, provider-capped). Every other consumer already reads B2 (native per-TF bars) or needs 1m intrabar resolution and is not starved. **The fix is one resolver, not per-consumer surgery** — a single `CompletedBars(tf, from, to)` that answers NT8-native-first, own-1m fallback, returning the source — plus persistence of higher-TF bars so the answer survives restart, because today only 1m is ever persisted and the whole pantry is memory-only.

---

## Section B — the sources

### B1 — own-persisted bars table (`bars`, SQLite)

Schema [A][DB][CODE `store/bar_history.go:26-35`]: `symbol, tf, open_time_ms (PK, bar OPEN time epoch-ms UTC), o, h, l, c, v`. Table name `bars`.

Per-TF census [A][DB], query: `SELECT tf, symbol, COUNT(*), MIN(open_time_ms), MAX(open_time_ms) FROM bars GROUP BY tf, symbol;`

| tf | symbol | n | earliest (UTC) | latest (UTC) |
|---|---|---|---|---|
| 1m | MNQ | 13,729 | **2026-08-19 15:00** | 2026-09-02 13:45 |
| 1m | ES | 9,329 | **2026-08-24 19:21** | 2026-09-02 13:45 |
| (total) | | 23,058 | | |

Only `1m` is ever stored — by design [A][CODE `store/bar_history.go:94-96`]: *"DELETE tf != '1m' rows — 5m/15m are DERIVED ON READ from 1m (the stored NT8 aggregates were inconsistent with their 1m constituents)"* and `:124-126` *"Only tf='1m' rows are stored: 5m/15m aggregates are DERIVED ON READ from 1m."*

**Retention knob (resolved):** `BAR_RETENTION_DAYS` — unset in `.env` → resolver default 90 [A][CODE `store/bar_history.go:37-45` `BarRetentionDays()`]. Live boot line [A][RUNTIME 09-01 00:43:45]: `📦 bars: persisting 2 symbol×tf retention=90d rows=19332 (backfilled 34893)` — note **2 symbol×tf pairs persisted vs 34,893 bars backfilled from cache**: the other 26 (symbol×tf) pairs live only in memory (see B2).

A one-shot safety copy `bars_pre_dedupe_2026-08-27` exists (pre-dedupe snapshot, `store/bar_history.go:77-84`) [A][DB].

### B2 — NT8 `bars_historical` path (the pantry)

**Subscription config (resolved, no env knob):** [A][CODE `provider/ninjatrader/tcp_server.go:424-438`]
```go
defaultAutoBarsTimeframes = []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
// 2000 (was 500): ... 2000 1m-bars span ~33h of trading, enough for a fresh BarsRequest
// to pull the gap window from the provider's (Tradovate) historical server ... Providers cap
// deep requests on coarse timeframes, so this is safe across the 14-tf set.
defaultAutoBarsBack = 2000
```
The 14-TF list and 2000-back are constants; no env override exists (`provider/ninjatrader` has only `INGEST_QUEUE_CAP`, `NT_TCP_LISTEN_ADDR`, `BAR_UPDATE_LOG_SAMPLE` getenv sites) [A][CODE]. C# twin [A][CODE `ninjascript/VLBarsSubscriptionManager.cs:79-90`]: `private const int DEFAULT_BARS_BACK = 2000;` with the same "coarse timeframes are capped by the provider" note; every BarsRequest uses the ETH template (`"CME US Index Futures ETH"`, `:97-101`).

**Storage:** memory-only ring buffer — [A][CODE `provider/ninjatrader/bar_cache.go:24-27`] `DefaultBarCacheMaxBars = 2500` per (symbol, timeframe) key. The bridge feeds every consumer from this cache: [A][CODE `trader/ninjatrader/bars_market_bridge.go:19-26`] `market.FuturesBarsProvider = func(symbol, timeframe, count) { bars := server.BarCache().Get(symbol, timeframe) ... }`.

**How far back as received:** requested depth = 2000 bars per TF on every (re)subscribe; NT8 caps coarse TFs at the provider. The received history is **not persisted for tf≠1m and cannot be measured from disk** — the cache is memory-only [A][CODE]. Lower-bound proof that deep native series are actually held: the planner regime reads `"1d", 300` [A][CODE `trader/auto_trader_planner.go:2054-2055`] and the executor market block renders `1d` in `📊 Strategy timeframes: [1h 4h 1d 15m 3m 5m]` at every boot [A][RUNTIME 09-02 08:42:30] — i.e. ≥20 native daily bars exist in the cache, far beyond the 2 weeks B1 can build.

**Restart behavior:** the ring is wiped on Go restart; the C# AddOn re-emits `bars_historical` seconds later (re-reads NT8's DB over the full 2000-back — gap self-heal, [A][CODE `VLBarsSubscriptionManager.cs:307-324`]). Only closed **1m** bars survive via the persister [A][CODE `trader/ninjatrader/bar_persist_wire.go:29-46`] → the `bars` table. Higher TFs are re-fetched into memory but never written to disk.

### B3 — the 14-TF candle tables (executor + planner prompts)

| Table | Source | TFs | Depth |
|---|---|---|---|
| Executor market block (`=== 1m Timeframe (oldest → latest) ===` ...) | B2 native per TF via `market.GetWithTimeframes` → `FuturesBarsProvider(symbol, tf, fetchDepth)` per TF [A][CODE `market/data.go:244-254`] | strategy `SelectedTimeframes` = `[1h 4h 1d 15m 3m 5m]` [A][RUNTIME 09-02 08:42:30], render order covers all 14 [A][CODE `kernel/engine_prompt.go:713-717`] | `klineCount` 20 per TF |
| Planner candle tables (W2b "PLANNER EYES") | B2 **1m × 12000**, aggregated in-kernel [A][CODE `trader/auto_trader_planner.go:2157-2162` + `kernel/planner_prompt.go:297-311`] | 12×15m, 12×1h, 8×4h (all `AggregateBars(bars1m, …)`), **8× daily = `DailySessionBars(bars1m)` (RTH session-day aggregation)** | 1m×12000 (~8 sessions) |
| Planner regime | B2 native [A][CODE `auto_trader_planner.go:2052-2058`] | `1d`×300, `1h`×300, `5m`×300, `5m`×3000 | 300/3000 |
| Chart `/api/klines` + `/api/klines/svp` | B2 BarCache [A][CODE `api/handler_klines.go:330-365`] | any subscribed TF | `limit` |

**Hazard inside the planner prompt itself:** "daily" has two definitions — NT8-native calendar-day `1d` (regime block) vs `DailySessionBars` RTH session-day aggregation (candle tables) [A][CODE `kernel/weekly_bias.go:240` vs `market/data.go:254`]. One prompt can carry both.

### B4 — third sources feeding futures bars

**None.** CoinAnk is reached only by the non-futures branch (`isFutures → FuturesBarsProvider` early path, [A][CODE `market/data.go:234-254`]); Databento exists only in the deprecated `cmd/nq_smoke` offline smoke (`db.GetOHLCV("NQ.c.0", "1m", …)` [A][CODE `cmd/nq_smoke/main.go:89`], `DATABENTO_API_KEY` optional); no IQFeed remnants in any `.go` file [A][grep].

### B5 — single source of truth?

**No.** There is no `CompletedBar(tf, t)` / `CompletedBars(tf, from, to)` function anywhere. Two unconnected source layers exist:

- B2 layer: `market.FuturesBarsProvider(symbol, tf, count)` — 32 call sites [A][census §12].
- B1 layer: `store.BarHistory().BarsBetween(...)` — 10 call sites (incl. the writer) [A][census §12].

And four independent 1m→TF aggregation helpers each roll their own bucketing: `AggregateBars` [A][CODE `kernel/fvg_entry.go:290`], `StructureAggregateToMinutes` [A][CODE `kernel/scenario_facts.go:142`], `aggregateBars` [A][CODE `kernel/levels_swing.go:186`], `DailySessionBars` [A][CODE `kernel/weekly_bias.go:240`]. No caller can answer "where did this daily bar come from" — the resolver's job.

---

## Section C — every consumer, one table

| # | Consumer | Computes | Source | TFs / depth | Needs 1m? | Starved? |
|---|---|---|---|---|---|---|
| C1 | **Weekly reader** (`trader/auto_trader_weekly.go`) | weekly OHLC, PWH/PWL/PWC, NWOG, IPDA, 3-week structure, depth guard | **B1** [A][CODE `:74` `at.store.BarHistory().BarsBetween(at.futuresSymbol(), "1m", 0, now)`] | all stored 1m | aggregates up | **STARVED — the only one** |
| C2 | Daily/session levels (`kernel/levels_multiday.go:38` `ExtractMultiDayLevels`) | PDH/PDL/PDC, RTH-H/L, AS/LDN/ON-H/L, PWH/PWL, PMH/PML | B2 [A][CODE `kernel/engine_analysis.go:294` 1m×2000; `kernel/levels_assemble.go:74`] | 1m×2000 (guards: 900/4320/10080 bars, `:224-231`) | yes | No (33h ≥ its 7d max guard is marginal for PM; B2 1m ring only reaches ~33h — PM/PW guards can under-cover) |
| C3 | VWAP / σ bands / eVWAP / POC / VAH / VAL (`kernel/levels_volume.go`) | session VWAP±1σ/±2σ, 15:00 eVWAP anchor, pdPOC profile, SVP | B2 1m [A][CODE `levels_assemble.go:78`; `engine_analysis.go:336` SVP] | 1m×2000 | yes | No (session-scoped) |
| C4 | ATR5m / ATR14 / Wilder variant (`kernel/structure.go:122`, `kernel/plan_confirm.go:137`, `market/data_klines.go:292`) | min-SL gate, MSS displacement, FVG min-disp, trail ATR, price-sanity | B2 — native 5m (`auto_trader_trailing.go:168`) or 1m→5m buckets (`StaleConfirmATR5m`); per-TF ATR14 in market block | 1m×2000 or 5m native | yes (bucketing path) | No |
| C5 | Swing detector BOS/CHoCH/MSS/SWEEP, SWG-H/L (`kernel/structure.go`, `kernel/levels_swing.go`, `kernel/mss.go`) | StructureTFs = 5m/15m/1h from 1m snapshot; 1m-MSS confirm | B2 1m [A][CODE `auto_trader_loop.go:404-405` `StructureSnapshot(bars1m,…)`] | 1m×2000 aggregated | yes | No |
| C6 | FVG / order-block / iFVG / S-D zones (`kernel/levels_zones.go`) | 3-candle gaps, OBs, HTF 1h/4h zones | B2 [A][CODE `levels_assemble.go:73-77` (1m); `engine_analysis.go:388-390` native `1h`,`4h`] | 1m×2000 + 1h/4h native | yes (1m detectors) | No |
| C7 | Level grader inputs (`kernel/levels_score.go`) | type-evidence × freshness × confluence × HTF | B2 1m for touch-count/levels [A][CODE `levels_role.go:132`]; freshness from `level_state` DB | 1m×2000 | yes | No |
| C8 | Touch telemetry + `level_stats` writer | live `touch_episodes` (B2 1m); nightly level_stats outcomes (**B1**) [A][CODE `level_stats_wire.go:110` `BarsBetween("MNQ","1m",dayStart,dayEnd)`] | mixed B2 (live) / B1 (nightly) | B1 needs the prior session-day 1m | yes | No for today; **borderline for ES** (9 days vs 2-week verdict window) |
| C9 | Regime (trend/range, VIX-less) (`kernel/regime.go:50`) | EMA200 daily, EMA50 1h, dATR, RV from 5m | B2 native [A][CODE `auto_trader_planner.go:2052-2058`] | 1d×300, 1h×300, 5m×300/3000 | no | No — already uses the pantry correctly |
| C10 | E8 counterfactual replay (`kernel/shadow_ab.go:51`) | 4 counterfactual fills per scenario by 1m replay from plan birth | B2 1m×2000 [A][CODE `armed_executor.go:147,553`] | 1m window | yes | No |
| C11 | 1C / detector offline scripts | `cmd/levelstats-backfill/main.go:89` (**B1**), `scripts/leveltruth_missed_turns.py:31` (**B1** direct sqlite), `scripts/stale_met_replay.py:41` (**B1**) | B1 | full 1m span | yes | No (1m needs; B1 span ≥ their windows) |
| C12 | Planner candle tables + executor 14-TF tables | see B3 | B2 | see B3 | candle tables aggregate 1m; executor native | No |
| C13 | Everything else (grep census, 32 B2 + 10 B1 sites) | feed-age, clock drift, price armor, plan card facts, chart SSE, signal clock, nPOC splice (B1+B2), weekly DOA/watch/shadow (B2) | see census §12 | — | — | Only C1's family starved |

**C1 detail — why "thin · low":** the reader loads *all stored 1m bars* [A][CODE `weeklyBars1m` `:74`] and `kernel.ComputeWeeklyFacts` derives everything from that one slice [A][CODE `kernel/weekly_prompt.go:44-53`]. `ThinHistory = CompletedWeekCount(bars1m, now) < 4` [A][CODE `:50`]. `CompletedWeekCandles` buckets by `weekStartMonday` and excludes a week only by TIME (Friday 16:00 CT passed), NOT by data completeness [A][CODE `kernel/weekly_bias.go:93-118` + `weekCompletedAt`]. DB week buckets today [A][DB]:
- week 2026-08-17: 3,243 rows, **first bar Tue 08-19 10:00 CT** (partial — Monday missing, Tuesday truncated)
- week 2026-08-24: 6,900 rows (complete)
- week 2026-08-31: in progress → excluded
→ **CompletedWeekCount = 2** (< 4) → thin/low confirmed [A]. And the 08-17 week candle is computed from a truncated tape — its "weekly open" is the Tuesday 10:00 CT bar, not the true Monday/Sunday open.

---

## Section D — the starvation map

### D1 — gaps in weeks

| Consumer | Sees today (source) | NT8 already supplied (same TF) | Gap |
|---|---|---|---|
| Weekly reader | 2 completed weeks, one of them partial (B1 1m since 08-19) | native `1w` × 2000 requested (provider-capped; ≥300 native `1d` proven in-cache) | **months to years vs 2 weeks** |
| IPDA rows 20d/40d/60d (inside C1) | "insufficient history" (B1 has ~2 weeks) | native `1d` × 300 | 60d window unmet by ~40d |
| level_stats 2-week verdict (ES) | 9 days of B1 1m | — (needs 1m specifically) | ~5 days, self-heals by 09-05 |

### D2 — two-source hazards (class 7/8) — concrete disagreement today

1. **Weekly doc vs its own invalidation watch [A].** The written WEEKLY doc's facts come from B1 (`weeklyBars1m`) — today `RefsOK=false`, "(no completed prior week — thin history)" — while the DOA guard, invalidation watch and shadow confluence compute the SAME references (PWH/PWL/NWOG) from B2 [A][CODE `auto_trader_weekly.go:198-203,298,348-349`]. The watch can compute a PWH/PWL from NT8's native 1w bars that the doc itself declares unavailable. Two tapes for one chip.
2. **nPOC splice (B1+B2 in one computation) [A][CODE `trader/auto_trader_dayplan.go:181-199`]:** the 2000-bar B2 slice is spliced with B1 history at the ring boundary; any stamping/dedupe divergence changes which POCs retire.
3. **Dual "daily" definition in one planner prompt [A]:** native calendar-day `1d` (regime block, `auto_trader_planner.go:2054`) vs `DailySessionBars` RTH session-day aggregation (candle tables, `kernel/planner_prompt.go:297-311`). Same label "daily", different boundaries.
4. **`level_stats` joins B2-derived touch episodes with B1-derived outcomes [A][CODE `level_stats_wire.go:158-160`]** — mixing tapes in the validation table.
5. (Deliberate, keep) the bar-truth arbiter `api/handler_bar_truth.go` is a B1-vs-B2 cross-check by design [A][CODE `:23`].

Concrete disagreement example for the report: for week 2026-08-17, B1-aggregated weekly open = first bar 2026-08-19 15:00 UTC (Tuesday 10:00 CT) [A][DB], while NT8's native `1w` bar opens at the Sunday-17:00-CT session open — the weekly candle the model sees is not the week the market saw.

### D3 — consumers that GENUINELY need 1m and must stay intrabar

C3 (session VWAP/σ/POC), C5 (swing/MSS — 1m MSS confirm), C6 (1m FVG/OB), C7 (touch-count), C8 (touch telemetry + level_stats), C10 (E8 1m replay), C12 candle-table aggregation, C11 scripts, and every feed-age/clock-health check. **None of these is starved.** The only consumers that should move OFF the 1m tape onto native higher-TF bars are the weekly reader family (C1: weekly candles, refs, NWOG, IPDA) and — by owner decision — the planner candle-table daily rows (to kill hazard D2.3).

---

## Section E — proposed wave (design only)

### E1 — one resolver
```go
// market/resolver.go (proposed)
type BarSource int // SourceNT8 | SourceOwn1mAgg
func CompletedBars(tf string, from, to int64) ([]Kline, BarSource) // NT8 native first (FuturesBarsProvider), own-1m (BarsBetween) aggregation fallback for gap/restart-cold, completed bars only (CloseTime < now — repaint law), returns the source used
func CompletedBar(tf string, t int64) (Kline, bool, BarSource)
```
Completed-bars-only invariant already exists as the closed-bar filters used by every detector; the resolver centralizes it. The source return value makes the E5 boot line and the log line possible.

### E2 — per consumer
- **C1 weekly reader → SWITCH** to `CompletedBars("1w", …)` (weekly candles, refs, NWOG) and `CompletedBars("1d", …)` (IPDA daily windows). Depth guard unchanged (E4).
- **C1 family (DOA / invalidation / shadow) → already B2; now agrees with the doc by construction** (kills D2.1).
- **Planner candle-table daily rows → switch to native `1d`** or relabel `DailySessionBars` output explicitly as session-day candles (kills D2.3). 15m/1h/4h rows can stay aggregated (W2b "PLANNER EYES" was built on the 1m slice intentionally) or switch to native for consistency with the executor block — owner ruling.
- **C2–C12 → KEEP** as-is (single-source B2 already; C11 scripts keep B1 1m which is their target substrate).
- **C8 nightly level_stats → KEEP B1** (needs a full session-day of 1m; only B1 has it; the 2-week ES window self-heals by 09-05).
- **nPOC splice (D2.2) → keep the B1 historical leg but route it through the resolver** so the splice is stamped with its source.

### E3 — persistence
NT8 higher-TF history is memory-only and lost on restart [A][CODE `bar_cache.go:24-27`, `bar_persist_wire.go:29-46` only persists the pairs' closed bars → DB stores 1m-only]. **Must persist for the resolver to be durable** (e.g. the Sunday weekly read with NT8 disconnected). Storage projection: 1h = 24 rows/day, 1d = 1 row/day, 1w = 1 row/week — trivial next to the 1m table (≈1.5 MB/day at 90d per `bar_history.go:18-19`). Recommend persisting `1h`, `1d`, `1w` (optionally all 14) via the existing `InsertBars` path (the `tf` column already exists; only the migrate step's `DELETE tf <> '1m'` invariant and the integrity check need updating — the prune loop and unique index are TF-agnostic).

### E4 — depth guard
Stays at 4 completed weeks. **Satisfied immediately under E1**: native `1w` × 2000 requested arrives with the post-boot replay; once persisted, even a cold NT8 no longer thins the weekly read. `CompletedWeekCount` is replaced by a count over `CompletedBars("1w")` — the guard itself does not change.

### E5 — boot line
Per TF, source and earliest bar read FROM the resolver (no literals), e.g.:
`📊 bar resolver: 1m src=nt8 first=2026-08-24T19:21Z n=2500 · 1d src=nt8 first=2025-06-03T00:00Z n=300 · 1w src=nt8 first=2025-11-30T22:00Z n=40 · 1h src=nt8 …` — plus the persisted-table line (already `📦 bars: persisting …`).

---

## Surprises (A8 — included, acted on none)

1. **Premise correction:** the dispatch said own-1m "only exists since ~08-26". Measured: MNQ since **08-19 15:00 UTC** (10:00 CT Tue), ES since 08-24 [A][DB]. The thin verdict is unchanged (2 completed weeks < 4).
2. **IPDA is starved too** — the 20d/40d/60d IPDA rows inside the weekly facts are B1-derived and today render "insufficient history"; they would be satisfied by native 1d bars under E1 [A][CODE `kernel/weekly_prompt.go:88-101`].
3. **The weekly doc and its own runtime watch already disagree** (D2.1) — the doc is thin by construction while the watch computes references the doc refuses to show.
4. **Dual daily definition** inside one planner prompt (native 1d vs session-day aggregation) [A][CODE].
5. **Settlement level has no producer at all** — `KindSETT` exists only in the role/evidence tables [A][CODE `kernel/levels_role.go:324-325`], nothing computes it.
6. `ExtractMultiDayLevels` PM/PW guards (10,080 bars) can under-cover from the 2,000-bar B2 1m ring (≈33h < 7d) — the prior-month level silently never appears; covered under E1 for the daily side, owner note for PM.
7. The `📦 bars: persisting 2 symbol×tf` boot line proves the persistence asymmetry directly (2 pairs persisted vs 34,893 bars backfilled at the same boot) [A][RUNTIME 09-01 00:43:45].
8. `wireFuturesBarsProvider` is bound on the singleton server's first start — a Go restart while NT8 is down leaves the provider bound but the cache cold; E3 is what makes the weekly read durable against exactly that window.

---

## Closeout

- Read-only maintained: no code/config/DB writes, no restart, no lock, main tree untouched.
- Report committed to `docs/bar-source-audit-0902`; raw URL 200-checked below.
- Commit ref: **e88395942fd570cb3ecdfff58ab25caa08d445db** · raw: https://raw.githubusercontent.com/johnwick2921-cyber/nofx/e88395942fd570cb3ecdfff58ab25caa08d445db/docs/superpowers/reports/2026-09-02-bar-source-audit.md → HTTP 200 (verified).
