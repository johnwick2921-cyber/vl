package main

import (
	"context"
	"encoding/base64"
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
	msiexecCalled  bool
	downloadCalled bool
	signCalled     bool
	polls          int
	installCmd     string
	execNames      []string
}

func (f *tailscaleFake) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	f.execNames = append(f.execNames, name)
	switch {
	case name == "tailscale" && len(args) > 0 && args[0] == "status":
		if !f.installed {
			return nil, fmt.Errorf("tailscale: executable not found")
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
		return []byte(`{"BackendState":"` + st + `"}`), nil
	case name == "tailscale" && len(args) > 0 && args[0] == "up":
		f.upCalled = true
		if f.upErr != nil {
			return nil, f.upErr
		}
		return nil, nil
	case name == tailscaleExeFallback:
		return f.exec(context.Background(), "tailscale", args...)
	case name == "powershell.exe" && len(args) >= 4 && args[2] == "-EncodedCommand":
		script := decodePSCommand(args[3])
		if !strings.Contains(script, "Start-Process msiexec") {
			return nil, nil
		}
		f.installCmd = script
		f.msiexecCalled = true
		if f.msiErr != nil {
			// Simulate the elevated script's UAC-cancel marker (W10: 1223).
			return []byte("VL_UAC_CANCELLED"), nil
		}
		f.installed = true
		return []byte(strconv.Itoa(f.msiExitCode)), nil
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
		writeFile: os.WriteFile,
		readLine:  func(string) (string, error) { return "", nil },
	}
}

func shortWaits(t *testing.T) {
	t.Helper()
	prev := []time.Duration{tailscaleReadyPoll, tailscaleApprovalPoll, tailscaleInstallWait, tailscaleApprovalWait}
	t.Cleanup(func() {
		tailscaleReadyPoll, tailscaleApprovalPoll, tailscaleInstallWait, tailscaleApprovalWait =
			prev[0], prev[1], prev[2], prev[3]
	})
	tailscaleReadyPoll = 2 * time.Millisecond
	tailscaleApprovalPoll = 2 * time.Millisecond
	tailscaleInstallWait = 100 * time.Millisecond
	tailscaleApprovalWait = 100 * time.Millisecond
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

func TestPauseOnErrorWaitsForEnter(t *testing.T) {
	lines := []string{}
	d := deps{
		print:    func(string) {},
		readLine: func(p string) (string, error) { lines = append(lines, p); return "", nil },
	}
	pauseOnError(d, "boom")
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

// TestTailscaleConnectSkipsUpWhenOnOurTailnet — F3: after a partial failure the
// PC is Running on OUR tailnet with the auth key spent; a re-run must skip
// `tailscale up` and continue when the friend bot's manifest verifies. MUTANT:
// always refuse non-NeedsLogin → RED.
func TestTailscaleConnectSkipsUpWhenOnOurTailnet(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running"}
	d := f.deps(t)
	ic := goodCode()
	base := ic.BaseURL()
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := ensureTailscale(context.Background(), d, "tskey-secret", base); err != nil {
		t.Fatalf("our-tailnet run refused: %v", err)
	}
	if f.upCalled {
		t.Fatal("tailscale up must NOT run when already on our tailnet (auth key is spent)")
	}
}

// TestTailscaleConnectRefusesOtherTailnetManifest — F3: Running on a DIFFERENT
// tailnet (the friend bot's manifest is NOT reachable) must still refuse.
func TestTailscaleConnectRefusesOtherTailnetManifest(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running"}
	d := f.deps(t)
	// fetch serves the MSI bytes (no manifest) → verification fails → refuse.
	err := ensureTailscale(context.Background(), d, "tskey-secret", "http://100.64.0.2:18081")
	if err == nil || !strings.Contains(err.Error(), "already logged in") {
		t.Fatalf("expected 'already logged in' refusal, got %v", err)
	}
}
