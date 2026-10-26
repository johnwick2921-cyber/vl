# AUDIT-0926 — Trading Pipeline (DS-106)

READ-ONLY full scan of the LIVE trading pipeline at `04ae1c2f` (= deployed boot 3, binary `6cac1b89`, marker `04ae1c2f`, booted 2026-09-25 19:33:22 CT, restarted 20:14:24 CT with `FAST_MARKET_REASONING=max`). Zero code changes. Base per L3: `git log -1` = `04ae1c2f RELEASE 6cac1b896bdb… — boot 3: #227 … booted 19:33:22 CT 2026-09-25`.

Method: worktree `/home/hoang/nofx-ds-106-audit0926` @ `04ae1c2f`, DB copy `audit.db` (made with `sqlite3 -readonly "file:…?mode=ro" ".backup"`), live log `data/nofx_2026-09-25.log` read-only. No tests run (L17: this is a read-only audit; DS-101/DS-104 own the box).

---

## 1. Map — the live trade path, stage by stage (file:line @ 04ae1c2f)

### Stage 1 — Market data in
1. NT8 AddOn → TCP `frame type=bar` → `TCPServer` read loop → `FrameBarUpdate` case `provider/ninjatrader/tcp_server.go:2257` → `BarCache` upsert (`provider/ninjatrader/bar_cache.go`; key `SYMBOL|TF`, ring cap `maxBars`). Ingest guards: out-of-order drops, boundary-bar finalisations (W4/D21), scale-mismatch checks, NO synthetic bars (P0 2026-08-17).
2. The KERNEL's single futures bar source: `market.FuturesBarsProvider` ← `wireFuturesBarsProvider` (`trader/ninjatrader/bars_market_bridge.go:18-34`) → `barsFromCache` (`:44-62`). The newest bar is usually FORMING; `CloseTime` is the bar's SCHEDULED close — consumers needing closed bars filter `CloseTime < now` (`:64-84` comment-truth A10). A short/holed/empty read is only OBSERVED (horizon WARN) — nothing is refused here by design (`:38-43`).
3. Indicators/levels/regime read `market.GetWithTimeframes` → the same provider; staleness is what it is — the horizon WARN at `trader/ninjatrader/bar_horizon_warn.go:214` is the only signal (e.g. live 08:56:16: 4h asked=500 served=446).
4. NT8 disconnect → no new bars → stale cache. Consumers: the 2m decision cycle keeps running on the last cache; entries are blocked by the `feed_down` admission gate (only once a `feed_status` frame says so — see Stage 4) and the dead-man watchdog; EOD flat/reconcile close paths are feed-independent.

### Stage 2 — Planner
1. Schedule: `maybeRunSessionReadsAt` `trader/auto_trader_planner.go:194` (registry read times: ASIA 16:30 / LONDON 01:30 / NY 08:00 CT; wall-clock, runs at the top of `tickOnce`, fires during halts with a `🗓 session read fired during halt` line — class 32).
2. Wake re-reads (W6): level-event wakes re-read opportunistically (`failClosed=false` — a failed wake does NOT write no_trade), `:1312`; holds + 30-min retry (`flipReread…` `:784`, `deathReread` machinery).
3. Death re-plans: `deathReplanAllowed` `:1189` (budget), `runDeathReplan` `:1219`; flip re-reads `:833`.
4. Claim: one read per (session,date) at a time (`claimPlannerRead` `:1279`); the claimed read core: `runPlannerReadWithTriggerClaimedCtx` `:1314` → `runPlannerReadCoreObserved` `:2052`.
5. Model call: `resolvePlannerClient` `:48` (planner model binding, `AI_PLAN_REASONING=max` owner rule; fast-market wake reads use `FAST_MARKET_REASONING`); `pinExactModel` `:121`; prompt assembled from facts + machine grades + HTF labels.
6. Retry/fail-closed: 3 attempts with the previous validator reason appended VERBATIM (`plannerRejectBlock` `:1742`); on exhaustion a NO-TRADE doc is written with the fail-closed map when `failClosed=true` (`writeNoTradePlan` `:1227`, `noTradeLevelMap` `:1156` obeying min_grade).
7. Write-time validators (in order at the write site): `ValidatePlanDocWithCaps` (hard caps) `:511`; `ValidatePlanDocWithFactsMachine` `:2285`; `ValidateFvgEntryScenarios` `:2363`; `ValidateBreakdownContinueScenarios` `:2385`; `ValidateMachineScenario` `:4044`; feasibility (`write-time feasibility` knob) and born-dead check.
8. Publish: plan row (plan_id, version) via the plan store; owner edits carried (`carryOwnerEditsInto` `:468`); versioning + lifecycle (active/dormant/no_trade); plan death/flip via `describeActivePlanDeath` `:587`.

### Stage 3 — Arming
1. Scenario → arm: `maybeManageArmedOrders` (`trader/armed_executor.go`); policy `planned_order` / `market_in_zone` per scenario; zones (`zone 30893.25–30903.00` example), stop floor/anchor (`stop=zone-edge+buffer`, `ResolveStructuralStop`), R:R at the arm's prices.
2. Admission at authoring: `entryGateForArm` `trader/armed_executor.go:799` (the SAME `EntryGate` the decision path runs) — only a row admitted THIS pass is placed (G1).
3. Re-arm / cancel / rest cap: re-arm never re-arms (terminal re-authorization only on version change); cancel: `cancelArmedOrdersSyncWith` `:2638` (signal-id decides, D1; a missing wire → `cancel_pending`, never a false `cancelled` — class 81 twice); EOD/session/dormancy/T1 cancel arms FIRST, synchronously (S-LIST CLOSER `:2552-2560`).

### Stage 4 — Admission (the ONE chain, in order)
`admitEntry` `trader/entry_admission.go:155` → `admitChain` `:169`. Call sites: decision `trader/auto_trader_orders.go:182` · arm `trader/arm_admission.go:68` · agent door `trader/entry_admission.go:457` · picture `trader/picture_htf_evaluator.go:719`.

Order of gates (file:line):
1. picture only: trader running + day-plan on `:172-184`
2. `feed_down` — ALL paths `:196` (default-ALLOW until a `feed_status` frame arrives)
3. dead-man watchdog (post-disconnect reconcile) — ALL `:206`
4. A4 freeze (identity/account echo mismatch) — ALL `:215`
5. boot integrity (`kernel.TradingRefused`) — ALL `:224` (outranks everything)
6. `stop_until` owner pause — ALL `:238`
7. installation maintenance hold (M2) — ALL `:249`
8. contract-roll block — ALL `:260`
9. consecutive-loss halt — decision+agent `:269`; arm+picture get `sessionRiskGateAt` (breaker + no-trade band + T1 + per-session cap) `:281`
10. last-entry cutoff — ALL `:296`
11. session gate + force-flat windows — decision+agent `:305/:315`; arm+picture ask the calendar directly (`cme_closed`) `:324`
12. plan-mode gate (W9) — decision+agent `:335`
13. approval required — ALL `:345`
14. re-entry cooldown — arm+picture `:357`
15. `EntryGate` (class 48) — decision `:376`, agent (bracket fail-closed first, W1b E9) `:394/:399`, picture `:408`; the arm path runs `EntryGate` at authoring (`armed_executor.go:799`).

`EntryGate` legs in order (`trader/entry_gate.go:154`): leg D daily_force_flat `:166` → leg 0 plan_mode strict `:195` → leg 1 plan bias (direction) `:213` → leg 2 scenario-direction (class 48) `:222` → leg 3 invalidation (arm only) `:237` → leg 4 shadow map `:253` → leg 5 R:R at WIRE-rounded prices `:280` (W1b E12) → leg 6 min-SL ×ATR5m `:300` → leg 7 one_open_position `:321` → NO-CHASE warn-first (refuses nothing) `:333`.

Every refusal goes through `admitRefuse` `:120`: always logged + `telemetry.IncGateBlock`; arm/picture deduped on (path,key,CLASS) — never on the moving reason (CTO M3). A gate refusal is never silent.

Kernel-level (BEFORE admission, decision path only): `parseFullDecisionResponse` `kernel/engine_analysis.go:573` → `validateDecisions` `:892` → `validateDecision` `kernel/engine_position.go:43`: min-confidence `:199-200` → min-SL `:215-250` → HTF veto `:261-264` → sizing → wrong-side. The arm path has no copy of min-conf/veto — its analogs are the arm-pass gates + EntryGate.

**No path skips admission.** Closes are not admissions (close half of feed-gate only, `auto_trader_orders.go:160-176`); Emergency Flat / drawdown monitor call the trader directly (`auto_trader_loop.go:870-875` comment) — they bypass admission BY DESIGN (they are exits).

### Stage 5 — Execution
1. Send: `TCPTrader` market entry `tcp_trader.go:~560` / armed limit `:697` / stop-entry `:794` — each takes the ENTRY LATCH first (`acquireEntryLatch`; wired by `wireNT8EntryLatch` `trader/entry_latch_wiring.go:27`; a stale/absent book = refusal).
2. Ordered execution worker (#226, live): per-(symbol,account) FIFO worker; `InstallOrderedExecutions` `trader/ninjatrader/ordered_execution.go`; the worker's close consumer `recordCloseOrdered` `close_sync.go`; the legacy advisory consumers skip `OrderedOwned` frames.
3. Fill → OPEN materialize: armed fill materialization (`armed_executor.go:2205` region, "🧩 armed fill … materialized OPEN"); fill funnel `handleFillInbound` `tcp_trader.go:330`.
4. Protection: bracket-on-fill is C#-side (`SubmitBracketOnEntryFill`, `ninjascript/VLTraderTCPClient.cs`); Go-side monitor `reconcileProtectionAt` `trader/protection_reconciler.go:273` — adjudicates the book vs open positions, fires `🚨 UNPROTECTED POSITION` P0 alert + places a standalone `PlaceProtectiveStop` (`tcp_trader.go:1117`). BE/trail = `MoveStopToBreakeven`/`ModifyBracket` (trailing=OFF live per boot line 01:40:11); hold-lock suppresses AI closes (`holdLockSuppressesClose`, `auto_trader_loop.go:880`).
5. Exits: target/stop (C# OCO) · AI close · force-flat · breaker · EOD flat 14:45 CT (`enforceEODFlat` `trader/auto_trader_clock.go:454`) — limit exit then 10s market fallback (`CloseWithLimit` `tcp_trader.go:1068`; live: "limit exit submitted MNQ SHORT @ 30909.25 (market fallback in 10s)"). Arms are cancelled BEFORE the flatten, synchronously (S-LIST CLOSER).
6. Close receipt (#227 live): `ApplyNT8Exit` (`store/nt8_exit_receipt.go`) — BEGIN IMMEDIATE on a dedicated pooled connection, bounded busy retry in `recordCloseOrdered` (`close_sync.go`, 5 tries, backoff ≤~1.6s), on final failure `putPricedClose` + `hasFill=false` + `SavePendingExit` (own small write) → `RetryPendingNT8Exits` applies later. P&L: `pnl_corrected` (corrected-column law; NULL = unresolved, excluded with COUNT shown).
7. Reconcile: position reconcile closes store-OPEN vs broker-FLAT with the latest opposite-side fill or the parked price; no evidence → `unresolved` (never exit=entry).

### Stage 6 — Missing/late/duplicate data, disconnect, restart
- Missing bars: horizon WARN only; plans still written (fail-open, documented); entries blocked by feed gate once status says so.
- Late/duplicate: BarCache refuses out-of-order bars (except boundary finalisations); fills deduped by `exchange_trade_id`; close receipts idempotent by receipt id; one-frame-applies-once pinned (#226 G1).
- NT8 disconnect: dead-man blocks NEW entries until a clean reconcile; EOD flat can still send closes (feed-gate close half warns); the protection monitor keeps running off the last book.
- Restart mid-stage: ordered owner re-installs on `Run`; pending exit receipts re-read on the next close/fill (`RetryPendingNT8Exits`); armed rows swept at boot ("boot sweep cancelled 0 pre-boot arms" in the live log); the entry latch starts UNWIRED→wired at construction; planner claims are in-memory (a killed read leaves no row — the next scheduled read re-authors).

---

## 2. Census (live values, audit.db copy)

- Positions since 2026-09-25 00:00 UTC: 2 rows, both CLOSED — 618 (`unresolved`, exit 0.0, pnl_corrected NULL) and 619 (`sync`, exit 30909.25, pnl_corrected −32.0). 0 OPEN at scan time.
- `nt8_exit_receipts`: 1 row, applied=1 (619's EOD close), 0 pending — the #227 path has never parked a receipt since boot 3 (and the 1 receipt proves the receipt path runs).
- `decision_records` since midnight UTC: 46,698 rows; 566 carry `risk_check_error` (refusals are recorded, not silent).
- Live sessions (boot line 01:40:11): ASIA 17:00→02:00 (last-entry 01:45, flat 02:00) · LONDON 02:00→08:30 (flat 08:30) · NY 08:30→14:45 (flat 14:45); cadence 2m; position_mode=ai_watch; trailing=OFF; guardrails=master OFF (soft-audit); stale_dodge=on, reeval_drift=0.25×ATR14; post_exit_rescan=on 2000ms.
- Boot 3 (RELEASE 6cac1b89, marker 04ae1c2f): clean-clone vcs.modified=false, goldens PASS, ordered execution installed, NT8 link ESTAB, addon proven 2026-09-23-m21.

---

## 3. Findings by severity

### P0 — none surviving.

### P1 — none surviving. Candidates examined and refuted in §4.

### P2 — DEFAULTS-ON KNOBS WITH NO ADMISSION COPY (cross-slice; one line to CTO)
Several defaults-ON policy knobs affect the trading pipeline but have no Studio control surface — DS-102 owns the full knob census; from the pipeline side the relevant ones are named there (`planner_contract`, `write_time_feasibility`, `condition_status`, `zone_place_within_pts`). [B]

### P2 — `pnl_corrected` NULL for the 618-class unresolved rows renders without P&L
Row 618 (the pre-#227 lost close, now reconcile-`unresolved`) has `exit_price=0` and `pnl_corrected NULL`; the live "10 recent closed trades for AI context" line at 08:56:16 says "0 unresolved, rendered without P&L" for that minute — unresolved rows are excluded per the corrected-column law (class 40), which is correct behaviour, but the AI context sees NO loss for that trade. NOTE-level: the loss is invisible to the decision engine, not mispriced. [A: DB row + log line 29808].

### NOTE — feed gate is default-ALLOW until the first `feed_status` frame
`admitChain:196` refuses only when `ninjaFeedDown()` reports a known-down status; before any `feed_status` frame arrives (fresh AddOn link), entries are admitted against a possibly stale/absent feed — mitigated by the entry latch (book staleness = refusal) and dead-man. Documented fail-open posture. [A: comment at entry_admission.go:190-195].

### NOTE — kernel validator chain (min-conf/min-SL/HTF-veto) is decision-path only
The arm path's protection is the arm-pass gates + EntryGate legs 5-7 + stop-floor anchor; it has no min-confidence or HTF-veto leg. This is the designed split (arms are plan-authored, not AI-proposed); recorded because "one admission chain" is true at the trader layer but the kernel chain is a second, path-specific validator set. [A: grep — `validateDecision` called only from `parseFullDecisionResponse` at `kernel/engine_analysis.go:892`, reached only from the decision loop at `auto_trader_loop.go:550`].

### NOTE — the 4h horizon SHORT (served 446/500) did not stop the plan
08:56:16 the planner wrote from a short 4h read (446/500, warn at `bar_horizon_warn.go:214`). The horizon observer is warn-only by design (A10/A24) — a plan CAN be authored on a shorter-than-asked window. Not a defect today (ask=500 > ring=2500 cap trade-off), named because "act on stale data" is in the dispatch's question list. [A: log line 29788].

---

## 4. Refuted / withdrawn

1. **Candidate: EOD/T1 synchronous cancel could write `cancelled` over a row that just FILLED (bypassing the filled guard).** REFUTED. `cancelArmedOrdersSyncWith` (`armed_executor.go:2638`) counts rows that filled during the drain separately (`filled` at :2649-2652), sends wire cancels by SIGNAL ID (D1, :2660), records `cancel_pending` (never `cancelled`) when the wire is missing (:2700), and the drain applies frames through the same `onArmedOrderUpdate` the cycle consumer uses (:2634). A filled order's cancel is refused by NT8 and the fill still materializes; the EOD flatten then closes it — that is the designed outcome.
2. **Candidate: the agent door could trade without EntryGate.** REFUTED — `admitAgent` runs the full `admitChain` plus the fail-closed bracket check (W1b E9) plus `EntryGate` (`entry_admission.go:394-405`), on top of `policy.CanExecuteTrade`/`AllowTradeExecution` at `agent/tools.go:2745`.
3. **Candidate: a gate refusal could be silent.** REFUTED — every refusal passes `admitRefuse` (`:120`), which always logs AND `IncGateBlock`s; decision refusals also stamp `actionRecord.Error` and the parent record carries the first refusal (`auto_trader_loop.go:888-918`, W16/R3). Live census: 566 decision rows carry `risk_check_error`.
4. **Candidate: two entries could double-open (no latch).** REFUTED — every entry path (market :568, armed :697, stop-entry :794) takes `acquireEntryLatch`, whose book/ledger source is wired for every `*TCPTrader` (`entry_latch_wiring.go:27`); a stale/absent book is a refusal, not a pass.
5. **Candidate: row 619's EOD close could misprice.** REFUTED — live log: 14:45:15 EOD-FLAT limit exit 30909.25 (market fallback 10s) → 14:45:24 `excursion closed row=600 pos=619 exit=30909.25 reason=sync` → pnl_corrected −32.0 present and non-NULL in the DB.

---

## 5. What I could NOT verify and why

- The C# side (`VLTraderTCPClient.cs` bracket-on-fill, OCO semantics) — verified only by reading the Go-facing contract + the live log's wire evidence (filled brackets, no naked exits today); the AddOn binary itself was not inspected.
- The live `.env` values — KEY NAMES only per L12; the actual `FAST_MARKET_REASONING=max` claim rests on the CTO's dispatch context + boot-3 restart line, not on the file.
- Race-level claims — no `-race` run (DS-101/DS-104 own the box; L16).
- The agent-chat door's live usage — `execute_trade` has no live invocations in today's log within my grep window; its chain was verified by code only.

---

## 6. Cross-slice notes (one line each)

- DS-102 (knobs): the pipeline's defaults-ON knobs with no Studio surface are named in my P2 — full census on your side.
- DS-107 (system): the `feed_status`-frame dependency of the feed gate and the dead-man reconcile window are system-pipeline items the trading slice consumes.
- CTO: no P0/P1 findings survive refutation at 04ae1c2f; the pipeline is gated end-to-end and every refusal is recorded; row 618's unresolved P&L is the visible scar of the pre-#227 defect and is correctly excluded (not mispriced).
