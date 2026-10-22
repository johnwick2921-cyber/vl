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
// ACTUALLY running (the last hello) with the VERIFIED installed release's
// AddOn build (m355: the signed manifest's addon.build_id of
// <VL_RELEASE_DIR>/<running revision> — never oldInstallBuildID()). If the
// running build differs, the worker rebuilds the AddOn from that verified
// release's own ninjascript and places it — restarting NT8 and proving the
// reconnect when NT8 is open, building+placing only when NT8 is closed (the
// new DLL loads at the next NT8 start).
//
// The catch-up runs as its own durable job (StateCatchup → addon_built →
// addon_placed → [nt8_restarted →] addon_reconnected → complete) so it gets
// the same self-recovery as a one-click install: a build/place/restart failure
// reverts clean (the old DLL is restored), a reconnect timeout rolls back.

// catchupDecision is the one read of "is a catch-up owed" (m355): the running
// AddOn build vs the verified installed release's manifest build.
type catchupDecision struct {
	needed  bool
	nt8Open bool
	facts   ReleaseFacts // the verified artifact's facts (its signed manifest build)
	running string       // the running AddOn's ack build ("" when NT8 is closed)
}

// catchupStatus reports whether a catch-up is owed. It is owed when the
// verified installed-release artifact names a build the running AddOn does not
// run — and is SKIPPED (with a log) when the artifact is unavailable, already
// current, or would DOWNGRADE the box (m355). nt8Open is true when the AddOn's
// last hello exists.
func (w *Worker) catchupStatus(ctx context.Context) (catchupDecision, error) {
	var d catchupDecision
	m, merr := w.app.Maintenance(ctx)
	if merr != nil {
		return d, fmt.Errorf("maintenance read: %w", merr)
	}
	if m.AddonAck != nil {
		d.nt8Open = true
		d.running = m.AddonAck.BuildID
	}
	facts, ferr := w.catchupAddonFacts()
	if ferr != nil {
		// m355 rule 1: no verified installed-release artifact → SKIP the AddOn
		// step. InstallDir/ninjascript is never a fallback.
		w.logf("updater: catch-up held — no verified installed-release artifact: %v", ferr)
		return d, nil
	}
	d.facts = facts
	want := facts.AddonBuildID
	if !validBuildID(want) {
		w.logf("updater: catch-up held — the verified release names no addon build_id")
		return d, nil
	}
	if d.nt8Open {
		if d.running == want {
			return d, nil // already current — nothing to place
		}
		if validBuildID(d.running) {
			// m355 rule 3 — NEVER DOWNGRADE, checked here so the job is not
			// even started for an older (or unrankable) build.
			if cmp, ok := compareBuildID(want, d.running); !ok || cmp < 0 {
				w.logf("updater: catch-up held — never downgrade: the verified release build %q is not newer than the running %q", want, d.running)
				return d, nil
			}
		}
		// NT8 is OPEN and the build differs (a strict upgrade): the catch-up
		// restarts NT8, which is only owed when the box is flat + SIM — never
		// force NT8 down while a position is live.
		if err := w.flatSIMGate(ctx, updaterjob.Job{}); err != nil {
			w.logf("updater: catch-up held — the box is not flat+SIM: %v", err)
			return d, nil
		}
	}
	d.needed = true
	return d, nil
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
	d, err := w.catchupStatus(ctx)
	if err != nil {
		return false, err
	}
	if !d.needed {
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
	j.CatchupNT8Open = d.nt8Open
	// m355 rule 2: the expected build is the VERIFIED artifact's signed-manifest
	// build, persisted so verify-on-place and the reconnect compare the SAME
	// value the catch-up was decided against — never oldInstallBuildID().
	// m357 D: also record the pre-job running build, so a legacy backup (no
	// meta sidecar) can only be accepted when it embeds that build.
	j.Addon = &updaterjob.AddonState{ExpectedBuild: d.facts.AddonBuildID, PreJobBuild: d.running}
	if !d.nt8Open {
		// Release #35: a catch-up that saw NT8 closed must carry the same
		// NT8-closed state an install does, so stepAddonPlace runs the no-F5
		// migration (migrate + RebuildCustom + PlaceCustom) INLINE while NT8
		// is closed instead of deferring it to a restart that will never run.
		j.NT8 = &updaterjob.NT8Decision{Decision: updaterjob.NT8Updated, Absent: true}
	}
	if err := updaterjob.Write(w.dataDir(), j); err != nil {
		return false, fmt.Errorf("write catch-up job: %w", err)
	}
	w.active = jobID
	w.signal()
	w.logf("updater: catch-up %s: the running AddOn differs from the verified release — rebuilding (%s)", jobID, map[bool]string{true: "NT8 open", false: "NT8 closed"}[d.nt8Open])
	return true, nil
}
