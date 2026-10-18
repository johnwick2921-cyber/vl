package trader

// W-FRIEND-HOSTED B5 S12 (DS-106) — "owner box unchanged", part (a): with NO
// connect.json the AddOn loader returns today's defaults (127.0.0.1:36974) and
// sends NO connect key. Pinned by source (the same pattern as
// split_cs_pin_test.go) because NT8 only compiles on the owner's F5 — a drift
// in the .cs must fail this suite.

import (
	"os"
	"strings"
	"testing"
)

const loaderSourcePath = "../ninjascript/VLTraderTCPClient.cs"

func readLoaderSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(loaderSourcePath)
	if err != nil {
		t.Fatalf("cannot read the AddOn source: %v", err)
	}
	return string(b)
}

// TestAddonLoaderDefaultsWhenNoConnectJson pins the no-connect.json behaviour:
// host/port default to 127.0.0.1:36974 and the connect key stays empty.
// Mutant: seed a non-loopback host or a literal key → RED.
func TestAddonLoaderDefaultsWhenNoConnectJson(t *testing.T) {
	src := readLoaderSource(t)
	for _, want := range []string{
		`GO_SERVER_HOST_DEFAULT  = "127.0.0.1"`,
		`GO_SERVER_PORT_DEFAULT  = 36974`,
		`serverHost             = GO_SERVER_HOST_DEFAULT`,
		`serverPort             = GO_SERVER_PORT_DEFAULT`,
		`connectKey             = ""`,
		// absent file keeps the defaults (today's behaviour)
		`if (!File.Exists(path)) return; // absent -> defaults (today's behaviour)`,
		// the key is set ONLY from a non-empty connect.json field
		`if (!string.IsNullOrEmpty(key)) connectKey = key;`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn loader lost its no-connect.json default: missing %q", want)
		}
	}
	// No literal connect key may be seeded in the source.
	if strings.Contains(src, `connectKey = "`) {
		t.Errorf("AddOn loader seeds a literal connect key — it must default empty")
	}
}
