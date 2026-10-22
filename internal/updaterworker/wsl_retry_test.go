package updaterworker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// wslSig is the exact interop-timeout signature the owner saw on 2026-10-22:
// "<3>WSL … UtilAcceptVsock:271: accept4 failed 110".
const wslSig = "<3>WSL: UtilAcceptVsock:271: accept4 failed 110"

func TestRetryWSLInteropRetriesThenSucceeds(t *testing.T) {
	calls := 0
	var sleeps []time.Duration
	var logs []string
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if calls <= 2 {
			return []byte(wslSig), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	}
	seam := retryWSLInterop(run, func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}, func(f string, a ...any) { logs = append(logs, f) }, nil)

	out, err := seam(context.Background(), "tasklist.exe", "/FI", "x")
	if err != nil {
		t.Fatalf("retry seam: %v", err)
	}
	if string(out) != "ok" {
		t.Fatalf("out = %q, want ok", out)
	}
	if calls != 3 {
		t.Fatalf("runner called %d times, want 3 (signature twice, then success)", calls)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != 15*time.Second {
		t.Fatalf("sleeps = %v, want [5s 15s]", sleeps)
	}
	if len(logs) != 2 {
		t.Fatalf("log lines = %d, want 2 (one per retry)", len(logs))
	}
}

func TestRetryWSLInteropOnlyRetriesOnTheSignature(t *testing.T) {
	calls := 0
	var sleeps []time.Duration
	var logs []string
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte("compiler error CS0001"), errors.New("exit status 1")
	}
	seam := retryWSLInterop(run, func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}, func(f string, a ...any) { logs = append(logs, f) }, nil)

	_, err := seam(context.Background(), "dotnet.exe", "build")
	if err == nil {
		t.Fatal("a non-signature failure must propagate, not retry")
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1 (non-signature = no retry)", calls)
	}
	if len(sleeps) != 0 || len(logs) != 0 {
		t.Fatalf("sleeps=%d logs=%d, want 0/0 for a non-signature failure", len(sleeps), len(logs))
	}
}

// TestRetryWSLInteropOneMarkerIsNotEnough: "UtilAcceptVsock" alone (without
// "accept4 failed") is a near-miss — the interop signature needs BOTH markers,
// so a one-marker failure must not retry.
func TestRetryWSLInteropOneMarkerIsNotEnough(t *testing.T) {
	calls := 0
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte("<3>WSL: UtilAcceptVsock:271: some other error"), errors.New("exit status 1")
	}
	seam := retryWSLInterop(run, func(_ context.Context, d time.Duration) error { return nil },
		func(string, ...any) {}, nil)
	_, err := seam(context.Background(), "tasklist.exe", "/FI", "x")
	if err == nil {
		t.Fatal("a near-miss failure must propagate")
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1 (one marker only = no retry)", calls)
	}
}

// TestRetryWSLInteropCancelledContextStopsTheLoop: a cancelled context (the
// sleep returns ctx.Err()) stops the retry loop after ONE call and returns the
// original error — no further re-issue.
func TestRetryWSLInteropCancelledContextStopsTheLoop(t *testing.T) {
	calls := 0
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte(wslSig), errors.New("exit status 1")
	}
	seam := retryWSLInterop(run, func(context.Context, time.Duration) error {
		return context.Canceled
	}, func(string, ...any) {}, nil)
	_, err := seam(context.Background(), "tasklist.exe", "/FI", "x")
	if err == nil {
		t.Fatal("the original error must propagate on a cancelled retry")
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1 (cancelled ctx stops the loop)", calls)
	}
}

// TestWinSeamStartAlreadyLandedDoesNotReissue is the m388 follow-up (a) pin:
// a Start-Process that fails with the interop signature but whose NT8 launch
// actually landed must NOT be re-issued — before the retry the worker asks
// tasklist, sees NT8 running, and returns success after ONE Start-Process call.
func TestWinSeamStartAlreadyLandedDoesNotReissue(t *testing.T) {
	r := newRig(t)
	startCalls := 0
	tasklistCalls := 0
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "Start-Process") {
			startCalls++
			return []byte(wslSig), errors.New("exit status 1")
		}
		if strings.Contains(name, "tasklist") {
			tasklistCalls++
			return []byte(`"NinjaTrader.exe","1234","Console"`), nil
		}
		return nil, nil
	}

	out, err := r.w.winSeam()(context.Background(), "powershell.exe", "-Command", "Start-Process -FilePath 'C:\\Program Files\\NinjaTrader 8\\bin\\NinjaTrader.exe'")
	if err != nil {
		t.Fatalf("start must read as success once NT8 is already running: %v", err)
	}
	if string(out) != "" {
		t.Fatalf("out = %q, want empty on a landed start", out)
	}
	if startCalls != 1 {
		t.Fatalf("Start-Process issued %d times, want 1 (not re-issued)", startCalls)
	}
	if tasklistCalls == 0 {
		t.Fatal("the isRunning guard never ran — the retry guard must check tasklist before re-issuing")
	}
}

// TestWinSeamRetriesTheInteropSignature pins the PRODUCTION wiring: winSeam
// must wrap the raw run seam with the retry. The mutant (drop the wrap, return
// the raw seam) makes this RED — the signature error reaches the step instead
// of being retried.
func TestWinSeamRetriesTheInteropSignature(t *testing.T) {
	r := newRig(t)
	calls := 0
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if calls <= 2 {
			return []byte(wslSig), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	}
	out, err := r.w.winSeam()(context.Background(), "tasklist.exe", "/FI", "x")
	if err != nil {
		t.Fatalf("winSeam: %v", err)
	}
	if string(out) != "ok" || calls != 3 {
		t.Fatalf("out=%q calls=%d, want ok/3 (two retries then success)", out, calls)
	}
}
