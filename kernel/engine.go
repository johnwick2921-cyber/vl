package kernel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"vl/logger"
	"vl/market"
	"vl/provider/databento"
	"vl/security"
	"vl/store"
)

// ============================================================================
// Type Definitions
// ============================================================================

// PositionInfo position information
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	PeakPnLPct       float64 `json:"peak_pnl_pct"` // Historical peak profit percentage
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"` // Position update timestamp (milliseconds)
}

// AccountInfo account information
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`      // Account equity
	AvailableBalance float64 `json:"available_balance"` // Available balance
	UnrealizedPnL    float64 `json:"unrealized_pnl"`    // Unrealized profit/loss
	TotalPnL         float64 `json:"total_pnl"`         // Total profit/loss
	TotalPnLPct      float64 `json:"total_pnl_pct"`     // Total profit/loss percentage
	MarginUsed       float64 `json:"margin_used"`       // Used margin
	MarginUsedPct    float64 `json:"margin_used_pct"`   // Margin usage rate
	PositionCount    int     `json:"position_count"`    // Number of positions
}

// CandidateCoin candidate coin (from coin pool)
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"` // Sources: "static" (legacy pool types collapse to static on load)
}

// TradingStats trading statistics (for AI input)
type TradingStats struct {
	TotalTrades int `json:"total_trades"` // RESOLVED closed trades (corrected-column law — never a coerced count)
	// P&L-TRUTH WAVE (2026-09-01): the exclusion count rides with every figure so
	// the prompt can state "+X over N resolved (K unresolved excluded)".
	ResolvedTrades     int     `json:"resolved_trades"`
	UnresolvedExcluded int     `json:"unresolved_excluded"`
	WinRate            float64 `json:"win_rate"`         // Win rate (%)
	ProfitFactor       float64 `json:"profit_factor"`    // Profit factor
	SharpeRatio        float64 `json:"sharpe_ratio"`     // Sharpe ratio
	TotalPnL           float64 `json:"total_pnl"`        // Total profit/loss
	AvgWin             float64 `json:"avg_win"`          // Average win
	AvgLoss            float64 `json:"avg_loss"`         // Average loss
	MaxDrawdownPct     float64 `json:"max_drawdown_pct"` // Maximum drawdown (%)
}

// RecentOrder recently completed order (for AI input)
type RecentOrder struct {
	ID           int64   `json:"id"`            // position row id (so an UNRESOLVED row can be named)
	Resolved     bool    `json:"resolved"`      // P&L-TRUTH WAVE: false = pnl_corrected NULL → no P&L, no percentage
	Symbol       string  `json:"symbol"`        // Trading pair
	Side         string  `json:"side"`          // long/short
	EntryPrice   float64 `json:"entry_price"`   // Entry price
	ExitPrice    float64 `json:"exit_price"`    // Exit price
	RealizedPnL  float64 `json:"realized_pnl"`  // Realized profit/loss
	PnLPct       float64 `json:"pnl_pct"`       // Profit/loss percentage
	EntryTime    string  `json:"entry_time"`    // Entry time
	ExitTime     string  `json:"exit_time"`     // Exit time
	HoldDuration string  `json:"hold_duration"` // Hold duration, e.g. "2h30m"
}

// Context trading context (complete information passed to AI)
type Context struct {
	CurrentTime    string `json:"current_time"`
	RuntimeMinutes int    `json:"runtime_minutes"`
	CallCount      int    `json:"call_count"`
	TraderID       string `json:"-"` // B6: identity for per-trader gate-block counters (set by the trader loop; never serialized)
	// SnapshotMs is WHEN this context's market data was assembled (epoch ms).
	// The B4 stale-feed guard evaluates against THIS clock, not post-AI-call
	// time, so a legal slow call can never turn a live-at-snapshot feed into
	// "stale" (stale-bar dispatch 2026-08-19). Prompt-invisible (json:"-").
	SnapshotMs int64 `json:"-"`
	// A5 (G5) — prompt-ownership tags: field-name → owning trader_id for each
	// account-scoped context field the trader populates. Final prompt assembly
	// asserts every tag == TraderID (the deciding trader); a mismatch is cross-trader
	// contamination → the cycle is skipped. Prompt-data only (json:"-"), so a
	// matching-tag context is byte-identical.
	OwnerTags      map[string]string                  `json:"-"`
	Account        AccountInfo                        `json:"account"`
	Positions      []PositionInfo                     `json:"positions"`
	CandidateCoins []CandidateCoin                    `json:"candidate_coins"`
	PromptVariant  string                             `json:"prompt_variant,omitempty"`
	TradingStats   *TradingStats                      `json:"trading_stats,omitempty"`
	RecentOrders   []RecentOrder                      `json:"recent_orders,omitempty"`
	MarketDataMap  map[string]*market.Data            `json:"-"`
	MultiTFMarket  map[string]map[string]*market.Data `json:"-"`
	// G2 (regime wave 2026-08-21) — per-cycle machine structure snapshot
	// (per-TF trend + swings + events); the HTF veto and the prompt line read
	// it. Prompt-invisible (json:"-"); set by the trader loop each cycle.
	Structure map[string]StructureState `json:"-"`
	// G1 (regime wave 2026-08-21) — HTF veto gate inputs: the Studio toggle
	// (default ON) and the veto timeframe (env HTF_VETO_TF, default 1h).
	// Prompt-invisible; set by the trader loop each cycle.
	HTFVetoEnabled bool   `json:"-"`
	HTFVetoTF      string `json:"-"`
	// G4 (regime wave 2026-08-21) — transition stand-down inputs (the trader
	// maintains the state machine from the cycle's structure snapshot + plan).
	TransitionActive bool   `json:"-"`
	TransitionDir    string `json:"-"` // plan bias "long"|"short" — paused direction
	TransitionDetail string `json:"-"`
	// C6 (2026-08-25) — executor dead-plan gate: non-empty when the active day
	// plan is machine-dead (or planless with day_plan on). ENTRIES are refused;
	// position management (closes/trails) proceeds. Set by the trader loop each
	// cycle; prompt-invisible.
	ExecutorPlanDead string `json:"-"`
	// R4 (2026-08-25) — min_scenario_quality gate inputs: the resolved floor
	// ("A"|"B"|"C", default C = no restriction) + the active plan's scenario
	// quality map (id → quality). Set by the trader loop each cycle;
	// prompt-invisible.
	MinScenarioQuality  string            `json:"-"`
	PlanScenarioQuality map[string]string `json:"-"`
	BTCETHLeverage      int               `json:"-"`
	AltcoinLeverage     int               `json:"-"`
	Timeframes          []string          `json:"-"`

	// Strategy Studio P1 — daily-guardrail inputs measured on the CME session-day
	// (set by the trader loop from the position store; read by the daily-guardrail
	// gate in engine_analysis.go).
	DailyRealizedPnL float64 `json:"-"`
	TradesToday      int     `json:"-"`
	TotalRealizedPnL float64 `json:"-"` // all-time realized P&L (Chunk 5 consistency rule)
}

// Decision AI trading decision
type Decision struct {
	Symbol string `json:"symbol"`
	Action string `json:"action"` // Standard: "open_long", "open_short", "close_long", "close_short", "hold", "wait"

	// P0-cleanup (2026-08-19) — when a gate/armor rewrites an entry to wait,
	// the refusal reason rides the decision so the record shows WHY, never a
	// bare unexplained wait (the C2 lesson).
	RefusalReason string `json:"refusal_reason,omitempty"`
	// Grid actions: "place_buy_limit", "place_sell_limit", "cancel_order", "cancel_all_orders", "pause_grid", "resume_grid", "adjust_grid"

	// Opening position parameters
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`

	// Grid trading parameters
	Price      float64 `json:"price,omitempty"`       // Limit order price (for grid)
	Quantity   float64 `json:"quantity,omitempty"`    // Order quantity (for grid)
	LevelIndex int     `json:"level_index,omitempty"` // Grid level index
	OrderID    string  `json:"order_id,omitempty"`    // Order ID (for cancel)

	// Common parameters
	Confidence int     `json:"confidence,omitempty"` // Confidence level (0-100)
	RiskUSD    float64 `json:"risk_usd,omitempty"`   // Maximum USD risk
	Reasoning  string  `json:"reasoning"`

	// P3.5 (advisory) — the plan scenario the executor cited for this decision,
	// or "off-plan". Empty when day_plan is inactive. Advisory only: it never
	// gates the trade (plan restricts, never compels; hard gates outrank).
	CitedScenario string `json:"cited_scenario,omitempty"`

	// MENTOR P3 — synthetic decisions from the mentor evaluator. EntryPrice is
	// the stop-entry trigger (0 for the AI's market entries); MentorSourced is
	// how the mentor-mode switches (AI-entry suppression, mentor max-contracts)
	// recognise their own decisions.
	EntryPrice    float64 `json:"entry_price,omitempty"`
	MentorSourced bool    `json:"-"`
}

// FullDecision AI's complete decision (including chain of thought)
type FullDecision struct {
	SystemPrompt        string     `json:"system_prompt"`
	UserPrompt          string     `json:"user_prompt"`
	CoTTrace            string     `json:"cot_trace"`
	Decisions           []Decision `json:"decisions"`
	RawResponse         string     `json:"raw_response"`
	Timestamp           time.Time  `json:"timestamp"`
	AIRequestDurationMs int64      `json:"ai_request_duration_ms,omitempty"`
	// F10 — when a guardrail/gate holds the cycle (no actionable decision), the
	// engine returns a FullDecision with SkipReason set (and empty Decisions)
	// instead of (nil, nil), so the caller can stamp the record truthfully
	// (execution_status="guardrail_skip" + reason) rather than leaving a
	// silently-empty success. Empty on a normal decision cycle.
	SkipReason string `json:"skip_reason,omitempty"`
}

// ============================================================================
// StrategyEngine - Core Strategy Execution Engine
// ============================================================================

// StrategyEngine strategy execution engine
type StrategyEngine struct {
	config *store.StrategyConfig

	// svpContextLine is the per-cycle Session Volume Profile line threaded in from
	// the decision loop (engine_analysis.go), where the live bars are reachable.
	// The engine itself has no bar access, so this is set right before
	// BuildSystemPrompt and consumed by the futures prompt when svp_enabled is ON.
	// Empty by default → the futures prompt is byte-identical (golden safe).
	svpContextLine string

	// venue is the trading venue of the trader this engine serves (its
	// exchange). The cycle's market reads route through it (CTO F2): the
	// NinjaTrader venue never reads a non-CME symbol from a crypto source.
	// Empty = route by symbol (Studio previews, agent tools).
	venue string

	// keyLevelsContextLine is the per-cycle day-plan KEY LEVELS block (P1.7),
	// threaded in from the decision loop like svpContextLine. Consumed by the
	// futures prompt ONLY when day_plan is enabled AND the line is non-empty, so
	// the default (day_plan off) keeps the futures prompt byte-identical.
	keyLevelsContextLine string

	// biasContextLine is the per-cycle bias-context facts line (addendum 2,
	// 2026-08-26) — rendered right after the KEY LEVELS block in the futures
	// prompt. Empty → byte-identical.
	biasContextLine string

	// armedContextLine (Wave 2 armed orders, 2026-08-27) — the per-cycle ARMED
	// order status lines (⏳/📌/⚡/✕ per armed scenario). Rendered when non-empty;
	// empty (no arms) → byte-identical prompt.
	armedContextLine string

	// weeklyContextLine (W3, weekly-bias wave 2026-08-30) — the ONE-line
	// executor context from the Sunday weekly read ("WEEKLY: refs only — PWH x · PWL y", class 50);
	// 30500.25"). Rendered when non-empty; empty (no weekly doc) →
	// byte-identical prompt. Soft law — never changes any gate.
	weeklyContextLine string

	// planBlockLine / planStatusLine are the P3.4 executor plan injection: the
	// byte-stable PLAN BLOCK (cached prefix) and the dynamic PLAN STATUS tail.
	// Non-empty planBlockLine (day_plan on + an active plan) triggers the RECON #4
	// reorder — SVP/KEY-LEVELS move to the end alongside PLAN STATUS. Empty (no
	// active plan) → the prompt is unchanged.
	planBlockLine  string
	planStatusLine string
	// planMode (W-EXEC-TRUTH W3 §3) — the RESOLVED plan mode the active PLAN
	// BLOCK was rendered under (setExecutorPlanContext). "" = advisory: the
	// field-description line stays byte-identical.
	planMode string

	// clockContextLine is the per-cycle labelled clock (P0 timezone fix):
	// "## Clock\n07:06 CT (12:06 UTC) — ALL times in this prompt are CT…".
	// Threaded in from the decision loop (one snapshot → one clock) and emitted
	// by BOTH the futures and crypto prompt builders. Empty → byte-identical.
	clockContextLine string
	// promptSnapshotMs (P10.2): the cycle's snapshot instant for the forming-
	// bar label in the market block. 0 = no label (tests/legacy paths).
	promptSnapshotMs int64
}

// SetSVPContext sets the Session Volume Profile line used by the futures prompt
// for the next BuildSystemPrompt call. Pass "" to inject nothing.
func (e *StrategyEngine) SetSVPContext(line string) { e.svpContextLine = line }

// SetKeyLevelsContext sets the day-plan KEY LEVELS block used by the futures
// prompt for the next BuildSystemPrompt call. Pass "" to inject nothing.
func (e *StrategyEngine) SetKeyLevelsContext(line string) { e.keyLevelsContextLine = line }

// SetBiasContext sets the per-cycle bias-context facts line (addendum 2).
func (e *StrategyEngine) SetBiasContext(line string) { e.biasContextLine = line }

// SetArmedContext sets the per-cycle ARMED order status lines (Wave 2).
func (e *StrategyEngine) SetArmedContext(line string) { e.armedContextLine = line }

// SetWeeklyContext (W3, weekly-bias wave) sets the one-line weekly context
// for the executor prompt. Pass "" to inject nothing.
func (e *StrategyEngine) SetWeeklyContext(line string) { e.weeklyContextLine = line }

// SetPlanContext sets the P3.4 executor plan injection: the byte-stable PLAN
// BLOCK (prefix) and the dynamic PLAN STATUS tail. Pass ("","") for no active
// plan (prompt unchanged).
func (e *StrategyEngine) SetPlanContext(planBlock, planStatus string) {
	e.planBlockLine = planBlock
	e.planStatusLine = planStatus
	e.planMode = "" // advisory unless setExecutorPlanContext names the mode
}

// executorPlanModeFor is the plan mode the executor prompt renders for a plan:
// the RESOLVED mode (store.ResolvePlanMode — per-session override → strategy
// → advisory) for the PLAN'S OWN session. The entry gate resolves the ACTIVE
// session's mode; for an active plan the two are the same session.
func executorPlanModeFor(dp *store.DayPlanConfig, session string) string {
	return dp.PlanModeFor(session)
}

// setExecutorPlanContext is the ONE production selection of the executor PLAN
// BLOCK (W3 §3): RenderPlanBlockForMode under the resolved mode, and the mode
// remembered for the cited_scenario field line. kernel/engine_analysis.go
// calls it; the boot self-check's strict fixture calls it too (canon 53).
func (e *StrategyEngine) setExecutorPlanContext(doc PlanDoc, session, planStatus string) {
	var dp *store.DayPlanConfig
	if e.config != nil {
		dp = e.config.DayPlan
	}
	mode := executorPlanModeFor(dp, session)
	e.SetPlanContext(RenderPlanBlockForMode(doc, session, mode), planStatus)
	e.planMode = mode
}

// SetClockContext sets the labelled per-cycle clock line (P0 timezone fix)
// emitted by both prompt builders. Pass "" to inject nothing.
func (e *StrategyEngine) SetClockContext(line string) { e.clockContextLine = line }

// SetPromptSnapshotMs (P10.2) hands the cycle's snapshot instant to the market
// -block renderer so the newest bar can be labelled FORMING/CLOSED honestly —
// interval cadence runs cycles mid-bar and the AI must know what it is looking
// at. 0 (tests/legacy) renders no label — goldens stay byte-identical.
func (e *StrategyEngine) SetPromptSnapshotMs(ms int64) { e.promptSnapshotMs = ms }

// NewStrategyEngine creates strategy execution engine.
// SetVenue records the venue (the trader's exchange) the cycle's market reads
// route through (CTO F2).
func (e *StrategyEngine) SetVenue(venue string) { e.venue = venue }

func NewStrategyEngine(config *store.StrategyConfig) *StrategyEngine {
	return &StrategyEngine{
		config: config,
	}
}

// GetRiskControlConfig gets risk control configuration
func (e *StrategyEngine) GetRiskControlConfig() store.RiskControlConfig {
	return e.config.RiskControl
}

// GetLanguage returns the language from config or falls back to auto-detection
func (e *StrategyEngine) GetLanguage() Language {
	switch e.config.Language {
	case "zh":
		return LangChinese
	case "en":
		return LangEnglish
	default:
		// Fall back to auto-detection from prompt content for backward compatibility
		return detectLanguage(e.config.PromptSections.RoleDefinition)
	}
}

// GetConfig gets complete strategy configuration
func (e *StrategyEngine) GetConfig() *store.StrategyConfig {
	return e.config
}

// ============================================================================
// Candidate Coins
// ============================================================================

// GetCandidateCoins gets candidate coins based on strategy configuration
func (e *StrategyEngine) GetCandidateCoins() ([]CandidateCoin, error) {
	var candidates []CandidateCoin

	coinSource := e.config.CoinSource

	// Defensive guard (additive): a strategy whose stored config omits
	// ai_config.coin_source (e.g. a hand-rolled config from cmd/create-strategy)
	// deserializes to an empty SourceType, which previously fell through to the
	// default branch ("unknown coin source type: ") and made the trader loop skip
	// EVERY cycle. Treat an empty SourceType as "static" so StaticCoins are still
	// used and an empty list degrades to the upstream "no candidates" path instead
	// of a hard error. Does not change behavior for any explicitly-set source type.
	if coinSource.SourceType == "" {
		logger.Infof("⚠️  coin source type empty; defaulting to 'static' (additive guard)")
		coinSource.SourceType = "static"
	}

	switch coinSource.SourceType {
	case "static":
		for _, symbol := range coinSource.StaticCoins {
			symbol = market.Normalize(symbol)
			candidates = append(candidates, CandidateCoin{
				Symbol:  symbol,
				Sources: []string{"static"},
			})
		}

		return e.filterExcludedCoins(candidates), nil

	default:
		return nil, fmt.Errorf("unknown coin source type: %s", coinSource.SourceType)
	}
}

// filterExcludedCoins removes excluded coins from the candidates list
func (e *StrategyEngine) filterExcludedCoins(candidates []CandidateCoin) []CandidateCoin {
	if len(e.config.CoinSource.ExcludedCoins) == 0 {
		return candidates
	}

	// Build excluded set for O(1) lookup
	excluded := make(map[string]bool)
	for _, coin := range e.config.CoinSource.ExcludedCoins {
		normalized := market.Normalize(coin)
		excluded[normalized] = true
	}

	// Filter out excluded coins
	filtered := make([]CandidateCoin, 0, len(candidates))
	for _, c := range candidates {
		if !excluded[c.Symbol] {
			filtered = append(filtered, c)
		} else {
			logger.Infof("🚫 Excluded coin: %s", c.Symbol)
		}
	}

	return filtered
}

// ============================================================================
// External Data
// ============================================================================

// FetchMarketData fetches market data based on strategy configuration
func (e *StrategyEngine) FetchMarketData(symbol string) (*market.Data, error) {
	return market.Get(symbol)
}

// FetchExternalData fetches external data sources
func (e *StrategyEngine) FetchExternalData() (map[string]interface{}, error) {
	externalData := make(map[string]interface{})

	for _, source := range e.config.Indicators.ExternalDataSources {
		data, err := e.fetchSingleExternalSource(source)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch external data source [%s]: %v", source.Name, err)
			continue
		}
		externalData[source.Name] = data
	}

	return externalData, nil
}

func (e *StrategyEngine) fetchSingleExternalSource(source store.ExternalDataSource) (interface{}, error) {
	// SSRF Protection: Validate URL before making request
	if err := security.ValidateURL(source.URL); err != nil {
		return nil, fmt.Errorf("external source URL validation failed: %w", err)
	}

	timeout := time.Duration(source.RefreshSecs) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Use SSRF-safe HTTP client
	client := security.SafeHTTPClient(timeout)

	req, err := http.NewRequest(source.Method, source.URL, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range source.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if source.DataPath != "" {
		result = extractJSONPath(result, source.DataPath)
	}

	return result, nil
}

func extractJSONPath(data interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return nil
		}
	}

	return current
}

// ============================================================================
// Helper Functions
// ============================================================================

// detectLanguage detects language from text content
// Returns LangChinese if text contains Chinese characters, otherwise LangEnglish
func detectLanguage(text string) Language {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			return LangChinese
		}
	}
	return LangEnglish
}

// ============================================================================
// Plan 2 Task 18 — CME session gating (owned by T18; T19 must not edit this block)
// ============================================================================

// ShouldSkipDecisionCycle reports whether the AI decision cycle should be
// skipped because the CME futures market is currently closed. Returns true
// decisionCycleNow is the clock seam for ShouldSkipDecisionCycle; tests pin it
// to an in-session instant so CME weekend/break hours cannot flip assertions
// that expect a non-cme_closed SkipReason.
var decisionCycleNow = time.Now

// ShouldSkipDecisionCycle reports whether the whole decision cycle should be
// skipped because the CME futures market is closed (futures-only build, C2).
//
// Callers (e.g. GetFullDecisionWithStrategy in engine_analysis.go) should
// invoke this at the top of each decision cycle BEFORE any expensive work
// like fetching klines or building prompts.
func ShouldSkipDecisionCycle() bool {
	if IsCMEOpen(decisionCycleNow()) {
		return false
	}
	logger.Info("CME closed, skipping decision cycle")
	return true
}

// ============================================================================
// Plan 2 Task 19 — Contract roll detection
// Block new entries within 5 days of CME quarterly expiry to avoid being
// caught in low-liquidity rollover periods. Owned by T19.
// ============================================================================

// ShouldBlockEntryForExpiry reports whether new entries for the given CME
// futures contract should be blocked because the contract is within 5 days
// of its quarterly expiry. The second return value is the resolved days-
// until-expiry. Unparseable symbols pass through (days=999) so they never
// trigger the block — a deliberately permissive fallback that prefers false
// negatives over false positives.
func ShouldBlockEntryForExpiry(symbol string, now time.Time) (bool, int) {
	days := databento.DaysUntilExpiry(symbol, now)
	return days >= 0 && days <= 5, days
}
