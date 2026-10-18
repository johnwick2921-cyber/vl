package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	updater "vl/internal/updaterworker"
)

// TestPortedVerifierParityWithLinux — CTO order 2026-10-18: the Windows-
// compilable port (verifySSHSIG) must produce the SAME accept/reject verdict as
// the Linux release verifier (internal/updaterworker.VerifySSHSIG) on identical
// input, against a REAL ssh-keygen signature (namespace + principal "release").
// This test file only compiles on Linux (it imports internal/updaterworker,
// which pulls Unix syscalls); the app binary itself never imports it, so the
// GOOS=windows build is unaffected.

func TestPortedVerifierParityWithLinux(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	dir := t.TempDir()
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
	}

	key := filepath.Join(dir, "k1")
	run("ssh-keygen", "-t", "ed25519", "-f", key, "-N", "", "-q")
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := []byte("release " + strings.TrimSpace(string(pub)) + "\n")
	allowedPath := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowedPath, allowed, 0o600); err != nil {
		t.Fatal(err)
	}

	msg := []byte(`{"addon_build":"37","addon_sha256":"` + strings.Repeat("ab", 32) +
		`","addon_url":"/x","app_version":"1.0.0","app_sha256":"` + strings.Repeat("cd", 32) + `","app_url":"/y"}` + "\n")

	sign := func(namespace, principal string, m []byte, k string) []byte {
		t.Helper()
		mp := filepath.Join(dir, "msg")
		if err := os.WriteFile(mp, m, 0o644); err != nil {
			t.Fatal(err)
		}
		run("ssh-keygen", "-Y", "sign", "-f", k, "-n", namespace, "-I", principal, mp)
		sig, err := os.ReadFile(mp + ".sig")
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}

	otherKey := filepath.Join(dir, "k2")
	run("ssh-keygen", "-t", "ed25519", "-f", otherKey, "-N", "", "-q")

	cases := []struct {
		name  string
		msg   []byte
		sig   []byte
		allow []byte
	}{
		{"valid", msg, sign("release", "release", msg, key), allowed},
		{"tampered message", append(msg, 'X'), sign("release", "release", msg, key), allowed},
		{"wrong namespace", msg, sign("other", "release", msg, key), allowed},
		{"wrong principal", msg, sign("release", "other", msg, key), allowed},
		{"foreign key", msg, sign("release", "release", msg, otherKey), allowed},
	}

	for _, tc := range cases {
		_, portedErr := verifySSHSIG(tc.msg, tc.sig, tc.allow)
		_, linuxErr := updater.VerifySSHSIG(tc.msg, tc.sig, allowedPath)
		if (portedErr == nil) != (linuxErr == nil) {
			t.Fatalf("%s: verdict mismatch — ported err=%v, linux err=%v", tc.name, portedErr, linuxErr)
		}
	}
}
