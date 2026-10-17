package updaterworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vl/internal/updaterjob"
)

// ── the transition bodies (brief §3.3, all accepted defaults) ──────────────
//
// Each runs with its state ALREADY persisted "started". Reads that fail
// before the hold refuse the job (nothing was held or changed); after the
// hold they keep it (recovery_needed) or, past the point of no return, roll
// back. The token never reaches a receipt: no evidence value is built from a
// request, a header or an environment variable.

const (
	// calendarFile is the owner-editable static T1 file (release owner_data,
	// "install: template-only"). The worker never writes it; a release whose
	// copy differs from the install's is refused (ruling 1790261377377).
	calendarFile = "calendar_static_t1.json"
	// ackMaxAgeMs is provider/ninjatrader MaintenanceAckMaxAge (3 × the 5 s
	// resend): an older ack is not fresh.
	ackMaxAgeMs = 15000
	// ackDistinct: two acks are DISTINCT only when their ARRIVALS (rebuilt on
	// the worker's monotonic clock as now − AgeMs) are this far apart. Acks
	// are resent every 5 s; the threshold only absorbs HTTP-latency jitter
	// between the app's age reading and the worker's now (~ms), never a
	// wall-clock step.
	ackDistinct = time.Second
)

// preflightFlatLegs: the pre-hold flat probe (C22) — every non-hold leg that
// can fail on a live position, a working order, an arm or a planner read, or
// a trader the gate cannot see. trader_cutover:* legs are added by prefix.
// The census leg here is addon_census_prehold, NOT addon_census (#206 review
// fold): the wire sends maintenance frames only while held, so a never-held
// connection has no census yet and addon_census would refuse every install
// on a fresh bot process (the normal production path — every activation
// restarts the bot). The prehold leg passes the no-census STATE, demands a
// FRESH census when one exists (a stale held:false release ack is evidence of
// nothing), and fails on every census content violation; drain's
// addon_census re-checks a fresh census right after the hold.
var preflightFlatLegs = []string{"addon_census_prehold", "ledger_exposure", "planner_in_flight", "traders_nt8"}

// drainLegs: drained_acked (C11 + ruling 1790258770876) — held, drained, the
// AddOn acked THIS job, flat, no working orders, nothing in flight.
var drainLegs = []string{"hold", "go_drained", "in_flight_sends", "queued_signals", "addon_ack", "addon_census", "ledger_exposure", "planner_in_flight"}

// ── the main-tree lock (WORKER-TAKES-THE-LOCK, owner order 10-02 12:1x) ────
//
// The worker ACQUIRES the main-tree lock itself, as session
// "updater-<job id first 12>", right before preflight — the SAME atomic
// acquire verb humans use, with an expiry covering the job budget. It never
// reclaims, never takes a held or stale lock, and never clears-incomplete.
// A lock held by anyone else refuses preflight naming the holder: an
// attended install is just a button install — humans never hold the lock
// across one. The worker releases its own lock at complete / rolled_back /
// refused; on recovery_needed it KEEPS the lock and names it in the job, so
// a human looks before anything else touches the tree. A worker restart
// mid-job finds its own lock by the deterministic session name and continues
// — it never double-acquires.

// lockSessionPrefix is the session name a worker lock always starts with.
const lockSessionPrefix = "updater-"

// lockSessionFor is the worker's lock identity for a job. It is DETERMINISTIC
// from the job id, so a worker restart mid-job recognises its own lock.
func lockSessionFor(jobID string) string {
	id := jobID
	if len(id) > 12 {
		id = id[:12]
	}
	return lockSessionPrefix + id
}

// lockWindowMinutes is the acquire expiry: every job budget plus the three
// planner waits plus a rollback, plus margin. The lock's keeper beats until
// this window and then stops; OUR OWN stale lock does not block us (the
// holder name is ours), and a park can outlast the keeper — only the holder
// name matters to us.
func (w *Worker) lockWindowMinutes() int {
	b := w.cfg.Budgets
	total := b.PreflightFlat + b.Drain + b.Gate + 3*b.PlannerWait + 3*b.Reprove +
		2*b.Watch + 2*b.PostBootAck + 2*b.IdentityRetry + 2*b.HoldClear + 20*time.Minute
	return int(total.Minutes()) + 1
}

// ensureMainTreeLock acquires-or-verifies the main-tree lock for THIS job
// and returns the outcome: "acquired" (we took a free lock) or "ours" (the
// lock already names our session — a restart mid-job). Any other state
// refuses, naming the holder.
func (w *Worker) ensureMainTreeLock(j updaterjob.Job) (string, error) {
	session := lockSessionFor(j.JobID)
	holder, err := w.host.LockHolder()
	if err != nil {
		return "", fmt.Errorf("the main-tree lock status is unreadable (C19: %v)", err)
	}
	switch {
	case holder == "":
		acquired, holderNow, err := w.host.LockAcquire(session, "worker job "+j.JobID, w.lockWindowMinutes())
		if err != nil {
			return "", fmt.Errorf("the main-tree lock acquire failed (C19: %v)", err)
		}
		if acquired {
			return "acquired", nil
		}
		// Lost the race to another acquirer: refuse naming whoever won.
		return "", fmt.Errorf("the main-tree lock is not held by this job (C19: held by %q) — the worker never takes a held or stale lock", orNA(holderNow))
	case holder == session:
		// Ours from before a restart: continue, never double-acquire. This
		// includes our OWN stale lock — the keeper window is a bound, not
		// liveness for a holder that is still running.
		return "ours", nil
	default:
		return "", fmt.Errorf("the main-tree lock is not held by this job (C19: held by %q) — the worker never takes a held or stale lock", holder)
	}
}

// releaseOurLock releases the main-tree lock ONLY when it is ours: a free
// lock is a no-op, and a lock held by anyone else is never touched. It is
// the ONE release path the worker uses.
func (w *Worker) releaseOurLock(session string) error {
	holder, err := w.host.LockHolder()
	if err != nil {
		return err
	}
	if holder == "" || holder == session {
		return w.host.LockRelease(session)
	}
	return nil // not ours — leave it to its holder
}

// lockReleaseReceipt performs the terminal release and renders its receipt:
// outcome "released" (ours, now free), "free" (nothing to release), or
// "not_ours" (an attended/foreign holder keeps it), and "release_failed"
// with the error.
func (w *Worker) lockReleaseReceipt(session string, now time.Time) updaterjob.Receipt {
	ev := map[string]string{"session": session}
	holder, herr := w.host.LockHolder()
	var err error
	switch {
	case herr != nil:
		err = herr
	case holder == "":
		ev["outcome"] = "free"
	case holder != session:
		ev["outcome"] = "not_ours"
		ev["holder"] = holder
	default:
		if err = w.host.LockRelease(session); err == nil {
			ev["outcome"] = "released"
		} else {
			ev["outcome"] = "release_failed"
		}
	}
	rec := updaterjob.Receipt{Step: "main_tree_lock", StartedAt: now, EndedAt: w.host.Now(), OK: err == nil, Evidence: ev}
	if err != nil {
		rec.Err = clipText(err.Error())
	}
	return rec
}

// keptLockReceipt names the kept lock on recovery_needed: the lock stays
// held by our session and a human looks before anything else touches the
// tree. It performs NO release.
func keptLockReceipt(session string, now time.Time) updaterjob.Receipt {
	return updaterjob.Receipt{Step: "main_tree_lock", StartedAt: now, EndedAt: now, OK: false,
		Evidence: map[string]string{"session": session, "outcome": "kept"},
		Err:      "recovery_needed: the lock stays held by this job — a human looks first"}
}

// drainPathNT8Absent is the drain path taken when NT8 is CLOSED
// (UPDATER-NT8-CLOSED, owner ruling 22:1x CT 09-27): the link has been down
// ≥ 60s continuously, so no AddOn ack can exist. The drain then passes on the
// gate's nt8_absent verdict — every ledger leg on its own evidence, plus the
// SIM-only bound-account check — and the job file records the path and the
// server's disconnect stamp. An AddOn that connects during the job revokes
// the verdict (the gate flips eligible=false) and the normal ack/census legs
// apply again from that moment, never grandfathered.
const drainPathNT8Absent = "nt8_absent"

func (w *Worker) stepDownload(j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	v, err := w.rel.Verdict(j.ReleaseID)
	if err == nil {
		ev["manifest_sha256"] = v.ManifestSHA256
		var n int
		n, err = w.rel.Rehash(v)
		if err == nil {
			ev["artifacts"] = strconv.Itoa(n)
		}
	}
	return stepResult{receipts: []Receipt{w.receipt("download", start, ev, err)}, err: err}
}

func (w *Worker) stepVerify(j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	var rel Release
	err := func() error {
		v, err := w.rel.Verdict(j.ReleaseID)
		if err != nil {
			return err
		}
		f, err := w.rel.Reverify(v)
		if err != nil {
			return err
		}
		ev["signer_fingerprint"], ev["source_sha"] = f.SignerFingerprint, f.SourceSHA
		if f.ReleaseID != j.ReleaseID || f.SourceSHA != v.SourceSHA || f.ManifestSHA256 != v.ManifestSHA256 || !isSHA40(f.SourceSHA) {
			return fmt.Errorf("the re-verified manifest (release %q, source %q) is not the verdict's (release %q, source %q)", f.ReleaseID, f.SourceSHA, j.ReleaseID, v.SourceSHA)
		}
		rel, err = w.lib.Resolve(v.ReleaseDir)
		if err != nil {
			return err
		}
		if rel.SHA != f.SourceSHA || filepath.Clean(rel.Dir) != filepath.Clean(v.ReleaseDir) {
			return fmt.Errorf("the release dir resolves to %s at %s, the verdict names %s at %s", rel.SHA, rel.Dir, f.SourceSHA, v.ReleaseDir)
		}
		return nil
	}()
	res := stepResult{receipts: []Receipt{w.receipt("verify", start, ev, err)}, err: err}
	if err == nil {
		res.set = func(k *updaterjob.Job) { k.SourceSHA, k.Release = rel.SHA, &rel }
	}
	return res
}

// stepPreflight is every check that must pass BEFORE the hold (brief row 4,
// C19, C20, C22). Any failure refuses the job: nothing was held or changed.
func (w *Worker) stepPreflight(ctx context.Context, j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	var receipts []Receipt
	failTo, reason := updaterjob.State(""), ""
	err := func() error {
		if j.Release == nil {
			return errors.New("no release recorded")
		}
		if j.Install == nil {
			return errors.New("the install binary's vcs.revision is unreadable, not 40 hex, or already this release")
		}
		inst, rel := *j.Install, *j.Release
		rev, mod, err := w.host.BuildInfo(inst.Binary)
		if err != nil {
			return fmt.Errorf("install binary build info: %w", err)
		}
		ev["install_sha"], ev["install_modified"] = rev, orNA(mod)
		if rev != inst.SHA || mod != "false" {
			return fmt.Errorf("the install binary is %s (modified=%s), not the clean %s the job recorded", rev, orNA(mod), inst.SHA)
		}
		marker, err := os.ReadFile(inst.ReleaseFile)
		if err != nil {
			return fmt.Errorf("the install's RELEASE marker: %w", err)
		}
		if m := firstMarkerLine(marker); !revisionsAgree(m, rev) {
			return fmt.Errorf("the install's RELEASE marker names %q, the running binary is %s", m, rev)
		}
		lockState, err := w.ensureMainTreeLock(j)
		if err != nil {
			return err
		}
		ev["main_tree_lock_state"] = lockState
		if cal, err := calendarVerdict(rel.Dir, inst.Dir); err != nil {
			return err
		} else {
			ev["calendar"] = cal
		}
		if fi, err := os.Stat(w.cfg.Target.DBFile); err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			return fmt.Errorf("no bot database at %s", w.cfg.Target.DBFile)
		}
		switch s, st := ReadHoldFor(w.dataDir(), j.JobID); s {
		case HoldAbsent:
		case HoldOurs:
			failTo, reason = updaterjob.StateRecoveryNeeded, "this job's hold is on disk before its hold step"
			return errors.New(reason)
		default:
			return fmt.Errorf("a hold is present that is not this job's: %s", holdSummary(s, st))
		}
		src, err := w.lib.Stage(rel)
		receipts = append(receipts, src)
		if err != nil {
			return fmt.Errorf("stage: %w", err)
		}
		id, err := w.lib.CurrentIdentity()
		if err != nil {
			return fmt.Errorf("the running identity: %w", err)
		}
		exe, err := w.host.ExeOf(id.PID)
		if err != nil || !sameBinary(exe, inst.Binary) {
			return fmt.Errorf("the unit's main process (pid %d) runs %q, not the install's %s", id.PID, exe, inst.Binary)
		}
		ev["pid"] = strconv.Itoa(id.PID)
		h, err := w.app.Health(ctx)
		if err != nil {
			return fmt.Errorf("health: %w", err)
		}
		ev["health_revision"] = h
		if !revisionsAgree(h, inst.SHA) {
			return fmt.Errorf("the app serves %q, not the install's %s", h, inst.SHA)
		}
		// C22: flat BEFORE the hold, or the job is refused with no hold. A 401
		// here is the token proof failing: refused at once.
		return w.pollPlanner(ctx, j, w.cfg.Budgets.PreflightFlat, w.cfg.Budgets.PlannerWait, func() (string, error) {
			g, err := w.app.InstallationGate(ctx)
			if errors.Is(err, ErrUnauthorized) {
				return "", err
			}
			if err != nil {
				return "installation-gate: " + err.Error(), nil
			}
			// UPDATER-NT8-CLOSED (P-D ruling item 3): with the AddOn's
			// socket gone >= the window, preflight runs the ABSENT leg set --
			// the census/ack/cutover legs cannot be answered by a closed
			// NT8 and must not refuse the safest install moment.
			if a := g.NT8Absent; a != nil && a.Eligible {
				if names, ok := absentPreflightLegs(a); ok {
					return firstFailingNamedLegs(GateView{Legs: a.Legs}, names), nil
				}
				return "nt8_absent: eligible but no preflight legs computed", nil
			}
			return firstFailingLeg(g, preflightFlatLegs), nil
		})
	}()
	if err == nil {
		ev["flat"] = "pass"
	}
	receipts = append(receipts, w.receipt("preflight", start, ev, err))
	return stepResult{receipts: receipts, err: err, failTo: failTo, reason: reason}
}

// proveCurrentBinary re-proves, right before a kill, what preflight proved
// ONCE (#206 review fold, runner.go:275): the unit's current MainPID runs the
// install's binary at the install's path, the install binary carries a build
// THIS JOB can legitimately be at — the pre-activate install, or the release
// (a resumed attempt whose first run crashed AFTER the swap legitimately
// finds the release installed) — and health serves the installed binary.
// Preflight binds the process; a park-hour restart or a hotfix deploy can
// silently replace it, and the kill (start-ticks-guarded to hit the unit's
// CURRENT process) would then signal something nobody proved.
func (w *Worker) proveCurrentBinary(ctx context.Context, j updaterjob.Job, id Identity) error {
	inst := *j.Install
	exe, err := w.host.ExeOf(id.PID)
	if err != nil || !sameBinary(exe, inst.Binary) {
		return fmt.Errorf("the unit's main process (pid %d) runs %q, not the install's %s", id.PID, exe, inst.Binary)
	}
	rev, mod, err := w.host.BuildInfo(inst.Binary)
	if err != nil {
		return fmt.Errorf("the install binary's build info before the kill: %w", err)
	}
	release := "n/a"
	if j.Release != nil {
		release = j.Release.SHA
	}
	if mod != "false" || (rev != inst.SHA && rev != release) {
		return fmt.Errorf("the install binary is now %s (modified=%s), neither the install's %s nor the release's %s", rev, orNA(mod), inst.SHA, release)
	}
	h, err := w.app.Health(ctx)
	if err != nil {
		return fmt.Errorf("health before the kill: %w", err)
	}
	if !revisionsAgree(h, rev) {
		return fmt.Errorf("health serves %q, not the installed binary's %s (before the kill)", h, rev)
	}
	return nil
}

// stepHold writes THIS job's hold through the census-admitted writer. A write
// that errs with our hold on disk is recovery_needed, never "refused" (U1
// verifier item 9: refused means nothing was held).
func (w *Worker) stepHold(j updaterjob.Job) stepResult {
	start := w.host.Now()
	already, err := HoldForJob(w.dataDir(), j.JobID, j.ReleaseID, start)
	ev := map[string]string{"owner": HoldOwner, "already": strconv.FormatBool(already)}
	res := stepResult{receipts: []Receipt{w.receipt("hold", start, ev, err)}, err: err}
	if err != nil {
		if s, _ := ReadHoldFor(w.dataDir(), j.JobID); s == HoldOurs {
			res.failTo, res.reason = updaterjob.StateRecoveryNeeded, "the hold write failed but this job's hold is on disk: "+clipText(err.Error())
		}
	}
	return res
}

// stepDrain READS the app's own view until it is held and drained. Two paths:
// the normal one (held + acked by the AddOn for THIS job + every drain leg)
// and nt8_absent (UPDATER-NT8-CLOSED): NT8 closed, link down ≥60s, every
// ledger leg passing on its own evidence. The gate's eligible flag flips off
// the moment an AddOn connects, so the absent verdict is revoked — the normal
// legs apply again, never grandfathered.
func (w *Worker) stepDrain(ctx context.Context, j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	err := w.pollPlanner(ctx, j, w.cfg.Budgets.Drain, w.cfg.Budgets.PlannerWait, func() (string, error) {
		g, gerr := w.app.InstallationGate(ctx)
		if gerr != nil {
			return "installation-gate: " + gerr.Error(), nil
		}
		if g.JobID != j.JobID {
			return fmt.Sprintf("installation-gate names job %q", g.JobID), nil
		}
		// The absent path: eligible + ready + the hold on disk is ours.
		if a := g.NT8Absent; a != nil && a.Eligible {
			if !a.Ready {
				if b := firstFailingLeg(GateView{Legs: a.Legs}, nil); b != "" {
					return b, nil
				}
				return "nt8_absent: not ready", nil
			}
			if s, st := ReadHoldFor(w.dataDir(), j.JobID); s != HoldOurs {
				return "the hold on disk is not this job's (" + holdSummary(s, st) + ")", nil
			}
			ev["drain_path"] = drainPathNT8Absent
			ev["link_down_since"] = a.LinkDownSince
			for _, l := range a.Legs {
				ev["leg_"+l.Name] = legEvidence(l)
			}
			return "", nil
		}
		// The normal path.
		b, m := w.heldView(ctx, j.JobID)
		if b != "" {
			return b, nil
		}
		ev["ack_received"], ev["ack_build_id"] = m.AddonAck.Received, orNA(m.AddonAck.BuildID)
		ev["drain_path"] = "normal"
		return firstFailingLeg(g, drainLegs), nil
	})
	res := stepResult{
		receipts: []Receipt{w.receipt("drain", start, ev, err)},
		err:      err,
		set: func(k *updaterjob.Job) {
			// The job file says which path the drain took, with the server's
			// disconnect stamp (READ, never a guess).
			if ev["drain_path"] == drainPathNT8Absent {
				k.DrainPath = drainPathNT8Absent
				k.LinkDownSince = ev["link_down_since"]
			}
		},
	}
	// F2: a live AI-plan read outlived the planner wait — a KNOWN BENIGN
	// blocker must not keep the desk held. Release THIS job's hold and refuse
	// (nothing was installed). If the release itself fails, the hold stays and
	// the failure is recovery_needed, never a refused job with a hold on disk.
	var pe plannerExpired
	if errors.As(err, &pe) {
		relStart := w.host.Now()
		if relErr := ReleaseJob(w.dataDir(), j.JobID); relErr != nil {
			return stepResult{receipts: res.receipts, err: err, failTo: updaterjob.StateRecoveryNeeded,
				reason: "AI plan still running and the hold could not be released: " + clipText(relErr.Error())}
		}
		rev := map[string]string{"reason": "planner wait expired", "hold_after": "released"}
		res.receipts = append(res.receipts, w.receipt("release_hold", relStart, rev, nil))
		res.failTo, res.reason = updaterjob.StateRefused, "AI plan still running — hold released, nothing installed"
	}
	return res
}

// legEvidence renders one leg's verdict for the receipt: its own detail text
// on both outcomes — the receipt carries each leg's EVIDENCE (UPDATER-NT8-CLOSED
// item 1: "The receipt records the path taken and each leg's evidence").
func legEvidence(l GateLeg) string {
	if l.Pass {
		return "PASS: " + l.Detail
	}
	return "FAIL: " + l.Detail
}

// absentGate returns "" when the nt8_absent verdict is ready for THIS job and
// the hold on disk is ours; else the blocker. The normal verdict is never
// consulted here: absent mode has its own legs.
func (w *Worker) absentGate(ctx context.Context, j updaterjob.Job) (string, GateView) {
	g, err := w.app.InstallationGate(ctx)
	if err != nil {
		return "installation-gate: " + err.Error(), g
	}
	if g.JobID != j.JobID {
		return fmt.Sprintf("installation-gate names job %q", g.JobID), g
	}
	a := g.NT8Absent
	if a == nil || !a.Eligible {
		return "nt8_absent: not eligible (the AddOn is connected, or the link has not been down long enough)", g
	}
	if !a.Ready {
		if b := firstFailingLeg(GateView{Legs: a.Legs}, nil); b != "" {
			return b, g
		}
		return "nt8_absent: not ready", g
	}
	if s, st := ReadHoldFor(w.dataDir(), j.JobID); s != HoldOurs {
		return "the hold on disk is not this job's (" + holdSummary(s, st) + ")", g
	}
	return "", g
}

// heldView reads /api/maintenance: our hold, held, and a fresh held ack for
// THIS job. It returns the blocker ("" = pass) and the view.
func (w *Worker) heldView(ctx context.Context, jobID string) (string, MaintenanceView) {
	m, err := w.app.Maintenance(ctx)
	if err != nil {
		return "maintenance: " + err.Error(), m
	}
	if !m.Held || m.State != "held" || m.JobID == nil || *m.JobID != jobID {
		job := "n/a"
		if m.JobID != nil {
			job = *m.JobID
		}
		return fmt.Sprintf("maintenance: hold is %s for job %s, not held for this job", m.State, job), m
	}
	if b := ackFor(m.AddonAck, jobID); b != "" {
		return b, m
	}
	return "", m
}

// ackFor is "" when a is a held, fresh maintenance_ack for jobID.
func ackFor(a *AckView, jobID string) string {
	switch {
	case a == nil:
		return "addon_ack: no AddOn maintenance_ack"
	case !a.Held:
		return "addon_ack: the AddOn ack is not held"
	case a.JobID != jobID:
		return fmt.Sprintf("addon_ack: the AddOn acks job %q, not this job", a.JobID)
	case a.AgeMs < 0 || a.AgeMs > ackMaxAgeMs:
		return fmt.Sprintf("addon_ack: the AddOn ack is %d ms old (max %d)", a.AgeMs, ackMaxAgeMs)
	}
	return ""
}

// stepGate: ready:true on two reads whose acks are DIFFERENT acks, both
// received after this step started (R-q). The comparison is on ONE clock
// (#206 review fold): each ack's ARRIVAL is rebuilt on the worker's
// monotonic clock as now − AgeMs (AgeMs is the app's monotonic age, the same
// box), so the same ack has a constant arrival across reads while a new ack
// (resent every 5 s) jumps it by 5 s. The rendered "received" strings were
// wall-clock: this box steps its clock (deploy/fix-wsl2-clock.sh,
// makestep 1 -1), and a ≥1 s forward step between two polls of the SAME ack
// moved its rendered time forward enough to pass the old "≥1 s apart and
// both ≥ start" test — C11 proven by a single ack. Sampling ages directly
// cannot work: fixed poll and resend periods make the age at each read a
// fixed phase, equal for every successive ack.
func (w *Worker) stepGate(ctx context.Context, j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	var firstArrival time.Time
	err := w.pollPlanner(ctx, j, w.cfg.Budgets.Gate, w.cfg.Budgets.PlannerWait, func() (string, error) {
		// UPDATER-NT8-CLOSED: with NT8 absent there is no ack to double-read.
		// The absent verdict is ready (every ledger leg + the hold on disk
		// ours); a reconnect flips eligible=false and the normal two-ack path
		// below applies again.
		if b, g := w.absentGate(ctx, j); b == "" {
			ev["gate_path"] = drainPathNT8Absent
			ev["link_down_since"] = g.NT8Absent.LinkDownSince
			return "", nil
		}
		g, err := w.app.InstallationGate(ctx)
		if err != nil {
			return "installation-gate: " + err.Error(), nil
		}
		if !g.Ready {
			if b := firstFailingLeg(g, nil); b != "" {
				return b, nil
			}
			return "installation-gate: not ready", nil
		}
		if g.JobID != j.JobID {
			return fmt.Sprintf("installation-gate names job %q", g.JobID), nil
		}
		b, m := w.heldView(ctx, j.JobID)
		if b != "" {
			return b, nil
		}
		age := m.AddonAck.AgeMs
		if age < 0 {
			return "addon_ack: negative age", nil
		}
		arrival := w.host.Now().Add(-time.Duration(age) * time.Millisecond)
		if arrival.Before(start) {
			return "gate: waiting for an ack received after the gate step started", nil
		}
		if firstArrival.IsZero() {
			firstArrival = arrival
			ev["ack_1_age_ms"] = strconv.FormatInt(age, 10)
			ev["ack_1_at"] = arrival.Format(time.RFC3339Nano)
			return "gate: ready once; waiting for a second, distinct ack", nil
		}
		if arrival.Sub(firstArrival) < ackDistinct {
			return "gate: ready once; waiting for a second, distinct ack", nil
		}
		ev["ack_2_age_ms"] = strconv.FormatInt(age, 10)
		ev["ack_2_at"] = arrival.Format(time.RFC3339Nano)
		return "", nil
	})
	res := stepResult{receipts: []Receipt{w.receipt("gate", start, ev, err)}, err: err}
	// F2: same planner-wait expiry as the drain — release THIS job's hold and
	// refuse; a benign blocker must not keep the desk held.
	var pe plannerExpired
	if errors.As(err, &pe) {
		relStart := w.host.Now()
		if relErr := ReleaseJob(w.dataDir(), j.JobID); relErr != nil {
			return stepResult{receipts: res.receipts, err: err, failTo: updaterjob.StateRecoveryNeeded,
				reason: "AI plan still running and the hold could not be released: " + clipText(relErr.Error())}
		}
		rev := map[string]string{"reason": "planner wait expired", "hold_after": "released"}
		res.receipts = append(res.receipts, w.receipt("release_hold", relStart, rev, nil))
		res.failTo, res.reason = updaterjob.StateRefused, "AI plan still running — hold released, nothing installed"
	}
	return res
}

// reprove is R-i: one more ready:true for THIS job, polled for the Reprove
// budget, before each step that changes something (backup, nt8, activate).
// It also re-reads the hold FILE itself (#206 review fold, hold.go:129): the
// gate only exposes held + job id, so an operator hold written over ours after
// the hold step (`maintenance-hold set --job <this job> --withdraw-entries`)
// looks identical to it — ReadHoldFor is the one reader that sees owner and
// withdraw_entries, and only HoldOurs may ride along.
func (w *Worker) reprove(ctx context.Context, j updaterjob.Job) error {
	return w.poll(ctx, j, w.cfg.Budgets.Reprove, func() (string, error) {
		// UPDATER-NT8-CLOSED: the absent verdict re-proves like any ready.
		if b, _ := w.absentGate(ctx, j); b == "" {
			return "", nil
		}
		g, err := w.app.InstallationGate(ctx)
		if err != nil {
			return "installation-gate: " + err.Error(), nil
		}
		if g.JobID != j.JobID {
			return fmt.Sprintf("installation-gate names job %q", g.JobID), nil
		}
		if !g.Ready {
			if b := firstFailingLeg(g, nil); b != "" {
				return b, nil
			}
			return "installation-gate: not ready", nil
		}
		if s, st := ReadHoldFor(w.dataDir(), j.JobID); s != HoldOurs {
			return "the hold on disk is not this job's (" + holdSummary(s, st) + ")", nil
		}
		return "", nil
	})
}

func (w *Worker) stepBackup(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	var receipts []Receipt
	err := func() error {
		if err := w.reprove(ctx, j); err != nil {
			return fmt.Errorf("ready not re-proven before backup: %w", err)
		}
		if j.Install == nil || j.Snapshot == nil || j.BackupPath == "" {
			return errors.New("no install, snapshot or backup path recorded")
		}
		brc, err := w.lib.Backup(w.cfg.Target.DBFile, j.BackupPath)
		receipts = append(receipts, brc)
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		vstart := w.host.Now()
		if verr := w.verifySnapshot(*j.Snapshot, *j.Install); verr == nil {
			receipts = append(receipts, w.receipt("snapshot_verify", vstart, map[string]string{"already": "true"}, nil))
			return nil
		}
		src, err := w.lib.Snapshot(*j.Install, j.Snapshot.Dir)
		receipts = append(receipts, src)
		if err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		vstart = w.host.Now()
		verr := w.verifySnapshot(*j.Snapshot, *j.Install)
		receipts = append(receipts, w.receipt("snapshot_verify", vstart, nil, verr))
		return verr
	}()
	if err != nil && len(receipts) == 0 {
		receipts = append(receipts, w.receipt("backup", start, nil, err))
	}
	return stepResult{receipts: receipts, err: err}
}

func (w *Worker) stepNT8(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	if d := j.NT8; d != nil {
		ev["decision"], ev["manifest_build_id"], ev["acked_build_id"] = d.Decision, orNA(d.ManifestBuildID), orNA(d.AckedBuildID)
		if d.Reason != "" {
			ev["reason"] = d.Reason
		}
		if d.CSUnchanged != nil {
			ev["cs_unchanged"] = strconv.FormatBool(*d.CSUnchanged)
		}
		if d.Absent {
			ev["absent"] = "true"
		}
		if d.F5Owed {
			ev["f5_owed"] = "true"
		}
	}
	err := w.reprove(ctx, j)
	if err != nil {
		err = fmt.Errorf("ready not re-proven at the AddOn step: %w", err)
	}
	res := stepResult{receipts: []Receipt{w.receipt("nt8", start, ev, err)}, err: err}
	if j.State == updaterjob.StateNT8Updated {
		why := "the AddOn must be updated"
		if j.NT8 != nil && j.NT8.Reason != "" {
			why = j.NT8.Reason
		}
		res.blocker = clipText("attended AddOn F5 required (" + why + "): compile the release's AddOn in NT8 (copy → F5 → full NT8 restart), then run: vl-updater resume " + j.JobID)
	}
	return res
}

// stepActivate is the point of no return. On a resume INSIDE it (attempts >
// 1) the identity on disk may be the process the first run already killed:
// ready is re-proven first (R-i — the resume never passes through advance's
// re-proof; verifier D2), then the CURRENT identity is re-read and persisted,
// with a new kill instant, BEFORE the re-run (C3: all three halves
// reinstalled, at most one extra restart). A re-proof that fails kills
// nothing: recovery_needed, the hold kept.
func (w *Worker) stepActivate(ctx context.Context, j updaterjob.Job) stepResult {
	if j.Release == nil || j.Install == nil || j.IdentityBefore == nil {
		return stepResult{err: errors.New("activate without its inputs")}
	}
	if j.Attempts > 1 {
		if err := w.reprove(ctx, j); err != nil {
			if errors.Is(err, errMoved) || ctx.Err() != nil {
				return stepResult{abort: err}
			}
			start := w.host.Now()
			why := "ready not re-proven before the resumed activate: " + err.Error()
			return stepResult{receipts: []Receipt{w.receipt("activate", start, map[string]string{"resumed": "true", "killed": "none"}, errors.New(why))},
				err: errors.New(why), failTo: updaterjob.StateRecoveryNeeded, reason: clipText(why)}
		}
		id, err := w.lib.CurrentIdentity()
		if err != nil {
			start := w.host.Now()
			return stepResult{receipts: []Receipt{w.receipt("activate", start, nil, err)}, err: fmt.Errorf("resume at activate: %w", err)}
		}
		k, err := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
			k.IdentityBefore = &id
			w.setBootWatch(k)
			k.UpdatedAt = w.now(*k)
			return nil
		})
		if err != nil {
			return stepResult{abort: err}
		}
		j = k
	}
	// R-i + the process re-proof (#206 review fold, runner.go:275): the kill
	// must hit a process that is STILL the one preflight proved.
	if err := w.proveCurrentBinary(ctx, j, *j.IdentityBefore); err != nil {
		start := w.host.Now()
		return stepResult{receipts: []Receipt{w.receipt("activate", start, map[string]string{"killed": "none"}, err)},
			err: err, failTo: updaterjob.StateRecoveryNeeded, reason: clipText("activate refused: " + err.Error())}
	}
	next, rc, err := w.lib.Activate(*j.Release, *j.Install, *j.IdentityBefore)
	res := stepResult{receipts: []Receipt{rc}, err: err}
	if err == nil {
		res.set = func(k *updaterjob.Job) {
			k.IdentityAfter = &next
			// the new process names its log by ITS boot date and ITS binary
			if p := w.predictedLog(w.host.Now(), j.Release.Binary); p != k.LogPath {
				zero := int64(0)
				k.LogPath, k.LogOffset = p, &zero
			}
		}
	}
	return res
}

func (w *Worker) stepWatch(j updaterjob.Job) stepResult {
	if j.Release == nil || j.IdentityAfter == nil || j.WatchSince == nil || j.LogPath == "" {
		return stepResult{err: errors.New("watch without its inputs")}
	}
	rc, err := w.lib.Watch(*j.Release, *j.IdentityAfter, WatchOpts{
		LogPath: j.LogPath, HealthURL: w.cfg.Target.HealthURL(), Since: *j.WatchSince, Within: w.cfg.Budgets.Watch,
	})
	return stepResult{receipts: []Receipt{rc}, err: err}
}

// stepBootVerify is what Watch GREEN does not prove (C13, OQ-4, OQ-6): the
// OK boot line (a REFUSED boot's rev token satisfies Watch), health as a
// prefix of the release, the AddOn acking the NEW process with the manifest's
// build, and the served UI being the release's.
func (w *Worker) stepBootVerify(ctx context.Context, j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	err := func() error {
		if j.Release == nil || j.LogOffset == nil || j.WatchSince == nil || j.IdentityAfter == nil {
			return errors.New("boot verify without its inputs")
		}
		sha := j.Release.SHA
		if err := verifyBootLine(j.LogPath, *j.LogOffset, sha, j.IdentityAfter.PID, ev); err != nil {
			return err
		}
		h, err := w.app.Health(ctx)
		if err != nil {
			return fmt.Errorf("health: %w", err)
		}
		ev["health_revision"] = h
		if !revisionsAgree(h, sha) {
			return fmt.Errorf("health serves %q, not a ≥7-char prefix of %s", h, sha)
		}
		f, err := w.facts(j)
		if err != nil {
			return err
		}
		// UPDATER-NT8-CLOSED: in the nt8_absent drain path the AddOn-ack wait
		// is SKIPPED — NT8 was closed at install time, so no ack can name the
		// new process. Recorded, never faked: no build-id comparison is
		// invented to stand in for the ack.
		if j.DrainPath == drainPathNT8Absent {
			ev["addon_ack_wait"] = "skipped (drain path nt8_absent)"
		} else {
			if !validBuildID(f.AddonBuildID) {
				return errors.New("the manifest names no addon build_id")
			}
			ev["manifest_build_id"] = f.AddonBuildID
			since := *j.WatchSince
			if err := w.poll(ctx, j, w.cfg.Budgets.PostBootAck, func() (string, error) {
				b, m := w.heldView(ctx, j.JobID)
				if b != "" {
					return b, nil
				}
				// One clock (#206 review fold): the ack was received at
				// now − AgeMs (monotonic on the app side, the same box), so
				// "received after the kill" is age ≤ now − since. The old
				// wall-clock compare of the rendered received string made a
				// backward clock step read the NEW process's fresh ack as
				// older than WatchSince — a good release rolled back.
				age := m.AddonAck.AgeMs
				if age < 0 || time.Duration(age)*time.Millisecond > w.host.Now().Sub(since) {
					return "addon_ack: waiting for the AddOn to ack the new process", nil
				}
				ev["acked_build_id"], ev["acked_age_ms"] = m.AddonAck.BuildID, strconv.FormatInt(age, 10)
				ev["acked_at"] = w.host.Now().Add(-time.Duration(age) * time.Millisecond).Format(time.RFC3339Nano)
				if m.AddonAck.BuildID != f.AddonBuildID {
					return fmt.Sprintf("addon_ack: the AddOn runs build %q, the release is %q", m.AddonAck.BuildID, f.AddonBuildID), nil
				}
				return "", nil
			}); err != nil {
				return err
			}
		}
		want := f.Artifacts["web/dist/index.html"]
		if want == "" {
			return errors.New("the signed manifest lists no web/dist/index.html")
		}
		body, err := w.app.Index(ctx)
		if err != nil {
			return fmt.Errorf("served UI: %w", err)
		}
		if got := sha256Of(body); got != want {
			return fmt.Errorf("the served UI hashes to %s, the release's index.html is %s", got, want)
		}
		ev["index_sha256"] = want
		return nil
	}()
	return stepResult{receipts: []Receipt{w.receipt("boot_verify", start, ev, err)}, err: err}
}

// stepRollback restores the job's snapshot of the install halves and proves
// the OLD sha is back: RollbackTo, then ALWAYS Watch on the snapshot's sha
// with since = the persisted rollback kill instant (a RollbackTo error after
// the files were restored may still boot the old build through
// Restart=on-failure), then the OK-line scan and health prefix for the old
// sha, then the DIST half: the served UI must hash to the snapshot's
// index.html (a RollbackTo that failed at restore dist leaves the failed
// release's bundle served). Anything unproven is recovery_needed: the worker
// stops, the hold stays.
func (w *Worker) stepRollback(ctx context.Context, j updaterjob.Job) stepResult {
	if j.Snapshot == nil || j.Install == nil || j.RollbackWatchSince == nil || j.RollbackLogOffset == nil {
		return stepResult{err: errors.New("rollback without its inputs (snapshot, install, rollback watch)"), reason: "rollback without its inputs"}
	}
	// #206 review fold (hold.go:129): the rollback kills and restores — never
	// under a hold the worker does not own (an operator overwrite mid-job).
	// Recovery_needed keeps the foreign hold on disk.
	if s, st := ReadHoldFor(w.dataDir(), j.JobID); s != HoldOurs {
		return stepResult{err: fmt.Errorf("the hold on disk is not this job's (%s)", holdSummary(s, st)), reason: "the hold on disk is not this job's"}
	}
	if j.Attempts > 1 || j.IdentityRollback == nil {
		id, ok := w.currentIdentityRetry(ctx)
		since := w.host.Now().Truncate(time.Second)
		bin := "nofx-bin" // R5 removes: the snapshot's own binary
		if j.Snapshot != nil {
			bin = j.Snapshot.Binary
		}
		path := w.predictedLog(since, bin)
		off := fileSize(path)
		k, err := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
			if ok {
				k.IdentityRollback = &id
			}
			k.RollbackLogPath, k.RollbackLogOffset, k.RollbackWatchSince = path, &off, &since
			k.UpdatedAt = w.now(*k)
			return nil
		})
		if err != nil {
			return stepResult{abort: err}
		}
		j = k
	}
	var kill Identity
	if j.IdentityRollback != nil {
		kill = *j.IdentityRollback
	}
	// The rollback's kill re-proves the process too (#206 review fold,
	// runner.go:275) — a hotfix during the rollback window is the same gap.
	if err := w.proveCurrentBinary(ctx, j, kill); err != nil {
		return stepResult{err: err, reason: clipText("rollback refused: " + err.Error())}
	}
	snap, inst := *j.Snapshot, *j.Install
	next, rrc, rerr := w.lib.RollbackTo(snap, inst, kill)
	receipts := []Receipt{rrc}
	watchID := next
	if rerr != nil || watchID.PID <= 0 {
		watchID, _ = w.currentIdentityRetry(ctx)
	}
	// The boot log is re-resolved AFTER the kill (#206 review fold,
	// runner.go:367): the path was predicted from the pre-kill instant, and a
	// restart that crosses local midnight makes the bot log to vl_<D+1>.log
	// — watching and scanning only the D file turned a successful rollback
	// into recovery_needed. A new file is scanned whole (offset 0): it holds
	// no earlier boot of today, so a stale OK line cannot satisfy it. The
	// activate path re-predicts the same way (stepActivate).
	watchLogPath, watchLogOff := j.RollbackLogPath, *j.RollbackLogOffset
	if p := w.predictedLog(w.host.Now(), j.Snapshot.Binary); p != watchLogPath {
		zero := int64(0)
		watchLogPath, watchLogOff = p, zero
	}
	wrc, werr := w.lib.Watch(snap, watchID, WatchOpts{
		LogPath: watchLogPath, HealthURL: w.cfg.Target.HealthURL(), Since: *j.RollbackWatchSince, Within: w.cfg.Budgets.Watch,
	})
	receipts = append(receipts, wrc)
	if werr != nil {
		why := "the old build " + snap.SHA + " is not proven running after the rollback: " + werr.Error()
		if rerr != nil {
			why = "rollback failed (" + rerr.Error() + ") and " + why
		}
		return stepResult{receipts: receipts, err: errors.New(clipText(why)), reason: clipText(why)}
	}
	vstart, ev := w.host.Now(), map[string]string{}
	verr := verifyBootLine(watchLogPath, watchLogOff, snap.SHA, watchID.PID, ev)
	if verr == nil {
		h, err := w.app.Health(ctx)
		switch {
		case err != nil:
			verr = fmt.Errorf("health: %w", err)
		case !revisionsAgree(h, snap.SHA):
			verr = fmt.Errorf("health serves %q, not the old %s", h, snap.SHA)
		default:
			ev["health_revision"] = h
		}
	}
	if verr == nil {
		// The DIST half (#206 review fold): RollbackTo that failed at
		// "restore dist" left the failed release's bundle served while the
		// binary and RELEASE halves above are proven fine — the job used to
		// end rolled_back and clear the hold on a mixed install. The served
		// UI must hash to the SNAPSHOT's index.html.
		expIndex, derr := os.ReadFile(filepath.Join(snap.Dist, "index.html"))
		switch {
		case derr != nil:
			verr = fmt.Errorf("the snapshot's dist: %w", derr)
		default:
			got, gerr := w.app.Index(ctx)
			switch {
			case gerr != nil:
				verr = fmt.Errorf("served UI: %w", gerr)
			case sha256Of(got) != sha256Of(expIndex):
				verr = fmt.Errorf("the served UI hashes to %s, the snapshot's index.html is %s", sha256Of(got), sha256Of(expIndex))
			default:
				ev["index_sha256"] = sha256Of(expIndex)
			}
		}
	}
	if rerr != nil {
		ev["rollback_error"] = clipText(rerr.Error())
	}
	receipts = append(receipts, w.receipt("boot_verify", vstart, ev, verr))
	if verr != nil {
		return stepResult{receipts: receipts, err: verr, reason: clipText("the old build is not proven after the rollback: " + verr.Error())}
	}
	return stepResult{receipts: receipts}
}

// stepReleaseHold clears THIS job's hold (complete, rolled_back — the only
// two states that clear). A clear that keeps failing is recovery_needed.
func (w *Worker) stepReleaseHold(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	deadline := start.Add(w.cfg.Budgets.HoldClear)
	var err error
	for {
		if err = ReleaseJob(w.dataDir(), j.JobID); err == nil {
			break
		}
		if !w.host.Now().Before(deadline) || w.host.Sleep(ctx, w.cfg.Budgets.Poll) != nil {
			break
		}
	}
	ev := map[string]string{}
	if err == nil {
		s, _ := ReadHoldFor(w.dataDir(), j.JobID)
		ev["hold_after"] = s.String()
	}
	receipts := []Receipt{w.receipt("release_hold", start, ev, err)}
	if err == nil {
		// WORKER-TAKES-THE-LOCK: complete/rolled_back also release OUR
		// main-tree lock. A failed release keeps the job recoverable (the
		// lock stays; the failure edge names it).
		lrec := w.lockReleaseReceipt(lockSessionFor(j.JobID), w.host.Now())
		receipts = append(receipts, lrec)
		if !lrec.OK {
			return stepResult{receipts: receipts, err: errors.New(lrec.Err), reason: clipText("the main-tree lock could not be released: " + lrec.Err)}
		}
	}
	return stepResult{receipts: receipts, err: err}
}

// plannerExpired is the F2 expiry error: the step waited the full planner
// budget while an AI plan read stayed in flight. Preflight refuses (no hold,
// as today); drain/gate RELEASE the hold and refuse — a known benign blocker
// must not leave the desk held.
type plannerExpired struct{ budget time.Duration }

func (e plannerExpired) Error() string {
	return fmt.Sprintf("AI plan still running after %s", e.budget)
}

// plannerBlocker reports whether the blocker is the planner_in_flight leg (the
// gate names it as "planner_in_flight: <detail>").
func plannerBlocker(b string) bool { return strings.HasPrefix(b, "planner_in_flight") }

// poll re-reads until check passes ("" blocker) or the budget runs out. Each
// NEW blocker is persisted (M5 shows it). A fatal error ends it at once.
func (w *Worker) poll(ctx context.Context, j updaterjob.Job, budget time.Duration, check func() (string, error)) error {
	return w.pollPlanner(ctx, j, budget, 0, check)
}

// pollPlanner is poll plus the F2 planner wait: while the CURRENT blocker is a
// live AI-plan read, the step waits up to plannerBudget (a benign blocker must
// not fail the step on its normal budget) and the live blocker — "waiting for
// the AI plan (started hh:mm:ss)" — is what the job shows. On planner-wait
// expiry it returns plannerExpired; every other blocker keeps the normal
// budget and the normal "not passed within" failure.
func (w *Worker) pollPlanner(ctx context.Context, j updaterjob.Job, budget, plannerBudget time.Duration, check func() (string, error)) error {
	start := w.host.Now()
	deadline := start.Add(budget)
	plannerDeadline := start.Add(plannerBudget)
	last := ""
	for {
		b, fatal := check()
		if fatal != nil {
			return fatal
		}
		if b == "" {
			return nil
		}
		if b != last {
			last = b
			if _, err := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
				k.Blocker = clipText(b)
				k.UpdatedAt = w.now(*k)
				return nil
			}); err != nil {
				return err
			}
		}
		if plannerBlocker(b) && plannerBudget > 0 {
			if !w.host.Now().Before(plannerDeadline) {
				return plannerExpired{plannerBudget}
			}
		} else if !w.host.Now().Before(deadline) {
			return fmt.Errorf("not passed within %s: %s", budget, b)
		}
		if err := w.host.Sleep(ctx, w.cfg.Budgets.Poll); err != nil {
			return err
		}
	}
}

// firstFailingLeg is "" when every leg named in want (and every
// trader_cutover:* leg) is present and passes; with want nil, every leg.
func firstFailingLeg(g GateView, want []string) string {
	if b := firstFailingNamedLegs(g, want); b != "" {
		return b
	}
	for _, l := range g.Legs {
		if strings.HasPrefix(l.Name, "trader_cutover:") && !l.Pass {
			return l.Name + ": " + l.Detail
		}
	}
	return ""
}

// firstFailingNamedLegs is the named-lookup half of firstFailingLeg (the
// trader_cutover sweep lives in firstFailingLeg). The nt8_absent preflight
// uses this directly: with NT8 closed the cutover legs cannot answer and must
// not be demanded (P-D ruling item 3).
func firstFailingNamedLegs(g GateView, want []string) string {
	legs := map[string]GateLeg{}
	for _, l := range g.Legs {
		legs[l.Name] = l
	}
	if want == nil {
		for _, l := range g.Legs {
			if !l.Pass {
				return l.Name + ": " + l.Detail
			}
		}
		if len(g.Legs) == 0 {
			return "installation-gate: no legs"
		}
		return ""
	}
	for _, n := range want {
		l, ok := legs[n]
		if !ok {
			return n + ": leg missing from the installation gate"
		}
		if !l.Pass {
			return n + ": " + l.Detail
		}
	}
	return ""
}

// absentPreflightLegs is the preflight leg set when the gate's nt8_absent
// verdict is eligible (the AddOn's socket has been down ≥ the window): the
// absent legs minus hold/go_drained, which only engage at drain (the hold and
// the barrier do not exist before the hold step). P-D ruling item 3 — preflight
// must use the absent leg set when the wire has been down long enough, or an
// NT8-closed install is refused on legs the AddOn can never answer.
func absentPreflightLegs(absent *NT8AbsentView) (names []string, ok bool) {
	for _, l := range absent.Legs {
		if l.Name == "hold" || l.Name == "go_drained" {
			continue
		}
		names = append(names, l.Name)
	}
	return names, len(names) > 0
}

// facts re-proves the release (verdict + signature NOW) for a step that reads
// the signed manifest (the nt8 rule, the resume, the boot check), bound to
// the job's source sha.
func (w *Worker) facts(j updaterjob.Job) (ReleaseFacts, error) {
	v, err := w.rel.Verdict(j.ReleaseID)
	if err != nil {
		return ReleaseFacts{}, err
	}
	f, err := w.rel.Reverify(v)
	if err != nil {
		return ReleaseFacts{}, err
	}
	if j.SourceSHA == "" || f.SourceSHA != j.SourceSHA || f.ReleaseID != j.ReleaseID {
		return ReleaseFacts{}, fmt.Errorf("the re-verified release (%s, %s) is not the job's (%s, %s)", f.ReleaseID, f.SourceSHA, j.ReleaseID, orNA(j.SourceSHA))
	}
	return f, nil
}

// validBuildID: a build id the nt8 rule may compare (never "" or n/a).
func validBuildID(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !strings.EqualFold(s, "n/a")
}

// calendarVerdict compares the release's calendar_static_t1.json with the
// install's by content. The worker never writes either (template-only).
func calendarVerdict(releaseDir, installDir string) (string, error) {
	rs, rerr := sha256File(filepath.Join(releaseDir, calendarFile))
	if errors.Is(rerr, os.ErrNotExist) {
		return "the release carries none", nil
	}
	if rerr != nil {
		return "", fmt.Errorf("the release's %s: %w", calendarFile, rerr)
	}
	is, ierr := sha256File(filepath.Join(installDir, calendarFile))
	if ierr != nil {
		return "", fmt.Errorf("the install's %s cannot be compared with the release's (%v) — copy the template attended, then re-run", calendarFile, ierr)
	}
	if rs != is {
		return "", fmt.Errorf("%s differs between the release (%s) and the install (%s): the worker never overwrites it (owner data) — reconcile it attended, then re-run", calendarFile, rs[:12], is[:12])
	}
	return "equal", nil
}

// sameBinary: /proc/<pid>/exe names the install's binary (directly or through
// a symlink). " (deleted)" never matches: a replaced file is not the install.
func sameBinary(exe, binary string) bool {
	if exe == "" {
		return false
	}
	if filepath.Clean(exe) == filepath.Clean(binary) {
		return true
	}
	real, err := filepath.EvalSymlinks(binary)
	return err == nil && filepath.Clean(exe) == real
}

func sha256Of(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// sha256File hashes a regular file's CONTENT (never its name or date).
func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
