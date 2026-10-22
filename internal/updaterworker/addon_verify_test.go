package updaterworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/addonbuild"
	"vl/internal/updaterjob"
)

// fakeDLLBytes renders a minimal "DLL" whose bytes embed the build id as UTF-16LE
// (the .NET user-string form), the exact layout addonbuild.BuildIDFromDLL scans.
func fakeDLLBytes(id string) []byte {
	b := make([]byte, 0, 2*len(id)+2)
	for i := 0; i < len(id); i++ {
		b = append(b, id[i], 0)
	}
	return append(b, 0x00, 0x00)
}

func writeFakeDLL(t *testing.T, path, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fakeDLLBytes(id), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PIN m343 verify-on-place — a placed DLL whose embedded build id is not the
// release's expected build is REFUSED; a matching id passes; an empty expected
// id skips the check (the reconnect step's rule). MUTANT: drop the build-id
// comparison in verifyAddonPlace → the wrong-id case passes → RED.
func TestVerifyAddonPlaceRefusesWrongBuildID(t *testing.T) {
	docs := t.TempDir()
	dest := filepath.Join(docs, "bin", "Custom", addonDllName)
	writeFakeDLL(t, dest, "2026-10-18-a")

	if err := verifyAddonPlace(docs, "2026-10-21-c"); err == nil {
		t.Fatal("a placed DLL embedding 2026-10-18-a must be refused when the release expects 2026-10-21-c")
	}
	if err := verifyAddonPlace(docs, "2026-10-18-a"); err != nil {
		t.Fatalf("a matching build id must pass: %v", err)
	}
	if err := verifyAddonPlace(docs, ""); err != nil {
		t.Fatalf("an empty expected build must skip the check: %v", err)
	}
}

// PIN m357 A — an UNREADABLE build id in the placed DLL REFUSES (never a silent
// pass). MUTANT: return nil on the empty id in verifyAddonPlace → RED.
func TestVerifyAddonPlaceRefusesAnUnreadableBuildID(t *testing.T) {
	docs := t.TempDir()
	dest := filepath.Join(docs, "bin", "Custom", addonDllName)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("no build id here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyAddonPlace(docs, "2026-10-21-c"); err == nil {
		t.Fatal("a placed DLL with no readable build id must be refused")
	}
}

// PIN m343 verify-on-restore — AddonRestore only moves back a backup whose proof
// (sha256 + build id) was recorded and still hashes. MUTANT: drop the sha
// comparison in verifyAddonBackup → the tampered case passes → RED.
func TestVerifyAddonBackupRefusesUnverifiedProof(t *testing.T) {
	backupDir := t.TempDir()
	dll := filepath.Join(backupDir, addonDllName)
	writeFakeDLL(t, dll, "2026-10-18-a")
	if _, err := sha256File(dll); err != nil {
		t.Fatal(err)
	}

	// Matching proof passes.
	if werr := addonbuild.WriteAddonBackupMeta(backupDir, dll); werr != nil {
		t.Fatalf("write proof: %v", werr)
	}
	if err := verifyAddonBackup(backupDir, ""); err != nil {
		t.Fatalf("a backup whose proof matches must pass: %v", err)
	}

	// Tampered proof (wrong sha) refuses.
	tampered := []byte(`{"sha256":"` + strings.Repeat("0", 64) + `","build_id":"2026-10-18-a"}`)
	if err := os.WriteFile(filepath.Join(backupDir, addonbuild.AddonBackupMetaName), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyAddonBackup(backupDir, ""); err == nil {
		t.Fatal("a backup whose sha no longer matches its proof must be refused")
	}

	// Missing proof + no pre-job build refuses (a legacy backup cannot be
	// accepted against nothing).
	if err := os.Remove(filepath.Join(backupDir, addonbuild.AddonBackupMetaName)); err != nil {
		t.Fatal(err)
	}
	if err := verifyAddonBackup(backupDir, ""); err == nil {
		t.Fatal("a legacy backup with no recorded proof and no pre-job build must be refused")
	}
}

// PIN m357 D — a LEGACY backup (no addon-backup.meta) is accepted only when its
// embedded build id equals the pre-job running build. MUTANT: accept any
// sidecar-less backup → the wrong-build case passes → RED.
func TestVerifyAddonBackupAcceptsLegacyOnlyForThePreJobBuild(t *testing.T) {
	backupDir := t.TempDir()
	writeFakeDLL(t, filepath.Join(backupDir, addonDllName), "2026-10-21-c")

	if err := verifyAddonBackup(backupDir, "2026-10-21-c"); err != nil {
		t.Fatalf("a legacy backup whose embedded id equals the pre-job build must pass: %v", err)
	}
	if err := verifyAddonBackup(backupDir, "2026-10-18-a"); err == nil {
		t.Fatal("a legacy backup whose embedded id differs from the pre-job build must be refused")
	}
}

// PIN m343 boot_verified guard — once a job has moved PAST boot_verified
// (worker_swapped or complete), the AddOn must never be touched again. MUTANT:
// AddonProven returns true for boot_verified alone → the gate opens early → RED.
func TestAddonProvenGatesTheAddOn(t *testing.T) {
	j := updaterjob.Job{Transitions: []updaterjob.Transition{{State: updaterjob.StateBootVerified}}}
	if j.AddonProven() {
		t.Fatal("boot_verified alone must not prove the AddOn (the step may still fail)")
	}
	j.Transitions = append(j.Transitions, updaterjob.Transition{State: updaterjob.StateWorkerSwapped})
	if !j.AddonProven() {
		t.Fatal("worker_swapped proves the AddOn — a late touch must be refused")
	}
	j2 := updaterjob.Job{Transitions: []updaterjob.Transition{{State: updaterjob.StateComplete}}}
	if !j2.AddonProven() {
		t.Fatal("complete proves the AddOn")
	}
}

// PIN m357 C — a failed verify-on-place AND a failed restore must not leave the
// unverified NEW DLL live: it is moved aside (never deleted) and the job ends
// recovery_needed. MUTANT: drop quarantineUnverifiedAddon in stepAddonPlace →
// the unverified DLL stays in bin\Custom → RED.
func TestAddonPlaceQuarantinesUnverifiedDLLWhenRestoreFails(t *testing.T) {
	r := newRig(t, withCSChanged()) // manifest build = boxNewBuild
	// an OLD DLL so Place creates a backup dir (verify-on-place is owed)
	writeFakeDLL(t, filepath.Join(r.nt8docs, "bin", "Custom", addonDllName), boxOldBuild)
	wrong := filepath.Join(t.TempDir(), "wrong.dll")
	writeFakeDLL(t, wrong, boxOldBuild) // the "new" DLL embeds the OLD build — verify refuses
	sum, err := sha256File(wrong)
	if err != nil {
		t.Fatal(err)
	}
	j := updaterjob.Job{JobID: boxJobID,
		Release: &updaterjob.Release{Dir: r.relDir, SHA: boxNew},
		NT8:     &updaterjob.NT8Decision{Decision: updaterjob.NT8Updated, ManifestBuildID: boxNewBuild},
		Addon:   &updaterjob.AddonState{DllPath: wrong, Sha256: sum}}
	r.set(func() { r.addonRestoreErr = errAddonRestore })
	res := r.w.stepAddonPlace(context.Background(), j)
	r.noViolations(t)
	if res.failTo != updaterjob.StateRecoveryNeeded {
		t.Fatalf("failTo=%s, want recovery_needed (verify failed and restore failed)", res.failTo)
	}
	dest := filepath.Join(r.nt8docs, "bin", "Custom", addonDllName)
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the unverified DLL must not stay live in bin\\Custom (stat err %v)", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(r.nt8docs, "vl-addon-backups", boxJobID, "VL.AddOn.dll.unverified")); len(matches) != 1 {
		t.Fatalf("the unverified DLL must be moved aside, got %v", matches)
	}
}

// PIN m357 E — a job PAST boot_verified must do nothing in stepAddonPlace (the
// AddOn is proven; no late place). MUTANT: delete the AddonProven guard in
// stepAddonPlace → a proven job places again → RED.
func TestProvenJobStepAddonPlaceDoesNothing(t *testing.T) {
	r := newRig(t, withCSChanged())
	j := updaterjob.Job{JobID: boxJobID,
		Transitions: []updaterjob.Transition{{State: updaterjob.StateWorkerSwapped, Phase: updaterjob.PhaseDone}}}
	res := r.w.stepAddonPlace(context.Background(), j)
	r.noViolations(t)
	if res.err != nil {
		t.Fatalf("a proven job must be skipped, got err %v", res.err)
	}
	if len(res.receipts) != 1 || res.receipts[0].Evidence["skipped"] != "boot_verified" {
		t.Fatalf("a proven job must return a single boot_verified-skip receipt, got %+v", res.receipts)
	}
	if _, err := os.Stat(filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll")); !os.IsNotExist(err) {
		t.Fatalf("bin\\Custom must be untouched (stat err %v)", err)
	}
}

// PIN m357 E — the AddOn restore call sites (stepRevertClean, the rollback)
// are gated by the SAME AddonProven() predicate stepAddonPlace uses: a job
// that reached worker_swapped/complete must never touch the AddOn again.
// AddonProven() itself is pinned by TestAddonProvenGatesTheAddOn; the
// stepAddonPlace gate is pinned behaviorally by TestProvenJobStepAddonPlaceDoesNothing.
