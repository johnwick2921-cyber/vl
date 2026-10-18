package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitUntilFlatReturnsWhenFlat(t *testing.T) {
	p := func(ctx context.Context) (float64, error) { return 0, nil }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitUntilFlat(ctx, p, time.Millisecond); err != nil {
		t.Fatalf("flat provider must return nil, got %v", err)
	}
}

func TestWaitUntilFlatWaitsUntilFlat(t *testing.T) {
	// closed is atomic: the poll reads it while the goroutine below flips it
	// (a plain float64 here was a DATA RACE under -race in CI).
	var closed atomic.Bool
	p := func(ctx context.Context) (float64, error) {
		if closed.Load() {
			return 0, nil
		}
		return 2, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		closed.Store(true)
	}()
	if err := waitUntilFlat(ctx, p, 10*time.Millisecond); err != nil {
		t.Fatalf("should observe the position close, got %v", err)
	}
}

func TestWaitUntilFlatDeferredOnDeadline(t *testing.T) {
	p := func(ctx context.Context) (float64, error) { return 1, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := waitUntilFlat(ctx, p, time.Millisecond)
	if err == nil {
		t.Fatal("a still-open position at the deadline must defer (error), not proceed")
	}
}

func TestWaitUntilFlatRetriesOnError(t *testing.T) {
	// A transient error must not abort the wait; the poll continues.
	call := 0
	p := func(ctx context.Context) (float64, error) {
		call++
		if call == 1 {
			return 0, errors.New("transient")
		}
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitUntilFlat(ctx, p, time.Millisecond); err != nil {
		t.Fatalf("a transient error should be retried, got %v", err)
	}
	if call < 2 {
		t.Fatalf("provider called %d times, want >= 2 (retried)", call)
	}
}
