// Package nt8restart is the tested seam for restarting NinjaTrader 8 from
// WSL as the last step of the one-click AddOn update (W-ADDON-ONE-CLICK,
// owner order 2026-10-17: "update = one click … and the NinjaTrader file
// too"). The worker never touches a process itself — every Windows-side
// action is a powershell.exe / tasklist.exe command run through a Runner,
// which tests replace with a fake. Production wires RunViaExec.
//
// Sequence (the worker calls Restart):
//
//  1. Close  — graceful: Get-Process → CloseMainWindow, then poll tasklist
//     for up to Grace (default 60s);
//  2. Force  — if it is still alive after Grace: Get-Process → Stop-Process;
//  3. Start  — Start-Process the executable;
//  4. the worker then waits for the AddOn reconnect (a new accept_seq +
//     build_id), NOT this package.
//
// Quoting from WSL: powershell.exe receives the script as ONE -Command argv.
// Paths are wrapped in PowerShell SINGLE quotes (no escaping needed for the
// backslashes in `C:\Program Files\…`); no double quotes appear in the script,
// so nothing is lost to the WSL→Windows command-line join.
package nt8restart

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner runs one external command and returns its combined output. It is the
// same shape as internal/addonbuild.Config.Run, so the worker threads one seam
// through build, place and restart. Tests pass a fake; production passes
// RunViaExec.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Config identifies the NinjaTrader process and the polling knobs.
type Config struct {
	// Exe is the full Windows path, e.g. `C:\Program Files\NinjaTrader 8\bin\NinjaTrader.exe`.
	Exe string
	// ExeName is the image name tasklist matches; "" → NinjaTrader.exe.
	ExeName string
	// Run executes each command; a nil Run makes every call fail with ErrNoRunner.
	Run Runner
	// Poll is the delay between liveness checks; ≤0 → 2s.
	Poll time.Duration
	// Grace is how long CloseMainWindow is given before Stop-Process; ≤0 → 60s.
	Grace time.Duration
}

const (
	// DefaultGrace is how long the graceful close is given before a force stop.
	DefaultGrace = 60 * time.Second
	// DefaultPoll is the liveness-check interval.
	DefaultPoll = 2 * time.Second
)

// Sentinel errors the worker can match.
var (
	ErrNoRunner  = errors.New("nt8restart: no runner configured")
	ErrNoExe     = errors.New("nt8restart: no NinjaTrader executable configured")
	ErrForceStop = errors.New("nt8restart: NinjaTrader did not exit gracefully and was force-stopped")
)

func (c Config) imageName() string {
	if c.ExeName != "" {
		return c.ExeName
	}
	return "NinjaTrader.exe"
}

// procName is the Get-Process -Name token (the image name without .exe).
func (c Config) procName() string {
	n := c.imageName()
	if strings.EqualFold(filepath.Ext(n), ".exe") {
		n = n[:len(n)-len(filepath.Ext(n))]
	}
	return n
}

func (c Config) poll() time.Duration {
	if c.Poll > 0 {
		return c.Poll
	}
	return DefaultPoll
}

func (c Config) grace() time.Duration {
	if c.Grace > 0 {
		return c.Grace
	}
	return DefaultGrace
}

func (c Config) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Run == nil {
		return nil, ErrNoRunner
	}
	return c.Run(ctx, name, args...)
}

// RunViaExec is the production Runner: exec.CommandContext with a per-command
// timeout. Tests never use it; they pass a fake Runner instead.
func RunViaExec(timeout time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return exec.CommandContext(cctx, name, args...).CombinedOutput()
	}
}

// IsRunning reports whether the NinjaTrader image is alive, read through
// `tasklist.exe /FI "IMAGENAME eq <exe>" /FO CSV /NH`. It is deliberately
// locale-independent: running ⇔ the image name appears as a CSV field; the
// localized "INFO: No tasks …" line contains no image name and reads false.
func IsRunning(ctx context.Context, c Config) (bool, error) {
	if c.Exe == "" && c.ExeName == "" {
		return false, ErrNoExe
	}
	out, err := c.run(ctx, "tasklist.exe", "/FI", "IMAGENAME eq "+c.imageName(), "/FO", "CSV", "/NH")
	if err != nil {
		return false, fmt.Errorf("tasklist: %w", err)
	}
	// Field match first: a CSV field that is exactly the image name is
	// authoritative. Then a whole-line contains as a fallback for tasklist
	// builds that do not quote CSV fields — the localized "INFO: No tasks"
	// line never names the image, so either read is safe.
	for _, line := range strings.Split(string(out), "\n") {
		for _, field := range strings.Split(line, ",") {
			if strings.EqualFold(strings.Trim(strings.TrimSpace(field), `"`), c.imageName()) {
				return true, nil
			}
		}
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(c.imageName())), nil
}

// gracefulClose asks every NinjaTrader process to close its main window.
// The PowerShell command uses only single quotes, so the WSL→Windows argv
// join cannot mangle it.
func gracefulClose(ctx context.Context, c Config) error {
	script := fmt.Sprintf(
		"Get-Process -Name '%s' -ErrorAction SilentlyContinue | ForEach-Object { $_.CloseMainWindow() } | Out-Null",
		c.procName(),
	)
	if _, err := c.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return fmt.Errorf("graceful close: %w", err)
	}
	return nil
}

func forceStop(ctx context.Context, c Config) error {
	script := fmt.Sprintf(
		"Get-Process -Name '%s' -ErrorAction SilentlyContinue | Stop-Process -Force",
		c.procName(),
	)
	if _, err := c.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return fmt.Errorf("force stop: %w", err)
	}
	return nil
}

// Close stops NinjaTrader: graceful first, then a force stop if it is still
// alive after Grace. It returns ErrForceStop when the force path was taken
// (the worker may want to log that distinctly).
func Close(ctx context.Context, c Config) error {
	if c.Exe == "" && c.ExeName == "" {
		return ErrNoExe
	}
	if err := gracefulClose(ctx, c); err != nil {
		return err
	}
	deadline := time.Now().Add(c.grace())
	for {
		running, err := IsRunning(ctx, c)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		if err := sleepCtx(ctx, c.poll()); err != nil {
			return err
		}
	}
	if err := forceStop(ctx, c); err != nil {
		return err
	}
	// One final confirmation: after Stop-Process the process must be gone.
	if running, err := IsRunning(ctx, c); err != nil {
		return err
	} else if running {
		return fmt.Errorf("%w: still running after Stop-Process", ErrForceStop)
	}
	return ErrForceStop
}

// Start launches NinjaTrader detached via Start-Process, so powershell.exe
// exits immediately and the bot's own restart wait (the AddOn reconnect) owns
// the liveness from there.
func Start(ctx context.Context, c Config) error {
	if c.Exe == "" {
		return ErrNoExe
	}
	script := fmt.Sprintf("Start-Process -FilePath '%s'", c.Exe)
	if _, err := c.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// Restart is Close followed by Start — the worker's one restart step.
func Restart(ctx context.Context, c Config) error {
	if err := Close(ctx, c); err != nil && !errors.Is(err, ErrForceStop) {
		return err
	}
	return Start(ctx, c)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
