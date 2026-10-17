package updaterworker

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The restart seam must drive powershell.exe with the right commands and never
// touch a real process: every call goes through the injected run seam, and the
// graceful-close wait goes through the injected sleep seam.
func TestNT8RestartSeamCommands(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	var slept []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	r := newNT8Restart(run, `C:\Program Files\NinjaTrader 8\bin\NinjaTrader.exe`, sleep)
	if err := r.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := r.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3 (signal, force, start)", len(calls))
	}
	if !strings.Contains(calls[0], "powershell.exe") || !strings.Contains(calls[0], "CloseMainWindow") {
		t.Errorf("first call must signal CloseMainWindow: %q", calls[0])
	}
	if len(slept) != 1 || slept[0] != nt8CloseWait {
		t.Errorf("sleep = %v, want one %v", slept, nt8CloseWait)
	}
	if !strings.Contains(calls[1], "Stop-Process -Name 'NinjaTrader' -Force") {
		t.Errorf("second call must force-stop: %q", calls[1])
	}
	if !strings.Contains(calls[2], "Start-Process -FilePath 'C:\\Program Files\\NinjaTrader 8\\bin\\NinjaTrader.exe'") {
		t.Errorf("third call must start the exe: %q", calls[2])
	}
}

// The default exe is VL_NT8_EXE, else the shipped default. Nothing is hardcoded
// to a specific Windows user.
func TestNT8RestartExeFromEnv(t *testing.T) {
	t.Setenv("VL_NT8_EXE", "")
	if got := addonExe(); got != defaultNT8Exe {
		t.Errorf("default exe = %q, want %q", got, defaultNT8Exe)
	}
	t.Setenv("VL_NT8_EXE", `D:\NT\bin\NinjaTrader.exe`)
	if got := addonExe(); got != `D:\NT\bin\NinjaTrader.exe` {
		t.Errorf("env exe = %q, want the env value", got)
	}
}
