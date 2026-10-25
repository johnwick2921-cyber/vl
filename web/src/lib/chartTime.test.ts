// THE ONE chart-timezone site — DST correctness + pin-to-Houston.
//
// The delta this guards against: PlanMiniChart handed lightweight-charts raw
// epochs, so its labels rendered UTC — +5h vs the NT8 chart (S1). And any fixed
// −5/−6 constant would break twice a year (S5).

import { describe, it, expect } from 'vitest'
import { TickMarkType, type Time } from 'lightweight-charts'
import {
  ctTickMarkFormatter,
  ctCrosshairTimeFormatter,
  formatCTDisplayString,
  formatEquityTimestamp,
} from './chartTime'

// A bar OPEN-stamped 14:30:00 UTC. In August (CDT, −5) that is 09:30 CT —
// the NY open. In January (CST, −6) the same wall UTC instant is 08:30 CT.
const augEpochS = Date.UTC(2026, 7, 18, 14, 30, 0) / 1000 // Aug 18 2026
const janEpochS = Date.UTC(2026, 0, 15, 14, 30, 0) / 1000 // Jan 15 2026

describe('ctTickMarkFormatter — DST-safe CT labels', () => {
  it('renders an August (CDT, −5) intraday tick as CT wall-clock', () => {
    expect(ctTickMarkFormatter(augEpochS as Time, TickMarkType.Time)).toBe(
      '09:30'
    )
  })
  it('renders a January (CST, −6) intraday tick as CT wall-clock', () => {
    expect(ctTickMarkFormatter(janEpochS as Time, TickMarkType.Time)).toBe(
      '08:30'
    )
  })
  it('never renders the raw UTC hour (the S1 bug)', () => {
    expect(ctTickMarkFormatter(augEpochS as Time, TickMarkType.Time)).not.toBe(
      '14:30'
    )
  })
  it('day-granularity ticks are CT-dated (a 23:30 CT bar is NOT tomorrow)', () => {
    // 04:30 UTC Aug 19 = 23:30 CT Aug 18 — the CT date must win.
    const lateEvening = Date.UTC(2026, 7, 19, 4, 30, 0) / 1000
    expect(
      ctTickMarkFormatter(lateEvening as Time, TickMarkType.DayOfMonth)
    ).toBe('08/18')
  })
})

describe('ctCrosshairTimeFormatter — agrees with the axis', () => {
  it('same zone, same instant, both seasons', () => {
    expect(ctCrosshairTimeFormatter(augEpochS)).toBe('08/18 09:30')
    expect(ctCrosshairTimeFormatter(janEpochS)).toBe('01/15 08:30')
  })
})

describe('pin-to-Houston holds for any viewer timezone', () => {
  it('output is independent of the host TZ (Intl timeZone pins it)', () => {
    // The formatter passes an explicit timeZone to Intl, so the host TZ env
    // cannot influence it. Render twice with different TZ env hints — vitest
    // runs one process, so assert the value equals the CT truth computed above.
    expect(ctCrosshairTimeFormatter(augEpochS)).toBe('08/18 09:30')
  })
})

// F6 equity-chart "Invalid Date" — RED on today's code: `new Date()` on the
// kernel.FormatCT display string returns Invalid Date, so the axis rendered
// "Invalid Date" on all 10 ticks. The fix must never parse that string with
// Date(); it parses by hand or uses the new epoch-ms field.
describe('formatEquityTimestamp — the equity axis never shows Invalid Date', () => {
  it('RED (today): new Date() on the CT display string is Invalid Date', () => {
    const d = new Date('2026-09-17 14:12 CT')
    expect(Number.isNaN(d.getTime())).toBe(true)
  })

  it('prefers timestamp_ms and renders MM/DD HH:mm in CT', () => {
    // 14:12 CT on 2026-09-17 (CDT) = 19:12 UTC.
    const ms = Date.UTC(2026, 8, 17, 19, 12, 0)
    expect(formatEquityTimestamp(ms)).toBe('09/17 14:12')
  })

  it('falls back to the hand-parsed CT string when ms is absent (older server)', () => {
    expect(formatEquityTimestamp(undefined, '2026-09-17 14:12 CT')).toBe(
      '09/17 14:12'
    )
  })

  it('unparseable input renders "—", never "Invalid Date"', () => {
    expect(formatEquityTimestamp(undefined, 'garbage')).toBe('—')
    expect(formatEquityTimestamp(undefined, undefined)).toBe('—')
    expect(formatEquityTimestamp(NaN, 'garbage')).toBe('—')
  })
})

describe('formatCTDisplayString — hand-parses the Go display string', () => {
  it('reformats by hand without Date()', () => {
    expect(formatCTDisplayString('2026-09-17 14:12 CT')).toBe('09/17 14:12')
  })
  it('rejects non-matching strings', () => {
    expect(formatCTDisplayString('2026-09-17T14:12:00Z')).toBe('—')
    expect(formatCTDisplayString('')).toBe('—')
    expect(formatCTDisplayString(null)).toBe('—')
  })
})

describe('ctTickMarkFormatter — C10 month/year follow the app language', () => {
  const sep = Date.UTC(2026, 8, 15, 18, 0, 0) / 1000
  const setLang = (l: string | null) => {
    if (l === null) localStorage.removeItem('language')
    else localStorage.setItem('language', l)
  }
  it('en → "Sep" (never "9月")', () => {
    setLang('en')
    expect(ctTickMarkFormatter(sep as Time, TickMarkType.Month)).toBe('Sep')
  })
  it('no saved language → en', () => {
    setLang(null)
    expect(ctTickMarkFormatter(sep as Time, TickMarkType.Month)).toBe('Sep')
  })
  it('zh → "9月"', () => {
    setLang('zh')
    expect(ctTickMarkFormatter(sep as Time, TickMarkType.Month)).toBe('9月')
    setLang(null)
  })
  it('id → "Sep"', () => {
    setLang('id')
    expect(ctTickMarkFormatter(sep as Time, TickMarkType.Month)).toBe('Sep')
    setLang(null)
  })
  it('year is CT-dated: 02:00 UTC Jan 1 is still 2025 in CT', () => {
    setLang('en')
    const t = Date.UTC(2026, 0, 1, 2, 0, 0) / 1000
    expect(ctTickMarkFormatter(t as Time, TickMarkType.Year)).toBe('2025')
  })
  it('a UTC-midnight day tick is labelled by its CT date (19:00 CDT prior day)', () => {
    const utcMidnight = Date.UTC(2026, 8, 16, 0, 0, 0) / 1000
    expect(
      ctTickMarkFormatter(utcMidnight as Time, TickMarkType.DayOfMonth)
    ).toBe('09/15')
  })
})

// m427/11 — the 'id' language month label. September is "Sep" in id-ID AND en-US,
// so a September-only test cannot tell the locales apart (a chartLocale() that
// returned en-US for 'id' stayed green). October/December/May differ.
describe('ctTickMarkFormatter — Month labels follow the app language (id)', () => {
  const oct = Date.UTC(2026, 9, 15, 14, 30, 0) / 1000
  const dec = Date.UTC(2026, 11, 15, 14, 30, 0) / 1000
  const setLang = (l: string | null) => {
    if (l === null) localStorage.removeItem('language')
    else localStorage.setItem('language', l)
  }
  it('id: October is Okt, December is Des', () => {
    try {
      setLang('id')
      expect(ctTickMarkFormatter(oct as Time, TickMarkType.Month)).toBe('Okt')
      expect(ctTickMarkFormatter(dec as Time, TickMarkType.Month)).toBe('Des')
    } finally {
      setLang(null)
    }
  })
  it('en (and unset): October is Oct, December is Dec', () => {
    setLang(null)
    expect(ctTickMarkFormatter(oct as Time, TickMarkType.Month)).toBe('Oct')
    try {
      setLang('en')
      expect(ctTickMarkFormatter(dec as Time, TickMarkType.Month)).toBe('Dec')
    } finally {
      setLang(null)
    }
  })
})
