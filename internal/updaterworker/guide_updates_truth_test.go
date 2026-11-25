package updaterworker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// The Updates guide page and the install checklists were written from what these two
// steps SHOULD do. Each test here shows what the code DOES and then pins the guide
// sentence, so that the two cannot drift apart without a red test.

func updatesGuide(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../web/src/guide/content/updates.ts")
	if err != nil {
		t.Fatalf("read the Updates guide: %v", err)
	}
	return string(b)
}

func guideMustSay(t *testing.T, want ...string) {
	t.Helper()
	g := updatesGuide(t)
	for _, w := range want {
		if !strings.Contains(g, w) {
			t.Errorf("web/src/guide/content/updates.ts does not say %q", w)
		}
	}
}

func guideMustNotSay(t *testing.T, bad ...string) {
	t.Helper()
	g := updatesGuide(t)
	for _, w := range bad {
		if strings.Contains(g, w) {
			t.Errorf("web/src/guide/content/updates.ts says %q, which is not what the code does", w)
		}
	}
}

// NT8 closed: the EQUAL build id that is already placed is rebuilt and re-placed, and its
// record is a catch-up job with an addon_build receipt that names the builder. Nothing in
// the build id shows it. NT8 open with the same id: nothing is owed.
func TestCatchupNT8ClosedReplacesAnEqualBuildAndTheGuideSaysSo(t *testing.T) {
	r := newRig(t, withVerifiedRunningRelease(boxOldBuild))
	dll := filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")
	writeFakeDLL(t, dll, boxOldBuild) // the same build id is already placed
	before, err := sha256File(dll)
	if err != nil {
		t.Fatal(err)
	}
	if started, _ := r.runCatchup(t); started {
		t.Fatal("NT8 open and the same build id: no catch-up is owed")
	}
	r.set(func() { r.addonConnected = false }) // NT8 closed, or its AddOn not linked yet
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started || !strings.HasPrefix(j.JobID, "catchup-") {
		t.Fatalf("NT8 closed and an EQUAL build id: started=%v job=%q, want a catch-up job (the guide and the install check say so)", started, j.JobID)
	}
	if j.State != updaterjob.StateComplete {
		t.Fatalf("catch-up ended %s (error %q), want complete", j.State, j.Error)
	}
	if after, err := sha256File(dll); err != nil || after == before {
		t.Fatalf("the placed file is unchanged (sha %s, err %v), want the rebuilt one", after, err)
	}
	if id := j.Addon.ExpectedBuild; id != boxOldBuild {
		t.Fatalf("expected build %q, want the EQUAL id %q", id, boxOldBuild)
	}
	builtBy := ""
	for _, rc := range j.Receipts {
		if rc.Step == "addon_build" {
			builtBy = rc.Evidence["built_by"]
		}
	}
	if builtBy == "" {
		t.Fatalf("no built_by in the catch-up's addon_build receipt: %+v", j.Receipts)
	}
	guideMustSay(t,
		"an OLDER build is refused and the AddOn is left alone; an EQUAL one is rebuilt and re-placed",
		"whenever the updater starts while NinjaTrader is closed or its AddOn is not linked yet",
		"catchup-*.json in data/updater/jobs", "built_by")
	guideMustNotSay(t, "a build that is not newer than the running or placed one is refused",
		"nothing touches the AddOn again")
}

// The AddOn build runs UNDER the maintenance hold: a build that fails has installed and
// placed nothing, and the hold is released.
func TestAddonBuildFailureReleasesTheHoldAlreadyTakenAndTheGuideSaysSo(t *testing.T) {
	r := newRig(t, withCSChanged())
	dll := filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")
	before, _ := os.ReadFile(dll)
	builds, held := 0, 0
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ps1 := strings.Contains(name, "powershell") && winFlag(args, "-Out") != ""
		if !ps1 && !strings.Contains(name, "dotnet") {
			return r.box.fakeRunWin(ctx, name, args...)
		}
		builds++
		if s, _ := ReadHoldFor(r.data, boxJobID); s == HoldOurs {
			held++
		}
		return []byte("ERROR CS1002: ; expected"), errors.New("build failed")
	}
	j := r.runToEnd(t)
	r.noViolations(t)
	if builds != 2 || held != 2 {
		t.Fatalf("%d builder runs, %d under this job's hold, want 2 and 2", builds, held)
	}
	if j.State != updaterjob.StateRevertedClean || !strings.Contains(j.Error, "CS1002") {
		t.Fatalf("job %s error %q, want reverted_clean with the compiler error", j.State, j.Error)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldAbsent {
		t.Fatalf("hold %s after a failed build, want it released", s)
	}
	if after, _ := os.ReadFile(dll); string(after) != string(before) || (j.Addon != nil && j.Addon.BackupDir != "") || j.IdentityBefore != nil {
		t.Fatalf("a failed build placed or activated something: addon=%+v identity_before=%+v", j.Addon, j.IdentityBefore)
	}
	guideMustSay(t, "The build runs after the maintenance hold, the flat-and-SIM gate and the database backup.",
		"If both builds fail, nothing is installed or placed, the maintenance hold already taken is released and the job ends “reverted clean”")
	guideMustNotSay(t, "REFUSED before anything is installed or held", "installed or held")
}

// The readiness item is an API answer only, and it says less than "the build will work".
func TestReadinessGuideSaysOnlyWhatTheProbeTests(t *testing.T) {
	guideMustSay(t, "only GET /api/updates/readiness, no page shows it",
		"it turns red when NinjaTrader.Core or the compiler cannot be used; it does not test the release’s own AddOn source, and it does not test the updater")
	guideMustNotSay(t, "it is red when the real build would be")
	err := filepath.WalkDir("../../web/src", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, "/guide/content/") {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(b), "updates/readiness") {
			t.Errorf("%s uses updates/readiness: the guide says no page shows it", p)
		}
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The worker_swap receipt says ok:true for a swap that did NOT happen too: it never fails
// the job. Only its evidence tells (new_sha, refused, self_update).
func TestWorkerSwapReceiptOkAloneProvesNothingAndTheGuideSaysSo(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, workerBinaryName)
	writeExecutable(t, exe, "old-binary")
	for name, c := range map[string]struct{ knob, key string }{
		"refused": {"1", "refused"}, // the release facts are not the job's: refused
		"off":     {"0", "self_update"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("VL_UPDATER_SELF_UPDATE", c.knob)
			w := suWorker(t, suRel{facts: ReleaseFacts{Artifacts: map[string]string{}}},
				suHost{exe: exe, revs: map[string][2]string{exe: {suOldSHA, "false"}}})
			res := w.stepWorkerSwap(context.Background(), suJob())
			if res.err != nil || len(res.receipts) != 1 {
				t.Fatalf("res = %+v", res)
			}
			rc := res.receipts[0]
			if !rc.OK || rc.Evidence[c.key] == "" || rc.Evidence["new_sha"] != "" {
				t.Fatalf("receipt ok=%v evidence=%v, want ok with %s set and no new_sha", rc.OK, rc.Evidence, c.key)
			}
			if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
				t.Fatalf("exe = %q, want the old updater", got)
			}
		})
	}
	guideMustSay(t, "ok: true in the worker_swap receipt alone proves nothing",
		"new_sha equal to the release’s source sha, no refused and no self_update")
}
