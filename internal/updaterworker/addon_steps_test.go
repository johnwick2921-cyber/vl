package updaterworker

import (
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// ── W-ADDON-ONE-CLICK: the three guards ─────────────────────────────────────
//
// Each guard is a separate refusal, each with its own terminal state and a
// distinct error, so the page can tell the owner WHY the update stopped.

// Guard #1: never restart NT8 unless every bound account is flat and
// SIM-tradeable. Both halves are checked independently: flat failing blocks,
// and a flat-but-non-SIM account blocks.
func TestFlatSIMGateBlocksTheRestart(t *testing.T) {
	for _, c := range []struct {
		name   string
		setup  func(r *rig)
		want   string
	}{
		{name: "not flat", setup: func(r *rig) { r.set(func() { r.flat = false }) }, want: "flat+SIM gate"},
		{name: "flat but a bound account is not SIM", setup: func(r *rig) { r.set(func() { r.simTradeable = false }) }, want: "sim_accounts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, withCSChanged())
			r.hookAt("nt8_restarted/started", func() { c.setup(r) })
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != updaterjob.StateRevertedClean || j.Addon == nil || j.Addon.Restarted {
				t.Fatalf("job %s addon=%+v, want reverted_clean with no restart", j.State, j.Addon)
			}
			if !strings.Contains(j.Error, c.want) {
				t.Fatalf("error %q must contain %q", j.Error, c.want)
			}
			if r.callCount("activate") != 0 {
				t.Fatalf("activate ran %d times under a non-flat/non-SIM account", r.callCount("activate"))
			}
		})
	}
}

// Guard #2: a double-loaded AddOn (the type exists in BOTH
// NinjaTrader.Custom.dll and VL.AddOn.dll — the one-time migration hazard) is
// refused BEFORE anything is placed, and the job ends reverted_clean.
func TestDoubleLoadedAddOnIsRefused(t *testing.T) {
	r := newRig(t, withCSChanged())
	// The hazard: NT8 would load the AddOn twice.
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll"), "type "+addonTypeName+"\n")
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean || !strings.Contains(j.Error, "double-load") {
		t.Fatalf("job %s (error %q), want reverted_clean: the double-load refused", j.State, j.Error)
	}
	if j.Addon != nil && j.Addon.BackupDir != "" {
		t.Fatalf("a refused double-load still placed the AddOn: %+v", j.Addon)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under a double-loaded AddOn", r.callCount("activate"))
	}
}

// Guard #3: after the restart, the AddOn must reconnect with the manifest's
// build on a NEW connection (accept_seq). A reconnect that never arrives is a
// timeout — never a silent pass: the job rolls back.
func TestAddonReconnectTimeoutRollsBack(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.noReconnect = true }) // NT8 restarts but the AddOn never reconnects
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack || !strings.Contains(j.Error, "did not reconnect") {
		t.Fatalf("job %s (error %q), want rolled_back: the reconnect timed out", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times before the reconnect proved", r.callCount("activate"))
	}
}
