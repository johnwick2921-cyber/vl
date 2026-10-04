package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── MENTOR P3 — the cycle hook ─────────────────────────────────────────────
//
// One evaluator tick per NEW 1m close. Inert unless the per-strategy
// mentor_mode is ON. Placements stay behind the MENTOR_PLACE env gate
// (owner step; the former P1 #309 was split into PRs #312/#313) — until it
// is set the hook sizes and LOGS/COUNTS every intent (the size audit the
// spec demands) and places NOTHING (L4, default OFF).

// mentorPlaceEnv resolves the placement gate (env MENTOR_PLACE, default OFF).
func mentorPlaceEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MENTOR_PLACE"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// MentorPlacementEnabled reports the MENTOR_PLACE placement gate (read-only; the
// Studio status line reads it — a name and a flag, never a secret).
func MentorPlacementEnabled() bool { return mentorPlaceEnv() }

// mentorBars1mDepth is the 1m history depth the mentor fetch asks for (P0 A6
// routing, CTO 1791058624275): the §7 Globex run window (17:00→08:30 CT) is
// 930 bars and the full RTH day to 15:00 CT is 1320 — 1500 covers both with
// slack. The old 4-hour ask started mid-window: past the 08:30 freeze the
// run window held ZERO of those bars and the day gate read DayNotMeasured
// every day, so the bot never traded.
const mentorBars1mDepth = 1500

// mentorTick runs the evaluator on the latest 1m bars (the 2-minute scan's
// fallback — the PRIMARY path is the event pass on every FINAL 1m bar). It
// never touches the wire unless mentor mode AND MENTOR_PLACE are both on.
func (at *AutoTrader) mentorTick(ctx *kernel.Context) {
	if !at.mentorEnabled() {
		return
	}
	if market.FuturesBarsProvider == nil {
		return
	}
	bars := mentorClosedBars(market.FuturesBarsProvider("MNQ", "1m", mentorBars1mDepth))
	if len(bars) == 0 {
		return
	}
	// Same once-per-bar watermark as the event pass: Tick is not idempotent
	// per bar (a repeat tick re-emits intents and double-counts refusals).
	if bars[len(bars)-1].OpenTime <= at.mentorLastTickOpen {
		return
	}
	at.mentorEvalOnce(bars)
}

// mentorClosedBars drops the FORMING tail bar. The NT8 cache's newest bar is
// usually still building (trader/ninjatrader/bars_market_bridge.go
// barsToKlines). Evaluating it reads a half-built candle as the current one,
// and the mentorLastTickOpen dedup then skips that bar's FINAL for good, so the
// bar is never evaluated closed. A bar is closed when NT8 marked it Final OR a
// newer bar exists (NT8 has started the next one, so this one is complete even
// if its Final frame was lost). The bot's wall clock is deliberately NOT used:
// a clock ahead of NT8's, or a Final frame that lags, would let an unfinished
// bar through and stamp the watermark on it.
func mentorClosedBars(bars []market.Kline) []market.Kline {
	if n := len(bars); n > 0 && !bars[n-1].Final {
		return bars[:n-1]
	}
	return bars
}

// noteLiveBarsForMentorPass is the sink's mentor half: a FINAL 1m/5m frame
// stamps the arrival time and kicks the event loop for a mentor-mode trader
// (event-driven, not scan-driven).
func (at *AutoTrader) noteLiveBarsForMentorPass(symbol, tf string, bars []ntwire.Bar, receivedAt time.Time) {
	if at == nil || len(bars) == 0 || !at.mentorEnabled() {
		return
	}
	if tf != "1m" && tf != "5m" {
		return
	}
	root := instrumentRoot(symbol)
	if root == "" || root != instrumentRoot(at.futuresSymbol()) {
		return
	}
	final := false
	for _, b := range bars {
		if b.Final {
			final = true
			break
		}
	}
	if !final {
		return
	}
	at.mentorFinalArrival.Store(receivedAt.UnixMilli())
	if l := at.armedEvent.Load(); l != nil {
		l.poke()
	}
}

// mentorEventPassAt runs one mentor event pass (the event loop calls it after
// the armed pass; ≤1/s by the loop's own gap). Returns whether it ran.
func (at *AutoTrader) mentorEventPassAt(now time.Time) bool {
	if at == nil || !at.mentorEnabled() {
		return false
	}
	if market.FuturesBarsProvider == nil {
		return false
	}
	bars := mentorClosedBars(market.FuturesBarsProvider("MNQ", "1m", mentorBars1mDepth))
	if len(bars) == 0 {
		return false
	}
	if bars[len(bars)-1].OpenTime <= at.mentorLastTickOpen {
		return false // already evaluated this bar (the scan or an earlier pass)
	}
	at.mentorEvalOnce(bars)
	return true
}

// mentorEvaluatorConfig applies strategy overrides to the evaluator. B22's
// structural departure remains primary; the fixed-points fallback is opt-in.
func (at *AutoTrader) mentorEvaluatorConfig() mentor.Config {
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	rc := at.mentorRiskControl()
	cfg.LossDeparturePts = mentorLossDeparturePts(rc)
	// The resolvers are nil-safe and return the ruled default for an unset field.
	cfg.LegBudgetEnabled = mentorLegBudgetEnabled(rc)
	cfg.LegResetOn = mentorLegResetOn(rc)
	cfg.LocTriggerFilter = mentorLocationTriggerFilter(rc)
	if rc != nil {
		cfg.LvlRevisitMinPts = mentorLvlRevisitMinPts(rc)
		cfg.EmaMaxCross30m = mentorEmaMaxCross30m(rc)
	}
	return cfg
}

// mentorEvalOnce runs one evaluator tick over the bars and processes every
// intent (size → latency → no-chase → place-or-hold).
func (at *AutoTrader) mentorEvalOnce(bars []market.Kline) {
	last := bars[len(bars)-1]
	at.mentorLastTickOpen = last.OpenTime

	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	emitMs := time.Now().UnixMilli()
	// The evaluator's clock is the instant the last bar CLOSED: every closedness
	// test inside Tick then reads the just-closed bar as closed, never as forming
	// (an OpenTime clock made the ORB escape unreachable and lagged every gate).
	intents := at.mentorEval.Tick(bars, mentor.BarCloseInstant(last))
	// The seed depth moves with the bars: re-check after every tick, so a
	// source that was short at boot (94/102 closed 4h candles) clears on its
	// own instead of needing a restart.
	at.mentorRefreshDepths()
	// S9 (D5.2 p2 @05:21): strong-day detection from the recent CLOSED 5m bars
	// — 50–80 pt candles cut every tier to 1–2.
	strongDay := false
	if bars5 := mentorClosedBars(market.FuturesBarsProvider("MNQ", "5m", 12)); len(bars5) > 0 && mentorStrongDayFrom5m(bars5) {
		strongDay = true
	}
	for _, in := range intents {
		// B20 trader half (CTO 1791058836784, FIXES.md B20): the trigger
		// LATER flipped to the trade's side — the OPEN position upgrades
		// to confluence: exit C (hold ≥ 1:2, stop untouched), size
		// UNCHANGED (the table is never re-run for an upgrade). The emit
		// itself is DS-103's (kernel/mentor is his). It rides its own path:
		// the dispatch below sizes and places ENTRIES only.
		if in.Action == mentor.ConfluenceUpgrade {
			mentorCount("intent_" + string(in.Action))
			at.mentorConfluenceUpgrade(in)
			continue
		}
		// A5: the spent-day flag rides the intent (stamped by the
		// evaluator); confluence comes from the R2 stub seam.
		extra := mentorExtraFor(in, strongDay)
		// P0-b ONE entry path: the injector's dispatch (ledger-routed,
		// ALWAYS stop-limit) handles PlaceStopEntry/PlaceStopLimitEntry and
		// every arm action; an unknown action is refused, never silent.
		at.mentorDispatchIntent(in, extra, last.CloseTime, emitMs)
	}
	// N12 funnel visibility (read-only): one closed bar + this tick's intents.
	at.mentorFunnelTick(1, len(intents))
}

// mentorPlaceIntent runs the placement gates (sources wired, stop rules,
// expiry guard, no-chase, the exit fork) and hands the intent to the ONE
// mentor entry path (mentorDirectPlace): a ledger arm with origin=mentor and
// the intent's expiry, placed ALWAYS stop-limit, never stop-market.
// The no-chase rule runs FIRST: a stop entry whose price is already through
// the trigger is skipped — he never enters at market (§3).
func (at *AutoTrader) mentorPlaceIntent(in mentor.Intent, choice mentorSizeChoice, barCloseMs, emitMs int64) {
	// N10 (stale intent): an entry whose reference candle is not the newest
	// closed bar came from a reload replay of stale box/ISB/PHL state — never
	// a live order. The swing is exempt (RefBarMs 0 — gated by its own 5m
	// watermark). Counted, never silent.
	if in.RefBarMs != 0 && in.RefBarMs != barCloseMs {
		mentorCount("stale_intent")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — stale %s intent: reference bar close %d ≠ newest closed bar %d", in.Setup, in.RefBarMs, barCloseMs)
		return
	}
	// WIRING PROOF (fail-closed): with any mentor source seam missing, EVERY
	// entry refuses here — the boot line logs it, this line enforces it.
	if missing := at.mentorSourcesBlocking(in.Setup); len(missing) > 0 {
		mentorCount("mentor_sources_missing")
		at.logErrorf("🧑‍🏫 mentor sources MISSING [%s] — refusing the %s entry (fail-closed)", strings.Join(missing, ", "), in.Setup)
		return
	}
	// STOP RULES (owner ruling 00:1x CT, "exactly like he said") — class
	// rules, default ON: (b) the trading window (swing exempt), (a) done for
	// the day after a win, F11 news 07:30, (d) never add/average. Then the
	// no-chase rule.
	if refuse, why := at.mentorWindowGate(in); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		return
	}
	if refuse, why := at.mentorDoneAfterWinGate(); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		return
	}
	if hold, why := at.mentorNewsGate(); hold {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		return
	}
	if refuse, why := at.mentorAddGate(in); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		return
	}
	// N12 per-order expiry (PR #313): the stop-limit is cancelled when
	// unfilled at its expiry. An intent-carried expiry (evaluator rules,
	// stacking extensions) wins; otherwise the injector sets the setup's
	// default.
	if in.ExpiryMs == 0 {
		in.ExpiryMs = mentorIntentExpiry(in, barCloseMs)
		mentorCount("expiry_defaulted")
	}
	// F3 (CTO 1791035117415): every mentor arm must carry an expiry — an arm
	// without one is REFUSED fail-closed (it must never sit unexpiring).
	if refuse, why := mentorExpiryGuard(in.ExpiryMs); refuse {
		mentorCount("expiry_missing_refused")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		return
	}
	if latest, ok := at.mentorLatestPrice(); ok {
		if skip, why := mentorNoChase(in.Side, latest, in.Price); skip {
			mentorCount("no_chase_skip")
			at.logWarnf("🧑‍🏫 mentor NO-CHASE skip: %s", why)
			return
		}
	}
	// A/B/C EXIT FORK AT ENTRY (CTO 1791029620038): the branch is chosen from
	// the intent flags NOW and recorded; the P1 exit loop drives the chosen
	// branch per fill. A PHL/PLH starts as B with the resonance watch armed.
	forkMode, forkTP, forkWhy := mentorExitFork(in, mentorConfluenceFlag(in))
	mentorCount("exit_fork_" + forkMode)
	at.logInfof("🧑‍🏫 mentor exit fork: %s — %s (leg 1 TP %.2f)", forkMode, forkWhy, forkTP)
	// B20: the chosen branch is REGISTERED per open position — a later
	// confluence upgrade switches it to C (hold ≥ 1:2, size untouched).
	at.setMentorExitMode(strings.ToLower(string(in.Side)), forkMode)
	if mentorPlaceRecorderForTest != nil {
		mentorPlaceRecorderForTest(in, choice.Contracts)
		return // test seam: the real pipeline is never reached from a test
	}
	at.mentorArmIntent(in, choice, barCloseMs, emitMs)
}

// mentorExpiryGuard is the F3 fail-closed pin (CTO 1791035117415): a mentor
// stop-limit arm MUST carry an expiry. Zero/negative refuses with a named
// reason — an unexpiring mentor arm must never sit at the broker.
func mentorExpiryGuard(expiryMs int64) (refuse bool, why string) {
	if expiryMs <= 0 {
		return true, "F3: a mentor stop-limit arm without an expiry is refused — the injector always stamps ExpiryMs"
	}
	return false, ""
}

// mentorSetArmExpiryWire is the F3 binding seam (CTO 1791035117415): the
// mentor injector must call SetArmExpiry with the intent's ExpiryMs when the
// arm is created. Production wiring binds it to store.SetArmExpiry (dev via
// #313). nil → nothing is stamped (the frame does not exist).
var mentorSetArmExpiryWire func(armID int64, expiryMs int64) error

// mentorIntentExpiry resolves the N12 per-order expiry (PR #313): the expiry
// belongs to the RULES, not a blanket timer. An intent-carried expiry wins;
// otherwise the injector computes the setup's default — a level touch or a
// single ISB expires at the close of the NEXT 1m candle; the swing lives until
// the close of the current 4h candle (mentorSwingExpiry).
func mentorIntentExpiry(in mentor.Intent, barCloseMs int64) int64 {
	if in.ExpiryMs > 0 {
		return in.ExpiryMs
	}
	if strings.EqualFold(in.Setup, "SWING4H") {
		return mentorSwingExpiry(barCloseMs + 1) // +1: the instant the bar closed, so a 4h-boundary bar reads the NEW 4h candle
	}
	return barCloseMs + 60_000 // the close of the NEXT 1m candle
}

// mentorSwingExpiry (RULING [C], knob swing_order_expiry): a swing stop order
// lives until the CLOSE of the current 4h candle (17:00 CT anchor). At that
// close the line re-bases, and any unfilled swing order is cancelled
// [D5.2 p3 @12:30]. The 4h candles chain from the 17:00 CT anchor.
func mentorSwingExpiry(nowMs int64) int64 {
	loc := kernel.CTLocation()
	now := time.UnixMilli(nowMs).In(loc)
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, loc)
	if now.Before(anchor) {
		anchor = anchor.AddDate(0, 0, -1) // before 17:00 CT → the chain started yesterday
	}
	const fourH = 4 * time.Hour
	k := now.Sub(anchor) / fourH
	return anchor.Add(fourH * (k + 1)).UnixMilli()
}

// mentorLatestPrice is the latest live price for the no-chase check. The
// default reads the newest cached 1m bar's close (the freshest tape we have
// without a dedicated tick accessor); tests substitute their own source.
var mentorLatestPriceSource func() (float64, bool)

func (at *AutoTrader) mentorLatestPrice() (float64, bool) {
	if mentorLatestPriceSource != nil {
		return mentorLatestPriceSource()
	}
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider("MNQ", "1m", 1)
	if len(bars) == 0 {
		return 0, false
	}
	return bars[len(bars)-1].Close, true
}

// mentorNoChase is the pure no-chase rule: a stop entry whose trigger is
// already AT OR BEYOND the live price must be skipped — a buy stop below the
// market (or a sell stop above it) fills like a market order, and he never
// enters at market (§3).
func mentorNoChase(side mentor.Side, latest float64, trigger float64) (skip bool, why string) {
	switch side {
	case mentor.SideLong:
		if latest >= trigger {
			return true, fmt.Sprintf("price %.2f already at/beyond the buy stop entry %.2f — no chase [§3]", latest, trigger)
		}
	case mentor.SideShort:
		if latest <= trigger {
			return true, fmt.Sprintf("price %.2f already at/beyond the sell stop entry %.2f — no chase [§3]", latest, trigger)
		}
	}
	return false, ""
}

// mentorPlaceRecorderForTest is a test seam called right before the real
// placement: the no-chase mutant (dropping the check) makes it fire on a
// through-price intent and the test goes RED.
var mentorPlaceRecorderForTest func(in mentor.Intent, contracts int)

// ── B20 CONFLUENCE UPGRADE (trader half; the emit is DS-103's kernel) ───────

// mentorExitMode / setMentorExitMode read/write the per-position exit branch
// registered at placement (A/B/C/swing). The P1 exit loop drives the branch;
// B20 switches it to C.
func (at *AutoTrader) mentorExitMode(key string) string {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	return at.mentorExitModes[key]
}

func (at *AutoTrader) setMentorExitMode(key, mode string) {
	at.mentorExitMu.Lock()
	defer at.mentorExitMu.Unlock()
	if at.mentorExitModes == nil {
		at.mentorExitModes = map[string]string{}
	}
	at.mentorExitModes[key] = mode
}

// mentorConfluenceUpgradeMode is the pure B20 switch: an upgrade moves the
// open position's exit branch to C — hold ≥ 1:2, the stop untouched. The SIZE
// is never re-read (the upgrade never touches the size table). A no-position
// and the swing (which holds by the 4h, not the 5m trigger) are no-ops — the
// kernel only emits for live school-1 entries, but the switch fails closed
// anyway.
func mentorConfluenceUpgradeMode(current string) (mode string, why string) {
	switch current {
	case "":
		return "", "no open mentor position — the upgrade names nothing"
	case "swing":
		return "swing", "the swing holds by the 4h — B20 upgrades school-1 entries only"
	case "C":
		return "C", "already confluence — hold ≥ 1:2, nothing to switch"
	default:
		return "C", fmt.Sprintf("exit %s → C: hold ≥ 1:2, stop untouched, size unchanged [D3.4 p3 @09:17–12:59]", current)
	}
}

// mentorConfluenceUpgrade applies the B20 switch to the OPEN position named by
// the intent's side: the exit branch moves to C (hold ≥ 1:2), the stop and the
// size stay exactly as placed. A no-position or already-C intent is counted
// and logged, never silent.
func (at *AutoTrader) mentorConfluenceUpgrade(in mentor.Intent) {
	key := strings.ToLower(string(in.Side))
	next, why := mentorConfluenceUpgradeMode(at.mentorExitMode(key))
	if next == "" {
		mentorCount("exit_upgrade_no_position")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade IGNORED — %s (%s)", why, in.Reason)
		return
	}
	if next == at.mentorExitMode(key) {
		mentorCount("exit_upgrade_noop")
		at.logInfof("🧑‍🏫 mentor confluence upgrade: %s (%s) — %s", key, why, in.Reason)
		return
	}
	at.setMentorExitMode(key, next)
	mentorCount("exit_upgrade_c")
	at.logInfof("🧑‍🏫 mentor confluence upgrade: %s %s (size unchanged) — %s", key, why, in.Reason)
}

// ── STRONG DAY (S9) ────────────────────────────────────────────────────────

const mentorStrongDayPtsMin = 50.0 // a 5m candle range ≥50 pts = the 50–80 band

// mentorStrongDayFrom5m reports a strong day: a closed 5m candle in the window
// ran 50–80 pts [D5.2 p2 @05:21] (anything wider is certainly strong — the 80
// is the band's top as quoted, not a cutoff).
func mentorStrongDayFrom5m(bars []market.Kline) bool {
	for _, b := range bars {
		if b.High-b.Low >= mentorStrongDayPtsMin {
			return true
		}
	}
	return false
}

// ── NEWS 07:30 CT (F11/R12) ────────────────────────────────────────────────

// The 07:30 CT CPI/PPI/Unemployment print: NO resting order through it. The
// hold window is the print minute −10m (no order may still be resting INTO the
// print) to +5m (let the print shake out) — both named rule parameters.
const (
	mentorNewsPreWindow  = 10 * time.Minute
	mentorNewsPostWindow = 5 * time.Minute
)

// mentorNewsPrintTitleTokens matches the BLS 07:30 CT prints the rule names.
var mentorNewsPrintTitleTokens = []string{"cpi", "ppi", "unemployment"}

// mentorNewsWindowActive reports whether `now` is inside the 07:20–07:35 CT
// print window (pure).
func mentorNewsWindowActive(now time.Time) bool {
	loc := kernel.CTLocation()
	ct := now.In(loc)
	printAt := time.Date(ct.Year(), ct.Month(), ct.Day(), 7, 30, 0, 0, loc)
	return !now.Before(printAt.Add(-mentorNewsPreWindow)) && now.Before(printAt.Add(mentorNewsPostWindow))
}

// mentorNewsHold is the pure gate: with the day's calendar events, reports
// whether a placement at `now` would rest through a 07:30 CT T1 CPI/PPI/
// Unemployment print.
func mentorNewsHold(events []calendar.Event, now time.Time) (hold bool, why string) {
	if !mentorNewsWindowActive(now) {
		return false, ""
	}
	for _, e := range events {
		if e.Impact != calendar.T1 || kernel.CloseHHMMCT(e.Time) != "07:30" {
			continue
		}
		title := strings.ToLower(e.Title)
		for _, tok := range mentorNewsPrintTitleTokens {
			if strings.Contains(title, tok) {
				return true, fmt.Sprintf("news: %s prints 07:30 CT — no resting order through the print [F11]", e.Title)
			}
		}
	}
	return false, ""
}

// mentorNowSource is the clock seam for every mentor time gate (tests).
var mentorNowSource func() time.Time

// mentorClockNow is the one clock read for the time gates.
func mentorClockNow() time.Time {
	if mentorNowSource != nil {
		return mentorNowSource()
	}
	return time.Now()
}

// mentorDayEventsForTest is the events seam (tests; nil → the stored slice).
var mentorDayEventsForTest func() ([]calendar.Event, bool)

// mentorDayEvents returns today's stored calendar events. ok=false means the
// slice is missing or unreadable — the news gate then holds the window
// FAIL-CLOSED (a missing calendar is not proof that there is no CPI).
func (at *AutoTrader) mentorDayEvents() ([]calendar.Event, bool) {
	if mentorDayEventsForTest != nil {
		return mentorDayEventsForTest()
	}
	if at.store == nil {
		return nil, false
	}
	slice, err := at.store.Calendar().GetSlice(plannerTradeDateCT(time.Now()))
	if err != nil || slice == nil {
		return nil, false
	}
	var evs []calendar.Event
	if json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil {
		return nil, false
	}
	return evs, true
}

// mentorNewsGate is the call-site half of F11: refuse a placement inside the
// 07:30 print window on a print day; with no readable calendar, hold the
// window anyway (fail-closed), counted news_hold_no_calendar.
func (at *AutoTrader) mentorNewsGate() (bool, string) {
	now := mentorClockNow()
	evs, ok := at.mentorDayEvents()
	if !ok {
		if mentorNewsWindowActive(now) {
			mentorCount("news_hold_no_calendar")
			at.logWarnf("🧑‍🏫 news: calendar slice missing/unreadable — holding the 07:20–07:35 CT window fail-closed [F11]")
			return true, "news: calendar slice missing/unreadable — the 07:20–07:35 CT window holds (fail-closed) [F11]"
		}
		return false, ""
	}
	hold, why := mentorNewsHold(evs, now)
	if hold {
		mentorCount("news_hold")
	}
	return hold, why
}

// ── STOP RULES (owner ruling 00:1x CT, "exactly like he said") ─────────────

// (b) TRADING WINDOW [D1.2 p1 @23:52–24:59]: a fixed window — when it ends, no
// new entries. Default 08:30–09:30 CT, 60 minutes; knobs for the start and the
// length (30/60/90/120; -1 disables the window — a stored 0 is "unset", so it
// resolves to the default 60). The SWING setup is exempt
// (D5.2: the swing may be at any hour).
const (
	mentorWindowDefaultStart   = "08:30"
	mentorWindowDefaultMinutes = 60
)

func (at *AutoTrader) mentorWindowKnobs() (start string, minutes int) {
	start, minutes = mentorWindowDefaultStart, mentorWindowDefaultMinutes
	if at.config.StrategyConfig == nil {
		return
	}
	rc := at.config.StrategyConfig.RiskControl
	if v := strings.TrimSpace(rc.MentorWindowStart); v != "" {
		start = v
	}
	if rc.MentorWindowMinutes != 0 {
		minutes = rc.MentorWindowMinutes
	}
	return
}

// mentorWindowActive is the pure window check: now inside [start, start+len)
// CT, where start is the most recent occurrence of the start time at or before
// now (today's, or yesterday's when today's is still ahead) — so a window that
// crosses midnight (23:00 + 120) is active at 00:30. minutes <= 0 disables the
// window (entries at any hour); the knob stores -1 for that, because a stored 0
// means "unset → default 60" (mentorWindowKnobs). A bad start string refuses
// fail-closed (why carries the refusal).
func mentorWindowActive(start string, minutes int, now time.Time) (active bool, why string) {
	if minutes <= 0 {
		return true, "" // the window is disabled
	}
	hour, minute, ok := parseMentorWindowStart(start)
	if !ok {
		return false, fmt.Sprintf("trading window start %q unparseable — entries refused (fail-closed) [D1.2 p1 @23:52]", start)
	}
	loc := kernel.CTLocation()
	ct := now.In(loc)
	open := time.Date(ct.Year(), ct.Month(), ct.Day(), hour, minute, 0, 0, loc)
	if open.After(now) {
		open = time.Date(ct.Year(), ct.Month(), ct.Day()-1, hour, minute, 0, 0, loc)
	}
	end := open.Add(time.Duration(minutes) * time.Minute)
	if !now.Before(open) && now.Before(end) {
		return true, ""
	}
	return false, fmt.Sprintf("outside the trading window %s–%s CT — no new entries [D1.2 p1 @23:52–24:59]", kernel.CloseHHMMCT(open), kernel.CloseHHMMCT(end))
}

func parseMentorWindowStart(start string) (hour, minute int, ok bool) {
	if len(start) != 5 || start[2] != ':' {
		return 0, 0, false
	}
	for i, r := range start {
		if i != 2 && (r < '0' || r > '9') {
			return 0, 0, false
		}
	}
	hour, err := strconv.Atoi(start[:2])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, false
	}
	minute, err = strconv.Atoi(start[3:])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// mentorWindowGate is the call-site half of (b); SWING4H is exempt.
func (at *AutoTrader) mentorWindowGate(in mentor.Intent) (bool, string) {
	if strings.EqualFold(in.Setup, "SWING4H") {
		return false, "" // D5.2: the swing may be at any hour
	}
	start, minutes := at.mentorWindowKnobs()
	active, why := mentorWindowActive(start, minutes, mentorClockNow())
	if active {
		return false, ""
	}
	mentorCount("window_refused")
	return true, why
}

// (a) DONE FOR THE DAY AFTER A WIN [D1.2 p1 @20:53–21:16; p2 @05:28–06:27]:
// once a trade closes in profit and the day's net P&L is above 0 → no new
// mentor entries until the next trading day (17:00 CT).
func mentorDoneAfterWin(dayNetPnl float64, closedInProfit bool) bool {
	return closedInProfit && dayNetPnl > 0
}

// mentorDayNetSource / mentorClosedProfitSource are the session seams for (a)
// (nil → no data → the gate stays open; the live driver fills them at P1).
var (
	mentorDayNetSource       func() float64
	mentorClosedProfitSource func() bool
)

// mentorDoneAfterWinGate is the call-site half of (a); the knob is default ON
// (nil → ON, an explicit false disables). FAIL-CLOSED: a missing day-net or
// closed-profit source refuses the entry — an unknown is not "no win".
func (at *AutoTrader) mentorDoneAfterWinGate() (bool, string) {
	if at.config.StrategyConfig != nil {
		if v := at.config.StrategyConfig.RiskControl.MentorDoneAfterWin; v != nil && !*v {
			return false, "" // the knob is explicitly OFF
		}
	}
	if mentorDayNetSource == nil || mentorClosedProfitSource == nil {
		mentorCount("done_after_win_no_data")
		at.logWarnf("🧑‍🏫 mentor done-after-win source missing (day net / closed profit) — refusing the entry (fail-closed)")
		return true, "done-after-win: day P&L or closed-trade source not wired — an unknown is not 'no win'; refusing (fail-closed) [D1.2 p1 @20:53–21:16]"
	}
	net := mentorDayNetSource()
	closed := mentorClosedProfitSource()
	if mentorDoneAfterWin(net, closed) {
		mentorCount("done_after_win_refused")
		return true, "done for the day after a win — a trade closed in profit and the day is net positive; no new entries until the next trading day (17:00 CT) [D1.2 p1 @20:53–21:16]"
	}
	return false, ""
}

// (d) NEVER ADD / AVERAGE [D1.1 p1 @17:06–17:44]: no second same-direction
// fill while a position is open. The resonance ISB is a hold signal, not an
// entry.

// mentorOpenSideSource is the session seam for (d): the side of the open
// mentor position (nil → none open; the live driver fills it at P1).
var mentorOpenSideSource func() string

// mentorAddGate is the call-site half of (d). FAIL-CLOSED: with no open-side
// source the entry refuses — an unknown open side could hide a same-direction
// add.
func (at *AutoTrader) mentorAddGate(in mentor.Intent) (bool, string) {
	if mentorOpenSideSource == nil {
		mentorCount("add_no_source")
		return true, "never-add guard: open-position source not wired — refusing the entry (fail-closed) [D1.1 p1 @17:06–17:44]"
	}
	open := mentorOpenSideSource()
	if open == "" {
		return false, ""
	}
	if strings.EqualFold(open, string(in.Side)) {
		mentorCount("add_refused")
		return true, fmt.Sprintf("never add/average [D1.1 p1 @17:06–17:44]: %s already open — the resonance ISB is a hold signal, not an entry", open)
	}
	return false, ""
}

// mentorMin4hEMA34 is the ABSOLUTE minimum 4h-candle floor: 34 is the EMA's
// FIRST value, not a warmed-up one.
const mentorMin4hEMA34 = 34

// mentorDefaultEMA34Warmup is the SAFE default warm-up (3×34): a seed that
// never calls SetMentor4hEMA34Warmup must not trade on a barely-formed EMA.
const mentorDefaultEMA34Warmup = 102

// mentor4hEMA34Warmup is the 4h EMA 34 floor the seed actually requires —
// DS-103's stated warm-up (his P0 PR). The default is the safe 3×34; the live
// driver changes it via SetMentor4hEMA34Warmup.
var mentor4hEMA34Warmup = mentorDefaultEMA34Warmup

// mentor1mEMA34Warmup is the 1m EMA 34 floor — the same safe default (3×34).
var mentor1mEMA34Warmup = mentorDefaultEMA34Warmup

// SetMentor4hEMA34Warmup is DS-103's seed hook: the stated 4h EMA 34 warm-up
// in 4h candles. Values below the absolute minimum are clamped to it.
func SetMentor4hEMA34Warmup(n int) {
	if n < mentorMin4hEMA34 {
		n = mentorMin4hEMA34
	}
	mentor4hEMA34Warmup = n
}

// mentorDepthRequirement is one per-source history floor (P0 seed plan point
// 1): depth per source, not one number — the refusal names the short source.
type mentorDepthRequirement struct {
	Name string // named in the refusal and the boot line
	Min  int    // minimum depth in the source's own bars (presence for 1)
}

// mentorDepthRequirements lists every source the seed must provide. DS-103's
// PR states the exact warm-up per source; these floors are the fail-closed
// gate (the 4h EMA 34 floor is mentor4hEMA34Warmup — the seed's stated
// warm-up, never below the absolute minimum 34 — derived from 1h at the
// 17:00 CT anchor; the 1m EMA 34 floor is mentor1mEMA34Warmup; the 1H RTH level
// set needs at least 2 candles (one colour change) and reads the FULL stored
// history — no cap; today's session feeds the boxes/ORB/day latch; the 15m
// source must have a closed candle).
var mentorDepthRequirements = []mentorDepthRequirement{
	{Name: "4h EMA34", Min: mentorMin4hEMA34},
	{Name: "1m EMA34", Min: 34},
	{Name: "1h level set", Min: 2},
	{Name: "today session", Min: 1},
	{Name: "closed 15m", Min: 1},
}

// mentorSourceDepthSource reports one source's seeded depth (names as in
// mentorDepthRequirements). nil or !known → the boot line prints n/a and the
// gate refuses (fail-closed). The live driver sets it from DS-103's seed.
var mentorSourceDepthSource func(name string) (depth int, known bool)

// mentorSourceDepth asks the seam, with a 1m BarCache fallback for the 1m
// source. Unknown means the seed has not provided it yet.
func mentorSourceDepth(name string) (int, bool) {
	if mentorSourceDepthSource != nil {
		return mentorSourceDepthSource(name)
	}
	if name == "1m EMA34" && market.FuturesBarsProvider != nil {
		return len(market.FuturesBarsProvider("MNQ", "1m", 34)), true
	}
	return 0, false
}

// ── P0 SPLICE — seed the evaluator at trader start ─────────────────────────
//
// The CTO splice (mail 1791030462901): at trader start, load the STORED 1m+1h
// bars over the read-only store path the bot already uses (data.db is never
// written by the seed), call mentor.Seed, wire the per-source depth seam from
// SeedDepths, print the seed boot line, and refuse every mentor entry while
// anything is missing (the evaluator refuses internally; the injector's
// per-source loop names the short source on the same numbers).

const (
	// mentorSeedBars1mN caps the store read. Retention bounds what the read
	// can return (1m 90d); the cap only bounds memory. Far above every seed
	// floor (102 closed 4h candles ≈ 17 days of 1m bars ≈ 24500 rows).
	mentorSeedBars1mN = 50000
)

// mentorSeedDepths is the per-source depth snapshot (nil until the splice
// runs): the seeded numbers SeedLine prints, then advanced after every
// evaluator tick (mentorRefreshDepths) so a source that was short at boot
// clears without a restart. Guarded by mentorSeedDepthsMu — the seam is read
// from the placement path while the tick path writes it.
var (
	mentorSeedDepths   map[string]int
	mentorSeedDepthsMu sync.RWMutex
)

// mentorRefreshDepths advances the depth snapshot from the evaluator's live
// depth (never lowering a number) and logs the ONE "depth met" line when the
// evaluator stops refusing entries. A nil snapshot (no splice ran: tests with
// a stub seam) is left alone.
func (at *AutoTrader) mentorRefreshDepths() {
	if at == nil || at.mentorEval == nil {
		return
	}
	if line := at.mentorEval.TakeDepthMet(); line != "" {
		at.logInfof("🧑‍🏫 %s", line)
	}
	live := at.mentorEval.Depths()
	if live == nil {
		return
	}
	mentorSeedDepthsMu.Lock()
	defer mentorSeedDepthsMu.Unlock()
	if mentorSeedDepths == nil {
		return
	}
	for name, d := range live {
		if d > mentorSeedDepths[name] {
			mentorSeedDepths[name] = d
		}
	}
}

// storeBarsToKlines converts persisted closed bars to market.Kline (CloseTime
// = open + tf − 1, the live-bar convention; every stored row is a CLOSED bar
// by the persistence contract). open + tf would make a seeded candle "close"
// exactly at the next candle's open, so the 1H body-cross deletion would also
// test the candle BEFORE a level's own candle.
func storeBarsToKlines(rows []store.BarHistoryDB, tfMs int64) []market.Kline {
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{
			OpenTime:  r.OpenTimeMs,
			Open:      r.O,
			High:      r.H,
			Low:       r.L,
			Close:     r.C,
			Volume:    r.V,
			CloseTime: r.OpenTimeMs + tfMs - 1,
		})
	}
	return out
}

// mentorSeedAtStart is the splice entry: called once at trader construction
// when mentor_mode is ON. A cold store (no rows) or a read error seeds with
// nothing and refuses — never a trade on a cold EMA.
func (at *AutoTrader) mentorSeedAtStart() {
	if at == nil || at.store == nil {
		return
	}
	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	now := time.Now().UnixMilli()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	rows1m, err1m := bh.LastNBarsCurrentContract("MNQ", "1m", mentorSeedBars1mN)
	if err1m != nil {
		at.logWarnf("🧑‍🏫 mentor seed: 1m store read failed (%v) — seeding with nothing (refusing)", err1m)
		mentorCount("seed_store_read_error")
		rows1m = nil
	}
	bars1m := storeBarsToKlines(rows1m, 60_000)
	// P1 (CTO 12:44:08Z / 12:47:11Z): ONE bar source — the seed aggregates
	// every higher timeframe from 1m; the store's native 1h is never read.
	// The trader-side 1h aggregation mirrors kernel barsTF(bars1m, 60)
	// exactly (epoch-aligned buckets) for the per-source depth seam.
	bars1h := mentorAgg1H(bars1m)
	missing := mentor.Seed(at.mentorEval, bars1m, now)
	depths := mentor.SeedDepths(bars1m, bars1h, now)
	mentorSeedDepthsMu.Lock()
	mentorSeedDepths = depths
	mentorSeedDepthsMu.Unlock()
	mentorSourceDepthSource = func(name string) (int, bool) {
		mentorSeedDepthsMu.RLock()
		defer mentorSeedDepthsMu.RUnlock()
		d, ok := mentorSeedDepths[name]
		return d, ok
	}
	at.logInfof("🧑‍🏫 %s", mentor.SeedLine(at.mentorEval.State, bars1m, now))
	at.logInfof("%s", mentorSeamBootLine())
	if len(missing) > 0 {
		mentorCount("seed_missing")
		at.logErrorf("%s", mentorSeedRefusalLine(missing))
	}
}

// mentorSeedRefusalLine prints every missing source and which entry kinds it
// blocks: a short 4h EMA 34 blocks SWING4H entries only (it feeds only the
// swing line); any other missing source blocks all entries.
func mentorSeedRefusalLine(missing []string) string {
	all := false
	parts := make([]string, 0, len(missing))
	for _, m := range missing {
		if strings.HasPrefix(m, "bar history depth: 4h EMA34") {
			parts = append(parts, m+" [blocks SWING4H entries only]")
			continue
		}
		all = true
		parts = append(parts, m+" [blocks ALL entries]")
	}
	scope := "SWING4H entries only (intraday entries allowed)"
	if all {
		scope = "ALL entries"
	}
	return fmt.Sprintf("🧑‍🏫 mentor seed REFUSING %s — missing: %s", scope, strings.Join(parts, "; "))
}

// mentorAgg1H aggregates 1m bars into clock-aligned 1h buckets on epoch
// boundaries — a byte-for-byte mirror of kernel mentor.barsTF(bars1m, 60)
// (the kernel's non-4h buckets are epoch-aligned; only the 4h bucket uses the
// 17:00 CT anchor). Used for the SeedDepths depth seam; the kernel's Seed
// aggregates its own copy from the same 1m input.
func mentorAgg1H(bars []market.Kline) []market.Kline {
	const hourMs = int64(3600_000)
	out := make([]market.Kline, 0, len(bars)/60)
	var cur *market.Kline
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, b := range bars {
		bucket := b.OpenTime / hourMs * hourMs
		if cur == nil || bucket != cur.OpenTime {
			flush()
			c := b
			c.OpenTime = bucket
			c.CloseTime = bucket + hourMs - 1
			cur = &c
			continue
		}
		if b.High > cur.High {
			cur.High = b.High
		}
		if b.Low < cur.Low {
			cur.Low = b.Low
		}
		cur.Close = b.Close
		cur.Volume += b.Volume
	}
	flush()
	return out
}

// mentorSourcesMissing names every mentor source that is not wired. With
// mentor_mode ON a missing source refuses the arm (fail-closed); the boot line
// reports them in ONE error line.
func (at *AutoTrader) mentorSourcesMissing() []string {
	return at.mentorMissingSources(true)
}

// mentorSourcesBlocking is the set of missing sources that block THIS setup's
// entry. The 4h EMA 34 feeds only the swing line, so a short 4h EMA blocks
// SWING4H entries only; every other missing source blocks every entry.
func (at *AutoTrader) mentorSourcesBlocking(setup string) []string {
	return at.mentorMissingSources(strings.EqualFold(setup, "SWING4H"))
}

// mentorMissingSources lists the missing sources; includeSwingOnly adds the
// ones that block only the swing (the 4h EMA 34 depth).
func (at *AutoTrader) mentorMissingSources(includeSwingOnly bool) []string {
	var missing []string
	for _, n := range mentorSeamMissing() {
		missing = append(missing, "seam: "+n)
	}
	if mentorDayNetSource == nil {
		missing = append(missing, "day net")
	}
	if mentorClosedProfitSource == nil {
		missing = append(missing, "closed profit")
	}
	if at.store == nil && mentorDayEventsForTest == nil {
		missing = append(missing, "news events")
	}
	if market.FuturesBarsProvider == nil {
		missing = append(missing, "5m feed")
	}
	for _, req := range mentorDepthRequirements {
		if req.Name == "4h EMA34" && !includeSwingOnly {
			continue
		}
		min := req.Min
		// the EMA 34 floors are the warm-up constants (safe default 3×34): the
		// 4h one is DS-103's stated warm-up via SetMentor4hEMA34Warmup, the 1m
		// one shares the same default.
		if req.Name == "4h EMA34" {
			min = mentor4hEMA34Warmup
		}
		if req.Name == "1m EMA34" {
			min = mentor1mEMA34Warmup
		}
		depth, known := mentorSourceDepth(req.Name)
		if !known {
			mentorCount("history_depth_short")
			missing = append(missing, fmt.Sprintf("history: %s (n/a)", req.Name))
			continue
		}
		if depth < min {
			mentorCount("history_depth_short")
			missing = append(missing, fmt.Sprintf("history: %s (%d/%d)", req.Name, depth, min))
		}
	}
	return missing
}

// MentorSourcesBootLine is the boot wiring check: with mentor_mode ON every
// mentor source seam must be non-nil, or mentor_mode refuses to arm — one ERROR
// line names the missing seams per trader. A wired trader prints the seeded
// depth per source on the same line (n/a when unknown).
func MentorSourcesBootLine(loaded map[string]*AutoTrader) string {
	var lines []string
	for id, at := range loaded {
		if at == nil || !at.mentorEnabled() {
			continue
		}
		if missing := at.mentorSourcesBlocking("ISB"); len(missing) > 0 {
			logger.Errorf("🧑‍🏫 mentor sources MISSING for trader %s: [%s] — mentor_mode refuses to arm (fail-closed)", id, strings.Join(missing, ", "))
			lines = append(lines, fmt.Sprintf("🧑‍🏫 mentor sources MISSING for trader %s: [%s] — mentor_mode refuses to arm (fail-closed)", id, strings.Join(missing, ", ")))
		} else if swingOnly := at.mentorSourcesMissing(); len(swingOnly) > 0 {
			// only the swing-only source (4h EMA 34) is short: intraday entries
			// are allowed, SWING4H entries are refused until it warms up.
			lines = append(lines, fmt.Sprintf("🧑‍🏫 mentor sources wired for trader %s — intraday entries allowed; SWING4H entries refused until: [%s]", id, strings.Join(swingOnly, ", ")))
		} else {
			var depths []string
			for _, req := range mentorDepthRequirements {
				depth, known := mentorSourceDepth(req.Name)
				if !known {
					depths = append(depths, fmt.Sprintf("%s=n/a", req.Name))
				} else {
					depths = append(depths, fmt.Sprintf("%s=%d", req.Name, depth))
				}
			}
			lines = append(lines, fmt.Sprintf("🧑‍🏫 mentor sources wired for trader %s — seeded depths: %s", id, strings.Join(depths, ", ")))
		}
	}
	if len(lines) == 0 {
		return "🧑‍🏫 mentor sources: n/a (no mentor-mode trader)"
	}
	return strings.Join(lines, " | ")
}

// ── LATENCY (§2: measure it) ───────────────────────────────────────────────

const mentorLatencyCap = 256

var (
	mentorLatencyMu   sync.Mutex
	mentorLatencySamp []int64 // close→ack ms, capped ring
)

// recordMentorLatency logs the four stamps and feeds the p50/p95 reservoir.
// A close→ack above 3 s logs a WARN (the target is under 1 s).
func recordMentorLatency(closeMs, arrivalMs, emitMs, ackMs int64) {
	d := ackMs - closeMs
	if d > 3000 {
		logger.Warnf("🧑‍🏫 mentor eval LATENCY WARN: close→ack %dms (close=%d arrival=%d emit=%d ack=%d) — target < 1000ms",
			d, closeMs, arrivalMs, emitMs, ackMs)
	} else {
		logger.Infof("🧑‍🏫 mentor eval latency: close→ack %dms (close=%d arrival=%d emit=%d ack=%d)",
			d, closeMs, arrivalMs, emitMs, ackMs)
	}
	mentorLatencyMu.Lock()
	mentorLatencySamp = append(mentorLatencySamp, d)
	if len(mentorLatencySamp) > mentorLatencyCap {
		mentorLatencySamp = mentorLatencySamp[len(mentorLatencySamp)-mentorLatencyCap:]
	}
	mentorLatencyMu.Unlock()
}

// MentorLatencySnapshot returns n samples and the p50/p95 of close→ack (ms).
func MentorLatencySnapshot() (n int, p50, p95 int64) {
	mentorLatencyMu.Lock()
	samp := append([]int64(nil), mentorLatencySamp...)
	mentorLatencyMu.Unlock()
	n = len(samp)
	if n == 0 {
		return 0, 0, 0
	}
	sort.Slice(samp, func(i, j int) bool { return samp[i] < samp[j] })
	p50 = samp[n*50/100]
	p95 = samp[n*95/100]
	return n, p50, p95
}

// ResetMentorLatencyForTest clears the reservoir.
func ResetMentorLatencyForTest() {
	mentorLatencyMu.Lock()
	mentorLatencySamp = nil
	mentorLatencyMu.Unlock()
}

// MentorLatencyBootLine is the boot line for the close→ack counter: n/a until
// the first sample exists (canon L7 — a value the process cannot know yet
// prints n/a, never a fabricated 0).
func MentorLatencyBootLine() string {
	n, p50, p95 := MentorLatencySnapshot()
	if n == 0 {
		return "🧑‍🏫 mentor eval latency (close→ack): n=0 p50=n/a p95=n/a (target <1s, WARN >3s)"
	}
	return fmt.Sprintf("🧑‍🏫 mentor eval latency (close→ack): n=%d p50=%dms p95=%dms (target <1s, WARN >3s)", n, p50, p95)
}
