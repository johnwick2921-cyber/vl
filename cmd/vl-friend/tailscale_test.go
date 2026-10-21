package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tailscale_test.go — the first-run Tailscale flow (P0 fix): MSI download +
// Authenticode verify, elevated install (UAC), PATH fallback, other-tailnet
// refusal, device-approval wait and timeout, and never-print-secrets. All over
// fakes; no real Windows action.

type tailscaleFake struct {
	installed      bool   // `tailscale status` answers? flips true after a successful install
	beforeState    string // BackendState before "up"
	afterUpState   string // BackendState after "up" (once approvePolls exhausted)
	approvePolls   int    // # of post-up status polls returning NeedsMachineAuth
	upErr          error
	signerStatus   string // Authenticode Status (default "Valid")
	signerSubject  string // signer certificate subject
	signerErr      error
	fetchErr       error
	msiExitCode    int   // exit code the elevated install returns (default 0)
	msiErr         error // UAC-cancel style error from the elevated install
	upCalled       bool
	upHangs        bool     // `tailscale up` blocks until its ctx is done (models a hang)
	upArgs         []string // args of the last `tailscale up` call
	msiexecCalled  bool
	downloadCalled bool
	signCalled     bool
	polls          int
	installCmd     string
	execNames      []string
	authURL        string // injected into status JSON while NeedsMachineAuth
	prints         []string
	statusCalls    int    // # of `tailscale status` calls so far
	statusErrStart int    // first status call index that errors (0 = never)
	statusErrCount int    // # of consecutive status calls that error from statusErrStart
	statusJSON     string // verbatim status JSON for every status call (overrides state machine)
}

// status models one `tailscale status` invocation. The BARE and the fixed-path
// binaries share one counter (they see the same daemon), so a transient daemon
// failure makes BOTH attempts fail — exactly what tailscaleExec's fallback does.
func (f *tailscaleFake) status() ([]byte, error) {
	if !f.installed {
		return nil, fmt.Errorf("tailscale: executable not found")
	}
	f.statusCalls++
	if f.statusErrStart != 0 && f.statusCalls >= f.statusErrStart &&
		f.statusCalls < f.statusErrStart+f.statusErrCount {
		return nil, fmt.Errorf("tailscale status: transient failure")
	}
	if f.statusJSON != "" {
		return []byte(f.statusJSON), nil
	}
	st := f.beforeState
	if f.upCalled {
		f.polls++
		if f.polls <= f.approvePolls {
			st = "NeedsMachineAuth"
		} else {
			st = f.afterUpState
		}
	}
	body := `{"BackendState":"` + st + `"`
	if st == "NeedsMachineAuth" && f.authURL != "" {
		body += `,"AuthURL":"` + f.authURL + `"`
	}
	return []byte(body + `}`), nil
}

func (f *tailscaleFake) exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.execNames = append(f.execNames, name)
	switch {
	case name == "tailscale" && len(args) > 0 && args[0] == "status":
		return f.status()
	case name == "tailscale" && len(args) > 0 && args[0] == "up":
		f.upCalled = true
		f.upArgs = append([]string(nil), args...)
		if f.upHangs {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if f.upErr != nil {
			return nil, f.upErr
		}
		return nil, nil
	case name == tailscaleExeFallback:
		if len(args) > 0 && args[0] == "status" {
			return f.status()
		}
		return f.exec(ctx, "tailscale", args...)
	case name == "powershell.exe" && len(args) >= 4 && args[2] == "-EncodedCommand":
		script := decodePSCommand(args[3])
		if !strings.Contains(script, "Start-Process msiexec") {
			return nil, nil
		}
		f.installCmd = script
		f.msiexecCalled = true
		if f.msiErr != nil {
			// Simulate the elevated script's UAC-cancel marker (W10: 1223).
			return []byte("VL_RESULT=UAC_CANCELLED"), nil
		}
		f.installed = true
		return []byte("VL_RESULT=" + strconv.Itoa(f.msiExitCode)), nil
	}
	return nil, nil
}

// decodePSCommand reverses psEncodedCommand (base64 UTF-16LE) for the test fake.
func decodePSCommand(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for i := 0; i+1 < len(raw); i += 2 {
		sb.WriteRune(rune(uint16(raw[i]) | uint16(raw[i+1])<<8))
	}
	return sb.String()
}

func (f *tailscaleFake) deps(t *testing.T) deps {
	t.Helper()
	return deps{
		exec: f.exec,
		fetch: func(_ context.Context, url string) ([]byte, error) {
			f.downloadCalled = true
			if f.fetchErr != nil {
				return nil, f.fetchErr
			}
			return []byte("MZ-fake-msi"), nil
		},
		tempDir: func() (string, error) { return t.TempDir(), nil },
		verifyMsiSigner: func(string) (msiSignature, error) {
			f.signCalled = true
			if f.signerErr != nil {
				return msiSignature{}, f.signerErr
			}
			st := f.signerStatus
			if st == "" {
				st = "Valid"
			}
			return msiSignature{Status: st, Subject: f.signerSubject}, nil
		},
		writeFile:      os.WriteFile,
		readLine:       func(string) (string, error) { return "", nil },
		readSecret:     func(string) (string, error) { return "", nil },
		print:          func(l string) { f.prints = append(f.prints, l) },
		messageBox:     func(string) bool { return false },
		errorBox:       func(string) {},
		infoBox:        func(string) {},
		singleInstance: func() bool { return true },
	}
}

func shortWaits(t *testing.T) {
	t.Helper()
	prev := []time.Duration{tailscaleReadyPoll, tailscaleApprovalPoll, tailscaleInstallWait, tailscaleApprovalWait, tailscaleUpTimeout, tailscaleApprovalPrint}
	t.Cleanup(func() {
		tailscaleReadyPoll, tailscaleApprovalPoll, tailscaleInstallWait, tailscaleApprovalWait, tailscaleUpTimeout, tailscaleApprovalPrint =
			prev[0], prev[1], prev[2], prev[3], prev[4], prev[5]
	})
	tailscaleReadyPoll = 2 * time.Millisecond
	tailscaleApprovalPoll = 2 * time.Millisecond
	tailscaleInstallWait = 100 * time.Millisecond
	tailscaleApprovalWait = 100 * time.Millisecond
	tailscaleUpTimeout = 50 * time.Millisecond
	tailscaleApprovalPrint = 20 * time.Millisecond
}

func TestEnsureTailscaleAlreadyInstalledJoins(t *testing.T) {
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running"}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("already-installed join refused: %v", err)
	}
	if !f.upCalled {
		t.Fatal("tailscale up was not called")
	}
	if f.msiexecCalled || f.downloadCalled {
		t.Fatal("should not download/install when already installed")
	}
	for _, n := range f.execNames {
		if n == "tailscaleInstall" {
			t.Fatalf("flow invoked the nonexistent 'tailscaleInstall' executable — mutant should be RED")
		}
	}
}

func TestEnsureTailscaleInstallsWhenMissing(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", afterUpState: "Running",
		signerSubject: "CN=Tailscale Inc., O=Tailscale Inc., L=Toronto, C=CA"}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("fresh install refused: %v", err)
	}
	if !f.downloadCalled || !f.signCalled || !f.msiexecCalled || !f.upCalled {
		t.Fatalf("expected download+verify+elevated-install+up, got download=%v sign=%v install=%v up=%v",
			f.downloadCalled, f.signCalled, f.msiexecCalled, f.upCalled)
	}
}

// REVIEW-BACK 1 pin: the install must run ELEVATED (Start-Process -Verb RunAs).
// MUTANT: drop "-Verb RunAs" from the command -> this test RED.
func TestInstallRunsElevated(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", afterUpState: "Running",
		signerSubject: "CN=Tailscale Inc., O=Tailscale Inc."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	if !strings.Contains(f.installCmd, "-Verb RunAs") {
		t.Fatalf("the install did not run elevated; command was %q", f.installCmd)
	}
	if !strings.Contains(f.installCmd, "/quiet") || !strings.Contains(f.installCmd, "/norestart") {
		t.Fatalf("the install args are wrong; command was %q", f.installCmd)
	}
	// S18b: the MSI path is passed as $args[0], not spliced into the script.
	if strings.Contains(f.installCmd, "MZ-fake-msi") {
		t.Fatalf("the MSI path must not be spliced into the script: %q", f.installCmd)
	}
}

func TestEnsureTailscaleInstallFails(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc.", msiExitCode: 1603}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err == nil {
		t.Fatal("a failing MSI install was accepted")
	}
	if f.upCalled {
		t.Fatal("up must not run after a failed install")
	}
}

// REVIEW-BACK 1: exit 1223 = the user clicked No on the UAC prompt.
func TestEnsureTailscaleUserDeclinedUAC(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc.", msiExitCode: 1223}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "admin rights") {
		t.Fatalf("expected the 'needs admin rights' message, got %v", err)
	}
	if f.upCalled {
		t.Fatal("up must not run after a declined UAC")
	}
}

func TestEnsureTailscaleUACCancelError(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc.",
		msiErr: fmt.Errorf("Start-Process: The operation was canceled by the user")}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "admin rights") {
		t.Fatalf("a UAC cancel must read as 'needs admin rights', got %v", err)
	}
}

// REVIEW-BACK 2 pins: a tampered MSI (HashMismatch) refuses even with the right
// subject; a Valid signature from the WRONG org refuses.
func TestEnsureTailscaleRefusesHashMismatch(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin",
		signerStatus: "HashMismatch", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err == nil {
		t.Fatal("a HashMismatch MSI was accepted")
	}
	if f.msiexecCalled {
		t.Fatal("msiexec must NOT run on a HashMismatch MSI")
	}
}

func TestEnsureTailscaleRefusesWrongOrg(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin",
		signerStatus: "Valid", signerSubject: "CN=Tailscale Inc., O=Some Other Corp."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err == nil {
		t.Fatal("a Valid signature from the wrong org was accepted")
	}
	if f.msiexecCalled {
		t.Fatal("msiexec must NOT run on a wrong-org MSI")
	}
}

// TestEnsureTailscaleRefusesSuffixOrg — S9: the O= value is matched EXACTLY. A
// cert whose org is "Tailscale Inc. Extra" (or whose CN merely contains
// "tailscale") refuses. MUTANT: substring match (Contains "O=Tailscale Inc.") →
// this test RED.
func TestEnsureTailscaleRefusesSuffixOrg(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin",
		signerStatus: "Valid", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc. Extra"}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err == nil {
		t.Fatal("an org that only PREFIXES 'Tailscale Inc.' was accepted")
	}
	if f.msiexecCalled {
		t.Fatal("msiexec must NOT run on a suffix-org MSI")
	}
}

func TestEnsureTailscaleRefusesOtherTailnet(t *testing.T) {
	f := &tailscaleFake{installed: true, beforeState: "Running"}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "already logged in") {
		t.Fatalf("expected 'already logged in' refusal, got %v", err)
	}
	if f.upCalled {
		t.Fatal("up must not run over another tailnet (no --force-reauth)")
	}
}

func TestTailscaleApprovalWaitThenRunning(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running", approvePolls: 2}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("approval wait then running refused: %v", err)
	}
	if f.polls < 3 {
		t.Fatalf("expected NeedsMachineAuth polls before Running, got %d", f.polls)
	}
}

func TestTailscaleApprovalTimeout(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "NeedsMachineAuth", approvePolls: 1 << 30}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err == nil {
		t.Fatal("a device stuck in NeedsMachineAuth past the deadline was accepted")
	}
}

func TestNeverPrintSecrets(t *testing.T) {
	key := "tskey-SECRETKEY123"
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running", upErr: fmt.Errorf("tailscale up --authkey=%s: 401", key)}
	err := ensureTailscale(context.Background(), f.deps(t), key, "http://100.64.0.2:18081")
	if err == nil {
		t.Fatal("expected an up error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the auth key leaked into the error: %v", err)
	}
}

// TestElevatedScriptRunsUpWithArgsKey — G3/H14a: the ELEVATED step runs
// `tailscale up` with the key from the TS_AUTHKEY env (one UAC covers MSI + up),
// and the key is NEVER spliced into the script text. MUTANT: splice the key into
// the script → this test RED.
func TestElevatedScriptRunsUpWithArgsKey(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", afterUpState: "Running",
		signerSubject: "CN=Tailscale Inc., O=Tailscale Inc."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-SECRET123", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	if !strings.Contains(f.installCmd, "up --unattended") || !strings.Contains(f.installCmd, "$env:TS_AUTHKEY") {
		t.Fatalf("the elevated step must run `up` with the key from TS_AUTHKEY: %q", f.installCmd)
	}
	if strings.Contains(f.installCmd, "tskey-SECRET123") {
		t.Fatalf("the key must NOT be spliced into the script text: %q", f.installCmd)
	}
}

// TestTailscaleStoppedTurnsOnNoAuthKey — H8: the "Stopped" state is its own
// case: it turns Tailscale back on with NO auth key, then verifies identity.
func TestTailscaleStoppedTurnsOnNoAuthKey(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Stopped", afterUpState: "Running"}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if err == nil {
		t.Fatal("expected the identity check to refuse (the fake has no peers)")
	}
	if !f.upCalled {
		t.Fatal("the Stopped state must run `tailscale up`")
	}
	for _, a := range f.upArgs {
		if strings.HasPrefix(a, "--authkey=") {
			t.Fatalf("the Stopped state must NOT pass an auth key: %v", f.upArgs)
		}
	}
}

// TestApprovalTimeoutDedicatedMessage — G5: the approval timeout is the
// dedicated message (ask the owner, run again with the SAME code), never the
// generic "nothing was changed / send a photo" framing.
func TestApprovalTimeoutDedicatedMessage(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "NeedsMachineAuth", approvePolls: 1 << 30}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if !errors.Is(err, errApprovalTimeout) {
		t.Fatalf("expected errApprovalTimeout, got %v", err)
	}
	msg := userFacing(err)
	if strings.Contains(msg, "Send a photo") || strings.Contains(msg, "Nothing was changed") {
		t.Fatalf("the approval message must not carry the generic framing: %q", msg)
	}
	if !strings.Contains(msg, "SAME code") {
		t.Fatalf("missing the SAME code instruction: %q", msg)
	}
}

func TestPauseOnErrorWaitsForEnter(t *testing.T) {
	lines := []string{}
	d := deps{
		print:    func(string) {},
		readLine: func(p string) (string, error) { lines = append(lines, p); return "", nil },
	}
	pauseOnError(d, errors.New("boom"))
	if len(lines) != 1 {
		t.Fatalf("pauseOnError must wait once for Enter, got %d reads", len(lines))
	}
}

func TestRedactSecrets(t *testing.T) {
	got := redactSecrets("auth=tskey-abc connect=deadbeef", "tskey-abc", "deadbeef")
	if strings.Contains(got, "tskey-abc") || strings.Contains(got, "deadbeef") {
		t.Fatalf("secrets not redacted: %q", got)
	}
}

// statusWithPeer builds a `tailscale status --json` body where the friend bot
// appears as a peer at peerIP (and the local node is a different 100.x IP).
func statusWithPeer(peerIP string) string {
	return `{"BackendState":"Running","Self":{"TailscaleIPs":["100.64.0.5"],"DNSName":"friend.example.ts.net.","HostName":"friend-pc"},"Peer":{"` +
		peerIP + `":{"TailscaleIPs":["` + peerIP + `"],"DNSName":"vl-bot.example.ts.net.","HostName":"vl-bot"}}}`
}

// TestTailscaleConnectSkipsUpWhenOnOurTailnet — S5: after a partial failure the
// PC is Running on OUR tailnet with the auth key spent; a re-run must skip
// `tailscale up` and continue when the bot host is among the status peers (an
// identity proof, not an HTTP fetch). MUTANT: always refuse non-NeedsLogin → RED.
func TestTailscaleConnectSkipsUpWhenOnOurTailnet(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer("100.64.0.2")}
	d := f.deps(t)
	ic := goodCode()
	base := ic.BaseURL()

	if err := ensureTailscale(context.Background(), d, "tskey-secret", base); err != nil {
		t.Fatalf("our-tailnet run refused: %v", err)
	}
	if f.upCalled {
		t.Fatal("tailscale up must NOT run when already on our tailnet (auth key is spent)")
	}
}

// TestTailscaleConnectHostNameMatchesMagicDNS — S5: the install code may carry a
// MagicDNS host name instead of an IP; it matches a peer's DNSName.
func TestTailscaleConnectHostNameMatchesMagicDNS(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer("100.64.0.2")}
	d := f.deps(t)
	ic := goodCode()
	ic.Host = "vl-bot.example.ts.net"
	base := ic.BaseURL()

	if err := ensureTailscale(context.Background(), d, "tskey-secret", base); err != nil {
		t.Fatalf("MagicDNS host must match a peer DNSName: %v", err)
	}
	if f.upCalled {
		t.Fatal("tailscale up must NOT run when the MagicDNS host matches a peer")
	}
}

// TestTailscaleConnectRefusesOtherTailnetPeer — S5: Running on a DIFFERENT
// tailnet (the bot host is NOT among the peers) must still refuse.
func TestTailscaleConnectRefusesOtherTailnetPeer(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running"}
	d := f.deps(t)
	err := ensureTailscale(context.Background(), d, "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "already logged in") {
		t.Fatalf("expected 'already logged in' refusal, got %v", err)
	}
	if f.upCalled {
		t.Fatal("up must NOT run over another tailnet (no --force-reauth)")
	}
}

// TestTailscaleConnectTransientStatusFailureNotWrongNetwork — S5: a TRANSIENT
// `tailscale status` failure must NOT read as "another network". MUTANT: return
// the "already logged in" error on any status error → RED.
func TestTailscaleConnectTransientStatusFailureNotWrongNetwork(t *testing.T) {
	shortWaits(t)
	// The peer-lookup status call is #3. It fails transiently for BOTH the bare
	// name and the fixed-path fallback (calls 3-4); waitTailscaleApproved's poll
	// (call 5) succeeds.
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusErrStart: 3, statusErrCount: 2}
	d := f.deps(t)
	ic := goodCode()
	base := ic.BaseURL()

	if err := ensureTailscale(context.Background(), d, "tskey-secret", base); err != nil {
		t.Fatalf("a transient status failure must not read as another network: %v", err)
	}
	if f.upCalled {
		t.Fatal("up must NOT run on a transient status failure")
	}
}

// TestTailscaleUpTimeoutThenApproved — W5: `tailscale up` hangs (daemon still
// applying the key), its short ctx times out, and that is NOT a failure: the
// flow proceeds to waitTailscaleApproved and finishes once Running.
func TestTailscaleUpTimeoutThenApproved(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running", upHangs: true}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("an up timeout must not fail the join: %v", err)
	}
	if !f.upCalled {
		t.Fatal("tailscale up must have been attempted")
	}
}

// TestTailscaleUpPrefsWriteAccessDenied — G3: a standard user's `up` denial is
// NOT answered with "run as administrator" (that switches profiles); it is the
// redacted raw error, and the auth key never leaks.
func TestTailscaleUpPrefsWriteAccessDenied(t *testing.T) {
	shortWaits(t)
	key := "tskey-SECRET123"
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin",
		upErr: fmt.Errorf("Access denied: tailscaled prefs write access denied")}
	err := ensureTailscale(context.Background(), f.deps(t), key, "http://100.64.0.2:18081")
	if err == nil {
		t.Fatal("a denied `up` must surface an error")
	}
	if strings.Contains(err.Error(), "run VL-Setup as administrator") {
		t.Fatalf("G3: must NOT tell a standard user to run as administrator: %v", err)
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the auth key leaked into the error: %v", err)
	}
}

// TestWaitTailscaleApprovedPrintsAuthURL — W5: while waiting, the owner's
// approval URL (from the status AuthURL) is printed exactly once, and the auth
// key is never printed.
func TestWaitTailscaleApprovedPrintsAuthURL(t *testing.T) {
	shortWaits(t)
	key := "tskey-SECRET123"
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running",
		approvePolls: 1, authURL: "https://login.tailscale.com/a/abc123"}
	if err := ensureTailscale(context.Background(), f.deps(t), key, "http://100.64.0.2:18081"); err != nil {
		t.Fatalf("join refused: %v", err)
	}
	joined := strings.Join(f.prints, "\n")
	if !strings.Contains(joined, "https://login.tailscale.com/a/abc123") {
		t.Fatalf("the approval URL was not printed; prints: %q", joined)
	}
	if strings.Contains(joined, key) {
		t.Fatalf("the auth key leaked into a print: %q", joined)
	}
}

// TestEnsureTailscaleMsiExitCodes — A9: every msiexec exit code maps to plain
// words for a non-technical user, and none says "run as administrator".
func TestEnsureTailscaleMsiExitCodes(t *testing.T) {
	cases := []struct {
		code int
		want string // "" = success
	}{
		{0, ""},
		{3010, ""},
		{1223, "Tailscale needs admin rights — click Yes in the Windows permission prompt, then run VL-Setup again"},
		{1618, "Windows is busy installing something else. Wait a minute, then run VL-Setup again."},
		{1602, "The Tailscale install was cancelled. Run VL-Setup again and click Yes."},
		{1603, "Tailscale could not be installed (error 1603). Restart the PC, then run VL-Setup again. If it happens again, send a photo to the owner."},
		{1, "Tailscale install failed (exit 1)"},
		{1638, "Tailscale install failed (exit 1638)"},
	}
	for _, c := range cases {
		err := tailscaleInstallExitError(c.code)
		if c.want == "" {
			if err != nil {
				t.Errorf("code %d: expected ok, got %v", c.code, err)
			}
			continue
		}
		if err == nil || err.Error() != c.want {
			t.Errorf("code %d: got %v, want %q", c.code, err, c.want)
			continue
		}
		if strings.Contains(strings.ToLower(err.Error()), "run as administrator") {
			t.Errorf("code %d: must not tell the user to run as administrator", c.code)
		}
	}
	// And through the real flow: 1618 surfaces its message and skips `up`.
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signerSubject: "CN=Tailscale Inc., O=Tailscale Inc.", msiExitCode: 1618}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "busy installing") {
		t.Fatalf("1618 must read as busy, got %v", err)
	}
	if f.upCalled {
		t.Fatal("up must not run after a failed install")
	}
}
