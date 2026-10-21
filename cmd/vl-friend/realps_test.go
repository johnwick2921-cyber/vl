package main

// realps_test.go — the REAL PowerShell gate (m122). Runs the app's actual
// PowerShell scripts against WSL's powershell.exe. Skipped unless VL_REAL_PS=1
// AND powershell.exe is on PATH (as in WSL). It NEVER runs msiexec, never
// creates a shortcut, never opens the browser, never shows a message box —
// only harmless scripts and a syntax parse of the rest.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realPSReady(t *testing.T) bool {
	if os.Getenv("VL_REAL_PS") != "1" {
		t.Skip("VL_REAL_PS=1 not set (run this on the WSL box: VL_REAL_PS=1 go test ./cmd/vl-friend -run RealPS -v)")
		return false
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not on PATH")
		return false
	}
	return true
}

// runPS runs a script through powershell.exe -EncodedCommand with the given env
// additions and returns STDOUT and STDERR separately (mirrors psRun, but Linux).
func runPS(t *testing.T, script string, env ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(script))
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// resultLine extracts the VL_RESULT=<value> line (P2c), ignoring noise.
func resultLine(t *testing.T, stdout string) string {
	t.Helper()
	for _, ln := range strings.Split(stdout, "\n") {
		ln = strings.TrimSpace(ln)
		if v, ok := strings.CutPrefix(ln, "VL_RESULT="); ok {
			return v
		}
	}
	t.Fatalf("no VL_RESULT line in stdout:\n%s", stdout)
	return ""
}

// TestRealPSEnvRoundTrip pins P1 on real PowerShell: the controlled value rides
// the VL_ARG0 env var (PowerShell 5.1 REJECTS a positional arg after
// -EncodedCommand), and a path with a space, an apostrophe, and non-ASCII comes
// through exactly.
func TestRealPSEnvRoundTrip(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	val := `C:\Users\O'Neil Nguyễn\folder with spaces\file.msi`
	script := psPreamble + "'VL_RESULT=' + $env:VL_ARG0"
	stdout, stderr, err := runPS(t, script, "VL_ARG0="+val)
	if err != nil {
		t.Fatalf("powershell failed: %v\nstderr: %s", err, stderr)
	}
	if got := resultLine(t, stdout); got != val {
		t.Fatalf("round trip = %q, want %q (stdout=%q stderr=%q)", got, val, stdout, stderr)
	}
}

// TestRealPSVerifyMsiSigner runs the production verifyMsiSigner script on the
// real Tailscale MSI and requires Status=Valid + O=Tailscale Inc.
func TestRealPSVerifyMsiSigner(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	dir := t.TempDir()
	msi := filepath.Join(dir, "tailscale-setup-latest-amd64.msi")
	// Download the official installer (a real signed MSI) — never run it.
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("Invoke-WebRequest -Uri 'https://pkgs.tailscale.com/stable/tailscale-setup-latest-amd64.msi' -OutFile '%s'", strings.ReplaceAll(msi, "'", "''")))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not download the MSI (%v): %s", err, out)
	}
	script := psPreamble + "$s = Get-AuthenticodeSignature -FilePath $env:VL_ARG0; 'VL_RESULT=' + $s.Status.ToString() + '|' + $s.SignerCertificate.Subject"
	stdout, stderr, err := runPS(t, script, "VL_ARG0="+msi)
	if err != nil {
		t.Fatalf("verify failed: %v\nstderr: %s", err, stderr)
	}
	line := resultLine(t, stdout)
	status, subject, ok := strings.Cut(line, "|")
	if !ok {
		t.Fatalf("unexpected verify output %q", line)
	}
	if strings.TrimSpace(status) != "Valid" {
		t.Fatalf("MSI status = %q, want Valid", status)
	}
	if !strings.Contains(subject, "O=Tailscale Inc.") {
		t.Fatalf("MSI signer = %q, want O=Tailscale Inc.", subject)
	}
}

// TestRealPSSyntaxParse parses EVERY PowerShell script the app builds (flow.go's
// shared ones) and requires 0 parser errors — so a syntax bug is caught on Linux
// before it reaches a Windows box.
func TestRealPSSyntaxParse(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	scripts := map[string]string{
		"openScript":            openScript(),
		"elevatedInstallScript": elevatedInstallScript,
	}
	for name, script := range scripts {
		// The Parser::ParseInput returns ($tokens, $errors). 0 errors = ok.
		ps := psPreamble + "$tokens=$null;$errors=$null;[System.Management.Automation.Language.Parser]::ParseInput(@'\n" + script + "\n'@, [ref]$tokens, [ref]$errors) > $null; 'VL_RESULT=' + $errors.Count"
		stdout, stderr, err := runPS(t, ps)
		if err != nil {
			t.Fatalf("%s parse run failed: %v\nstderr: %s", name, err, stderr)
		}
		if got := resultLine(t, stdout); got != "0" {
			t.Fatalf("%s has %s parser error(s)", name, got)
		}
	}
}
