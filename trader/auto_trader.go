package trader

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"vl/discipline"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	"vl/mcp"
	_ "vl/mcp/provider"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
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

// ninjaLinkEpoch is the current NT8 connection's accept sequence (1, 2, 3 …
// per accept, per process), read from the same server record the
// installation gate reads. ok=false for a non-NT trader or no server.
func (at *AutoTrader) ninjaLinkEpoch() (uint64, bool) {
	if at.exchange != "ninjatrader" {
		return 0, false
	}
	ntTCP, isNT := at.trader.(*ntTrader.TCPTrader)
	if !isNT {
		return 0, false
	}
	rec, _, _, ok := ntTCP.MaintenanceView()
	if !ok || rec.AcceptSeq == 0 {
		return 0, false
	}
	return rec.AcceptSeq, true
}

// deadManEntriesBlockedNow is the ADMISSION-time dead-man read: the cached
// watchdog state plus a live sample of the NT8 link and connection epoch (S5).
func (at *AutoTrader) deadManEntriesBlockedNow() bool {
	connected, ok := at.ninjaLinkConnected()
	epoch, epochKnown := at.ninjaLinkEpoch()
	return at.deadMan.entriesBlockedLive(connected, ok, epoch, epochKnown)
}

// driveDeadManWatchdog (B5) advances the dead-man watchdog once per cycle from the
// NT8 TCP link. A dropped link blocks NEW entries (open-position management/exits
// are never gated); on reconnect it sweeps any unfilled entries and holds entries
// blocked until a clean positions/orders reconciliation, then auto-resumes. Called
// from runCycle; non-NT traders are a no-op.
func (at *AutoTrader) driveDeadManWatchdog() watchdogEvent {
	connected, ok := at.ninjaLinkConnected()
	if !ok {
		return wdNone
	}
	// Release #22 (EXECUTION F-3): the connection epoch catches a disconnect +
	// reconnect that both fell between two samples.
	epoch, epochKnown := at.ninjaLinkEpoch()
	ev, missed, prev := at.deadMan.stepEpoch(connected, epoch, epochKnown, time.Now().UnixMilli(), at.watchdogReconcileClean)
	switch ev {
	case wdWentDown:
		at.logWarnf("🚨 dead-man watchdog: NT8 TCP link DOWN — NEW entries BLOCKED until a clean reconciliation. Open positions & exits are unaffected.")
	case wdReconnected:
		if missed {
			at.logWarnf("🔌 dead-man watchdog: NT8 TCP link was REPLACED between two samples (accept_seq %d → %d) — a disconnect this trader never saw; treating it as a reconnect: sweeping unfilled entries; entries BLOCKED until a clean positions/orders reconciliation.", prev, epoch)
		} else {
			at.logWarnf("🔌 dead-man watchdog: NT8 TCP link back UP — sweeping unfilled entries; entries stay BLOCKED until a clean positions/orders reconciliation.")
		}
		at.noteFeedLinkUp(time.Now()) // FOLD 3: the FEED DOWN reconnect grace starts here
		at.cancelUnfilledEntriesAfterReconnect()
		// D5 (2026-09-07) — C6: nothing rebuilds the bracket after a reconnect.
		// The AddOn's placedBrackets is in-memory and repopulated from nowhere,
		// so after NT8 restarts our side has forgotten the protections exist.
		// This asks the broker directly. It usually answers UNKNOWN on this
		// edge — the AddOn sends hello, accounts, balances and positions on
		// connect but NO order_snapshot until its next beat — and saying so is
		// the point; the monitor beat re-asks a minute later with a fresh book.
		at.reconcileProtectionAt(time.Now(), "reconnect")
	case wdResumed:
		at.logInfof("✅ dead-man watchdog: clean positions/orders reconciliation — NEW entries RESUMED.")
		at.ackDeadManStuck()
	}
	// W26: escalate a reconcile block that will not clear (order snapshots
	// stopped) — one P0 per episode, entries stay BLOCKED.
	at.deadManEscalate(time.Now())
	return ev
}

// ackDeadManStuck (m420b/B3) clears every open dead_man_reconcile_stuck banner
// for this trader. The episode that raised it is over (watchdog resumed, or the
// trader (re)started with the watchdog not blocked), and AckAllByKind is
// restart-safe: a fresh process has no memory of the old episode's event id.
func (at *AutoTrader) ackDeadManStuck() {
	if at == nil || at.store == nil {
		return
	}
	if _, err := at.store.Alert().AckAllByKind(at.id, "dead_man_reconcile_stuck"); err != nil {
		at.logWarnf("dead-man: could not ack stale reconcile_stuck alerts: %v", err)
	}
}

// deadManEscalate (W26) fires ONE P0 `dead_man_reconcile_stuck` alert per blocked
// episode, after deadManStuckProbeMin consecutive failed reconcile probes or
// deadManStuckAgeMs blocked — whichever comes first. Entries stay BLOCKED (the
// alert is an operator signal, never an auto-unblock). A clean reconcile ends
// the episode; the next block alerts again.
func (at *AutoTrader) deadManEscalate(now time.Time) {
	esc := at.deadMan.escalation()
	if esc.BlockedSinceMs == 0 || esc.Alerted {
		return
	}
	nowMs := now.UnixMilli()
	if esc.FailedProbes < deadManStuckProbeMin && nowMs-esc.BlockedSinceMs < deadManStuckAgeMs {
		return
	}
	at.deadMan.markStuckAlerted()
	episode := fmt.Sprintf("%d", esc.BlockedSinceMs)
	at.emitAlert("P0", "dead_man_reconcile_stuck", "deadman-stuck-"+at.id+"-"+episode,
		"dead-man reconcile stuck — NEW entries blocked",
		fmt.Sprintf("trader %s: entries blocked since %s (%d consecutive failed reconcile probes) — an order snapshot may have stopped arriving; manage by hand",
			at.id, kernel.ClockCT(time.UnixMilli(esc.BlockedSinceMs)), esc.FailedProbes))
}

// HealthDeadMan is the /api/health `dead_man` field for THIS trader (READ
// values): blocked, the current episode's start (0 = not blocked), and the
// consecutive failed reconcile probes. Never a literal.
func (at *AutoTrader) HealthDeadMan() (blocked bool, sinceMs int64, probes int) {
	if at == nil {
		return false, 0, 0
	}
	h := at.deadMan.HealthSnapshot()
	return h.Blocked, h.SinceMs, h.Probes
}

// watchdogReconcileClean is the reconcile probe: the link is considered reconciled
// when it answers BOTH a positions and an open-orders query without error AND
// (N-C, release #22 rescan) a positions snapshot and an order snapshot for this
// trader's account have both been RECEIVED on the current connection — after
// its accept sequence. Two reads that merely did not error can be answered
// entirely from the OLD link's cached frames; with runCycle and monitorTick
// both driving the watchdog the wait could shrink to seconds. Until both
// snapshots land, entries stay BLOCKED and the missing one is named once per
// connection.
func (at *AutoTrader) watchdogReconcileClean() bool {
	if _, err := at.trader.GetPositions(); err != nil {
		return false
	}
	if _, err := at.trader.GetOpenOrders(""); err != nil {
		return false
	}
	if nt, ok := at.trader.(*ntTrader.TCPTrader); ok {
		pos, ord, seq := nt.SnapshotsSinceAccept()
		if !pos || !ord {
			yn := func(b bool) string {
				if b {
					return "yes"
				}
				return "NO"
			}
			state := fmt.Sprintf("%d|%t|%t", seq, pos, ord)
			if prev, _ := deadManAwaitLogged.Swap(at.id, state); prev != state {
				deadManAwaitCount.Add(1)
				at.logWarnf("🔌 dead-man watchdog: NOT resuming yet — on the new connection (accept_seq %d) positions snapshot received: %s · order snapshot received: %s; NEW entries stay BLOCKED until both arrive.",
					seq, yn(pos), yn(ord))
			}
			return false
		}
		deadManAwaitLogged.Delete(at.id)
	}
	return true
}

// deadManAwaitLogged dedupes the "NOT resuming yet" line per trader to one per
// (accept_seq, positions, orders) state; deadManAwaitCount counts those lines.
var (
	deadManAwaitLogged sync.Map // trader id → state string
	deadManAwaitCount  atomic.Int64
)

// cancelUnfilledEntriesAfterReconnect reports the resting entry orders the
// ledger shows working when the link comes back. N-C (release #22 rescan): the
// old text said entries "are MARKET orders … no-op" and that cancellation was a
// "Part A AddOn capability". Both are false now — armed entries rest as limits
// / stops, and cancel_order exists (the withdraw path and #508 use it).
//
// WHETHER to cancel resting entries on a reconnect is an OWNER DECISION that
// this release does not take: they are LEFT resting (unchanged behaviour) and
// NAMED, so the owner sees exactly what is live while NEW entries are blocked.
func (at *AutoTrader) cancelUnfilledEntriesAfterReconnect() {
	orders, err := at.trader.GetOpenOrders("")
	if err != nil {
		at.logWarnf("🔌 dead-man watchdog: post-reconnect working-order read FAILED: %v — resting entries UNKNOWN; NEW entries stay BLOCKED.", err)
		return
	}
	if len(orders) == 0 {
		return
	}
	names := make([]string, 0, len(orders))
	for _, o := range orders {
		names = append(names, fmt.Sprintf("%s %s %s@%.2f", shortID(o.OrderID), o.PositionSide, o.Type, firstNonZero(o.Price, o.StopPrice)))
	}
	at.logWarnf("🔌 dead-man watchdog: %d working entry order(s) visible post-reconnect %v — LEFT RESTING (cancelling resting entries on a reconnect is an owner decision; cancel_order is available). NEW entries stay BLOCKED until the new connection's positions + order snapshots arrive.",
		len(orders), names)
}

// firstNonZero returns a when it is non-zero, else b.
func firstNonZero(a, b float64) float64 {
	if a != 0 {
		return a
	}
	return b
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
	// N-G (release #22 rescan): judge a SENT move on the first fresh book —
	// before the trigger check, so a position that fell back under the
	// trigger still gets its verdict.
	at.verifyPlannerBEMove(symbol+"_"+side, time.Now())
	fire, pts := at.breakevenTriggerFor(symbol, side, entryPrice, markPrice) // release #25 PK-5 (R=0 → the points rule)
	if !fire {
		return
	}
	// FIX-AUTOBE-OFF-IN-MENTOR (owner ruling 2026-10-09): with mentor mode ON,
	// a mentor trade follows ONLY the mentor's exits — the AI-era auto-BE must
	// not move a mentor-owned stop. Log ONCE per position + a visible counter.
	// Checked before the 0B suspension: mentor trades bypass the 0B seam, so the
	// mentor gate must be the first thing that decides this is a mentor trade.
	// PEP MODE: the same rule for pep (modeOwnsExits = mentor or pep) - pep's
	// stop and target are fixed at the wire; nothing moves them.
	if at.modeOwnsExits() {
		at.autoBEMentorBlockedOnce(symbol, side, pts)
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
	// P0 stop-move-wrong-side (2026-10-21): a BE stop through the market would
	// be refused by NT8 or trigger immediately — close at market instead, never
	// send. A stale price keeps today's behaviour (send).
	if price, fresh := mentorLatestPriceSeam(at); fresh && mentorStopMoveWrongSide(side, entryPrice, price) {
		at.mentorCount("stop_move_through_market_close")
		at.logWarnf("⚠️ auto-breakeven: %s %s stop would move to entry %.2f but the market is already through it (price %.2f) — closing at market instead (= the BE exit)",
			symbol, side, entryPrice, price)
		if err := mentorCloseLegAtMarketWire(at, ntTCP, side, 0); err != nil {
			at.logWarnf("⚠️ auto-breakeven: close-at-market failed for %s %s: %v", symbol, side, err)
			// m198 #43-C: the close never went out — retry the BE next cycle,
			// exactly like the moveStopWire failure branch below. Leaving
			// breakevenDone=true here silently discarded the BE for the life
			// of the position (pruned only when flat).
			at.breakevenMu.Lock()
			at.breakevenDone[key] = false
			at.breakevenMu.Unlock()
			return
		}
		// Idempotent: the position is closing, never re-offer BE for it.
		return
	}
	if err := moveStopWire(ntTCP, side, entryPrice); err != nil {
		at.logWarnf("⚠️ auto-breakeven: move-stop send failed for %s %s: %v", symbol, side, err)
		at.breakevenMu.Lock()
		at.breakevenDone[key] = false // let it retry next cycle
		at.breakevenMu.Unlock()
		return
	}
	// WARN (honest-logs 2026-08-19): a stop amendment is an owner-visible event —
	// WARN reaches the log_events DB sink + dashboard even when journald's
	// frame-flood suppression is dropping INFO lines (the "breakeven not
	// moving" false alarm was exactly this line being invisible).
	// N-G: at.logWarnf carries the trader tag (the 10:08:27 line had none),
	// and SENT is not CONFIRMED — the next fresh book judges it.
	at.logWarnf("🎯 auto-breakeven: %s %s +%.1f pts in profit → stop moved to breakeven (entry %.2f) — SENT; NT8's verdict follows on the next fresh book",
		symbol, side, pts, entryPrice)
	at.recordPlannerBEPending(ntTCP, symbol, side, key, entryPrice, time.Now())
}

// autoBEMentorBlockedCount is the session counter for "auto-BE not applied
// because mentor mode is ON" (owner ruling 2026-10-09). Visible to tests and
// reports via AutoBEMentorBlockedCount.
var autoBEMentorBlockedCount atomic.Int64

// AutoBEMentorBlockedCount returns the session count of auto-BE applications
// refused because the trader runs mentor mode ON.
func AutoBEMentorBlockedCount() int64 { return autoBEMentorBlockedCount.Load() }

// resetAutoBEMentorBlockedCountForTest clears the process-wide counter.
func resetAutoBEMentorBlockedCountForTest() { autoBEMentorBlockedCount.Store(0) }

// autoBEMentorBlockedOnce logs ONCE per position that the AI-era auto-BE was
// not applied because mentor mode is ON (the mentor's exits own the stop), and
// bumps the visible counter. Idempotent per "symbol_side" via autoBEMentorBlocked.
func (at *AutoTrader) autoBEMentorBlockedOnce(symbol, side string, pts float64) {
	key := symbol + "_" + side
	at.breakevenMu.Lock()
	if at.autoBEMentorBlocked == nil {
		at.autoBEMentorBlocked = make(map[string]bool)
	}
	if at.autoBEMentorBlocked[key] {
		at.breakevenMu.Unlock()
		return
	}
	at.autoBEMentorBlocked[key] = true
	at.breakevenMu.Unlock()
	autoBEMentorBlockedCount.Add(1)
	// WARN (same visibility rule as the success line): an owner-visible event
	// must reach the log_events DB sink + dashboard even when INFO is flooded.
	mode, owner := at.exitOwnerWords()
	logger.Warnf("🎯 auto-breakeven: not applied — %s ON (%s the stop) [%s %s +%.1f pts]",
		mode, owner, symbol, side, pts)
}

// drawdownMentorBlockedCount is the session counter for "drawdown emergency close
// not applied because mentor mode is ON" (owner ruling 2026-10-09). Visible to
// tests and reports via DrawdownMentorBlockedCount.
var drawdownMentorBlockedCount atomic.Int64

// DrawdownMentorBlockedCount returns the session count of drawdown emergency
// closes refused because the trader runs mentor mode ON.
func DrawdownMentorBlockedCount() int64 { return drawdownMentorBlockedCount.Load() }

// resetDrawdownMentorBlockedCountForTest clears the process-wide counter.
func resetDrawdownMentorBlockedCountForTest() { drawdownMentorBlockedCount.Store(0) }

// drawdownMentorBlockedOnce logs ONCE per position that the AI-era drawdown
// emergency close was not applied because mentor mode is ON (the mentor's exits
// own the trade), and bumps the visible counter. Idempotent per "symbol_side"
// via drawdownMentorBlocked.
func (at *AutoTrader) drawdownMentorBlockedOnce(symbol, side string, profitPct, peakPct, drawdownPct float64) {
	key := symbol + "_" + side
	at.breakevenMu.Lock()
	if at.drawdownMentorBlocked == nil {
		at.drawdownMentorBlocked = make(map[string]bool)
	}
	if at.drawdownMentorBlocked[key] {
		at.breakevenMu.Unlock()
		return
	}
	at.drawdownMentorBlocked[key] = true
	at.breakevenMu.Unlock()
	drawdownMentorBlockedCount.Add(1)
	mode, owner := at.exitOwnerWords()
	logger.Warnf("🎯 drawdown emergency close: not applied — %s ON (%s the trade) [%s %s profit %.2f%% peak %.2f%% drawdown %.2f%%]",
		mode, owner, symbol, side, profitPct, peakPct, drawdownPct)
}

// defaultBreakevenTriggerPoints is the shipped auto-breakeven trigger (points in
// profit) when the strategy leaves breakeven_trigger_points unset or ≤ 0.
const defaultBreakevenTriggerPoints = 50.0

// breakevenTriggerPoints is the ONE resolution of the breakeven trigger (W1 (g):
// breakevenTrigger and the Settings page both read it).
func breakevenTriggerPoints(rc store.RiskControlConfig) float64 {
	trigger := rc.BreakevenTriggerPoints
	if trigger <= 0 {
		trigger = defaultBreakevenTriggerPoints
	}
	return trigger
}

// breakevenTrigger is the pure decision for auto-breakeven: given the strategy's
// breakeven config and a position's side/entry/mark, it returns whether the stop
// should move to breakeven now and the current points in profit. Default trigger
// is 50 points when unset. Testable without any broker/NT8 dependency.
func breakevenTrigger(rc store.RiskControlConfig, side string, entry, mark float64) (bool, float64) {
	if !hlBool(rc.BreakevenEnabled, false) {
		return false, 0
	}
	trigger := breakevenTriggerPoints(rc)
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
	for k := range at.autoBEMentorBlocked {
		if !openKeys[k] {
			delete(at.autoBEMentorBlocked, k)
		}
	}
	for k := range at.drawdownMentorBlocked {
		if !openKeys[k] {
			delete(at.drawdownMentorBlocked, k)
		}
	}
	at.prunePlannerBEPending(openKeys) // N-G: a flat position has no verdict to wait for
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
	Exchange   string // Exchange type: "ninjatrader" (futures-only build)
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
	NinjaTraderDataDir string // /mnt/c/Users/<u>/VLTrader/data
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
	exchange          string // Trading platform type ("ninjatrader")
	exchangeID        string // Exchange account UUID
	showInCompetition bool   // Whether to show in competition page
	config            AutoTraderConfig
	trader            Trader // Use Trader interface (supports multiple platforms)
	mcpClient         mcp.AIClient
	store             *store.Store           // Data storage (decision records, etc.)
	strategyEngine    *kernel.StrategyEngine // Strategy engine (uses strategy configuration)
	cycleNumber       int                    // Current cycle number
	cycleMu           sync.Mutex             // guards cycleNumber's increment in saveDecision (PEP: two goroutines save decision rows)
	initialBalance    float64
	dailyPnL          float64
	// F3 fast-market wake reads (waterfall-class wave, 2026-08-28): the price
	// at the last successful plan write + a one-shot flag for the next read.
	lastPlanWritePrice atomic.Uint64 // math.Float64bits

	// MENTOR P3 — evaluator instance + one-tick-per-1m-close dedup. Both are
	// only consulted when the per-strategy mentor_mode is ON.
	mentorEval         *mentor.Evaluator
	mentorLastTickOpen int64
	// mentorSeedHistGen (release #23 N1) is the MNQ 1m history-refill
	// generation this evaluator's state was last seeded at (mentor_reseed.go).
	mentorSeedHistGen uint64
	// mentorChartSeedGen (m427/2) is the refill generation the DISPLAY-ONLY chart
	// seed was last built at. Separate from mentorSeedHistGen on purpose: the
	// closed-market chart rebuild must never claim the generation the first real
	// Tick's state re-seed (mentorReseedOnRefill) is waiting on. Touched only
	// under mentorEvalMu (and once at seed time).
	mentorChartSeedGen uint64
	// mentorDayOffClearLogged is the DayRecheck.ClearedAt value last WARN'd +
	// counted. The recheck clears at most once per trading day, so the value
	// is a stable per-process dedup key. Read/written only inside
	// mentorEvalOnce under mentorEvalMu.
	mentorDayOffClearLogged int64
	// MENTOR-NY item 5: the ORB lines already logged — the draw once per CT
	// day, the escape on its first appearance and on every side change (the
	// evaluator owns the ORB; these only dedup the log, never decide).
	mentorORBDrawLogged int64
	mentorORBEscDay     int64
	mentorORBEscSide    string
	// mentorDayGateReportLogged is the DayGateReport.Key last INFO-logged — the
	// once-per-day day-gate line. Read/written only inside mentorEvalOnce
	// under mentorEvalMu.
	mentorDayGateReportLogged string
	// mentorEvalMu serializes the evaluator: the scan loop (mentorTick) and the
	// event loop (mentorEventPassAt) both call mentorEvalOnce, whose Tick
	// mutates the evaluator's maps — N11 (DS-104). One mutex, both callers.
	mentorEvalMu       sync.Mutex
	mentorFinalArrival atomic.Int64 // ms — when the FINAL frame hit the sink
	// MENTOR B20 — the chosen exit branch per open position (keyed by side:
	// "long"/"short"), set at placement from the entry-time fork (A/B/C/swing)
	// and switched to C by a confluence upgrade. The P1 exit loop drives the
	// branch. Size is never touched by the upgrade.
	mentorExitMu    sync.Mutex
	mentorExitModes map[string]string
	// mentorNeverAddCancelDone is the B3 never-add latch: the never-add cancel
	// fires once per open-position episode and re-arms when the account is flat
	// again. Read/written ONLY inside mentorDayStopSweep (serialized under
	// mentorEvalMu).
	mentorNeverAddCancelDone bool
	// mentorLivePos is the exit-drive's input: every FILLED mentor position,
	// keyed by its entry signal id, registered at the fill callback and driven
	// by the exit-drive loop (DS-107). Guarded by mentorExitMu.
	mentorLivePos map[string]*mentorLivePos
	// mentorFlatSignals tombstones signal ids the exit drive unregistered as
	// FLAT — a stale partial-fill sweep (B1 expiry / I4 cancel, re-run every bar
	// while the row is cancel_pending) must not resurrect them. Guarded by
	// mentorExitMu. Cleared by a full fill (the position is provably open) and
	// by the cancel-settlement forget (the row is terminal).
	mentorFlatSignals map[string]struct{}
	// REL17-F2 restart rebuild: one boot-time pass that rebuilds open mentor
	// positions from the broker book before the first exit-drive pass. The
	// pass retries on every candle until it has run with a FRESH book, then
	// mentorRebuildDone latches true. Guarded by mentorRebuildMu.
	mentorRebuildMu              sync.Mutex
	mentorRebuildDone            bool
	mentorRebuildPositionsWarned bool
	// REL18-A: the per-trader arm epoch. Each construction (boot/reload) gets
	// its own epoch so a rebuilt evaluator's "isb-1" never collides with a
	// prior terminal row — and never with ANOTHER trader's row (the ledger key
	// carries the trader too). Read once per placement, never shared.
	mentorArmEpoch int64
	// mentorIntentTap is a READ-ONLY test observer (FULL-DAY-REPLAY): when set,
	// mentorEvalOnce hands it every intent the evaluator emitted this tick,
	// before any dispatch. nil in production (never assigned outside tests) —
	// the tick is byte-identical.
	mentorIntentTap func(in mentor.Intent)
	// mentorEvalIntentsSeam (test-only, nil in production) replaces the
	// evaluator's Tick OUTPUT for one tick: the m134 (a) negative pin feeds a
	// raw synthetic ISB intent through the PRODUCTION mentorEvalOnce dispatch /
	// refusal path without building a full kernel ISB state. nil in production —
	// the tick is byte-identical.
	mentorEvalIntentsSeam func(bars []market.Kline) []mentor.Intent
	// mentorPendingStopMoves is the F1 stop-move TRUTH registry: one entry per
	// (signal, wire leg) stop move that has been SENT but not yet VERIFIED
	// against the broker's order book. Keyed "<signalID>#<wireLeg>". Guarded by
	// mentorExitMu.
	mentorPendingStopMoves map[string]*mentorPendingStopMove
	// mentorSwingExitSent (release #54, #15): per swing ArmID, when its hold-close
	// marketable limit was sent — guarded by mentorExitMu.
	mentorSwingExitSent map[string]mentorSwingExitRecord
	// mentorStopMoveEpisodes is the F1 miss-count ledger that SURVIVES a
	// superseding move: one entry per (signal, wire leg), keyed the same as the
	// pending map, so a fresh drive move every candle does NOT reset the count.
	// Reset only when a verdict CONFIRMS (or a tighter broker stop is adopted).
	// Guarded by mentorExitMu.
	mentorStopMoveEpisodes map[string]*mentorStopMoveEpisode
	// mentorStopMoveManual is the F1 manual-management latch: once a leg misses
	// twice the drive sends nothing more for it (belief = broker truth) until a
	// snapshot shows broker == belief again or the position is gone. Keyed
	// "<signalID>#<wireLeg>". Guarded by mentorExitMu.
	mentorStopMoveManual map[string]*mentorStopMoveManual
	// mentorFillCh carries REAL mentor fill receipts from the fill callback
	// (the executor goroutine) to the evaluator (mentorEvalOnce, under
	// mentorEvalMu) for FU-1: G1/G2 fed from real fills, never a
	// cross-goroutine evaluator mutation. Bounded; a full channel drops the
	// receipt (counted).
	mentorFillCh chan mentorFillReceipt
	// mentorExitCh carries broker position_close receipts (the signal id + the
	// exit reason) from the close-sync goroutine to the evaluator drain, so a
	// real exit closes the matching Limits.openTrade under mentorEvalMu (#47).
	// Bounded; a full channel drops the receipt (counted).
	mentorExitCh chan mentorExitReceipt
	// mentorDeferredFills holds FU-1 receipts whose row fallback needs
	// State.Levels (R1: the first post-restart Tick has not set them yet).
	// Only the drain, under mentorEvalMu, touches it.
	mentorDeferredFills []mentorFillReceipt
	// mentorFunnel is the N12 visibility counter (read-only): one INFO line per
	// 15 minutes + on change, session-day scoped (17:00 CT). Never gates a trade.
	mentorFunnel mentorFunnel
	// REL18-B (two mentor traders in one process): the mentor seams, counters
	// and seed depths are PER-TRADER. The old package-level vars captured the
	// last-constructed trader and the global counter map summed both traders'
	// funnels / rebuild line. Per trader, no fallback.
	mentorOpenSideSource       func() string
	mentorLegProtectedSource   func(leg string) bool
	mentorDayNetSource         func() (float64, bool)
	mentorClosedProfitSource   func() (bool, bool)
	mentorClosedLossSource     func() (bool, bool)
	mentorStopAfterLossTripped func() (bool, string)
	mentorSourceDepthSource    func(name string) (int, bool)
	mentorSeedDepthsMu         sync.RWMutex
	mentorSeedDepths           map[string]int
	mentorCounterMu            sync.Mutex
	mentorCounters             map[string]int
	// mentorStaleDataBlock (release #11): the episode state of the mentor
	// stale-data gate. mentorStaleMu guards it — the authoring gate runs under
	// mentorEvalMu but the placement pass (armed executor goroutine) does not,
	// so the episode flag gets its own mutex (never a data race).
	mentorStaleMu      sync.Mutex
	mentorStaleEpisode bool
	// integrity is the DATA-INTEGRITY pause episode (m445, data_integrity_gate.go).
	integrity integrityEpisode
	// seedSeries is what the mentor seed's 1m series was MEASURED to hold: the
	// RTH holes in the stitched history the key levels were walked over
	// (data_integrity_seed.go). Set at the seed and every re-seed.
	seedSeries seedSeriesIntegrity
	// mentorStaleSince is when the current episode began, on the gate's own
	// clock (the seam's now — time.Now() in production, so the END line's
	// length is a monotonic difference; OPS NEW-8, class 113).
	mentorStaleSince time.Time
	mentorStaleWhy   string
	// feedLinkUpAt is when this trader last saw the NT8 link come (back) UP
	// (dead-man wdReconnected): with startTime, the anchor of the FEED DOWN
	// reconnect grace (FOLD 3, 10-14). feedGraceSaid is the anchor whose ⏳
	// line was already said. Guarded by feedGraceMu.
	feedGraceMu   sync.Mutex
	feedLinkUpAt  time.Time
	feedGraceSaid time.Time
	// feedLagMu guards the FEED-LAG alarm episode (m134 DS-108 (c)): the newest
	// NT8 bar age sitting in the soft band just under FEED DOWN (feedDownAfter()
	// ∓ 100 s) for > 3 min is LOUD — a WARN plus a dashboard line — the early
	// warning before the hard FEED DOWN fires.
	// feedEpisodeMu guards feedDownEpisode: the event id of the FEED DOWN episode
	// whose alert this trader raised (m432). Set when it fires, cleared (with ONE
	// "feed back" owner message) when a fresh bar returns, cleared silently when
	// CME closes — so the owner is told down once and back once per episode.
	feedEpisodeMu    sync.Mutex
	feedDownEpisode  string
	feedLagMu        sync.Mutex
	feedLagEnteredAt time.Time // zero = outside the band
	feedLagSaid      bool      // the WARN already fired for this episode
	feedLagActive    bool      // the band is active right now (dashboard line)
	// mentorInvalidLog is the item-18 noise gate (DS-105 replay): the last INFO
	// emit instant + the suppressed count per LevelKey, so a busy level's
	// level_invalid logs at most once per 15 minutes while the counter still
	// fires on every event. Guarded by mentorInvalidLogMu.
	mentorInvalidLogMu sync.Mutex
	mentorInvalidLog   map[string]mentorInvalidLogState
	fastTapePending    atomic.Bool
	// lastClockHealthSession: which session the last clock-health line was
	// logged for (PHASE 3.5) — one line per session roll, not per tick.
	lastClockHealthSession string
	lastResetTime          time.Time
	pauseUntilMs           atomic.Int64 // P2 stop_until producer state (unix ms; 0 = not paused) — see auto_trader_pause.go
	pauseStoreMu           sync.Mutex   // E7-v2: orders memory-vs-store pause writes (expiry CAS vs concurrent re-pause)
	lastRollWarnContract   string       // P3 roll gate: dedupes the unresolved-contract WARN per contract-string change
	// onceMu guards the dedupe-once fields read and written through firstFor
	// (lastRollWarnContract, lastCalFailClosedAlert, lastT1NoCurrencyWarn,
	// lastClockWidenLog): W-EXEC-TRUTH W0 (Q15) — the admission gate reaches
	// them from Picture's live-bar goroutine as well as from runCycle.
	onceMu sync.Mutex
	// admitLast dedupes arm/picture admission refusals per (path, key) —
	// W-EXEC-TRUTH W0 (a): a 2-minute pass or a live-bar frame is not a new
	// event. Locked (Picture runs on the live-bar goroutine).
	admitLast admitDedupe
	// cancelConfirmMu serializes confirmPendingCancels between the armed pass and
	// the withdraw beat (W-EXEC-TRUTH W0 (f)).
	cancelConfirmMu    sync.Mutex
	lastHalfDaySeedDay string // P4 half-days producer: once-per-CME-session-day throttle
	lastCycleBarSig    string // P10.4 no-new-data dedup: newest primary-TF bar signature at last cycle
	// entrySendMu is the ONE entry-send section (W1b FOLD-10): it spans an
	// entry's bracket set → its open (and, separately, the post-open set), on
	// the AI decision's cycle goroutine and the chat door's HTTP goroutine
	// alike — the broker's (symbol, side) SL/TP maps are set-then-read, and a
	// foreign set between them sent one entry on another's bracket. Taken only
	// in openEntryWithRecord, via lockEntrySend.
	entrySendMu sync.Mutex

	// Two-picture mode (W-PICTURE-HTF): the deterministic evaluator, lazily
	// built from the strategy knobs and rebuilt when they change.
	pictureHtfMu       sync.Mutex
	pictureHtfSig      string
	pictureHtf         *PictureHtfEvaluator
	ai402OutageStartMs atomic.Int64 // P5 402-outage latch (0 = no outage) — one banner per outage; atomic (F5: planner/watcher clear from other goroutines)
	// G4 (regime wave 2026-08-21) — transition stand-down state + the G4.6 MSS
	// wake dedupe key (plan:version:eventInstant — one planner wake per MSS).
	transition           kernel.TransitionState
	transitionClosedAtMs int64 // G4: last closed trigger — the same event must not reopen the stand-down
	lastMSSWakeKey       string
	// W6 (2026-08-25) — level-event wake state: the dedupe key
	// (plan:version:kind:label:tier:birth) and the SHARED planner-wake clock
	// (deaths don't reset it; MSS + level wakes do, so the two wake classes
	// can never double-fire inside one wake_min_interval_min window).
	lastLevelWakeKey  string
	lastPlannerWakeAt time.Time
	// wakeStateMu guards the W6 wake-state fields (lastLevelWakeKey,
	// lastMSSWakeKey, lastPlannerWakeAt): the W26 F-1 re-fire goroutine and the
	// per-trader monitor loop can both touch them, so every access holds it.
	wakeStateMu sync.Mutex
	// flipRereadLaunchAt (W-FLIP-REREAD-IMMEDIATE) — per plan|version, when a
	// structure_flip read last LAUNCHED and wrote nothing; the flip read's
	// self-backoff clock (see maybeRereadAfterFlip). Per trader on purpose:
	// a process-global map keyed by plan id let one trader's (or one test's)
	// failed launch hold another's retry.
	flipRereadLaunchAt sync.Map
	lastAIBalanceDay   string // P5 daily balance poll throttle (AI_BALANCE_WARN)
	isRunning          bool
	isRunningMutex     sync.RWMutex // Mutex to protect isRunning flag
	// statusMu guards callCount and lastResetTime, written by runCycle and
	// read by GetStatus on another goroutine (release #24: the run-owner
	// tests made that read concurrent, and -race named it).
	statusMu sync.Mutex
	// MENTOR-NY item 3 — retired: removed from the roster (RemoveTrader); its
	// Run never starts. Guarded by runOwners' mutex (run_owner.go).
	retired bool
	// stopGen counts Stops (bumped under isRunningMutex on EVERY Stop, W26 N4:
	// a Stop on a not-running instance must cancel an armed run retry). Run
	// reads it at its CAS: a Stop that lands while Run is still starting is
	// seen before the loop, and Run tears down what it started instead of
	// entering the loop.
	stopGen uint64
	// DS-104 P2-2 (run_owner.go armRunRetry): a retry is armed after a
	// handoff timeout; the retry claims only a FREE trader id.
	runRetryArmed   atomic.Bool
	retryIfFreeOnly atomic.Bool
	// runReadyMu/runReadyCh is the REL18-C FOLD handshake between Run and the
	// start handler: Run reports nil once it reaches the ordered-execution
	// install (ready) or the O2 refusal error, so the handler can answer 409
	// instead of "Trader started". Buffered cap 1; a boot start (no reader)
	// never blocks.
	runReadyMu          sync.Mutex
	runReadyCh          chan error
	limitFlattenMu      sync.Mutex // W117-F F6: serializes delayed exits with Stop
	limitFlattenStopped bool
	limitFlattens       map[int64]*pendingLimitFlatten
	// pictureGen (W4/D25) opens on every registerPictureHtf and closes on
	// unregisterPictureHtf. An evaluation records the generation it began
	// under and re-checks it immediately before the wire, so a frame in
	// flight across a Stop or a restart cannot send for a trader that is
	// gone. Atomic: read from the live-bar goroutine, written on Run/Stop.
	pictureGen            atomic.Int64
	startTime             time.Time        // System start time
	callCount             int              // AI call count
	positionFirstSeenTime map[string]int64 // Position first seen time (symbol_side -> timestamp in milliseconds)
	pep                   pepRuntime       // PEP MODE: this trader's pep working memory (pep_mode.go); the one-trade-per-day record is the database's
	positionMentorOwned   map[string]bool  // MENTOR P3: symbol_side -> the open position was opened by a mentor-sourced decision (the mentor driver owns its exits)
	stopMonitorMu         sync.Mutex       // guards stopMonitorCtx/stopMonitorCancel (per-Run)
	stopMonitorCtx        context.Context
	stopMonitorCancel     context.CancelFunc
	monitorWg             sync.WaitGroup        // Used to wait for monitoring goroutine to finish
	kickCh                chan string           // discard-burn/post-exit: one-shot deferred-cycle kicks into the run loop (reason payload)
	kickPending           atomic.Bool           // at most one kick armed at a time (CAS)
	skipDodgeOnce         bool                  // a dodge-kicked cycle must not re-dodge at the boundary (run-loop goroutine only)
	skipCadenceOnce       bool                  // U2: a post_exit kick bypasses the cadence gates exactly once (run-loop goroutine only)
	lastTickClosedSkip    bool                  // E5: this cycle took the closed-market backoff, so its duration is deliberate, not a fault (run-loop goroutine only)
	cycleTrigger          string                // why this cycle fired: "" (timer) | "stale_dodge" | "post_exit" (run-loop goroutine only)
	aiCallMs              [aiCallRingSize]int64 // last-N AI call durations (run-loop goroutine only)
	aiCallIdx             int
	aiCallN               int
	peakPnLCache          map[string]float64     // Peak profit cache (symbol -> peak P&L percentage)
	peakPnLCacheMutex     sync.RWMutex           // Cache read-write lock
	breakevenDone         map[string]bool        // auto-breakeven: "symbol_side" already moved to breakeven (idempotent; reset on flat)
	autoBEMentorBlocked   map[string]bool        // auto-breakeven: "symbol_side" already logged "not applied — mentor mode ON" (idempotent; reset on flat)
	drawdownMentorBlocked map[string]bool        // drawdown emergency close: "symbol_side" already logged "not applied — mentor mode ON" (idempotent; reset on flat)
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
	consecutiveAIFailures int             // Consecutive AI call failures
	safeMode              bool            // Safe mode: no new positions, protect existing ones
	safeModeReason        string          // Why safe mode was activated
	deadMan               deadManWatchdog // B5 dead-man watchdog: NT8 link-gap → block NEW entries until reconciled (zero value = live/allowed; driven from runCycle AND monitorTick, step serialized by its own mutex)

	// armedSyncSeam — fixture seam for the synchronous armed cancel (S-list
	// closer FIX1): wire cancel + ack stream + timeout. nil = production TCP
	// path (armedTrader().CancelOrder + the shared order_update subscription).
	armedSyncSeam *armedSyncSeam

	// armRefusalLast dedupes the every-cycle arm REFUSED log (F4, LONDON-
	// FORENSICS 2026-08-28): one line per (plan, version, scenario, verdict);
	// silent until the spec or the refusal reason changes. Run-loop goroutine
	// only — no lock.
	armRefusalLast map[string]string

	// m447 - the data-truth broker_agreement check's per-trader first-seen
	// tracker (trader/broker_agreement.go).
	agreementOnce sync.Once
	agreementTk   *agreementTracker
	// 0B — per-arm stop-composition log dedup (plan:version:scenario:leg:stop).
	armStopCompLast map[string]string
	// oneSetupFactsForTest (dispatch 102) is a TEST SEAM ONLY: when non-nil it
	// supplies the map, price, band and per-scenario permission the one-setup
	// call site would otherwise assemble from the live tape, so the seam tests
	// can drive the REAL arm path with a known map. Nil in production
	// (TestOneSetupTestSeamIsNilInProduction pins it).
	oneSetupFactsForTest func(now time.Time) oneSetupTestFacts
	// armAuthoredLast dedupes the every-cycle "⚔️ armed" log (PRE-REOPEN F3,
	// 2026-08-28) — the dead-row re-log spam (69+ lines/day) fired on every
	// placement beat because the ledger row already existed. Log once per
	// (plan:version:scenario) spec, again only when the prices change.
	armAuthoredLast map[string]string
	// windowSweepSentMs (W1b FOLD-12) — row id → pass-clock ms of the window
	// sweep's last cancel request for that row; the sweep's pace
	// (window_sweep_pace.go). Pass state: armedPassMu, like the maps above.
	windowSweepSentMs map[int64]int64

	// W-EXEC-TRUTH W3 D14 — ONE armed pass at a time per trader. The scan
	// (runCycle → maybeManageArmedOrdersAt), the live-bar event pass and the
	// strict nudge all take it; the maps above are pass state and are safe
	// only because every writer holds this lock. Never re-entered.
	armedPassMu sync.Mutex
	// zoneArmActive — the ACTIVE plan doc has an enabled market_in_zone arm
	// (cached by every pass); the live-bar sink kicks the event pass only
	// while it is true. zoneWatch holds []zoneWatch — the armed policy rows'
	// zones and last verdicts — so the sink can see a verdict CHANGE.
	zoneArmActive atomic.Bool
	zoneWatch     atomic.Value
	// armedEvent is the per-trader event-pass loop (started in Run, stopped
	// in Stop); nil while the trader is not running.
	armedEvent atomic.Pointer[armedEventLoop]

	// W117 F2 (R5) — the ordered-execution unregister closure, installed at
	// Run (installNTOrderedExecutions) and called on Stop. Guarded so a Stop
	// racing the install can never drop a live registration.
	orderedExecMu    sync.Mutex
	orderedExecUnreg func()
	// armedEventNowForTest is a TEST SEAM ONLY (nil in production): the event
	// loop's clock, so a fixture-time plan can be driven through the REAL
	// goroutine. TestArmedEventClockSeamIsNilInProduction pins it.
	armedEventNowForTest func() time.Time

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
	// R22 P1-B — the calendar date whose early-close retirement completed on
	// the closed path (runCycle goroutine only, like cmePrevOpen).
	earlyCloseRetiredDate string
	earlyCloseNotedDate   string // the closed-path retirement line is logged once per date

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
	// outage; a stored slice short-circuits it). Persisted ACROSS reconstruction
	// by the package-level calFetchThrottle (keyed by trader id), not this struct.
	// P0.6 (2026-08-19) — calendar fail-closed alert, once per trade date.
	lastCalFailClosedAlert string
	// CAL-EMPTY-DAY — "fetched — no events scheduled" line, once per trade
	// date, so a stored [] slice is visibly distinct from an ABSENT slice.
	lastCalEmptyDate string
	// F6 (2026-08-30) — clock-hold T1-widening warn line, once per trade date.
	lastClockWidenLog string
	// W-T1-CURRENCIES (2026-09-18) — "T1 event without currency treated as
	// hard" warn line, once per trade date.
	lastT1NoCurrencyWarn string
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

	mcpClient.SetAPIKey(apiKey, customURL, config.CustomModelName)
	logger.Infof("🤖 [%s] Using %s AI", config.Name, aiModel)

	if config.CustomAPIURL != "" || config.CustomModelName != "" {
		logger.Infof("🔧 [%s] Custom config - URL: %s, Model: %s", config.Name, config.CustomAPIURL, config.CustomModelName)
	}

	// Set default trading platform
	if config.Exchange == "" {
		config.Exchange = "ninjatrader"
	}

	// CTO F2 (critic G1), fail-closed at the SOURCE: the NinjaTrader venue
	// trades CME futures only. A trader configured on it with a non-CME symbol
	// (its NT8 symbol or a strategy static symbol) is REFUSED here — named,
	// never started — instead of reading that symbol from an outside source
	// every cycle.
	if strings.EqualFold(strings.TrimSpace(config.Exchange), "ninjatrader") {
		if bad := nonCMESymbolsForNT8(config); len(bad) > 0 {
			return nil, fmt.Errorf("refused: trader %q is on the NinjaTrader venue but trades %s — not CME futures symbols; this trader does not load", config.Name, strings.Join(bad, ","))
		}
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
		// release #55 (m452): the data-truth gate is switched on for EVERY NinjaTrader
		// trader, OUTSIDE the `st != nil` guard below. A trader built without a store
		// has no bar persistence and so never lands the eight m446 checks; with the
		// gate ON those required checks stay stubs that BLOCK after the boot grace
		// (fail closed), where leaving the call inside the guard left dataTruthWired
		// false and the whole m446/m447 leg silently inert (fail OPEN).
		// trader/data_truth_wiring_pin_test.go pins this call site (AST: a live call,
		// in NewAutoTrader, under no `if`).
		wireDataTruthBoot() // release #55: m446/m447 data-truth checks -> the entry verdict (data_truth_gate.go)
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
					// D2 (2026-09-03) — the ORDINAL SEED, installed beside the
					// sink that writes the rows it reads. Without it
					// TouchEpisode.Number restarts at 1 on every boot while the
					// closed episodes keep persisting: the live table reads
					// touch_number 1 → 513 rows · 2 → 229 · 3 → 131 · 4 → 95.
					kernel.SetTouchOrdinalSeed(func(traderID, symbol, label string, price float64, sessionDay string) int {
						return te.MaxTouchNumber(traderID, symbol, label, price, sessionDay)
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
			logger.Infof("✓ [%s] Auto-fetched initial balance: %.2f", config.Name, foundBalance)
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
	strategyEngine := kernel.NewStrategyEngine(config.StrategyConfig)
	// CTO F2: the cycle's market reads route through the trader's venue.
	strategyEngine.SetVenue(config.Exchange)
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
		positionMentorOwned:   make(map[string]bool),
		kickCh:                make(chan string, 4),
		mentorFillCh:          make(chan mentorFillReceipt, 256),
		mentorExitCh:          make(chan mentorExitReceipt, 256),
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
	// WAVE 1a-plan N5 — the CSV transport takes the same maintenance
	// permit (only entry sends); wired at construction like the TCP path.
	if csv, ok := at.trader.(*ntTrader.Trader); ok {
		wireNT8MaintenanceCSV(csv)
	}
	if nt, ok := at.trader.(*ntTrader.TCPTrader); ok {
		nt.SetOpenOrdersSource(at.ledgerOpenOrders)
		// THE LEDGER'S EAR FOR A REFUSAL (2026-09-07). Every entry-reject path
		// in the NT8 trader calls this with the BROKER'S reason, verbatim.
		nt.SetRejectSink(at.recordBrokerRejection)
		// W-ONE-BUTTON M2 — the installation maintenance hold (sites 4, 4b, 7
		// and the M-2 drop sink), wired at construction, before Run.
		wireNT8Maintenance(at, nt)
		// W-EXEC-TRUTH W0 (b) — the one entry latch's evidence (book + both
		// ledgers), wired at construction, before Run.
		wireNT8EntryLatch(at, nt)
	}
	// N4 (release #23): pdVWAP / the prior-day profile read the WHOLE prior
	// session from the store when the 1m ring holds only its tail.
	if at.exchange == "ninjatrader" && at.store != nil {
		installPriorSessionProvider(at.store)
	}
	// P0 SPLICE (CTO 1791030462901) — a mentor-mode trader seeds the evaluator
	// from the stored 1m+1h bars at start (read-only); while any source is
	// missing every mentor entry is refused with the named source.
	// P0 WIRING (CTO 1791039541371) — the production seams bind FIRST; a
	// missing seam refuses entries, named, on the same boot line.
	if at.mentorEnabled() {
		at.mentorWireProductionSeams()
		at.mentorSeedAtStart()
	}
	return at, nil
}

// Run runs the automatic trading main loop
// panicInTickOnce is the P1-F TEST SEAM (default off): when set, the next
// run-loop beat panics, so tests can pin the recovery path.
var panicInTickOnce atomic.Bool

// recoverPanic is the P1-F safety net (audit 2026-09-26): a panic in one
// trader's loop or monitor must never kill the process. ERROR + stack, the
// trader FREEZES (admitChain's frozen leg refuses new entries; closes and
// exits keep working — they bypass admission by design), and the goroutine
// exits cleanly while the other goroutines (protection monitor, picture
// consumer) and every other trader keep running.
func (at *AutoTrader) recoverPanic(where string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		at.logErrorf("💥 PANIC in %s for trader %s: %v\n%s", where, at.id, r, stack)
		telemetry.RecordError(at.id, "trader_panic", fmt.Sprintf("%s: %v", where, r), telemetry.CostNone)
		discipline.FreezeTrader(at.id, fmt.Sprintf("panic in %s: %v", where, r), time.Now().UnixMilli())
		at.emitAlert("P0", "trader_panic", "panic:"+at.id,
			"💥 Trader panic — entries frozen (closes still work)", fmt.Sprintf("%s: %v", where, r))
	}
}

// runBeatSafely runs one loop/monitor beat under the P1-F recovery.
func (at *AutoTrader) runBeatSafely(where string, fn func()) {
	defer at.recoverPanic(where)
	fn()
}

// ErrTraderAlreadyRunning is returned by Run when the trader is already running
// in memory: a second start of a running trader is a NO-OP, never a rollback.
// Callers that write is_running on a Run error must NOT write 0 for this error —
// the winning instance is live and the DB bit must keep matching it.
var ErrTraderAlreadyRunning = errors.New("trader is already running")

func (at *AutoTrader) Run() error {
	// FIX-RUNNING-FLAG: idempotent start. The CAS is the FIRST statement — a
	// losing duplicate start must not mutate ANY state (not even
	// limitFlattenStopped) and never reaches startPictureRun / the ordered
	// install. Two concurrent starts yield exactly one running instance.
	at.isRunningMutex.Lock()
	if at.isRunning {
		at.isRunningMutex.Unlock()
		// Logged by every caller ("duplicate start ignored"); counted here.
		telemetry.IncGateBlock(at.id, "trader_run_duplicate_start")
		return ErrTraderAlreadyRunning
	}
	at.isRunning = true
	runStopGen := at.stopGen
	at.isRunningMutex.Unlock()

	// PEP MODE — mutual exclusion at load: a strategy with pep ON together with
	// mentor mode or the day plan is REFUSED here, before anything starts (one P0
	// alert, one count, one ERROR, the trader marked not running). Never silent.
	if perr := at.pepStartRefusal(); perr != nil {
		at.isRunningMutex.Lock()
		at.isRunning = false
		at.isRunningMutex.Unlock()
		return perr
	}

	// MENTOR-NY item 3 — ONE live run per trader_id (run_owner.go): claim the
	// trader_id BEFORE anything starts. A retired instance (removed from the
	// roster by a reload) is refused here with no side effect; an older live
	// run of the same trader_id is stopped and must EXIT before this one
	// proceeds. The release runs last, after the Run has fully exited.
	release, ownErr := at.claimRunOwner()
	if ownErr != nil {
		at.isRunningMutex.Lock()
		at.isRunning = false
		at.isRunningMutex.Unlock()
		return ownErr
	}
	// PR #522 CI: the claim is released only after EVERYTHING this run
	// started is torn down — a Run can leave its loop on the running flag
	// before a concurrent Stop has finished its teardown (ordered slot,
	// event loop), and the next run must never see that half-stopped state.
	// Idempotent beside Stop's own teardown.
	defer func() {
		at.teardownRunStarted()
		release()
	}()
	// A Run that PANICS out (not a beat — runBeatSafely recovers those) must
	// not release the claim with its event loop still live: tear down first,
	// then re-panic (the caller's recovery is unchanged). LIFO: this runs
	// before release().
	defer func() {
		if r := recover(); r != nil {
			at.teardownRunStarted()
			at.isRunningMutex.Lock()
			at.isRunning = false
			at.isRunningMutex.Unlock()
			panic(r)
		}
	}()

	at.limitFlattenMu.Lock()
	at.limitFlattenStopped = false
	at.limitFlattenMu.Unlock()
	at.startPictureRun()                        // W3 D14 event loop + run epoch, THEN Picture's live-bar routing (W5 R2)
	at.logInfof("🚦 %s", entryLatchBootLine(at)) // W-EXEC-TRUTH W0 (b) — READ, never asserted
	if at.exchange == "ninjatrader" {
		if _, ok := kernel.TFDurationMs(at.primaryTimeframe()); !ok {
			at.teardownRunStarted() // MENTOR-NY item 3: no event loop survives a refused start
			at.isRunningMutex.Lock()
			at.isRunning = false
			at.isRunningMutex.Unlock()
			return fmt.Errorf("primary_timeframe %q is not in the timeframe table (kernel/timeframes.go) — refusing to run on a corrupt bar clock", at.primaryTimeframe())
		}
	}
	at.stopMonitorMu.Lock()
	stopCtx, stopCancel := context.WithCancel(context.Background())
	at.stopMonitorCtx = stopCtx
	at.stopMonitorCancel = stopCancel
	at.stopMonitorMu.Unlock()
	stopDone := stopCtx.Done()
	at.kickCh = make(chan string, 4) // fresh kick channel per Run (restart-safe)
	at.kickPending.Store(false)
	registerPostExitDispatch(at) // Phase 4: close events → one post-exit rescan
	at.startTime = time.Now()
	// m420b/B3: a fresh run whose watchdog is not blocked has no live
	// reconcile-stuck episode; clear any stale banner a dead process left.
	if !at.deadMan.entriesBlocked() {
		at.ackDeadManStuck()
	}

	// P2 (ledger-close 2026-08-19) — restore an owner pause across restart.
	at.loadPersistedPause()
	// E1 — the per-trader ledger boot block (sessions/cutoffs, pause, cadence,
	// roll, balance-alert). The process half prints in main.go.
	at.logLedgerBootBlock(time.Now())
	at.logLevelIdentityBootAt(time.Now())
	at.logLevelZonesBootAt(time.Now())
	// W-PICTURE-HTF — the mode's own boot line: mode, rule version,
	// SIM status, native-data readiness, AddOn capability (READ, never
	// assumed).
	at.logPictureHtfBootLine()
	at.logInfof("⚙️  Scan interval: %v", at.config.ScanInterval)
	logger.Info("🤖 AI will make full decisions on leverage, position size, stop loss/take profit, etc.")

	at.monitorWg.Add(1)
	defer at.monitorWg.Done()

	// Start drawdown monitoring
	at.startDrawdownMonitor()

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
			// W117 F2 (R5) — install the ordered-execution worker registration at
			// RUN (never at construction); the closure is kept for Stop.
			if err := at.installNTOrderedExecutions(ntTCP); err != nil {
				// REL18-C O2 — the start FAILS: the duplicate owner was logged,
				// this trader marked not-running, and the P0 raised. Clean the
				// per-Run monitor up and exit (same shape as the grid-init error
				// path) — never fall back to the legacy advisory path.
				at.signalRunReady(err)
				// MENTOR-NY item 3: tear down EVERYTHING this Run started —
				// on 10-13 16:35:48 only the stop monitor was cancelled, so the
				// refused instance's armed event loop (which drives the mentor
				// evaluator) ran on beside the owner: two evaluators.
				at.teardownRunStarted()
				at.isRunningMutex.Lock()
				at.isRunning = false
				at.isRunningMutex.Unlock()
				return fmt.Errorf("ordered-execution install refused: %w", err)
			}
			// Anchor entry_price to the NT8 position average + clear orphan rows
			// (the 5m-mark entry the AI-decision write records goes stale/frozen).
			ntTCP.StartPositionReconcile(at.id, at.exchangeID, at.exchange, at.store)
			// m447 - the data-truth broker_agreement check samples on that loop.
			at.registerBrokerAgreement()
			at.logInfof("🔧 NinjaTrader position-reconcile enabled (entry→NT8 avg, orphan clear)")
		}
	}

	// REL18-C FOLD — the start handler waits on this: nil means the ordered
	// install is done (or this trader has no ordered install to do); the O2
	// refusal was already signalled on the error path above.
	at.signalRunReady(nil)

	// MENTOR-NY item 3: a Stop (or a reload's Retire+Stop) that landed while
	// this Run was still starting found little to tear down; whatever Run
	// started after it (the armed event loop = the mentor evaluator, the
	// Picture / post-exit registrations, the ordered worker) is torn down here,
	// so a stopped instance never keeps a live evaluator.
	at.isRunningMutex.RLock()
	stoppedWhileStarting := !at.isRunning || at.stopGen != runStopGen
	at.isRunningMutex.RUnlock()
	if stoppedWhileStarting {
		at.logWarnf("⏹ stopped while starting — tearing down this run before its loop (no event loop or mentor evaluator survives)")
		at.teardownRunStarted()
		return nil
	}

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// Check if this is a grid trading strategy
	isGridStrategy := at.IsGridStrategy()
	if isGridStrategy {
		at.logInfof("🔲 Grid trading strategy detected, initializing grid...")
		if err := at.InitializeGrid(); err != nil {
			at.logErrorf("❌ Failed to initialize grid: %v", err)
			at.teardownRunStarted() // NOTE-leak fix + MENTOR-NY item 3: the monitor AND the event loop / registrations started above must not orphan
			at.isRunningMutex.Lock()
			at.isRunning = false
			at.isRunningMutex.Unlock()
			return fmt.Errorf("grid initialization failed: %w", err)
		}
	}

	// Execute immediately on first run. Under bar-close cadence (P2.1) this runs
	// once on the last CLOSED primary-TF bar and sets the watermark, then the loop
	// idles until the next bar closes; the scan-timer default is unchanged.
	at.runBeatSafely("run loop (first beat)", func() { at.tickOnce(isGridStrategy) })

	for {
		at.isRunningMutex.RLock()
		running := at.isRunning
		at.isRunningMutex.RUnlock()

		if !running {
			break
		}

		select {
		case <-ticker.C:
			var tickStart time.Time
			var closedSkip bool
			at.runBeatSafely("run loop", func() {
				// Deterministic two-picture fallback: the event path is the 5m bar
				// frame; the tick covers a missed boundary (feed stall). Cheap no-op
				// while the mode is off.
				at.pictureHtfTickFallback(time.Now())
				// The loop is single-goroutine: a tick that fires while a cycle is
				// still running WAITS here (the ticker drops missed ticks), so an
				// in-flight AI read is structurally never cancelled by the next
				// tick. Log the overrun so a slow call is visible, not mysterious.
				tickStart = time.Now()
				closedSkip = at.tickOnce(isGridStrategy)
			})
			if d := time.Since(tickStart); shouldWarnOverrun(d, at.config.ScanInterval, closedSkip) {
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
		case <-stopDone:
			at.logInfof("⏹ Stop signal received, exiting automatic trading main loop")
			return nil
		}
	}

	return nil
}

// Stop stops the automatic trading
func (at *AutoTrader) Stop() {
	at.stopLimitFlattens()
	at.isRunningMutex.Lock()
	// W26 N4 — bump the generation even when the run is not running: a refused
	// instance (isRunning=false) sits in the armed run retry, and its Stop must
	// cancel that retry (armRunRetry compares this against the gen it captured).
	at.stopGen++
	if !at.isRunning {
		at.isRunningMutex.Unlock()
		return
	}
	at.isRunning = false
	at.isRunningMutex.Unlock()

	at.teardownRunStarted()
	at.monitorWg.Wait()                   // Wait for monitoring goroutine to finish
	ntTrader.StopLevelStatsNightly(at.id) // NOTE-leak fix: the 24h-idling nightly goroutine exits on Stop
	logger.Info("⏹ Automatic trading system stopped")
}

// teardownRunStarted undoes everything Run starts, in Stop's order: the
// post-exit routing, the Picture registration and broker consumer, the armed
// event loop (which also drives the mentor evaluator), the ordered worker and
// the per-Run stop monitor. Every step is idempotent. It does NOT wait on
// monitorWg — Run's own error paths call it while their deferred Done is still
// pending (MENTOR-NY item 3: before this, a refused Run left its armed event
// loop running = a second live mentor evaluator for the trader_id).
func (at *AutoTrader) teardownRunStarted() {
	unregisterPostExitDispatch(at)    // Phase 4: stop routing close events here
	at.unregisterBrokerAgreement()    // m447 — a stopped trader is not a voice in the agreement check
	at.unregisterPictureHtf()         // W4 D25 — no Picture frame after Stop
	at.stopPictureHtfBrokerConsumer() // P2-15 — the order_update consumer goroutine exits, never leaks across restarts
	at.stopArmedEventLoop()           // W3 D14 — no event pass after Stop
	// W117 F2 (R5) — the ordered worker's unregister closure is kept and
	// called HERE (the worker drains its queue, then exits; nothing is lost).
	at.orderedExecMu.Lock()
	if at.orderedExecUnreg != nil {
		at.orderedExecUnreg()
		at.orderedExecUnreg = nil
	}
	at.orderedExecMu.Unlock()
	at.cancelStopMonitor() // Notify monitoring goroutine to stop (idempotent per-Run cancel)
}

// cancelStopMonitor asks the drawdown monitor (and the run loop's stop select)
// to exit. Idempotent: the per-Run cancel can be called by Stop AND by the
// grid-init error path in any order.
func (at *AutoTrader) cancelStopMonitor() {
	at.stopMonitorMu.Lock()
	if at.stopMonitorCancel != nil {
		at.stopMonitorCancel()
	}
	at.stopMonitorMu.Unlock()
}

// setupStopMonitorForTest pre-arms a per-Run stop signal the way Run does, for
// tests that drive Stop (or the run loop) without a full Run. Production Run
// replaces it with its own ctx; cancelStopMonitor is nil-safe regardless.
func (at *AutoTrader) setupStopMonitorForTest() {
	at.stopMonitorMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	at.stopMonitorCtx = ctx
	at.stopMonitorCancel = cancel
	at.stopMonitorMu.Unlock()
}

// GetID gets trader ID
func (at *AutoTrader) GetID() string {
	return at.id
}

// RunReadyChan returns the per-start channel on which Run reports its ordered
// install verdict (nil = ready, error = refused by O2). The start handler reads
// it to keep the 200/409 honest; call it BEFORE spawning Run so both share the
// same channel. Lazy-created and buffered (cap 1) so a boot start never blocks.
func (at *AutoTrader) RunReadyChan() <-chan error {
	at.runReadyMu.Lock()
	defer at.runReadyMu.Unlock()
	if at.runReadyCh == nil {
		at.runReadyCh = make(chan error, 1)
	}
	return at.runReadyCh
}

// signalRunReady publishes Run's install verdict exactly once and clears the
// channel so the NEXT Run handshakes a fresh one. A nil channel (boot start,
// no reader) is a no-op.
func (at *AutoTrader) signalRunReady(err error) {
	at.runReadyMu.Lock()
	ch := at.runReadyCh
	at.runReadyCh = nil
	at.runReadyMu.Unlock()
	if ch != nil {
		select {
		case ch <- err:
		default:
		}
	}
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

// shouldWarnOverrun decides whether a finished cycle deserves the scan-interval
// warning. PURE — no clock, no receiver — so E5 can drive it with the exact
// durations the log recorded (class 60 / A28).
//
// THE CLOSED PATH IS EXEMPT (owner ruling 2026-09-07). backoffWhileClosed sleeps
// a deliberate 3 minutes; the live ScanInterval is 2. A 3-minute sleep can never
// fit inside a 2-minute interval, so on a closed market this warning was
// GUARANTEED rather than diagnostic — 165 of them in the 2026-09-06 boot log,
// every one reading "3m0.0XXs > 2m0s". A warning that cannot indicate a fault
// must not fire, because it teaches the reader to skip the line that one day
// does indicate one (A24).
//
// The sleep itself is KEPT: idling slowly while the market is shut is correct.
// Only the false alarm goes.
func shouldWarnOverrun(d, interval time.Duration, closedSkip bool) bool {
	if closedSkip {
		return false
	}
	return d > interval
}

// recordBrokerRejection moves an armed row terminal in the broker's own words.
//
// It is the ledger half of the C8 handler, which for two weeks cleaned every
// in-memory trace of a rejected entry and told nobody who could write it down.
// On 2026-09-07 that left arm 117 reading `working` for 33 minutes after NT8
// refused it, with the chart drawing a line for an order that did not exist.
func (at *AutoTrader) recordBrokerRejection(signalID, brokerReason string) {
	if at == nil || at.store == nil {
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return
	}
	row, err := ledger.FindBySignal(at.id, signalID)
	if err != nil || row == nil {
		// A rejection for a signal we hold no row for is not an error — a
		// manual NT8-side order can be rejected too. Say so; never guess.
		at.logWarnf("🚨 broker rejected signal %s (%s) — no armed row carries that signal; nothing to move", shortID(signalID), brokerReason)
		return
	}
	if err := ledger.RejectPlacement(row.ID, brokerReason); err != nil {
		at.logWarnf("🚨 broker rejected %s but the ledger write FAILED (%v) — the row may still claim a broker state", shortID(signalID), err)
		return
	}
	reason := brokerReason
	if strings.TrimSpace(reason) == "" {
		reason = store.PlacementReasonUnavailable
	}
	at.logWarnf("🚨 received armed entry rejection %s leg %d signal=%s reason=%q", row.Scenario, row.LegIndex+1, signalID, reason)
	telemetry.IncGateBlock(at.id, "place_rejected_by_broker")
}

// installNTOrderedExecutions (W117 F2, R5) registers this trader's durable
// execution consumers with the server's per-(symbol,account) ordered worker at
// RUN time — never at construction. The unregister closure is kept and called
// on Stop.
//
// REL18-C O2: a second LIVE owner for the same (symbol, account) is refused
// loudly and FAILS this trader's start — the trader is stopped, marked
// not-running, and a P0 names both traders. It must NEVER continue on the
// legacy advisory path (two traders on one account would double-apply fills).
// A non-owner install error (nil server/store — impossible at Run) keeps the
// historical log-only path: those are setup faults, not ownership conflicts.
func (at *AutoTrader) installNTOrderedExecutions(nt *ntTrader.TCPTrader) error {
	if at == nil || at.store == nil || nt == nil {
		return nil
	}
	unreg, err := nt.InstallOrderedExecutions(at.id, at.exchangeID, at.exchange, at.store,
		func(u ntwire.OrderUpdatePayload) {
			at.onArmedOrderUpdate(u, at.store.ArmedOrders())
			// R6 — a cumulative entry update is exactly what a parked exit was
			// waiting for: retry the account's pending exit receipts now.
			if strings.EqualFold(u.State, "filled") || strings.EqualFold(u.State, "partfilled") {
				nt.RetryPendingNT8Exits(at.store)
			}
		})
	if err != nil {
		if errors.Is(err, ntwire.ErrOrderedOwnerExists) {
			otherID, _ := nt.OrderedExecutionOwnerID()
			symbol := at.futuresSymbol()
			account := nt.BoundAccount()
			other := otherID
			if strings.TrimSpace(other) == "" {
				other = "an already-running trader"
			}
			// FIX-RUNNING-FLAG: after Run's idempotent CAS, a same-ID hit here can
			// only be a STALE slot held by ANOTHER instance of this trader (a Run
			// that exited without Stop, or a reload whose old object still holds
			// the slot). This object is NOT the live instance, so it must be
			// refused the same as any foreign owner — P0 + is_running=0 — never
			// silently "already running", which would leave is_running=1 with
			// nothing running. A different-ID hit is the REL18-C O2 duplicate —
			// its owner is a different trader, not a second copy of this one.
			var reason string
			if otherID == at.id {
				reason = fmt.Sprintf("slot held by a stale instance of this same trader (%q)", other)
			} else {
				reason = fmt.Sprintf("slot already owned by trader %q", other)
			}
			telemetry.IncGateBlock(at.id, "ordered_owner_exists")
			at.emitAlert("P0", "ordered_owner_conflict",
				"ordered-owner:"+at.id,
				"Two traders bound to one account — trader stopped",
				fmt.Sprintf("Trader %q (%s) tried to start on %s / %s, but the ordered-execution %s. %q was STOPPED and marked not-running; it did not fall back to the legacy path.",
					at.name, at.id, symbol, account, reason, at.name))
			at.logErrorf("❌ ordered-execution install refused (%v) — %s (symbol=%s, account=%s); stopping %q (%s), no legacy fallback",
				err, reason, symbol, account, at.name, at.id)
			// Mark this trader not-running so the DB matches the refused start.
			if at.store != nil {
				if serr := at.store.Trader().UpdateStatus(at.userID, at.id, false); serr != nil {
					at.logErrorf("❌ ordered-execution refused but the not-running DB write FAILED (%v) — trader %q may still read running", serr, at.id)
				}
			}
			return fmt.Errorf("ordered-execution owner for (symbol=%s, account=%s) already exists — %s: %w",
				symbol, account, reason, err)
		}
		at.logErrorf("❌ ordered-execution install refused (%v) — the durable consumers stay on the legacy advisory path", err)
		return nil
	}
	at.orderedExecMu.Lock()
	// A previous registration (reload edge) is retired before the new one is
	// recorded — the old owner's worker drains and exits.
	if at.orderedExecUnreg != nil {
		at.orderedExecUnreg()
	}
	at.orderedExecUnreg = unreg
	at.orderedExecMu.Unlock()
	at.logInfof("🧭 ordered execution installed: (symbol,account) worker applies order/fill/close in TCP receive order")
	return nil
}

// firstFor reports whether key is new for the dedupe-once field *f and records
// it, atomically under onceMu (W-EXEC-TRUTH W0 Q15). The caller logs OUTSIDE
// the lock.
func (at *AutoTrader) firstFor(f *string, key string) bool {
	at.onceMu.Lock()
	defer at.onceMu.Unlock()
	if *f == key {
		return false
	}
	*f = key
	return true
}

// nonCMESymbolsForNT8 lists the symbols an NT8-venue trader is configured to
// trade that are not CME futures symbols: its NT8 symbol (when set) and its
// strategy's static coins (the engine's candidates). Empty = the pair is valid.
func nonCMESymbolsForNT8(config AutoTraderConfig) []string {
	var syms []string
	if s := strings.TrimSpace(config.NinjaTraderSymbol); s != "" {
		syms = append(syms, s)
	}
	if config.StrategyConfig != nil {
		syms = append(syms, config.StrategyConfig.CoinSource.StaticCoins...)
	}
	var bad []string
	for _, s := range syms {
		if strings.TrimSpace(s) != "" && !market.IsCMEFuturesSymbol(market.Normalize(s)) {
			bad = append(bad, s)
		}
	}
	return bad
}

// lockEntrySend takes the AutoTrader's ONE entry-send section (W1b FOLD-10)
// and returns its release, which is idempotent: a caller defers it (every
// early return and a panic release the section) AND calls it where the
// section ends, so the section never outlives the send it guards.
func (at *AutoTrader) lockEntrySend() (release func()) {
	at.entrySendMu.Lock()
	var once sync.Once
	return func() { once.Do(at.entrySendMu.Unlock) }
}
