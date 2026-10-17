package updaterworker

import (
	"context"
	"os"
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

// restartCounts wraps the fake win seam and counts Stop-Process / Start-Process
// calls (one force-stop = one Stop-Process; one start = one Start-Process).
func restartCounts(r *rig) (stops, starts *int) {
	s, t := 0, 0
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if strings.Contains(a, "Stop-Process") {
				s++
			}
			if strings.Contains(a, "Start-Process") {
				t++
			}
		}
		return prev(ctx, name, args...)
	}
	return &s, &t
}

// Guard #1 TOCTOU (release #34, CTO STOP-AND-FIX): CloseMainWindow is given up
// to 60 s to close NT8. Three cases:
//
// (a) NT8 exits gracefully → no re-check, no force, one start → completes.
func TestNT8GracefulCloseSkipsTheRecheckAndForce(t *testing.T) {
	r := newRig(t, withCSChanged())
	stops, starts := restartCounts(r)
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete: NT8 closed itself and the one-click completed", j.State, j.Error)
	}
	if *stops != 0 {
		t.Fatalf("Stop-Process ran %d times; NT8 closed itself, so none is owed", *stops)
	}
	if *starts != 1 {
		t.Fatalf("Start-Process ran %d times, want exactly 1", *starts)
	}
}

// (b) NT8 is still running after 60 s + the re-check passes → force once, then
// start → completes.
func TestNT8HangsThenRecheckPassesAndForces(t *testing.T) {
	r := newRig(t, withCSChanged())
	stops, starts := restartCounts(r)
	r.set(func() { r.nt8HangsOnClose = true }) // a dialog/hang keeps NT8 alive
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete: the re-check passed and NT8 was force-stopped", j.State, j.Error)
	}
	if *stops != 1 {
		t.Fatalf("Stop-Process ran %d times, want exactly 1 (NT8 was still running)", *stops)
	}
	if *starts != 1 {
		t.Fatalf("Start-Process ran %d times, want exactly 1", *starts)
	}
}

// (c)/(d) NT8 is still running after 60 s + the re-check FAILS (a fill landed
// during the wait) → no Stop-Process, no start, reverted_clean with NT8 LEFT
// running. Mutant: run the re-check unconditionally (before knowing NT8 exited)
// → case (a) RED.
func TestFlatSIMGateRecheckBeforeStopProcessAborts(t *testing.T) {
	r := newRig(t, withCSChanged())
	stops, starts := restartCounts(r)
	r.set(func() { r.nt8HangsOnClose = true; r.flipSimOnClose = true })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean || j.Addon == nil || j.Addon.Restarted {
		t.Fatalf("job %s addon=%+v, want reverted_clean with no restart", j.State, j.Addon)
	}
	if !strings.Contains(j.Error, "NinjaTrader did not close and the flat+SIM re-check failed — nothing was stopped") {
		t.Fatalf("error %q must say NT8 was left running", j.Error)
	}
	if *stops != 0 {
		t.Fatalf("Stop-Process ran %d times; the re-check failed, so NT8 must be left running", *stops)
	}
	if *starts != 0 {
		t.Fatalf("Start-Process ran %d times; the restart was aborted", *starts)
	}
	if !r.nt8Running {
		t.Fatalf("NT8 must still be running after the aborted restart")
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under an aborted restart", r.callCount("activate"))
	}
}

// Guard #2 (GAP 1): while NinjaTrader.Custom.dll still holds the AddOn type,
// the AddOn is NEVER placed — regardless of whether VL.AddOn.dll already exists
// (F1: on the first install both DLLs can hold the type, and the reconnect
// outcome would depend on load order). The hazard is Custom.dll ALONE, so a
// `custom && ours` check would miss the first-install case (VL.AddOn.dll absent)
// and place the AddOn while Custom.dll still holds the type. The update MOVEs
// the AddOn sources out of bin/Custom/AddOns and ends reverted_clean with the
// one-time F5 blocker.
func TestAddOnNeverPlacedWhileCustomHoldsTheType(t *testing.T) {
	for _, c := range []struct {
		name       string
		withOldDll bool
	}{
		{name: "first install, VL.AddOn.dll absent", withOldDll: false},
		{name: "both DLLs hold the type", withOldDll: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, withCSChanged())
			writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
			if c.withOldDll {
				writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", addonDllName), "old AddOn DLL: type "+addonTypeName+"\n")
			}
			writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source NT8 recompiles\n")
			writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs.bak"), "// a stale .bak\n")
			j := r.runToEnd(t)
			r.noViolations(t)
			if j.State != updaterjob.StateRevertedClean {
				t.Fatalf("job %s (error %q), want reverted_clean", j.State, j.Error)
			}
			if j.Error != addonMigrationBlocker {
				t.Fatalf("error %q, want the plain one-time F5 blocker %q", j.Error, addonMigrationBlocker)
			}
			if j.Addon != nil && j.Addon.BackupDir != "" {
				t.Fatalf("a refused install still placed the AddOn: %+v", j.Addon)
			}
			// NEVER placed: a pre-existing VL.AddOn.dll is left byte-identical,
			// and on a first install none is created.
			if c.withOldDll {
				b, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", addonDllName))
				if err != nil || string(b) != "old AddOn DLL: type "+addonTypeName+"\n" {
					t.Fatalf("the pre-existing VL.AddOn.dll was disturbed (err=%v, content=%q)", err, b)
				}
			} else {
				if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", addonDllName)); !os.IsNotExist(err) {
					t.Fatalf("the AddOn was placed on a first install (VL.AddOn.dll exists)")
				}
			}
			// Sources moved (never deleted): gone from AddOns, present in migrated-sources.
			for _, f := range []string{"VLTraderTCPClient.cs", "VLTraderTCPClient.cs.bak"} {
				if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", f)); !os.IsNotExist(err) {
					t.Fatalf("source %s was not moved out of AddOns", f)
				}
				if _, err := os.Stat(filepath.Join(r.nt8docs, "vl-addon-backups", boxJobID, "migrated-sources", f)); err != nil {
					t.Fatalf("source %s not in migrated-sources: %v", f, err)
				}
			}
			if r.callCount("activate") != 0 {
				t.Fatalf("activate ran %d times under a refused install", r.callCount("activate"))
			}
		})
	}
}

// Guard #2 (GAP 1) second half: after the owner's one-time F5, Custom.dll no
// longer holds the type, and the next Update places and completes one-click.
func TestFirstInstallPlacesAfterTheOneTimeF5(t *testing.T) {
	r := newRig(t, withCSChanged())
	// The owner pressed F5 once: Custom.dll is now clean (no AddOn type).
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "// clean Custom.dll, no AddOn type\n")
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete after the one-time F5", j.State, j.Error)
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", addonDllName)); err != nil {
		t.Fatalf("the post-F5 update did not place the AddOn: %v", err)
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
