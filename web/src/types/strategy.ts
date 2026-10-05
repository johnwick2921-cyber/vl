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
  htf_gate_news_only?: boolean // D4.4-11 — unset = OFF (the all-day 4h/1h gate)
  exec_2m_after_30m?: boolean // X5-10 — unset = OFF (the 1m ISB read)
  day_gate_spent_pts?: number
  day_gate_target_cap_pts?: number
  swing_max_stop_pts?: number
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
  guardrails_enabled?: boolean // master switch (default ON)
  // Mentor mode (🧑‍🏫) — per strategy, default OFF. Placements also need the
  // server env MENTOR_PLACE=1; without it every intent is a dry run.
  mentor_mode?: boolean
  // Mentor trading window (CT): start HH:MM (default "08:30") and length in
  // minutes (unset/0 → 60; -1 = no window, entries at any hour). SWING is exempt.
  mentor_window_start?: string
  mentor_window_minutes?: number
  // Mentor method numbers (K3). Every field unset = the ruled default; the
  // Go side (store.MentorTuning) fails closed to the default on a bad value.
  mentor_tuning?: MentorTuning
  // Hold-lock: once in a position, suppress AI-initiated closes so the trade
  // rides to the AI's stop/target (a real OCO bracket at the exchange). Default OFF.
  hold_discipline?: boolean
  // Auto-breakeven (NT8 futures): once +N pts in profit, move the stop to entry.
  breakeven_enabled?: boolean
  breakeven_trigger_points?: number // default 50
  // Trailing profit (Phase 3B, NT8 futures only; default OFF)
  trailing_enabled?: boolean
  trailing_atr_mult?: number // default 2.0
  trailing_atr_period?: number // default 14 (5m ATR)
  trailing_arm?: string // 'after_breakeven' (default) | 'after_trigger_points' | 'immediate'
  trailing_arm_points?: number // used iff after_trigger_points
  daily_loss_limit_usd?: number // daily realized-loss limit (USD)
  daily_loss_enabled?: boolean // default ON (preserves the live env gate)
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
  max_contracts_enabled?: boolean // default ON
  max_notional_leverage?: number // futures notional ceiling = equity × this (default 20)
  notional_cap_enabled?: boolean // default ON
  blackout_enabled?: boolean // default OFF
  blackout_start_ct?: string // HH:MM, America/Chicago
  blackout_end_ct?: string // HH:MM, America/Chicago
  consistency_max_day_pct?: number // no single day > this % of total realized profit
  consistency_enabled?: boolean // default OFF
}
