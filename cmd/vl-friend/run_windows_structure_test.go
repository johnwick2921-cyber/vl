package main

import (
	"os"
	"strings"
	"testing"
)

// m11: the desktop icon launches vl-friend.exe --open. Because the binary is a
// console-subsystem executable, the OS creates a console window at process
// start; without FreeConsole the icon would flash a console before the
// dashboard opens. This pin (source-structure — run_windows.go only compiles on
// Windows) proves the --open branch hides the console BEFORE it does anything
// else, and that hideConsole itself calls the Windows FreeConsole API. It
// asserts ORDER, not just presence, so a regression that moves hideConsole
// after the quick update check (or drops the call) fails here.
func TestRunOpenHidesConsoleBeforeAnythingElse(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	// 1. hideConsole exists and calls the kernel32 FreeConsole entry point.
	hideFn := `func hideConsole()`
	hideIdx := strings.Index(s, hideFn)
	if hideIdx < 0 {
		t.Fatalf("run_windows.go must define %s", hideFn)
	}
	if !strings.Contains(s[hideIdx:], "procFreeConsole.Call()") {
		t.Fatalf("hideConsole must call procFreeConsole.Call()")
	}
	if !strings.Contains(s, `NewProc("FreeConsole")`) {
		t.Fatalf("procFreeConsole must resolve kernel32 FreeConsole via NewProc")
	}

	// 2. In run(), the --open branch calls hideConsole() BEFORE runOpen.
	openBranch := `args[0] == "--open" {`
	openIdx := strings.Index(s, openBranch)
	if openIdx < 0 {
		t.Fatalf("run_windows.go must have the %q branch", openBranch)
	}
	branch := s[openIdx:]
	hideCall := strings.Index(branch, "hideConsole()")
	runOpenCall := strings.Index(branch, "runOpen(ctx, d)")
	if hideCall < 0 {
		t.Fatalf("the --open branch must call hideConsole()")
	}
	if runOpenCall < 0 {
		t.Fatalf("the --open branch must call runOpen(ctx, d)")
	}
	if hideCall > runOpenCall {
		t.Fatalf("hideConsole() must run BEFORE runOpen(ctx, d) in the --open branch (hide at %d, open at %d)", hideCall, runOpenCall)
	}

	// 3. The Windows import must be present so windows.FreeConsole resolves.
	if !strings.Contains(s, `"golang.org/x/sys/windows"`) {
		t.Fatalf("run_windows.go must import golang.org/x/sys/windows")
	}
}

// TestReadLineInstallCodeEchoOff pins S7: the install code is read with an
// EMPTY prompt, so the readLine seam treats an empty prompt as a secret — it
// must read through golang.org/x/term ReadPassword (terminal echo OFF) and
// confirm with "Code received (hidden)". This is source-structure because
// run_windows.go only compiles on Windows; the mutant that reverts readLine to
// a plain bufio echo read (or drops the empty-prompt gate) fails here.
// TestReadSecretEchoOffPinsG7 pins G7: the install code is read by a DEDICATED
// readSecret seam via term.ReadPassword (echo OFF), and readLine is echo-ON for
// everything else (y/N, "Type N", pauses). The old S7 empty-prompt heuristic
// (echo-off on every readLine("")) hid the "Type N" answer — that mutant (or a
// revert to the heuristic) fails here.
func TestReadSecretEchoOffPinsG7(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, `"golang.org/x/term"`) {
		t.Fatalf("run_windows.go must import golang.org/x/term for ReadPassword")
	}
	// readSecret must exist and read echo-off.
	sec := `readSecret: func(prompt string) (string, error) {`
	secIdx := strings.Index(s, sec)
	if secIdx < 0 {
		t.Fatalf("run_windows.go must define the %s seam", sec)
	}
	secBody := s[secIdx:]
	if !strings.Contains(secBody, "term.ReadPassword(fd)") {
		t.Fatalf("readSecret must call term.ReadPassword")
	}
	if !strings.Contains(secBody, "term.IsTerminal(fd)") {
		t.Fatalf("readSecret must guard with term.IsTerminal")
	}
	if !strings.Contains(secBody, `"Code received (hidden)"`) {
		t.Fatalf(`readSecret must confirm with "Code received (hidden)"`)
	}
	// readLine must be echo-ON: the ReadPassword call must NOT sit inside the
	// readLine closure (it moved to readSecret).
	line := `readLine: func(prompt string) (string, error) {`
	lineIdx := strings.Index(s, line)
	if lineIdx < 0 {
		t.Fatalf("run_windows.go must define the %s seam", line)
	}
	lineBody := s[lineIdx:secIdx]
	if strings.Contains(lineBody, "term.ReadPassword") {
		t.Fatalf("readLine must NOT call term.ReadPassword (the secret read moved to readSecret)")
	}
}

// TestWindowsDocumentsDirUsesKnownFolderPath pins W3: resolving MyDocuments must
// go through the SHGetKnownFolderPath API (windows.KnownFolderPath) — never a
// PowerShell child whose output codepage corrupts non-ASCII paths — and must
// reject an empty or non-absolute result. The mutant that re-adds the
// `[Environment]::GetFolderPath` shell-out (or drops the emptiness/absoluteness
// gate) fails here.
func TestWindowsDocumentsDirUsesKnownFolderPath(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	fn := `func windowsDocumentsDir() (string, error) {`
	idx := strings.Index(s, fn)
	if idx < 0 {
		t.Fatalf("run_windows.go must define %s", fn)
	}
	body := s[idx:]

	if !strings.Contains(body, "windows.KnownFolderPath(") {
		t.Fatalf("windowsDocumentsDir must call windows.KnownFolderPath")
	}
	if !strings.Contains(body, "windows.FOLDERID_Documents") {
		t.Fatalf("windowsDocumentsDir must request windows.FOLDERID_Documents")
	}
	if strings.Contains(body, "[Environment]::GetFolderPath") {
		t.Fatalf("windowsDocumentsDir must NOT shell out to PowerShell GetFolderPath (OEM-codepage corruption)")
	}
	if !strings.Contains(body, "filepath.IsAbs(p)") {
		t.Fatalf("windowsDocumentsDir must gate on filepath.IsAbs(p)")
	}
	if !strings.Contains(body, `p == ""`) {
		t.Fatalf("windowsDocumentsDir must reject an empty result")
	}
}

// TestPowerShellOutputForcedUTF8 pins the W3 remainder: any PowerShell output
// this file PARSES (the signer subject, the ntRunning yes/no, the messageBox
// True/False) must be preceded by [Console]::OutputEncoding=UTF8 so a non-ASCII
// signer subject is not corrupted by the OEM codepage. The mutant that drops the
// psUTF8 prefix from verifyMsiSigner fails here.
func TestPowerShellOutputForcedUTF8(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, `const psUTF8 = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;"`) {
		t.Fatalf("run_windows.go must define psUTF8 forcing UTF-8 output")
	}
	if !strings.Contains(s, `psUTF8 + "$s = Get-AuthenticodeSignature`) {
		t.Fatalf("verifyMsiSigner must force UTF-8 before parsing the signer subject")
	}
	if !strings.Contains(s, `psUTF8+"if (Get-Process NinjaTrader`) {
		t.Fatalf("ntRunning must force UTF-8 before parsing its output")
	}
	if !strings.Contains(s, `psUTF8 + "Add-Type -AssemblyName System.Windows.Forms`) {
		t.Fatalf("messageBox must force UTF-8 before parsing its output")
	}
}

// TestEveryChildGetsNoWindow pins W7: every child process run_windows.go starts
// must go through noWindow (HideWindow + CREATE_NO_WINDOW), so the friend app
// never flashes a console window while shelling out. The mutant that drops a
// noWindow(cmd) call (leaving a child that can pop a console) fails here.
func TestEveryChildGetsNoWindow(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, `HideWindow: true`) {
		t.Fatalf("noWindow must set HideWindow: true")
	}
	if !strings.Contains(s, `CreationFlags: windows.CREATE_NO_WINDOW`) {
		t.Fatalf("noWindow must set CreationFlags: windows.CREATE_NO_WINDOW")
	}

	// Every exec.Command / exec.CommandContext child must be paired with a
	// noWindow(cmd) call in the same function body.
	cmds := strings.Count(s, "exec.Command")
	noWindows := strings.Count(s, "noWindow(cmd)")
	if cmds != noWindows {
		t.Fatalf("exec.Command sites = %d but noWindow(cmd) calls = %d — every child must be hidden", cmds, noWindows)
	}
}

// TestReadLineEmptyEOFReturnsEOF pins S20: an empty read at EOF must return
// io.EOF (never ("", nil)), otherwise the "close NinjaTrader" prompt loop spins
// forever on an exhausted stdin. The mutant that reverts to returning the
// trimmed empty string with nil error fails here.
func TestReadLineEmptyEOFReturnsEOF(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	line := `readLine: func(prompt string) (string, error) {`
	idx := strings.Index(s, line)
	if idx < 0 {
		t.Fatalf("run_windows.go must define the %s seam", line)
	}
	body := s[idx:]
	if !strings.Contains(body, `err == io.EOF && trimmed == ""`) {
		t.Fatalf("readLine must detect an empty read at EOF")
	}
	if !strings.Contains(body, `return "", io.EOF`) {
		t.Fatalf("readLine must return io.EOF on an empty EOF read")
	}
}

// TestMessageBoxTopmost pins W9 (the run_windows.go half): the messageBox seam
// must render TOPMOST via MessageBoxOptions.ServiceNotification so the dialog is
// never buried behind other windows. The mutant that drops the option (back to
// the plain Show(text,caption,buttons,icon) overload) fails here.
func TestMessageBoxTopmost(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	line := `messageBox: func(text string) bool {`
	idx := strings.Index(s, line)
	if idx < 0 {
		t.Fatalf("run_windows.go must define the %s seam", line)
	}
	body := s[idx:]
	// G16: MB_TOPMOST (0x40000) | MB_SETFOREGROUND (0x10000) on the interactive
	// desktop — ServiceNotification was dropped (it targets the service desktop).
	if !strings.Contains(body, "0x40000") || !strings.Contains(body, "0x10000") {
		t.Fatalf("messageBox must use MB_TOPMOST(0x40000)|MB_SETFOREGROUND(0x10000)")
	}
	if strings.Contains(s, "::ServiceNotification") {
		t.Fatalf("messageBox must NOT use MessageBoxOptions::ServiceNotification")
	}
}

// TestFetchUsesTimeoutClient pins G12: the friend-bot fetches (manifest,
// identity, MSI) go through a 30s-timeout client, never http.DefaultClient (no
// timeout) which could hang the whole run on a wedged network.
func TestFetchUsesTimeoutClient(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, "friendHTTPClient = &http.Client{Timeout: 30 * time.Second}") {
		t.Fatalf("run_windows.go must define friendHTTPClient with a 30s timeout")
	}
	if !strings.Contains(s, "friendHTTPClient.Do(req)") {
		t.Fatalf("the fetch seam must call friendHTTPClient.Do")
	}
	if strings.Contains(s, "http.DefaultClient.Do(req)") {
		t.Fatalf("the fetch seam must NOT use http.DefaultClient (no timeout)")
	}
}

// TestErrorInfoBoxesTopmostPinsG16 pins the error/info dialog seams: errorBox is
// OK + Error icon, infoBox is OK + Information icon, both through the same
// MB_TOPMOST|MB_SETFOREGROUND msgBoxScript. The mutant that makes an error box a
// Yes/No, or swaps the icons, fails here.
func TestErrorInfoBoxesTopmostPinsG16(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, `errorBox: func(text string) {`) {
		t.Fatalf("run_windows.go must define errorBox")
	}
	if !strings.Contains(s, `infoBox: func(text string) {`) {
		t.Fatalf("run_windows.go must define infoBox")
	}
	if !strings.Contains(s, `msgBoxScript("Error", "OK")`) {
		t.Fatalf("errorBox must be OK + Error icon")
	}
	if !strings.Contains(s, `msgBoxScript("Information", "OK")`) {
		t.Fatalf("infoBox must be OK + Information icon")
	}
	if !strings.Contains(s, `msgBoxScript("Question", "YesNo")`) {
		t.Fatalf("messageBox must be YesNo + Question icon")
	}
}

// TestPowerShellNoScriptTextSplicing pins S18b (the run_windows.go half): the
// controlled strings (the MSI path in verifyMsiSigner and the text in
// messageBox) must ride -EncodedCommand + $args[0] — never be spliced into the
// script text with quote-escaping. The mutant that re-adds the
// strings.ReplaceAll(...,"'","”") splicing fails here.
func TestPowerShellNoScriptTextSplicing(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	// psEncodedCommand now lives in osdeps.go (shared with flow.go) so the
	// Linux test build can use it too.
	osdeps, err := os.ReadFile("osdeps.go")
	if err != nil {
		t.Fatalf("read osdeps.go: %v", err)
	}
	if !strings.Contains(string(osdeps), "func psEncodedCommand(script string) string") {
		t.Fatalf("osdeps.go must define psEncodedCommand (shared with flow.go)")
	}
	if !strings.Contains(string(osdeps), "utf16.Encode") || !strings.Contains(string(osdeps), "base64.StdEncoding") {
		t.Fatalf("psEncodedCommand must base64-encode UTF-16LE")
	}
	if strings.Contains(s, `ReplaceAll(msiPath, "'", "''")`) {
		t.Fatalf("verifyMsiSigner must NOT splice the MSI path with quote-escaping")
	}
	if strings.Contains(s, `ReplaceAll(text, "'", "''")`) {
		t.Fatalf("messageBox must NOT splice the text with quote-escaping")
	}
	// Both controlled-string call sites must go through -EncodedCommand and
	// hand the value to the script as $args[0].
	if !strings.Contains(s, `"-EncodedCommand", psEncodedCommand(script), msiPath`) {
		t.Fatalf("verifyMsiSigner must pass the path via -EncodedCommand + $args[0]")
	}
	if !strings.Contains(s, `"-EncodedCommand", psEncodedCommand(script), text`) {
		t.Fatalf("messageBox must pass the text via -EncodedCommand + $args[0]")
	}
	if !strings.Contains(s, `$args[0]`) {
		t.Fatalf("the encoded scripts must read the controlled string from $args[0]")
	}
}

// TestAppendFileSeamUsesOAppend pins S17: the appendFile seam must be a TRUE
// append (os.O_APPEND|os.O_CREATE|os.O_WRONLY via os.OpenFile), never a
// read-modify-write that races and drops concurrent log lines. The mutant that
// reverts to readFile+writeFile (or os.WriteFile) fails here.
func TestAppendFileSeamUsesOAppend(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	line := `appendFile: func(path string, data []byte, perm fs.FileMode) error {`
	if !strings.Contains(s, line) {
		t.Fatalf("run_windows.go must define the %s seam", line)
	}
	if !strings.Contains(s, "os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)") {
		t.Fatalf("appendFile must open with O_APPEND|O_CREATE|O_WRONLY")
	}

	osdeps, err := os.ReadFile("osdeps.go")
	if err != nil {
		t.Fatalf("read osdeps.go: %v", err)
	}
	if !strings.Contains(string(osdeps), "appendFile func(path string, data []byte, perm fs.FileMode) error") {
		t.Fatalf("osdeps.go must declare the appendFile seam")
	}
}
