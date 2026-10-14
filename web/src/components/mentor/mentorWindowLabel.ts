// UI-GUIDE-TRUTH T30 (release #22): the card printed "open" for a window of
// -1 (no window — trader/mentor_tick.go mentorWindowActive returns active for
// minutes <= 0) and "disabled" for any closed window that had not yet ended
// (before the start, or an unparseable start where every entry is refused).
// One honest word per state the API can tell apart.
//
// UG-4 (release #24): "no window" never meant "entries at any hour". Two
// clock rules bind every mentor trader whatever the window says:
//   - trader/mentor_mode.go mentorPastDailyHaltCutoff — no NEW mentor entry
//     from 15:45 CT until the 17:00 CT reopen;
//   - trader/auto_trader_clock.go enforceEODFlatAt (+ armed_executor.go
//     cancelIntradayMentorArms) — from the session flat (NY 14:45 CT; a
//     registered half-day pulls it earlier) until 17:00 every INTRADAY mentor
//     order is cancelled (one authored then dies at once) and an open intraday
//     position is flattened. Only the SWING4H is exempt.
export const MENTOR_NO_WINDOW_LABEL =
  'no time window — but no new entry 15:45–17:00 CT, and the EOD flat (14:45 CT; earlier on a half-day) cancels intraday orders until 17:00'

export function mentorWindowLabel(w: {
  minutes: number
  active: boolean
  ended: boolean
}): string {
  if (w.minutes <= 0) return MENTOR_NO_WINDOW_LABEL
  if (w.active) return 'open'
  if (w.ended) return 'ended'
  return 'closed (no new entries now)'
}
