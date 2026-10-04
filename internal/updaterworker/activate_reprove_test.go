package updaterworker

import (
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// PIN (#206 review fold, runner.go:275): the activate and rollback kills used
// the re-read MainPID without re-proving what preflight proved ONCE about the
// process — its exe, the install binary's build, and health. A hotfix deploy
// or a restart during a 30-minute park silently replaces the process; the
// kill is start-ticks-guarded so it hits the unit's CURRENT process, but that
// process is no longer the one preflight proved. The proof now runs right
// before the kill.
func TestActivateReProvesTheProcessBeforeTheKill(t *testing.T) {
	r := newRig(t, withCSChanged()) // park at nt8_updated: time for the swap
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateNT8Updated {
		t.Fatalf("no park: %s/%s", j.State, j.Phase)
	}
	// During the park the unit is restarted onto another build and the
	// install binary is hotfixed — the preflighted process is gone.
	r.set(func() { r.exe = "/opt/somewhere-else/other-bin" })
	writeFile(r.t, filepath.Join(r.inst, "vl-bin"), binaryBody(strings.Repeat("c3", 20)))
	r.f5()
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume verb: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	r.noViolations(t)
	j := r.job()
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s: a process nobody re-proved was killed and the release activated\n%v", j.State, states(j))
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under an unproved process", r.callCount("activate"))
	}
	if !strings.Contains(j.Error, "not the install's") {
		t.Fatalf("error %q must name the wrong exe", j.Error)
	}
}
