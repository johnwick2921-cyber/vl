package manager

import (
	"errors"
	"testing"
	"time"

	"vl/trader"
)

// TestRemoveTraderRetiresAStartingInstance — MENTOR-NY item 3 at the manager
// call site. 10-13 16:35:46: request B's RemoveTrader dropped request A's
// instance whose Run goroutine had been spawned but had not yet set
// is_running — RemoveTrader stops only a RUNNING trader, so the instance was
// removed un-stopped and then ran orphaned beside the instance B loaded. Now
// RemoveTrader retires it first: its Run, when it gets there, is refused
// before any side effect, with the no-op sentinel (callers never write
// is_running=0 over the roster's current instance).
// MUTANT: drop t.Retire() from RemoveTrader → the removed instance runs → RED.
func TestRemoveTraderRetiresAStartingInstance(t *testing.T) {
	tm := NewTraderManager()
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", nil, false)
	tm.SetTraderForTest("t1", at)

	tm.RemoveTrader("t1") // request B: "stopped" (is_running false) → removed

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- errors.New("the removed instance RAN (it got past the start and panicked on the bare fixture)")
			}
		}()
		done <- at.Run() // request A's goroutine finally runs
	}()
	select {
	case err := <-done:
		if !errors.Is(err, trader.ErrTraderInstanceRetired) {
			t.Fatalf("a removed (retired) instance must never run, got %v", err)
		}
		if !errors.Is(err, trader.ErrTraderAlreadyRunning) {
			t.Fatalf("the refusal must be the no-op sentinel family, got %v", err)
		}
	case <-time.After(5 * time.Second):
		at.Stop()
		t.Fatal("the removed instance RAN (its Run did not return)")
	}
	if r, _ := at.GetStatus()["is_running"].(bool); r {
		t.Fatal("the refused instance must not read running")
	}
	if _, err := tm.GetTrader("t1"); err == nil {
		t.Fatal("RemoveTrader must still drop the instance from the roster")
	}
}
