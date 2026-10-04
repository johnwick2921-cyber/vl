package updaterworker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"vl/internal/updaterjob"
)

// The 2026-10-04 install (job de4cf900) ended recovery_needed: "worker_swapped
// ran 3 times without finishing (attempts cap 3)". The old worker swapped its
// own binary and signalled serve() to exit INSIDE the step, before the swap
// receipt and the complete transition were written; each restarted worker
// found worker_swapped/started on disk, saw it already ran the new binary
// ("already"), signalled the exit again, and exited again before persisting.
//
// These tests drive the PRODUCTION runner over that job's real history
// (testdata/selfswap-loop, copied from the box) with the real swap step.

const (
	swapLoopJobID  = "de4cf900256570636a9077e028e092e8"
	swapLoopNewSHA = "a057ab834a364db24b3045a293295c2c3e7afb96"
	swapLoopOldSHA = "52f1989ca0a5ba4a4f126caa8b9bd2c0d790b96b"
)

// writeSwapLoopJob writes the real job into data as it stood when the first
// worker entered worker_swapped: the history up to boot_verified/done, then
// worker_swapped/started with attempts 1 (what Enter writes) and no recovery
// verdict. The fixture is loaded and rewritten through the production
// reader/writer, so the result is canonical and validated.
func writeSwapLoopJob(t *testing.T, data string) {
	t.Helper()
	fixDir := t.TempDir() // a data dir holding only the fixture, for the production reader
	jobsDir, err := updaterjob.JobsDir(fixDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "selfswap-loop", swapLoopJobID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, swapLoopJobID+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	j, err := updaterjob.Read(fixDir, swapLoopJobID)
	if err != nil {
		t.Fatalf("the real job fixture does not validate: %v", err)
	}
	if j.State != updaterjob.StateRecoveryNeeded || len(j.Transitions) < 2 {
		t.Fatalf("fixture is %s with %d transitions, want recovery_needed", j.State, len(j.Transitions))
	}
	prev := j.Transitions[len(j.Transitions)-2]
	if prev.State != updaterjob.StateWorkerSwapped || prev.Phase != updaterjob.PhaseStarted {
		t.Fatalf("fixture's last real step is %s/%s, want worker_swapped/started", prev.State, prev.Phase)
	}
	j.Transitions = j.Transitions[:len(j.Transitions)-1]
	j.State, j.Phase, j.Attempts = prev.State, prev.Phase, 1
	j.RecoveryReason = ""
	// A job file is born at requested; the rest of the history extends it.
	born, err := updaterjob.New(j.JobID, j.ReleaseID, j.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := updaterjob.Write(data, born); err != nil {
		t.Fatalf("birth of the job file: %v", err)
	}
	if err := updaterjob.Write(data, j); err != nil {
		t.Fatalf("writing the pre-swap job: %v", err)
	}
	back, err := updaterjob.Read(data, swapLoopJobID)
	if err != nil {
		t.Fatalf("the rebuilt pre-swap job does not validate: %v", err)
	}
	if back.State != updaterjob.StateWorkerSwapped || back.Phase != updaterjob.PhaseStarted || back.Attempts != 1 {
		t.Fatalf("pre-swap job at %s/%s attempts %d", back.State, back.Phase, back.Attempts)
	}
}

// swapLoopWorker is a worker over the rig's dirs whose own executable is exe
// (build info exeRev) and whose release carries the new updater binary.
func swapLoopWorker(t *testing.T, r *rig, exe, exeRev, candidate, relDir string) *Worker {
	t.Helper()
	rel := suRel{
		verdict: Verdict{ReleaseID: "v2026.10.04.1", SourceSHA: swapLoopNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v2026.10.04.1", SourceSHA: swapLoopNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {exeRev, "false"}, candidate: {swapLoopNewSHA, "false"}}}
	w, err := New(r.cfg, Deps{Lib: &fakeLib{r.box}, App: r.app, Rel: rel, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func readSwapLoopJob(t *testing.T, r *rig) updaterjob.Job {
	t.Helper()
	j, err := updaterjob.Read(r.data, swapLoopJobID)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func swapDoneClosed(w *Worker) bool {
	select {
	case <-w.SwapDone():
		return true
	default:
		return false
	}
}

func hasReceipt(j updaterjob.Job, step string) (updaterjob.Receipt, bool) {
	for _, rc := range j.Receipts {
		if rc.Step == step {
			return rc, true
		}
	}
	return updaterjob.Receipt{}, false
}

// TestWorkerSwapSignalsExitOnlyAfterTheJobFinished: the FIRST worker swaps its
// binary. The exit signal must not fire at the swap, nor after the swap receipt
// is written, nor after complete is entered: only once the job has FINISHED
// (receipt, complete and the hold/lock release persisted).
// Mutant: closeSwapDone(w) back inside swapWorkerBinary → RED.
func TestWorkerSwapSignalsExitOnlyAfterTheJobFinished(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	r := newRig(t)
	writeSwapLoopJob(t, r.data)
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	w := swapLoopWorker(t, r, exe, swapLoopOldSHA, candidate, relDir)

	w.crash = func(point string) {
		switch point {
		case "worker_swapped/effect", "worker_swapped/done", "complete/started", "complete/effect":
			if swapDoneClosed(w) {
				t.Errorf("SwapDone closed at %s — the worker would exit before the job is persisted as finished", point)
			}
		}
	}
	if err := w.drive(context.Background(), swapLoopJobID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("exe = %q, want the swapped binary", got)
	}
	j := readSwapLoopJob(t, r)
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("job ended %s/%s, want complete/done", j.State, j.Phase)
	}
	rc, ok := hasReceipt(j, "worker_swap")
	if !ok || rc.Evidence["new_sha"] != swapLoopNewSHA {
		t.Fatalf("worker_swap receipt = %+v (found %v)", rc, ok)
	}
	if !swapDoneClosed(w) {
		t.Fatal("SwapDone not closed after the job finished — the unit would never restart onto the new binary")
	}
}

// TestWorkerSwapRestartOnTheNewBinaryCompletesInsteadOfLooping replays the
// incident: the worker dies right after the swap (before the receipt), and
// restarts on the NEW binary three times. The first restart must finish the
// job as "already" — without signalling another exit, without burning
// attempts, never recovery_needed.
// Mutant: closeSwapDone(w) back in the oldRev == rev branch → RED.
func TestWorkerSwapRestartOnTheNewBinaryCompletesInsteadOfLooping(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	r := newRig(t)
	writeSwapLoopJob(t, r.data)
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")

	// Run 1: the swap happens, then the process is gone before finish() writes
	// anything (the crash seam plays the exit).
	w1 := swapLoopWorker(t, r, exe, swapLoopOldSHA, candidate, relDir)
	w1.crash = func(point string) {
		if point == "worker_swapped/effect" {
			panic("process exits right after the swap")
		}
	}
	func() {
		defer func() { _ = recover() }()
		_ = w1.drive(context.Background(), swapLoopJobID)
	}()
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("run 1 did not swap the binary: %q", got)
	}
	j := readSwapLoopJob(t, r)
	if j.State != updaterjob.StateWorkerSwapped || j.Phase != updaterjob.PhaseStarted {
		t.Fatalf("after the crash the job is %s/%s, want worker_swapped/started (the real incident shape)", j.State, j.Phase)
	}
	if _, ok := hasReceipt(j, "worker_swap"); ok {
		t.Fatal("the swap receipt must not be on disk after a crash before finish()")
	}

	// Restart on the new binary: the running worker's build is now the
	// release's.
	for i := 1; i <= 3; i++ {
		w := swapLoopWorker(t, r, exe, swapLoopNewSHA, candidate, relDir)
		if err := w.drive(context.Background(), swapLoopJobID); err != nil {
			t.Fatalf("restart %d: %v", i, err)
		}
		j = readSwapLoopJob(t, r)
		if j.State == updaterjob.StateRecoveryNeeded {
			t.Fatalf("restart %d: job went to recovery_needed: %s", i, j.RecoveryReason)
		}
		if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
			t.Fatalf("restart %d: job is %s/%s, want complete/done on the FIRST restart", i, j.State, j.Phase)
		}
		if swapDoneClosed(w) {
			t.Fatalf("restart %d: SwapDone closed on the already-swapped path — the worker would exit and loop", i)
		}
		if i == 1 {
			rc, ok := hasReceipt(j, "worker_swap")
			if !ok || rc.Evidence["already"] != "true" {
				t.Fatalf("restart 1 worker_swap receipt = %+v (found %v), want already=true", rc, ok)
			}
		}
	}
}

// TestWorkerSwapKnobOffNeverSignalsExit: with the knob off the step records
// "off" and the job completes; nothing was swapped, so nothing may exit.
func TestWorkerSwapKnobOffNeverSignalsExit(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "")
	r := newRig(t)
	writeSwapLoopJob(t, r.data)
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	w := swapLoopWorker(t, r, exe, swapLoopOldSHA, candidate, relDir)
	if err := w.drive(context.Background(), swapLoopJobID); err != nil {
		t.Fatal(err)
	}
	if j := readSwapLoopJob(t, r); j.State != updaterjob.StateComplete {
		t.Fatalf("job ended %s", j.State)
	}
	if swapDoneClosed(w) {
		t.Fatal("SwapDone closed with the self-update knob off")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe changed with the knob off: %q", got)
	}
}
