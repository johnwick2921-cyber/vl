package trader

import (
	"time"

	"vl/safe"
)

// ── THE DATA-GATE BEATS, LAST AND BOUNDED (m452, release #55) ───────────────
//
// dataIntegrityBeat (m445) and dataTruthBeat (m446/m447) advance an ENTRY-pause
// episode: one WARN + one owner push at the start, one INFO + push at the end.
// They do real reads - the hole tracker and, for data truth, a bar-store read of
// ~32k rows per symbol-minute (storeSwingInputs: ~100 ms warm, up to the SQLite
// busy_timeout of 5 s on a contended DB) - and used to sit at the TOP of
// monitorTick, ahead of checkPositionDrawdown, reconcileProtectionAt,
// dailyLossFlattenAt and enforceEODFlat. A slow or locked store therefore delayed
// protection and flatten, which is the one thing the owner's rule forbids: the
// pause is for NEW entries; exits, stops and protection must never wait on it.
//
// So: they run at the END of the beat, and under a budget. runDataGateBeats runs
// both on one goroutine and waits at most dataBeatBudget for them; past that the
// beat is abandoned to finish on its own (it only writes the episode state) and
// monitorTick returns. A single in-flight flag keeps a slow store from piling one
// goroutine per minute behind it - a beat that finds the previous one still
// running skips this minute and says so ONCE per stall.
//
// A panic inside a beat is recovered by runBeatSafely exactly as before (the
// trader's entries freeze, closes still work).

// dataBeatBudget bounds how long monitorTick waits for the data-gate beats.
// (a var only so the pin can shrink it; production never assigns it).
var dataBeatBudget = 3 * time.Second

// runDataGateBeats runs the two episode beats after the guards, bounded.
func (at *AutoTrader) runDataGateBeats(now time.Time) {
	if at == nil {
		return
	}
	if !at.integrity.beatInFlight.CompareAndSwap(false, true) {
		// the previous minute's beats are still reading: do not stack another
		if at.integrity.beatStallSaid.CompareAndSwap(false, true) {
			at.logWarnf("⏳ data-gate beats: the previous beat is still running (a slow bar-store read) - skipping this minute. Exits, stops and protection do not wait on it")
		}
		return
	}
	at.integrity.beatStallSaid.Store(false)
	done := make(chan struct{})
	safe.GoNet("data-gate-beats", "", func() { // the panic net; runBeatSafely below is the trader's own recovery (entries freeze)
		defer close(done)
		defer at.integrity.beatInFlight.Store(false)
		at.runBeatSafely("data-gate beats", func() {
			at.dataIntegrityBeat(now) // m445: the missing-data pause episode; entries only
			at.dataTruthBeat(now)     // release #55: m446/m447 data-truth episodes; entries only
		})
	})
	t := time.NewTimer(dataBeatBudget)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		at.logWarnf("⏳ data-gate beats: still running after %s - the beat moves on (it only advances the entry-pause episode; exits and protection never wait on it)", dataBeatBudget)
	}
}
