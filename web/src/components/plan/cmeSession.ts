// m420a — CME session-day helpers for the alert banner and the status banner.
// The CME session-day rolls at 17:00 America/Chicago (the daily reopen), so a
// "current session" alert is one created at or after the most recent 17:00 CT.

const CT = 'America/Chicago'

function ctParts(ms: number) {
  const f = new Intl.DateTimeFormat('en-US', {
    timeZone: CT,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    weekday: 'short',
  })
  const o: Record<string, string> = {}
  for (const p of f.formatToParts(new Date(ms))) o[p.type] = p.value
  return {
    y: +o.year,
    m: +o.month,
    d: +o.day,
    h: +o.hour,
    min: +o.minute,
    wd: o.weekday,
  }
}

// UTC ms of the wall-clock instant y-m-d hh:00 in Chicago.
function ctWallToUtcMs(y: number, m: number, d: number, hh: number): number {
  const naive = Date.UTC(y, m - 1, d, hh, 0, 0)
  // two passes settle the DST offset even when the guess lands across a change
  let guess = naive
  for (let i = 0; i < 2; i++) {
    const p = ctParts(guess)
    const seen = Date.UTC(p.y, p.m - 1, p.d, p.h, p.min, 0)
    guess += naive - seen
  }
  return guess
}

/** Unix seconds of the most recent 17:00 CT at or before `nowMs`. */
export function cmeSessionStartUnix(nowMs: number = Date.now()): number {
  const p = ctParts(nowMs)
  const base = Date.UTC(p.y, p.m - 1, p.d)
  const day = p.h >= 17 ? base : base - 86400000
  const dt = new Date(day)
  return Math.floor(
    ctWallToUtcMs(
      dt.getUTCFullYear(),
      dt.getUTCMonth() + 1,
      dt.getUTCDate(),
      17
    ) / 1000
  )
}

/** Next 17:00 CT strictly after `nowMs` that is a CME reopen (Sun-Fri). */
export function cmeNextReopenMs(nowMs: number = Date.now()): number {
  const p = ctParts(nowMs)
  let day = Date.UTC(p.y, p.m - 1, p.d)
  for (let i = 0; i < 8; i++) {
    const dt = new Date(day + i * 86400000)
    const ms = ctWallToUtcMs(
      dt.getUTCFullYear(),
      dt.getUTCMonth() + 1,
      dt.getUTCDate(),
      17
    )
    // 17:00 CT Saturday never reopens; Friday 17:00 is still closed
    const wd = ctParts(ms).wd
    if (ms > nowMs && wd !== 'Sat' && wd !== 'Fri') return ms
  }
  return nowMs
}

/** "Sun 17:00 CT" */
export function formatCT(ms: number): string {
  const f = new Intl.DateTimeFormat('en-US', {
    timeZone: CT,
    hourCycle: 'h23',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
  const o: Record<string, string> = {}
  for (const p of f.formatToParts(new Date(ms))) o[p.type] = p.value
  return `${o.weekday} ${o.hour}:${o.minute} CT`
}

// "close" and "fill" are informational trade events; old rows stored them as
// P0 but they must never light the persistent red banner.
export function isInfoAlertKind(kind: string): boolean {
  return kind === 'close' || kind === 'fill'
}
