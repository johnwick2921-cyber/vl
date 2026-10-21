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
