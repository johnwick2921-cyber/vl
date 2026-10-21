package updaterworker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vl/internal/addonbuild"
	"vl/internal/updaterjob"
)

// ── W-ADDON-ONE-CLICK e2e (DS-103) — the FULL job against a fake NT8 ────────
//
// The REAL runner, job file, hold file, lock and loopback app are the production
// ones (the rig); only dotnet.exe, powershell.exe and the AddOn wire are faked.
// These are the happy-path / refusal cases that do not need DS-102's
// self-recovery (feat/update-self-recovery); the timeout-rollback (c) and the
// transitional-connection wait (d) stay PENDING-DS-102.

// startCount wraps the fake win seam and counts powershell Start-Process calls
// (one NT8 restart = exactly one Start-Process).
func startCount(r *rig) *int {
	n := 0
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if strings.Contains(a, "Start-Process") {
				n++
			}
		}
		return prev(ctx, name, args...)
	}
	return &n
}

// (a) NT8 open + changed AddOn → completes with no human: no park, no resume,
// NT8 restarted ONCE, the new build acked.
func TestE2EAddonChangedCompletesWithOneRestartNoPark(t *testing.T) {
	r := newRig(t, withCSChanged())
	starts := startCount(r)
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete — the one-click flow never parks", j.State, j.Error)
	}
	if j.Addon == nil || !j.Addon.Restarted {
		t.Fatalf("AddOn must be restarted exactly once, addon=%+v", j.Addon)
	}
	if j.Addon.DllPath == "" || j.Addon.Sha256 == "" {
		t.Fatalf("the AddOn must be built: %+v", j.Addon)
	}
	// GAP 2: the PRIMARY path is the no-SDK ps1 build, and the job records it.
	if j.Addon.BuiltBy != addonbuild.BuiltByRoslynPs1 {
		t.Fatalf("the AddOn must be built by the no-SDK ps1 path, got %q", j.Addon.BuiltBy)
	}
	if *starts != 1 {
		t.Fatalf("NT8 restarted %d times, want exactly 1", *starts)
	}
	// the reconnect proved the NEW build on a NEW connection
	if r.box.addonBuild != boxNewBuild {
		t.Fatalf("after restart the ack build = %q, want %q", r.box.addonBuild, boxNewBuild)
	}
	if r.box.addonSeq == 0 {
		t.Fatal("accept_seq never bumped — the reconnect did not prove a new connection")
	}
}

// (b) build failure → reverted_clean: nothing placed, hold + lock released.
func TestE2EAddonBuildFailureRevertsCleanNothingPlaced(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Fail BOTH builders: the primary no-SDK ps1 path AND the dotnet
		// fallback — a build failure must refuse the update before any place.
		if strings.Contains(name, "dotnet") {
			return []byte("Program.cs(1,1): error CS1002: ; expected"), errors.New("dotnet build failed")
		}
		if strings.Contains(name, "powershell") && winFlag(args, "-Out") != "" {
			return []byte("ERROR CS1002: ; expected"), errors.New("ps1 build failed")
		}
		return r.box.fakeRunWin(ctx, name, args...)
	}
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("job %s (error %q), want reverted_clean", j.State, j.Error)
	}
	if j.Addon != nil && j.Addon.BackupDir != "" {
		t.Fatalf("a failed build still placed the AddOn: %+v", j.Addon)
	}
	if r.hold().Present {
		t.Fatalf("hold must be released after reverted_clean, got %+v", r.hold())
	}
	if r.box.lockHolder != "" {
		t.Fatalf("lock must be released after reverted_clean, holder %q", r.box.lockHolder)
	}
}

// (e) NT8 closed → completes, the DLL placed, NO restart (nothing owed).
//
// DS-104: the closed-NT8 one-click path is wired — decideNT8Absent routes a
// changed AddOn to nt8_updated, so addon_build + addon_place run and only the
// restart is skipped.
func TestE2EAddonNT8ClosedCompletesDLLPlacedNoRestart(t *testing.T) {
	r := newRig(t, withCSChanged(), withNT8Down(absentDown))
	starts := startCount(r)
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete", j.State, j.Error)
	}
	if j.Addon == nil || j.Addon.DllPath == "" || j.Addon.Sha256 == "" {
		t.Fatalf("the DLL must be built and placed: %+v", j.Addon)
	}
	if j.Addon.Restarted {
		t.Fatalf("NT8 was closed — it must NOT be restarted: %+v", j.Addon)
	}
	if *starts != 0 {
		t.Fatalf("NT8 restarted %d times with NT8 closed, want 0", *starts)
	}
}
