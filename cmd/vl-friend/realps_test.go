package main

// realps_test.go — the REAL PowerShell gate (m122). Runs the app's actual
// PowerShell scripts against WSL's powershell.exe. Skipped unless VL_REAL_PS=1
// AND powershell.exe is on PATH (as in WSL). It NEVER runs msiexec, never
// creates a shortcut, never opens the browser, never shows a message box —
// only harmless scripts and a syntax parse of the rest.

import (
	"os"
	"os/exec"
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
//
// WSL interop note: a bare Linux env var is NOT forwarded to powershell.exe;
// WSLENV=VL_ARG0/w forwards it verbatim (the /w keeps the Windows path
// untranslated). Production psRun runs natively on Windows, where cmd.Env
// reaches the child directly and no WSLENV is needed.
func runPS(t *testing.T, script string, env ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(script))
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "WSLENV=VL_ARG0/w")
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
// -EncodedCommand), and a path with a space and an apostrophe comes through
// exactly. (Non-ASCII is exercised but asserted loosely: the WSL→Windows
// interop layer mangles it to '?', while native Windows passes it through —
// verified by the CTO on the owner box.)
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
	got := resultLine(t, stdout)
	for _, want := range []string{`C:\Users\O'Neil`, `folder with spaces`, `file.msi`} {
		if !strings.Contains(got, want) {
			t.Fatalf("round trip = %q, missing %q (full want %q)", got, want, val)
		}
	}
}

// downloadRealMsi fetches the official Tailscale installer into PowerShell's own
// %TEMP% (a Windows path it can write — a WSL temp dir is unreachable from
// powershell.exe) and returns that Windows path. It NEVER runs the MSI.
func downloadRealMsi(t *testing.T) string {
	t.Helper()
	dl := psPreamble + "$msi = Join-Path $env:TEMP ('tailscale-setup-' + [guid]::NewGuid().ToString('N') + '.msi'); Invoke-WebRequest -Uri 'https://pkgs.tailscale.com/stable/tailscale-setup-latest-amd64.msi' -OutFile $msi; 'VL_RESULT=' + $msi"
	stdout, stderr, err := runPS(t, dl)
	if err != nil {
		t.Skipf("could not download the MSI (%v): %s", err, stderr)
	}
	return resultLine(t, stdout)
}

// TestRealPSVerifyMsiSigner runs the production verifyMsiSigner script on the
// real Tailscale MSI and requires Status=Valid + O=Tailscale Inc.
func TestRealPSVerifyMsiSigner(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	msi := downloadRealMsi(t)
	script := psPreamble + verifyMsiSignerScript
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

// TestRealPSSyntaxParse parses EVERY PowerShell script the app builds and
// requires 0 parser errors — so a syntax bug is caught on Linux before it
// reaches a Windows box.
func TestRealPSSyntaxParse(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	scripts := map[string]string{
		"openScript":            openScript(),
		"elevatedInstallScript": elevatedInstallScript,
		"verifyMsiSignerScript": verifyMsiSignerScript,
		"ntRunningScript":       ntRunningScript,
		"shortcutScript":        shortcutScript(),
	}
	for name, script := range scripts {
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

// TestRealPSStatusJSONNoise pins P2 on real PowerShell: stderr CLIXML noise must
// not corrupt the stdout-only VL_RESULT extraction (the shape of the
// `tailscale status --json` parse). A script writes noise to stderr and a JSON
// VL_RESULT line to stdout; resultLine must recover the JSON exactly.
func TestRealPSStatusJSONNoise(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	json := `{"BackendState":"Running","Self":{"TailscaleIPs":["100.64.0.7"]}}`
	// [Console]::Error.WriteLine emits genuine stderr without becoming a
	// terminating error under $ErrorActionPreference='Stop' (Write-Error would).
	script := psPreamble + "[Console]::Error.WriteLine('CLIXML noise'); 'VL_RESULT=' + '" + json + "'"
	stdout, stderr, err := runPS(t, script)
	if err != nil {
		t.Fatalf("status-json run failed: %v\nstderr: %s", err, stderr)
	}
	if got := resultLine(t, stdout); got != json {
		t.Fatalf("status-json = %q, want %q (stderr noise must not corrupt stdout)", got, json)
	}
}

// TestRealPSElevatedScriptDryRun runs the elevated install script with
// Start-Process stubbed out, so the signature re-verify + the msiexec command
// are exercised WITHOUT ever running msiexec. It uses the real (validly signed)
// MSI so the script passes its own Authenticode re-verify and reaches the
// stubbed launch, which prints the command as VL_RESULT. (DS-108's item-1
// adaptation makes the script read VL_ARG0; this test is green against it.)
func TestRealPSElevatedScriptDryRun(t *testing.T) {
	if !realPSReady(t) {
		return
	}
	msi := downloadRealMsi(t)
	// Stub Start-Process: print the launch line instead of running msiexec, and
	// return an object with ExitCode like -PassThru does (CTO 10-21: the old stub
	// returned a string, which -PassThru captured into $p, so nothing printed).
	stub := "function Start-Process { param($FilePath,$ArgumentList,$Verb,[switch]$Wait,[switch]$PassThru) [Console]::Out.WriteLine('DRYRUN ' + $Verb + ' ' + $FilePath + ' ' + ($ArgumentList -join ' ')); [pscustomobject]@{ExitCode=0} }\n"
	script := psPreamble + stub + elevatedInstallScript
	stdout, stderr, err := runPS(t, script, "VL_ARG0="+msi)
	if err != nil {
		t.Fatalf("elevated dry-run failed: %v\nstderr: %s", err, stderr)
	}
	want := `DRYRUN RunAs msiexec /i "` + msi + `" /quiet /norestart`
	if !strings.Contains(stdout, want) {
		t.Fatalf("elevated dry-run must launch %q, stdout:\n%s", want, stdout)
	}
	if line := resultLine(t, stdout); line != "0" {
		t.Fatalf("elevated dry-run VL_RESULT = %q, want the msiexec exit code 0", line)
	}
	if strings.Count(stdout, "VL_RESULT=") != 1 {
		t.Fatalf("want exactly ONE VL_RESULT line, stdout:\n%s", stdout)
	}
}
