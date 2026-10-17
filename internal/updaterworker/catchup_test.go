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

// The catch-up does nothing when the running AddOn already matches the install.
func TestCatchupSkipsWhenTheAddOnIsCurrent(t *testing.T) {
	r := newRig(t) // running boxOldBuild == install's boxOldBuild
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a current AddOn must not start a catch-up")
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
	r := newRig(t, withNT8Down(absentDown)) // NT8 closed through the whole install
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
