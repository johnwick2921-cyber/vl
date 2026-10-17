package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F1 (D2-OPS-REST fold): manifest.sh must read the AddOn build id from
// ninjascript/VLTraderTCPClient.cs — NOT `head -1` over the *.cs glob. The day
// a second .cs file sorts first (e.g. AAA.cs), the glob form records the WRONG
// file's build id and the install verifies the wrong AddOn. The production grep
// is pinned; this test proves the pin at the production call site (canon 53).
func TestManifestAddonBuildReadsThePinnedFileNotTheGlob(t *testing.T) {
	stage := t.TempDir()
	mustWrite(t, filepath.Join(stage, "nofx-bin"), "x")
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	mustWrite(t, filepath.Join(stage, "deploy", "RELEASE"), sha+"\n")
	if err := os.MkdirAll(filepath.Join(stage, "ninjascript"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The OTHER file sorts first (AAA < VLTraderTCPClient) and carries a
	// DIFFERENT build id — `head -1` over the glob would record aaa's value.
	mustWrite(t, filepath.Join(stage, "ninjascript", "AAA.cs"),
		`private const string  VL_BUILD_ID             = "2099-01-01-aaa";`+"\n")
	mustWrite(t, filepath.Join(stage, "ninjascript", "VLTraderTCPClient.cs"),
		`private const string  VL_BUILD_ID             = "2026-09-30-pin";`+"\n")

	out, err := runScript(t, "deploy/release/manifest.sh", stage, sha, "v9.9.9")
	if err != nil {
		t.Fatalf("manifest failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"build_id": "2026-09-30-pin"`) {
		t.Fatalf("the manifest must record the PINNED file's build id (2026-09-30-pin), got:\n%s", out)
	}
	if strings.Contains(out, "2099-01-01-aaa") {
		t.Fatalf("the manifest recorded the OTHER file's build id (2099-01-01-aaa):\n%s", out)
	}
}
