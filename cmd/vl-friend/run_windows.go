//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

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
			// P1: PowerShell 5.1 rejects a positional arg after -EncodedCommand,
			// so a call that appends a value must move it into VL_ARG0 env. The
			// script reads $env:VL_ARG0. P2: STDOUT only is returned.
			if i := indexOf(args, "-EncodedCommand"); i >= 0 && i+2 < len(args) {
				val := args[i+2]
				trimmed := append([]string{}, args[:i+2]...)
				trimmed = append(trimmed, args[i+3:]...)
				cmd := exec.CommandContext(ctx, name, trimmed...)
				cmd.Env = append(os.Environ(), "VL_ARG0="+val)
				noWindow(cmd)
				return cmdOutputStdout(cmd)
			}
			cmd := exec.CommandContext(ctx, name, args...)
			noWindow(cmd)
			return cmdOutputStdout(cmd)
		},
		fetch: func(ctx context.Context, url string) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			// G12: a 30s timeout on friend-bot calls — a wedged network must not
			// hang the whole 30-min run on the manifest, identity, or MSI fetch.
			resp, err := friendHTTPClient.Do(req)
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
		// S17: a TRUE append — O_APPEND, never read-modify-write — so
		// logSetupError's error history can never be clobbered by a concurrent
		// line (the old read+write raced and dropped lines).
		appendFile: func(path string, data []byte, perm fs.FileMode) error {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(data)
			return err
		},
		mkdirAll: os.MkdirAll,
		rename:   os.Rename,
		readFile: os.ReadFile,
		readLine: func(prompt string) (string, error) {
			// G7: readLine is ALWAYS echo-on — it serves y/N, the "Type N" gate,
			// and pauses. The install code goes through readSecret.
			fmt.Print(prompt)
			r := bufio.NewReader(os.Stdin)
			s, err := r.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			trimmed := strings.TrimSpace(s)
			// S20: an empty read at EOF must return io.EOF, not ("", nil) —
			// otherwise a prompt loop spins forever on an exhausted stdin.
			if err == io.EOF && trimmed == "" {
				return "", io.EOF
			}
			return trimmed, nil
		},
		readSecret: func(prompt string) (string, error) {
			// G7: the install code is read echo-OFF so the pasted one-time code
			// never appears on the console (and no later error screen can show
			// it). Non-terminal stdin falls back to a normal read (piped input is
			// not echoed anyway).
			fmt.Print(prompt)
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
			r := bufio.NewReader(os.Stdin)
			s, err := r.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			return strings.TrimSpace(s), nil
		},
		// H14b: the print seam applies the CENTRAL redactor — the tskey regex
		// plus the connect key read from connect.json at run() start — so a
		// secret can never reach the console even from a path that forgot to
		// redact.
		print:          func(line string) { fmt.Println(redactCentral(line)) },
		allowedSigners: []byte(friendAllowedSigners),
		tempDir:        func() (string, error) { return os.TempDir(), nil },
		verifyMsiSigner: func(msiPath string) (msiSignature, error) {
			// P1: the path rides VL_ARG0 (PowerShell 5.1 rejects args after
			// -EncodedCommand); P2: only the VL_RESULT line is parsed.
			script := "$s = Get-AuthenticodeSignature -FilePath $env:VL_ARG0; 'VL_RESULT=' + $s.Status.ToString() + '|' + $s.SignerCertificate.Subject"
			out, err := psRun(script, msiPath)
			if err != nil {
				return msiSignature{}, fmt.Errorf("Get-AuthenticodeSignature: %w", err)
			}
			line, err := psResultLine(out)
			if err != nil {
				return msiSignature{}, fmt.Errorf("Get-AuthenticodeSignature: %w", err)
			}
			status, subject, ok := strings.Cut(line, "|")
			if !ok {
				return msiSignature{}, fmt.Errorf("Get-AuthenticodeSignature: unexpected output %q", line)
			}
			return msiSignature{Status: strings.TrimSpace(status), Subject: strings.TrimSpace(subject)}, nil
		},
		openURL: func(ctx context.Context, url string) error {
			// P1: the URL rides VL_ARG0; openScript reads $env:VL_ARG0.
			_, err := psRun(openScript(), url)
			return err
		},
		docsDir: windowsDocumentsDir,
		dirExists: func(path string) bool {
			st, err := os.Stat(path)
			return err == nil && st.IsDir()
		},
		ntRunning: func(ctx context.Context) (bool, error) {
			script := "if (Get-Process NinjaTrader -ErrorAction SilentlyContinue) { 'VL_RESULT=yes' } else { 'VL_RESULT=no' }"
			out, err := psRun(script, "")
			if err != nil {
				return false, err
			}
			line, err := psResultLine(out)
			if err != nil {
				return false, err
			}
			return line == "yes", nil
		},
		localAppDataDir: func() string { return os.Getenv("LOCALAPPDATA") },
		selfExe:         os.Executable,
		messageBox: func(text string) bool {
			// A5: native windows.MessageBox (MB_YESNO|MB_ICONQUESTION|MB_TOPMOST|
			// MB_SETFOREGROUND), no PowerShell child per box. H14b: the text is
			// centrally redacted. H15: on failure fall back to console + setup.log
			// so an error is never invisible.
			yes, err := nativeMessageBox(redactCentral(text), "VL", windows.MB_YESNO|windows.MB_ICONQUESTION|windows.MB_TOPMOST|windows.MB_SETFOREGROUND)
			if err != nil {
				msgBoxFallback(text)
				return false
			}
			return yes
		},
		errorBox: func(text string) {
			// A5: OK + Error icon, topmost/foreground. No Yes/No — dismiss only.
			if _, err := nativeMessageBox(redactCentral(text), "VL", windows.MB_OK|windows.MB_ICONERROR|windows.MB_TOPMOST|windows.MB_SETFOREGROUND); err != nil {
				msgBoxFallback(text)
			}
		},
		infoBox: func(text string) {
			// A5: OK + Information icon, topmost/foreground. No Yes/No.
			if _, err := nativeMessageBox(redactCentral(text), "VL", windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_TOPMOST|windows.MB_SETFOREGROUND); err != nil {
				msgBoxFallback(text)
			}
		},
		// G2: a named mutex makes --open single-instance. The handle is held for
		// the process lifetime (a named mutex is auto-released on exit); the
		// second icon click returns false and only opens the dashboard.
		singleInstance: func() bool {
			h, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(`Local\VL-friend`))
			if err != nil {
				return true // fail open
			}
			if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
				windows.CloseHandle(h)
				return false
			}
			singleInstanceHandle = h
			return true
		},
	}
}

// singleInstanceHandle holds the --open mutex for the process lifetime (G2).
var singleInstanceHandle windows.Handle

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

// psPreamble (P2a) heads EVERY PowerShell script this file runs: it silences
// the CLIXML progress noise, fails fast on a script error, and forces UTF-8
// output with no BOM (so a non-ASCII signer subject survives the parse).
const psPreamble = "$ProgressPreference='SilentlyContinue';$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);"

// friendHTTPClient is the 30s-timeout client for friend-bot calls (G12): the
// manifest, the identity check, and the Tailscale MSI download must not hang
// the whole 30-minute run on a wedged network.
var friendHTTPClient = &http.Client{Timeout: 30 * time.Second}

// cmdOutputStdout runs cmd and returns STDOUT ONLY; stderr is folded into the
// error text (P2: PowerShell writes CLIXML progress noise to stderr, which must
// never corrupt an exit-code or marker parse).
func cmdOutputStdout(cmd *exec.Cmd) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// psRun runs a PowerShell script with the controlled value passed via the
// VL_ARG0 environment variable (P1: PowerShell 5.1 REJECTS a positional arg
// after -EncodedCommand — "Cannot process command because a command is already
// specified"), and returns STDOUT only.
func psRun(script, arg string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncodedCommand(psPreamble+script))
	cmd.Env = append(os.Environ(), "VL_ARG0="+arg)
	noWindow(cmd)
	return cmdOutputStdout(cmd)
}

// psResultLine extracts the single VL_RESULT=<value> line from stdout (P2c),
// ignoring every other line a PowerShell run may emit.
func psResultLine(stdout []byte) (string, error) {
	for _, ln := range strings.Split(string(stdout), "\n") {
		ln = strings.TrimSpace(ln)
		if v, ok := strings.CutPrefix(ln, "VL_RESULT="); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("no VL_RESULT line in output")
}

// idYes is the Win32 MessageBoxW return for "Yes" (IDYES).
const idYes = 6

// nativeMessageBox shows a native Windows message box (A5) — no PowerShell child
// per box. flags selects buttons/icon/topmost. It reports whether the user chose
// Yes (only meaningful for MB_YESNO).
func nativeMessageBox(text, caption string, flags uint32) (bool, error) {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return false, err
	}
	c, err := windows.UTF16PtrFromString(caption)
	if err != nil {
		return false, err
	}
	ret, err := windows.MessageBox(0, t, c, flags)
	if err != nil {
		return false, err
	}
	return ret == idYes, nil
}

// msgBoxFallback is the H15 fallback when a native message box fails: an error
// must never be invisible — print to the console AND append to setup.log.
func msgBoxFallback(text string) {
	fmt.Println(text)
	if dir := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); dir != "" {
		p := filepath.Join(dir, "VL", "setup.log")
		if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
			if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				_, _ = f.Write([]byte(time.Now().Format(time.RFC3339) + " " + text + "\n"))
				_ = f.Close()
			}
		}
	}
}

// singleInstance is the G2/H18c guard: a named mutex (Local\VL-friend) so two
// icon clicks never race placeAddon. It reports whether THIS process owns the
// mutex (true) or another instance already holds it (false — that instance just
// opens the dashboard). It fails OPEN on any mutex error (the guard is advisory,
// never a blocker).
func singleInstance() bool {
	name, err := windows.UTF16PtrFromString(`Local\VL-friend`)
	if err != nil {
		return true
	}
	if h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name); err == nil {
		_ = windows.CloseHandle(h)
		return false // another instance already holds it
	}
	_, err = windows.CreateMutex(nil, false, name)
	return err == nil
}

// indexOf returns the first index of s in a, or -1.
func indexOf(a []string, s string) int {
	for i, v := range a {
		if v == s {
			return i
		}
	}
	return -1
}

// noWindow marks a child process to never create a console window: HideWindow
// sets the startup show state to SW_HIDE and CreationFlags adds
// CREATE_NO_WINDOW. W7: EVERY child this file starts (powershell.exe, and the
// generic exec seam used for tailscale and the elevated install) must pass
// through it, so the friend app never flashes a console while it shells out.
func noWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
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

// centralSecrets holds the connect key read from connect.json at run() start
// (H14b), so EVERY output surface — print, setup.log, pause, message box —
// redacts it (a backstop on top of the tskey regex).
var centralSecrets struct {
	mu   sync.Mutex
	vals []string
}

func registerCentralSecret(sec string) {
	if sec == "" {
		return
	}
	centralSecrets.mu.Lock()
	centralSecrets.vals = append(centralSecrets.vals, sec)
	centralSecrets.mu.Unlock()
}

// redactCentral applies the registered secrets then the tskey regex backstop.
func redactCentral(s string) string {
	centralSecrets.mu.Lock()
	vals := append([]string{}, centralSecrets.vals...)
	centralSecrets.mu.Unlock()
	for _, sec := range vals {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return redactTSKeys(s)
}

// validateLocalAppData enforces H18b: LOCALAPPDATA must be set and absolute —
// otherwise setup.log and the exe copy would land on a relative path (cwd).
func validateLocalAppData() error {
	dir := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if dir == "" {
		return fmt.Errorf("LOCALAPPDATA is not set (Windows should always set it)")
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("LOCALAPPDATA is not an absolute path: %q", dir)
	}
	return nil
}

// run is the app entry: first run = install; every later start = update check.
// On ANY error it prints the (redacted) reason, writes setup.log, and waits for
// Enter so the double-clicked console window does not close (P0 b).
func run(args []string) error {
	// H18b: refuse a missing/relative LOCALAPPDATA before anything writes.
	if err := validateLocalAppData(); err != nil {
		fmt.Println("Something went wrong: " + err.Error() + ". Nothing was changed. Send a photo of this window to the owner.")
		fmt.Println("Press Enter to close")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return err
	}
	d := realDeps()
	// H14b: register the connect key with the CENTRAL redactor before any
	// output is written (best-effort — a missing connect.json is first run).
	if docs, err := d.docsDir(); err == nil {
		if cfg, err := readConnectConfig(d, docs); err == nil {
			registerCentralSecret(cfg.ConnectKey)
		}
	}
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
			pauseOnError(d, err)
			return err
		}
		return nil
	}

	// --new-code (S6): on an installed PC, apply a fresh install code (the
	// owner may re-issue one). The console flow lives in flow.go.
	if len(args) > 0 && args[0] == "--new-code" {
		if err := runNewCode(ctx, d); err != nil {
			logSetupError(d, err.Error())
			pauseOnError(d, err)
			return err
		}
		return nil
	}

	if err := runInner(ctx, d); err != nil {
		logSetupError(d, err.Error())
		pauseOnError(d, err)
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

	// S19: an existing-but-UNREADABLE connect.json is an INCOMPLETE install —
	// not first-run and not a healthy install. Offer a new install code.
	if _, err := readConnectConfig(d, docs); err != nil {
		return runIncomplete(ctx, d, docs)
	}

	// Installed: announce, update check, ensure icons, print + open the
	// dashboard. No install-code prompt (CTO 23:47 CT).
	return runInstalled(ctx, d, docs)
}
