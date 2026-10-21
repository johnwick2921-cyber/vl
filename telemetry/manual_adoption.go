// R22 PNL-TRUTH (owner ruling R1, 2026-10-14) — manual NT8 adoptions.
//
// The reconcile loop adopts an NT8 position it has no open row for. When no
// fill-ring signal and no in-window armed fill of this trader explains it, the
// row is flagged origin=manual AT ADOPTION and excluded from the bot record.
// Each such flag is logged and counted here; a flag that FAILED to write is
// counted separately, so a manual row that slipped into the bot record is never
// silent. Counters RECORD (class 35). Reading them never resets them.

package telemetry

import "sync/atomic"

var (
	manualAdopted        atomic.Int64
	manualAdoptFlagFails atomic.Int64
	manualAdoptKeptBot   atomic.Int64
)

// IncManualAdoptKeptBot (RT-11) counts one adoption with no fill-ring or
// in-window armed-fill evidence that was KEPT bot because a live bot order of
// that trader on that side could not be ruled out (unknown is not manual).
// Returns the new count.
func IncManualAdoptKeptBot() int64 { return manualAdoptKeptBot.Add(1) }

// ManualAdoptKeptBotCount returns the kept-bot adoptions since boot.
func ManualAdoptKeptBotCount() int64 { return manualAdoptKeptBot.Load() }

// IncManualAdopted counts one adopted NT8 position flagged origin=manual.
func IncManualAdopted() { manualAdopted.Add(1) }

// IncManualAdoptFlagFailed counts one adoption that SHOULD have been flagged
// manual but whose write failed — that row is still in the bot record.
func IncManualAdoptFlagFailed() { manualAdoptFlagFails.Add(1) }

// ManualAdoptionCounts returns (flagged, flag-write failures) since boot.
func ManualAdoptionCounts() (flagged, failed int64) {
	return manualAdopted.Load(), manualAdoptFlagFails.Load()
}
