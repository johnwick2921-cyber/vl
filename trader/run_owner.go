package trader

import (
	"fmt"
	"sync"
	"time"

	"vl/store"
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

// runRetryMaxWait bounds how long the armed run retry waits for the old run's
// claim to release (W26 N3). The old run was already given runOwnerHandoffWait
// to exit after Stop and did not; if it never releases, the retry goroutine
// must not leak — it gives up, logged and counted, instead of blocking forever.
var runRetryMaxWait = 10 * time.Minute

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

		if prev != at && at.retryIfFreeOnly.Load() {
			// The auto-retry after a handoff timeout (below) never stops a
			// run that claimed the trader since — that run is NEWER.
			telemetry.IncGateBlock(at.id, "trader_run_retry_skipped")
			at.logWarnf("⏹ run retry skipped: another run of trader %s owns it now (counter trader_run_retry_skipped)", at.id)
			return nil, ErrTraderOldInstanceAlive
		}
		stopped := closedChan
		if prev != at {
			// One owner: the OLD run is stopped BEFORE the new one starts.
			telemetry.IncGateBlock(at.id, "trader_run_replaced_old_instance")
			at.logWarnf("⏹ a previous run of trader %s (%q) is still live — stopping it and waiting for it to exit before this run starts (counter trader_run_replaced_old_instance)", at.id, at.name)
			// Stop waits for that Run's monitor; never block the claim on it.
			// The claim waits for BOTH the old Run's exit (done) AND the end
			// of its Stop teardown (stopped): PR #522 CI (-race) caught a new
			// run installing before the old Stop had unregistered the ordered
			// slot ("slot held by a stale instance of this same trader").
			ch := make(chan struct{})
			stopped = ch
			prev.goNetted("run-owner-stop-old", func() { defer close(ch); prev.Stop() })
		}
		if !waitBoth(done, stopped, time.Until(deadline)) {
			telemetry.IncGateBlock(at.id, "trader_run_refused_old_instance_alive")
			at.logErrorf("⛔ run refused: the previous run of trader %s did not exit within %s after Stop — refusing to start a second run beside it; trader %q has NO evaluator until it does — an automatic retry is armed for the moment it exits (counter trader_run_refused_old_instance_alive)", at.id, runOwnerHandoffWait, at.name)
			at.emitAlert("P0", "trader_down_no_evaluator", store.EpisodeEventID("trader_down_no_evaluator", at.id, time.Now().UnixMilli()),
				fmt.Sprintf("Trader %s is DOWN: no evaluator", at.name),
				fmt.Sprintf("Trader %q (%s): the old run did not stop within %s, so the new run was refused (never two evaluators). It retries automatically the moment the old run exits; Start it again if it stays down.", at.name, at.id, runOwnerHandoffWait))
			at.armRunRetry(done)
			return nil, ErrTraderOldInstanceAlive
		}
		continue // re-check under the lock: retired? another claimant?
	}
}

// closedChan is an already-closed channel ("nothing to wait for").
var closedChan = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// waitBoth waits for a and b to close within d (at least 1 ms). Returns false on
// timeout.
func waitBoth(a, b <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		d = time.Millisecond
	}
	t := time.NewTimer(d)
	defer t.Stop()
	for a != nil || b != nil {
		select {
		case <-a:
			a = nil
		case <-b:
			b = nil
		case <-t.C:
			return false
		}
	}
	return true
}

// armRunRetry (DS-104 P2-2): after a handoff timeout the trader would stay
// down until someone clicks Start. Instead, once the old run's claim is
// released, THIS instance retries its Run — unless it was retired (a reload
// replaced it), a Stop was asked for during the wait (W26 N4), the wait ran
// past runRetryMaxWait (W26 N3), or another run claimed the trader first
// (retryIfFreeOnly: the retry never stops a newer run). One evaluator at a
// time either way: the retry goes through the same claim.
func (at *AutoTrader) armRunRetry(done <-chan struct{}) {
	if !at.runRetryArmed.CompareAndSwap(false, true) {
		return
	}
	gen := at.currentStopGen()
	at.goNetted("run-owner-retry", func() {
		defer at.runRetryArmed.Store(false)
		select {
		case <-done:
		case <-time.After(runRetryMaxWait):
			// W26 N3 — the old run never released its claim; the goroutine must
			// not leak. Give up, logged and counted (a Start still works).
			telemetry.IncGateBlock(at.id, "trader_run_retry_gave_up")
			at.logWarnf("⏹ run retry gave up: the old run of trader %s did not exit within %s of the handoff timeout — no automatic retry was attempted (counter trader_run_retry_gave_up); Start it again to run it", at.id, runRetryMaxWait)
			return
		}
		if at.currentStopGen() != gen {
			// W26 N4 — the operator asked for a Stop while the retry waited. The
			// refused instance has isRunning=false, so Stop() does not tear a run
			// down, but it still bumps stopGen — this cancels the armed retry.
			telemetry.IncGateBlock(at.id, "trader_run_retry_stopped")
			at.logWarnf("⏹ run retry cancelled: Stop was requested for trader %s while it waited — not restarting it (counter trader_run_retry_stopped)", at.id)
			return
		}
		runOwners.Lock()
		retired := at.retired
		runOwners.Unlock()
		if retired {
			at.logWarnf("⏹ run retry dropped: this %q instance was retired while it waited", at.name)
			return
		}
		at.retryIfFreeOnly.Store(true)
		defer at.retryIfFreeOnly.Store(false)
		telemetry.IncGateBlock(at.id, "trader_run_retried")
		at.logWarnf("🔁 the old run of trader %s exited — retrying this run now (counter trader_run_retried)", at.id)
		if err := at.Run(); err != nil {
			at.logWarnf("🔁 run retry for trader %s ended: %v", at.id, err)
		}
	})
}

// currentStopGen reads stopGen under isRunningMutex (Stop writes it there).
func (at *AutoTrader) currentStopGen() uint64 {
	at.isRunningMutex.RLock()
	defer at.isRunningMutex.RUnlock()
	return at.stopGen
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
