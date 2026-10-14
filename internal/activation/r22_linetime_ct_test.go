package activation

// R22 TIME-WEEKLY P3-3 — the logger writes its line stamps in America/Chicago
// (logger.CTLocation, since #488); lineTime parsed them in time.Local, which
// is CT only because of how the box is set up.

import (
	"testing"
	"time"
)

func TestR22LineTimeParsesCT(t *testing.T) {
	prev := time.Local
	time.Local = time.UTC // a box whose machine zone is not Chicago
	t.Cleanup(func() { time.Local = prev })

	got, ok := lineTime("10-14 02:10:19 [INFO] trader/auto_trader.go:36 boot")
	if !ok {
		t.Fatal("lineTime refused a well-formed line")
	}
	ct, _ := time.LoadLocation("America/Chicago")
	if c := got.In(ct); c.Hour() != 2 || c.Minute() != 10 || c.Second() != 19 {
		t.Fatalf("lineTime read 02:10:19 as %s CT — parsed in the machine zone, not the logger's CT", c.Format("15:04:05"))
	}
}
