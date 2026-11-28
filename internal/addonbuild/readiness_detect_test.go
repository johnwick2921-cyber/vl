package addonbuild_test

import (
	"os"
	"path/filepath"
	"testing"

	"vl/internal/addonbuild"
	"vl/internal/updatereadiness"
)

// The readiness list finds the NT8 Documents folder with its own DefaultDetect, which
// scans /mnt/c/Users/*/Documents only; the updater's addonbuild.Detect also scans the
// OneDrive-redirected path (m385). This pins the OneDrive half of "the readiness probe
// looks only in the default folders": here only Detect's globs can find the folder, so a
// DefaultDetect that asked addonbuild.Detect (reviewer mutant Y4) would return it. It
// lives in this external package because it needs Detect's unexported globs
// (SetDetectGlobsForTest, export_test.go) and updatereadiness imports addonbuild.
func TestReadinessDefaultDetectDoesNotLookInOneDrive(t *testing.T) {
	root := t.TempDir()
	od := filepath.Join(root, "alice", "OneDrive", "Tài liệu", "NinjaTrader 8")
	if err := os.MkdirAll(filepath.Join(od, "bin", "Custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	restore := addonbuild.SetDetectGlobsForTest(
		filepath.Join(root, "*", "Documents", "NinjaTrader 8"),
		filepath.Join(root, "*", "OneDrive", "*", "NinjaTrader 8"))
	defer restore()

	// Control: the updater's Detect finds the OneDrive folder.
	if got, err := addonbuild.Detect(); err != nil || got != od {
		t.Fatalf("addonbuild.Detect = %q, %v; want the OneDrive folder %q (the control is broken)", got, err, od)
	}
	got, err := updatereadiness.DefaultDetect()
	if got == od {
		t.Fatalf("updatereadiness.DefaultDetect = %q: the readiness list looked in OneDrive; the guide says it looks only in C:\\Users\\<you>\\Documents", got)
	}
	t.Logf("DefaultDetect = %q, %v (not the OneDrive folder)", got, err)
}
