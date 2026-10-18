package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"vl/internal/updaterworker"
)

// verify.go — B5 S9: the friend app verifies EVERY download (the SSHSIG
// signature over the manifest, then the SHA-256 of each artifact) before
// installing anything. A tampered manifest or artifact refuses; nothing changes
// on the friend's PC.

// FriendManifest is the friend manifest JSON (B2 contract). The URLs are
// RELATIVE paths on the friend's own bot; the app resolves them against the
// install code's base URL.
type FriendManifest struct {
	AddonBuild  string `json:"addon_build"`
	AddonSHA256 string `json:"addon_sha256"`
	AddonURL    string `json:"addon_url"`
	AppVersion  string `json:"app_version"`
	AppSHA256   string `json:"app_sha256"`
	AppURL      string `json:"app_url"`
}

// VerifyFriendManifest verifies the manifest bytes against the armored SSHSIG
// signature with the release trust anchor (the same VerifySSHSIG the release
// worker uses), then validates every SHA-256 field is well-formed. The signature
// is checked FIRST — a manifest whose signature fails is never parsed.
func VerifyFriendManifest(manifestBytes, sigBytes []byte, allowedSignersPath string) (FriendManifest, error) {
	if _, err := updaterworker.VerifySSHSIG(manifestBytes, sigBytes, allowedSignersPath); err != nil {
		return FriendManifest{}, fmt.Errorf("friend manifest signature: %w", err)
	}
	var m FriendManifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return FriendManifest{}, fmt.Errorf("friend manifest JSON: %w", err)
	}
	if m.AddonBuild == "" || m.AppVersion == "" {
		return FriendManifest{}, fmt.Errorf("friend manifest: missing addon_build or app_version")
	}
	for _, h := range []struct{ name, v string }{
		{"addon_sha256", m.AddonSHA256},
		{"app_sha256", m.AppSHA256},
	} {
		if !sha256HexRe.MatchString(h.v) {
			return FriendManifest{}, fmt.Errorf("friend manifest: %s is not a sha256", h.name)
		}
	}
	return m, nil
}

// VerifySHA256 streams r and checks its SHA-256 against wantHex (64 lowercase
// hex). It returns an error on any mismatch, so a bad download never installs.
func VerifySHA256(r io.Reader, wantHex string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantHex {
		return fmt.Errorf("artifact sha256 mismatch: got %s, want %s", got, wantHex)
	}
	return nil
}

// encodeInstallCode is the inverse of ParseInstallCode, used by tests and by the
// provisioning tooling (never by the app at runtime).
func encodeInstallCode(ic InstallCode) string {
	raw, _ := json.Marshal(ic)
	return base64.RawURLEncoding.EncodeToString(raw)
}
