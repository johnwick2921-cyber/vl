package updaterworker

import (
	"context"
	"fmt"
	"strconv"

	"vl/internal/updaterjob"
)

// ── feat/addon-catchup ──────────────────────────────────────────────────────
//
// The owner's "why do I have to copy to NinjaTrader again": when the worker
// starts, and at the end of every install, it compares the AddOn build NT8 is
// ACTUALLY running (the last hello) with the INSTALLED release's AddOn build.
// If they differ, the worker rebuilds the AddOn from the install's own
// ninjascript and places it — restarting NT8 and proving the reconnect when NT8
// is open, building+placing only when NT8 is closed (the new DLL loads at the
// next NT8 start). This is how the partner's first install (run by their OLD
// worker) still gets the AddOn with no hands.
//
// The catch-up runs as its own durable job (StateCatchup → addon_built →
// addon_placed → [nt8_restarted →] addon_reconnected → complete) so it gets
// the same self-recovery as a one-click install: a build/place/restart failure
// reverts clean (the old DLL is restored), a reconnect timeout rolls back.

// catchupStatus reports whether the running AddOn build differs from the
// INSTALLED release's AddOn build. needed is true when they differ (or NT8 is
// closed and the install ships a build the AddOn never reported). nt8Open is
// true when the AddOn's last hello exists.
func (w *Worker) catchupStatus(ctx context.Context) (needed, nt8Open bool, err error) {
	m, merr := w.app.Maintenance(ctx)
	if merr != nil {
		return false, false, fmt.Errorf("maintenance read: %w", merr)
	}
	running := ""
	if m.AddonAck != nil {
		nt8Open = true
		running = m.AddonAck.BuildID
	}
	installed := w.oldInstallBuildID()
	if installed == "" || !validBuildID(installed) {
		// Cannot name the install's AddOn build — nothing to compare against.
		return false, nt8Open, nil
	}
	if nt8Open && running == installed {
		return false, nt8Open, nil
	}
	// NT8 is OPEN and the build differs: the catch-up restarts NT8, which is
	// only owed when the box is flat + SIM — never force NT8 down while a
	// position is live. If not flat+SIM, wait for the next start or install.
	if nt8Open {
		if err := w.flatSIMGate(ctx, updaterjob.Job{}); err != nil {
			w.logf("updater: catch-up held — the box is not flat+SIM: %v", err)
			return false, nt8Open, nil
		}
	}
	return true, nt8Open, nil
}

// maybeStartCatchup starts a catch-up job when one is owed and the worker is
// idle. It returns true when it created a job (the runner will drive it).
func (w *Worker) maybeStartCatchup(ctx context.Context) (bool, error) {
	w.mu.Lock()
	idle := w.active == "" && w.stopped == ""
	w.mu.Unlock()
	if !idle {
		return false, nil
	}
	needed, nt8Open, err := w.catchupStatus(ctx)
	if err != nil {
		return false, err
	}
	if !needed {
		return false, nil
	}
	// Re-check idle under the mutex: an install may have landed meanwhile.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active != "" || w.stopped != "" {
		return false, nil
	}
	jobID := "catchup-" + strconv.FormatInt(w.host.Now().UnixNano(), 36)
	j, err := updaterjob.NewCatchup(jobID, w.host.Now())
	if err != nil {
		return false, err
	}
	j.CatchupNT8Open = nt8Open
	if err := updaterjob.Write(w.dataDir(), j); err != nil {
		return false, fmt.Errorf("write catch-up job: %w", err)
	}
	w.active = jobID
	w.signal()
	w.logf("updater: catch-up %s: the running AddOn differs from the install — rebuilding (%s)", jobID, map[bool]string{true: "NT8 open", false: "NT8 closed"}[nt8Open])
	return true, nil
}
