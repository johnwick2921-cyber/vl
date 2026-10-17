names rewritten to vl on 2026-09-30 (VL rename)
# CLASS 35 — replan budget arithmetic (PART 1) + 1C touch-band calibration & friction floor (PART 2)

Date: 2026-09-01 · Owner: hoang · Agent: Fable 5 · Worktree: `../vl-class35` (branch `fix/class35-replan-budget`)
Evidence tiers: **[A]** directly verified · **[B]** inferred from strong evidence · **[C]** speculation.

## STATUS AT CLOSEOUT

| Item | State |
|---|---|
| PART 1 code | **MERGED to dev @ `ec6632f9`**, pushed to origin (fast-forward from `d4b38604`) |
| Build | clean clone `--no-local` at `ec6632f9`, `vcs.modified=false` (sha256 `b460def0d42469be…`) — now the running `~/vl/vl-bin` |
| Marker | `b51f8f03` — `deploy/RELEASE` + `GUIDE_BUILT_REV` = `ec6632f9…` (one marker, one boot) |
| Cutover | **DONE 17:23:54 CT on owner GO** — boot `🔐 BOOT INTEGRITY OK — rev ec6632f9 · goldens PASS` 17:24:00, PID 1908258, live replans_left 4/4 (see CUTOVER section at the end); rollback `vl-bin.prev.boot` kept |
| PART 2 | read-only analysis complete; recommendation **PARKED, NOT APPLIED** (no config, no knob, no DB write) |
| Lock | `~/vl-main.lock` acquired 16:18 CT under a dead subshell PID, found stale by a second agent, restored, then cleared+re-acquired by me at 16:40:57 CT under live pid 1860416 (note in A2 section); released at closeout |

---

## PRE-FLIGHT (A1/A2/A6/A7)

- **Porcelain gate [A]:** `git status --porcelain` → empty (quoted at 16:17:58 CT, again at 16:40 CT before the worktree, again before the ff-merge and before the marker commit).
- **Lock [A]:** initial acquisition wrote the *subshell* PID (1861644), which died immediately. A second agent found it stale, cleared it, then restored it with a note. On my re-acquire: `kill -0 1861644` → dead → cleared with this note, re-acquired as `owner=hoang pid=1860416 expiry=2026-09-01T23:00:00-0500 task=class35-replan-budget+1C-touchband`, `kill -0 1860416` → alive. **Lesson recorded:** the lock PID must be the long-lived session process (the parent of the shell), never `$$`.
- **Worktree [A]:** `git worktree add ../vl-class35 -b fix/class35-replan-budget origin/dev` at `d4b38604`, locked. Removed at closeout.
- **Session state at pre-flight (A7) [A]:** 16:17 CT — NY closed (EOD flat 14:45 CT: `🔒 armed cancel: session ended (EOD flat) — 1 order(s) disarmed`), ASIA read window opens 16:30 CT, CME halt 16:00–17:00. No cutover was attempted (owner GO not given).

---

# PART 1 — CLASS 35: REPLAN BUDGET ARITHMETIC

## 1. Root cause [A]

`store/strategy.go` (pre-fix, `d4b38604`):

```go
func ReplansUsedFrom(version, baseline int) int { if version < baseline { return 0 }; return version - baseline }
func MayReplanFrom(version, baseline, cap int) bool { return ReplansUsedFrom(version, baseline) < cap }
func ReplansLeftFrom(version, baseline, cap int) int { … cap − used … }
```

Trigger-agnostic: every appended row counted as a spent re-plan. Confirmed from the live DB (rows quoted by `rowid`):

| rowid | session | v | trigger_reason | lifecycle | written CT |
|---|---|---|---|---|---|
| 177 | LONDON | 1 | planner_fail_closed | no_trade | 01:49:31 |
| 178 | LONDON | 2 | level_event | active | 02:48:27 |
| 179 | LONDON | 3 | dormant:flip:flip-condition: 2x5m close above 29231.63 … | dormant | 04:52:43 |
| 180 | LONDON | 4 | level_event | active | 05:34:30 |
| 181 | LONDON | 5 | level_event | active | 07:07:32 |
| 182 | LONDON | 6 | level_event | active | 07:56:33 |

`dayplan_reset:…:2026-09-01:LONDON` → **no row** (baseline 1). LONDON cap = 4 (session override). Old arithmetic: used = 6−1 = 5 → `replans_left = 0`, `MayReplanFrom = false` → the next scenario death would have fail-closed the session (`writeNoTradePlan`, `replans_exhausted`) for a budget that was never spent.

Two compounding facts the DB adds:
1. `trigger_reason` is **overwritten in place** by dormant/re-arm transitions (`UpdatePlanLifecycle`, `store/plan.go:290`) — row 179's original trigger is gone. A class stamped in `trigger_reason` (approach a) could not be trusted.
2. Death re-plans landed **unlabelled**: the death path called `runPlannerReadWithCtx(…, "", …)` → trigger `<S>_scheduled_read`. Across the whole `plans` table there was no `death_replan` label at all, and zero `owner_reread` rows.

The live API on the old binary at 17:02:36 CT [A]: LONDON `version 6, replans_left 0, replan_cap 4`; NY `version 5, replans_left 0, replan_cap 4` (NY chain: scheduled_read, dormant:death, dormant:flip, rearmed, level_event — also zero spends).

## 2. Which classes gate (verified before editing, A17) [A]

CONSUME (call the gate): death re-plan `trader/auto_trader_planner.go:328` (+ the `:336` "+1" alert check) → fail-closed write `:763`; owner re-read `trader/auto_trader_reread.go:76` (CanForceReread) and `:118-119` (ForceReread TOCTOU re-check).
FREE (no gate on the path): level_event wake `auto_trader_wake_levels.go:279` (failClosed=false); structure MSS wake `auto_trader_transition.go:194`; dormant/re-arm `auto_trader_planner.go:~304/~262` (lifecycle update, no row); the initial scheduled read `:1009`; owner reset `auto_trader_reset.go:129` ("owner_reset"); fast-market is a *reasoning mode* of the wake reads (`:875-880`, F3), not a trigger class. Cap source: `auto_trader_planconfig.go:48 ReplanCapFor`.

## 3. Fix — approach (b), recorded counter, and why

**Chosen: (b)** — a recorded `replans_used` per (trader, date, session) in `system_config`. Reasons: (i) `trigger_reason` is mutable (fact 1 above), so (a) needed a new column plus a migration; (ii) a recorded counter cannot drift from the gate the way an inferred one can; (iii) smaller diff.

One deliberate refinement to the dispatch's "incremented at exactly the two gating sites": the increment fires **when the consuming row actually lands** (`runPlannerReadCoreWithFactsGrades`, keyed by the trigger class both gates pass — `death_replan` / `owner_reread`), not at the gate call itself. Reason: a read refused by preflight (stale bars), clock-hold, a lost claim, or a missing client writes no row, and the existing invariant "no plan row, no budget consumed" (`clockHoldDeferLine`, F6) must keep holding — spending at the gate would let four preflight refusals fail-close a session (exactly the 16:39–16:57 CT tape today: nine consecutive `planner_preflight_refused … stale_bars`). The gate reads the counter; the TOCTOU between gate and row is closed by the existing single-flight claim (`claimPlannerRead`) plus the re-read's fresh re-check. Both gates and the write site are named in tests.

**Counter key:** `dayplan_replans_used:<trader>:<date>:<session>:b<baseline>` — keyed UNDER the reset baseline, so an owner reset (new baseline) starts a fresh counter at 0 with **no change to the reset path**; the abandoned chain's spends stay on record. Baseline semantics, cap values and which classes gate are unchanged (Section F).

**Malformed / negative counter:** reads as 0 (full budget) with an `[ERRO] 🧮 replan budget: malformed counter …` line — never a panic, never a silent exhaustion (A10/A24). A failed `SpendReplan` write logs `[ERRO] … the gate may over-allow by one`.

### Files and lines touched (post-fix, dev `ec6632f9`) [A]

| File | Lines | Change |
|---|---|---|
| `store/strategy.go` | 1204-1206 | `TriggerDeathReplan = "death_replan"`, `TriggerOwnerReread = "owner_reread"` |
| | 1210 | `TriggerSpendsReplan(trigger)` — the ONLY two spending classes |
| | 1222-1238 | `type ReplanBudget{Used,Cap,Baseline}` · `Left()` · `May()` |
| | 1242 | `ReplansUsedKey(trader,date,session,baseline)` |
| | 1254 | `readReplansUsed` (malformed → 0 + loud log) |
| | 1270 | `GetReplanBudget(st,trader,date,session,cap)` — baseline → counter under it |
| | 1288 | `SpendReplan(st,trader,date,session)` — mutex-serialized read-modify-write |
| | 1304 | `ReplanBudgetBootLine()` |
| | (deleted) | `ReplansUsed`, `MayReplan`, `ReplansLeftFor`, `ReplansUsedFrom`, `MayReplanFrom`, `ReplansLeftFrom` |
| `trader/auto_trader_planner.go` | 320 | death path resolves `budget := store.GetReplanBudget(…)` |
| | 329-334 | `if at.deathReplanAllowed(…) { go at.runDeathReplan(…) }` |
| | 735 | `deathReplanAllowed` — gate: `!budget.May()` → NO-TRADE marker; last-replan alert on `Used+1 ≥ Cap` |
| | 765 | `runDeathReplan` — the read with trigger `death_replan` (+ sticky-level carry) |
| | (deleted) | `runPlannerReadWithCtx` (the unlabelled death read) |
| | 1588 | `spendClass, spends := trigger, store.TriggerSpendsReplan(trigger)` captured BEFORE the fail-closed relabel |
| | 1660 | `store.SpendReplan` after `AppendPlan` succeeds → `🧮 replan budget: <class> spent one — n/cap used, left …` |
| | 2237 | executor prompt `ReplansLeft` from `GetReplanBudget(...).Left()` |
| `trader/auto_trader_reread.go` | 67-78 | CanForceReread: `budget.Left()` / `!budget.May()` refusal "(n of cap used)" |
| | 119 | ForceReread TOCTOU re-check reads the counter |
| `api/handler_plan.go` | 205-215 | `planRulesWithCap(traderID, session, tradeDate)` → `GetReplanBudget(...).Left()` (the `version` param removed) |
| | 221 | `dayPlanCfgFor` — nil-safe config resolver (no trader manager → shipped defaults) |
| | 152, 318, 1346, 2124 | `planRules` callers drop the dead `version` argument |
| `main.go` | 262 | boot line `🧮 replan budget: recorded-counter (class 35) — spends: death_replan, owner_reread · free: …` |
| `web/src/components/plan/SessionPlanCard.tsx` | 337-341 | **client formula deleted** (`noTradeVersion ? noTradeVersion−2 : version−1`); `replansSpent = replan_cap − replans_left` from the API, `undefined` when absent |
| | 532-533 | NO-TRADE banner renders `?` instead of a fabricated `0` when the API omits the numbers |
| `docs/superpowers/AUDIT-CHECKLIST.md` | 286 | **CLASS 35** appended after 34 (highest was 34; 27/28/29/33 left unoccupied) |
| `web/src/guide/content/{settings,glossary,faq,planCard}.ts` | 75, 88-92, 50, 31 | budget = recorded counter; which classes spend; `death_replan` label |
| `web/src/guide/types.ts` · `deploy/RELEASE` | 6 · 1 | `GUIDE_BUILT_REV` / RELEASE = `ec6632f9…` (marker `b51f8f03`) |

Behavior change to name plainly: death re-plan rows are now **labelled `death_replan`** in `trigger_reason` (they used to read `<S>_scheduled_read`). Nothing branches on that string (`grep` of Go/TS: only the guide example list and the sandbox seeder).

## 4. Tests (A8 — written first; C6) [A]

**Pin test, RED on the pre-fix tree.** `trader/class35_pin_test.go` uses only the pre-fix surface (`CanForceReread` over an appended chain), so it compiles on `d4b38604`. Run in a throwaway worktree at `d4b38604`:

```
=== PRE-FIX TREE d4b38604: TestClass35PinTodayChain ===
--- FAIL: TestClass35PinTodayChain (0.15s)
    class35_pin_test.go:66: replans_left = 0, want 4 — nothing in this chain spent budget (chain: fail_closed, level_event, dormant:flip, level_event ×3)
    class35_pin_test.go:69: MayReplan must be TRUE for an unspent budget; the gate refused: "the re-read budget for LONDON is spent (5 of 4 used)"
FAIL	vl/trader	0.155s
```

**GREEN on the fix** (`ec6632f9`):

```
--- PASS: TestClass35PinTodayChain (0.15s)
--- PASS: TestClass35DeathReplanSpendsAndFifthFailsClosed (0.18s)   // 4 deaths spend 1..4 (rows labelled death_replan); 5th → no_trade/replans_exhausted, counter stays 4
--- PASS: TestClass35OwnerRereadSpends (0.13s)                       // ForceReread → v2 owner_reread, Used 1, Left 3; CanForceReread then says 3
--- PASS: TestClass35FreeReadsDoNotSpend (0.14s)                     // "", level_event, structure_mss, owner_reset all land rows, Used stays 0; fast-market = those classes
--- PASS: TestClass35RefusedDeathReplanDoesNotSpend (0.12s)          // no client → no row → Used 0
ok  	vl/trader	0.729s
```

Plus (all PASS): `store`: `TestReplanBudgetLeftAndMay`, `TestSpendReplanRecordsEachSpend`, `TestResetBaselineRearmsTheRecordedBudget` (4 spends → exhausted → baseline 7 → Used 0, Left 4; original chain's `4` still on record), `TestMalformedReplanCounterReadsAsZero`, `TestTriggerSpendsReplanClasses`, `TestNoLiteralReplanBudgetInThePath` (now also forbids `version − baseline` and the deleted function names in non-test code); `trader`: `TestForceResetWritesFreshChainAndRestoresBudget` (end-to-end reset re-arm, migrated to seed 4 recorded spends), `TestRereadRefusesWhenTheBudgetIsSpent` (migrated: 2 spends against cap 2 refuse with "0 left"); `api`: `TestClass35APIReplansLeftIsTheRecordedBudget` (the 6-row chain → `replans_left == cap`; one spend → `cap−1`); web: `Class35_budget.test.tsx` — "4 re-reads left" for today's chain; NO-TRADE marker at v3 with API `0/4` renders **"4 of 4 re-plans"** (old formula would print "1 of 4"); absent numbers render `? of ?`, never a fabricated `0 of`.

Suites: `go test ./...` → **27 ok / 0 FAIL** (includes `vl/kernel` goldens: `TestFuturesPlanGolden`, `TestFuturesKeyLevelsGolden`, `TestVerifyPromptGoldensPasses` … PASS) · `vitest run` → **37 files / 295 tests passed** · `tsc --noEmit` clean · `go vet` clean on the touched packages.

Fixture note: the canned `planClient` plan predates the `confirm{}` contract; the class-35 fixtures set `CONFIRM_GRACE_SESSIONS=100` (test seam) so consecutive reads land ACTIVE rows — the confirm contract is not what these tests exercise.

## 5. The pattern — CLASS 35 (checklist entry appended at line 286)

Fourth silent counter defect in one week: **replan budget** (this wave — spend inferred from row count) · **guardrail ENTRIES count** includes test-seam rows (open) · **P&L aggregation** summed `realized_pnl` not `pnl_corrected` (fixed) · **GORM alias scan** returned a plausible zero (class 30, fixed). **Law: counters record events; they do not infer them.** Probe: for every cap/budget/quota, find the increment site — if "used" is derived (rows, versions, ids, timestamps) rather than written by the consuming path, it is inferred; fixture the live chain shape and assert the resolved value at the gate.

## 6. Boot line + guide

Boot block (after `🔐 ConfirmRuleLedger`): `🧮 replan budget: recorded-counter (class 35) — spends: death_replan, owner_reread · free: <S>_scheduled_read, level_event, structure_mss (incl. fast-market), owner_reset, dormant/rearm + fail-closed markers · key dayplan_replans_used:<trader>:<date>:<session>:b<baseline>`. Guide entries updated (settings "Max re-plans", glossary Re-plan/Re-read, FAQ ⛔, plan-card trigger list) and `GUIDE_BUILT_REV` bumped to `ec6632f9…` in the marker commit.

## 7. Build, stage, rollback (A4/A13)

```
git clone --no-local https://github.com/johnwick2921-cyber/nofx.git <scratch>/clone && git checkout ec6632f9de41060b52398f41f9ffbbf840814c40
go build -o vl-bin.next .
go version -m vl-bin.next:
	build	vcs.revision=ec6632f9de41060b52398f41f9ffbbf840814c40
	build	vcs.time=2026-09-01T21:54:27Z
	build	vcs.modified=false
cp vl-bin.next /home/hoang/vl/vl-bin.next   # sha256 b460def0d42469be…  (70,858,376 bytes)
```

Running binary (unchanged): `/home/hoang/vl/vl-bin` = `fef656a4…` (vcs.modified=false, built 2026-09-01T04:52:30Z), PID 1625428. Between `fef656a4` and `ec6632f9` on dev there is exactly one non-docs commit besides this fix: the class-34 marker `d03db52a` (RELEASE/GUIDE_BUILT_REV only). So the cutover ships **this fix only**.

**Cutover (on GO, in a flat window outside 16:45–17:10 CT, no read in flight, no live arms):**
```
cd /home/hoang/vl && cp vl-bin vl-bin.prev.boot && mv vl-bin vl-bin.old.fef656a4 && mv vl-bin.next vl-bin && kill -9 1625428
# then within 90s expect: 🔐 BOOT INTEGRITY OK — rev ec6632f9 · expected ec6632f9 · goldens PASS   and   🧮 replan budget: recorded-counter (class 35)
```
**Rollback (exact):**
```
cd /home/hoang/vl && mv vl-bin vl-bin.bad.ec6632f9 && cp vl-bin.prev.boot vl-bin && printf 'fef656a4ee7c45860ad0237f48cef90c6b148d17' > deploy/RELEASE && kill -9 $(pgrep -f '^/home/hoang/vl/vl-bin$') && git checkout -- deploy/RELEASE web/src/guide/types.ts   # (revert marker locally; push a revert of b51f8f03 if the rollback sticks)
```

## 8. Proof — flat gate, in-flight, session state (A5/A6/A7) [A]

Quoted fresh 16:56–17:03 CT, all four:
1. DB: `SELECT COUNT(*) FROM trader_positions WHERE status='OPEN'` → **0** (DB open orders 0, live armed 0).
2. API `/api/positions` → `[]` (17:0x CT, owner token minted locally via `cmd/gate-jwt` with the `.env` secret; the first attempt signed with the process-default secret and was rejected "signature is invalid" — the tool needs `JWT_SECRET` in its environment because `godotenv.Load()` lives in `main.go:33`, not in `config.Init`).
3. NT8 positions snapshot (journal): `2026/09/01 16:56:50 INFO tcp_server: positions snapshot account=Sim101 count=0` (+ `SimAccount1 count=0`).
4. API `/api/open-orders?symbol=MNQ` → `[]`.

In-flight (A6): `/api/plan/today?trader_id=…` at 17:02:36 CT → `active_session ASIA, found false, replan_in_flight false`; but the log shows the ASIA session read **IN FLIGHT** since 17:01:05 (`🧠 planner model: … pinned "deepseek-v4-pro"`; 17:03:05 `planner read for 2026-09-01:ASIA:… already in flight — skipping duplicate call`). Note: `replan_in_flight` is only true while a read runs against a *committed* row, so the API cannot flag a first read — the claim line in the log is the truth for A6. **No cutover while this read is in flight; and the 16:45–17:10 window forbids it anyway.**

Session state (A7): ASIA opened 17:00 CT after the halt; preflight refused the 16:30–16:57 read attempts (`stale_bars_2345s…3185s`, nine lines) until bars resumed. LONDON/NY chains are closed for the day.

**Live `replans_left` for today's chains (old binary, 17:02:36 CT):** LONDON v6 → `0` of cap 4; NY v5 → `0` of cap 4. **Fixture expectation on the fix: LONDON → 4, NY → 4** (both chains contain zero `death_replan`/`owner_reread` spends; the counter keys are absent → 0 used). This cannot be observed live until cutover — **the session has rolled to ASIA, whose chain is being written now; its v1 will show `replans_left 4` on either binary.**

## 9. What the owner will STILL see wrong (A15)

- Until cutover, every card/prompt reads the OLD arithmetic: LONDON and NY show **"0 re-reads left"** (truth: 4/4). A scenario death in any chain before cutover would still fail-close wrongly.
- The vite dev server serves the main tree: the **FE change is already live** (the card now renders `replan_cap − replans_left` from the API) and the **guide drift banner shows** until the binary matches `GUIDE_BUILT_REV` — both expected.
- At cutover, any death re-plan that already happened under the old binary in the *current* ASIA chain is not in the counter (rows are unlabelled) → a one-time under-count in the permissive direction for that chain only. Today's LONDON/NY chains are closed; no impact.
- Historical version views (`?version=n`) now show the **session's current** budget, not the budget at that version (the budget is a chain property, not a row property).
- `trigger_reason` continues to be overwritten by dormant/re-arm transitions — the budget no longer depends on it, but the version list still loses the original class for those rows (out of scope; noted).

---

# PART 2 — 1C TOUCH-BAND CALIBRATION + FRICTION FLOOR (read-only; PARKED)

Scripts (scratch, not committed): `touchband.py` (Δ, detector port, IID shuffle, stationary bootstrap, D5 tests 1–3), `friction.py`. Both ran against a **copy** of `data/data.db`; no write to the live DB, config, env or knob. Detector = a faithful port of `kernel/touch_telemetry.go` (`TouchUpdate` open/close, `minBarDist`, `approachSide`, `closeSide`, `penetrationStats`, `classifyShape`, `TOUCH_EPISODE_MAX_BARS=12`); levels = the round-number grid (multiples of 50, "RN") + prior-session-day high/low ("PDH/PDL"), the same rule on real and surrogate paths.

## D1 — band unit Δ [A]

MNQ 1m tape: **12,782 bars**, 2026-08-19 10:00 → 2026-09-01 15:58 CT (`bars` table, symbol MNQ, tf 1m). Δ = mean |close_t − close_{t−1}| over the **12,772 consecutive (60 s-spaced) pairs** = **5.4204 pts = 21.68 ticks** (median 3.75 pts; including the 10 gap pairs Δ = 5.432). The live fixed band is **16 ticks = 4.00 pts = 0.74 Δ**. D5 test 1: a fixed-step synthetic (+1.0/bar) → Δ = 1.0 exactly (PASS).

## D2 — calibration against a memoryless null [A]

Surrogates rebuild OHLC from shuffled bar-shaped increments (o−c₋₁, h−c₋₁, l−c₋₁, c−c₋₁), so H/L exist for the detector. IID shuffle: 12 paths (seed 35). Stationary bootstrap (Politis–Romano, geometric blocks, mean block 30 bars): 12 paths. p(bounce) = rejections / (rejections + acceptances); chop was 0 at every k.

| k | band pts | band ticks | p real | n real | p shuffle (sd) | p bootstrap (sd) | real − shuffle | real − bootstrap |
|---|---|---|---|---|---|---|---|---|
| 0.05 | 0.27 | 1.1 | 0.696 | 1326 | 0.742 (0.010) | 0.699 (0.011) | −0.046 | −0.003 |
| 0.1 | 0.54 | 2.2 | 0.689 | 1319 | 0.737 (0.009) | 0.697 (0.013) | −0.048 | −0.008 |
| 0.2 | 1.08 | 4.3 | 0.690 | 1331 | 0.732 (0.010) | 0.694 (0.009) | −0.042 | −0.004 |
| 0.3 | 1.63 | 6.5 | 0.691 | 1351 | 0.731 (0.011) | 0.690 (0.012) | −0.040 | +0.001 |
| 0.5 | 2.71 | 10.8 | 0.687 | 1386 | 0.730 (0.011) | 0.685 (0.010) | −0.043 | +0.001 |
| **0.74 (live 16t)** | 4.01 | 16.0 | 0.700 | 1414 | 0.733 (0.011) | 0.697 (0.011) | −0.033 | +0.003 |
| 1 | 5.42 | 21.7 | 0.707 | 1441 | 0.741 (0.012) | 0.705 (0.015) | −0.034 | +0.002 |
| 1.5 | 8.13 | 32.5 | 0.736 | 1550 | 0.756 (0.007) | 0.731 (0.013) | −0.020 | +0.005 |
| 2 | 10.84 | 43.4 | 0.747 | 1611 | 0.766 (0.005) | 0.749 (0.013) | −0.019 | −0.002 |
| 3 | 16.26 | 65.0 | 0.780 | 1871 | 0.786 (0.006) | 0.781 (0.011) | −0.006 | −0.001 |
| 4 | 21.68 | 86.7 | 0.791 | 2098 | 0.804 (0.007) | 0.801 (0.007) | −0.012 | −0.010 |
| 6 | 32.52 | 130.1 | 0.835 | 2627 | 0.831 (0.010) | 0.835 (0.006) | +0.004 | −0.001 |
| 8 | 43.36 | 173.5 | 0.867 | 3048 | 0.848 (0.006) | 0.856 (0.006) | +0.019 | +0.010 |

**Findings.**
1. **The null never reaches 0.50.** Under an IID shuffle our detector's bounce rate is 0.73–0.74 for every band up to Δ and rises to 0.85 at 8Δ. The reason is structural [B]: an episode opens when a bar's extreme first comes within `band` of the level, i.e. on the approach side; `classifyShape` calls an exit on the approach side a "rejection". For a memoryless path that starts at the band's edge, the approach-side exit is the likely one (gambler's ruin), so the verdict is entry-side biased by construction. The "choose k where p ≈ 0.50" criterion therefore **cannot select a k for this detector**; the point closest to 0.50 is the whole small-k plateau (k ≤ 1, p ≈ 0.73), not a crossing. D5 test 2 ("the shuffle yields ≈ 0.50 at the chosen k") **FAILS for every k** — reported, not hidden.
2. **Real − bootstrap ≈ 0 at every k** (|gap| ≤ 0.010). The stationary bootstrap preserves 1m autocorrelation and reproduces the real tape's bounce rate; the ~0.04 shortfall of the real tape *below* the IID null at small k is explained by that autocorrelation, not by level memory. On this 12,782-bar sample the touch telemetry shows **no measurable level edge** in either direction.
3. The live band (16 ticks, k ≈ 0.74) sits in the flat region of the curve; moving to k = 1 (Δ) changes the null by +0.008 and the real rate by +0.007 — no calibration signal either way.

## D3 — friction floor from our own fills [A], guess-grade

`trader_fills` MNQ: **424 fills, ids 1–424**, qty 425, `commission = 0.00 on all 424 rows` (assets USD/USDT) → **commission UNRESOLVED from fills (SIM records none)**. `decision_records.fill_price` is NULL on every row and `trader_orders.price` is a stale placeholder (30629.75 on Sept market orders), so slippage-vs-intent is **not observable**. What our fills + our tape do support: execution deviation vs the last completed 1m close (signed adverse-positive), fills with a bar within 5 min — **n = 81, ids 344–424** (the 343 older fills predate the stored tape, 08-19 10:00):

| Sample | n | mean ticks/side | median | p25 / p75 | adverse share |
|---|---|---|---|---|---|
| bot fills (order_id ≠ 0) | 44 | +4.11 | +3.0 | −2 / +8 | 61% |
| non-bot fills (order_id = 0, manual/system) | 37 | +15.1 | +19.0 | −2 / +49 | 70% |
| all | 81 | +9.15 | +6.0 | −2 / +19 | 65% |

This deviation includes up to 60 s of drift plus decision latency; it is an **upper bound**, not pure slippage. Half-spread: not stored; **ASSUMED** 0.5 tick/side (1-tick quoted spread). The code's own standing assumption is `kernel/shadow_ab.go:43`: "2 ticks per contract round trip (1 tick adverse per side), no commission".

**Floor (round trip, per contract, EXCLUDING commission — commission UNRESOLVED):**

| basis | slip/side | RT ticks | RT $ |
|---|---|---|---|
| bot median (n=44) | 3.0 | **7.0** | **$3.50** |
| bot mean (n=44) | 4.11 | 9.2 | $4.61 |
| all-fills mean (n=81) | 9.15 | 19.3 | $9.65 |

Labelled a **guess** (n = 44 bot fills). D5 test 3 (hand example: $0.35/side commission + 1 tick slip/side + 0.5-tick half-spread/side → 2×0.35 + 2×0.50 + 2×0.25 = **$2.20 = 4.4 ticks**) reproduces to the cent (PASS).

## D4 — PARKED recommendation (NOT APPLIED — no config, no knob, no DB write)

- **Band unit:** define the touch band in Δ (volatility-scaled), re-estimated per tape window, instead of a fixed 16 ticks. Today Δ = 21.7 ticks; **k = 1 → band = 22 ticks = 5.50 pts** (rounded to the tick) is the literature default (Garzarelli et al. 2014; Chung & Bellotti 2021). **This is a unit change, not an edge claim:** the ≈0.50 criterion did not select it (no k does with this detector), and real − bootstrap ≈ 0.
- **Bounce score:** report the touch telemetry's rejection rate **relative to the stationary-bootstrap baseline** (real − boot), not as a raw rate — the raw rate is 0.70 on memoryless data.
- **Detector (owner ruling needed before any change):** to make the 0.50 criterion meaningful, the verdict would need a symmetric definition (e.g. exit side relative to the level *midpoint*, or excluding episodes shorter than N bars). Out of scope here; measurement only.
- **Friction:** enter `$3.50 RT excl. commission` as the guess-grade floor; commission stays UNRESOLVED until a live (non-SIM) fill or a documented Tradovate MNQ rate is recorded.
- **Live touch band: UNCHANGED (16 ticks).**

## Owner-visible caveats for Part 2 (A15)

- The bounce probability shown anywhere in the UI as a raw rate (T4 chips: rejected/accepted) is not null-referenced; a 70% rejection rate is the memoryless baseline.
- SIM fills carry $0.00 commission; any P&L "net of friction" that reads `trader_fills.commission` is net of nothing.

---

## Closeout

- Commits: `ec6632f9` (fix, 20 files) · `b51f8f03` (marker) · this report (docs).
- Lock released, worktree `../vl-class35` removed, repo memory updated (`project_class35_replan_budget.md`).
- **Next action is the owner's:** "GO" → cutover per §7 in a flat window (outside 16:45–17:10 CT, no planner read in flight, no live arms), then quote the boot line, `🧮` line and the live `replans_left` for the ASIA chain.

---

## CUTOVER — DONE (owner GO, 2026-09-01) [A]

- **GO received ~17:13 CT.** Lock re-acquired (pid 1860416, alive). Gates at 17:13:31 CT: outside the 16:45–17:10 window ✓; DB OPEN 0, armed 0, API positions `[]`, open-orders `[]`, NT8 `positions snapshot account=Sim101 count=0` ✓ — **but the ASIA session read was in flight** (17:10:09 `📐 planner attempt 1/3 rejected` → `🧩 attempt 2/3 repair`; 17:13:05 `planner read for 2026-09-01:ASIA … already in flight`). **Held per A6.**
- **Read landed 17:23:14 CT:** `🗓️ PLAN written 2026-09-01 ASIA v1 (model deepseek-v4-pro, lifecycle no_trade …)` — fail-closed after three rejected attempts (class-34 style validator rejects; not this wave's concern). No claim after it. Gates re-quoted 17:23:37 CT: DB OPEN 0, armed 0, API positions `[]`, open-orders `[]`, NT8 snapshot `Sim101 count=0` (17:23:21), ASIA `armed: {}`.
- **Swap 17:23:54 CT:** `cp vl-bin vl-bin.prev.boot` · `mv vl-bin vl-bin.old.fef656a4` · `mv vl-bin.next vl-bin` (rev check `ec6632f9` passed first) · `kill -9 1625428`.
- **Boot checklist (6 s after the kill):**
  `09-01 17:24:00 🔐 BOOT INTEGRITY OK — rev ec6632f9de41 · built 2026-09-01T21:54:27Z · expected ec6632f9de41 · goldens PASS`
  `09-01 17:24:00 🧮 replan budget: recorded-counter (class 35) — spends: death_replan, owner_reread · free: <S>_scheduled_read, level_event, structure_mss (incl. fast-market), owner_reset, dormant/rearm + fail-closed markers · key dayplan_replans_used:<trader>:<date>:<session>:b<baseline>`
  Exactly ONE PID: `1908258`. `📐 NT8 instrument_info MNQ (MNQ 09-26): point_value=2 tick=0.25 — matches table ✓`. Feed re-warmed: the newest MNQ 1m bar in `bars` is `2026-09-01 17:24:00 CT`, written after the boot. `[ERRO]` lines since boot: **0**. Positions after boot: `[]`.
- **Live `replans_left` on the new binary (17:24:16 CT, `/api/plan/today?trader_id=…&session=`):**

| session | version | lifecycle | old binary (fef656a4, 17:02 CT) | **new binary (ec6632f9)** | fixture expectation |
|---|---|---|---|---|---|
| LONDON | 6 | active | 0 / 4 | **4 / 4** | 4 (zero spends) ✓ |
| NY | 5 | active | 0 / 4 | **4 / 4** | 4 (zero spends) ✓ |
| ASIA | 1 | no_trade (fail-closed) | 4 / 4 | **4 / 4** | 4 (v1 is free) ✓ |

  `system_config` counter rows: none yet (no `death_replan` / `owner_reread` has landed since boot — the first live spend will write `dayplan_replans_used:…:b1`).
- **Rollback (still valid):** `mv vl-bin vl-bin.bad.ec6632f9 && cp vl-bin.prev.boot vl-bin && printf 'fef656a4ee7c45860ad0237f48cef90c6b148d17' > deploy/RELEASE && kill -9 1908258`.
- **Still to observe live (A20):** a real `death_replan` spend and an owner re-read spend on this binary — neither has occurred yet; the proof so far is the fixture chain matching the live chain shape, plus the corrected live numbers above. Class 35's proving condition (a death on a wake-inflated chain now re-planning instead of fail-closing) has **not** occurred since boot — stated plainly.
