package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// tailscale_test.go — the first-run Tailscale flow (P0 fix): MSI download +
// Authenticode verify, PATH fallback, other-tailnet refusal, device-approval
// wait and timeout, and the never-print-secrets rule. All over fakes; no real
// Windows action.

type tailscaleFake struct {
	installed      bool   // `tailscale status` answers? flips true after msiexec
	beforeState    string // BackendState before "up"
	afterUpState   string // BackendState after "up" (once approvePolls exhausted)
	approvePolls   int    // # of post-up status polls returning NeedsMachineAuth
	upErr          error
	msiexecErr     error
	signer         string
	signerErr      error
	fetchErr       error
	upCalled       bool
	msiexecCalled  bool
	downloadCalled bool
	signCalled     bool
	polls          int
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
	case name == "msiexec":
		f.msiexecCalled = true
		f.installed = true
		if f.msiexecErr != nil {
			return nil, f.msiexecErr
		}
		return nil, nil
	}
	return nil, nil
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
		verifyMsiSigner: func(string) (string, error) {
			f.signCalled = true
			if f.signerErr != nil {
				return "", f.signerErr
			}
			return f.signer, nil
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
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err != nil {
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
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", afterUpState: "Running", signer: "CN=Tailscale Inc."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err != nil {
		t.Fatalf("fresh install refused: %v", err)
	}
	if !f.downloadCalled || !f.signCalled || !f.msiexecCalled || !f.upCalled {
		t.Fatalf("expected download+verify+msiexec+up, got download=%v sign=%v msiexec=%v up=%v",
			f.downloadCalled, f.signCalled, f.msiexecCalled, f.upCalled)
	}
}

func TestEnsureTailscaleInstallFails(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signer: "CN=Tailscale Inc.", msiexecErr: fmt.Errorf("msi exit 1603")}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err == nil {
		t.Fatal("a failing MSI install was accepted")
	}
	if f.upCalled {
		t.Fatal("up must not run after a failed install")
	}
}

func TestEnsureTailscaleRefusesBadSigner(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: false, beforeState: "NeedsLogin", signer: "CN=Evil Corp."}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err == nil {
		t.Fatal("an MSI with a non-Tailscale Authenticode signer was accepted")
	}
	if f.msiexecCalled {
		t.Fatal("msiexec must NOT run on a bad-signer MSI")
	}
}

func TestEnsureTailscaleRefusesOtherTailnet(t *testing.T) {
	f := &tailscaleFake{installed: true, beforeState: "Running"}
	err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret")
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
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err != nil {
		t.Fatalf("approval wait then running refused: %v", err)
	}
	if f.polls < 3 {
		t.Fatalf("expected NeedsMachineAuth polls before Running, got %d", f.polls)
	}
}

func TestTailscaleApprovalTimeout(t *testing.T) {
	shortWaits(t)
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "NeedsMachineAuth", approvePolls: 1 << 30}
	if err := ensureTailscale(context.Background(), f.deps(t), "tskey-secret"); err == nil {
		t.Fatal("a device stuck in NeedsMachineAuth past the deadline was accepted")
	}
}

func TestNeverPrintSecrets(t *testing.T) {
	key := "tskey-SECRETKEY123"
	f := &tailscaleFake{installed: true, beforeState: "NeedsLogin", afterUpState: "Running", upErr: fmt.Errorf("tailscale up --authkey=%s: 401", key)}
	err := ensureTailscale(context.Background(), f.deps(t), key)
	if err == nil {
		t.Fatal("expected an up error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the auth key leaked into the error: %v", err)
	}
}

func TestPauseOnErrorWaitsForEnter(t *testing.T) {
	lines := []string{}
	d := deps{readLine: func(p string) (string, error) { lines = append(lines, p); return "", nil }}
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
