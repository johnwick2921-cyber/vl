package updaterworker

import (
	"os"
	"path/filepath"
	"testing"

	"vl/internal/updaterjob"
)

// R5 single binary: the boot-watch inputs are predicted from the BINARY the
// release carries (vl-bin → vl_ log), never from which
// log file happens to exist.

func TestLogPrefixFollowsTheBinary(t *testing.T) {
	for bin, want := range map[string]string{
		"/i/vl-bin":                "vl_",
		"/i/vl-bin.old.00090003": "vl_", // anything but vl-bin is still the vl name
	} {
		if got := logPrefixForBinary(bin); got != want {
			t.Errorf("logPrefixForBinary(%s) = %s, want %s", bin, got, want)
		}
	}
}

// The predictor end-to-end: run a full boot on a vl-bin release; the
// Watch's LogPath must carry the release's prefix, the
// offset must be 0 (the fake writes the boot line only AFTER Activate
// returned — the log the worker predicted was ABSENT while it predicted it),
// and the boot line must land in exactly that file.
func TestBootWatchPredictsTheLogFromTheReleaseBinary(t *testing.T) {
	for _, bin := range []string{"vl-bin"} {
		t.Run(bin, func(t *testing.T) {
			r := newRig(t, withBinary(bin))
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.Phase != updaterjob.PhaseDone {
				t.Fatalf("job %s/%s (error %q), want a done phase", j.State, j.Phase, j.Error)
			}
			prefix := "vl_"
			want := filepath.Join(r.data, prefix+"2026-09-24.log")
			if len(r.watchOpts) == 0 {
				t.Fatalf("Watch never ran (job %s/%s, error %q)", j.State, j.Phase, j.Error)
			}
			if got := r.watchOpts[0].LogPath; got != want {
				t.Fatalf("Watch log = %s, want the predicted %s", got, want)
			}
			if j.LogOffset == nil || *j.LogOffset != 0 {
				t.Fatalf("the persisted offset is %v, want 0 (the predicted log did not exist when setBootWatch measured it)", j.LogOffset)
			}
			if _, err := os.Stat(want); err != nil {
				t.Fatalf("the boot line never landed in the predicted file %s: %v", want, err)
			}
		})
	}
}

func TestArchiveBinaryExactlyOne(t *testing.T) {
	write := func(t *testing.T, names ...string) string {
		dir := t.TempDir()
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("\x7fELF"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	openRoot := func(t *testing.T, dir string) *os.Root {
		r, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		return r
	}
	t.Run("vl present", func(t *testing.T) {
		root := openRoot(t, write(t, "vl-bin"))
		if got, err := binaryInRoot(root); err != nil || got != "vl-bin" {
			t.Fatalf("binaryInRoot = %q/%v, want vl-bin", got, err)
		}
	})
	t.Run("missing keeps the old lenient reading", func(t *testing.T) {
		root := openRoot(t, write(t))
		if got, err := binaryInRoot(root); err != nil || got != "vl-bin" {
			t.Fatalf("binaryInRoot on neither = %q/%v, want the lenient vl-bin", got, err)
		}
	})
}

// A rollback from a vl-bin release back to the snapshot: the activate
// Watch points at the release's vl_ log, and the rollback Watch is re-predicted
// from the SNAPSHOT's binary — the vl_ log.
func TestRollbackFromVlReleaseRePredictsFromTheSnapshotBinary(t *testing.T) {
	r := newRig(t, withReleaseBinary("vl-bin"))
	r.watchFail[boxNew] = true
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("job %s/%s (error %q), want rolled_back/done", j.State, j.Phase, j.Error)
	}
	if len(r.watchOpts) != 2 {
		t.Fatalf("Watch ran %d times, want 2 (activate + rollback)", len(r.watchOpts))
	}
	if got := filepath.Base(r.watchOpts[0].LogPath); got != "vl_2026-09-24.log" {
		t.Fatalf("the activate Watch log = %s, want the release's vl_ file", got)
	}
	if got := filepath.Base(r.watchOpts[1].LogPath); got != "vl_2026-09-24.log" {
		t.Fatalf("the rollback Watch log = %s, want the snapshot's vl_ file", got)
	}
	if _, err := os.Stat(filepath.Join(r.data, "vl_2026-09-24.log")); err != nil {
		t.Fatalf("the vl boot line never landed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.data, "vl_2026-09-24.log")); err != nil {
		t.Fatalf("the vl rollback boot line never landed: %v", err)
	}
}

// R5: the install side reads vl-bin.
func TestInstallSideIsVl(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"vl-bin"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("\x7fELF"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tgt := Target{InstallDir: dir}
	if got := tgt.InstallBinaryPath(); filepath.Base(got) != "vl-bin" {
		t.Fatalf("InstallBinaryPath = %s, want the vl-bin install side", got)
	}
	if got := tgt.InstallRelease("x").Binary; filepath.Base(got) != "vl-bin" {
		t.Fatalf("InstallRelease.Binary = %s, want vl-bin", got)
	}
}
