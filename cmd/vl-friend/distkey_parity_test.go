package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedKeyMatchesRealDistKey is the production parity test for B10.2:
// the friendAllowedSigners constant baked into the binary MUST be the public
// half of the real friend-distribution key (/home/hoang/.vl-friend-dist/key).
// It signs a manifest with that real key and verifies it against the embedded
// constant — catching the class-53 trap where the unit tests sign with a
// throwaway key and pass that same throwaway key back (self-consistency only).
// Skipped where the key does not exist (CI, other boxes): the key is created
// once on the box by DS-108 and is never in git.
func TestEmbeddedKeyMatchesRealDistKey(t *testing.T) {
	dir := "/home/hoang/.vl-friend-dist"
	key := filepath.Join(dir, "key")
	if _, err := os.Stat(key); err != nil {
		t.Skip("friend-distribution key not present on this box")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	pub, err := os.ReadFile(filepath.Join(dir, "key.pub"))
	if err != nil {
		t.Fatal(err)
	}
	want := "release " + strings.TrimSpace(string(pub)) + "\n"
	if friendAllowedSigners != want {
		t.Fatalf("embedded friendAllowedSigners does not match the real key.pub:\n got: %q\nwant: %q", friendAllowedSigners, want)
	}

	tmp := t.TempDir()
	msg := testManifest()
	msgPath := filepath.Join(tmp, "manifest.json")
	if err := os.WriteFile(msgPath, msg, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", "release", msgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen sign: %v: %s", err, bytes.TrimSpace(out))
	}
	sig, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyFriendManifest(msg, sig, []byte(friendAllowedSigners)); err != nil {
		t.Fatalf("the real dist key's signature did not verify against the embedded key: %v", err)
	}
}
