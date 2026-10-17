# SYSTEM MAP — every subsystem, its knobs, gates, windows, and boot lines

**Maintained file on dev.** Built from code @ `492d2067` (boot 8) + the settings registry (`web/src/guide/content/settings.ts`) + two censuses (`docs/superpowers/reports/2026-08-30-knob-census.md`, `2026-09-02-belief-census.md`). Every fact carries `file:line` at that revision; a moved line means the map section is stale — see the contract below.

## Maintenance contract (the update rule)

> **Every wave that changes a rule — a knob value, a gate leg, a window, a threshold, a refusal string, or a boot-line text — updates this map's section in the SAME commit.** A contract test greps this map for the boot-line text and fails if a code boot line has no matching line here (the text is the join key). Labels `[R]/[X]/[T]/[I]/[O]` change with evidence, per the legend.

**Research label legend** (from `docs/superpowers/reports/2026-09-02-belief-census.md:8-16`):

| Label | Meaning |
|---|---|
| `[R]` | researched-supported (cite source) |
| `[X]` | researched-CONTRADICTED (cite) |
| `[T]` | measured on own tape (cite report, n) |
| `[I]` | invented / doctrine, untested |
| `[O]` | owner-ruled |

**Boot-line origin tags** (W1 settings truth, 2026-09-23 — `store.OriginLetter`, one mapping for every line that uses it): `[O]` the owner saved the value (saved / strategy value / session override) · `[E]` the process environment set it (e.g. `BREAKER_HALT_N`, consulted only when the strategy saved nothing) · `[I]` the shipped default.

Visible brand (Dispatch 102): `branding/product.txt` = VL Intelligent; `branding/persona.txt` = VL. `branding/branding.go` supplies the Go banner and rendered agent/Telegram names; `web/src/constants/branding.ts` and Vite read the same files for UI text and the page title. Operational identifiers remain nofx. `fix/rebrand-phase-1-2` remains held and is not part of this wave.

---

## 1 · BARS — NT8 TCP → BarCache → kernel

**What it does:** NinjaTrader 8 (Tradovate) is the single live data source. The C# AddOn streams bars/account/positions/orders over TCP (`127.0.0.1:36974`, `provider/ninjatrader/tcp_server.go:34`); Go caches them in a ring `BarCache` and hands them to the kernel through `market.FuturesBarsProvider`. No NT8 → stale cache → no decisions.

- Flow: frames (`bars_historical`, `bar_update`) → `readLoop` → `barIngestCh` → drain goroutine → `BarCache` — `tcp_server.go:399-407,492`; cache keyed `"SYMBOL|TIMEFRAME"`, ring per key — `bar_cache.go:30-37`.
- Provider wiring: `wireFuturesBarsProvider` → `market.FuturesBarsProvider = server.BarCache().Get(...)` — `trader/ninjatrader/bars_market_bridge.go:17-33`; var declaration `market/futures_data.go:14`.
- Kernel reads the SVP snapshot via `FuturesBarsProvider(activeSymbol, AISVPBarInterval, AISVPBarCount)` — `kernel/engine_analysis.go:297`.
- **Served-scope observability (BARS HORIZON 2026-09-09):** `barsFromCache` measures every served slice with `kernel.HorizonOf` and WARNs on SHORT | HOLED | EMPTY — `trader/ninjatrader/bars_market_bridge.go`, `kernel/bar_horizon.go`. A COUNT is not a HORIZON: the 2026-09-09 13:18 CT planner read was served 2000 of 2000 bars across a 3,055-minute span with 696 open-market minutes missing inside it, so a served-count check would have been silent. WARN only — it gates nothing.
- **The store is the horizon; the ring is the cache (D2):** `store/bar_history.go::LastNBars` + `trader/bars_store_depth.go::barsWithStoreDepthFrom` splice the store onto the OLDER end of a short ring read. Rules: an EMPTY ring is never substituted (the store deepens a live tape, never stands in for one) · a satisfied ring is not second-guessed (no store read) · only bars strictly older than the ring's oldest are taken, so a live/forming bar is never replaced · a failed read WARNs and degrades to the ring. Measured 2026-09-09: `bars` holds MNQ 1m 20,929 rows back to 2026-08-19 (21 days; re-measured 18:4x CT) and MNQ 5m 3,342 rows back to 2026-08-24 (~16 days); retention per TF is `store/bar_history.go` (1m 90d · 3m/5m 180d · 15m/30m 365d · 1h+ forever) and nothing has ever been pruned.
- **`RVBaseline20d` → `RVBaseline` + `RVBaselineDays`, AND THE WINDOW GETS DEEPER (D2b + owner ruling 2026-09-09 18:18 CT):** the field was fed about 7 complete session-days while its name said 20, and the regime line rendered `RV=…%-of-normal` — a baseline with no stated window. The regime now renders `RV=…%-of-baseline(N complete session-days)`, or `(window UNKNOWN)` when the count was not reported. **THE COMPUTED VALUE MOVES — an earlier revision of this bullet claimed it did not, which was false (class 82).** The RULE (`kernel.RVBaselineFrom5mDays`) is byte-for-byte unchanged and pinned so by an E7 golden (`TestRegimeLabelUnchangedWhenTheWindowDoesNotChange`); the INPUT is corrected. Owner condition (b): the 5m ask is served from the store-deepened 1m tail aggregated to 5m (`trader.ResolveRVBaselineTape`), not from the 5m ring. Measured 2026-09-09 on MNQ: stored 5m ×2000 → 0.876371 / 7 days · stored 5m ×2500 → 0.884746 / 9 days · 1m ×12000 → agg 5m → **0.893543 / 9 days**. The last two cover the SAME nine days and disagree by 0.9%, which is why the stored 5m rows are refused. A `📈 regime input window` boot line reports the served window in days BEFORE and AFTER (owner condition (c)); a thin 1m tape falls back to the pre-wave 5m ring read so the baseline is never turned OFF.
- **The ring rehydrates from the store on boot (D3, owner-authorised):** `BarCache.RehydrateOlder` + `rehydrateRingFromStore` (`trader/ninjatrader/bar_persist_wire.go`), fired once the AddOn replay lands. It is NOT `SeedHistorical`: a COLD key is a no-op (a dead feed must never be made to look alive) and `existing` wins every overlap (the store is by definition not fresher than the live ring), so only strictly-older bars enter. Merges via `mergeBarsByTime`, bounded by `maxBars`, placeholder bars refused at this door as at every other, WARN-and-continue on a store failure. Logs per (symbol, tf) with resolved counts. Before this, every Go restart dropped the ring back to the 2000-bar seed while the store held 21 days. **1m ONLY (owner condition (a)):** `pairsToRehydrate` selects the 1m pairs and skips every other timeframe — the stored 5m/15m/1h rows are NT8 aggregates this repo has judged inconsistent with their own 1m constituents (`store/bar_history.go` Migrate step 4 once deleted every non-1m row for that reason), and they must never reach a live regime input. **The ring CEILING is still 2,500** — the rehydrate adds at most ~500 bars per pair (about 8.3 h at 1m) and restores the ring to its ceiling immediately instead of climbing to it over a session; the 21-day depth reaches the planner through the D2 store splice, a different mechanism. It runs BEFORE `afterBackfillHook`, so the well-known `📊 bars after backfill` line and the `BarResolver` behind it describe the ring the bot will actually use.
- **`planner_read_facts` records the horizon, not just a count (D4):** four additive columns (`scope_requested_bars`, `scope_span_ms`, `scope_oldest_age_ms`, `scope_gap_count`) plus `read_horizons` (JSON `kernel.BarHorizon[]`), built by `buildReadFactRow` (`trader/auto_trader_planner.go`). **`scope_bars` is NOT redefined** — it has always been `len(scope.Bars)`, the SERVED count, and 68 of 68 rows (ids 1–68, re-measured 2026-09-09 18:3x CT) record 2000 against a 2000 ask, so the void scope has never been short. What a count could not express is a SPAN or a HOLE: ids 64 and 66 were identical on the record, yet id 66's tape spanned 3,055 minutes with **696 open-market minutes missing** inside it. `read_horizons` as shipped carries **exactly one** entry (the void 1m tape); the ATR 5m entry is an OPEN ITEM, not a delivered one — the array shape exists so it can be appended without a migration. UNKNOWN vs ZERO: a row whose horizon was never computed holds **NULL or `''`** (`HorizonRecorded()`). Include with `where read_horizons is not null and read_horizons != ''`; **count the excluded with `where read_horizons is null or read_horizons = ''`** — verified 2026-09-09 on a COPY of `data.db`: after `ALTER TABLE … ADD COLUMN` all 68 pre-existing rows are NULL, so `where read_horizons = ''` returns **0** where the truth is **68** (a plausible zero, the exact A24 failure the column exists to prevent).
- **THE TAPE IS ONE CONTRACT (ROLL WAVE 2026-09-10):** `bars.contract TEXT` (indexed `(symbol, tf, contract, open_time_ms)`), stamped at write by `trader/ninjatrader/bar_persist_wire.go::contractFor` from the AddOn's most recent `subscribed` ACK — the ONE frame that names the instrument (the hello carries only `build_id`; bar frames carry none). `TCPServer.CurrentContract(symbol)` (`provider/ninjatrader/contract_roll.go`) returns it with its receipt time; **never a date rule** — the AddOn's own date rule (`VLContractResolver.cs:80`, UTC compare, expiry−8d) is the defect this replaces. A different name on the ACK than last named is a ROLL: `observeContract` PURGES the symbol's ring (every tf), records both names + timestamp, notifies once; the persist wire reseeds from the store for the new contract only and raises a P0. Every live store reader asks for a contract: `LastNBarsOn` / `BarsBetweenOn` (current tape) or `WindowContract` first (a historical window — a window across the roll is `unrecomputable:spans_roll`, never read mixed). Retired bars are FILTERED, never deleted. Backfill (one-time, idempotent, on a `bars_pre_dedupe`-style safety copy per A13): three-state by WINDOW INTERSECTION per `(symbol, tf)` from the measured boundary — MNQ last U26 rowid 464732 (21:14 CT), spans 464738/464739/464744, first Z26 464749 (21:18); ES 464728 / 464729–464741 / 464745. Boot line `📜 contract: current=… (source=subscribed@ts) · bars by contract: 09-26=n 12-26=n spans-roll=n null=n · readers filtered=kept/read · ring reseeded=y/n · last roll=…`, every field read. Lint `trader/bar_reader_filter_lint_test.go` fails on a new unfiltered `.LastNBars(`/`.BarsBetween(` in live trader code (text-match, class 113 caveat stated).
- **ONE CONTRACT, TWO SOURCES (BAR-SOURCE WAVE 2026-09-10):** `bars.source TEXT` (`live` | `historical` | `mixed` | `replay:off-scale`; indexed `(symbol, tf, source, open_time_ms)`), stamped at write from the persister's `historical bool` (`barRowsForPersist`) and by the ring at `SeedHistorical`/`Upsert` (`Bar.Source`, Go-side only, `json:"-"`). NT8's replay served the SAME contract ~290 pts below the live feed (research facts 16516009 live 29358.25 vs 16518205 replay 29068.25, 22:37 CT); a contract column cannot separate one contract from itself. **The upsert rule** (`store/bar_history.go::InsertBars`): `ON CONFLICT DO UPDATE … WHERE NOT (bars.source IN ('live','mixed') AND excluded.source='historical')` — live overwrites anything, historical fills only what live never wrote, mixed is kept as the seam's evidence. **The ring rule** (`provider/ninjatrader/bar_source.go::mergeSeedKeepingLive`): an existing live/mixed bar is kept over an incoming historical one. **The verdict** (`BarCache.detectScaleMismatch`, `SeedVerdict(symbol, tf) → (checked, offScale)`): on the FIRST live bar after EACH seed, if the live close differs from the last replay close by > `ScaleMismatchPct` (0.5%) of price AND > `ScaleMismatchRangeMult` (20×) the seed's median body, the seed is dropped from the ring, the straddling bar is labelled mixed (values untouched), the persist wire refills from the store's live rows and raises one P0. **The hold** (`trader/ninjatrader/replay_hold.go`): every replay row — persister historical frames AND the boot backfill's historical ring bars — is HELD per symbol×tf until the verdict; on-scale → released as `historical`, off-scale → discarded loudly, unjudged → held. An unverified replay never reaches the store; an empty minute reads as a gap. Readers (`LastNBarsOn`, `BarsBetweenOn`) exclude `mixed` and `replay:off-scale`. Migration: pre-column rows LIVE (the record of what traded, which a replay must not repaint); MEASURED exceptions `store/bar_source.go::offScale20260910` (98 rows MNQ 51 / ES 47, December, 21:15–22:38 CT, both sides below 29200 / 7630 → `replay:off-scale`) and `straddle20260910` (25 rows, one side each scale → `mixed`, recovering the roll's spans-roll rows whose contract label the 22:39 replay overwrote). Boot line `📼 bar source: SYM live=n historical=n mixed=n off-scale=n null=n · replay-never-overwrites-live=on · unverified-replay-held=on · replay-hold: held=n released=n discarded=n · scale-mismatch threshold=0.50% AND 20x median body [I] · mismatches this process: tf@ts Δ=… (replay … vs live …, n dropped) | none`, every field read. `NOFX_BAR_SCALE_MISMATCH_PCT` / `_MULT` override. NT8's merge/back-adjust policy on the subscription is the AddOn wave's finding, filed in `docs/superpowers/reports/2026-09-10-bar-source.md` §NT8.
- **YEARS OF HISTORY, CONTRACT BY CONTRACT (HISTORY IMPORT, wave 101):** `bars_history_request` / `bars_history_data` / `bars_history_error` frames (Go↔C#, `tcp_framing.go`, `ninjascript/VLHistoryPull.cs`) pull one NAMED contract's history — never the live `bars_subscribe` path, which resolves only the platform's front month (`VLInstrumentLookup.cs`). The C# side resolves the EXPLICIT contract name (`Instrument.GetInstrument("MNQ 09-23")`), always `MergePolicy.DoNotMerge` (a back-adjusted series is a different scale wearing the same label), answers in ~8k-bar chunks, and echoes the instrument's real `ContractName` on every frame. The Go importer (`trader/historical_import.go`, env-gated `POST /api/admin/bars/import`, `HISTORICAL_IMPORT_SEAM=on`) writes ONLY through `store.ImportBars`: `ON CONFLICT DO NOTHING` (a key collision keeps the existing row and is COUNTED — there is no upsert in the import door), every row stamped `contract` (from the wire, never from a date) + `source=historical_import` (a third feed, distinct from the live-path `historical` replay). Imported rows are **never pruned** (`PruneByTF` skips `source='historical_import'`), are **not live-readable** (`IsReadableSource` unchanged; backtests use `IsBacktestReadable` + `BarsBetweenOn`), and a deliberately continuous series must be built with `store.BuildContinuous` — labelled `continuous:adjusted`, an explicit per-seam basis REQUIRED, and the store refuses to persist it. Boot line `📚 history held: <contract>·<tf>=n [first→last] …`, read from the store. Production call sites of the import path: 0 (A29).
- **The prompt's candle tables declare HELD vs REQUESTED (D1):** `kernel/candle_disclosure.go` + `BuildPlannerCandleTablesAt` — every heading states held-of-requested (including when complete), one TAPE line above the tables names bars held/requested, oldest age and gaps, and every row whose window is only partly held is marked ⚠PARTIAL / ⏳FORMING / ❓COVERAGE-UNKNOWN against `kernel/session_calendar.json`. Measured over the stored prompts (`planner_rejected_prompts`, n=55 with a Candles block, ids 70–143, re-measured 2026-09-09 18:3x CT): 15m 12/12 in 55/55 · 1h 12/12 in 55/55 · 4h 8/8 in 55/55 · **daily 2 rows in 20 and 3 rows in 35, never 8, in 0 of 55**. **A COUNT CANNOT SEE A ROW THAT IS NOT THERE:** `AggregateBars` emits no row for an empty bucket, so the heading also counts whole ABSENT windows between held rows and the following row carries a ⛔ marker naming the CT bounds of the break — without it a table straddling an 11h45m outage printed "all held rows COMPLETE". **The minute IN PROGRESS is counted on neither side** (`observableEndMs` floors to the last CLOSED minute), so a gapless tape is never marked ⚠PARTIAL merely for having a forming bar. Nothing is interpolated or synthesised to fill a hole (A24).

**Knobs (resolved default · source):**

| Knob | Value | Source | Label |
|---|---|---|---|
| `AISVPBarInterval` | `"1m"` | kernel/svp.go:46 | — |
| `AISVPBarCount` | 2000 | kernel/svp.go:47 | — |
| `DefaultBarCacheMaxBars` | 2500 | bar_cache.go:24 | — |
| `plannerCandleTapeBars` | 12000 (1m, store-deepened) | trader/auto_trader_planner.go | [D2] (a) read the store |
| `weeklyFactsTapeBars` | 12000 (1m, store-deepened) | trader/auto_trader_weekly.go | [D2] (a) read the store |
| `rvBaselineFallback5mBarsAsk` | `nt.DefaultBarCacheMaxBars` = 2500 (was `rvBaseline5mBarsAsk` = 3000 = 250.0 h, above the 208.3 h ring) — FALLBACK ONLY; the primary input is the 1m tail | trader/auto_trader_planner.go | [D2] (b) + owner (b) |
| `rvBaselineMinDays` | 5 | trader/regime_input_window.go | [owner (b)] |
| `rehydrateTimeframe` | `"1m"` (the ONLY tf the boot rehydrate touches) | trader/ninjatrader/bar_persist_wire.go | [owner (a)] |
| `rvBaselineMaxDays` | 20 (cap; the FED count now travels as `RVBaselineDays`) | trader/auto_trader_planner.go | [D2] (b) name corrected |
| auto-subscribe symbol | `"MNQ"` | tcp_server.go:438 | — |
| auto-subscribe TFs | 1m 3m 5m 15m 30m 1h 2h 4h 6h 8h 12h 1d 3d 1w | tcp_server.go:442 | — |
| auto-subscribe back | 2000 | tcp_server.go:450 | — |
| stale-bar grace | 15 s (`STALE_BAR_GRACE_S`) | kernel/stale_data.go:46 | [R] C2 feed-stamp fix |
| clock-drift tolerance | 60 s | kernel/clock_drift.go:29 | [R] |
| clock warn | 30 s (`CLOCK_WARN_MS`) | kernel/clock_health.go:38-46 | [R] |

**Gates:** stale-data (runtime) refuses with `"stale-data: freshest %s bar %dms old (>%dms) hint=%s"` and converts entries to `wait` — `stale_data.go:129-175`. C2 clock drift is WARN-only (signals are feed-stamped) — `clock_drift.go:58-83`.

**Boot lines:** `"tcp_server: listening"` `tcp_server.go:988` · `"wire_liveness"` `:1024` · `"tcp_server: hello handshake OK"` `:1713` · `"tcp_server: sent bars_subscribe"` `:1478` · `"tcp_server: feed status"` `:2028` · `"⚠️ %s %s: CME futures symbol but no NT8 bar provider wired; skipping"` `market/data.go:247`.

**101 — history at subscribe, the scale check, and the chart across the roll (2026-09-16).**
NT8 honours `bars_back` at subscribe: the AddOn's own `emitted bars_historical
<key> bars=<n>` lines showed 2,000 per tf ≤ 30m, 1,536 for 1h, 69 for 1d at the
09-15 22:15 boot — 13,054 MNQ bars. `📼 bar source: … historical=<n>` is a STORE
census (rows by source; the replay-hold keeps replay rows out of the store by
design), not a delivery count. The Go-side delivery count is now the `🧯 nt8
history at subscribe:` line — per tf `received/asked`, `n/a` for no frame, and
a ZERO frame called out by name (a BarsRequest that ran while the feed was down;
five such reconnects on 09-16).

**Non-1m rings are live + NT8-replay only.** The store rehydrate is 1m-only
(`rehydrateTimeframe`, class 86, owner condition 2026-09-09). After a confirmed
scale break the 🚨 line now says what the ring IS for that tf — "LIVE-ONLY UNTIL
NT8's NEXT FULL REPLAY" for anything but 1m — and `telemetry.ScaleBreakCounts`
records events and bars dropped since boot.

**The scale check (bar_source.go `detectScaleMismatch`) judges ADJACENT bars
only.** Its reference must lie within `scaleCheckAdjacencyIntervals` (2) of the
live bar; older is a time gap, not a scale gap — SKIPPED, WARNed with both ages
(`🕳 scale check SKIPPED …`), the seed kept and the check left armed. An EMPTY
replay no longer re-arms the check (`SeedHistorical` returns before the re-arm
on an empty frame onto a populated ring). Class 127 carries the 09-16 event.

**The horizon WARN keys on the condition, not the observer:** (symbol, tf, why),
one line per five minutes, callers aggregated on the line that closes the window.

**The chart shows every stored contract [O].** `/api/klines` serves the current
contract's ring + store, then prior contracts fill STRICTLY BEFORE the current
contract's first LIVE row (`store.FirstLiveOn` / `PriorContractBarsBefore`,
display-only); every kline carries `contract`; the roll is a visible basis step,
never adjusted. `NOFX_CHART_ACROSS_ROLL=off` disables; the resolved value is on
the `📈 chart:` boot line. **The bot decides on the current contract only** —
`LastNBarsOn`/`BarsBetweenOn` are byte-identical and no kernel/levels/arm path
calls the display readers (E4 grep: 0). `PriorContractBarsBefore` takes the
current contract and EXCLUDES it — the current contract's rows before its first
live row are imports in holes of the prior series, and a hole stays a hole.

**Full data [O "i want fuull data" 2026-09-16].** Every symbol×tf pair rehydrates
its ring from the store at boot and after a confirmed drop, through ONE door
(`rehydrateRowsFor`): (i) post-drop only live rows refill; (ii) every row enters
stamped `historical`; (iii) `historical_import` is refused at the door and the
`🧯 ring rehydrated … import=<n>` line prints the refused count — `LastNBarsOn`
itself filters only mixed+off-scale and hands imports to the CHART; every
PLANNER door reads the NT8-only readers instead (`store.LastNBarsFromNT8On` /
`BarsBetweenFromNT8On`, CTO ruling 2026-09-16): `storeBarReader` +
`BarsWithStoreDepth` (the 12,000-bar 1m tape), `auto_trader_weekly.go` (weeks +
`Own1m`), `auto_trader_dayplan.go` (POC-touch historical leg). The `🧮 planner
tape` boot line prints import rows on the contract, the tape length and the
regime baseline BOTH ways (class 82); `TestNoPlannerDoorReadsTheSharedBarReader`
pins the doors by function body, `TestPlannerStoreReaderServesNoImportRows` by
a real store. The `🧮`/`📈`/R1 lines ride `afterBackfillHook`, which since
2026-09-16 REMEMBERS the event (landed + once under a mutex): install after the
backfill fires immediately (class 130 — the 15:32 boot lost all three lines to a
two-second race). The `🖥 ui:` line judges the served bundle by its embedded
`GUIDE_BUILT_REV` against the binary's `vcs.revision` (`api.UIServingBootLine`);
the mtime is a secondary note. Still reading the shared readers (imports included), NOT planner
doors, named for the CTO: `level_stats_wire.go:121`, `trade_excursion_hook.go:168`,
`trade_excursion_backfill.go:120`, `follow_plan_wiring.go:217`,
`one_setup_boot.go:174`, the display seam, the rehydrate read (whose door counts
its own refusals). (iv) the
contract is the current one. A CONFIRMED scale break re-requests NT8's full replay
**once per symbol per boot** (`RequestHistoryReplayAt`, `historyReplayMaxPerBoot`);
a second break in the same boot prints `second scale break this boot — replay on
another contract, restart the AddOn`; NT8 is not re-asked, the store refill still
runs (live rows, replay-grade) — a time floor
would loop, because historical-over-historical the incoming replay wins in
`mergeSeedKeepingLive`.

## 2 · LEVELS — detection, scoring, seating, roles

**What it does:** detects ~30 level kinds across ELEVEN timeframes, scores them (evidence type × HTF zone tier × size × freshness × anchors), seats a per-trader TOTAL of `max_levels` into the plan (NOT per side — `levels_score.go:603-605` truncates the whole slice; `seatBothSides` rebalances WITHIN that total), and assigns roles.

**Level zones (2026-09-11, corrected owner scope).** The planner also carries an uncut, frozen presentation map (`kernel/level_zones.go`, `kernel/level_zone_inputs.go`). `BuildLevelZones` consumes `researchRaw`, not the seated pool. Every source survives. Point width uses exact defining-wick evidence plus source ATR where available; missing width stays NULL and cannot pass merge compatibility. Native bands wider than the resolved context/merge threshold stay separate. Compatible anchors join one nearest fixed-anchor cluster within m×ATR5m and under the union-width cap; no transitive clustering. Display families and four-term ranking never feed the legacy score or execution anchors. `PlannerInput.Zones` reaches the model table and freezes into `PlanDoc.zone_map`; `LevelZoneMap.tsx` renders the same snapshot in the card. The chart retains authored levels. No historical backfill. All new numerical choices are [I]; the existing bound shortlist cap is unchanged.

**W3 (2026-09-09) — candidates, not entitlements.** The whole map is kept; what changed is how many of its levels are offered as ENTRY candidates and in what order, plus references PROJECTED beyond the mapped range. All of it is a RENDER-TIME view (`kernel/map_candidates.go`, `kernel/map_projections.go`) built from `[]ScoredLevel` — **no score, weight, ladder, cap or detector moved**, and `stage_a_score_legacy.json` is byte-identical.

| step | what it does | where |
|---|---|---|
| merge | references within one zone-width become ONE candidate carrying ALL names (`"29657.38 — Supply·1h · PDC · VWAP+1σ"`) and contributing ONE credit | `map_candidates.go BuildMapCandidates` |
| map role | `entry-candidate` / `target` / `obstacle` / `invalidation` — a SECOND axis, distinct from the five `LevelRole` values, which are unchanged and still serialized into the parity golden | `map_candidates.go MapRole` |
| candidacy `[I]` | a level is an ENTRY candidate only with an opposing reference at ≥ the resolved minimum (default: the stop floor, `MinSLATRMult()×ATR5m`) — the wave's ONLY new refusal, and it refuses CANDIDACY, never an authored scenario | `map_candidates.go assignMapRoles` |
| order `[I]` | the entry shortlist ranks by REACHABILITY (distance, nearest first); the score is carried and shown but ranks second | `map_candidates.go BuildMapCandidates` |
| projections `[I]` | beyond the mapped range: PWH/PWL from the **daily** bar source, round numbers, ATR-projected session extreme, measured move. TARGETS/OBSTACLES only — never entry candidates | `map_projections.go` |

**W-TF (2026-09-10) — every detector, every timeframe.** The per-timeframe pass already existed (`DetectHTFLevels`, G2/G3 2026-08-24); what changed is which timeframes reach it and whether a timeframe is part of a level's identity. **No detector definition, score weight, multiplier, cap or tolerance moved.**

| step | what it does | where |
|---|---|---|
| detection set | ONE ordered source, `HTFDetectionTFs` = 15m·30m·1h·2h·4h·6h·8h·12h·**1d·3d·1w**; `isHTFDetectionTF` ranges over it. Sub-15m stays OUT — intraday noise adds nothing to HTF structure and base swings already serve 5m/15m | `levels_assemble.go isHTFDetectionTF` |
| alias | `canonicalDetectionTF` resolves the config's `"D"`/`"W"` to the store's `1d`/`1w`, at the point the value ENTERS detection (class 28) | `levels_assemble.go canonicalDetectionTF` |
| detectors | a TABLE, not four inline loops, so "how many detectors per timeframe" is a value the boot line READS: equal-highs-lows · supply-demand · fair-value-gaps · order-blocks | `levels_assemble.go htfDetectors` |
| identity | the dedupe key is `(kind, tf, price±tick)`. A 1h and a 1d order block at one price are TWO references; before this the second vanished into the first with no record | `levels_assemble.go dedupeSameKind` |
| lookback `[I]` | `DetectedLevel.LookbackBars` records the window actually searched on that timeframe — 500 weekly bars and 500 quarter-hourly bars are the same lookback in bars and nine years apart in time (round 12, 12a) | `levels.go DetectedLevel` |
| partial window | a timeframe below `htfMinClosedBars` emits NOTHING and records why; `Counts` and `Skipped` are disjoint, so "produced nothing" and "was never read" stay distinguishable | `levels_assemble.go detectHTFLevels` |
| daily tier `[I]` | `zoneTierFor` classifies `1d`/`3d`/`1w` into the **4h** tier. Nothing was reweighted: this classifies an input that was previously impossible. Without it the default routed a daily level to the 1m noise floor — multiplier 1.0 vs 4h's 1.3, `KindOB` evidence 0.40 vs 0.72, and the zone grader's 1m clause forcing grade C | `levels_score.go zoneTierFor` |

**The daily family was configured all along.** The bound strategy's `planner_timeframes` reads `["D","4h","1h","15m","5m"]` and has since the default was written (`store/strategy.go:1407`) — `"D"` is named FIRST. The gate only knew `"1d"`, so `"D"` was dropped in silence. Measured 2026-09-10: **0 of 297** stored plans carry a level from any daily timeframe, while the store holds 1d n=1902 back to 2019-05-02 and 1w n=384 back to 2019-04-26.

**Nothing here asserts that a daily level is stronger.** The 4h-tier inheritance and the ×1.2 HTF weight are both `[I]`: round 12 (12c) finds the multiplier untested and establishes no timeframe hierarchy. This wave makes the question answerable by E4; it does not answer it.

**Exclusion is not invalidation (class 93).** A level cut from the entry shortlist stays in the map, the plan document and the chart, carrying its role.

**The confluence term is NOT touched by W3** and two findings about it are RECORDED for experiment E4, not fixed here: three names at one price earn a 1.40× score premium (cap 1.60×), and the window confluence is COUNTED over (`confBand = 0.10×dATR`, ≈±20 pt) is ~6.8× the width clusters COLLAPSE within (3.00 pt) — both commented "cluster tolerance". See `docs/superpowers/reports/2026-09-09-candidates-data/E4-recorded-findings.md`.

**Level kinds — definitions with windows** (all CT; `kernel/levels*.go`):

| Kind | Definition | Source |
|---|---|---|
| PDH/PDL/PDC | prior calendar-day high/low/close (needs ≥900 closed 1m bars) | levels_multiday.go:147-160,225-229 |
| RTH-H/RTH-L | prior day's bars whose active session == NY | levels_multiday.go:95-103,163-169 |
| AS-H/AS-L | overnight Asia (session ASIA) of current session-day | levels_multiday.go:105-121,176-181 |
| LDN-H/LDN-L | overnight London of current session-day | levels_multiday.go:105-121,182-187 |
| ONH/ONL | composite overnight high/low = max/min(AS, LDN) | levels_multiday.go:188-196 |
| PWH/PWL (projection, W3) | prior week Mon–Sun (needs ≥4320 bars) | levels_multiday.go:131-141,207-215 |
| PMH/PML | prior calendar month (needs ≥10080 bars) | levels_multiday.go:142-150,216-223 |
| RN (round) | multiples of 100/50/25 within ±proximityK×dATR | levels_intraday.go:17-48 |
| GAP | unfilled gap ≥ 1.0×ATR | levels_intraday.go:56-105 |
| **OR-H/OR-L** | high/low of the **first 5 minutes** after RTH open (08:30–08:35 CT) | levels_intraday.go:111-139 |
| **IB-H/IB-L** (+1.5×/2× ext) | high/low of the **first 60 minutes** (08:30–09:30 CT) | levels_intraday.go:140,171-183 |
| nPOC | prior-session POC not retraded (retires on bracket beyond ±0.25 tick), ≤10 sessions | levels_volume.go:249-305 |
| VWAP (±1σ, ±2σ) | session VWAP anchored at 17:00 CT roll; ±2σ emitted at 0.85 | levels_volume.go:33-66 |
| eVWAP | extended VWAP anchored 15:00 CT cash close | levels_volume.go:93-118 |
| POC/VAH/VAL | prior session-day 120-bin profile; POC=max-vol bin, 70% VA | levels_volume.go:129-232 |
| pdVWAP | prior session-day's session VWAP | levels_volume.go:313-335 |
| SETT | prior settlement ≈ prior session final 1m close (16:00 CT) | levels_volume.go:339-360 |
| MID-O | overnight midpoint (ONH+ONL)/2, 17:00→08:30 CT | levels_volume.go:365-389 |
| EQH/EQL | k=2 strict pivots clustered within 3×tick | levels_zones.go:31-70, levels_assemble.go:77-79 |
| SUPPLY/DEMAND | base ≤6 candles, bodies ≤0.5×ATR, departure ≥1.5×ATR | levels_zones.go:110-130 |
| FVG | 3-candle imbalance, unfilled; floor max(2×tick, 2.0 pt) | levels_zones.go:165-190 |
| IFVG / OB | inverse FVG; order blocks | levels_zones.go, levels_assemble.go:84-85 |
| SWG-H/SWG-L | recent 5m/15m fractal swings (k=2, min-move 0.25×ATR), ≤3 per TF/side, lookback 144/96 | levels_swing.go:22-34,57-170 |
| OWNER | sticky owner-set level | levels.go:65 |
| NWOG (weekly) | weekend gap Friday ≤16:00 → Sunday first print | weekly_bias.go:47-58 |
| IPDA (weekly) | trailing 20/40/60-day HH/LL | weekly_bias.go:58-63 |

Assembly order: MultiDay → Round → OR/IB → Gap → EQH/EQL → S/D → FVG → OB → Volume → Swing → nPOC, then same-kind dedupe within 1 tick — `levels_assemble.go:81-96`.

**Scoring weights** (`levels_score.go`): kind weights :87-122 (structural 1.0 · VWAP/POC 0.90 · ON/nPOC/SWG/VWAP±2σ/eVWAP/pdVWAP 0.85 · VAH/VAL/SETT 0.80 · AS/LDN/OR/IB/EQ 0.70 · MID-O 0.60 · Round/Gap 0.55 · zones 0.30 confluence-only · default 0.50) `[I]` · zone TF tiers 1.0/1.1/1.2/1.3 (:148-161) `[I]` · zone reversal bonus ×1.1 `[I]` · ConfluenceCap 3 (:192-203) `[I]` · zoneSizeMult ladder ≤0.3×ATR ×1.25 … >2.5 ×0.50 (:205-222) `[I]` · freshness ladders (anchor 1/.8/.6/.5 :359-372; zone 1/.6/.3/.15 :378-390) `[I]` · proximity band = proximityK×dATR (:423), default 1.5, owner retune 0.3 per-trader config (plan_lifecycle.go:16,23-29) `[O]` · confluence band 0.10×dATR (:427) · cluster tolerance 12 ticks = 3.0 pt (:717-726) `[I]` · tier-1 proximity 12 ticks (:256) `[I]` · DefaultMaxLevels 8 (:54) · MinSideLevels 3 (:753). **`[T]`-positive** (conformance 2026-09-04 corrected a census misread): swing seats DO improve turn capture — missed-turns 80.0/75.0/79.2% → 65.0/60.0/66.7% (grand-audit.md:74, PROVEN) — seats kept `[T]`.

**Roles — TWO axes, never merged.** (1) `LevelRole`, five values describing auction CHARACTER — `levels_role.go:24-29`; consumed / 3rd-touch / far-HTF → **target_only, never entry** (:28,107-118) `[I]`. (2) `MapRole` (W3), four values describing USE IN THIS READ — entry-candidate / target / obstacle / invalidation, `map_candidates.go`. A level can be a `react_zone` (character) and an `obstacle` (use) at once.

**Touch record (`touch_outcomes`) — WAVE A, 2026-09-05.** One row per D1′ episode, written per planner read from `trader/detector_record.go`.

- **The scan starts at `max(FormedAtMs, watermark)`** :57-70 — never before the level existed, never re-scanning a recorded window. The watermark (`store/touch_outcomes.go` `LastOpenedAtMs`) covers the WHOLE scanned window and the ordinal (`NextOrdinal`) counts within the EPISODE's own session-day. Handing either the *current* session-day is what produced 677 rows for 423 episodes and 471 rows reading ordinal 1.
- **`validity` gates every rate.** `RatesBy` — the one chokepoint `DetectorReport` and all callers inherit — draws from `validity='valid'` only. Values: `valid` · `unverified:no_formation` · `invalid:pre_formation` · `invalid:duplicate` · `legacy:unverified`. No SQL default: an empty validity means NOT CERTIFIED.
- **KNOWN LIMIT — 74.3% of the corpus cannot be certified.** `FormedAtMs` is set only by `kernel/levels_zones.go` (DEMAND/SUPPLY/OB/FVG). Every LINE level comes from `lineLevel` (`kernel/levels.go:93-95`), which never sets it — 503 of 677 live rows, including all 140 RTH-L rows. Those episodes are recorded as `unverified:no_formation` and excluded from rates. Giving line levels a birth time is what unlocks them.

**105 identity metadata (implemented; awaiting cutover).** The primary merged
reference carries a nullable SHA256 id over symbol, kind, bounds, origin date,
formation timeframe and separately captured `formed_close_ms`. The existing
`FormedAtMs` is unchanged. Actual source closes are captured at emitter outputs;
VWAP uses its explicit source anchor. Missing formation, round numbers and
uncaptured durable legacy references retain NULL IDs. Source:
`kernel/scenario_level_identity.go`, `levelidentity/identity.go`. Detector
selection, dedupe, scores, seats and merge width do not read these new fields.

## 3 · PLANNER — plan authoring and sessions

**105 scenario identity (implemented; awaiting cutover).** New scenarios cite the
map's `level_id`; accepted plans freeze the candidate metadata in `identity_levels`.
`StampAuthoredIdentity` is WARN-only; missing and unknown IDs never add a refusal.
`LevelByID` is the common attribution resolver for evaluation recording, episode
identity, the card and desk. The trading evaluator keeps its original anchor and
decisions. Disagreement is recorded once per trader/plan/version/scenario. The
boot line counts recorded new-authoring events, not inferred legacy omissions.
`touch_outcomes.level_id` is additive; the W1 proximity fields remain unchanged
as corroboration and NULL-ID fallback. Multiple scenarios naming one candidate
remain ambiguous at the scenario join. Backfill classifies pre-W-TF episode rows
untouched, and later rows recomputed only with all seven recorded inputs, else
`unrecomputable:<missing inputs>`. Legacy plans are never rewritten.


**What it does:** the prompt (`kernel/planner_prompt.go`) instructs the LLM; the output parses into a plan document (`kernel/plan_doc.go`) with bias, levels, scenarios, confirms, arms; `ValidatePlanDocWithCaps` chains every write-site validator.

**Rules in the prompt** (resolved, current lines): bias tree — close>PDH→bull HIGH · PDH sweep+close back→bear MEDIUM · inside-day→close vs PDC LOW — `planner_prompt.go:158-160` `[I]` · NY AM 08:30–11:00 CT primary, 10:00–11:00 premium FVG (:653-655) `[I]` · conviction "down Monday, up Thu/Fri" (:656) `[I]` · STOP-DOING: acceptance without prior sweep+displacement = 0% win evidence (:658-660) `[T]` · HTF zones are confluence, never standalone triggers (:532-546) `[O]` · scenario mix follows regime+day_type (:706-707) `[I]` · `entry_mode=ce` default (:626) `[R/O]`.

**Sessions** (`kernel/session_registry.go:87-126`, CT): ASIA 17:00→02:00 (Read 16:30, kz 19:00–23:00, disabled) · LONDON 02:00→08:30 (Read 01:30, kz 02:00–05:00, disabled) · NY 08:30→14:45 (Read 08:00, kz 08:30–11:00 + 13:00–14:45, enabled; **session end == EOD flat**, owner contract) `[O]`.

**Blackouts:** T1 red news ±15 min hard no-trade (`T1BlackoutMinutes=15` `calendar_blackout.go:14`, windows :23-39) `[O]` · T2 caution-only (:21-22) · lunch 12:00–13:30 CT (`no_trade_band.go:42`) `[O]` · first-5-min no-trade (:34-37) `[O]` · session gate `auto_trader_session.go:98-127`.

**WHICH PATH EACH BLACKOUT BINDS** (measured W5 at rev `954f11b1`; **CHANGED by dispatch 104, 2026-09-09**): the lunch and first-N bands had exactly two consumers — the AI-DECISION entry gate (`auto_trader_orders.go:297` → `sessionEntryBlocked`, the only production call site) and the adherence grader, which scores after the fact and refuses nothing. **UNTIL `954f11b1` they did not bind the ARM path:** `grep -cE 'InLunchNoTrade|InFirstNoTradeMinutes|sessionEntryBlocked' trader/armed_executor.go` = **0**, so with `plan_mode=strict` — where a resting order is the only way into the market — neither band could refuse an entry. `[R]` **SINCE dispatch 104 they bind BOTH paths:** `sessionEntryBlockedAt(now)` (the A28 seam, so one band is read with one clock) is consulted in `maybeManageArmedOrdersAt` once per cycle BEFORE the scenario loop, an arm inside the band is refused under class `no_trade_band`, and an arm already RESTING when the band opens is cancelled through the close's own seam rather than grandfathered — refusing only new arms while one placed at 11:58 rests into 12:00 is a band that stops authoring and not entering. T1 red news binds both paths as before (decision refusal + force-flat cancel/flatten). The W5 measurement is retained rather than deleted: it is the evidence that this was ever a gap.

**Boot lines:** `"📜 prompt/validator contract: %d restrictions, all stated in prompt (class 38 guard)"` `prompt_contract.go:164-172` · `"no-trade band: first_n=%dm lunch=%s–%s …"` `no_trade_band.go:199-203` · `"void scope: session-day window · %s×%d · one resolver for prompt AND validator (parity)"` `void_scope.go:100-104`.

## 4 · VALIDATORS — REJECT-at-write law

All chained through `ValidatePlanDocWithCaps` — `kernel/plan_doc.go:588`. Each refuses authoring at write time:

| Validator | Knobs (resolved) | Refuses | Source · Label |
|---|---|---|---|
| breakdown/continue | `BD_MIN_DISP_ATR=1.0`, `BD_MAX_PULLBACK=0.4`, `BD_MIN_CLOSES=1`, `BD_MAX_LEVEL_DIST_ATR=5.0`, `BD_MIN_SL_ATR=1.0` | missing breakdown{}, wrong direction, level >5×ATR, **"a close came back across %.2f — the breakdown is void"** (owner entry law), no confirming close, displacement <1.0×ATR5m, pullback-only/wait_confirm/confirm/min-SL arms | breakdown_continue.go:43-93,213-284 · [T]/[I]/[O] |
| entry law | law table :33-88 | `fade_requires_touch`, `2x5m_reserved`, `sweep_leg1_requires_touch` (leg 1 needs a real sweep touch [O]), `sweep_leg2_requires_mss_or_1x5m`, fade stop <2 ticks beyond level | entry_law.go:153-216 · [O] |
| FVG entry | `FVG_ENTRY_MIN_DISP_ATR=1.5`, `FVG_CE_WIDTH_PTS=20` ("NQ gap sweet spot 20–80 pts"), gap floor max(2×tick, 2.0 pt) | displacement <1.5×ATR5m, gap < floor, CE band = max(0.5, 10% width) | fvg_entry.go:26-49,235-362 · [R] |
| min-SL | `MinSLATRMultDefault=1.5` (was 1.0, owner-ruled 2026-09-02 with citation), `MinSLTickClearance=2` | `"sl_too_tight: %.1f < %.1f×ATR (%.1f) — widen or skip"` | min_sl.go:40-68 · [O] |
| confirm staleness | `StaleConfirmATR=2.0×ATR5m` | a MET confirm farther than 2.0×ATR5m from ref_price is stale-MET | plan_confirm.go:118-177 · [I] |
| HTF veto | mode `1h\|cross\|4h`, default 1h | `"htf_veto: %s vs %s %s (%s)"` — 1h (and 4h in cross) blocks counter-trend entries; fail-open WARN | htf_veto.go:17-142 · [O] |
| structure | k=2, min-move 0.25×ATR, MSS body 1.5×ATR, MSS displacement 0.5×ATR5m | swing/MSS confirm conditions not met | structure.go:27-29, mss.go:22-30 · [T]/[I] |
| accepts | 2x5m needs 2 closes, 5m-close needs 1; `AcceptHoldMin=10` min | confirm not MET per rule | plan_confirm.go:52-115, scenario_facts.go:100-119 · [I] |

**Boot lines:** `"entry law: bd_min_closes=%d (authoring_default) bd_min_disp_atr=%.2f mss_min_disp_atr=%.2f accept_hold_min=%d (authoring_default) …"` `entry_law.go:108-124` · confirm-rule ledger at main.go:332. (`bd_min_closes`/`accept_hold_min` print with the planner's authoring-default label: since W2 the STORED rule governs — #190.)

## 5 · ARMS — resting orders at plan levels

**What it does:** turns plan scenarios into resting limit orders ("arms") at levels, gates them at arm time, places inside a tick band, manages their lifecycle (working → filled/rejected/cancelled), and re-arms after plan-version changes.

**Structural-stop candidate (booted 2026-09-13; daily-loss clarification).** For `reject` level-fade entry legs, `composeArmStop` receives the frozen `PlanDoc.Zones`, scenario identity and `ResolveStructuralStop` policy. `ComposeLevelFadeGeometry` derives stop from far zone edge plus buffer, target from the first distinct complete zone near edge, then refuses bad geometry without resizing or price repair. Missing provenance records ATR fallback and refuses. A failed decision write or failed retirement withholds the placement phase for that cycle. The two arm-time min-SL consumers skip only their ATR floor for already structurally composed fades; other EntryGate legs remain unchanged. `system_config` geometry records carry plan/version/scenario/leg, prices, boundary names, buffer provenance, costs/exposure, quantity and exact decision; API/card read those records. Owner clarification 2026-09-13: the owner meant the existing daily loss limit. No separate per-trade cap or unset-cap refusal exists. Modeled contract loss is recorded for visibility; existing daily-loss/entry gates retain their value, enablement and wiring. Default buffer is C5 training p95, 4.50 MNQ points [I]; no profitability claim. Implementation: `trader/structural_geometry.go`, `store/structural_geometry.go`, `trader/armed_executor.go`, `trader/entry_gate.go` (ATR role only). The historical chain below is superseded for reject stop/target composition by this paragraph.

**Chain per tick** (`maybeManageArmedOrders` `armed_executor.go:193`): session-risk gate → **one-setup verdicts, ONCE per cycle** (`oneSetupVerdictsAt`, dispatch 102) → **retire pass** (`oneSetupRetireDeclined`, owner ruling 2026-09-11: a non-terminal `armed` row with no signal id whose scenario is currently DECLINED is cancelled by ledger state with the owner's reason and its three verdicts — never placed, nothing sent to the broker; an allowed one still places) → the scenario loop in **D4 rank order** (`kernel.OneSetupOrder`: allowed first, by quality) → stop-anchor composition (0B, `composeArmStop`) → **obstacle target** (`oneSetupObstacleTarget`: `leg.Target` = the scenario's recorded first obstacle, composed BEFORE the gate so the R:R leg judges it) → wait_confirm → `armGateVerdictFor` (an R:R refusal of an obstacle target is the existing refusal, counted `obstacle_below_floor` beside `rr`) → `oneLiveArmGuard` → `entryGateForArm` (class 48) → **the one-setup consult** (`oneSetupConsult`: declined → all three verdicts logged, episode rows stamped, counted, `continue`; allowed but another scenario holds the plan's one non-terminal row → `second_setup_waiting`; never a cancel) → kind validation → far-arm warn → `UpsertArm` → shadow-AB → split-sibling stop-out cancel → the scenario record saved (`oneSetupSaveRecord`) → placement. (Line numbers for this paragraph are not contract-checked; the MAPCHECK region below is.)

**One setup (dispatch 102, 2026-09-11) — the book arms ONE play; the follow is recorded.** `[O]` The predicate `kernel.OneSetupAllowsAt(now, sc, level, perm, cfg)` is pure and takes `now`; three verdicts, all reported: (1) LEVEL — the scenario's level (by `level_id` via 105's `LevelByID`, else its anchor price with basis `price_proximity`; an unresolved id NEVER resolves by nearest) IS the top-ranked merged candidate inside the reachability band at `now` — grade ≥ `one_setup_min_grade` (B) first, distance second, any tf, any kind, projections never; (2) PLAY — `reject`; (3) PERMISSION — W2's `FadePermissionAt(now)` reads permitted; NULL never permits. `one_setup_enabled` nil=ON `[O]`; Historically OFF was byte-identical to the base commit's arm path (`trader/testdata/one_setup_arm_path.golden`, generated at dev 499e4f83 where BOTH fixture rejects armed — "whichever fires first"). The map, seat race, merge and planner render are untouched (`kernel/one_setup_map_pin_test.go`: structural + the identity golden). With the structural-stop candidate, OFF still disables selection but reject geometry follows the new structural rules. The predicate gates AUTHORIZATION only: a resting arm declined by a later best-level flip is left alone (a cancel would meet MANUAL-CANCEL-WINS and kill the re-arm) and counted `one_setup:declined_while_resting`. Episode rows carry the verdict (`touch_outcomes.one_setup_*`, stamped once, linked by price inside the map's 3.0-pt width because W1's `scenario_nearest` resolved 0 of 999 live rows — `ParsePlanDoc` rejects the stored doc "too many levels: 12 (max 8)", A15 for 101). The scenario record `one_setup:<trader>:<plan>:v<n>` feeds the card's `one_setup` payload and the desk SCENARIOS line — what the seam DECIDED, never a re-evaluation. Counters (class 35) under the arm-refusal namespace: `one_setup:armable|level|play|day|not_evaluated|waiting|declined_while_resting`, `obstacle_below_floor`, `obstacle_missing`. Boot line `OneSetupBootLine` (`trader/one_setup_boot.go`, main.go beside W2's): `"🎯 one setup: ON[O] · level=best-near-price(min-grade B)[O] · play=reject · target=first-obstacle · permission-required=yes · map=untouched · today armable=… declined=… (level=… play=… day=… not-evaluated=… waiting=…) · obstacle-below-floor=… · follow-plan=RECORDED-ONLY[T] breaks=… retests=… role-reversed=…/… · bias-flip=recorded-only · off-switch=one_setup_enabled (source: …) · backfill …"` — every field READ. **The follow-plan** `[T]` (`kernel/follow_plan.go` pure; `trader/follow_plan_wiring.go` at the detector hook once per planner read + D9 backfill through the roll wave's contract filter): BREAK = first CLOSED 5m bucket beyond the level (`closedConfirmationBuckets`, never forming) → role reversed on the row, never on the map → RETEST = first 1m bar containing the level whose previous close is on the far side → would-be entry = passive limit AT the level, filled iff the bar traded through by a tick (`follow:passive_limit_at_level:through_1_tick`; else `follow:touch_not_fill`, entry NULL) → MAE/MFE/net(−2 pt) at 10 and 20 closed buckets → the retest's own detector verdict joined by level + session-day + order → `bias_would_flip_to` beside `plan_bias_frozen`. States `open | broken | retested | complete | no_break | no_retest | no_fill | incomplete`. Nothing reaches the wire (E9: recorder on/off → arm path identical; the file names no placer). Pre-registered null: role reversal ≤ 50%, follow ≤ 0 net; the cell that changes the ruling: 95% lower bounds above 50% and above 0 at ~385 episodes per approach-direction × level-timeframe cell, pooled first. Report: `reports/2026-09-11-one-setup.md`.

**Gate legs + refusals** (`armGateVerdictFor` :1701): invalid ArmSpec → err as-is (:1703-1704) · direction not armable `"direction %q not armable"` (:1708) · plan bias `"against plan bias %q (plan_mode=direction)"` (:1719) · quality `"quality %s below min_scenario_quality %s"` (:1725) · R:R `"R:R %.2f below arm min %.2f (studio min_risk_reward_ratio)"` (:1738, one floor = `min_risk_reward_ratio`, default 3.0 `store/strategy.go:76`; ARM_MIN_RR env DELETED `[O]`) · min-SL `"stop %.2f too close (%.2f < %.2f = %.1f×ATR5m)"` (:1747) · HTF veto `"HTF veto: " + reason` (:1759). `oneLiveArmGuard` (:641, class-27 FIX 4): `"one_open_position: %s arm %s refused — position %d open …; no adds, no flips (owner ruling 2026-09-03)"` (:668-669). Marketable-side limits never placed: `"level accepted through — marketable, never placed"` (:961, `limitMarketableWrongSide` :990 with the AUTHORED ENTRY) `[R]` 08-30 incident.

<!-- MAPCHECK:trader/armed_executor.go — the two paragraphs below are line-checked by
     TestSystemMapStopEntryRefsResolve (class 75 has no global contract test; this
     region has one). Every `symbol` :NNN pair must resolve in the named file. -->
**Stop-entry placement, the gates IN THE ORDER PRODUCTION RUNS THEM** (`runArmedPlacementAt` :1288, `stop_entry` branch :1372-1419, WAVE B 2026-09-05): canonical side folded once at the row (`side` :1355, class 77) → `stopEntrySeamOn` (:92 — **OFF at the 2026-09-05 cutover by owner ruling: NO stop entry is placed at all, and the D4 boot line leads with it**) → retest window, FALLBACK ONLY — a reclaim skips it (`stopEntryNeedsRetestWindow` :1379) → `decideStopEntry` :1673 (**stop-side wrong-way guard**, tick-rounded trigger) → `placeOneStopEntry` :1715 → `PlaceStopEntry` :1784, and the **AddOn build floor is LAST**, inside that call (`tcp_trader.go:593`). ONE PREDICATE PER ORDER KIND, chosen by kind: `stopEntryMarketableWrongSide` :1568 is **at-or-through** (long `price >= trigger`, short `price <= trigger`) because a stop AT its trigger fires; `limitMarketableWrongSide` :1507 stays **strictly-through** because a limit AT its price rests. Three verdicts (`stopGuardVerdict` :1541, UNKNOWN = iota zero) mapped to three actions (`stopEntryAction` :1617, NO-OP = iota zero, and PLACEMENT IS REACHABLE ONLY BY NAMING `stopEntryPlace` :1695 — the `default:` arm no-ops): THROUGH → cancel `"accepted through (stop side): price %.2f %s trigger %.2f — never placed"` (:1447) · REST → place (:1536) · **UNKNOWN → no cancel, no placement**, WARN `"⚠️ armed %s stop-entry NOT adjudicated"` (:1583) + `countStopEntryRefusal` class `stop_entry:guard_unknown` (:1582) `[O]`. Build refusal: `ErrAddonBuildTooOld` → WARN + class `stop_entry:addon_build` (:1609). Refusal keys are 1-based and `:place`-namespaced (`armKey` :1739) so they cannot alias the arm-gate keys in the same map. `[R]` — until 2026-09-05 this branch called the LIMIT predicate with the trigger and all four of its answers were inverted: 21 already-through sell stops admitted on 2026-09-04 (armed_orders 38, 62-102), 0 valid stops ever placed, 0 fills on 22 post-E7 lifetime submissions.

**The side the wire carries** (class 77, 2026-09-05) `[R]`: `store.UpsertArm` canonicalizes `armed_orders.side` to **UPPERCASE** at the write (`store/armed_orders.go:181`, class 28) and the placement branch compared it to the lowercase literal `"long"`. Two silent consequences: a LONG stop entry was built at `entry − offset` (below the level a buy stop must sit above), and the wire carried `"LONG"` to a C# ternary reading `side == "long" ? Buy : SellShort` — a LONG entry, limit or stop, submitted as a live SELL. Never fired because no LONG row exists post-canonicalizer (21 stop_entry + 9 limit rows, all SHORT). Now folded ONCE at `side` :1355 and used for the trigger, the guard, the log and both wire calls (`decideStopEntry` :1673 folds again, defensively); the AddOn folds on arrival too (`VLTraderTCPClient.cs:979`).
<!-- /MAPCHECK -->

**Cancel confirmation and the per-slot invariant** (cancel-confirmation, 2026-09-06) `[R]`: a cancel is settled by the BROKER'S BOOK, never by `CancelOrder`'s return — that return means a frame reached the socket and nothing more (`tcp_trader.go` `CancelOrder`, one-line pass-through to `SendCancelOrder`). `RequestCancel` moves the row to **`cancel_pending`, which is NON-TERMINAL** (`ListNonTerminal` includes it, so it holds its slot, is swept at boot and is counted by cutover leg 4); only `ConfirmCancel(id, snapshotID)` may write `cancelled`, and it records which snapshot settled it. A stale or absent book settles NOTHING and promotes nothing — past the resolved timeout the row stays pending, WARNs with its age, and is re-requested to a cap. **D3, the per-slot invariant:** before ANY placement the broker's fresh book must show zero non-terminal orders carrying that slot's signal ids, on BOTH paths (`armSlotGuard` at the stop-entry call and at the limit call) — a live order refuses (`arm_slot_live_at_broker`), and a book we cannot see ALSO refuses (`arm_slot_unverifiable`), because an unverifiable slot is not an empty slot. Slot key is **(plan_id, scenario, leg_index)** — version is a mutable last-touch column and is NOT in it. `CancelSubmitted`/`CancelPending` are non-terminal in the book (measured 130 and 22 across 360 frames), so a cancel in flight correctly keeps the slot locked. **A dark book is an OUTAGE** (owner ruling 2026-09-06): when the refusal cause is staleness rather than a live slot, ONE P0 in-app alert is raised per outage (`emitAlert` `broker_book`, event id `cancel_book_stale:<outage-start-ms>`, carrying the book age), and it is acked — banner cleared — when a fresh book returns. Every refusal inside one outage collapses onto that alert; a LATER outage raises its own. The `arm_slot_unverifiable` counter is unchanged: one alert beside it, not a second counter. **Boot line:** `"cancels: confirm=broker-snapshot · pending=<n> · unconfirmed=<n> · slot-guard=on(refuse-on-live|stale) · timeout=<d> · stale-bound=<d> · rerequest-cap=<n> · reconciled(confirmed=<n> live=<n> unconfirmed=<n>)"` — `CancelBootLine`, emitted from main.go; the reconciliation half reads `reconciled=n/a (no broker book yet)` until a book exists, because at process start there is none. **Its 🧾 glyph is shared by nine other log sites: key a watcher on the text `cancels:`, never on the glyph.** `[R]` evidence: only 3 of 47 broker-reaching ledger rows ever had a broker-confirmed cancel (ids 8, 37, 102).

**Re-arm rules** (`store/armed_orders.go` UpsertArm :171): working row → refuse rewrite (:194-198) · **cancel_pending row → refuse rewrite AND refuse to mint a successor** (a cancel in flight is a live broker order; without this the mint branch fires on "not armed AND has a signal id" and places a second order beside the first) · terminal + signal ≠ → re-authorize ONLY on plan-version change (MANUAL-CANCEL-WINS :244-250) · boot-sweep rows re-arm under same version (:254-265) · canonical side UPPER at write (class 28, :181).

**Knobs:** `ARM_PLACE_TICKS=100` (:34-42) `[T]` · `ARM_WORKING_STALE_MIN=15` (:122-130) `[T]` → cancel `"absent from a fresh NT8 order_snapshot (reconciled to the broker)"` (:1453 — the `"no order_update within stale window (reconnect/reconcile)"` string this map quoted no longer exists in the code; it survives only as a `state_reason` on pre-reaper rows) · `ARM_STOP_ANCHOR_MAX_ATR=3.0` (arm_stop_anchor.go:38) `[I]` provisional · `ARM_FAR_ATR_MULT=3.0` (arm_far_counter.go:36) `[T]` warn-first · `ARMED_CANCEL_ACK_TIMEOUT_MS=2000` (:1800) `[T]`.

**Boot lines:** `"⚔️ armed_orders=on place_band=%dt stale_working=%dm test_seam=%s arm_rr=%.1f (gate-at-arm only; market-entry floor %.1f unchanged) (resting limits fill at the authorized price; stale_reeval NOT applied)"` `auto_trader_dayplan.go:64` · `"🎯 arms: bias-coherent=warn · stop-entry=… · far-arm counter=on(%.1f×ATR5m) · ledger append-only=on"` `arms_boot_line.go:14/26`, main.go:435 · `"🎯 stop-entry: slots=%s · guard=%s · unknown=%s · addon build_id=%s expected=%s match=%s"` `StopEntryBootLine` armed_executor.go:1727, emitted from the first armed cycle with a bound NT8 trader and re-emitted ONLY when the rendered line CHANGES (`logStopEntryBootLine` :1723, called :1190 — NOT hung off the class-33 sweep, which latches and is skipped when the sweep defers). The build id arrives asynchronously, so a latch on HAVING EMITTED pinned `build_id=none match=NO` for the life of the process whenever the first armed cycle beat the AddOn's first frame (the sibling 🔌 line did exactly that on 3 of its 8 observed emissions); dedupe is on the LINE, so the none→proven transition is recorded exactly once. Every field RESOLVED: `slots` from `FarSideProven(received, MinAddonBuildStopSlot)`, `guard` by probing `stopEntryGuardVerdict`, `unknown` from the verdict enum, the build half from `AddonBuildLine`.

**Accepted risk (`accepted_risk`) — WAVE A, 2026-09-05.** APPEND-ONLY record of what the broker agreed to, written from the `accepted`/`working` order_update (`trader/armed_executor.go:1205-1211` → `trader/accepted_risk_hook.go`), with the broker's own prices read from the same F12 book cutover leg 4 answers from.

- `armed_orders` stays MUTABLE and is re-composed per cycle; this table is never updated. A re-authorization APPENDS a row, so ledger and broker are both readable and the drift is a subtraction. Arm 35 is the fixture: ledger stop 29351.6284728996 vs accepted 29355 = **3.3715271 pts of ledger drift** (not slippage — the far-side stop was placed at 29355 and filled at 29355).
- Accepted prices are NULL when the book did not carry the order. An unknown accepted price is never 0 and never the ledger's number wearing the broker's name. `book_age_ms` / `book_source` record how fresh the view was.

## 6 · ENTRYGATE — the one canonical gate, both seams

**What it does:** one gate (`trader/entry_gate.go` `EntryGate` :140) called by BOTH the arm seam (`entryGateForArm` :328) and the decision path (`entryGateForDecision` :388). Fail-open contract: a leg with missing inputs SKIPS (header :27-30).

**Legs in order + exact refusals:**

| # | Leg | Refusal string | Line |
|---|---|---|---|
| 0 | plan_mode STRICT (R4) | `"entry_gate: refused: strict — …"` (4 variants) | :160-172 `[O]` |
| 1 | direction vs plan bias | `"entry_gate: %s entry against plan bias %q (plan_mode=direction)"` | :179-182 |
| 2 | scenario-direction consistency (class 48) | `"entry_gate: %s entry cites scenario %s authored %s — direction mismatch (class 48)"` | :190-194 |
| 3 | INVALIDATION (arm path, ruling 2026-09-03) | `"entry_gate: scenario %s invalidated at %s (accepted through %.2f) — <reason>"`; unavailable → PASSES (no verdict is not a refusal) | :205-226 `[O]` |
| 4 | shadow map (0C) | `"entry_gate: scenario %s condition %s is SHADOW (0C) — authored + E8-scored, never placed on any path"` | :233-234 `[O]` |
| 5 | R:R at REAL execution price | `"entry_gate: R:R %.2f below floor %.2f at execution price %.4f (SL %.4f TP %.4f)"`; floor = `min_risk_reward_ratio` (default 3.0) | :237-257 `[O]` R1 single floor (bound MNQ strategy carries 2.0 — drift D-21, conformance 2026-09-04) |
| 6 | min-SL ×ATR5m | `"entry_gate: stop %.2f too close (%.2f < %.2f = %.1f×ATR5m)"`; mult = `MinSLATRMult` 1.5 | :262-268 `[O]` owner-ruled 2026-09-02 |
| 7 | one open position per instrument | `"entry_gate: %s entry refused: %s (one_open_position, owner ruling 2026-09-03); no adds, no flips"` | :277-280 `[O]` |
| — | NO-CHASE (WARN-first, refuses nothing, A24) | — | :288 |

**ATR5m source:** `armSeamATR5m(d.Symbol)` :425 → `market.ExportCalculateATR(kernel.AcceptanceBars(bars, "2x5m"), 14)` :300-317 — **never `kernel.PlanDATRFor`** (that stores the DAILY ATR; "one gate, two ATRs" bug, no-trade-rider 2026-09-03) `[O]`.

**Telemetry:** decision-path refusals → `entryGateDecisionTelemetry` :475 → `telemetry.IncGateBlock(at.id, "entry_gate")` :478; arm path → `store.IncArmRefusal` :469.

## 7 · EXECUTOR — order placement on NT8

**What it does:** places market/limit/stop entries over the TCP wire to the C# AddOn (NT8 SIM), sizes futures contracts, cancels/modifies, and processes order updates.

- Placement: `placeEntry` `tcp_trader.go:396` · `PlaceLimitEntry` :433 · `PlaceStopEntry` :489 → `tcp_server.go:1054 SendSignal`. `CancelOrder` :554 · `ModifyBracket` :562 · `MoveStopToBreakeven` via `SendMoveStop` :1078.
- **AddOn build floor on stop entries** (`tcp_trader.go:593`, WAVE B 2026-09-05): `PlaceStopEntry`'s FIRST check, ahead of the bound-account and SIM checks. `ntwire.FarSideProven(FarSideBuildID(), ntwire.MinAddonBuildStopSlot)` — refusal `"refusing stop-entry %s %s trigger=%.2f qty=%.0f [guard=far_side_build] — addon build predates the stop-slot fix (build_id=%s, need ≥ %s)…"` wrapping `ntwire.ErrAddonBuildTooOld` so the caller counts it apart from a transport failure. Constants: `MinAddonBuildStopSlot` `tcp_framing.go:306` (the gate) · `FarSideBuildE7` :230 (SUPERSEDED, gates nothing — it proved the AddOn PARSED a stop_entry frame, not that the trigger reached the stop slot) · `ExpectedAddonBuild` `order_snapshot.go:214` and C# `VL_BUILD_ID` `VLTraderTCPClient.cs:55` move in LOCKSTEP with it. `FarSideProven` compares BYTEWISE and suffixes are not zero-padded (`-f9` > `-f12`), so a new floor must advance the ISO DATE `[O]`.
- **The entry order's price slots** (`VLTraderTCPClient.cs:1000-1005`): `Account.CreateOrder` is positional — after `quantity` come (limitPrice, stopPrice, oco, name, gtd, customOrder). `limitArg = isLimit ? limitPx : 0` (:1000), `stopArg = isStopEntry ? stopPx : 0` (:1001), passed at :1004. `[R]` Before 2026-09-05 both kinds shared one `orderPx` in the limitPrice slot with a literal 0 in stopPrice (dev :974-979): every StopMarket entry reached NT8 as `Limit price=<trigger> Stop price=0` — accepted, listed in the book, never worked, never rejected (22 post-E7 submissions, 0 fills; the ONE lifetime fill, 2026-08-30 22:32:55 CT at 29346.25 on a 28700 trigger, armed_orders id 15, was the PRE-E7 AddOn executing the frame as a MARKET order). The bracket SL at :1863-1865 (`b.Qty, 0, b.Sl`) is the in-file control and is proven correct by a live fill (2026-09-03, 29355). The entry ACTION is chosen at :975 by a case-folded `string.Equals(side, "long", OrdinalIgnoreCase)` — the ordinal `side == "long"` it replaced turned an UPPERCASE long into a live SellShort (class 77).
- **A cancel targets the ENTRY, and only the entry** (`VLTraderTCPClient.cs` `HandleCancelOrder`, BRACKET-OCO SEPARATION 2026-09-07) `[O]`. It cancels the resting entry out of `workingEntries` and does not read or walk `placedBrackets` at all — a read is one edit away from a cancel. `[R]` Until 2026-09-07 it was a TWO-part cancel: the entry, then unconditionally `SlOrder` + `TpOrder`. On 2026-09-06 23:37:02 the entry had already filled (`workingEntries.Remove` fires on the fill), so part one found nothing and ONLY the second half ran — accepted stop 29554 and working target 29623 withdrawn, position 592 unprotected 8h19m through the Monday open. This was never OCO propagation: the entry has carried an EMPTY oco group and the children their own `"<signal>-exit"` id since before the incident (class 86). Retiring protective legs is still done where that IS the purpose — the close path and the netting-flat sweep. A cancelled entry also clears its DEFERRED `pendingBrackets` note (previously only a REJECT did, so a cancelled entry left a note any later fill of that key would honour).
- **The bracket is placed on the fill, from the EVENT** (`SubmitBracketOnEntryFill(signalId, filledQty, avgFillPx)`): quantity comes from `OrderEventArgs.Filled`, not the cached `PendingBracket` captured at submit. A PART fill is bracketed for what filled and AMENDED (`QuantityChanged` + `Account.Change`) as more arrives — never cancel-replaced (that unprotects the filled part for the round trip) and never given a second pair in the same OCO group (one stop firing would cancel the other). `[O]`
- **Protective orders are GTC**, the entry stays Day (`TimeInForce.Gtc` on the `-sl`/`-tp` legs). A Day protective order is dropped at session end while the position it protects survives; an unfilled entry must die with its session. The TIF now rides the order_snapshot (`tif`) so the boot line READS it instead of asserting it. `[O]`
- **`place_protective_stop`** (`tcp_trader.go PlaceProtectiveStop` → `tcp_server.go SendPlaceProtectiveStop` → C# `HandlePlaceProtectiveStop`): places a STANDALONE stop, no bracket and no OCO group, named `"<signal>-sl"`, GTC. Gated FIRST on `ntwire.MinAddonBuildProtectiveStop` = `2026-09-07-h1` — an older AddOn has no handler, so the reconciler refuses to send rather than log a placement that never happened. Note `SetStopLoss` `tcp_trader.go` is NOT this: it writes a local map a later `placeEntry` reads and puts nothing on the wire. Before 2026-09-07 there was no way for this process to place a stop for an already-open position at all.
- **One order-state vocabulary** (`provider/ninjatrader/order_state.go ClassifyOrderState`): `Accepted`/`Working`/`Suspended`/`PartFilled` = LIVE at the exchange · `Initialized`/`Submitted`/`Change*` = PENDING · `TriggerPending` = LOCAL, held on this PC and never protection · `CancelPending`/`CancelSubmitted` = DYING (still fillable, never "accepted") · `Filled`/`Cancelled`/`Rejected`/`Expired` = TERMINAL · anything else = UNKNOWN, which is NON-terminal and takes no destructive branch `[O]`. `IsWorking()` (the flat gate's question) is "not terminal"; `IsLiveAtExchange()` (the reconciler's question) is LIVE only. The AddOn mirrors the live pair in `IsLiveAtExchange(OrderState)`. `[R]` TriggerPending appeared NOWHERE in the tree before 2026-09-07 (0 occurrences), and `unknown` sat in the terminal set beside `filled` — and the AddOn dropped Unknown orders from the snapshot entirely, so the Go reclassification was unreachable until that filter changed too (class 85).
- **SIM-only:** `isAccountTradeable` `tcp_trader.go:368` — SIM (`Account.Simulation`) AND on `NT_ALLOWED_ACCOUNTS` if set; "The LIVE/funded account is never tradeable" (:307,324); enforced at entry :333, armed :438, stop-entry :501. `assertBoundAccount` pre-submit identity invariant (A1) :399,456,519.
- **Sizing:** `futuresOrderQuantity` `auto_trader_orders.go:31` — `round(notional / (price × pointValue))`, floor 1, cap `maxFuturesContracts=2.0` (:25, "researched 2" `[R]`); notional ceiling 20×equity (`futuresMaxNotionalLeverage=20.0`, auto_trader_risk.go:14 + engine_position.go:66-73) `[X]` per census.
- `telemetry.IncGateBlock(at.id, "boot_integrity")` blocks opens when `kernel.TradingRefused()` — auto_trader_orders.go:198-210.

**Boot lines:** `"🚀 AI-driven automatic trading system started"` `auto_trader.go:833` · `"⚙️  Scan interval: %v"` :837 · `"🔧 NinjaTrader position-reconcile started (anchors entry_price to NT8 avg + clears orphan rows)"` `reconcile.go:101`.

## 8 · EXITS — stop management, suspensions, EOD flat

**What it does:** breakeven and trailing moves, EOD/T1 flattening, dormant/re-arm lifecycle.

- Breakeven: `maybeMoveStopToBreakeven` `auto_trader.go:148`, trigger `breakeven_trigger_points` default **50 pts** when unset (:201, opt-in OFF; owner-ruled ON at +40 pt per census E1 — the saved strategy carries `breakeven_enabled:true`, drift D-3, conformance 2026-09-04) `[O]`. WARN `"🎯 auto-breakeven: %s %s +%.1f pts in profit → stop moved to breakeven (entry %.2f)"` :189.
- Trailing: `defaultTrailingATRMult=2.0` × ATR(14,5m) after breakeven — auto_trader_trailing.go:22-30, rails R-A ratchet / R-B never-below-entry :94-112 `[O]` (census E2 owner-ruled 2.0×ATR14).
- **SUSPENDED by default:** `exitMechsSuspended()` returns true — `exit_mechs_suspend.go:33-41` ("suspended 2026-09-02 pending MFE data (wave 1A)"; Round-7: worst exit family of 15, 567k backtests `[R]`); env `EXIT_MECHS_SUSPENDED=0` re-enables. Note: suspension contradicts the census's owner-ruled ON position (drift D-3 — which ruling stands is open; conformance 2026-09-04).
- **EOD flat — FLAT MEANS THE BOOK, NOT THE POSITION** (`enforceEODFlatAt` `auto_trader_clock.go`, rewritten 2026-09-09) `[O]`: at the resolved close (session end − offset, half-day pull-in) it cancels ALL non-terminal arms FIRST — unconditionally, whether or not a position is open — then re-reads positions and flattens. `[R]` Until 2026-09-09 it read positions first and returned on `len(positions)==0` BEFORE the cancel, so a session that ended flat BY LUCK left every resting arm alive past the close. A failed position read logs `flatness UNVERIFIED` and never claims flat (A24). `cancelArmedOrdersSyncWith` now keys on the SIGNAL ID rather than `state == "working"`: `BeginPlacement` sets signal_id and `place_pending` in one update before the order reaches the broker, so a place_pending row always has a signal id and used to be written `cancelled` with no wire cancel at all. T1 force-flat lead 2 min (`research v5 C.5` `[R]`).
- NT8 OCO close → `position_close` frame → close-sync records realized P&L — `close_sync.go:156-166`, `"📕 NT position closed: %s %s qty=%.2f exit=%.2f reason=%s pnl=%.2f (owner=%s)"` :196.
- Flip/death → dormant + auto re-arm: `maybeRunSessionReadsAt` auto_trader_planner.go:190,315-330 (`"😴 plan %s %s v%d DORMANT — %s (entries blocked; auto re-arms when price closes back; replan budget untouched)"` :328); re-arm `"⚡ plan %s %s v%d REARMED — %s"` :287 `[O]`.

**Session risk limits (2026-09-09)** `[O]`:

- **Consecutive-loss breaker** — `trader/session_risk.go`. N consecutive losing closes in one CME session-day → no new entry on EITHER path until the roll; N resolves through `breakerHaltN` → `store.ResolveBreakerHalt` (W1, 2026-09-23, presence-aware `*int`: a SAVED value wins INCLUDING `0` = OFF `[O]`; ABSENT inherits `BREAKER_HALT_N` `[E]` (`0` = off; an invalid value falls to the default and the source says so), else `8` `[I]`), WARN at M=`5` `[I]`. `[R]` Before W1 the knob was an `int` whose `0` read as "unset → 8" while the UI and the struct comment called it OFF, and no writer could store a `0` (omitempty). The W1 conversion is FAIL-CLOSED: `store/settings_truth.go` reports every stored strategy at boot (`🩺 settings truth [<id>] … before=… after=… · CHANGED|UNCHANGED` + summary) and a stored `0` no Studio save confirmed (`system_config settings_truth_zero:<id>`, written by POST/PUT `/api/strategies` in ONE transaction with the row, carrying `saved_at`; the 🩺 line and the effective row say `OFF — confirmed by Studio save <time CT>` or `explicit 0 UNCONFIRMED — re-save in Studio`) refuses its trader at `addTraderFromStore` (`"⛔ settings truth: trader %s REFUSED at load — %s"`, naming the strategy id, the exact field and both meanings). No DB write at boot. NOT gated by the guardrails master. `[T]` On the retained tape (n=73 usable era closes) the longest run is SEVEN, ids 585-591 — N=8 never fires. An UNRESOLVED P&L **ends** a run rather than bridging it (`CountConsecutiveLossesSince`, `store/position_query.go`): bridging lengthens runs and so pushes toward a halt, which is the destructive branch (A24).
- **No-trade band on the ARM path** — `sessionEntryBlockedAt` (A28 seam) consulted once per cycle in `maybeManageArmedOrdersAt` before the scenario loop. `[R]` Previously enforced ONLY at `auto_trader_orders.go:297` on the decision path, which `plan_mode=strict` forbids from entering; `armed_executor.go` held zero references to it (class 95). An arm resting when the band opens is cancelled, not grandfathered.
- **Post-loss re-arm counter** — `store/post_loss_counter.go`, COUNTER ONLY, never refuses. Triggered on a LOSING CLOSE, not a stop-out: of 47 era losers only 4 carry `close_reason='stop'`. K=`30` min `[I]`.
- **Daily limit** — leg D (`trader/entry_gate.go`) needs BOTH `guardrails_enabled` AND `daily_loss_enabled` (`kernel/risk_limits.go`); both are OFF today, so the configured $450 `[O]` is decorative. `deskGuardrail` now requires both (it checked only the master — class 82). The trip is lifted by `MaybeResetDaily` at the CME roll (`kernel/risk_limits.go`, class 96): before 2026-09-09 nothing on the recurring path cleared it, so a trip persisted until an operator reset or a restart. `armRefusalClass` gains a `daily_force_flat` case so the refusal is countable.

**Boot line:** `"🛑 session risk: daily=… · breaker=…[O|E|I] warn=…[I] · no-trade-band=arm+decision · post-loss counter=on(…m) · flat@…=position+arms+pending"` `main.go` via `trader.SessionRiskBootLineForBoot` — `breaker=8[I]` / `3[O]` / `off[O]` / `5[E]` / `off[E]` READ from the one bound strategy; `breaker=n/a (<why>)` when zero or several strategies are bound (W1); the retained-tape clause only when N ≥ 8. Per trader at load (`manager/trader_manager.go`): `"🛑 [<trader>] breaker=… (consecutive-loss halt; …)"` and `"🧮 replan cap: strategy=N[O|I] · NY=… · ASIA=… · LONDON=…"` (`trader.ReplanCapBootLine`, resolved by `store.ResolveReplanCap`: session override → strategy → `2` `[I]`; an explicit `0` is honoured at BOTH levels since W1).

## 9 · RECONCILE — NT8 truth vs DB

**What it does:** every 20 s (`reconcileInterval` `reconcile.go:32`) reconciles DB positions/orders against NT8 TCP snapshots; fixes, freezes, or materializes.

- Entry-truth: stale `entry_price` → anchored to NT8 avg (:101 boot line) · orphan-clear: NT8 flat → close row (`orphanGraceMs=120s` :35, `flatGraceMs=60s` :41, `entryConfirmGraceMs=45s` :63) · qty divergence → A4 FREEZE after `reconcileDivergenceGraceMs=60s` :207-210 · untracked NT8 position → materialize OPEN row after 60 s :351-446.
- Netting fills (class 27): latest OPPOSITE-side fill within `nettingFillWindowMs=25s` is the real exit price, else `CloseReasonUnresolved` — netting_fills.go:31-56. Unknown-P&L reasons: `ReconcileFlat / Unresolved / TestSeam` — store/position.go:102-122.
- P&L integrity counter: `pnl_integrity_mismatch` gate block when recomputed-vs-recorded Δ > $0.50 — auto_trader_clock.go:810-818.
- **Protection reconciler** (`trader/protection_reconciler.go`, BRACKET-OCO SEPARATION 2026-09-07) `[O]`: every minute a position is open (`monitorTick`) and again on the dead-man RECONNECT edge, it asks the broker's own book whether a LIVE stop exists for the open position. None → P0 raised AND a stop placed, at the `accepted_risk` price if one exists, else the plan's composed stop from the filled arm; no price for either → P0 only, never invented. Deliberately NOT in `maybeManageArmedOrders`, which returns early unless day_plan is enabled — a position is unprotected regardless of day_plan. Three A24 unknowns each say so and do nothing: no/stale book, a protective order in an unreadable state, and a live stop the book carries no quantity for. A PARTIALLY covered position is raised, never patched (a second stop into an OCO topology we cannot see can double-exit). On the reconnect edge the answer is usually UNVERIFIED and that is correct: the AddOn sends hello/accounts/balances/positions on connect but no order_snapshot until its next 30 s beat. `[R]` C6: nothing rebuilt the stop/target pairing after a restart — `placedBrackets` is in-memory, written only in `SubmitBracketOnEntryFill`, repopulated from nowhere.

**Boot line:** `"🧷 brackets: entry-oco=<own(none)|SHARED(id)|n/a> · bracket-oco=<on-fill(shared)|MIXED|n/a> · state-source=<broker|none> · protective-tif=<Gtc|Day|n/a> · reconcile-on-reconnect=on · can-place-stop=<yes|no (addon <build>)> · unprotected-found=<n>"` `main.go` via `trader.BracketsBootLine`. Most fields read n/a at startup BY DESIGN — they describe the AddOn, which this process can only know from a received book (A11).

## 10 · P&L — columns, corrected law, expectancy

**What it does:** records realized P&L at close with the correction column; the expectancy package is a read model (never gates — expectancy/model.go:4-6).

- Columns (`store/position.go:128-200`): `entry_price`, `exit_price`, `exit_time`, `realized_pnl` :152, **`pnl_corrected` (nullable) + `pnl_correction_note`** :157-158 (corrections additive, original never edited), `fee`, `close_reason`, `source`, `mae/mfe` (wave 1A), `plan_id/plan_version/cited_scenario_id/plan_matched/plan_band/adherence_grade` :186-197.
- **Corrected-column law:** `pnl_corrected` only; NULL = UNRESOLVED, excluded AND counted — model.go:14-17 · sample-id law: every row claim names ids — `row_ids` :117-134 · `MinN=30` :37 · z=1.96 :41 · eras pre/post-0B (`Era0BStart` = 2026-09-02 07:49:06 CT) :83-90.
- Realized formula `(ExitPrice−EntryPrice)×qty×pointValue` — close_sync.go:156-166.

**Exit cause — WAVE A, 2026-09-05.** `close_reason` is written from NT8's OWN exit event, never inferred from price proximity or from the sign of the P&L.

- The wire already carried it: `provider/ninjatrader/tcp_framing.go:353` `exit_reason` = `sl|tp|manual`, parsed and used at `trader/ninjatrader/close_sync.go:204` to arm the re-entry cooldown. Both writers previously stored the literal `"sync"` (`store/position_builder.go`, `ReducePositionQuantity`'s auto-close branch).
- Mapping (`store.ExitCauseFromBroker`): `sl`→`stop` · `tp`→`target` · `manual`→`manual` · absent/unrecognized→`sync` (never a guess). The mapped words are what `expectancy/aggregate.go`'s substring test for "stop"/"target" actually matches; the raw wire tokens match neither.
- **A stop can be profitable.** Rows 557 and 570 exited at their protective stop in profit, so "stopped out" is not "lost". The eleven CEX/DEX sync paths are unchanged and still record `sync`.
- **Excursions:** `trade_excursions` is populated by the wave-1A hooks (open/bar-tick/close) plus the epoch-range backfill. Measured 2026-09-05: scanned 587, computed 68, **unrecomputable 519** (`resolution='none'`, columns NULL — the 1m tape does not reach them). `trader_positions.mae/mfe` keep `DEFAULT 0` in the DDL; the wave converted the 4 surviving pre-E4 zeros (ids 569, 579, 580, 584) to NULL. NULL = UNMEASURED; 0.0 = measured zero.

**Boot line (`📐`, `store/wave_a_migration.go` `WaveARecordBootLine`, emitted `main.go`):**
`record: touches=<n> (valid=<n> no_formation=<n> invalid:pre_formation=<n> invalid:dup=<n> legacy=<n> unclassified=<n>) · excursions=<n> (backfilled=<n> unresolvable=<n>) · exit-cause=broker · accepted-risk rows=<n> (with broker stop=<n>) · mae/mfe 0→NULL=<n>/<n> · arms live=<n> (superseded=<n> cancelled=<n> filled=<n>) · migration <state>`
The arms census is READ from `armed_orders` (class 75): cutover 2026-09-05 was blocked by leg 4 on two never-placed arms (104, 105), and their terminalization had to be visible at boot rather than only in a chat log.
One-boot migration flag `WAVE_A_RECORD_MIGRATE`; backup first (no backup, no write); idempotent; the line reports what is PENDING when the flag is off.

## 11 · WAKES — what re-plans the plan

| Wake | Trigger | Source · Label |
|---|---|---|
| scheduled session read | inside ReadCT window per session (ASIA 16:30 / LONDON 01:30 / NY 08:00 CT) | auto_trader_clock.go:98,126 → auto_trader_planner.go:190 `[O]` |
| death re-plan | plan death → capped replan (`replan_cap`, only deaths spend it) | auto_trader_planner.go:315-339 |
| MSS wake | 15m MSS event after plan birth; one per event | auto_trader_transition.go:146-197 |
| level-event wake | fresh zones/FVG/OB/iFVG/invalidation candidates | auto_trader_wake_levels.go:86-250 |
| fast-market | `\|Δprice\| > 1.5×ATR5m` since plan write (`FAST_MARKET_ATR=1.5` auto_trader_loop.go:80) | `[R]` |
| stale_reeval | superseded entry re-validated; drift ≥0.25×ATR14 discards (`discard_burn.go:38`) | `[T]` |
| kick channel | stale-dodge, post-exit one-shot | auto_trader_clock.go:890-929 |

Cadence governance (class 47): `WakeCutoffMinDefault=25` (:52), `WakeCooldownMinDefault=30` (:59) — `[R]` 7-day tape + live observation. **"wake-predicate" cutover: NOT FOUND** in production code (only a comment in `trader/wiring_gate_test.go:20`).

**Boot line:** `"⏱ wake cadence …"` main.go:345 (the full string lives in the cadence boot helper).

## 12 · CADENCE — clock and session rhythm

- Scan interval: `scan_interval_minutes` default **3**, min 3 — store/trader.go:28-29, api/handler_trader.go:451-453, agent/tools.go:2487-2488 `[X]` per census (never tape-tested).
- Cadence modes: `CadenceInterval` / `CadenceBarClose` — auto_trader_clock.go:42-50; main loop ticker `auto_trader.go:943`; bar-close gate :905; stale-dodge :921.
- Sessions (CT): ASIA 17:00→02:00 · LONDON 02:00→08:30 · NY 08:30→14:45 (see §3) — session_registry.go:83-117.
- **SESSION CALENDAR — the one owner of what a day IS** (fold, 2026-09-07): `kernel/session_calendar.json` (embedded, 14 dated rows, each citing its source) + `kernel/session_calendar.go`. Three classes: `closed` · `shortened` (+`close_ct`) · absent = normal. `SessionStateAt(now)` is the single join of calendar + weekly rules; `IsCMEOpen` / `CMEClosedReason` read it. Safe side is CLOSED in every ambiguous branch (unreadable `close_ct`, unrecognised class, uncovered year → the superseded boolean, which errs closed). Boot line `SessionCalendarBootLine` → main.go; status text `SessionDayNote` → the DESK MODE row.
- Half-days: **FOLDED IN**. `half_days.json` is DELETED and `SessionRegistry.HalfDays` removed — that fact had three owners with two key conventions and, on three dates, two different times (the gate stopped at 12:00 where the sourced file said 12:15). `EffectiveFlatCT` (session_registry.go) and `halfDayCutoffMin` / `effectiveEODFlatCT` (auto_trader_clock.go) all resolve through `kernel.SessionEarlyCloseCTForKey`. The override is PULL-IN ONLY and never earlier than the session's own `window_start_ct`. Pin: `TestFoldOneFactOneOwner` (kernel/session_registry_test.go).
- Calendar (economic events, unrelated): live ForexFactory JSON + static T1 fallback `calendar_static_t1.json` — calendar/calendar.go:33-92.
- Closed-market backoff: 3 min in 10 s slices — auto_trader_loop.go. **Exempt from the overrun warning** (`shouldWarnOverrun`, auto_trader.go): a 3-minute sleep cannot fit a 2-minute interval, so the warning was guaranteed rather than diagnostic — 165 in one day, all `3m0.0XXs > 2m0s`. `tickOnce` reports the closed path; a real overrun on a trading day still warns.
- Daily report 21:00 local, risk check 4 h — agent/scheduler.go:37-52.

## 13 · SETTINGS — registry and resolved values

- **Guide knob registry:** `web/src/guide/content/settings.ts` — dayPlan 18 knobs :7, risk 22 :277, sessions 1 :575; `KnobSpec` fields `label, where, what, trader, consumer, range, systemDefault, recommended, whenToTouch, perSession` — guide/types.ts:56-68; "9 env knobs" callout :620-669 (ARM_MIN_RR=2.0, HTF_VETO_MODE=cross, HTF_VETO_TF=1h, FAST_MARKET_ATR=1.5, BD_MIN_DISP_ATR=1.0, FVG_ENTRY_MIN_DISP_ATR=1.5, INGEST_QUEUE_CAP=1024, AI_PLAN_MAX_TOKENS=65536, PERSIST_STALL_WATCHDOG_S=60).
- **Agent-side field catalog:** `agent/entity_field_catalog.go:3-113` (trader/model/exchange fields, editability, keywords).
- **Safe defaults + hard limits** (`store/strategy.go`): `SafeDefaultMinRiskReward=3.0` :76 `[R/O]` · `SafeDefaultMinConfidence=60` :83 `[O]` · `MinRiskReward=1.0` :54 · `MinConfidence=50` :60 · MaxRR 10.0 · `ClampLimits` applies them :196-224.
- Trader table defaults: `ScanIntervalMinutes default:3` :28 · `IsCrossMargin default:true` :48 · `CadenceMode ''→interval` :30-34 · `PositionMode ''→ai_watch` :37-46 · 8 deprecated leverage fields DEAD at runtime :59-67.
- **Research-snapshot recorder knobs (dispatch 103, 2026-09-16):** `RESEARCH_SNAPSHOT` (opt-out: explicit 0/false = OFF; unset = ON (default)) · `RESEARCH_LOG_EVERY_S` (default 60 — rollup cadence; rows-per-object + drops + queue depth) · `RESEARCH_RETAIN_DAYS` (unset = never prune; set = batched boot + daily prune, no auto-VACUUM) · drop notices WARN-coalesced 1/min — researchsnapshot/runtime.go, researchsnapshot/volume.go.
- **Resolved endpoint:** `GET /api/config/resolved` — api/server.go:152 → api/config_resolved.go (same resolvers the engine calls).
- **Boot line:** `"⚙ knob registry …"` main.go:434.

## 14 · UI — pages, guide, endpoints

**THE DESK STRIP** (2026-09-06) `[R]`: `GET /api/desk` (read-only, protected, writes nothing) returns TWELVE rows rendered server-side — MODE · POSITION · PROTECTION · DRIFT · TARGET · DAY · ARMS · BOOK · FEED · PLANNER · RANGE · LAST FILL — each carrying its value, unit, SOURCE, as-of instant, age and a `verified` flag (`trader/desk_facts.go` `DeskStripAt`, `api/handler_desk.go`). Mounted at the TOP of PlanCard above AlertCenter (`web/src/components/plan/DeskStrip.tsx`), expanded by default, collapse held in COMPONENT STATE (artifacts forbid browser storage). **The browser computes nothing** — a row it did not receive it cannot invent. **Its law:** nothing renders undated · UNKNOWN is a value and carries its reason, never 0 and never a dash · no green word for a compound question (row 1 names process, feed, link and book separately, because `/api/health` answers only the first — it stayed green through 113 min of silence on 09-03) · a stale source is AMBER with its age, never the last value silently · **the ledger's price is never shown as the broker's** (PROTECTION renders the ACCEPTED stop from `accepted_risk` or UNKNOWN; DRIFT carries the difference — arm 35 was ledger 29351.628 vs accepted 29355 = 3.371527 pts). Row 8 is cutover **leg 4 on screen, continuously**, reusing `CutoverGateStatus()` rather than re-deriving it. **Cadence** is resolved server-side: 5s while a position or arm is live, 15s otherwise — decided on the ARMS row's STATE, never a substring of its text ("none resting" contains "resting"). **Boot line** `"🔭 desk strip: lines=<n> · unknown=<n> · cadence=5s live / 15s idle · book-age-bound=<d> · …"` — `DeskBootLine`, main.go; the UNKNOWN count is per-REQUEST so at boot it reads `n/a`, never 0. **🔭 not 🖥**: the screen glyph already belongs to the UI-serving/dist-staleness line (main.go:310).

**Not enforced, and now labelled** (D6d): `GET /api/risk/status` publishes `max_notional_usd` (the futures-unaware CRYPTO cap; the pre-prompt gate passes 0 for both notional args at `kernel/engine_analysis.go:118-136`, and notional is enforced at EXECUTION time as equity × max_notional_leverage — a different number), `kill_switch_armed` (derived from the env limit being non-zero, not an armed state) and `daily_loss_limit_usd` (the ENV value; a Studio guardrail supersedes it). The response now carries a `not_enforced[]` naming each. The desk strip's DAY row renders the RESOLVED enforced limit with its source instead.

**Pages** (web/src/pages/): AgentChatPage (chat: ticker, positions, trader status, messages) · BeginnerOnboardingPage · FAQPage · SettingsPage (exchange/telegram/model modals + ResolvedKnobPanel) · StrategyStudioPage (plan/risk/indicator editor) · TraderDashboardPage (header reads `PROCESS::RESPONDING`, relabelled 2026-09-06 from `SYSTEM_STATUS::ONLINE` — /api/health answers one question and the word implied all of them; charts, DecisionCard, DecisionAudit, pause, EmergencyFlatButton, PositionHistory) · PageNotFound.
**HeaderBar tabs:** Agent (Beta) · Config · Dashboard · Strategy · 📖 Guide — HeaderBar.tsx:112-140 (mobile duplicates ~:450-510).
**Guide:** 14 sections (welcome…expectancy) — GuidePage.tsx:24-40; drift check vs `/api/health` revision, 12-char prefix compare :455-477; `GUIDE_BUILT_REV` stamped by `web/scripts/stamp-guide-rev.sh` (never hand-typed) — types.ts:6.
**i18n:** en/zh/id — translations.ts:1; agent Go side zh/en — agent/i18n.go:3-83.
**Design system:** `--nofx-*` app chrome vars (index.css:18-66) + `--vl-*` Plan-Card tokens (theme/vl-tokens.css) + tailwind palette (tailwind.config.js:10-31). These are invisible identifiers and remain unchanged by Dispatch 102.
**Endpoints:** `GET /api/health` `{status,time,revision}` — api/server.go:625-631 · `GET /api/cutover-gate` — :430-434 → class33_cutover_gate.go:58 (5 legs: db_open_positions · api_positions · nt8_positions_snapshot · working_orders · planner_in_flight; `ready` only if ALL pass).
**Boot lines:** `"🖥 ui: served-by=go-static build=…"` (or STALE warning) — api/ui_serving.go:110-146, main.go:308-308.

## 15 · DEPLOY — units, lock, cutover, boot integrity

**Units** (deploy/): `nofx.service` (template, `ExecStart=__NOFX_DIR__/nofx-bin`, Restart=on-failure, journal-only — nofx.service:42-47) · `nofx-web.service` (Vite :3000) · user units `nofx-backup.service/.timer` (OnCalendar 05:00 + 17:30, Persistent) · `nofx-clock-guard.service/.timer` (OnCalendar `*:0/15`). Backup retention `KEEP_DAILY=14`, `KEEP_WEEKLY=8`, layout `~/nofx-backups/auto/{daily,weekly}` — nofx-db-backup.sh:17-23. journald dropin `SystemMaxUse=2G` — journald-nofx.conf:16-19.

**Main-tree lock** (`deploy/nofx-lock.sh`): `LOCK_DIR=~/nofx-main.lock.d` (:43) · atomic `mkdir` acquire (:73) · owner-scoped heartbeat every 120 s (:46) · STALE after 300 s, never "dead" (:45) · `check` rc 0 free / 1 held / 2 stale (:133) · `reclaim` refuses fresh heartbeats, appends history, rc 3 (:173-200) · `release` holder-only (:203).

**Cutover** (`leveltruth-cutover.sh`): FLAT-GATE (DB OPEN=0 + NT8 count=0 snapshot in last 5 min) → RELEASE marker = build sha → binary swap + `kill -9` (SIGTERM exits 0, no relaunch) → poll ≤90 s for `BOOT INTEGRITY OK`.

**Boot integrity** (`kernel/boot_integrity.go`): `NOFX_EXPECTED_REVISION` env wins, else first line of `deploy/RELEASE` (:86-98); prefix match vs embedded `vcs.revision` (:102-118); 3 prompt goldens re-rendered (:92-127 golden_selfcheck.go); latches `tradingRefused` (:122-158). **Boot lines:** `"🔐 BOOT INTEGRITY %s — rev %s%s · built %s · expected %s · goldens %s"` :73 · `"🔐 TRADING REFUSED — %s"` main.go:293 · `"🔐 No new positions will be opened until this is fixed and the bot is restarted."` :289 · banner `"║           🚀 NOFX - AI-Powered Trading System              ║"` :44 · `"🔑 JWT secret configured"` :141 (fires unconditionally; verify override via `grep JWT_SECRET .env`) · `"🛡 clock-guard [boot] rtc_vs_go=%s timer=%s last_check=%s%s warn_ms=%d tolerance_ms=%d resync=unavailable-no-root …"` clock_health.go:164 · `"🕰 clock-health [<tag>] go=… nt8_last_bar=… drift_ms=… timesync{…} tolerance_ms=…"` :80.

---

*Map generated 2026-09-04 from code @ `492d2067` + settings registry + the 2026-08-30 knob census and 2026-09-02 belief census, then aligned with the 2026-09-04 research-conformance corrections (D9 swing seats [T]-positive, min-SL [O], breakeven/trailing [O]-ruled-but-suspended, R:R 2.0-vs-3.0 drift). Drift found and recorded: `BD_MIN_CLOSES` 1 (was 2), `MinSLATRMultDefault` 1.5 (was 1.0), code breakeven default 50 when unset (owner ruling 40), OR = first 5 min (IB = first 60 min), no wake-predicate cutover in production code.*

### Confirmation truth (fix/confirmation-truth)

`EvaluateBucketClose(open, minutes, now)` is the shared bucket-end predicate for confirmation and waterfall validation/facts. It resolves the canonical bucket's actual end and accepts equality at that end; it never counts source minutes to satisfy a 5m condition. `AcceptanceRunEver` and `BreakdownContinueState` use this same answer. Aggregation-only ATR/structure consumers retain their existing data inputs. `ConfirmReferenceInstant` returns `(instant, ok)`; ordered scenarios accept part two only after a known first event, with UNKNOWN for missing evidence and no publication fallback. Touch OHLC establishes an event by the closed minute's end, not an invented tick time. Waiting arms and decline telemetry now read the entire scenario verdict.

Immediate-mode validation uses the explicitly separate `Evaluate1mDisplacement` / `1m_displacement` observation for its displacement floor; its void judgment remains a completed-5m judgment. Void and displacement feeds-forward read corrected five-minute facts; `BreakdownState.ReclaimedAt` supplies the actual judged reclaim close rather than a separate minute scan. Subsequent planner authoring may differ. No condition vocabulary, level scoring, entry gate, stop composition or cadence policy changes.

Versioned `scenario_meta.confirm` stores outcome, bucket open/end/closed, evaluation time, reference provenance and ordered event evidence. The plan chip and the thirteenth desk row show this record, separately from ≈ activation and order authorization. The desk never recomputes it. The boot line reads enforcing policies, process refusal counters and an embedded, test-checked **audit** receipt: 23 validation REJECT→PASS observations across two scenarios; 627 evaluated and 162 unevaluated scenarios. These frozen replay counts are explicitly distinct from live counters. See `reports/2026-09-08-confirmation-truth.md` for corpus identities, coverage and owner ruling.

### Plan liveness (fix/plan-liveness)

The existing `recordScenarioStateAt` recorder writes status/meta and first-observed invalidation records under plan ID + version + scenario ID. The record retains the judged anchor and price; `scenarioInvalidationResolverClock` reads only matching version/anchor evidence. Legacy unversioned stamps remain untouched and are not imported. The card and PLANNER desk line read versioned status snapshots through `store.ScenarioLivenessFor`; missing, unknown or stale data renders UNKNOWN. Recorded invalidation is history, distinct from the reversible current evaluator status.

`validateAuthoredScenariosAt(doc, session, date, read, now)` is called inside the existing planner candidate retry loop after the model returns; it wraps the pure `kernel.EvaluateBornCheck`. **W-EXEC-TRUTH W2 A1:** an `invalid` sentence outside the grammar (`authoredCloseRule`; the prompt states the four canonical forms `5m|2x5m close above|below <price>` and one placeholder example, class-38 row) is a GRAMMAR unknown and is REFUSED — every offending sentence quoted in ONE error opening `invalidation grammar refusal`, one counted `authored_grammar_refusal` event per scenario, repair law `RepairInvalidationGrammarLaw`. A TAPE unknown (missing/malformed/duplicate minute) is still accepted with a warning and `authored_unknown`. **A2:** the rule is judged on every 5m group whose close B satisfies read < B ≤ publish plus the latest completed group (`AuthoredBornGroups`, canonical `EvaluateBucketClose`, all five minutes per group); the read clock is `PlanFacts.ReadAt` (= `input.Now`; zero on legacy facts-less callers → latest window only, read n/a). **D5:** the plan's death{} met or flip{} fired in the same span refuses (raw line, rule, side; the runtime buffer/touch gate/flip hold are unchanged). The accepted attempt's record — policy (`kernel.AuthoredInvalidationPolicy()` = `enforced (grammar)`), clocks, judged group opens, verdicts — lands on the NULLable `plans.read_clock_ms / publish_clock_ms / born_check` columns and, for a refused attempt, in the liveness event's `born_check`; the card reads it as `authored_invalidation`. Evidence: plans rowid 455 (four grammar unknowns accepted pre-W2, log_events 92916–92919; death met by 01:35 31081.00 + 01:40 31090.75 before publication) and rowid 452; earlier replay plan row 265 / bar 451050 and constituent rows 451031, 451034, 451037, 451039, 451051. This does not change scenario verdicts, EntryGate legs, cadence, replan budgeting or execution.

Exhaustion is **warn-only**, once per version in the existing recorder; it does not wake or bypass any throttle/cutoff. `PlanLivenessBootLine` reads recorded event counts, says tradeable=n/a before an active snapshot, READS `invalidation: <kernel.AuthoredInvalidationPolicy()>` and `grammar refusals=N` (authored UNKNOWN is tape-only from W2; pre-W2 events mix both), and warns rather than panicking when counters cannot be read or event identity generation fails.


## Stage A · Research snapshot (record only)

Implementation branch `fix/stage-a-snapshot`; not a live receipt. `researchsnapshot/`
provides a separate append-only SQLite archive at `<configured DBPath>.research.db`.
`main.go` installs its bounded worker before loading traders. Five object tags share
`research_facts`: market, candidate, plan, scenario, exec. Nullable epoch-millisecond
columns distinguish observation, receipt, publication and permission. Payload fields
are explicit JSON null (SQL `json_extract` NULL) with missing-field explanations;
computed zeros remain numbers. No historical trading rows are rewritten.

Capture points: scorer computation and actual cut branches in `kernel/levels_score.go`
and `levels_assemble.go`; planner inputs/retries/publication in
`trader/auto_trader_planner.go`; existing scenario verdict metadata in
`trader/auto_trader_levelstate.go`; received NT8 frames in
`provider/ninjatrader/research_wire.go`. Producer decisions never read this archive.
`cmd/research_export` uses read-only SQLite and receipt-time [from,to) membership,
with stable row ordering, per-object counts, writing revisions and SHA-256.

Boot join key: `🗄 research snapshot:`. Schema and rows/nulls come from the archive;
objects from the schema registry; drops and admission latency from the worker.
Unmeasured latency or inaccessible data prints UNKNOWN. The queue admission budget
is `researchsnapshot.OfferBudget`; full/late/faulted work drops with a counted WARN.
This is admission latency, not a claim that all capture overhead is already measured.
Live counts, per-read overhead and final field coverage remain deployment proofs.


Stage A recording coverage update: actual EntryGate return values are observed at
its two callers, without editing EntryGate or its legs. `store/armed_orders.go`
records the existing `ExpirePlacement` write; pending state remains held. The
closed analytics caller records only `pnl_corrected` and explicit exclusions.
Candidate episodes reuse `recordDetectorOutputs` results. The source registry and
NULL limitations are documented in the Stage A report. Every producer admission
and worker write contains recorder panics; the archive never supplies a trading
verdict. Offline 72-candidate scoring added 0.027760 ms at the median of three
1,000-call runs; whole live read latency remains unproven until deployment.


Research price classification: raw fills and book order prices remain retained;
entry-specific fields stay NULL until their role is established. Admission p50
prints an upper bound at microsecond resolution, with UNKNOWN on overflow or no
measurements. The guarded stop-entry source coordinates above were refreshed after
the added recording calls; no stop-entry predicate changed.
### Scenario economics (fix/scenario-economics)

New model output enters `ParsePlanDocCapped` → `parsePlanDocument(newAuthoring=true)` → `validateNewScenarioEconomics`. Stored-plan readers use `ValidatePlanDocWithCaps`; absent economics remains UNKNOWN and is never backfilled or refused. The new-authoring parser requires the complete contract regardless of any model-supplied version, and stamps the accepted contract version itself. Existing plan JSON persistence carries the object without schema migration or new recorder hooks.

`scenarioEconomicsIssues` checks target-path membership (one MNQ registry tick, or explicit exception), obstacle beyond target, and declared R against geometry in price units. The arm's entry/stop/target remain authoritative; a non-armed scenario may carry hypothetical geometry without authorizing an order. `scenarioRoleWarnings` diagnoses recognized role differences only and never refuses them. Sub-1R obstacles are facts, WARN only. Counters record complete new-authoring observations under a mutex (path coherence means membership or a declared exception), including retries, since boot; no legacy or trade count is inferred. `LogVolumeWaveBoot` reads `ScenarioEconomicsBootLine` from the enforcing policy and counters; the line explicitly says `legacy UNKNOWN by design`.

The card's `ScenarioEconomics` block and desk SCENARIOS line display obstacle provenance/response and both R values, keeping authored geometry separate from broker prices. Legacy R/obstacle fields remain UNKNOWN. No target family, R:R floor, stop floor, armable set, arm composition, EntryGate, confirmation, liveness, cadence or executor behavior changes. C5 uses `E=p*b-(1-p)-c`; C6 floor-binding is NOT ESTABLISHED and dropped.


### W7 level-state clock boundary (class 60)

`recordLevelState` is a single-statement delegate to `recordLevelStateAt(now)` and is listed in `clock-seams.list`. The predicate body is unchanged apart from receiving its clock. The W7 consumed-level fixture uses explicit clocks and one completed-hour acceptance sequence, with an asserted activation-window prerequisite; this corrects a pre-existing test failure around the 17:00 CME day boundary and changes no trading behavior.


### Stage A archive startup repair

`researchsnapshot.Open` and `OpenReadOnly` resolve filesystem paths with `filepath.Abs` before constructing the escaped SQLite file URI. The production default `data/data.db.research.db` must resolve relative to the service working directory, not serialize as a URI authority. The startup pin calls `Start` and `CurrentBootLineAt`, verifies the actual archive schema and persisted row count, and reads the same file through the relative export opener. Absolute-path-only fixtures missed the deployed failure. A failed startup still WARNs and prints schema=UNKNOWN; no schema value is fabricated. The 18:11:54 CT boot of 6f677b55 proved scenario-economics live but Stage A unavailable.

### Arm state and cutover order classification

`store.IsTerminalArmState` owns the arm lifecycle classification; `TerminalArmStateSQL` and its negation are generated from the same table. Store liveness queries, arm readers and the boot census use it. Pre-boot selection uses the named `SweepableArmStateSQL`: non-terminal MINUS cancel_pending. The settlement pass owns cancel_pending and only confirms cancellation with a persisted snapshot id; the sweep must not bypass that evidence through raw SetState. Read-only scripts obtain the SQL through `go run ./cmd/arm-state-sql` or `scripts/arm_state.py`; they do not retype state lists. Unknown/NULL states remain non-terminal.

`Leg4FromBrokerAt` splits authorized `armed` rows with no signal id from placed/unconfirmed rows. The former are reported separately (`armed_unplaced`) and do not fail the leg. `place_pending` remains working/unconfirmed; broker-only orders and missing broker placements fail. The received broker snapshot still supplies order truth. The class-33 sweep does not gain scenario-validity adoption in this wave; it still cancels selected prior-process placed rows. The boot's `arm placement census` reads the same classification and prints UNKNOWN on a failed ledger read.

### The episode contract (fix/episode-contract, classes 109/111)

RECORDING ONLY. No rule, threshold, gate, order, plan content, level score or
surface behaviour changes; the wave adds columns to `touch_outcomes` and fills
them. Every added VALUE column is a pointer — NULL means NOT CAPTURED, never zero
and never false. The one exception is `ScenarioLinkBasis`, a plain string,
because `ResolveScenarioLink` always returns a basis: on a row this wave wrote it
is one of the four constants, so an EMPTY basis means a row written before this
wave and is the pre-wave marker, not a missing reading.

**The unit.** A touch was already the row; what it lacked was the ladder above
it. `store/opportunity_outcome.go` defines five rungs — `never_reached`,
`reached_declined`, `confirmed_not_armed`, `armed_not_filled`, `filled` — and
`OpportunityOutcomeFor` derives them from four booleans in one switch, highest
rung first, so the set is exhaustive by construction rather than by a default
branch. `store/opportunity_close.go` selects on NULL outcome (idempotent by
predicate, not by a flag) and calls `factsFor` PER ROW; a batch cannot smear one
row's facts across its neighbours.

**The link is a heuristic and says so.** A touch has a price, not a scenario id;
nothing in the plan path stamps one. `store/scenario_link.go` resolves the
nearest anchor within a band and records `ScenarioLinkBasis = price_proximity`
with the distance in points AND in Δ. The column is `scenario_nearest`, never
`scenario` — a name that would read as fact. Two anchors inside the band yield
NULL with basis `unresolved:two_scenarios_within_band`: ambiguity is recorded as
ambiguity, never resolved nearest-wins. Nothing close yields
`unresolved:nearest_outside_band`; no plan at the seat yields
`unresolved:no_scenario_at_seat`. A zero Δ leaves `dist_delta` NULL rather than
dividing. `trader/scenario_anchor.go` takes `Confirm.RefPrice` when present and
falls back to `Arm.Entry`, counts what it cannot anchor, and sizes the band from
`kernel.LevelClusterTicks` — the map's own clustering distance, not a new knob.
`trader/auto_trader_planner.go` now parses the `latest.Doc` it previously fetched
and discarded. The identity wave — a real scenario id on the touch — comes after;
this is the labelled stand-in, and the label is the point.

**The attainable entry.** `store/attainable_entry.go` orders MEASURED before
ASSUMED: `observed_fill` when a fill exists, then
`resting_limit:assumed_fill_at_entry` and `stop_entry:assumed_fill_at_trigger`
for the two arm shapes, then `first_tradeable_after_confirm`. Never confirmed and
never armed is `none:never_confirmed_never_armed`; confirmed with no price on the
record is `not_captured:confirmed_but_no_price_recorded` — a different fact from
`none`, and stored differently. The level price is never a branch: what the level
said is not what the tape offered.

**The backfill recomputes nothing, and that is the finding.**
`store/opportunity_backfill.go` is three-state — `unrecomputable:no_formation`,
`unrecomputable:no_scenario_link`, `unrecomputable:terms_mutated_in_place` — and
on the live archive it recomputes ZERO historical rows. Measured on
`data/data.db` at 2026-09-10: 4,860 in-era rows examined, 0 pre-era untouched,
4,677 `unrecomputable:no_formation`, 183 `unrecomputable:no_scenario_link`,
**0 recomputed**. `formed_at_ms` is present on 183 of 4,860 rows — 3.77%,
measured, not the research's 26% — and those 183 are exactly the rows that then
fall at the second gate, because no historical row carries a scenario link at
all. `armed_orders` is a
state row mutated in place rather than an event log, so historical terms are
unrecomputable by construction and are marked so rather than reconstructed from
`updated_at`. `trade_excursions` is UNIQUE(position_id) — filled-only — and is
the wrong key for an opportunity; it is not extended. The zero is reported as
the research's claim MEASURED, not as a failure of the backfill.

**Boot line.** Join key `🎫 episodes:`. Open and closed-today counts, the
five-rung breakdown, and the backfill's recomputed/unrecomputable pair are READ,
never literal. The detector scope names its RESOLVER, not a value —
`Δ=resolved-per-read (kernel.MeanAbsIncrement, the tape's own scale)` — because Δ
is per-read and a number printed at boot is a literal by another name; `k` and
`H` print `[I]` for their env source. `store` cannot import `kernel` (kernel
imports store), so `store/episode_detector_scope.go` reads DETECTOR_K and
DETECTOR_HORIZON_BARS through the identical env names — one source, two readers
by name, which is the closest this dependency direction allows and is stated
here rather than left to be discovered.

**Not done, deliberately.** No scenario identity on the touch. No episode table —
the ruling was to extend `touch_outcomes`, and a second table would have been a
second key for the same unit. No historical terms. The ordinal was already true
and was dropped rather than re-implemented.


### Fade permission (fix/fade-permission, W2 — a LABEL, never a gate)

RECORDING AND SHOWING ONLY. No gate, arm, order, scenario, level, exit or cadence
changes. `trader/fade_no_refusal_test.go` fails if any file on the
arm-authorization path (`armed_executor.go`, `entry_gate.go`,
`auto_trader_orders.go`, `session_risk.go`, `kernel/risk_limits.go`,
`plan_authored_invalidation.go`, `engine_analysis.go`) can reference the label
— the same seven files C1 measured as holding zero `day_type` reads.

**The predicate** `kernel.FadePermissionAt(now, facts)` is pure: no clock, no
store, no globals. Five pre-declared exclusions, `[I]`, evaluated independently
with ALL that fire named: `or_wide` (OR > k× prior-session median; k from
`day_plan.fade_or_wide_k` else the C5 default 1.28 = p80/median, n=13),
`ib_held` (beyond the IB and holding a CLOSED 5m bucket — CONTINUOUS, by owner
ruling, because the dispatch's "by 09:30" deadline was blind to 09-03 whose
break came at 10:00), `beyond_map` (past every seated reference in the
scenario's own direction), `t1_news` (UNKNOWN when no real calendar slice; UNKNOWN
never excludes — the gate's static fail-closed fallback is right for refusing and
wrong for a label), `first_n` (reuses `kernel.FirstNoTradeMinutes()`).
`day_type` is NEVER an input: it is model-worded free text (ten distinct values in
the corpus) and `TestFadeFactsCarriesNoDayType` reflects over the struct.

**The facts builder** `trader/fade_facts.go` is the only production assembler of
`FadeFacts`, and where the research law bites: bars opening at or after `now` are
not read, a 5m bar counts as closed only once `now >= open+5m`, the completed
session is never consulted. `now` is passed in — pinned by PATH (class 113),
because the predicate's purity would prove nothing if the builder reached the wall.
The prior-session OR median comes from `store.BarHistoryStore.PriorSessionORMedian`
and returns the n it FOUND (13 at this boot; the dispatch asked for 20).

**The stamp** lands on W1's episode row as four additive NULL-able columns —
`fade_permitted *bool`, `fade_exclusions`, `fade_measured` (JSON), `fade_evaluated_ms`.
`*bool` not `bool`: NULL means NOT EVALUATED, which is a different fact from
"permitted", and a plain bool would have rendered every historical row permitted
in one migration. FIXED AT OPEN by predicate (`WHERE fade_permitted IS NULL`), not
by flag: a second stamp is a no-op. The call site is `detector_record.go` at episode
open with the clock set to the episode's own open; `recover()` pinned (A10).

**The surface.** `/api/plan/today` carries `fade_permission` (per-scenario, live at
`now`) and `fade_counter` (session-day counts READ from the table). The card's
`FadePermissionChip` renders three states; an ABSENT label renders `not evaluated`,
never `permitted` (vitest pins). The desk strip's SCENARIOS line carries the chip
per scenario and the counter line. Live vs durable are labelled as such.

**Boot line** join key `🚦 fade permission:`. Counts READ; k names its resolver
(`[I:strategy]` or `[I:default:C5 p80/median]`); the three-state backfill's counts
(`recomputed / unrecomputable / untouched`); and the coverage note in the line's own
text.

**The finding this wave carries** (owner's headline): **on the one day we have,
the label would have PERMITTED the damaging trade.** 2026-09-03 NY authorized
three arms (ids 35, 36, 37); the one that FILLED (35, short 29285.00 at 09:02) is
covered by none of the five — OR was 0.77× median, the IB did not exist until
09:30, and price never cleared the authored map because the planner re-seated
ahead of price all morning (v4 29375.25 → v5 29539.38 → v6/v7 29619.50). E3's null,
stated in advance.

**A15, not fixed here:** (c) is structurally blind to a planner that re-seats ahead
of price — a finding for the planner. "Fade" has two definitions in the codebase
(by condition 157/269, by direction-vs-bias 64/269); the stamp labels every
scenario so it sidesteps this, the counter counts episodes not fades. The
corrected rulebook (PR #99) is NOT on dev — its §A sentence is in the report.
