//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
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
			noWindow(cmd)
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
			// S7: the install code is read with an EMPTY prompt (runFirstRun's
			// [1/6] step); the "Press Enter to close" pauses also pass "" but
			// discard the result. An empty prompt therefore means "secret": read
			// echo-OFF so the one-time code never appears on the console, and no
			// later error screen can show it — the OS terminal echo was what
			// leaked the pasted code before. The y/N prompts pass non-empty text
			// and keep normal echo.
			if prompt == "" {
				fd := int(os.Stdin.Fd())
				if term.IsTerminal(fd) {
					b, err := term.ReadPassword(fd)
					fmt.Println()
					if err != nil {
						return "", err
					}
					s := strings.TrimSpace(string(b))
					if s != "" {
						fmt.Println("Code received (hidden)")
					}
					return s, nil
				}
			}
			r := bufio.NewReader(os.Stdin)
			s, err := r.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			trimmed := strings.TrimSpace(s)
			// S20: an empty read at EOF must return io.EOF, not ("", nil) —
			// otherwise a prompt loop (e.g. the "close NinjaTrader" wait) spins
			// forever on an exhausted stdin.
			if err == io.EOF && trimmed == "" {
				return "", io.EOF
			}
			return trimmed, nil
		},
		print:          func(line string) { fmt.Println(line) },
		allowedSigners: []byte(friendAllowedSigners),
		tempDir:        func() (string, error) { return os.TempDir(), nil },
		verifyMsiSigner: func(msiPath string) (msiSignature, error) {
			// S18b: the MSI path is passed as $args[0], never spliced into
			// script text — no quote-escaping, no injection surface. W3: force
			// UTF-8 output so a non-ASCII signer subject is not corrupted by the
			// OEM codepage before we parse it.
			script := psUTF8 + "$s = Get-AuthenticodeSignature -FilePath $args[0]; $s.Status.ToString() + '|' + $s.SignerCertificate.Subject"
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(script), msiPath)
			noWindow(cmd)
			out, err := cmd.CombinedOutput()
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
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", openScript(url))
			noWindow(cmd)
			_, err := cmd.CombinedOutput()
			return err
		},
		docsDir: windowsDocumentsDir,
		dirExists: func(path string) bool {
			st, err := os.Stat(path)
			return err == nil && st.IsDir()
		},
		ntRunning: func(ctx context.Context) (bool, error) {
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psUTF8+"if (Get-Process NinjaTrader -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }")
			noWindow(cmd)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(string(out)) == "yes", nil
		},
		localAppDataDir: func() string { return os.Getenv("LOCALAPPDATA") },
		selfExe:         os.Executable,
		messageBox: func(text string) bool {
			// W9: the MessageBoxOptions.ServiceNotification flag renders the box
			// TOPMOST, so the update/error dialog is never buried behind the
			// console or other windows. Buttons stay YesNo + Question here; the
			// error-vs-update split (OK+Error for failures) is a separate seam.
			// S18b: the text is passed as $args[0], never spliced into script text.
			script := psUTF8 + "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show($args[0],'VL','YesNo','Question','Button1',[System.Windows.Forms.MessageBoxOptions]::ServiceNotification) -eq 'Yes'"
			cmd := exec.Command("powershell.exe", "-NoProfile", "-EncodedCommand", psEncodedCommand(script), text)
			noWindow(cmd)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			return strings.TrimSpace(string(out)) == "True"
		},
	}
}

// windowsDocumentsDir resolves the Windows Known Folder MyDocuments (F2: the C#
// AddOn and NinjaTrader read MyDocuments, which OneDrive redirection can move
// off <USERPROFILE>\Documents).
//
// W3: this used to shell out to PowerShell `[Environment]::GetFolderPath(...)`
// and parse its stdout — the child's output codepage is the console OEM
// codepage, so a non-ASCII path (e.g. "C:\Users\Tomás\Documents") was
// corrupted. KnownFolderPath goes straight to the SHGetKnownFolderPath API and
// returns a UTF-16-decoded Go string, so there is no codepage to lose. The
// result must be non-empty and absolute; anything else refuses.
func windowsDocumentsDir() (string, error) {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("resolve Documents: %w", err)
	}
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("resolve Documents: KnownFolderPath returned %q", p)
	}
	return p, nil
}

// psUTF8 forces PowerShell's console output to UTF-8 before any output this
// file PARSES is written (W3): without it a non-ASCII signer subject would
// come back in the OEM codepage and be corrupted before the exact-org check.
const psUTF8 = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;"

// noWindow marks a child process to never create a console window: HideWindow
// sets the startup show state to SW_HIDE and CreationFlags adds
// CREATE_NO_WINDOW. W7: EVERY child this file starts (powershell.exe, and the
// generic exec seam used for tailscale and the elevated install) must pass
// through it, so the friend app never flashes a console while it shells out.
func noWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// psEncodedCommand base64-encodes a PowerShell command as UTF-16LE — the format
// powershell.exe -EncodedCommand expects. S18b: a command sent this way, with any
// user/controlled string handed to the script as $args[0], is NEVER spliced into
// script text, so there is no quote-escaping to get wrong and no injection
// surface in the path/text.
func psEncodedCommand(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// procFreeConsole is the kernel32 FreeConsole entry point. golang.org/x/sys
// does not wrap FreeConsole, so it is resolved lazily against kernel32.dll.
var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

// hideConsole detaches the process from its console (kernel32 FreeConsole).
// It is called ONLY at the start of the --open path, so the double-clicked
// desktop icon never flashes a console window. It is a no-op on error: the
// icon still works, only the window might flash briefly.
func hideConsole() {
	procFreeConsole.Call()
}

// run is the app entry: first run = install; every later start = update check.
// On ANY error it prints the (redacted) reason, writes setup.log, and waits for
// Enter so the double-clicked console window does not close (P0 b).
func run(args []string) error {
	d := realDeps()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// --open (the desktop icon): quick silent update check (≤5s, errors
	// ignored), then open the dashboard; if an update exists, a Windows dialog
	// offers Update now / Later. No console, no install-code prompt.
	if len(args) > 0 && args[0] == "--open" {
		// m11: the icon double-click must not flash a console. FreeConsole
		// detaches this (console-subsystem) process from the console the OS
		// created at start, before any output is written. Best-effort: a
		// failure to detach is ignored (a brief flash, never a broken app).
		hideConsole()
		if err := runOpen(ctx, d); err != nil {
			logSetupError(d, err.Error())
			return err
		}
		return nil
	}

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
