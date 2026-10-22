package updaterworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// ── feat/addon-catchup (release #35) ─────────────────────────────────────────
//
// The owner's "why do I have to copy to NinjaTrader again": an idle worker (at
// start, and after every install) compares the AddOn build NT8 actually runs
// with the INSTALLED release's AddOn build, and rebuilds it with no hands.

// m355 pin (4, case 2): a verified artifact whose build EQUALS the running
// AddOn is a no-op — even though the install tree's own ninjascript is STALE
// (it is never read). MUTANT: read oldInstallBuildID() in catchupStatus →
// the stale install tree starts a catch-up → RED.
func TestCatchupSkipsWhenTheAddOnIsCurrent(t *testing.T) {
	r := newRig(t, withVerifiedRunningRelease(boxOldBuild))
	writeFile(t, filepath.Join(r.inst, "ninjascript", "VLTraderTCPClient.cs"),
		"// C# stale\nprivate const string VL_BUILD_ID = \""+boxNewBuild+"\";\n")
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a current AddOn (verified artifact == running) must not start a catch-up")
	}
}

// m355 pin (4, case 1): a stale InstallDir/ninjascript with NO verified
// installed-release artifact → the catch-up is SKIPPED and bin\Custom is
// untouched (InstallDir/ninjascript is never a fallback). MUTANT: fall back to
// InstallDir/ninjascript in addonConfig → the stale DLL is placed → RED.
func TestCatchupSkipsWhenNoVerifiedArtifact(t *testing.T) {
	r := newRig(t) // no withVerifiedRunningRelease → no artifact for the running build
	writeFile(t, filepath.Join(r.inst, "ninjascript", "VLTraderTCPClient.cs"),
		"// C# stale\nprivate const string VL_BUILD_ID = \""+boxNewBuild+"\";\n")
	r.set(func() { r.addonBuild = boxOldBuild }) // NT8 runs the OLD AddOn
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a stale install tree with no verified artifact must NOT start a catch-up")
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); !os.IsNotExist(err) {
		t.Fatalf("bin\\Custom must be untouched (stat err %v)", err)
	}
}

// m355 pin (3): NEVER DOWNGRADE — a verified artifact that ships an OLDER build
// than the running AddOn is refused at the decision, bin\Custom untouched.
// MUTANT: drop the compareBuildID guard in catchupStatus → the downgrade is
// placed → RED.
func TestCatchupRefusesDowngrade(t *testing.T) {
	r := newRig(t, withVerifiedRunningRelease("2026-10-10-a"))
	r.set(func() { r.addonBuild = "2026-10-20-b" }) // the box already runs a NEWER build
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a catch-up that would DOWNGRADE the AddOn must not start")
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); !os.IsNotExist(err) {
		t.Fatalf("bin\\Custom must be untouched (stat err %v)", err)
	}
}

// m355 pin (3, NT8 closed): with no ack, the currently PLACED DLL's embedded
// build is the downgrade reference — a verified artifact OLDER than it is
// refused at the place step and the job ends reverted_clean.
func TestCatchupRefusesDowngradeAgainstPlacedDLL(t *testing.T) {
	r := newRig(t, withVerifiedRunningRelease("2026-10-10-a"))
	// a NEWER AddOn is already placed (what the next NT8 start loads)
	writeFakeDLL(t, filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll"), "2026-10-20-b")
	r.set(func() { r.addonConnected = false }) // NT8 closed: no ack to compare
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("the catch-up starts (no ack), the place step must refuse the downgrade")
	}
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("catch-up ended %s/%s (error %q), want reverted_clean (downgrade refused at place)", j.State, j.Phase, j.Error)
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); err != nil {
		t.Fatalf("bin\\Custom must still hold the placed DLL (untouched): %v", err)
	}
}

// On worker start, an idle worker whose AddOn is stale begins a catch-up.
func TestCatchupStartsOnWorkerStart(t *testing.T) {
	r := newRig(t, withStaleAddon())
	if _, err := r.w.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// maybeStartCatchup ran synchronously inside Start: a catchup-* job is on
	// disk (the runner goroutine may have driven it to completion meanwhile).
	jobs, _, err := updaterjob.ListTolerant(r.data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range jobs {
		if j.Catchup {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Start must begin a catch-up when the AddOn is stale")
	}
}

// NT8 open + stale but NOT flat/SIM → no catch-up; the worker waits for the
// next start or install (never forces NT8 down while a position is live).
func TestCatchupSkipsWhenNotFlatOrSim(t *testing.T) {
	r := newRig(t, withStaleAddon())
	r.set(func() { r.simTradeable = false }) // a bound account is not SIM-tradeable
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a stale AddOn with a non-flat/non-SIM box must not start a catch-up")
	}
}

// NT8 open + stale → build → place → restart → reconnect → complete. Nothing is
// activated (this is not an install).
func TestCatchupRebuildsAndRestartsWhenStale(t *testing.T) {
	r := newRig(t, withStaleAddon())
	starts := startCount(r)
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("a stale AddOn must start a catch-up")
	}
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("catch-up ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	if j.Addon == nil || !j.Addon.Restarted {
		t.Fatalf("the catch-up with NT8 open must have restarted NT8: %+v", j.Addon)
	}
	if *starts != 1 {
		t.Fatalf("NT8 restarts = %d, want exactly 1", *starts)
	}
	if r.addonBuild != boxNewBuild {
		t.Fatalf("the AddOn now reports %q, want the install's %q", r.addonBuild, boxNewBuild)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("a catch-up must never activate a release (activate ran %d times)", r.callCount("activate"))
	}
	// The DLL is placed in bin/Custom.
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); err != nil {
		t.Fatalf("the catch-up did not place the AddOn: %v", err)
	}
}

// NT8 closed + stale → build + place only (no restart, no reconnect wait): the
// new DLL loads the next time NinjaTrader starts.
func TestCatchupBuildAndPlaceOnlyWhenNT8Closed(t *testing.T) {
	r := newRig(t, withStaleAddon())
	r.set(func() { r.addonConnected = false }) // NT8 closed: no hello
	starts := startCount(r)
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("a stale AddOn with NT8 closed must start a catch-up")
	}
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("catch-up ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	if j.Addon == nil || j.Addon.Restarted {
		t.Fatalf("the catch-up with NT8 closed must NOT restart NT8: %+v", j.Addon)
	}
	if *starts != 0 {
		t.Fatalf("NT8 restarts = %d, want 0 (NT8 closed)", *starts)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("a catch-up must never activate a release (activate ran %d times)", r.callCount("activate"))
	}
}

// After an install finishes, an idle worker whose AddOn cannot be proven
// current (NT8 closed — no hello to compare) starts a catch-up on its own: the
// partner's first-install handoff.
func TestCatchupRunsAfterAnInstall(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown), withInstalledReleaseVerdict()) // NT8 closed through the whole install
	r.install()
	if err := r.drive(); err != nil {
		t.Fatalf("drive: %v", err)
	}
	if j := r.job(); j.State != updaterjob.StateComplete {
		t.Fatalf("install ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	// finished() must have started a catch-up.
	r.w.mu.Lock()
	active := r.w.active
	r.w.mu.Unlock()
	if active == "" || !strings.HasPrefix(active, "catchup-") {
		t.Fatalf("after an install with NT8 closed, the worker must start a catch-up; active=%q", active)
	}
	if err := r.driveActive(); err != nil {
		t.Fatalf("drive catch-up: %v", err)
	}
	j, err := updaterjob.Read(r.data, active)
	if err != nil {
		t.Fatal(err)
	}
	r.noViolations(t)
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("catch-up ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	// A completed catch-up must not start another: the worker is idle again.
	r.w.mu.Lock()
	active = r.w.active
	r.w.mu.Unlock()
	if active != "" {
		t.Fatalf("a completed catch-up must not start another; active=%q", active)
	}
}
