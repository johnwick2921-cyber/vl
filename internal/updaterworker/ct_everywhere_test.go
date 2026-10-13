package updaterworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/logger"

	"github.com/sirupsen/logrus"
)

// TestPredictedLogAndBootcheckAcrossUTCCTDateBoundary pins CT-EVERYWHERE's
// updater half: at 2026-01-01 02:00 UTC the machine (UTC) date is 2026-01-01 but
// the CT date is 2025-12-31 (CST, UTC-6). The writer and the predictor must both
// name the CT date, and the boot check must then find the boot line in that
// CT-dated file — the proof that a good install is NOT rolled back on a box
// whose TZ is not Chicago. RED at base: predictedLog used time.Local, so under
// TZ=UTC it named vl_2026-01-01.log while the writer named the CT date.
func TestPredictedLogAndBootcheckAcrossUTCCTDateBoundary(t *testing.T) {
	instant := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)

	wantFile := filepath.Join("data", "vl_2025-12-31.log")
	if got := logger.LogFileNameFor("data", instant); got != wantFile {
		t.Fatalf("writer names %q, want %q", got, wantFile)
	}
	if got := (&Worker{cfg: Config{Target: Target{LogDir: "data"}}}).predictedLog(instant, filepath.Join("some", "install", "vl-bin")); got != wantFile {
		t.Fatalf("predictor names %q, want %q", got, wantFile)
	}

	// The boot check must find the boot line inside the CT-dated file. The line
	// is rendered by the production formatter (renderBoot) so the formatter's CT
	// timestamp and the file's CT date are the same pair the updater reads.
	sha := strings.Repeat("ab", 20)
	pid := os.Getpid() // kernel.BootIntegrity.Line() prints the real pid
	b := kernel.BootIntegrity{Revision: sha, Expected: sha, BuildTime: "2026-01-01T00:00:00Z", GoldensOK: true}
	line := renderBoot(t, b, logrus.InfoLevel)

	dir := t.TempDir()
	path := filepath.Join(dir, "vl_2025-12-31.log")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := map[string]string{}
	if err := verifyBootLine(path, 0, sha, pid, ev); err != nil {
		t.Fatalf("boot check across the UTC/CT date boundary: %v", err)
	}
}
