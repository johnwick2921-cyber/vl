package safepath_test

import (
	"os"
	"testing"

	"vl/internal/testhome"
)

func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }
