# AUDIT-0926 — SETTINGS & KNOBS (DS-102 slice)

- Lane: DS-102 · Branch: `audit/0926-knobs` · Claim: `fa5c05a7` · Base: `04ae1c2f` (RELEASE 6cac1b89, boot 3, live since 19:33:22 CT 2026-09-25; restarted 20:14:24 CT with FAST_MARKET_REASONING=max).
- Method: READ-ONLY. Live values from a DB copy (`audit.db`, 2.4GB, `.backup` of `data/data.db` opened `mode=ro`) and the live log `data/nofx_2026-09-25.log`. `.env` read with secrets masked. No writes, no API mutation, no deploys.
- Spec freshness (L3): built against `04ae1c2f`; every cited file:line re-read at this base.
- Evidence tiers: [A] ran/read it · [B] inferred · [C] speculation.

---

## 1. MAP — where settings live, in the order the live system touches them

1. `.env` (37 keys) → `config/config.go` (`getEnv*` helpers, defaults at lines ~200-236) → `config.Get()`.
2. `config.Config` struct (`config/config.go:65-~180`) — service/DB/security/market/NT/risk-limits knobs.
3. `store.StrategyConfig` (`store/strategy.go:716-1190`) — the strategy JSON: `ai_config.{coin_source,indicators,custom_prompt,risk_control,prompt_sections}` + `day_plan` + `regime` + `grid_config` + `publish_config`. Custom `MarshalJSON`/`UnmarshalJSON` (`strategy.go:806-866`) nest the five `json:"-"` compat fields under `ai_config`.
4. Knob registry: `store/knob_registry.go` (schema enumeration THROUGH the marshaller) + `store/knob_registry_table.go` (per-leaf classification: live/ineffective/candidate/suspended/advisory/display-only/infra/folded).
5. Per-session overrides: `DayPlanSessionOverride` (`strategy.go:1190-1213`), resolved by `SessionOverride` / `*For(session)` resolvers; precedence override → strategy → shipped default.
6. DB rows: `strategies` (config JSON), `traders` (bindings + interval/cadence/balance), `ai_models`, `exchanges`, `system_config` (counters/markers), `preferences`-style tables.
7. Env-only runtime knobs (188 `os.Getenv` sites) — never persisted, read per-cycle or at boot (seams, ATR/band/clock knobs).
8. Studio UI: `web/src/components/strategy/*Editor.tsx` + `web/src/types/strategy.ts` — the only places an owner edits persisted knobs.
9. Guide: `web/src/guide/content/*.ts` (settings.ts 1069 lines) — the owner-facing documentation; drift vs code = findings.
10. Agent/chat: `agent/skill_*_handlers.go` — reads some knobs for diagnosis, never trades.

---

## 2. CENSUS

### 2.1 Strategy schema census (counts)

- `store/strategy.go` JSON leaves: 230 tag lines; the production marshaller emits **174 schema paths** (boot line `schema=174` [A]).
- Registry classifications at boot [A]: **classified=200 · live=167 · ineffective=12 · candidate-unverified=9 · suspended=0 · advisory=0 · display-only=0 · infra=0 · folded=12 · env-shadows=n/a (not counted)**.
- The 12 ineffective: `max_margin_usage`, `min_position_size`, `max_contracts_enabled`, `notional_cap_enabled`, `external_data_sources` + 7 children (all pinned in the registry).
- The 9 candidate-unverified (field-grep only, method-readers possible): `context_limit`, `eod_flat_offset_min`, `fixed_overhead`, `htf_veto`, `last_entry_offset_min`, `longer_count`, `max_trades`, `suggestions`, `transition_standdown`. NOTE: several have since gained method readers — see findings.
- The 12 folded: `acceptance_rule`, `evening_digest`, `levels_fresh_by_tf`, `realign_cap`, `scenario_cap`, `structure_map`, `wake_min_interval_min`, `wake_on_15m_zone`, `wake_on_htf_ob`, `wake_on_htf_zone`, `wake_on_ifvg`, `wake_on_seated_invalidation`.

### 2.2 Env knobs

188 distinct `os.Getenv`/`os.LookupEnv` call names in non-test Go (`/tmp/env-knobs.txt` from the worktree grep). Notable families: AI-* (24 knobs: timeouts, retries, reasoning, tokens, temperature), ARM*/STOP*/BD*/STRUCTURE*/TOUCH*/STALE*/TRANSITION* (trading-law knobs), NT_* (bridge), CLAW402_*, RISK_* (absent from .env → code defaults 500/2/50000), *_SEAM test seams (ARMED_TEST_SEAM, STOP_ENTRY_SEAM, HISTORICAL_IMPORT_SEAM), SANDBOX_*.

`.env` has 37 keys (values masked; duplicates preserved):
`AI_HTTP_TIMEOUT_SECONDS=600 · AI_MAX_RETRIES=2 · AI_MAX_TOKENS=<set> · ARMED_TEST_SEAM=off · CLAW402_DEFAULT_MODEL=deepseek-v4-flash · CLAW402_WALLET_*=<set> · DATABENTO_* · DATA_ENCRYPTION_KEY=<set> · DB_PATH=data/data.db · DB_TYPE=sqlite · EOD_FLAT_LIMIT_TICKS=2 · EOD_FLAT_MARKET_AFTER_SEC=10 · EXIT_MECHS_SUSPENDED=0 · FAST_MARKET_REASONING=max ×4 (duplicate key, owner comments "planner reads stay MAX, fast-market included") · HISTORICAL_IMPORT_SEAM=on · HTF_VETO_MODE=cross · JWT_SECRET=<set> · NINJATRADER_DATA_DIR=<set> · NOFX_BACKEND_PORT=8080 · NOFX_FRONTEND_PORT=3000 · NOFX_TIMEZONE=UTC · NT_EXTRA_SYMBOLS=ES · NT_RUNTIME_SYMBOLS=true · NT_TRANSPORT=tcp · RSA_PRIVATE_KEY=<set> · STOP_ENTRY_SEAM=on · TRADING_MODE=futures · TRANSPORT_ENCRYPTION=<set>`

### 2.3 LIVE effective values (rows named — sample-id law)

- Live trader: `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265` (user `396db319-…`, name `hoang`) → strategy `a5b7662e-7bf7-49bb-9f09-7efa48f95ac8` (MNQ) → model `8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek` (enabled) → exchange `8d5c8af5-5366-4646-b1b6-0958db9e6f69` (ninjatrader, enabled). Second enabled deepseek row exists (`396db319-…_deepseek_0751c0b6` "DeepSeek 2") — exact-ID binding wins, no ambiguity for this trader [A].
- Live strategy `a5b7662e` (4824B) effective [A] (DB row + boot lines):
  - `strategy_type=ai_trading · language=en · prompt_variant` unset (futures venue rule) ·
  - coin_source static [MNQ] · klines 5m/20 + 4h/10, TFs [1h,4h,1d,15m,3m,5m] ·
  - indicators: raw/ema(50,200)/rsi(14)/atr(14)/volume/funding/svp ON; macd/boll/oi/quant/rankings OFF ·
  - risk_control: max_positions=3 · leverage 5/5 · ratios 5/1 · margin 0.9 (ineffective) · min_position_size=12 (ineffective) · **min_risk_reward_ratio=2** (code default 3.0) · **min_confidence=60** (code default 75) · **guardrails_enabled=false** (all 5 guardrail values stored but bypassed) · max_contracts_per_order=2 · hold_discipline=true · breakeven_enabled=false (trigger 40 stored) · trailing_enabled=false ·
  - day_plan: plan_enabled=true · **plan_mode=strict** · **one_setup_enabled=false [O]** · **picture_htf.enabled=true** · planner_timeframes [D,4h,1h,15m,5m] · structure_map=true (folded, honoured) · proximity_filter_atr=1 · max_levels=12 · scenario_cap=5 (folded, honoured) · flip_reread=true · acceptance_rule=5m_close · replan_cap=4 (top + every session) · sessions_enabled=[NY] · approval_required=false · evening_digest=true (folded) · realign_cap=10 (folded) · wake_on_htf_ob=true (folded legacy) · levels_fresh_by_tf=true (folded) ·
  - per-session blocks: NY {replan 4, min_grade B, max_trades 10} · ASIA {enable=true, replan 4, min_grade B, max_trades 7} · LONDON {enable=true, replan 4, min_grade B, max_trades 10} →
  - ABSENT (→ shipped defaults): htf_veto (ON via Regime nil), transition_standdown (ON), planner_contract (ON — A3), planner_fresh_tape (ON — A6), death_reread (ON), write_time_feasibility (ON), geometry_reference_levels (ON), one_setup_min_grade (B), htf_seats (2), t1_currencies ([USD]), entry_policy_default (market_in_zone), zone_max_pts (10), zone_rest_max_min (30), zone_place_within_pts (25), min_hold_min (3), replan base 4 [O].
- Live boot lines 20:14:26 [A]: `one setup: OFF[O]` · `picture-htf: mode=on rule=v1 SIM-only` · `geom_ref_ids=on(default)` · `entry law: bd_min_closes=1 bd_min_disp_atr=1.00 mss_min_disp_atr=0.50 accept_hold_min=10 stop_entry_offset_ticks=2 retest_wait_bars=6 **stop_entry_seam=ON**` · `arms: bias-coherent=warn · stop-entry=on(reclaim) · far-arm counter=on(3.0×ATR5m)` · folded-knob lines honouring every stored folded value listed above.
- `.env` effective: TRADING_MODE=futures · NT_TRANSPORT=tcp · FAST_MARKET_REASONING=max · EXIT_MECHS_SUSPENDED=0 (NOT suspended) · STOP_ENTRY_SEAM=on · HTF_VETO_MODE=cross · ARMED_TEST_SEAM=off · RISK_MAX_* absent → code defaults 500 USD / 2 concurrent / 50_000 USD notional / (contract cap in strategy=2).
- `system_config`: ~5k rows, telemetry counters + markers only — no user-facing knob lives there [A].

### 2.4 Studio UI coverage (control exists?)

DayPlanEditor.tsx controls (grep counts): picture_htf, plan_mode, min_scenario_quality, replan_cap, max_trades, planner_timeframes, proximity_filter_atr, min_grade, wake_on_level_events, t1_currencies, sessions_enabled, one_setup_min_grade, one_setup_enabled, max_levels, htf_seats, approval_required, flip_reread, death_reread, planner_model, acceptance_rule. RiskControlEditor.tsx covers all risk_control leaves. IndicatorEditor/CoinSourceEditor/PromptSectionsEditor/GridConfigEditor/PublishSettingsEditor cover their blocks.
NO control and NO frontend type: `zone_place_within_pts`, `planner_contract`, `planner_fresh_tape`, `write_time_feasibility`, `geometry_reference_levels`, `condition_status`, `fade_or_wide_k`, `last_entry_offset_min`, `eod_flat_offset_min`. Types-only: `zone_max_pts`, `min_hold_min`, `entry_policy_default`.

### 2.5 Guide coverage

Guide documents: W3 zone knobs (plays.ts + settings.ts), write_time_feasibility, geometry_reference_levels, condition_status, planner_fresh_tape, the suspend env restore. NOT documented: `planner_contract`, `fade_or_wide_k`, `last_entry_offset_min`, `eod_flat_offset_min`, `NOFX_TIMEZONE`.

---

## 3. FINDINGS (severity ordered)

### P1-1 — LIVE stop-entry seam is ON against the standing 2026-09-05 owner ruling
- Files: `.env` `STOP_ENTRY_SEAM=on`; reader `kernel/entry_law.go:138` `StopEntrySeamOn()`; gate `trader/armed_executor.go:1378` `if !stopEntrySeamOn() { continue }`.
- Live evidence [A]: boot line `🎛 entry law: … stop_entry_seam=ON` (20:14:26) and `🎯 arms: … stop-entry=on(reclaim)` (main.go:629, from `trader/arms_boot_line.go:21`). The seam is the ONLY gate on the stop_entry order path; with it ON, stop-entry legs (breakout-retest fallback and reclaim) can reach the wire (subject to retest window for non-reclaim and the stop-side guard).
- Contradiction [A]: the `.env` inline comment ABOVE the key says "…Stays off until a cancel-confirmation wave lands."; SYSTEM-MAP MAPCHECK paragraph says "OFF at the 2026-09-05 cutover by owner ruling: NO stop entry is placed at all"; `kernel/entry_law.go:135` comment says "Default OFF".
- Refute attempt: could be a deliberate owner re-enable after the AddOn proof (`addon proven 2026-09-23-m21` in the RELEASE marker) — plausible [B], but nothing in `.env` comments, guide, or a ruling message records it; the inline comment actively says it should be off. Cannot confirm intent from the box.
- Consequence: 0 stop-entry placements since boot 3 [A] (log grep count=0), so no loss so far; a breakout-retest or reclaim leg could now place where the ruling said never.
- Action: owner/CTO confirmation — if deliberate, the inline `.env` comment, the entry-law comment and SYSTEM-MAP must say so (guide content law).

### P2-1 — Saved prompt text claims exit mechs are SUSPENDED; the live env un-suspended them
- The live strategy `a5b7662e` `prompt_sections.trading_frequency` says: "the saved breakeven_trigger_points (40) and trailing are both SUSPENDED at the wire (trader/exit_mechs_suspend.go)".
- Live [A]: `.env EXIT_MECHS_SUSPENDED=0` → `exitMechsSuspended()` false (`trader/exit_mechs_suspend.go:37`) → NOT suspended since the owner's 2026-09-15 `.env` comment ("unsuspend breakeven/trailing (0B freeze lifted; knobs control them)").
- Guide `settings.ts:583-608` also still labels both rows "SUSPENDED (0B)" with "env EXIT_MECHS_SUSPENDED=0 restores" — true as a conditional but stale as the LIVE state.
- Harm today: zero — both knobs are false in the live config, so nothing fires [A]. Misleading to the model (the prompt is the live instruction text) and to the owner. P2.
- Refute: no. The env value and the code path are unambiguous.

### P2-2 — `.env` keys with NO reader (dead)
- `NOFX_TIMEZONE=UTC` — grep across all non-test Go, deploy/, scripts/, docker-compose, start.sh: zero readers [A]. Timezone is pinned to CT everywhere (`kernel.CanonicalZone`, main.go:156 "host TZ ignored", owner rule 2026-08-19). `.env.example` even suggests `Asia/Shanghai` — an owner setting it would change nothing and think they had.
- `CLAW402_DEFAULT_MODEL=deepseek-v4-flash` — the only references are writers: `api/handler_onboarding.go:72` `os.Setenv(...)` and the onboarding payload; the payment client uses the hardcoded constant `mcp/payment/claw402.go:53 DefaultClaw402Model` [A]. The `.env` value is read by nothing; it coincidentally equals the constant.
- `FAST_MARKET_REASONING=max` ×4 duplicate keys — `godotenv`-style loaders keep the LAST occurrence; all four are `max` (owner comment: intentional), so behaviour is max either way. Presence bug class (duplicate key), NOTE unless the loader semantics differ — see §5.
- Class match: 09-03 audit "two env vars with no consumer" (registry header) — this is the same class recurring. P2.

### P2-3 — Shipped-default-ON knobs with no Studio control and no frontend type
- `zone_place_within_pts` (B1, default 25 ON), `planner_contract` (A3, default ON), `planner_fresh_tape` (A6, default ON), `write_time_feasibility` (default ON), `geometry_reference_levels` (default ON), `condition_status`, `fade_or_wide_k`, `last_entry_offset_min`, `eod_flat_offset_min`: zero references in non-test, non-guide `web/src` [A].
- The owner cannot see or change them from the Studio; only a raw JSON edit or agent path would touch them. Several ARE in the guide (W3, write_time_feasibility, geometry) — documented but uneditable; `planner_contract`, `fade_or_wide_k` and the offsets are undocumented too.
- P2: misleading/uneven surface; no behaviour loss (defaults are the intended shipped posture).

### P2-4 — Guide drift: planner_contract absent from the guide
- `planner_contract` (WAVE PLANNER A3, 2026-09-25, nil=ON) has a registry row, a resolver, prompt fragments and pins, but `web/src/guide/content/*.ts` never mentions it [A]. GUIDE CONTENT LAW (a knob/chip/gate added by a wave must reach `web/src/guide/content/*` in the same PR) was not followed for A3.
- P2: guide drift vs 6cac1b89.

### P2-5 — Folded-knob legacy switch keeps an independent effect, silently
- `wake_on_htf_ob` is folded, BUT per the registry row it keeps its OWN effect: the HTF order-block wake class runs only when stored true, ANDed with the new `wake_on_level_events`. Live: stored true → OB class runs [A] (boot line "wake_on_htf_ob (order-block class)=true honoured").
- The Studio has no control; the owner cannot turn the OB class OFF without editing JSON (or turning the master wake switch off). P2 (surprise coupling, pinned by `TestKnobPrunePin_WakeCandidates_SingleSwitchOwnsOB` so it is INTENTIONAL — downgrade to NOTE for severity; keep as P2 only for discoverability).

### P2-6 — sessions_enabled=["NY"] in the Studio but ASIA and LONDON run
- Live `a5b7662e`: `sessions_enabled=["NY"]`, yet the ASIA and LONDON session override blocks carry `enable: true`, and `sessionEnabledForStrategy` (`trader/auto_trader_planconfig.go:74-79`) makes the override authoritative [A]. The top-level subset is silently overridden for two of three sessions.
- The resolver comment says this is the designed inherit/override model and the accordion shows chips. Severity P2 (UI surprise: the "sessions" checklist reads NY-only while the bot trades three sessions). Not a code conflict.

### P2-7 — Two `is_default=1` strategy rows
- `strategies`: id `""` "MNQ SIM Default" (is_default=1, is_active=1) and `578ac8f6-…` "MNQ SIM Default" (is_default=1, is_active=0) [A]. `GetDefault()` is `First()` with no ORDER BY → which row wins is unspecified. Today nothing live binds the default (the trader binds a5b7662e), so NOTE-level, but a fresh/empty trader could read either row's config.

### P2-8 — Ineffective knobs still visible and editable in the Studio
- Registry-ineffective but present in `RiskControlEditor.tsx`: `max_margin_usage`, `min_position_size` [A]. `min_position_size` is a crypto-path hardcode (12/60, engine_position.go); `max_margin_usage` is prompt-text only. Editing them in the Studio changes nothing that gates a trade. The EffectiveChip (W1) renders the registry label, so the UI likely already warns — verify in §5.
- P2 (label-vs-code), already inventoried by the registry; listed for completeness.

### NOTE-1 — `max_trades` is LIVE (method reader found); the registry row is stale
- Live stores `max_trades` 10/7/10 per session. RESOLVED [A]: `store/strategy.go:1566 DayPlanConfig.MaxTradesFor` → `trader/auto_trader_session.go:97 sessionTradeCapBlocked` — per-session trade caps ARE enforced on the live box (NY 10 · ASIA 7 · LONDON 10; a cap of 0 = no entries that session). The registry's `candidate-unverified` row for `max_trades` is stale and should be reclassified live with these consumers (the row's own note requires a quoted method-grep — this is it).

### NOTE-2 — `均衡策略` 4104ca0a is `is_active=1` but bound to NO trader
- No `traders` row references it [A]. Active-but-unbound is display-only; the Studio "active" chip can lie about what runs.

### NOTE-3 — guardrails master switch OFF (live, owner-documented)
- `guardrails_enabled=false` with daily loss 450 / profit 900 / max trades 3 all stored-but-bypassed. The SAVED PROMPT TEXT itself tells the model "no daily trade cap is enforced … never applied" [A] — honest. Server-side env kill switches (RISK_MAX_* code defaults 500/2/50_000) remain the backstop [A]. No defect; recorded so the "everything guarded" impression cannot form.

### NOTE-4 — AI_MAX_TOKENS set in .env; boot prints AI params with WARNINGs for unset ones
- main.go P0 2026-08-19 block prints every effective AI parameter; `.env` sets AI_MAX_TOKENS, AI_HTTP_TIMEOUT_SECONDS=600, AI_MAX_RETRIES=2. The four identical FAST_MARKET_REASONING=max lines + owner comments confirm the restart intent "planner reads stay MAX". No defect.

---

## 4. REFUTED / WITHDRAWN

- "No daily loss cap is enforced anywhere" — REFUTED [A]: `config/config.go:223-236` defaults `RISK_MAX_DAILY_LOSS_USD=500`, `RISK_MAX_CONCURRENT_TRADES=2`, `RISK_MAX_NOTIONAL_USD=50_000` and `kernel/risk_limits.go` enforces them regardless of the strategy guardrails master. Withdrawn; folded into NOTE-3.
- "The registry mislabels transition_standdown (no reader)" — REFUTED by method reader `StrategyConfig.TransitionStanddownEnabled()` (strategy.go:775) and `ResolveHTFVeto` for htf_veto; the candidate rows' notes already require a method-grep before any change. Withdrawn as findings; the candidate rows stay as registry maintenance debt (NOTE).
- "STOP_ENTRY_SEAM=on is a boot-time-only artefact" — REFUTED [A]: the gate is read per placement (`armed_executor.go:1378`) from the env via `kernel.StopEntrySeamOn()`, so the live process is genuinely on (finding P1-1 stands).
- "eod_flat_offset_min / last_entry_offset_min have no reader" — PARTIALLY REFUTED: `LastEntryOffsetFor`/`EODFlatOffsetFor` (strategy.go:1266-1280) + `*ForWithSource` exist as method resolvers [A]; the registry's field-grep note is stale for at least these two (they move to "method-reader confirmed" in NOTE-1-style). The UI gap (P2-3) remains — the methods read a field the Studio cannot edit.

---

## 5. WHAT I COULD NOT VERIFY

- How `.env` is loaded — RESOLVED [A]: `main_dotenv.go:28` `godotenv.Load(".env")` (v1.5.1), fails open to the process environment; within the file later duplicate keys win. All four `FAST_MARKET_REASONING` lines are `max`, so last-wins is still `max` [A] — no ambiguity, only hygiene.
- Whether the EffectiveChip warns for `max_margin_usage`/`min_position_size` in the live UI (I did not run the frontend; the chip renders the effective-feed origins, `useStudioEffective.ts` — the label text stays [B]).
- Method-level reader check for the remaining candidate-unverified rows (6 of 9 checked: `max_trades` live, `eod_flat_offset_min`/`last_entry_offset_min` method-resolved; `htf_veto`/`transition_standdown` method-resolved). Still unverified: `context_limit`, `fixed_overhead`, `longer_count`, `suggestions`.
- `plans`, `armed_orders` live contents (DS-106's slice), `telegram_configs`, `config_changes` history semantics.
- Which session clock windows are actually open for ASIA/LONDON on the live box (session registry owns the clock; the enable override gates the strategy, the registry gates the windows — the net live set is [B] all three).

---

## 6. CROSS-SLICE NOTES (one line each)

- To DS-106 (pipeline): stop-entry seam is LIVE-ON (P1-1) — the entry-path census should count stop-entry legs as placeable.
- To DS-107 (system): `.env` duplicates (`FAST_MARKET_REASONING` ×4) and dead keys (`NOFX_TIMEZONE`, `CLAW402_DEFAULT_MODEL`) are settings-slice, but the loader semantics question is a system question.
- To CTO: P1-1 needs an owner confirmation (deliberate re-enable vs accidental); everything else is P2/NOTE.

---

## 7. PRUNE-LIST MAP (parked 7 remove / 5 fold / 17 keep / 2 dead) onto 6cac1b89

Source: `docs/superpowers/reports/2026-09-18-knob-prune.md` (knob table rows 1-14 + KEEP list). Re-verified at 04ae1c2f:

| # | knob | parked fate | TODAY at 04ae1c2f | still true? |
|---|------|-------------|--------------------|-------------|
| 1 | levels_fresh_by_tf | REMOVE→folded | folded; live stores true; boot line honours | ✓ |
| 2 | htf_score_multiplier | REMOVE, hard-wire 1.0 | field + resolver GONE; `kernel.HTFScoreMultiplier` const | ✓ |
| 3 | seat_1h_zone | REMOVE | field GONE; 1h seat unconditional | ✓ |
| 4 | structure_map | REMOVE UI, keep path | folded; live true honoured | ✓ |
| 5-7 | wake_on_15m_zone/htf_ob/ifvg (+2) | COLLAPSE | collapsed into wake_on_level_events; OB keeps own effect | ✓ |
| 8 | acceptance_rule | FOLD | `AcceptanceRuleFor` returns the one rule; live 5m_close | ✓ |
| 9 | realign_cap | FOLD | folded; live 10 honoured | ✓ |
| 10 | evening_digest | FOLD | folded; live true honoured | ✓ |
| 11 | wake_min_interval_min | FOLD | folded; const 30 | ✓ |
| 12 | scenario_cap | FOLD | folded; live 5 honoured | ✓ |
| 13-14 | last_entry_ct / eod_flat_ct | DELETE | GONE from struct + readers | ✓ |
| — | KEEP (17) | keep | all 17 still present + gated (plan_enabled, planner_model, plan_mode, planner_timeframes, sessions, structural_stop, proximity_filter_atr, max_levels, htf_seats, one_setup_enabled+min_grade, min_scenario_quality, replan_cap, approval_required, flip_reread, wake collapse pair, t1_currencies) | ✓ (t1_currencies added as 18th keep by the wave) |

Verdict: the parked prune list matches today's code exactly; the only drift is additive (picture_htf, planner_contract, planner_fresh_tape, W3 zone knobs landed later with their own registry rows).

---

## 8. CENSUS DELIVERABLE (CTO 04:14Z spec) — 2026-09-26

**File: `docs/superpowers/reports/2026-09-26-audit-knobs-census.csv` — one row per setting.**

Columns exactly as specified: `name, kind, defined_at, type, code_default, ui_fallback, live_value, origin, readers, controls, label_guide_match, status`.

Generation (all in-branch, reproducible): `scripts/audit0926fields` (Go AST walk of the StrategyConfig graph + config.Config — 187 + 29 rows; leaf set matches the production marshaller's 174 schema paths EXACTLY, zero drift) · `scripts/audit0926/scan_env.py` (195 env call sites + fallbacks) · `scripts/audit0926/build_census.py` (merges the knob registry's 200 rows, the marshalled defaults, the live strategy row `a5b7662e`, the masked `.env`, and the reader/UI/guide greps). Hand-filled: `controls` and the label/guide verdicts per row.

**Row counts:** 419 total — strategy 187 · env 203 · system_config 29.
**By status:** LIVE 387 · DEAD 16 · SHADOWED 13 · CONFLICT 3.
**Origins:** owner-set strategy rows (O) 94 · env-set rows (E) 30 · the rest shipped defaults.
**label_guide_match:** yes 75 · no 110 (incl. guide-only and UI-only) · n/a 234.

Status legend: LIVE = read by non-test code · DEAD = no reader anywhere (or registry-ineffective) · SHADOWED = folded (stored value honoured, control removed) · CONFLICT = two sources disagree · UNREGISTERED = no registry row (zero rows today).

The three CONFLICT rows are the three live contradictions the census pins: `day_plan.sessions_enabled` (P2-6), env `STOP_ENTRY_SEAM` (P1-1), env `FAST_MARKET_REASONING` (4 duplicate `.env` keys).

## 9. REFUTE PASS (CTO 04:14Z) — P1-1 and P2-6

### P1-1 — the owner ruling itself, quoted with date + path

- **`docs/superpowers/reports/2026-09-05-wave-b-stop-entry.md` (~:247), 2026-09-05**: *"THE LINE THIS BOOT WILL ACTUALLY PRINT (owner ruling, 2026-09-05). The cutover runs with `STOP_ENTRY_SEAM=off` … `🎯 stop-entry: seam=OFF — NO stop entry is placed (owner ruling 2026-09-05: cancel-confirmation wave owed; broker-side stacking)`."* Same report :516 *"`STOP_ENTRY_SEAM=off` is in force"*; :541 records the original sin — *"`STOP_ENTRY_SEAM=on` in `/home/hoang/nofx/.env`. This is a LIVE path, not a dormant one — that is how 21 malformed orders reached a broker."*
- **`.env` :39-43 (inline comment)**: *"TURNED OFF 2026-09-05 by owner ruling: the cancel path is unconfirmed … Stays off until a cancel-confirmation wave lands."* — and the line directly below it reads `STOP_ENTRY_SEAM=on`.
- **`docs/superpowers/SYSTEM-MAP.md:282` (MAPCHECK)**: *"`stopEntrySeamOn` (:92 — OFF at the 2026-09-05 cutover by owner ruling: NO stop entry is placed at all)"*.
- **`kernel/entry_law.go:138`**: the reader — `strings.EqualFold(strings.TrimSpace(os.Getenv("STOP_ENTRY_SEAM")), "on")`.

**Both sides, no decision:**
- FOR "accidental/stale": every written artefact (`.env` comment, SYSTEM-MAP MAPCHECK, entry-law comment, wave-B report) still says OFF; nothing anywhere records a re-enable ruling.
- FOR "deliberate re-enable": the ruling's own condition was *"Stays off until a cancel-confirmation wave lands"* — and W117-B cancel-truth (PR #216: F8-F12, ConfirmCancel tx, cancel-confirmation timeout, book-settled cancels) merged into dev 2026-09-25, BEFORE boot 3's restart (20:14:24 CT). The boot-3 RELEASE marker also carries *"addon proven 2026-09-23-m21"*. Both halves of the stay condition are arguably met, and the restart with `FAST_MARKET_REASONING=max` was an owner session where the seam could have been flipped deliberately.
- Harm today: 0 stop-entry placements since boot 3 [A]. Owner confirmation required (unchanged from the draft).

### P2-6 — contradicting call sites: none found

- `sessionEnabledForStrategy` (`trader/auto_trader_planconfig.go:72-88`): *"A per-session Enable override wins over the subset"* — override first, subset as fallback, default [NY].
- `sessionRunnable` (`:105-118`): explicit per-session override → authoritative; otherwise inherit registry Enabled AND the `sessions_enabled` subset. The comment documents the *"inherit/override model the accordion chips already show"* — the UI surprise is designed-in, not accidental.
- All three non-test readers of the knob — `auto_trader_planconfig.go:117`, `auto_trader_planner.go:225-229`, `auto_trader_session.go:52` — go through the override-first resolvers. No call site reads the top-level subset as authoritative.
- Verdict: the finding STANDS as written (P2, UI surprise: Studio checklist reads NY-only while ASIA/LONDON run via their `enable: true` overrides). No code conflict.

## 10. FINDINGS ADDED BY THE CENSUS (new in this round)

- **P2-9 — two more dead `.env` keys:** `NOFX_BACKEND_PORT=8080` and `NOFX_FRONTEND_PORT=3000` have zero readers anywhere in non-test Go [A] (ports come from elsewhere at boot). Extends the P2-2 dead-key class (now five dead keys incl. `DATABENTO_DATASET`, whose load was removed per `config/config.go:211` 6.8 note).
- **P2-10 — `max_trades` registry row is stale (confirmed by the census):** the CSV row `day_plan.sessions.max_trades` is LIVE (readers: `MaxTradesFor` resolver + `sessionTradeCapBlocked`) while the registry still files it `candidate-unverified` — NOTE-1 now has the per-row evidence in the CSV. Same for `last_entry_offset_min`/`eod_flat_offset_min` (method readers `LastEntryOffsetFor`/`EODFlatOffsetFor`).
- **Re-checked against the CSV:** P1-1 (CONFLICT row present) ✓ · P2-1 (env EXIT_MECHS_SUSPENDED LIVE row, prompt text stale) ✓ · P2-2 ✓ · P2-3 (9 no-control rows confirmed, all default-ON) ✓ · P2-4 (`planner_contract` absent from guide) ✓ · P2-5 (`wake_on_htf_ob` SHADOWED row) ✓ · P2-6 ✓ · P2-7 (strategy rows `is_default` — DB-level, outside CSV) ✓ · P2-8 (4 ineffective DEAD rows still UI-visible) ✓.

## 11. ETA

The CSV is generated and committed with this update. Remaining: your review. If the controls column needs deeper per-row prose for a subset, name the rows and I'll fill them; a full second pass over all 419 controls is ~1-2 h more if you want every row hand-written rather than family-mapped (the family maps cover the trading-law families explicitly).
