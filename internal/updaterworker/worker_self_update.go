package updaterworker

// worker-self-update (owner order 10-02 12:0x CT): after boot_verified proves
// the NEW BOT, the worker installs the release's verified updater/vl-updater
// over its own binary ATOMICALLY, records a receipt, and — once the job has
// FINISHED — signals serve() to exit so systemd restarts the unit on the new
// binary.
//
// Ordering guarantees (state table, internal/updaterjob/states.go):
//   - the step runs ONLY from StateWorkerSwapped, whose sole predecessor is
//     StateBootVerified (never refused/rolled_back/recovery_needed — no edge).
//   - a refusal records a named reason in the receipt and the job COMPLETES
//     (the bot install itself succeeded; the swap is best-effort).
//   - the exit comes only AFTER the job finished (receipt, complete and the
//     hold/lock release persisted): the step sets swapPending, finished()
//     closes SwapDone. A restart that lands on an already-swapped binary
//     completes the step as "already" and never exits again (job de4cf900).
//
// Knob (L4): VL_UPDATER_SELF_UPDATE=1 enables the swap; anything else is OFF.
//
// Atomicity: write <exe>.new.<job> in the SAME directory (0600 owner-exec),
// fsync, hard-link the old binary to <exe>.old.<oldrev>, fsync, rename the
// temp over <exe>. POSIX rename is atomic, so a crash at ANY point leaves
// either the old or the new binary whole — never a half file.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vl/internal/updaterjob"
)

// workerBinaryName is the updater binary the release ships and the unit runs.
const workerBinaryName = "vl-updater"

// selfUpdateOn is the knob: exactly "1" enables the swap (L4, default OFF).
func selfUpdateOn() bool {
	v := os.Getenv("VL_UPDATER_SELF_UPDATE")
	return v == "1"
}

// SwapDone closes when a job that swapped the worker binary has finished
// (receipt and terminal effects persisted). cmd/vl-updater serve exits on it.
func (w *Worker) SwapDone() <-chan struct{} { return w.swapDone }

// stepWorkerSwap is the EffectWorkerSwap side effect.
func (w *Worker) stepWorkerSwap(ctx context.Context, j updaterjob.Job) stepResult {
	start, ev := w.host.Now(), map[string]string{}
	if !selfUpdateOn() {
		ev["self_update"] = "off"
		return stepResult{receipts: []Receipt{w.receipt("worker_swap", start, ev, nil)}, err: nil}
	}
	err := w.swapWorkerBinary(ctx, j, ev)
	return stepResult{receipts: []Receipt{w.receipt("worker_swap", start, ev, err)}, err: nil}
}

// swapWorkerBinary performs the swap. It never fails the job: every refusal
// lands in ev["refused"] with a named reason and a nil error.
func (w *Worker) swapWorkerBinary(ctx context.Context, j updaterjob.Job, ev map[string]string) error {
	refuse := func(reason string) {
		ev["refused"] = reason
		w.logf("updater: worker self-update refused: %s", reason)
	}

	// The binary must come from the SAME verified release dir: the verdict's
	// release dir plus the signed manifest's own artifact hash (facts() re-
	// verifies the manifest NOW, at this step).
	facts, err := w.facts(j)
	if err != nil {
		refuse("release facts unavailable: " + err.Error())
		return nil
	}
	want, ok := facts.Artifacts[filepath.ToSlash(filepath.Join("updater", workerBinaryName))]
	if !ok {
		refuse("updater/" + workerBinaryName + " is not in the signed manifest")
		return nil
	}
	v, err := w.rel.Verdict(j.ReleaseID)
	if err != nil {
		refuse("verdict unreadable: " + err.Error())
		return nil
	}
	candidate := filepath.Join(v.ReleaseDir, "updater", workerBinaryName)
	sum, err := sha256File(candidate)
	if err != nil {
		refuse("candidate unreadable: " + err.Error())
		return nil
	}
	if sum != want {
		refuse(fmt.Sprintf("manifest sha256 mismatch: candidate %s, manifest %s", sum[:16], want[:16]))
		return nil
	}
	rev, modified, err := w.host.BuildInfo(candidate)
	if err != nil || modified != "false" || rev != facts.SourceSHA {
		refuse(fmt.Sprintf("candidate build info: revision %q modified %q, want %s (err %v)", rev, modified, facts.SourceSHA, err))
		return nil
	}

	exe, err := w.host.ExeOf(os.Getpid())
	if err != nil {
		refuse("own executable unknown: " + err.Error())
		return nil
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		refuse("own executable path: " + err.Error())
		return nil
	}
	oldRev, oldMod, err := w.host.BuildInfo(exe)
	if err != nil || oldMod != "false" {
		refuse(fmt.Sprintf("running worker build info unreadable: %v %q %q", err, oldRev, oldMod))
		return nil
	}
	if oldRev == rev {
		// Idempotent: the running worker already IS the release's updater
		// (a restart after the swap, or a release that did not change it).
		// Nothing to swap and nothing to restart — never signal an exit here:
		// the restarted worker would exit again before this step persists, and
		// the job would loop to the attempts cap.
		ev["already"] = "true"
		ev["old_sha"], ev["new_sha"] = oldRev, rev
		return nil
	}

	// Atomic sequence: orphan cleanup → temp write → fsync → hard-link old →
	// fsync → rename. Orphans are only vl-updater.new.* files — never the
	// running exe, never the .old being kept.
	dir := filepath.Dir(exe)
	if err := pruneOrphans(dir); err != nil {
		refuse("orphan cleanup: " + err.Error())
		return nil
	}
	tmp := filepath.Join(dir, workerBinaryName+".new."+j.JobID)
	if err := copyExecutable(candidate, tmp); err != nil {
		refuse("temp write: " + err.Error())
		return nil
	}
	defer os.Remove(tmp) // best-effort: after a successful rename it no longer exists
	old := exe + ".old." + oldRev
	if fi, err := os.Lstat(old); err != nil {
		if err := os.Link(exe, old); err != nil {
			refuse("link old: " + err.Error())
			return nil
		}
	} else if !fi.Mode().IsRegular() {
		refuse("the old-binary name is taken by a non-file")
		return nil
	}
	if err := syncDir(dir); err != nil {
		refuse("dir sync before swap: " + err.Error())
		return nil
	}
	if err := os.Rename(tmp, exe); err != nil {
		refuse("rename: " + err.Error())
		return nil
	}
	if err := syncDir(dir); err != nil {
		w.logf("updater: worker self-update: dir sync after swap: %v", err)
	}
	ev["old_sha"], ev["new_sha"], ev["path"] = oldRev, rev, exe
	if err := pruneOldBinaries(dir); err != nil {
		w.logf("updater: worker self-update: prune: %v", err)
	}
	w.logf("updater: worker self-update: swapped %s %s -> %s", workerBinaryName, oldRev, rev)
	// Exit is deferred to finished(): the receipt and the complete transition
	// are not on disk yet.
	w.swapPending.Store(true)
	return nil
}

// closeSwapDone closes the exit signal exactly once.
func closeSwapDone(w *Worker) {
	select {
	case <-w.swapDone:
	default:
		close(w.swapDone)
	}
}

// pruneOrphans removes every vl-updater.new.* file in dir (crash leftovers).
func pruneOrphans(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), workerBinaryName+".new.") && e.Type().IsRegular() {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// pruneOldBinaries keeps only the newest 3 vl-updater.old.* files (by mtime).
func pruneOldBinaries(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var olds []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), workerBinaryName+".old.") && e.Type().IsRegular() {
			olds = append(olds, e)
		}
	}
	if len(olds) <= 3 {
		return nil
	}
	sort.Slice(olds, func(i, j int) bool {
		fi, _ := olds[i].Info()
		fj, _ := olds[j].Info()
		return fi.ModTime().After(fj.ModTime())
	})
	for _, e := range olds[3:] {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyExecutable copies src to dst preserving the execute bits.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}
