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
	// N11: the once-per-bar watermark is enforced inside mentorEvalOnce under
	// the evaluator mutex (the scan and the event loop can race here).
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
	// N11: the once-per-bar watermark is enforced inside mentorEvalOnce under
	// the evaluator mutex (the scan and the event loop can race here).
	return at.mentorEvalOnce(bars)
}

// mentorEvaluatorConfig applies strategy overrides to the evaluator. B22's
// structural departure remains primary; the fixed-points fallback is opt-in.
func (at *AutoTrader) mentorEvaluatorConfig() mentor.Config {
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	// FU-1: live G1/G2 are fed from REAL broker fills (Limits.RecordFill via
	// the queued drain), never the simulated candle-touch fill.
	cfg.RealFillOnly = true
	rc := at.mentorRiskControl()
	cfg.LossDeparturePts = mentorLossDeparturePts(rc)
	// The resolvers are nil-safe and return the ruled default for an unset field.
	cfg.LegBudgetEnabled = mentorLegBudgetEnabled(rc)
	cfg.LegResetOn = mentorLegResetOn(rc)
	cfg.LocTriggerFilter = mentorLocationTriggerFilter(rc)
	applyMentorTuning(&cfg, rc)
	if rc != nil {
		cfg.LvlRevisitMinPts = mentorLvlRevisitMinPts(rc)
		cfg.EmaMaxCross30m = mentorEmaMaxCross30m(rc)
	}
	return cfg
}

// mentorPlaceNow (B3 item 4) runs the mentor-only armed pass immediately after
// the evaluator authors rows, instead of waiting for the 2-minute scan. It is
// the same serialized pass the scan would run (armedPassMu) — the pass is now
// mentor-aware and needs no day plan.
func (at *AutoTrader) mentorPlaceNow(bars []market.Kline) {
	now := mentorClockNow()
	at.maybeManageArmedOrdersAt(kernel.StructureSnapshot(bars, now.UnixMilli()), now)
}

// mentorPlaceCadence (N7 part 3) is how often the event loop re-runs the
// mentor-only placement while an unexpired mentor arm sits UNPLACED. A package
// var so the event-loop pin can shorten it; production leaves it at 5s.
var mentorPlaceCadence = 5 * time.Second

// mentorPlacementDue reports whether the mentor-only placement pass should run
// NOW: mentor mode + MENTOR_PLACE on, AND at least one mentor-authored row is
// UNPLACED (armed, no signal) and unexpired. A working row needs no retry — the
// pass already reached it; an expired row is cancelled by the pass, not placed.
func (at *AutoTrader) mentorPlacementDue(now time.Time) bool {
	if at == nil || at.store == nil || !at.mentorEnabled() || !mentorPlaceEnv() {
		return false
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		return false
	}
	nowMs := now.UnixMilli()
	for _, r := range rows {
		if !mentorAuthoredRow(r) || r.State != store.StateArmed {
			continue
		}
		if r.ExpiryMs == 0 || nowMs < r.ExpiryMs {
			return true
		}
	}
	return false
}

// mentorPlacementCadencePass runs the mentor-only placement pass when due (the
// event loop's cadence tick calls it). Returns whether a pass ran.
func (at *AutoTrader) mentorPlacementCadencePass() bool {
	if !at.mentorPlacementDue(mentorClockNow()) {
		return false
	}
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	at.mentorPlaceNow(bars)
	return true
}

// mentorEvalOnce runs one evaluator tick over the bars and processes every
// intent (size → latency → no-chase → place-or-hold). Returns whether it ran —
// false when another goroutine (scan vs event) already ticked this bar.
// N11 (DS-104): the scan loop and the event loop both call this; the mutex
// serializes the evaluator (its Tick mutates shared maps) AND makes the
// once-per-bar watermark atomic.
func (at *AutoTrader) mentorEvalOnce(bars []market.Kline) bool {
	last := bars[len(bars)-1]
	at.mentorEvalMu.Lock()
	defer at.mentorEvalMu.Unlock()
	// FU-1: drain queued REAL fills first, under the mutex — a fill that
	// arrived since the last tick registers before this tick's placement
	// decisions read the leg budget / loss box.
	at.mentorDrainFills()
	if last.OpenTime <= at.mentorLastTickOpen {
		return false // already ticked (the other goroutine won the race)
	}
	at.mentorLastTickOpen = last.OpenTime

	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	// item 18 part 1: plumb the day's red-folder 07:30 print windows from the
	// calendar into the evaluator so the print candle never moves the 1h/4h
	// trigger lines [D4.4 p1 @18:13, @22:15]. No calendar / no print today →
	// empty → the HTF feed is byte-identical.
	at.mentorEval.Cfg.PrintWindows = at.mentorNewsPrintWindows()
	emitMs := time.Now().UnixMilli()
	// The evaluator's clock is the instant the last bar CLOSED: every closedness
	// test inside Tick then reads the just-closed bar as closed, never as forming
	// (an OpenTime clock made the ORB escape unreachable and lagged every gate).
	intents := at.mentorEval.Tick(bars, mentor.BarCloseInstant(last))
	// The seed depth moves with the bars: re-check after every tick, so a
	// source that was short at boot (94/102 closed 4h candles) clears on its
	// own instead of needing a restart.
	at.mentorRefreshDepths()
	// EXIT DRIVE (DS-107): event-driven resonance arming first — a same-side
	// ISB within 3 candles of a PHL/PLH fill flips it to mode A (both stops to
	// BE NOW) — then the candle-driven exit loop, once per closed 1m bar.
	for _, in := range intents {
		if isISBEntryIntent(in) {
			at.mentorArmResonanceOnISB(string(in.Side))
		}
	}
	at.mentorExitDrive(bars)
	// S9 (D5.2 p2 @05:21): strong-day detection from the recent CLOSED 5m bars
	// — 50–80 pt candles cut every tier to 1–2.
	strongDay := false
	if bars5 := mentorClosedBars(market.FuturesBarsProvider("MNQ", "5m", 12)); len(bars5) > 0 && mentorStrongDayFrom5m(bars5) {
		strongDay = true
	}
	// item 18 part 2 (R12): at print −10m on a red-folder print day, cancel
	// live INTRADAY mentor arms by ArmID and flatten intraday mentor positions
	// BEFORE the 07:30 print. SWING4H arms and positions are exempt (the course
	// holds the swing by the 4h [D5.2]). Idempotent: retried each bar in the
	// window; no-ops once flat. Runs under the N11 mutex so the ArmID cancel
	// can clear the evaluator's LevelArms safely.
	at.mentorNewsCancelFlattenAt(mentorClockNow())
	// B3 (L4): the day-stop gates below (done-after-win, the news hold, the
	// trading-window end, and the stop-after-loss hook) used to run ONLY while
	// an entry was being WRITTEN — so a resting arm could still fill after the
	// day was shut. This sweep trips them every tick and cancels the resting
	// unfilled intraday arms. Logged + counted; fail-closed.
	at.mentorDayStopSweep(mentorClockNow())
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
	// B3 (release #3b) item 4: place at the authoring event, not on the 2-min
	// scan — a 1-candle mentor arm must not expire before the scan runs.
	if mentorPlaceEnv() {
		at.mentorPlaceNow(bars)
	}
	// D2-44 (item 11): reconcile the evaluator's LevelArms against the ledger —
	// a level order the trader cancelled/expired on its side must stop resting
	// in the evaluator, so the level can re-emit on the next valid touch.
	// Under the N11 mutex (the whole function is the critical section).
	at.mentorReconcileLevelArms()
	// N12 funnel visibility (read-only): one closed bar + this tick's intents.
	// Runs inside the N11 evaluator mutex (taken at the top of mentorEvalOnce),
	// after the event placement so this tick's placements are counted.
	at.mentorFunnelTick(1, len(intents))
	return true
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
	if refuse, why := at.mentorStopAfterLossGate(); refuse {
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
	// §5(a) — the daily loss limit is checked AT PLACEMENT, not only by the
	// 60s sweep: a placement while the session-day's realized loss is already
	// at/past the limit is refused and counted.
	if refuse, why := at.mentorDailyLossGate(mentorClockNow()); refuse {
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
	if forkTP != 0 {
		at.logInfof("🧑‍🏫 mentor exit fork: %s — %s (leg 1 TP %.2f)", forkMode, forkWhy, forkTP)
	} else {
		// mode B/SWING4H: leg 1's TP is computed later (mentorWireLeg1TP at
		// placement / the +1R default) — don't print a misleading 0.00.
		at.logInfof("🧑‍🏫 mentor exit fork: %s — %s", forkMode, forkWhy)
	}
	// B20: the chosen branch is REGISTERED per open position — a later
	// confluence upgrade switches it to C (hold ≥ 1:2, size untouched).
	// B5 (L8): it is registered PER ARM (the scenario/signal id), never per
	// SIDE — a later same-side arm must not rewrite an earlier arm's branch
	// before it fills. Resolve the arm identity ONCE so mentorArmIntent
	// authors the row under the SAME key.
	in.ArmID = mentorResolveArmID(in.ArmID)
	at.setMentorExitMode(mentorScenarioFor(in.ArmID), forkMode)
	if mentorPlaceRecorderForTest != nil {
		mentorPlaceRecorderForTest(in, choice.Contracts)
		return // test seam: the real pipeline is never reached from a test
	}
	at.mentorArmIntent(in, choice, barCloseMs, emitMs, forkMode, forkTP)
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
// otherwise the injector computes the setup's default — a LEVEL order (PHL/PLH/
// EMA, ArmID prefix "lvl-") RESTS until the RTH window end at 15:00 CT
// (D2-44, item 11); a single ISB expires at the close of the NEXT 1m candle
// (the one-candle rule is the ISB only [D1.4 p1 @18:32]); the swing lives until
// the close of the current 4h candle (mentorSwingExpiry).
func mentorIntentExpiry(in mentor.Intent, barCloseMs int64) int64 {
	if in.ExpiryMs > 0 {
		return in.ExpiryMs
	}
	if strings.HasPrefix(strings.TrimSpace(in.ArmID), "lvl-") {
		return mentorLevelExpiry(barCloseMs)
	}
	if strings.EqualFold(in.Setup, "SWING4H") {
		return mentorSwingExpiry(barCloseMs + 1) // +1: the instant the bar closed, so a 4h-boundary bar reads the NEW 4h candle
	}
	return barCloseMs + 60_000 // the close of the NEXT 1m candle
}

// mentorLevelExpiry (D2-44, item 11) is the B6 lifetime for a LEVEL order
// (ArmID prefix "lvl-"): it RESTS until the RTH window end at 15:00 CT, or
// until the evaluator's close-through sweep cancels it — never the next 1m
// candle [D2.3 p1 @17:42–18:14, @18:46–19:12]. At or after 15:00 CT it
// fail-safes to tomorrow's 15:00 (placement past RTH is already refused
// upstream, so this is a guard, never the live path).
func mentorLevelExpiry(nowMs int64) int64 {
	return mentor.LevelArmExpiry(nowMs) // one definition, shared with the leg budget
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

// mentorExitMode / setMentorExitMode read/write the per-ARM exit branch
// registered at placement (A/B/C/swing), keyed by the arm's SCENARIO (the
// signal id) — B5 (L8): never by side, so a later same-side arm cannot
// rewrite an earlier arm's branch before it fills. The P1 exit loop reads the
// branch at fill (keyed by the row's scenario); B20 switches it to C.
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

// deleteMentorExitMode prunes one staged exit branch (I13) — called when the
// arm's staged branch is consumed (the fill copies it onto the live-position
// struct) or the arm goes terminal without ever filling.
func (at *AutoTrader) deleteMentorExitMode(key string) {
	at.mentorExitMu.Lock()
	delete(at.mentorExitModes, key)
	at.mentorExitMu.Unlock()
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
//
// B5 (L8): the branch is stored PER ARM, not per side. The OPEN (filled)
// position's branch lives on its live-position struct — the exit drive reads
// the struct, so the switch moves it there. A school-1 arm that is STILL
// RESTING has no live position yet; its staged branch (keyed by the arm's
// scenario) is switched so the fill registers C. A side that names neither is
// a no-op.
func (at *AutoTrader) mentorConfluenceUpgrade(in mentor.Intent) {
	side := strings.ToLower(string(in.Side))
	found := false
	for _, lp := range at.mentorLivePosList() {
		if lp == nil || lp.Pos.Side != side {
			continue
		}
		found = true
		next, why := mentorConfluenceUpgradeMode(lp.Pos.Mode)
		if next == "" {
			mentorCount("exit_upgrade_no_position")
			at.logWarnf("🧑‍🏫 mentor confluence upgrade IGNORED — %s (%s)", why, in.Reason)
			continue
		}
		if next == lp.Pos.Mode {
			mentorCount("exit_upgrade_noop")
			at.logInfof("🧑‍🏫 mentor confluence upgrade: %s (%s) — %s", side, why, in.Reason)
			continue
		}
		lp.Pos.Mode = next
		mentorCount("exit_upgrade_c")
		at.logInfof("🧑‍🏫 mentor confluence upgrade: %s %s (size unchanged) — %s", side, why, in.Reason)
	}
	// A school-1 arm that is still RESTING: switch its staged branch so the
	// fill registers C. The live-arm registry names the arm by its ArmID; its
	// scenario is the key the placement registered under. I3/I5: the entries
	// are COPIED under mentorLiveMu before iterating (a concurrent write must
	// not race the iteration), and only THIS trader's arms are considered.
	type restingArm struct{ id, side string }
	resting := make([]restingArm, 0)
	mentorLiveMu.Lock()
	for id, arm := range mentorLiveArms {
		if arm.TraderID == at.id {
			resting = append(resting, restingArm{id: id, side: arm.Side})
		}
	}
	mentorLiveMu.Unlock()
	for _, ra := range resting {
		if !strings.EqualFold(ra.side, side) {
			continue
		}
		scenario := mentorScenarioFor(ra.id)
		cur := at.mentorExitMode(scenario)
		if cur == "" {
			continue // no staged branch for this arm — nothing to switch
		}
		found = true
		next, why := mentorConfluenceUpgradeMode(cur)
		if next == "" || next == cur {
			continue
		}
		at.setMentorExitMode(scenario, next)
		mentorCount("exit_upgrade_c")
		at.logInfof("🧑‍🏫 mentor confluence upgrade (resting %s): %s %s (size unchanged) — %s", ra.id, side, why, in.Reason)
	}
	if !found {
		mentorCount("exit_upgrade_no_position")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade IGNORED — no open or resting mentor position on the %s side (%s)", side, in.Reason)
	}
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

// mentorNewsPrintWindowsFromEvents is the pure window builder (item 18 part 1):
// one [printAt−10m, printAt+5m) window per T1 CPI/PPI/Unemployment 07:30 CT
// print — the same events and the same window the hold above uses. No match →
// nil (a non-print day is byte-identical).
func mentorNewsPrintWindowsFromEvents(events []calendar.Event) []mentor.PrintWindow {
	var out []mentor.PrintWindow
	for _, e := range events {
		if e.Impact != calendar.T1 || kernel.CloseHHMMCT(e.Time) != "07:30" {
			continue
		}
		title := strings.ToLower(e.Title)
		matched := false
		for _, tok := range mentorNewsPrintTitleTokens {
			if strings.Contains(title, tok) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		out = append(out, mentor.PrintWindow{
			FromMs: e.Time.Add(-mentorNewsPreWindow).UnixMilli(),
			ToMs:   e.Time.Add(mentorNewsPostWindow).UnixMilli(),
		})
	}
	return out
}

// mentorNewsPrintWindows returns the day's red-folder print windows from the
// stored calendar. ok=false (missing/unreadable calendar) → nil: the HTF feed
// stays byte-identical (the destructive news flat below is the half that needs
// the calendar to fire).
func (at *AutoTrader) mentorNewsPrintWindows() []mentor.PrintWindow {
	evs, ok := at.mentorDayEvents()
	if !ok {
		return nil
	}
	return mentorNewsPrintWindowsFromEvents(evs)
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

// mentorNewsCancelFlattenAt (item 18 part 2, R12) cancels live INTRADAY mentor
// arms by ArmID and flattens intraday mentor positions at print −10m on a
// red-folder print day [D4.4 p1 @20:44–21:46]. It is the DESTRUCTIVE half of
// the news window: the hold above refuses NEW placements through the print;
// this sweep clears what already REMAINS before the 07:30 print. SWING4H arms
// and positions are exempt (the course holds the swing by the 4h [D5.2]).
// Idempotent by construction: an already-cancelled arm and an already-closed
// position both no-op, so the sweep may run on every bar of the window.
// The calendar is REQUIRED — a destructive flatten must not fire on a
// guess; with no readable calendar the hold still blocks placements fail-closed
// but nothing is force-closed. Returns whether it acted.
func (at *AutoTrader) mentorNewsCancelFlattenAt(now time.Time) bool {
	if !at.mentorEnabled() || at.store == nil || at.trader == nil {
		return false
	}
	evs, ok := at.mentorDayEvents()
	if !ok {
		return false // cannot name a print day — no force-close on a guess
	}
	if hold, _ := mentorNewsHold(evs, now); !hold {
		return false // not inside a print window on a print day
	}
	acted := at.cancelLiveIntradayMentorArms()
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		at.logWarnf("🧑‍🏫 news flat: open-position read failed (%v) — flatness UNVERIFIED before the print", err)
		return acted
	}
	if len(positions) > 0 {
		at.logWarnf("🧑‍🏫 news flat: print −10m — flattening %d intraday mentor position(s) (SWING4H exempt)", len(positions))
	}
	for _, p := range positions {
		if isSwingPosition(p) {
			at.logInfof("🧑‍🏫 news flat: SWING4H position %d (%s) EXEMPT — held by the 4h", p.ID, p.CitedScenarioID)
			continue
		}
		at.flattenPosition(p, "🧑‍🏫 NEWS FLAT")
		acted = true
	}
	return acted
}

// cancelLiveIntradayMentorArms cancels every live (current-process) INTRADAY
// mentor arm by its ArmID — SWING4H arms ("swing-…") are exempt. It resolves
// through the injector's live-arm registry and the real mentorCancelArm path,
// so each cancel is named and the evaluator's LevelArms entry is cleared.
// Returns whether any cancel was requested.
func (at *AutoTrader) cancelLiveIntradayMentorArms() bool {
	return at.cancelLiveIntradayMentorArmsWith("news 07:30 print — cancel intraday mentor arm before the print [D4.4 p1 @20:44]")
}

// cancelLiveIntradayMentorArmsWith is the B3-generalized sweep: the same
// intraday cancel, with the tripping gate named in each cancel's reason (so the
// ledger history records WHICH day-stop gate cleared the arm). The "news flat"
// logging above becomes one caller of many.
func (at *AutoTrader) cancelLiveIntradayMentorArmsWith(reason string) bool {
	return at.cancelLiveMentorArms(reason, true)
}

// cancelAllLiveMentorArmsWith cancels EVERY live mentor arm — intraday AND
// SWING4H — by its ArmID. The never-add sweep's fail-closed shape: the course's
// never-add [D1.1 p1 @17:06–17:44] names no swing exception (it is silent on
// whether a swing may coexist with an intraday position), so one position at a
// time means the swing is cancelled too.
func (at *AutoTrader) cancelAllLiveMentorArmsWith(reason string) bool {
	return at.cancelLiveMentorArms(reason, false)
}

// cancelLiveMentorArms is the shared sweep body. swingExempt skips "swing-…"
// arms (the news/window/day-stop cancels hold the swing by the 4h [D5.2]); the
// never-add sweep passes false (no exemption).
//
// I5/I3: the entries are COPIED under mentorLiveMu before iterating (a
// concurrent write must not race the iteration or the len), the sweep acts only
// on THIS trader's arms, and only a cancel that was ACTUALLY requested is
// counted or logged (mentorCancelArm returns false for a refused / already-
// terminal row).
func (at *AutoTrader) cancelLiveMentorArms(reason string, swingExempt bool) bool {
	type entry struct{ id, traderID string }
	entries := make([]entry, 0)
	mentorLiveMu.Lock()
	for id, arm := range mentorLiveArms {
		entries = append(entries, entry{id: id, traderID: arm.TraderID})
	}
	mentorLiveMu.Unlock()
	cancelled := 0
	for _, e := range entries {
		if e.traderID != at.id {
			continue // not this trader's arm (the registry is process-global)
		}
		if swingExempt && strings.HasPrefix(strings.TrimSpace(e.id), "swing-") {
			continue // SWING4H arm — held by the 4h
		}
		if at.mentorCancelArm(mentor.Intent{
			Action: mentor.CancelArm,
			ArmID:  e.id,
			Reason: reason,
		}) {
			cancelled++
		}
	}
	if cancelled > 0 {
		if swingExempt {
			at.logWarnf("🧑‍🏫 day-stop cancel: cancelled %d live intraday mentor arm(s) (SWING4H exempt) — %s", cancelled, reason)
		} else {
			at.logWarnf("🧑‍🏫 never-add cancel: cancelled %d live mentor arm(s) (SWING4H included — fail-closed) — %s", cancelled, reason)
		}
	}
	return cancelled > 0
}

// mentorOpenPositionActive reports whether a mentor position is open. The
// open-side seam is the primary read (the P1 driver wires it to the bound
// account's position snapshot); the account position count is the fail-closed
// backstop — an unreadable position list counts as OPEN, never flat.
func (at *AutoTrader) mentorOpenPositionActive() bool {
	if mentorOpenSideSource != nil && mentorOpenSideSource() != "" {
		return true
	}
	return at.openPositionCount() != 0
}

// ── B3 (L4) DAY-STOP SWEEP ────────────────────────────────────────────────
//
// The day-stop gates below used to run ONLY while an entry was being written
// (the placement path), so a resting arm placed earlier could still fill after
// the day had been shut. This sweep trips them EVERY TICK and cancels the
// resting unfilled intraday arms. Idempotent: an already-cancelled arm no-ops.
// SWING4H is exempt (the course holds the swing by the 4h [D5.2]).
//
// The sweep cancels only on a DEFINITE trip. An UNRESOLVED read (done-after-win
// day data missing, or a NULL pnl_corrected row) does NOT force-cancel — the
// placement gate still fails closed for NEW entries, but an unknown is not
// evidence the day is over, and destroying a resting arm on a data gap is worse
// than the gap it closes.
func (at *AutoTrader) mentorDayStopSweep(now time.Time) {
	if !at.mentorEnabled() || at.store == nil || at.trader == nil {
		return
	}
	if trip, why := at.mentorDoneAfterWinTripped(); trip {
		if at.cancelLiveIntradayMentorArmsWith("day-stop done-after-win: " + why) {
			mentorCount("day_stop_cancel_done_after_win")
		}
	}
	if ended, why := at.mentorWindowEnded(now); ended {
		if at.cancelLiveIntradayMentorArmsWith("day-stop window end: " + why) {
			mentorCount("day_stop_cancel_window_end")
		}
	}
	if hold, why := at.mentorNewsGate(); hold {
		if at.cancelLiveIntradayMentorArmsWith("day-stop news hold: " + why) {
			mentorCount("day_stop_cancel_news")
		}
	}
	// NEVER-ADD [D1.1 p1 @17:06–17:44], now enforced while a position is OPEN,
	// not only at write time (L4): with B4 more mentor arms rest at the broker
	// at once, so a second fill while a position is open would ADD (same side)
	// or REDUCE/FLIP (opposite side) it. Cancel the resting unfilled mentor
	// arms on BOTH sides, once per open-position episode. SWING4H: the course
	// is silent on a swing coexisting with an intraday position, so it is
	// cancelled too (fail-closed: one position at a time).
	if at.mentorOpenPositionActive() {
		if !at.mentorNeverAddCancelDone {
			if at.cancelAllLiveMentorArmsWith("day-stop never-add: a mentor position is open — one position at a time, cancel both sides [D1.1 p1 @17:06–17:44]") {
				mentorCount("day_stop_cancel_never_add")
			}
			at.mentorNeverAddCancelDone = true
		}
	} else {
		at.mentorNeverAddCancelDone = false
	}
	if mentorStopAfterLossTripped != nil {
		if stop, why := mentorStopAfterLossTripped(); stop {
			if at.cancelLiveIntradayMentorArmsWith("day-stop stop-after-loss: " + why) {
				mentorCount("day_stop_cancel_stop_after_loss")
			}
		}
	}
}

// mentorDoneAfterWinTripped reports the DEFINITE done-after-win trip only — a
// trade closed in profit AND the day is net positive. An unwired or unresolved
// source is "unknown", not a trip: the placement gate still refuses fail-closed,
// but the sweep does NOT force-cancel a resting arm on an unknown.
func (at *AutoTrader) mentorDoneAfterWinTripped() (bool, string) {
	if at.config.StrategyConfig != nil {
		if v := at.config.StrategyConfig.RiskControl.MentorDoneAfterWin; v != nil && !*v {
			return false, "" // the knob is explicitly OFF
		}
	}
	if mentorDayNetSource == nil || mentorClosedProfitSource == nil {
		return false, ""
	}
	net, okNet := mentorDayNetSource()
	closed, okClosed := mentorClosedProfitSource()
	if !okNet || !okClosed {
		return false, ""
	}
	if mentorDoneAfterWin(net, closed) {
		return true, "a trade closed in profit and the day is net positive — no entries until the next trading day (17:00 CT) [D1.2 p1 @20:53–21:16]"
	}
	return false, ""
}

// mentorWindowEnded reports the window-END half of the window gate: the window
// is ENDED when minutes > 0, the window is not currently active, and the most
// recent window that OPENED at or before now (today's open if it has passed,
// else yesterday's) has closed. This reads ENDED at 01:30 for a 23:00/120
// cross-midnight window (the most recent open was yesterday 23:00, ended 01:00)
// and NOT ended at 00:30 (still active). A pre-open 07:00 with an 08:30/60
// window reads ENDED (yesterday's window) — but no intraday arm can be authored
// outside the window (mentorWindowGate refuses it; only the SWING is exempt and
// the sweep skips it), so a pre-open resting arm can only come from an ended
// window. minutes <= 0 disables the window (no end to trip).
func (at *AutoTrader) mentorWindowEnded(now time.Time) (bool, string) {
	start, minutes := at.mentorWindowKnobs()
	if minutes <= 0 {
		return false, ""
	}
	if active, _ := mentorWindowActive(start, minutes, now); active {
		return false, "" // the window is open right now
	}
	hour, minute, ok := parseMentorWindowStart(start)
	if !ok {
		return false, "" // unparseable start: the placement gate refuses; no force-cancel on a guess
	}
	loc := kernel.CTLocation()
	ct := now.In(loc)
	open := time.Date(ct.Year(), ct.Month(), ct.Day(), hour, minute, 0, 0, loc)
	if open.After(now) {
		open = time.Date(ct.Year(), ct.Month(), ct.Day()-1, hour, minute, 0, 0, loc)
	}
	end := open.Add(time.Duration(minutes) * time.Minute)
	if !now.Before(end) {
		return true, fmt.Sprintf("the trading window ended at %s CT [D1.2 p1 @23:52–24:59]", kernel.CloseHHMMCT(end))
	}
	return false, ""
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

// mentorDayNetSource / mentorClosedProfitSource are the session seams for (a).
// nil → unwired (fail closed). A wired seam that returns ok=false means the
// day's data is UNRESOLVED (a NULL pnl_corrected closed row or a read error) —
// an unknown is not "no win", so the gate fails CLOSED. The production wiring
// (mentorWireProductionSeams) binds both to the strict-corrected day read.
var (
	mentorDayNetSource       func() (float64, bool)
	mentorClosedProfitSource func() (bool, bool)
	mentorClosedLossSource   func() (bool, bool)
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
	net, okNet := mentorDayNetSource()
	closed, okClosed := mentorClosedProfitSource()
	if !okNet || !okClosed {
		mentorCount("done_after_win_unresolved")
		at.logWarnf("🧑‍🏫 mentor done-after-win day UNRESOLVED (a NULL pnl_corrected closed row or a read error) — refusing the entry (fail-closed)")
		return true, "done-after-win: day P&L unresolved (a NULL pnl_corrected closed row) — an unknown is not 'no win'; refusing (fail-closed) [D1.2 p1 @20:53–21:16]"
	}
	if mentorDoneAfterWin(net, closed) {
		mentorCount("done_after_win_refused")
		return true, "done for the day after a win — a trade closed in profit and the day is net positive; no new entries until the next trading day (17:00 CT) [D1.2 p1 @20:53–21:16]"
	}
	return false, ""
}

// mentorStopAfterLossGate — STOP-AFTER-LOSS (owner "ok" 2026-10-05): once a
// MENTOR trade closes today (17:00 CT session-day) with a net LOSS (both legs
// combined; pnl_corrected < 0), refuse new mentor entries until the next
// session day [D1.2 p1 @ 23:34]. A breakeven close (0) is NOT a loss. The
// knob is default OFF (nil → OFF; byte-identical while off). FAIL-CLOSED like
// done-after-win: an unwired source or an UNRESOLVED close refuses, with its
// own counters.
func (at *AutoTrader) mentorStopAfterLossGate() (bool, string) {
	if at.config.StrategyConfig == nil {
		return false, "" // no config → the knob reads OFF (nil → OFF, the default)
	}
	if v := at.config.StrategyConfig.RiskControl.MentorStopAfterLoss; v == nil || !*v {
		return false, "" // the knob is OFF (nil → OFF, the default)
	}
	if mentorClosedLossSource == nil {
		mentorCount("stop_after_loss_no_data")
		at.logWarnf("🧑‍🏫 mentor stop-after-loss source missing (closed loss) — refusing the entry (fail-closed)")
		return true, "stop-after-loss: closed-loss source not wired — an unknown is not 'no loss'; refusing (fail-closed) [D1.2 p1 @ 23:34]"
	}
	closed, ok := mentorClosedLossSource()
	if !ok {
		mentorCount("stop_after_loss_unresolved")
		at.logWarnf("🧑‍🏫 mentor stop-after-loss day UNRESOLVED (a NULL pnl_corrected closed row or a read error) — refusing the entry (fail-closed)")
		return true, "stop-after-loss: day unresolved (a NULL pnl_corrected closed row) — an unknown is not 'no loss'; refusing (fail-closed) [D1.2 p1 @ 23:34]"
	}
	if closed {
		mentorCount("stop_after_loss_refused")
		return true, "stop for the day after a loss — a mentor trade closed today with a net loss; no new entries until the next trading day (17:00 CT) [D1.2 p1 @ 23:34]"
	}
	return false, ""
}

// mentorStopAfterLossTripped is the B3 day-stop-sweep hook for the
// stop-after-loss gate (wired in mentorWireProductionSeams next to
// mentorClosedLossSource). nil → unwired (the sweep does not trip). It returns
// (true, why) ONLY on a DEFINITE loss (the knob ON and a resolved closed loss);
// an unwired or unresolved read returns (false, "") — the placement gate stays
// fail-closed, but the sweep never force-cancels a resting arm on an unknown
// (the same contract as mentorDoneAfterWinTripped).
var mentorStopAfterLossTripped func() (bool, string)

// mentorStopAfterLossTrip is the DEFINITE-trip computation behind the B3 sweep
// hook: the knob ON and a resolved closed loss, nothing else.
func (at *AutoTrader) mentorStopAfterLossTrip() (bool, string) {
	if at.config.StrategyConfig == nil {
		return false, ""
	}
	if v := at.config.StrategyConfig.RiskControl.MentorStopAfterLoss; v == nil || !*v {
		return false, "" // the knob is OFF (nil → OFF, the default)
	}
	if mentorClosedLossSource == nil {
		return false, ""
	}
	closed, ok := mentorClosedLossSource()
	if !ok {
		return false, ""
	}
	if closed {
		return true, "a mentor trade closed today with a net loss — no entries until the next trading day (17:00 CT) [D1.2 p1 @ 23:34]"
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

// mentorDailyLossGate is the §5(a) placement-time daily-loss check: the daily
// loss limit is enforced HERE, not only by the 60s sweep. A placement while the
// session-day's realized loss is already at/past the limit is refused and
// counted. It reads the SAME production readers the desk strip uses
// (deskGuardrail for the enforced limit, deskRealizedToday for the corrected
// session-day P&L) — one definition, no second copy (A24).
//
// FAIL-OPEN on a read error (deskRealizedToday returns 0 when the store read
// fails) — the 60s sweep still enforces the flatten, and a circuit breaker that
// trips on a DB hiccup is worse than the gap it closes (sessionRiskGateAt's
// contract). The limit is only checked when BOTH toggles are on and a value is
// configured (deskGuardrail returns enforced=false otherwise).
func (at *AutoTrader) mentorDailyLossGate(now time.Time) (bool, string) {
	limit, _, enforced := at.deskGuardrail()
	if !enforced || limit <= 0 {
		return false, ""
	}
	// CTO fold (release 10-05-1): fail CLOSED on an UNRESOLVED close today
	// (pnl NULL — canon 40): an unknown loss is not a confident "under the
	// limit" — the same rule as the B1 day-net source. The 60s sweep is a
	// backstop, not the gate.
	if at.store == nil {
		return false, "" // no store exists only in unit fixtures; production always has one
	}
	realized, _, unresolved := at.deskRealizedToday(now)
	if unresolved > 0 {
		mentorCount("daily_loss_refused")
		return true, fmt.Sprintf("daily loss limit enforced but %d close(s) today have unresolved P&L — no new entry (fail-closed) [guardrail]", unresolved)
	}
	if realized <= -limit {
		mentorCount("daily_loss_refused")
		return true, fmt.Sprintf("daily loss limit hit at placement (realized today=%.2f, limit=-%.2f) — no new entry [guardrail]", realized, limit)
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
	// N1 (DS-104): each construction (boot or reload) gets its own arm epoch, so
	// a rebuilt evaluator's "isb-1" never collides with a prior terminal row.
	bumpMentorArmEpoch()
	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	// The seed clock reads the mentor seam (mentorNowSource) so tests can pin a
	// fixed mid-session instant; production defaults to time.Now() (X-07-follow,
	// FLAKE-SEED-CLOCK). Same wall clock, same behaviour.
	now := mentorClockNow().UnixMilli()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	rows1m, err1m := bh.LastNBarsCurrentContract("MNQ", "1m", mentorSeedBars1mN)
	if err1m != nil {
		at.logWarnf("🧑‍🏫 mentor seed: 1m store read failed (%v) — seeding with nothing (refusing)", err1m)
		mentorCount("seed_store_read_error")
		rows1m = nil
	}
	bars1m := storeBarsToKlines(rows1m, 60_000)
	// P1 (CTO 12:44:08Z / 12:47:11Z): ONE bar source — the seed aggregates
	// every higher timeframe from 1m; the store's native 1h is never read
	// (except for the roll-gap measurement, which is a separate concern).

	// KEYLEVEL-FULL-HISTORY (release #10): the 1H RTH key-level walk seeds from
	// the FULL stitched history — every contract's 1m, back-adjusted through
	// measured roll gaps — not the current contract's last 50000 bars. The
	// stitched 1m also feeds the 4h EMA 34 and the #435 seeded trigger/HTF
	// lines (FIX 3): all three run on the SAME back-adjusted series, so every
	// boot sees >=102 closed 4h candles instead of the current contract's ~95.
	// The 1m EMA 34/9 tail is identical (the stitched tail IS the current
	// contract's recent bars). A stitch that produces nothing degrades to the
	// current-contract walk (cold store / read error already refused above).
	stitched1m, full1HRTH, contracts, gaps, stoppedAt := at.mentorFullKeyLevelHistory(bh)
	var missing []string
	if len(full1HRTH) > 0 {
		missing = mentor.SeedFull(at.mentorEval, stitched1m, full1HRTH, now)
		at.logInfof("%s", mentor.KeyLevelHistoryLine(contracts, full1HRTH, gaps, stoppedAt))
	} else {
		missing = mentor.Seed(at.mentorEval, bars1m, now)
	}
	// The evaluator's own depth (computed from the stitched series) is the
	// single source of truth for the per-source refusal seam.
	depths := at.mentorEval.Depths()
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

// mentorFullKeyLevelHistory reads the contracts' 1m NEWEST→OLDEST and stitches
// the full 08:30-anchored 1H RTH series for the key-level seed
// (KEYLEVEL-FULL-HISTORY, release #10). It returns the stitched 1m series
// (newest scale), the 1H RTH key-level walk, the contracts stitched (oldest
// first), the measured roll gaps, and the roll where the stitch stopped
// ("" = full). FIX 5: it STOPS READING at the first roll whose gap cannot be
// measured instead of reading every contract. A cold store / read error
// returns nil and the caller degrades to the current-contract walk.
func (at *AutoTrader) mentorFullKeyLevelHistory(bh *store.BarHistoryStore) (stitched1m, full1HRTH []market.Kline, contracts []mentor.Contract1M, gaps []mentor.RollGap, stoppedAt string) {
	if at == nil || bh == nil {
		return nil, nil, nil, nil, ""
	}
	spans, err := bh.ContractSpans("MNQ", "1m")
	if err != nil || len(spans) == 0 {
		if err != nil {
			at.logWarnf("🧑‍🏫 key-level history: contract span read failed (%v) — falling back to current contract", err)
		}
		return nil, nil, nil, nil, ""
	}
	read := func(contract string) (mentor.Contract1M, bool) {
		rows, err := bh.AllBarsOn("MNQ", "1m", contract)
		if err != nil {
			at.logWarnf("🧑‍🏫 key-level history: %s 1m read failed (%v) — stopping the stitch", contract, err)
			return mentor.Contract1M{}, false
		}
		if len(rows) == 0 {
			return mentor.Contract1M{}, false
		}
		c := mentor.Contract1M{Contract: contract, Bars: storeBarsToKlines(rows, 60_000)}
		// The native 1h store feeds ONLY the roll-gap measurement (its 1h is
		// whole-hour aligned, so it cannot build the 08:30 RTH candles — 1m
		// wins for the series by construction; KEY DB FINDING). 1h retention
		// outlives 1m, so adjacent contracts overlap on more 1h bars.
		if rows1h, err := bh.AllBarsOn("MNQ", "1h", contract); err == nil {
			c.Bars1H = storeBarsToKlines(rows1h, 3600_000)
		}
		return c, true
	}
	newest, ok := read(spans[len(spans)-1].Contract)
	if !ok {
		return nil, nil, nil, nil, ""
	}
	s := mentor.NewRollStitcher(newest)
	contracts = append(contracts, newest)
	for i := len(spans) - 2; i >= 0; i-- {
		c, ok := read(spans[i].Contract)
		if !ok {
			break
		}
		if !s.Add(c) {
			break
		}
		contracts = append(contracts, c)
	}
	stitched1m, full1HRTH, gaps, stoppedAt = s.Result()
	// Oldest-first, so the boot line reads left-to-right from the earliest date.
	for i, j := 0, len(contracts)-1; i < j; i, j = i+1, j-1 {
		contracts[i], contracts[j] = contracts[j], contracts[i]
	}
	return
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

// mentorSourcesLine is the single-trader "mentor sources" line ("" when the
// trader is nil or not mentor-mode). The boot line and the reload line both
// join these per-trader lines, so a save that arms mentor mode reports the
// exact same facts as boot.
func mentorSourcesLine(id string, at *AutoTrader) string {
	if at == nil || !at.mentorEnabled() {
		return ""
	}
	if missing := at.mentorSourcesBlocking("ISB"); len(missing) > 0 {
		line := fmt.Sprintf("🧑‍🏫 mentor sources MISSING for trader %s: [%s] — mentor_mode refuses to arm (fail-closed)", id, strings.Join(missing, ", "))
		logger.Errorf("%s", line)
		return line
	}
	if swingOnly := at.mentorSourcesMissing(); len(swingOnly) > 0 {
		// only the swing-only source (4h EMA 34) is short: intraday entries
		// are allowed, SWING4H entries are refused until it warms up.
		return fmt.Sprintf("🧑‍🏫 mentor sources wired for trader %s — intraday entries allowed; SWING4H entries refused until: [%s]", id, strings.Join(swingOnly, ", "))
	}
	var depths []string
	for _, req := range mentorDepthRequirements {
		depth, known := mentorSourceDepth(req.Name)
		if !known {
			depths = append(depths, fmt.Sprintf("%s=n/a", req.Name))
		} else {
			depths = append(depths, fmt.Sprintf("%s=%d", req.Name, depth))
		}
	}
	return fmt.Sprintf("🧑‍🏫 mentor sources wired for trader %s — seeded depths: %s", id, strings.Join(depths, ", "))
}

// MentorSourcesBootLine is the boot wiring check: with mentor_mode ON every
// mentor source seam must be non-nil, or mentor_mode refuses to arm — one ERROR
// line names the missing seams per trader. A wired trader prints the seeded
// depth per source on the same line (n/a when unknown).
func MentorSourcesBootLine(loaded map[string]*AutoTrader) string {
	var lines []string
	for id, at := range loaded {
		if line := mentorSourcesLine(id, at); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "🧑‍🏫 mentor sources: n/a (no mentor-mode trader)"
	}
	return strings.Join(lines, " | ")
}

// MentorSourcesReloadLine re-prints the sources line for exactly the traders a
// strategy reload just rebuilt (B1). The boot line covers process boot; this
// covers "mentor mode turned ON by a save" without a restart. Empty when none
// of the ids is a mentor-mode trader.
func MentorSourcesReloadLine(loaded map[string]*AutoTrader, ids []string) string {
	var lines []string
	for _, id := range ids {
		if line := mentorSourcesLine(id, loaded[id]); line != "" {
			lines = append(lines, line)
		}
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
