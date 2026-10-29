package datatruth

// THE ONE RULE guard (owner order 2026-10-17): see internal/retry.

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }
