package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/mcp"
	_ "nofx/mcp/payment"
	_ "nofx/mcp/provider"
	"nofx/store"
	"nofx/trader/aster"
	"nofx/trader/binance"
	"nofx/trader/bitget"
	"nofx/trader/bybit"
	"nofx/trader/gate"
	"nofx/trader/hyperliquid"
	"nofx/trader/indodax"
	"nofx/trader/kucoin"
	"nofx/trader/lighter"
	ntTrader "nofx/trader/ninjatrader"
	"nofx/trader/okx"
	"nofx/wallet"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

func (at *AutoTrader) logTag() string {
	if at == nil {
		return "[trader_id=unknown]"
	}
	if at.name != "" {
		return fmt.Sprintf("[trader_id=%s trader_name=%s]", at.id, at.name)
	}
	return fmt.Sprintf("[trader_id=%s]", at.id)
}

func (at *AutoTrader) logInfof(format string, args ...interface{}) {
	values := append([]interface{}{at.logTag()}, args...)
	logger.Infof("%s "+format, values...)
}

func (at *AutoTrader) logWarnf(format string, args ...interface{}) {
	values := append([]interface{}{at.logTag()}, args...)
	logger.Warnf("%s "+format, values...)
}

func (at *AutoTrader) logErrorf(format string, args ...interface{}) {
	values := append([]interface{}{at.logTag()}, args...)
	logger.Errorf("%s "+format, values...)
}

// ninjaFeedDown reports whether this is a NinjaTrader trader whose price feed is
// explicitly NOT Connected. Used to gate opens/closes: the SIM rejects orders
// with "no market data" while the feed is down (the upstream condition behind the
// phantom-close mess). DEFAULT-ALLOW: returns false for non-NT traders and until
// the first feed_status frame arrives, so a healthy bot is never false-halted.
func (at *AutoTrader) ninjaFeedDown() (bool, string) {
	if at.exchange != "ninjatrader" {
		return false, ""
	}
	ntTCP, ok := at.trader.(*ntTrader.TCPTrader)
	if !ok {
		return false, ""
	}
	status := ntTCP.FeedStatus()
	if ntTCP.IsFeedConnected() {
		return false, status
	}
	return true, status
}

// ninjaLinkConnected reports the RAW NT8 TCP link status for this trader, and
// whether the watchdog applies at all (ok=false for non-NT traders → the dead-man
// watchdog is never engaged, so crypto is byte-identical).
func (at *AutoTrader) ninjaLinkConnected() (connected, ok bool) {
	if at.exchange != "ninjatrader" {
		return false, false
	}
	ntTCP, isNT := at.trader.(*ntTrader.TCPTrader)
	if !isNT {
		return false, false
	}
	return ntTCP.IsConnected(), true
}

// driveDeadManWatchdog (B5) advances the dead-man watchdog once per cycle from the
// NT8 TCP link. A dropped link blocks NEW entries (open-position management/exits
// are never gated); on reconnect it sweeps any unfilled entries and holds entries
// blocked until a clean positions/orders reconciliation, then auto-resumes. Called
// from runCycle; non-NT traders are a no-op.
func (at *AutoTrader) driveDeadManWatchdog() {
	connected, ok := at.ninjaLinkConnected()
	if !ok {
		return
	}
	switch at.deadMan.step(connected, at.watchdogReconcileClean) {
	case wdWentDown:
		at.logWarnf("🚨 dead-man watchdog: NT8 TCP link DOWN — NEW entries BLOCKED until a clean reconciliation. Open positions & exits are unaffected.")
	case wdReconnected:
		at.logWarnf("🔌 dead-man watchdog: NT8 TCP link back UP — sweeping unfilled entries; entries stay BLOCKED until a clean positions/orders reconciliation.")
		at.cancelUnfilledEntriesAfterReconnect()
	case wdResumed:
		at.logInfof("✅ dead-man watchdog: clean positions/orders reconciliation — NEW entries RESUMED.")
	}
}

// watchdogReconcileClean is the reconcile probe: the link is considered reconciled
// when it answers BOTH a positions and an open-orders query without error (the
// fill-confirmed position source of truth is refreshed by StartPositionReconcile in
// the background; here we just require the link to respond cleanly). A one-cycle
// defer after reconnect (see deadManWatchdog.step) lets the 15s positions cache
// expire and a fresh frame arrive before this probes.
func (at *AutoTrader) watchdogReconcileClean() bool {
	if _, err := at.trader.GetPositions(); err != nil {
		return false
	}
	if _, err := at.trader.GetOpenOrders(""); err != nil {
		return false
	}
	return true
}

// cancelUnfilledEntriesAfterReconnect sweeps resting UNFILLED entry orders on
// reconnect. On the live NT8 path this is a documented no-op: entries are MARKET
// orders that fill or reject instantly (no resting unfilled entries), and the Go
// side has no order-cancel wire command (CancelAllOrders/CancelStopOrders return
// "not supported"; GetOpenOrders is empty) — exposing one is a Part A (C# AddOn)
// change. The real protection is the block-until-reconcile gate. If a future Part A
// surfaces resting entry orders + a cancel frame, wire the cancellation HERE.
func (at *AutoTrader) cancelUnfilledEntriesAfterReconnect() {
	orders, err := at.trader.GetOpenOrders("")
	if err != nil || len(orders) == 0 {
		return
	}
	at.logWarnf("dead-man watchdog: %d working order(s) visible post-reconnect — entry cancellation is a Part A AddOn capability (no-op on the current market-order path).", len(orders))
}

// maybeMoveStopToBreakeven (auto-breakeven, NT8 futures only): once an open
// position is at least breakeven_trigger_points in profit, move its stop to the
// entry price (breakeven), ONCE per position. Opt-in per strategy, default OFF.
// The move is a wire command to the AddOn (it modifies the live bracket in
// place) — it never closes the position. Idempotent via breakevenDone, which is
// cleared when the position goes flat (pruneBreakevenDone).
func (at *AutoTrader) maybeMoveStopToBreakeven(symbol, side string, entryPrice, markPrice float64) {
	if at.exchange != "ninjatrader" || at.config.StrategyConfig == nil {
		return
	}
	fire, pts := breakevenTrigger(at.config.StrategyConfig.RiskControl, side, entryPrice, markPrice)
	if !fire {
		return
	}
	// 0B — SUSPENDED: the trigger fired, the wire stays untouched.
	if at.exitMechSuspendedRefuse("auto-breakeven", fmt.Sprintf("%s %s +%.1f pts, stop would move to entry %.2f", symbol, side, pts, entryPrice)) {
		return
	}
	key := symbol + "_" + side
	at.breakevenMu.Lock()
	if at.breakevenDone == nil {
		at.breakevenDone = make(map[string]bool)
	}
	if at.breakevenDone[key] {
		at.breakevenMu.Unlock()
		return
	}
	at.breakevenDone[key] = true
	at.breakevenMu.Unlock()

	ntTCP, ok := at.trader.(*ntTrader.TCPTrader)
	if !ok {
		return
	}
	if err := moveStopWire(ntTCP, side, entryPrice); err != nil {
		logger.Warnf("⚠️ auto-breakeven: move-stop send failed for %s %s: %v", symbol, side, err)
		at.breakevenMu.Lock()
		at.breakevenDone[key] = false // let it retry next cycle
		at.breakevenMu.Unlock()
		return
	}
	// WARN (honest-logs 2026-08-19): a stop amendment is an owner-visible event —
	// WARN reaches the log_events DB sink + dashboard even when journald's
	// frame-flood suppression is dropping INFO lines (the "breakeven not
	// moving" false alarm was exactly this line being invisible).
	logger.Warnf("🎯 auto-breakeven: %s %s +%.1f pts in profit → stop moved to breakeven (entry %.2f)",
		symbol, side, pts, entryPrice)
}

// breakevenTrigger is the pure decision for auto-breakeven: given the strategy's
// breakeven config and a position's side/entry/mark, it returns whether the stop
// should move to breakeven now and the current points in profit. Default trigger
// is 50 points when unset. Testable without any broker/NT8 dependency.
func breakevenTrigger(rc store.RiskControlConfig, side string, entry, mark float64) (bool, float64) {
	if !hlBool(rc.BreakevenEnabled, false) {
		return false, 0
	}
	trigger := rc.BreakevenTriggerPoints
	if trigger <= 0 {
		trigger = 50
	}
	// Normalise side casing once. The sole production caller (checkPositionDrawdown)
	// feeds pos["side"] from NT8's GetPositions/positionMap, which emits UPPERCASE
	// "LONG"/"SHORT" (upperSideStr). A case-sensitive == "long" never matched, so the
	// profit math was inverted: breakeven never armed on a winning trade and would
	// only "fire" on a loser. Compare lowercase so either casing works.
	var pts float64
	if strings.ToLower(side) == "long" {
		pts = mark - entry
	} else {
		pts = entry - mark
	}
	return pts >= trigger, pts
}

// pruneBreakevenDone clears the breakeven idempotency flag for any position that
// is no longer open (so a fresh trade on the same symbol/side re-arms breakeven).
// openKeys is the set of "symbol_side" currently open this cycle.
func (at *AutoTrader) pruneBreakevenDone(openKeys map[string]bool) {
	at.breakevenMu.Lock()
	defer at.breakevenMu.Unlock()
	for k := range at.breakevenDone {
		if !openKeys[k] {
			delete(at.breakevenDone, k)
		}
	}
}

// AutoTraderConfig auto trading configuration (simplified version - AI makes all decisions)
type AutoTraderConfig struct {
	// Trader identification
	ID      string // Trader unique identifier (for log directory, etc.)
	Name    string // Trader display name
	AIModel string // AI model: "qwen" or "deepseek"
	// AIModelID is the ai_models ROW id the trader is bound to (class 37,
	// 2026-09-01): named on every failed planner call so a provider failure is
	// attributable to a row without ever printing the key.
	AIModelID string

	// Trading platform selection
	Exchange   string // Exchange type: "binance", "bybit", "okx", "bitget", "gate", "hyperliquid", "aster", "lighter", "indodax", or "ninjatrader"
	ExchangeID string // Exchange account UUID (for multi-account support)

	// Binance API configuration
	BinanceAPIKey    string
	BinanceSecretKey string

	// Bybit API configuration
	BybitAPIKey    string
	BybitSecretKey string

	// OKX API configuration
	OKXAPIKey     string
	OKXSecretKey  string
	OKXPassphrase string

	// Bitget API configuration
	BitgetAPIKey     string
	BitgetSecretKey  string
	BitgetPassphrase string

	// Gate API configuration
	GateAPIKey    string
	GateSecretKey string

	// KuCoin API configuration
	KuCoinAPIKey     string
	KuCoinSecretKey  string
	KuCoinPassphrase string

	// Indodax API configuration
	IndodaxAPIKey    string
	IndodaxSecretKey string

	// NinjaTrader CSV bridge configuration
	NinjaTraderDataDir string // /mnt/c/Users/<u>/NofxTrader/data
	NinjaTraderSymbol  string // e.g. "MNQ" (informational; NT uses chart's instrument)
	NinjaTraderAccount string // P5.4 — the NT8 sub-account this trader is bound to (store.Trader.Account); empty = active account

	// Hyperliquid configuration
	HyperliquidPrivateKey  string
	HyperliquidWalletAddr  string
	HyperliquidTestnet     bool
	HyperliquidUnifiedAcct bool // Unified Account mode: Spot USDC as Perp collateral

	// Aster configuration
	AsterUser       string // Aster main wallet address
	AsterSigner     string // Aster API wallet address
	AsterPrivateKey string // Aster API wallet private key

	// LIGHTER configuration
	LighterWalletAddr       string // LIGHTER wallet address (L1 wallet)
	LighterPrivateKey       string // LIGHTER L1 private key (for account identification)
	LighterAPIKeyPrivateKey string // LIGHTER API Key private key (40 bytes, for transaction signing)
	LighterAPIKeyIndex      int    // LIGHTER API Key index (0-255)
	LighterTestnet          bool   // Whether to use testnet

	// AI configuration
	UseQwen     bool
	DeepSeekKey string
	QwenKey     string

	// Custom AI API configuration
	CustomAPIURL     string
	CustomAPIKey     string
	CustomModelName  string
	Claw402WalletKey string

	// Scan configuration
	ScanInterval time.Duration // Scan interval (recommended 3 minutes)
	// 4.3 — limit-then-market EOD/T1 flatten (research v5 "slippage budgeted").
	// DORMANT by default: LimitCloseTicks 0 = pure market flatten (byte-identical
	// to the historical behavior). > 0 places a limit exit LimitCloseTicks beyond
	// the latest bar close first, then market-flattens any remainder after
	// LimitCloseMarketAfterS seconds.
	LimitCloseTicks        int
	LimitCloseMarketAfterS int
	// CadenceMode (P10): "interval" (default — every tick runs a full cycle on
	// the latest bar state) | "bar_close" (legacy: one cycle per closed
	// primary-TF bar). Resolved via cadenceMode(); only meaningful for day-plan
	// futures traders (crypto/plan-off always ran per-tick).
	CadenceMode string
	// PositionMode (Phase 3): "ai_watch" (default — watch cycles while holding,
	// zero order authority) | "bracket_only" (legacy skip-while-open).
	PositionMode string

	// Account configuration
	InitialBalance float64 // Initial balance (for P&L calculation, must be set manually)

	// Risk control (only as hints, AI can make autonomous decisions)
	MaxDailyLoss    float64       // Maximum daily loss percentage (hint)
	MaxDrawdown     float64       // Maximum drawdown percentage (hint)
	StopTradingTime time.Duration // Pause duration after risk control triggers

	// Position mode
	IsCrossMargin bool // true=cross margin mode, false=isolated margin mode

	// Competition visibility
	ShowInCompetition bool // Whether to show in competition page

	// Strategy configuration (use complete strategy config)
	StrategyConfig *store.StrategyConfig // Strategy configuration (includes coin sources, indicators, risk control, prompts, etc.)
}

// AutoTrader automatic trader
type AutoTrader struct {
	id                string // Trader unique identifier
	name              string // Trader display name
	aiModel           string // AI model name
	exchange          string // Trading platform type (binance/bybit/etc)
	exchangeID        string // Exchange account UUID
	showInCompetition bool   // Whether to show in competition page
	config            AutoTraderConfig
	trader            Trader // Use Trader interface (supports multiple platforms)
	mcpClient         mcp.AIClient
	store             *store.Store           // Data storage (decision records, etc.)
	strategyEngine    *kernel.StrategyEngine // Strategy engine (uses strategy configuration)
	cycleNumber       int                    // Current cycle number
	initialBalance    float64
	dailyPnL          float64
	// F3 fast-market wake reads (waterfall-class wave, 2026-08-28): the price
	// at the last successful plan write + a one-shot flag for the next read.
	lastPlanWritePrice atomic.Uint64 // math.Float64bits
	fastTapePending    atomic.Bool
	// lastClockHealthSession: which session the last clock-health line was
	// logged for (PHASE 3.5) — one line per session roll, not per tick.
	lastClockHealthSession string
	lastResetTime          time.Time
	pauseUntilMs           atomic.Int64 // P2 stop_until producer state (unix ms; 0 = not paused) — see auto_trader_pause.go
	pauseStoreMu           sync.Mutex   // E7-v2: orders memory-vs-store pause writes (expiry CAS vs concurrent re-pause)
	lastRollWarnContract   string       // P3 roll gate: dedupes the unresolved-contract WARN per contract-string change
	lastHalfDaySeedDay     string       // P4 half-days producer: once-per-CME-session-day throttle
	lastCycleBarSig        string       // P10.4 no-new-data dedup: newest primary-TF bar signature at last cycle
	ai402OutageStartMs     int64        // P5 402-outage latch (0 = no outage) — one banner per outage
	// G4 (regime wave 2026-08-21) — transition stand-down state + the G4.6 MSS
	// wake dedupe key (plan:version:eventInstant — one planner wake per MSS).
	transition           kernel.TransitionState
	transitionClosedAtMs int64 // G4: last closed trigger — the same event must not reopen the stand-down
	lastMSSWakeKey       string
	// W6 (2026-08-25) — level-event wake state: the dedupe key
	// (plan:version:kind:label:tier:birth) and the SHARED planner-wake clock
	// (deaths don't reset it; MSS + level wakes do, so the two wake classes
	// can never double-fire inside one wake_min_interval_min window).
	lastLevelWakeKey      string
	lastPlannerWakeAt     time.Time
	lastAIBalanceDay      string // P5 daily balance poll throttle (AI_BALANCE_WARN)
	isRunning             bool
	isRunningMutex        sync.RWMutex          // Mutex to protect isRunning flag
	startTime             time.Time             // System start time
	callCount             int                   // AI call count
	positionFirstSeenTime map[string]int64      // Position first seen time (symbol_side -> timestamp in milliseconds)
	stopMonitorCh         chan struct{}         // Used to stop monitoring goroutine
	monitorWg             sync.WaitGroup        // Used to wait for monitoring goroutine to finish
	kickCh                chan string           // discard-burn/post-exit: one-shot deferred-cycle kicks into the run loop (reason payload)
	kickPending           atomic.Bool           // at most one kick armed at a time (CAS)
	skipDodgeOnce         bool                  // a dodge-kicked cycle must not re-dodge at the boundary (run-loop goroutine only)
	skipCadenceOnce       bool                  // U2: a post_exit kick bypasses the cadence gates exactly once (run-loop goroutine only)
	cycleTrigger          string                // why this cycle fired: "" (timer) | "stale_dodge" | "post_exit" (run-loop goroutine only)
	aiCallMs              [aiCallRingSize]int64 // last-N AI call durations (run-loop goroutine only)
	aiCallIdx             int
	aiCallN               int
	peakPnLCache          map[string]float64     // Peak profit cache (symbol -> peak P&L percentage)
	peakPnLCacheMutex     sync.RWMutex           // Cache read-write lock
	breakevenDone         map[string]bool        // auto-breakeven: "symbol_side" already moved to breakeven (idempotent; reset on flat)
	entryTheses           map[string]entryThesis // Phase 3: original entry decision per "symbol_side" (run-loop goroutine)
	watchStates           map[string]*watchState // Phase 3: watcher hysteresis state per "symbol_side" (run-loop goroutine)
	trailStates           map[string]*trailState // Phase 3B: trailing-stop state per "symbol_SIDE" (guarded by trailMu — monitor + watcher goroutines)
	trailMu               sync.Mutex             // guards trailStates
	postExitSeen          map[int64]bool         // Phase 4: position IDs whose post-exit rescan already fired (guarded by postExitMu)
	postExitMu            sync.Mutex
	breakevenMu           sync.Mutex      // guards breakevenDone (lazy-inited)
	lastBalanceSyncTime   time.Time       // Last balance sync time
	userID                string          // User ID
	gridState             *GridState      // Grid trading state (only used when StrategyType == "grid_trading")
	claw402WalletAddr     string          // Claw402 wallet address (derived from private key at start)
	consecutiveAIFailures int             // Consecutive AI call failures
	safeMode              bool            // Safe mode: no new positions, protect existing ones
	safeModeReason        string          // Why safe mode was activated
	deadMan               deadManWatchdog // B5 dead-man watchdog: NT8 link-gap → block NEW entries until reconciled (zero value = live/allowed; touched only from runCycle)

	// armedSyncSeam — fixture seam for the synchronous armed cancel (S-list
	// closer FIX1): wire cancel + ack stream + timeout. nil = production TCP
	// path (armedTrader().CancelOrder + the shared order_update subscription).
	armedSyncSeam *armedSyncSeam

	// armRefusalLast dedupes the every-cycle arm REFUSED log (F4, LONDON-
	// FORENSICS 2026-08-28): one line per (plan, version, scenario, verdict);
	// silent until the spec or the refusal reason changes. Run-loop goroutine
	// only — no lock.
	armRefusalLast map[string]string
	// 0B — per-arm stop-composition log dedup (plan:version:scenario:leg:stop).
	armStopCompLast map[string]string
	// armAuthoredLast dedupes the every-cycle "⚔️ armed" log (PRE-REOPEN F3,
	// 2026-08-28) — the dead-row re-log spam (69+ lines/day) fired on every
	// placement beat because the ledger row already existed. Log once per
	// (plan:version:scenario) spec, again only when the prices change.
	armAuthoredLast map[string]string

	// Plan 4 Stage 4 — NinjaTrader TCP balance tracking (defer-until-balance guard)
	// For NinjaTrader TCP traders, we track if account_balance frame has arrived yet.
	// If equity == 0 and this is false, we skip the cycle silently (no phantom HOLD record).
	hasReceivedBalance bool
	balanceMutex       sync.RWMutex

	// PART A — CME session gate. Tracks the last-observed market open/closed
	// state so the loop logs the open⇄closed transition once (not every cycle).
	// nil = not yet observed. Touched only from runCycle (single goroutine), so
	// no mutex is required.
	cmePrevOpen *bool

	// P2.1 — bar-close cadence: CloseTime (ms) of the last primary-TF bar we ran
	// a cycle for. Only meaningful when barCloseCadenceActive() (day_plan futures);
	// otherwise the scan timer drives the loop unchanged. Touched only from Run's
	// single goroutine, so no mutex is required.
	lastBarCloseMs int64

	// P3.6-D — night mode: last-observed night/day state for edge-triggered
	// transition events. nil = unobserved (a restart starts here → no spurious
	// edge). Touched only from runCycle (single goroutine).
	nightPrev *bool
	// W16/R1 — last scenario-status blob written, so the per-scenario log line
	// fires on CHANGE rather than every cycle.
	scenarioStateLog string

	// P5.5 — the last entry's plan citation, captured in recordPlanCitation and
	// consumed once by the very next position-open stamp (single-goroutine loop).
	lastCitation planCitation

	// W3 — throttle for the calendar producer (retry the FF fetch ≤1/hour on
	// outage; a stored slice short-circuits it). Touched only from runCycle.
	lastCalFetch time.Time
	// P0.6 (2026-08-19) — calendar fail-closed alert, once per trade date.
	lastCalFailClosedAlert string
	// F6 (2026-08-30) — clock-hold T1-widening warn line, once per trade date.
	lastClockWidenLog string
	// F0 — calendar test seams + log dedupe: calFetch overrides the live FF
	// fetch in tests (nil → calendar.DefaultFetch); lastCalSkipDate makes the
	// "skip-fresh" line log once per trade date, not every 3-min cycle.
	calFetch        func() ([]byte, error)
	lastCalSkipDate string
	// lastAlertPruneDay throttles the acked-P2 alert-feed prune to once per
	// CME session-day (B-fix: PruneAckedOlderThan had no production caller).
	lastAlertPruneDay string

	// P2 — regime health from the most recent planner read (dark-field count +
	// DEGRADED verdict), stamped onto the plan row at the write site.
	lastRegimeHealth kernel.RegimeHealth

	// W8 — admin session-registry cache. Loaded from system_config and refreshed
	// once per CME session-day so an edit is honored by the NEXT session-day's
	// gates (never mid-session — a running session's windows never move under it).
	regMu       sync.Mutex
	regCache    kernel.SessionRegistry
	regCacheDay string

	// W2 (weekly-bias wave 2026-08-30) — Sunday weekly read / mid-week
	// invalidation / W5 shadow state. See trader/auto_trader_weekly.go.
	weeklyState weeklyState
}

// planCitation is the transient plan-link snapshot stamped onto a new position.
type planCitation struct {
	planVersion int
	scenarioID  string
	matched     bool
	band        string // B3 (F6): "" | "ok" | "off_band" | "struct"
	// S3 (mega-research 2026-08-26) — the ACTIVE plan's identity at DECISION
	// time. Stamped onto the position at open so a session handoff can never
	// re-attribute an entry to the prior session's plan.
	planID    string
	tradeDate string
	session   string
	valid     bool
}

// NewAutoTrader creates an automatic trader
// st parameter is used to store decision records to database
func NewAutoTrader(config AutoTraderConfig, st *store.Store, userID string) (*AutoTrader, error) {
	// Set default values
	if config.ID == "" {
		config.ID = "default_trader"
	}
	if config.Name == "" {
		config.Name = "Default Trader"
	}
	if config.AIModel == "" {
		if config.UseQwen {
			config.AIModel = "qwen"
		} else {
			config.AIModel = "deepseek"
		}
	}

	// Initialize AI client based on provider
	var mcpClient mcp.AIClient
	aiModel := config.AIModel
	if config.UseQwen && aiModel == "" {
		aiModel = "qwen"
	}

	// Resolve API key (provider-specific overrides)
	apiKey := config.CustomAPIKey
	customURL := config.CustomAPIURL
	switch aiModel {
	case "qwen":
		if config.QwenKey != "" {
			apiKey = config.QwenKey
		}
	case "deepseek", "":
		if config.DeepSeekKey != "" {
			apiKey = config.DeepSeekKey
		}
	}

	// Create client via registry (covers all registered providers)
	if aiModel == "custom" {
		mcpClient = mcp.New()
	} else if aiModel == "" {
		aiModel = "deepseek"
		mcpClient = mcp.NewAIClientByProvider(aiModel)
	} else {
		mcpClient = mcp.NewAIClientByProvider(aiModel)
	}
	if mcpClient == nil {
		mcpClient = mcp.New()
	}
	// 4.5 — per-model thinking knobs override the env defaults (best-effort row
	// lookup; a miss keeps the env defaults).
	if st != nil {
		if row, err := st.AIModel().GetByID(aiModel); err == nil && row != nil {
			mcp.ApplyThinking(mcpClient, row.ThinkingMode, row.ReasoningEffort)
		}
	}

	// P0-latency — the timeout applied here is the ONE config-driven AI timeout
	// (mcp.ResolvedAITimeout). NOTE (audit 2026-08-18): with an EMPTY
	// day_plan.planner_model binding, resolvePlannerClient returns THIS SAME
	// client — the old claim that "the planner read uses its OWN client" is only
	// true when a planner model is explicitly bound. Sharing is now harmless
	// because executor and planner resolve the identical timeout, but the
	// comment was wrong and hid a class-7 hazard. Crypto cadence untouched; the
	// stale-bar discard in runCycle is the second half of the guarantee.
	applyDecisionCallTimeout(mcpClient, config.Exchange)

	// Payment providers (claw402) ignore customURL
	switch aiModel {
	case "claw402":
		mcpClient.SetAPIKey(apiKey, "", config.CustomModelName)
	default:
		mcpClient.SetAPIKey(apiKey, customURL, config.CustomModelName)
	}
	logger.Infof("🤖 [%s] Using %s AI", config.Name, aiModel)

	if config.CustomAPIURL != "" || config.CustomModelName != "" {
		logger.Infof("🔧 [%s] Custom config - URL: %s, Model: %s", config.Name, config.CustomAPIURL, config.CustomModelName)
	}

	// Set default trading platform
	if config.Exchange == "" {
		config.Exchange = "binance"
	}

	// Create corresponding trader based on configuration
	var trader Trader
	var err error

	// Record position mode (general)
	marginModeStr := "Cross Margin"
	if !config.IsCrossMargin {
		marginModeStr = "Isolated Margin"
	}
	logger.Infof("📊 [%s] Position mode: %s", config.Name, marginModeStr)

	switch config.Exchange {
	case "binance":
		logger.Infof("🏦 [%s] Using Binance Futures trading", config.Name)
		trader = binance.NewFuturesTrader(config.BinanceAPIKey, config.BinanceSecretKey, userID)
	case "bybit":
		logger.Infof("🏦 [%s] Using Bybit Futures trading", config.Name)
		trader = bybit.NewBybitTrader(config.BybitAPIKey, config.BybitSecretKey)
	case "okx":
		logger.Infof("🏦 [%s] Using OKX Futures trading", config.Name)
		trader = okx.NewOKXTrader(config.OKXAPIKey, config.OKXSecretKey, config.OKXPassphrase)
	case "bitget":
		logger.Infof("🏦 [%s] Using Bitget Futures trading", config.Name)
		trader = bitget.NewBitgetTrader(config.BitgetAPIKey, config.BitgetSecretKey, config.BitgetPassphrase)
	case "gate":
		logger.Infof("🏦 [%s] Using Gate.io Futures trading", config.Name)
		trader = gate.NewGateTrader(config.GateAPIKey, config.GateSecretKey)
	case "kucoin":
		logger.Infof("🏦 [%s] Using KuCoin Futures trading", config.Name)
		trader = kucoin.NewKuCoinTrader(config.KuCoinAPIKey, config.KuCoinSecretKey, config.KuCoinPassphrase)
	case "hyperliquid":
		logger.Infof("🏦 [%s] Using Hyperliquid trading", config.Name)
		trader, err = hyperliquid.NewHyperliquidTrader(config.HyperliquidPrivateKey, config.HyperliquidWalletAddr, config.HyperliquidTestnet, config.HyperliquidUnifiedAcct)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Hyperliquid trader: %w", err)
		}
	case "aster":
		logger.Infof("🏦 [%s] Using Aster trading", config.Name)
		trader, err = aster.NewAsterTrader(config.AsterUser, config.AsterSigner, config.AsterPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Aster trader: %w", err)
		}
	case "lighter":
		logger.Infof("🏦 [%s] Using LIGHTER trading", config.Name)

		if config.LighterWalletAddr == "" || config.LighterAPIKeyPrivateKey == "" {
			return nil, fmt.Errorf("Lighter requires wallet address and API Key private key")
		}

		// Lighter only supports mainnet (testnet disabled)
		trader, err = lighter.NewLighterTraderV2(
			config.LighterWalletAddr,
			config.LighterAPIKeyPrivateKey,
			config.LighterAPIKeyIndex,
			false, // Always use mainnet for Lighter
		)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize LIGHTER trader: %w", err)
		}
		logger.Infof("✓ LIGHTER trader initialized successfully")
	case "indodax":
		logger.Infof("🏦 [%s] Using Indodax Spot trading", config.Name)
		trader = indodax.NewIndodaxTrader(config.IndodaxAPIKey, config.IndodaxSecretKey)
	case "ninjatrader":
		logger.Infof("🏦 [%s] Using NinjaTrader (transport via NT_TRANSPORT env, CME futures via SIM)", config.Name)
		if config.NinjaTraderDataDir == "" {
			return nil, fmt.Errorf("ninjatrader requires NinjaTraderDataDir (set the NT8 data dir on the exchange row (Settings → Exchange → nt_data_dir); the NINJATRADER_DATA_DIR env is not read by the live path)")
		}
		trader, err = ntTrader.NewTraderFromEnv(ntTrader.Config{
			DataDir: config.NinjaTraderDataDir,
			Symbol:  config.NinjaTraderSymbol,
			Account: config.NinjaTraderAccount, // P5.4 per-trader account binding
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize NinjaTrader: %w", err)
		}
		// Bar persistence (2026-08-26) — closed-bar history writer for
		// replay/calibration. Idempotent (once); nil-safe; never blocks the loop.
		if st != nil {
			ntTrader.WireBarPersistence(st)
			// B4 (2026-08-26) — level_stats nightly forward validation.
			ntTrader.WireLevelStatsNightly(st, config.ID)
			// T1 (2026-08-26) — touch-telemetry sink: persist closed episodes
			// (advisory; append-only). One sink per process.
			if te := st.TouchEpisodes(); te != nil {
				if err := te.Migrate(); err == nil {
					kernel.SetTouchEpisodeSink(func(ep kernel.TouchEpisode) {
						_ = te.Insert(store.TouchEpisodeDB{
							TraderID: ep.TraderID, SessionDay: kernel.CMESessionDayKey(time.UnixMilli(ep.ClosedAtMs)),
							Symbol: ep.Symbol, Label: ep.Label, LevelPrice: ep.LevelPrice,
							Number: ep.Number, OpenedAtMs: ep.OpenedAtMs, ClosedAtMs: ep.ClosedAtMs,
							BarsIn: ep.BarsIn, PenetrationPts: ep.PenetrationPts, WickPenPts: ep.WickPenPts,
							BodyPenPts: ep.BodyPenPts, Close1m: ep.Close1m, Close5m: ep.Close5m,
							VolRatio: ep.VolRatio, ApproachATR: ep.ApproachATR, Shape: ep.Shape,
						})
					})
				}
			}
		}
		// Plan 4 Stage 4 — set parent reference for defer-until-balance guard.
		// This is set AFTER the AutoTrader is partially initialized, so we defer
		// it until later in NewAutoTrader.
	default:
		return nil, fmt.Errorf("unsupported trading platform: %s", config.Exchange)
	}

	// Validate initial balance configuration, auto-fetch from exchange if 0
	if config.InitialBalance <= 0 {
		logger.Infof("📊 [%s] Initial balance not set, attempting to fetch current balance from exchange...", config.Name)
		account, err := trader.GetBalance()
		if err != nil {
			return nil, fmt.Errorf("initial balance not set and unable to fetch balance from exchange: %w", err)
		}
		// Try multiple balance field names (different exchanges return different formats)
		balanceKeys := []string{"total_equity", "totalWalletBalance", "wallet_balance", "totalEq", "balance"}
		var foundBalance float64
		for _, key := range balanceKeys {
			if balance, ok := account[key].(float64); ok && balance > 0 {
				foundBalance = balance
				break
			}
		}
		if foundBalance > 0 {
			config.InitialBalance = foundBalance
			logger.Infof("✓ [%s] Auto-fetched initial balance: %.2f USDT", config.Name, foundBalance)
			// Save to database so it persists across restarts
			if st != nil {
				if err := st.Trader().UpdateInitialBalance(userID, config.ID, foundBalance); err != nil {
					logger.Infof("⚠️  [%s] Failed to save initial balance to database: %v", config.Name, err)
				} else {
					logger.Infof("✓ [%s] Initial balance saved to database", config.Name)
				}
			}
		} else {
			return nil, fmt.Errorf("initial balance must be greater than 0, please set InitialBalance in config or ensure exchange account has balance")
		}
	}

	// Get last cycle number (for recovery)
	var cycleNumber int
	if st != nil {
		cycleNumber, _ = st.Decision().GetLastCycleNumber(config.ID)
		logger.Infof("📊 [%s] Decision records will be stored to database", config.Name)
	}

	// Create strategy engine (must have strategy config)
	if config.StrategyConfig == nil {
		return nil, fmt.Errorf("[%s] strategy not configured", config.Name)
	}
	// Pass claw402 wallet key to strategy engine so nofxos data requests
	// are routed through claw402 (reuses the same wallet as AI calls)
	claw402Key := config.Claw402WalletKey
	if claw402Key == "" && config.AIModel == "claw402" && config.CustomAPIKey != "" {
		claw402Key = config.CustomAPIKey
	}
	strategyEngine := kernel.NewStrategyEngine(config.StrategyConfig, claw402Key)
	logger.Infof("✓ [%s] Using strategy engine (strategy configuration loaded)", config.Name)

	at := &AutoTrader{
		id:                    config.ID,
		name:                  config.Name,
		aiModel:               config.AIModel,
		exchange:              config.Exchange,
		exchangeID:            config.ExchangeID,
		showInCompetition:     config.ShowInCompetition,
		config:                config,
		trader:                trader,
		mcpClient:             mcpClient,
		store:                 st,
		strategyEngine:        strategyEngine,
		cycleNumber:           cycleNumber,
		initialBalance:        config.InitialBalance,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		stopMonitorCh:         make(chan struct{}),
		kickCh:                make(chan string, 4),
		monitorWg:             sync.WaitGroup{},
		peakPnLCache:          make(map[string]float64),
		peakPnLCacheMutex:     sync.RWMutex{},
		lastBalanceSyncTime:   time.Now(),
		userID:                userID,
	}
	// CLASS 33 (2026-09-02) — wire flat-gate leg 4 to the armed_orders ledger.
	// NT8 emits NO working-order frame (audit F12): before this, GetOpenOrders
	// was a stub returning empty, so the leg passed VACUOUSLY at every cutover
	// 35 → 41. Wired HERE (not lazily) so the dead-man watchdog's own
	// GetOpenOrders probe has a source from the first cycle.
	if nt, ok := at.trader.(*ntTrader.TCPTrader); ok {
		nt.SetOpenOrdersSource(at.ledgerOpenOrders)
	}
	return at, nil
}

// Run runs the automatic trading main loop
func (at *AutoTrader) Run() error {
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()

	// B1 (T3): an unmapped primary timeframe is a BOOT FAIL, never a silent
	// 60s default corrupting the bar-close gate / supersession watermark.
	if at.exchange == "ninjatrader" {
		if _, ok := kernel.TFDurationMs(at.primaryTimeframe()); !ok {
			return fmt.Errorf("primary_timeframe %q is not in the timeframe table (kernel/timeframes.go) — refusing to run on a corrupt bar clock", at.primaryTimeframe())
		}
	}
	at.stopMonitorCh = make(chan struct{})
	at.kickCh = make(chan string, 4) // fresh kick channel per Run (restart-safe)
	at.kickPending.Store(false)
	registerPostExitDispatch(at) // Phase 4: close events → one post-exit rescan
	at.startTime = time.Now()

	// P2 (ledger-close 2026-08-19) — restore an owner pause across restart.
	at.loadPersistedPause()
	// E1 — the per-trader ledger boot block (sessions/cutoffs, pause, cadence,
	// roll, balance-alert). The process half prints in main.go.
	at.logLedgerBootBlock(time.Now())

	logger.Info("🚀 AI-driven automatic trading system started")
	at.logInfof("💰 Initial balance: %.2f USDT", at.initialBalance)
	at.logInfof("⚙️  Scan interval: %v", at.config.ScanInterval)
	logger.Info("🤖 AI will make full decisions on leverage, position size, stop loss/take profit, etc.")

	// Pre-launch checks for claw402 users
	at.runPreLaunchChecks()
	at.monitorWg.Add(1)
	defer at.monitorWg.Done()

	// Start drawdown monitoring
	at.startDrawdownMonitor()

	// Start Lighter order sync if using Lighter exchange
	if at.exchange == "lighter" {
		if lighterTrader, ok := at.trader.(*lighter.LighterTraderV2); ok && at.store != nil {
			lighterTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Lighter order+position sync enabled (every 30s)")
		}
	}

	// Start Hyperliquid order sync if using Hyperliquid exchange
	if at.exchange == "hyperliquid" {
		if hyperliquidTrader, ok := at.trader.(*hyperliquid.HyperliquidTrader); ok && at.store != nil {
			hyperliquidTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Hyperliquid order+position sync enabled (every 30s)")
		}
	}

	// Start Bybit order sync if using Bybit exchange
	if at.exchange == "bybit" {
		if bybitTrader, ok := at.trader.(*bybit.BybitTrader); ok && at.store != nil {
			bybitTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Bybit order+position sync enabled (every 30s)")
		}
	}

	// Start OKX order sync if using OKX exchange
	if at.exchange == "okx" {
		if okxTrader, ok := at.trader.(*okx.OKXTrader); ok && at.store != nil {
			okxTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 OKX order+position sync enabled (every 30s)")
		}
	}

	// Start Bitget order sync if using Bitget exchange
	if at.exchange == "bitget" {
		if bitgetTrader, ok := at.trader.(*bitget.BitgetTrader); ok && at.store != nil {
			bitgetTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Bitget order+position sync enabled (every 30s)")
		}
	}

	// Start Aster order sync if using Aster exchange
	if at.exchange == "aster" {
		if asterTrader, ok := at.trader.(*aster.AsterTrader); ok && at.store != nil {
			asterTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Aster order+position sync enabled (every 30s)")
		}
	}

	// Start Binance order sync if using Binance exchange
	if at.exchange == "binance" {
		if binanceTrader, ok := at.trader.(*binance.FuturesTrader); ok && at.store != nil {
			binanceTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Binance order+position sync enabled (every 30s)")
		}
	}

	// Start Gate order sync if using Gate exchange
	if at.exchange == "gate" {
		if gateTrader, ok := at.trader.(*gate.GateTrader); ok && at.store != nil {
			gateTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 Gate order+position sync enabled (every 30s)")
		}
	}

	// Start KuCoin order sync if using KuCoin exchange
	if at.exchange == "kucoin" {
		if kucoinTrader, ok := at.trader.(*kucoin.KuCoinTrader); ok && at.store != nil {
			kucoinTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			at.logInfof("🔄 KuCoin order+position sync enabled (every 30s)")
		}
	}

	// Start NinjaTrader close-sync (TCP transport only). NT closes positions
	// broker-side via the OCO bracket and has no order-sync, so this records
	// SL/TP exits into position history (event-driven off position_close frames).
	if at.exchange == "ninjatrader" {
		if ntTCP, ok := at.trader.(*ntTrader.TCPTrader); ok && at.store != nil {
			// Plan 4 Stage 4 — set parent reference for defer-until-balance guard.
			// TCPTrader will call SetHasReceivedBalance(true) when the first
			// account_balance frame arrives, so the runCycle defer-gate opens.
			ntTCP.SetParentAutoTrader(at)
			ntTCP.StartCloseSync(at.id, at.exchangeID, at.exchange, at.store)
			at.logInfof("🔄 NinjaTrader close-sync enabled (SL/TP exits → position history)")
			// Anchor entry_price to the NT8 position average + clear orphan rows
			// (the 5m-mark entry the AI-decision write records goes stale/frozen).
			ntTCP.StartPositionReconcile(at.id, at.exchangeID, at.exchange, at.store)
			at.logInfof("🔧 NinjaTrader position-reconcile enabled (entry→NT8 avg, orphan clear)")
		}
	}

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// Check if this is a grid trading strategy
	isGridStrategy := at.IsGridStrategy()
	if isGridStrategy {
		at.logInfof("🔲 Grid trading strategy detected, initializing grid...")
		if err := at.InitializeGrid(); err != nil {
			at.logErrorf("❌ Failed to initialize grid: %v", err)
			return fmt.Errorf("grid initialization failed: %w", err)
		}
	}

	// Execute immediately on first run. Under bar-close cadence (P2.1) this runs
	// once on the last CLOSED primary-TF bar and sets the watermark, then the loop
	// idles until the next bar closes; the scan-timer default is unchanged.
	at.tickOnce(isGridStrategy)

	for {
		at.isRunningMutex.RLock()
		running := at.isRunning
		at.isRunningMutex.RUnlock()

		if !running {
			break
		}

		select {
		case <-ticker.C:
			// The loop is single-goroutine: a tick that fires while a cycle is
			// still running WAITS here (the ticker drops missed ticks), so an
			// in-flight AI read is structurally never cancelled by the next
			// tick. Log the overrun so a slow call is visible, not mysterious.
			tickStart := time.Now()
			at.tickOnce(isGridStrategy)
			if d := time.Since(tickStart); d > at.config.ScanInterval {
				at.logWarnf("⏱ cycle overran the scan interval (%v > %v) — next tick delayed, in-flight work never cancelled; intervening ticks skipped",
					d.Round(time.Millisecond), at.config.ScanInterval)
			}
		case reason := <-at.kickCh:
			// Discard-burn 2.1 / post-exit 4.x: a deferred one-shot cycle. Same
			// single-goroutine guarantee as the ticker case — a kick that fires
			// mid-cycle waits here, never cancels in-flight work.
			at.kickPending.Store(false)
			at.cycleTrigger = reason
			at.noteKick(reason)
			at.tickOnce(isGridStrategy)
			at.cycleTrigger = ""
		case <-at.stopMonitorCh:
			at.logInfof("⏹ Stop signal received, exiting automatic trading main loop")
			return nil
		}
	}

	return nil
}

// Stop stops the automatic trading
func (at *AutoTrader) Stop() {
	at.isRunningMutex.Lock()
	if !at.isRunning {
		at.isRunningMutex.Unlock()
		return
	}
	at.isRunning = false
	at.isRunningMutex.Unlock()

	unregisterPostExitDispatch(at) // Phase 4: stop routing close events here
	close(at.stopMonitorCh)        // Notify monitoring goroutine to stop
	at.monitorWg.Wait()            // Wait for monitoring goroutine to finish
	logger.Info("⏹ Automatic trading system stopped")
}

// GetID gets trader ID
func (at *AutoTrader) GetID() string {
	return at.id
}

// GetUnderlyingTrader returns the underlying Trader interface implementation
// This is used by grid trading and other components that need direct exchange access
func (at *AutoTrader) GetUnderlyingTrader() Trader {
	return at.trader
}

// currentAccountName returns the NT sub-account THIS trader is bound to (ITEM 2
// per-account attribution), or "" for crypto traders / an unbound trader.
// Stamped onto equity snapshots, positions, and decision records, and used to
// scope per-account stat reads — so it MUST be the trader's OWN bound account.
//
// G6 fix: reads the trader's boundAccount, NOT server.CurrentAccount(). The
// shared streamed "current" account flaps to whichever account's account_balance
// frame landed last (and, since 88b54e8b, ALL sim accounts stream), so stamping
// it cross-attributed records between two same-symbol traders (15m's SimAccount1
// orders recorded under Sim101, and vice-versa). boundAccount is stable per
// trader and matches the account each order actually executes on.
func (at *AutoTrader) currentAccountName() string {
	if ntTCP, ok := at.trader.(*ntTrader.TCPTrader); ok {
		return ntTCP.BoundAccount()
	}
	return ""
}

// HasReceivedBalance reports whether the NT account_balance frame has arrived (Plan 4 Stage 4).
// Used by the defer-until-balance guard in runCycle to skip cycles until balance is available.
func (at *AutoTrader) HasReceivedBalance() bool {
	at.balanceMutex.RLock()
	defer at.balanceMutex.RUnlock()
	return at.hasReceivedBalance
}

// SetHasReceivedBalance marks that the NT account_balance frame has arrived.
// Called by the TCPTrader when the first balance frame is received.
func (at *AutoTrader) SetHasReceivedBalance(received bool) {
	at.balanceMutex.Lock()
	defer at.balanceMutex.Unlock()
	at.hasReceivedBalance = received
}

// GetName gets trader name
func (at *AutoTrader) GetName() string {
	return at.name
}

// GetAIModel gets AI model
func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

// GetExchange gets exchange
func (at *AutoTrader) GetExchange() string {
	return at.exchange
}

// GetShowInCompetition returns whether trader should be shown in competition
func (at *AutoTrader) GetShowInCompetition() bool {
	return at.showInCompetition
}

// SetShowInCompetition sets whether trader should be shown in competition
func (at *AutoTrader) SetShowInCompetition(show bool) {
	at.showInCompetition = show
}

// GetSystemPromptTemplate gets current system prompt template name (from strategy config)
func (at *AutoTrader) GetSystemPromptTemplate() string {
	if at.strategyEngine != nil {
		config := at.strategyEngine.GetConfig()
		if config.CustomPrompt != "" {
			return "custom"
		}
	}
	return "strategy"
}

// GetCandidateCoins returns the current candidate coin set from the trader's strategy engine.
func (at *AutoTrader) GetCandidateCoins() ([]kernel.CandidateCoin, error) {
	if at.strategyEngine == nil {
		return nil, fmt.Errorf("strategy engine not configured")
	}
	return at.strategyEngine.GetCandidateCoins()
}

// GetStrategyConfig returns the current strategy config used by the trader.
func (at *AutoTrader) GetStrategyConfig() *store.StrategyConfig {
	if at.strategyEngine == nil {
		return at.config.StrategyConfig
	}
	return at.strategyEngine.GetConfig()
}

// GetStore gets data store (for external access to decision records, etc.)
func (at *AutoTrader) GetStore() *store.Store {
	return at.store
}

// calculatePnLPercentage calculates P&L percentage (based on margin, automatically considers leverage)
// Return rate = Unrealized P&L / Margin x 100%
func calculatePnLPercentage(unrealizedPnl, marginUsed float64) float64 {
	if marginUsed > 0 {
		return (unrealizedPnl / marginUsed) * 100
	}
	return 0.0
}

// runPreLaunchChecks performs pre-launch checks for claw402 users (wallet balance, runway estimate)
func (at *AutoTrader) runPreLaunchChecks() {
	if !store.IsClaw402Config(at.config.AIModel) {
		return
	}

	logger.Info("🔍 Running pre-launch checks (claw402)...")

	// Derive wallet address from CustomAPIKey (which is the private key for claw402)
	if at.config.CustomAPIKey != "" {
		// Try to derive address using go-ethereum
		addr := deriveWalletAddress(at.config.CustomAPIKey)
		if addr != "" {
			at.claw402WalletAddr = addr
			logger.Infof("💳 [%s] Claw402 wallet: %s", at.name, addr)

			// Query USDC balance
			balance, err := wallet.QueryUSDCBalance(addr)
			if err != nil {
				logger.Warnf("⚠️ [%s] Could not query USDC balance: %v", at.name, err)
			} else {
				// Estimate runway
				scanMinutes := int(at.config.ScanInterval.Minutes())
				modelName := at.config.CustomModelName
				if modelName == "" {
					modelName = "deepseek"
				}
				dailyCost, runway := store.EstimateRunway(balance, modelName, scanMinutes)
				logger.Infof("💰 [%s] USDC Balance: $%.2f | Daily AI cost: ~$%.2f | Runway: ~%.1f days",
					at.name, balance, dailyCost, runway)

				if balance < 1.0 {
					logger.Warnf("⚠️ [%s] Low USDC balance! Consider topping up.", at.name)
				}
				if balance <= 0 {
					logger.Errorf("🚨 [%s] USDC balance is ZERO — AI calls will fail!", at.name)
				}
			}
		}
	}

	logger.Info("✅ Pre-launch checks complete")
}

// deriveWalletAddress derives an Ethereum address from a hex private key
func deriveWalletAddress(privateKeyHex string) string {
	// Remove 0x prefix if present
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return ""
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	return address.Hex()
}
