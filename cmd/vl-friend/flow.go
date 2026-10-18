package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

// flow.go — the install / update orchestration. Pure logic over the deps
// seams (osdeps.go): no direct os/exec, network or file access here, so the
// ordering (verify BEFORE write, flat BEFORE swap) is provable in tests.

// baseURLFromInstallCode is the friend bot's web base (B2: web_port on the
// tunnel host).
func baseURLFromInstallCode(ic InstallCode) string {
	return fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
}

// flatProbeResult is the friend-mode-only flat signal (CTO ruling 2026-10-18):
// GET /api/friend/flat → {"flat":true|false,"at":<unix ms>} — nothing else.
type flatProbeResult struct {
	Flat bool `json:"flat"`
}

// probeFlat asks the friend bot's /api/friend/flat endpoint and maps it to the
// positionsProvider contract: 0 = flat, non-zero = open. Any probe error is an
// error (fail closed): the caller's waitUntilFlat treats it as "not flat" and
// defers, never swapping mid-trade.
func probeFlat(ctx context.Context, d deps, base string) (float64, error) {
	b, err := d.fetch(ctx, base+"/api/friend/flat")
	if err != nil {
		return 0, fmt.Errorf("flat probe: %w", err)
	}
	var r flatProbeResult
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, fmt.Errorf("flat probe: %w", err)
	}
	if r.Flat {
		return 0, nil
	}
	return 1, nil
}

// fetchManifest GETs the signed manifest and verifies it against the embedded
// friend-distribution key. The signature is checked BEFORE the JSON is used.
func fetchManifest(ctx context.Context, d deps, base string) (FriendManifest, error) {
	man, err := d.fetch(ctx, base+"/api/friend/manifest")
	if err != nil {
		return FriendManifest{}, fmt.Errorf("fetch manifest: %w", err)
	}
	sig, err := d.fetch(ctx, base+"/api/friend/manifest.sig")
	if err != nil {
		return FriendManifest{}, fmt.Errorf("fetch manifest signature: %w", err)
	}
	m, err := VerifyFriendManifest(man, sig, d.allowedSigners)
	if err != nil {
		return FriendManifest{}, err
	}
	return m, nil
}

// resolveArtifactURL resolves a manifest-relative artifact URL against base.
func resolveArtifactURL(base, rel string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	relU, err := url.Parse(rel)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(relU).String(), nil
}

// fetchVerifiedAddon downloads the add-on and checks its SHA-256 BEFORE it is
// returned (S9: verify before install).
func fetchVerifiedAddon(ctx context.Context, d deps, base string, m FriendManifest) ([]byte, error) {
	u, err := resolveArtifactURL(base, m.AddonURL)
	if err != nil {
		return nil, fmt.Errorf("addon url: %w", err)
	}
	b, err := d.fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch addon: %w", err)
	}
	if err := VerifySHA256(bytes.NewReader(b), m.AddonSHA256); err != nil {
		return nil, fmt.Errorf("addon: %w", err)
	}
	return b, nil
}

// installFirstRun runs the first-run install: (a) Tailscale, (b) connect.json,
// (c) download + verify + place the add-on, (d) the desktop shortcut.
func installFirstRun(ctx context.Context, d deps, code string) error {
	ic, err := ParseInstallCode(code)
	if err != nil {
		return err
	}
	home, err := d.homeDir()
	if err != nil {
		return err
	}
	if err := ensureTailscale(ctx, d, ic.TSAuthKey); err != nil {
		return err
	}
	if err := writeConnectJSON(d, home, ic); err != nil {
		return err
	}
	base := baseURLFromInstallCode(ic)
	m, err := fetchManifest(ctx, d, base)
	if err != nil {
		return err
	}
	addon, err := fetchVerifiedAddon(ctx, d, base, m)
	if err != nil {
		return err
	}
	if err := d.mkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		return err
	}
	if err := d.writeFile(addonDstPath(home), addon, 0o644); err != nil {
		return err
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
		return err
	}
	return createDesktopShortcut(ctx, d, ic)
}

// ensureTailscale installs Tailscale silently when missing, then joins the
// tailnet with the one-time auth key from the install code.
func ensureTailscale(ctx context.Context, d deps, authKey string) error {
	if _, err := d.exec(ctx, "tailscale", "status"); err != nil {
		// Not installed: run the official MSI silently (B2 outside ref: the
		// Tailscale silent Windows install). The installer path is provided by
		// the Windows runner.
		if _, err := d.exec(ctx, "tailscaleInstall", authKey); err != nil {
			return fmt.Errorf("tailscale install: %w", err)
		}
	}
	if _, err := d.exec(ctx, "tailscale", "up", "--authkey="+authKey); err != nil {
		return fmt.Errorf("tailscale up: %w", err)
	}
	return nil
}

// createDesktopShortcut makes the VL desktop icon open the friend's dashboard
// in its own app window (msedge --app). The PowerShell one-liner is invoked
// through d.exec so tests assert the effect without Windows.
func createDesktopShortcut(ctx context.Context, d deps, ic InstallCode) error {
	appURL := fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
	ps := "$s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:USERPROFILE+'\\Desktop\\VL.lnk');" +
		"$s.TargetPath='msedge';$s.Arguments='--app=" + appURL + "';$s.Save()"
	if _, err := d.exec(ctx, "powershell", "-NoProfile", "-Command", ps); err != nil {
		return fmt.Errorf("desktop shortcut: %w", err)
	}
	return nil
}

// UpdateDecision is what RunUpdateCheck reports.
type UpdateDecision struct {
	Manifest             FriendManifest
	AddonUpdateAvailable bool
	AppUpdateAvailable   bool
	Applied              bool
}

// RunUpdateCheck fetches the signed manifest, compares it with what is
// installed (and the app's own version), and — when an update is available and
// approved — verifies, waits until flat, then swaps the add-on (old moved to
// VL\old\, never deleted).
func RunUpdateCheck(ctx context.Context, d deps, base string, autoApprove bool) (UpdateDecision, error) {
	var dec UpdateDecision
	m, err := fetchManifest(ctx, d, base)
	if err != nil {
		return dec, err
	}
	dec.Manifest = m
	home, err := d.homeDir()
	if err != nil {
		return dec, err
	}
	cur, err := readInstalledState(d, home)
	if err != nil {
		return dec, err
	}
	dec.AddonUpdateAvailable = addonBuildNewer(m.AddonBuild, cur.AddonBuild)
	dec.AppUpdateAvailable = appVersionNewer(m.AppVersion, appVersion)
	if !dec.AddonUpdateAvailable && !dec.AppUpdateAvailable {
		return dec, nil
	}
	if !autoApprove {
		ans, err := d.readLine("VL update available — Update now? (y/N)")
		if err != nil {
			return dec, err
		}
		if ans != "y" && ans != "Y" && ans != "yes" {
			return dec, nil // Later: nothing changes
		}
	}
	// Verify again, then download the new add-on (S9: a bad signature or hash
	// refuses here and nothing below runs).
	addon, err := fetchVerifiedAddon(ctx, d, base, m)
	if err != nil {
		return dec, err
	}
	// Never update during a trade: wait until the friend bot is flat.
	if err := waitUntilFlat(ctx, d.positions, 60*time.Second); err != nil {
		return dec, err
	}
	// Swap: move the current add-on aside (never delete), write the new one.
	if err := d.mkdirAll(filepath.Dir(oldAddonPath(home)), 0o755); err != nil {
		return dec, err
	}
	_ = d.rename(addonDstPath(home), oldAddonPath(home)) // absent on first-ever update is fine
	if err := d.writeFile(addonDstPath(home), addon, 0o644); err != nil {
		return dec, err
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
		return dec, err
	}
	dec.Applied = true
	return dec, nil
}

// addonBuildNewer compares build numbers (numeric when both parse; string
// inequality otherwise; an empty current build means "never installed").
func addonBuildNewer(remote, current string) bool {
	if current == "" {
		return true
	}
	r, err1 := strconv.Atoi(remote)
	c, err2 := strconv.Atoi(current)
	if err1 == nil && err2 == nil {
		return r > c
	}
	return remote != current
}

// appVersionNewer compares semver "MAJOR.MINOR.PATCH"; falls back to string
// inequality for non-semver versions.
func appVersionNewer(remote, current string) bool {
	rp, err1 := parseSemver(remote)
	cp, err2 := parseSemver(current)
	if err1 == nil && err2 == nil {
		for i := 0; i < 3; i++ {
			if rp[i] != cp[i] {
				return rp[i] > cp[i]
			}
		}
		return false
	}
	return remote != current
}

func parseSemver(v string) ([3]int, error) {
	var out [3]int
	if _, err := fmt.Sscanf(v, "%d.%d.%d", &out[0], &out[1], &out[2]); err != nil {
		return out, err
	}
	return out, nil
}
