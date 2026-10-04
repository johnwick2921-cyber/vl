package main

import (
	"context"
	"fmt"
	"vl/safe"
	"os"
	"time"

	ntpkg "vl/provider/ninjatrader"
)

// runRoundtripSmoke exercises the full signal -> mockNT -> fill cycle on a
// temp directory. Needs no network, no NT8, no env vars. Depends on
// provider/ninjatrader.StartMockNT (Plan 5 Task 27 mock harness).
func runRoundtripSmoke() {
	dir, err := os.MkdirTemp("", "vl-smoke-roundtrip-*")
	if err != nil {
		fmt.Printf("FAIL roundtrip: tempdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	stop := ntpkg.StartMockNT(dir, 200*time.Millisecond)
	defer stop()

	writer := ntpkg.NewCSVWriter(dir)
	sig := ntpkg.SignalRow{
		DateTime:   time.Now().Format("01/02/2006 15:04:05"),
		Direction:  "LONG",
		EntryPrice: 21500.00,
		StopLoss:   21480.00,
		TakeProfit: 21540.00,
	}
	if err := writer.WriteSignal(sig); err != nil {
		fmt.Printf("FAIL roundtrip: write signal: %v\n", err)
		os.Exit(1)
	}

	tailer := ntpkg.NewCSVTailer(dir, 100*time.Millisecond)
	fillCh := make(chan ntpkg.FillRow, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	safe.GoNamed("nq-smoke-tailer", func() {
		_ = tailer.TailFills(ctx, func(f ntpkg.FillRow) {
			select {
			case fillCh <- f:
			default:
			}
		})
	})

	startWait := time.Now()
	select {
	case fill := <-fillCh:
		fmt.Printf("OK roundtrip: signal->fill in %v (direction=%s entry=%.2f)\n",
			time.Since(startWait), fill.Direction, fill.EntryPrice)
	case <-time.After(2 * time.Second):
		fmt.Println("FAIL roundtrip: no fill within 2s")
		os.Exit(1)
	}
}
