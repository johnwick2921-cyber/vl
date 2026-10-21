package main

import (
	"context"
	"io/fs"
)

// osdeps.go — the OS side effects the install/update flow needs. Everything is
// a function field so the pure flow is unit-testable on Linux with fakes; the
// real Windows implementation lives in run_windows.go (GOOS=windows). The flow
// logic NEVER calls os/exec, os.WriteFile or the network directly — only these
// fields — so a test can prove ordering (verify-before-write) and refusal.

type deps struct {
	// exec runs a program and returns its combined output.
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
	// fetch GETs a URL over HTTP(S) and returns the body.
	fetch func(ctx context.Context, url string) ([]byte, error)
	// homeDir returns the Windows user profile dir (e.g. C:\Users\Tom).
	homeDir func() (string, error)
	// positions reports the friend bot's net position size (0 == flat); used
	// by the update flat-wait.
	positions func(ctx context.Context) (float64, error)
	// writeFile / mkdirAll / rename are thin fs wrappers. rename only MOVES
	// (the old add-on is moved aside to VL\old\, never deleted — THE ONE RULE).
	writeFile func(path string, data []byte, perm fs.FileMode) error
	mkdirAll  func(path string, perm fs.FileMode) error
	rename    func(oldpath, newpath string) error
	readFile  func(path string) ([]byte, error)
	// readLine prompts the user (install code on first run, update yes/no) and
	// returns the trimmed answer. autoApprove=true bypasses it in tests.
	readLine func(prompt string) (string, error)
	// print writes one plain-English console line (step headers, done lines,
	// the error screen). Production: fmt.Println; tests: a recorder.
	print func(line string)
	// allowedSigners is the trust anchor the flow verifies manifests against:
	// the embedded friend-distribution public key in production, a test key in
	// tests. Empty means "no trust" → every signature refuses.
	allowedSigners []byte
	// tempDir returns a writable temp dir for downloads (the Tailscale MSI).
	// Production: os.TempDir(); tests: t.TempDir().
	tempDir func() (string, error)
	// verifyMsiSigner returns the Authenticode STATUS + signer subject of an MSI,
	// or an error. Production: powershell Get-AuthenticodeSignature; tests: a
	// fake. The caller requires Status == "Valid" AND subject contains the
	// exact org "O=Tailscale Inc.".
	verifyMsiSigner func(msiPath string) (msiSignature, error)
	// openURL opens the dashboard in the user's browser: msedge --app when Edge
	// is present (resolved to a full path), else the default browser. Production:
	// powershell Start-Process; tests: a fake that records the URL.
	openURL func(ctx context.Context, url string) error
	// docsDir returns the Windows MyDocuments (Known Folder) path — NOT
	// <USERPROFILE>\Documents. OneDrive Known-Folder redirection moves the real
	// Documents off the profile, and NinjaTrader + the C# AddOn both read
	// MyDocuments, so every NT path must be under it.
	docsDir func() (string, error)
	// dirExists reports whether a directory exists. Production: os.Stat + IsDir;
	// used as the F2 exactness proof (NT8 must have created bin\Custom) before any
	// write.
	dirExists func(path string) bool
	// ntRunning reports whether NinjaTrader.exe is running (it locks the loaded
	// VL.AddOn.dll — never write a locked DLL).
	ntRunning func(ctx context.Context) (bool, error)
	// localAppDataDir returns %LOCALAPPDATA% (for setup.log).
	localAppDataDir func() string
	// selfExe returns the running executable's own path (os.Executable). The
	// installer copies itself to %LOCALAPPDATA%\VL\vl-friend.exe so the desktop
	// icon can launch --open without the original installer.
	selfExe func() (string, error)
	// messageBox shows a Windows Yes/No dialog (no console) and reports whether
	// the user chose Yes. Production: PowerShell MessageBox; tests: a fake.
	messageBox func(text string) bool
}

// msiSignature is what verifyMsiSigner reports: the Authenticode status and the
// signer certificate subject. A tampered MSI keeps its certificate but its
// Status becomes HashMismatch — so Status, not the subject, is the tamper gate.
type msiSignature struct {
	Status  string // "Valid", "HashMismatch", "NotSigned", "UnknownError", …
	Subject string // signer certificate subject, e.g. "CN=Tailscale Inc., O=Tailscale Inc., …"
}

// appVersion / appBuild are the friend app's own version, embedded at build
// time. appVersion is overridden by -ldflags "-X main.appVersion=…" in the
// build script; appBuild stays a compile-time constant.
var (
	appVersion = "1.0.0"
	appBuild   = "1"
)
