package trader

import (
	"sync"
	"sync/atomic"
)

// W26 deadman-escalation thresholds (bug fix, ships ON — no knob): a blocked
// episode that has not reconciled after this many CONSECUTIVE failed probes, or
// has stood for this long, fires ONE P0 `dead_man_reconcile_stuck` alert per
// episode. Entries stay BLOCKED regardless — the alert is an operator signal,
// never an auto-unblock.
const (
	deadManStuckProbeMin = 3
	deadManStuckAgeMs    = int64(5 * 60_000)
)

// B5 — dead-man's watchdog. On an NT8 TCP disconnect / missed heartbeats, NEW
// entries are blocked; a reconnect alone does NOT resume trading — the link must
// first pass a clean positions/orders reconciliation (so the bot never opens on top
// of a state it hasn't re-verified after a gap). Then it auto-resumes.
//
// State machine (pure + testable):
//
//	dmLive ──disconnect──▶ dmDisconnected ──reconnect──▶ dmAwaitingReconcile ──reconciled──▶ dmLive
//	  ▲                         │                              │
//	  └──────────── flap back down (reconnect lost) ───────────┘
//
// Entries are blocked in dmDisconnected and dmAwaitingReconcile. Exits /
// open-position management are never gated by the watchdog.

type deadManState int

const (
	dmLive              deadManState = iota // connected + reconciled → entries allowed
	dmDisconnected                          // link down → entries blocked
	dmAwaitingReconcile                     // reconnected but not yet reconciled → blocked
)

// state is atomic (W-EXEC-TRUTH W0 Q15): runCycle is the ONLY writer (step),
// but the admission gate reads it from Picture's live-bar goroutine too.
//
// Release #22 OPS-SAFETY (EXECUTION F-3): a boolean sampled once per decision
// cycle could not see a disconnect + reconnect that both landed between two
// samples — on 10-14 trader 12345, holding position #650, never saw either of
// the two forced link drops at 00:21:35 / 00:22:43. The watchdog now also
// compares the connection EPOCH (the TCP server's accept sequence): a changed
// epoch on a live link is a gap we missed, handled exactly like a reconnect.
// It is driven from monitorTick (every trader, every 60 s) as well as
// runCycle, so step() is serialized by mu.
type deadManWatchdog struct {
	st atomic.Int32

	mu        sync.Mutex // serializes step(): runCycle and monitorTick both drive it
	epoch     uint64     // accept sequence of the link last seen connected
	haveEpoch bool

	// W26 escalation state (guarded by mu; written inside step/stepEpoch, read
	// by escalation()/HealthSnapshot()).
	blockedSinceMs int64 // Unix ms the current blocked episode began; 0 = not blocked
	failedProbes   int   // consecutive failed reconcile probes in this episode
	stuckAlerted   bool  // the P0 stuck alert has fired for this episode
}

// deadManEscalation is the snapshot of the W26 escalation state.
type deadManEscalation struct {
	BlockedSinceMs int64 // 0 = not blocked
	FailedProbes   int
	Alerted        bool
}

// deadManHealth is the per-trader /api/health `dead_man` field (READ values).
type deadManHealth struct {
	Blocked bool  `json:"blocked"`
	SinceMs int64 `json:"since_ms"` // 0 = not blocked
	Probes  int   `json:"probes"`
}

// beginEpisode starts a NEW blocked episode at nowMs — called only on a
// transition from dmLive into a blocked state (a drop, or a replaced link
// epoch), under mu. A flap within an episode (down ↔ awaiting) does NOT start
// a new episode: the alert fires once per continuous block, not once per flap.
func (w *deadManWatchdog) beginEpisode(nowMs int64) {
	w.blockedSinceMs = nowMs
	w.failedProbes = 0
	w.stuckAlerted = false
}

// endEpisode clears the escalation state when a clean reconcile resumes.
func (w *deadManWatchdog) endEpisode() {
	w.blockedSinceMs = 0
	w.failedProbes = 0
	w.stuckAlerted = false
}

// bumpFailedProbe records one failed reconcile probe (called only while the
// link is up and entries are blocked, under mu).
func (w *deadManWatchdog) bumpFailedProbe(nowMs int64) {
	if w.blockedSinceMs == 0 {
		w.blockedSinceMs = nowMs
	}
	w.failedProbes++
}

// escalation returns the current escalation snapshot.
func (w *deadManWatchdog) escalation() deadManEscalation {
	w.mu.Lock()
	defer w.mu.Unlock()
	return deadManEscalation{BlockedSinceMs: w.blockedSinceMs, FailedProbes: w.failedProbes, Alerted: w.stuckAlerted}
}

// markStuckAlerted records that the P0 alert has fired for the current episode.
func (w *deadManWatchdog) markStuckAlerted() {
	w.mu.Lock()
	w.stuckAlerted = true
	w.mu.Unlock()
}

// HealthSnapshot is the /api/health `dead_man` field (READ values, never
// literal). blocked = entries are currently blocked; since_ms = the current
// blocked episode's start (0 = not blocked); probes = consecutive failed
// reconcile probes in the current episode.
func (w *deadManWatchdog) HealthSnapshot() deadManHealth {
	w.mu.Lock()
	defer w.mu.Unlock()
	return deadManHealth{Blocked: w.get() != dmLive, SinceMs: w.blockedSinceMs, Probes: w.failedProbes}
}

func (w *deadManWatchdog) get() deadManState  { return deadManState(w.st.Load()) }
func (w *deadManWatchdog) set(s deadManState) { w.st.Store(int32(s)) }

// observe updates the state from the current link status. Returns (newState,
// changed) so the caller can log transitions.
func (w *deadManWatchdog) observe(connected bool) (deadManState, bool) {
	prev := w.get()
	switch prev {
	case dmLive:
		if !connected {
			w.set(dmDisconnected)
		}
	case dmDisconnected:
		if connected {
			w.set(dmAwaitingReconcile) // reconnected → must reconcile before resuming
		}
	case dmAwaitingReconcile:
		if !connected {
			w.set(dmDisconnected) // flapped back down
		}
	}
	cur := w.get()
	return cur, cur != prev
}

// reconciled marks a clean positions/orders reconciliation complete. It resumes
// trading ONLY from the awaiting-reconcile state (a reconcile while merely live is
// a no-op). Returns true if it resumed.
func (w *deadManWatchdog) reconciled() bool {
	if w.get() == dmAwaitingReconcile {
		w.set(dmLive)
		return true
	}
	return false
}

// entriesBlocked reports whether NEW entries are currently blocked.
func (w *deadManWatchdog) entriesBlocked() bool {
	return w.get() != dmLive
}

// entriesBlockedLive is entriesBlocked plus a LIVE sample of the link (m425b
// S5). The cached state is advanced only every ~60s (runCycle / monitorTick),
// so a drop — or a drop AND reconnect inside that window — left admission
// reading dmLive. This also refuses when (a) the link is down right now, or
// (b) it is up under a different connection epoch than the last sample (the
// link was replaced unseen). It READS only: the state machine itself still
// advances in stepEpoch, which will log, sweep and begin the episode.
// connKnown=false (non-NT / no server) is exactly entriesBlocked().
func (w *deadManWatchdog) entriesBlockedLive(connected, connKnown bool, epoch uint64, epochKnown bool) bool {
	if w.entriesBlocked() {
		return true
	}
	if !connKnown {
		return false
	}
	if !connected {
		return true
	}
	if epochKnown {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.haveEpoch && epoch != w.epoch {
			return true
		}
	}
	return false
}

// watchdogEvent is what a single per-cycle step() produced, so the caller can log
// and act (cancel unfilled entries on reconnect, etc.). Only transitions emit a
// non-none event.
type watchdogEvent int

const (
	wdNone        watchdogEvent = iota // no transition this cycle
	wdWentDown                         // live → disconnected: entries now blocked
	wdReconnected                      // disconnected → awaiting reconcile: sweep unfilled, stay blocked
	wdResumed                          // awaiting reconcile → live: clean reconciliation, entries resumed
)

// stepEpoch is step() plus the connection-epoch check. epochKnown=false (no
// server / non-NT) is exactly step(). A link that is connected now under a
// DIFFERENT epoch than the last connected sample was replaced between two
// samples: it went down and came back unseen. From dmLive that is
// wdReconnected (entries block, unfilled entries swept, protection re-asked,
// reconcile on a LATER step); from dmAwaitingReconcile it restarts the wait.
// missed reports that the event came from the epoch, for the log line.
func (w *deadManWatchdog) stepEpoch(connected bool, epoch uint64, epochKnown bool, nowMs int64, reconcileOK func() bool) (ev watchdogEvent, missed bool, prevEpoch uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prevEpoch = w.epoch
	replaced := epochKnown && connected && w.haveEpoch && epoch != w.epoch
	if epochKnown && connected {
		w.epoch, w.haveEpoch = epoch, true
	}
	if replaced {
		switch w.get() {
		case dmLive, dmAwaitingReconcile:
			if w.get() == dmLive {
				w.beginEpisode(nowMs) // a NEW block begins (live → blocked via epoch)
			}
			w.set(dmAwaitingReconcile)
			return wdReconnected, true, prevEpoch
		}
		// dmDisconnected: the ordinary reconnect path below sees it.
	}
	return w.step(connected, nowMs, reconcileOK), false, prevEpoch
}

// step advances the watchdog one trading cycle from the current link status and a
// reconcile probe, returning the event to log/act on. Semantics:
//
//   - A drop (connected=false) → wdWentDown; entries block.
//   - A reconnect → wdReconnected; the caller sweeps unfilled entries, but entries
//     STAY blocked — reconciliation is deferred to a LATER cycle so the link can
//     re-sync before we probe (never resume on the same cycle as the reconnect).
//   - While awaiting reconcile with the link up, a clean probe (reconcileOK()==true)
//     → wdResumed; entries resume. A dirty/failed probe keeps entries blocked and
//     retries next cycle.
//
// reconcileOK is invoked ONLY when the link is up and entries are already blocked
// (awaiting reconciliation), never on the reconnect cycle itself.
func (w *deadManWatchdog) step(connected bool, nowMs int64, reconcileOK func() bool) watchdogEvent {
	prev := w.get()
	if cur, changed := w.observe(connected); changed {
		switch cur {
		case dmDisconnected:
			if prev == dmLive {
				w.beginEpisode(nowMs) // a NEW block begins
			}
			return wdWentDown
		case dmAwaitingReconcile:
			// prev was dmDisconnected — a within-episode reconnect, the same
			// continuous block; the episode is not restarted.
			return wdReconnected // reconnected — defer reconcile to a later cycle
		}
		return wdNone
	}
	// No transition; if still awaiting reconcile over a live link, try to reconcile.
	if w.entriesBlocked() && connected {
		if reconcileOK != nil && reconcileOK() {
			if w.reconciled() {
				w.endEpisode()
				return wdResumed
			}
			return wdNone
		}
		w.bumpFailedProbe(nowMs)
	}
	return wdNone
}
