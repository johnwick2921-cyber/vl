package updaterworker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// W-ADDON-ONE-CLICK: the NT8 restart seam.
//
// The worker drives NinjaTrader 8 through powershell.exe (WSL execs Windows
// binaries directly). stop is three calls behind the seam — signal CloseMainWindow,
// wait up to closeWait, then Stop-Process — so a test can inject a fake runner
// and a fake clock and never touch the real NT8 (THE ONE RULE: SIM only, never
// start or stop the real NT8 in a test).

const (
	defaultNT8Exe    = `C:\Program Files\NinjaTrader 8\bin\NinjaTrader.exe`
	nt8CloseWait     = 60 * time.Second
	nt8ReconnectWait = 5 * time.Minute
)

// addonExe returns VL_NT8_EXE or the default. Nothing is hardcoded to "hoang":
// the partner box's own env supplies its path.
func addonExe() string {
	if v := strings.TrimSpace(os.Getenv("VL_NT8_EXE")); v != "" {
		return v
	}
	return defaultNT8Exe
}

// nt8RunSeam executes a Windows executable with args. Production binds it to
// powershell.exe; tests bind a fake.
type nt8RunSeam func(ctx context.Context, name string, args ...string) ([]byte, error)

// nt8Restart stops and starts NT8 through the run seam, with a sleep seam for
// the graceful-close wait and the reconnect poll (both bounded, both fakeable).
type nt8Restart struct {
	run   nt8RunSeam
	exe   string
	sleep func(ctx context.Context, d time.Duration) error
}

func newNT8Restart(run nt8RunSeam, exe string, sleep func(context.Context, time.Duration) error) nt8Restart {
	if exe == "" {
		exe = addonExe()
	}
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		}
	}
	return nt8Restart{run: run, exe: exe, sleep: sleep}
}

// signalClose asks every NinjaTrader process to close its main window. It does
// NOT force-kill: a hung window is killed by forceStop after the wait.
func (r nt8Restart) signalClose(ctx context.Context) error {
	const script = `(Get-Process -Name 'NinjaTrader' -ErrorAction SilentlyContinue).CloseMainWindow() | Out-Null`
	_, err := r.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	return err
}

// forceStop kills any NinjaTrader still running after the graceful wait.
func (r nt8Restart) forceStop(ctx context.Context) error {
	const script = `Stop-Process -Name 'NinjaTrader' -Force -ErrorAction SilentlyContinue`
	_, err := r.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	return err
}

// start launches the configured NT8 exe.
func (r nt8Restart) start(ctx context.Context) error {
	script := fmt.Sprintf("Start-Process -FilePath '%s'", r.exe)
	_, err := r.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	return err
}

// stop runs the graceful-close → bounded-wait → force-stop sequence.
func (r nt8Restart) stop(ctx context.Context) error {
	if err := r.signalClose(ctx); err != nil {
		return fmt.Errorf("nt8 restart: CloseMainWindow failed: %w", err)
	}
	if err := r.sleep(ctx, nt8CloseWait); err != nil {
		return fmt.Errorf("nt8 restart: graceful-close wait aborted: %w", err)
	}
	if err := r.forceStop(ctx); err != nil {
		return fmt.Errorf("nt8 restart: Stop-Process failed: %w", err)
	}
	return nil
}
