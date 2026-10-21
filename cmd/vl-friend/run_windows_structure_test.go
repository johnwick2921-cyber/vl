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
func TestReadLineInstallCodeEchoOff(t *testing.T) {
	src, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatalf("read run_windows.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, `"golang.org/x/term"`) {
		t.Fatalf("run_windows.go must import golang.org/x/term for ReadPassword")
	}
	line := `readLine: func(prompt string) (string, error) {`
	idx := strings.Index(s, line)
	if idx < 0 {
		t.Fatalf("run_windows.go must define the %s seam", line)
	}
	body := s[idx:]
	if !strings.Contains(body, "term.ReadPassword(fd)") {
		t.Fatalf("readLine must call term.ReadPassword for the secret read")
	}
	if !strings.Contains(body, "term.IsTerminal(fd)") {
		t.Fatalf("readLine must guard the secret read with term.IsTerminal")
	}
	if !strings.Contains(body, `"Code received (hidden)"`) {
		t.Fatalf(`readLine must confirm with "Code received (hidden)"`)
	}
	// The secret read is gated on the empty prompt (the install-code call
	// site) and the confirmation follows the read, so a regression that moves
	// ReadPassword into the non-empty y/N path (or echoes before the gate)
	// fails here.
	gate := strings.Index(body, `prompt == ""`)
	read := strings.Index(body, "term.ReadPassword(fd)")
	confirm := strings.Index(body, `"Code received (hidden)"`)
	if gate < 0 || read < 0 || confirm < 0 {
		t.Fatalf("gate=%d read=%d confirm=%d (all must be present)", gate, read, confirm)
	}
	if read < gate {
		t.Fatalf("term.ReadPassword must sit INSIDE the prompt == \"\" gate (read at %d, gate at %d)", read, gate)
	}
	if confirm < read {
		t.Fatalf(`"Code received (hidden)" must follow the ReadPassword read (confirm at %d, read at %d)`, confirm, read)
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
	if !strings.Contains(body, "[System.Windows.Forms.MessageBoxOptions]::ServiceNotification") {
		t.Fatalf("messageBox must pass MessageBoxOptions.ServiceNotification (TopMost)")
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

	if !strings.Contains(s, "func psEncodedCommand(script string) string") {
		t.Fatalf("run_windows.go must define psEncodedCommand")
	}
	if !strings.Contains(s, "utf16.Encode") || !strings.Contains(s, "base64.StdEncoding") {
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
