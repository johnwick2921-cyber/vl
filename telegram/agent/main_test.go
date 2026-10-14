package agent

import (
	"os"
	"testing"

	"vl/internal/testnet"
)

// TestMain keeps every test in this package OFF the live bot's :8080 (release
// #22 OPS-SAFETY): a dial there is refused and fails the run.
func TestMain(m *testing.M) { os.Exit(testnet.Guard(m)) }
