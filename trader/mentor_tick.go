package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
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
	cfg.LegResetOn = mentorLegResetOn(at, rc)
	cfg.LocTriggerFilter = mentorLocationTriggerFilter(rc)
	cfg.DayOffRecheck = mentorDayOffRecheck(rc)
	applyMentorTuning(at, &cfg, rc)
	applyMentorCourseExits(at, &cfg, rc) // release #25 course-exits knobs (CX-1/6/8/9/10/11)
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
	at.maybeManageArmedOrdersAt(kernel.StructureSnapshotForVeto(at.futuresSymbol(), bars, now.UnixMilli()), now) // F-4: + native 4h
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
	// #47: drain queued broker EXITS too — a real exit closes the matching
	// Limits.openTrade before this tick's simulate() can phantom-book a
	// stop-out through the original stop.
	at.mentorDrainExits()
	if last.OpenTime <= at.mentorLastTickOpen {
		return false // already ticked (the other goroutine won the race)
	}
	prevTickOpen := at.mentorLastTickOpen
	at.mentorLastTickOpen = last.OpenTime

	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	// N1 (release #23): a history refill since the seed → re-seed the state.
	at.mentorReseedOnRefill(mentor.BarCloseInstant(last))
	// Release #54 (m441, N2): feed the evaluator the stop / target / close
	// receipt BEFORE it ticks — a swing whose position no longer exists is
	// cleared here, so it cannot emit a BE or a market close against a later
	// trade. Cheap: a no-op unless the evaluator holds an open swing.
	at.mentorSwingReconcile()
	// item 18 part 1: plumb the day's red-folder 07:30 print windows from the
	// calendar into the evaluator so the print candle never moves the 1h/4h
	// trigger lines [D4.4 p1 @18:13, @22:15]. No calendar / no print today →
	// empty → the HTF feed is byte-identical.
	at.mentorEval.Cfg.PrintWindows = at.mentorNewsPrintWindows()
	emitMs := time.Now().UnixMilli()
	// MENTOR-NY item 7 (rescan P1-N1): CATCH UP every closed bar the event path
	// missed. A tick evaluates only its newest bar (eval.go reads prev/cur), so
	// when a bar's FINAL was missed (10-14 08:32–08:52: the funnel fell 6 bars
	// behind the clock while the bar ingest was saturated) the next tick jumped
	// over it — never an ISB or touch candle, never counted. Each missed CLOSED
	// bar is ticked in order at its own close instant before the newest one.
	// Their intents go through the same dispatch with the NEWEST bar's close,
	// so an ENTRY from a caught-up bar is refused by N10 (stale RefBarMs —
	// never a late order); arm actions (keep/cancel, invalidations, upgrades)
	// apply. Counted (mentor_bars_caught_up) and WARN'd with the range.
	caught, caughtBars := at.mentorCatchUp(bars, prevTickOpen)
	// The evaluator's clock is the instant the last bar CLOSED: every closedness
	// test inside Tick then reads the just-closed bar as closed, never as forming
	// (an OpenTime clock made the ORB escape unreachable and lagged every gate).
	// mentorEvalIntentsSeam (test-only, nil in production) supplies the intents
	// for this tick — the m134 (a) negative pin drives the PRODUCTION dispatch /
	// refusal below with a raw synthetic ISB intent; the (removed) raw-intent
	// loop a mutant re-adds would see these same intents.
	var intents []mentor.Intent
	if at.mentorEvalIntentsSeam != nil {
		intents = at.mentorEvalIntentsSeam(bars)
	} else {
		intents = at.mentorEval.Tick(bars, mentor.BarCloseInstant(last))
	}
	// FULL-DAY-REPLAY observer (test-only, nil in production): sees every
	// intent this tick emitted before anything acts on it.
	if tap := at.mentorIntentTap; tap != nil {
		for _, in := range intents {
			tap(in)
		}
	}
	intents = append(caught, intents...)
	// Day-gate report (owner ruling 2026-10-09): ONE INFO line per trading day,
	// emitted at the 08:30 decision, with the gate's own inputs (the 08:30
	// decision was silent in the log — only the refusal counters showed it).
	if rep := at.mentorEval.State.DayGateReport; rep.Key != "" && rep.Key != at.mentorDayGateReportLogged {
		at.mentorDayGateReportLogged = rep.Key
		// m134 DS-108 (b): the §7 verdict is WARN so it reaches the persisted
		// log_events (which stores only error+warning) — the owner ruling
		// 2026-10-09 day-gate line was INFO and silently invisible.
		at.logWarnf("%s", mentorDayGateLine(rep))
	}
	// MENTOR-NY item 5: the ORB state, READ from the evaluator — the range
	// once per day, the escape side + time when it first appears and on every
	// side change. It was never logged (10-14: zero ORB lines while the ORB
	// latch held every short out 08:33–15:00).
	for _, l := range at.mentorNoteORB(at.mentorEval.State.ORB, mentor.BarCloseInstant(last)) {
		at.logInfof("%s", l)
	}
	// Release #22 LINES-RESTART: every change the evaluator recorded this tick
	// (4h/1h trigger move, box latched/cleared, trendline valid/dead, swing
	// line gap fold) is logged once, as it happens — READ values only.
	for _, n := range at.mentorEval.TakeNotes() {
		at.logInfof("🧑‍🏫 %s", n)
	}
	// Day-off recheck (owner ruling 2026-10-09): when the evaluator cleared a
	// latched DayOff this tick, WARN + count ONCE (per process per trading day
	// — the kernel clear is one-way, so ClearedAt is a stable dedup key).
	if rc := at.mentorEval.State.DayRecheck; rc.ClearedAt != 0 && rc.ClearedAt != at.mentorDayOffClearLogged {
		at.mentorDayOffClearLogged = rc.ClearedAt
		at.mentorCount("day_off_recheck_cleared")
		at.logWarnf("🧑‍🏫 day-off cleared: 4h and 1h now agree (%s) at %s CT — trading resumes in that direction [owner ruling 2026-10-09; D5.1 p1 @19:22 premise no longer holds]", rc.ClearedDir, kernel.ClockHHMMCT(time.UnixMilli(rc.ClearedAt)))
	}
	// The seed depth moves with the bars: re-check after every tick, so a
	// source that was short at boot (94/102 closed 4h candles) clears on its
	// own instead of needing a restart.
	at.mentorRefreshDepths()
	// EXIT DRIVE (DS-107): the candle-driven exit loop, once per closed 1m bar.
	// Resonance arming (mode A) is NOT driven from raw evaluator intents here —
	// a raw ISB intent must never move an open position's stops. m134 (a): only
	// an ISB entry the ledger ACCEPTED arms resonance, from the ledger-accept
	// point in mentorArmIntent.
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
	// holds the swing by the 4h [D5.2 p1 @06:03–06:20]). Idempotent: retried each bar in the
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
			at.mentorCount("intent_" + string(in.Action))
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
	// STALLED-SETUP ALARM (release #22): after this tick's emits are counted,
	// fire the once-per-day P1 for any kind with emitted >= 3 and placed == 0.
	at.mentorStallAlarmCheck(mentorClockNow())
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
	at.mentorFunnelTick(1+caughtBars, intents)
	return true
}

// mentorStampCaughtUp (DS-104 P2-1) gives every ENTRY intent a caught-up bar
// emitted that bar's close as its RefBarMs when it carries none. The SWING4H
// entry leaves RefBarMs 0 (N10 exempts it: its own 5m watermark gates it), and
// its dropStaleSwingIntents ran inside the caught-up tick where that bar WAS
// the newest — so without the stamp a swing entry from a missed bar passed N10
// and could rest late. With it, N10 refuses every caught-up entry the same way
// (stale_intent; the dispatch's arm cleanup applies). Arm actions untouched.
func mentorStampCaughtUp(ins []mentor.Intent, barCloseMs int64) []mentor.Intent {
	for i := range ins {
		if (ins[i].Action == mentor.PlaceStopEntry || ins[i].Action == mentor.PlaceStopLimitEntry) && ins[i].RefBarMs == 0 {
			ins[i].RefBarMs = barCloseMs
		}
	}
	return ins
}

// mentorSweepsOnBeat runs the news cancel/flatten and the day-stop sweep from
// the 60 s monitor beat (MENTOR-NY item 8), under the evaluator mutex like
// mentorEvalOnce. Both are idempotent. TryLock: when a tick holds the mutex it
// is running these same sweeps itself, so the beat never queues behind it.
// Returns whether it ran (inert unless mentor mode is ON — both sweeps check).
func (at *AutoTrader) mentorSweepsOnBeat() bool {
	if at == nil || !at.mentorEnabled() {
		return false
	}
	if !at.mentorEvalMu.TryLock() {
		return false
	}
	defer at.mentorEvalMu.Unlock()
	now := mentorClockNow()
	at.mentorNewsCancelFlattenAt(now)
	at.mentorDayStopSweep(now)
	return true
}

// mentorCatchUpMax bounds one catch-up (a long outage has no bars to replay
// anyway; the bound keeps a single tick's latency finite). Bars beyond it are
// counted as skipped, never silently.
const mentorCatchUpMax = 240

// mentorCatchUp ticks every CLOSED bar after prevTickOpen and before the newest
// bar, in order, at its own close instant (MENTOR-NY item 7). Returns the
// intents those ticks emitted (in order) and how many bars it ticked. Runs
// under mentorEvalMu (the caller holds it). prevTickOpen 0 = the first tick
// after a seed — nothing to catch up.
func (at *AutoTrader) mentorCatchUp(bars []market.Kline, prevTickOpen int64) ([]mentor.Intent, int) {
	n := len(bars)
	if prevTickOpen <= 0 || n < 2 || bars[n-1].OpenTime <= prevTickOpen+60_000 {
		return nil, 0
	}
	first := -1
	for i := 0; i < n-1; i++ {
		if bars[i].OpenTime > prevTickOpen {
			first = i
			break
		}
	}
	if first < 0 {
		return nil, 0
	}
	if missed := (n - 1) - first; missed > mentorCatchUpMax {
		skipped := missed - mentorCatchUpMax
		for k := 0; k < skipped; k++ {
			at.mentorCount("mentor_bars_skipped")
		}
		at.logWarnf("🧑‍🏫 mentor catch-up capped: %d missed closed bars, the oldest %d are skipped (counter mentor_bars_skipped)", missed, skipped)
		first += skipped
	}
	var out []mentor.Intent
	ticked := 0
	for i := first; i < n-1; i++ {
		closeMs := mentor.BarCloseInstant(bars[i])
		ins := at.mentorEval.Tick(bars[:i+1], closeMs)
		if tap := at.mentorIntentTap; tap != nil {
			for _, in := range ins {
				tap(in)
			}
		}
		for _, l := range at.mentorNoteORB(at.mentorEval.State.ORB, closeMs) {
			at.logInfof("%s", l)
		}
		out = append(out, mentorStampCaughtUp(ins, bars[i].CloseTime)...)
		ticked++
		at.mentorCount("mentor_bars_caught_up")
	}
	at.logWarnf("🧑‍🏫 mentor caught up %d missed closed bar(s) %s–%s CT before the %s CT bar (the event path missed their FINAL) — entries from them are refused as stale (N10), arm actions apply (counter mentor_bars_caught_up)",
		ticked, kernel.ClockHHMMCT(time.UnixMilli(bars[first].OpenTime)), kernel.ClockHHMMCT(time.UnixMilli(bars[n-2].OpenTime)), kernel.ClockHHMMCT(time.UnixMilli(bars[n-1].OpenTime)))
	return out, ticked
}

// mentorDayGateVerdictLabel maps the §7 day-gate verdict to the label the
// once-per-day line prints (owner ruling 2026-10-09: TRADE | DAY OFF | SPENT).
// DayNotMeasured is fail-closed and unmeasurable → n/a.
func mentorDayGateVerdictLabel(v mentor.DayVerdict) string {
	switch v {
	case mentor.DayOff:
		return "DAY OFF"
	case mentor.DaySpent:
		return "SPENT"
	case mentor.DayNotMeasured:
		return "n/a"
	default:
		return "TRADE"
	}
}

// mentorDayGateLine renders the once-per-day day-gate INFO line from the
// report the evaluator captured at the 08:30 latch — the gate's OWN inputs,
// with n/a for anything the gate did not know (owner ruling 2026-10-09).
func mentorDayGateLine(r mentor.DayGateReport) string {
	run, hi, lo := "n/a", "n/a", "n/a"
	if r.HaveRun {
		run = fmt.Sprintf("%.0f", r.RunPts)
		hi = fmt.Sprintf("%.2f @%s", r.HiPx, kernel.ClockHHMMCT(time.UnixMilli(r.HiAt)))
		lo = fmt.Sprintf("%.2f @%s", r.LoPx, kernel.ClockHHMMCT(time.UnixMilli(r.LoAt)))
	}
	four, one := string(r.FourHDir), string(r.OneHDir)
	if four == "" {
		four = "n/a"
	}
	if one == "" {
		one = "n/a"
	}
	conflict := "no"
	if r.Conflict {
		conflict = "yes"
	}
	// Release #22 (P2-3): "@08:30" only when the latch was READ at 08:30. A
	// restart (or a feed gap) after the open re-latches on the 4h/1h state of
	// that later moment — the line names the real read instant.
	when := "@08:30 CT"
	open := ""
	if r.Restored {
		// B1 (release #30): the seed restored the 08:30 latch from the
		// 08:30 read — the 4h/1h below ARE the 08:30 decision. Checked
		// BEFORE ReadAtOpen: a restored report's ReadAt IS the 08:30
		// instant (that is what was restored), but this process did not
		// read it live at 08:30 — it replayed it at boot. Say so.
		when = fmt.Sprintf("RESTORED from the 08:30 read (trading day %s — the 4h/1h below are the 08:30 decision's)", r.Key)
	} else if !r.ReadAtOpen() {
		at := "n/a"
		if r.ReadAt > 0 {
			at = kernel.ClockHHMMCT(time.UnixMilli(r.ReadAt))
		}
		if r.BootRead {
			// MENTOR-NY item 6 (S2): a boot after 08:30 re-reads the day
			// gate on the 4h/1h of the boot moment. Say so, and show what the
			// 08:30 decision read (replayed by the seed) when it differs —
			// 10-13 printed "4h short · 1h short" at 08:30 and "4h long · 1h
			// long" at the 08:49 boot, which read as a contradiction.
			when = fmt.Sprintf("re-read at boot %s CT, not the 08:30 decision (trading day %s — the 4h/1h below are the boot's)", at, r.Key)
			o4, o1 := string(r.OpenFourHDir), string(r.OpenOneHDir)
			if o4 == "" {
				o4 = "n/a"
			}
			if o1 == "" {
				o1 = "n/a"
			}
			if o4 != four || o1 != one {
				open = fmt.Sprintf(" · the 08:30 decision read 4h %s · 1h %s (DIFFERENT — the boot re-latched on the values above)", o4, o1)
			} else {
				open = " · same 4h/1h as the 08:30 decision"
			}
		} else {
			when = fmt.Sprintf("RE-LATCHED @%s CT, not the 08:30 read (restart or feed gap; trading day %s — the 4h/1h below are read at %s)", at, r.Key, at)
		}
	}
	return fmt.Sprintf("🧑‍🏫 day gate %s: %s — overnight run %s pts (hi %s, lo %s, line %.0f) · 4h %s · 1h %s · conflict %s%s [D5.1 p1 @19:22]",
		when, mentorDayGateVerdictLabel(r.Verdict), run, hi, lo, r.SpentPts, four, one, conflict, open)
}

// mentorNoteORB returns the ORB lines due at this tick (MENTOR-NY item 5) and
// records them as logged. It reads the evaluator's ORB only — it never moves
// it (the ORB logic is orb.go's). closeMs is the evaluator clock of the tick.
func (at *AutoTrader) mentorNoteORB(orb mentor.ORB, closeMs int64) []string {
	if !orb.Drawn || orb.Day == 0 {
		return nil
	}
	day := time.UnixMilli(orb.Day).In(kernel.CTLocation()).Format("2006-01-02")
	var out []string
	if at.mentorORBDrawLogged != orb.Day {
		at.mentorORBDrawLogged = orb.Day
		out = append(out, fmt.Sprintf("🧑‍🏫 ORB %s: range %.2f–%.2f (%.2f pts) from the 08:30–08:32 CT candle — the escape picks the side [X5 @01:20–04:58]",
			day, orb.Low, orb.High, orb.High-orb.Low))
	}
	if orb.Escaped != "" && (at.mentorORBEscDay != orb.Day || at.mentorORBEscSide != string(orb.Escaped)) {
		prev := at.mentorORBEscSide
		if at.mentorORBEscDay != orb.Day {
			prev = ""
		}
		at.mentorORBEscDay, at.mentorORBEscSide = orb.Day, string(orb.Escaped)
		was := ""
		if prev != "" {
			was = fmt.Sprintf(" (was %s)", prev)
		}
		out = append(out, fmt.Sprintf("🧑‍🏫 ORB %s: escape %s%s read @%s CT (range %.2f–%.2f) — only %s entries pass the ORB gate while it stands",
			day, orb.Escaped, was, kernel.ClockHHMMCT(time.UnixMilli(closeMs)), orb.Low, orb.High, orb.Escaped))
	}
	return out
}

// mentorEODFlatAuthorRefusal (m425a S3) refuses to AUTHOR an intraday mentor
// arm inside the EOD-flat window. It asks the SAME two questions the rest of
// the system asks — never a copy of the times: forceFlatWindowAt (the entry
// admission gate's force-flat / in-session EOD-flat window) and the session
// registry's ActiveSession (the armed pass's "session ended (EOD flat)"
// cancel, armed_executor.go). SWING4H is exempt, exactly as it is at
// admission (the course holds the swing by the 4h, D5.2).
func (at *AutoTrader) mentorEODFlatAuthorRefusal(in mentor.Intent, now time.Time) (bool, string) {
	if strings.EqualFold(in.Setup, "SWING4H") {
		return false, ""
	}
	if why, due := at.forceFlatWindowAt(now, nil); due {
		return true, why
	}
	// m426: mirror the armed pass EXACTLY — it cancels "session ended (EOD
	// flat)" only when there is NO active AI day plan (plan == nil && !sessOK,
	// armed_executor.go). With a plan active the arm would survive there, so
	// authoring must not refuse on this clause either.
	if kernel.ActivePlanFor(at.id, at.futuresSymbol()) != nil {
		return false, ""
	}
	if _, active := at.sessionRegistry(now).ActiveSession(now); !active {
		return true, "session ended (EOD flat) — the armed pass cancels an intraday mentor arm the moment the session is over"
	}
	return false, ""
}

// mentorPlaceIntent runs the placement gates (sources wired, stop rules,
// expiry guard, no-chase, the exit fork) and hands the intent to the ONE
// mentor entry path (mentorDirectPlace): a ledger arm with origin=mentor and
// the intent's expiry, placed ALWAYS stop-limit, never stop-market.
// The no-chase rule runs FIRST: a stop entry whose price is already through
// the trigger is skipped — he never enters at market (§3).
func (at *AutoTrader) mentorPlaceIntent(in mentor.Intent, choice mentorSizeChoice, barCloseMs, emitMs int64) {
	// STALE-DATA BLOCK (release #11): refuse to AUTHOR a new arm while the live
	// 1m feed is stale (B4's formula). Counted + WARN'd once per episode inside
	// mentorStaleDataBlocked. Exits / protection are never gated; resting broker
	// orders are never cancelled by this.
	// m445: the stale leg and the missing-bars legs are ONE verdict
	// (dataIntegrityVerdictAt); the stale leg answers first, exactly as before.
	dv := at.dataIntegrityVerdictAt(mentorIntegrityWindow(in), mentorClockNow(), true)
	if dv.Blocked && !dv.isData() {
		why := dv.Reason
		// A setup computed on stale prices is not a setup. The deferred guard in
		// mentorDispatchEntry drops the evaluator's arm on this refusal. LOUD:
		// every sized Place refused here is named and counted (the "never
		// silent" rule). The bar age is READ from the same source
		// mentorStaleDataBlocked reads; "n/a" when there is no bar to read.
		now := mentorClockNow()
		newestOpen := int64(0)
		if bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 1); len(bars) > 0 {
			newestOpen = bars[len(bars)-1].OpenTime
		}
		age := "n/a"
		if newestOpen > 0 {
			age = fmt.Sprintf("%ds", (now.UnixMilli()-(newestOpen+60_000))/1000)
		}
		at.mentorCount("placement_refused_stale_data")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — stale 1m feed (newest bar %s old; %s): %s %s %s @ %.2f — never reaches the ledger",
			age, why, in.ArmID, in.Setup, in.Side, in.Price)
		// STALLED-SETUP ALARM (release #22): record the refusal reason so a
		// stalled kind's alert names "stale_data" instead of "silent drop"
		// (the 10-11 Sunday NT8 lag refused level orders quietly here).
		at.mentorRecordStallRefusal(in, "stale_data")
		at.mentorGateBlock("mentor_stale_data")
		return
	}
	// m445 DATA INTEGRITY: a setup computed on a data window with a hole in it
	// (or while the detector cannot say) is not a setup either. Counted, named,
	// never silent; the deferred guard in mentorDispatchEntry drops the arm.
	if dv.Blocked {
		at.mentorCount("placement_refused_data_integrity")
		// ONE WARN per episode and class (review round 2, reviewer A); the
		// counters and the stall record below still see every refusal.
		if at.admitLast.changed("data-integrity|mentor-place", dv.Class) {
			at.logWarnf("🧑‍🏫 mentor placement REFUSED — data_integrity (%s): %s — %s %s %s @ %.2f never reaches the ledger (further refusals in this episode are counted, not logged)", dv.Class, dv.Reason, in.ArmID, in.Setup, in.Side, in.Price)
		}
		at.mentorRecordStallRefusal(in, "data_integrity")
		at.mentorGateBlock(dv.Class)
		return
	}
	at.admitLast.clear("data-integrity|mentor-place") // the episode ended: the next one logs again
	// N10 (stale intent): an entry whose reference candle is not the newest
	// closed bar came from a reload replay of stale box/ISB/PHL state — never
	// a live order. The swing is exempt (RefBarMs 0 — gated by its own 5m
	// watermark). Counted, never silent.
	if in.RefBarMs != 0 && in.RefBarMs != barCloseMs {
		at.mentorCount("stale_intent")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — stale %s intent: reference bar close %d ≠ newest closed bar %d", in.Setup, in.RefBarMs, barCloseMs)
		at.mentorRecordStallRefusal(in, "stale_intent") // MENTOR-NY item 9 (rescan P3-N9)
		at.mentorGateBlock("mentor_stale_intent")
		return
	}
	// WIRING PROOF (fail-closed): with any mentor source seam missing, EVERY
	// entry refuses here — the boot line logs it, this line enforces it.
	if missing := at.mentorSourcesBlocking(in.Setup); len(missing) > 0 {
		at.mentorCount("mentor_sources_missing")
		at.logErrorf("🧑‍🏫 mentor sources MISSING [%s] — refusing the %s entry (fail-closed)", strings.Join(missing, ", "), in.Setup)
		at.mentorRecordStallRefusal(in, "sources_missing: "+strings.Join(missing, ", "))
		at.mentorGateBlock("mentor_sources_missing")
		return
	}
	// STOP RULES (owner ruling 00:1x CT, "exactly like he said") — class
	// rules, default ON: (b) the trading window (swing exempt), (a) done for
	// the day after a win, F11 news 07:30, (d) never add/average. Then the
	// no-chase rule.
	if refuse, why := at.mentorWindowGate(in); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "window: "+why)
		at.mentorGateBlock("mentor_window")
		return
	}
	// m425a S3: no INTRADAY arm is authored inside the EOD-flat window — the
	// armed pass cancels intraday mentor arms the instant the session has
	// ended, so an arm authored at 15:30:00.081 / 15:35:00.084 CT (rows 447 /
	// 448, 10-23) lived 10 ms and wrote a ledger row for nothing.
	if refuse, why := at.mentorEODFlatAuthorRefusal(in, mentorClockNow()); refuse {
		at.mentorCount("placement_refused_eod_flat")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — eod_flat: %s", why)
		at.mentorRecordStallRefusal(in, "eod_flat: "+why)
		at.mentorGateBlock("mentor_eod_flat")
		return
	}
	if refuse, why := at.mentorDoneAfterWinGate(); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "done_after_win: "+why)
		at.mentorGateBlock("mentor_done_after_win")
		return
	}
	if refuse, why := at.mentorStopAfterLossGate(); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "stop_after_loss: "+why)
		at.mentorGateBlock("mentor_stop_after_loss")
		return
	}
	if hold, why := at.mentorNewsGate(); hold {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "news_hold: "+why)
		at.mentorGateBlock("mentor_news_hold")
		return
	}
	// The never-add gate moved BELOW no-chase (m151): the resonance arm fires at
	// the add-gate refusal ONLY after every other gate has passed.
	// §5(a) — the daily guardrails are checked AT PLACEMENT (r22: daily loss
	// on the bot's own P&L incl. a latched trip, profit target, max daily
	// trades), and again at the send by the admission chain; the 60s monitor
	// beat cancels resting entries and flattens on a daily-loss breach.
	if refuse, why := at.mentorDailyLossGate(mentorClockNow()); refuse {
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "daily_loss: "+why)
		return
	}
	// N12 per-order expiry (PR #313): the stop-limit is cancelled when
	// unfilled at its expiry. An intent-carried expiry (evaluator rules,
	// stacking extensions) wins; otherwise the injector sets the setup's
	// default.
	if in.ExpiryMs == 0 {
		in.ExpiryMs = mentorIntentExpiry(in, barCloseMs)
		at.mentorCount("expiry_defaulted")
	}
	// F3 (CTO 1791035117415): every mentor arm must carry an expiry — an arm
	// without one is REFUSED fail-closed (it must never sit unexpiring).
	if refuse, why := mentorExpiryGuard(in.ExpiryMs); refuse {
		at.mentorCount("expiry_missing_refused")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "expiry_missing")
		at.mentorGateBlock("mentor_expiry_missing")
		return
	}
	if latest, ok := at.mentorLatestPrice(); ok {
		if skip, why := mentorNoChase(in.Side, latest, in.Price); skip {
			at.mentorCount("no_chase_skip")
			at.logWarnf("🧑‍🏫 mentor NO-CHASE skip: %s", why)
			at.mentorRecordStallRefusal(in, "no_chase: "+why)
			at.mentorGateBlock("mentor_no_chase")
			return
		}
	}
	// ONE WORKING ENTRY PER SIDE (slot; release #30): the reference is the FIRST
	// working entry; a new same-side arm forms only after the previous one is
	// filled, cancelled or expired [CTO parity ruling 1791008332386 #1,
	// generalized from isbArmActive to EVERY kind via the armed ledger]. m151:
	// this runs BEFORE the add gate — a same-side ISB must pass THIS gate too
	// before it can arm resonance.
	if same := at.mentorWorkingSameSideRows(string(in.Side)); len(same) > 0 {
		at.mentorCount("refused_same_side_working")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — one working entry per side: a %s entry already works (%s) [D1.1 p1 @17:28–17:44 + CTO parity ruling 1791008332386 #1]",
			in.Side, mentorArmIDFromScenario(same[0].Scenario))
		at.mentorRecordStallRefusal(in, "same_side_working: one working entry per side")
		at.mentorGateBlock("mentor_same_side_working")
		return
	}
	// NEVER-ADD gate (m151: moved BELOW no-chase, so every other gate has run).
	// A same-side ISB entry that passes every gate EXCEPT this one is the
	// resonance HOLD signal — arm resonance (mode A), place nothing. An
	// opposite-side (no-flip) entry, and every non-ISB same-side entry, is a
	// normal refusal.
	if refuse, why := at.mentorAddGate(in); refuse {
		if isISBEntryIntent(in) {
			if open := at.mentorOpenSideSource(); open != "" && strings.EqualFold(open, string(in.Side)) {
				at.mentorArmResonanceOnISB(string(in.Side))
				at.mentorGateBlock("mentor_resonance_hold")
				return
			}
		}
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — %s", why)
		at.mentorRecordStallRefusal(in, "never_add: "+why)
		at.mentorGateBlock("mentor_never_add")
		return
	}
	// A/B/C EXIT FORK AT ENTRY (CTO 1791029620038): the branch is chosen from
	// the intent flags NOW and recorded; the P1 exit loop drives the chosen
	// branch per fill. A PHL/PLH starts as B with the resonance watch armed.
	forkMode, forkTP, forkWhy := mentorExitFork(in, mentorConfluenceFlag(in))
	at.mentorCount("exit_fork_" + forkMode)
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
	at.setMentorExitMode(at.mentorScenarioFor(in.ArmID), forkMode)
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
// candle. The close-through cancel is [D2.3 p1 @23:16–24:03] ("a later candle
// closes through the level → cancel"). The 15:00 CT end is an INFERENCE — no
// course timestamp states it (level_lifetime.go admits it) — so it carries no
// course tag. At or after 15:00 CT it fail-safes to tomorrow's 15:00
// (placement past RTH is already refused upstream, so this is a guard, never
// the live path).
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
			at.mentorCount("exit_upgrade_no_position")
			at.logWarnf("🧑‍🏫 mentor confluence upgrade IGNORED — %s (%s)", why, in.Reason)
			continue
		}
		if next == lp.Pos.Mode {
			at.mentorCount("exit_upgrade_noop")
			at.logInfof("🧑‍🏫 mentor confluence upgrade: %s (%s) — %s", side, why, in.Reason)
			continue
		}
		lp.Pos.Mode = next
		at.mentorCount("exit_upgrade_c")
		at.logInfof("🧑‍🏫 mentor confluence upgrade: %s %s (size unchanged) — %s", side, why, in.Reason)
		// C52 W1 A2 [D3.4 p3 @10:07–10:33, @11:01–11:05 "vô rồi mới đợi
		// trigger… thấy trigger buy setup luôn thì hold, at least risk reward
		// 1-2"]: the branch switch alone left leg 1's TP at +1R on the wire (it
		// was set at entry). Move it out to 2R now, through the same leg-scoped
		// modify_bracket the other leg-1 TP moves use.
		at.mentorLeg1TPToC(at.armedTrader(), lp, "confluence upgrade")
		at.mentorPersistUpgradedExitMode("open position "+lp.Legs[0].SignalID, func(l *store.ArmedOrderStore) (int64, error) {
			return l.SetExitModeBySignal(lp.Legs[0].SignalID, next)
		})
	}
	// A school-1 arm that is still RESTING: switch its staged branch so the
	// fill registers C. The live-arm registry names the arm by its ArmID; its
	// scenario is the key the placement registered under. I3/I5: the entries
	// are COPIED under mentorLiveMu before iterating (a concurrent write must
	// not race the iteration), and only THIS trader's arms are considered.
	// REL18-A: the scenario is the STORED one on the entry, never recomputed
	// with the current epoch (a reload would break the lookup).
	type restingArm struct {
		id, scenario, side string
		rowID              int64
	}
	resting := make([]restingArm, 0)
	mentorLiveMu.Lock()
	for _, arm := range mentorLiveArms {
		if arm.TraderID == at.id {
			resting = append(resting, restingArm{id: arm.ArmID, scenario: arm.Scenario, side: arm.Side, rowID: arm.RowID})
		}
	}
	mentorLiveMu.Unlock()
	for _, ra := range resting {
		if !strings.EqualFold(ra.side, side) {
			continue
		}
		scenario := ra.scenario
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
		at.mentorCount("exit_upgrade_c")
		at.logInfof("🧑‍🏫 mentor confluence upgrade (resting %s): %s %s (size unchanged) — %s", ra.id, side, why, in.Reason)
		rowID := ra.rowID
		at.mentorPersistUpgradedExitMode(fmt.Sprintf("resting arm %s (row %d)", ra.id, rowID), func(l *store.ArmedOrderStore) (int64, error) {
			return l.SetExitMode(rowID, next)
		})
	}
	if !found {
		at.mentorCount("exit_upgrade_no_position")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade IGNORED — no open or resting mentor position on the %s side (%s)", side, in.Reason)
	}
}

// mentorPersistUpgradedExitMode writes the upgraded exit branch onto the arm's
// row (MENTOR-NY item 11, rescan P2-N3) so a restart rebuilds the position as
// C instead of the row's original B. Counted either way; a failed or empty
// write is a WARN, never silent. The per-arm vs per-side reach of the upgrade
// itself is an OWNER decision and is unchanged here.
func (at *AutoTrader) mentorPersistUpgradedExitMode(what string, write func(*store.ArmedOrderStore) (int64, error)) {
	if at.store == nil {
		at.mentorCount("exit_upgrade_persist_failed")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade NOT persisted for %s — no store (a restart would rebuild the row's original mode)", what)
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		at.mentorCount("exit_upgrade_persist_failed")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade NOT persisted for %s — no armed ledger", what)
		return
	}
	n, err := write(ledger)
	if err != nil || n == 0 {
		at.mentorCount("exit_upgrade_persist_failed")
		at.logWarnf("🧑‍🏫 mentor confluence upgrade NOT persisted for %s — %d row(s) changed, err=%v (a restart would rebuild the row's original mode)", what, n, err)
		return
	}
	at.mentorCount("exit_upgrade_persisted")
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

// mentorNowSource is the clock seam for every mentor time gate (tests). It is
// an atomic.Value because runArmedEventLoop (a background goroutine) reads it
// through mentorClockNow while a test writes it; a plain func global raced
// (release #26 -race: TestStaleGateRefusesDeadFeedUnderBackwardWallStep write
// vs mentorPlacementCadencePass read).
var mentorNowSource atomic.Value // func() time.Time

// setMentorNowSource installs (or clears, with nil) the test clock seam.
// Every test write goes through here so the read in mentorClockNow is atomic.
func setMentorNowSource(fn func() time.Time) {
	mentorNowSource.Store(fn)
}

// getMentorNowSource returns the installed seam (nil when unset) for tests that
// capture and restore it.
func getMentorNowSource() func() time.Time {
	if fn, ok := mentorNowSource.Load().(func() time.Time); ok {
		return fn
	}
	return nil
}

// mentorClockNow is the one clock read for the time gates.
func mentorClockNow() time.Time {
	if fn, ok := mentorNowSource.Load().(func() time.Time); ok && fn != nil {
		return fn()
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
	slice, err := at.store.Calendar().GetSlice(plannerTradeDateCT(mentorClockNow()))
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
			at.mentorCount("news_hold_no_calendar")
			at.logWarnf("🧑‍🏫 news: calendar slice missing/unreadable — holding the 07:20–07:35 CT window fail-closed [F11]")
			return true, "news: calendar slice missing/unreadable — the 07:20–07:35 CT window holds (fail-closed) [F11]"
		}
		return false, ""
	}
	hold, why := mentorNewsHold(evs, now)
	if hold {
		at.mentorCount("news_hold")
	}
	return hold, why
}

// mentorNewsCancelFlattenAt (item 18 part 2, R12) cancels live INTRADAY mentor
// arms by ArmID and flattens intraday mentor positions at print −10m on a
// red-folder print day. INFERENCE [B], not a course instruction (release #52
// B6): D4.4 p1 @20:44–21:46 gives three reasons not to be IN the print (the
// candle jerks and "banh 2 đầu", the machine can lag, you cannot size the
// candle) and "lát không out lệnh được cũng banh"; no sentence says to close
// an open trade before it. The flatten is the 10-04 CTO re-read (DAY-4 row
// D4.4-08, "PARTIAL"), kept as the conservative reading until the owner
// rules; whether an open runner is held through the print is a mentor
// question. It is the DESTRUCTIVE half of
// the news window: the hold above refuses NEW placements through the print;
// this sweep clears what already REMAINS before the 07:30 print. SWING4H arms
// and positions are exempt (the course holds the swing by the 4h [D5.2 p1 @06:03–06:20]).
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
// arms (the news/window/day-stop cancels hold the swing by the 4h [D5.2 p1 @06:03–06:20]); the
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
	for _, arm := range mentorLiveArms {
		entries = append(entries, entry{id: arm.ArmID, traderID: arm.TraderID})
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
			// r22: this all-arms path is shared by the never-add sweep and the
			// daily-loss breach; the reason names which one acted.
			at.logWarnf("🧑‍🏫 all-arms cancel: cancelled %d live mentor arm(s) (SWING4H included — fail-closed) — %s", cancelled, reason)
		}
	}
	return cancelled > 0
}

// mentorOpenPositionActive reports whether a mentor position is open. The
// open-side seam is the primary read (the P1 driver wires it to the bound
// account's position snapshot); the account position count is the fail-closed
// backstop — an unreadable position list counts as OPEN, never flat.
func (at *AutoTrader) mentorOpenPositionActive() bool {
	if at.mentorOpenSideSource != nil && at.mentorOpenSideSource() != "" {
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
// SWING4H is exempt (the course holds the swing by the 4h [D5.2 p1 @06:03–06:20]).
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
			at.mentorCount("day_stop_cancel_done_after_win")
		}
	}
	if ended, why := at.mentorWindowEnded(now); ended {
		if at.cancelLiveIntradayMentorArmsWith("day-stop window end: " + why) {
			at.mentorCount("day_stop_cancel_window_end")
		}
	}
	if hold, why := at.mentorNewsGate(); hold {
		if at.cancelLiveIntradayMentorArmsWith("day-stop news hold: " + why) {
			at.mentorCount("day_stop_cancel_news")
		}
	}
	// NEVER-ADD [D1.1 p1 @17:06–17:44 — same side only], now enforced while a
	// position is OPEN, not only at write time (L4): with B4 more mentor arms
	// rest at the broker at once, so a second fill while a position is open
	// would ADD (same side, D1.1) or REDUCE/FLIP (opposite side — the owner's
	// "no flip" ruling 2026-09-03). Cancel the resting unfilled mentor arms on
	// BOTH sides, once per open-position episode. SWING4H: the course is silent
	// on a swing coexisting with an intraday position, so it is cancelled too
	// (fail-closed: one position at a time).
	if at.mentorOpenPositionActive() {
		if !at.mentorNeverAddCancelDone {
			if at.cancelAllLiveMentorArmsWith("day-stop never-add: a mentor position is open — one position at a time, cancel both sides [D1.1 p1 @17:06–17:44 same side; no-flip = owner ruling 2026-09-03 opposite side]") {
				at.mentorCount("day_stop_cancel_never_add")
			}
			at.mentorNeverAddCancelDone = true
		}
	} else {
		at.mentorNeverAddCancelDone = false
	}
	if at.mentorStopAfterLossTripped != nil {
		if stop, why := at.mentorStopAfterLossTripped(); stop {
			if at.cancelLiveIntradayMentorArmsWith("day-stop stop-after-loss: " + why) {
				at.mentorCount("day_stop_cancel_stop_after_loss")
			}
		}
	}
}

// mentorDoneAfterWinEnabled resolves the done-after-win switch. Owner order
// 2026-10-16 ("it's been off … follow it"): nil → OFF — only an EXPLICIT true
// turns the rule on. The day-boundary knob mentor_done_after_win_day_start is
// read only when this switch is ON.
func (at *AutoTrader) mentorDoneAfterWinEnabled() bool {
	if at.config.StrategyConfig == nil {
		return false
	}
	v := at.config.StrategyConfig.RiskControl.MentorDoneAfterWin
	if v != nil {
		return *v
	}
	return true // nil → ON (the course default: "làm ơn đóng máy lại" D1.2 p2 @06:09; m186 reverts the 2026-10-16 OFF)
}

// mentorDoneAfterWinTripped reports the DEFINITE done-after-win trip only — a
// trade closed in profit AND the day is net positive AND the switch is ON (nil
// → ON, the course default; explicit false turns it off). An unwired or
// unresolved source is "unknown", not a trip: the placement gate still refuses
// fail-closed, but the sweep does NOT force-cancel a resting arm on an unknown.
func (at *AutoTrader) mentorDoneAfterWinTripped() (bool, string) {
	if !at.mentorDoneAfterWinEnabled() {
		return false, "" // the switch is OFF (an explicit false; nil → ON)
	}
	if at.mentorDayNetSource == nil || at.mentorClosedProfitSource == nil {
		return false, ""
	}
	net, okNet := at.mentorDayNetSource()
	closed, okClosed := at.mentorClosedProfitSource()
	if !okNet || !okClosed {
		return false, ""
	}
	if mentorDoneAfterWin(net, closed) {
		return true, at.mentorDoneAfterWinReason()
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
		return true, fmt.Sprintf("the trading window ended at %s CT [D1.2 p1 @23:49–24:21, @24:36–24:59]", kernel.CloseHHMMCT(end))
	}
	return false, ""
}

// ── STOP RULES (owner ruling 00:1x CT, "exactly like he said") ─────────────

// (b) TRADING WINDOW [D1.2 p1 @23:49–24:21, @24:36–24:59]: a fixed window —
// when it ends, no new entries. Default 08:30–09:30 CT, 60 minutes; knobs for
// the start and the length (30/60/90/120; -1 disables the window — a stored 0
// is "unset", so it resolves to the default 60). The SWING setup is exempt
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
		return false, fmt.Sprintf("trading window start %q unparseable — entries refused (fail-closed) [D1.2 p1 @23:49–24:21, @24:36–24:59]", start)
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
	return false, fmt.Sprintf("outside the trading window %s–%s CT — no new entries [D1.2 p1 @23:49–24:21, @24:36–24:59]", kernel.CloseHHMMCT(open), kernel.CloseHHMMCT(end))
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
	at.mentorCount("window_refused")
	return true, why
}

// (a) DONE FOR THE DAY AFTER A WIN [D1.2 p1 @20:53–21:16; p2 @05:28–06:27]:
// once a trade closes in profit and the day's net P&L is above 0 → no new
// mentor entries until the next NY open (08:30 CT; day boundary = owner ruling
// 2026-10-09 — the mentor's "day" is the NY day, not the CME 17:00 session day).
// The switch is default OFF (nil → OFF, owner order 2026-10-16 "it's been off…
// follow it"; only an explicit true turns it on).
func mentorDoneAfterWin(dayNetPnl float64, closedInProfit bool) bool {
	return closedInProfit && dayNetPnl > 0
}

// mentorDoneAfterWinDayStartDefault is the done-after-win day boundary (owner
// ruling 2026-10-09): 08:30 CT — the NY open. A win at 18:06 CT belongs to the
// day that started 08:30 and no longer blocks the next NY open; a NY win at
// 10:00 blocks until 08:30 the next day (the evening session included).
const mentorDoneAfterWinDayStartDefault = "08:30"

// mentorDoneAfterWinDayStart resolves the knob (""/nil → the 08:30 default).
func (at *AutoTrader) mentorDoneAfterWinDayStart() string {
	start := mentorDoneAfterWinDayStartDefault
	if at.config.StrategyConfig != nil {
		if v := strings.TrimSpace(at.config.StrategyConfig.RiskControl.MentorDoneAfterWinDayStart); v != "" {
			start = v
		}
	}
	return start
}

// mentorDayStartAt returns the most recent occurrence of the "HH:MM" CT day
// start at or before now (today's, or yesterday's when today's is still ahead).
// ok=false on an unparseable start (the caller fails closed).
func mentorDayStartAt(start string, now time.Time) (time.Time, bool) {
	hour, minute, ok := parseMentorWindowStart(start)
	if !ok {
		return time.Time{}, false
	}
	loc := kernel.CTLocation()
	ct := now.In(loc)
	d := time.Date(ct.Year(), ct.Month(), ct.Day(), hour, minute, 0, 0, loc)
	if d.After(now) {
		d = time.Date(ct.Year(), ct.Month(), ct.Day()-1, hour, minute, 0, 0, loc)
	}
	return d, true
}

// mentorDoneAfterWinReason renders the refusal with the resolved boundary.
func (at *AutoTrader) mentorDoneAfterWinReason() string {
	start := at.mentorDoneAfterWinDayStart()
	label := "the next NY open (" + start + " CT)"
	if start != mentorDoneAfterWinDayStartDefault {
		label = "the next trading day (" + start + " CT)"
	}
	return fmt.Sprintf("done for the day after a win — a trade closed in profit and the day is net positive; no new entries until %s [rule D1.2 p1 @20:53–21:16 + p2 @05:28–06:27; boundary = owner ruling 2026-10-09 (transcript silent)]", label)
}

// at.mentorDayNetSource / at.mentorClosedProfitSource are the session seams for (a).
// REL18-B: they are per-trader AutoTrader fields (set by the wiring); a nil
// source → unwired (fail closed). A wired seam that returns ok=false means the
// day's data is UNRESOLVED (a NULL pnl_corrected closed row or a read error) —
// an unknown is not "no win", so the gate fails CLOSED.

// mentorDoneAfterWinGate is the call-site half of (a); the switch is default
// ON (nil → ON, the course default — only an explicit false turns it off;
// m186 reverts the 2026-10-16 owner order). FAIL-CLOSED while ON: a missing
// day-net or closed-profit source refuses the entry — an unknown is not
// "no win".
func (at *AutoTrader) mentorDoneAfterWinGate() (bool, string) {
	if !at.mentorDoneAfterWinEnabled() {
		return false, "" // the switch is OFF (an explicit false; nil → ON)
	}
	if at.mentorDayNetSource == nil || at.mentorClosedProfitSource == nil {
		at.mentorCount("done_after_win_no_data")
		at.logWarnf("🧑‍🏫 mentor done-after-win source missing (day net / closed profit) — refusing the entry (fail-closed)")
		return true, "done-after-win: day P&L or closed-trade source not wired — an unknown is not 'no win'; refusing (fail-closed) [D1.2 p1 @20:53–21:16]"
	}
	net, okNet := at.mentorDayNetSource()
	closed, okClosed := at.mentorClosedProfitSource()
	if !okNet || !okClosed {
		at.mentorCount("done_after_win_unresolved")
		at.logWarnf("🧑‍🏫 mentor done-after-win day UNRESOLVED (a NULL pnl_corrected closed row or a read error) — refusing the entry (fail-closed)")
		return true, "done-after-win: day P&L unresolved (a NULL pnl_corrected closed row) — an unknown is not 'no win'; refusing (fail-closed) [D1.2 p1 @20:53–21:16]"
	}
	if mentorDoneAfterWin(net, closed) {
		at.mentorCount("done_after_win_refused")
		return true, at.mentorDoneAfterWinReason()
	}
	return false, ""
}

// mentorStopAfterLossGate — STOP-AFTER-LOSS (owner "ok" 2026-10-05; FOLD
// 2026-10-16 reverts course-defaults-30): once a MENTOR trade closes today
// (17:00 CT session-day) with a net LOSS (both legs combined; pnl_corrected
// < 0), refuse new mentor entries until the next session day
// [D1.2 p1 @23:34–23:46 (personal routine); p2 @02:44–03:27 (general,
// state-based)].
// A breakeven close (0) is NOT a loss. The knob is default OFF (nil → OFF;
// byte-identical while off). FAIL-CLOSED like done-after-win: an unwired source
// or an UNRESOLVED close refuses, with its own counters.
func (at *AutoTrader) mentorStopAfterLossGate() (bool, string) {
	if at.config.StrategyConfig == nil {
		return false, "" // no config → the knob reads OFF (nil → OFF, the default)
	}
	if v := at.config.StrategyConfig.RiskControl.MentorStopAfterLoss; v == nil || !*v {
		return false, "" // the knob is OFF (nil → OFF, the default)
	}
	if at.mentorClosedLossSource == nil {
		at.mentorCount("stop_after_loss_no_data")
		at.logWarnf("🧑‍🏫 mentor stop-after-loss source missing (closed loss) — refusing the entry (fail-closed)")
		return true, "stop-after-loss: closed-loss source not wired — an unknown is not 'no loss'; refusing (fail-closed) [D1.2 p1 @23:34–23:46 (personal routine); p2 @02:44–03:27 (general, state-based)]"
	}
	closed, ok := at.mentorClosedLossSource()
	if !ok {
		at.mentorCount("stop_after_loss_unresolved")
		at.logWarnf("🧑‍🏫 mentor stop-after-loss day UNRESOLVED (a NULL pnl_corrected closed row or a read error) — refusing the entry (fail-closed)")
		return true, "stop-after-loss: day unresolved (a NULL pnl_corrected closed row) — an unknown is not 'no loss'; refusing (fail-closed) [D1.2 p1 @23:34–23:46 (personal routine); p2 @02:44–03:27 (general, state-based)]"
	}
	if closed {
		at.mentorCount("stop_after_loss_refused")
		return true, "stop for the day after a loss — a mentor trade closed today with a net loss; no new entries until the next trading day (17:00 CT) [D1.2 p1 @23:34–23:46 (personal routine); p2 @02:44–03:27 (general, state-based)]"
	}
	return false, ""
}

// at.mentorStopAfterLossTripped is the B3 day-stop-sweep hook for the
// stop-after-loss gate (wired in mentorWireProductionSeams next to
// at.mentorClosedLossSource). REL18-B: per-trader field. nil → unwired (the sweep
// does not trip). It returns (true, why) ONLY on a DEFINITE loss (the knob ON
// and a resolved closed loss); an unwired or unresolved read returns (false, "")
// — the placement gate stays fail-closed, but the sweep never force-cancels a
// resting arm on an unknown (the same contract as mentorDoneAfterWinTripped).

// mentorStopAfterLossTrip is the DEFINITE-trip computation behind the B3 sweep
// hook: the knob ON and a resolved closed loss, nothing else.
// nil → OFF (the default; FOLD 2026-10-16 reverts course-defaults-30).
func (at *AutoTrader) mentorStopAfterLossTrip() (bool, string) {
	if at.config.StrategyConfig == nil {
		return false, ""
	}
	if v := at.config.StrategyConfig.RiskControl.MentorStopAfterLoss; v == nil || !*v {
		return false, "" // the knob is OFF (nil → OFF, the default)
	}
	if at.mentorClosedLossSource == nil {
		return false, ""
	}
	closed, ok := at.mentorClosedLossSource()
	if !ok {
		return false, ""
	}
	if closed {
		return true, "a mentor trade closed today with a net loss — no entries until the next trading day (17:00 CT) [D1.2 p1 @23:34–23:46 (personal routine); p2 @02:44–03:27 (general, state-based)]"
	}
	return false, ""
}

// (d) NEVER ADD / AVERAGE [D1.1 p1 @17:00–17:44]: no second same-direction
// fill while a position is open. The resonance ISB is a hold signal, not an
// entry [D2.4 p1 @05:41–05:57].

// at.mentorOpenSideSource is the session seam for (d): the side of the open
// mentor position (nil → none open; the live driver fills it at P1).
// REL18-B: per-trader field.

// mentorAddGate is the call-site half of (d). FAIL-CLOSED: with no open-side
// source the entry refuses — an unknown open side could hide a same-direction
// add.
func (at *AutoTrader) mentorAddGate(in mentor.Intent) (bool, string) {
	if at.mentorOpenSideSource == nil {
		at.mentorCount("add_no_source")
		return true, "never-add guard: open-position source not wired — refusing the entry (fail-closed) [D1.1 p1 @17:00–17:44]"
	}
	open := at.mentorOpenSideSource()
	if open == "" {
		return false, ""
	}
	if strings.EqualFold(open, string(in.Side)) {
		at.mentorCount("add_refused")
		return true, fmt.Sprintf("never add/average [D1.1 p1 @17:00–17:44]: %s already open — the resonance ISB is a hold signal, not an entry [D2.4 p1 @05:41–05:57]", open)
	}
	// NO FLIP (release #30 fold, CTO ruling): an OPPOSITE-side entry while a
	// mentor position is open is refused too. At NT8 a same-bar opposite fill
	// nets the position and leaves the open trade's stop/TP working against the
	// wrong side (a long's sell-stop later opens a NEW short). The planner
	// already runs "no adds, no flips" (owner ruling 2026-09-03); the mentor
	// now matches. Once flat, the next tick may place the other side (the box
	// "Uno reverse" still trades after the stop, never before).
	at.mentorCount("mentor_no_flip")
	return true, fmt.Sprintf("no flip: %s open — flat first [derived: owner ruling 2026-09-03 (planner: no adds, no flips); course silent on flips]", open)
}

// mentorDailyLossGate is the §5(a) placement-time daily guardrail check: a
// mentor placement is refused while the daily loss limit is hit (or latched
// earlier today), and — r22 RISK-GATES — while the daily profit target or MAX
// DAILY TRADES is reached. Before r22 this read only the realized loss and the
// mentor path never evaluated max trades at all (they lived only inside the AI
// decision cycle, which a mentor trader never runs).
//
// It asks the ONE verdict every entry path asks (dailyGuardVerdictAt — the
// same kernel Check, the same resolved posture, the bot-only P&L), so the
// authoring step and the send-time admission can never disagree. Each refusal
// is counted per kind (and reaches the gate-block table through mentorCount).
// FAIL-CLOSED on an unresolved close today (pnl NULL — canon 40, CTO fold
// release 10-05-1), unchanged. Inert unless the guardrails master AND the leg
// are ON (the owner's live strategies: both OFF).
func (at *AutoTrader) mentorDailyLossGate(now time.Time) (bool, string) {
	v := at.dailyGuardVerdictAt(now)
	if !v.Refuse {
		return false, ""
	}
	switch v.Class {
	case "max_daily_trades":
		at.mentorCount("max_daily_trades_refused")
	case "daily_profit_target":
		at.mentorCount("daily_profit_refused")
	default: // daily_loss, daily_guardrail_unverified
		at.mentorCount("daily_loss_refused")
	}
	at.mentorGateBlock("mentor_" + v.Class)
	return true, v.Reason + " [guardrail]"
}

// mentorGateBlock files ONE mentor placement refusal in the per-trader
// gate-block table (GET /api/risk/gate-blocks, the daily summary line) — r22
// RISK-GATES, review P2-3: before r22 only mentor_ai_entries_off /
// mentor_ai_closes_off reached it, so 39 done-after-win refusals on 10-12 were
// absent from that day's summary. Called at each REFUSAL site in
// mentorPlaceIntent, never from a gate function (the day-stop sweep calls
// those every tick, and a sweep check is not a refusal — canon 35).
func (at *AutoTrader) mentorGateBlock(class string) {
	telemetry.IncGateBlock(at.id, class)
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
// REL18-B: per-trader field.

// mentorSourceDepth asks the seam, with a 1m BarCache fallback for the 1m
// source. Unknown means the seed has not provided it yet.
func (at *AutoTrader) mentorSourceDepth(name string) (int, bool) {
	if at.mentorSourceDepthSource != nil {
		return at.mentorSourceDepthSource(name)
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
// clears without a restart. REL18-B: per-trader fields (at.mentorSeedDepths /
// at.mentorSeedDepthsMu) — the seam is read from the placement path while the
// tick path writes it.

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
	at.mentorSeedDepthsMu.Lock()
	defer at.mentorSeedDepthsMu.Unlock()
	if at.mentorSeedDepths == nil {
		return
	}
	for name, d := range live {
		if d > at.mentorSeedDepths[name] {
			at.mentorSeedDepths[name] = d
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
	// REL18-A: each construction (boot or reload) gets its OWN arm epoch (a
	// per-AutoTrader field, never shared), so a rebuilt evaluator's "isb-1"
	// never collides with a prior terminal row — or another trader's row.
	// The epoch reads the mentor seam so a pinned test clock names the seed
	// instant; production (nil source) is time.Now(), unchanged.
	at.mentorArmEpoch = mentorClockNow().UnixNano()
	if at.mentorEval == nil {
		at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
	}
	// N1 (release #23): the refill generation is read BEFORE the store, so a
	// replay released while the seed reads it still triggers a re-seed.
	at.mentorSeedHistGen, _ = mentorRefillGeneration()
	at.mentorChartSeedGen = at.mentorSeedHistGen
	// The seed clock reads the mentor seam (mentorNowSource) so tests can pin a
	// fixed mid-session instant; production defaults to time.Now() (X-07-follow,
	// FLAKE-SEED-CLOCK). Same wall clock, same behaviour.
	now := mentorClockNow().UnixMilli()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	rows1m, err1m := bh.LastNBarsCurrentContract("MNQ", "1m", mentorSeedBars1mN)
	if err1m != nil {
		at.logWarnf("🧑‍🏫 mentor seed: 1m store read failed (%v) — seeding with nothing (refusing)", err1m)
		at.mentorCount("seed_store_read_error")
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
	stitched1m, full1HRTH, contracts, gaps, stoppedAt, seamCovered := at.mentorFullKeyLevelHistory(bh)
	// m445 round 2 (reviewer A, D1): measure the series the key levels are about
	// to be walked over for RTH holes — the gate reads it for every mentor entry.
	if len(full1HRTH) > 0 {
		at.noteSeedSeries(stitched1m, seamCovered, mentorClockNow())
	} else {
		at.noteSeedSeries(bars1m, nil, mentorClockNow())
	}
	var missing []string
	priorHTF := at.mentorEval.State.HTF // MENTOR-NY item 6: the state the seed replays from
	if len(full1HRTH) > 0 {
		// N6 (release #23): the seed replays the HTF lines with every replayed
		// day's print windows, as the running bot dropped them; the 08:30
		// day-gate replay (MENTOR-NY) reads the same windows.
		at.mentorWithHistoryPrintWindows(stitched1m, func() {
			missing = mentor.SeedFull(at.mentorEval, stitched1m, full1HRTH, now)
			mentor.SeedOpenHTF(at.mentorEval, stitched1m, priorHTF, now)
			mentor.SeedDayLatch(at.mentorEval, stitched1m, now)   // B1: restore the 08:30 day latch
			mentor.SeedTrendlines(at.mentorEval, stitched1m, now) // B3: mark already-crossed trendline pairs
		})
		at.logInfof("%s", mentor.KeyLevelHistoryLine(contracts, full1HRTH, gaps, stoppedAt))
	} else {
		at.mentorWithHistoryPrintWindows(bars1m, func() {
			missing = mentor.Seed(at.mentorEval, bars1m, now)
			mentor.SeedOpenHTF(at.mentorEval, bars1m, priorHTF, now)
			mentor.SeedDayLatch(at.mentorEval, bars1m, now)   // B1
			mentor.SeedTrendlines(at.mentorEval, bars1m, now) // B3
		})
	}
	// m422 (C5): paint the chart's overlays from the last close so a restart on
	// a closed market is not blank until the next bar. Display only; reads the
	// CURRENT-CONTRACT tick window (the ~1500 closed 1m bars Tick reads), never
	// the stitched back-adjusted series above.
	at.mentorSeedChartDisplay(bars1m)
	// The evaluator's own depth (computed from the stitched series) is the
	// single source of truth for the per-source refusal seam.
	depths := at.mentorEval.Depths()
	at.mentorSeedDepthsMu.Lock()
	at.mentorSeedDepths = depths
	at.mentorSeedDepthsMu.Unlock()
	at.mentorSourceDepthSource = func(name string) (int, bool) {
		at.mentorSeedDepthsMu.RLock()
		defer at.mentorSeedDepthsMu.RUnlock()
		d, ok := at.mentorSeedDepths[name]
		return d, ok
	}
	at.logInfof("🧑‍🏫 %s", at.mentorEval.SeedLine(len(contracts)))
	at.logInfof("🧑‍🏫 %s", at.mentorEval.SeedBoxLine()) // N2 (release #23): the replayed box latch
	at.logInfof("%s", at.mentorSeamBootLine())
	at.logInfof("%s", at.mentorStaleDataBlockBootLine())
	at.logInfof("%s", at.mentorCourseExitsBootLine())
	at.logInfof("%s", mentorCourseEntryBootLine(at, at.mentorRiskControl())) // release #25: READ per trader
	// trail_tf 30s/45s: refused LOUDLY at config load (no sub-minute bars).
	at.mentorTrailTFLoadCheck()
	if len(missing) > 0 {
		at.mentorCount("seed_missing")
		at.logErrorf("%s", mentorSeedRefusalLine(missing))
	}
	// #13: rebuild the G1 leg budget + G2 loss-box registry from today's REAL
	// fills and closed stop-outs (Limits is in-memory only — a restart wiped
	// "two losses at one place = OFF" and the stopped-leg block). Runs AFTER
	// the seed so State.Levels is set for the row-fallback old-extreme
	// resolution. Best-effort: a read error leaves the fresh (empty) Limits.
	at.mentorRebuildLimitsAtBoot()
}

// mentorRebuildLimitsAtBoot (#13) replays today's session-day FILLED mentor arms
// (non-swing) into Limits.RecordFill via the row fallback (the in-memory pends
// died with the process), then today's CLOSED stop-outs into Limits.RealExit
// with wasStop — so a restart re-derives the legs, the per-place loss counts and
// the stopped-leg block. The swing never enters Limits (its own machine).
func (at *AutoTrader) mentorRebuildLimitsAtBoot() {
	if at == nil || at.store == nil || at.mentorEval == nil {
		return
	}
	now := mentorClockNow()
	since := kernel.CMESessionDayStart(now).UnixMilli()
	levels := at.mentorEval.State.Levels

	fills, err := at.store.ArmedOrders().ListFilled(at.id, 500)
	if err != nil {
		at.mentorCount("limits_rebuild_fill_read_error")
		at.logWarnf("🧑‍🏫 limits rebuild: filled-arm read failed (%v) — G1/G2 not rebuilt", err)
		return
	}
	rebuiltFills := 0
	for i := range fills {
		r := &fills[i]
		if !isMentorArmOrigin(*r) || strings.EqualFold(strings.TrimSpace(r.Condition), "SWING4H") {
			continue
		}
		if r.SignalID == "" || r.EntryPx == 0 || r.FillPrice == 0 {
			continue
		}
		if r.UpdatedAt.UnixMilli() < since {
			continue // a prior session-day's fill must not re-spend today's budget
		}
		row := &mentor.FillRow{
			Side:     mentor.Side(strings.ToLower(strings.TrimSpace(r.Side))),
			Entry:    r.EntryPx,
			Stop:     r.StopPx,
			Target:   r.TargetPx,
			ISB:      strings.EqualFold(strings.TrimSpace(r.Condition), "ISB"),
			SignalID: r.SignalID,
		}
		at.mentorEval.State.Limits.RecordFill(r.SignalID, "", r.FillPrice, r.FillPrice, row, levels)
		rebuiltFills++
	}

	closed, err := at.store.Position().GetClosedPositions(at.id, 500)
	if err != nil {
		at.mentorCount("limits_rebuild_close_read_error")
		at.logWarnf("🧑‍🏫 limits rebuild: closed-position read failed (%v) — stop-outs not replayed", err)
		return
	}
	rebuiltStops := 0
	for _, pos := range closed {
		if pos == nil || pos.EntryOrderID == "" || pos.EntryTime < since {
			continue
		}
		if pos.CloseReason != store.CloseReasonStop {
			continue // only the original stop is a G1/G2 loss
		}
		// #47 P1 / m308 P2(a): a break-even or trailed stop keeps CloseReasonStop
		// but is NOT a loss — a loss = the fill on the losing side of ENTRY by
		// more than one tick.
		tick := market.FuturesTickSize(pos.Symbol)
		if !mentorStopFillIsLoss(pos.Side, pos.ExitPrice, pos.EntryPrice, tick) {
			// m308 P2(b): DROP the open trade (not a loss) — a bare continue left
			// the fill as a phantom open trade and a later retrace through the
			// original stop booked a phantom stop-out.
			at.mentorEval.State.Limits.RealExit(pos.EntryOrderID, false)
			continue
		}
		at.mentorEval.State.Limits.RealExit(pos.EntryOrderID, true)
		rebuiltStops++
	}
	if rebuiltFills > 0 || rebuiltStops > 0 {
		at.mentorCount("limits_rebuilt")
		at.logInfof("🧑‍🏫 limits rebuild: %d fills + %d stop-outs replayed into G1/G2 (%s)", rebuiltFills, rebuiltStops, now.Format("15:04:05 CT"))
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
func (at *AutoTrader) mentorFullKeyLevelHistory(bh *store.BarHistoryStore) (stitched1m, full1HRTH []market.Kline, contracts []mentor.Contract1M, gaps []mentor.RollGap, stoppedAt string, seamCovered []int64) {
	if at == nil || bh == nil {
		return nil, nil, nil, nil, "", nil
	}
	spans, err := bh.ContractSpans("MNQ", "1m")
	if err != nil || len(spans) == 0 {
		if err != nil {
			at.logWarnf("🧑‍🏫 key-level history: contract span read failed (%v) — falling back to current contract", err)
		}
		return nil, nil, nil, nil, "", nil
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
		return nil, nil, nil, nil, "", nil
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
	seamCovered = s.SeamCovered() // minutes a contract stores that the cut dropped: not lost (round 4, D1)
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
	for _, n := range at.mentorSeamMissing() {
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
		depth, known := at.mentorSourceDepth(req.Name)
		if !known {
			at.mentorCount("history_depth_short")
			missing = append(missing, fmt.Sprintf("history: %s (n/a)", req.Name))
			continue
		}
		if depth < min {
			at.mentorCount("history_depth_short")
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
		depth, known := at.mentorSourceDepth(req.Name)
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

// mentorSeedChartDisplay (m422, C5) runs mentor.SeedChartDisplay over the tick
// window: the live provider's closed bars when it has them, else the tail of
// the store's current-contract 1m read (the same contract Tick reads).
func (at *AutoTrader) mentorSeedChartDisplay(storeBars1m []market.Kline) {
	var win []market.Kline
	if market.FuturesBarsProvider != nil {
		win = mentorClosedBars(market.FuturesBarsProvider("MNQ", "1m", mentorBars1mDepth))
	}
	if len(win) == 0 {
		win = storeBars1m
		if n := len(win); n > mentorBars1mDepth {
			win = win[n-mentorBars1mDepth:]
		}
	}
	if len(win) < 2 {
		return
	}
	at.mentorEval.SeedChartDisplay(win, mentor.BarCloseInstant(win[len(win)-1]))
}
