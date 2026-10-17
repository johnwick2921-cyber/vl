# Knob recheck Q4 of 4 — DS-104 (read-only audit)

Branch: `audit/knob-recheck-q4` · Base: `origin/release/2026-10-16-32` @ `a8b849161`
Lane: DS-104 · Date: 2026-10-17 CT · Knob list: `/home/hoang/fleet-1017/knobs-q03.txt` (58 knobs)
Read in full: `/home/hoang/fleet-1017/COMMON.md` (all 8 safety laws followed). No writes outside this file + the report directory. `data/data.db` opened only with `sqlite3 -readonly`.

**The owner's hunt:** "OFF on screen, ON in the bot" — a knob whose Studio default disagrees with the Go resolver when nothing is saved. For every knob the question is: with the key absent/zero, does the screen say one thing and the bot do another?

**Verdict vocabulary:** MATCH (screen == bot) · MISMATCH (screen ≠ bot) · DEAD (no production reader on futures) · HIDDEN (bot behaviour with no visible control) · FOLDED (control removed, stored value honoured + logged). Severity: P0 = changes entries/exits/size while the screen says otherwise; P1 = wrong default text/display only; P2 = cosmetic.

**Key authority for mentor_tuning defaults:** `kernel/mentor/mentor.go:524 DefaultConfig()` (the ruled defaults live there once; `trader/mentor_tuning.go:55 mentorTuningResolve` applies stored overrides and fails closed to them). The Studio placeholders are `web/src/components/strategy/MentorTuningPanel.tsx MENTOR_TUNING_DEFAULTS`.

---

## A. `risk_control.mentor_tuning.*` (14 knobs)

| # | knob | SCREEN (nothing saved) | BOT (nil/absent) | STORE | VERDICT | Sev |
|---|---|---|---|---|---|---|
| 1 | `news_bars_excluded` | bool placeholder **false = OFF** (`MentorTuningPanel.tsx MENTOR_TUNING_DEFAULTS`) | `DefaultConfig` → **true = ON** (freeze pre-print 1h/4h read) `mentor.go:600` | KnobLive `mentor_tuning.go:NewsBarsExcluded` | **MISMATCH** — screen OFF, bot ON. **FIXED-BY-571** (sets the Studio default to `true`) | P1 |
| 2 | `news_wait_both_lines` | bool placeholder **true** | `DefaultConfig` → **true** `mentor.go:598` | KnobLive | MATCH | — |
| 3 | `orb_edge_veto_pts` | num placeholder **0 = off** | `mentor_tuning.go` pick(0.25,50); absent → **0 = off** | KnobLive | MATCH | — |
| 4 | `orb_gate_enabled` | bool placeholder **true** | `DefaultConfig` → **true** `mentor.go:568` | KnobLive | MATCH | — |
| 5 | `orb_isb_fraction_outside` | num placeholder **0.8** | nil → **0.80 (ON)**; explicit 0 → OFF `mentor_tuning.go:167-186` | KnobLive | MATCH | — |
| 6 | `ping_pong_candle_lookback` | num placeholder **30** | `DefaultConfig` → **30** | KnobLive | MATCH | — |
| 7 | `ping_pong_candle_max_pts` | num placeholder **20** | `DefaultConfig` → **20** | KnobLive | MATCH | — |
| 8 | `ping_pong_min_gap_pts` | num placeholder **50** | `DefaultConfig` → **50** | KnobLive | MATCH | — |
| 9 | `swing_max_stop_pts` | num placeholder **100** | `DefaultSwingCfg` → **100** | KnobLive | MATCH | — |
| 10 | `swing_min_stop_pts` | **no control** (not in `MentorTuningPanel`) | nil → **0 = OFF** (R5) `mentor_tuning.go` | KnobLive (resolver only) | **HIDDEN** — bot has a W26 min-stop rule, no Studio control; absent = OFF so no silent gate | P2 |
| 11 | `tf_conflict_end` | enum placeholder **both_boxes** | `DefaultConfig` → **both_boxes** | KnobLive | MATCH | — |
| 12 | `trendline_between_boxes` | bool placeholder **true** | `DefaultConfig` → **true** | KnobLive | MATCH | — |
| 13 | `trendline_is_target` | bool placeholder **true** | `DefaultConfig` → **true** | KnobLive | MATCH | — |
| 14 | `trigger_school` | select placeholder **1** | `DefaultConfig` → **1**; bad → fail closed to 1 | KnobLive | MATCH | — |

*(The `mentor_tuning` default-vs-Studio parity is pinned by #571's new `kernel/mentor/mentor_tuning_defaults_parity_test.go` — [A] read its diff.)*

## B. Prompt / indicator / session (10 knobs)

| # | knob | SCREEN | BOT | STORE | VERDICT | Sev |
|---|---|---|---|---|---|---|
| 15 | `role_definition` | textarea, empty | empty → prompt builds the fixed CME role `engine_prompt_futures.go:99` | KnobLive | MATCH | — |
| 16 | `rsi_periods` | input default `[14]` (`IndicatorEditor.tsx`) | reads stored `[14]` `engine_analysis.go:790` | KnobLive | MATCH | — |
| 17 | `selected_timeframes` | multi-select default `[1h,4h,1d,15m,3m,5m]` | reads stored `engine_analysis.go:762` | KnobLive | MATCH | — |
| 18 | `session` | prompt box, empty | empty → omitted `engine_analysis.go:403` | KnobLive | MATCH | — |
| 19 | `sessions_enabled` | multi-select default `['NY']` (`DayPlanEditor.tsx:432`) | reads stored `auto_trader_planconfig.go:77` | KnobLive | MATCH | — |
| 20 | `strategy_type` | selector (`ai_trading`) | normalizeStrategyType `auto_trader_decision.go:107` | KnobLive | MATCH | — |
| 21 | `system_prompt` | Prompt Editor box (owner-visible, 6.5k chars) | read on the AI path `engine_analysis.go:590` (inert on mentor; the futures prompt uses `buildFuturesPrompt`) | KnobLive (built at prompt time) | MATCH | — |
| 22 | `trading_frequency` | textarea, empty | empty → omitted `engine_prompt_futures.go:142` | KnobLive | MATCH | — |
| 23 | `t1_currencies` | text input, empty (`DayPlanEditor.tsx:380`) | empty → `[USD]` hard-block (`T1CurrenciesFor`); ALL/* = every currency | KnobLive | MATCH (guide says empty=[USD]); the blank input alone understates the USD hard-block | P2 |
| 24 | `trailing_enabled` | toggle default **false** + mentor caveat + SUSPENDED chip (`RiskControlEditor.tsx`, #570) | false; exit-mechs suspended at the wire; inert in mentor mode | KnobLive | MATCH | — |

## C. Trailing config (4 knobs, all in `RiskControlEditor.tsx`)

| # | knob | SCREEN | BOT | STORE | VERDICT | Sev |
|---|---|---|---|---|---|---|
| 25 | `trailing_arm` | select `after_breakeven` | default `after_breakeven` `auto_trader_trailing.go` | KnobLive | MATCH | — |
| 26 | `trailing_arm_points` | num `0` | default 0 | KnobLive | MATCH | — |
| 27 | `trailing_atr_mult` | num `2.0` | `defaultTrailingATRMult = 2.0` | KnobLive | MATCH | — |
| 28 | `trailing_atr_period` | num `14` | `defaultTrailingATRPeriod = 14` | KnobLive | MATCH | — |

## D. Crypto-era / grid / data-source (DEAD on futures — 10 knobs)

On both live strategies (`ninjatrader` CME futures) these are only read by the crypto grid / crypto data engine, which the futures path never runs (Studio itself says "Grid cannot run on CME futures"). Registry marks them CLASS 151 — the stored value stays honoured on the crypto path.

| # | knob | VERDICT | Note |
|---|---|---|---|
| 29 | `source_type` | DEAD (futures) | crypto coin source `engine.go:418` |
| 30 | `static_coins` | DEAD (futures) | crypto coin list `engine_prompt.go:272`; futures symbol comes from the NT8 binding |
| 31 | `stop_loss_pct` | DEAD (futures) | grid `auto_trader_grid_orders.go:362` |
| 32 | `symbol` | DEAD (futures) | grid `grid_engine.go:153` |
| 33 | `total_investment` | DEAD (futures) | grid `grid_engine.go:153` |
| 34 | `upper_price` | DEAD (futures) | grid `grid_engine.go:283` |
| 35 | `url` | DEAD (futures) | crypto data fetch, no production caller |
| 36 | `use_atr_bounds` | DEAD (futures) | grid `auto_trader_grid.go:304` |
| 37 | `use_maker_only` | DEAD (futures) | grid `auto_trader_grid_orders.go:127` |
| 38 | `total` | DEAD (computed) | computed metadata `engine_analysis.go:256`, not a stored leaf |

## E. Folded / computed / internal (12 knobs)

| # | knob | VERDICT | Note |
|---|---|---|---|
| 39 | `scenario_cap` | FOLDED | constant 3 unless stored (owner stores 5, honoured + logged); control removed |
| 40 | `structure_map` | FOLDED | OFF until R25, stored true honoured; toggle removed |
| 41 | `wake_min_interval_min` | FOLDED | legacy, collapsed into the wake switch |
| 42 | `wake_on_15m_zone` | FOLDED | legacy per-class, collapsed into `wake_on_level_events`; Studio never writes it |
| 43 | `wake_on_htf_ob` | FOLDED | legacy; **keeps its own effect** — the HTF OB wake class runs only when this is stored `true`, ANDed with the single switch |
| 44 | `wake_on_htf_zone` | FOLDED | legacy, collapsed into the single switch |
| 45 | `wake_on_ifvg` | FOLDED | legacy, collapsed into the single switch |
| 46 | `wake_on_seated_invalidation` | FOLDED | legacy, collapsed into the single switch |
| 47 | `suggestions` | DEAD (computed) | no stored StrategyConfig leaf; no production reader |
| 48 | `updated_at` | DEAD (computed) | DB column, not a stored leaf |
| 49 | `usage_pct` | DEAD (computed) | API-computed usage (`api/strategy.go:385`), display only (`TokenEstimateBar`) |
| 50 | `user_id` | DEAD (internal) | auth/planner internal, not a strategy knob |

## F. Planner / day-plan / wake (8 knobs — mostly HIDDEN)

| # | knob | SCREEN | BOT | STORE | VERDICT | Sev |
|---|---|---|---|---|---|---|
| 51 | `wake_on_level_events` | toggle **ON unless false** (`DayPlanEditor.tsx:893`) | nil → **ON** `auto_trader_wake_levels.go:105` | KnobLive | MATCH | — |
| 52 | `write_time_feasibility` | **no control** | nil → **ON** (repair-hint then write disabled) | KnobLive | **HIDDEN** — an entries/exits-changing gate with no Studio control; nil=ON | P2 |
| 53 | `zone_max_pts` | **no control** | nil → **10** pt width cap at write | KnobLive | **HIDDEN** | P2 |
| 54 | `zone_place_within_pts` | **no control** | nil → **25 (ON)** placement band; 0 = legacy | KnobLive | **HIDDEN** — a placement gate with no Studio control; nil=ON | P2 |
| 55 | `zone_rest_max_min` | **no control** | nil → **30** min rest cap | KnobLive | **HIDDEN** | P2 |
| 56 | `transition_standdown` | **no control, no guide card** | nil → **True** (regime transition standdown) | KnobLive (promoted in #570) | **HIDDEN** | P2 |
| 57 | `round_trip_cost_points` | **no control** (types only) | default **2** pt structural stop | KnobLive | **HIDDEN** | P2 |
| 58 | `type` | **no control** | internal `htf_veto.go:115` | KnobLive | HIDDEN (internal) | P2 |

---

## Summary counts (58 knobs)

| Verdict | Count |
|---|---|
| MATCH | 27 |
| MISMATCH | 1 (`news_bars_excluded`, FIXED-BY-571) |
| DEAD | 14 (10 crypto-era/grid + `total` + `suggestions` + `updated_at` + `usage_pct` + `user_id`) |
| FOLDED | 8 (`scenario_cap`, `structure_map`, `wake_min_interval_min` + the five legacy `wake_on_*` classes) |
| HIDDEN | 8 (`swing_min_stop_pts`, `write_time_feasibility`, `zone_max_pts`, `zone_place_within_pts`, `zone_rest_max_min`, `transition_standdown`, `round_trip_cost_points`, `type`) |

27 + 1 + 14 + 8 + 8 = 58.

## MISMATCH / HIDDEN / DEAD — the actionable list

- **MISMATCH (P1):** `risk_control.mentor_tuning.news_bars_excluded` — Studio default OFF, bot default ON. **FIXED-BY-571** (`MentorTuningPanel` default → true + `strategy.go` comment + parity pin).
- **HIDDEN (P2, no Studio control):** `write_time_feasibility` (nil→ON), `zone_place_within_pts` (nil→25 ON), `zone_max_pts` (10), `zone_rest_max_min` (30), `transition_standdown` (nil→True), `round_trip_cost_points` (2), `swing_min_stop_pts` (nil→0 OFF), `type` (internal).
- **DEAD on futures (P2):** `source_type`, `static_coins`, `stop_loss_pct`, `symbol`, `total_investment`, `upper_price`, `url`, `use_atr_bounds`, `use_maker_only` (crypto/grid path never runs); `total`, `suggestions`, `updated_at`, `usage_pct`, `user_id` (computed/internal).

No P0 found in this slice. The one true "screen OFF vs bot ON" case (`news_bars_excluded`) is already fixed by the open #571.

— DS-104
