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
	mustWrite(t, filepath.Join(stage, "vl-bin"), "x")
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

// F3 (release #34): the VL_BUILD_ID extraction is ANCHORED to the const
// declaration line. A comment that mentions VL_BUILD_ID placed BEFORE the const
// must not change the result — the manifest reads the const, never prose.
// Mutant: the old unanchored grep (VL_BUILD_ID[^"]*"[^"]+") → RED.
func TestManifestAddonBuildAnchorsToTheConstLine(t *testing.T) {
	stage := t.TempDir()
	mustWrite(t, filepath.Join(stage, "vl-bin"), "x")
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	mustWrite(t, filepath.Join(stage, "deploy", "RELEASE"), sha+"\n")
	if err := os.MkdirAll(filepath.Join(stage, "ninjascript"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A comment naming VL_BUILD_ID sits BEFORE the const. The anchored grep must
	// skip it and read the const's value.
	mustWrite(t, filepath.Join(stage, "ninjascript", "VLTraderTCPClient.cs"),
		"// VL_BUILD_ID is declared below; the build id was once \"1999-01-01-comment\".\n"+
			"// const string VL_BUILD_ID = \"1999-01-01-comment\";\n"+
			// A NON-comment code line that mentions VL_BUILD_ID with a quoted
			// value (DS-107 gate 10-17: the comment strip alone hid the anchor —
			// only the const-declaration anchor rejects this line).
			`Log("VL_BUILD_ID is \"1999-01-01-codeline\"");`+"\n"+
			`private const string  VL_BUILD_ID             = "2026-09-30-anchor";`+"\n")

	out, err := runScript(t, "deploy/release/manifest.sh", stage, sha, "v9.9.9")
	if err != nil {
		t.Fatalf("manifest failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"build_id": "2026-09-30-anchor"`) {
		t.Fatalf("the manifest must record the CONST's build id (2026-09-30-anchor), got:\n%s", out)
	}
	if strings.Contains(out, "1999-01-01-comment") {
		t.Fatalf("the manifest read a comment as the build id (1999-01-01-comment):\n%s", out)
	}
	if strings.Contains(out, "1999-01-01-codeline") {
		t.Fatalf("the manifest read a non-const code line as the build id (1999-01-01-codeline):\n%s", out)
	}
}

// F3 (release #34): an absent const string VL_BUILD_ID declaration is a
// REFUSAL, never "n/a".
func TestManifestAddonBuildFailsWhenTheConstIsAbsent(t *testing.T) {
	stage := t.TempDir()
	mustWrite(t, filepath.Join(stage, "vl-bin"), "x")
	if err := os.MkdirAll(filepath.Join(stage, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	mustWrite(t, filepath.Join(stage, "deploy", "RELEASE"), sha+"\n")
	if err := os.MkdirAll(filepath.Join(stage, "ninjascript"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only a comment — no const declaration at all.
	mustWrite(t, filepath.Join(stage, "ninjascript", "VLTraderTCPClient.cs"),
		"// VL_BUILD_ID used to live here, but the const is gone.\n")

	out, err := runScript(t, "deploy/release/manifest.sh", stage, sha, "v9.9.9")
	if err == nil {
		t.Fatalf("the manifest must REFUSE when the const is absent, got success:\n%s", out)
	}
	if !strings.Contains(out, "REFUSED") || !strings.Contains(out, "VL_BUILD_ID") {
		t.Fatalf("the refusal must name the missing const, got:\n%s", out)
	}
}
