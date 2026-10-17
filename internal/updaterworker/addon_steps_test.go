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

// Guard #1 missing-leg refusal (#3 fold): the flat+SIM gate must REFUSE when
// any one of addon_census / ledger_exposure / sim_accounts is absent from the
// gate view — a missing leg is missing evidence, never a pass.
func TestFlatSIMGateRefusesWhenALegIsMissing(t *testing.T) {
	for _, leg := range []string{"addon_census", "ledger_exposure", "sim_accounts"} {
		t.Run(leg, func(t *testing.T) {
			r := newRig(t, withCSChanged())
			// Omit the leg only at the restart step's gate read — the earlier
			// drain/gate steps passed with every leg present.
			r.hookAt("nt8_restarted/started", func() { r.set(func() { r.omitLeg = leg }) })
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != updaterjob.StateRevertedClean || j.Addon == nil || j.Addon.Restarted {
				t.Fatalf("job %s addon=%+v, want reverted_clean with no restart", j.State, j.Addon)
			}
			if !strings.Contains(j.Error, leg) || !strings.Contains(j.Error, "did not run") {
				t.Fatalf("error %q must name the missing leg %q", j.Error, leg)
			}
			if r.callCount("activate") != 0 {
				t.Fatalf("activate ran %d times with a missing %s leg", r.callCount("activate"), leg)
			}
		})
	}
}

// Guard #3 #5a: a NEW build_id on the OLD accept_seq is not a reconnect — the
// reconnect step itself must fail with its own "did not reconnect" error.
func TestAddonReconnectNewBuildOldSeqRollsBack(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.reconnectSameSeq = true }) // new build, but the connection never changed
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack || !strings.Contains(j.Error, "did not reconnect") {
		t.Fatalf("job %s (error %q), want rolled_back: a new build on the old connection is not a reconnect", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times on an unproven reconnect", r.callCount("activate"))
	}
}

// Guard #3 #5b: a NEW accept_seq on the OLD build_id is NT8 coming back on the
// old AddOn — the reconnect step itself must fail with "did not reconnect".
func TestAddonReconnectOldBuildNewSeqRollsBack(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.reconnectOldBuild = true }) // NT8 came back on the OLD AddOn
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack || !strings.Contains(j.Error, "did not reconnect") {
		t.Fatalf("job %s (error %q), want rolled_back: NT8 came back on the old AddOn", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times on an unproven reconnect", r.callCount("activate"))
	}
}
