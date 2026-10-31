// Strategy Studio Types
export interface Strategy {
  id: string
  name: string
  description: string
  is_active: boolean
  is_default: boolean
  is_public: boolean // 是否在策略市场公开
  config_visible: boolean // 配置参数是否公开可见
  config: StrategyConfig
  created_at: string
  updated_at: string
}

// 策略使用统计
export interface StrategyStats {
  clone_count: number // 被克隆次数
  active_users: number // 当前使用人数
  top_performers?: StrategyPerformer[] // 收益排行
}

// 策略使用者收益排行
export interface StrategyPerformer {
  user_id: string
  user_name: string // 脱敏后的用户名
  total_pnl_pct: number // 总收益率
  total_pnl: number // 总收益金额
  win_rate: number // 胜率
  trade_count: number // 交易次数
  using_since: string // 使用开始时间
  rank: number // 排名
}

export interface PromptSectionsConfig {
  role_definition?: string
  trading_frequency?: string
  entry_standards?: string
  decision_process?: string
}

export interface StrategyConfig {
  // Strategy type: "ai_trading" (default) or "grid_trading"
  strategy_type?: 'ai_trading' | 'grid_trading'
  // Language setting: "zh" for Chinese, "en" for English
  // Determines the language used for data formatting and prompt generation
  language?: 'zh' | 'en'
  // Prompt mode persisted per-strategy (balanced/aggressive/conservative/
  // scalping/futures). When unset, the live loop uses the venue rule
  // (ninjatrader→futures, else balanced). Prompt-layer only — not a risk gate.
  prompt_variant?: string
  // AI trading configuration. Legacy flat fields below are accepted only for
  // old data returned before the schema was split by strategy type.
  ai_config?: AIStrategyConfig
  coin_source?: CoinSourceConfig
  indicators?: IndicatorConfig
  custom_prompt?: string
  risk_control?: RiskControlConfig
  prompt_sections?: PromptSectionsConfig
  // Grid trading configuration (only used when strategy_type is 'grid_trading')
  grid_config?: GridStrategyConfig | null
  publish_config?: PublishStrategyConfig
  // Day Plan settings block (root-level, additive; mirrors Go DayPlanConfig).
  // Absent/undefined = feature off (byte-identical to a pre-day-plan strategy).
  day_plan?: DayPlanConfig
  // Pep trading-mode block (root-level, additive; mirrors Go store.PepConfig).
  // Absent = pep OFF (byte-identical to a pre-pep strategy). The block is a
  // MODE of its own, never combined with mentor_mode or the day plan.
  pep?: PepConfig
}

// PepConfig — mirrors Go store.PepConfig (store/pep.go). Every leaf name is
// unique (pep_*, pep_a_*, pep_e_*, pep_c_*). Optional = the server resolves the shipped
// default; the Studio panel always SENDS a concrete value (fullPepBlock) so the
// server-side merge (which keeps absent keys) can never leave a stale number.
export interface PepStyleAConfig {
  pep_a_box_minutes?: number
  pep_a_sl_fraction?: number
  pep_a_tp_fraction?: number
  pep_a_both_edges?: string // no_trade | nearest_open
}
export interface PepStyleEConfig {
  pep_e_box_minutes?: number
  pep_e_sl_fraction?: number
  pep_e_tp_fraction?: number
  pep_e_ema_period?: number
  pep_e_ema_tf_min?: number
  pep_e_signal_source?: string // ema | close
  pep_e_warmup_bars?: number // 0 = auto (3 x period)
}
// Custom style (m461): the ONLY editable style. A and E are locked to the PDF
// (the server ignores their stored numbers); Custom reads these plus the root
// rule leaves (box start, flat time, stop rounding, max trades, max box).
export interface PepStyleCustomConfig {
  pep_c_entry_type?: string // ema_cross | breakout
  pep_c_box_minutes?: number
  pep_c_sl_fraction?: number
  pep_c_tp_fraction?: number
  pep_c_both_edges?: string // breakout only: no_trade | nearest_open
  pep_c_ema_period?: number // ema_cross only
  pep_c_ema_tf_min?: number // ema_cross only
  pep_c_signal_source?: string // ema_cross only: ema | close
  pep_c_warmup_bars?: number // ema_cross only; 0 = auto (3 x period)
}
export interface PepConfig {
  pep_enabled?: boolean
  pep_style?: string // 'A' | 'E' | 'Custom'
  pep_contracts?: number
  pep_box_start_ct?: string // HH:MM America/Chicago
  pep_flat_time_ct?: string // HH:MM America/Chicago
  pep_tick_size?: number
  pep_sl_rounding?: string // outward | nearest
  pep_max_trades_per_day?: number
  pep_fee_per_contract_usd?: number
  pep_max_box_points?: number // 0 = OFF
  pep_signal_max_age_min?: number
  style_a?: PepStyleAConfig
  style_e?: PepStyleEConfig
  style_custom?: PepStyleCustomConfig
}

// DayPlanSessionOverride — per-session overrides; an ABSENT field inherits the
// strategy-level value (⚪ inherit), a present field overrides it (🔸 override).
export interface DayPlanSessionOverride {
  session: string // NY | ASIA | LONDON
  enable?: boolean
  replan_cap?: number
  plan_mode?: string
  acceptance_rule?: string // FOLDED (W-KNOB-PRUNE 2026-09-18): read by nothing, one rule exists
  min_grade?: string // A | B | C
  min_scenario_quality?: string // A | B | C (R4, 2026-08-25)
  max_trades?: number
}

// DayPlanConfig — mirrors Go store.DayPlanConfig. plan_enabled=false is the
// master switch (off). Additive + defaults-off.
export interface DayPlanConfig {
  structural_stop?: { buffer_points?: number; round_trip_cost_points?: number }
  plan_enabled: boolean
  planner_model?: string
  plan_mode?: string // advisory | direction | strict
  planner_timeframes?: string[]
  proximity_filter_atr?: number
  max_levels?: number
  /** FOLDED (W-KNOB-PRUNE 2026-09-18): constant 3 unless stored; no control. */
  scenario_cap?: number
  /** S3 (2026-09-16) — HTF seat count 0-6 (absent = legacy, non-effective).
   *  Pointer semantics mirror Go: absent ≠ 0. */
  htf_seats?: number
  // htf_score_multiplier REMOVED (W-KNOB-PRUNE 2026-09-18): the weight is the
  // constant 1.0 in the engine; a stored value is ignored.
  /** S1 (2026-09-16) — the STRUCTURE table (D/4h/1h, bias only). FOLDED
   *  (W-KNOB-PRUNE): no control; a stored true is honoured by the engine. */
  structure_map?: boolean
  /** S2 (2026-09-16) — HTF levels graded on their own timeframe bars. FOLDED
   *  (W-KNOB-PRUNE): no control; a stored true is honoured by the engine. */
  levels_fresh_by_tf?: boolean
  /** W-FLIP-REREAD (2026-09-17) — a fired flip goes dormant AND requests one
   *  free re-read in the flipped direction. Default false (legacy dormant). */
  flip_reread?: boolean
  /** W-DEATH-REREAD (2026-09-18, owner ruling 12:3x CT "fix all") — a fired
   *  death-condition kill goes dormant AND requests ONE BUDGETED re-read
   *  (spends one class-35 replan unit) that authors a fresh bias-free plan.
   *  Absent/true = ON (the owner's default); explicit false = today's
   *  behaviour byte-identical (dormant only). */
  death_reread?: boolean
  /** W-T1-CURRENCIES (2026-09-18) — currencies whose T1 (red) events HARD-block
   *  entries. Absent/empty = ["USD"] (shipped default); ["ALL"] = every
   *  currency (pre-wave behaviour). Other T1 events render as advisory only. */
  t1_currencies?: string[]
  /** FOLDED (W-KNOB-PRUNE 2026-09-18): one rule exists (1×5m close); a
   *  stored value is read by nothing. */
  acceptance_rule?: string
  /** W1 PRESENCE-AWARE — re-plans per session 0-4: absent/null = the shipped
   *  default 2; an explicit 0 = no re-plan. Send null to clear a stored value. */
  replan_cap?: number | null
  sessions_enabled?: string[]
  approval_required?: boolean
  /** FOLDED (W-KNOB-PRUNE 2026-09-18): constant OFF unless stored true. */
  evening_digest?: boolean
  // last_entry_ct / eod_flat_ct DELETED (W-KNOB-PRUNE 2026-09-18): unreachable
  // since the P2 session-scope clock; old stored values are ignored by Go.
  /** W13 auto re-align ceiling per plan. FOLDED (W-KNOB-PRUNE 2026-09-18):
   *  constant 5 unless stored; no control. */
  realign_cap?: number
  /** W-KNOB-PRUNE (2026-09-18) — the ONE level-event wake switch (replaces the
   *  five wake_on_* toggles). Pointer-bool mirrors Go: absent = ON. A legacy
   *  stored wake_on_* set is mapped by Go (any ON → ON) and passes through
   *  untouched on save. */
  wake_on_level_events?: boolean
  /** FOLDED (W-KNOB-PRUNE 2026-09-18): constant 30 unless stored; no control. */
  wake_min_interval_min?: number
  // seat_1h_zone REMOVED (W-KNOB-PRUNE 2026-09-18): the 1h seat guarantee is
  // unconditional in the engine.
  /** R4 (2026-08-25) — scenario quality floor: A | B | C. Default C = no
   *  restriction. */
  min_scenario_quality?: string
  /** ONE SETUP (dispatch 102, 2026-09-10) — arm only the single best reject
   *  (fade) setup. Pointer-bool mirrors Go: absent = ON. */
  one_setup_enabled?: boolean
  /** Lowest merged-candidate grade the best level may carry: A | B | C.
   *  Absent = B (mirrors Go). */
  one_setup_min_grade?: string
  /** W-PICTURE-HTF (2026-09-20) — the owner's two-picture method: a
   *  deterministic 4H-pivot → H1-close-break → 5m-swing setup evaluated from
   *  NATIVE bar events (the AI is commentary only). Absent/disabled = off.
   *  Zero/blank knobs inherit the Go resolved defaults (tick 0.25, pivot
   *  window 120, swing lookback 24, entry window 10s, freshness 2s); min_rr
   *  blank inherits the strategy's risk-control minimum. */
  picture_htf?: PictureHtfConfig
  /** W-EXEC-TRUTH W3 (2026-09-23) — the entry policy stamped on every arm of a
   *  NEWLY authored plan: market_in_zone (absent = the shipped default) — a
   *  limit at the far edge of the planner's entry_zone; planned_order — the
   *  resting order at the exact entry (reject / fvg_entry / sweep_reclaim leg
   *  1 only); legacy — stamp nothing (the explicit off). A string, not a
   *  union: Go resolves an unrecognised stored value to the default and names
   *  it (ResolveEntryPolicyDefault), so the wire can carry one. */
  entry_policy_default?: string
  /** W3 — widest entry_zone (points) a market_in_zone arm may carry. Absent =
   *  10. Pointer semantics mirror Go: absent ≠ 0. */
  zone_max_pts?: number
  /** W3 — minutes a market_in_zone limit may rest before it is cancelled
   *  ("zone rest expired"). Absent = 30. */
  zone_rest_max_min?: number
  /** W3 — floor (minutes) on the resolved hold of an ARMED market_in_zone
   *  time_hold scenario, refused at write below it. Absent = 3. */
  min_hold_min?: number
  /** Release #25 loss-fix switches (STRICT-12345 F-2/F-3/F-5/F-7). Every one
   *  ships OFF: absent = off (COMMON-25 knob rule case b — planner numbers the
   *  owner has not given). */
  /** F-2 room gate — 'off' | 'refuse' | 'tp1_at_obstacle'. Absent = off. */
  planner_room_gate?: string
  /** F-2 minimum r_to_obstacle (R). Absent = 1.0; read only when the gate is on. */
  planner_room_min_r?: number
  /** F-3 exit on thesis death — 'off' | 'invalidation_only' | 'all'. Absent = off. */
  planner_exit_on_thesis_death?: string
  /** F-5 arm no-chase WOULD_REFUSE refuses the placement. Absent = OFF. */
  planner_no_chase_enforce?: boolean
  /** F-5 momentum-into-zone ceiling in ATR5m over the last 15 min. 0/absent = OFF. */
  planner_momentum_into_zone_max_atr?: number
  /** F-7 the ATR stop floor never widens past the authored invalidation. Absent = OFF. */
  planner_atr_floor_cap_at_invalidation?: boolean
  /** W26 stale-zones wake: when EVERY armed market_in_zone leg is > X × ATR5m from price, re-read. 0/absent = OFF. */
  planner_stale_zones_max_atr?: number
  sessions?: DayPlanSessionOverride[]
}

/** W-PICTURE-HTF (2026-09-20) — deterministic two-picture knobs. */
export interface PictureHtfConfig {
  enabled?: boolean
  tick_size?: number
  pivot_window?: number
  swing_lookback?: number
  entry_window_sec?: number
  freshness_sec?: number
  /** blank = inherit the risk-control minimum R:R */
  min_rr?: number
}

export interface AIStrategyConfig {
  coin_source: CoinSourceConfig
  indicators: IndicatorConfig
  custom_prompt?: string
  risk_control: RiskControlConfig
  prompt_sections?: PromptSectionsConfig
}

export interface PublishStrategyConfig {
  is_public: boolean
  config_visible: boolean
}

// Grid trading specific configuration
export interface GridStrategyConfig {
  symbol: string
  // Number of grid levels (5-50)
  grid_count: number
  total_investment: number
  // Leverage (1-20)
  leverage: number
  // Upper price boundary (0 = auto-calculate from ATR)
  upper_price: number
  // Lower price boundary (0 = auto-calculate from ATR)
  lower_price: number
  // Use ATR to auto-calculate bounds
  use_atr_bounds: boolean
  // ATR multiplier for bound calculation (default 2.0)
  atr_multiplier: number
  // Position distribution: "uniform" | "gaussian" | "pyramid"
  distribution: 'uniform' | 'gaussian' | 'pyramid'
  // Maximum drawdown percentage before emergency exit
  max_drawdown_pct: number
  // Stop loss percentage per position
  stop_loss_pct: number
  // Daily loss limit percentage
  daily_loss_limit_pct: number
  // Use maker-only orders for lower fees
  use_maker_only: boolean
  // Enable automatic grid direction adjustment based on box breakouts
  enable_direction_adjust?: boolean
  // Direction bias ratio for long_bias/short_bias modes (default 0.7 = 70%/30%)
  direction_bias_ratio?: number
}

export interface CoinSourceConfig {
  source_type: 'static'
  static_coins?: string[]
  excluded_coins?: string[] // 排除的币种列表
}

export interface IndicatorConfig {
  klines: KlineConfig
  // Raw OHLCV kline data - required for AI analysis
  enable_raw_klines: boolean
  // Technical indicators (optional)
  enable_ema: boolean
  enable_macd: boolean
  enable_rsi: boolean
  enable_atr: boolean
  enable_boll: boolean
  enable_volume: boolean
  enable_oi: boolean
  enable_funding_rate: boolean
  enable_svp?: boolean // session volume profile → futures AI prompt line; default OFF
  ema_periods?: number[]
  rsi_periods?: number[]
  atr_periods?: number[]
  boll_periods?: number[]
  external_data_sources?: ExternalDataSource[]
}

export interface KlineConfig {
  primary_timeframe: string
  primary_count: number
  longer_timeframe?: string
  longer_count?: number
  enable_multi_timeframe: boolean
  // 新增：支持选择多个时间周期
  selected_timeframes?: string[]
}

export interface ExternalDataSource {
  name: string
  type: 'api' | 'webhook'
  url: string
  method: string
  headers?: Record<string, string>
  data_path?: string
  refresh_secs?: number
}

export interface MentorTuning {
  trigger_school?: 1 | 2
  ping_pong_min_gap_pts?: number
  ping_pong_candle_max_pts?: number
  ping_pong_candle_lookback?: number
  level_max_visits?: number // explicit 0 = no per-day cap
  orb_gate_enabled?: boolean
  isb_reverse_ema9_enabled?: boolean
  htf_gate_news_only?: boolean // D4.4-11 — unset = ON (news 07:20–07:35 + open 08:30–09:30 CT; the mentor's rule); OFF = all-day gate
  exec_2m_after_30m?: boolean // X5-10 — unset = ON (execution frame: 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit)
  day_gate_spent_pts?: number
  day_gate_target_cap_pts?: number
  swing_max_stop_pts?: number
  // Course-entry knobs (release #25). Unset = the ruled default in brackets.
  // D4.2 p1 — a 1m ISB under a standing 15m box [require_5m_isb]
  '15m_ib_requires_5m_isb'?: 'off' | 'require_5m_isb' | 'no_trade_until_escape'
  // D4.2 p1 @07:54 — when a 5m/15m box conflict ends [both_boxes] (OQ-2)
  tf_conflict_end?: 'first_box' | 'both_boxes'
  // D4.4 p1 @21:55 — news day: wait beyond the 4h trigger line until 08:30 [true]
  news_wait_both_lines?: boolean
  // D4.4 p1 @18:18 — drop print→08:30 bars from the 1h/4h feed [true]
  news_bars_excluded?: boolean
  // D4.1 p1 @20:52 — ISB rest box escape test [body]
  isb_box_escape?: 'close' | 'body'
  // D4.2 p2 @01:33 — EMA 34 back after one loss [until_zone_exit]
  ema34_loss_relock?: 'session' | 'until_zone_exit'
  // X5 @08:52 — refuse an entry within N pts of the ORB edge [0 = off]
  orb_edge_veto_pts?: number
  // rule 5 (release #31): the ORB-edge ISB candle must lie ≥ this fraction outside [0.80]
  orb_isb_fraction_outside?: number
  // D4.3 @12:43 — the box-return 5m trend source [structure]
  d43_trend_source?: 'trigger_line' | 'structure'
  // D3.4 p1 @17:48 — the 5m trigger side binds every level reject [false]
  level_entries_need_trigger_side?: boolean
  // D3.3 p1 @05:18 — a trendline in the path is a target [true]
  trendline_is_target?: boolean
  // D3.2 p2 @10:56 — a trendline reject between two boxes may trade [true]
  trendline_between_boxes?: boolean
  // X-FOMO @06:10 — a stopped-out leg also resets on a wave break [true]
  leg_reset_on_wave_break?: boolean
  // X9 @05:47 — inside a live flag the opposite wall is a closer target (#416) [true]
  flag_wall_target?: boolean
  // D3.4 p3 recovered2 @00:00 — after a box escapes, a new same-role box
  // outside the old range; D3.2 p2 @09:47 "Đừng vẽ vùng thứ 3… Rối lắm" [true]
  box_new_zone_after_escape?: boolean
  // Release #52 B19 — the ORB opens only on a candle COMPLETELY outside the
  // 2-minute range, wicks included; "Routine trước khi trade" part1
  // @06:47–07:16 [false = the body test]
  orb_escape_completely?: boolean
  // Release #52 W11 — invented-constant knobs: every number is OURS, not the
  // course's; each default is what the kernel hard-coded before.
  // B8 [4] — the candle a resting 2m ISB arm is cancelled on (3–12);
  // "Ngày 4.2 Multitime frame 15_" part2 @00:08:53 counts 1m candles only
  isb_stack_cancel_candle_2m?: number
  // B8 [0 = off] — a stop at or under this keeps the arm past the cancel
  // candle; "Ngày 5.1 Go over lại tất cả" part2 @02:50
  isb_stack_tiny_stop_pts?: number
  // B10 [100 / 15 / 0] — evening "slow": cutoff pts, start hour CT (15–17),
  // trailing window minutes (0 = cumulative); X11 @17:40–18:26, owner Q6
  evening_slow_pts?: number
  evening_slow_from_hour?: number
  evening_slow_window_min?: number
  // B11 [2 / true] — near-box room × risk (0 = off) and whether it gates PHL/PLH
  near_box_room_multiple?: number
  near_box_room_phl?: boolean
  // C16 [5] — PHL/PLH target pts short of the old extreme (0 = at it)
  phl_target_shy_pts?: number
  // B27 [2 / 0] — ISB size cuts: "at the old extreme" ± pts; "in range"
  // key-level gap (0 = inherit ping_pong_min_gap_pts)
  isb_old_extreme_pts?: number
  isb_in_range_gap_pts?: number
}

export interface RiskControlConfig {
  // Max number of coins held simultaneously (CODE ENFORCED)
  max_positions: number

  // Trading Leverage - exchange leverage for opening positions (AI guided)
  btc_eth_max_leverage: number // BTC/ETH max exchange leverage
  altcoin_max_leverage: number // Altcoin max exchange leverage

  // Position Value Ratio - single position notional value / account equity (CODE ENFORCED)
  // Max position value = equity × this ratio
  btc_eth_max_position_value_ratio?: number // default: 5 (BTC/ETH max position = 5x equity)
  altcoin_max_position_value_ratio?: number // default: 1 (Altcoin max position = 1x equity)

  // Risk Parameters
  max_margin_usage: number // Max margin utilization, e.g. 0.9 = 90% (CODE ENFORCED)
  min_position_size: number // Min position size in USDT (CODE ENFORCED)
  min_risk_reward_ratio: number // Min take_profit / stop_loss ratio (CODE ENFORCED, Chunk 1)
  min_confidence: number // Min AI confidence to open position (CODE ENFORCED, Chunk 1)

  // === Strategy Studio Phase 1 — prop-firm guardrails (Chunks 2-5; surfaced in Chunk 6).
  // The kernel gate reads these exact fields; the toggle (…_enabled) governs enforcement. ===
  guardrails_enabled?: boolean // master switch (absent = OFF since r22 — owner R4)
  // Mentor mode (🧑‍🏫) — per strategy, default OFF. Placements also need the
  // server env MENTOR_PLACE=1; without it every intent is a dry run.
  mentor_mode?: boolean
  // Mentor trading window (CT): start HH:MM (default "08:30") and length in
  // minutes (unset/0 → 60; -1 = no time window — the 15:45 CT entry cutoff
  // and the 14:45 CT EOD flat still apply, never "any hour"). SWING is exempt.
  mentor_window_start?: string
  mentor_window_minutes?: number
  // Stop-after-win / done-after-win (mentor day-stop): once a mentor trade
  // closes today in profit and the day is net positive, refuse new entries
  // until the next NY open (08:30 CT). Absent = OFF (owner order 2026-10-16:
  // nil → OFF, an explicit true turns it on).
  mentor_done_after_win?: boolean
  // Stop-after-loss (mentor day-stop): once a mentor trade closes today with a
  // net loss, refuse new entries until the next trading day (17:00 CT).
  // Absent = OFF (byte-identical to the prior config).
  mentor_stop_after_loss?: boolean
  // Runner exit (course-defaults-30, owner "fix all"; release #22): "candle"
  // (absent = candle) = FIRST break-even at +0.5R before leg 1's 1:1 partial
  // (mentor_be_before_partial ON), THEN once leg 1's 1:1 take-profit is
  // CONFIRMED filled at NT8, 1 tick beyond each closed candle; "off" = the
  // older half-distance rule.
  mentor_trail_mode?: string
  // Release #25 course-exits knobs (trader/mentor_course_exits.go). Absent =
  // the knob-rule default (course-defaults-30, owner "fix all"): first-target
  // ON, runner ON, resonance runner TP ON, BE-before-partial ON (reverses R3),
  // candle-3 any-profit ON (today's rule), reverse-ISB-as-PHL ON, EMA 9 visits
  // shared, ISB max stop 30, keypair ratio 1, box height 0 (off), box fresh
  // RTH pair OFF.
  mentor_swing_ema34_is_first_target?: boolean
  mentor_swing_runner?: boolean
  mentor_resonance_runner_tp_old_high?: boolean
  mentor_be_before_partial?: boolean
  mentor_isb_c3_partial_any_profit?: boolean
  mentor_reverse_isb_as_phl?: boolean
  mentor_ema9_visit_cap?: string
  mentor_isb_max_pts?: number
  mentor_keypair_candle_ratio?: number
  mentor_box_fresh_rth_pair?: boolean
  mentor_box_max_height_pts?: number
  // Mentor method numbers (K3). Every field unset = the ruled default; the
  // Go side (store.MentorTuning) fails closed to the default on a bad value.
  mentor_tuning?: MentorTuning
  // Hold-lock: once in a position, suppress AI-initiated closes so the trade
  // rides to the AI's stop/target (a real OCO bracket at the exchange). Default OFF.
  hold_discipline?: boolean
  // Auto-breakeven (NT8 futures): once +N pts in profit, move the stop to entry.
  breakeven_enabled?: boolean
  breakeven_trigger_points?: number // default 50
  // F-8 (release #25): breakeven at N × the position's initial risk instead of
  // points. 0/absent = OFF = the points rule above. AI/planner trades only.
  breakeven_trigger_r?: number
  // Trailing profit (Phase 3B, NT8 futures only; default OFF)
  trailing_enabled?: boolean
  trailing_atr_mult?: number // default 2.0
  trailing_atr_period?: number // default 14 (5m ATR)
  trailing_arm?: string // 'after_breakeven' (default) | 'after_trigger_points' | 'immediate'
  trailing_arm_points?: number // used iff after_trigger_points
  daily_loss_limit_usd?: number // daily realized-loss limit (USD)
  daily_loss_enabled?: boolean // absent = OFF since r22 (owner R4); the $ keeps its env fallback
  daily_profit_target_usd?: number // daily realized-profit target (USD)
  daily_profit_enabled?: boolean // default OFF
  max_daily_trades?: number // max entries per CME session-day
  max_daily_trades_enabled?: boolean // default OFF
  /** D1 — halt new entries after N consecutive losing trades this CME
   *  session-day (not master-gated). W1 PRESENCE-AWARE: absent/null = inherit
   *  (env BREAKER_HALT_N, else 8 — ON); an explicit 0 = OFF; N = N. Send null
   *  (never undefined) to clear a stored value — the PUT merge keeps absent keys. */
  consecutive_loss_halt?: number | null
  reentry_cooldown_minutes?: number // B7: after a stop-loss, block same-dir re-entry for N min or until price moves ≥1×ATR15 from the stop (0=off; futures-only)
  max_contracts_per_order?: number // futures contracts-per-order clamp
  planner_contracts?: number // dispatch-31: planner contracts per entry (unset/0 → 1; ≥ 2 splits 2+2 at the first obstacle)
  max_contracts_enabled?: boolean // parse-only — no consumer (knob registry KnobIneffective)
  max_notional_leverage?: number // futures notional ceiling = equity × this (default 20)
  notional_cap_enabled?: boolean // default ON
  blackout_enabled?: boolean // default OFF
  blackout_start_ct?: string // HH:MM, America/Chicago
  blackout_end_ct?: string // HH:MM, America/Chicago
  consistency_max_day_pct?: number // no single day > this % of total realized profit
  consistency_enabled?: boolean // default OFF
}
