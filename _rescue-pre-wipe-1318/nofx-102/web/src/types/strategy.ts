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
  acceptance_rule?: string
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
  scenario_cap?: number
  /** S3 (2026-09-16) — HTF seat count 0-6 (absent = legacy, non-effective).
   *  Pointer semantics mirror Go: absent ≠ 0. */
  htf_seats?: number
  /** S3 (2026-09-16) — HTF score weight 1.0-1.5 (absent = 1.2 default). */
  htf_score_multiplier?: number
  /** S1 (2026-09-16) — the STRUCTURE table (D/4h/1h, bias only). Pointer-bool
   *  semantics mirror Go: absent/false = off. */
  structure_map?: boolean
  /** S2 (2026-09-16) — HTF levels graded on their own timeframe bars.
   *  Default false. */
  levels_fresh_by_tf?: boolean
  /** W-FLIP-REREAD (2026-09-17) — a fired flip goes dormant AND requests one
   *  free re-read in the flipped direction. Default false (legacy dormant). */
  flip_reread?: boolean
  acceptance_rule?: string // 2x5m | 15m-close
  replan_cap?: number
  sessions_enabled?: string[]
  approval_required?: boolean
  evening_digest?: boolean
  last_entry_ct?: string
  eod_flat_ct?: string
  /** W13 auto re-align ceiling per plan (default 5). Go reads it; the FE type
   *  was missing it entirely, so the value could never be edited from the app. */
  realign_cap?: number
  /** W6 (2026-08-25) — planner wake-up knobs (level events). Pointer-bool
   *  semantics mirror Go: absent = ON for these five except HTF OBs (OFF). */
  wake_on_15m_zone?: boolean
  wake_on_htf_zone?: boolean
  wake_on_htf_ob?: boolean
  wake_on_seated_invalidation?: boolean
  wake_on_ifvg?: boolean
  wake_min_interval_min?: number
  /** 1h wave (2026-08-25) — reserve one of the two HTF seats for an in-band
   *  1h S/D zone when one exists. Absent = ON (mirrors Go pointer-bool). */
  seat_1h_zone?: boolean
  /** R4 (2026-08-25) — scenario quality floor: A | B | C. Default C = no
   *  restriction. */
  min_scenario_quality?: string
  /** ONE SETUP (dispatch 102, 2026-09-10) — arm only the single best live
   *  setup. Pointer-bool mirrors Go: absent = ON. */
  one_setup_enabled?: boolean
  /** Lowest merged-candidate grade the best level may carry: A | B | C.
   *  Absent = B (mirrors Go). */
  one_setup_min_grade?: string
  sessions?: DayPlanSessionOverride[]
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
  // Trading pair (e.g., "BTCUSDT")
  symbol: string
  // Number of grid levels (5-50)
  grid_count: number
  // Total investment in USDT
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
  source_type: 'static' | 'ai500' | 'oi_top' | 'oi_low'
  static_coins?: string[]
  excluded_coins?: string[] // 排除的币种列表
  use_ai500: boolean
  ai500_limit?: number
  use_oi_top: boolean
  oi_top_limit?: number
  use_oi_low: boolean
  oi_low_limit?: number
  // Note: API URLs are now built automatically using nofxos_api_key from IndicatorConfig
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

  // ========== NofxOS 数据源统一配置 ==========
  // Unified NofxOS API Key - used for all NofxOS data sources
  nofxos_api_key?: string

  // 量化数据源（资金流向、持仓变化、价格变化）
  enable_quant_data?: boolean
  enable_quant_oi?: boolean
  enable_quant_netflow?: boolean

  // OI 排行数据（市场持仓量增减排行）
  enable_oi_ranking?: boolean
  oi_ranking_duration?: string // "1h", "4h", "24h"
  oi_ranking_limit?: number

  // NetFlow 排行数据（机构/散户资金流向排行）
  enable_netflow_ranking?: boolean
  netflow_ranking_duration?: string // "1h", "4h", "24h"
  netflow_ranking_limit?: number

  // Price 排行数据（涨跌幅排行）
  enable_price_ranking?: boolean
  price_ranking_duration?: string // "1h", "4h", "24h" or "1h,4h,24h"
  price_ranking_limit?: number
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
  consecutive_loss_halt?: number // D1: halt new entries after N consecutive losing trades this session (0=off; not master-gated)
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
