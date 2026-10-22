package updaterworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vl/internal/addonbuild"
	"vl/internal/updaterjob"
)

// ── W-ADDON-ONE-CLICK: the AddOn build/place/restart/reconnect steps ─────────
//
// These replace the nt8_updated PARK. Nothing here deletes: the old DLL is
// MOVED to a backups dir by addonbuild.Place, and a revert MOVEs it back
// (Host.AddonRestore). Every step runs BEFORE activation, so a refusal or
// timeout ends reverted_clean (or rolled_back for the reconnect timeout, which
// already restarted NT8) — never a held system, never a silent pass.

const (
	addonTypeName = "VLTraderTCPClient" // the AddOn's public type, present in the compiled DLLs
	addonDllName  = "VL.AddOn.dll"
)

// addonDocsDir resolves VL_NT8_DOCS_DIR, else addonbuild.Detect() (the single
// /mnt/c/Users/*/Documents/NinjaTrader 8 with bin/Custom; >1 refuses naming them).
func addonDocsDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("VL_NT8_DOCS_DIR")); v != "" {
		return v, nil
	}
	return addonbuild.Detect()
}

// winSeam is the Windows subprocess seam (dotnet.exe build, powershell.exe restart).
// It is wrapped with the m388 WSL interop-timeout retry: a transient
// "UtilAcceptVsock … accept4 failed 110" (Windows and WSL briefly fail to talk
// to each other) is retried a bounded number of times; any other failure stays
// immediate. Before a retry of a Start-Process the worker checks whether NT8 is
// already running (m388 follow-up) so it never launches a second NinjaTrader.
func (w *Worker) winSeam() nt8RunSeam {
	raw := w.runWin
	if raw == nil {
		raw = execWindows
	}
	return retryWSLInterop(raw, w.host.Sleep, w.logf, func(ctx context.Context, name string, args []string) bool {
		return w.nt8StartLanded(ctx, raw, name, args)
	})
}

// addonConfig assembles the addonbuild.Config for THIS job. WorkDir is a
// worker-made dir under /tmp — never the NT8 folder. SourceDir is the
// RELEASE's ninjascript/*.cs (the NEW AddOn sources), never the install's —
// EXCEPT for a catch-up job, which rebuilds the VERIFIED installed release's
// own ninjascript (m355: <VL_RELEASE_DIR>/<running revision>/ninjascript —
// NEVER the install tree's, which is not a proof and can be stale).
func (w *Worker) addonConfig(j updaterjob.Job) (addonbuild.Config, error) {
	docsDir, err := addonDocsDir()
	if err != nil {
		return addonbuild.Config{}, err
	}
	src := ""
	if j.Catchup {
		src, err = w.catchupAddonSource()
		if err != nil {
			return addonbuild.Config{}, err
		}
	} else {
		if j.Release == nil || j.Release.Dir == "" {
			return addonbuild.Config{}, errors.New("addon build without a verified release dir")
		}
		src = filepath.Join(j.Release.Dir, "ninjascript")
	}
	return addonbuild.Config{
		DocsDir:   docsDir,
		Exe:       addonExe(),
		DotnetExe: "dotnet.exe", // bare name — addonbuild.Build resolves it (W-UPDATER-WINPATH)
		SourceDir: src,
		WorkDir:   filepath.Join(os.TempDir(), "vl-addon-build", j.JobID),
		Run:       w.winSeam(),
	}, nil
}

// stepAddonBuild compiles the release's ninjascript/*.cs into VL.AddOn.dll. A
// build error refuses the update BEFORE any hold, kill or place — the page
// shows the compiler error. A release whose signed manifest lists no
// ninjascript/*.cs cannot be compiled from its own unproven sources: refused.
// A CATCH-UP job builds the VERIFIED installed release's own ninjascript
// (m355: <VL_RELEASE_DIR>/<running revision>/ninjascript, re-proved through its
// verdict + SSHSIG) instead — the running release's proven sources, never the
// install tree's stale copy.
func (w *Worker) stepAddonBuild(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	res := addonbuild.Result{}
	err := func() error {
		if j.Catchup {
			ev["catchup"] = "true"
			cfg, err := w.addonConfig(j)
			if err != nil {
				return fmt.Errorf("addon config: %w", err)
			}
			var berr error
			res, berr = addonbuild.Build(ctx, cfg)
			return berr
		}
		f, ferr := w.facts(j)
		if ferr != nil {
			return fmt.Errorf("release re-verify: %w", ferr)
		}
		if len(csFromArtifacts(f.Artifacts)) == 0 {
			return errors.New("the release lists no ninjascript/*.cs — the AddOn source cannot be proven")
		}
		cfg, err := w.addonConfig(j)
		if err != nil {
			return fmt.Errorf("addon config: %w", err)
		}
		var berr error
		res, berr = addonbuild.Build(ctx, cfg)
		return berr
	}()
	ev["dll"], ev["sha256"], ev["built_by"] = res.DllPath, res.Sha256, res.BuiltBy
	out := stepResult{receipts: []Receipt{w.receipt("addon_build", start, ev, err)}, err: err}
	if err != nil {
		out.failTo, out.reason = updaterjob.StateRevertedClean, "the AddOn build failed: "+clipText(err.Error())
		return out
	}
	out.set = func(k *updaterjob.Job) {
		if k.Addon == nil {
			k.Addon = &updaterjob.AddonState{}
		}
		k.Addon.DllPath, k.Addon.Sha256, k.Addon.BuiltBy = res.DllPath, res.Sha256, res.BuiltBy
	}
	return out
}

// addonMigrationNeeded reports whether NinjaTrader.Custom.dll (the F5-compiled
// copy) already contains the AddOn's type. That is the one-time-migration
// hazard: if it does, placing VL.AddOn.dll would make NT8 load the AddOn twice
// after the restart. The hazard is Custom.dll containing the type ALONE — on a
// first one-click install VL.AddOn.dll does not exist yet, so checking BOTH
// DLLs would miss it and the AddOn would be placed while Custom.dll still holds
// the type. If Custom.dll has the type → do NOT place.
func addonMigrationNeeded(docsDir string) (bool, error) {
	hasType := func(p string) (bool, error) {
		b, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return strings.Contains(string(b), addonTypeName), nil
	}
	return hasType(filepath.Join(docsDir, "bin", "Custom", "NinjaTrader.Custom.dll"))
}

// addonMigrationBlocker is the plain, owner-facing message a refused first
// install ends with: the owner runs the one-time F5, then clicks Update again.
const addonMigrationBlocker = "One-time step: open NinjaTrader → NinjaScript Editor → press F5 once, then click Update again."

// migrateAddonSources MOVEs the release's own AddOn sources (bin/Custom/AddOns/
// VL*.cs, plus their .bak files) out into
// <docs>/vl-addon-backups/<job>/migrated-sources/, so a rebuild (or the owner's
// F5) recompiles NinjaTrader.Custom.dll WITHOUT the AddOn types. Scope is the
// AddOns folder ONLY: other VL*.cs elsewhere (e.g. Strategies/vltrader.cs, the
// legacy CSV strategy) are NOT the release's files and are left untouched.
// Nothing is deleted: the sources are only moved.
func migrateAddonSources(docsDir, jobID string) error {
	addons := filepath.Join(docsDir, "bin", "Custom", "AddOns")
	matches, _ := filepath.Glob(filepath.Join(addons, "VL*.cs*"))
	if len(matches) == 0 {
		return nil
	}
	dst := filepath.Join(docsDir, "vl-addon-backups", jobID, "migrated-sources")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("mkdir migrated-sources: %w", err)
	}
	for _, m := range matches {
		if err := os.Rename(m, filepath.Join(dst, filepath.Base(m))); err != nil {
			return fmt.Errorf("move %s: %w", m, err)
		}
	}
	return nil
}

// restoreMigratedSources MOVEs the migrated AddOn sources back into AddOns —
// the reverse of migrateAddonSources, used when the inline no-F5 rebuild fails
// so the old files are exactly as they were (moves only, never deletes).
func restoreMigratedSources(docsDir, jobID string) {
	src := filepath.Join(docsDir, "vl-addon-backups", jobID, "migrated-sources")
	matches, _ := filepath.Glob(filepath.Join(src, "*"))
	for _, m := range matches {
		_ = os.Rename(m, filepath.Join(docsDir, "bin", "Custom", "AddOns", filepath.Base(m)))
	}
}

// stepAddonPlace MOVEs the old DLL out, copies the new one in, verifies its
// sha256. Guard: a first install whose Custom.dll still holds the AddOn type is
// refused BEFORE anything is placed — the sources are migrated out and the job
// ends reverted_clean with the one-time F5 blocker.
func (w *Worker) stepAddonPlace(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	// m343 guard #3: a job that has already boot-verified must never touch the
	// AddOn again (no late place/revert/catch-up overwrite).
	if j.AddonProven() {
		return stepResult{receipts: []Receipt{w.receipt("addon_place", start,
			map[string]string{"skipped": "boot_verified"}, nil)}}
	}
	// Catch-up records the pre-restart accept_seq from the live hello (there is
	// no nt8 decision to read it from).
	var preSeq uint64
	if j.Catchup {
		if m, merr := w.app.Maintenance(ctx); merr == nil && m.AddonAck != nil {
			preSeq = m.AddonAck.AcceptSeq
		}
	}
	deferred := false
	recoveryNeeded := false
	backupDir, err := func() (string, error) {
		if j.Addon == nil || j.Addon.DllPath == "" || j.Addon.Sha256 == "" {
			return "", errors.New("addon_place without a built DLL")
		}
		cfg, err := w.addonConfig(j)
		if err != nil {
			return "", fmt.Errorf("addon config: %w", err)
		}
		migrate, err := addonMigrationNeeded(cfg.DocsDir)
		if err != nil {
			return "", fmt.Errorf("one-time migration check: %w", err)
		}
		if migrate {
			ev["migration_needed"] = "true"
			// NT8 is CLOSED for this job when an install took the nt8_absent
			// drain (j.NT8.Absent), or when a catch-up was created with NT8
			// already closed (!CatchupNT8Open — a catch-up never sets j.NT8).
			nt8Closed := (j.Catchup && !j.CatchupNT8Open) || (!j.Catchup && j.NT8 != nil && j.NT8.Absent)
			if nt8Closed {
				// NT8 CLOSED (release #35, no F5): migrate + rebuild + place
				// now, in one go. The rebuild ACCEPTS the new Custom.dll only
				// when compile errors = 0 AND the public type list equals the
				// old one's minus the release's AddOn types. On failure the old
				// files are restored and the job falls back to the F5 blocker
				// as the last resort.
				{
					// The NT8-closed state was read BEFORE the build (a catch-up's
					// status read, or an install's nt8_absent drain); the owner may
					// have opened NT8 since. CTO #35: this re-check runs for BOTH
					// a catch-up and an install. NEVER rebuild
					// Custom.dll (or place the AddOn) under a running NT8 — if
					// it is running now, defer to the restart path, which stops
					// NT8 first and migrates while it is closed.
					running, rerr := newNT8Restart(w.winSeam(), addonExe(), w.host.Sleep).isRunning(ctx)
					if rerr != nil {
						return "", fmt.Errorf("NT8 running check before the Custom.dll rebuild: %w", rerr)
					}
					if running {
						ev["nt8_started_mid_catchup"] = "true"
						ev["migration_deferred"] = "true"
						deferred = true
					}
				}
				if !deferred {
					if err := migrateAddonSources(cfg.DocsDir, j.JobID); err != nil {
						return "", fmt.Errorf("one-time migration: %w", err)
					}
					creb, cerr := addonbuild.RebuildCustom(ctx, cfg)
					if cerr != nil {
						restoreMigratedSources(cfg.DocsDir, j.JobID)
						ev["custom_rebuild"] = "failed"
						ev["custom_rebuild_error"] = clipText(cerr.Error())
						return "", errors.New(addonMigrationBlocker)
					}
					cbak, perr := addonbuild.PlaceCustom(cfg, creb.DllPath, creb.Sha256, j.JobID)
					if perr != nil {
						restoreMigratedSources(cfg.DocsDir, j.JobID)
						return "", fmt.Errorf("Custom.dll place failed: %w", perr)
					}
					ev["custom_rebuild"] = "accepted"
					ev["custom_rebuild_built_by"] = creb.BuiltBy
					ev["custom_backup"] = cbak
					// fall through: place VL.AddOn.dll, the AddOn now lives
					// only there.
				}
			} else {
				// NT8 OPEN (release #35 FOLD 2): Custom.dll is loaded in memory
				// and cannot be rebuilt in place. Defer the WHOLE migration to
				// the restart step, which stops NT8, then migrates + rebuilds +
				// places while it is closed, then starts it again.
				ev["migration_deferred"] = "true"
				deferred = true
			}
		}
		if deferred {
			return "", nil
		}
		// m355 rule 3 — NEVER DOWNGRADE: a catch-up only places a build that
		// is strictly newer than what the box runs (the ack) or loads (the
		// placed DLL). Refused here means bin\Custom is untouched.
		if j.Catchup {
			if derr := w.catchupNeverDowngrade(ctx, cfg.DocsDir, w.wantAddonBuild(j)); derr != nil {
				return "", derr
			}
		}
		b, err := addonbuild.Place(cfg, j.Addon.DllPath, j.Addon.Sha256, j.JobID)
		if err != nil {
			return "", fmt.Errorf("addon place: %w", err)
		}
		ev["backup_dir"] = b
		// m343 verify-on-place: read back the EMBEDDED build id and refuse+revert
		// when it is not the release's expected build (the 10-21 18:31 stale
		// placement was never checked). With no prior DLL (b == "") there is
		// nothing to revert to — refuse and leave it to the owner.
		if b != "" {
			if verr := verifyAddonPlace(cfg.DocsDir, w.wantAddonBuild(j)); verr != nil {
				if rerr := w.host.AddonRestore(cfg.DocsDir, b, w.preJobBuild(j)); rerr != nil {
					// m357 C: the unverified NEW DLL must not stay live — move it
					// aside (never delete) and end recovery_needed, loudly.
					if qerr := quarantineUnverifiedAddon(cfg.DocsDir, b); qerr != nil {
						return "", fmt.Errorf("%v; the old AddOn could not be restored (%v) and the unverified DLL could not be moved aside: %w", verr, rerr, qerr)
					}
					ev["unverified_dll"] = "quarantined"
					recoveryNeeded = true
					return "", fmt.Errorf("%v; the old AddOn could not be restored (%v) — the unverified DLL was moved aside", verr, rerr)
				}
				return "", verr
			}
		}
		return b, nil
	}()
	res := stepResult{receipts: []Receipt{w.receipt("addon_place", start, ev, err)}, err: err}
	if err != nil {
		if recoveryNeeded {
			res.failTo, res.reason = updaterjob.StateRecoveryNeeded, "the AddOn place failed and the unverified DLL was moved aside: "+clipText(err.Error())
		} else {
			res.failTo, res.reason = updaterjob.StateRevertedClean, "the AddOn place failed: "+clipText(err.Error())
		}
		return res
	}
	res.set = func(k *updaterjob.Job) {
		if k.Addon == nil {
			k.Addon = &updaterjob.AddonState{}
		}
		k.Addon.BackupDir = backupDir
		k.Addon.MigrationPending = deferred
		// The reconnect proof starts from the connection BEFORE the restart.
		if k.NT8 != nil {
			k.Addon.OldAcceptSeq = k.NT8.AckAcceptSeq
		} else if j.Catchup {
			k.Addon.OldAcceptSeq = preSeq
		}
	}
	return res
}

// flatSIMGate is guard #1: never restart NT8 unless every bound account is
// flat (positions=0, working=0) and SIM-tradeable. It reads the SAME gate one
// more time, immediately before the restart. FLAT = addon_census (positions=0
// AND working=0 on EVERY account, live included) + ledger_exposure clear.
// SIM = sim_accounts (every bound account passes IsAccountTradeable). A leg
// that is missing, or fails, is a refusal — never a silent restart.
func (w *Worker) flatSIMGate(ctx context.Context, j updaterjob.Job) error {
	g, err := w.app.InstallationGate(ctx)
	if err != nil {
		return fmt.Errorf("installation-gate: %w", err)
	}
	want := map[string]bool{
		"addon_census":    false,
		"ledger_exposure": false,
		"sim_accounts":    false,
	}
	for _, leg := range g.Legs {
		if _, ok := want[leg.Name]; !ok {
			continue
		}
		want[leg.Name] = true
		if !leg.Pass {
			return fmt.Errorf("flat+SIM gate: %s: %s", leg.Name, leg.Detail)
		}
	}
	for name, seen := range want {
		if !seen {
			return fmt.Errorf("flat+SIM gate: the %s leg did not run — never restart NT8 without it", name)
		}
	}
	return nil
}

// migrateRebuildPlaceWhileClosed (release #35 FOLD 2) runs the deferred
// migration while NT8 is CLOSED: migrate the AddOn sources out, rebuild
// NinjaTrader.Custom.dll, place it, then place VL.AddOn.dll. It is ONLY called
// from stepNT8Restart after the stop and before the start. On any failure it
// restores every move (sources, Custom.dll, VL.AddOn.dll) so NT8 can be started
// on the OLD files — moves only, never deletes.
func (w *Worker) migrateRebuildPlaceWhileClosed(ctx context.Context, j updaterjob.Job, ev map[string]string) error {
	cfg, err := w.addonConfig(j)
	if err != nil {
		return err
	}
	docs := cfg.DocsDir
	customDir := filepath.Join(docs, "bin", "Custom")
	backupRoot := filepath.Join(docs, "vl-addon-backups", j.JobID)

	// restoreAll moves every artifact back (reverse order), best-effort.
	restoreAll := func() {
		customBackup := filepath.Join(backupRoot, "custom-rebuild", "NinjaTrader.Custom.dll")
		if isRegularFile(customBackup) {
			_ = os.Rename(customBackup, filepath.Join(customDir, "NinjaTrader.Custom.dll"))
		}
		addonBackup := filepath.Join(backupRoot, addonDllName)
		if isRegularFile(addonBackup) {
			_ = os.Rename(addonBackup, filepath.Join(customDir, addonDllName))
		}
		if matches, _ := filepath.Glob(filepath.Join(backupRoot, "migrated-sources", "*")); len(matches) > 0 {
			for _, m := range matches {
				_ = os.Rename(m, filepath.Join(customDir, "AddOns", filepath.Base(m)))
			}
		}
	}

	// 1. Migrate the sources out.
	if err := migrateAddonSources(docs, j.JobID); err != nil {
		return err
	}

	// 2. Rebuild Custom.dll (no moves; the old DLL is only read for the diff).
	creb, cerr := addonbuild.RebuildCustom(ctx, cfg)
	if cerr != nil {
		restoreAll()
		ev["custom_rebuild"] = "failed"
		ev["custom_rebuild_error"] = clipText(cerr.Error())
		return fmt.Errorf("Custom.dll rebuild failed: %w", cerr)
	}

	// 3. Place Custom.dll (moves the old to a backup, copies the new in).
	cbak, perr := addonbuild.PlaceCustom(cfg, creb.DllPath, creb.Sha256, j.JobID)
	if perr != nil {
		restoreAll()
		return fmt.Errorf("Custom.dll place failed: %w", perr)
	}
	ev["custom_rebuild"] = "accepted"
	ev["custom_rebuild_built_by"] = creb.BuiltBy
	ev["custom_backup"] = cbak

	// 4. Place VL.AddOn.dll (moves the old to a backup, copies the new in).
	destAddon := filepath.Join(customDir, addonDllName)
	hadOldAddon := isRegularFile(destAddon)
	if j.Addon == nil || j.Addon.DllPath == "" || j.Addon.Sha256 == "" {
		restoreAll()
		return errors.New("migration place without a built AddOn DLL")
	}
	// m355 rule 3 — NEVER DOWNGRADE, also on the deferred-migration place path.
	if j.Catchup {
		if derr := w.catchupNeverDowngrade(ctx, docs, w.wantAddonBuild(j)); derr != nil {
			restoreAll()
			return derr
		}
	}
	abak, perr2 := addonbuild.Place(cfg, j.Addon.DllPath, j.Addon.Sha256, j.JobID)
	if perr2 != nil {
		// addonbuild.Place may have moved the old AddOn to the deterministic
		// backup before failing: restore it. If there was no old AddOn, the
		// failed new copy must be MOVED aside (never deleted) so a rollback
		// does not double-load it.
		addonBackup := filepath.Join(backupRoot, addonDllName)
		switch {
		case isRegularFile(addonBackup):
			_ = os.Rename(addonBackup, destAddon)
		case !hadOldAddon:
			rejected := filepath.Join(backupRoot, "rejected-addon")
			if err := os.MkdirAll(rejected, 0o755); err == nil {
				_ = os.Rename(destAddon, filepath.Join(rejected, addonDllName))
			}
		}
		restoreAll()
		return fmt.Errorf("AddOn place failed: %w", perr2)
	}
	ev["backup_dir"] = abak
	return nil
}

// isRegularFile reports whether p is an existing regular file.
func isRegularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// stepNT8Restart stops and starts NT8 through powershell.exe, ONLY after the
// flat+SIM gate passes. NT8 was running when the drain did not take the
// nt8_absent path. CloseMainWindow is given up to nt8CloseWait to close NT8
// gracefully; while it is still running the flat+SIM gate is re-read RIGHT
// before Stop-Process (TOCTOU — a fill or a new position can land during the
// wait). If the re-check fails, NT8 is NOT force-stopped: it is left running
// and the job ends reverted_clean. If NT8 exits on its own, there is no
// re-check and no force — it closed itself, and nothing can trade while closed.

// nt8PollRunning polls r.isRunning for `want` until deadline. A transient probe
// error (tasklist exit 1, WSL interop hiccup, vsock accept failure) is RETRIED
// until the deadline — one hiccup must not abort a bounded wait (m246). It
// returns confirmed=true when the state matched `want` before the deadline;
// otherwise lastErr is the most recent probe error (nil when the deadline simply
// passed with the state never matching).
func (w *Worker) nt8PollRunning(ctx context.Context, r nt8Restart, want bool, deadline time.Time) (bool, error) {
	var lastErr error
	for {
		running, err := r.isRunning(ctx)
		if err != nil {
			lastErr = err
		} else if running == want {
			return true, nil
		}
		if !w.host.Now().Before(deadline) {
			return false, lastErr
		}
		if err := w.host.Sleep(ctx, nt8RestartPoll); err != nil {
			return false, err
		}
	}
}

// startAndVerifyNT8 starts NinjaTrader and proves it is actually running with a
// bounded process check. Start-Process succeeding proves only "process
// started", never "AddOn ready" — the AddOn-ready proof (build id + a new
// accept_seq) stays in stepAddonReconnect. The wait is recorded in ev.
func (w *Worker) startAndVerifyNT8(ctx context.Context, r nt8Restart, ev map[string]string) error {
	if err := r.start(ctx); err != nil {
		return err
	}
	startStart := w.host.Now()
	startDeadline := startStart.Add(nt8StartWait)
	confirmed, lastErr := w.nt8PollRunning(ctx, r, true, startDeadline)
	if lastErr != nil {
		ev["nt8_start_probe_error"] = clipText(lastErr.Error())
		return fmt.Errorf("the NinjaTrader liveness probe kept failing after the start: %w", lastErr)
	}
	if !confirmed {
		ev["nt8_start_wait"] = "timeout_not_running"
		return fmt.Errorf("NinjaTrader did not come up within %s of the start", nt8StartWait)
	}
	ev["nt8_start_wait_ms"] = strconv.FormatInt(w.host.Now().Sub(startStart).Milliseconds(), 10)
	return nil
}

func (w *Worker) stepNT8Restart(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	// nt8Down is set once NinjaTrader has been stopped and a start attempt
	// (or its liveness verification) leaves it down (or liveness unproven):
	// the new DLL is already placed, so a plain reverted_clean — which releases
	// the hold and declares the old build "still running normally" — would be
	// FALSE. Those failures end recovery_needed (the hold stays, the honest
	// reason is named) instead.
	nt8Down := false
	err := func() error {
		if err := w.flatSIMGate(ctx, j); err != nil {
			return err
		}
		ev["flat_sim"] = "pass"
		r := newNT8Restart(w.winSeam(), addonExe(), w.host.Sleep)
		if err := r.signalClose(ctx); err != nil {
			return err
		}
		// m246: the point of no return — the updater is now stopping NT8. Record
		// it on the job so the revert path brings NinjaTrader back up (restoring
		// the old AddOn alone leaves NT8 closed, the owner's #43 Update failure).
		if _, uerr := w.update(j.JobID, j.State, j.Phase, func(k *updaterjob.Job) error {
			if k.Addon == nil {
				k.Addon = &updaterjob.AddonState{}
			}
			k.Addon.StoppedNT8 = true
			return nil
		}); uerr != nil {
			return fmt.Errorf("the NT8 stop could not be recorded on the job: %w", uerr)
		}
		// Poll IsRunning every 2 s up to nt8CloseWait. If NT8 exited on its own,
		// no re-check and no force. A transient tasklist error is retried; when
		// the deadline passes without a confirmed exit, NT8 is force-stopped.
		exited, closeProbeErr := w.nt8PollRunning(ctx, r, false, w.host.Now().Add(nt8CloseWait))
		if closeProbeErr != nil {
			ev["nt8_close_probe_error"] = clipText(closeProbeErr.Error())
		}
		if !exited {
			// NT8 is STILL running (a dialog, a hang). The AddOn is still
			// connected, so the census is valid — re-check the gate right before
			// the force stop. A failed re-check leaves NT8 RUNNING, never killed.
			if err := w.flatSIMGate(ctx, j); err != nil {
				ev["flat_sim_recheck"] = "fail"
				return fmt.Errorf("NinjaTrader did not close and the flat+SIM re-check failed — nothing was stopped: %w", err)
			}
			ev["flat_sim_recheck"] = "pass"
			if err := r.forceStop(ctx); err != nil {
				return err
			}
		}
		// W-UPDATER-WINPATH (d): wait until the old NinjaTrader.exe has FULLY
		// exited before starting the new one. The owner saw "Unable to start ATI
		// server component … socket forbidden" when NT8 restarted before the old
		// process released the port. Poll IsRunning, bounded; record the wait.
		exitStart := w.host.Now()
		exitDeadline := exitStart.Add(nt8ExitWait)
		exited, lastErr := w.nt8PollRunning(ctx, r, false, exitDeadline)
		if !exited {
			// The OLD process is still running (or liveness never confirmed)
			// while the NEW DLL is already on disk — a mixed state, not a clean
			// one. recovery_needed (hold kept) until the human resolves it.
			ev["nt8_exit_wait"] = "timeout_still_running"
			nt8Down = true
			if lastErr != nil {
				ev["nt8_exit_probe_error"] = clipText(lastErr.Error())
				return fmt.Errorf("NinjaTrader did not confirm a full exit within %s of the stop (the liveness probe kept failing: %w)", nt8ExitWait, lastErr)
			}
			return fmt.Errorf("NinjaTrader did not fully exit within %s of the stop — not starting a second instance", nt8ExitWait)
		}
		ev["nt8_exit_wait_ms"] = strconv.FormatInt(w.host.Now().Sub(exitStart).Milliseconds(), 10)
		// FOLD 2: a deferred migration (NT8 was open and Custom.dll still held
		// the AddOn type) runs while NT8 is closed — right after the stop and
		// before the start. A failure here must NEVER leave NT8 closed: restore
		// the old files and start NT8 on them, then fail to reverted_clean.
		if j.Addon != nil && j.Addon.MigrationPending {
			if merr := w.migrateRebuildPlaceWhileClosed(ctx, j, ev); merr != nil {
				if serr := w.startAndVerifyNT8(ctx, r, ev); serr != nil {
					nt8Down = true
					return fmt.Errorf("the Custom.dll migration failed (%v) and NT8 could not be restarted: %w", merr, serr)
				}
				ev["nt8_restarted_after_migration_failure"] = "true"
				return fmt.Errorf("the Custom.dll migration failed and NT8 was restarted on the old files: %w", merr)
			}
			ev["migration"] = "rebuilt"
		}
		if err := w.startAndVerifyNT8(ctx, r, ev); err != nil {
			nt8Down = true
			return err
		}
		return nil
	}()
	res := stepResult{receipts: []Receipt{w.receipt("nt8_restart", start, ev, err)}, err: err}
	if err != nil {
		if nt8Down {
			// The AddOn DLL is already placed and NinjaTrader is down (or its
			// liveness is unproven): never declare reverted_clean (which
			// releases the hold and claims the old build is still running).
			// recovery_needed keeps the hold and names the honest reason —
			// a human opens NinjaTrader before anything else. (Auto-rollback
			// from nt8_restarted would need a state-machine edge and a
			// "NT8 down" rollback signal; kept out of this minimal fix.)
			res.failTo, res.reason = updaterjob.StateRecoveryNeeded, "the NT8 restart left NinjaTrader down: "+clipText(err.Error())
		} else {
			res.failTo, res.reason = updaterjob.StateRevertedClean, "the NT8 restart failed: "+clipText(err.Error())
		}
		return res
	}
	res.set = func(k *updaterjob.Job) {
		if k.Addon == nil {
			k.Addon = &updaterjob.AddonState{}
		}
		k.Addon.Restarted = true
	}
	return res
}

// stepAddonReconnect waits for the AddOn to reconnect with a NEW accept_seq and
// the manifest's build_id (guard #3, bounded at nt8ReconnectWait). A timeout is
// never a silent pass: it fails to rolling_back, which restores the old DLL.
// When NT8 was closed at press time (no restart), there is nothing to wait for.
func (w *Worker) stepAddonReconnect(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	if j.Addon == nil || !j.Addon.Restarted {
		// NT8 was closed at press time: the DLL is placed and will load at the
		// next NT8 start. Nothing to wait for, and nothing is owed.
		return stepResult{receipts: []Receipt{w.receipt("addon_reconnect", start,
			map[string]string{"skipped": "nt8 not restarted", "note": "NT8 closed — the new AddOn loads the next time NinjaTrader starts"}, nil)}}
	}
	wantBuild := ""
	if j.Catchup {
		// Catch-up rebuilds the VERIFIED installed release's AddOn (m355): the
		// reconnect must prove the persisted expected build — never
		// oldInstallBuildID() (the nt8 decision never ran).
		if j.Addon != nil {
			wantBuild = j.Addon.ExpectedBuild
		}
	} else if j.NT8 != nil {
		wantBuild = j.NT8.ManifestBuildID
	}
	oldSeq := j.Addon.OldAcceptSeq
	err := func() error {
		deadline := w.host.Now().Add(nt8ReconnectWait)
		for {
			m, merr := w.app.Maintenance(ctx)
			if merr != nil {
				return fmt.Errorf("maintenance read: %w", merr)
			}
			if a := m.AddonAck; a != nil {
				ev["acked_build_id"], ev["acked_accept_seq"] = a.BuildID, strconv.FormatUint(a.AcceptSeq, 10)
				if a.AcceptSeq > oldSeq && wantBuild != "" && a.BuildID == wantBuild {
					return nil
				}
			}
			if !w.host.Now().Before(deadline) {
				return fmt.Errorf("the AddOn did not reconnect with build %q (new accept_seq) within %s", wantBuild, nt8ReconnectWait)
			}
			if err := w.host.Sleep(ctx, w.cfg.Budgets.Poll); err != nil {
				return err
			}
		}
	}()
	res := stepResult{receipts: []Receipt{w.receipt("addon_reconnect", start, ev, err)}, err: err}
	if err != nil {
		res.failTo, res.reason = updaterjob.StateRollingBack, "the AddOn reconnect timed out: "+clipText(err.Error())
	}
	return res
}

// rollbackAddonDown is the plain reason a rollback ends recovery_needed when
// NT8 did not come back on the OLD AddOn after the rollback's second restart.
const rollbackAddonDown = "NinjaTrader did not come back after the rollback — open NinjaTrader and check it is logged in"

// verifyRollbackAddon (F2, release #34) proves, after the rollback's second
// restart, that the AddOn came back on the OLD install's AddOn build over a NEW
// accept_seq. It is bounded at nt8ReconnectWait (the same 5 minutes as the
// forward reconnect). A timeout is never a silent pass: the caller ends
// recovery_needed — never rolled_back + "trading normal" while the AddOn is
// down.
func (w *Worker) verifyRollbackAddon(ctx context.Context, j updaterjob.Job) error {
	oldSeq := uint64(0)
	if j.Addon != nil {
		oldSeq = j.Addon.OldAcceptSeq
	}
	// The OLD install's AddOn build: what the AddOn actually acked at the park,
	// else the build the old install's own ninjascript declares.
	oldBuild := ""
	if j.NT8 != nil {
		oldBuild = j.NT8.AckedBuildID
		if oldBuild == "" {
			oldBuild = j.NT8.OldInstallBuildID
		}
	}
	deadline := w.host.Now().Add(nt8ReconnectWait)
	for {
		m, merr := w.app.Maintenance(ctx)
		if merr != nil {
			return fmt.Errorf("maintenance read: %w", merr)
		}
		if a := m.AddonAck; a != nil {
			if a.AcceptSeq > oldSeq && (oldBuild == "" || a.BuildID == oldBuild) {
				return nil
			}
		}
		if !w.host.Now().Before(deadline) {
			return errors.New(rollbackAddonDown)
		}
		if err := w.host.Sleep(ctx, w.cfg.Budgets.Poll); err != nil {
			return err
		}
	}
}
