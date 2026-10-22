package updaterworker

import (
	"fmt"
	"path/filepath"
	"strings"

	"vl/internal/addonbuild"
	"vl/internal/updaterjob"
)

// ── m343 — verify-on-place / verify-on-restore ──────────────────────────────
//
// The 10-21 18:31 incident: the catch-up job's addon_build compiled the
// INSTALLED release's ninjascript, which still declared VL_BUILD_ID
// "2026-10-18-a", and addon_place put that stale DLL into bin\Custom with NO
// check of the EMBEDDED build id — the sha256 it verified was the stale DLL's
// own sha. The owner's 00:02 hand-reopen then loaded the old AddOn.
//
// Two read-back checks close it:
//   - verify-on-place: after Place copies the new DLL, the embedded build id is
//     read back and compared with the release's expected build; a mismatch
//     restores the recorded backup.
//   - verify-on-restore: AddonRestore only moves back a backup whose sha256 +
//     build id were recorded at backup time (addonbuild.Place writes the
//     sidecar), and whose bytes still hash to the recorded sha.

// verifyAddonBackup is the pure verify-on-restore check: the backup dir must
// carry the sidecar addonbuild.Place wrote, and the backed-up DLL's current
// sha256 must equal the recorded one. It never moves anything.
func verifyAddonBackup(backupDir string) error {
	if strings.TrimSpace(backupDir) == "" {
		return nil // nothing was backed up, nothing to restore
	}
	meta, err := addonbuild.ReadAddonBackupMeta(backupDir)
	if err != nil {
		return fmt.Errorf("addon restore: no verified backup proof (%s): %w", addonbuild.AddonBackupMetaName, err)
	}
	if meta.Sha256 == "" {
		return fmt.Errorf("addon restore: the backup proof records no sha256 — refusing an unverified backup")
	}
	backupDll := filepath.Join(backupDir, addonDllName)
	got, err := sha256File(backupDll)
	if err != nil {
		return fmt.Errorf("addon restore: read backup: %w", err)
	}
	if got != meta.Sha256 {
		return fmt.Errorf("addon restore: the backup no longer matches its proof (sha256 got %s want %s) — refusing an unverified backup", got, meta.Sha256)
	}
	if meta.BuildID == "" || !validBuildID(meta.BuildID) {
		return fmt.Errorf("addon restore: the backup proof records no build id — refusing an unverified backup")
	}
	return nil
}

// verifyAddonPlace is the verify-on-place check: the placed bin\Custom\VL.AddOn.dll
// must embed the expected build id. wantBuild "" (or "n/a") means there is no
// expectation to check (nothing is compared, like the reconnect step). An
// UNREADABLE id in the placed DLL also skips: a real build always embeds
// VL_BUILD_ID, so an id-less DLL is a build anomaly the sha check and the
// reconnect ack (guard #3) still catch — refusing here would block the
// no-id test fakes and add nothing over those backstops.
func verifyAddonPlace(docsDir, wantBuild string) error {
	if !validBuildID(wantBuild) {
		return nil
	}
	got, err := addonbuild.BuildIDFromDLL(filepath.Join(docsDir, "bin", "Custom", addonDllName))
	if err != nil {
		return fmt.Errorf("addon place: read back the placed DLL's build id: %w", err)
	}
	if got == "" {
		return nil
	}
	if got != wantBuild {
		return fmt.Errorf("addon place: the placed DLL embeds build %q, the release expects %q — refusing", got, wantBuild)
	}
	return nil
}

// wantAddonBuild is the ONE resolution of the expected AddOn build for a job
// (mirrors stepAddonReconnect): a catch-up rebuilds the INSTALLED release, an
// install must match the signed manifest's addon.build_id.
func (w *Worker) wantAddonBuild(j updaterjob.Job) string {
	if j.Catchup {
		return w.oldInstallBuildID()
	}
	if j.NT8 != nil {
		return j.NT8.ManifestBuildID
	}
	return ""
}
