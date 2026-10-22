package updaterworker

import (
	"context"
	"errors"
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
	}, func(f string, a ...any) { logs = append(logs, f) })

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
	}, func(f string, a ...any) { logs = append(logs, f) })

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
