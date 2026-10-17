package updaterjob

import (
	"errors"
	"testing"
	"time"
)

// TestStaleIsThirtyMinutesFromCreatedAt: the 30-minute rule (C21 as ruled)
// is a pure function of the job and the injected clock. Age is now −
// created_at — NOT the last transition — so a job still moving after 30
// minutes is stale; exactly 30 minutes is not; a finished job never is; an
// unfinished terminal (complete/started: the hold clear has not run) is;
// and a created_at after now (the clock stepped back) or zero fails closed.
func TestStaleIsThirtyMinutesFromCreatedAt(t *testing.T) {
	restoreSeams(t)
	dd := t.TempDir()
	now := t0
	j := mustNew(t, "job-0030", "v1.2.0", now)
	mustWrite(t, dd, j)
	step(t, dd, &j, &now, StateDownloaded)
	// Read it back: the production reader's times (no monotonic reading).
	j, err := Read(dd, j.JobID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Time
		want bool
	}{
		{t0, false},
		{t0.Add(MaxJobAge), false},
		{t0.Add(MaxJobAge + time.Nanosecond), true},
		{t0.Add(10 * time.Hour), true},
		{t0.Add(-time.Nanosecond), true}, // clock stepped back past the job's birth
	} {
		if got := Stale(j, c.at); got != c.want {
			t.Errorf("Stale(downloaded/done created %s, now %s) = %v, want %v", t0.Format(time.RFC3339Nano), c.at.Format(time.RFC3339Nano), got, c.want)
		}
	}
	// age is from created_at: a transition a second ago does not make it young
	late := j
	late.UpdatedAt = t0.Add(MaxJobAge + time.Minute)
	if !Stale(late, t0.Add(MaxJobAge+time.Minute+time.Second)) {
		t.Error("a job 31 min old with a fresh updated_at read as not stale — age must be from created_at")
	}
	if MaxJobAge != 30*time.Minute {
		t.Errorf("MaxJobAge = %v, want 30m (dispatch §2)", MaxJobAge)
	}
	zero := j
	zero.CreatedAt = time.Time{}
	if !Stale(zero, t0) {
		t.Error("a zero created_at is not stale")
	}
	// finished vs unfinished terminals, far past the limit
	for _, c := range []struct {
		s    State
		p    Phase
		want bool
	}{
		{StateComplete, PhaseDone, false},
		{StateRolledBack, PhaseDone, false},
		{StateRecoveryNeeded, PhaseDone, false},
		{StateCancelled, PhaseDone, false},
		{StateRefused, PhaseDone, false},
		{StateComplete, PhaseStarted, true},
		{StateRolledBack, PhaseStarted, true},
		{StateNT8Updated, PhaseDone, true}, // the attended park is subject to the rule too (C21 default)
		{StateRequested, PhaseDone, true},
	} {
		k := j
		k.State, k.Phase = c.s, c.p
		if got := Stale(k, t0.Add(time.Hour)); got != c.want {
			t.Errorf("Stale(%s/%s, +1h) = %v, want %v", c.s, c.p, got, c.want)
		}
	}
}

// TestJobLifecycleKeepsTheHistoryLegal: New is requested/done with attempts
// 0, no receipts ([] — computed empty) and a one-entry history at created_at;
// Enter starts a state with attempts 1 (a no-effect state is entered done
// with 0) and clears the blocker; Finish only from started; Retry counts to
// MaxAttempts and then refuses; AddReceipt caps at MaxReceipts; and every
// job these build is one Write accepts.
func TestJobLifecycleKeepsTheHistoryLegal(t *testing.T) {
	restoreSeams(t)
	local := time.FixedZone("CDT", -5*3600)
	now := t0.In(local)
	j := mustNew(t, "job-0040", "v1.2.0", now)
	if j.State != StateRequested || j.Phase != PhaseDone || j.Attempts != 0 || j.Schema != SchemaVersion {
		t.Fatalf("New = %s/%s attempts %d schema %d", j.State, j.Phase, j.Attempts, j.Schema)
	}
	if j.Receipts == nil || len(j.Receipts) != 0 || len(j.Transitions) != 1 {
		t.Fatalf("New receipts %v transitions %v", j.Receipts, j.Transitions)
	}
	if j.CreatedAt.Location() != time.UTC || !j.CreatedAt.Equal(t0) || j.Transitions[0].At != j.CreatedAt {
		t.Fatalf("New times not UTC/created_at: %v %v", j.CreatedAt, j.Transitions[0].At)
	}
	if _, err := New("job-0040", "v1.2.0", time.Time{}); err == nil {
		t.Error("New without a clock reading")
	}
	if err := j.Finish(now); !errors.Is(err, ErrForbiddenEdge) {
		t.Errorf("Finish(requested/done) = %v", err)
	}
	if err := j.Retry(now); !errors.Is(err, ErrForbiddenEdge) {
		t.Errorf("Retry(requested/done) = %v", err)
	}
	j.Blocker = "waiting"
	if err := j.Enter(StateDownloaded, now); err != nil {
		t.Fatal(err)
	}
	if j.Phase != PhaseStarted || j.Attempts != 1 || j.Blocker != "" {
		t.Fatalf("Enter(downloaded) = %s attempts %d blocker %q", j.Phase, j.Attempts, j.Blocker)
	}
	for i := 2; i <= MaxAttempts; i++ {
		if err := j.Retry(now); err != nil || j.Attempts != i {
			t.Fatalf("Retry #%d: %v attempts %d", i, err, j.Attempts)
		}
	}
	if err := j.Retry(now); !errors.Is(err, ErrAttemptsExhausted) || j.Attempts != MaxAttempts {
		t.Fatalf("Retry past the cap = %v attempts %d", err, j.Attempts)
	}
	if err := j.AddReceipt(Receipt{}, now); err == nil {
		t.Error("a receipt without a step was added")
	}
	ev := map[string]string{"sha256": "x"}
	if err := j.AddReceipt(Receipt{Step: "download", StartedAt: now, EndedAt: now, OK: true, Evidence: ev}, now); err != nil {
		t.Fatal(err)
	}
	ev["sha256"] = "changed after the fact"
	if j.Receipts[0].Evidence["sha256"] != "x" {
		t.Error("AddReceipt kept the caller's map: a later edit rewrote the receipt")
	}
	if j.Receipts[0].StartedAt.Location() != time.UTC {
		t.Error("receipt times not UTC")
	}
	if err := j.Finish(now); err != nil || j.Phase != PhaseDone {
		t.Fatalf("Finish = %v %s", err, j.Phase)
	}
	if err := j.Finish(now); err == nil {
		t.Error("Finish twice")
	}
	// the attempts on disk: a written job at the cap still reads; one past it does not
	dd := t.TempDir()
	w := mustNew(t, "job-0041", "v1.2.0", t0)
	mustWrite(t, dd, w)
	if err := w.Enter(StateDownloaded, t0); err != nil {
		t.Fatal(err)
	}
	w.Attempts = MaxAttempts
	mustWrite(t, dd, w)
	w.Attempts = MaxAttempts + 1
	if err := Write(dd, w); !errors.Is(err, ErrCorrupt) {
		t.Errorf("attempts past the cap written: %v", err)
	}
	// a no-effect state is entered done, attempts 0
	c := mustNew(t, "job-0042", "v1.2.0", t0)
	if err := c.Enter(StateCancelled, t0); err != nil || c.Phase != PhaseDone || c.Attempts != 0 {
		t.Fatalf("Enter(cancelled) = %v %s %d", err, c.Phase, c.Attempts)
	}
	// receipts cap
	r := mustNew(t, "job-0043", "v1.2.0", t0)
	for i := 0; i < MaxReceipts; i++ {
		if err := r.AddReceipt(Receipt{Step: "x"}, t0); err != nil {
			t.Fatalf("receipt %d: %v", i, err)
		}
	}
	if err := r.AddReceipt(Receipt{Step: "x"}, t0); err == nil {
		t.Error("more than MaxReceipts receipts")
	}
	// Enter/Finish do not write through a shared backing array
	a := mustNew(t, "job-0044", "v1.2.0", t0)
	a.Transitions = append(make([]Transition, 0, 8), a.Transitions...)
	b := a
	if err := a.Enter(StateDownloaded, t0); err != nil {
		t.Fatal(err)
	}
	if err := b.Enter(StateCancelled, t0); err != nil {
		t.Fatal(err)
	}
	if a.Transitions[1].State != StateDownloaded {
		t.Errorf("two copies of one job share history: %v", a.Transitions)
	}
}

// TestActivationIsNeverPersistedWithoutItsRollbackInputs: the writer refuses
// a job whose history reaches activated unless the release, the install it
// replaces, the job-scoped snapshot, the DB backup and the identity the kill
// aims at are all on disk — a crash after the kill must always be able to
// roll back.
func TestActivationIsNeverPersistedWithoutItsRollbackInputs(t *testing.T) {
	restoreSeams(t)
	dd := t.TempDir()
	now := t0
	j := mustNew(t, "job-0050", "v1.2.0", now)
	mustWrite(t, dd, j)
	for _, s := range []State{StateDownloaded, StateVerified, StatePreflightOK, StateMaintenanceHeld, StateDrainedAcked, StateGateOK, StateBackupDone, StateNT8Skipped} {
		if s == StateNT8Skipped { // the branch carries its own decision (TestNT8StateCarriesItsOwnDecision)
			j.NT8 = &NT8Decision{Decision: NT8Skipped}
		}
		step(t, dd, &j, &now, s)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	old := "89abcdef0123456789abcdef0123456789abcdef"
	full := func() Job {
		k := j
		k.SourceSHA = sha
		k.Release = &Release{Dir: "/r/" + sha, SHA: sha, Binary: "/r/" + sha + "/nofx-bin", Dist: "/r/" + sha + "/web/dist", ReleaseFile: "/r/" + sha + "/RELEASE", ManifestPath: "/r/" + sha + "/manifest.json"}
		k.Install = &Release{Dir: "/i", SHA: old, Binary: "/i/nofx-bin", Dist: "/i/web/dist", ReleaseFile: "/i/deploy/RELEASE"}
		k.Snapshot = &Release{Dir: "/b/install", SHA: old, Binary: "/b/install/nofx-bin", Dist: "/b/install/web/dist", ReleaseFile: "/b/install/deploy/RELEASE"}
		k.BackupPath = "/b/data.db"
		k.IdentityBefore = &Identity{PID: 172, StartTicks: 23987}
		if err := k.Enter(StateActivated, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		return k
	}
	for name, drop := range map[string]func(k *Job){
		"no release":         func(k *Job) { k.Release = nil },
		"no install":         func(k *Job) { k.Install = nil },
		"no snapshot":        func(k *Job) { k.Snapshot = nil },
		"no backup_path":     func(k *Job) { k.BackupPath = "" },
		"no identity_before": func(k *Job) { k.IdentityBefore = nil },
	} {
		k := full()
		drop(&k)
		if err := Write(dd, k); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: Write = %v, want ErrCorrupt", name, err)
		}
	}
	if got, _ := Read(dd, j.JobID); got.State != StateNT8Skipped {
		t.Fatalf("a refused activation changed the file: %s", got.State)
	}
	// positive control, and the inputs are write-once from here on
	k := full()
	mustWrite(t, dd, k)
	k2 := k
	k2.Snapshot = &Release{Dir: "/elsewhere", SHA: old, Binary: "/elsewhere/nofx-bin", Dist: "/elsewhere/web/dist", ReleaseFile: "/elsewhere/deploy/RELEASE"}
	if err := Write(dd, k2); !errors.Is(err, ErrRewrite) {
		t.Errorf("snapshot changed after activation: Write = %v, want ErrRewrite", err)
	}
	k3 := k // same sha, another directory: only the release pointer changes
	k3.Release = &Release{Dir: "/r/x", SHA: sha, Binary: "/r/x/nofx-bin", Dist: "/r/x/web/dist", ReleaseFile: "/r/x/RELEASE"}
	if err := Write(dd, k3); !errors.Is(err, ErrRewrite) {
		t.Errorf("release changed after activation: Write = %v, want ErrRewrite", err)
	}
	// identity fields are NOT write-once: a resume re-reads the current identity (C3 as ruled)
	k4 := k
	k4.IdentityBefore = &Identity{PID: 999, StartTicks: 1}
	mustWrite(t, dd, k4)
}
