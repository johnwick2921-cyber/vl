package safepath

import (
	"os"
	"strings"
	"testing"
)

// HOLE PROBE: internal tests live in package safepath, but the TestMain that
// mounts testhome.Guard is in package safepath_test (homeguard_main_test.go).
// Go only runs a TestMain declared in the package under test — so the internal
// tests here may run with the REAL HOME. If so, this probe fails.
func TestProbeInternalTestsAreHomeRedirected(t *testing.T) {
	h := os.Getenv("HOME")
	if !strings.Contains(h, "vl-testhome-") {
		t.Fatalf("HOLE: internal tests (package safepath) run with HOME=%q — the TestMain in package safepath_test does not govern this package, so these tests can reach the real home", h)
	}
}
