# FLOW PIPELINE + FULL PROMPT REFERENCE — verified @ origin/dev `8f5f26ee` (2026-09-15)

For a new agent session: this file describes the EXACT flow, every stage with its
file:line reference and its verification command, then the FULL prompt contracts
verbatim. Evidence tier [A] = read the exact line today. Live binary = `3ce4281a`.

---

## PART 1 — EXACT FLOW PIPELINE (stage → code → verify)

### 1.0 Boot
- `main.go` ~34-690: .env → logger → config → crypto service → research →
  store → DB sink → migrations → JWT secret → CT timezone pin → trader manager →
  strategy migrations → NT snapshot sink → `LoadTradersFromStore` → boot
  integrity assert → boot ledger (~80 lines) → API server → agent → telegram.
- **Verify:** `journalctl -u nofx -o cat | grep 'BOOT INTEGRITY OK'` — must show
  `rev <live> · built … · expected <RELEASE> · goldens PASS`.

### 1.1 Data in (NT8 → cache → DB)
`ninjascript/VLTraderTCPClient.cs:716` (signals) + `VLBarsSubscriptionManager.cs`
(bars) → TCP v3 (`tcp_framing.go:108`) → `tcp_server.go:1708` readLoop →
`:1659` enqueue → `:1592` drain → `bar_cache.go:323` ring (cap 2500) →
`bar_persist_wire.go` → `store/bar_history.go:265` (contract-stamped).
- **Verify:** `sqlite3 -readonly data/data.db "SELECT contract, COUNT(*) FROM bars WHERE symbol='MNQ' GROUP BY contract;"`
  + journal `bars integrity OK … total=` at boot.

### 1.2 Planner cycle (plan authoring)
Trigger (`auto_trader_planner.go:193`) → claim+clock-hold+preflight (:863) →
input assembly (:2143) → `BuildPlannerPrompt` (`planner_prompt.go:439`) → AI
stream → parse `ParsePlanDocCappedWithMinRR(…, at.armMinRRFor(nil))` (:1605) →
validation chain (:1654-1801: flip bias, mislabeled levels, machine facts, FVG,
breakdown, `validateAuthoredScenariosAt`, confirm-grace) → economics honesty
(`scenario_economics.go:139-228`) → `AppendPlan` → fail-closed no-trade doc.
- **Verify:** journal `scenario economics PASS/REFUSED` lines carry
  `obstacle R=… arm R=… issues=[…]` + boot counter line with
  `contradictions refused= corrected=`; `plans` table gains a version row.

### 1.3 Arm cycle (the only futures entry)
`armed_executor.go:197` — see pipeline-map.md §C for the 17 stages. Key refusals:
session risk :336 → one-setup :369 → geometry :484-512 → confirm :580 → arm gate
:2114 (R:R :2150, min-SL :2162, HTF veto :2175) → one-live-arm :897 → entry gate
:705 (daily_force_flat leg `entry_gate.go:163`) → consult :744 → leg-kind → place.
- **Verify:** `armed_orders` rows + `system_config` keys
  `arm_refusals_0b:<trader>:<date>:<session>:<class>`; journal `armed …` lines.

### 1.4 Decision cycle (legacy AI path — strict-blocked for futures entries)
`auto_trader_loop.go:174-935` → `engine_analysis.go:57` (14 pre-gates) →
`engine_position.go:44` (13 validations) → `auto_trader_orders.go:138` (12 entry
gates) → `decision_records` row with `RiskCheckPassed/RiskCheckError`.
- **Verify:** `decision_records` latest rows;
  `/api/risk/gate-blocks?trader_id=<id>`.

### 1.5 Execution truth layer
Placement proof `place_confirm.go:22` · cancel proof `cancel_confirm.go` ·
one-contract `one_contract.go:112` · fill-confirmed close `close_sync.go:82` ·
reconcile `reconcile.go` · SIM-only `tcp_trader.go:289`.
- **Verify:** `/api/positions` (open) vs `/api/positions/history` (closed only);
  journal `positions snapshot account=Sim101 count=N`.

### 1.6 Observers & exits
Watcher observer `engine_prompt_observer.go:61` (watch-only AI) ·
auto-breakeven `auto_trader.go:157` + trailing `auto_trader_trailing.go`
(unsuspended 2026-09-15; strategy knobs gate them) · EOD flat / T1 force flat
`auto_trader_clock.go:368/:698`.
- **Verify:** journal `auto-breakeven …` / `trailing_armed …` lines.

---

## PART 2 — FULL PROMPT DETAIL (verbatim contracts)

### 2.1 Planner OUTPUT contract — `kernel/planner_prompt.go:752`

```text
## OUTPUT — one JSON object, reasoning FIRST, no prose outside it
{
  "reasoning": "<your read: what the auction is doing and why this plan — ≤200 words, decision-focused>",
  "bias": {"direction": "long|short|neutral", "conviction": "high|medium|low", "flip_condition": "<explicit>"},
  "levels": [{"price": <n>, "label": "<PDH|ONH|nPOC…>", "grade": "A|B|C", "instruction": "<verb>"}],  // max {maxL}, MUST include ≥3 below AND ≥3 above the current price
  "scenarios": [{"id": "S1", "level_id": "<candidate id from map, or null when map id is NULL>", "trigger": "<setup>", "condition": "reclaim|hold|sweep_reclaim|reject|acceptance|breakout_retest|fvg_entry|breakdown_continue|breakup_continue", "direction": "long|short", "target_chain": [<n>,…], "invalid": "<line>", "quality": "A+|A|B|C", "chain_after": "<S# of the sweep_reclaim this fvg_entry follows, or omit>", "confirm": {"rule": "touch|1x5m_close|2x5m_close|1m_mss|time_hold", "ref_price": <n>, "side": "above|below"}, "confirm2": {…} (OPTIONAL second trigger leg), "fvg": {…}, "breakdown": {…}, "arm": {…}],  // 1..{maxS} — confirm{} REQUIRED per scenario; fvg{} iff fvg_entry; arm{} legal ONLY on sweep_reclaim split or armable singles
  "death_condition": "<the single line that invalidates this whole plan>",
  "death": {"price": <level>, "side": "below|above", "rule": "2x5m|5m_close"},
  "flip": {"price": <level>, "side": "below|above", "rule": "2x5m|5m_close", "flip_to": "long|short"},
  "day_type": "trend|balance|<optional>"
}
Rules: levels chosen ONLY from the ranked table above; levels MUST be at least 3
points apart — near-duplicates are merged by the system; S/D & FVG are
confluence, never standalone. Copy the EXACT label from the table row for the
price you choose — never re-label a table level as a different anchor (a zone
price relabeled 'PDH/PDL/PDC' is a phantom level and is REJECTED at write).
Quality: A+ = highest-conviction setup, A = strong, B = workable, C =
machine-demoted (trigger level consumed at write — G5) — use C honestly for a
demoted setup, never as a default. The scenario MIX must follow the regime +
day_type. If price sits BELOW PDL the plan MUST include a SHORT-direction
scenario; ABOVE PDH, a LONG-direction scenario.
```

### 2.2 ENTRY LAW — `kernel/planner_prompt.go:781`

```text
ENTRY LAW (per condition — the machine REJECTS violations by name): reject →
touch ONLY (fade_requires_touch) with a structure stop ≥2 ticks beyond the level
· fvg_entry → touch ONLY, entry price inside the FVG edge..CE band ·
sweep_reclaim → leg-1 touch at the sweep ref, leg-2 1m_mss (1x5m_close accepted
as the leg-2 alternative) · reclaim → 1x5m_close or 1m_mss, never 2x5m_close ·
breakout_retest → touch at the retest + stop-entry fallback, 1x5m_close legal for
the break leg · acceptance/hold → time_hold (price holds beyond ref for
ACCEPT_HOLD_MIN minutes of 1m closes) with 1x5m_close as fallback ·
breakdown/breakup_continue → 1 confirming close + displacement ≥ BD_MIN_DISP_ATR
×ATR5m (BD_MIN_CLOSES=1) or stop-entry — 2x5m_close is legal ONLY here. Default
confirm = 1x5m_close (single close).
```

### 2.3 ARM rules — `kernel/planner_prompt.go:793`

```text
ARM SPLIT vs ARM SINGLE (class 38 — the validator refuses every other shape):
legs[] are the sweep_reclaim SPLIT contract and nothing else — EXACTLY 2 legs,
confirm=touch at the sweep ref, leg 1 rests there (wait_confirm false) and leg 2
chains (wait_confirm true) on confirm2 = 1m_mss or 1x5m_close with leg 2's rule
EQUAL to confirm2.rule, and the top-level entry/stop/target mirror leg 1. EVERY
other condition — breakdown_continue, breakup_continue, reject, fvg_entry — must
arm SINGLE: arm{} with wait_confirm:true and no legs. A breakdown/breakup arm
additionally needs breakdown{} with entry_mode=pullback.
ARMS FOLLOW THE BIAS: with the decision path closed, a RESTING ORDER IS THE ONLY
WAY INTO THE MARKET — a scenario with no arm cannot trade, however well argued.
Every scenario in the plan's bias direction that has a concrete trigger price
MUST carry an arm. A long plan with no long arm is invalid; a short plan with no
short arm is invalid.
ENTRY TYPE FOLLOWS THE CONDITION (the machine derives it and REFUSES a
contradiction): a play that rests AT a price is a limit (reject→limit,
fvg_entry→limit, sweep_reclaim→limit); a play that is only valid once price
travels BEYOND its trigger is a stop entry (reclaim→stop_entry — BUY STOP above
the level for a long, SELL STOP below for a short). A waterfall rests as a
PULLBACK limit AT the broken level.
FEASIBILITY CONTRACT: an arm{} MUST be gate-feasible or it is REFUSED every cycle
— R:R = |target−entry| ÷ |stop−entry| must be ≥ 2.0 (ARM_MIN_RR) AND the stop
distance must be ≥ {MinSLATRMult}× the current 5m ATR. If your setup cannot meet
BOTH, OMIT arm{} and let the AI path take it. Keep targets REAL: a planned R:R
above ~6 is a fantasy target and gets WARN-flagged at write.
```

### 2.4 SCENARIO ECONOMICS CONTRACT — `kernel/planner_prompt.go:782`

```text
SCENARIO ECONOMICS CONTRACT (required for NEW AUTHORING; legacy reads remain
UNKNOWN): every scenario states entry zone, trigger, confirm{}, structural
invalidation (invalid), protective stop, first opposing obstacle with
level/family/price provenance, planned response there, arm target and both
implied R values.
Include economics:{entry_zone:[low,high],first_obstacle:{price:<n>,level:<label>,
family:<family>,response:pass_through|reduce|exit|decline_setup},r_to_obstacle:<n>,
r_to_arm_target:<n>,target_path_exception:<reason if needed>,role_exceptions:[…]}
on EVERY scenario.
R = abs(price-entry)/abs(entry-stop), from the same proposed geometry, before
rounding/costs. State r_to_obstacle and r_to_arm_target EXACTLY as computed (6+
decimals, never a rounded shorthand like 2.0); the machine recomputes both and
refuses a stated R that drifts more than one tick of price distance from
geometry, though an arm-target R whose computed value meets the minimum floor is
auto-corrected to the computed value and accepted. A sub-1R obstacle is WARN
only. No structural, fixed-R, ATR, partial, trailing or mandatory-1R target
policy is prescribed.
NEW-authoring REFUSALS: missing complete economics/obstacle; arm target absent
from target_chain within one MNQ tick without target_path_exception; obstacle
beyond arm target; stated R differing from geometry by more than one tick of
price distance (an arm-target R whose machine-computed value meets the minimum
R:R floor is auto-corrected and accepted instead — never round R values).
target_chain is GUIDANCE for the executor AI (which sets the actual take_profit)
— validated for reachability at write time but never enforced at execution (D2).
```

### 2.5 Death/flip — `kernel/planner_prompt.go:776-778`

```text
death.flip objects are MACHINE-EVALUATED — choose levels from your level list and
a rule; they must match the prose lines.
The flip and death MUST be DIFFERENT events: never the same level AND same rule
for both (a flip at the same tick death fires is void). A short-biased plan's
flip sits BELOW its death line or uses a stricter rule, so the flip can actually
fire.
Every scenario's confirm{} is MACHINE-EVALUATED the same way: rule + ref_price +
side, and ref_price MUST equal a number written in that scenario's
trigger/invalid prose.
```

### 2.6 No-trade gates — `kernel/planner_prompt.go:691`

```text
## No-trade gates (the machine enforces the windows below; the rest are yours)
  - balance-day (open inside prior value area AND VAs overlap) → edges-only, or skip
  - opening gap >1.2×ATR or open outside the prior range → NEVER fade; the gap is a target
  - no A/B zone in reach AND no pool swept by 10:30 CT → declare the skip in the plan
  - lunch <start>–<end> CT: no new entries (refused on the AI-decision path only —
    no band predicate exists in the arm path, so with plan_mode=strict nothing
    refuses an entry here)
  - Tier-1 news → stand aside until a fresh post-news swing prints
```

### 2.7 BIAS-TREE — `kernel/planner_prompt.go:163`

```text
## BIAS-TREE (machine branches — your reasoning MUST state the branch you took,
e.g. "bias-tree: inside-day long LOW")
  1. close > PDH → bull-continuation, conviction HIGH
  2. sweep of PDH + close back inside → bear, conviction MEDIUM (mirror: PDL)
  3. inside the day (between PDH/PDL) → direction of close vs PDC, conviction LOW
  4. closed OUTSIDE the prior day's range but now inside → NO bias (write neutral)
  5. premium/discount: longs ONLY below the 50% mark of the dealing range,
     shorts ONLY above it
  6. draw-on-liquidity: the runner target is the DRAW — the nearest opposing
     liquidity pool beyond the first target
```

### 2.8 A1/A2/A2b/A2c chains — `kernel/planner_prompt.go:775`

```text
A1: your reasoning MUST open by naming the bias-tree branch you took, then argue
from it. A2: an fvg_entry SHOULD chain after a sweep_reclaim (chain_after: S#) —
bare gaps at non-A/B origins get a WARN at write, not a reject. A2b (machine
grounding): author an fvg_entry scenario ONLY from the ## FRESH FVGs list above —
copy its direction and lo–hi EXACTLY. If the list is empty, do NOT author any
fvg_entry (invented/stale gaps are REJECTED at write). A2c (FVG demand): when
## FRESH FVGs is NON-empty and at least one candidate's direction agrees with
your bias, you SHOULD author an fvg_entry from that candidate; if you decide not
to, state the reason in ONE line in your reasoning.
```

### 2.9 Futures decision prompt (executor AI) — `engine_prompt_futures.go`

Hard Constraints (`:120-128`): trade ONLY `<sym>`; every open_long/open_short
MUST include stop_loss and take_profit as ABSOLUTE prices in tick increments;
stop distance ~1.5–3× ATR (sanity ~15–50 pts); reward ≥ {minRR}× risk;
confidence ≥ {minConf}; one position at a time, no pyramiding; leverage always
1; position_size_usd = intended contract notional (start with 1 contract).

Plan block (`plan_render.go:156-194`, injected at `:152-156`): `# DAY PLAN
(<session>) — preferred: follow it · a valid off-plan setup may still be traded
(cite "off-plan")` · Bias/Levels/Scenarios rows · `No-trade NOTES (the model's
prose — NOT machine-enforced…)` · `Plan dies if: <cond>` · cite rule:
`cited_scenario = "S1"|…|"off-plan"` REQUIRED.

Decision Process (`:216-222`): 1. open position → hold or close for profit/stop;
2. flat → 5m/15m/1h setup?; 3. chain of thought THEN JSON; 4. `action=wait …
and action=hold … are valid, frequently-correct answers. Do NOT force a trade.`

Output (`:225-241`): XML tags `<reasoning>` and `<decision>`; `<decision>`
MANDATORY before `<reasoning>`; reasoning ≤200 words; decision is a JSON ARRAY,
example `{"symbol":"<sym>","action":"open_long","leverage":1,
"position_size_usd":60000,"stop_loss":21480.00,"take_profit":21560.00,
"confidence":80,"cited_scenario":"S1"}`; wait example `[{"symbol":"<sym>",
"action":"wait"}]`. Field description `:244-255`: action enum
`open_long|open_short|close_long|close_short|hold|wait`; concrete numbers not
formulas; `cited_scenario` REQUIRED on every open when a DAY PLAN is shown;
ARMED PATH preference.

Live map tail (`:266-284`): `# Live map (dynamic — re-read each bar)` → SVP →
KEY LEVELS → bias → PLAN STATUS → weekly.

### 2.10 Observer prompt (watch-only watcher AI) — `engine_prompt_observer.go`

ObserverInput (`:20-43`): Symbol, Side, EntryPrice, StopLoss (original stated),
TakeProfit, Thesis (entry reasoning, verbatim), EntryConfidence, AgeMinutes,
PnLPoints, MFEPoints, MAEPoints, CurrentStop, BreakevenFired, TrailLevel,
PrevStatus, StructureLine (machine truth), TouchLines.

Prompt (`:61-110`): Clock → `# ROLE: POSITION OBSERVER (WATCH-ONLY)` (zero order
authority) → `## THE POSITION` → `## THE ORIGINAL ENTRY DECISION (verbatim —
this is the ONLY thesis you judge)` (fenced) → `## MACHINE STRUCTURE (Go-computed)`
→ `## TOUCH` → `## YOUR QUESTIONS` (2: did the original invalidation trigger;
does machine structure contradict — `structure_conflict: none|warning|confirmed`).

Output contract (`:93-98`): one JSON object —
`{"thesis_status":"intact|weakening|invalidated","invalidation_cited":"REQUIRED
iff invalidated…","structure_conflict":"none|warning|confirmed","note":"one short
paragraph","confidence":0-100}`. Any action-like field is IGNORED and logged
(`ParseObserverAssessment:116+`).

### 2.11 Economics refusal strings — `scenario_economics.go`

- `:245` — `implied_r %s stated %.6f disagrees with geometry %.6f (entry %.2f
  stop %.2f; tolerance %.2f price points)` when `|stated−computed|×risk > tick`.
- `:236-244` — **R4 (owner 2026-09-15):** arm-R computed ≥ min floor →
  auto-correct stated := computed, accept, count `Corrected`. minRR ≤ 0 keeps
  the strict refusal. Downstream minimum gates still refuse sub-floor arms.
- `:251-256` — target_path absent within tick without `target_path_exception`.
- `:263-267` — obstacle beyond arm target.

---

## PART 3 — HOW TO VERIFY EVERY STAGE (5-minute pass)

```bash
cd /home/hoang/nofx-rrfix   # clean worktree at origin/dev
git fetch origin && git log -1 --oneline origin/dev
grep -n 'corrected' kernel/scenario_economics.go | head -3     # R4 present
grep -n 'armMinRRFor(nil)' trader/auto_trader_planner.go       # R4 call site
grep -n 'EXIT_MECHS_SUSPENDED' /home/hoang/nofx/.env           # unsuspend line
curl -s http://127.0.0.1:8080/api/health                       # live rev
sqlite3 -readonly /home/hoang/nofx/data/data.db \
  "SELECT COUNT(*) FROM trader_positions WHERE status='OPEN';"
journalctl -u nofx --since '30 min ago' -o cat | grep -a 'scenario economics:' | tail -1
```

See companion files `2026-09-15-cto-handoff.md`, `2026-09-15-pipeline-map.md`,
`2026-09-15-trace-anchors.md` for the rest.
