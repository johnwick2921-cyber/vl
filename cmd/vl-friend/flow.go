package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
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
const (
	// tailscaleMSIURL is the official silent-installer URL (B2 outside ref:
	// Tailscale silent Windows install).
	tailscaleMSIURL = "https://pkgs.tailscale.com/stable/tailscale-setup-latest-amd64.msi"
	// tailscaleExeFallback is where the MSI installs the CLI; used when the bare
	// "tailscale" name is not on PATH (same lesson as W-UPDATER-WINPATH).
	tailscaleExeFallback = `C:\Program Files\Tailscale\tailscale.exe`
)

// The waits and poll intervals are vars so tests can shorten them.
var (
	tailscaleInstallWait  = 5 * time.Minute
	tailscaleApprovalWait = 15 * time.Minute
	tailscaleReadyPoll    = 2 * time.Second
	tailscaleApprovalPoll = 5 * time.Second
)

// tailscaleStatusJSON is the subset of `tailscale status --json` this flow reads.
type tailscaleStatusJSON struct {
	BackendState string `json:"BackendState"`
}

// tailscaleExec runs a tailscale subcommand, preferring the bare "tailscale"
// name and falling back to the fixed install path when the bare name is not on
// PATH.
func tailscaleExec(ctx context.Context, d deps, args ...string) ([]byte, error) {
	out, err := d.exec(ctx, "tailscale", args...)
	if err == nil {
		return out, nil
	}
	return d.exec(ctx, tailscaleExeFallback, args...)
}

// tailscaleBackendState returns Tailscale's BackendState ("NeedsLogin",
// "NeedsMachineAuth", "Running", …) from `tailscale status --json`.
func tailscaleBackendState(ctx context.Context, d deps) (string, error) {
	out, err := tailscaleExec(ctx, d, "status", "--json")
	if err != nil {
		return "", err
	}
	var st tailscaleStatusJSON
	if err := json.Unmarshal(out, &st); err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}
	return st.BackendState, nil
}

// redactSecrets replaces each secret with "***" so it never reaches stdout,
// stderr or the setup log. Never print the auth key or the connect key (P0 d).
func redactSecrets(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

// pauseOnError prints the (already-redacted) error and waits for Enter so a
// double-clicked console window does not close before the user reads it (P0 b).
// Testable on Linux through the readLine seam.
func pauseOnError(d deps, msg string) {
	fmt.Println("VL-Setup error:", msg)
	fmt.Println()
	fmt.Println("Press Enter to close")
	_, _ = d.readLine("")
}

// downloadTailscaleMSI downloads the official installer to a temp file and
// verifies its Authenticode signer is Tailscale Inc BEFORE anything is run.
// A bad or missing signature refuses (the MSI is never executed).
func downloadTailscaleMSI(ctx context.Context, d deps) (string, error) {
	b, err := d.fetch(ctx, tailscaleMSIURL)
	if err != nil {
		return "", fmt.Errorf("download Tailscale installer: %w", err)
	}
	dir, err := d.tempDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "tailscale-setup-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".msi")
	if err := d.writeFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("write Tailscale installer: %w", err)
	}
	sig, err := d.verifyMsiSigner(path)
	if err != nil {
		return "", fmt.Errorf("verify Tailscale installer signature: %w", err)
	}
	// REVIEW-BACK 2: a tampered MSI keeps its certificate (Status becomes
	// HashMismatch), so the STATUS is the tamper gate — and the subject must be
	// the exact org, not merely "contains tailscale".
	if sig.Status != "Valid" {
		return "", fmt.Errorf("refusing Tailscale installer: Authenticode status %q (not Valid)", sig.Status)
	}
	if !strings.Contains(sig.Subject, "O=Tailscale Inc.") {
		return "", fmt.Errorf("refusing Tailscale installer: unexpected signer %q (want O=Tailscale Inc.)", sig.Subject)
	}
	return path, nil
}

// waitTailscaleReady polls `tailscale status` until the daemon answers
// (bounded) — i.e. until the silent MSI install has completed far enough that
// the CLI exists and responds.
func waitTailscaleReady(ctx context.Context, d deps) error {
	t := time.NewTicker(tailscaleReadyPoll)
	defer t.Stop()
	deadline := time.Now().Add(tailscaleInstallWait)
	for {
		if _, err := tailscaleBackendState(ctx, d); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Tailscale did not become ready within %s after install", tailscaleInstallWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// tailscaleUp joins the tailnet with the one-time auth key: it refuses to
// --force-reauth over an existing login to ANOTHER network, and then waits
// (bounded) for device approval if the owner has approval enabled.
func tailscaleUp(ctx context.Context, d deps, authKey string) error {
	state, err := tailscaleBackendState(ctx, d)
	if err == nil && state != "NeedsLogin" {
		return fmt.Errorf("this PC is already logged in to another Tailscale network (state %q). Log out of Tailscale first, then run VL-Setup again", state)
	}
	out, err := tailscaleExec(ctx, d, "up", "--authkey="+authKey, "--unattended")
	if err != nil {
		// Redact the auth key from BOTH the error and the command output —
		// exec errors can echo the full command line (P0 d: never print keys).
		return fmt.Errorf("tailscale up: %s", redactSecrets(err.Error()+" "+string(out), authKey))
	}
	return waitTailscaleApproved(ctx, d)
}

// waitTailscaleApproved polls BackendState until Running; while NeedsMachineAuth
// it announces (once) that the owner must approve the device, then keeps polling
// up to a bounded deadline — never silently fails.
func waitTailscaleApproved(ctx context.Context, d deps) error {
	t := time.NewTicker(tailscaleApprovalPoll)
	defer t.Stop()
	deadline := time.Now().Add(tailscaleApprovalWait)
	announced := false
	for {
		state, err := tailscaleBackendState(ctx, d)
		if err == nil && state == "Running" {
			return nil
		}
		if err == nil && state == "NeedsMachineAuth" && !announced {
			fmt.Println("Waiting for the owner to approve this PC in Tailscale…")
			announced = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Tailscale device was not approved within %s", tailscaleApprovalWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// ensureTailscale makes sure the friend's PC is on the owner's tailnet:
// download + verify + silently install Tailscale when missing, then join with
// the one-time auth key.
func ensureTailscale(ctx context.Context, d deps, authKey string) error {
	if _, err := tailscaleBackendState(ctx, d); err == nil {
		return tailscaleUp(ctx, d, authKey)
	}
	msiPath, err := downloadTailscaleMSI(ctx, d)
	if err != nil {
		return err
	}
	code, err := installTailscaleElevated(ctx, d, msiPath)
	if err != nil {
		return err
	}
	// REVIEW-BACK 1: 0 or 3010 (reboot required) = ok; 1223 = the user clicked No
	// on the UAC prompt.
	switch code {
	case 0, 3010:
	case 1223:
		return fmt.Errorf("Tailscale needs admin rights — click Yes in the Windows permission prompt, then run VL-Setup again")
	default:
		return fmt.Errorf("Tailscale install failed (exit %d)", code)
	}
	if err := waitTailscaleReady(ctx, d); err != nil {
		return err
	}
	return tailscaleUp(ctx, d, authKey)
}

// installTailscaleElevated starts msiexec ELEVATED so Windows shows the UAC
// Yes/No prompt (a per-machine /quiet MSI from a non-admin process fails with
// no prompt — 1603/1625). It returns the msiexec exit code.
func installTailscaleElevated(ctx context.Context, d deps, msiPath string) (int, error) {
	fmt.Println("Windows will ask for permission to install Tailscale — click Yes")
	escaped := strings.ReplaceAll(msiPath, "'", "''")
	ps := "$p = Start-Process msiexec -Verb RunAs -Wait -PassThru -ArgumentList '/i','" + escaped + "','/quiet','/norestart'; $p.ExitCode"
	out, err := d.exec(ctx, "powershell.exe", "-NoProfile", "-Command", ps)
	if err != nil {
		// UAC declined makes Start-Process throw "canceled by the user" (1223).
		msg := strings.ToLower(string(out) + " " + err.Error())
		if strings.Contains(msg, "1223") || strings.Contains(msg, "canceled by the user") || strings.Contains(msg, "cancelled by the user") {
			return 1223, nil
		}
		return 0, fmt.Errorf("tailscale install: %w", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("tailscale install: unreadable exit code %q", strings.TrimSpace(string(out)))
	}
	return code, nil
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
