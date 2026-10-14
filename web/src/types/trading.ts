export interface SystemStatus {
  trader_id: string
  trader_name: string
  ai_model: string
  is_running: boolean
  start_time: string
  runtime_minutes: number
  call_count: number
  initial_balance: number
  scan_interval: string
  stop_until: string
  last_reset_time: string
  ai_provider: string
  strategy_type?: 'ai_trading' | 'grid_trading'
  grid_symbol?: string
}

export interface AccountInfo {
  total_equity: number
  wallet_balance: number
  unrealized_profit: number // 未实现盈亏（交易所API官方值）
  available_balance: number
  total_pnl: number
  total_pnl_pct: number
  initial_balance: number
  daily_pnl: number
  // P&L-TRUTH WAVE (2026-09-01): the ledger day total — the SAME rule as the
  // position-history footer (rows closed today CT, unknown-P&L / test-seam
  // reasons out, strict pnl_corrected; unresolved rows counted + excluded).
  // Absent when the backend could not compute it (never a fabricated 0).
  ledger_day_pnl?: number
  ledger_day_resolved?: number
  ledger_day_unresolved?: number
  ledger_day_date?: string
  ledger_day_status?: string
  // R22 PNL-TRUTH: the ONE "today" is the CME session day from 17:00 CT;
  // manual NT8 rows (owner ruling R1) are excluded from the bot figure above
  // and counted here.
  ledger_day_start_ms?: number
  ledger_day_manual_excluded?: number
  ledger_day_manual_excluded_pnl?: number
  // The BOT's record (all time, bot rows only) and the manual rows' own line.
  bot_record_pnl?: number
  bot_record_trades?: number
  bot_record_unresolved?: number
  bot_record_status?: string
  manual_excluded_trades?: number
  manual_excluded_pnl?: number
  manual_excluded_line?: string
  // ACCOUNT figures, as the broker reports them — each under its own name.
  // total_pnl holds whatever total_pnl_source names ("nt8_session" or
  // "equity_minus_initial"); absent fields are unknown, never 0.
  total_pnl_source?: 'nt8_session' | 'equity_minus_initial'
  nt8_session_pnl?: number
  account_pnl_vs_initial?: number
  account_pnl_vs_initial_pct?: number
  position_count: number
  margin_used: number
  margin_used_pct: number
}

export interface Account {
  name: string
  is_sim: boolean
  is_current: boolean
}

export interface AccountsResponse {
  current: string // the NT8 connection's streamed current account
  selected?: string // the trader's PERSISTED chosen account ("" = none chosen → gated)
  accounts: Account[]
}

export interface Position {
  symbol: string
  side: string
  entry_price: number
  mark_price: number
  quantity: number
  leverage: number
  unrealized_pnl: number
  unrealized_pnl_pct: number
  liquidation_price: number
  margin_used: number
}

export interface DecisionAction {
  action: string
  symbol: string
  quantity: number
  leverage: number
  price: number
  stop_loss?: number // Stop loss price
  take_profit?: number // Take profit price
  confidence?: number // AI confidence (0-100)
  reasoning?: string // Brief reasoning
  order_id: number
  timestamp: string
  success: boolean
  error?: string
}

export interface AccountSnapshot {
  total_balance: number
  available_balance: number
  total_unrealized_profit: number
  position_count: number
  margin_used_pct: number
}

export interface DecisionRecord {
  timestamp: string
  cycle_number: number
  system_prompt: string
  input_prompt: string
  cot_trace: string
  decision_json: string
  account_state: AccountSnapshot
  positions: any[]
  candidate_coins: string[]
  decisions: DecisionAction[]
  execution_log: string[]
  success: boolean
  error_message?: string
}

export interface Statistics {
  total_cycles: number
  successful_cycles: number
  failed_cycles: number
  total_open_positions: number
  total_close_positions: number
}

// AI Trading相关类型
export interface TraderInfo {
  trader_id: string
  trader_name: string
  ai_model: string
  exchange_id?: string
  is_running?: boolean
  startup_warning?: string
  show_in_competition?: boolean
  strategy_id?: string
  strategy_name?: string
  custom_prompt?: string
  system_prompt_template?: string
}

// Competition related types
export interface CompetitionTraderData {
  trader_id: string
  trader_name: string
  ai_model: string
  exchange: string
  total_equity: number
  total_pnl: number
  total_pnl_pct: number
  position_count: number
  margin_used_pct: number
  is_running: boolean
}

export interface CompetitionData {
  traders: CompetitionTraderData[]
  count: number
}

// Trader Configuration Data for View Modal
export interface TraderConfigData {
  trader_id?: string
  trader_name: string
  ai_model: string
  exchange_id: string
  strategy_id?: string // 策略ID
  strategy_name?: string // 策略名称
  is_cross_margin: boolean
  show_in_competition: boolean // 是否在竞技场显示
  scan_interval_minutes: number
  initial_balance: number
  is_running: boolean
  // 以下为旧版字段（向后兼容）
  btc_eth_leverage?: number
  altcoin_leverage?: number
  trading_symbols?: string
  custom_prompt?: string
  override_base_prompt?: boolean
  system_prompt_template?: string
}

// Position History Types
export interface HistoricalPosition {
  id: number
  trader_id: string
  exchange_id: string
  exchange_type: string
  symbol: string
  side: string
  quantity: number
  entry_quantity: number
  entry_price: number
  entry_order_id: string
  entry_time: string
  exit_price: number
  exit_order_id: string
  exit_time: string
  realized_pnl: number
  // pnl_corrected wins over realized_pnl in every ledger aggregate (store COALESCE).
  // null = legacy unverified row (excluded from ledger sums).
  pnl_corrected?: number | null
  pnl_correction_note?: string
  fee: number
  leverage: number
  status: string
  close_reason: string
  // R22 PNL-TRUTH (owner ruling R1): "manual" = an NT8 position the bot
  // adopted with no order/arm of its own — listed, never in a bot total.
  origin?: 'bot' | 'manual' | string
  created_at: string
  updated_at: string
}

// Matches Go TraderStats struct exactly
export interface TraderStats {
  total_trades: number
  win_trades: number
  loss_trades: number
  win_rate: number
  profit_factor: number
  sharpe_ratio: number
  total_pnl: number
  total_fee: number
  avg_win: number
  avg_loss: number
  max_drawdown_pct: number
  // R22 PNL-TRUTH (owner ruling R1): the manual NT8 rows left out of every
  // figure above, as their own line ("Manual (excluded): 4 trades, −$3,202.00").
  manual_excluded_trades?: number
  manual_excluded_pnl?: number
  manual_excluded_ids?: number[]
  manual_excluded_line?: string
}

// Matches Go SymbolStats struct exactly
export interface SymbolStats {
  symbol: string
  total_trades: number
  win_trades: number
  win_rate: number
  total_pnl: number
  avg_pnl: number
  avg_hold_mins: number
}

// Matches Go DirectionStats struct exactly
export interface DirectionStats {
  side: string
  trade_count: number
  win_rate: number
  total_pnl: number
  avg_pnl: number
}

export interface PositionHistoryResponse {
  positions: HistoricalPosition[]
  stats: TraderStats | null
  symbol_stats: SymbolStats[]
  direction_stats: DirectionStats[]
  // R22: the CME session day start (17:00 CT) the footer's day total uses.
  session_day_start_ms?: number
}

// Grid Risk Information for frontend display
export interface GridRiskInfo {
  // Leverage info
  current_leverage: number
  effective_leverage: number
  recommended_leverage: number

  // Position info
  current_position: number
  max_position: number
  position_percent: number

  // Liquidation info
  liquidation_price: number
  liquidation_distance: number

  // Market state
  regime_level: string

  // Box state
  short_box_upper: number
  short_box_lower: number
  mid_box_upper: number
  mid_box_lower: number
  long_box_upper: number
  long_box_lower: number
  current_price: number

  // Breakout state
  breakout_level: string
  breakout_direction: string
}
