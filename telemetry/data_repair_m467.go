package telemetry

import "sync/atomic"

// m467 (release #62) counters. Counters record, never infer: each moves at the
// one place the event happens, and /api/health reads them.
var (
	feedStillDownReminders atomic.Int64
	dataStillPausedRems    atomic.Int64
	barRepairAsked         atomic.Int64
	barRepairSent          atomic.Int64
	barRepairDeferred      atomic.Int64
	barRepairRefused       atomic.Int64
	barRepairRepaired      atomic.Int64
	barRepairGaveUp        atomic.Int64
	ringPartialReplaced    atomic.Int64
)

// IncFeedStillDownReminder counts one hourly FEED STILL DOWN owner reminder.
func IncFeedStillDownReminder() { feedStillDownReminders.Add(1) }

// FeedStillDownReminders is the count since boot.
func FeedStillDownReminders() int64 { return feedStillDownReminders.Load() }

// IncDataStillPausedReminder counts one hourly STILL PAUSED owner reminder.
func IncDataStillPausedReminder() { dataStillPausedRems.Add(1) }

// DataStillPausedReminders is the count since boot.
func DataStillPausedReminders() int64 { return dataStillPausedRems.Load() }

// IncBarRepairAsked counts one bar-repair item noted (a 1-2 min 1m gap, a
// live_partial row, a cross_frame mismatch).
func IncBarRepairAsked() { barRepairAsked.Add(1) }

// IncBarRepairSent counts one NT8 re-request the repair driver SENT.
func IncBarRepairSent() { barRepairSent.Add(1) }

// IncBarRepairDeferred counts one repair re-request held back (link/feed down,
// CME closed) - said once per attempt.
func IncBarRepairDeferred() { barRepairDeferred.Add(1) }

// IncBarRepairRefused counts one repair re-request the wire refused (budget,
// spacing past its retries, a send error) - said once.
func IncBarRepairRefused() { barRepairRefused.Add(1) }

// IncBarRepairRepaired counts one repair item measured repaired.
func IncBarRepairRepaired() { barRepairRepaired.Add(1) }

// IncBarRepairGaveUp counts one repair item still bad after its schedule.
func IncBarRepairGaveUp() { barRepairGaveUp.Add(1) }

// IncRingPartialReplaced counts ring bars a replay REPLACED: a live bar the
// AddOn never finalised (the partial snapshot), in a period that has closed.
func IncRingPartialReplaced(n int) { ringPartialReplaced.Add(int64(n)) }

// BarRepairCounts is the repair driver's counters since boot.
type BarRepairCounts struct {
	Asked, Sent, Deferred, Refused, Repaired, GaveUp, RingPartialReplaced int64
}

// BarRepairSnapshot reads them.
func BarRepairSnapshot() BarRepairCounts {
	return BarRepairCounts{
		Asked: barRepairAsked.Load(), Sent: barRepairSent.Load(), Deferred: barRepairDeferred.Load(),
		Refused: barRepairRefused.Load(), Repaired: barRepairRepaired.Load(), GaveUp: barRepairGaveUp.Load(),
		RingPartialReplaced: ringPartialReplaced.Load(),
	}
}
