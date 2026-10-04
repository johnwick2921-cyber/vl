package ninjatrader

import (
	"os"
	"testing"

	"vl/internal/testtmpfs"
)

// TestMain keeps t.TempDir() on tmpfs: the per-test SQLite migrations fsync a
// file on disk (~1.2 s each) and cost ~0.02 s on /dev/shm (internal/testtmpfs).
func TestMain(m *testing.M) { os.Exit(testtmpfs.Run(m.Run)) }
