package nt8restart

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCall struct {
	name string
	args []string
}

type fakeRunner struct {
	calls    []fakeCall
	respond  func(name string, args []string) ([]byte, error)
	ctxCheck func(ctx context.Context) // optional: assert the ctx flows through
}

func (f *fakeRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeCall{name: name, args: args})
	if f.ctxCheck != nil {
		f.ctxCheck(ctx)
	}
	if f.respond != nil {
		return f.respond(name, args)
	}
	return nil, nil
}

func (f *fakeRunner) cfg(exe string) Config {
	return Config{
		Exe:     exe,
		ExeName: "NinjaTrader.exe",
		Run:     f.run,
		Poll:    time.Millisecond,
		Grace:   30 * time.Millisecond,
	}
}

// lastScript returns the -Command argument of the most recent powershell call.
func (f *fakeRunner) lastScript() string {
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].name == "powershell.exe" {
			for j, a := range f.calls[i].args {
				if a == "-Command" && j+1 < len(f.calls[i].args) {
					return f.calls[i].args[j+1]
				}
			}
		}
	}
	return ""
}

const exePath = `C:\Program Files\NinjaTrader 8\bin\NinjaTrader.exe`

func TestIsRunningTrueAndFalse(t *testing.T) {
	f := &fakeRunner{}
	c := f.cfg(exePath)

	f.respond = func(name string, args []string) ([]byte, error) {
		if name == "tasklist.exe" {
			return []byte(`"NinjaTrader.exe","1234","Console","1","123,456 K"` + "\n"), nil
		}
		return nil, nil
	}
	on, err := IsRunning(context.Background(), c)
	if err != nil || !on {
		t.Fatalf("running = %v, err=%v, want true", on, err)
	}

	f.respond = func(name string, args []string) ([]byte, error) {
		if name == "tasklist.exe" {
			return []byte("INFO: No tasks are running which match the specified criteria.\n"), nil
		}
		return nil, nil
	}
	on, err = IsRunning(context.Background(), c)
	if err != nil || on {
		t.Fatalf("running = %v, err=%v, want false (the localized no-tasks line names no image)", on, err)
	}
}

func TestCloseStopsGracefullyWithoutForce(t *testing.T) {
	f := &fakeRunner{}
	c := f.cfg(exePath)
	// After the graceful close, tasklist reports the process gone.
	f.respond = func(name string, args []string) ([]byte, error) {
		if name == "tasklist.exe" {
			return []byte("INFO: No tasks are running which match the specified criteria.\n"), nil
		}
		return nil, nil
	}
	if err := Close(context.Background(), c); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if got := f.lastScript(); !strings.Contains(got, "CloseMainWindow") {
		t.Fatalf("graceful close script = %q, want CloseMainWindow", got)
	}
	for _, call := range f.calls {
		if call.name == "powershell.exe" && strings.Contains(strings.Join(call.args, " "), "Stop-Process") {
			t.Fatal("a graceful exit must not force-stop")
		}
	}
}

func TestCloseForcesAfterGrace(t *testing.T) {
	f := &fakeRunner{}
	c := f.cfg(exePath)
	// Never exits: tasklist always reports the process alive.
	f.respond = func(name string, args []string) ([]byte, error) {
		if name == "tasklist.exe" {
			return []byte(`"NinjaTrader.exe","1234","Console","1","123,456 K"` + "\n"), nil
		}
		return nil, nil
	}
	err := Close(context.Background(), c)
	if !errors.Is(err, ErrForceStop) {
		t.Fatalf("Close = %v, want ErrForceStop after the grace window", err)
	}
	force := false
	for _, call := range f.calls {
		if call.name == "powershell.exe" && strings.Contains(strings.Join(call.args, " "), "Stop-Process") {
			force = true
		}
	}
	if !force {
		t.Fatal("a process that outlives Grace must be force-stopped")
	}
}

func TestStartUsesSingleQuotedPath(t *testing.T) {
	f := &fakeRunner{}
	c := f.cfg(exePath)
	if err := Start(context.Background(), c); err != nil {
		t.Fatalf("Start = %v", err)
	}
	script := f.lastScript()
	if !strings.Contains(script, `Start-Process -FilePath '`+exePath+`'`) {
		t.Fatalf("start script = %q, want a single-quoted path", script)
	}
	if strings.Contains(script, `"`) {
		t.Fatalf("start script %q must not contain double quotes (WSL argv join safety)", script)
	}
}

func TestRestartRunsCloseThenStart(t *testing.T) {
	f := &fakeRunner{}
	c := f.cfg(exePath)
	f.respond = func(name string, args []string) ([]byte, error) {
		if name == "tasklist.exe" {
			return []byte("INFO: No tasks are running which match the specified criteria.\n"), nil
		}
		return nil, nil
	}
	if err := Restart(context.Background(), c); err != nil {
		t.Fatalf("Restart = %v", err)
	}
	var closeIdx, startIdx = -1, -1
	for i, call := range f.calls {
		if call.name != "powershell.exe" {
			continue
		}
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "CloseMainWindow") {
			closeIdx = i
		}
		if strings.Contains(joined, "Start-Process") {
			startIdx = i
		}
	}
	if closeIdx < 0 || startIdx < 0 || closeIdx > startIdx {
		t.Fatalf("restart order wrong: close at %d, start at %d", closeIdx, startIdx)
	}
}

func TestCloseAndStartRequireAnExe(t *testing.T) {
	f := &fakeRunner{}
	if err := Close(context.Background(), Config{Run: f.run}); !errors.Is(err, ErrNoExe) {
		t.Fatalf("Close without an exe/name = %v, want ErrNoExe", err)
	}
	if err := Start(context.Background(), Config{Run: f.run}); !errors.Is(err, ErrNoExe) {
		t.Fatalf("Start without an exe = %v, want ErrNoExe", err)
	}
}

func TestCallsFailClosedWithoutRunner(t *testing.T) {
	c := Config{Exe: exePath, Run: nil}
	if _, err := IsRunning(context.Background(), c); !errors.Is(err, ErrNoRunner) {
		t.Fatalf("IsRunning without a runner = %v, want ErrNoRunner", err)
	}
	if err := Start(context.Background(), c); !errors.Is(err, ErrNoRunner) {
		t.Fatalf("Start without a runner = %v, want ErrNoRunner", err)
	}
}
