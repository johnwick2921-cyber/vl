package ninjatrader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew_Smoke(t *testing.T) {
	dir := t.TempDir()
	tr := New(Config{DataDir: dir, Symbol: "MNQ"})
	if tr == nil {
		t.Fatal("New returned nil")
	}
}

func TestOpenLong_WritesSignal(t *testing.T) {
	dir := t.TempDir()
	tr := New(Config{DataDir: dir, Symbol: "MNQ"})

	// Stash SL/TP first — they get bundled into the order on OpenLong.
	_ = tr.SetStopLoss("MNQ", "LONG", 1, 21480.00)
	_ = tr.SetTakeProfit("MNQ", "LONG", 1, 21540.00)

	res, err := tr.OpenLong("MNQ", 1, 1)
	if err != nil {
		t.Fatalf("OpenLong: %v", err)
	}
	if res["status"] != "submitted" {
		t.Errorf("status = %v, want submitted", res["status"])
	}

	body, _ := os.ReadFile(filepath.Join(dir, "trade_signals.csv"))
	if !strings.Contains(string(body), "LONG") {
		t.Errorf("signal file missing LONG row:\n%s", string(body))
	}
	if !strings.Contains(string(body), "21480.00") || !strings.Contains(string(body), "21540.00") {
		t.Errorf("signal file missing SL/TP:\n%s", string(body))
	}
}
