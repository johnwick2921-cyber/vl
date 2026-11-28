// m427/11 — cmeNextReopenMs: Friday never reopens at its own 17:00 and the
// search must reach Sunday (two days past a Friday evening).
import { describe, it, expect } from 'vitest'
import { cmeNextReopenMs, formatCT } from './cmeSession'

// 2026-10-23 is a Friday; CDT (UTC-5) until 2026-11-01.
const fri = (hCT: number, mi = 0) => Date.UTC(2026, 9, 23, hCT + 5, mi)
const sunReopen = Date.UTC(2026, 9, 25, 22, 0) // Sun 17:00 CDT

describe('cmeNextReopenMs', () => {
  it('Friday evening (after the 16:00 close) -> Sunday 17:00 CT', () => {
    const got = cmeNextReopenMs(fri(18))
    expect(got).toBe(sunReopen)
    expect(formatCT(got)).toBe('Sun 17:00 CT')
    // the fallback must never be "now" itself
    expect(got).toBeGreaterThan(fri(18))
  })
  it('Friday midday (before 17:00) -> still Sunday, not Friday 17:00', () => {
    expect(cmeNextReopenMs(fri(12))).toBe(sunReopen)
  })
  it('Saturday -> Sunday 17:00 CT', () => {
    expect(cmeNextReopenMs(Date.UTC(2026, 9, 24, 15, 0))).toBe(sunReopen)
  })
  it('Mon-Thu daily break -> same-day 17:00 CT', () => {
    const mon1630 = Date.UTC(2026, 9, 26, 16 + 5, 30)
    expect(formatCT(cmeNextReopenMs(mon1630))).toBe('Mon 17:00 CT')
  })
})
