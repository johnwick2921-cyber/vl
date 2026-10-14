package telegram

import (
	"os"
	"testing"

	"vl/internal/testnet"
)

// TestMain keeps every test in this package OFF the live bot's :8080 (release
// #22 OPS-SAFETY): the bot builds the agent on the configured API port.
func TestMain(m *testing.M) { os.Exit(testnet.Guard(m)) }
