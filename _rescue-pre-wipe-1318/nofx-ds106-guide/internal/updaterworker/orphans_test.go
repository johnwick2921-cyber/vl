package updaterworker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// PIN (D8b, CTO 1790279155144 (4) + 1790280128238): a fetch killed between
// its rename and its verdict link leaves <release_root>/<sha> with no verdict
// — and its own pending marker. That dir is NEVER activated (no verdict ⇒ the
// install verb refuses; a verdict gone mid-job ⇒ the downloaded re-proof
// refuses), and the next fetch's recovery QUARANTINES it (renamed, bytes
// kept, never deleted) so a clean re-fetch can take the name. A <sha> dir
// WITHOUT the marker is not the fetch's — untouched (U3's "never overwritten"
// refusal stands, TestVerdictWrittenOnlyAfterEveryCheck); a marker whose dir
// a verdict names (the fetch finished) or whose dir never landed is just
// removed. An unreadable verdict, or a fetch in flight (the release-root
// lock), refuses the whole recovery.
func TestInterruptedFetchIsQuarantinedAndNeverActivated(t *testing.T) {
	r := newRig(t)
	root := filepath.Dir(r.relDir) // <tmp>/releases: the release dir exists, no verdict names it
	r.verdictMissing = true        // the re-proof finds no verdict, as the real reader would

	// never activated: the install verb refuses; no job file is born
	if resp := r.w.Handle(updaterwireInstallOf(boxJobID)); resp.OK || resp.Error != "release not verified" {
		t.Fatalf("install of a verdict-less release = %+v", resp)
	}
	if _, err := updaterjob.Read(r.data, boxJobID); !errors.Is(err, updaterjob.ErrNotFound) {
		t.Fatalf("a refused install wrote a job: %v", err)
	}
	// a verdict that vanishes after the install: the job's own re-proof refuses
	r.verdictMissing = false
	r.install()
	r.verdictMissing = true
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateRefused || r.callCount("activate") != 0 {
		t.Fatalf("a job whose verdict vanished ended %s (activate ran %d times)", j.State, r.callCount("activate"))
	}

	// the neighbours: a foreign verdict-less dir (no marker — maybe the
	// running release placed by hand), a finished fetch whose marker survived
	// (a verdict names its dir), a marker whose rename never happened, a
	// staging leftover and a symlink
	sha := func(p string) string { return strings.Repeat(p, 20) }
	foreign := filepath.Join(root, sha("c3"))
	finished := filepath.Join(root, sha("e5"))
	writeFile(t, filepath.Join(foreign, "nofx-bin"), binaryBody(sha("c3")))
	writeFile(t, filepath.Join(finished, "nofx-bin"), binaryBody(sha("e5")))
	writeFile(t, filepath.Join(r.data, "updater", "verdicts", "v1.1.0.json"), `{"schema":1,"release_id":"v1.1.0","release_dir":"`+finished+`"}`)
	writeFile(t, filepath.Join(root, ".fetch-v1.3.0-123", "x"), "staging")
	os.Symlink(finished, filepath.Join(root, sha("f6")))
	for _, s := range []string{boxNew, sha("e5"), sha("d4")} { // d4: killed before its rename
		if err := MarkFetchPending(root, s); err != nil {
			t.Fatal(err)
		}
	}

	// a fetch in flight holds the lock: refused, nothing moved
	unlock, err := LockReleaseRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := QuarantineInterruptedFetches(root, r.data, r.clock.Now()); !errors.Is(err, ErrFetchInFlight) || len(moved) != 0 {
		t.Fatalf("recovery during a fetch = %v, %v", moved, err)
	}
	unlock()
	// an unreadable verdict: refused, nothing moved
	bad := filepath.Join(r.data, "updater", "verdicts", "v0.9.0.json")
	writeFile(t, bad, `{"release_id":"v0.9.0"}`)
	if moved, err := QuarantineInterruptedFetches(root, r.data, r.clock.Now()); err == nil || len(moved) != 0 {
		t.Fatalf("recovery with an unreadable verdict = %v, %v", moved, err)
	}
	os.Remove(bad)

	moved, err := QuarantineInterruptedFetches(root, r.data, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || !strings.HasPrefix(moved[0], filepath.Join(root, ".orphan-"+boxNew+"-")) {
		t.Fatalf("quarantined %v, want exactly the interrupted fetch's %s", moved, r.relDir)
	}
	if _, err := os.Stat(r.relDir); !os.IsNotExist(err) {
		t.Fatalf("the interrupted fetch's dir still blocks the name: %v", err)
	}
	if got, _ := parseBinaryBody(filepath.Join(moved[0], "nofx-bin")); got != boxNew {
		t.Fatal("the quarantined release lost its bytes")
	}
	for _, keep := range []string{foreign, finished, filepath.Join(root, ".fetch-v1.3.0-123"), filepath.Join(root, sha("f6"))} {
		if _, err := os.Lstat(keep); err != nil {
			t.Fatalf("%s was touched: %v", keep, err)
		}
	}
	for _, s := range []string{boxNew, sha("e5"), sha("d4")} {
		if _, err := os.Lstat(filepath.Join(root, ".pending-"+s)); !os.IsNotExist(err) {
			t.Fatalf("marker for %s survives the recovery", s[:4])
		}
	}
	// idempotent: nothing more to move
	if again, err := QuarantineInterruptedFetches(root, r.data, r.clock.Now()); err != nil || len(again) != 0 {
		t.Fatalf("second recovery = %v, %v", again, err)
	}
}
