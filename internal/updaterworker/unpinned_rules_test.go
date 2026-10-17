package updaterworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// ── pins for the rules the adversarial verifier found unpinned (D5) ────────
//
// Each rule below had a compiling revert that left the whole package green
// (mutants A23, A42, A43, A25, A30, A46, A36, A44, A18). Every pin runs the
// REAL runner at its production call site on the test box.

// receiptOf is the LAST receipt of step in j (nil when absent).
func receiptOf(j updaterjob.Job, step string) *updaterjob.Receipt {
	for i := len(j.Receipts) - 1; i >= 0; i-- {
		if j.Receipts[i].Step == step {
			return &j.Receipts[i]
		}
	}
	return nil
}

// hookAt runs fn (no crash) every time the runner passes point.
func (r *rig) hookAt(point string, fn func()) {
	prev := r.w.crash
	r.w.crash = func(q string) {
		if prev != nil {
			prev(q)
		}
		if q == point {
			fn()
		}
	}
}

func (b *box) set(fn func()) {
	b.mu.Lock()
	fn()
	b.mu.Unlock()
}

// PIN (C13, mutant A23): after the boot, /api/health must serve the release's
// sha or a prefix of it of at least 7 characters. A shorter prefix, or a
// revision that is not the release's, fails boot_verified (and rolls back);
// a 7-character prefix passes.
func TestBootVerifyNeedsHealthToServeTheRelease(t *testing.T) {
	for _, tc := range []struct {
		name, rev string
		final     updaterjob.State
	}{
		{"a 7-char prefix", boxNew[:7], updaterjob.StateComplete},
		{"a 6-char prefix", boxNew[:6], updaterjob.StateRolledBack},
		{"another build", "c3c3c3c3c3c3", updaterjob.StateRolledBack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.hookAt("boot_verified/started", func() { r.set(func() { r.healthRev = tc.rev }) })
			r.hookAt("rolling_back/started", func() { r.set(func() { r.healthRev = "" }) })
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != tc.final {
				t.Fatalf("health %q: job ended %s (error %q), want %s", tc.rev, j.State, j.Error, tc.final)
			}
			if tc.final == updaterjob.StateRolledBack && !strings.Contains(j.Error, "health serves") {
				t.Fatalf("rolled back for %q, want the health revision", j.Error)
			}
		})
	}
}

// PIN (C11 as ruled, mutants A42/A43): gate_ok needs ready:true on two reads
// whose acks are DIFFERENT acks (arrivals rebuilt on the worker's monotonic
// clock as now − AgeMs, at least a second apart), both received AFTER the gate
// step started. The gate step is made to start 3 s after an ack tick, so the
// ack standing at its start is older than it.
func TestGateNeedsTwoDistinctAcksBothAfterItsStart(t *testing.T) {
	r := newRig(t)
	r.hookAt("gate_ok/started", func() {
		now := r.clock.Now()
		off := now.Sub(now.Truncate(5 * time.Second))
		r.clock.Advance((3*time.Second - off + 5*time.Second) % (5 * time.Second))
	})
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job ended %s (error %q)", j.State, j.Error)
	}
	g := receiptOf(j, "gate")
	if g == nil || !g.OK {
		t.Fatalf("no OK gate receipt: %v", receiptSteps(j))
	}
	a1, err1 := time.Parse(time.RFC3339Nano, g.Evidence["ack_1_at"])
	a2, err2 := time.Parse(time.RFC3339Nano, g.Evidence["ack_2_at"])
	if err1 != nil || err2 != nil {
		t.Fatalf("gate evidence %v", g.Evidence)
	}
	if a1.Before(g.StartedAt) {
		t.Fatalf("the first ack %s was received before the gate step started %s", a1, g.StartedAt)
	}
	if a2.Sub(a1) < time.Second {
		t.Fatalf("the two acks %s and %s are one ack read twice", a1, a2)
	}
}

// PIN (OQ-4, mutant A25): after the boot, the AddOn must ack the NEW process:
// an ack received before the kill instant (still fresh by age) is the old
// process's and does not count.
func TestPostBootAckMustBeNewerThanTheKill(t *testing.T) {
	r := newRig(t)
	r.hookAt("boot_verified/started", func() { r.set(func() { r.ackLag = 8 * time.Second }) })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job ended %s (error %q)", j.State, j.Error)
	}
	bv := receiptOf(j, "boot_verify")
	at, err := time.Parse(time.RFC3339Nano, bv.Evidence["acked_at"])
	if err != nil {
		t.Fatalf("boot_verify evidence %v", bv.Evidence)
	}
	if at.Before(*j.WatchSince) {
		t.Fatalf("boot_verified accepted an ack received %s, before the kill %s", at, *j.WatchSince)
	}
}

// PIN (dispatch §0, mutant A30): a parked job leaves nt8_updated ONLY on the
// attended resume verb — never on its own, not when the AddOn already runs
// the release's build, not on a runner wake, not on a fresh worker.
func TestTheParkIsLeftOnlyOnTheAttendedResume(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateNT8Updated || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("no park: %s/%s", j.State, j.Phase)
	}
	r.f5() // the AddOn now acks the release's build
	for i := 0; i < 2; i++ {
		if err := r.drive(); err != nil {
			t.Fatal(err)
		}
	}
	r.w = r.newWorker()
	if rep, err := r.w.sweep(); err != nil || rep.Active != boxJobID {
		t.Fatalf("sweep = %+v, %v", rep, err)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateNT8Updated || j.ResumedAt != nil || r.callCount("activate") != 0 {
		t.Fatalf("the park was left without a resume: %s/%s resumed_at=%v activate=%d", j.State, j.Phase, j.ResumedAt, r.callCount("activate"))
	}
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateComplete || j.ResumedAt == nil {
		t.Fatalf("after the resume: %s (resumed_at %v)", j.State, j.ResumedAt)
	}
}

// PIN (C9 as ruled, mutant A46): verified re-proves the release and the
// re-verified manifest must BE the verdict's — the same release id, source
// sha and manifest hash. Any drift refuses the job before the release dir is
// even resolved: nothing held, nothing changed.
func TestTheReverifiedManifestMustBeTheVerdicts(t *testing.T) {
	for name, tamper := range map[string]func(*ReleaseFacts){
		"release id":    func(f *ReleaseFacts) { f.ReleaseID = "v9.9.9" },
		"source sha":    func(f *ReleaseFacts) { f.SourceSHA = boxOld },
		"manifest hash": func(f *ReleaseFacts) { f.ManifestSHA256 = strings.Repeat("cd", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.reverifyTamper = tamper
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != updaterjob.StateRefused || !strings.Contains(j.Error, "is not the verdict's") {
				t.Fatalf("a re-verified %s that is not the verdict's: job %s (error %q), want refused", name, j.State, j.Error)
			}
			if n := r.callCount("resolve"); n != 0 {
				t.Fatalf("the release dir was resolved %d times", n)
			}
			if r.hold().Present {
				t.Fatal("a refused job left a hold")
			}
		})
	}
}

// PIN (C11 as ruled, mutant A44): drained_acked reads /api/maintenance held
// for THIS job. The app naming another job (whatever the hold file on disk
// says) is not drained: reverted_clean — the hold is released (it is ours on
// disk), the lock is released, nothing was backed up or installed.
func TestDrainNeedsTheMaintenanceHoldForThisJob(t *testing.T) {
	r := newRig(t)
	r.hookAt("drained_acked/started", func() { r.set(func() { r.maintJob = "job-u4-9999abcd" }) })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean || !strings.Contains(j.Error, "not held for this job") {
		t.Fatalf("job %s (error %q), want reverted_clean: the hold is not held for this job", j.State, j.Error)
	}
	if r.callCount("backup") != 0 || r.callCount("activate") != 0 {
		t.Fatalf("a job the app does not hold went on: %v", r.calls)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldAbsent {
		t.Fatalf("hold %s, want released (reverted_clean)", s)
	}
}

// PIN (C12 as ruled, mutant A18): a release whose signed manifest lists no
// ninjascript/*.cs cannot prove the AddOn source unchanged — even when the
// install has none either (two empty sets are not a proof). It parks.
func TestNT8ParksWhenTheReleaseListsNoCSharp(t *testing.T) {
	r := newRig(t)
	r.noCS = true
	if err := os.Remove(filepath.Join(r.inst, "ninjascript", "VLTrader.cs")); err != nil {
		t.Fatal(err)
	}
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State != updaterjob.StateNT8Updated || j.NT8 == nil || !strings.Contains(j.NT8.Reason, "lists no ninjascript/*.cs") {
		t.Fatalf("job %s decision %+v, want the park: the release lists no C#", j.State, j.NT8)
	}
	if r.callCount("activate") != 0 {
		t.Fatal("activated without proving the AddOn source")
	}
}

// PIN (U4 re-verify note 2, mutant M32 — the verifier's probe P1): a resume
// while the AddOn still runs the OLD build does NOT leave the park — the
// owner typed resume but never did the F5. The job stays nt8_updated, nothing
// is activated, the resume receipt is a refusal and the blocker names both
// builds. After the F5 (the AddOn now acks the release's build) the next
// resume leaves the park and the job completes.
func TestResumeWaitsForTheAddOnBuildToMove(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateNT8Updated || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("fixture: job %s/%s, want parked at nt8_updated/done", j.State, j.Phase)
	}
	r.clock.Advance(3 * time.Minute) // the owner takes as long as a real F5 would — but never does it
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume verb: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	rc := receiptOf(j, "resume")
	wantWhy := `the AddOn acks build "` + boxOldBuild + `", the release is "` + boxNewBuild + `"`
	if j.State != updaterjob.StateNT8Updated || r.callCount("activate") != 0 || j.ResumedAt != nil ||
		!strings.Contains(j.Blocker, "resume refused") || !strings.Contains(j.Blocker, wantWhy) || rc == nil || rc.OK {
		t.Fatalf("a resume without the F5 left the park: %s/%s activate=%d resumed_at=%v blocker=%q resume receipt=%+v",
			j.State, j.Phase, r.callCount("activate"), j.ResumedAt, j.Blocker, rc)
	}
	r.f5()
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume verb after the F5: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateComplete || r.callCount("activate") != 1 {
		t.Fatalf("after the F5 the resume ended %s/%s (activate=%d), want complete", j.State, j.Phase, r.callCount("activate"))
	}
}
