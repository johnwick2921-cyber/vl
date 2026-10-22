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
	nt8ExitWait      = 60 * time.Second
	nt8StartWait     = 60 * time.Second
	nt8RestartPoll   = 2 * time.Second
	nt8ReconnectWait = 5 * time.Minute
	// nt8ImageName is the image tasklist.exe matches (the exe's file name).
	nt8ImageName = "NinjaTrader.exe"
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

// wslInteropBackoffs are the m388 retry sleeps between tries (three tries
// total, so two sleeps).
var wslInteropBackoffs = []time.Duration{5 * time.Second, 15 * time.Second}

// wslInteropTimeout reports whether a failed Windows exec is the WSL
// interop-timeout the owner saw ("<3>WSL … UtilAcceptVsock:271: accept4 failed
// 110"): Windows and WSL briefly fail to talk to each other and the vsock
// accept call times out. The match is specific — BOTH markers must be present
// in the command's output (or the error) — so an unrelated failure is never
// retried.
func wslInteropTimeout(out []byte, err error) bool {
	if err == nil {
		return false
	}
	text := string(out)
	if text == "" {
		text = err.Error()
	}
	return strings.Contains(text, "UtilAcceptVsock") && strings.Contains(text, "accept4 failed")
}

// retryWSLInterop wraps the Windows exec seam with a bounded retry that fires
// ONLY on the WSL interop-timeout signature. Three tries total (two retries,
// 5 s then 15 s backoff); each retry is logged. Any other failure is returned
// immediately, and a cancelled context stops the loop.
//
// landed, when non-nil, is consulted BEFORE each retry: if it reports that the
// command's effect already landed despite the interop error (m388 follow-up:
// a Start-Process whose NT8 launch actually succeeded), the command is NOT
// re-issued and the wrapper returns success. This closes the one non-idempotent
// call the retry could otherwise duplicate.
func retryWSLInterop(run nt8RunSeam, sleep func(context.Context, time.Duration) error, logf func(string, ...any), landed func(context.Context, string, []string) bool) nt8RunSeam {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for attempt := 0; ; attempt++ {
			out, err := run(ctx, name, args...)
			if err == nil || !wslInteropTimeout(out, err) || attempt >= len(wslInteropBackoffs) {
				return out, err
			}
			if landed != nil && landed(ctx, name, args) {
				logf("🔁 WSL interop timeout on %s (try %d/3) but the effect already landed — not re-issuing", name, attempt+1)
				return nil, nil
			}
			logf("🔁 WSL interop timeout on %s (try %d/3): %s — retrying in %s", name, attempt+1, clipText(err.Error()), wslInteropBackoffs[attempt])
			if serr := sleep(ctx, wslInteropBackoffs[attempt]); serr != nil {
				return out, err
			}
		}
	}
}

// nt8StartLanded is the m388 follow-up idempotency guard for the ONE
// non-idempotent Windows call the retry could duplicate: a Start-Process for
// NinjaTrader.exe. If a start "failed" with the interop signature but the
// launch actually landed, re-issuing would start a SECOND NinjaTrader. Before
// any retry of a Start-Process command the worker asks tasklist whether NT8 is
// already running; if it is, the retry is skipped and the start is treated as
// success. Every other command returns false (its retry is safe: builds
// overwrite, CloseMainWindow/Stop-Process are idempotent, tasklist is
// read-only). The probe runs on the same raw seam so tests can fake it.
func (w *Worker) nt8StartLanded(ctx context.Context, run nt8RunSeam, name string, args []string) bool {
	if !strings.Contains(strings.Join(args, " "), "Start-Process") {
		return false
	}
	running, err := newNT8Restart(run, addonExe(), w.host.Sleep).isRunning(ctx)
	return err == nil && running
}

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

// isRunning reports whether the NinjaTrader image is alive, read through
// `tasklist.exe /FI "IMAGENAME eq <name>" /FO CSV /NH`. Locale-independent:
// running ⇔ the image name appears as a CSV field; the localized
// "INFO: No tasks …" line contains no image name and reads false.
func (r nt8Restart) isRunning(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "tasklist.exe", "/FI", "IMAGENAME eq "+nt8ImageName, "/FO", "CSV", "/NH")
	if err != nil {
		// execWindows uses CombinedOutput: on a run error the stderr is in out.
		// Keep it (m246 — the receipt names the stderr, never just "exit 1").
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return false, fmt.Errorf("tasklist: %w (stderr: %s)", err, trimmed)
		}
		return false, fmt.Errorf("tasklist: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		for _, field := range strings.Split(line, ",") {
			if strings.EqualFold(strings.Trim(strings.TrimSpace(field), `"`), nt8ImageName) {
				return true, nil
			}
		}
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(nt8ImageName)), nil
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
