package updaterworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// suRel and suHost are the smallest fakes for the worker-self-update step.

type suRel struct {
	verdict  Verdict
	facts    ReleaseFacts
	factsErr error
}

func (r suRel) Verdict(string) (Verdict, error)        { return r.verdict, nil }
func (r suRel) Rehash(Verdict) (int, error)            { return 0, nil }
func (r suRel) Reverify(Verdict) (ReleaseFacts, error) { return r.facts, r.factsErr }

type suHost struct {
	revs map[string][2]string // path -> (revision, modified)
	exe  string
}

func (h suHost) Now() time.Time                             { return time.Now().UTC() }
func (h suHost) Sleep(context.Context, time.Duration) error { return nil }
func (h suHost) MainTreeLockHeld() (bool, string, error)    { return true, "", nil }
func (h suHost) LockAcquire(string, string, int) (bool, string, error) {
	return true, "", nil
}
func (h suHost) LockHolder() (string, error) { return "", nil }
func (h suHost) LockRelease(string) error    { return nil }
func (h suHost) BuildInfo(binary string) (string, string, error) {
	if r, ok := h.revs[binary]; ok {
		return r[0], r[1], nil
	}
	return strings.Repeat("00", 20), "false", nil
}
func (h suHost) ExeOf(int) (string, error) { return h.exe, nil }

const suNewSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const suOldSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func shaOf(b string) string {
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}

func suWorker(t *testing.T, rel suRel, host suHost) *Worker {
	t.Helper()
	inst := t.TempDir()
	data := t.TempDir()
	return &Worker{
		cfg: Config{
			Target: Target{InstallDir: inst, DataDir: data},
			Logf:   func(string, ...any) {},
		},
		host:     host,
		rel:      rel,
		swapDone: make(chan struct{}),
	}
}

func suJob() updaterjob.Job {
	return updaterjob.Job{JobID: "job-su-0001abcd", ReleaseID: "v1.2.3", SourceSHA: suNewSHA}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func refused(t *testing.T, ev map[string]string, want string) {
	t.Helper()
	if !strings.Contains(ev["refused"], want) {
		t.Fatalf("refused = %q, want it to contain %q", ev["refused"], want)
	}
}

// TestWorkerSwapTableReachableOnlyAfterBootVerified: the swap state's ONLY
// predecessor is boot_verified — refused/rolled_back/recovery_needed jobs can
// never reach it (the table has no edge), and the step never runs before the
// new bot is proven.
func TestWorkerSwapTableReachableOnlyAfterBootVerified(t *testing.T) {
	for _, s := range updaterjob.AllStates() {
		r, ok := updaterjob.Lookup(s)
		if !ok {
			t.Fatalf("no row for %s", s)
		}
		for _, succ := range r.Success {
			if succ == updaterjob.StateWorkerSwapped {
				if s != updaterjob.StateBootVerified {
					t.Fatalf("%s lists worker_swapped as a successor — swap reachable before boot_verified", s)
				}
			}
		}
	}
	for _, s := range []updaterjob.State{updaterjob.StateRefused, updaterjob.StateRolledBack, updaterjob.StateRecoveryNeeded, updaterjob.StateCancelled} {
		r, _ := updaterjob.Lookup(s)
		if len(r.Success) != 0 {
			t.Fatalf("terminal %s has successors %v", s, r.Success)
		}
	}
}

func TestWorkerSwapKnobOffDoesNothing(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "0")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	w := suWorker(t, suRel{}, suHost{exe: exe, revs: map[string][2]string{exe: {suOldSHA, "false"}}})
	res := w.stepWorkerSwap(context.Background(), suJob())
	if res.err != nil || len(res.receipts) != 1 {
		t.Fatalf("res = %+v", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe = %q, want untouched", got)
	}
	select {
	case <-w.SwapDone():
		t.Fatal("SwapDone closed with the knob off")
	default:
	}
}

func TestWorkerSwapSuccessIsAtomicAndReceipted(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suOldSHA, "false"}, candidate: {suNewSHA, "false"}}}
	w := suWorker(t, rel, host)
	res := w.stepWorkerSwap(context.Background(), suJob())
	if res.err != nil {
		t.Fatalf("err = %v", res.err)
	}
	ev := res.receipts[0].Evidence
	if ev["old_sha"] != suOldSHA || ev["new_sha"] != suNewSHA {
		t.Fatalf("receipt = %+v", ev)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("exe = %q, want the new binary", got)
	}
	if got, _ := os.ReadFile(exe + ".old." + suOldSHA); string(got) != "old-binary" {
		t.Fatalf("kept old = %q, want the old binary", got)
	}
	select {
	case <-w.SwapDone():
		t.Fatal("SwapDone closed by the step — the exit signal belongs to finished(), after the receipt and complete are persisted")
	default:
	}
	if !w.swapPending.Load() {
		t.Fatal("swapPending not set after a successful swap")
	}
}

func TestWorkerSwapRefusesManifestMismatch(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("some-other-bytes")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suOldSHA, "false"}, candidate: {suNewSHA, "false"}}}
	res := suWorker(t, rel, host).stepWorkerSwap(context.Background(), suJob())
	refused(t, res.receipts[0].Evidence, "manifest sha256 mismatch")
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe touched on refusal: %q", got)
	}
}

func TestWorkerSwapRefusesMissingManifestEntry(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts:   ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, Artifacts: map[string]string{}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{exe: {suOldSHA, "false"}, candidate: {suNewSHA, "false"}}}
	res := suWorker(t, rel, host).stepWorkerSwap(context.Background(), suJob())
	refused(t, res.receipts[0].Evidence, "not in the signed manifest")
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe touched on refusal: %q", got)
	}
}

func TestWorkerSwapRefusesCandidateBuildInfoMismatch(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suOldSHA, "false"}, candidate: {strings.Repeat("cc", 20), "false"}}}
	res := suWorker(t, rel, host).stepWorkerSwap(context.Background(), suJob())
	refused(t, res.receipts[0].Evidence, "build info")
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe touched on refusal: %q", got)
	}
}

// TestWorkerSwapLinkFailureLeavesOldIntact: a failing old-link (a directory
// squats on the .old name) must leave the running binary whole and remove the
// temp — a crash at ANY point never leaves a half file.
func TestWorkerSwapLinkFailureLeavesOldIntact(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	if err := os.Mkdir(exe+".old."+suOldSHA, 0o755); err != nil {
		t.Fatal(err)
	}
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suOldSHA, "false"}, candidate: {suNewSHA, "false"}}}
	res := suWorker(t, rel, host).stepWorkerSwap(context.Background(), suJob())
	refused(t, res.receipts[0].Evidence, "non-file")
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("exe changed on a failed link: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, workerBinaryName+".new."+suJob().JobID)); !os.IsNotExist(err) {
		t.Fatalf("temp not cleaned up: %v", err)
	}
}

// TestWorkerSwapSecondRunDoesNotSwap: the binary already carries the release
// revision — no re-swap, exactly ONE .old file, the exit signal closed on a
// fresh worker (the already path is also a completion).
func TestWorkerSwapSecondRunDoesNotSwap(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "new-binary")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suNewSHA, "false"}, candidate: {suNewSHA, "false"}}}
	w := suWorker(t, rel, host)
	res := w.stepWorkerSwap(context.Background(), suJob())
	ev := res.receipts[0].Evidence
	if ev["already"] != "true" {
		t.Fatalf("receipt = %+v, want already", ev)
	}
	entries, _ := os.ReadDir(dir)
	olds := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), workerBinaryName+".old.") {
			olds++
		}
	}
	if olds != 0 {
		t.Fatalf("%d .old files after an already-swapped run, want 0", olds)
	}
	select {
	case <-w.SwapDone():
		t.Fatal("SwapDone closed on the already-swapped path — the restarted worker would exit again and loop (job de4cf900)")
	default:
	}
	if w.swapPending.Load() {
		t.Fatal("swapPending set on the already-swapped path: nothing was swapped")
	}
}

// TestWorkerSwapPrunesOldAndOrphans: the swap deletes orphan .new files and
// keeps only the newest 3 .old files.
func TestWorkerSwapPrunesOldAndOrphans(t *testing.T) {
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		writeExecutable(t, filepath.Join(dir, workerBinaryName+".old."+strings.Repeat(n, 40)), "stale")
	}
	writeExecutable(t, filepath.Join(dir, workerBinaryName+".new.orphan1"), "orphan")
	writeExecutable(t, filepath.Join(dir, workerBinaryName+".new.orphan2"), "orphan")
	relDir := t.TempDir()
	candidate := filepath.Join(relDir, "updater", workerBinaryName)
	writeExecutable(t, candidate, "new-binary")
	rel := suRel{
		verdict: Verdict{ReleaseID: "v1.2.3", SourceSHA: suNewSHA, ReleaseDir: relDir},
		facts: ReleaseFacts{ReleaseID: "v1.2.3", SourceSHA: suNewSHA,
			Artifacts: map[string]string{"updater/" + workerBinaryName: shaOf("new-binary")}},
	}
	host := suHost{exe: exe, revs: map[string][2]string{
		exe: {suOldSHA, "false"}, candidate: {suNewSHA, "false"}}}
	res := suWorker(t, rel, host).stepWorkerSwap(context.Background(), suJob())
	if res.err != nil {
		t.Fatal(res.err)
	}
	entries, _ := os.ReadDir(dir)
	olds, news := 0, 0
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Name(), workerBinaryName+".old."):
			olds++
		case strings.HasPrefix(e.Name(), workerBinaryName+".new."):
			news++
		}
	}
	if olds != 3 {
		t.Fatalf("%d .old files after the swap, want 3 (prune keeps the newest 3 + the new keep)", olds)
	}
	if news != 0 {
		t.Fatalf("%d orphan .new files after the swap, want 0", news)
	}
}
