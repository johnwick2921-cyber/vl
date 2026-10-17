package updaterworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// ── release #35 partner-first-install e2e (DS-106) ──────────────────────────
//
// The partner's OLD worker ran an NT8-closed install (bot side done, "F5 owed"):
// Custom.dll still holds the AddOn type, VL*.cs sit in AddOns, no VL.AddOn.dll,
// and the accounts are persisted SIM. The NEW worker then starts (after
// worker_swap) and must hand the box over with NO F5 and NO human.
//
// These tests encode the SPEC for each case; a RED here names a real gap in
// feat/addon-catchup for the CTO / DS-104 to close.

// withPartnerFirstInstall stages the partner's exact first-install box on top
// of withStaleAddon (the install ships boxNewBuild; NT8 — if up — runs the old
// build). Persisted SIM accounts are already the rig default (simTradeable and
// absentSim are true).
func withPartnerFirstInstall() rigOpt {
	return func(b *box) {
		withStaleAddon()(b)
		writeFile(b.t, filepath.Join(b.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"),
			"fake Custom.dll holding the type "+addonTypeName+"\n")
		writeFile(b.t, filepath.Join(b.nt8docs, "bin", "Custom", "AddOns", "VLTrader.cs"), "// C# v1\n")
	}
}

// nt8Closed marks the fake NT8 as closed: no hello, tasklist sees no process.
func nt8Closed(b *box) {
	b.mu.Lock()
	b.addonConnected = false
	b.nt8Running = false
	b.mu.Unlock()
}

func customDLLText(t *testing.T, r *rig) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"))
	if err != nil {
		return ""
	}
	return string(b)
}

func addonSourcesInAddOns(r *rig) []string {
	matches, _ := filepath.Glob(filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VL*.cs"))
	return matches
}

// (a) NT8 still closed → the catch-up builds + migrates + rebuilds Custom.dll +
// places, with NO F5 and NO human. The new DLL loads at the next NT8 start.
func TestPartnerFirstInstallNT8ClosedCatchupMigratesRebuildsPlaces(t *testing.T) {
	r := newRig(t, withPartnerFirstInstall())
	nt8Closed(r.box)
	starts := startCount(r)
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("the partner's stale install with NT8 closed must start a catch-up")
	}
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("catch-up ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	if *starts != 0 {
		t.Fatalf("NT8 restarts = %d, want 0 (NT8 closed — no restart, no F5, no human)", *starts)
	}
	if strings.Contains(customDLLText(t, r), addonTypeName) {
		t.Fatal("Custom.dll must be rebuilt WITHOUT the AddOn type — the no-F5 rebuild is owed")
	}
	if srcs := addonSourcesInAddOns(r); len(srcs) != 0 {
		t.Fatalf("VL*.cs still in AddOns: %v — the migration did not run", srcs)
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); err != nil {
		t.Fatalf("the catch-up did not place the AddOn: %v", err)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("a catch-up must never activate a release (activate ran %d times)", r.callCount("activate"))
	}
}

// (b) NT8 opened before the catch-up, flat + SIM → close → work while closed →
// start → reconnect with the new build.
func TestPartnerFirstInstallNT8OpenCatchupRestartsAndMigrates(t *testing.T) {
	r := newRig(t, withPartnerFirstInstall()) // NT8 open, stale, flat+SIM (defaults)
	starts := startCount(r)
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("the partner's stale install with NT8 open + flat/SIM must start a catch-up")
	}
	if j.State != updaterjob.StateComplete || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("catch-up ended %s/%s (error %q), want complete", j.State, j.Phase, j.Error)
	}
	if *starts != 1 {
		t.Fatalf("NT8 restarts = %d, want exactly 1 (close → work while closed → start)", *starts)
	}
	if strings.Contains(customDLLText(t, r), addonTypeName) {
		t.Fatal("Custom.dll must be rebuilt WITHOUT the AddOn type")
	}
	if srcs := addonSourcesInAddOns(r); len(srcs) != 0 {
		t.Fatalf("VL*.cs still in AddOns: %v — the migration did not run", srcs)
	}
	if r.box.addonBuild != boxNewBuild {
		t.Fatalf("after restart the ack build = %q, want the install's %q", r.box.addonBuild, boxNewBuild)
	}
}

// (c) NT8 open but NOT flat → the catch-up waits and never touches NT8.
func TestPartnerFirstInstallNT8OpenNotFlatCatchupWaits(t *testing.T) {
	r := newRig(t, withPartnerFirstInstall()) // NT8 open, stale
	r.set(func() { r.flat = false })          // a live position — never force NT8 down
	starts := startCount(r)
	started, _ := r.runCatchup(t)
	r.noViolations(t)
	if started {
		t.Fatal("a stale AddOn with a non-flat box must NOT start a catch-up")
	}
	if *starts != 0 {
		t.Fatalf("NT8 restarts = %d, want 0 (never touch a non-flat NT8)", *starts)
	}
	if !r.nt8Running {
		t.Fatal("NT8 must still be running")
	}
}

// (d) the Custom.dll rebuild fails → old files restored, NT8 left as it was,
// a plain message, F5 asked as the last resort.
func TestPartnerFirstInstallCustomRebuildFailureAsksF5(t *testing.T) {
	r := newRig(t, withPartnerFirstInstall())
	nt8Closed(r.box)
	// Make the no-F5 Custom rebuild FAIL: the ps1 reports a public type-list
	// diff (indicators/strategies/Apex would be disturbed — never accepted).
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name, "powershell") && hasWinFlag(args, "-Custom") {
			return []byte("VL_CUSTOM_TYPES_DIFF\n"), nil
		}
		return r.box.fakeRunWin(ctx, name, args...)
	}
	starts := startCount(r)
	started, j := r.runCatchup(t)
	r.noViolations(t)
	if !started {
		t.Fatal("the partner's stale install with NT8 closed must start a catch-up")
	}
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("catch-up ended %s/%s (error %q), want reverted_clean with the F5 blocker", j.State, j.Phase, j.Error)
	}
	if !strings.Contains(j.Error, addonMigrationBlocker) {
		t.Fatalf("error %q must carry the F5-as-last-resort blocker", j.Error)
	}
	if *starts != 0 {
		t.Fatalf("NT8 restarts = %d, want 0", *starts)
	}
	if !strings.Contains(customDLLText(t, r), addonTypeName) {
		t.Fatal("the old Custom.dll must be untouched after a failed rebuild")
	}
	if srcs := addonSourcesInAddOns(r); len(srcs) == 0 {
		t.Fatal("VL*.cs must still be in AddOns after a failed rebuild (nothing migrated)")
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under a failed rebuild", r.callCount("activate"))
	}
}
