package updaterworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
func (w *Worker) winSeam() nt8RunSeam {
	if w.runWin != nil {
		return w.runWin
	}
	return execWindows
}

// addonConfig assembles the addonbuild.Config for THIS job. WorkDir is a
// worker-made dir under /tmp — never the NT8 folder. SourceDir is the
// RELEASE's ninjascript/*.cs (the NEW AddOn sources), never the install's.
func (w *Worker) addonConfig(j updaterjob.Job) (addonbuild.Config, error) {
	if j.Release == nil || j.Release.Dir == "" {
		return addonbuild.Config{}, errors.New("addon build without a verified release dir")
	}
	docsDir, err := addonDocsDir()
	if err != nil {
		return addonbuild.Config{}, err
	}
	return addonbuild.Config{
		DocsDir:   docsDir,
		Exe:       addonExe(),
		DotnetExe: "dotnet.exe",
		SourceDir: filepath.Join(j.Release.Dir, "ninjascript"),
		WorkDir:   filepath.Join(os.TempDir(), "vl-addon-build", j.JobID),
		Run:       w.winSeam(),
	}, nil
}

// stepAddonBuild compiles the release's ninjascript/*.cs into VL.AddOn.dll. A
// build error refuses the update BEFORE any hold, kill or place — the page
// shows the compiler error. A release whose signed manifest lists no
// ninjascript/*.cs cannot be compiled from its own unproven sources: refused.
func (w *Worker) stepAddonBuild(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	dll, sha, err := func() (string, string, error) {
		f, ferr := w.facts(j)
		if ferr != nil {
			return "", "", fmt.Errorf("release re-verify: %w", ferr)
		}
		if len(csFromArtifacts(f.Artifacts)) == 0 {
			return "", "", errors.New("the release lists no ninjascript/*.cs — the AddOn source cannot be proven")
		}
		cfg, err := w.addonConfig(j)
		if err != nil {
			return "", "", fmt.Errorf("addon config: %w", err)
		}
		return addonbuild.Build(ctx, cfg)
	}()
	ev["dll"], ev["sha256"] = dll, sha
	res := stepResult{receipts: []Receipt{w.receipt("addon_build", start, ev, err)}, err: err}
	if err != nil {
		res.failTo, res.reason = updaterjob.StateRevertedClean, "the AddOn build failed: "+clipText(err.Error())
		return res
	}
	res.set = func(k *updaterjob.Job) {
		if k.Addon == nil {
			k.Addon = &updaterjob.AddonState{}
		}
		k.Addon.DllPath, k.Addon.Sha256 = dll, sha
	}
	return res
}

// addonDoubleLoaded reports whether the AddOn's type exists in BOTH
// NinjaTrader.Custom.dll (the F5-compiled copy) and VL.AddOn.dll (ours). That
// is the one-time-migration hazard: NT8 would load the AddOn twice.
func addonDoubleLoaded(docsDir string) (bool, error) {
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
	custom, err := hasType(filepath.Join(docsDir, "bin", "Custom", "NinjaTrader.Custom.dll"))
	if err != nil {
		return false, err
	}
	ours, err := hasType(filepath.Join(docsDir, "bin", "Custom", addonDllName))
	if err != nil {
		return false, err
	}
	return custom && ours, nil
}

// stepAddonPlace MOVEs the old DLL out, copies the new one in, verifies its
// sha256. Guard: a double-load is refused BEFORE anything is placed.
func (w *Worker) stepAddonPlace(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	backupDir, err := func() (string, error) {
		if j.Addon == nil || j.Addon.DllPath == "" || j.Addon.Sha256 == "" {
			return "", errors.New("addon_place without a built DLL")
		}
		cfg, err := w.addonConfig(j)
		if err != nil {
			return "", fmt.Errorf("addon config: %w", err)
		}
		dup, err := addonDoubleLoaded(cfg.DocsDir)
		if err != nil {
			return "", fmt.Errorf("double-load check: %w", err)
		}
		if dup {
			ev["double_load"] = "true"
			return "", errors.New("double-load: the AddOn type is in both NinjaTrader.Custom.dll and VL.AddOn.dll — the one-time migration must run first")
		}
		b, err := addonbuild.Place(cfg, j.Addon.DllPath, j.Addon.Sha256, j.JobID)
		if err != nil {
			return "", fmt.Errorf("addon place: %w", err)
		}
		ev["backup_dir"] = b
		return b, nil
	}()
	res := stepResult{receipts: []Receipt{w.receipt("addon_place", start, ev, err)}, err: err}
	if err != nil {
		res.failTo, res.reason = updaterjob.StateRevertedClean, "the AddOn place failed: "+clipText(err.Error())
		return res
	}
	res.set = func(k *updaterjob.Job) {
		if k.Addon == nil {
			k.Addon = &updaterjob.AddonState{}
		}
		k.Addon.BackupDir = backupDir
		// The reconnect proof starts from the connection BEFORE the restart.
		if k.NT8 != nil {
			k.Addon.OldAcceptSeq = k.NT8.AckAcceptSeq
		}
	}
	return res
}

// flatSIMGate is guard #1: never restart NT8 unless every bound account is
// flat (positions=0, working=0) and SIM-tradeable. It reads the SAME gate one
// more time, immediately before the restart. The running view proves flat via
// addon_census, exposure via ledger_exposure, and open positions + working
// orders via every trader_cutover leg (its leg 1 IS db_open_positions, leg 4
// is working_orders). sim_accounts and db_open_positions appear as their own
// legs only on the nt8_absent view, which never reaches a restart — required
// when present, never the only evidence.
func (w *Worker) flatSIMGate(ctx context.Context, j updaterjob.Job) error {
	g, err := w.app.InstallationGate(ctx)
	if err != nil {
		return fmt.Errorf("installation-gate: %w", err)
	}
	want := map[string]bool{
		"addon_census":    false,
		"ledger_exposure": false,
	}
	cutover := 0
	for _, leg := range g.Legs {
		switch {
		case leg.Name == "addon_census" || leg.Name == "ledger_exposure":
			want[leg.Name] = true
			if !leg.Pass {
				return fmt.Errorf("flat+SIM gate: %s: %s", leg.Name, leg.Detail)
			}
		case leg.Name == "sim_accounts" || leg.Name == "db_open_positions":
			if !leg.Pass {
				return fmt.Errorf("flat+SIM gate: %s: %s", leg.Name, leg.Detail)
			}
		case strings.HasPrefix(leg.Name, "trader_cutover:"):
			cutover++
			if !leg.Pass {
				return fmt.Errorf("flat+SIM gate: %s: %s", leg.Name, leg.Detail)
			}
		}
	}
	for name, seen := range want {
		if !seen {
			return fmt.Errorf("flat+SIM gate: the %s leg did not run — never restart NT8 without it", name)
		}
	}
	if cutover == 0 {
		return fmt.Errorf("flat+SIM gate: no trader_cutover leg ran — never restart NT8 without the open-positions proof")
	}
	return nil
}

// stepNT8Restart stops and starts NT8 through powershell.exe, ONLY after the
// flat+SIM gate passes. NT8 was running when the drain did not take the
// nt8_absent path.
func (w *Worker) stepNT8Restart(ctx context.Context, j updaterjob.Job) stepResult {
	start := w.host.Now()
	ev := map[string]string{}
	err := func() error {
		if err := w.flatSIMGate(ctx, j); err != nil {
			return err
		}
		ev["flat_sim"] = "pass"
		r := newNT8Restart(w.winSeam(), addonExe(), w.host.Sleep)
		if err := r.stop(ctx); err != nil {
			return err
		}
		if err := r.start(ctx); err != nil {
			return err
		}
		return nil
	}()
	res := stepResult{receipts: []Receipt{w.receipt("nt8_restart", start, ev, err)}, err: err}
	if err != nil {
		res.failTo, res.reason = updaterjob.StateRevertedClean, "the NT8 restart failed: "+clipText(err.Error())
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
		// next NT8 start. Nothing to wait for.
		return stepResult{receipts: []Receipt{w.receipt("addon_reconnect", start, map[string]string{"skipped": "nt8 not restarted"}, nil)}}
	}
	wantBuild := ""
	if j.NT8 != nil {
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
