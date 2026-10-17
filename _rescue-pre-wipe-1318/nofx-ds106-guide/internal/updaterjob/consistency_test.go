package updaterjob

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// refusedAtBothCallSites: a crafted job is refused by the production writer
// (Write) AND, planted on disk in the writer's exact byte form (so only the
// content rules can refuse it), by the production reader (Read).
func refusedAtBothCallSites(t *testing.T, name string, j Job) {
	t.Helper()
	if err := Write(t.TempDir(), j); !errors.Is(err, ErrCorrupt) {
		t.Errorf("%s: Write = %v, want ErrCorrupt", name, err)
	}
	dd := t.TempDir()
	seed := mustNew(t, j.JobID, j.ReleaseID, j.CreatedAt)
	mustWrite(t, dd, seed)
	p, _ := Path(dd, j.JobID)
	b, err := encode(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(dd, j.JobID); !errors.Is(err, ErrCorrupt) {
		t.Errorf("%s: Read = %s/%s, %v — want ErrCorrupt", name, got.State, got.Phase, err)
	}
}

// crafted builds a job directly (no Enter/Finish), in the shape Read returns.
func crafted(state State, phase Phase, attempts int, tr []Transition, rc []Receipt) Job {
	last := tr[len(tr)-1].At
	return Job{Schema: SchemaVersion, JobID: "job-0100", ReleaseID: "v1.2.0", State: state, Phase: phase, Attempts: attempts,
		CreatedAt: t0, UpdatedAt: last, Transitions: tr, Receipts: rc}
}

// TestDoneStepCarriesItsReceipt (U1 verifier defect 2, probe H3): a step with
// a side effect becomes DONE only with a receipt appended since its STARTED
// transition. Each transition RECORDS the receipt count it was written with
// (canon 35: counters record, never infer), so the rule survives a Read:
//
//   - Finish (the runner's call) refuses when no receipt was added since the
//     state started — a receipt from an earlier step (the park's resume
//     receipt) does not count — and leaves the job unchanged;
//   - Validate (every Write and every Read) refuses a history whose DONE
//     transition records no more receipts than its STARTED one, whose
//     counts run backwards (two finished steps sharing one receipt), or
//     whose counts claim receipts the file does not hold.
func TestDoneStepCarriesItsReceipt(t *testing.T) {
	restoreSeams(t)
	// H3 at the production call site: Enter, Write, Finish with no receipt.
	dd := t.TempDir()
	now := t0
	j := mustNew(t, "job-0100", "v1.2.0", now)
	mustWrite(t, dd, j)
	now = now.Add(time.Second)
	if err := j.Enter(StateDownloaded, now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	before := len(j.Transitions)
	if err := j.Finish(now.Add(time.Second)); !errors.Is(err, ErrNoReceipt) {
		t.Errorf("Finish(downloaded/started, no receipt) = %v, want ErrNoReceipt", err)
	}
	if j.Phase != PhaseStarted || len(j.Transitions) != before {
		t.Errorf("a refused Finish changed the job: %s, %d transitions", j.Phase, len(j.Transitions))
	}
	// A receipt from an EARLIER step does not finish this one: the park's
	// resume receipt is on file before activated starts.
	p := t.TempDir()
	pn := t0
	pj := mustNew(t, "job-0101", "v1.2.0", pn)
	mustWrite(t, p, pj)
	for _, s := range []State{StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone} {
		step(t, p, &pj, &pn, s)
	}
	pj.NT8 = &NT8Decision{Decision: NT8Updated, Reason: "ninjascript/*.cs changed"}
	step(t, p, &pj, &pn, StateNT8Updated)
	pn = pn.Add(time.Minute)
	resumed := pn
	pj.ResumedAt = &resumed
	if err := pj.AddReceipt(Receipt{Step: "resume", StartedAt: pn, EndedAt: pn, OK: true}, pn); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, pj)
	withRollbackInputs(&pj)
	pn = pn.Add(time.Second)
	if err := pj.Enter(StateActivated, pn); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, pj)
	if err := pj.Finish(pn.Add(time.Second)); !errors.Is(err, ErrNoReceipt) {
		t.Errorf("Finish(activated/started) on the resume receipt alone = %v, want ErrNoReceipt", err)
	}
	// positive control: the activate receipt finishes it, and the file reads.
	if err := pj.AddReceipt(Receipt{Step: "activate", StartedAt: pn, EndedAt: pn.Add(time.Second), OK: true}, pn.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := pj.Finish(pn.Add(time.Second)); err != nil {
		t.Fatalf("Finish with its receipt: %v", err)
	}
	mustWrite(t, p, pj)
	if got, err := Read(p, pj.JobID); err != nil || got.State != StateActivated || got.Phase != PhaseDone {
		t.Fatalf("positive control: %s/%s %v", got.State, got.Phase, err)
	}

	// Crafted histories (Validate at Write and at Read).
	r := func(step string) Receipt { return Receipt{Step: step, StartedAt: t0, EndedAt: t0, OK: true} }
	req := Transition{State: StateRequested, Phase: PhaseDone, At: t0}
	at := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }
	refusedAtBothCallSites(t, "H3: downloaded done with no receipt since it started", crafted(StateDownloaded, PhaseDone, 1,
		[]Transition{req, {State: StateDownloaded, Phase: PhaseStarted, At: at(1)}, {State: StateDownloaded, Phase: PhaseDone, At: at(2)}},
		[]Receipt{}))
	refusedAtBothCallSites(t, "a done count claiming a receipt the file does not hold", crafted(StateDownloaded, PhaseDone, 1,
		[]Transition{req, {State: StateDownloaded, Phase: PhaseStarted, At: at(1)}, {State: StateDownloaded, Phase: PhaseDone, At: at(2), Receipts: 1}},
		[]Receipt{}))
	refusedAtBothCallSites(t, "two finished steps sharing one receipt (counts run backwards)", crafted(StateVerified, PhaseDone, 1,
		[]Transition{req, {State: StateDownloaded, Phase: PhaseStarted, At: at(1)}, {State: StateDownloaded, Phase: PhaseDone, At: at(2), Receipts: 1},
			{State: StateVerified, Phase: PhaseStarted, At: at(3)}, {State: StateVerified, Phase: PhaseDone, At: at(4), Receipts: 1}},
		[]Receipt{r("download")}))
	// control: the same shapes with honest counts read back.
	ok := crafted(StateVerified, PhaseDone, 1,
		[]Transition{req, {State: StateDownloaded, Phase: PhaseStarted, At: at(1)}, {State: StateDownloaded, Phase: PhaseDone, At: at(2), Receipts: 1},
			{State: StateVerified, Phase: PhaseStarted, At: at(3), Receipts: 1}, {State: StateVerified, Phase: PhaseDone, At: at(4), Receipts: 2}},
		[]Receipt{r("download"), r("verify")})
	if err := ok.Validate(); err != nil {
		t.Fatalf("control: honest counts refused: %v", err)
	}
}

// withRollbackInputs sets what activated requires (the rollback inputs) with
// SHAs that agree: release = source_sha, install = snapshot = the old build.
func withRollbackInputs(j *Job) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	old := "89abcdef0123456789abcdef0123456789abcdef"
	j.SourceSHA = sha
	j.Release = &Release{Dir: "/r/" + sha, SHA: sha, Binary: "/r/" + sha + "/nofx-bin", Dist: "/r/" + sha + "/web/dist", ReleaseFile: "/r/" + sha + "/RELEASE", ManifestPath: "/r/" + sha + "/manifest.json"}
	j.Install = &Release{Dir: "/i", SHA: old, Binary: "/i/nofx-bin", Dist: "/i/web/dist", ReleaseFile: "/i/deploy/RELEASE"}
	j.Snapshot = &Release{Dir: "/b/install", SHA: old, Binary: "/b/install/nofx-bin", Dist: "/b/install/web/dist", ReleaseFile: "/b/install/deploy/RELEASE"}
	j.BackupPath = "/b/data.db"
	j.IdentityBefore = &Identity{PID: 172, StartTicks: 23987}
}

// walkTo steps a fresh job through states with the production API (step =
// Enter, Write, receipt, Finish, Write), setting the AddOn decision each nt8
// state requires.
func walkTo(t *testing.T, dd, jobID string, states ...State) (Job, time.Time) {
	t.Helper()
	now := t0
	j := mustNew(t, jobID, "v1.2.0", now)
	mustWrite(t, dd, j)
	for _, s := range states {
		switch s {
		case StateNT8Skipped:
			j.NT8 = &NT8Decision{Decision: NT8Skipped}
		case StateNT8Updated:
			j.NT8 = &NT8Decision{Decision: NT8Updated, Reason: "ninjascript/*.cs changed"}
		}
		step(t, dd, &j, &now, s)
	}
	return j, now
}

var toPark = []State{StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone, StateNT8Updated}

// TestLeavingTheParkNeedsAnAttendedResume (U1 verifier defect 3, probes H20
// and H18): the job leaves the nt8_updated park for activated only with a
// resumed_at recorded AFTER the park was done and no later than the move
// out of it (the attended `vl-updater resume` is the only way out); and a
// resumed_at on a job that never parked is refused.
func TestLeavingTheParkNeedsAnAttendedResume(t *testing.T) {
	restoreSeams(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	leave := func(t *testing.T, resumed *time.Time) (Job, string, []byte) {
		t.Helper()
		dd := t.TempDir()
		j, now := walkTo(t, dd, "job-0110", toPark...)
		withRollbackInputs(&j)
		p, _ := Path(dd, j.JobID)
		before, _ := os.ReadFile(p)
		j.ResumedAt = resumed
		if err := j.Enter(StateActivated, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return j, dd, before
	}
	parkDone := t0.Add(16 * time.Second) // walkTo: 8 steps × 2 s
	for name, resumed := range map[string]*time.Time{
		"H20: no resumed_at":             nil,
		"resumed_at at the park instant": ptr(parkDone),
		"resumed_at before the park":     ptr(parkDone.Add(-time.Second)),
		"resumed_at after it left":       ptr(parkDone.Add(2 * time.Minute)),
	} {
		t.Run(name, func(t *testing.T) {
			j, dd, before := leave(t, resumed)
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(nt8_updated → activated, %s) = %v, want ErrCorrupt", name, err)
			}
			p, _ := Path(dd, j.JobID)
			if b, _ := os.ReadFile(p); string(b) != string(before) {
				t.Error("a refused leave changed the file")
			}
			refusedAtBothCallSites(t, name, j)
		})
	}
	// H18: a resume time on a job that never parked — mid-flight, and on the
	// nt8_skipped path.
	for name, states := range map[string][]State{
		"H18: downloaded":      {StateDownloaded},
		"nt8_skipped, no park": {StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone, StateNT8Skipped},
		"nt8_updated not done": {StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone},
	} {
		t.Run(name, func(t *testing.T) {
			dd := t.TempDir()
			j, now := walkTo(t, dd, "job-0111", states...)
			if name == "nt8_updated not done" {
				j.NT8 = &NT8Decision{Decision: NT8Updated}
				if err := j.Enter(StateNT8Updated, now.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, dd, j)
			}
			j.ResumedAt = ptr(now.Add(time.Minute))
			j.UpdatedAt = *j.ResumedAt
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(resumed_at, %s) = %v, want ErrCorrupt", name, err)
			}
			refusedAtBothCallSites(t, name, j)
		})
	}
	// positive control: resumed inside the park window, then activated.
	j, dd, _ := leave(t, ptr(parkDone.Add(30*time.Second)))
	mustWrite(t, dd, j)
	if got, err := Read(dd, j.JobID); err != nil || got.State != StateActivated || got.ResumedAt == nil || got.Release.SHA != sha {
		t.Fatalf("positive control: %+v %v", got, err)
	}
	// and the park itself may carry the resume before the runner moves
	k := t.TempDir()
	pj, now := walkTo(t, k, "job-0112", toPark...)
	pj.ResumedAt = ptr(now.Add(time.Second))
	pj.UpdatedAt = *pj.ResumedAt
	mustWrite(t, k, pj)
}

func ptr[T any](v T) *T { return &v }

// TestNT8StateCarriesItsOwnDecision (U1 verifier defect 4, probe H2): a
// history that enters nt8_skipped carries nt8.decision "skipped", one that
// enters nt8_updated carries "updated" — present, and never the other one,
// when entered and at every later write.
func TestNT8StateCarriesItsOwnDecision(t *testing.T) {
	restoreSeams(t)
	toBackup := []State{StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone}
	for _, c := range []struct {
		name string
		to   State
		dec  *NT8Decision
	}{
		{"H2: nt8_skipped with decision updated", StateNT8Skipped, &NT8Decision{Decision: NT8Updated, Reason: "C# changed"}},
		{"nt8_updated with decision skipped", StateNT8Updated, &NT8Decision{Decision: NT8Skipped}},
		{"nt8_skipped with no decision", StateNT8Skipped, nil},
		{"nt8_updated with no decision", StateNT8Updated, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dd := t.TempDir()
			j, now := walkTo(t, dd, "job-0120", toBackup...)
			j.NT8 = c.dec
			if err := j.Enter(c.to, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(%s) = %v, want ErrCorrupt", c.name, err)
			}
			if got, err := Read(dd, j.JobID); err != nil || got.State != StateBackupDone {
				t.Errorf("a refused decision changed the file: %s %v", got.State, err)
			}
			refusedAtBothCallSites(t, c.name, j)
		})
	}
	// the decision cannot flip after the state was entered
	for _, c := range []struct {
		to   State
		flip string
	}{{StateNT8Skipped, NT8Updated}, {StateNT8Updated, NT8Skipped}} {
		dd := t.TempDir()
		j, _ := walkTo(t, dd, "job-0121", append(append([]State(nil), toBackup...), c.to)...)
		j.NT8 = &NT8Decision{Decision: c.flip}
		if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s/done with the decision flipped to %q: Write = %v, want ErrCorrupt", c.to, c.flip, err)
		}
	}
	// positive controls: both branches, with their own decision, write and read
	for _, to := range []State{StateNT8Skipped, StateNT8Updated} {
		dd := t.TempDir()
		j, _ := walkTo(t, dd, "job-0122", append(append([]State(nil), toBackup...), to)...)
		if got, err := Read(dd, j.JobID); err != nil || got.State != to || got.NT8 == nil {
			t.Fatalf("positive control %s: %+v %v", to, got, err)
		}
	}
}

// TestLastGoodReceiptIsAnOKReceipt (U1 verifier defect 5, probe H15): the
// receipt recovery_needed names as the last good one is a receipt whose ok
// is true — the operator restores from it.
func TestLastGoodReceiptIsAnOKReceipt(t *testing.T) {
	restoreSeams(t)
	build := func(t *testing.T, good int) (Job, string) {
		t.Helper()
		dd := t.TempDir()
		j, now := walkTo(t, dd, "job-0130", StateDownloaded)
		now = now.Add(time.Second)
		if err := j.Enter(StateVerified, now); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, dd, j)
		if err := j.AddReceipt(Receipt{Step: "verify", StartedAt: now, EndedAt: now, OK: false, Err: "signature refused"}, now); err != nil {
			t.Fatal(err)
		}
		j.LastGoodReceipt = &good
		j.RecoveryReason = "attended"
		if err := j.Enter(StateRecoveryNeeded, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		return j, dd
	}
	// receipts: 0 = download (ok), 1 = verify (failed)
	j, dd := build(t, 1)
	if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
		t.Errorf("H15: last_good_receipt → the failed receipt: Write = %v, want ErrCorrupt", err)
	}
	refusedAtBothCallSites(t, "H15: last_good_receipt → an ok:false receipt", j)
	// positive control: the OK one
	j, dd = build(t, 0)
	mustWrite(t, dd, j)
	if got, err := Read(dd, j.JobID); err != nil || got.LastGoodReceipt == nil || *got.LastGoodReceipt != 0 {
		t.Fatalf("positive control: %+v %v", got, err)
	}
}

// TestReleaseInstallAndSnapshotSHAsAgree (U1 verifier defect 6, probes H4 and
// H21): a present release is the source_sha; the snapshot is a copy of the
// install, so their shas are equal; and neither is the release being
// installed (installing the running build is refused upstream, and a
// snapshot of the NEW build restores nothing).
func TestReleaseInstallAndSnapshotSHAsAgree(t *testing.T) {
	restoreSeams(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	old := "89abcdef0123456789abcdef0123456789abcdef"
	other := "fedcba9876543210fedcba9876543210fedcba98"
	rel := func(s string) *Release {
		return &Release{Dir: "/r/" + s, SHA: s, Binary: "/r/" + s + "/nofx-bin", Dist: "/r/" + s + "/web/dist", ReleaseFile: "/r/" + s + "/RELEASE", ManifestPath: "/r/" + s + "/manifest.json"}
	}
	inst := func(dir, s string) *Release {
		return &Release{Dir: dir, SHA: s, Binary: dir + "/nofx-bin", Dist: dir + "/web/dist", ReleaseFile: dir + "/deploy/RELEASE"}
	}
	for _, c := range []struct {
		name string
		set  func(j *Job)
	}{
		{"H4: source_sha differs from release.sha", func(j *Job) { j.SourceSHA, j.Release = old, rel(sha) }},
		{"release present, source_sha absent", func(j *Job) { j.Release = rel(sha) }},
		{"H21: install.sha is the release sha", func(j *Job) { j.SourceSHA, j.Release, j.Install = sha, rel(sha), inst("/i", sha) }},
		{"install.sha is source_sha, release not yet resolved", func(j *Job) { j.SourceSHA, j.Install = sha, inst("/i", sha) }},
		{"snapshot.sha differs from install.sha", func(j *Job) {
			j.SourceSHA, j.Release, j.Install, j.Snapshot = sha, rel(sha), inst("/i", old), inst("/b/install", other)
		}},
		{"snapshot.sha is the release sha (no install)", func(j *Job) { j.SourceSHA, j.Release, j.Snapshot = sha, rel(sha), inst("/b/install", sha) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			dd := t.TempDir()
			j, _ := walkTo(t, dd, "job-0140", StateDownloaded, StateVerified)
			c.set(&j)
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(%s) = %v, want ErrCorrupt", c.name, err)
			}
			refusedAtBothCallSites(t, c.name, j)
		})
	}
	// positive control: release = source_sha, install = snapshot = the old build
	dd := t.TempDir()
	j, _ := walkTo(t, dd, "job-0141", StateDownloaded, StateVerified)
	j.SourceSHA, j.Release, j.Install, j.Snapshot = sha, rel(sha), inst("/i", old), inst("/b/install", old)
	mustWrite(t, dd, j)
	if got, err := Read(dd, j.JobID); err != nil || got.Snapshot == nil {
		t.Fatalf("positive control: %+v %v", got, err)
	}
}

// TestHistoryTimesNeverRunBackwards (U1 verifier defect 7, probes H1 and
// H5): every transition is at or after the one before it, and updated_at is
// at or after the last transition — hence at or after created_at, which is
// the first transition's time. Equal instants are allowed.
func TestHistoryTimesNeverRunBackwards(t *testing.T) {
	restoreSeams(t)
	// H1: a transition before the previous one, through Enter and through Finish
	for name, back := range map[string]func(j *Job, now time.Time) error{
		"H1: entered before the last transition": func(j *Job, now time.Time) error { return j.Enter(StateVerified, now.Add(-time.Hour)) },
		"finished before it started": func(j *Job, now time.Time) error {
			if err := j.Enter(StateVerified, now.Add(time.Second)); err != nil {
				return err
			}
			if err := j.AddReceipt(Receipt{Step: "verify", StartedAt: now, EndedAt: now, OK: true}, now.Add(time.Second)); err != nil {
				return err
			}
			return j.Finish(now.Add(-time.Second))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dd := t.TempDir()
			j, now := walkTo(t, dd, "job-0150", StateDownloaded)
			if err := back(&j, now); err != nil {
				t.Fatal(err)
			}
			j.UpdatedAt = now.Add(time.Hour) // not the rule under test
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(%s) = %v, want ErrCorrupt", name, err)
			}
			refusedAtBothCallSites(t, name, j)
		})
	}
	// H5 and its neighbour: updated_at before created_at, and before the
	// last transition (but after created_at)
	for name, at := range map[string]time.Duration{
		"H5: updated_at before created_at":      -time.Hour,
		"updated_at before the last transition": time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			dd := t.TempDir()
			j, _ := walkTo(t, dd, "job-0151", StateDownloaded, StateVerified)
			j.UpdatedAt = t0.Add(at)
			if err := Write(dd, j); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Write(%s) = %v, want ErrCorrupt", name, err)
			}
			refusedAtBothCallSites(t, name, j)
		})
	}
	// positive control: equal instants throughout
	dd := t.TempDir()
	j := mustNew(t, "job-0152", "v1.2.0", t0)
	mustWrite(t, dd, j)
	if err := j.Enter(StateDownloaded, t0); err != nil {
		t.Fatal(err)
	}
	if err := j.AddReceipt(Receipt{Step: "download", StartedAt: t0, EndedAt: t0, OK: true}, t0); err != nil {
		t.Fatal(err)
	}
	if err := j.Finish(t0); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
}

// TestFirstWriteMakesEveryNewDirDurable (U1 verifier defect 8, probe H6): a
// directory entry is durable only once its PARENT is fsynced, so every
// directory Write's Mkdir actually creates (<data>/updater, then its jobs
// dir) is followed by an fsync of its parent, before the job file is
// written; a dir that already existed is not re-synced; and when that
// parent fsync fails the write fails, the job file is not written, and the
// just-made empty dir is removed so the next write makes it — and syncs it —
// again.
func TestFirstWriteMakesEveryNewDirDurable(t *testing.T) {
	restoreSeams(t)
	var synced []string
	failOn := ""
	dd := t.TempDir()
	rel := func(p string) string {
		r, _ := filepath.Rel(dd, p)
		return r
	}
	fsyncDir = func(dir string) error {
		synced = append(synced, rel(dir))
		if rel(dir) == failOn {
			return errCrash
		}
		return realFsyncDir(dir)
	}
	jobsDir := filepath.Join("updater", "jobs")
	// the parent fsync fails: nothing half-made survives
	failOn = "."
	if err := Write(dd, mustNew(t, "job-0160", "v1.2.0", t0)); !errors.Is(err, errCrash) {
		t.Fatalf("Write with the data dir fsync failing = %v, want the fsync error", err)
	}
	if _, err := os.Lstat(filepath.Join(dd, "updater")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the updater dir whose creation was not made durable survived: %v", err)
	}
	// first write into a fresh data dir: data dir, updater, then (after the
	// rename) the jobs dir
	failOn, synced = "", nil
	mustWrite(t, dd, mustNew(t, "job-0160", "v1.2.0", t0))
	if got, want := strings.Join(synced, ","), strings.Join([]string{".", "updater", jobsDir}, ","); got != want {
		t.Errorf("first write fsyncs dirs [%s], want [%s]", got, want)
	}
	// nothing created: only the jobs dir after the rename
	synced = nil
	mustWrite(t, dd, mustNew(t, "job-0161", "v1.2.0", t0))
	if got := strings.Join(synced, ","); got != jobsDir {
		t.Errorf("a write that creates no dir fsyncs [%s], want [%s]", got, jobsDir)
	}
	// updater dir already there (the hold writer / the wire made it): only
	// the jobs dir is new, so only its parent is synced before the write
	dd = t.TempDir()
	if err := os.Mkdir(filepath.Join(dd, "updater"), 0o700); err != nil {
		t.Fatal(err)
	}
	synced = nil
	mustWrite(t, dd, mustNew(t, "job-0162", "v1.2.0", t0))
	if got, want := strings.Join(synced, ","), strings.Join([]string{"updater", jobsDir}, ","); got != want {
		t.Errorf("first job write under an existing updater dir fsyncs [%s], want [%s]", got, want)
	}
}
