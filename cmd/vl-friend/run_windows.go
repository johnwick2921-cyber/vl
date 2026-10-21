//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// run_windows.go — the real Windows wiring of the friend app. The install /
// update decisions live in flow.go (pure, tested); this file only supplies the
// OS deps. It is compiled only with GOOS=windows; the Linux build uses
// run_other.go so `go build ./...` and the test suite stay green on the box.

// realDeps wires the flow's seams to the real Windows environment.
func realDeps() deps {
	return deps{
		exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		},
		fetch: func(ctx context.Context, url string) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		},
		homeDir: os.UserHomeDir,
		positions: func(ctx context.Context) (float64, error) {
			// Fail-closed default; run() replaces this with the real probe
			// against the friend's /api/friend/flat once the base URL is known.
			return 0, fmt.Errorf("flat probe not wired")
		},
		writeFile: os.WriteFile,
		mkdirAll:  os.MkdirAll,
		rename:    os.Rename,
		readFile:  os.ReadFile,
		readLine: func(prompt string) (string, error) {
			fmt.Print(prompt)
			r := bufio.NewReader(os.Stdin)
			s, err := r.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			return strings.TrimSpace(s), nil
		},
		print:          func(line string) { fmt.Println(line) },
		allowedSigners: []byte(friendAllowedSigners),
		tempDir:        func() (string, error) { return os.TempDir(), nil },
		verifyMsiSigner: func(msiPath string) (msiSignature, error) {
			esc := strings.ReplaceAll(msiPath, "'", "''")
			ps := "$s = Get-AuthenticodeSignature -FilePath '" + esc + "'; $s.Status.ToString() + '|' + $s.SignerCertificate.Subject"
			out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
			if err != nil {
				return msiSignature{}, fmt.Errorf("Get-AuthenticodeSignature: %w: %s", err, strings.TrimSpace(string(out)))
			}
			line := strings.TrimSpace(string(out))
			status, subject, ok := strings.Cut(line, "|")
			if !ok {
				return msiSignature{}, fmt.Errorf("Get-AuthenticodeSignature: unexpected output %q", line)
			}
			return msiSignature{Status: strings.TrimSpace(status), Subject: strings.TrimSpace(subject)}, nil
		},
		openURL: func(ctx context.Context, url string) error {
			_, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", openScript(url)).CombinedOutput()
			return err
		},
		docsDir: windowsDocumentsDir,
		dirExists: func(path string) bool {
			st, err := os.Stat(path)
			return err == nil && st.IsDir()
		},
		ntRunning: func(ctx context.Context) (bool, error) {
			out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "if (Get-Process NinjaTrader -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }").CombinedOutput()
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(string(out)) == "yes", nil
		},
		localAppDataDir: func() string { return os.Getenv("LOCALAPPDATA") },
	}
}

// windowsDocumentsDir resolves the Windows Known Folder MyDocuments (F2: the C#
// AddOn and NinjaTrader read MyDocuments, which OneDrive redirection can move
// off <USERPROFILE>\Documents).
func windowsDocumentsDir() (string, error) {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "[Environment]::GetFolderPath('MyDocuments')").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Documents: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// run is the app entry: first run = install; every later start = update check.
// On ANY error it prints the (redacted) reason, writes setup.log, and waits for
// Enter so the double-clicked console window does not close (P0 b).
func run(args []string) error {
	d := realDeps()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// --repair-icon re-creates the desktop + Start Menu icons on an
	// already-installed PC (B8 live bug: OneDrive desktop redirection hid the
	// icon). The normal start (runInner) also re-creates a missing icon.
	if len(args) > 0 && args[0] == "--repair-icon" {
		if err := repairIcon(ctx, d); err != nil {
			logSetupError(d, err.Error())
			pauseOnError(d, err.Error())
			return err
		}
		return nil
	}

	if err := runInner(ctx, d); err != nil {
		logSetupError(d, err.Error())
		pauseOnError(d, err.Error())
		return err
	}
	return nil
}

// runInner is the actual flow; its errors are already secret-redacted.
func runInner(ctx context.Context, d deps) error {
	home, err := d.homeDir()
	if err != nil {
		return err
	}
	docs, err := d.docsDir()
	if err != nil {
		return err
	}
	// F2 MIGRATION: an install under <USERPROFILE>\Documents must move into
	// MyDocuments when Known-Folder redirection put it elsewhere (never delete).
	if err := migrateDocsIfNeeded(d, home, docs); err != nil {
		return err
	}

	// First run: no connect.json yet → the six-step setup console flow.
	if _, err := d.readFile(connectJSONPath(docs)); err != nil {
		return runFirstRun(ctx, d)
	}

	// Installed: announce, update check, ensure icons, print + open the
	// dashboard. No install-code prompt (CTO 23:47 CT).
	return runInstalled(ctx, d, docs)
}
