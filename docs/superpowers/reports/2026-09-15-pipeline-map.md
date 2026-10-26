# PIPELINE MAP — full logic inventory (read at origin/dev `8f5f26ee`, 2026-09-15, [A])

Successor to the deleted `2026-07-10-architecture-codebase-map.md`. Line numbers
= dev tip `8f5f26ee` unless stated. Live binary = `3ce4281a` (R4).

## A. DATA PIPELINE (bars in)

| Stage | File:line |
|---|---|
| NT8 C# AddOn TCP client (framing, HandleSignal order placement, bracket on fill) | `ninjascript/VLTraderTCPClient.cs:716` |
| Bars subscription manager (one NT8 BarsRequest per symbol/tf, 20s stall watchdog) | `ninjascript/VLBarsSubscriptionManager.cs` |
| Contract resolver (front-month, auto-roll) | `ninjascript/VLContractResolver.cs` |
| Go server frame dispatch | `provider/ninjatrader/tcp_server.go:1708` |
| Bar update enqueue (drop-oldest, freshness) | `:1659` → `drainBarIngest:1592` |
| Ring cache (cap 2500, live-never-overwritten) | `provider/ninjatrader/bar_cache.go:323` |
| DB persist (contract-required, replay-never-overwrites-live) | `trader/ninjatrader/bar_persist_wire.go` → `store/bar_history.go:265` |
| Market bridge to kernel | `trader/ninjatrader/bars_market_bridge.go:20` → `market/data.go:46` |
| Normalize CME early-return invariant | `market/data.go:670-677` |
| Symbol guard (roots, `.c.`, space-form failure mode) | `market/futures_symbol.go:50` |

## B. PLAN PIPELINE (`trader/auto_trader_planner.go`, 3054 L)

Triggers: session window `maybeRunSessionReadsAt:193` (gates: plan on, window,
sunday-asia defer, CME-open, W9 runnable, chain identity, dedupe) · death replan
`:768` (budget-gated) · MSS wake (`auto_trader_transition.go:156`) · level-event
wake (`auto_trader_wake_levels.go:250`) · owner reset/reread.

Attempt loop `runPlannerReadCoreObserved:1480` — 3 attempts
(author/resend-identical/repair/reauthor+block). Reject sites: provider err,
`IsPlanFragment` guard, **parse `ParsePlanDocCappedWithMinRR(raw, maxLevels,
scenarioCap, at.armMinRRFor(nil))` :1605**, level auto-collapse, mandatory flip
bias, mislabeled levels, `ValidatePlanDocWithFactsMachine:1679`, FVG re-verify
`:1733`, breakdown re-verify `:1755`, `validateAuthoredScenariosAt:1801`,
confirm-grace `:1948`. Fail-closed → no-trade doc + `planner_fail_closed` + P0
alert; else `kept_active`.

Input assembly `assemblePlannerInputWithCtx:2143`: 1m primary + planner TFs
structure + HTF zones (cap 4) + 12,000-bar tape + regime (1d/1h/5m) + levels
(`AssembleResearchLevels`, Seat1HZone, sticky owner levels) + bias-tree +
calendar slice + digest chain + FreshFVGs + void parity + weekly.

Inside-planner extra pipelines: owner overlays (`carryOwnerEditsInto:395` →
`AppendOverlay`, uncarried parked) · dormant/flip/death (`describeActivePlanDeath
:508`, hysteresis) · realign (cap 5, proposal → ask/apply) · shadow A/B
(`rootfix_shadow_ab.go:129`, replays live chain with same minRR) · digests ·
plan citation.

Prompt sections order: `kernel/planner_prompt.go:439` — Session → clock →
Regime → Candles → Weekly → Indicators → void/floor/displacement → Ranked
levels → Consumed → FRESH FVGs → roles → Structure → HTF zones → Auction →
Calendar → digests → owner note → prior invalidation → prior levels → anchors →
BIAS-TREE (`:140` RenderBiasTree, branches `:163-170`) → chain → no-trade gates
→ killzone → stop-doing → OUTPUT contract (`:743-835`: caps 8/3, ≥3 levels each
side, exact labels, arm rules, entry law, economics 6-decimal R).

Economics honesty (R4): `kernel/scenario_economics.go:139-228` — stated R must
match computed within 1 tick in price units; **arm-R at/above min floor is
auto-corrected + accepted** (`Corrected` counter), sub-floor/lying stays refused.

## C. ARM PIPELINE (`trader/armed_executor.go`, 2663 L) — per-cycle order `:197`

1. sweep pre-boot dead rows · 2. fill drain FIRST · 3. lifecycle cancel (no
plan/session end) · 4. supersede unplaced · 5. inputs (doc/cfg/bars/ATR5m) ·
6. **session risk** `:336` (halt N=8, no-trade band cancels resting) ·
7. **one-setup** `:369` + retire declined (OFF today) · 8. shadow demotion ·
9. **geometry** compose (`composeArmStop` + saveArmGeometry) · 10. stop anchor
`arm_stop_anchor.go:80` (widest-of) · 11. obstacle target · 12. wait_confirm
dormant · 13. **arm gate** `armGateVerdictFor:2114` (direction, bias, quality,
**R:R `:2150`**, min-SL `:2162`, HTF veto `:2175`) · 14. one-live-arm `:897` ·
15. **entry gate** `entryGateForArm:705` (class-48, incl. daily_force_flat leg
`entry_gate.go:163`) · 16. consult `:744` · 17. leg-kind `armLegKindFor`
(contradiction refused by name) · 18. UpsertArm · counters.

Placement `runArmedPlacementAt:1174`: limit (±100-tick band, wrong-side cancel)
/ stop-entry (`decideStopEntry:1503` tick-rounds, `placeOneStopEntry:1548`:
through→cancel, rest→place, unknown→counted) → tail: reconcile stale → fills →
place-confirm → cancel-confirm → boot reconcile.

Refusal classes (census `arm_refusals_0b:<trader>:<date>:<session>:<class>`):
`invalidated, rr, min_sl, veto, daily_force_flat, consecutive_loss,
no_trade_band, not_armable, other` + `geometry_<reason>` + `stop_entry:*` +
`one_setup:*`.

## D. DECISION PIPELINE (legacy crypto cycle — runs, cannot open in strict futures)

`runCycle` `trader/auto_trader_loop.go:174-935`: producers → CME gate → NT
account → dead-man → B6 rollover → profiles/night/digests/levelstate → EOD-flat
→ T1 force-flat → context → G2 structure → G1 HTF → G4 transition → C6 dead-plan
→ arm manage → R4 quality → skip-while-open → AI call (variant ninjatrader→
futures) → latency → persist (CoT, decision, 402 latch, safe-mode) → stale-bar
discard → close-first → execute → saveDecision.

Kernel `engine_analysis.go:57`: cme_closed, contract_roll, concurrent cap,
`DailyGuardrails:152-197` (loss/profit/trades/blackout/consistency — **decision
path only**), token cap 131072, ONE snapshot, OI≥15M, SVP/levels/bias/armed/
plan contexts, prompt ownership, `callWithSchemaRetry` (2 retries), post-armors.

`validateDecision` `engine_position.go:44`: action, leverage, min size $12/$60,
futures notional ceiling, SL/TP side, F1 rr `:122`, confidence `:198`, min-SL
`:207`, HTF veto, transition, dead-plan `GateRefusalError:292`, min-quality `:288`.

Execution gates `auto_trader_orders.go:138`: feed-down, dead-man, freeze, boot
integrity, stop_until, roll, consecutive-loss, last-entry, session, W9 plan-mode
(strict blocks), approval, `entryGateForDecision`, NT flatten-first.

## E. EXECUTION TRUTH LAYER

- placement proven ONLY by fresh received book naming the entry
  (`place_confirm.go:22`) · cancels proven ONLY by absence (`cancel_confirm.go`,
  90s timeout, ≤5 re-requests) · ONE contract/account (`one_contract.go:112`,
  unverifiable refuses) · fill-confirmed closes + `pnl_corrected`
  (`close_sync.go:82`) · reconcile 20s (`reconcile.go`) · SIM-only
  (`tcp_trader.go:289` + account allowlist + `account_select` live-reject).
- TCP wire: v3 (`tcp_framing.go:108`), 4-byte BE + JSON, identity stamp verified
  on ack/fill (mismatch freezes trader), `account_register` allowlist, stop-slot
  far-side gating.

## F. RISK LAYERS

- `kernel/risk_limits.go`: CheckPreTrade strict<, 17:00 CT roll resets + lifts
  force-flat; DailyGuardrails (soft/hard); ResolveMaxContracts.
- `trader/session_risk.go`: halt N=8 (warn 5), no-trade band, 30-min re-arm.
- `discipline/`: FreezeTrader kill-switch, reentry cooldown.
- `entry_gate.go` class-48: ONE canonical gate both paths.
- Auto-breakeven `auto_trader.go:157` + trailing `auto_trader_trailing.go`
  (both **unsuspended** 2026-09-15 via env; knobs gate them).

## G. TRACE / RESEARCH

40+ tables (see trace-anchors), `telemetry/gate_blocks` (in-memory, 17:00
rollover, `/api/risk/gate-blocks`), research snapshots journal-honest about
`missing=` fields.

## H. API (~90 routes, `api/server.go:82`)

Public: health, config, symbols, SSE bars (ticket), public strategies/
leaderboard, register/login, reset-password 410. Protected (JWT + ownership):
traders CRUD/start/stop/pause/prompt · strategies CRUD/activate/duplicate/
preview/test-run · models · exchanges(+account-state) · telegram · positions/
orders/trades/decisions/stats/klines/svp · risk (force-flat/status/gate-blocks/
errors/stream-cuts/freezes/clear-freeze/desk/audit) · accounts+select (SIM
double guard) · nt/symbols · bar-arbiter · admin/bars/import (env-gated) ·
plan/* (today/versions/history/alerts/overlay POST RFC-6902/owner-level/reread/
reset/ask/ask-apply/realign/approve/session-registry/trades/stats) · expectancy
· config/resolved (knob narration).

## I. NOFXi AGENT

9 skills (`agent/skills/*.json`): trader/exchange/model/strategy management +
trade_execution + 4 diagnosis. needs_confirmation: trader start/stop/delete,
exchange/model/strategy delete, trade execute+confirm_large; strategy config
patch gets a SECOND clamp-warning confirmation. Handlers:
`skill_execution_handlers.go` (6 big handlers), `active_session.go` (slot
machine), `atomic_skill_executor.go`, `llm_skill_router.go`. `tools.go` 22 tools
(7 mutating incl. execute_trade); secret redaction of api_key/secret_key/
passphrase/private_key/password_hash/lighter key.

## J. WEB (React 18 + Vite + Tailwind + SWR)

Routes `/settings /dashboard /strategy /agent /traders /welcome /faq`
(`router/AppRoutes.tsx`). Guide: 15 sections + drift banner (12-char rev prefix
vs `/api/health`); `GUIDE_BUILT_REV=3ce4281a`. Plan components: PlanCard,
SessionTabs, ScenarioEconomics, StructuralGeometry, ExpectancyPanel, AskPlanner,
Realign, DeskStrip, ArmedUnderBlock, OneSetupChip, GateBlocksPanel, NoTradeBand.
DayPlanEditor: one-setup toggle `:648`/grade `:656`, structural buffer input
`:352`. RiskControlEditor: dailyLossLimit `:774`, profitTarget `:809`,
maxDailyTrades `:836`, consecutiveLossHalt `:860`, reentryCooldown `:888`,
consistency `:918`, maxContracts `:946`, notionalCap `:971`, blackout `:997`,
breakeven/trailing `:221-304`.

## K. PERIPHERY

manager (trader lifecycle) · auth (JWT HS256 24h, blacklist 100k, bcrypt) ·
telegram (single-chat bind) · safe (panic-recovered goroutines, 10MB cap) ·
security (SSRF validator) · hook (4 hooks) · crypto (RSA secrets) · calendar
(CME slices) · expectancy (computed) · wallet · grid · mcp · nq_smoke.
