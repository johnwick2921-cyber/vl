# CTO HANDOFF — nofx system map, 2026-09-15 (for a fresh session)

**Scope:** everything a new CTO/agent session needs to understand + trace this
repo WITHOUT re-doing the deep reads. All line numbers were read at **origin/dev
tip `8f5f26ee`** (marker "RELEASE 3ce4281a"), except live-state facts which were
queried against the running binary.

**Evidence legend (standing repo rule):**
`[A]` = directly verified (ran the command / read the exact line / saw the frame)
`[B]` = inferred from strong evidence · `[C]` = speculation.
Everything in these handoff files was verified `[A]` on 2026-09-15 unless marked.

---

## 0. The system in one loop (30 seconds)

```
NT8 C# AddOn ──MNQ bars/TCP──▶ Go bot ──▶ AI writes a PLAN (scenarios) ──▶
validator (honesty checks) ──▶ ARM GATES (7 doors) ──▶ SIM order back over TCP
```

- **Data:** NinjaTrader 8 Tradovate real-time bars via TCP (protocol v3),
  cached in `BarCache` key `"MNQ"`, persisted to the `bars` table with contract
  stamps. NT8 is BOTH data and execution — not optional.
- **Brain:** every cycle the planner asks DeepSeek to author a day-plan
  (levels → scenarios with entry/stop/target + economics). The machine
  re-validates everything; contradictions are refused.
- **Arm path (the only futures entry):** when a scenario's trigger fires, a
  12-stage gate chain admits or refuses the arm; admitted arms place ONE
  contract via stop/limit entries. **SIM only** (hard gate).
- **Decision path (legacy crypto AI cycle):** still runs every cycle but in
  strict futures mode cannot open positions; its guardrails gate only itself.
- **Everything is recorded** (40+ tables, journal counters, refusal census) so
  "why did it not trade?" is always answerable.

---

## 1. Session-start checklist (run in this order)

```bash
cd /home/hoang/nofx
git fetch origin --quiet && git log -1 --oneline origin/dev      # spec-freshness: read the TIP before building anything
bash deploy/nofx-lock.sh status                                   # rc 0 free · 1 held · 2 stale — NEVER rm a lock, reclaim on the record
curl -s http://127.0.0.1:8080/api/health                          # live rev must match RELEASE + deploy/RELEASE
sqlite3 -readonly data/data.db "SELECT COUNT(*) FROM trader_positions WHERE status='OPEN';"   # flat gate truth
journalctl -u nofx --since '5 min ago' -o cat | grep -a 'BOOT INTEGRITY OK' | tail -1
```

Then read, in order:
1. This file (sections 2–8)
2. `2026-09-15-pipeline-map.md` (companion — full A–K logic inventory)
3. `2026-09-15-trace-anchors.md` (companion — tables/counters/journal/API)
4. Repo memory: `/memories/repo/system-read-2026-09-15.md` + `full-pipeline-2026-09-15.md` (same content, session-local)
5. `AGENTS.md` / `CLAUDE.md` at repo root (canons, deploy law)

---

## 2. Live state as of 2026-09-15 22:20 CT

- **Live rev `3ce4281a4b6b`** = R4 wave ("auto-accept over-minimum arm R"),
  PID 1834811, boot integrity OK, goldens PASS. dev tip `8f5f26ee`.
- **Position:** MNQ LONG 1ct @ 29241.25 Sim101, opened 21:06 CT (first entry
  since R4 + one-setup OFF). Unrealized +$68 at mark 29275.
- **R4 verified working:** planner authors clean scenarios (arm R=4.89,
  `issues=[]` at 18:35); `contradictions refused=5` class eliminated.
- **One-setup:** owner turned OFF (D1). Refusal census today: ASIA
  `geometry_rr=3`, LONDON/NY `geometry_no_provenance=3/4`, `no_trade_band=1`.
- **Exit mechs unsuspended 22:15:** `EXIT_MECHS_SUSPENDED=0` appended to
  `~/nofx/.env` (line 47). Breakeven/trailing now obey ONLY the strategy knobs
  (both currently OFF by owner). Boot line `suspended=0`.
- **JWT for API curls:** mint HS256 with `.env` JWT_SECRET, claims
  sub=`396db319-…-516ff0008f53`, email=`johnwick2921@gmail.com`, iss=`nofxAI`
  (working token cached at `/tmp/pw-token`).
- **Trader:** `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265`
  ("hoang") · strategy `a5b7662e-7bf7-49bb-9f09-7efa48f95ac8` ("MNQ") ·
  Sim101 · plan_mode=strict · min R:R 2 · one_setup 0 · breakeven/trailing 0.

---

## 3. Reading map (tiered)

**Tier 1 — 30-minute map (7 files, in order):**
1. `AGENTS.md` "Project state" + canons
2. `main.go` (~700 L) — boot order
3. `market/data.go:46-54,246-249` — futures branch reads BarCache
4. `kernel/planner_prompt.go:439` + `kernel/scenario_economics.go:139-228` — prompt + honesty check (R4 auto-correct)
5. `trader/armed_executor.go:197-884` + `trader/entry_gate.go:147-353` — gate chain
6. `trader/ninjatrader/tcp_trader.go` + `provider/ninjatrader/tcp_server.go` — order out / fills in / SIM gate
7. `ninjascript/vltrader_tcp_PROTOCOL.md` — wire spec (Go↔C# lockstep)

**Tier 2 — subsystem docs** (exist on disk in dirty `~/nofx` main tree only;
NOT tracked at dev tip — treat as historical, verify line numbers): kernel/,
market/, trader/, trader/ninjatrader/, provider/, provider/ninjatrader/,
web/, cmd/nq_smoke/ `CLAUDE.md` files.

**Tier 3 — reports:** this handoff + `2026-09-15-pipeline-map.md` +
`2026-09-15-trace-anchors.md` (new, authoritative); `2026-09-01-full-system-audit.md`
(794 L, previous best map); `2026-06-02-strategy-studio-universal-futures-map.md`
(UI); `2026-08-14-dayplan-p1{,-b}-map.md` (kernel levels, pre-pivot).

**Tier 4 — machine graphs:**
- **cgc** (FalkorDB Lite, `cgc` CLI at `~/.local/bin/cgc`): reindexed
  `--force` 2026-09-15 (~35 min). Query read-only: `cgc query "CYPHER"`.
- **understand-anything knowledge graph: ABSENT** — the `.understand-anything/`
  dir and `knowledge-graph.json` claimed by AGENTS.md do not exist in the tree
  (verified 2026-09-15). The claim is stale; these handoff files are the
  replacement.
- **`2026-07-10-architecture-codebase-map.md`: ABSENT** (deleted from history).
  `2026-09-15-pipeline-map.md` is its successor.

---

## 4. Standing canons (do not re-derive; cite them)

- **WORKTREE LAW:** `~/nofx` main tree = ONE dispatch (deploy-only, dirty 66
  files — NEVER build from it). All other work in `git worktree add` dirs.
- **MAIN-TREE LOCK:** `deploy/nofx-lock.sh acquire|heartbeat|with-heartbeat|
  status|release|reclaim <session> "<task>"`. Stale ≠ dead — corroborate, then
  `reclaim` (names holder + why). No `git reset` on dev outside deploy dispatch.
- **PUSH-EMPTY-AT-ACCEPT:** claim the branch on origin BEFORE building.
- **FLAT GATE + NO UNATTENDED DEPLOYS:** deploy only on owner's "go"; DB OPEN=0
  + NT8 snapshot count=0; RELEASE=<sha> before kill; rollback copy; kill -9
  (systemd Restart=on-failure); boot-watch `🔐 BOOT INTEGRITY OK` ≤90s; marker
  pushed before lock release. **Build ONLY from a fresh GitHub clone** — local
  `git clone --no-local` misses `refs/remotes/origin/*` and builds the WRONG sha
  (verified failure 2026-09-15).
- **SIM-only.** Never weaken `isAccountTradeable` / `NT_ALLOWED_ACCOUNTS`.
- **GUIDE CONTENT LAW:** any knob/gate/default change ships with
  `web/src/guide/content/*` + `GUIDE_BUILT_REV` bump in the SAME PR.
- **AUDIT-PLAYBOOK LAW:** audits cite `docs/superpowers/AUDIT-CHECKLIST.md`.
- **SPEC-FRESHNESS LAW:** `git log -1 -- <spec>` before building against a spec.
- **CTO REVIEW RULE (user memory):** always commit + push; granular commits;
  PR from create URL; quote PR number/URL in every report.

---

## 5. Known-issues queue (verified current)

1. **EquityChart "Invalid Date"** — `web/src/components/charts/EquityChart.tsx:187`
   unguarded `new Date(point.timestamp)`; backend sends `"2006-01-02 15:04 CT"`
   (`kernel/tz.go:40`) which JS cannot parse. Fix = copy the guard from
   `PositionHistory.tsx:40-48`.
2. **~~planApi.getExpectancy missing~~ — CORRECTED by the 20-agent sweep:**
   `getExpectancy` IS defined as a `planApi` method at
   `web/src/lib/api/plan.ts:817` and called at `ExpectancyPanel.tsx:102` and
   `InstrumentsDrawer.tsx:76`. The earlier "missing" claim was wrong and is
   retracted. (A dashboard pageerror, if still seen, must be traced elsewhere.)
3. **Raw i18n keys** `ACTIVE_NODES`/`SYSTEM_READY`/`MODELS_CONFIG` at
   `AITradersPage.tsx:626/631/643` render as raw keys (absent from translations).
4. **Risk-control coverage gap (owner asked 2026-09-15):** daily profit target,
   max daily trades, blackout, consistency gate only the DECISION path — the ARM
   path (real futures entry) never consults them. Daily loss reaches arms only
   via decision-path `daily_force_flat` trip → `entry_gate.go:163` leg.
   `/api/risk/status` honestly lists `not_enforced: max_notional_usd,
   kill_switch_armed, daily_loss_limit_usd`. Candidate wave: wire these into the
   arm chain as refusal legs.
5. **Exit-mech honesty (owner asked):** suspension now lifted; the UI knobs
   still give no "suspended" indicator — candidate: badge on the toggles.
6. `agent/skills/scenarioEconomics.ts` guide content exists but is not
   registered in `GuidePage` section list.
7. WSL2 clock drift ERRO at boot (log-only warning; `deploy/fix-wsl2-clock.sh`
   exists).

---

## 6. Deploy runbook (condensed, full law in AGENTS.md)

1. Owner "go" only · lock `acquire rrfix` · flat gate (DB + journal NT8 snapshot)
2. PR merged → **fresh GitHub clone** → checkout MERGED sha → `go test ./...`
   (SUITE_EXIT=0) → `go build -o nofx-bin .` → `go version -m` stamp == sha,
   modified=false
3. `echo -n <sha> > deploy/RELEASE` (before kill) · `mv nofx-bin
   nofx-bin.old.<sha12>` · `cp` build in · `kill -9 <MainPID>`
4. Poll ≤90s for `BOOT INTEGRITY OK` · `/api/health` rev == sha · marker commit
   (RELEASE + GUIDE_BUILT_REV) → push `HEAD:dev` · `release`
5. Rollback: restore `nofx-bin.old.<rev>` + RELEASE + kill again.

---

## 8. 20-AGENT VERIFICATION SWEEP (2026-09-15) — corrections this file is bound by

A 20-agent read-only sweep re-verified every claim in this handoff package
against dev tip `8f5f26ee`. Every semantic claim survived; line-number drift was
corrected. The corrected anchors are the ONLY ones to trust:

- `kernel/scenario_economics.go` — Corrected counter :113, boot line :134,
  validateNewScenarioEconomics :142, issues :194, auto-correct branch :240-243,
  refusal format :245. ALL EXACT.
- `trader/armed_executor.go` — chain calls at :197/:336/:369/:580/:588/:660/
  :705/:744 (definitions live in session_risk.go:120, one_setup_wiring.go:105/
  222, entry_gate.go:353); armGateVerdictFor def :2114 (R:R string :2151 "below
  arm min", min-SL :2160, HTF veto :2172); placement def :1174;
  placeOneStopEntry :1548. armRefusalClass is a FUNCTION at :1746 (not a map).
  UpsertArm has 9 sites (475/806/862/1220/2191/2539/2596/2647/2652).
- `trader/entry_gate.go` — EntryGate def :147; refusals: daily_force_flat :172,
  strict :185-193 (+207), bias :207, direction :219, invalidated :244, SHADOW
  :259, R:R :282, min-SL :293, one_open_position :305; entryGateForArm def :353,
  entryGateForDecision def :415.
- `trader/auto_trader_loop.go` — runCycle :174, CME gate :220, arm manage :432,
  AI call :550, stale-bar :763, close-first :811, saveDecision :926.
- `kernel/engine_analysis.go` — GetFullDecisionWithStrategy :57, cme_closed :69,
  contract_roll :93, CheckPreTrade :135, DailyGuardrails :152-198, token cap
  131072 :259, snapshot :286, OI :312-325, SVP :336, plan context :475,
  ownership :537, callWithSchemaRetry maxParseRetries=2 :561.
- `kernel/engine_position.go` — validateDecision def :43, F1 rr refs :129-151,
  confidence :199, min-SL :214, HTF veto :261, transition :274, dead-plan
  :285-292, min-quality :300.
- `kernel/planner_prompt.go` — BuildPlannerPrompt :439, OUTPUT :743-835, FRESH
  FVGs header :520 (not 583), ENTRY LAW :781, ARM :793, ECONOMICS :782 (R4
  sentence at :785), death/flip :776-778, no-trade :691, BIAS-TREE :140/163-170,
  A1/A2 :775-776.
- `kernel/engine_prompt_futures.go` — buildFuturesPrompt def :59, hard
  constraints :120-128, plan block :152-156, decision process :216-222, output
  :225-241, field desc :244-255, live map :266-285.
- `kernel/engine_prompt_observer.go` — ObserverInput :20-43, BuildObserverSystem
  Prompt :61, OUTPUT contract :107-113 (not 93-98), ParseObserverAssessment :116.
- `kernel/plan_render.go` RenderPlanBlock :150-187.
- `kernel/levels_*.go` — AssembleScoredLevels :100, pipeline :124-135,
  DefaultHTFDetectionTFs :589; typeEvidence :89 EXACT, but zoneEvidenceByKind
  :150, zoneTFMult :159, HTF×1.2 :167, reversalBonus :170, zoneSizeMult :227,
  freshMult :388, zoneFreshMult :407, grade :722, Tier1ProxTicks :286,
  ClusterTicks :737, MaxLevels :54, MinSideLevels :820, seating :590-621;
  EqualHighsLows :34, SupplyDemand :121, FVGNoise :186, fvgMinGap :191,
  FairValueGaps :233, OrderBlocks :321, OB lookback :301, ExtractMultiDay :45,
  coverage :273-275, Round :21, Gap :57, OpeningRange :128.
- `store/*` — MaxLevels :939, ScenarioCap :941, ClampLimits ends :226,
  SafeDefaultMinRiskReward :76; log_events :47, touch_episodes :39;
  StopUnanchoredKey :24, ArmRefusalKey :48; UncarriedEditsKey :439, LifecycleLog
  :542; RiskCheckPassed/Error :54/:55; armed_orders TableName :133, states
  :120-129; StampOneSetup predicate :132.
- `provider/ninjatrader` — tcp_server Start :1046 / Stop :1113 / acceptLoop :1375
  / readLoop :1708 / enqueueBarUpdate :1659 / drainBarIngest :1592 / SendSignal
  :1128; bar_cache cap 2500 :25, Upsert :323, Get :376, SeedHistorical :230;
  ProtocolVersion=3 at tcp_framing.go:109; VLTraderTCPClient.cs HandleSignal
  :743; tcp_trader placeEntry :324, isAccountTradeable :296, PlaceLimitEntry
  :437, PlaceStopEntry :498, MoveStopToBreakeven :655.
- `market/*` — Get :28, GetWithExchange :33, futures branch :50-54, long-TF
  :80-96, isStaleData call :73 (def :773), GetWithTimeframes :190, F8 clamp :226,
  Normalize :670 (CME early-return :672), IsCMEFuturesSymbol :50, TickSize :121,
  PointValue :133, indicators :203-228 exact, Kline :134, bars_market_bridge
  lives in trader/ninjatrader/ (wire :20, barsFromCache :44, barsToKlines :72).
- `api/*` — handler_plan.go offsets: today :250, overlay :910, reread :1164,
  reset :1212, realign :2067, approve :1960, ownership :97; handleRiskStatus
  :183; handleGateBlocks :30.
- `agent/*` — buildAgentTools :473, missingRequiredFields :229; everything else
  exact (9 skills, flag matrix, handler lines).
- `main.go` boot anchors ±3 lines all confirmed; `deploy/nofx-lock.sh`
  subcommands + stale 300s confirmed; nofx.service Restart=on-failure
  RestartSec=5; deploy/RELEASE tracked.
- **JWTSecret default line: `config/config.go:120-121`** (AGENTS.md's "67-69" is
  stale).
- **Trader interface: `trader/types/interface.go:43-105` (19 methods)** — not
  trader/types.go. A separate GridTrader interface adds 4 more methods.
- **Databento lag: code says ~3h** (`provider/databento/client.go`, circuit
  breaker 5 fails/5 min at :24); the "8h" figure exists only in CLAUDE.md prose.
- **NEVER verify against the main tree `~/nofx`** — it holds pre-R4 code
  (4-value scenarioEconomicsIssues) and pre-dev line numbers.

TRUST STATEMENT: after this sweep, every anchor in §1-§7 of this file was either
confirmed exact or corrected above. No fabricated references remain.

## 7. Where the answers live (index of prior waves)

| Question | File |
|---|---|
| Why no trades 09-11→09-15? | session memory R1-R4 sections (`/memories/session/plan.md`) |
| TF grading big→small (2.3× 4h vs 1m) | `plan.md` R1 + `levels_score.go` |
| FVG / S-R reaction in-session | `plan.md` R2 |
| Hardcoded-vs-knob (structural stop) | `plan.md` HARDCODED-Q&A |
| R4 auto-accept ruling | PR #128, deployed 3ce4281a |
| Full pipeline A–K | `2026-09-15-pipeline-map.md` |
| Tables/counters/journal/API anchors | `2026-09-15-trace-anchors.md` |
| Prior full-system read (older rev) | `/memories/repo/system-read-2026-09-15.md` |
