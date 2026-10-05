package trader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// ── MENTOR INJECTOR — THE ACTION EXECUTORS (P0, CTO 1791040360324) ────────────
//
// The evaluator's action set used to die in a 3-case switch: every
// PlaceStopLimitEntry (the ISB order type — the largest setup group),
// ExtendArm, ActionMoveStopBE and ActionClosePosition was silently dropped,
// and CancelArm never reached the broker. This file is the ONE mentor entry
// path and the executor for every remaining action. A mentor entry is ALWAYS
// stop-limit (PLAN: never stop-market) whatever MENTOR_STOP_LIMIT says; an
// AddOn below the c2 floor REFUSES, never a stop-market fallback.

// mentorLiveArm is the injector's own ArmID registry entry. ArmID is the
// evaluator's id ("isb-<seq>" …); the registry maps it to the ledger row the
// placement created, so CancelArm / ExtendArm / MoveStopBE / ClosePosition can
// find the real order. In-memory only: an ArmID from a previous process does
// not resolve — every handler refuses that NAMED, never silent.
type mentorLiveArm struct {
	RowID int64
	Side  string  // "long" | "short"
	Entry float64 // the arm's entry price (MoveStopBE target)
}

var (
	mentorLiveMu   sync.Mutex
	mentorLiveArms = map[string]mentorLiveArm{}
)

// N1 (DS-104): the evaluator's ArmSeq restarts at 0 after a reload/restart, so
// a bare ArmID ("isb-1") collides with a TERMINAL ledger row from the prior
// evaluator/process and UpsertArm silently no-ops it (row id 0 → never placed).
// Each trader construction bumps this epoch (mentorSeedAtStart), so the ledger
// scenario is unique per process AND per reload.
var mentorArmEpoch atomic.Int64

func bumpMentorArmEpoch() {
	mentorArmEpoch.Store(time.Now().UnixNano())
}

func mentorRegisterLiveArm(armID string, rowID int64, side string, entry float64) {
	mentorLiveMu.Lock()
	mentorLiveArms[armID] = mentorLiveArm{RowID: rowID, Side: side, Entry: entry}
	mentorLiveMu.Unlock()
}

func mentorLiveArmFor(armID string) (mentorLiveArm, bool) {
	mentorLiveMu.Lock()
	defer mentorLiveMu.Unlock()
	a, ok := mentorLiveArms[armID]
	return a, ok
}

// mentorDispatchEntry is the ONE mentor entry path (X-07, refactored out of
// the dispatch switch). A single deferred guard drops the kernel's pending
// G1/G2 sim fill for the intent's ArmID on EVERY refusal inside this
// function: a trader-side refusal (R8 25-pt ceiling, window, done-after-win,
// news, no-chase, N4, MENTOR_PLACE off) must not phantom-fill on a later
// candle and spend the leg budget / open a loss box. An intent that reached
// mentorRegisterLiveArm (mentorLiveArmFor) is accepted — no drop.
func (at *AutoTrader) mentorDispatchEntry(in mentor.Intent, extra mentorTierInputs, lastCloseTime, emitMs int64) {
	defer func() {
		if in.ArmID == "" {
			return
		}
		if _, ok := mentorLiveArmFor(in.ArmID); ok {
			return
		}
		at.mentorDropEvalArm(in.ArmID)
	}()
	// ONE MENTOR ENTRY PATH: the order type differs only inside the
	// evaluator; the injector always rests a stop-limit. The confluence
	// flag feeds the size tier (10/20) and the exit fork (C).
	extra.Confluence = mentorConfluenceFlag(in)
	tuned := mentorTuningResolve(at.mentorRiskControl())
	extra.SwingMaxStopPts = tuned.SwingMaxStopPts
	extra.SpentDayStopCapPts = tuned.DayGateTargetCapPts
	if why := mentorRuleGate(in, extra); why != "" {
		rule := "other"
		if i := strings.Index(why, ":"); i > 0 {
			rule = strings.ToLower(strings.TrimSpace(why[:i]))
		}
		mentorCount("refused_" + rule)
		at.logWarnf("🧑‍🏫 mentor intent REFUSED — %s", why)
		return
	}
	choice, err := at.mentorSizeFor(in, extra)
	if err != nil {
		return
	}
	mentorCount("intent_" + in.Setup)
	if !mentorPlaceEnv() {
		at.logInfof("🧑‍🏫 mentor intent SIZED, NOT PLACED (MENTOR_PLACE env off — set MENTOR_PLACE=1 to place): %s %s %d contracts @ %.2f (stop %.2f, target %.2f, tier %s)",
			in.Setup, in.Side, choice.Contracts, in.Price, in.Stop, in.Target, choice.Tier)
		mentorCount("placement_held")
		return
	}
	at.mentorPlaceIntent(in, choice, lastCloseTime, emitMs)
}

// mentorDropEvalArm drops the kernel G1/G2 sim pend for a refused entry
// (X-07). Nil-safe: a direct dispatch in a test may run before the evaluator
// exists (the guard no-ops there — nothing was simulated).
func (at *AutoTrader) mentorDropEvalArm(armID string) {
	if at == nil || at.mentorEval == nil {
		return
	}
	at.mentorEval.State.Limits.DropArm(armID)
}

// mentorDispatchIntent is the injector's action switch as ONE call (the eval
// loop delegates here so every action is testable at the call site). Never
// silent: every action either executes, or refuses with a named counter.
func (at *AutoTrader) mentorDispatchIntent(in mentor.Intent, extra mentorTierInputs, lastCloseTime, emitMs int64) {
	switch in.Action {
	case mentor.PlaceStopEntry, mentor.PlaceStopLimitEntry:
		at.mentorDispatchEntry(in, extra, lastCloseTime, emitMs)
	case mentor.ExtendArm:
		at.mentorExtendArm(in)
	case mentor.CancelArm:
		at.mentorCancelArm(in)
	case mentor.LevelInvalid:
		at.mentorRecordLevelInvalid(in)
	case mentor.ActionMoveStopBE:
		at.mentorMoveStopBE(in)
	case mentor.ActionClosePosition:
		at.mentorClosePosition(in)
	default:
		// P0: an action the injector does not know is REFUSED and named —
		// a silent drop is exactly the failure this wave exists to close.
		mentorCount("unknown_action_" + strings.ToLower(string(in.Action)))
		at.logWarnf("🧑‍🏫 mentor UNKNOWN action %q REFUSED — never silent: %s", in.Action, in.Reason)
	}
}

// mentorAuthoredRow reports whether the ledger row carries the mentor origin.
// The injector is its own authoring pass: its rows are admitted by armAdmitted
// without the planner's admission set — every other placement gate still runs.
func mentorAuthoredRow(r store.ArmedOrderDB) bool {
	return isMentorArmOrigin(r)
}

// mentorArmIntent is the ONE mentor entry path (P0-b, CTO 1791040400571): it
// creates an ARMED LEDGER row — kind stop_entry, the intent's expiry — and
// lets the armed executor place it. No direct wire call here: the armed
// pass runs the admission chain, the slot guard, the c2 floor (refused,
// never a stop-market fallback), the stop-limit origin routing and, at
// expiry, the F1 cancel — all on the one path. The ArmID registry records
// the row for CancelArm / ExtendArm / MoveStopBE / ClosePosition.
func (at *AutoTrader) mentorArmIntent(in mentor.Intent, choice mentorSizeChoice, barCloseMs, emitMs int64, forkMode string, forkTP float64) {
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		mentorCount("placement_refused_no_ledger")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — no armed ledger")
		return
	}
	armID := strings.TrimSpace(in.ArmID)
	if armID == "" {
		armID = fmt.Sprintf("mentor-%d", time.Now().UnixNano())
	}
	// N1 (DS-104): the ledger scenario is the evaluator's ArmID prefixed with
	// the per-construction epoch. The in-memory registry stays keyed by the
	// UNPREFIXED armID, so ExtendArm / CancelArm / MoveStopBE / ClosePosition
	// still resolve by the evaluator's id.
	scenario := fmt.Sprintf("%s-%s", armID, strconv.FormatInt(mentorArmEpoch.Load(), 10))
	if in.RunnerTarget > 0 {
		mentorRunnerTargets.Store(scenario, in.RunnerTarget)
	}
	side := strings.ToLower(strings.TrimSpace(string(in.Side)))
	if side != "long" && side != "short" {
		mentorCount("placement_refused_bad_side")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — unknown side %q", in.Side)
		return
	}
	row := store.ArmedOrderDB{
		TraderID:  at.id,
		PlanID:    "mentor",
		Version:   1,
		Session:   "MENTOR",
		Scenario:  scenario,
		Side:      side,
		State:     store.StateArmed,
		EntryPx:   in.Price,
		StopPx:    in.Stop,
		TargetPx:  in.Target,
		Kind:      "stop_entry",
		Condition: in.Setup,
		ExpiryMs:  in.ExpiryMs,
		// P0 bind wiring step 1 (CTO 02:56Z): the ONLY author of the mentor
		// origin — the stop-limit origin routing (stop_limit.go isMentorArmOrigin)
		// and DS-106's admission gate both key off it. Every other author leaves
		// it empty.
		Origin: store.ArmOriginMentor,
		// B2 (2026-10-04): the signed contract count rides the row so the armed
		// pass sends it (never 1). The sizing table already clamped it to
		// mentor_max_contracts.
		Contracts: store.IntPtr(choice.Contracts),
	}
	// REVIEW-353: the split AT ENTRY rides the ONE frame — leg1_qty + leg1_tp
	// go on the wire; the AddOn places TWO OCO pairs on the one fill. (0, 0)
	// = the single-bracket legacy path (n <= 1 or swing).
	leg1Qty, leg1TP := mentorLeg1ForFrame(in, choice.Contracts, forkMode, forkTP)
	if leg1Qty > 0 {
		row.Leg1Qty = store.IntPtr(leg1Qty)
		row.Leg1TP = leg1TP
	}
	if err := ledger.UpsertArm(&row); err != nil {
		mentorCount("placement_refused_upsert")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm upsert failed: %v", err)
		return
	}
	// N1 (DS-104): a terminal row with the same scenario makes UpsertArm a
	// no-op that leaves row.ID at 0. Refuse + log — never register a phantom.
	if row.ID == 0 {
		mentorCount("placement_refused_arm_id_zero")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm %q authored no row (id 0)", armID)
		return
	}
	mentorRegisterLiveArm(armID, row.ID, side, in.Price)
	mentorCount("armed_" + choice.Tier)
	at.mentorFunnel.bumpAuthored() // N12 funnel stage: the arm row was authored
	ackMs := time.Now().UnixMilli()
	recordMentorLatency(barCloseMs, at.mentorFinalArrival.Load(), emitMs, ackMs)
	at.logInfof("🧑‍🏫 mentor arm authored: %s %s %d contracts (tier %s) — row %d, the armed pass places it (stop-limit by the origin rule), expiry %d",
		in.Setup, side, choice.Contracts, choice.Tier, row.ID, in.ExpiryMs)
}

// mentorArmQuantity resolves the contract count the armed pass sends for a
// mentor-authored arm (B2, 2026-10-04). A mentor row WITHOUT a count is a
// refusal (why != "") — never sent as 1 (absent ≠ 0). A non-positive count is
// also refused. Otherwise the count is clamped to the trader's current max
// (resolveMaxContracts): mentor_max_contracts in mentor mode, the Stage-A
// 1-contract cap in AI mode — so a stale mentor row authored under a higher
// cap is never oversized on a reload.
func (at *AutoTrader) mentorArmQuantity(r store.ArmedOrderDB) (float64, string) {
	if r.Contracts == nil {
		return 0, "the mentor arm carries no contract count (absent ≠ 0) — never sent as 1"
	}
	n := *r.Contracts
	if n < 1 {
		return 0, fmt.Sprintf("the mentor arm's contract count %d is not a positive count — never sent as 1", n)
	}
	if mx := at.resolveMaxContracts(); mx > 0 && n > mx {
		return float64(mx), ""
	}
	return float64(n), ""
}

// mentorWireLeg1 is the leg-1 size the ONE entry frame carries when `sent`
// contracts go out for row r (0 = the single bracket). Leg 1 = ceil(sent/2),
// except that the runner never exceeds the row's own runner (contracts −
// leg1_qty, which already carries the spent-day cap D) — the clamp to the
// trader max shrinks both legs, never grows the runner. No runner → no split.
func mentorWireLeg1(r store.ArmedOrderDB, sent int) int {
	if r.Leg1Qty == nil || *r.Leg1Qty <= 0 || sent <= 1 {
		return 0
	}
	rowN := sent
	if r.Contracts != nil && *r.Contracts > 0 {
		rowN = *r.Contracts
	}
	runner := sent - (sent+1)/2
	if rowRunner := rowN - *r.Leg1Qty; runner > rowRunner {
		runner = rowRunner
	}
	if runner <= 0 {
		return 0
	}
	return sent - runner
}

// mentorWireLeg1TP re-bases leg 1's TP on the wire trigger, keeping its
// R-multiple: k = (leg1TP − entry) / (entry − stop) at the authored entry,
// then trigger + k·(trigger − stop). 1R stays 1R and mode C's 2R stays 2R
// measured from where the order actually fills. Degenerate input (no risk,
// no TP, a TP on the loss side) returns the authored TP unchanged — the wire
// check (wireLeg1TP) still refuses a loss-side TP.
func mentorWireLeg1TP(entry, trigger, stop, leg1TP float64) float64 {
	risk := entry - stop
	if leg1TP == 0 || risk == 0 || trigger == 0 {
		return leg1TP
	}
	k := (leg1TP - entry) / risk
	if k <= 0 {
		return leg1TP
	}
	return trigger + k*(trigger-stop)
}

// mentorWireOneRFloor re-bases a target authored at EXACTLY 1:1 (within half
// a tick — the 1R floor, derived from the entry) to the 1:1 point at the wire
// trigger. Any other target (a level, or > 1R) is returned unchanged and the
// N4 check judges it as a fixed price.
func mentorWireOneRFloor(entry, trigger, stop, target, tick float64) float64 {
	risk := math.Abs(entry - stop)
	if risk == 0 || target == 0 || trigger == 0 || tick <= 0 {
		return target
	}
	if math.Abs(math.Abs(target-entry)-risk) > tick/2 {
		return target // not on the floor — a level target never moves
	}
	return mentorWireLeg1TP(entry, trigger, stop, target)
}

// mentorExtendArm pushes a resting arm's expiry forward (ISB stacking, N12).
// The row may be place_pending or working (resting at NT8) — SetArmExpiry
// accepts both, working only while unfilled.
func (at *AutoTrader) mentorExtendArm(in mentor.Intent) {
	if in.ExpiryMs <= 0 {
		mentorCount("extend_refused_no_expiry")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED — no new expiry: %s", in.Reason)
		return
	}
	arm, ok := mentorLiveArmFor(in.ArmID)
	if !ok {
		mentorCount("extend_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		mentorCount("extend_refused_no_ledger")
		return
	}
	if err := ledger.SetArmExpiry(arm.RowID, in.ExpiryMs); err != nil {
		mentorCount("extend_refused")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED for ArmID %q (row %d): %v", in.ArmID, arm.RowID, err)
		return
	}
	mentorCount("extend_ok")
	at.logInfof("🧑‍🏫 mentor arm %q expiry extended to %d — %s", in.ArmID, in.ExpiryMs, in.Reason)
}

// mentorCancelArm is the REAL broker cancel — the F1 shape: safety gate,
// wire cancel on the SAME pass, then the ledger request the settlement path
// owns. An unknown ArmID or a terminal row refuses NAMED, never silent.
func (at *AutoTrader) mentorCancelArm(in mentor.Intent) {
	arm, ok := mentorLiveArmFor(in.ArmID)
	if !ok {
		mentorCount("cancel_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor CancelArm REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	ledger := at.store.ArmedOrders()
	nt := at.armedTrader()
	if ledger == nil || nt == nil {
		mentorCount("cancel_refused_unavailable")
		at.logWarnf("🧑‍🏫 mentor CancelArm REFUSED — ledger/broker unavailable: %s", in.Reason)
		return
	}
	var r store.ArmedOrderDB
	if err := ledger.DB().First(&r, arm.RowID).Error; err != nil || store.IsTerminalArmState(r.State) {
		mentorCount("cancel_refused_row_gone")
		at.logInfof("🧑‍🏫 mentor CancelArm for ArmID %q — the row is already terminal; nothing to cancel (%s)", in.ArmID, in.Reason)
		return
	}
	now := time.Now()
	if mentorNowSource != nil {
		now = mentorNowSource()
	}
	// N7 part 4 (CTO, release #4): a row that was NEVER SENT ends cancelled
	// at once — a cancel_pending row with no signal id can never settle and
	// would WARN every pass forever. CAS: if a placement stamped a signal id
	// in the meantime, fall through and cancel the placed order normally.
	if strings.TrimSpace(r.SignalID) == "" {
		done, err := ledger.CancelUnplaced(r.ID, "mentor: "+in.Reason+" — never placed")
		if err == nil && done {
			at.clearMentorLevelArmLocked(in.ArmID)
			mentorCount("cancel_unplaced")
			at.logInfof("🧑‍🏫 mentor cancel for ArmID %q: never placed — row cancelled directly (%s)", in.ArmID, in.Reason)
			return
		}
		if err != nil {
			at.logWarnf("🧑‍🏫 mentor unplaced cancel write failed for ArmID %q: %v", in.ArmID, err)
		}
		if rerr := ledger.DB().First(&r, arm.RowID).Error; rerr != nil || store.IsTerminalArmState(r.State) {
			at.clearMentorLevelArmLocked(in.ArmID)
			return
		}
	}
	if strings.TrimSpace(r.SignalID) != "" {
		if v := at.cancelSafetyFor(r, now); !v.Allow {
			at.logWarnf("🛟 mentor cancel REFUSED: %s %s — %s", r.Session, r.Scenario, v.Why)
		} else if cerr := nt.CancelOrder(r.SignalID); cerr != nil {
			at.logWarnf("✕ mentor cancel SEND failed: %s %s: %v", r.Session, r.Scenario, cerr)
		}
	}
	// D2-44 (item 11): the injector cancels a level arm — clear the evaluator's
	// LevelArms entry so it stops believing the order rests. The caller
	// (mentorEvalOnce) holds mentorEvalMu, so this is the lock-free half.
	at.clearMentorLevelArmLocked(in.ArmID)
	at.armLifecycleWrite("request_cancel(mentor)", r,
		ledger.RequestCancel(r.ID, "mentor: "+in.Reason, now.UnixMilli()))
	mentorCount("cancel_requested")
	at.logInfof("🧑‍🏫 mentor cancel requested for ArmID %q: %s", in.ArmID, in.Reason)
}

// mentorRecordLevelInvalid records the evaluator's level-invalidation intent
// (only ISBs may trade there after — the evaluator owns that rule; the
// injector records it, never silently).
// mentorInvalidLogWindow is how long one LevelKey's invalid log is held before
// the next line (item 18 noise gate, DS-105 replay: level_invalid ~240/day is
// course-correct — each new visit is a new first touch — but the per-event INFO
// line flooded the journal).
const mentorInvalidLogWindow = 15 * time.Minute

// mentorInvalidLogState is the per-LevelKey rate-limit state.
type mentorInvalidLogState struct {
	lastLogMs  int64
	suppressed int
}

// mentorInvalidLogDecision is the pure rate-limit: logNow when the window has
// elapsed since the last emit (or this is the first — lastLogMs 0), and
// `suppressed` is how many events were held back in the just-ended window.
func mentorInvalidLogDecision(st mentorInvalidLogState, nowMs int64) (logNow bool, suppressed int, next mentorInvalidLogState) {
	if st.lastLogMs == 0 || nowMs-st.lastLogMs >= mentorInvalidLogWindow.Milliseconds() {
		return true, st.suppressed, mentorInvalidLogState{lastLogMs: nowMs}
	}
	return false, 0, mentorInvalidLogState{lastLogMs: st.lastLogMs, suppressed: st.suppressed + 1}
}

func (at *AutoTrader) mentorRecordLevelInvalid(in mentor.Intent) {
	mentorCount("intent_" + string(in.Action)) // the counter fires on EVERY event
	now := mentorClockNow().UnixMilli()
	at.mentorInvalidLogMu.Lock()
	if at.mentorInvalidLog == nil {
		at.mentorInvalidLog = map[string]mentorInvalidLogState{}
	}
	st := at.mentorInvalidLog[in.LevelKey]
	logNow, suppressed, next := mentorInvalidLogDecision(st, now)
	at.mentorInvalidLog[in.LevelKey] = next
	at.mentorInvalidLogMu.Unlock()
	if !logNow {
		return
	}
	if suppressed > 0 {
		at.logInfof("🧑‍🏫 mentor level invalidated: %s — %s (%d more suppressed in the last 15m)", in.LevelKey, in.Reason, suppressed)
		return
	}
	at.logInfof("🧑‍🏫 mentor level invalidated: %s — %s", in.LevelKey, in.Reason)
}

// mentorSwingFill resolves the swing arm's ledger row and returns the row, the
// contracts the swing leg's own close must send, and whether acting is safe.
// S1: an arm that never filled (still resting, cancelled, expired or refused)
// must never drive a stop move or a close — otherwise a phantom swing would
// close a DIFFERENT mentor position on the same side. A FILLED arm whose
// contracts cannot be attributed is REFUSED (never guessed as 1).
func (at *AutoTrader) mentorSwingFill(arm mentorLiveArm) (r store.ArmedOrderDB, qty float64, ok bool) {
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		mentorCount("swing_fill_no_ledger")
		at.logErrorf("🧑‍🏫 mentor swing fill check REFUSED — no armed ledger")
		return store.ArmedOrderDB{}, 0, false
	}
	if err := ledger.DB().First(&r, arm.RowID).Error; err != nil {
		mentorCount("swing_fill_row_gone")
		at.logWarnf("🧑‍🏫 mentor swing fill check REFUSED — ArmID row %d gone: %v", arm.RowID, err)
		return store.ArmedOrderDB{}, 0, false
	}
	if r.State != store.StateFilled {
		return r, 0, false
	}
	if r.FillQuantity > 0 {
		return r, float64(r.FillQuantity), true
	}
	if r.Contracts != nil && *r.Contracts > 0 {
		return r, float64(*r.Contracts), true
	}
	mentorCount("swing_fill_unknown_qty")
	at.logWarnf("🧑‍🏫 mentor swing fill REFUSED — ArmID row %d filled but no contracts (FillQuantity=0, Contracts absent): cannot size its own close; never guessed", arm.RowID)
	return r, 0, false
}

// mentorMoveStopBE executes the swing's stop-to-break-even intent. S1: it acts
// only on the swing leg's OWN FILLED position, and the never-widen guard reads
// the swing leg's OWN recorded stop (the arm row's StopPx) — never the ledger's
// open-orders list. Unknown stop → refuse + log (fail closed), never skip.
func (at *AutoTrader) mentorMoveStopBE(in mentor.Intent) {
	arm, ok := mentorLiveArmFor(in.ArmID)
	if !ok {
		mentorCount("move_be_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		mentorCount("move_be_refused_unavailable")
		return
	}
	// S1: a phantom swing (never filled, or an unattributable fill) must not
	// move another trade's stop.
	r, _, ok := at.mentorSwingFill(arm)
	if !ok {
		mentorCount("move_be_refused_not_filled")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q never filled; the swing leg has no position of its own: %s", in.ArmID, in.Reason)
		return
	}
	// The widen guard reads the swing leg's OWN stop (SwingState.Pos.Stop's
	// ledger mirror = the arm row's StopPx). Fail closed when it is unknown.
	curStop := r.StopPx
	if curStop <= 0 {
		mentorCount("move_be_refused_no_stop")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q filled but its own stop is unknown (StopPx=0); never skip the guard: %s", in.ArmID, in.Reason)
		return
	}
	if refuse, why := mentorNeverWiden(arm.Side, curStop, arm.Entry); refuse {
		mentorCount("widen_refused")
		at.logWarnf("🧑‍🏫 %s", why)
		return
	}
	if err := moveStopWire(nt, arm.Side, arm.Entry); err != nil {
		mentorCount("move_be_refused")
		at.logWarnf("🧑‍🏫 mentor stop→BE refused: %v", err)
		return
	}
	mentorCount("move_be_sent")
	at.logInfof("🧑‍🏫 mentor stop moved to break-even (entry %.2f) for ArmID %q: %s", arm.Entry, in.ArmID, in.Reason)
}

// mentorClosePosition executes the swing's hold-close intent: a mentor-owned
// market close of the swing leg's OWN filled position — never a side-wide
// close (S1: CloseLong/CloseShort(sym, 0) would flatten a different mentor
// position open on the same side).
func (at *AutoTrader) mentorClosePosition(in mentor.Intent) {
	arm, ok := mentorLiveArmFor(in.ArmID)
	if !ok {
		mentorCount("close_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor ClosePosition REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		mentorCount("close_refused_unavailable")
		return
	}
	// S1: the close must be attributed to the swing leg's own fill, sized by
	// its own contracts — never a side-wide 0, never guessed.
	_, qty, ok := at.mentorSwingFill(arm)
	if !ok {
		mentorCount("close_refused_not_filled")
		at.logWarnf("🧑‍🏫 mentor ClosePosition REFUSED — ArmID %q never filled (or its fill has no attributable contracts); a phantom swing must not close another trade: %s", in.ArmID, in.Reason)
		return
	}
	var err error
	switch arm.Side {
	case "long":
		_, err = nt.CloseLong(at.futuresSymbol(), qty)
	case "short":
		_, err = nt.CloseShort(at.futuresSymbol(), qty)
	default:
		mentorCount("close_refused_bad_side")
		at.logWarnf("🧑‍🏫 mentor ClosePosition REFUSED — unknown side %q for ArmID %q", arm.Side, in.ArmID)
		return
	}
	if err != nil {
		mentorCount("close_refused")
		at.logWarnf("🧑‍🏫 mentor close failed for ArmID %q: %v", in.ArmID, err)
		return
	}
	mentorCount("close_sent")
	at.logInfof("🧑‍🏫 mentor position closed (%s, %.0f contracts) for ArmID %q: %s", arm.Side, qty, in.ArmID, in.Reason)
}
