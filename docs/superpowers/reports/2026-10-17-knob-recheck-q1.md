# 2026-10-17 — Knob recheck Q1 (DS-101)

Base: `origin/release/2026-10-16-32` @ `a8b849161` (release #32).
Scope: 68 knobs = the first 68 rows of `store/knob_registry_table.go` (A–F), from
`/home/hoang/fleet-1017/knobs-q00.txt`.

Method per knob: SCREEN = Studio control + TS default (`web/src/types/strategy.ts`)
+ guide card (`web/src/guide/content/*`); BOT = Go resolver/reader nil-absence
behaviour + every production reader; STORE = registry Status/Consumers/Note. All
claims are [A] on the release #32 head unless noted.

## Verdict counts

| verdict | count |
|---|---|
| MATCH | 52 |
| MISMATCH | 0 |
| HIDDEN | 4 |
| DEAD | 12 |

**No P0 and no P1.** Every non-MATCH knob is P2 (vestigial / inert / cosmetic) —
none changes entries, exits or size against a disagreeing screen.

## HIDDEN (bot behaviour, no visible control)

| knob | severity | evidence |
|---|---|---|
| `entry_policy_default` | P2 | No Studio control — `web/src/guide/content/settings.ts:290` "API/config field"; `web/src/types/strategy.ts:163` optional string. BOT stamps new plans: `store/resolve_source.go:ResolveEntryPolicyDefault` + `trader/auto_trader_planner.go` (registry row). TS comment "absent = market_in_zone (shipped default)" matches the resolver; a stored plan doc is never re-stamped (guide `plays.ts:134`). Hidden but documented and default-correct — not the owner's OFF/ON shape. |
| `evening_digest` | P2 | KnobFolded — toggle removed (`store/knob_registry_table.go:63`); `store/strategy.go:1681 EveningDigestOn` = `c != nil && c.EveningDigest` (constant OFF unless a stored true). SCREEN: no control; `web/src/types/strategy.ts:125` "FOLDED". BOT honours a stored true. |
| `fade_or_wide_k` | P2 | No Studio control (`grep fade_or_wide_k web/src` → nothing). Registry `:157`: "W2 LABEL knob: sets exclusion (a)'s k; changes what is RECORDED, never what is armed. Zero = C5 default 1.28." Reader `trader/fade_facts.go:155`. |
| `enable_raw_klines` | P2 | `web/src/components/strategy/IndicatorEditor.tsx:212-222` force-sets `enable_raw_klines=true` on mount and hides the "Required" card on futures (`:251` "DEAD lever on futures … a switch that does nothing"); flag stays true underneath. BOT: only reader is `agent/skill_management_handlers.go:1197` (agent path) — the futures engine never gates on it. |

## DEAD (no effective reader)

| knob | severity | evidence |
|---|---|---|
| `acceptance_rule` | P2 | KnobFolded — dropdown removed; `store/strategy.go:AcceptanceRuleFor` returns the one rule (1×5m close); a stored other value is logged and never takes effect (`store/knob_registry_table.go:13`). |
| `context_limit` | P2 | KnobCandidate — `ContextLimit` is a computed API output from `store.ModelContextLimits`, no stored-strategy reader (`store/knob_registry_table.go:34`). |
| `fixed_overhead` | P2 | KnobCandidate — token-breakdown component computed in `store/strategy.go:2757`, no behavioural reader (`store/knob_registry_table.go:66`). |
| `external_data_sources` | P2 | KnobIneffective — zero engine consumers (`store/knob_registry_table.go:65`). |
| `ai_config.indicators.external_data_sources.name` | P2 | KnobIneffective — child; `FetchExternalData` has no production caller (`store/knob_registry_table.go:252`). |
| `ai_config.indicators.external_data_sources.type` | P2 | KnobIneffective — child (`:253`). |
| `ai_config.indicators.external_data_sources.url` | P2 | KnobIneffective — child (`:254`). |
| `ai_config.indicators.external_data_sources.method` | P2 | KnobIneffective — child (`:255`). |
| `ai_config.indicators.external_data_sources.headers` | P2 | KnobIneffective — child (`:256`). |
| `ai_config.indicators.external_data_sources.data_path` | P2 | KnobIneffective — child (`:257`). |
| `ai_config.indicators.external_data_sources.refresh_secs` | P2 | KnobIneffective — child (`:258`). |
| `data_path` | P2 | Standalone flat field. Registry `:42` KnobLive but Note: "crypto data fetch (FetchExternalData has no production caller); inert on futures" — reader `kernel/engine.go:815` is never reached. |

## MATCH (52 — one line each; screen default = Go nil/absent behaviour)

- `altcoin_max_leverage` — crypto-only (CLASS 151), `kernel/engine_analysis.go:552`.
- `altcoin_max_position_value_ratio` — crypto-only, `kernel/engine_analysis.go:554`.
- `approval_required` — `store/strategy.go:1024` `bool` absent→false; TS "OFF (default) = fully automatic".
- `atr_multiplier` — crypto grid only (`trader/auto_trader_grid_levels.go:35`).
- `atr_periods` — `kernel/engine_analysis.go:791`, numeric.
- `blackout_enabled` — `*bool`→`boolOrDefault(rc.BlackoutEnabled,false)` OFF; TS "default OFF" (`strategy.ts:437`).
- `blackout_end_ct` — `kernel/engine_analysis.go:167`, string HH:MM.
- `blackout_start_ct` — `kernel/engine_analysis.go:167`.
- `boll_periods` — `kernel/engine_analysis.go:792`.
- `breakdown` — per-scenario field `kernel/plan_confirm.go:190`; structural, not a toggle.
- `breakeven_enabled` — `*bool`→`hlBool(rc.BreakevenEnabled,false)` OFF (`trader/auto_trader.go:407`); TS "Default OFF".
- `breakeven_trigger_points` — default 50 (`trader/auto_trader.go:199`); TS "default 50".
- `breakeven_trigger_r` — `store/planner_knobs.go:ResolveBreakevenTriggerR`; 0/absent = OFF = points rule.
- `btc_eth_max_leverage` — crypto-only, `kernel/engine_analysis.go:551`.
- `btc_eth_max_position_value_ratio` — crypto-only, `kernel/engine_analysis.go:553`; TS default 5.
- `buffer_points` — `store/structural_geometry.go:ResolveStructuralStop`; missing uncalibrated value refuses.
- `condition_status` — runtime live|shadow map (`trader/armed_executor.go:36`), bot-written state, not a user knob.
- `config` — grid config object (`trader/auto_trader_grid_regime.go:157`).
- `config_visible` — `store/strategy.go:652` `bool gorm default:true`; publish-visibility only.
- `consecutive_loss_halt` — `*int` nil→`breakerHaltN` (env else 8 = ON); Studio `RiskControlEditor.tsx:1119 enabled={config.consecutive_loss_halt !== 0}` shows enabled when null; TS "absent = inherit (…else 8 — ON)". Screen ON ↔ bot ON.
- `consistency_enabled` — `*bool`→`boolOrDefault(rc.ConsistencyEnabled,false)` OFF; TS "default OFF".
- `consistency_max_day_pct` — `kernel/engine_analysis.go:169`.
- `created_at` — metadata (`trader/auto_trader_calendar.go:36`).
- `custom_prompt` — `kernel/engine_prompt_futures.go:257`.
- `daily_loss_enabled` — `*bool`→OFF (`kernel/risk_limits.go`); TS "absent = OFF since r22".
- `daily_loss_limit_pct` — crypto grid only (`trader/auto_trader_grid.go:187`).
- `daily_loss_limit_usd` — `kernel/engine_analysis.go:158`.
- `daily_profit_enabled` — `*bool`→OFF; TS "default OFF".
- `daily_profit_target_usd` — `kernel/engine_analysis.go:161`.
- `death_reread` — `*bool` nil→ON (`store/strategy.go:1841 DeathRereadEnabled` = `c==nil || c.DeathReread==nil || *c.DeathReread`); Studio `DayPlanEditor.tsx:678 on={cfg.death_reread !== false}` shows ON when unset; guide "nil=ON". Screen ON ↔ bot ON.
- `death_reread_retry_min` — `store/strategy.go:1746` nil = wake_min_interval_min; numeric.
- `decision_process` — `kernel/engine_prompt_futures.go:213`.
- `description` — `api/strategy.go:76`, metadata.
- `direction_bias_ratio` — crypto grid only (`trader/auto_trader_grid_levels.go:103`).
- `distribution` — crypto grid only (`kernel/grid_engine.go:153`).
- `ema_periods` — `kernel/engine_analysis.go:789`.
- `enable` — session override `*bool` nil = inherit `SessionsEnabled` (default [NY]) (`trader/auto_trader_planconfig.go:73`); TS "⚪ inherit / 🔸 override".
- `enable_atr` — `bool` absent→false; `kernel/engine_prompt.go:241`; Studio `IndicatorEditor.tsx` `checked=config.enable_atr` → OFF when undefined.
- `enable_boll` — `kernel/engine_prompt.go:249`.
- `enable_direction_adjust` — crypto grid only (`trader/auto_trader_grid_regime.go:65`).
- `enable_ema` — `kernel/engine_prompt.go:221`.
- `enable_funding_rate` — `kernel/engine_prompt.go:265`.
- `enable_macd` — `kernel/engine_prompt.go:229`.
- `enable_multi_timeframe` — `kernel/engine_prompt.go:209`; `store/strategy.go:2664` Normalize fills true only when the klines block was never set (matches Studio `enable_multi_timeframe: current.length > 1`).
- `enable_oi` — `kernel/engine_prompt.go:261`.
- `enable_rsi` — `kernel/engine_prompt.go:233`.
- `enable_svp` — `bool` absent→false; TS "default OFF"; `kernel/engine_analysis.go:331`.
- `enable_volume` — `kernel/engine_prompt.go:257`.
- `entry_standards` — `kernel/engine_prompt_futures.go:203`.
- `eod_flat_offset_min` — `store/strategy.go:EODFlatOffsetFor` resolver chain; `trader/auto_trader_clock.go:1091`.
- `excluded_coins` — `kernel/engine.go:613`.
- `flip_reread` — `bool` absent→false; `store/strategy.go:1730 FlipRereadEnabled` = `c != nil && c.FlipReread` (OFF); Studio `DayPlanEditor.tsx:665 on={cfg.flip_reread === true}` → OFF when unset; registry "false=OFF (legacy dormant)".

## Three-state check (boolean / toggle knobs) — owner order 06:20Z

Each toggle compared in three states: MISSING (no saved value), explicit
OFF/false/0, explicit ON/true/value. Format `Studio shows → bot does`.

| knob | MISSING | explicit OFF/false/0 | explicit ON/true/value | verdict |
|---|---|---|---|---|
| `approval_required` | OFF (`strategy.ts:123` optional; Go `bool` absent→false) | OFF→false | ON→true | MATCH ×3 |
| `blackout_enabled` | OFF (`strategy.ts:437`) → OFF (`risk_limits.go:398 boolOrDefault(…,false)`) | OFF→false | ON→true | MATCH ×3 |
| `breakeven_enabled` | OFF (TS "Default OFF") → OFF (`auto_trader.go:407 hlBool(…,false)`) | OFF→false | ON→true | MATCH ×3 |
| `config_visible` | ON (DB `gorm default:true`, `strategy.go:652`) | OFF→false | ON→true | MATCH ×3 |
| `consistency_enabled` | OFF (`strategy.ts:441`) → OFF (`risk_limits.go:399`) | OFF→false | ON→true | MATCH ×3 |
| `consecutive_loss_halt` | inherit→8 ON (`RiskControlEditor.tsx:1119 enabled={!==0}` → ON; bot `breakerHaltN` 8) | 0→OFF (`RiskControlEditor.tsx:1120 v?null:0`) | N→N | MATCH ×3 |
| `daily_loss_enabled` | OFF (`strategy.ts:421`) → OFF (`risk_limits.go`) | OFF→false | ON→true | MATCH ×3 |
| `daily_profit_enabled` | OFF (`strategy.ts:423`) → OFF | OFF→false | ON→true | MATCH ×3 |
| `death_reread` | ON (`DayPlanEditor.tsx:678 on={!==false}` → ON; `strategy.go:1841 nil→true`) | OFF (`!==false`→OFF; `*bool` false) | ON | MATCH ×3 |
| `enable` (session) | inherit (⚪; `auto_trader_planconfig.go:73`) | override OFF | override ON | MATCH ×3 |
| `enable_atr` | OFF (undefined→`checked` false; Go `bool` false) | OFF | ON (`engine_prompt.go:241`) | MATCH ×3 |
| `enable_boll` | OFF | OFF | ON (`engine_prompt.go:249`) | MATCH ×3 |
| `enable_direction_adjust` | OFF (crypto grid only) | OFF | ON (`auto_trader_grid_regime.go:65`) | MATCH ×3 |
| `enable_ema` | OFF | OFF | ON (`engine_prompt.go:221`) | MATCH ×3 |
| `enable_funding_rate` | OFF | OFF | ON (`engine_prompt.go:265`) | MATCH ×3 |
| `enable_macd` | OFF | OFF | ON (`engine_prompt.go:229`) | MATCH ×3 |
| `enable_multi_timeframe` | `current.length>1` (`IndicatorEditor.tsx:159`); bot Normalize fills true when the klines block was never set (`strategy.go:2664`) | OFF→false | ON→true | MATCH ×3 |
| `enable_oi` | OFF | OFF | ON (`engine_prompt.go:261`) | MATCH ×3 |
| `enable_raw_klines` | force-ON (Studio `ensureRawKlines` sets true; card hidden on futures) — no futures reader | n/a (force-set true) | ON (only agent skill handler reads) | HIDDEN (P2) |
| `enable_rsi` | OFF | OFF | ON (`engine_prompt.go:233`) | MATCH ×3 |
| `enable_svp` | OFF (`strategy.ts:269`; Go `bool` false) | OFF | ON (`engine_analysis.go:331`) | MATCH ×3 |
| `enable_volume` | OFF | OFF | ON (`engine_prompt.go:257`) | MATCH ×3 |
| `evening_digest` | OFF (folded, no control; `EveningDigestOn` false) | OFF (no control) | ON only via a stored true (honoured, no control) | HIDDEN (P2) |
| `flip_reread` | OFF (`DayPlanEditor.tsx:665 on={===true}` → OFF; `FlipRereadEnabled` false) | OFF→false | ON→true (`maybeRereadAfterFlip`) | MATCH ×3 |

Numeric / string knobs (the other ~44): two-state only — MISSING → the documented
default, explicit value → that value. Every one is MATCH (defaults and resolvers
cited in the MATCH list above). No numeric/string knob flips a meaning between
MISSING and explicit 0.

## Notes

- The owner's exact failure shape — "nothing saved read as ON while every switch
  read OFF" — does not reproduce in this slice. The two nil→ON knobs here
  (`death_reread`, and `consecutive_loss_halt`'s inherit→8) are BOTH mirrored by
  their Studio controls (`!== false` / `!== 0`), so the screen shows ON when the
  bot reads ON — and explicit OFF flips BOTH screen and bot to OFF together.
- The whole `external_data_sources` family (parent + 7 leaves + standalone
  `data_path`) is inert: `kernel/engine.go:795/815` feed `FetchExternalData`,
  which has no production caller. Registry rows already say so.
- `enable_raw_klines` is the only three-state oddity: MISSING and explicit-OFF
  are BOTH force-set to true by the Studio (`ensureRawKlines`), and the futures
  engine has no reader — a switch that can never read OFF and does nothing when
  ON. HIDDEN, P2.
- No fixes made, per the dispatch.
