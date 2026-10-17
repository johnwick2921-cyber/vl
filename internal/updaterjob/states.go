package updaterjob

import (
	"errors"
	"fmt"
)

// ErrForbiddenEdge wraps every move the state table does not allow.
var ErrForbiddenEdge = errors.New("updaterjob: forbidden transition")

// State is one row of the updater job's state machine (brief §3.3 as ruled:
// CTO 1790259689740 accepted C10 — the second "verified" is boot_verified,
// and rolling_back, cancelled and refused are added).
type State string

const (
	StateRequested       State = "requested"
	StateDownloaded      State = "downloaded"
	StateVerified        State = "verified"
	StatePreflightOK     State = "preflight_ok"
	StateMaintenanceHeld State = "maintenance_held"
	StateDrainedAcked    State = "drained_acked"
	StateGateOK          State = "gate_ok"
	StateBackupDone      State = "backup_done"
	StateNT8Skipped      State = "nt8_skipped"
	StateNT8Updated      State = "nt8_updated"
	// W-ADDON-ONE-CLICK states (DS-104): a changed AddOn builds → places →
	// restarts NT8 → waits for the reconnect, all before the point of no return.
	StateAddonBuilt       State = "addon_built"
	StateAddonPlaced      State = "addon_placed"
	StateNT8Restarted     State = "nt8_restarted"
	StateAddonReconnected State = "addon_reconnected"
	StateActivated        State = "activated"
	StateBooted           State = "booted"
	StateBootVerified     State = "boot_verified"
	StateWorkerSwapped    State = "worker_swapped"
	StateComplete         State = "complete"
	StateRollingBack      State = "rolling_back"
	StateRolledBack       State = "rolled_back"
	// SELF-RECOVERY (DS-102): a refusal/timeout BEFORE activation ends here.
	StateRevertedClean  State = "reverted_clean"
	StateRecoveryNeeded State = "recovery_needed"
	StateCancelled      State = "cancelled"
	StateRefused        State = "refused"
)

// Phase says whether a state's side effect is under way or finished. Every
// state with a side effect is persisted "started" BEFORE the effect runs and
// "done" (with its receipt) after it; a state with no side effect is entered
// "done".
type Phase string

const (
	PhaseStarted Phase = "started"
	PhaseDone    Phase = "done"
)

// Effect names the side effect a state performs. The worker (U4) owns the
// code behind each name; this package owns WHICH state performs WHICH effect,
// so a second hold write or an early hold clear is a table edit a test sees.
type Effect string

const (
	EffectNone        Effect = "none"         // the state is its own effect (the file) or terminal
	EffectDownload    Effect = "download"     // locate the verified release + re-hash every artifact
	EffectVerify      Effect = "verify"       // re-verify the signature + resolve the release dir
	EffectPreflight   Effect = "preflight"    // stage + host/app/flat/lock checks, before any hold
	EffectHold        Effect = "hold"         // the ONLY hold write (internal/updaterworker/hold.go)
	EffectDrain       Effect = "drain"        // READ the app's view until held+drained+acked+flat
	EffectGate        Effect = "gate"         // ready:true on two reads with distinct ack.received
	EffectBackup      Effect = "backup"       // DB backup + snapshot of the install halves
	EffectNT8         Effect = "nt8"          // record the AddOn decision (a read, C12)
	// W-ADDON-ONE-CLICK effects (DS-104).
	EffectAddonBuild     Effect = "addon_build"     // compile the release's ninjascript/*.cs into VL.AddOn.dll
	EffectAddonPlace     Effect = "addon_place"     // MOVE the old DLL to backups, copy the new one in, verify sha
	EffectNT8Restart     Effect = "nt8_restart"     // stop+start NT8 via powershell.exe (only when it was running)
	EffectAddonReconnect Effect = "addon_reconnect" // wait for a NEW accept_seq + hello build_id (bounded)
	EffectActivate       Effect = "activate"        // install the release halves + kill by identity
	EffectWatch          Effect = "watch"           // the new process proves itself (log + health)
	EffectBootVerify     Effect = "boot_verify"     // OK boot line, health prefix, AddOn ack build
	EffectWorkerSwap     Effect = "worker_swap"     // atomic self-update of the worker binary (worker-self-update)
	EffectRollback       Effect = "rollback"        // restore the snapshot + watch the OLD sha
	EffectReleaseHold    Effect = "release_hold"    // the ONLY hold clears
	EffectRevertClean    Effect = "revert_clean"    // pre-activation self-recovery: hold + lock + AddOn, nothing swapped
)

// Row is one state's contract.
type Row struct {
	State  State
	Effect Effect
	// Success lists the states a DONE step may move to (two only after
	// backup_done: the AddOn decision). Empty ⇔ the state is terminal.
	Success []State
	// Failure is where a STARTED step that failed goes. "" ⇔ Effect is
	// EffectNone (nothing runs, so nothing can fail).
	Failure State
	// ExtraFailure is an additional legal failure edge (phase started) beyond
	// Failure. F2: drained_acked/gate_ok may end REFUSED when the planner-wait
	// expires and the hold is released — a benign blocker must not park the
	// desk in recovery_needed.
	ExtraFailure []State
	// Cancellable: cancel-before-boundary may move this state to cancelled.
	// True exactly for the states before maintenance_held — nothing has been
	// held, drained or changed yet.
	Cancellable bool
	// Parks: once DONE the runner stops and waits for an attended resume.
	Parks bool
}

// table is the state machine. Order is the dispatch's order (§2), then the
// C10 additions.
var table = []Row{
	{State: StateRequested, Effect: EffectNone, Success: []State{StateDownloaded}, Cancellable: true},
	{State: StateDownloaded, Effect: EffectDownload, Success: []State{StateVerified}, Failure: StateRefused, Cancellable: true},
	{State: StateVerified, Effect: EffectVerify, Success: []State{StatePreflightOK}, Failure: StateRefused, Cancellable: true},
	{State: StatePreflightOK, Effect: EffectPreflight, Success: []State{StateMaintenanceHeld}, Failure: StateRefused, Cancellable: true},
	// The boundary. A failed hold write leaves no hold of ours, so it is still
	// "refused"; from here on a hold may exist, and no path reaches refused or
	// cancelled (both mean "nothing was held or changed") — EXCEPT the F2
	// planner-wait expiry: drained_acked/gate_ok release the hold and refuse,
	// via ExtraFailure (the step cleared the hold BEFORE the edge).
	{State: StateMaintenanceHeld, Effect: EffectHold, Success: []State{StateDrainedAcked}, Failure: StateRefused},
	// SELF-RECOVERY (DS-102): pre-activation failures go reverted_clean, not
	// recovery_needed — nothing was swapped yet, the old build keeps running.
	{State: StateDrainedAcked, Effect: EffectDrain, Success: []State{StateGateOK}, Failure: StateRevertedClean, ExtraFailure: []State{StateRevertedClean}},
	{State: StateGateOK, Effect: EffectGate, Success: []State{StateBackupDone}, Failure: StateRevertedClean, ExtraFailure: []State{StateRevertedClean}},
	{State: StateBackupDone, Effect: EffectBackup, Success: []State{StateNT8Skipped, StateNT8Updated}, Failure: StateRevertedClean},
	{State: StateNT8Skipped, Effect: EffectNT8, Success: []State{StateActivated}, Failure: StateRevertedClean},
	// W-ADDON-ONE-CLICK (DS-104): a changed AddOn no longer parks. It builds →
	// places → restarts NT8 → waits for the reconnect, all before the point of
	// no return. addon_* failures revert clean; the reconnect timeout (NT8 was
	// already restarted) rolls back.
	{State: StateNT8Updated, Effect: EffectNT8, Success: []State{StateAddonBuilt}, Failure: StateRevertedClean},
	{State: StateAddonBuilt, Effect: EffectAddonBuild, Success: []State{StateAddonPlaced}, Failure: StateRevertedClean},
	{State: StateAddonPlaced, Effect: EffectAddonPlace, Success: []State{StateNT8Restarted, StateAddonReconnected}, Failure: StateRevertedClean},
	{State: StateNT8Restarted, Effect: EffectNT8Restart, Success: []State{StateAddonReconnected}, Failure: StateRevertedClean},
	{State: StateAddonReconnected, Effect: EffectAddonReconnect, Success: []State{StateActivated}, Failure: StateRollingBack},
	// The point of no return: any failure from here restores the snapshot.
	{State: StateActivated, Effect: EffectActivate, Success: []State{StateBooted}, Failure: StateRollingBack},
	{State: StateBooted, Effect: EffectWatch, Success: []State{StateBootVerified}, Failure: StateRollingBack},
	{State: StateBootVerified, Effect: EffectBootVerify, Success: []State{StateWorkerSwapped}, Failure: StateRollingBack},
	// The new bot is PROVEN before the worker swaps itself (never before
	// boot_verified; never on refused/rolled_back/recovery_needed — the table
	// has no edge from those). A swap refusal is recorded in the step's
	// receipt and the job still completes (the install itself succeeded);
	// Failure: StateComplete is the same completion for an unexpected error.
	{State: StateWorkerSwapped, Effect: EffectWorkerSwap, Success: []State{StateComplete}, Failure: StateComplete},
	{State: StateComplete, Effect: EffectReleaseHold, Failure: StateRecoveryNeeded},
	{State: StateRollingBack, Effect: EffectRollback, Success: []State{StateRolledBack}, Failure: StateRecoveryNeeded},
	{State: StateRolledBack, Effect: EffectReleaseHold, Failure: StateRecoveryNeeded},
	{State: StateRevertedClean, Effect: EffectRevertClean, Failure: StateRecoveryNeeded},
	// recovery_needed keeps whatever hold exists: only an attended operator
	// clears it (brief §3.4.6).
	{State: StateRecoveryNeeded, Effect: EffectNone},
	{State: StateCancelled, Effect: EffectNone},
	{State: StateRefused, Effect: EffectNone},
}

var rowByState = func() map[State]Row {
	m := make(map[State]Row, len(table))
	for _, r := range table {
		m[r.State] = r
	}
	return m
}()

// AllStates lists every state, in table order.
func AllStates() []State {
	out := make([]State, 0, len(table))
	for _, r := range table {
		out = append(out, r.State)
	}
	return out
}

// Lookup returns s's row.
func Lookup(s State) (Row, bool) {
	r, ok := rowByState[s]
	if ok {
		r.Success = append([]State(nil), r.Success...)
	}
	return r, ok
}

// IsTerminal reports whether s has no success successor.
func IsTerminal(s State) bool {
	r, ok := rowByState[s]
	return ok && len(r.Success) == 0
}

// Finished reports whether a job at (s, p) is over: a terminal state whose
// effect, if any, is done. complete/started and rolled_back/started are NOT
// finished — their hold clear has not run yet, and a resume must run it.
func Finished(s State, p Phase) bool {
	return IsTerminal(s) && p == PhaseDone
}

// EntryPhase is the phase a state is entered in: started when it has a side
// effect (persisted BEFORE the effect runs), done when it has none.
func EntryPhase(s State) Phase {
	if r, ok := rowByState[s]; ok && r.Effect == EffectNone {
		return PhaseDone
	}
	return PhaseStarted
}

// CheckMove is the transition validator: nil only for an edge the table
// allows from (from, phase).
//
//   - from DONE: to one of the row's Success states;
//   - from STARTED: to the row's Failure state;
//   - from any unfinished state, either phase: to recovery_needed (the
//     30-minute rule, the attempts cap, a step budget);
//   - from a Cancellable state, either phase: to cancelled.
//
// Everything else — a skipped state, a success edge from STARTED (the step
// has not persisted its receipt), a move out of a finished job, a self-edge
// — is an error.
func CheckMove(from State, phase Phase, to State) error {
	r, ok := rowByState[from]
	if !ok {
		return fmt.Errorf("%w: unknown state %q", ErrForbiddenEdge, from)
	}
	if _, ok := rowByState[to]; !ok {
		return fmt.Errorf("%w: unknown state %q", ErrForbiddenEdge, to)
	}
	if phase != PhaseStarted && phase != PhaseDone {
		return fmt.Errorf("%w: unknown phase %q", ErrForbiddenEdge, phase)
	}
	if Finished(from, phase) {
		return fmt.Errorf("%w: %s is finished", ErrForbiddenEdge, from)
	}
	if to == StateRecoveryNeeded {
		return nil
	}
	// W-ADDON-ONE-CLICK self-recovery: a DONE pre-activation state may end
	// reverted_clean too — the R-i re-proof before activate failed (nothing
	// was swapped yet). Only the two states whose advance performs that
	// re-proof (nt8_skipped and addon_reconnected) may take this edge; every
	// other state must fail its own step to reach reverted_clean.
	if to == StateRevertedClean && phase == PhaseDone && (from == StateNT8Skipped || from == StateAddonReconnected) {
		return nil
	}
	if to == StateCancelled && r.Cancellable {
		return nil
	}
	if phase == PhaseDone {
		for _, s := range r.Success {
			if s == to {
				return nil
			}
		}
	} else {
		if to == r.Failure {
			return nil
		}
		for _, s := range r.ExtraFailure {
			if s == to {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s/%s → %s", ErrForbiddenEdge, from, phase, to)
}

// CheckFinish is nil only when (s, p) is a STARTED state with a side effect —
// the only thing that may become DONE in place.
func CheckFinish(s State, p Phase) error {
	r, ok := rowByState[s]
	if !ok {
		return fmt.Errorf("%w: unknown state %q", ErrForbiddenEdge, s)
	}
	if r.Effect == EffectNone {
		return fmt.Errorf("%w: %s has no side effect to finish", ErrForbiddenEdge, s)
	}
	if p != PhaseStarted {
		return fmt.Errorf("%w: %s/%q is not started", ErrForbiddenEdge, s, p)
	}
	return nil
}
