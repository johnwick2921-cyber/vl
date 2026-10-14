package trader

import (
	"fmt"
	"sync"
	"time"

	"vl/telemetry"
)

// MENTOR-NY item 3 — ONE live run per trader_id.
//
// 10-13 16:35:42–48 (data/vl_2026-10-13.log 19112–19277, release #18): two
// start requests for hoang overlapped. Request A removed the stopped instance,
// loaded AT1 and spawned AT1.Run(); request B read AT1 as "stopped" (its Run
// goroutine had not set is_running yet), removed it from the roster WITHOUT
// stopping it (RemoveTrader stops only a running trader), loaded AT2 and
// started it — twice (the auto-start of an is_running=1 row + the handler's
// own Run). AT1 kept running orphaned (no roster entry, no Stop could reach
// it); AT2's Runs were refused by the ordered-execution owner check, but Run
// had already started AT2's armed event loop, which never stopped. Result: two
// live mentor evaluators for one trader_id (the alternating funnel lines, bars
// 44 at 17:43:00 and bars 22 at 17:43:38) until the 18:57 restart.
//
// The rule enforced here, at the one place every start path goes through
// (AutoTrader.Run — the API start, the update reload, the auto-start and the
// agent tool all call it):
//
//   - a run CLAIMS its trader_id before it starts anything;
//   - an instance REMOVED from the roster (RemoveTrader → Retire) never runs
//     again: its Run is refused before any side effect;
//   - a NEW instance never runs beside an OLD one: the old run is stopped and
//     its Run must have EXITED before the new run starts;
//   - every refused start is logged and counted (telemetry gate blocks).

// ErrTraderInstanceRetired — the instance was removed from the roster (a reload
// replaced it); it never runs. It wraps ErrTraderAlreadyRunning so every caller
// treats it as the no-op it is (the roster's CURRENT instance owns the trader
// and its is_running bit) and never writes is_running=0 over it.
var ErrTraderInstanceRetired = fmt.Errorf("%w: this instance was removed from the roster (a reload replaced it) and never runs", ErrTraderAlreadyRunning)

// ErrTraderOldInstanceAlive — the previous run of this trader_id did not exit
// within runOwnerHandoffWait after it was stopped; the new run is refused
// rather than run beside it. Wraps ErrTraderAlreadyRunning: the old run IS the
// live one, so no caller may write is_running=0 for it.
var ErrTraderOldInstanceAlive = fmt.Errorf("%w: the previous run of this trader did not exit after Stop — refusing to start a second one beside it", ErrTraderAlreadyRunning)

// runOwnerHandoffWait bounds how long a new run waits for the old run to exit
// after Stop. A run exits at its next loop turn, which can sit behind one
// in-flight cycle (an AI read can take minutes), hence the generous bound.
var runOwnerHandoffWait = 10 * time.Minute

type runOwnerSlot struct {
	at   *AutoTrader
	seq  uint64        // the claiming Run's sequence (a same-object restart is a new seq)
	done chan struct{} // closed when that Run releases (after it has fully exited)
}

var runOwners = struct {
	sync.Mutex
	m   map[string]*runOwnerSlot
	seq uint64
}{m: map[string]*runOwnerSlot{}}

// Retire marks this instance as removed from the roster. A Run that has not
// claimed yet is refused (ErrTraderInstanceRetired) before any side effect; a
// running instance is stopped by the caller (RemoveTrader keeps its existing
// stop-if-running). Idempotent.
func (at *AutoTrader) Retire() {
	if at == nil {
		return
	}
	runOwners.Lock()
	at.retired = true
	runOwners.Unlock()
}

// claimRunOwner makes this Run the one live run of at.id. It returns the
// release func the Run defers (called after the Run has fully exited), or the
// refusal error.
func (at *AutoTrader) claimRunOwner() (release func(), err error) {
	deadline := time.Now().Add(runOwnerHandoffWait)
	for {
		runOwners.Lock()
		if at.retired {
			runOwners.Unlock()
			telemetry.IncGateBlock(at.id, "trader_run_refused_retired")
			at.logWarnf("⛔ run refused: this %q instance was removed from the roster by a reload — the roster's current instance owns trader %s; nothing was started (counter trader_run_refused_retired)", at.name, at.id)
			return nil, ErrTraderInstanceRetired
		}
		slot := runOwners.m[at.id]
		if slot == nil {
			runOwners.seq++
			mine := &runOwnerSlot{at: at, seq: runOwners.seq, done: make(chan struct{})}
			runOwners.m[at.id] = mine
			runOwners.Unlock()
			return func() { releaseRunOwner(at.id, mine) }, nil
		}
		prev, done := slot.at, slot.done
		runOwners.Unlock()

		if prev != at {
			// One owner: the OLD run is stopped BEFORE the new one starts.
			telemetry.IncGateBlock(at.id, "trader_run_replaced_old_instance")
			at.logWarnf("⏹ a previous run of trader %s (%q) is still live — stopping it and waiting for it to exit before this run starts (counter trader_run_replaced_old_instance)", at.id, at.name)
			go prev.Stop() // Stop waits for that Run's monitor; never block the claim on it
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-done:
			t.Stop()
			continue // re-check under the lock: retired? another claimant?
		case <-t.C:
			telemetry.IncGateBlock(at.id, "trader_run_refused_old_instance_alive")
			at.logErrorf("⛔ run refused: the previous run of trader %s did not exit within %s after Stop — refusing to start a second run beside it (counter trader_run_refused_old_instance_alive)", at.id, runOwnerHandoffWait)
			return nil, ErrTraderOldInstanceAlive
		}
	}
}

func releaseRunOwner(id string, mine *runOwnerSlot) {
	runOwners.Lock()
	defer runOwners.Unlock()
	if cur := runOwners.m[id]; cur == mine {
		delete(runOwners.m, id)
		close(mine.done)
	}
}

// liveRunOwner reports the instance that currently owns id's run (nil = none).
// Read-only; tests and the health surface use it.
func liveRunOwner(id string) *AutoTrader {
	runOwners.Lock()
	defer runOwners.Unlock()
	if s := runOwners.m[id]; s != nil {
		return s.at
	}
	return nil
}
