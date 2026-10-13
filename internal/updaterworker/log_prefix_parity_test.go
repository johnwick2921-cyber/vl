package updaterworker

import (
	"path/filepath"
	"testing"
	"time"

	"vl/logger"
)

// F2 (D2 PRE-CHECK): the updater predicts the boot log from the binary name
// (logPrefixForBinary) and the bot's logger names its file with LogFileNameFor.
// If the writer and the predictor ever disagree the updater cannot see the boot
// line and ROLLS BACK A GOOD INSTALL. This parity test calls BOTH production
// functions on a fixed date and pins the same prefix.
func TestLogPrefixParityWriterPredictor(t *testing.T) {
	fixed := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)

	writerFile := logger.LogFileNameFor("data", fixed)
	writerPrefix := filepath.Base(writerFile)[:3] // "vl_"

	predictorPrefix := logPrefixForBinary(filepath.Join("some", "install", "vl-bin"))

	if writerPrefix != "vl_" {
		t.Fatalf("logger writes prefix %q (file %s) — the bot writes vl_ from the R2 boot on", writerPrefix, writerFile)
	}
	if predictorPrefix != "vl_" {
		t.Fatalf("predictor says %q for vl-bin", predictorPrefix)
	}
	if writerPrefix != predictorPrefix {
		t.Fatalf("PARITY BROKEN: logger writes %q, updater predicts %q — a good install would be rolled back", writerPrefix, predictorPrefix)
	}

	// predictedLog for the same instant must be exactly <dir>/<prefix><CT date>.log
	// — the CT date comes from logger.CTDate, the SAME function the writer uses,
	// so the pair agrees on any machine zone.
	want := filepath.Join("data", "vl_"+logger.CTDate(fixed)+".log")
	if got := (&Worker{cfg: Config{Target: Target{LogDir: "data"}}}).predictedLog(fixed, filepath.Join("some", "install", "vl-bin")); got != want {
		t.Fatalf("predictedLog = %q, want %q", got, want)
	}
}
