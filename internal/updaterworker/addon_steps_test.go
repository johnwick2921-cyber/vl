package updaterworker

import (
	"context"
	"errors"
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
		name  string
		setup func(r *rig)
		want  string
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

// W-UPDATER-WINPATH (d): after the stop, start waits until the OLD
// NinjaTrader.exe has FULLY exited (the owner saw "Unable to start ATI server
// component … socket forbidden" when NT8 restarted before the old process
// released the port). The wait is polled, bounded, and recorded in the receipt.
func TestNT8ExitWaitBeforeStart(t *testing.T) {
	r := newRig(t, withCSChanged())
	_, starts := restartCounts(r)
	// A hang forces the Stop-Process path; the old process then lingers for 2
	// tasklist polls before fully exiting, so the exit-wait must poll it.
	r.set(func() { r.nt8HangsOnClose = true; r.nt8ExitLinger = 2 })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete: the old NT8 fully exited and the new one started", j.State, j.Error)
	}
	if *starts != 1 {
		t.Fatalf("Start-Process ran %d times, want exactly 1", *starts)
	}
	var waitMS string
	for _, rc := range j.Receipts {
		if rc.Step == "nt8_restart" && rc.Evidence != nil {
			waitMS = rc.Evidence["nt8_exit_wait_ms"]
		}
	}
	if waitMS == "" || waitMS == "0" {
		t.Fatalf("the nt8_restart receipt must record a non-zero nt8_exit_wait_ms, got %q", waitMS)
	}
}

// W-UPDATER-WINPATH (d) timeout: when the old NinjaTrader.exe NEVER fully exits
// within nt8ExitWait, the step errors, starts ZERO new processes (never a second
// instance racing the old one for the ATI socket), and records
// nt8_exit_wait=timeout_still_running. The job reverts clean.
func TestNT8ExitWaitTimeoutNoStart(t *testing.T) {
	r := newRig(t, withCSChanged())
	stops, starts := restartCounts(r)
	// A hang forces Stop-Process; the old process then lingers far past the
	// ~31 tasklist polls of the 60 s nt8ExitWait deadline — it never exits.
	r.set(func() { r.nt8HangsOnClose = true; r.nt8ExitLinger = 1000 })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean || j.Addon == nil || j.Addon.Restarted {
		t.Fatalf("job %s addon=%+v, want reverted_clean with no restart", j.State, j.Addon)
	}
	if !strings.Contains(j.Error, "did not fully exit") {
		t.Fatalf("error %q must say the old NT8 did not fully exit", j.Error)
	}
	if *stops != 1 {
		t.Fatalf("Stop-Process ran %d times, want exactly 1 (the hang forced the stop)", *stops)
	}
	if *starts != 0 {
		t.Fatalf("Start-Process ran %d times, want 0: never start a second instance while the old one still runs", *starts)
	}
	var wait string
	for _, rc := range j.Receipts {
		if rc.Step == "nt8_restart" && rc.Evidence != nil {
			wait = rc.Evidence["nt8_exit_wait"]
		}
	}
	if wait != "timeout_still_running" {
		t.Fatalf("the nt8_restart receipt must record nt8_exit_wait=timeout_still_running, got %q", wait)
	}
}
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

// Guard #2 (release #35 FOLD 2): NT8 OPEN + Custom.dll holding the AddOn type
// is no longer a refusal — the migration is DEFERRED to the restart step, which
// stops NT8, rebuilds Custom.dll while it is closed, places the AddOn, and
// starts NT8 again. The update completes one-click, no F5.
func TestFirstInstallNT8OpenMigratesWhileClosedAndCompletes(t *testing.T) {
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
			if j.State != updaterjob.StateComplete {
				t.Fatalf("job %s (error %q), want complete — NT8 open + migration is one-click, no F5", j.State, j.Error)
			}
			// Custom.dll was rebuilt WITHOUT the type (so the AddOn runs only
			// from VL.AddOn.dll — the double-load hazard is gone).
			cur, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(cur), addonTypeName) {
				t.Fatalf("the rebuilt Custom.dll still holds the AddOn type: %q", cur)
			}
			// The AddOn itself was placed.
			if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", addonDllName)); err != nil {
				t.Fatalf("the AddOn was not placed: %v", err)
			}
			// Sources migrated out (never deleted), present in migrated-sources.
			for _, f := range []string{"VLTraderTCPClient.cs", "VLTraderTCPClient.cs.bak"} {
				if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", f)); !os.IsNotExist(err) {
					t.Fatalf("source %s was not moved out of AddOns", f)
				}
				if _, err := os.Stat(filepath.Join(r.nt8docs, "vl-addon-backups", boxJobID, "migrated-sources", f)); err != nil {
					t.Fatalf("source %s not in migrated-sources: %v", f, err)
				}
			}
			if r.callCount("activate") != 1 {
				t.Fatalf("activate ran %d times, want exactly 1", r.callCount("activate"))
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

// Guard #2 (release #35, no F5): with NT8 CLOSED and Custom.dll still holding
// the AddOn type, the worker rebuilds Custom.dll itself (build-addon.ps1
// -Custom), accepts it, and the update completes one-click — no F5, no hands.
func TestFirstInstallNT8ClosedRebuildsCustomAndPlaces(t *testing.T) {
	r := newRig(t, withCSChanged(), withNT8Down(absentDown))
	// First install: Custom.dll holds the type; VL.AddOn.dll does not exist yet.
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source\n")
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete — the no-F5 rebuild replaces the one-time F5", j.State, j.Error)
	}
	// Custom.dll was rebuilt WITHOUT the type, and the old one was backed up.
	cur, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cur), addonTypeName) {
		t.Fatalf("the rebuilt Custom.dll still holds the AddOn type: %q", cur)
	}
	backup, err := os.ReadFile(filepath.Join(r.nt8docs, "vl-addon-backups", boxJobID, "custom-rebuild", "NinjaTrader.Custom.dll"))
	if err != nil || !strings.Contains(string(backup), addonTypeName) {
		t.Fatalf("the old Custom.dll was not backed up (err=%v): %q", err, backup)
	}
	// The AddOn itself was placed.
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", addonDllName)); err != nil {
		t.Fatalf("the AddOn was not placed: %v", err)
	}
	// The AddOn source was migrated out of AddOns.
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs")); !os.IsNotExist(err) {
		t.Fatalf("the AddOn source was not moved out: %v", err)
	}
}

// Guard #2 (release #35) failure: with NT8 CLOSED the rebuild fails (the fake
// -Custom path returns a type diff), so the old Custom.dll is left untouched and
// the job falls back to the F5 blocker as the last resort.
func TestFirstInstallNT8ClosedRebuildFailureAsksForF5(t *testing.T) {
	r := newRig(t, withCSChanged(), withNT8Down(absentDown))
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	// Make the no-F5 rebuild fail: the -Custom ps1 returns a type diff.
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name, "powershell") && hasWinFlag(args, "-Custom") {
			return []byte("VL_CUSTOM_TYPES_DIFF\nTYPE <= NinjaTrader.NinjaScript.Strategies.VLTrader\n"), errors.New("exit 1")
		}
		return prev(ctx, name, args...)
	}
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean || j.Error != addonMigrationBlocker {
		t.Fatalf("job %s (error %q), want reverted_clean with the F5 blocker", j.State, j.Error)
	}
	// The old Custom.dll is untouched.
	cur, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"))
	if err != nil || !strings.Contains(string(cur), addonTypeName) {
		t.Fatalf("the old Custom.dll must be untouched on rebuild failure: %q (err=%v)", cur, err)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under a failed no-F5 rebuild", r.callCount("activate"))
	}
}

// Guard #2 (release #35 FOLD 2) failure: NT8 OPEN + migration needed, and the
// rebuild fails while NT8 is closed. The worker must restore the old files and
// START NT8 again — never leave it closed after it was open — then end
// reverted_clean.
func TestFirstInstallNT8OpenMigrationFailureRestartsOnOldFiles(t *testing.T) {
	r := newRig(t, withCSChanged())
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source\n")
	// The -Custom rebuild fails (type diff).
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name, "powershell") && hasWinFlag(args, "-Custom") {
			return []byte("VL_CUSTOM_TYPES_DIFF\nTYPE <= NinjaTrader.NinjaScript.Strategies.VLTrader\n"), errors.New("exit 1")
		}
		return prev(ctx, name, args...)
	}
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("job %s (error %q), want reverted_clean", j.State, j.Error)
	}
	// NT8 must be RUNNING again (it was open, and must never be left closed).
	if !r.box.nt8Running {
		t.Fatalf("NT8 must be running again after the migration failure")
	}
	// The old Custom.dll is untouched (still holds the type).
	cur, err := os.ReadFile(filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"))
	if err != nil || !strings.Contains(string(cur), addonTypeName) {
		t.Fatalf("the old Custom.dll must be restored: %q (err=%v)", cur, err)
	}
	// The migrated source was moved back to AddOns.
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs")); err != nil {
		t.Fatalf("the AddOn source must be restored on migration failure: %v", err)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under a failed migration", r.callCount("activate"))
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

// TestMigrateAddonSourcesScope pins FOLD 1/FOLD 5 F2: migrateAddonSources moves
// ONLY the release's own AddOns/VL*.cs files. A VL*.cs elsewhere (Strategies,
// Indicators) or a non-VL file in AddOns is left byte-identical. A mutant that
// widens the glob to bin/Custom/** would move vltrader.cs/VLTrader.cs/VLfoo.cs
// and this test goes RED.
func TestMigrateAddonSourcesScope(t *testing.T) {
	docs := t.TempDir()
	base := filepath.Join(docs, "bin", "Custom")

	// The release's own AddOn source (moves) + a stale .bak (moves).
	writeFile(t, filepath.Join(base, "AddOns", "VLTraderTCPClient.cs"), "// release AddOn source\n")
	writeFile(t, filepath.Join(base, "AddOns", "VLTraderTCPClient.cs.bak"), "// stale bak\n")
	// NOT the release's files: a lower/upper-case VL strategy, a VL indicator,
	// and a non-VL file sitting in AddOns. All must stay.
	writeFile(t, filepath.Join(base, "Strategies", "vltrader.cs"), "// legacy CSV strategy\n")
	writeFile(t, filepath.Join(base, "Strategies", "VLTrader.cs"), "// uppercase strategy\n")
	writeFile(t, filepath.Join(base, "Indicators", "VLfoo.cs"), "// VL indicator\n")
	writeFile(t, filepath.Join(base, "AddOns", "SomethingElse.cs"), "// not ours\n")

	if err := migrateAddonSources(docs, "job-scope"); err != nil {
		t.Fatalf("migrateAddonSources: %v", err)
	}

	// The release's AddOns files moved out (never deleted).
	for _, f := range []string{"VLTraderTCPClient.cs", "VLTraderTCPClient.cs.bak"} {
		if _, err := os.Stat(filepath.Join(base, "AddOns", f)); !os.IsNotExist(err) {
			t.Fatalf("AddOns/%s was not moved out (stat err=%v)", f, err)
		}
		if _, err := os.Stat(filepath.Join(docs, "vl-addon-backups", "job-scope", "migrated-sources", f)); err != nil {
			t.Fatalf("AddOns/%s not in migrated-sources: %v", f, err)
		}
	}

	// Everything else is byte-identical and still in place.
	for rel, want := range map[string]string{
		filepath.Join("Strategies", "vltrader.cs"):  "// legacy CSV strategy\n",
		filepath.Join("Strategies", "VLTrader.cs"):  "// uppercase strategy\n",
		filepath.Join("Indicators", "VLfoo.cs"):     "// VL indicator\n",
		filepath.Join("AddOns", "SomethingElse.cs"): "// not ours\n",
	} {
		got, err := os.ReadFile(filepath.Join(base, rel))
		if err != nil {
			t.Fatalf("%s was moved or lost: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want untouched %q", rel, got, want)
		}
	}

	// No migrated-sources entry for anything outside AddOns/VL*.cs.
	entries, _ := filepath.Glob(filepath.Join(docs, "vl-addon-backups", "job-scope", "migrated-sources", "*"))
	if len(entries) != 2 {
		t.Fatalf("migrated-sources has %d entries (%v), want exactly the 2 release AddOns files", len(entries), entries)
	}
}

// CTO #35: the "NEVER replace Custom.dll under a running NT8" re-check holds
// for an INSTALL too, not only a catch-up. The install took the nt8_absent
// drain (NT8 closed), then the owner opens NT8 before addon_place. Custom.dll
// may only change if the worker stopped and restarted NT8 around it (the
// restart path); an inline swap under the running NT8 is a failure.
// Mutant: ignore `running` in the re-check → RED.
func TestFirstInstallNT8OpenedMidInstallNoSwapUnderRunningNT8(t *testing.T) {
	r := newRig(t, withCSChanged(), withNT8Down(absentDown))
	custom := filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll")
	writeFile(r.t, custom, "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source\n")
	r.hookAt("addon_placed/started", func() {
		r.set(func() { r.nt8Running = true })
	})
	starts := startCount(r)
	j := r.runToEnd(t)
	b, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := !strings.Contains(string(b), addonTypeName)
	if rebuilt && *starts == 0 {
		t.Fatalf("Custom.dll was replaced while NT8 was running, with no stop/start around it (job %s, error %q)", j.State, j.Error)
	}
}
