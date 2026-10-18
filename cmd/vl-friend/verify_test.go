package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// verify_test.go — B5 S9. The manifest must be signed with the friend-
// distribution key and verified (ported SSHSIG) before anything is installed.
// A tampered manifest or artifact refuses; nothing changes.

// signManifest generates an ed25519 key, signs msg with
// `ssh-keygen -Y sign -n release`, and returns (manifest, sig, allowedSigners
// CONTENT — the public key line the app embeds at build time).
func signManifest(t *testing.T, msg []byte) ([]byte, []byte, []byte) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, bytes.TrimSpace(out))
		}
	}
	run("ssh-keygen", "-t", "ed25519", "-f", key, "-N", "", "-C", "test")
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	signers := []byte("release " + strings.TrimSpace(string(pub)) + "\n")
	msgPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(msgPath, msg, 0o644); err != nil {
		t.Fatal(err)
	}
	run("ssh-keygen", "-Y", "sign", "-f", key, "-n", "release", msgPath)
	sig, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	return msg, sig, signers
}

func testManifest() []byte {
	return []byte(`{"addon_build":"37","addon_sha256":"` + strings.Repeat("ab", 32) +
		`","addon_url":"/api/friend/artifact/addon","app_version":"1.0.0","app_sha256":"` +
		strings.Repeat("cd", 32) + `","app_url":"/api/friend/artifact/app"}` + "\n")
}

func TestVerifyFriendManifestAcceptsASignedManifest(t *testing.T) {
	msg, sig, signers := signManifest(t, testManifest())
	m, err := VerifyFriendManifest(msg, sig, signers)
	if err != nil {
		t.Fatalf("a correctly signed manifest refused: %v", err)
	}
	if m.AddonBuild != "37" || m.AppVersion != "1.0.0" {
		t.Fatalf("parsed fields wrong: %+v", m)
	}
}

// TestVerifyFriendManifestRefusesTamper — S9: the named mutant. If the manifest
// is changed after signing (here: a different addon_sha256), the signature no
// longer matches and VerifyFriendManifest refuses. MUTANT: drop the
// verifySSHSIG call → this test RED (the tampered manifest is accepted).
func TestVerifyFriendManifestRefusesTamper(t *testing.T) {
	msg, sig, signers := signManifest(t, testManifest())
	tampered := bytes.Replace(msg, []byte(strings.Repeat("ab", 32)), []byte(strings.Repeat("11", 32)), 1)
	if _, err := VerifyFriendManifest(tampered, sig, signers); err == nil {
		t.Fatal("a tampered manifest was accepted — the signature check is missing")
	}
}

func TestVerifyFriendManifestRefusesBadSignature(t *testing.T) {
	msg, _, signers := signManifest(t, testManifest())
	other := []byte("-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----\n")
	if _, err := VerifyFriendManifest(msg, other, signers); err == nil {
		t.Fatal("a garbage signature was accepted")
	}
}

func TestVerifyFriendManifestRefusesForeignKey(t *testing.T) {
	msg, sig, _ := signManifest(t, testManifest())
	// A signature made by a key that is NOT embedded must refuse.
	foreign := []byte("release ssh-ed25519 " +
		"AAAAC3NzaC1lZDI1NTE5AAAAIBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")
	if _, err := VerifyFriendManifest(msg, sig, foreign); err == nil {
		t.Fatal("a manifest signed by a foreign key was accepted")
	}
}

func TestVerifyFriendManifestRefusesEmptySigners(t *testing.T) {
	msg, sig, _ := signManifest(t, testManifest())
	if _, err := VerifyFriendManifest(msg, sig, nil); err == nil {
		t.Fatal("a manifest verified against no embedded key was accepted")
	}
}

func TestVerifyFriendManifestRefusesBadSHAFields(t *testing.T) {
	bad := []byte(`{"addon_build":"37","addon_sha256":"zzz","addon_url":"/x","app_version":"1","app_sha256":"` +
		strings.Repeat("cd", 32) + `","app_url":"/y"}` + "\n")
	msg, sig, signers := signManifest(t, bad)
	if _, err := VerifyFriendManifest(msg, sig, signers); err == nil {
		t.Fatal("a manifest with a non-sha256 addon_sha256 was accepted")
	}
}

// TestVerifyFriendManifestRejectsUnknownField — R8: the manifest is build
// metadata ONLY. An extra key (a host / key / account) must refuse, even when
// the signature is valid: the app must not carry or trust smuggled fields.
func TestVerifyFriendManifestRejectsUnknownField(t *testing.T) {
	extra := `{"addon_build":"37","addon_sha256":"` + strings.Repeat("ab", 32) +
		`","addon_url":"/api/friend/artifact/addon","app_version":"1.0.0","app_sha256":"` +
		strings.Repeat("cd", 32) + `","app_url":"/api/friend/artifact/app","host":"100.64.0.2"}` + "\n"
	msg, sig, signers := signManifest(t, []byte(extra))
	if _, err := VerifyFriendManifest(msg, sig, signers); err == nil {
		t.Fatal("a manifest with an extra host field was accepted")
	}
}

// TestVerifyFriendManifestRejectsMissingField — a manifest missing one of the
// six R8 fields refuses.
func TestVerifyFriendManifestRejectsMissingField(t *testing.T) {
	missing := `{"addon_build":"37","addon_sha256":"` + strings.Repeat("ab", 32) +
		`","addon_url":"/api/friend/artifact/addon","app_version":"1.0.0","app_sha256":"` +
		strings.Repeat("cd", 32) + `}` + "\n"
	msg, sig, signers := signManifest(t, []byte(missing))
	if _, err := VerifyFriendManifest(msg, sig, signers); err == nil {
		t.Fatal("a manifest missing app_url was accepted")
	}
}

func TestVerifySHA256(t *testing.T) {
	content := []byte("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" // sha256("hello")
	if err := VerifySHA256(bytes.NewReader(content), want); err != nil {
		t.Fatalf("matching sha256 refused: %v", err)
	}
	if err := VerifySHA256(bytes.NewReader(content), strings.Repeat("00", 32)); err == nil {
		t.Fatal("a mismatched sha256 was accepted")
	}
}
