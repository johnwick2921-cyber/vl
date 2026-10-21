package trader

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
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
// read in ONE place, botDayPnLAt, on the ONE manual rule (origin='manual',
// store/manual_origin.go — r22 pnl-truth).
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
	dailyLossMu              sync.Mutex
	dailyLossRestored        = map[*AutoTrader]bool{} // the persisted latch was read once for this instance
	dailyLossPersisted       = map[string]string{}    // traderID → session-day already written
	dailyLossFlatSent        = map[string]time.Time{} // traderID|positionID → last flatten send
	dailyLossManualLog       = map[string]bool{}      // traderID|positionID → manual position logged once
	dailyLossTripDay         = map[string]string{}    // traderID → session-day the trip was announced
	dailyLossSoftAuditLogged = map[string]bool{}      // traderID|session-day|hit → soft-audit line logged once
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
//
// "Manual" has ONE definition in the tree (r22 pnl-truth, #512): the position's
// origin column — store.BotRealizedPnL / store.BotOriginSQL for the closed
// half, store.IsManualOrigin (its Go twin) for the open half. The realized
// half IS store.BotRealizedPnL with the same arguments the desk DAY line uses
// (deskBotDay: trader-wide, the CME session-day start), so the strip and the
// gate cannot disagree.
func (at *AutoTrader) botDayPnLAt(now time.Time) (botDayPnL, error) {
	var out botDayPnL
	if at.store == nil {
		return out, fmt.Errorf("no store")
	}
	day, err := at.store.Position().BotRealizedPnL(at.id, kernel.CMESessionDayStart(now).UnixMilli())
	if err != nil {
		return out, err
	}
	out.Realized, out.Closed, out.Unresolved, out.ManualExcluded = day.Total, day.Resolved, day.Unresolved, day.Manual.Trades
	opens, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		return out, err
	}
	for _, p := range opens {
		if p == nil {
			continue
		}
		if store.IsManualOrigin(p.Origin) {
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
		// RT-1 (release #24): the latch may have been set by ANOTHER side
		// first — the AI decision cycle's kernel guardrail
		// (kernel/engine_analysis.go → SetDailyForceFlat) trips 2 s after a
		// stop-out, before this beat evaluates. Announce it here, once per
		// (trader, session-day), or that trip is silent: no 🔴 line, no
		// daily_loss_tripped count, no alert.
		at.announceDailyLossTrip(why, at.dailyLossLimitText(), now)
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
			Reason: fmt.Sprintf("daily_guardrail_unverified: daily loss limit enforced but %d close(s) today have unresolved P&L (pnl_corrected NULL, or a reconcile_flat/unresolved close) — no new entry (fail-closed)", bot.Unresolved)}
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
	at.announceDailyLossTrip(reason, fmt.Sprintf("-%.2f", limit), now)
	return reason
}

// announceDailyLossTrip is THE ONE announcement of a daily-loss trip: one
// "🔴 DAILY LOSS LIMIT HIT" line, one daily_loss_tripped count and one P0
// alert per (trader, CME session-day) — whichever side latched first (this
// gate's own trip, or the AI decision cycle's kernel guardrail that the next
// verdict finds already latched). A latch restored after a restart was
// announced by the process that tripped it, so the restore marks the day
// announced and logs its own RESTORED line instead. limit is the READ value
// ("-450.00"), or "n/a" when no strategy is readable.
func (at *AutoTrader) announceDailyLossTrip(reason, limit string, now time.Time) {
	day := kernel.CMESessionDayKey(now)
	dailyLossMu.Lock()
	first := dailyLossTripDay[at.id] != day
	dailyLossTripDay[at.id] = day
	dailyLossMu.Unlock()
	if !first {
		return
	}
	at.logErrorf("🔴 DAILY LOSS LIMIT HIT (limit %s): %s. New entries REFUSED on every path, resting entry orders CANCELLED, bot positions FLATTENED; latched until CME session-day %s ends (17:00 CT).",
		limit, reason, day)
	telemetry.IncGateBlock(at.id, "daily_loss_tripped")
	at.emitAlert("P0", "daily-loss", "daily-loss:"+day, "🔴 Daily loss limit hit", reason)
}

// dailyLossLimitText is the resolved daily-loss limit as the announcement
// prints it ("-450.00"), READ from the resolver the gate runs; "n/a" when the
// trader has no strategy config.
func (at *AutoTrader) dailyLossLimitText() string {
	if at.config.StrategyConfig == nil {
		return "n/a"
	}
	gp := kernel.ResolveStrategyGuardrails(at.config.StrategyConfig.RiskControl, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	return fmt.Sprintf("-%.2f", gp.DailyLossLimitUSD)
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
	// P2-4 (release #26): the soft guardrail audit (CheckSoft) only ever ran on
	// the AI decision path (kernel/engine_analysis.go), which a mentor trader
	// never runs — so a mentor trader saw NOTHING of what the cage would have
	// caught. Run it here, on the mentor's own beat, before the hard verdict.
	// Pure audit: logs + counters, never refuses.
	at.mentorSoftAudit(now)
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

// mentorSoftAudit is the mentor path's half of the soft guardrail audit
// (P2-4, release #26). The AI decision path runs CheckSoft in
// kernel/engine_analysis.go; a mentor trader never enters that cycle, so this
// runs it on the mentor's 60 s beat instead. It is PURE AUDIT: CheckSoft
// returns "would have tripped" reasons and never blocks anything; here they are
// logged and counted, once per (trader, CME session-day, reason), and the
// hard gate's own trip is still announced by dailyGuardVerdictAt.
func (at *AutoTrader) mentorSoftAudit(now time.Time) {
	if at == nil || at.store == nil || at.config.StrategyConfig == nil {
		return
	}
	if !at.mentorEnabled() {
		return // the AI path already soft-audits; do not double-log
	}
	rc := at.config.StrategyConfig.RiskControl
	gp := kernel.ResolveStrategyGuardrails(rc, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	if gp.MasterEnabled {
		return // the hard gate is live; the soft audit is the master-OFF diagnostic
	}
	g := kernel.DailyGuardrails{
		MasterEnabled:         false,
		DailyLossEnabled:      gp.DailyLossEnabled,
		DailyLossLimitUSD:     gp.DailyLossLimitUSD,
		DailyProfitEnabled:    gp.DailyProfitEnabled,
		DailyProfitTargetUSD:  gp.DailyProfitTargetUSD,
		MaxDailyTradesEnabled: gp.MaxDailyTradesEnabled,
		MaxDailyTrades:        gp.MaxDailyTrades,
		BlackoutConfigured:    rc.BlackoutStartCT != "" && rc.BlackoutEndCT != "",
		InBlackoutNow:         kernel.InBlackoutWindow(now, rc.BlackoutStartCT, rc.BlackoutEndCT),
		ConsistencyMaxDayPct:  rc.ConsistencyMaxDayPct,
	}
	if bot, err := at.botDayPnLAt(now); err == nil {
		g.DailyLossPnL, g.DailyLossPnLKnown = bot.Total(), true
	}
	if realized, trades, err := at.dailyGuardrailActivityAt(now); err == nil {
		g.DailyRealizedPnL, g.TradesToday = realized, trades
	}
	if rec, err := at.store.Position().GetFullStats(at.id, at.currentAccountName()); err == nil {
		g.TotalRealizedPnL = rec.TotalPnL
	}
	day := kernel.CMESessionDayKey(now)
	for _, hit := range g.CheckSoft() {
		key := at.id + "|" + day + "|" + hit
		dailyLossMu.Lock()
		seen := dailyLossSoftAuditLogged[key]
		dailyLossSoftAuditLogged[key] = true
		dailyLossMu.Unlock()
		if seen {
			continue
		}
		logger.Warnf("🔍 mentor guardrail WOULD have tripped (master OFF, not enforced): %s", hit)
		telemetry.RecordError(at.id, "guardrail_would_trip", hit, telemetry.CostNone)
		telemetry.IncTraderEvent(at.id, "mentor_guardrail_would_trip")
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
		if store.IsManualOrigin(p.Origin) {
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

// DailyLossLatchLifts is the one sentence every surface prints beside a held
// latch (RT-2, release #24). OWNER DECISION PENDING on lifting a latch when the
// gate is switched OFF; the fail-closed default KEEPS it, so the surfaces must
// say so instead of the switch's OFF.
const DailyLossLatchLifts = "switching the gate OFF does not lift it; Emergency Flat lifts it; it ends at 17:00 CT"

// DailyLossLatchStateAt READS a trader's daily-loss latch without evaluating,
// tripping or restoring anything: the live kernel latch, else the persisted
// copy a restart restores on the first beat (only when it is stamped with
// now's CME session-day). day "" = not latched.
func DailyLossLatchStateAt(st *store.Store, traderID string, now time.Time) (day, reason, source string) {
	if why := kernel.DailyForceFlatReasonAt(traderID, now); why != "" {
		return kernel.CMESessionDayKey(now), why, "live"
	}
	if st == nil {
		return "", "", ""
	}
	raw, err := st.GetSystemConfig(dailyLossLatchKey(traderID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", "", ""
	}
	d, why, ok := strings.Cut(raw, "|")
	if !ok || d != kernel.CMESessionDayKey(now) {
		return "", "", ""
	}
	return d, why, "persisted (restored on the first beat)"
}

// DailyLossGateBootLine is the per-trader load line (trader load, Studio save
// reload, restart): the posture line of DailyLossGateBootLineForStrategy,
// UNLESS a latch holds for now's session-day — then it reads LATCHED whatever
// the switches say, because the latch outlives switching the gate OFF (RT-2).
func DailyLossGateBootLine(st *store.Store, traderID string, cfg *store.StrategyConfig, now time.Time) string {
	day, reason, source := DailyLossLatchStateAt(st, traderID, now)
	if day == "" {
		return DailyLossGateBootLineForStrategy(cfg)
	}
	posture, trades := "n/a (no strategy config)", "max_daily_trades n/a"
	if cfg != nil {
		_, posture, trades = dailyLossGatePosture(cfg)
	}
	return fmt.Sprintf("daily-loss gate: LATCHED for session-day %s (%s) — new entries REFUSED on every path, resting entry orders CANCELLED, bot positions FLATTENED until then · latch %s: %s · switches now: %s · %s",
		day, DailyLossLatchLifts, source, reason, posture, trades)
}

// dailyLossGatePosture reads the resolved switches once: whether the loss leg
// is enforced, the posture text, and the max_daily_trades text.
func dailyLossGatePosture(cfg *store.StrategyConfig) (enforced bool, posture, trades string) {
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
	posture = fmt.Sprintf("master %s, daily_loss_enabled %s, limit $%.0f (%s)",
		toggle(cfg.RiskControl.GuardrailsEnabled, gp.MasterEnabled), toggle(cfg.RiskControl.DailyLossEnabled, gp.DailyLossEnabled), gp.DailyLossLimitUSD, src)
	trades = "max_daily_trades off"
	if gp.MasterEnabled && gp.MaxDailyTradesEnabled && gp.MaxDailyTrades > 0 {
		trades = fmt.Sprintf("max_daily_trades %d ENFORCED on every path (mentor included)", gp.MaxDailyTrades)
	}
	return gp.MasterEnabled && gp.DailyLossEnabled && gp.DailyLossLimitUSD > 0, posture, trades
}

// DailyLossScopeBootLine is the per-trader load line for the TWO scopes the
// daily-loss gate's legs read (N9, release #26 — REPORT ONLY, behaviour
// unchanged; the owner has not decided the scope). The loss leg is trader-wide
// (store.BotRealizedPnL + GetOpenPositions, no account filter); the
// trades/profit legs are account-scoped (GetSessionDayActivity with the active
// account). Printed so the owner can see the difference.
func DailyLossScopeBootLine(activeAccount string) string {
	acct := strings.TrimSpace(activeAccount)
	if acct == "" {
		acct = "n/a (not bound yet — resolved per evaluation)"
	}
	return fmt.Sprintf("daily-loss gate scopes: loss leg = trader-wide (bot P&L realized+open, every account) · trades/profit legs = account-scoped (active account %s)", acct)
}

// DailyLossGateBootLineForStrategy is the per-trader posture line: what the
// gate does for THIS strategy, every field READ from the resolver the gate
// runs. It cannot see a latch — DailyLossGateBootLine is the load line.
func DailyLossGateBootLineForStrategy(cfg *store.StrategyConfig) string {
	if cfg == nil {
		return "daily-loss gate: n/a (no strategy config)"
	}
	gp := kernel.ResolveStrategyGuardrails(cfg.RiskControl, kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD)
	enforced, posture, trades := dailyLossGatePosture(cfg)
	if !enforced {
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
	Enforced    bool    `json:"enforced"`
	LimitUSD    float64 `json:"limit_usd"`
	Latched     bool    `json:"latched"`
	LatchReason string  `json:"latch_reason"`
	// RT-2 (release #24): a latch outlives switching the gate OFF, so
	// enforced=false with latched=true is a real state. LatchDay is the CME
	// session-day it holds for, LatchSource "live" or the persisted copy a
	// restart restores, LatchLifts the lift rule. All "" when not latched.
	LatchDay       string   `json:"latch_day"`
	LatchSource    string   `json:"latch_source"`
	LatchLifts     string   `json:"latch_lifts"`
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
	s.LatchDay, s.LatchReason, s.LatchSource = DailyLossLatchStateAt(at.store, at.id, now)
	s.Latched = s.LatchDay != ""
	if s.Latched {
		s.LatchLifts = DailyLossLatchLifts
	}
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
