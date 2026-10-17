package updatereadiness

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

// THE ONE RULE guard (owner order 2026-10-17): every test package runs with
// HOME pointed at a throwaway folder and the run FAILS if the real home
// changed. internal/homeguard fails the build when a test package is missing
// this TestMain.
func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }
