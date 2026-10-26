package mentor

// THE ONE RULE guard (owner order 2026-10-17, after the 2026-10-16 wipe of
// /home/hoang): this package's tests run with HOME pointed at a throwaway
// folder, and the run FAILS if anything in the real home folder was deleted
// or an updater path changed. internal/homeguard fails the build when a test
// package is missing this TestMain.

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

func TestMain(m *testing.M) {
	flatNearScale = 0 // m428: see flatNearScale — R2 pins opt in with withFlatNear
	os.Exit(testhome.Guard(m))
}
