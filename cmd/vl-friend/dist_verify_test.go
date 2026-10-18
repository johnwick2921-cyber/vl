package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDistManifestVerifiesWithEmbeddedKey is the production parity test for the
// dist package (DS-108 DIST PACKAGE): the manifest at
// /home/hoang/vl-friends/_dist/LATEST must verify through the PORTED verifier
// (VerifyFriendManifest) against the embedded friend-distribution public key,
// and must carry exactly the six R8 fields. Skipped where no dist exists (CI).
func TestDistManifestVerifiesWithEmbeddedKey(t *testing.T) {
	root := "/home/hoang/vl-friends/_dist"
	latest, err := os.ReadFile(filepath.Join(root, "LATEST"))
	if err != nil {
		t.Skip("no friend dist on this box")
	}
	dir := filepath.Join(root, strings.TrimSpace(string(latest)))
	man, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		t.Fatalf("manifest.json.sig: %v", err)
	}
	m, err := VerifyFriendManifest(man, sig, []byte(friendAllowedSigners))
	if err != nil {
		t.Fatalf("dist manifest failed the ported verifier: %v", err)
	}
	if m.AddonBuild == "" || m.AppVersion == "" {
		t.Fatalf("dist manifest parsed empty: %+v", m)
	}
}
