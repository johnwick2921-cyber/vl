> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Weekly-Bias + Planner-Eyes Wave — 2026-08-30

- **Branch:** `feat/weekly-bias` (off `dev` f08a300a), worktree `/home/hoang/vl-weekly`
- **Commit:** 2ed6a1704b77a0232242adf34bfb2f90509dd7d5 — https://github.com/johnwick2921-cyber/vl/commit/2ed6a1704b77a0232242adf34bfb2f90509dd7d5
- **Status:** built, gated, pushed — WARN/shadow only. NO deploy, NO C# changes, NO gate changes, NO DB writes outside the code path (no live DB touched).
- **Law recap honored:** main tree untouched (all work in the worktree); SIM-only; no hard gates added; guide law satisfied in the same PR (no `GUIDE_BUILT_REV` bump — the cutover marker does that).

## What shipped

**W2 — Sunday weekly read (planner seat).** `trader/auto_trader_weekly.go` wires into the EXISTING cycle above the session gate (same hoist as `maybeFetchCalendar`, `auto_trader_loop.go`). `weeklyReadVerdict` (`:93`) is the pure decision: before the week's `WeeklyReadDeadline` → wait; after with no doc → read once (boot-backfill); with a doc → skip forever (idempotent). `runWeeklyRead` (`:150`) loads STORED 1m bars (`store.BarHistory().BarsBetween`, `:71`), builds the prompt (`kernel/weekly_prompt.go` `BuildWeeklyPrompt` `:276` — sections EXACT per spec W2.2: Weekly candles via `WeekCandle.RenderRow`, Weekly references, NWOG table, IPDA, Prior week recap, Instructions, exact JSON schema), calls the SAME planner client path (`resolvePlannerClient` + `CallWithMessages`), validates fail-closed (`ValidateWeeklyDoc` `:225`, r1–r6), retries ONCE with the reject reason appended, and on second reject logs `⚠️ WEEKLY READ FAILED` and stores nothing (fail-open downstream). Accepted docs stamp `facts_hash` (sha256 of the rendered facts sections, `WeeklyFactsHash` `:128`) and store as a plans row with `session='WEEKLY'`, `trade_date` = the governing Monday, `lifecycle=active` — no new DB columns. Every outcome logs `📅 WEEKLY READ …`.

**W2b — planner gets eyes.** `FormatCandleTable` (`kernel/engine_prompt.go:812`) is extracted from `formatTimeframeSeriesData` (which now calls it — no second formatter). `BuildPlannerCandleTables` (`kernel/planner_prompt.go:297`) renders 12×15m · 12×1h · 8×4h · 8×daily from the 1m slice via `kernel.AggregateBars`/`DailySessionBars`; `PlannerInput.CandleTables` feeds the `## Candles (oldest→latest)` section (`planner_prompt.go:346`). Playbook line appended: "Candles are ground truth for structure…". `planner_candle_citations` counts scenario prose lines containing "per candles" at the write site, logged once per read; counters in `telemetry/weekly.go`.

**W3 — session injection (soft law, fail-open).** `## Weekly Context` always renders (`planner_prompt.go:354`), `WEEKLY: none` when no doc; playbook counter-weekly guidance appended. Executor one-line context via `StrategyEngine.SetWeeklyContext` + per-trader `WeeklyDoc` provider (`plan_render.go`) rendered in `engine_prompt_futures.go` (both layouts). No gate touched.

**W4 — mid-week invalidation watch.** `maybeCheckWeeklyInvalidation` (`trader/auto_trader_weekly.go:258`) runs in the EXISTING cycle; pure predicate `WeeklyInvalidationCrossed` (`kernel/weekly_prompt.go:343`) on closed basis-TF bars. Crossed → NEW appended WEEKLY plan version with `bias→neutral` + `invalidated_at` (plans rows are immutable), loud `📅 WEEKLY INVALIDATED <old-bias> @ <px>`; guard flag makes it once per week; never auto-flips.

**W5 — shadow mode (real system frozen).** 5.1 `weeklyConfluenceShadow` (`:324`) logs `🌗 SHADOW wk-confl` once per level per session + a per-session `🌗 SHADOW wk-seating` reorder line. 5.2 `weeklyCounterShadow` (`:384`) logs `⚖️ WEEKLY-COUNTER` clauses only where the hypothetical Sep-9 rules would change the trade; counters `weekly_counter` / `weekly_counter_block` / `weekly_counter_resize`. 5.3 `DrawAlign` column on `decision_records` (`store/decision.go:69`, AutoMigrate additive) tagged toward_draw|away|neutral on every row via `saveDecision`. 5.4 THE LAW: fixture proves zero effect.

**W6 — knobs.** `kernel/weekly_knobs.go`: `WEEKLY_READ_CT` (default "sun 16:30"), `WEEKLY_CONFLUENCE_BAND_ATR` (0.25), `WEEKLY_SHADOW_MULT` (1.5), `WEEKLY_COUNTER_MODE` (warn), `WEEKLY_INVALIDATION_TF_DEFAULT` (1h), `PLANNER_CANDLES` (on). Garbage → default, mirroring `persistWatchdogSeconds()`.

**W7 — UI + guide.** `web/src/components/plan/WeeklyChip.tsx` (4 states: bull/bear arrow + conviction dot + draw px · grey none · strikethrough invalidated · thin history; tooltip = 3-line narrative), mounted on `SessionPlanCard`, payload from `/api/plan/today` (`weeklyPayload`, `api/handler_plan.go:34`). Guide section `web/src/guide/content/weeklyBias.ts` (num 13): Sunday-read cards, Tier-A/closed-bars, day-of-week ban, WARN-never-blocks + Sep-9 promotion, the 6 knobs, invalidation semantics + Planner candles card. `GUIDE_BUILT_REV` untouched.

## S-list

- **[A] Boot-backfill idempotence** — `weeklyReadVerdict` pure (`auto_trader_weekly.go:93-100`): past-deadline boot with no doc → read once; doc present → skip. Fixture `TestWeeklyReadVerdictBootBackfillIdempotent` proves all three verdicts.
- **[A] W5.4 THE LAW (zero real effect)** — `TestWeeklyShadowZeroRealEffect` (`kernel/weekly_shadow_test.go`) runs the FULL shadow pass between two `AssembleScoredLevelsMinGrade` calls and asserts byte-identical seated output; shadow funcs never touch the seating path (`WeeklyShadowReorder` builds its own score slice).
- **[B] Planner reuse of the executor formatter** — `formatTimeframeSeriesData` now delegates to `FormatCandleTable` (`engine_prompt.go`), so the two tables cannot drift; parity fixture `TestFormatCandleTableVolumeFlag`.
- **[C] Shadow promo thresholds (W8)** — n=15 counters / ≥10 reorder cases are pre-registered hypotheses; no live data existed to size them at wave time (shadow counters start at 0 on deploy).

## W8 — Sep-9 promotion table (pre-registered)

| Rule | Promote to HARD only if |
|---|---|
| P1 counter-trend rules | `counter_n ≥ 15` AND counter expectancy < aligned expectancy (`pnl_corrected`) |
| P2 shadow confluence → REAL scoring | shadow-reordered seatings improved touch outcomes (`level_stats` join) on ≥10 reorder cases |
| P3 planner-candles stays ON | candle citations (`planner_candle_citations`) correlate with plan quality |
| else | knobs stay warn/off — no crowns below n |

## Knob census

| Knob | Default | Resolver |
|---|---|---|
| WEEKLY_READ_CT | "sun 16:30" | `kernel/weekly_knobs.go WeeklyReadSpec/WeeklyReadDeadline` |
| WEEKLY_CONFLUENCE_BAND_ATR | 0.25 | `WeeklyConfluenceBandATR` |
| WEEKLY_SHADOW_MULT | 1.5 | `WeeklyShadowMult` |
| WEEKLY_COUNTER_MODE | warn | `WeeklyCounterMode` |
| WEEKLY_INVALIDATION_TF_DEFAULT | 1h | `WeeklyInvalidationTFDefault` |
| PLANNER_CANDLES | on | `PlannerCandlesEnabled` |

All garbage inputs fall back to the default (fixture `TestWeeklyKnobs`).

## Fixture inventory (proving lines)

- weekly-agg twins · current-week-excluded · NWOG · IPDA · depth-guard — W1, `kernel/weekly_bias_test.go` (pre-existing, still green).
- validator r1-r6 — `kernel/weekly_prompt_test.go`: r1 "bias enum — got", r2 "basis non-empty", r3 "draw-not-a-reference", r4 "narrative >3 lines", r5 "day-of-week token", r6 "thin_history + high conviction".
- boot-backfill idempotent — "boot-backfill fires on a post-deadline boot" + "stored doc → skip (idempotent)" (`trader/auto_trader_weekly_test.go`).
- storage contract — "Sunday 17:00 CT belongs to the FOLLOWING Monday — got 2026-08-31" (`TestWeeklyPlanRowStorageContract`).
- invalidation twins — "bull invalidation high — close 30180 < 30200 must cross" / "bear invalidation low — close 30140 > 30120 must cross".
- injection render all 4 states — "no-doc state", "active state", "invalidated state", "thin-history state" (`TestWeeklyContextLineAllStates`).
- fail-open no-doc — "fail-open — no-doc renders the none form" (`TestPlannerPromptNoWeeklyDocFailOpen`).
- candle-table render + token count — "token count: 16052 chars → 4013 tokens (budget 32768)" = 12.2% of 65536 (< 50%).
- shadow twins — "confluent twin — 2 levels in band" / "non-confluent twin — want 0/0" / "shadow reorder — want 1/2".
- counter-annotation twins — "counter-blocked-clause twin — got … would-halve-size|would-require-A-grade|would-need-RR≥4.0" / "aligned-silent twin — want silent".
- draw-align tag — "bull long below the draw → toward_draw" (both directions + neutral).
- zero-real-effect diff — "THE LAW — real seating byte-identical with shadow code present".
- WEEKLY chip vitest ×4 states — `W20_weekly_chip.test.tsx` (bull/bear/none/invalidated + card mount, 6 tests).

## Gates (fresh)

- `go build ./...` → BUILD_OK (no output, exit 0)
- `go vet ./...` → VET_CLEAN
- `go test ./...` → 27 packages ok, 0 FAIL (goldens PASS)
- web `npx vitest run` → **34 files passed / 283 tests passed**
- `npx tsc --noEmit` → clean; `npm run build` → "✓ built in 4.12s"

## Cutover notes

This wave ships NOTHING that needs a cutover: no binary swap, no DB migration beyond the additive AutoMigrate `draw_align` column (zero-downtime), no C# change. The marker sequence for the next REAL deploy is unchanged from prior waves: lock marker → swap `vl-bin` → RELEASE marker commit → `kill -9 <PID>` → boot ack within 90s → goldens. The promotion decision (W8) is a **Sep-9 data decision** — no knobs flip before the table's thresholds are met.

## R12 coverage

Read: `kernel/weekly_bias.go` (W1), `trader/auto_trader_loop.go` (cycle + `maybeFetchCalendar` hoist), `trader/auto_trader_calendar.go`, `trader/auto_trader_planner.go` (client + input assembly + write core), `kernel/planner_prompt.go`, `kernel/engine_prompt.go` + `engine_prompt_futures.go` + `engine_analysis.go`, `kernel/plan_render.go` (providers), `store/plan.go` + `store/decision.go` + `store/bar_history.go`, `kernel/levels_assemble.go` + `levels_score.go` (seating), `telemetry/gate_blocks.go`, `api/handler_plan.go`, `web` plan card + guide (PlanCard/SessionPlanCard/chips, `lib/api/plan.ts`, guide content + `GuidePage.tsx`).

Not read: `ninjascript/*` (no C# changes this wave), `provider/ninjatrader/*` beyond the `persistWatchdogSeconds` pattern (no wire changes), `trader/*broker*` implementations (no broker path touched), `cmd/*` (no new binaries), `web` pages outside the plan card + guide (chip is self-contained). Skipped deliberately: no need — the wave touches only the planner/executor prompt assembly, the plan store, and the card.
