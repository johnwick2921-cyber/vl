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
// H1 adds "known" and "last_known_flat_ms": known=false means the bot cannot
// currently see NinjaTrader (e.g. the user just closed it), and
// last_known_flat_ms is the latest ms at which every trader was known AND flat
// (0 = never).
type flatProbeResult struct {
	Flat            bool  `json:"flat"`
	Traders         *int  `json:"traders"`
	Known           bool  `json:"known"`
	LastKnownFlatMs int64 `json:"last_known_flat_ms"`
}

// probeFlatState returns the RAW /api/friend/flat state (H1).
func probeFlatState(ctx context.Context, d deps, base string) (flatProbeResult, error) {
	b, err := d.fetch(ctx, base+"/api/friend/flat")
	if err != nil {
		return flatProbeResult{}, fmt.Errorf("flat probe: %w", err)
	}
	var r flatProbeResult
	if err := json.Unmarshal(b, &r); err != nil {
		return flatProbeResult{}, fmt.Errorf("flat probe: %w", err)
	}
	return r, nil
}

// probeFlat asks the friend bot's /api/friend/flat endpoint and maps it to the
// positionsProvider contract: 0 = flat/safe, non-zero = open. Any probe error
// is an error (fail closed): the caller's waitUntilFlat treats it as "not flat"
// and defers, never swapping mid-trade.
func probeFlat(ctx context.Context, d deps, base string) (float64, error) {
	r, err := probeFlatState(ctx, d, base)
	if err != nil {
		return 0, err
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
	already, err := ensureTailscaleInstalled(ctx, d, ic.TSAuthKey)
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
		deferred, err := waitNinjaTraderClosed(ctx, d, false)
		if err != nil {
			return err
		}
		if deferred {
			return fmt.Errorf("install deferred — close NinjaTrader and run VL-Setup again")
		}
		// H11: a first run that REPLACES an existing DLL is flat-gated too —
		// never swap over an open position.
		d.positions = func(c context.Context) (float64, error) {
			return probeFlat(c, d, base)
		}
		if err := waitFlat(ctx, d); err != nil {
			return err
		}
	}
	if err := placeAddon(d, docs, m.AddonBuild, addon); err != nil {
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
		// G11: README is non-fatal.
		d.print("warning: could not write the README — " + err.Error())
	}
	d.print("[6/6] All done ✓. A VL icon is on your desktop. Double-click it, create your login, then add your AI key, your strategy and a trader on your SIM account.")
	d.print("Your VL dashboard:  " + base)
	d.print("A VL icon is on your desktop and in the Start Menu.")
	if err := d.openURL(ctx, base); err != nil {
		// G11: the dashboard open is non-fatal — the URL is already on screen.
		d.print("warning: could not open the dashboard — " + err.Error())
	}
	return nil
}

// runFirstRun is the whole first-run console flow: the [1/6] install-code
// prompt, installFirstRun ([2/6]…[6/6]), then "Press Enter to close". It lives
// here (not run_windows.go) so a Linux test can assert the step order. G8: a
// bad paste re-prompts up to 3 times, never echoing the code.
func runFirstRun(ctx context.Context, d deps) error {
	d.print("[1/6] Paste the install code you were sent, then press Enter.")
	for attempt := 1; ; attempt++ {
		code, err := d.readSecret("")
		if err != nil {
			return err
		}
		ic, err := ParseInstallCode(code)
		if err != nil {
			// Never echo the code (it holds ts_authkey + connect_key).
			if attempt >= 3 {
				return fmt.Errorf("invalid install code — check the copy you were sent")
			}
			d.print("That code looks wrong — paste it again.")
			continue
		}
		secrets := []string{ic.TSAuthKey, ic.ConnectKey}
		if err := installFirstRun(ctx, d, code); err != nil {
			// G5: the approval-timeout error carries its own instruction — never
			// wrap it with the generic "install failed" framing.
			if errors.Is(err, errApprovalTimeout) {
				return err
			}
			return fmt.Errorf("install failed: %s", redactSecrets(err.Error(), secrets...))
		}
		break
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

// errApprovalTimeout is the sentinel for "the owner has not approved this PC
// yet" (G5). It carries its own instruction, so the generic "send a photo"
// framing is never appended to it.
var errApprovalTimeout = errors.New("the owner has not approved this PC yet")

// userFacing turns an error into the console line. G5: the approval-timeout
// error is complete and actionable on its own — never append the
// "nothing was changed / send a photo" framing to it.
func userFacing(err error) string {
	if errors.Is(err, errApprovalTimeout) {
		return "The owner has not approved this PC yet. Ask the owner to approve it, then run VL-Setup again with the SAME code."
	}
	return "Something went wrong: " + err.Error() + ". Nothing was changed on NinjaTrader. Send a photo of this window to the owner."
}

// pauseOnError prints the (already-redacted) error screen and waits for Enter
// so a double-clicked console window does not close before the user reads it
// (P0 b). Testable on Linux through the print + readLine seams.
func pauseOnError(d deps, err error) {
	d.print(redactTSKeys(userFacing(err)))
	d.print("Press Enter to close")
	_, _ = d.readLine("")
}

// setupLogPath returns %LOCALAPPDATA%\VL\setup.log (F7: LOCALAPPDATA, not
// <home>\AppData\Local).
func setupLogPath(d deps) string {
	return filepath.Join(d.localAppDataDir(), "VL", "setup.log")
}

// logSetupError appends the (already-redacted) error to setup.log; best effort.
// F7: APPEND, never overwrite — the full error history survives. S17: it uses
// the O_APPEND seam (a TRUE append), never read-modify-write (which raced and
// dropped concurrent lines). S10: the tskey regex is applied centrally here.
func logSetupError(d deps, msg string) {
	dir := filepath.Join(d.localAppDataDir(), "VL")
	if d.mkdirAll(dir, 0o755) != nil {
		return
	}
	line := time.Now().Format(time.RFC3339) + " " + redactTSKeys(msg) + "\n"
	_ = d.appendFile(setupLogPath(d), []byte(line), 0o600)
}

// downloadTailscaleMSI downloads the official installer to a temp file and
// verifies its Authenticode signer is Tailscale Inc BEFORE anything is run.
// A bad or missing signature refuses (the MSI is never executed).
func downloadTailscaleMSI(ctx context.Context, d deps) (string, error) {
	// G12: tell the user the download started (it is ~12 MB and silent otherwise).
	d.print("Downloading Tailscale… 12 MB")
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
			// G3: never tell a standard user to "run as administrator" — that
			// switches profiles and points at the wrong Documents/LOCALAPPDATA.
			// The elevated MSI step already ran `up`; a refusal here surfaces as
			// the (redacted) raw error.
			// Redact the auth key from BOTH the error and the command output —
			// exec errors can echo the full command line (P0 d: never print keys).
			return fmt.Errorf("tailscale up: %s", redactSecrets(err.Error()+" "+string(out), authKey))
		}
		return waitTailscaleApproved(ctx, d)
	case "NeedsMachineAuth":
		// S5: mid-approval — the device is ALREADY on our tailnet and just waiting
		// for the owner to approve it. Never refuse this as "another network".
		if err := waitTailscaleApproved(ctx, d); err != nil {
			return err
		}
		// H8: after approval, re-verify the tailnet identity before continuing.
		return verifyOurTailnet(ctx, d, base)
	case "Stopped":
		// H8: Tailscale is installed but turned off — turn it back on with NO
		// auth key, then verify identity.
		d.print("Tailscale is turned off — turning it on…")
		upCtx, cancel := context.WithTimeout(ctx, tailscaleUpTimeout)
		_, err := tailscaleExec(upCtx, d, "up", "--unattended")
		cancel()
		if err != nil {
			return fmt.Errorf("tailscale up: %s", redactSecrets(err.Error(), authKey))
		}
		if err := waitTailscaleApproved(ctx, d); err != nil {
			return err
		}
		return verifyOurTailnet(ctx, d, base)
	case "Running":
		// S5: prove OUR tailnet by IDENTITY — the bot host must appear among
		// `tailscale status --json` peers — never by an HTTP fetch. The auth key
		// is spent, so a re-run must skip `up` and continue when it is ours.
		return verifyOurTailnet(ctx, d, base)
	default:
		return fmt.Errorf("this PC is already logged in to another Tailscale network (state %q). Log out of Tailscale first, then run VL-Setup again", state)
	}
}

// verifyOurTailnet proves the bot host is on OUR tailnet by IDENTITY (S5): the
// host (literal IP or MagicDNS name) must appear among Self + every Peer's
// TailscaleIPs/DNSName/HostName in `tailscale status --json`. A transient CLI
// failure is NOT a refusal — the flow proceeds and the [4/6] manifest fetch is
// the real proof downstream.
func verifyOurTailnet(ctx context.Context, d deps, base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("install code base URL: %w", err)
	}
	ips, names, err := tailscaleTailnet(ctx, d)
	if err != nil {
		return waitTailscaleApproved(ctx, d)
	}
	if hostOnTailnet(u.Hostname(), ips, names) {
		return waitTailscaleApproved(ctx, d)
	}
	return fmt.Errorf("this PC is already logged in to another Tailscale network. Log out of Tailscale first, then run VL-Setup again")
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
			case "NoState", "Starting":
				// keep polling until it settles
			default:
				// "Stopped" settles too (H8) — it is handled as its own state.
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
// line every tailscaleApprovalPrint. G5: the 15-minute budget is measured from
// NOW (its own context), not eaten by idle time already spent in the caller's
// 30-minute context; on timeout the error is the dedicated approval message.
func waitTailscaleApproved(ctx context.Context, d deps) error {
	actx, cancel := context.WithTimeout(context.Background(), tailscaleApprovalWait)
	defer cancel()
	t := time.NewTicker(tailscaleApprovalPoll)
	defer t.Stop()
	printT := time.NewTicker(tailscaleApprovalPrint)
	defer printT.Stop()
	printedURL := false
	for {
		st, err := tailscaleStatus(actx, d)
		if err == nil && st.BackendState == "Running" {
			return nil
		}
		// H17: the approval link is printed ONLY while NeedsMachineAuth, labelled
		// for the owner; NeedsLogin prints nothing (never a login token).
		if err == nil && !printedURL && st.BackendState == "NeedsMachineAuth" && st.AuthURL != "" {
			d.print("Approval link (for the owner): " + st.AuthURL)
			printedURL = true
		}
		select {
		case <-actx.Done():
			return errApprovalTimeout
		case <-printT.C:
			d.print("Still waiting for the owner to approve…")
		case <-t.C:
		}
	}
}

// ensureTailscaleInstalled downloads + verifies + silently installs Tailscale
// when it is not already present, then waits for it to become ready. It reports
// whether Tailscale was already installed so the caller can print the skip line
// ([2/6]). G3: the one-time auth key is passed into the ELEVATED step, so a
// standard (non-admin) user gets ONE UAC prompt that covers both the MSI and
// `tailscale up` — never "run as administrator" (which would switch profiles).
func ensureTailscaleInstalled(ctx context.Context, d deps, authKey string) (bool, error) {
	if _, err := tailscaleBackendState(ctx, d); err == nil {
		return true, nil
	}
	msiPath, err := downloadTailscaleMSI(ctx, d)
	if err != nil {
		return false, err
	}
	code, err := installTailscaleElevated(ctx, d, msiPath, authKey)
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
	if _, err := ensureTailscaleInstalled(ctx, d, authKey); err != nil {
		return err
	}
	return tailscaleConnect(ctx, d, authKey, base)
}

// elevatedInstallScript is the PowerShell that runs ELEVATED. P2a: it starts
// with the psPreamble. P1: the MSI path arrives in $env:VL_ARG0 and the auth
// key in $env:VL_ARG1 (never positional args, never spliced). It re-verifies
// the Authenticode signature IMMEDIATELY before msiexec (S8 TOCTOU guard) and
// parses the signer DN for an EXACT O= (H13). UAC cancellation is reported by
// NativeErrorCode 1223 (W10) — never English text. G3/H14a: after msiexec it
// runs `tailscale up` with the key from the TS_AUTHKEY ENV (never argv), so one
// UAC prompt covers both and a standard user is never told to "run as
// administrator". P2c: it prints ONE marker line VL_RESULT=<msiexec exit code>.
const elevatedInstallScript = psPreamble + `$msi = $env:VL_ARG0
$key = $env:VL_ARG1
$sig = Get-AuthenticodeSignature -FilePath $msi
if ($sig.Status -ne 'Valid') { Write-Output 'VL_RESULT=MSI_NOT_VALID'; exit 2 }
$org = ''
foreach ($part in ($sig.SignerCertificate.Subject -split ',')) { $kv = $part.Trim(); if ($kv.StartsWith('O=')) { $org = $kv.Substring(2); break } }
if ($org -ne 'Tailscale Inc.') { Write-Output 'VL_RESULT=MSI_BAD_SIGNER'; exit 3 }
$arg = '/i "' + $msi + '" /quiet /norestart'
try {
  $p = Start-Process msiexec -Verb RunAs -Wait -PassThru -ArgumentList $arg
  Write-Output ('VL_RESULT=' + $p.ExitCode)
  if ($key) {
    $env:TS_AUTHKEY = $key
    & 'C:\Program Files\Tailscale\tailscale.exe' up --unattended 2>$null | Out-Null
  }
} catch {
  if ($_.Exception.InnerException.NativeErrorCode -eq 1223) { Write-Output 'VL_RESULT=UAC_CANCELLED'; exit 0 }
  throw
}`

// installTailscaleElevated starts msiexec ELEVATED so Windows shows the UAC
// Yes/No prompt (a per-machine /quiet MSI from a non-admin process fails with
// no prompt — 1603/1625). It returns the msiexec exit code. S8/S23a/W10/S18b/G3:
// the command is sent via -EncodedCommand with the MSI path as $args[0] and the
// auth key as $args[1] (never spliced into the script, never echoed or logged).
func installTailscaleElevated(ctx context.Context, d deps, msiPath, authKey string) (int, error) {
	out, err := d.exec(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(elevatedInstallScript), msiPath, authKey)
	if err != nil {
		return 0, fmt.Errorf("tailscale install: %w: %s", err, strings.TrimSpace(string(out)))
	}
	line, err := psResultLine(out)
	if err != nil {
		return 0, fmt.Errorf("tailscale install: %w", err)
	}
	switch line {
	case "UAC_CANCELLED":
		return 1223, nil
	case "MSI_NOT_VALID", "MSI_BAD_SIGNER":
		// H18a: a signature-verify failure is most likely an update being
		// published mid-flight — tell the user to retry shortly.
		return 0, fmt.Errorf("the Tailscale installer signature did not verify (%s) — try again in a minute (an update was being published)", line)
	}
	code, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("tailscale install: unreadable exit code %q", line)
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
		"if(Test-Path $exe){$w=New-Object -ComObject WScript.Shell;foreach($dir in @($d,$s)){$p=\"$dir\\VL.lnk\";if(-not (Test-Path $p)){$l=$w.CreateShortcut($p);$l.TargetPath=$exe;$l.Arguments='--open';$l.IconLocation=$exe;$l.Save()}}}else{$u=\"[InternetShortcut]`r`nURL=" + appURL + "`r`n\";foreach($dir in @($d,$s)){$p=\"$dir\\VL.url\";if(-not (Test-Path $p)){[System.IO.File]::WriteAllText($p,$u)}}}",
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

// localExeVersionPath records the version of the LOCAL copy (S23), written
// beside it when the copy is made.
func localExeVersionPath(d deps) string {
	return localExePath(d) + ".version"
}

// ensureLocalExe copies the running executable to %LOCALAPPDATA%\VL\vl-friend.exe
// so the desktop icon can launch --open. When the copy already exists and
// differs (a newer installer ran), the old copy is MOVED to VL\old\ (never
// deleted) before the new one is written.
// S23 / DS-104 I2: it replaces the copy ONLY when the running exe is NEWER than
// the recorded copy version — an older installer must not downgrade the copy
// the icon launches.
func ensureLocalExe(d deps) error {
	// Newer-only gate: if the recorded copy version is current-or-newer, skip.
	if vb, err := d.readFile(localExeVersionPath(d)); err == nil {
		recorded := strings.TrimSpace(string(vb))
		if recorded != "" && !appVersionNewer(appVersion, recorded) {
			return nil
		}
	}
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
		// Already byte-identical: record the version and stop (idempotent).
		return writeFileAtomic(d, localExeVersionPath(d), []byte(appVersion+"\n"), 0o600)
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
	if err := d.writeFile(dst, srcBytes, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(d, localExeVersionPath(d), []byte(appVersion+"\n"), 0o600)
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

// runOpen is the desktop-icon path (vl-friend.exe --open): single-instance
// guarded (G2), open the dashboard FIRST, then the shortcut check (G13), then a
// quick silent update check (≤5s); only an ADD-ON update shows a Windows dialog
// (M5). Update now = the verified flat-gated swap with GUI-only prompts (G1),
// bounded to its own ctx so it can never hang the icon launch (M3).
func runOpen(ctx context.Context, d deps) error {
	// G2: a second icon click must not race the add-on swap — it just opens the
	// dashboard and exits.
	if !d.singleInstance() {
		if base, err := resolveDashboardBase(ctx, d); err == nil {
			_ = d.openURL(ctx, base)
		}
		return nil
	}

	// W13b: migrate a legacy <USERPROFILE>\Documents layout into MyDocuments
	// (non-fatal).
	if home, herr := d.homeDir(); herr == nil {
		if docs, derr := d.docsDir(); derr == nil {
			_ = migrateDocsIfNeeded(d, home, docs)
		}
	}

	base, err := resolveDashboardBase(ctx, d)
	if err != nil {
		d.errorBox("VL could not start: " + err.Error() + ". Send a photo of this to the owner.")
		return err
	}

	// B2: wire the production flat probe — runOpen never set d.positions, so an
	// icon-launched "Update now" used the stub ("flat probe not wired") and hung
	// until the ctx expired.
	d.positions = func(c context.Context) (float64, error) {
		return probeFlat(c, d, base)
	}

	// G13/F8: open the dashboard FIRST, then re-create a MISSING shortcut
	// (non-fatal, and the .lnk/.url presence check is in the script).
	if oerr := d.openURL(ctx, base); oerr != nil {
		d.errorBox("VL could not start: " + oerr.Error() + ". Send a photo of this to the owner.")
		return oerr
	}
	if err := createDesktopShortcut(ctx, d, base); err != nil {
		// G1: no console in --open; never print.
		_ = err
	}

	// Quick bounded update check; a probe error is shown (m12/W15), never silent.
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	dec, derr := detectUpdate(cctx, d, base)
	cancel()
	if derr != nil {
		// W15: the 5-second manifest probe failed — the dashboard is already open,
		// so just tell the mentor the bot is unreachable.
		d.infoBox("VL cannot reach your bot — open Tailscale and check it says Connected.")
		return nil
	}
	if !dec.AddonUpdateAvailable {
		return nil
	}

	// M4: the dialog names the build it would move between.
	msg := fmt.Sprintf("VL update available (add-on %s → %s). Yes = update now, No = later.", dec.CurrentBuild, dec.Manifest.AddonBuild)
	if !d.messageBox(msg) {
		return nil
	}

	// Update now: GUI prompts only, bounded to its own ctx (M3, ≤5 min) so the
	// icon launch can never hang; on a flat timeout it DEFERS with a message (G2).
	uctx, ucancel := context.WithTimeout(ctx, 5*time.Minute)
	defer ucancel()
	dec2, uerr := RunUpdateCheck(uctx, d, base, true, true)
	if uerr != nil {
		if errors.Is(uerr, errNotFlat) {
			d.infoBox("Update postponed — a trade is open. VL will offer it again next time.")
			return nil
		}
		d.errorBox("VL could not start: " + uerr.Error() + ". Send a photo of this to the owner.")
		return uerr
	}
	// H4: announce only when the swap actually applied.
	if dec2.Applied {
		d.infoBox("Updated ✓ — restart NinjaTrader to load it.")
	}
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
// Edge is present (resolved to a full path), else the default browser. P1: the
// URL arrives in $env:VL_ARG0 (PowerShell 5.1 rejects a positional arg after
// -EncodedCommand) — never spliced into the script text.
func openScript() string {
	return strings.Join([]string{
		"$edge=$null",
		"foreach($r in @('HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\App Paths\\msedge.exe','HKCU:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\App Paths\\msedge.exe')){try{$v=(Get-ItemProperty -Path $r -ErrorAction Stop).'(default)';if($v -and (Test-Path $v)){$edge=$v;break}}catch{}}",
		"if(-not $edge){foreach($p in @(\"${env:ProgramFiles(x86)}\\Microsoft\\Edge\\Application\\msedge.exe\",\"$env:ProgramFiles\\Microsoft\\Edge\\Application\\msedge.exe\")){if(Test-Path $p){$edge=$p;break}}}",
		"if($edge){Start-Process $edge -ArgumentList ('--app=' + $env:VL_ARG0)}else{Start-Process $env:VL_ARG0}",
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
	// S14: when the bot serves an identity endpoint, prove it is MY bot (the
	// connect key must match sha256). A bot with a DIFFERENT key refuses; an old
	// bot that does not serve the endpoint yet fails open (the manifest
	// signature above already proved authenticity).
	if err := verifyBotIdentity(ctx, d, base, cfg.ConnectKey); err != nil {
		if errors.Is(err, errIdentityMismatch) {
			return appState{}, fmt.Errorf("this is not your VL bot (identity key mismatch) — send this to the owner")
		}
		// network / endpoint-missing: fail open.
	}
	st := appState{WebPort: port}
	if err := writeAppState(d, docs, st); err != nil {
		return appState{}, err
	}
	return st, nil
}

// fetchIdentityKeySHA GETs /api/friend/identity and returns the key_sha256
// field (DS-102's endpoint). The bot returns sha256(its connect key), so it
// identifies WHICH bot we are talking to.
func fetchIdentityKeySHA(ctx context.Context, d deps, base string) (string, error) {
	b, err := d.fetch(ctx, base+"/api/friend/identity")
	if err != nil {
		return "", err
	}
	var id struct {
		KeySHA256 string `json:"key_sha256"`
	}
	if err := json.Unmarshal(b, &id); err != nil {
		return "", fmt.Errorf("identity: %w", err)
	}
	return id.KeySHA256, nil
}

// errIdentityMismatch means the bot at base holds a DIFFERENT connect key than
// this PC — it is another friend's bot.
var errIdentityMismatch = errors.New("identity key mismatch")

// verifyBotIdentity proves base is MY bot: GET /api/friend/identity must return
// sha256(my connect key). A network/unreachable endpoint is also an error — the
// caller decides whether to fail open (an old bot may not serve it yet).
func verifyBotIdentity(ctx context.Context, d deps, base, connectKey string) error {
	got, err := fetchIdentityKeySHA(ctx, d, base)
	if err != nil {
		return err
	}
	want := sha256.Sum256([]byte(connectKey))
	if got != hex.EncodeToString(want[:]) {
		return errIdentityMismatch
	}
	return nil
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
			// S23: a migration failure is NON-fatal — warn and continue; never
			// abort the whole run over a best-effort move.
			d.print("warning: could not move " + m.from + " — " + err.Error())
			continue
		}
		if err := d.rename(m.from, m.to); err != nil {
			d.print("warning: could not move " + m.from + " — " + err.Error())
			continue
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
	code, err := d.readSecret("")
	if err != nil {
		return err
	}
	if err := applyNewCode(ctx, d, docs, code); err != nil {
		return err
	}
	d.print("New install code applied ✓")
	return nil
}

// runIncomplete handles an existing-but-UNREADABLE connect.json (S19): that is
// an INCOMPLETE install, not a first run and not a healthy install. It offers a
// new install code; otherwise it returns a clear error.
func runIncomplete(ctx context.Context, d deps, docs string) error {
	d.print("VL setup is incomplete (connect.json is unreadable).")
	d.print("Have a new install code? Type N then press Enter (or just press Enter to close).")
	if line, _ := d.readLine(""); strings.EqualFold(strings.TrimSpace(line), "N") {
		return promptAndApplyNewCode(ctx, d, docs)
	}
	return fmt.Errorf("VL setup is incomplete — connect.json is unreadable")
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
	// S14: prove the NEW bot is MY bot before swapping the connect key — a bot
	// with a different key refuses; an old bot without the identity endpoint
	// fails open (the tailnet peer check above already passed).
	if err := verifyBotIdentity(ctx, d, ic.BaseURL(), ic.ConnectKey); err != nil {
		if errors.Is(err, errIdentityMismatch) {
			return errors.New("this is not your VL bot (identity key mismatch) — ask the owner for a fresh install code")
		}
		// network / endpoint-missing: fail open.
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
		if err := placeAddon(d, docs, m.AddonBuild, addon); err != nil {
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
	// G4: exactly one outcome — Updated, app-only note, or deferred. Never
	// "Updated ✓" followed by "deferred".
	if dec.Applied {
		d.print("Updated ✓ — restart NinjaTrader to load the new add-on.")
	} else if dec.AppUpdateAvailable {
		// F6: there is no self-update — say it honestly.
		d.print("A new VL-Setup is available — ask the owner for it.")
	} else if dec.AddonUpdateAvailable {
		d.print("Update deferred (Later).")
	}

	// S6: offer a new install code (the owner may re-issue one). Skipping is
	// the default — this is a gate, never a forced prompt.
	d.print("Answer in this black window ↓")
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
// VL\old\, never deleted). gui selects the --open prompt style (message boxes,
// no stdin); a readLine error always DEFERS (G1).
func RunUpdateCheck(ctx context.Context, d deps, base string, autoApprove bool, gui ...bool) (UpdateDecision, error) {
	isGUI := len(gui) > 0 && gui[0]
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
		// G9: point at the console before the first y/N after the browser opened.
		if !isGUI {
			d.print("Answer in this black window ↓")
		}
		ans, err := d.readLine("VL update available — Update now? (y/N)")
		if err != nil {
			return dec, nil // G1: a read error defers, never fails
		}
		// G10: accept y/yes (case-insensitive); anything else (including n/no)
		// is Later.
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
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
		// H4: the DLL already matches — record the build so the check stays
		// consistent, then stop (nothing to swap).
		if err := writeInstalledState(d, docs, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
			return dec, err
		}
		return dec, nil
	}
	// Never update during a trade: wait until the friend bot is flat.
	if err := waitFlat(ctx, d); err != nil {
		return dec, err
	}
	// H1: record T0 — the latest known-flat instant — right BEFORE asking the
	// user to close NinjaTrader.
	st0, err := probeFlatState(ctx, d, base)
	if err != nil {
		return dec, err
	}
	t0 := st0.LastKnownFlatMs
	// F4: never write a locked DLL — ask the user to close NinjaTrader (or defer).
	deferred, err := waitNinjaTraderClosed(ctx, d, isGUI)
	if err != nil {
		return dec, err
	}
	if deferred {
		return dec, nil // Later
	}
	// H1: after NT closed the bot may go known→unknown (it can no longer see
	// NinjaTrader). Accept only if still flat, or if unknown AND the
	// last-known-flat instant did not move backwards while the user closed NT.
	st1, err := probeFlatState(ctx, d, base)
	if err != nil {
		return dec, err
	}
	if !st1.Flat && !(st1.Known == false && st1.LastKnownFlatMs >= t0) {
		return dec, errNotFlat
	}
	// S2/S16: atomic swap (write .new → move old aside → rename into place).
	if err := placeAddon(d, docs, m.AddonBuild, addon); err != nil {
		return dec, err
	}
	if err := writeInstalledState(d, docs, installedState{AddonBuild: m.AddonBuild, AppVersion: appVersion}); err != nil {
		return dec, err
	}
	dec.Applied = true
	return dec, nil
}

// waitNinjaTraderClosed prompts the user to close NinjaTrader (F4) while it is
// running, re-checking each time. It returns (true, nil) when the user defers,
// or (false, nil) once NinjaTrader is closed. G1: in GUI mode (--open) it uses a
// Yes/No message box, never stdin; a readLine error is a DEFER, never an error
// (after FreeConsole stdin is invalid and must not read as "could not start").
func waitNinjaTraderClosed(ctx context.Context, d deps, gui bool) (bool, error) {
	for {
		running, err := d.ntRunning(ctx)
		if err != nil {
			return false, err
		}
		if !running {
			return false, nil
		}
		if gui {
			if !d.messageBox("Close NinjaTrader, then click Yes to install the add-on update (No = later)") {
				return true, nil // defer
			}
			continue
		}
		ans, err := d.readLine("Close NinjaTrader, then press Enter to install the add-on update (or type L for later)")
		if err != nil {
			return true, nil // G1: a read error (detached console) defers, never fails
		}
		if strings.EqualFold(ans, "L") {
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
// is moved back. Never truncates a loaded DLL. H7: it refuses to DOWNGRADE
// (install an older build over a newer one) unless neither the DLL nor
// installed.json exists. H12: "absent" means fs.ErrNotExist only (any other read
// error aborts), the bin\Custom write is gated on the F2 dirExists proof, and a
// failed rollback reports the backup path.
func placeAddon(d deps, docs, build string, addon []byte) error {
	ntBinCustom := filepath.Join(docs, "NinjaTrader 8", "bin", "Custom")
	if !d.dirExists(ntBinCustom) {
		return fmt.Errorf("NinjaTrader 8 has not created its folders — open NinjaTrader 8 once, then run VL-Setup again")
	}
	dst := addonDstPath(docs)
	tmp := dst + ".new"

	// H7: never downgrade — refuse to install an older build over a newer
	// recorded one. A fresh install (no installed.json) has no "current" build
	// to protect, so the check is a no-op there.
	cur, _ := readInstalledState(d, docs)
	if cur.AddonBuild != "" && addonBuildNewer(cur.AddonBuild, build) {
		return fmt.Errorf("refusing to replace a newer add-on (build %s) with an older one (build %s)", cur.AddonBuild, build)
	}

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
	} else if !errors.Is(err, fs.ErrNotExist) {
		// H12: a read error on the existing DLL aborts — never write over it
		// blind.
		return fmt.Errorf("read existing add-on: %w", err)
	}
	if err := d.rename(tmp, dst); err != nil {
		// S2: roll the old DLL back so the PC is never left without an add-on.
		if old != "" {
			if rb := d.rename(old, dst); rb != nil {
				return fmt.Errorf("install add-on: %w (and could not restore the previous add-on from %s: %v)", err, old, rb)
			}
		}
		return fmt.Errorf("install add-on: %w", err)
	}
	return nil
}

// waitFlat prints the plain-English wait line and polls the flat probe until
// flat (M3/S13), returning errNotFlat on timeout.
func waitFlat(ctx context.Context, d deps) error {
	// G6: the next step may ask the user to CLOSE NinjaTrader, so never say
	// "must be open" here.
	d.print("Checking that you have no open trade…")
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
