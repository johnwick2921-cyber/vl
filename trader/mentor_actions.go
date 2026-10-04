package trader

import (
	"fmt"
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
	// RowIDs are the armed-ledger rows this ArmID authored — ONE for a swing /
	// n=1 single leg, TWO for a split intraday entry (leg 1 + leg 2, DS-103).
	RowIDs []int64
	Side   string  // "long" | "short"
	Entry  float64 // the arm's entry price (MoveStopBE target)
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
	mentorRegisterLiveArmGroup(armID, []int64{rowID}, side, entry)
}

// mentorRegisterLiveArmGroup registers every ledger row of one ArmID — ONE for a
// swing / n=1 single leg, TWO for a split intraday entry (DS-103).
func mentorRegisterLiveArmGroup(armID string, rowIDs []int64, side string, entry float64) {
	mentorLiveMu.Lock()
	mentorLiveArms[armID] = mentorLiveArm{RowIDs: rowIDs, Side: side, Entry: entry}
	mentorLiveMu.Unlock()
}

func mentorLiveArmFor(armID string) (mentorLiveArm, bool) {
	mentorLiveMu.Lock()
	defer mentorLiveMu.Unlock()
	a, ok := mentorLiveArms[armID]
	return a, ok
}

// mentorDispatchIntent is the injector's action switch as ONE call (the eval
// loop delegates here so every action is testable at the call site). Never
// silent: every action either executes, or refuses with a named counter.
func (at *AutoTrader) mentorDispatchIntent(in mentor.Intent, extra mentorTierInputs, lastCloseTime, emitMs int64) {
	switch in.Action {
	case mentor.PlaceStopEntry, mentor.PlaceStopLimitEntry:
		// ONE MENTOR ENTRY PATH: the order type differs only inside the
		// evaluator; the injector always rests a stop-limit. The confluence
		// flag feeds the size tier (10/20) and the exit fork (C).
		extra.Confluence = mentorConfluenceFlag(in)
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
// creates the ARMED LEDGER rows — kind stop_entry, the intent's expiry — and
// lets the armed executor place them. No direct wire call here: the armed
// pass runs the admission chain, the slot guard, the c2 floor (refused,
// never a stop-market fallback), the stop-limit origin routing and, at
// expiry, the F1 cancel — all on the one path. The ArmID registry records
// the rows for CancelArm / ExtendArm / MoveStopBE / ClosePosition.
//
// SPLIT AT PLACEMENT (DS-103, EXIT-SPEC-v3): one intent of n contracts becomes
// leg 1 (ceil(n/2), its own TP) + leg 2 (the runner, TP = the trade target) —
// TWO rows sharing one EntryGroup, each with its own Contracts. n = 1 and the
// swing are a single row. forkMode / forkTP are the exit fork from
// mentorPlaceIntent (leg-1 TP: the +1R default, or ≥2R for mode C).
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
	// the per-construction epoch. Both leg rows share it as their EntryGroup,
	// so the in-memory registry stays keyed by the UNPREFIXED armID while the
	// ledger group is unique per process and per reload.
	groupID := fmt.Sprintf("%s-%s", armID, strconv.FormatInt(mentorArmEpoch.Load(), 10))
	side := strings.ToLower(strings.TrimSpace(string(in.Side)))
	if side != "long" && side != "short" {
		mentorCount("placement_refused_bad_side")
		at.logWarnf("🧑‍🏫 mentor placement REFUSED — unknown side %q", in.Side)
		return
	}
	rows := mentorArmRows(in, choice, forkMode, forkTP, groupID)
	rowIDs := make([]int64, 0, len(rows))
	for i := range rows {
		rows[i].TraderID = at.id
		if err := ledger.UpsertArm(&rows[i]); err != nil {
			mentorCount("placement_refused_upsert")
			at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm upsert failed (leg %d): %v", i, err)
			return
		}
		// N1 (DS-104): a terminal row with the same scenario makes UpsertArm a
		// no-op that leaves the row ID at 0. Refuse + log — never register a phantom.
		if rows[i].ID == 0 {
			mentorCount("placement_refused_arm_id_zero")
			at.logErrorf("🧑‍🏫 mentor placement REFUSED — arm %q leg %d authored no row (id 0)", armID, i)
			return
		}
		rowIDs = append(rowIDs, rows[i].ID)
	}
	mentorRegisterLiveArmGroup(armID, rowIDs, side, in.Price)
	mentorCount("armed_" + choice.Tier)
	at.mentorFunnel.bumpAuthored() // N12 funnel stage: the arm row was authored
	ackMs := time.Now().UnixMilli()
	recordMentorLatency(barCloseMs, at.mentorFinalArrival.Load(), emitMs, ackMs)
	at.logInfof("🧑‍🏫 mentor arm authored: %s %s %d contracts (tier %s) — %d leg row(s) (group %s), the armed pass places them (stop-limit by the origin rule), expiry %d",
		in.Setup, side, choice.Contracts, choice.Tier, len(rowIDs), groupID, in.ExpiryMs)
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
	// DS-103 split legs: a split entry has TWO rows — the expiry extension
	// covers BOTH legs (they are one entry, same expiry).
	for _, id := range arm.RowIDs {
		if err := ledger.SetArmExpiry(id, in.ExpiryMs); err != nil {
			mentorCount("extend_refused")
			at.logWarnf("🧑‍🏫 mentor ExtendArm REFUSED for ArmID %q (row %d): %v", in.ArmID, id, err)
			return
		}
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
	now := time.Now()
	if mentorNowSource != nil {
		now = mentorNowSource()
	}
	// DS-103 split legs: a split entry has TWO rows — the cancel covers BOTH
	// legs (they are one entry).
	for _, id := range arm.RowIDs {
		var r store.ArmedOrderDB
		if err := ledger.DB().First(&r, id).Error; err != nil || store.IsTerminalArmState(r.State) {
			mentorCount("cancel_refused_row_gone")
			at.logInfof("🧑‍🏫 mentor CancelArm for ArmID %q — the row is already terminal; nothing to cancel (%s)", in.ArmID, in.Reason)
			continue
		}
		if strings.TrimSpace(r.SignalID) != "" {
			if v := at.cancelSafetyFor(r, now); !v.Allow {
				at.logWarnf("🛟 mentor cancel REFUSED: %s %s — %s", r.Session, r.Scenario, v.Why)
			} else if cerr := nt.CancelOrder(r.SignalID); cerr != nil {
				at.logWarnf("✕ mentor cancel SEND failed: %s %s: %v", r.Session, r.Scenario, cerr)
			}
		}
		at.armLifecycleWrite("request_cancel(mentor)", r,
			ledger.RequestCancel(r.ID, "mentor: "+in.Reason, now.UnixMilli()))
	}
	mentorCount("cancel_requested")
	at.logInfof("🧑‍🏫 mentor cancel requested for ArmID %q: %s", in.ArmID, in.Reason)
}

// mentorRecordLevelInvalid records the evaluator's level-invalidation intent
// (only ISBs may trade there after — the evaluator owns that rule; the
// injector records it, never silently).
func (at *AutoTrader) mentorRecordLevelInvalid(in mentor.Intent) {
	mentorCount("intent_" + string(in.Action))
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
	// The swing is a SINGLE leg — exactly one row. A phantom registry entry
	// with no rows refuses (never index an empty slice).
	if len(arm.RowIDs) != 1 {
		mentorCount("swing_fill_row_gone")
		at.logWarnf("🧑‍🏫 mentor swing fill check REFUSED — ArmID has %d row(s), want exactly 1", len(arm.RowIDs))
		return store.ArmedOrderDB{}, 0, false
	}
	if err := ledger.DB().First(&r, arm.RowIDs[0]).Error; err != nil {
		mentorCount("swing_fill_row_gone")
		at.logWarnf("🧑‍🏫 mentor swing fill check REFUSED — ArmID row %d gone: %v", arm.RowIDs[0], err)
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
	at.logWarnf("🧑‍🏫 mentor swing fill REFUSED — ArmID row %d filled but no contracts (FillQuantity=0, Contracts absent): cannot size its own close; never guessed", arm.RowIDs[0])
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
