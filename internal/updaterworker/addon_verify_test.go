package updaterworker

import (
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
	if err := verifyAddonBackup(backupDir); err != nil {
		t.Fatalf("a backup whose proof matches must pass: %v", err)
	}

	// Tampered proof (wrong sha) refuses.
	tampered := []byte(`{"sha256":"` + strings.Repeat("0", 64) + `","build_id":"2026-10-18-a"}`)
	if err := os.WriteFile(filepath.Join(backupDir, addonbuild.AddonBackupMetaName), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyAddonBackup(backupDir); err == nil {
		t.Fatal("a backup whose sha no longer matches its proof must be refused")
	}

	// Missing proof refuses.
	if err := os.Remove(filepath.Join(backupDir, addonbuild.AddonBackupMetaName)); err != nil {
		t.Fatal(err)
	}
	if err := verifyAddonBackup(backupDir); err == nil {
		t.Fatal("a backup with no recorded proof must be refused")
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
