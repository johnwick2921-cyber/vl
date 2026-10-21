package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
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
// GET /api/friend/flat → {"flat":true|false,"traders":<n>,"at":<unix ms>}.
type flatProbeResult struct {
	Flat    bool `json:"flat"`
	Traders *int `json:"traders"`
}

// probeFlat asks the friend bot's /api/friend/flat endpoint and maps it to the
// positionsProvider contract: 0 = flat/safe, non-zero = open. Any probe error
// is an error (fail closed): the caller's waitUntilFlat treats it as "not flat"
// and defers, never swapping mid-trade.
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
	// F5/R11: zero traders = nothing to manage → safe. A brand-new friend bot
	// answers flat=false forever (it has no traders), which would otherwise hang
	// the update. Missing "traders" field → today's rule (not flat).
	if r.Traders != nil && *r.Traders == 0 {
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
// (c) download + verify + place the add-on, (d) the desktop shortcut. It prints
// the plain-English step screens [2/6]…[6/6] (the non-technical-owner order);
// [1/6] and the final "Press Enter to close" live in runFirstRun.
func installFirstRun(ctx context.Context, d deps, code string) error {
	ic, err := ParseInstallCode(code)
	if err != nil {
		return err
	}
	docs, err := d.docsDir()
	if err != nil {
		return err
	}
	base := baseURLFromInstallCode(ic)

	// F2 exactness (owner 00:05): prove NT8 has created its folders BEFORE any
	// write — <MyDocuments>\NinjaTrader 8\bin\Custom must exist. If not, write
	// NOTHING.
	ntBinCustom := filepath.Join(docs, "NinjaTrader 8", "bin", "Custom")
	if !d.dirExists(ntBinCustom) {
		return fmt.Errorf("Open NinjaTrader 8 once (so it creates its folders), then run VL-Setup again")
	}

	// [2/6] Tailscale — install (UAC) or note it is already present.
	d.print("[2/6] Installing Tailscale (the private connection). Windows will ask for permission — click YES.")
	already, err := ensureTailscaleInstalled(ctx, d)
	if err != nil {
		return err
	}
	if already {
		d.print("Tailscale is already installed ✓")
	}

	// [3/6] Join the tailnet and wait for the owner's approval (if enabled).
	d.print("[3/6] Connecting this PC… Waiting for the owner to approve it. Tell the owner: 'my PC is waiting for approval'. This window waits up to 15 minutes.")
	if err := tailscaleConnect(ctx, d, ic.TSAuthKey, base); err != nil {
		return err
	}
	d.print("Approved ✓")

	if err := writeConnectJSON(d, docs, ic); err != nil {
		return err
	}
	// P0: every installed run reads vl-app.json — write it now from the install
	// code (name + web_port) so it exists on a real PC, never only in tests.
	if err := writeAppState(d, docs, appState{Name: ic.Name, WebPort: ic.WebPort}); err != nil {
		return err
	}

	// [4/6] Download the add-on and verify the signed manifest + SHA-256 BEFORE
	// anything is written.
	d.print("[4/6] Downloading the VL add-on and checking its signature…")
	m, err := fetchManifest(ctx, d, base)
	if err != nil {
		return err
	}
	addon, err := fetchVerifiedAddon(ctx, d, base, m)
	if err != nil {
		return err
	}
	d.print("Add-on verified ✓")

	// [5/6] Place the verified add-on into NinjaTrader (F2: print the resolved
	// folder so the user can SEE where it is going).
	d.print("[5/6] Installing the add-on into: " + ntBinCustom)
	// S16: if an add-on DLL is already there, use the swap path — never truncate
	// a loaded DLL. Only ask to close NinjaTrader when one already exists.
	if _, statErr := d.readFile(addonDstPath(docs)); statErr == nil {
		deferred, err := waitNinjaTraderClosed(ctx, d)
		if err != nil {
			return err
		}
		if deferred {
			return fmt.Errorf("install deferred — close NinjaTrader and run VL-Setup again")
		}
	}
	if err := placeAddon(d, docs, addon); err != nil {
		return err
	}
	if err := writeInstalledState(d, docs, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
		return err
	}
	d.print("Done ✓. If NinjaTrader is open, close it and open it again.")

	// [6/6] Desktop + Start Menu shortcuts + README, then the final screen, then
	// OPEN the dashboard so the non-technical owner lands on the login page.
	// The icon launches vl-friend.exe --open, so copy ourselves to LOCALAPPDATA
	// first. B1: shortcut creation is NON-fatal — warn and continue.
	if err := ensureLocalExe(d); err != nil {
		return err
	}
	if err := createDesktopShortcut(ctx, d, base); err != nil {
		d.print("warning: could not create the VL icon — " + err.Error())
	}
	if err := writeDashboardREADME(d, docs, base); err != nil {
		return err
	}
	d.print("[6/6] All done ✓. A VL icon is on your desktop. Double-click it, create your login, then add your AI key, your strategy and a trader on your SIM account.")
	d.print("Your VL dashboard:  " + base)
	d.print("A VL icon is on your desktop and in the Start Menu.")
	if err := d.openURL(ctx, base); err != nil {
		return fmt.Errorf("open dashboard: %w", err)
	}
	return nil
}

// runFirstRun is the whole first-run console flow: the [1/6] install-code
// prompt, installFirstRun ([2/6]…[6/6]), then "Press Enter to close". It lives
// here (not run_windows.go) so a Linux test can assert the step order.
func runFirstRun(ctx context.Context, d deps) error {
	d.print("[1/6] Paste the install code you were sent, then press Enter.")
	code, err := d.readLine("")
	if err != nil {
		return err
	}
	ic, err := ParseInstallCode(code)
	if err != nil {
		// Never echo the code (it holds ts_authkey + connect_key).
		return fmt.Errorf("invalid install code — check the copy you were sent")
	}
	secrets := []string{ic.TSAuthKey, ic.ConnectKey}
	if err := installFirstRun(ctx, d, code); err != nil {
		return fmt.Errorf("install failed: %s", redactSecrets(err.Error(), secrets...))
	}
	d.print("Press Enter to close")
	_, _ = d.readLine("")
	return nil
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
	tailscaleInstallWait   = 5 * time.Minute
	tailscaleApprovalWait  = 15 * time.Minute
	tailscaleReadyPoll     = 2 * time.Second
	tailscaleApprovalPoll  = 5 * time.Second
	tailscaleUpTimeout     = 30 * time.Second
	tailscaleApprovalPrint = time.Minute
)

// tailscalePeer is one node (Self or a Peer) in `tailscale status --json`.
type tailscalePeer struct {
	TailscaleIPs []string `json:"TailscaleIPs"`
	DNSName      string   `json:"DNSName"`
	HostName     string   `json:"HostName"`
}

// tailscaleStatusJSON is the subset of `tailscale status --json` this flow reads.
type tailscaleStatusJSON struct {
	BackendState string                   `json:"BackendState"`
	AuthURL      string                   `json:"AuthURL"`
	Self         tailscalePeer            `json:"Self"`
	Peer         map[string]tailscalePeer `json:"Peer"`
}

// tailscaleExec runs a tailscale subcommand. S10: the ABSOLUTE install path is
// tried FIRST (a bare "tailscale" on PATH could be a different binary), and the
// bare name is used only when the known path does not EXIST — never on any
// other failure (a daemon error is not fixed by retrying another name).
func tailscaleExec(ctx context.Context, d deps, args ...string) ([]byte, error) {
	out, err := d.exec(ctx, tailscaleExeFallback, args...)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return d.exec(ctx, "tailscale", args...)
}

// tailscaleStatus returns the parsed `tailscale status --json`.
func tailscaleStatus(ctx context.Context, d deps) (tailscaleStatusJSON, error) {
	out, err := tailscaleExec(ctx, d, "status", "--json")
	if err != nil {
		return tailscaleStatusJSON{}, err
	}
	var st tailscaleStatusJSON
	if err := json.Unmarshal(out, &st); err != nil {
		return tailscaleStatusJSON{}, fmt.Errorf("tailscale status: %w", err)
	}
	return st, nil
}

// tailscaleBackendState returns Tailscale's BackendState ("NeedsLogin",
// "NeedsMachineAuth", "Running", …) from `tailscale status --json`.
func tailscaleBackendState(ctx context.Context, d deps) (string, error) {
	st, err := tailscaleStatus(ctx, d)
	if err != nil {
		return "", err
	}
	return st.BackendState, nil
}

// tailscaleTailnet returns the set of Tailscale IPs and DNS/host names present
// on the LOCAL tailnet (Self + every Peer). S5: this is the identity proof that
// a Running device is on OUR tailnet — a local CLI call, never an HTTP fetch
// that a transient bot/network blip could fail and misread as "wrong network".
func tailscaleTailnet(ctx context.Context, d deps) (ips, names map[string]bool, err error) {
	st, err := tailscaleStatus(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	ips = map[string]bool{}
	names = map[string]bool{}
	add := func(n tailscalePeer) {
		for _, ip := range n.TailscaleIPs {
			ips[ip] = true
		}
		if dns := strings.TrimSuffix(n.DNSName, "."); dns != "" {
			names[strings.ToLower(dns)] = true
		}
		if hn := n.HostName; hn != "" {
			names[strings.ToLower(hn)] = true
		}
	}
	add(st.Self)
	for _, p := range st.Peer {
		add(p)
	}
	return ips, names, nil
}

// hostOnTailnet reports whether the bot host from the install code appears on
// the local tailnet. A literal IP must be one of a node's TailscaleIPs; a DNS
// name must match a node's DNSName (MagicDNS) or HostName.
func hostOnTailnet(host string, ips, names map[string]bool) bool {
	host = strings.TrimSuffix(host, ".")
	if net.ParseIP(host) != nil {
		return ips[host]
	}
	return names[strings.ToLower(host)]
}

// tskeyRe matches a Tailscale auth key (tskey-auth-XXXX…). S10: even when a
// caller forgets to pass the key explicitly, the regex redacts it.
var tskeyRe = regexp.MustCompile(`tskey-[A-Za-z0-9_-]+`)

// redactTSKeys replaces every Tailscale auth key with "***" so it can never
// reach the console or the setup log.
func redactTSKeys(s string) string {
	return tskeyRe.ReplaceAllString(s, "***")
}

// redactSecrets replaces each secret with "***" so it never reaches stdout,
// stderr or the setup log. Never print the auth key or the connect key (P0 d).
// S10: it also applies the tskey regex as a backstop.
func redactSecrets(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return redactTSKeys(s)
}

// pauseOnError prints the (already-redacted) error screen and waits for Enter
// so a double-clicked console window does not close before the user reads it
// (P0 b). The text tells a non-technical user exactly what to do: nothing was
// changed on NinjaTrader, and to send a photo to the owner. Testable on Linux
// through the print + readLine seams.
func pauseOnError(d deps, msg string) {
	d.print(redactTSKeys("Something went wrong: " + msg + ". Nothing was changed on NinjaTrader. Send a photo of this window to the owner."))
	d.print("Press Enter to close")
	_, _ = d.readLine("")
}

// setupLogPath returns %LOCALAPPDATA%\VL\setup.log (F7: LOCALAPPDATA, not
// <home>\AppData\Local).
func setupLogPath(d deps) string {
	return filepath.Join(d.localAppDataDir(), "VL", "setup.log")
}

// logSetupError appends the (already-redacted) error to setup.log; best effort.
// F7: APPEND, never overwrite — the full error history survives. S10: the tskey
// regex is applied here centrally, so a key can never land in the log.
func logSetupError(d deps, msg string) {
	dir := filepath.Join(d.localAppDataDir(), "VL")
	if d.mkdirAll(dir, 0o755) != nil {
		return
	}
	prev, _ := d.readFile(setupLogPath(d))
	_ = d.writeFile(setupLogPath(d), append(prev, []byte(time.Now().Format(time.RFC3339)+" "+redactTSKeys(msg)+"\n")...), 0o600)
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
	// HashMismatch), so the STATUS is the tamper gate — and S9: the ORG is parsed
	// from the DN and compared EXACTLY (O == "Tailscale Inc."), never a
	// substring match.
	if sig.Status != "Valid" {
		return "", fmt.Errorf("refusing Tailscale installer: Authenticode status %q (not Valid)", sig.Status)
	}
	if signerOrg(sig.Subject) != "Tailscale Inc." {
		return "", fmt.Errorf("refusing Tailscale installer: unexpected signer %q (want O=Tailscale Inc.)", sig.Subject)
	}
	return path, nil
}

// signerOrg parses an X.509 subject DN ("CN=…, O=…, …") and returns the O= value
// (EXACT, S9). A cert from another org whose CN merely contains "tailscale"
// refuses.
func signerOrg(subject string) string {
	for _, part := range strings.Split(subject, ",") {
		kv := strings.TrimSpace(part)
		if strings.HasPrefix(kv, "O=") {
			return strings.TrimSpace(strings.TrimPrefix(kv, "O="))
		}
	}
	return ""
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

// tailscaleConnect joins the tailnet with the one-time auth key. F3: after a
// partial failure the PC may already be Running on OUR tailnet with the auth key
// spent — so Running is checked by IDENTITY (the bot host among the status
// peers, S5) and NeedsMachineAuth just waits for approval; NoState/Starting/
// Stopped poll until the state settles instead of a false "another network"
// refusal.
func tailscaleConnect(ctx context.Context, d deps, authKey, base string) error {
	state, err := tailscaleSettledState(ctx, d)
	if err != nil {
		state = "NeedsLogin" // unsettled: let `up` fail loudly if that is wrong
	}
	switch state {
	case "NeedsLogin":
		// S11: an empty auth key cannot join a fresh device — fail early rather
		// than running `up --authkey=` and failing with a spent/blank key.
		if authKey == "" {
			return fmt.Errorf("This install code has no Tailscale key — ask the owner for a new code")
		}
		// W5: `tailscale up` can block indefinitely while the daemon applies the
		// key. Give it its own short context; a timeout is NOT a failure — it
		// means "still connecting", so waitTailscaleApproved then polls to
		// Running.
		upCtx, cancel := context.WithTimeout(ctx, tailscaleUpTimeout)
		out, err := tailscaleExec(upCtx, d, "up", "--authkey="+authKey, "--unattended")
		timedOut := upCtx.Err() == context.DeadlineExceeded
		cancel()
		if err != nil {
			if timedOut {
				return waitTailscaleApproved(ctx, d)
			}
			// W5: the first-ever connection needs admin rights; say that exactly
			// instead of a raw `up` failure.
			if lo := strings.ToLower(err.Error() + " " + string(out)); strings.Contains(lo, "access denied") || strings.Contains(lo, "prefs write access denied") {
				return fmt.Errorf("Tailscale needs admin rights for the first connection — run VL-Setup as administrator (right-click → Run as administrator)")
			}
			// Redact the auth key from BOTH the error and the command output —
			// exec errors can echo the full command line (P0 d: never print keys).
			return fmt.Errorf("tailscale up: %s", redactSecrets(err.Error()+" "+string(out), authKey))
		}
		return waitTailscaleApproved(ctx, d)
	case "NeedsMachineAuth":
		// S5: mid-approval — the device is ALREADY on our tailnet and just waiting
		// for the owner to approve it. Never refuse this as "another network".
		return waitTailscaleApproved(ctx, d)
	case "Running":
		// S5: prove OUR tailnet by IDENTITY — the bot host must appear among
		// `tailscale status --json` peers — never by an HTTP fetch. The auth key
		// is spent, so a re-run must skip `up` and continue when it is ours.
		u, err := url.Parse(base)
		if err != nil {
			return fmt.Errorf("install code base URL: %w", err)
		}
		ips, names, err := tailscaleTailnet(ctx, d)
		if err != nil {
			// The CLI hiccupped (daemon restart, first status after install).
			// Never misread that as a wrong network: the [4/6] manifest fetch is
			// the real proof downstream.
			return waitTailscaleApproved(ctx, d)
		}
		if hostOnTailnet(u.Hostname(), ips, names) {
			return waitTailscaleApproved(ctx, d)
		}
		return fmt.Errorf("this PC is already logged in to another Tailscale network (state %q). Log out of Tailscale first, then run VL-Setup again", state)
	default:
		return fmt.Errorf("this PC is already logged in to another Tailscale network (state %q). Log out of Tailscale first, then run VL-Setup again", state)
	}
}

// tailscaleSettledState polls BackendState until it leaves the transient states
// (NoState/Starting/Stopped right after a fresh MSI install), bounded. It
// returns the settled state, or an error if it never settles.
func tailscaleSettledState(ctx context.Context, d deps) (string, error) {
	t := time.NewTicker(tailscaleReadyPoll)
	defer t.Stop()
	deadline := time.Now().Add(tailscaleInstallWait)
	for {
		state, err := tailscaleBackendState(ctx, d)
		if err == nil {
			switch state {
			case "NoState", "Starting", "Stopped":
				// keep polling until it settles
			default:
				return state, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("Tailscale state did not settle within %s", tailscaleInstallWait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-t.C:
		}
	}
}

// waitTailscaleApproved polls BackendState until Running, up to a bounded
// deadline — never silently fails. W5: while waiting it prints the owner's
// approval URL (from the status AuthURL, never the key) and a "still waiting"
// line every tailscaleApprovalPrint.
func waitTailscaleApproved(ctx context.Context, d deps) error {
	t := time.NewTicker(tailscaleApprovalPoll)
	defer t.Stop()
	printT := time.NewTicker(tailscaleApprovalPrint)
	defer printT.Stop()
	deadline := time.Now().Add(tailscaleApprovalWait)
	printedURL := false
	for {
		st, err := tailscaleStatus(ctx, d)
		if err == nil && st.BackendState == "Running" {
			return nil
		}
		if err == nil && !printedURL && st.AuthURL != "" {
			d.print("Owner approval link: " + st.AuthURL)
			printedURL = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Tailscale device was not approved within %s", tailscaleApprovalWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-printT.C:
			d.print("Still waiting for the owner to approve…")
		case <-t.C:
		}
	}
}

// ensureTailscaleInstalled downloads + verifies + silently installs Tailscale
// when it is not already present, then waits for it to become ready. It reports
// whether Tailscale was already installed so the caller can print the skip line
// ([2/6]). It does NOT join the tailnet.
func ensureTailscaleInstalled(ctx context.Context, d deps) (bool, error) {
	if _, err := tailscaleBackendState(ctx, d); err == nil {
		return true, nil
	}
	msiPath, err := downloadTailscaleMSI(ctx, d)
	if err != nil {
		return false, err
	}
	code, err := installTailscaleElevated(ctx, d, msiPath)
	if err != nil {
		return false, err
	}
	// REVIEW-BACK 1: 0 or 3010 (reboot required) = ok; 1223 = the user clicked No
	// on the UAC prompt.
	switch code {
	case 0, 3010:
	case 1223:
		return false, fmt.Errorf("Tailscale needs admin rights — click Yes in the Windows permission prompt, then run VL-Setup again")
	default:
		return false, fmt.Errorf("Tailscale install failed (exit %d)", code)
	}
	if err := waitTailscaleReady(ctx, d); err != nil {
		return false, err
	}
	return false, nil
}

// ensureTailscale keeps the combined install + join shape the tests exercise:
// install when missing, then join with the one-time auth key.
func ensureTailscale(ctx context.Context, d deps, authKey, base string) error {
	if _, err := ensureTailscaleInstalled(ctx, d); err != nil {
		return err
	}
	return tailscaleConnect(ctx, d, authKey, base)
}

// elevatedInstallScript is the PowerShell that runs ELEVATED. It re-verifies the
// Authenticode signature IMMEDIATELY before msiexec (S8 TOCTOU guard), quotes the
// MSI path (S23a: "/i `"<path>`""), and reports UAC cancellation by
// NativeErrorCode 1223 (W10) — never by matching English text. The MSI path
// arrives as $args[0] (S18b: no script-text splicing).
const elevatedInstallScript = `$msi = $args[0]
[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
$sig = Get-AuthenticodeSignature -FilePath $msi
if ($sig.Status -ne 'Valid') { Write-Output 'VL_MSI_NOT_VALID'; exit 2 }
if (-not ($sig.SignerCertificate.Subject -match 'O=Tailscale Inc\.')) { Write-Output 'VL_MSI_BAD_SIGNER'; exit 3 }
$arg = '/i "' + $msi + '" /quiet /norestart'
try {
  $p = Start-Process msiexec -Verb RunAs -Wait -PassThru -ArgumentList $arg
  $p.ExitCode
} catch {
  if ($_.Exception.InnerException.NativeErrorCode -eq 1223) { Write-Output 'VL_UAC_CANCELLED'; exit 0 }
  throw
}`

// installTailscaleElevated starts msiexec ELEVATED so Windows shows the UAC
// Yes/No prompt (a per-machine /quiet MSI from a non-admin process fails with
// no prompt — 1603/1625). It returns the msiexec exit code. S8/S23a/W10/S18b:
// the command is sent via -EncodedCommand with the MSI path as $args[0], the
// script re-verifies the signature right before msiexec, and a UAC cancel is
// detected by NativeErrorCode 1223 (never English text).
func installTailscaleElevated(ctx context.Context, d deps, msiPath string) (int, error) {
	out, err := d.exec(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(elevatedInstallScript), msiPath)
	if err != nil {
		return 0, fmt.Errorf("tailscale install: %w: %s", err, strings.TrimSpace(string(out)))
	}
	switch strings.TrimSpace(string(out)) {
	case "VL_UAC_CANCELLED":
		return 1223, nil
	case "VL_MSI_NOT_VALID", "VL_MSI_BAD_SIGNER":
		return 0, fmt.Errorf("tailscale install: elevated signature re-verify refused (%s)", strings.TrimSpace(string(out)))
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("tailscale install: unreadable exit code %q", strings.TrimSpace(string(out)))
	}
	return code, nil
}

// shortcutScript builds the PowerShell that creates the VL shortcuts. It is a
// separate function so tests can pin the argv: the REAL Desktop + Start Menu
// (via [Environment]::GetFolderPath — OneDrive Known-Folder safe, NOT
// $env:USERPROFILE\Desktop). The icon launches the LOCAL copy of vl-friend.exe
// with --open (quick update check + open dashboard); the .url Internet Shortcut
// fallback opens the dashboard in the default browser when the exe copy is
// missing. B1: the if/else is ONE statement — PowerShell rejects "};else{".
// m10: each .lnk/.url is written only when missing (Test-Path), so a daily click
// is a no-op, not a rewrite.
func shortcutScript(appURL string) string {
	return strings.Join([]string{
		"$d=[Environment]::GetFolderPath('Desktop')",
		"$s=[Environment]::GetFolderPath('Programs')",
		"$exe=\"$env:LOCALAPPDATA\\VL\\vl-friend.exe\"",
		"if(Test-Path $exe){$w=New-Object -ComObject WScript.Shell;foreach($dir in @($d,$s)){$p=\"$dir\\VL.lnk\";if(-not (Test-Path $p)){$l=$w.CreateShortcut($p);$l.TargetPath=$exe;$l.Arguments='--open';$l.Save()}}}else{$u=\"[InternetShortcut]`r`nURL=" + appURL + "`r`n\";foreach($dir in @($d,$s)){$p=\"$dir\\VL.url\";if(-not (Test-Path $p)){[System.IO.File]::WriteAllText($p,$u)}}}",
	}, ";")
}

// createDesktopShortcut creates the VL shortcuts (Desktop + Start Menu) opening
// the friend's dashboard in its own app window (msedge --app), or a .url
// shortcut to the default browser when Edge is absent. The PowerShell is invoked
// through d.exec so tests assert the effect without Windows.
func createDesktopShortcut(ctx context.Context, d deps, appURL string) error {
	if _, err := d.exec(ctx, "powershell", "-NoProfile", "-Command", shortcutScript(appURL)); err != nil {
		return fmt.Errorf("desktop shortcut: %w", err)
	}
	return nil
}

// localExePath returns %LOCALAPPDATA%\VL\vl-friend.exe — the copy the desktop
// icon launches with --open (so the original installer can be deleted).
func localExePath(d deps) string {
	return filepath.Join(d.localAppDataDir(), "VL", "vl-friend.exe")
}

// ensureLocalExe copies the running executable to %LOCALAPPDATA%\VL\vl-friend.exe
// so the desktop icon can launch --open. When the copy already exists and
// differs (a newer installer ran), the old copy is MOVED to VL\old\ (never
// deleted) before the new one is written.
func ensureLocalExe(d deps) error {
	src, err := d.selfExe()
	if err != nil {
		return err
	}
	dst := localExePath(d)
	srcBytes, err := d.readFile(src)
	if err != nil {
		return err
	}
	if dstBytes, err := d.readFile(dst); err == nil && bytes.Equal(srcBytes, dstBytes) {
		return nil // already current
	}
	// Replace: MOVE the old copy aside (never delete), then write the new. The
	// old file is timestamped so a re-run never overwrites a previous backup.
	if _, err := d.readFile(dst); err == nil {
		old := filepath.Join(d.localAppDataDir(), "VL", "old", "vl-friend-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".exe")
		if err := d.mkdirAll(filepath.Dir(old), 0o755); err != nil {
			return err
		}
		if err := d.rename(dst, old); err != nil {
			return err
		}
	}
	if err := d.mkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return d.writeFile(dst, srcBytes, 0o755)
}

// detectUpdate fetches the signed manifest and compares it with what is
// installed — WITHOUT prompting or applying. runOpen uses it for the quick
// silent check (≤5s).
func detectUpdate(ctx context.Context, d deps, base string) (UpdateDecision, error) {
	var dec UpdateDecision
	m, err := fetchManifest(ctx, d, base)
	if err != nil {
		return dec, err
	}
	dec.Manifest = m
	docs, err := d.docsDir()
	if err != nil {
		return dec, err
	}
	cur, err := readInstalledState(d, docs)
	if err != nil {
		return dec, err
	}
	dec.CurrentBuild = cur.AddonBuild
	dec.AddonUpdateAvailable = addonBuildNewer(m.AddonBuild, cur.AddonBuild)
	dec.AppUpdateAvailable = appVersionNewer(m.AppVersion, appVersion)
	return dec, nil
}

// resolveDashboardBase resolves the friend dashboard URL from the installed
// config (repairing vl-app.json if needed).
func resolveDashboardBase(ctx context.Context, d deps) (string, error) {
	docs, err := d.docsDir()
	if err != nil {
		return "", err
	}
	cfg, err := readConnectConfig(d, docs)
	if err != nil {
		return "", err
	}
	st, err := loadAppState(ctx, d, docs, cfg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s:%d", cfg.Host, st.WebPort), nil
}

// runOpen is the desktop-icon path (vl-friend.exe --open): open the dashboard
// FIRST, then a quick silent update check (≤5s); only an ADD-ON update shows a
// Windows dialog (M5). Update now = the verified flat-gated swap with the
// NT8-running prompt, bounded to its own ctx so it can never hang the icon
// launch (M3). On any error the mentor sees a dialog "VL could not start…" (I3).
func runOpen(ctx context.Context, d deps) error {
	// W13b: migrate a legacy <USERPROFILE>\Documents layout into MyDocuments
	// (non-fatal).
	if home, herr := d.homeDir(); herr == nil {
		if docs, derr := d.docsDir(); derr == nil {
			_ = migrateDocsIfNeeded(d, home, docs)
		}
	}

	base, err := resolveDashboardBase(ctx, d)
	if err != nil {
		d.messageBox("VL could not start: " + err.Error() + ". Send a photo of this to the owner.")
		return err
	}

	// B2: wire the production flat probe — runOpen never set d.positions, so an
	// icon-launched "Update now" used the stub ("flat probe not wired") and hung
	// until the ctx expired.
	d.positions = func(c context.Context) (float64, error) {
		return probeFlat(c, d, base)
	}

	// M3/F8: open the dashboard FIRST, then offer the update. Shortcut
	// re-creation is non-fatal and (m10) only writes when missing.
	if err := createDesktopShortcut(ctx, d, base); err != nil {
		d.print("warning: could not create the VL icon — " + err.Error())
	}
	if oerr := d.openURL(ctx, base); oerr != nil {
		d.messageBox("VL could not start: " + oerr.Error() + ". Send a photo of this to the owner.")
		return oerr
	}

	// Quick bounded update check; a probe error is shown (m12/W15), never silent.
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	dec, derr := detectUpdate(cctx, d, base)
	cancel()
	if derr != nil {
		// W15: the 5-second manifest probe failed — the dashboard is already open,
		// so just tell the mentor the bot is unreachable.
		d.messageBox("VL cannot reach your bot — open Tailscale and check it says Connected.")
		return nil
	}
	if !dec.AddonUpdateAvailable {
		if dec.AppUpdateAvailable {
			// M5: an app-only update is a one-time info line, not a dialog on
			// every click.
			d.print("A new VL-Setup is available — ask the owner for it.")
		}
		return nil
	}

	// M4: the dialog names the build it would move between.
	msg := fmt.Sprintf("VL update available (add-on %s → %s). Yes = update now, No = later.", dec.CurrentBuild, dec.Manifest.AddonBuild)
	if !d.messageBox(msg) {
		return nil
	}

	// Update now: the flat wait is bounded to its own ctx (M3, ≤5 min) so the
	// icon launch can never hang; on a flat timeout it DEFERS (no error) and the
	// dashboard is already open.
	uctx, ucancel := context.WithTimeout(ctx, 5*time.Minute)
	defer ucancel()
	if _, uerr := RunUpdateCheck(uctx, d, base, true); uerr != nil {
		if errors.Is(uerr, errNotFlat) {
			return nil // deferred — dashboard is already open
		}
		d.messageBox("VL could not start: " + uerr.Error() + ". Send a photo of this to the owner.")
		return uerr
	}
	// W8b: tell the mentor the add-on updated and NinjaTrader must be restarted.
	d.messageBox("Updated ✓ — restart NinjaTrader to load it.")
	return nil
}

// repairIcon re-creates the VL shortcuts on an ALREADY-INSTALLED PC
// (VL-Setup.exe --repair-icon). The normal start (runInner) also re-creates a
// missing icon, so either path fixes a vanished / OneDrive-moved icon.
func repairIcon(ctx context.Context, d deps) error {
	// W13b: migrate a legacy <USERPROFILE>\Documents layout into MyDocuments
	// (non-fatal).
	if home, herr := d.homeDir(); herr == nil {
		if docs, derr := d.docsDir(); derr == nil {
			_ = migrateDocsIfNeeded(d, home, docs)
		}
	}
	docs, err := d.docsDir()
	if err != nil {
		return err
	}
	cfg, err := readConnectConfig(d, docs)
	if err != nil {
		return fmt.Errorf("repair icon: not installed yet — run VL-Setup with your install code first")
	}
	st, err := loadAppState(ctx, d, docs, cfg)
	if err != nil {
		return err
	}
	d.print("Re-creating the VL icon…")
	if err := ensureLocalExe(d); err != nil {
		return err
	}
	if err := createDesktopShortcut(ctx, d, fmt.Sprintf("http://%s:%d", cfg.Host, st.WebPort)); err != nil {
		return err
	}
	d.print("Done ✓ — the VL icon is back on your desktop and Start Menu.")
	d.print("Press Enter to close")
	_, _ = d.readLine("")
	return nil
}

// writeDashboardREADME writes the dashboard URL into
// <MyDocuments>\NinjaTrader 8\VL\README.txt so a new user can find VL later.
func writeDashboardREADME(d deps, docs, appURL string) error {
	if err := d.mkdirAll(ntVLdir(docs), 0o755); err != nil {
		return err
	}
	return d.writeFile(filepath.Join(ntVLdir(docs), "README.txt"),
		[]byte("Your VL dashboard: "+appURL+"\n"), 0o644)
}

// openScript builds the PowerShell that opens the dashboard: msedge --app when
// Edge is present (resolved to a full path), else the default browser. S18b: the
// URL arrives as $args[0] (the caller sends it via -EncodedCommand) — never
// spliced into the script text.
func openScript() string {
	return strings.Join([]string{
		"$edge=$null",
		"foreach($r in @('HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\App Paths\\msedge.exe','HKCU:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\App Paths\\msedge.exe')){try{$v=(Get-ItemProperty -Path $r -ErrorAction Stop).'(default)';if($v -and (Test-Path $v)){$edge=$v;break}}catch{}}",
		"if(-not $edge){foreach($p in @(\"${env:ProgramFiles(x86)}\\Microsoft\\Edge\\Application\\msedge.exe\",\"$env:ProgramFiles\\Microsoft\\Edge\\Application\\msedge.exe\")){if(Test-Path $p){$edge=$p;break}}}",
		"if($edge){Start-Process $edge -ArgumentList ('--app=' + $args[0])}else{Start-Process $args[0]}",
	}, ";")
}

// repairAppState re-derives the dashboard web port when vl-app.json is missing
// (P0: every 1.1.x install never wrote it, so an installed run must repair, not
// fail). Candidate = connect.json nt_port − 18900 (B2: web 18080+N, nt 36980+N).
// The candidate is VERIFIED before it is trusted: GET /api/friend/manifest must
// answer with a signature-valid manifest (proving it is this friend's bot), then
// vl-app.json is written. A failed verification is a clear error, never a silent
// guess.
func repairAppState(ctx context.Context, d deps, docs string, cfg connectConfig) (appState, error) {
	port := cfg.Port - 18900
	if port < 1 || port > 65535 {
		return appState{}, fmt.Errorf("cannot find your VL dashboard port — send this to the owner")
	}
	base := fmt.Sprintf("http://%s:%d", cfg.Host, port)
	if _, err := fetchManifest(ctx, d, base); err != nil {
		return appState{}, fmt.Errorf("cannot find your VL dashboard port — send this to the owner")
	}
	st := appState{WebPort: port}
	if err := writeAppState(d, docs, st); err != nil {
		return appState{}, err
	}
	return st, nil
}

// loadAppState reads vl-app.json, repairing it (derive + verify) when missing.
func loadAppState(ctx context.Context, d deps, docs string, cfg connectConfig) (appState, error) {
	st, err := readAppState(d, docs)
	if err == nil {
		return st, nil
	}
	return repairAppState(ctx, d, docs, cfg)
}

// migrateDocsIfNeeded moves the app's files from <USERPROFILE>\Documents into
// MyDocuments when Known-Folder redirection differs (F2). Move-only, never
// delete, and only when the old file exists and the new one does not.
func migrateDocsIfNeeded(d deps, home, docs string) error {
	oldDocs := filepath.Join(home, "Documents")
	if strings.EqualFold(filepath.Clean(oldDocs), filepath.Clean(docs)) {
		return nil
	}
	newVL := ntVLdir(docs)
	ntBinCustom := filepath.Join(docs, "NinjaTrader 8", "bin", "Custom")
	type mv struct{ from, to string }
	moves := []mv{
		{filepath.Join(oldDocs, "NinjaTrader 8", "VL", "connect.json"), filepath.Join(newVL, "connect.json")},
		{filepath.Join(oldDocs, "NinjaTrader 8", "VL", "installed.json"), filepath.Join(newVL, "installed.json")},
		{filepath.Join(oldDocs, "NinjaTrader 8", "VL", "vl-app.json"), filepath.Join(newVL, "vl-app.json")},
		{filepath.Join(oldDocs, "NinjaTrader 8", "VL", "README.txt"), filepath.Join(newVL, "README.txt")},
		{filepath.Join(oldDocs, "NinjaTrader 8", "bin", "Custom", "VL.AddOn.dll"), filepath.Join(ntBinCustom, "VL.AddOn.dll")},
		{filepath.Join(oldDocs, "NinjaTrader 8", "VL", "old", "VL.AddOn.dll"), filepath.Join(newVL, "old", "VL.AddOn.dll")},
	}
	for _, m := range moves {
		if _, err := d.readFile(m.from); err != nil {
			continue // nothing to migrate
		}
		if _, err := d.readFile(m.to); err == nil {
			continue // already migrated
		}
		// m9: only move the add-on DLL when the F2 proof holds — NinjaTrader has
		// created bin\Custom. Otherwise skip (and log the skip).
		if strings.Contains(m.from, filepath.Join("bin", "Custom")) && !d.dirExists(ntBinCustom) {
			d.print("skipped moving " + m.from + " — NinjaTrader 8 has not created bin\\Custom yet")
			continue
		}
		if err := d.mkdirAll(filepath.Dir(m.to), 0o755); err != nil {
			return err
		}
		if err := d.rename(m.from, m.to); err != nil {
			return err
		}
		d.print("Moved " + m.from + " → " + m.to)
	}
	return nil
}

// runNewCode is the --new-code path: on an already-installed PC, apply a fresh
// install code (the owner may re-issue one). It joins/verifies Tailscale —
// skipping `up` when already Running on our tailnet (S5) — then swaps the
// connect.json + vl-app.json over to the new bot.
func runNewCode(ctx context.Context, d deps) error {
	docs, err := d.docsDir()
	if err != nil {
		return err
	}
	if _, err := d.readFile(connectJSONPath(docs)); err != nil {
		return fmt.Errorf("VL is not set up on this PC yet — run VL-Setup normally first")
	}
	if err := promptAndApplyNewCode(ctx, d, docs); err != nil {
		return err
	}
	return nil
}

// promptAndApplyNewCode prints the paste prompt and applies the code. It is the
// shared body of --new-code and the installed-screen "Type N" gate.
func promptAndApplyNewCode(ctx context.Context, d deps, docs string) error {
	d.print("Paste the new install code, then press Enter.")
	code, err := d.readLine("")
	if err != nil {
		return err
	}
	if err := applyNewCode(ctx, d, docs, code); err != nil {
		return err
	}
	d.print("New install code applied ✓")
	return nil
}

// applyNewCode validates the code, joins/verifies the NEW bot's tailnet
// (tailscaleConnect skips `up` when Running on our tailnet, S5), then MOVES the
// old connect.json aside (never overwrite/delete — the ONE RULE) and writes the
// new connect.json + vl-app.json.
func applyNewCode(ctx context.Context, d deps, docs, code string) error {
	ic, err := ParseInstallCode(code)
	if err != nil {
		// Never echo the code (it holds ts_authkey + connect_key).
		return fmt.Errorf("invalid install code — check the copy you were sent")
	}
	secrets := []string{ic.TSAuthKey, ic.ConnectKey}
	if err := tailscaleConnect(ctx, d, ic.TSAuthKey, ic.BaseURL()); err != nil {
		return errors.New(redactSecrets(err.Error(), secrets...))
	}
	oldPath := connectJSONPath(docs)
	backup := filepath.Join(ntVLdir(docs), "old", "connect-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".json")
	if err := d.mkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := d.rename(oldPath, backup); err != nil {
		return err
	}
	if err := writeConnectJSON(d, docs, ic); err != nil {
		_ = d.rename(backup, oldPath) // rollback: never leave a PC without connect.json
		return err
	}
	if err := writeAppState(d, docs, appState{Name: ic.Name, WebPort: ic.WebPort}); err != nil {
		return err
	}
	return nil
}

// runInstalled is the installed-PC path (connect.json exists): a plain
// double-click, NO install-code prompt. It announces, runs the update check,
// ENSUREs the Desktop + Start Menu shortcuts (re-create if missing, never
// duplicate), then prints + opens the dashboard and waits for Enter.
func runInstalled(ctx context.Context, d deps, docs string) error {
	cfg, err := readConnectConfig(d, docs)
	if err != nil {
		return err
	}
	st, err := loadAppState(ctx, d, docs, cfg)
	if err != nil {
		return err
	}
	base := fmt.Sprintf("http://%s:%d", cfg.Host, st.WebPort)

	// S4: a PARTIAL install (connect.json written but the add-on step failed) is
	// resumed from the stored config — no install-code prompt, no Tailscale.
	if _, err := d.readFile(addonDstPath(docs)); err != nil {
		d.print("VL is installed but the add-on is missing — re-installing it.")
		m, merr := fetchManifest(ctx, d, base)
		if merr != nil {
			return merr
		}
		addon, aerr := fetchVerifiedAddon(ctx, d, base, m)
		if aerr != nil {
			return aerr
		}
		if err := placeAddon(d, docs, addon); err != nil {
			return err
		}
		if err := writeInstalledState(d, docs, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
			return err
		}
		d.print("Done ✓. If NinjaTrader is open, close it and open it again.")
	}

	// a) no code prompt — announce instead.
	d.print("VL is already set up on this PC ✓")

	// F8: the dashboard comes FIRST — print the URL, ensure the icons, open it —
	// so a non-technical user lands on the login page before any y/N question.
	d.print("Your VL dashboard: " + base)
	if err := ensureLocalExe(d); err != nil {
		return err
	}
	// B1: shortcut creation is NON-fatal — warn and continue to the dashboard.
	if err := createDesktopShortcut(ctx, d, base); err != nil {
		d.print("warning: could not create the VL icon — " + err.Error())
	}
	if err := d.openURL(ctx, base); err != nil {
		return fmt.Errorf("open dashboard: %w", err)
	}

	// b) the update check as today (add-on/app), flat-gated.
	d.positions = func(ctx context.Context) (float64, error) {
		return probeFlat(ctx, d, base)
	}
	dec, err := RunUpdateCheck(ctx, d, base, false)
	if err != nil {
		if errors.Is(err, errNotFlat) {
			// M3: not flat in time — DEFER, not an error to the user.
			d.print("Update deferred (Later).")
		} else {
			return fmt.Errorf("update check failed: %s", redactSecrets(err.Error(), cfg.ConnectKey))
		}
	}
	if dec.Applied {
		d.print("Updated ✓ — restart NinjaTrader to load the new add-on.")
	}
	if dec.AppUpdateAvailable {
		// F6: there is no self-update — say it honestly.
		d.print("A new VL-Setup is available — ask the owner for it.")
	} else if dec.AddonUpdateAvailable && err == nil {
		d.print("Update deferred (Later).")
	}

	// S6: offer a new install code (the owner may re-issue one). Skipping is
	// the default — this is a gate, never a forced prompt.
	d.print("Have a new install code? Type N then press Enter (or just press Enter).")
	if line, _ := d.readLine(""); strings.EqualFold(strings.TrimSpace(line), "N") {
		if err := promptAndApplyNewCode(ctx, d, docs); err != nil {
			return err
		}
	}

	// e) keep the console readable.
	d.print("Press Enter to close")
	_, _ = d.readLine("")
	return nil
}

// UpdateDecision is what RunUpdateCheck reports.
type UpdateDecision struct {
	Manifest             FriendManifest
	CurrentBuild         string
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
	docs, err := d.docsDir()
	if err != nil {
		return dec, err
	}
	cur, err := readInstalledState(d, docs)
	if err != nil {
		return dec, err
	}
	dec.CurrentBuild = cur.AddonBuild
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
	// F6: only the add-on self-updates. An app-only update is announced honestly
	// by the caller — nothing is downloaded or swapped here.
	if !dec.AddonUpdateAvailable {
		return dec, nil
	}
	// Verify again, then download the new add-on (S9: a bad signature or hash
	// refuses here and nothing below runs).
	addon, err := fetchVerifiedAddon(ctx, d, base, m)
	if err != nil {
		return dec, err
	}
	// S2: only swap when the on-disk DLL differs from the manifest (a
	// missing/torn DLL is repaired, not blindly re-written).
	if !addonNeedsUpdate(d, docs, m.AddonSHA256) {
		return dec, nil // already current
	}
	// Never update during a trade: wait until the friend bot is flat.
	if err := waitFlat(ctx, d); err != nil {
		return dec, err
	}
	// F4: never write a locked DLL — ask the user to close NinjaTrader (or defer).
	deferred, err := waitNinjaTraderClosed(ctx, d)
	if err != nil {
		return dec, err
	}
	if deferred {
		return dec, nil // Later
	}
	// m7: re-probe flat right before the swap — a trade may have opened while the
	// user was closing NinjaTrader.
	if err := waitFlat(ctx, d); err != nil {
		return dec, err
	}
	// S2/S16: atomic swap (write .new → move old aside → rename into place).
	if err := placeAddon(d, docs, addon); err != nil {
		return dec, err
	}
	if err := writeInstalledState(d, docs, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
		return dec, err
	}
	dec.Applied = true
	return dec, nil
}

// waitNinjaTraderClosed prompts the user to close NinjaTrader (F4) while it is
// running, re-checking each time. It returns (true, nil) when the user defers
// ("L"), or (false, nil) once NinjaTrader is closed.
func waitNinjaTraderClosed(ctx context.Context, d deps) (bool, error) {
	for {
		running, err := d.ntRunning(ctx)
		if err != nil {
			return false, err
		}
		if !running {
			return false, nil
		}
		ans, err := d.readLine("Close NinjaTrader, then press Enter to install the add-on update (or type L for later)")
		if err != nil {
			return false, err
		}
		if ans == "L" || ans == "l" {
			return true, nil
		}
	}
}

// addonNeedsUpdate reports whether the on-disk DLL's SHA-256 differs from the
// manifest's (S2): a missing or torn DLL is "needs update" too, so it is
// repaired rather than trusted.
func addonNeedsUpdate(d deps, docs, wantSHA string) bool {
	b, err := d.readFile(addonDstPath(docs))
	if err != nil {
		return true // missing → repair
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) != wantSHA
}

// placeAddon atomically installs a verified add-on (S2/S16). It writes
// VL.AddOn.dll.new in the SAME folder, moves the existing DLL aside (timestamped,
// never deleted), then renames .new into place; on a rename failure the old DLL
// is moved back. Never truncates a loaded DLL.
func placeAddon(d deps, docs string, addon []byte) error {
	dst := addonDstPath(docs)
	tmp := dst + ".new"
	if err := d.mkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return err
	}
	if err := d.writeFile(tmp, addon, 0o644); err != nil {
		return err
	}
	old := ""
	if _, err := d.readFile(dst); err == nil {
		old = oldAddonTimestampedPath(docs)
		if err := d.mkdirAll(filepath.Dir(old), 0o755); err != nil {
			return err
		}
		if err := d.rename(dst, old); err != nil {
			return err
		}
	}
	if err := d.rename(tmp, dst); err != nil {
		// S2: roll the old DLL back so the PC is never left without an add-on.
		if old != "" {
			_ = d.rename(old, dst)
		}
		return fmt.Errorf("install add-on: %w", err)
	}
	return nil
}

// waitFlat prints the plain-English wait line and polls the flat probe until
// flat (M3/S13), returning errNotFlat on timeout.
func waitFlat(ctx context.Context, d deps) error {
	d.print("Waiting until your trades are flat (NinjaTrader must be open)…")
	return waitUntilFlat(ctx, d.positions, 60*time.Second)
}

// oldAddonTimestampedPath returns a timestamped backup path for the old add-on
// (m6): <MyDocuments>\NinjaTrader 8\VL\old\VL.AddOn-<ts>.dll — never
// overwritten.
func oldAddonTimestampedPath(docs string) string {
	return filepath.Join(ntVLdir(docs), "old", "VL.AddOn-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".dll")
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
