package trader

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// ── r22 RISK-GATES — THE DAILY-LOSS GATE REALLY PROTECTS ────────────────────
//
// Before this wave the daily loss limit, when the owner switched it ON, was
// three partial gates that disagreed:
//
//   - the AI decision path (kernel/engine_analysis.go) tripped it on a P&L that
//     counted MANUAL trades, and only on a decision cycle — a mentor trader
//     never runs one;
//   - the mentor path checked it only while AUTHORING a new arm
//     (mentorDailyLossGate), never on an arm already resting at the broker;
//   - nothing flattened. The guide promised a "60-second force-flat sweep" and
//     a "force-flat class"; the code comment said the sweep "does NOT enforce
//     the dollar limit". There was no dollar stop anywhere.
//
// Now ONE verdict (dailyGuardVerdictAt) is asked by every entry path through
// the one admission chain (decision, agent door, arm/planner, mentor arms,
// Picture) and by the mentor's authoring step, and the 60-second monitor beat
// acts on a breach:
//
//	REFUSE   every new entry, on every path, counted (daily_loss)
//	CANCEL   resting ENTRY orders (withdrawEntriesIfDue + the mentor's own
//	         live-arm cancel) — protective stops/targets of open positions are
//	         never touched by the cancel
//	FLATTEN  every open BOT position through the existing flatten path
//	         (flattenPosition, the EOD-flat path), re-sent at most every 2 min
//	         while the ledger still shows it open
//	LATCH    until the CME session-day ends (17:00 CT), whatever the P&L does
//	         afterwards; kept across a restart (system_config)
//
// The P&L is the bot's OWN: realized + open, MANUAL trades excluded (P1-1:
// 76% of one trader's recorded loss was four manual 20/40-lot trades). It is
// read in ONE place, botDayPnLAt.
//
// STILL OFF BY DEFAULT for the owner (owner ruling R4): both live strategies
// store guardrails_enabled=false and daily_loss_enabled=false, so this whole
// file is inert until the owner switches BOTH on and sets the $. Nothing here
// changes a stored value.

// dailyLossFlattenResend is how long a flatten waits before it is re-sent for
// a position the ledger still shows open (the ledger lags the broker ~80s).
const dailyLossFlattenResend = 2 * time.Minute

// dailyLossLatchKey is the system_config key carrying a trader's latch across
// a restart: "<CME session-day>|<reason>".
func dailyLossLatchKey(traderID string) string { return "daily_loss_latch:" + traderID }

var (
	dailyLossMu        sync.Mutex
	dailyLossRestored  = map[*AutoTrader]bool{}  // the persisted latch was read once for this instance
	dailyLossPersisted = map[string]string{}     // traderID → session-day already written
	dailyLossFlatSent  = map[string]time.Time{}  // traderID|positionID → last flatten send
	dailyLossManualLog = map[string]bool{}       // traderID|positionID → manual position logged once
	dailyLossTripDay   = map[string]string{}     // traderID → session-day the trip was announced
)

// ── THE ONE P&L READER ──────────────────────────────────────────────────────

// botDayPnL is the bot's own P&L on the CME session-day, with every exclusion
// COUNTED rather than hidden (corrected-column law).
type botDayPnL struct {
	Realized       float64 // closed BOT rows this session-day, pnl_corrected only
	Closed         int     // closed bot rows summed
	Unresolved     int     // closed bot rows with pnl_corrected NULL — excluded, counted
	ManualExcluded int     // closed + open rows excluded as manual (non-bot)
	Open           float64 // open BOT positions: partial legs already booked + the residual marked
	OpenPositions  int     // open bot positions
	OpenUnmarked   int     // open bot positions with no mark: their residual is NOT in Open
}

// Total is the number the loss leg judges: realized + open.
func (b botDayPnL) Total() float64 { return b.Realized + b.Open }

// isBotTrade reports whether a ledger row is the bot's own trade.
//
// TODO(r22 integration — fix/r22-pnl-truth): this is the plan_id UNRESOLVABLE
// sentinel, the rule deskRealizedToday has used: a reconcile-materialized
// position with no order of ours (the 4 manual NT8 trades #608 #609 #637 #640
// carry it). The pnl-truth lane adds a real manual flag and a bot-only helper
// (e.g. BotRealizedPnL). Swap THIS function (and botClosedTodayAt's realized
// half) to it at integration — every daily-loss read goes through here.
// Known limit of the sentinel: a late AI fill adopted by reconcile also keeps
// UNRESOLVABLE (reconcile_late_fill.go) and is excluded with the manual rows.
func isBotTrade(p *store.TraderPosition) bool {
	return p != nil && store.ClassifyPlanLink(p.PlanID) != store.PlanLinkUnresolvable
}

// botClosedTodayAt sums the bot's CLOSED trades on the CME session-day under
// the A22 rules. deskRealizedToday (the desk DAY line) delegates here, so the
// strip and the gate read one definition.
//
// KNOWN LIMIT (owner ruling 10-05, unchanged): trader-wide, newest 500 closes.
func (at *AutoTrader) botClosedTodayAt(now time.Time) (realized float64, n, unresolved, manual int, err error) {
	if at.store == nil {
		return 0, 0, 0, 0, fmt.Errorf("no store")
	}
	dayMs := kernel.CMESessionDayStart(now).UnixMilli()
	rows, err := at.store.Position().GetClosedPositions(at.id, 500)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, p := range rows {
		if p == nil || p.ExitTime < dayMs {
			continue
		}
		if store.UnknownPnLReason(p.CloseReason) {
			continue // reconcile-flat / unresolved orphans: P&L unknown by construction
		}
		if !isBotTrade(p) {
			manual++
			continue
		}
		if p.PnlCorrected == nil {
			unresolved++
			continue
		}
		realized += *p.PnlCorrected
		n++
	}
	return realized, n, unresolved, manual, nil
}

// dailyLossMarkSource is the mark the open half reads (test seam; nil = the
// newest cached 1m close, the same tape the mentor's no-chase reads).
var dailyLossMarkSource func(symbol string) (float64, bool)

func dailyLossMark(symbol string) (float64, bool) {
	if dailyLossMarkSource != nil {
		return dailyLossMarkSource(symbol)
	}
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(symbol, "1m", 1)
	if len(bars) == 0 || bars[len(bars)-1].Close <= 0 {
		return 0, false
	}
	return bars[len(bars)-1].Close, true
}

// botDayPnLAt is THE ONE READ of the daily-loss gate's P&L.
func (at *AutoTrader) botDayPnLAt(now time.Time) (botDayPnL, error) {
	var out botDayPnL
	realized, n, unresolved, manual, err := at.botClosedTodayAt(now)
	if err != nil {
		return out, err
	}
	out.Realized, out.Closed, out.Unresolved, out.ManualExcluded = realized, n, unresolved, manual
	opens, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		return out, err
	}
	for _, p := range opens {
		if !isBotTrade(p) {
			out.ManualExcluded++
			continue
		}
		out.OpenPositions++
		pv := market.FuturesPointValue(p.Symbol)
		side := positionSide(p.Side)
		dir := 1.0
		if side == "short" {
			dir = -1.0
		}
		// Partial legs already closed by a broker receipt while the row stays
		// OPEN (store.ApplyNT8Exit keeps the residual in Quantity and the legs'
		// average exit in ExitPrice): the SAME receipt math, re-derived from
		// the row's prices — never the raw realized_pnl column (corrected-
		// column guard, store/pnl_surface_guard_test.go).
		if closedLegs := p.EntryQuantity - p.Quantity; closedLegs > 0 && p.ExitPrice > 0 && pv > 0 && side != "" {
			out.Open += dir * (p.ExitPrice - p.EntryPrice) * closedLegs * pv
		}
		mark, ok := dailyLossMark(p.Symbol)
		if !ok || pv <= 0 || side == "" || p.EntryPrice <= 0 || p.Quantity <= 0 {
			out.OpenUnmarked++
			continue
		}
		out.Open += dir * (mark - p.EntryPrice) * p.Quantity * pv
	}
	return out, nil
}

// ── THE ONE VERDICT ─────────────────────────────────────────────────────────

// dailyGuardVerdict is the daily-guardrail answer for one entry attempt.
type dailyGuardVerdict struct {
	Refuse  bool
	Class   string // gate-block class: daily_loss | daily_profit_target | max_daily_trades | daily_guardrail_unverified
	Reason  string
	Latched bool // a daily-loss latch holds (this call's trip or an earlier one)
	// Unverified: the refusal is FAIL-CLOSED on an unknown (an unresolved
	// close today, or a failed read), not on a definite limit. Only the
	// mentor's authoring gate refuses on it — the rule it has had since
	// release 10-05-1; the admission chain refuses only on a definite limit,
	// so no path gains a refusal it did not have before r22.
	Unverified bool
}

// dailyGuardVerdictAt evaluates the daily guardrails (loss, profit target,
// max trades) for this trader at now — the SAME kernel.DailyGuardrails.Check
// the AI decision path runs, on the same resolved posture
// (kernel.ResolveStrategyGuardrails), with the loss leg judged on the bot's own
// P&L. A definite loss breach LATCHES (tripDailyLoss). Master OFF, or every
// leg OFF → the gate is inert.
func (at *AutoTrader) dailyGuardVerdictAt(now time.Time) dailyGuardVerdict {
	if why := at.dailyLossLatchAt(now); why != "" {
		return dailyGuardVerdict{Refuse: true, Latched: true, Class: "daily_loss",
			Reason: "daily_loss: " + why + " — latched until the CME session-day ends (17:00 CT)"}
	}
	if at.config.StrategyConfig == nil {
		return dailyGuardVerdict{}
	}
	rc := at.config.StrategyConfig.RiskControl
	gp := kernel.ResolveStrategyGuardrails(rc, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	if !gp.MasterEnabled {
		return dailyGuardVerdict{}
	}
	lossOn := gp.DailyLossEnabled && gp.DailyLossLimitUSD > 0
	profitOn := gp.DailyProfitEnabled && gp.DailyProfitTargetUSD > 0
	tradesOn := gp.MaxDailyTradesEnabled && gp.MaxDailyTrades > 0
	if !lossOn && !profitOn && !tradesOn {
		return dailyGuardVerdict{}
	}
	if at.store == nil {
		return dailyGuardVerdict{} // unit fixtures only; production always has a store
	}
	g := kernel.DailyGuardrails{
		MasterEnabled:         true,
		DailyLossEnabled:      gp.DailyLossEnabled,
		DailyLossLimitUSD:     gp.DailyLossLimitUSD,
		DailyProfitEnabled:    gp.DailyProfitEnabled,
		DailyProfitTargetUSD:  gp.DailyProfitTargetUSD,
		MaxDailyTradesEnabled: gp.MaxDailyTradesEnabled,
		MaxDailyTrades:        gp.MaxDailyTrades,
	}
	var bot botDayPnL
	if lossOn {
		b, err := at.botDayPnLAt(now)
		if err != nil {
			return dailyGuardVerdict{Refuse: true, Unverified: true, Class: "daily_guardrail_unverified",
				Reason: fmt.Sprintf("daily_guardrail_unverified: daily loss limit enforced but the P&L read failed (%v) — no new entry (fail-closed)", err)}
		}
		bot = b
		g.DailyLossPnL, g.DailyLossPnLKnown = b.Total(), true
	}
	if profitOn || tradesOn {
		// The SAME reader the AI decision path feeds the kernel gate
		// (attachTradeContext → GetSessionDayActivity), so "max trades" and
		// "profit target" mean one thing on every path.
		realized, trades, err := at.dailyGuardrailActivityAt(now)
		if err != nil {
			return dailyGuardVerdict{Refuse: true, Unverified: true, Class: "daily_guardrail_unverified",
				Reason: fmt.Sprintf("daily_guardrail_unverified: daily profit/trade limit enforced but the session-day read failed (%v) — no new entry (fail-closed)", err)}
		}
		g.DailyRealizedPnL, g.TradesToday = realized, trades
	}
	decision, err := g.Check()
	if err != nil {
		if decision == kernel.RiskForceFlat {
			reason := at.tripDailyLoss(err.Error(), bot, gp.DailyLossLimitUSD, now)
			return dailyGuardVerdict{Refuse: true, Latched: true, Class: "daily_loss", Reason: "daily_loss: " + reason}
		}
		if profitOn && g.DailyRealizedPnL >= g.DailyProfitTargetUSD {
			return dailyGuardVerdict{Refuse: true, Class: "daily_profit_target", Reason: "daily_profit_target: " + err.Error() + " — no new entry this CME session-day"}
		}
		return dailyGuardVerdict{Refuse: true, Class: "max_daily_trades", Reason: "max_daily_trades: " + err.Error() + " — no new entry this CME session-day"}
	}
	// FAIL-CLOSED on an unresolved close today (pnl_corrected NULL, canon 40):
	// an unknown loss is not a confident "under the limit" — the mentor's
	// placement gate has ruled this since release 10-05-1 (Unverified: the
	// admission chain does not refuse on it, see the field).
	if lossOn && bot.Unresolved > 0 {
		return dailyGuardVerdict{Refuse: true, Unverified: true, Class: "daily_guardrail_unverified",
			Reason: fmt.Sprintf("daily_guardrail_unverified: daily loss limit enforced but %d close(s) today have unresolved P&L (pnl_corrected NULL) — no new entry (fail-closed)", bot.Unresolved)}
	}
	return dailyGuardVerdict{}
}

// dailyGuardrailActivityAt is the decision path's daily-guardrail activity
// read (realized P&L + entries since the CME session-day start, scoped to the
// active account). attachTradeContext and the admission step both call it.
func (at *AutoTrader) dailyGuardrailActivityAt(now time.Time) (float64, int, error) {
	sinceMs := kernel.CMESessionDayStart(now).UnixMilli()
	return at.store.Position().GetSessionDayActivity(at.id, sinceMs, at.currentAccountName())
}

// tripDailyLoss latches a definite breach: the kernel latch (read by EntryGate
// leg D, the withdraw beat and this gate), the persisted copy, ONE log line +
// ONE gate-block count per trip. Returns the latch reason.
func (at *AutoTrader) tripDailyLoss(why string, bot botDayPnL, limit float64, now time.Time) string {
	day := kernel.CMESessionDayKey(now)
	reason := fmt.Sprintf("%s — bot realized %.2f on %d close(s) + open %.2f on %d position(s); %d manual row(s) excluded, %d unresolved, %d unmarked",
		why, bot.Realized, bot.Closed, bot.Open, bot.OpenPositions, bot.ManualExcluded, bot.Unresolved, bot.OpenUnmarked)
	kernel.SetDailyForceFlatAt(at.id, reason, now)
	at.persistDailyLossLatch(day, reason)
	dailyLossMu.Lock()
	first := dailyLossTripDay[at.id] != day
	dailyLossTripDay[at.id] = day
	dailyLossMu.Unlock()
	if first {
		at.logErrorf("🔴 DAILY LOSS LIMIT HIT (limit -%.2f): %s. New entries REFUSED on every path, resting entry orders CANCELLED, bot positions FLATTENED; latched until CME session-day %s ends (17:00 CT).",
			limit, reason, day)
		telemetry.IncGateBlock(at.id, "daily_loss_tripped")
		at.emitAlert("P0", "daily-loss", "daily-loss:"+day, "🔴 Daily loss limit hit", reason)
	}
	return reason
}

// ── THE LATCH ───────────────────────────────────────────────────────────────

// dailyLossLatchAt returns the latch reason for this trader at now ("" = not
// latched). The kernel map is the live latch; the persisted copy is read ONCE
// per trader instance so a restart cannot lift a trip early.
func (at *AutoTrader) dailyLossLatchAt(now time.Time) string {
	if why := kernel.DailyForceFlatReasonAt(at.id, now); why != "" {
		return why
	}
	dailyLossMu.Lock()
	done := dailyLossRestored[at]
	dailyLossRestored[at] = true
	dailyLossMu.Unlock()
	if done || at.store == nil {
		return ""
	}
	raw, err := at.store.GetSystemConfig(dailyLossLatchKey(at.id))
	if err != nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	day, reason, ok := strings.Cut(raw, "|")
	if !ok || day != kernel.CMESessionDayKey(now) {
		return "" // a latch from an earlier session-day has lifted at 17:00 CT
	}
	reason = "restored after restart — " + reason
	kernel.SetDailyForceFlatAt(at.id, reason, now)
	dailyLossMu.Lock()
	dailyLossPersisted[at.id] = day
	dailyLossTripDay[at.id] = day
	dailyLossMu.Unlock()
	at.logWarnf("🔴 daily-loss latch RESTORED for session-day %s (it survives a restart until 17:00 CT): %s", day, reason)
	return reason
}

// persistDailyLossLatch writes the latch once per session-day.
func (at *AutoTrader) persistDailyLossLatch(day, reason string) {
	if at.store == nil {
		return
	}
	dailyLossMu.Lock()
	already := dailyLossPersisted[at.id] == day
	dailyLossMu.Unlock()
	if already {
		return
	}
	if err := at.store.SetSystemConfig(dailyLossLatchKey(at.id), day+"|"+reason); err != nil {
		at.logWarnf("⚠️ daily-loss latch NOT persisted (%v) — it holds in this process; a restart re-derives it from the day's P&L", err)
		return
	}
	dailyLossMu.Lock()
	dailyLossPersisted[at.id] = day
	dailyLossMu.Unlock()
}

// ClearDailyLossLatch lifts THIS trader's daily-loss latch (the Emergency Flat
// on this trader — T4). Every other trader's latch stands. If this trader's
// own loss still stands, its gate re-trips on the next beat.
func (at *AutoTrader) ClearDailyLossLatch(why string) {
	if at == nil {
		return
	}
	kernel.ClearDailyForceFlat(at.id)
	if at.store != nil {
		if err := at.store.SetSystemConfig(dailyLossLatchKey(at.id), ""); err != nil {
			at.logWarnf("⚠️ daily-loss latch: persisted copy NOT cleared (%v)", err)
		}
	}
	dailyLossMu.Lock()
	delete(dailyLossPersisted, at.id)
	delete(dailyLossTripDay, at.id)
	dailyLossMu.Unlock()
	at.logWarnf("🔓 daily-loss latch cleared for THIS trader only (%s) — every other trader's latch stands; this trader's gate re-checks its own P&L on the next beat", why)
}

// ── THE BREACH RESPONSE (60-second monitor beat) ────────────────────────────

// dailyLossBeatAt runs on monitorTick BEFORE withdrawEntriesIfDue: it evaluates
// the gate (a definite breach latches here even when no entry is being
// attempted — the open half moves with the mark), keeps the persisted copy,
// and cancels the mentor's live arms through the mentor's own cancel path. The
// withdraw that follows cancels every resting entry row the latch names.
func (at *AutoTrader) dailyLossBeatAt(now time.Time) {
	if at == nil {
		return
	}
	v := at.dailyGuardVerdictAt(now)
	if !v.Latched {
		return
	}
	why := kernel.DailyForceFlatReasonAt(at.id, now)
	at.persistDailyLossLatch(kernel.CMESessionDayKey(now), why)
	if at.mentorEnabled() {
		// A dollar stop has no swing exemption: every live mentor arm goes,
		// SWING4H included. Entries only — the mentor cancel never touches a
		// filled position's bracket.
		at.cancelAllLiveMentorArmsWith("daily-loss limit: " + why)
	}
}

// dailyLossFlattenAt runs on monitorTick AFTER withdrawEntriesIfDue (cancel
// first, then flatten — a working entry must not fill after the flat): while
// the latch holds, every open BOT position is flattened through the existing
// flatten path. A manual position is the owner's and is left alone (logged
// once). Re-sent at most every dailyLossFlattenResend while still open.
func (at *AutoTrader) dailyLossFlattenAt(now time.Time) {
	if at == nil || at.store == nil || at.trader == nil {
		return
	}
	why := kernel.DailyForceFlatReasonAt(at.id, now)
	if why == "" {
		return
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		at.logWarnf("⚠️ DAILY-LOSS FLAT: open-position read failed (%v) — flatness UNVERIFIED this beat", err)
		return
	}
	for _, p := range positions {
		key := at.id + "|" + strconv.FormatInt(p.ID, 10)
		if !isBotTrade(p) {
			dailyLossMu.Lock()
			seen := dailyLossManualLog[key]
			dailyLossManualLog[key] = true
			dailyLossMu.Unlock()
			if !seen {
				at.logWarnf("🔴 DAILY-LOSS FLAT: position %d (%s %s qty %.0f) is MANUAL (not the bot's) — left alone; it is not counted in the bot's daily P&L", p.ID, p.Symbol, p.Side, p.Quantity)
			}
			continue
		}
		dailyLossMu.Lock()
		last, sent := dailyLossFlatSent[key]
		if sent && now.Sub(last) < dailyLossFlattenResend && !now.Before(last) {
			dailyLossMu.Unlock()
			continue
		}
		dailyLossFlatSent[key] = now
		dailyLossMu.Unlock()
		resend := ""
		if sent {
			resend = " (RE-SENT — the ledger still shows it open)"
		}
		at.logErrorf("🔴 DAILY-LOSS FLAT: flattening position %d (%s %s qty %.0f)%s — %s", p.ID, p.Symbol, p.Side, p.Quantity, resend, why)
		at.flattenPosition(p, "🔴 DAILY-LOSS FLAT")
	}
}

// ── BOOT / STATUS ───────────────────────────────────────────────────────────

// DailyLossGateBootLineForStrategy is the per-trader load line: what the gate
// does for THIS strategy, every field READ from the resolver the gate runs.
func DailyLossGateBootLineForStrategy(cfg *store.StrategyConfig) string {
	if cfg == nil {
		return "daily-loss gate: n/a (no strategy config)"
	}
	gp := kernel.ResolveStrategyGuardrails(cfg.RiskControl, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	src := "studio"
	if cfg.RiskControl.DailyLossLimitUSD <= 0 {
		src = "env RISK_MAX_DAILY_LOSS_USD fallback"
	}
	// Each switch says whether it was SAVED or is the default (absent = OFF
	// since r22, owner R4) — an OFF the owner never chose reads "(default)".
	toggle := func(p *bool, resolved bool) string {
		if p == nil {
			return onOff(resolved) + " (default)"
		}
		return onOff(resolved) + " (saved)"
	}
	posture := fmt.Sprintf("master %s, daily_loss_enabled %s, limit $%.0f (%s)",
		toggle(cfg.RiskControl.GuardrailsEnabled, gp.MasterEnabled), toggle(cfg.RiskControl.DailyLossEnabled, gp.DailyLossEnabled), gp.DailyLossLimitUSD, src)
	trades := "max_daily_trades off"
	if gp.MasterEnabled && gp.MaxDailyTradesEnabled && gp.MaxDailyTrades > 0 {
		trades = fmt.Sprintf("max_daily_trades %d ENFORCED on every path (mentor included)", gp.MaxDailyTrades)
	}
	if !gp.MasterEnabled || !gp.DailyLossEnabled || gp.DailyLossLimitUSD <= 0 {
		head := "OFF"
		if (!gp.MasterEnabled && cfg.RiskControl.GuardrailsEnabled == nil) || (!gp.DailyLossEnabled && cfg.RiskControl.DailyLossEnabled == nil) {
			head = "OFF (default)"
		}
		return fmt.Sprintf("daily-loss gate: %s (%s) — nothing refused, cancelled or flattened on a dollar loss · %s", head, posture, trades)
	}
	return fmt.Sprintf("daily-loss gate: ENFORCED (%s) — at bot P&L (realized+open, manual excluded) ≤ -limit: refuse new entries on every path, cancel resting entry orders, flatten bot positions, latched until 17:00 CT · %s", posture, trades)
}

// DailyLossGateStatus is the gate's live state for GET /api/risk/status.
type DailyLossGateStatus struct {
	Enforced       bool     `json:"enforced"`
	LimitUSD       float64  `json:"limit_usd"`
	Latched        bool     `json:"latched"`
	LatchReason    string   `json:"latch_reason"`
	BotPnLUSD      *float64 `json:"bot_pnl_usd"` // nil = not read (gate off or read failed)
	ManualExcluded *int     `json:"manual_rows_excluded"`
	Unresolved     *int     `json:"unresolved_rows"`
}

// DailyLossGateStatusAt reads the gate for the status endpoint. It never trips
// anything — a status read is not an evaluation.
func (at *AutoTrader) DailyLossGateStatusAt(now time.Time) DailyLossGateStatus {
	var s DailyLossGateStatus
	if at == nil {
		return s
	}
	s.LatchReason = kernel.DailyForceFlatReasonAt(at.id, now)
	s.Latched = s.LatchReason != ""
	if at.config.StrategyConfig == nil {
		return s
	}
	gp := kernel.ResolveStrategyGuardrails(at.config.StrategyConfig.RiskControl, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	s.LimitUSD = gp.DailyLossLimitUSD
	s.Enforced = gp.MasterEnabled && gp.DailyLossEnabled && gp.DailyLossLimitUSD > 0
	if !s.Enforced || at.store == nil {
		return s
	}
	if b, err := at.botDayPnLAt(now); err == nil {
		total, manual, unres := b.Total(), b.ManualExcluded, b.Unresolved
		s.BotPnLUSD, s.ManualExcluded, s.Unresolved = &total, &manual, &unres
	}
	return s
}
