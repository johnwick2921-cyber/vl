package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"nofx/telemetry"
)

// P2 — THE CLOCK. Bar-close cadence + the skip-while-open gate. Both are GATED on
// day_plan being enabled for a futures trader, so the default (crypto / plan-off)
// keeps the scan-timer loop and same-side-refusal behavior byte-identical —
// additive + dormant until the owner arms day_plan at ★ RESTART 1.

// dayPlanEnabled is the shared activation for every P2 clock behavior: a futures
// (NinjaTrader) trader whose strategy has day_plan enabled. Default false → the
// whole clock is dormant and crypto/plan-off behavior is byte-identical.
func (at *AutoTrader) dayPlanEnabled() bool {
	if at.exchange != "ninjatrader" || at.config.StrategyConfig == nil {
		return false
	}
	dp := at.config.StrategyConfig.DayPlan
	return dp != nil && dp.PlanEnabled
}

// barCloseCadenceActive reports whether this trader should fire cycles on
// primary-TF bar closes instead of the scan timer.
// P10 note: since the owner ruling (2026-08-19) this only ARMS the cadence
// machinery; the MODE below decides whether the bar-close gate actually gates.
func (at *AutoTrader) barCloseCadenceActive() bool { return at.dayPlanEnabled() }

// Cadence modes (P10 — OWNER RULING 2026-08-19): the Studio scan interval is
// the ACTUAL decision cadence. "interval" (default) runs a full cycle every
// scheduler tick on the LATEST bar state including the forming primary-TF bar;
// "bar_close" is the legacy day-plan P2 gate (one cycle per closed primary
// bar), selectable per-trader in Studio, stored in traders.cadence_mode.
const (
	CadenceInterval = "interval"
	CadenceBarClose = "bar_close"
)

// cadenceMode resolves the trader's mode: explicit "bar_close" keeps the
// legacy gate; everything else (empty, "interval", garbage) is interval — the
// P10 default. Garbage never invents the stricter gate silently.
func (at *AutoTrader) cadenceMode() string {
	if at.config.CadenceMode == CadenceBarClose {
		return CadenceBarClose
	}
	return CadenceInterval
}

// skipNoNewData (P10.4) is the ONLY allowed interval-mode skip besides the
// existing gates: when a tick finds a byte-identical world — same newest
// primary-TF bar (open/high/low/close/volume, forming or not), and FLAT — the
// cycle is skipped with a logged reason instead of burning a paid AI call on
// an identical snapshot. Any bar mutation (a forming bar's close/volume moves
// on every real tick) or an open position runs the cycle.
func (at *AutoTrader) skipNoNewData(now time.Time) bool {
	if market.FuturesBarsProvider == nil {
		return false // no bar state to compare — never invent a skip
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), at.primaryTimeframe(), 1)
	if len(bars) == 0 {
		return false
	}
	b := bars[len(bars)-1]
	sig := fmt.Sprintf("%d|%.4f|%.4f|%.4f|%.4f|%.4f", b.OpenTime, b.Open, b.High, b.Low, b.Close, b.Volume)
	if sig != at.lastCycleBarSig {
		at.lastCycleBarSig = sig
		return false
	}
	// Identical bar state — only skip when flat (in-position cycles are the
	// dashboard heartbeat and must keep running; PR #49 contract).
	if at.store != nil {
		if positions, err := at.store.Position().GetOpenPositions(at.id); err == nil && len(positions) > 0 {
			return false
		}
	}
	at.logInfof("⏭ cycle_skip=no_new_data — newest %s bar unchanged since the last cycle and flat; not burning an AI call on an identical snapshot. (%s)",
		at.primaryTimeframe(), kernel.FormatCT(now))
	return true
}

// evaluateWallClockSessionReads (class 32, 2026-08-31) runs the scheduled
// session reads on wall-clock, independent of bar arrival. It is invoked at the
// TOP of tickOnce, BEFORE the bar-close gate and the no-new-data dedup, so a
// quiet tape or the 16:00-17:00 CME halt can never delay a scheduled read.
// When a read starts while the market is halted, it logs the halt-fired line
// with the stored-bar age (4.3) so wall-clock authoring is never mistaken for a
// data bug. Halt-fired authoring from last stored bars is RULED CORRECT (4.2):
// a plan is structure judged on its facts snapshot; staleness at trade time is
// the executor guards' job (stale_reeval, gate-at-arm, dormant/rearm).
func (at *AutoTrader) evaluateWallClockSessionReads() {
	now := traderNow()
	fired := at.maybeRunSessionReadsAt(now)
	if len(fired) == 0 {
		return
	}
	if kernel.IsCMEOpen(now) {
		return // live tape — the ordinary path, no halt line
	}
	newestCT, ageMin, have := newestStoredBarInfo(at.futuresSymbol(), at.primaryTimeframe(), now)
	for _, f := range fired {
		at.logInfof(haltSessionReadLine(f.Session, at.primaryTimeframe(), now, newestCT, ageMin, have))
	}
}

// evaluateWallClockWeeklyRead (CLASS 36, 2026-09-01) — the Sunday 16:30 CT
// weekly read is WALL-CLOCK work like the session reads: it used to live
// inside runCycle (auto_trader_loop.go), behind the bar-close gate and the
// no-new-data dedup that class 32 hoisted the session reads above — so it was
// data-gated on a closed market (31 minutes late on 2026-08-30). It runs
// BEFORE evaluateWallClockSessionReads so the Sunday sequence holds: the weekly
// doc lands, then sundayAsiaDeferred (unchanged) lets the ASIA read follow.
// maybeRunWeeklyRead is idempotent (skip-fresh + in-flight claim) and reads
// STORED 1m bars — it has no freshness preflight to bypass.
func (at *AutoTrader) evaluateWallClockWeeklyRead() {
	at.maybeRunWeeklyRead(traderNow())
}

// newestStoredBarInfo reports the newest stored bar for the halt-fired log line.
func newestStoredBarInfo(symbol, tf string, now time.Time) (newestCT string, ageMin int, have bool) {
	if market.FuturesBarsProvider == nil {
		return "", 0, false
	}
	bars := market.FuturesBarsProvider(symbol, tf, 1)
	if len(bars) == 0 {
		return "", 0, false
	}
	b := bars[len(bars)-1]
	return kernel.FormatCT(time.UnixMilli(b.OpenTime)), int(now.Sub(time.UnixMilli(b.OpenTime)).Minutes()), true
}

// haltSessionReadLine is the pure log-line builder for a read fired during a
// CME halt (4.3) — fixture-tested for content and age math.
func haltSessionReadLine(session, tf string, now time.Time, newestCT string, ageMin int, have bool) string {
	if !have {
		return fmt.Sprintf("🗓 session read fired during halt (%s) — authoring with NO stored bars (bar cache empty)", session)
	}
	return fmt.Sprintf("🗓 session read fired during halt (%s) — authoring from last stored bars (newest %s %s, age %dm)", session, tf, newestCT, ageMin)
}

// skipWhileOpen (P2.2) reports whether the AI decision cycle should be skipped
// because the strategy is already holding a position — calmer and cheaper than
// per-decision same-side refusal. GATED on day_plan → dormant by default. The
// held trade is still managed independently: the NT8 OCO bracket, auto-breakeven,
// and close-sync/reconcile all run outside the decision cycle, so exits and flips
// (bracket/target/stop) are unaffected.
func (at *AutoTrader) skipWhileOpen() (bool, string) {
	if !at.dayPlanEnabled() || at.store == nil {
		return false, ""
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil || len(positions) == 0 {
		return false, ""
	}
	// P0 hotfix (2026-08-19) — the gate never trusts a stale local row against
	// live NT8 truth: bracket exits land in the store via the reconcile "sync"
	// path (≤~80s), and during that window this row is a phantom. On desync
	// (broker flat, store open): CRITICAL + no skip; the reconciler keeps
	// closing authority. See position_desync.go. POSITION_RECONCILE default on.
	if at.skipGateDesync(positions) {
		return false, ""
	}
	p := positions[0]
	return true, fmt.Sprintf("%d open (%s %s)", len(positions), p.Symbol, p.Side)
}

// primaryTimeframe is the strategy's primary bar interval (default 5m).
func (at *AutoTrader) primaryTimeframe() string {
	if at.config.StrategyConfig == nil {
		return "5m"
	}
	tf := at.config.StrategyConfig.Indicators.Klines.PrimaryTimeframe
	if tf == "" {
		return "5m"
	}
	return tf
}

func (at *AutoTrader) futuresSymbol() string {
	s := at.config.NinjaTraderSymbol
	if s == "" {
		return "MNQ"
	}
	return s
}

// sessionChainDate is the plan-chain identity date for a session: the CT date
// of the session INSTANCE the moment belongs to (P0-B), falling back to the
// midnight-roll planner date when the session has no parseable window.
func sessionChainDate(sess *kernel.SessionDef, now time.Time) string {
	if d, ok := kernel.PlanChainTradeDate(sess, now); ok {
		return d
	}
	return plannerTradeDateCT(now)
}

// latestClosedPrimaryBarMs returns the CloseTime (ms) of the most recent CLOSED
// primary-TF bar, ok=false when none is available (provider down / warming).
func (at *AutoTrader) latestClosedPrimaryBarMs() (int64, bool) {
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), at.primaryTimeframe(), 5)
	nowMs := time.Now().UnixMilli()
	var latest int64
	for i := range bars {
		if bars[i].CloseTime < nowMs && bars[i].CloseTime > latest {
			latest = bars[i].CloseTime
		}
	}
	if latest == 0 {
		return 0, false
	}
	return latest, true
}

// barCloseGate decides whether a tick should run a decision cycle under bar-close
// cadence, and returns the updated last-close watermark. When cadence is not
// active it ALWAYS runs (scan-timer behavior unchanged). When active it runs only
// once per NEW primary-TF bar close — never mid-bar, and a restart resumes on the
// next close.
func barCloseGate(active bool, lastCloseMs, latestClosedMs int64, haveBar bool) (run bool, newLastMs int64) {
	if !active {
		return true, lastCloseMs
	}
	if !haveBar || latestClosedMs <= lastCloseMs {
		return false, lastCloseMs
	}
	return true, latestClosedMs
}

// ---- P2.3 — day-trader clock: last_entry + eod_flat ------------------------

// hhmmToMin parses "HH:MM" (24h) into minutes-since-midnight; ok=false on bad input.
func hhmmToMin(s string) (int, bool) {
	var h, m int
	n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m)
	if err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// ctMinutesNow returns now's minutes-since-midnight in America/Chicago.
func ctMinutesNow(now time.Time) int {
	loc := kernel.CTLocation()
	ct := now.In(loc)
	return ct.Hour()*60 + ct.Minute()
}

// timeReachedCT reports whether now's CT wall-clock is at/after hhmm. Correct for
// all-day-after-threshold gates (last-entry, eod-flat) — NOT for the planner read,
// which must fire only inside its session's window (see inSessionReadWindow).
func timeReachedCT(now time.Time, hhmm string) bool {
	target, ok := hhmmToMin(hhmm)
	if !ok {
		return false
	}
	return ctMinutesNow(now) >= target
}

// inSessionReadWindow (W1) reports whether now (CT) is inside a session's read
// window [ReadCT, WindowEndCT) — wrap-aware for midnight-spanning sessions (ASIA
// 16:55→02:00). This is the correct "should the planner read THIS session now"
// gate: it fires at the session's read time and stays valid through the session,
// but NEVER during another session's hours. This kills the spurious Sunday-17:00
// NY read (17:00 is past NY's 15:00 window end, so NY never fires there), so
// Monday's plan is built at the real 08:25 read, not from stale Sunday-evening data.
func inSessionReadWindow(now time.Time, readCT, windowEndCT string) bool {
	read, ok1 := hhmmToMin(readCT)
	end, ok2 := hhmmToMin(windowEndCT)
	if !ok1 || !ok2 {
		return false
	}
	n := ctMinutesNow(now)
	if end > read {
		return n >= read && n < end // same-day window (NY 08:25→15:00)
	}
	return n >= read || n < end // wraps midnight (ASIA 16:55→02:00)
}

// inDailyRollWindow (W1) reports whether now (CT) is in the trade-date roll-up
// window [15:00, 16:00) — after the RTH close, BEFORE the 16:00 CME maintenance
// break/17:00 roll. The current trade_date + P&L window are still the CLOSING
// day's here (they roll at 17:00), so the daily digest summarizes the RIGHT day;
// and it is reachable Mon–FRI (the old >=16:00 trigger fell inside the break, so
// it fired at 17:00+ with the NEW day's empty window, and Friday's never fired).
func inDailyRollWindow(now time.Time) bool {
	n := ctMinutesNow(now)
	return n >= 15*60 && n < 16*60
}

// effectiveEODFlatCT returns the flat time, pulled IN by a registered half-day
// early-close for the current CME session-day (holiday/half-day awareness via the
// P0 registry, which the P1.8 calendar populates). Empty HalfDays → configFlat.
// P4 note (2026-08-19): the live flatten now resolves through halfDayCutoffMin
// (early_close − offset); this zero-offset form is kept for its tests/API.
func effectiveEODFlatCT(reg kernel.SessionRegistry, sessionDayKey, configFlat string) string {
	if early, ok := reg.HalfDays[sessionDayKey]; ok && strings.TrimSpace(early) != "" {
		em, ok1 := hhmmToMin(early)
		cm, ok2 := hhmmToMin(configFlat)
		if ok1 && (!ok2 || em < cm) {
			return early // half-day closes earlier → pull the flat in
		}
	}
	return configFlat
}

// lastEntryCT — the LEGACY day-scoped cutoff. UNREACHABLE since the P2
// session-scope redesign (2026-08-18): entryBlockedByLastEntry resolves the
// cutoff per session via sessionCutoffCT below. Kept only so an old
// dp.LastEntryCT config value is visible to a reader; nothing evaluates it.
// See sessionCutoffCT + entryBlockedByLastEntry for the live path.
func (at *AutoTrader) lastEntryCT() string {
	if dp := at.config.StrategyConfig.DayPlan; dp != nil && strings.TrimSpace(dp.LastEntryCT) != "" {
		return dp.LastEntryCT
	}
	return "13:00" // 14:00 ET (NY-only era)
}

// sessionCutoffCT resolves "session end − offsetMin" as CT wall-clock minutes
// and as an "HH:MM" string, wrap-aware for midnight-spanning sessions (ASIA
// 17:00→02:00, offset 15 → 01:45). ok=false on malformed registry times.
func sessionCutoffCT(sess *kernel.SessionDef, offsetMin int) (cutoffMin int, hhmm string, ok bool) {
	endMin, okE := hhmmToMin(sess.WindowEndCT)
	if !okE {
		return 0, "", false
	}
	cutoffMin = ((endMin-offsetMin)%1440 + 1440) % 1440
	return cutoffMin, fmt.Sprintf("%02d:%02d", cutoffMin/60, cutoffMin%60), true
}

// pastSessionCutoff reports whether now (CT) is INSIDE the session window and
// at/after the session-relative cutoff. Comparison is done in minutes-since-
// session-start so a midnight wrap cannot invert it. Outside the window it is
// always false — the session-open gate, not last-entry, owns that refusal.
func pastSessionCutoff(now time.Time, sess *kernel.SessionDef, cutoffMin int) bool {
	startMin, okS := hhmmToMin(sess.WindowStartCT)
	endMin, okE := hhmmToMin(sess.WindowEndCT)
	if !okS || !okE {
		return false
	}
	sessLen := ((endMin-startMin)%1440 + 1440) % 1440
	nowOff := ((ctMinutesNow(now)-startMin)%1440 + 1440) % 1440
	cutOff := ((cutoffMin-startMin)%1440 + 1440) % 1440
	return nowOff < sessLen && nowOff >= cutOff
}

// eodFlatCT — LEGACY day-scoped flat time. UNREACHABLE since the session-scope
// redesign (2026-08-18); enforceEODFlatAt resolves per session. Kept for
// visibility of an old dp.EODFlatCT config value only.
func (at *AutoTrader) eodFlatCT() string {
	if dp := at.config.StrategyConfig.DayPlan; dp != nil && strings.TrimSpace(dp.EODFlatCT) != "" {
		return dp.EODFlatCT
	}
	return "14:45" // 15:45 ET
}

// entryBlockedByLastEntry (P2.3, session-scoped 2026-08-18) reports (reason,
// blocked): whether NEW entries are blocked because the ACTIVE session's
// last-entry cutoff (session end − last_entry_offset_min, America/Chicago,
// DST-correct via kernel.CTLocation) has passed. Gated on day_plan.
//
// THE BUG THIS REPLACES (incident B, zero-trade cause): the old body was
// timeReachedCT(now, "13:00") — a DAY-scoped comparison that stayed true from
// 13:00 CT to midnight, so the single NY-era cutoff refused every Asia-evening
// entry (live refusal observed at ~21:00 CT). Defect classes 2 (day-scope where
// session-scope required) and 3 (the 13:00 literal shadowing per-session
// config). The old lastEntryCT path is now unreachable from any gate.
//
// Outside every session window this gate never fires — the session gate owns
// that refusal — and the message names the session + resolved time.
func (at *AutoTrader) entryBlockedByLastEntry() (string, bool) {
	return at.entryBlockedByLastEntryAt(time.Now())
}

// entryBlockedByLastEntryAt is the injectable-clock body, so the T1–T4 table
// tests can pin real CT instants (including a DST-transition date).
func (at *AutoTrader) entryBlockedByLastEntryAt(now time.Time) (string, bool) {
	if !at.dayPlanEnabled() {
		return "", false
	}
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok {
		return "", false // no session → the session-open gate is the refusal
	}
	offset := at.config.StrategyConfig.DayPlan.LastEntryOffsetFor(sess.Name)
	cutoffMin, hhmm, okC := sessionCutoffCT(sess, offset)
	if !okC {
		return "", false // malformed registry times — never invent a block
	}
	// P4 (ledger-close 2026-08-19) — a registered half-day pulls the LAST-ENTRY
	// cutoff in too: the cutoff resolves against early_close_CT − offset when
	// that is earlier (min semantics, same raw-minute convention as the flat
	// pull-in). Sessions that end before the early close are unaffected.
	if adj, adjHHMM, okH := halfDayCutoffMin(at.sessionRegistry(now), kernel.CMESessionDayKey(now), offset); okH && halfDayPullsIn(sess, adj, cutoffMin) {
		cutoffMin, hhmm = adj, adjHHMM
	}
	if pastSessionCutoff(now, sess, cutoffMin) {
		return fmt.Sprintf("past last-entry %s CT (%s)", hhmm, sess.Name), true
	}
	return "", false
}

// halfDayCutoffMin resolves "early_close_CT − offset" for the session-day's
// registered half-day (P4). ok=false when no half-day / unparseable value.
func halfDayCutoffMin(reg kernel.SessionRegistry, sessionDayKey string, offsetMin int) (int, string, bool) {
	early, ok := reg.HalfDays[sessionDayKey]
	if !ok || strings.TrimSpace(early) == "" {
		return 0, "", false
	}
	em, okE := hhmmToMin(early)
	if !okE {
		return 0, "", false // fail-safe: garbage half-day never invents a cutoff
	}
	adj := ((em-offsetMin)%1440 + 1440) % 1440
	return adj, fmt.Sprintf("%02d:%02d", adj/60, adj%60), true
}

// halfDayPullsIn (E7-v2 fix) decides whether the half-day cutoff REPLACES the
// session-resolved one — compared in SESSION-RELATIVE minutes, wrap-safe. The
// raw-minute `adj < cutoff` compare could (a) map an out-of-window early close
// into a bogus earlier cutoff that pastSessionCutoff can never fire (silently
// REMOVING the original protection on custom registries) and (b) invert
// ordering across midnight-wrapped sessions. An early close outside this
// session's window truncates nothing → no pull-in (the dispatch 4.4 contract:
// only the session(s) the early close truncates move).
func halfDayPullsIn(sess *kernel.SessionDef, adjMin, cutoffMin int) bool {
	startMin, okS := hhmmToMin(sess.WindowStartCT)
	endMin, okE := hhmmToMin(sess.WindowEndCT)
	if !okS || !okE {
		return false
	}
	sessLen := ((endMin-startMin)%1440 + 1440) % 1440
	relAdj := ((adjMin-startMin)%1440 + 1440) % 1440
	relCut := ((cutoffMin-startMin)%1440 + 1440) % 1440
	return relAdj < sessLen && relAdj < relCut
}

// enforceEODFlat (P2.3, session-scoped 2026-08-18) force-flattens any open
// position at/after the ACTIVE session's flat time (session end −
// eod_flat_offset_min, half-day pull-in preserved) by routing DIRECTLY through
// the trader close path — bypassing hold-lock naturally (RECON #10). Returns
// true when it acted (the caller then skips the rest of the cycle). Gated on
// day_plan.
//
// THE TWIN BUG (class 7 of the last-entry fix): the old body was
// timeReachedCT(now, "14:45") — day-scoped, true 14:45 CT → midnight — which
// never mattered while the 13:00 last-entry bug guaranteed no Asia positions,
// but the moment Asia entries flow it would flatten each one ON SIGHT at, say,
// 21:00 CT. Fixing last-entry without this would have been worse than fixing
// neither.
//
// Scope: INSIDE the active session, flatten past its resolved flat time.
// BETWEEN sessions (the 14:45→17:00 CT gap, weekends) any open position is
// flattened too — that is by definition past the previous session's flat, and
// it preserves the old rule's one virtue: nothing rides through the close.
func (at *AutoTrader) enforceEODFlat() bool {
	return at.enforceEODFlatAt(time.Now())
}

func (at *AutoTrader) enforceEODFlatAt(now time.Time) bool {
	if !at.dayPlanEnabled() || at.store == nil || at.trader == nil {
		return false
	}
	reg := at.sessionRegistry(now)
	flat := "" // resolved wall-clock, for the log only
	if sess, ok := reg.ActiveSession(now); ok {
		offset := at.config.StrategyConfig.DayPlan.EODFlatOffsetFor(sess.Name)
		flatMin, hhmm, okC := sessionCutoffCT(sess, offset)
		if !okC {
			return false // malformed registry times — never invent a flatten
		}
		// Half-day early close pulls the flat IN. P4 (ledger-close 2026-08-19):
		// the flat now resolves against early_close_CT − eod_flat_offset (the
		// dispatch 4.4 contract) — with the default offset 0 this is byte-
		// identical to the original effectiveEODFlatCT pull-in.
		if adj, adjHHMM, okH := halfDayCutoffMin(reg, kernel.CMESessionDayKey(now), offset); okH && halfDayPullsIn(sess, adj, flatMin) {
			flatMin, hhmm = adj, adjHHMM
		}
		if !pastSessionCutoff(now, sess, flatMin) {
			return false
		}
		flat = fmt.Sprintf("%s CT (%s)", hhmm, sess.Name)
	} else {
		// No active session: past every session's flat by definition.
		flat = "no active session"
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil || len(positions) == 0 {
		return false
	}
	// S-LIST CLOSER (2026-08-27) — cancel ALL resting arms FIRST, synchronously
	// (ack-waited, one retry), THEN flatten. This kills the ≤2m window where a
	// working limit could fill after the flat (deep-verify hole 11). The same
	// ordering guards the session-end branch (no active session above), the T1
	// force-flat, and the dormancy path in maybeManageArmedOrders.
	n, unacked := at.cancelArmedOrdersSync("session close — EOD flat")
	if n > 0 {
		at.logWarnf("🔒 EOD-FLAT: %d armed order(s) cancelled before flattening", n)
	}
	if unacked > 0 {
		at.logWarnf("⚠️ EOD-FLAT: %d armed cancel(s) unacked after retry — flattening anyway (wire reconciles next cycle)", unacked)
	}
	// A fill that won the race mid-cancel must be flattened too — re-read.
	if fresh, err2 := at.store.Position().GetOpenPositions(at.id); err2 == nil {
		positions = fresh
	}
	at.logWarnf("🕒 EOD-FLAT (%s): session close — flattening %d open position(s) via the trader close path.", flat, len(positions))
	for _, p := range positions {
		at.flattenPosition(p, "🕒 EOD-FLAT")
	}
	return true
}

// t1ForceFlatLead is how many minutes BEFORE a T1 (red-news) blackout starts
// that existing positions are force-closed (research v5 C.5: FOMC/NFP positions
// forced closed at T-2 min; the blackout itself starts T1BlackoutMinutes before
// the event, so closing at blackout−2min is strictly earlier = safer).
const t1ForceFlatLead = 2

// limitClosePrice computes the favorable-side limit exit price for the 4.3
// limit-then-market flatten: LONG exits at close + ticks*tick, SHORT at
// close − ticks*tick. Returns 0 when the inputs can't produce a sane price.
func limitClosePrice(close float64, ticks int, tick float64, long bool) float64 {
	if ticks <= 0 || tick <= 0 || close <= 0 {
		return 0
	}
	if long {
		return close + float64(ticks)*tick
	}
	return close - float64(ticks)*tick
}

// flattenPosition (4.3) closes ONE open position. With the dormant default
// (LimitCloseTicks 0) it is byte-identical to the historical market flatten:
// market close + bracket cancel. When enabled it FIRST places a limit exit
// LimitCloseTicks beyond the latest 1m bar close (favorable side; entry-price
// fallback when bars are unavailable), KEEPS the protective bracket during the
// limit's life (the C# cancels it on the limit fill), and schedules a market
// fallback after LimitCloseMarketAfterS — the fallback re-checks the
// open-position table, so a filled limit no-ops.
func (at *AutoTrader) flattenPosition(p *store.TraderPosition, tag string) {
	side := "LONG"
	if !strings.EqualFold(p.Side, "LONG") {
		side = "SHORT"
	}
	closeMarket := func() bool {
		var e error
		if side == "LONG" {
			_, e = at.trader.CloseLong(p.Symbol, 0) // 0 = close all
		} else {
			_, e = at.trader.CloseShort(p.Symbol, 0)
		}
		if e != nil {
			at.logErrorf("%s: close %s %s failed: %v", tag, p.Symbol, p.Side, e)
			return false
		}
		return true
	}

	ticks := at.config.LimitCloseTicks
	if ticks <= 0 {
		// Dormant default — byte-identical to the historical flatten.
		if closeMarket() {
			if err := at.trader.CancelStopOrders(p.Symbol); err != nil {
				at.logWarnf("%s: cancel bracket %s failed (non-fatal): %v", tag, p.Symbol, err)
			}
		}
		return
	}

	// Limit-then-market (active only when EOD_FLAT_LIMIT_TICKS > 0).
	ref := 0.0
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(p.Symbol, "1m", 3); len(bars) > 0 {
			ref = bars[len(bars)-1].Close
		}
	}
	if ref <= 0 {
		ref = p.EntryPrice
	}
	limit := limitClosePrice(ref, ticks, market.FuturesTickSize(p.Symbol), side == "LONG")
	type limitCloser interface {
		CloseWithLimit(symbol, side string, quantity float64, limitPrice float64) (map[string]interface{}, error)
	}
	lc, ok := at.trader.(limitCloser)
	if !ok || limit <= 0 {
		// No limit support or no sane reference — market close, bracket cancel.
		if closeMarket() {
			_ = at.trader.CancelStopOrders(p.Symbol)
		}
		return
	}
	if _, err := lc.CloseWithLimit(p.Symbol, side, 0, limit); err != nil {
		at.logWarnf("%s: limit close %s %s failed (%v) — market fallback now", tag, p.Symbol, p.Side, err)
		closeMarket()
		_ = at.trader.CancelStopOrders(p.Symbol)
		return
	}
	at.logInfof("%s: limit exit submitted %s %s @ %.2f (market fallback in %ds)",
		tag, p.Symbol, side, limit, at.config.LimitCloseMarketAfterS)
	after := time.Duration(at.config.LimitCloseMarketAfterS) * time.Second
	if after <= 0 {
		after = 10 * time.Second
	}
	sym, sd := p.Symbol, side
	time.AfterFunc(after, func() {
		if at.store == nil {
			return
		}
		if open, err := at.store.Position().GetOpenPositions(at.id); err == nil {
			for _, po := range open {
				if market.Normalize(po.Symbol) == market.Normalize(sym) && strings.EqualFold(po.Side, sd) {
					at.logWarnf("%s: limit unfilled after %ds — market flatten %s %s", tag, int(after.Seconds()), sym, sd)
					if sd == "LONG" {
						_, _ = at.trader.CloseLong(po.Symbol, 0)
					} else {
						_, _ = at.trader.CloseShort(po.Symbol, 0)
					}
					_ = at.trader.CancelStopOrders(po.Symbol)
					return
				}
			}
		}
	})
}

// t1ForceFlatDue reports whether nowMin (CT minute-of-day) falls inside
// [window.Start − lead, window.End] for any T1 window, returning the matching
// label ("" = not due). Wrap-aware, same range math as kernel.InT1Blackout.
func t1ForceFlatDue(nowMin int, windows []kernel.CTWindow, lead int) string {
	for _, w := range windows {
		start := (w.Start - lead + 1440) % 1440
		in := false
		if w.End >= start {
			in = nowMin >= start && nowMin <= w.End
		} else {
			in = nowMin >= start || nowMin <= w.End
		}
		if in {
			return w.Label
		}
	}
	return ""
}

// enforceT1ForceFlat closes any open position from t1ForceFlatLead minutes
// before each T1 blackout window through the end of that window. Same shape as
// enforceEODFlatAt: gated dormant (day_plan), never invents a flatten outside
// an active session, close is retried each cycle while positions remain open
// (GetOpenPositions only returns open ones, so retries are naturally
// idempotent). Windows come from currentT1Windows — so the fail-closed static
// fallback protects this path too.
func (at *AutoTrader) enforceT1ForceFlat() bool {
	return at.enforceT1ForceFlatAt(time.Now())
}

func (at *AutoTrader) enforceT1ForceFlatAt(now time.Time) bool {
	if !at.dayPlanEnabled() || at.store == nil || at.trader == nil {
		return false
	}
	if _, ok := at.sessionRegistry(now).ActiveSession(now); !ok {
		return false // no active session — never flatten from an invented clock
	}
	windows := at.currentT1Windows(now)
	if len(windows) == 0 {
		return false
	}
	due := t1ForceFlatDue(ctMinutesNow(now), windows, t1ForceFlatLead)
	if due == "" {
		return false
	}
	// NEWS-HYGIENE (2026-08-29) — cancel FIRST, BEFORE the open-position
	// check: a FLAT trader with a working armed limit must not let that
	// limit fill into the print. The pre-wave order only cancelled when a
	// position existed, so a resting limit survived the whole window.
	n, unacked := at.cancelArmedOrdersSync("news_window")
	if n > 0 {
		at.logWarnf("🔒 T1-FORCE-FLAT: %d armed order(s) cancelled before the red-news window", n)
	}
	if unacked > 0 {
		at.logWarnf("⚠️ T1-FORCE-FLAT: %d armed cancel(s) unacked after retry", unacked)
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil || len(positions) == 0 {
		return n+unacked > 0 // flat — the arm-cancel above is the whole job
	}
	at.logWarnf("📰 T1-FORCE-FLAT (%s): flattening %d open position(s) — red-news forced close at T-%dmin (research v5 C.5).", due, len(positions), t1ForceFlatLead)
	for _, p := range positions {
		at.flattenPosition(p, "📰 T1-FORCE-FLAT")
	}
	return true
}

// recordExcursionForClosedSymbol (P2.4) processes the just-closed trade for the
// AI-emitted-close path. W5 — the SAME analytics also fire from the loop poll for
// real exits (NT8 OCO / EOD-flat / manual), so no exit path is missed.
func (at *AutoTrader) recordExcursionForClosedSymbol(symbol string) {
	if !at.dayPlanEnabled() || at.store == nil {
		return
	}
	closed, err := at.store.Position().GetClosedPositions(at.id, 1)
	if err != nil || len(closed) == 0 {
		return
	}
	if p := closed[0]; p.Symbol == symbol {
		at.recordClosedTradeAnalytics(p)
	}
}

// recordClosedTradeAnalytics (W5) computes MAE/MFE + the A–F adherence grade +
// the matched-random reaction verdict for ONE closed trade. Idempotent (a graded
// row is skipped), so the AI-close call and the loop poll never double-process.
// Gated on day_plan → dormant for crypto / plan-off.
func (at *AutoTrader) recordClosedTradeAnalytics(p *store.TraderPosition) {
	if p == nil || p.AdherenceGrade != "" { // already processed
		return
	}
	if !at.dayPlanEnabled() || at.store == nil || market.FuturesBarsProvider == nil {
		return
	}
	if p.EntryPrice <= 0 || p.EntryTime <= 0 {
		return
	}
	exitMs := p.ExitTime
	if exitMs <= 0 {
		exitMs = time.Now().UnixMilli()
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", kernel.AISVPBarCount)
	// E4 (wave 1A, 2026-09-02) — the same path computation the excursion table
	// uses, so trader_positions.mae/mfe and trade_excursions cannot disagree.
	// It INCLUDES the bar containing the fill; ComputeExcursion dropped it
	// whenever a fill did not land exactly on a bar boundary.
	path := kernel.ComputePathExcursion(p.EntryPrice, p.Side, 0, 0, bars, p.EntryTime, exitMs, "1m")
	if !path.Computed {
		// No coverage. The columns stay NULL: an uncomputed excursion is not a
		// zero, and writing 0 here is what made 517 closed rows unreadable.
		at.logWarnf("📐 excursion for %s pos=%d has no 1m coverage — mae/mfe left NULL, not zeroed", p.Symbol, p.ID)
	} else if err := at.store.Position().UpdateExcursion(p.ID, path.MAEPts, path.MFEPts); err != nil {
		at.logWarnf("📐 excursion update failed for %s: %v", p.Symbol, err)
		return
	}
	ex := kernel.Excursion{MAE: path.MAEPts, MFE: path.MFEPts}
	// E3 (wave 1A) — the excursion row's exit half. This funnel is reached by
	// EVERY exit path (AI close, NT8 OCO, EOD-flat, manual), which is why the
	// hook lives here and not in one of them.
	at.excursionOnClose(p)

	// P5.5 — ADHERENCE GRADE (A–F), separate from P&L.
	inKZ, inNoTrade := kernel.SessionWindowFacts(at.sessionRegistry(time.Now()), time.UnixMilli(p.EntryTime))
	grade, _ := kernel.GradeAdherence(kernel.AdherenceInput{
		Cited:      p.CitedScenarioID != "",
		Matched:    p.PlanMatched,
		OffPlan:    p.CitedScenarioID == "",
		InNoTrade:  inNoTrade,
		InKillzone: inKZ,
		Band:       p.PlanBand, // B3 (F6): forward-only — legacy rows carry ""
	})
	if err := at.store.Position().SetAdherence(p.ID, grade); err != nil {
		at.logWarnf("🎓 adherence grade update failed for %s: %v", p.Symbol, err)
		return
	}
	at.logInfof("📐🎓 %s close: MAE %.2f / MFE %.2f pts · adherence %s (%s) cited=%q matched=%v",
		p.Symbol, ex.MAE, ex.MFE, grade, kernel.AdherenceLabel(grade), p.CitedScenarioID, p.PlanMatched)

	// W6 — P0 close+P&L alert.
	at.emitAlert("P0", "close", fmt.Sprintf("close:%d", p.ID),
		fmt.Sprintf("Closed %s — P&L %.2f", p.Symbol, p.EffectivePnL()),
		fmt.Sprintf("adherence %s (%s)", grade, kernel.AdherenceLabel(grade)))

	// P5.6 — matched-random reaction verdict for the traded level type.
	at.recordMatchedRandomForClose(p, ex)

	// P0 pnl-record-integrity (2026-08-20) — the class-killer: every close is
	// verified recorded-vs-recomputed (stored prices × ROW qty × point value);
	// any delta > $0.50 WARNs to log_events + dashboard. Wrong PnL can never
	// again sit silent (the #526 −$1,458-on-a-−$69 trade class).
	if pv := market.FuturesPointValue(p.Symbol); pv > 0 && p.EntryPrice > 0 && p.ExitPrice > 0 {
		pts := p.ExitPrice - p.EntryPrice
		if p.Side == "SHORT" {
			pts = -pts
		}
		recomputed := pts * p.Quantity * pv
		if d := p.EffectivePnL() - recomputed; d > 0.5 || d < -0.5 {
			at.logWarnf("⚖️ pnl_integrity MISMATCH on close #%d: recorded %.2f vs recomputed %.2f (Δ %+.2f) — stored prices × row qty disagree with the recorded P&L; investigate the writer path.",
				p.ID, p.EffectivePnL(), recomputed, d)
			telemetry.IncGateBlock(at.id, "pnl_integrity_mismatch")
		}
	}

	// Phase 3.6 (final-bundle) — watcher scoring backfill: stamp this position's
	// watch assessments with the final outcome and, best-effort, the excursion
	// AFTER each read (1m bars may have rotated out of the cache — rows keep 0).
	outcome := "win"
	if p.EffectivePnL() < 0 {
		outcome = "loss"
	}
	_ = at.store.WatchAssessment().BackfillClose(p.ID, fmt.Sprintf("%s:%+.2f", outcome, p.EffectivePnL()))
	if rows, err := at.store.WatchAssessment().ByPosition(p.ID); err == nil && len(rows) > 0 && len(bars) > 0 {
		for _, row := range rows {
			if row.MFEAfterPts != 0 || row.MAEAfterPts != 0 {
				continue // idempotent
			}
			exAfter := kernel.ComputeExcursion(row.PriceAtRead, p.Side, bars, row.Timestamp, exitMs)
			_ = at.store.WatchAssessment().SetExcursionAfter(row.ID, exAfter.MFE, exAfter.MAE)
		}
	}
}

// maybeRecordClosedTradeAnalytics (W5) is the LOOP POLL that catches trades closed
// on the REAL exit paths (NT8 OCO SL/TP, EOD-flat, manual) — none of which run the
// AI decision cycle. It grades every ungraded close since the analytics epoch
// (set on first run so pre-existing history is ignored). Idempotent via the grade
// flag; gated on day_plan.
func (at *AutoTrader) maybeRecordClosedTradeAnalytics() {
	if !at.dayPlanEnabled() || at.store == nil {
		return
	}
	const key = "dayplan_analytics_since"
	sinceStr, _ := at.store.GetSystemConfig(key)
	if sinceStr == "" {
		// First run: stamp the epoch + skip all pre-existing closes (not day-plan trades).
		_ = at.store.SetSystemConfig(key, strconv.FormatInt(time.Now().UnixMilli(), 10))
		return
	}
	sinceMs, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		return
	}
	rows, err := at.store.Position().GetUngradedClosedPositions(at.id, sinceMs, 20)
	if err != nil {
		return
	}
	for _, p := range rows {
		at.recordClosedTradeAnalytics(p)
	}
}

// tickOnce runs one loop iteration: a grid cycle, or (for AI strategies) a
// decision cycle gated by bar-close cadence.
func (at *AutoTrader) tickOnce(isGrid bool) {
	if isGrid {
		if err := at.RunGridCycle(); err != nil {
			at.logErrorf("❌ Grid execution failed: %v", err)
		}
		return
	}
	// CLASS 32 (owner ruling 2026-08-31) — scheduled session reads are
	// WALL-CLOCK work, evaluated on EVERY tick BEFORE the data-gated skips.
	// The bar-close gate and the no-new-data dedup idle the whole tick for
	// minutes-to-hours whenever the tape is quiet (tonight the 16:00-17:00
	// CME halt froze the bars, and the 16:30 ASIA read sat behind
	// cycle_skip=no_new_data from 16:26 until the ~17:00:03 reopen tick —
	// 30 minutes late, no error, no alarm, no plan at the open). A
	// scheduled read must never inherit the market's calendar.
	at.evaluateWallClockWeeklyRead() // CLASS 36 — weekly first (Sunday: weekly lands → ASIA follows)
	at.evaluateWallClockSessionReads()
	// U2 (watcher-eyes hotfix): a post_exit kick is a PROMISED immediate rescan
	// — it bypasses the bar-close gate and the no-new-data dedup exactly once
	// (the dodge bypass rides skipDodgeOnce, set together in noteKick).
	bypassCadence := at.skipCadenceOnce
	at.skipCadenceOnce = false
	active := at.barCloseCadenceActive()
	// P10 (owner ruling 2026-08-19): the bar-close gate is a MODE now, not a
	// hard gate. mode=bar_close keeps the legacy behavior byte-identical (E14);
	// mode=interval (default) runs a full cycle every tick on the latest bar
	// state — forming bar included — subject only to the no-new-data dedup.
	if !bypassCadence && active && at.cadenceMode() == CadenceBarClose {
		latest, have := at.latestClosedPrimaryBarMs()
		run, newLast := barCloseGate(true, at.lastBarCloseMs, latest, have)
		at.lastBarCloseMs = newLast
		if !run {
			return // bar-close mode: no new primary-TF bar closed → idle this tick
		}
	}
	if !bypassCadence && active && at.cadenceMode() == CadenceInterval && at.skipNoNewData(traderNow()) {
		return
	}
	// Discard-burn 2.1 — DODGE: starting a cycle just before the decision-TF's
	// close is a near-guaranteed supersession discard (the AI call spans the
	// close). Defer the start to close+1s instead of burning the call. A cycle
	// kicked BY the dodge runs immediately (skipDodgeOnce) so it can never
	// re-defer at the next boundary. Futures-only (crypto never discards).
	if at.skipDodgeOnce {
		at.skipDodgeOnce = false
	} else if at.exchange == "ninjatrader" && staleDodgeEnabled() {
		if deferMs, dodge := at.staleDodgeCheck(time.Now()); dodge {
			if at.scheduleKick(time.Duration(deferMs)*time.Millisecond, "stale_dodge") {
				at.logInfof("⏳ stale_dodge deferred_ms=%d — cycle start within avg_call×%.1f of the %s close; deferring to close+1s (avg_call=%dms over last %d)",
					deferMs, staleDodgeSafetyFactor, at.primaryTimeframe(), at.avgAICallMs(), at.aiCallN)
				return
			}
		}
	}
	if err := at.runCycle(); err != nil {
		at.logErrorf("❌ Execution failed: %v", err)
	}
}
