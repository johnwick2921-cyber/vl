package updaterworker

import (
	"fmt"
	"os"
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
// sha256 must equal the recorded one. A LEGACY backup (m357 D — written by an
// older updater, no sidecar) is accepted only when its embedded build id, read
// NOW, equals preJobBuild (the build the box ran before the job). It never
// moves anything.
func verifyAddonBackup(backupDir, preJobBuild string) error {
	if strings.TrimSpace(backupDir) == "" {
		return nil // nothing was backed up, nothing to restore
	}
	meta, err := addonbuild.ReadAddonBackupMeta(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return verifyLegacyBackup(backupDir, preJobBuild)
		}
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

// verifyLegacyBackup is the m357 D fallback: a backup dir with NO sidecar (an
// old-updater backup). The backup DLL's embedded build id + sha are read NOW;
// it is accepted only when the id equals preJobBuild (the pre-job maintenance
// ack the job recorded) — otherwise refused exactly as a sidecar-less backup
// always was.
func verifyLegacyBackup(backupDir, preJobBuild string) error {
	backupDll := filepath.Join(backupDir, addonDllName)
	id, err := addonbuild.BuildIDFromDLL(backupDll)
	if err != nil {
		return fmt.Errorf("addon restore: read the legacy backup's build id: %w", err)
	}
	if _, err := sha256File(backupDll); err != nil {
		return fmt.Errorf("addon restore: read the legacy backup: %w", err)
	}
	if id == "" || !validBuildID(id) {
		return fmt.Errorf("addon restore: the legacy backup carries no build id — refusing an unverified backup")
	}
	if preJobBuild == "" || !validBuildID(preJobBuild) {
		return fmt.Errorf("addon restore: the job recorded no pre-job AddOn build to accept a legacy backup against — refusing an unverified backup")
	}
	if id != preJobBuild {
		return fmt.Errorf("addon restore: the legacy backup embeds build %q, the box ran %q before the job — refusing an unverified backup", id, preJobBuild)
	}
	return nil
}

// verifyAddonPlace is the verify-on-place check: the placed bin\Custom\VL.AddOn.dll
// must embed the expected build id. wantBuild "" (or "n/a") means there is no
// expectation to check (nothing is compared, like the reconnect step). An
// UNREADABLE build id in the placed DLL REFUSES (m357 A) — a real build always
// embeds VL_BUILD_ID, so an id-less DLL is a build anomaly that must never
// pass through as "verified".
func verifyAddonPlace(docsDir, wantBuild string) error {
	if !validBuildID(wantBuild) {
		return nil
	}
	got, err := addonbuild.BuildIDFromDLL(filepath.Join(docsDir, "bin", "Custom", addonDllName))
	if err != nil {
		return fmt.Errorf("addon place: read back the placed DLL's build id: %w", err)
	}
	if got == "" {
		return fmt.Errorf("addon place: cannot read a build id from the placed AddOn — refusing")
	}
	if got != wantBuild {
		return fmt.Errorf("addon place: the placed DLL embeds build %q, the release expects %q — refusing", got, wantBuild)
	}
	return nil
}

// preJobBuild is the AddOn build the box ran BEFORE this job: the pre-job
// maintenance ack, recorded in the job (m357 D). For an install it is the nt8
// decision's acked build; for a catch-up it is Addon.PreJobBuild.
func (w *Worker) preJobBuild(j updaterjob.Job) string {
	if j.Addon != nil && j.Addon.PreJobBuild != "" {
		return j.Addon.PreJobBuild
	}
	if j.NT8 != nil {
		return j.NT8.AckedBuildID
	}
	return ""
}

// wantAddonBuild is the ONE resolution of the expected AddOn build for a job
// (mirrors stepAddonReconnect): a catch-up must match the VERIFIED installed
// release's signed-manifest build (persisted on the job at creation — m355,
// never oldInstallBuildID()), an install must match the signed manifest's
// addon.build_id.
func (w *Worker) wantAddonBuild(j updaterjob.Job) string {
	if j.Catchup {
		if j.Addon != nil {
			return j.Addon.ExpectedBuild
		}
		return ""
	}
	if j.NT8 != nil {
		return j.NT8.ManifestBuildID
	}
	return ""
}

// quarantineUnverifiedAddon (m357 C) MOVES a placed-but-unverified VL.AddOn.dll
// aside into the backup dir, so NT8 never loads it next start. Moves only,
// never deletes. The job then ends recovery_needed with a loud reason.
func quarantineUnverifiedAddon(docsDir, backupDir string) error {
	dest := filepath.Join(docsDir, "bin", "Custom", addonDllName)
	if !isRegularFile(dest) {
		return nil // nothing placed — nothing to quarantine
	}
	aside := filepath.Join(backupDir, "VL.AddOn.dll.unverified")
	if err := os.Rename(dest, aside); err != nil {
		return fmt.Errorf("move the unverified AddOn aside: %w", err)
	}
	return nil
}
