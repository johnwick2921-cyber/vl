package updaterworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"vl/internal/updaterjob"
)

// ── the runner ──────────────────────────────────────────────────────────────
//
// The ONE rule this file exists for: every transition is persisted BEFORE its
// side effect (dispatch §2). A state with an effect is written "started"
// (with every field the effect and its rollback need) and only then does the
// effect run; its receipt is appended and the state written "done" after.
// A crash anywhere therefore leaves a file that says exactly which effect may
// have run, and a fresh worker re-runs that one step (brief §3.4):
//
//	started on disk  → Retry (attempts+1, cap 3 ⇒ recovery_needed), re-run the step
//	done on disk     → advance: decide the next state, persist it started, run it
//	finished on disk → nothing (recovery_needed also stops the worker: OQ-3)
//
// TestEveryTransitionPersistsBeforeItsSideEffect has every fake side effect
// re-read the job file and find its own state "started".

// stepResult is what one step's side effect produced.
type stepResult struct {
	receipts []Receipt               // appended BEFORE Finish or the failure edge (U1 fold)
	set      func(j *updaterjob.Job) // fields persisted with a SUCCESSFUL finish
	err      error                   // the step failed
	failTo   updaterjob.State        // overrides the row's Failure state ("" = the row's)
	reason   string                  // recovery_reason when the failure edge is recovery_needed
	blocker  string                  // persisted with DONE (the nt8 park)
	abort    error                   // a persist inside the step failed: write nothing more
}

// drive runs job id until it finishes, parks (nt8_updated) or stops.
func (w *Worker) drive(ctx context.Context, id string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		j, err := updaterjob.Read(w.dataDir(), id)
		if err != nil {
			return fmt.Errorf("read job %s: %w", id, err)
		}
		if updaterjob.Finished(j.State, j.Phase) {
			w.finished(j)
			return nil
		}
		if j.Phase == updaterjob.PhaseStarted {
			// A started step on disk that this call did not start: the worker
			// crashed or restarted inside it. Count the run, then re-run it.
			k, err := w.retry(j)
			if errors.Is(err, errMoved) {
				continue
			}
			if err != nil {
				return err
			}
			if k.State != j.State {
				continue // attempts cap → recovery_needed
			}
			if err := w.execute(ctx, k); err != nil && !errors.Is(err, errMoved) {
				return err
			}
			continue
		}
		parked, err := w.advance(ctx, j)
		if errors.Is(err, errMoved) {
			continue
		}
		if err != nil {
			return err
		}
		if parked {
			return nil
		}
	}
}

// finished releases the worker from a job that is over. recovery_needed
// latches the worker: install is refused until it is restarted (OQ-3). The
// main-tree lock is settled EARLIER, in the terminal edge itself (finish's
// failure edges + stepReleaseHold): a finished job file is immutable.
func (w *Worker) finished(j updaterjob.Job) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active == j.JobID {
		w.active = ""
	}
	if j.State == updaterjob.StateRecoveryNeeded && w.stopped == "" {
		w.stopped = "recovery_needed: " + j.JobID
	}
	w.logf("updater: job %s → %s", j.JobID, j.State)
	// The worker binary was swapped during this job: only NOW, with the swap
	// receipt and the terminal effects persisted, may serve() exit for the
	// unit restart.
	if w.swapPending.Load() {
		closeSwapDone(w)
	}
}

// retry counts one more run of the started step on disk (persisted before the
// re-run). Past MaxAttempts the job goes to recovery_needed instead.
func (w *Worker) retry(j updaterjob.Job) (updaterjob.Job, error) {
	return w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
		err := k.Retry(w.now(*k))
		if errors.Is(err, updaterjob.ErrAttemptsExhausted) {
			markRecovery(k, fmt.Sprintf("%s ran %d times without finishing (attempts cap %d)", k.State, k.Attempts, updaterjob.MaxAttempts))
			return k.Enter(updaterjob.StateRecoveryNeeded, w.now(*k))
		}
		return err
	})
}

// execute runs the started state's side effect and persists its outcome.
func (w *Worker) execute(ctx context.Context, j updaterjob.Job) error {
	w.boundary(string(j.State) + "/started")
	// A cancel may land between the started write and here (the socket takes
	// the job mutex, the runner does not hold it across a step): never start
	// an effect for a job that has moved. The mutex on every WRITE is what
	// guarantees no hold after a cancel; this keeps a cancelled job's
	// preflight reads from running at all.
	cur, err := updaterjob.Read(w.dataDir(), j.JobID)
	if err != nil {
		return err
	}
	if cur.State != j.State || cur.Phase != j.Phase || cur.Attempts != j.Attempts {
		return fmt.Errorf("%w: at %s/%s before its effect", errMoved, cur.State, cur.Phase)
	}
	res := w.step(ctx, j)
	if res.abort != nil {
		return res.abort
	}
	if ctx.Err() != nil {
		return ctx.Err() // shutting down: the job stays started; a restart re-runs it
	}
	w.boundary(string(j.State) + "/effect")
	k, err := w.finish(ctx, j, res)
	if err != nil {
		return err
	}
	w.boundary(string(j.State) + "/done")
	// A failure edge into a STARTED state (rolling_back) runs it now, as its
	// first attempt — as advance does for a success edge. Left to the drive
	// loop, it would be taken for a crash inside it and counted as a retry
	// (verifier D6).
	if k.State != j.State && k.Phase == updaterjob.PhaseStarted {
		return w.execute(ctx, k)
	}
	return nil
}

// step dispatches to the state's side effect (the table's Effect).
func (w *Worker) step(ctx context.Context, j updaterjob.Job) stepResult {
	switch j.State {
	case updaterjob.StateDownloaded:
		return w.stepDownload(j)
	case updaterjob.StateVerified:
		return w.stepVerify(j)
	case updaterjob.StatePreflightOK:
		return w.stepPreflight(ctx, j)
	case updaterjob.StateMaintenanceHeld:
		return w.stepHold(j)
	case updaterjob.StateDrainedAcked:
		return w.stepDrain(ctx, j)
	case updaterjob.StateGateOK:
		return w.stepGate(ctx, j)
	case updaterjob.StateBackupDone:
		return w.stepBackup(ctx, j)
	case updaterjob.StateNT8Skipped, updaterjob.StateNT8Updated:
		return w.stepNT8(ctx, j)
	case updaterjob.StateActivated:
		return w.stepActivate(ctx, j)
	case updaterjob.StateBooted:
		return w.stepWatch(j)
	case updaterjob.StateBootVerified:
		return w.stepBootVerify(ctx, j)
	case updaterjob.StateWorkerSwapped:
		return w.stepWorkerSwap(ctx, j)
	case updaterjob.StateRollingBack:
		return w.stepRollback(ctx, j)
	case updaterjob.StateComplete, updaterjob.StateRolledBack:
		return w.stepReleaseHold(ctx, j)
	}
	return stepResult{err: fmt.Errorf("no step for %s", j.State), failTo: updaterjob.StateRecoveryNeeded, reason: "no step for " + string(j.State)}
}

// finish persists a step's outcome: its receipts FIRST, then DONE, or the
// failure edge (the row's, or the step's override) with the fields that edge
// needs. A failure edge into rolling_back carries the rollback's inputs, read
// BEFORE the write (outside the job mutex: the identity read may wait).
func (w *Worker) finish(ctx context.Context, j updaterjob.Job, res stepResult) (updaterjob.Job, error) {
	row, _ := updaterjob.Lookup(j.State)
	to := res.failTo
	if to == "" {
		to = row.Failure
	}
	var rb func(k *updaterjob.Job)
	if res.err != nil && to == updaterjob.StateRollingBack {
		rb = w.rollbackIntent(ctx, j)
	}
	return w.update(j.JobID, j.State, updaterjob.PhaseStarted, func(k *updaterjob.Job) error {
		now := w.now(*k)
		for _, rc := range res.receipts {
			if err := k.AddReceipt(rc, now); err != nil {
				return err
			}
		}
		if res.err == nil {
			if res.set != nil {
				res.set(k)
			}
			k.Blocker = res.blocker
			// a rollback keeps WHY it rolled back: M5 shows rolled_back with it
			if k.State != updaterjob.StateRollingBack && k.State != updaterjob.StateRolledBack {
				k.Error = ""
			}
			return k.Finish(now)
		}
		k.Error = clipText(res.err.Error())
		switch to {
		case updaterjob.StateRecoveryNeeded:
			reason := res.reason
			if reason == "" {
				reason = string(k.State) + " failed: " + k.Error
			}
			markRecovery(k, reason)
			// WORKER-TAKES-THE-LOCK: recovery_needed KEEPS the lock and
			// names it in the job — a human looks before anything else
			// touches the tree.
			if err := k.AddReceipt(keptLockReceipt(lockSessionFor(k.JobID), now), now); err != nil {
				return err
			}
		case updaterjob.StateRollingBack:
			rb(k)
		}
		if to == updaterjob.StateRefused {
			// WORKER-TAKES-THE-LOCK: refused releases OUR lock (never a
			// stranger's — an attended session's lock is left to its holder).
			if err := k.AddReceipt(w.lockReleaseReceipt(lockSessionFor(k.JobID), now), now); err != nil {
				return err
			}
		}
		return k.Enter(to, now)
	})
}

// advance moves a DONE job to its next state, persisting every field the next
// effect needs BEFORE it runs, then runs it. parked: the job waits for an
// attended resume (nt8_updated).
func (w *Worker) advance(ctx context.Context, j updaterjob.Job) (parked bool, err error) {
	var next updaterjob.State
	var set func(k *updaterjob.Job)
	switch j.State {
	case updaterjob.StateRequested:
		next = updaterjob.StateDownloaded
	case updaterjob.StateDownloaded:
		next = updaterjob.StateVerified
	case updaterjob.StateVerified:
		next, set = updaterjob.StatePreflightOK, w.installIntent(j)
	case updaterjob.StatePreflightOK:
		next = updaterjob.StateMaintenanceHeld
	case updaterjob.StateMaintenanceHeld:
		next = updaterjob.StateDrainedAcked
	case updaterjob.StateDrainedAcked:
		next = updaterjob.StateGateOK
	case updaterjob.StateGateOK:
		next, set = updaterjob.StateBackupDone, w.backupIntent(j)
	case updaterjob.StateBackupDone:
		d := w.decideNT8(ctx, j)
		next = updaterjob.StateNT8Updated
		if d.Decision == updaterjob.NT8Skipped {
			next = updaterjob.StateNT8Skipped
		}
		set = func(k *updaterjob.Job) { k.NT8 = &d }
	case updaterjob.StateNT8Updated, updaterjob.StateNT8Skipped:
		if j.State == updaterjob.StateNT8Updated {
			k, proceed, err := w.resumeFromPark(ctx, j)
			if err != nil || !proceed {
				return true, err
			}
			j = k
		}
		// R-i: the LAST re-proof before the point of no return. A failure
		// here has installed nothing: recovery_needed, hold kept.
		if err := w.reprove(ctx, j); err != nil {
			if errors.Is(err, errMoved) || ctx.Err() != nil {
				return false, err
			}
			return false, w.toRecovery(j, "ready not re-proven before activate: "+err.Error())
		}
		id, err := w.lib.CurrentIdentity()
		if err != nil {
			return false, w.toRecovery(j, "cannot read the running identity before activate: "+err.Error())
		}
		next = updaterjob.StateActivated
		set = func(k *updaterjob.Job) {
			k.IdentityBefore = &id
			w.setBootWatch(k)
		}
	case updaterjob.StateActivated:
		next = updaterjob.StateBooted
	case updaterjob.StateBooted:
		next = updaterjob.StateBootVerified
	case updaterjob.StateBootVerified:
		next = updaterjob.StateWorkerSwapped
	case updaterjob.StateWorkerSwapped:
		next = updaterjob.StateComplete
	case updaterjob.StateRollingBack:
		next = updaterjob.StateRolledBack
	default:
		return false, fmt.Errorf("updaterworker: no successor for %s/%s", j.State, j.Phase)
	}
	k, err := w.update(j.JobID, j.State, updaterjob.PhaseDone, func(k *updaterjob.Job) error {
		if set != nil {
			set(k)
		}
		return k.Enter(next, w.now(*k))
	})
	if err != nil {
		return false, err
	}
	if k.Phase == updaterjob.PhaseStarted {
		return false, w.execute(ctx, k)
	}
	return false, nil
}

// installIntent: the install's three halves as the activation addresses
// them, persisted with preflight_ok BEFORE its checks (brief §3.3 row 4). The
// sha is the running binary's vcs.revision; when it cannot be read as 40 hex,
// or it IS the release, Install stays absent and the preflight refuses with
// the reason (U1 fold: install and release SHAs differ).
func (w *Worker) installIntent(j updaterjob.Job) func(k *updaterjob.Job) {
	bin := w.cfg.Target.InstallBinaryPath()
	rev, _, err := w.host.BuildInfo(bin)
	return func(k *updaterjob.Job) {
		if err != nil || !isSHA40(rev) || (k.Release != nil && rev == k.Release.SHA) {
			return
		}
		inst := w.cfg.Target.InstallRelease(rev)
		k.Install = &inst
	}
}

// backupIntent: the DB backup file and the job-scoped snapshot of the install
// halves (103's Snapshot layout: <dest>/{vl-bin,web/dist,RELEASE}), both
// under <BackupRoot>/<job>/, persisted BEFORE backup_done's effect.
func (w *Worker) backupIntent(j updaterjob.Job) func(k *updaterjob.Job) {
	return func(k *updaterjob.Job) {
		dir := filepath.Join(w.cfg.BackupRoot, k.JobID)
		k.BackupPath = filepath.Join(dir, "data.db")
		if k.Install != nil {
			snap := snapshotRelease(filepath.Join(dir, "install"), k.Install.SHA, filepath.Base(k.Install.Binary))
			k.Snapshot = &snap
		}
	}
}

// setBootWatch persists the boot proof's inputs BEFORE the kill: the log the
// new process will write (named by its boot date, predicted from now), the
// offset to scan from, and the kill instant Watch takes as since (truncated
// to the second: log lines carry whole seconds, and a boot in the kill's own
// second must still count).
func (w *Worker) setBootWatch(k *updaterjob.Job) {
	since := w.host.Now().Truncate(time.Second)
	bin := "vl-bin" // no release yet means the install's name
	if k.Release != nil {
		bin = k.Release.Binary
	}
	path := w.predictedLog(since, bin)
	off := fileSize(path)
	k.LogPath, k.LogOffset, k.WatchSince = path, &off, &since
}

// rollbackIntent reads the rollback's inputs BEFORE rolling_back is written:
// the identity to kill (CurrentIdentity, retried for IdentityRetry, else the
// last identity the job recorded), and the old sha's boot-proof inputs.
func (w *Worker) rollbackIntent(ctx context.Context, j updaterjob.Job) func(k *updaterjob.Job) {
	id, ok := w.currentIdentityRetry(ctx)
	if !ok {
		switch {
		case j.IdentityAfter != nil:
			id = *j.IdentityAfter
		case j.IdentityBefore != nil:
			id = *j.IdentityBefore
		}
	}
	since := w.host.Now().Truncate(time.Second)
	bin := "vl-bin" // the snapshot's own binary name
	if j.Snapshot != nil {
		bin = j.Snapshot.Binary
	}
	path := w.predictedLog(since, bin)
	off := fileSize(path)
	return func(k *updaterjob.Job) {
		if id.PID > 0 {
			k.IdentityRollback = &id
		}
		k.RollbackLogPath, k.RollbackLogOffset, k.RollbackWatchSince = path, &off, &since
	}
}

func (w *Worker) currentIdentityRetry(ctx context.Context) (Identity, bool) {
	deadline := w.host.Now().Add(w.cfg.Budgets.IdentityRetry)
	for {
		if id, err := w.lib.CurrentIdentity(); err == nil && id.PID > 0 {
			return id, true
		}
		if !w.host.Now().Before(deadline) || w.host.Sleep(ctx, w.cfg.Budgets.Poll) != nil {
			return Identity{}, false
		}
	}
}

// logPrefixForBinary is the log prefix of the binary that will run: vl_ when
// it is vl-bin — the prefix comes from the BINARY, never from
// which file happens to exist.
func logPrefixForBinary(binPath string) string {
	if filepath.Base(binPath) == "vl-bin" {
		return "vl_"
	}
	return "vl_"
}

// predictedLog is <install>/data/<prefix><local date of t>.log — the bot's
// logger names its file by its BOOT date in its own local zone (logger.go),
// and the worker refuses to run with TZ set so its zone is the bot's. The
// prefix is chosen from the binary that will run (logPrefixForBinary).
func (w *Worker) predictedLog(t time.Time, binPath string) string {
	return filepath.Join(w.cfg.Target.LogDir, logPrefixForBinary(binPath)+t.In(time.Local).Format("2006-01-02")+".log")
}

// fileSize is the size of path, 0 when absent (a log the new boot creates).
func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// resumeFromPark is the attended resume from nt8_updated (dispatch §0): the
// job leaves the park ONLY on a resume verb (never on its own), only while
// the 30-minute rule holds (C21), and only when the AddOn now acks THIS job
// with the manifest's build. The resume is persisted (resumed_at + a
// "resume" receipt) BEFORE anything else happens.
func (w *Worker) resumeFromPark(ctx context.Context, j updaterjob.Job) (updaterjob.Job, bool, error) {
	w.mu.Lock()
	requested := w.resume[j.JobID]
	delete(w.resume, j.JobID)
	w.mu.Unlock()
	if !requested {
		return j, false, nil
	}
	if updaterjob.Stale(j, w.host.Now()) {
		if err := w.toRecovery(j, "stale at resume: older than 30 minutes — never resumed (C21)"); err != nil {
			return j, false, err
		}
		return j, false, errMoved // the runner re-reads: recovery_needed stops it
	}
	start := w.host.Now()
	ev := map[string]string{}
	err := w.resumeProof(ctx, j, ev)
	rc := w.receipt("resume", start, ev, err)
	k, uerr := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
		now := w.now(*k)
		if err := k.AddReceipt(rc, now); err != nil {
			return err
		}
		if err != nil {
			k.Blocker = clipText("resume refused: " + err.Error() + " — fix it, then vl-updater resume " + k.JobID)
			return nil
		}
		k.ResumedAt = &now
		k.Blocker = ""
		return nil
	})
	if uerr != nil {
		return j, false, uerr
	}
	return k, err == nil, nil
}

// resumeProof re-reads what the park waited for: the AddOn, restarted by the
// owner's F5, acks THIS job, held, fresh, with the manifest's build.
func (w *Worker) resumeProof(ctx context.Context, j updaterjob.Job, ev map[string]string) error {
	facts, err := w.facts(j)
	if err != nil {
		return err
	}
	want := facts.AddonBuildID
	ev["manifest_build_id"] = orNA(want)
	if !validBuildID(want) {
		return fmt.Errorf("the manifest names no addon build_id")
	}
	m, err := w.app.Maintenance(ctx)
	if err != nil {
		return err
	}
	a := m.AddonAck
	if a == nil {
		return errors.New("no AddOn maintenance_ack")
	}
	ev["acked_build_id"], ev["acked_at"] = orNA(a.BuildID), a.Received
	ev["acked_accept_seq"] = strconv.FormatUint(a.AcceptSeq, 10)
	if why := ackFor(a, j.JobID); why != "" {
		return errors.New(why)
	}
	if a.BuildID != want {
		return fmt.Errorf("the AddOn acks build %q, the release is %q", a.BuildID, want)
	}
	// #206 review fold (runner.go:471): when the park was because the
	// release's ninjascript/*.cs differs but the manifest's build id was NOT
	// bumped (it equals what the OLD AddOn already acks), the build check
	// cannot tell a restarted AddOn from the old one — the resume then
	// passed without any F5 and activated a Go/C# mismatch. The proof of a
	// restart is a NEW connection: accept_seq is assigned per connection,
	// so the resume demands an ack from a different connection than the one
	// the park recorded (an F5 + NT8 restart reconnects the AddOn).
	if j.NT8 != nil && j.NT8.CSUnchanged != nil && !*j.NT8.CSUnchanged && a.AcceptSeq == j.NT8.AckAcceptSeq {
		return fmt.Errorf("the release's %s differs and the AddOn still runs on connection accept_seq=%d — copy it over, F5 and restart NT8, then resume", ninjascriptGlob, a.AcceptSeq)
	}
	return nil
}

// clipText is a short error for a receipt or the job file (never a token: no
// error the worker builds carries one — the HTTP errors name the path only).
func clipText(s string) string {
	const max = 1024
	if len(s) > max {
		return s[:max]
	}
	return s
}

// receipt is a worker-side receipt (the library's carry their own).
func (w *Worker) receipt(step string, start time.Time, ev map[string]string, err error) Receipt {
	end := w.host.Now()
	if end.Before(start) {
		end = start
	}
	r := Receipt{Step: step, StartedAt: start, EndedAt: end, OK: err == nil}
	if len(ev) > 0 {
		r.Evidence = ev
	}
	if err != nil {
		r.Err = clipText(err.Error())
	}
	return r
}

func isSHA40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
