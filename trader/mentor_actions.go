package trader

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
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
//
// REL18-A: the registry is keyed by traderID+"|"+armID, and every read verifies
// TraderID — two mentor traders in one bot mint the same ArmIDs ("isb-1"), and
// a bare-keyed map let B's cancel/sweep act on A's resting entry.
type mentorLiveArm struct {
	TraderID string // the trader that authored the arm (REL18-A: the sweep acts on its own trader's arms only)
	ArmID    string // the evaluator's BARE ArmID (the map key is trader|armID; the bare id rides the entry)
	RowID    int64
	Side     string  // "long" | "short"
	Entry    float64 // the arm's entry price (MoveStopBE target)
	Scenario string  // the LEDGER scenario this arm authored (REL18-A #8: stored at placement, never recomputed)
}

var (
	mentorLiveMu   sync.Mutex
	mentorLiveArms = map[string]mentorLiveArm{} // key mentorArmKey(traderID, armID)
)

// mentorArmKey scopes a bare ArmID to its trader (REL18-A #1).
func mentorArmKey(traderID, armID string) string {
	return traderID + "|" + armID
}

// mentorPlanID is the ledger plan_id for a mentor trader (REL18-A #2): the
// trader rides the unique key so two traders authoring the same scenario can
// never overwrite each other's ARMED row. Readers accept BOTH this form and the
// legacy "mentor" (pre-change rows).
func mentorPlanID(traderID string) string {
	return "mentor:" + traderID
}

// mentorPlanIDIsMentor reports whether a plan_id is a mentor plan — the legacy
// "mentor" or the new "mentor:<trader>" (REL18-A #2: readers accept both).
func mentorPlanIDIsMentor(planID string) bool {
	return planID == "mentor" || strings.HasPrefix(planID, "mentor:")
}

// mentorRunnerTargetKey scopes a runner target to its trader (REL18-A #7).
func mentorRunnerTargetKey(traderID, scenario string) string {
	return traderID + "|" + scenario
}

// mentorScenarioFor builds the ledger scenario (the per-arm signal identity):
// the resolved ArmID prefixed with the CALLING TRADER'S construction epoch.
// The exit branch is registered under THIS key, so the placement-time write and
// the fill-time read name the same arm — and a second trader's identical ArmID
// never collides (its epoch differs, and the ledger key carries its trader too).
func (at *AutoTrader) mentorScenarioFor(armID string) string {
	return fmt.Sprintf("%s-%d", armID, at.mentorArmEpoch)
}

func (at *AutoTrader) mentorRegisterLiveArm(armID string, rowID int64, side string, entry float64, scenario string) {
	mentorLiveMu.Lock()
	mentorLiveArms[mentorArmKey(at.id, armID)] = mentorLiveArm{TraderID: at.id, ArmID: armID, RowID: rowID, Side: side, Entry: entry, Scenario: scenario}
	mentorLiveMu.Unlock()
}

// mentorUnregisterLiveArm prunes one arm from the registry (I5) — called when
// its row goes terminal, so the sweeps stop re-iterating a dead arm and stop
// counting a cancel that never happened. REL18-A: only the CALLER'S entry is
// pruned; another trader's same-named arm is untouched.
func (at *AutoTrader) mentorUnregisterLiveArm(armID string) {
	mentorLiveMu.Lock()
	delete(mentorLiveArms, mentorArmKey(at.id, armID))
	mentorLiveMu.Unlock()
}

// mentorLiveArmFor resolves the CALLER's own registry entry. A missing entry,
// or one whose TraderID is not the caller's (a mis-keyed entry) both read as
// "not found" — REL18-A: a lookup that would resolve to another trader's entry
// is REFUSED with a WARN naming both traders, never acted on.
func (at *AutoTrader) mentorLiveArmFor(armID string) (mentorLiveArm, bool) {
	mentorLiveMu.Lock()
	defer mentorLiveMu.Unlock()
	a, ok := mentorLiveArms[mentorArmKey(at.id, armID)]
	if !ok {
		return mentorLiveArm{}, false
	}
	if a.TraderID != at.id {
		at.logWarnf("🧑‍🏫 mentor arm %q resolves to ANOTHER trader's entry (registry %s, caller %s) — REFUSED, never acting", armID, a.TraderID, at.id)
		return mentorLiveArm{}, false
	}
	return a, true
}

// mentorRegisterLiveArmForTest registers a live arm under an EXPLICIT trader
// (test fixtures, including cross-trader fixtures). The scenario defaults to
// the bare armID — production always uses the trader-scoped method above.
func mentorRegisterLiveArmForTest(armID, traderID string, rowID int64, side string, entry float64) {
	mentorLiveMu.Lock()
	mentorLiveArms[mentorArmKey(traderID, armID)] = mentorLiveArm{TraderID: traderID, ArmID: armID, RowID: rowID, Side: side, Entry: entry, Scenario: armID}
	mentorLiveMu.Unlock()
}

// mentorFillReceipt is a REAL fill receipt queued from the fill callback
// (the executor goroutine) to the evaluator drain (FU-1). receiptID dedupes
// (the entry signal id); armID is the evaluator's bare ArmID; lo/hi seed the
// B22 wave (the fill candle's low/high); row is the armed-row fallback for a
// fill with no pend (a restart dropped the pends — P1-2).
type mentorFillReceipt struct {
	receiptID string
	armID     string
	lo, hi    float64
	row       *mentor.FillRow
}

// mentorArmIDFromScenario strips the per-construction epoch suffix the ledger
// scenario carries ("<armID>-<epoch>") back to the evaluator's bare ArmID. A
// scenario with no trailing digit suffix (a foreign author) is returned
// unchanged.
func mentorArmIDFromScenario(scenario string) string {
	s := strings.TrimSpace(scenario)
	if i := strings.LastIndex(s, "-"); i > 0 {
		suffix := s[i+1:]
		if isDecimalDigits(suffix) {
			return s[:i]
		}
	}
	return s
}

func isDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// mentorEnqueueFill queues a REAL fill receipt from the armed fill callback
// (FU-1). SWING exempt (the swing never enters Limits — it runs its own
// machine). Non-blocking: a full queue drops the receipt (counted) rather
// than stalling the executor goroutine.
func (at *AutoTrader) mentorEnqueueFill(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) {
	if at == nil || at.mentorFillCh == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.Condition), "SWING4H") {
		return // swing exempt
	}
	armID := mentorArmIDFromScenario(r.Scenario)
	if armID == "" {
		return
	}
	lo, hi := u.FillPrice, u.FillPrice
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 1); len(bars) > 0 {
			lo, hi = bars[len(bars)-1].Low, bars[len(bars)-1].High
		}
	}
	// P1-2: carry the armed-row fallback so a fill with no pend (a restart
	// dropped the in-memory pends) registers from the row, never dropped.
	row := &mentor.FillRow{
		Side:     mentor.Side(strings.ToLower(strings.TrimSpace(r.Side))),
		Entry:    r.EntryPx,
		Stop:     r.StopPx,
		Target:   r.TargetPx,
		ISB:      strings.EqualFold(strings.TrimSpace(r.Condition), "ISB"),
		SignalID: u.SignalID, // #47: match a later broker position_close
	}
	select {
	case at.mentorFillCh <- mentorFillReceipt{receiptID: u.SignalID, armID: armID, lo: lo, hi: hi, row: row}:
	default:
		at.mentorCount("record_fill_queue_full")
	}
}

// mentorDrainFills drains the real-fill queue into the evaluator's Limits
// (FU-1). It MUST be called with mentorEvalMu held — mentorEvalOnce is the
// production caller (the drain is the lock-safe seam the CTO named). Nil-safe.
func (at *AutoTrader) mentorDrainFills() {
	if at == nil || at.mentorFillCh == nil || at.mentorEval == nil {
		return
	}
	apply := func(f mentorFillReceipt) mentor.FillOutcome {
		return at.mentorEval.State.Limits.RecordFill(f.receiptID, f.armID, f.lo, f.hi, f.row, at.mentorEval.State.Levels)
	}
	// R1: deferred receipts (row fallback waiting for the first Tick's levels)
	// are retried first; a still-empty level set re-defers them.
	if len(at.mentorDeferredFills) > 0 {
		kept := at.mentorDeferredFills[:0]
		for _, f := range at.mentorDeferredFills {
			if apply(f) == mentor.FillDefer {
				kept = append(kept, f)
			}
		}
		at.mentorDeferredFills = kept
	}
	for {
		select {
		case f := <-at.mentorFillCh:
			if apply(f) == mentor.FillDefer {
				// R1: the row fallback needs State.Levels, still empty — park
				// the receipt for the next drain (after the first Tick sets
				// the levels) and keep draining the pend-backed receipts.
				at.mentorDeferredFills = append(at.mentorDeferredFills, f)
			}
		default:
			return
		}
	}
}

// mentorExitReceipt is a broker exit receipt queued from the close-sync
// goroutine to the evaluator drain (#47). signalID is the entry signal id;
// wasStop is true when the broker closed at the ORIGINAL stop ("sl") — a
// break-even, trailed stop, partial, news or EOD flat exits with wasStop=false.
type mentorExitReceipt struct {
	signalID string
	wasStop  bool
}

// mentorEnqueueExit queues a broker position_close receipt for the mentor
// limits (#47). Non-blocking: a full queue drops it (counted).
func (at *AutoTrader) mentorEnqueueExit(signalID string, wasStop bool) {
	if at == nil || at.mentorExitCh == nil || signalID == "" {
		return
	}
	select {
	case at.mentorExitCh <- mentorExitReceipt{signalID: signalID, wasStop: wasStop}:
	default:
		at.mentorCount("record_exit_queue_full")
	}
}

// mentorDrainExits drains the broker-exit queue into the evaluator's Limits
// (#47). It MUST be called with mentorEvalMu held (mentorEvalOnce is the
// production caller). Nil-safe.
func (at *AutoTrader) mentorDrainExits() {
	if at == nil || at.mentorExitCh == nil || at.mentorEval == nil {
		return
	}
	for {
		select {
		case x := <-at.mentorExitCh:
			at.mentorEval.State.Limits.RealExit(x.signalID, x.wasStop)
		default:
			return
		}
	}
}

// mentorDispatchEntry is the ONE mentor entry path (X-07, refactored out of
// the dispatch switch). A single deferred guard drops the kernel's pending
// G1/G2 sim fill for the intent's ArmID on EVERY refusal inside this
// function: a trader-side refusal (R8 mentor_isb_max_pts ceiling, window, done-after-win,
// news, no-chase, N4, MENTOR_PLACE off) must not phantom-fill on a later
// candle and spend the leg budget / open a loss box. An intent that reached
// mentorRegisterLiveArm (mentorLiveArmFor) is accepted — no drop.
func (at *AutoTrader) mentorDispatchEntry(in mentor.Intent, extra mentorTierInputs, lastCloseTime, emitMs int64) {
	defer func() {
		if in.ArmID == "" {
			return
		}
		if _, ok := at.mentorLiveArmFor(in.ArmID); ok {
			return
		}
		at.mentorDropEvalArm(in.ArmID)
	}()
	// ONE MENTOR ENTRY PATH: the order type differs only inside the
	// evaluator; the injector always rests a stop-limit. The confluence
	// flag feeds the size tier (10/20) and the exit fork (C).
	extra.Confluence = mentorConfluenceFlag(in)
	tuned := mentorTuningResolve(at, at.mentorRiskControl())
	extra.SwingMaxStopPts = tuned.SwingMaxStopPts
	extra.SpentDayStopCapPts = tuned.DayGateTargetCapPts
	// R8 reads the SAME knob as the kernel (mentor_isb_max_pts, unset/0 → 30) —
	// one number, not two (P2-1).
	extra.ISBMaxStopPts = mentorCourseExitsResolve(at, at.mentorRiskControl()).ISBMaxPts
	if why := mentorRuleGate(in, extra); why != "" {
		rule := "other"
		if i := strings.Index(why, ":"); i > 0 {
			rule = strings.ToLower(strings.TrimSpace(why[:i]))
		}
		at.mentorCount("refused_" + rule)
		at.logWarnf("🧑‍🏫 mentor intent REFUSED — %s", why)
		// STALLED-SETUP ALARM (release #22): the LAST refusal reason rides the
		// per-kind tally so a stalled setup's alert names why it died.
		at.mentorRecordStallRefusal(in, why)
		return
	}
	choice, err := at.mentorSizeFor(in, extra)
	if err != nil {
		at.mentorRecordStallRefusal(in, "size: "+err.Error())
		return
	}
	at.mentorCount("intent_" + in.Setup)
	if !mentorPlaceEnv() {
		at.logInfof("🧑‍🏫 mentor intent SIZED, NOT PLACED (MENTOR_PLACE env off — set MENTOR_PLACE=1 to place): %s %s %d contracts @ %.2f (stop %.2f, target %.2f, tier %s)",
			in.Setup, in.Side, choice.Contracts, in.Price, in.Stop, in.Target, choice.Tier)
		at.mentorCount("placement_held")
		at.mentorRecordStallRefusal(in, "MENTOR_PLACE off (dry run)")
		return
	}
	at.mentorPlaceIntent(in, choice, lastCloseTime, emitMs)
}

// mentorDropEvalArm drops EVERY evaluator trace of a refused entry (X-07 +
// FIX-MENTOR-PHANTOM-ARM): the kernel G1/G2 sim pend AND the evaluator's own
// ISB arm / swing Pending for that id. It is the ONE guard the deferred drop
// in mentorDispatchEntry runs on every refusal, so a refused placement can
// never leave a phantom that suppresses the next same-side setup or emits a
// follow-up ExtendArm/CancelArm/MoveStopBE for an order that was never
// authored. Nil-safe: a direct dispatch in a test may run before the
// evaluator exists (the guard no-ops there — nothing was simulated).
func (at *AutoTrader) mentorDropEvalArm(armID string) {
	if at == nil || at.mentorEval == nil {
		return
	}
	at.mentorEval.State.Limits.DropArm(armID)
	at.mentorEval.DropArm(armID)
}

// mentorDispatchIntent is the injector's action switch as ONE call (the eval
// loop delegates here so every action is testable at the call site). Never
// silent: every action either executes, or refuses with a named counter.
func (at *AutoTrader) mentorDispatchIntent(in mentor.Intent, extra mentorTierInputs, lastCloseTime, emitMs int64) {
	switch in.Action {
	case mentor.PlaceStopEntry, mentor.PlaceStopLimitEntry:
		// STALLED-SETUP ALARM (release #22): the emit half of the seen-vs-placed
		// pair — every placement intent the evaluator emitted is counted HERE,
		// keyed by setup kind, whatever the rule gate / placement does next.
		at.mentorCountSetupEmitted(in)
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
		at.mentorCount("unknown_action_" + strings.ToLower(string(in.Action)))
		at.logWarnf("🧑‍🏫 mentor UNKNOWN action %q REFUSED — never silent: %s", in.Action, in.Reason)
	}
}

// mentorAuthoredRow reports whether the ledger row carries the mentor origin.
// The injector is its own authoring pass: its rows are admitted by armAdmitted
// without the planner's admission set — every other placement gate still runs.
func mentorAuthoredRow(r store.ArmedOrderDB) bool {
	return isMentorArmOrigin(r)
}

// mentorResolveArmID trims the evaluator's ArmID and supplies the fallback
// identity for a missing one. mentorPlaceIntent calls it ONCE before the exit
// branch is registered, so mentorArmIntent authors the row under the SAME
// identity the mode was stored under (B5/L8).
func mentorResolveArmID(armID string) string {
	armID = strings.TrimSpace(armID)
	if armID == "" {
		armID = fmt.Sprintf("mentor-%d", time.Now().UnixNano())
	}
	return armID
}

// mentorArmIntent is the ONE mentor entry path (P0-b, CTO 1791040400571): it
// creates an ARMED LEDGER row — kind stop_entry, the intent's expiry — and
// lets the armed executor place it. No direct wire call here: the armed
// pass runs the admission chain, the slot guard, the c2 floor (refused,
// never a stop-market fallback), the stop-limit origin routing and, at
// expiry, the F1 cancel — all on the one path. The ArmID registry records
// the row for CancelArm / ExtendArm / MoveStopBE / ClosePosition.
// mentorExitModePtr (REL17-F2) returns a heap pointer to the exit branch for
// persistence on the armed row. A blank mode returns nil (absent ≠ fabricated):
// the restart rebuild derives the mode from the setup for those rows.
func mentorExitModePtr(mode string) *string {
	if mode == "" {
		return nil
	}
	m := mode
	return &m
}

func (at *AutoTrader) mentorArmIntent(in mentor.Intent, choice mentorSizeChoice, barCloseMs, emitMs int64, forkMode string, forkTP float64) {
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		at.mentorCount("placement_refused_no_ledger")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — no armed ledger")
		at.mentorRecordStallRefusal(in, "no armed ledger")
		return
	}
	armID := mentorResolveArmID(in.ArmID)
	// REL18-A: the ledger scenario is the evaluator's ArmID prefixed with THIS
	// trader's construction epoch. The in-memory registry stays keyed by
	// traderID+"|"+armID, so ExtendArm / CancelArm / MoveStopBE / ClosePosition
	// resolve the CALLER'S OWN row and never another trader's.
	scenario := at.mentorScenarioFor(armID)
	if in.RunnerTarget > 0 {
		// REL18-A #7: the runner target is keyed trader+scenario — two traders
		// mint the same scenario, and a bare-scenario key would hand B's fill
		// A's runner target.
		mentorRunnerTargets.Store(mentorRunnerTargetKey(at.id, scenario), in.RunnerTarget)
	}
	if in.ExecTFMin > 1 {
		// X8 @18:02–18:14 "trade khung nào thì gồng theo khung đó": the frame
		// the entry was READ on rides to the fill, so the runner trails on it.
		mentorExecFrames.Store(mentorRunnerTargetKey(at.id, scenario), in.ExecTFMin)
	} else if in.ExecTFMin == 1 {
		// An explicit 1m stamp tells the X8 trap-dodge "1m". No kernel site
		// stamps 1 today (ISB / reverse-ISB / level-PHL / rolling-PHL all gate
		// their stamp on `execTFMin > 1`), so this branch is reserved; it keeps
		// the m409 seam alive without widening it.
		mentorExecFrames.Store(mentorRunnerTargetKey(at.id, scenario), 1)
	} else {
		// ExecTFMin == 0 = the frame is UNKNOWN (a box/ORB/trendline entry, or a
		// 1m read whose authoring site did not stamp). m417: Delete so the X8
		// trap-dodge falls back to the 2m (the pre-m409 behaviour) instead of
		// reading the 1m on a 2m-drawn box at 11:00 CT.
		mentorExecFrames.Delete(mentorRunnerTargetKey(at.id, scenario))
	}
	side := strings.ToLower(strings.TrimSpace(string(in.Side)))
	if side != "long" && side != "short" {
		at.mentorCount("placement_refused_bad_side")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — unknown side %q", in.Side)
		at.mentorRecordStallRefusal(in, fmt.Sprintf("unknown side %q", in.Side))
		return
	}
	row := store.ArmedOrderDB{
		TraderID: at.id,
		// REL18-A #2: the trader is IN the unique key — two traders authoring
		// the same scenario can no longer overwrite each other's ARMED row.
		PlanID:    mentorPlanID(at.id),
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
		// REL17-F2: persist the exit branch (A/B/C/swing) chosen at placement so
		// a restart can rebuild the open position with the SAME mode. A blank
		// fork never persists (absent ≠ fabricated); the rebuild derives it.
		ExitMode: mentorExitModePtr(forkMode),
		// B2 (2026-10-04): the signed contract count rides the row so the armed
		// pass sends it (never 1). The sizing table already clamped it to
		// mentor_max_contracts.
		Contracts: store.IntPtr(choice.Contracts),
	}
	// REVIEW-353: the split AT ENTRY rides the ONE frame — leg1_qty + leg1_tp
	// go on the wire; the AddOn places TWO OCO pairs on the one fill. (0, 0)
	// = the single-bracket legacy path (n <= 1 or swing).
	leg1Qty, leg1TP := mentorLeg1ForFrame(in, choice.Contracts, forkMode, forkTP)
	if q, tp, runnerTP, ok := at.mentorSwingRunnerLegs(in, choice.Contracts, forkMode); ok {
		// CX-2 (knob mentor_swing_runner, default OFF): leg 1 at the swing's
		// first target, the runner held by the 4h with its own TP.
		leg1Qty, leg1TP = q, tp
		row.TargetPx = runnerTP
	}
	if leg1Qty > 0 {
		row.Leg1Qty = store.IntPtr(leg1Qty)
		row.Leg1TP = leg1TP
	}
	// release #31 (leg-3 bracket): the runner of ≥ 2 contracts sells half
	// (rounded down) at the first line beyond the leg-1 exit, stamped by the
	// evaluator (Intent.RunnerPartialTP); the rest trails as today. Mode B only
	// (leg 1 exits at 1:1); mode C's leg 1 holds to 2R and its runner is not
	// split further. The SEND is gated on the AddOn proving the leg-3 build; a
	// 0 line (or a runner < 2) leaves the 2-pair plan.
	leg2Qty, leg2TP := 0, 0.0
	if runner := choice.Contracts - leg1Qty; forkMode == "B" && runner >= 2 && in.RunnerPartialTP > 0 {
		leg2Qty = runner / 2
		leg2TP = in.RunnerPartialTP
	}
	if leg2Qty > 0 {
		row.Leg2Qty = store.IntPtr(leg2Qty)
		row.Leg2TP = leg2TP
	}
	// m134 DS-108 (a): persist the evaluator snapshot the arm is authored under
	// (EMA9/34, 1h/4h/5m triggers, HTF verdict, ORB, valid/forming trendlines,
	// swing, nearest levels). A nil/empty evaluator leaves it NULL (absent).
	if at.mentorEval != nil {
		if snap, err := at.mentorEval.State.ArmSnapshot(in.Price).JSON(); err == nil {
			s := snap
			row.MentorSnapshot = &s
		}
	}
	if err := ledger.UpsertArm(&row); err != nil {
		at.mentorCount("placement_refused_upsert")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm upsert failed: %v", err)
		at.mentorRecordStallRefusal(in, "arm upsert: "+err.Error())
		return
	}
	// N1 (DS-104): a terminal row with the same scenario makes UpsertArm a
	// no-op that leaves row.ID at 0. Refuse + log — never register a phantom.
	if row.ID == 0 {
		at.mentorCount("placement_refused_arm_id_zero")
		at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm %q authored no row (id 0)", armID)
		at.mentorRecordStallRefusal(in, "arm authored no row (id 0)")
		return
	}
	at.mentorRegisterLiveArm(armID, row.ID, side, in.Price, scenario)
	at.mentorCount("armed_" + choice.Tier)
	at.mentorFunnel.bumpAuthored() // N12 funnel stage: the arm row was authored
	// STALLED-SETUP ALARM (release #22): the placed half of the seen-vs-placed
	// pair — this intent reached the ledger (row ID nonzero). Counted HERE, at
	// the ledger-author site, never inferred from the emit counter.
	at.mentorCountSetupPlaced(in)
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
		at.mentorCount("extend_refused_no_expiry")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED — no new expiry: %s", in.Reason)
		return
	}
	arm, ok := at.mentorLiveArmFor(in.ArmID)
	if !ok {
		at.mentorCount("extend_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		at.mentorCount("extend_refused_no_ledger")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED for ArmID %q (row %d) — the armed-orders ledger is unavailable; the keep was not recorded: %s", in.ArmID, arm.RowID, in.Reason)
		return
	}
	if err := ledger.SetArmExpiry(arm.RowID, in.ExpiryMs); err != nil {
		// P1-1 (rel22): name WHY the keep could not land, from the row's OWN
		// state and reason (counters record, never infer). A filled arm has
		// nothing to keep; a row the expiry sweep already took is the race the
		// pass order now prevents — if it ever shows again it is counted apart.
		var r store.ArmedOrderDB
		if rerr := ledger.DB().First(&r, arm.RowID).Error; rerr == nil {
			switch {
			case strings.EqualFold(r.State, store.StateFilled):
				at.mentorCount("extend_skipped_filled")
				at.logInfof("🧑‍🏫 mentor ExtendArm for ArmID %q (row %d) — the arm already FILLED; nothing to keep (%s)", in.ArmID, arm.RowID, in.Reason)
				return
			case mentorRowTakenByExpiry(r):
				at.mentorCount("extend_lost_expiry_race")
				at.logWarnf("🧑‍🏫 mentor ExtendArm LOST THE EXPIRY RACE for ArmID %q (row %d): the expiry sweep had already moved it to %s (%q) before the evaluator's keep landed — the course keeps this order [D4.2 p2 @ 08:21–09:01; P1-1]", in.ArmID, arm.RowID, r.State, r.StateReason)
				return
			}
			at.mentorCount("extend_refused")
			at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED for ArmID %q (row %d, state %s, reason %q): %v", in.ArmID, arm.RowID, r.State, r.StateReason, err)
			return
		}
		at.mentorCount("extend_refused")
		at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED for ArmID %q (row %d, state n/a — row unreadable): %v", in.ArmID, arm.RowID, err)
		return
	}
	at.mentorCount("extend_ok")
	at.logInfof("🧑‍🏫 mentor arm %q expiry extended to %d — %s", in.ArmID, in.ExpiryMs, in.Reason)
}

// mentorPrunableTerminalState reports whether a row's state means the registry
// entry should be pruned: terminal EXCEPT filled. A FILLED arm is a live
// position — its registry entry must survive so the swing's MoveStopBE /
// ClosePosition resolve (P1). Cancelled / canceled / rejected / expired /
// superseded / shadowed prune.
// mentorRowTakenByExpiry reports whether the expiry sweep (not the evaluator)
// is what took a row: it is cancel_pending / cancelled / expired AND its own
// reason names the expiry ("stop-limit expiry elapsed", "… — never placed",
// "stop-limit expiry: remainder cancelled …"). Read from the row, never
// inferred from timing.
func mentorRowTakenByExpiry(r store.ArmedOrderDB) bool {
	s := strings.ToLower(strings.TrimSpace(r.State))
	if s != store.StateCancelPending && s != "cancelled" && s != "canceled" && s != "expired" {
		return false
	}
	return strings.Contains(strings.ToLower(r.StateReason), "expiry")
}

func mentorPrunableTerminalState(state string) bool {
	s := strings.ToLower(strings.TrimSpace(state))
	return s != store.StateFilled && store.IsTerminalArmState(s)
}

// mentorCancelArm is the REAL broker cancel — the F1 shape: safety gate,
// wire cancel on the SAME pass, then the ledger request the settlement path
// owns. An unknown ArmID or a terminal row refuses NAMED, never silent.
//
// It returns whether a cancel was ACTUALLY requested (unplaced → cancelled
// directly, or placed → wire cancel + RequestCancel). A refusal (unknown arm,
// unavailable ledger/broker, already-terminal row) returns false — the caller
// counts and logs only real cancels, never an inference (I5).
func (at *AutoTrader) mentorCancelArm(in mentor.Intent) bool {
	arm, ok := at.mentorLiveArmFor(in.ArmID)
	if !ok {
		at.mentorCount("cancel_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor CancelArm REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return false
	}
	ledger := at.store.ArmedOrders()
	nt := at.armedTrader()
	if ledger == nil || nt == nil {
		at.mentorCount("cancel_refused_unavailable")
		at.logWarnf("🧑‍🏫 mentor CancelArm REFUSED — ledger/broker unavailable: %s", in.Reason)
		return false
	}
	var r store.ArmedOrderDB
	if err := ledger.DB().First(&r, arm.RowID).Error; err != nil {
		// P3: a DB read error must NOT prune a resting arm — log and retry next
		// tick.
		at.mentorCount("cancel_refused_read_error")
		at.logWarnf("🧑‍🏫 mentor CancelArm for ArmID %q — row read failed: %v (retrying next tick)", in.ArmID, err)
		return false
	}
	// P1: a FILLED row is a live position — never prune it, never cancel it,
	// and count/log NOTHING (the never-add sweep reaches here for a just-filled
	// swing; its registry entry must survive for MoveStopBE / ClosePosition).
	if strings.EqualFold(r.State, store.StateFilled) {
		return false
	}
	// P5 (I5b): a cancel already in flight needs no new request and must not be
	// re-counted.
	if strings.EqualFold(r.State, store.StateCancelPending) {
		at.mentorCount("cancel_already_pending")
		at.logInfof("🧑‍🏫 mentor CancelArm for ArmID %q — already cancel_pending; no re-request (%s)", in.ArmID, in.Reason)
		return false
	}
	if mentorPrunableTerminalState(r.State) {
		at.mentorCount("cancel_refused_row_gone")
		at.logInfof("🧑‍🏫 mentor CancelArm for ArmID %q — the row is already terminal; nothing to cancel (%s)", in.ArmID, in.Reason)
		at.mentorUnregisterLiveArm(in.ArmID) // I5: prune the terminal entry
		at.deleteMentorExitMode(r.Scenario)  // P3: delete by the ROW's own scenario, not the package epoch
		return false
	}
	now := mentorClockNow()
	// N7 part 4 (CTO, release #4): a row that was NEVER SENT ends cancelled
	// at once — a cancel_pending row with no signal id can never settle and
	// would WARN every pass forever. CAS: if a placement stamped a signal id
	// in the meantime, fall through and cancel the placed order normally.
	if strings.TrimSpace(r.SignalID) == "" {
		done, err := ledger.CancelUnplaced(r.ID, "mentor: "+in.Reason+" — never placed")
		if err == nil && done {
			at.clearMentorLevelArmLocked(in.ArmID)
			at.mentorUnregisterLiveArm(in.ArmID) // I5: the row is now cancelled (terminal)
			at.deleteMentorExitMode(r.Scenario)  // P3: delete by the ROW's own scenario
			at.mentorCount("cancel_unplaced")
			at.logInfof("🧑‍🏫 mentor cancel for ArmID %q: never placed — row cancelled directly (%s)", in.ArmID, in.Reason)
			return true
		}
		if err != nil {
			at.logWarnf("🧑‍🏫 mentor unplaced cancel write failed for ArmID %q: %v", in.ArmID, err)
		}
		if rerr := ledger.DB().First(&r, arm.RowID).Error; rerr != nil {
			// P3: a re-read error must not prune — retry next tick.
			at.logWarnf("🧑‍🏫 mentor CancelArm for ArmID %q — row re-read failed: %v (retrying next tick)", in.ArmID, rerr)
			return false
		}
		if mentorPrunableTerminalState(r.State) {
			at.clearMentorLevelArmLocked(in.ArmID)
			at.mentorUnregisterLiveArm(in.ArmID) // I5: the row is terminal now
			at.deleteMentorExitMode(r.Scenario)  // P3: delete by the ROW's own scenario
			return false
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
	// I4 (BUILD-ALL): a PARTIALLY filled arm whose remainder is cancelled here
	// must still hand its FILLED quantity to the exit drive — the B1 expiry path
	// covers only the expiry sweep, not this B3 day-stop / never-add cancel.
	at.mentorRegisterPartialFillIfAbsent(r)
	at.mentorCount("cancel_requested")
	at.logInfof("🧑‍🏫 mentor cancel requested for ArmID %q: %s", in.ArmID, in.Reason)
	return true
}

// mentorWorkingSameSideRows lists this trader's WORKING mentor rows on one
// side (release #30: ONE working entry per side, across every kind — ISB,
// ISB-reverse, PHL/PLH, BOX, trendline, swing). The armed ledger is the source
// of truth: it survives a restart where the in-memory registry is only
// partially rebuilt. A cancel_pending row is NOT working (its cancel is already
// in flight); armed / place_pending / working all are — each can still fill.
func (at *AutoTrader) mentorWorkingSameSideRows(side string) []store.ArmedOrderDB {
	if at == nil || at.store == nil {
		return nil
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return nil
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		// Fail-open on a read error: the fill-time OCO is the backstop, and a
		// transient DB lock must not stall every entry.
		at.logWarnf("🧑‍🏫 mentor same-side scan read failed: %v (fail-open — the fill-time OCO is the backstop)", err)
		return nil
	}
	side = strings.ToLower(strings.TrimSpace(side))
	var out []store.ArmedOrderDB
	for _, r := range rows {
		if r.TraderID != at.id || !isMentorArmOrigin(r) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.State), store.StateCancelPending) {
			continue // already being cancelled — not a working entry
		}
		if side != "" && !strings.EqualFold(strings.TrimSpace(r.Side), side) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// mentorCancelRestingRow cancels ONE resting mentor row directly from its row
// (release #30, OCO). Unlike mentorCancelArm (registry-keyed), it takes the
// row, so it works across a restart where the in-memory registry was not yet
// rebuilt for a resting arm. Returns whether a cancel was ACTUALLY requested —
// a filled / already-pending / terminal row returns false (never re-cancelled).
func (at *AutoTrader) mentorCancelRestingRow(r store.ArmedOrderDB, reason string) bool {
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return false
	}
	armID := mentorArmIDFromScenario(r.Scenario)
	if strings.EqualFold(strings.TrimSpace(r.State), store.StateFilled) {
		return false // a filled row is a live position — never cancel it
	}
	if strings.EqualFold(strings.TrimSpace(r.State), store.StateCancelPending) {
		return false // a cancel is already in flight — no re-request
	}
	if mentorPrunableTerminalState(r.State) {
		at.mentorUnregisterLiveArm(armID)
		at.deleteMentorExitMode(r.Scenario)
		return false
	}
	now := mentorClockNow()
	// Never-placed (no signal id): cancel the authorization directly — the
	// placement pass will never fire for it.
	if strings.TrimSpace(r.SignalID) == "" {
		done, err := ledger.CancelUnplaced(r.ID, "mentor: "+reason+" — never placed")
		if err == nil && done {
			at.clearMentorLevelArmLocked(armID)
			at.mentorUnregisterLiveArm(armID)
			at.deleteMentorExitMode(r.Scenario)
			at.mentorCount("cancel_unplaced")
			at.logInfof("🧑‍🏫 mentor OCO cancel for ArmID %q: never placed — row cancelled directly (%s)", armID, reason)
			return true
		}
		if err != nil {
			at.logWarnf("🧑‍🏫 mentor OCO unplaced cancel write failed for ArmID %q: %v", armID, err)
		}
	}
	nt := at.armedTrader()
	if nt != nil && strings.TrimSpace(r.SignalID) != "" {
		if v := at.cancelSafetyFor(r, now); !v.Allow {
			at.logWarnf("🛟 mentor OCO cancel REFUSED: %s %s — %s", r.Session, r.Scenario, v.Why)
		} else if cerr := nt.CancelOrder(r.SignalID); cerr != nil {
			at.logWarnf("✕ mentor OCO cancel SEND failed: %s %s: %v", r.Session, r.Scenario, cerr)
		}
	}
	at.clearMentorLevelArmLocked(armID)
	at.armLifecycleWrite("request_cancel(mentor OCO)", r,
		ledger.RequestCancel(r.ID, "mentor: "+reason, now.UnixMilli()))
	at.mentorRegisterPartialFillIfAbsent(r)
	at.mentorCount("cancel_requested")
	at.logInfof("🧑‍🏫 mentor OCO cancel requested for ArmID %q: %s", armID, reason)
	return true
}

// mentorCancelOtherWorkingOnFill (release #30 fold, CTO ruling): when one
// mentor entry fills (a position opens), every OTHER working mentor entry is
// cancelled before it can fill — the SAME side (one working entry per side) AND
// the OPPOSITE side (no flip: a resting opposite-side stop can net the position
// and leave the open trade's stop/TP working against the wrong side). One
// working entry period while a position is open. The cancel rides the real
// broker path (wire cancel + ledger RequestCancel), confirmed by NT8's
// per-order report like every other cancel.
func (at *AutoTrader) mentorCancelOtherWorkingOnFill(filled store.ArmedOrderDB) {
	side := strings.ToLower(strings.TrimSpace(filled.Side))
	for _, other := range at.mentorWorkingSameSideRows("") {
		if other.ID == filled.ID {
			continue
		}
		if side != "" && strings.EqualFold(strings.TrimSpace(other.Side), side) {
			at.mentorCancelRestingRow(other, "one working entry per side — the same-side entry filled, cancel the rest (OCO) [derived from D1.1 p1 @17:28–17:44 + CTO parity ruling 1791008332386 #1]")
		} else {
			at.mentorCancelRestingRow(other, "no flip: a position opened — cancel the opposite-side resting entry before it can net/flip [owner ruling 2026-09-03]")
		}
	}
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
	at.mentorCount("intent_" + string(in.Action)) // the counter fires on EVERY event
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
		at.mentorCount("swing_fill_no_ledger")
		at.logErrorf("🧑‍🏫 mentor swing fill check REFUSED — no armed ledger")
		return store.ArmedOrderDB{}, 0, false
	}
	if err := ledger.DB().First(&r, arm.RowID).Error; err != nil {
		at.mentorCount("swing_fill_row_gone")
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
	at.mentorCount("swing_fill_unknown_qty")
	at.logWarnf("🧑‍🏫 mentor swing fill REFUSED — ArmID row %d filled but no contracts (FillQuantity=0, Contracts absent): cannot size its own close; never guessed", arm.RowID)
	return r, 0, false
}

// mentorMoveStopBE executes the swing's stop-to-break-even intent. S1: it acts
// only on the swing leg's OWN FILLED position, and the never-widen guard reads
// the swing leg's OWN recorded stop (the arm row's StopPx) — never the ledger's
// open-orders list. Unknown stop → refuse + log (fail closed), never skip.
func (at *AutoTrader) mentorMoveStopBE(in mentor.Intent) {
	arm, ok := at.mentorLiveArmFor(in.ArmID)
	if !ok {
		at.mentorCount("move_be_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		at.mentorCount("move_be_refused_unavailable")
		return
	}
	// S1: a phantom swing (never filled, or an unattributable fill) must not
	// move another trade's stop.
	r, _, ok := at.mentorSwingFill(arm)
	if !ok {
		at.mentorCount("move_be_refused_not_filled")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q never filled; the swing leg has no position of its own: %s", in.ArmID, in.Reason)
		return
	}
	// The widen guard reads the swing leg's OWN stop (SwingState.Pos.Stop's
	// ledger mirror = the arm row's StopPx). Fail closed when it is unknown.
	curStop := r.StopPx
	if curStop <= 0 {
		at.mentorCount("move_be_refused_no_stop")
		at.logWarnf("🧑‍🏫 mentor MoveStopBE REFUSED — ArmID %q filled but its own stop is unknown (StopPx=0); never skip the guard: %s", in.ArmID, in.Reason)
		return
	}
	if refuse, why := mentorNeverWiden(arm.Side, curStop, arm.Entry); refuse {
		at.mentorCount("widen_refused")
		at.logWarnf("🧑‍🏫 %s", why)
		return
	}
	if err := moveStopWire(nt, arm.Side, arm.Entry); err != nil {
		at.mentorCount("move_be_refused")
		at.logWarnf("🧑‍🏫 mentor stop→BE refused: %v", err)
		return
	}
	at.mentorCount("move_be_sent")
	at.logInfof("🧑‍🏫 mentor stop moved to break-even (entry %.2f) for ArmID %q: %s", arm.Entry, in.ArmID, in.Reason)
}

// mentorClosePosition executes the swing's hold-close intent: a mentor-owned
// market close of the swing leg's OWN filled position — never a side-wide
// close (S1: CloseLong/CloseShort(sym, 0) would flatten a different mentor
// position open on the same side).
func (at *AutoTrader) mentorClosePosition(in mentor.Intent) {
	arm, ok := at.mentorLiveArmFor(in.ArmID)
	if !ok {
		at.mentorCount("close_refused_unknown_arm")
		at.logWarnf("🧑‍🏫 mentor ClosePosition REFUSED — ArmID %q not in this process's registry: %s", in.ArmID, in.Reason)
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		at.mentorCount("close_refused_unavailable")
		return
	}
	// S1: the close must be attributed to the swing leg's own fill, sized by
	// its own contracts — never a side-wide 0, never guessed.
	r, qty, ok := at.mentorSwingFill(arm)
	if ok {
		// CX-2: a split swing closes only what the broker still holds.
		if qty, ok = at.mentorSwingCloseQty(r, arm.Side, qty); !ok {
			return
		}
	} else {
		at.mentorCount("close_refused_not_filled")
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
		at.mentorCount("close_refused_bad_side")
		at.logWarnf("🧑‍🏫 mentor ClosePosition REFUSED — unknown side %q for ArmID %q", arm.Side, in.ArmID)
		return
	}
	if err != nil {
		at.mentorCount("close_refused")
		at.logWarnf("🧑‍🏫 mentor close failed for ArmID %q: %v", in.ArmID, err)
		return
	}
	at.mentorCount("close_sent")
	at.logInfof("🧑‍🏫 mentor position closed (%s, %.0f contracts) for ArmID %q: %s", arm.Side, qty, in.ArmID, in.Reason)
}
