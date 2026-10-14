// UI-GUIDE-TRUTH T30 (release #22): the card printed "open" for a window of
// -1 (no window — trader/mentor_tick.go mentorWindowActive returns active for
// minutes <= 0) and "disabled" for any closed window that had not yet ended
// (before the start, or an unparseable start where every entry is refused).
// One honest word per state the API can tell apart.
export function mentorWindowLabel(w: {
  minutes: number
  active: boolean
  ended: boolean
}): string {
  if (w.minutes <= 0) return 'no window (entries at any hour)'
  if (w.active) return 'open'
  if (w.ended) return 'ended'
  return 'closed (no new entries now)'
}
