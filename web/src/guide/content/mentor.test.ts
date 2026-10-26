import { describe, expect, it } from 'vitest'
import { mentor } from './mentor'

// FIX-LESSON-LINKS: every mentor guide claim carries a lesson link OR an
// explicit owner/engineering-ruling label. The owner reads the guide and must
// be able to open the exact lesson (or see "Owner setting — X" /
// "Engineering rule — Y, not from the course") for every rule and number.
//
// A blank source → the test fails (named RED). A vague phrase that names no
// lesson, timestamp, ruling or "not from the course" marker → fails too.

// A source is lesson-shaped when it names a course day (D x.y / Ngày), an
// extra (X n), a timestamp (@mm:ss), or a slide/table from the written spec.
const LESSON_SOURCE =
  /(Ngày|D\d+\.\d+|X\d+|\d{1,2}:\d{2}|slide|written spec|\btable\b)/i
// A source is ruling-shaped when it names an owner setting or an engineering
// rule that is NOT from the course.
const RULING_SOURCE =
  /(Owner setting|Engineering rule|ruling|not from the course|code constant|CTO|B\d{1,2}|G\d|L\d|R\d)/i

const hasSource = (s: string): boolean =>
  LESSON_SOURCE.test(s) || RULING_SOURCE.test(s)

describe('mentor guide — every claim card carries a lesson-or-ruling source', () => {
  it('every callout item cites a lesson or a ruling', () => {
    for (const block of mentor.blocks) {
      if (block.kind !== 'callout') continue
      for (const item of block.items) {
        const src = item.cite ?? ''
        expect(
          src.trim().length,
          `callout "${item.title}" has an empty cite`
        ).toBeGreaterThan(0)
        expect(
          hasSource(src),
          `callout "${item.title}" cite has no lesson/ruling source: "${src}"`
        ).toBe(true)
      }
    }
  })

  it('every setup card tags its lesson', () => {
    for (const block of mentor.blocks) {
      if (block.kind !== 'cards') continue
      for (const card of block.cards) {
        const src = card.tag ?? ''
        expect(
          src.trim().length,
          `card "${card.title}" has an empty tag`
        ).toBeGreaterThan(0)
        expect(
          hasSource(src),
          `card "${card.title}" tag has no lesson source: "${src}"`
        ).toBe(true)
      }
    }
  })

  it('every knob names a lesson, a ruling, or "not from the course"', () => {
    for (const block of mentor.blocks) {
      if (block.kind !== 'knobs') continue
      for (const knob of block.knobs) {
        const src = knob.recommended ?? ''
        expect(
          src.trim().length,
          `knob "${knob.label}" has an empty recommended`
        ).toBeGreaterThan(0)
        expect(
          hasSource(src),
          `knob "${knob.label}" recommended has no lesson/ruling source: "${src}"`
        ).toBe(true)
      }
    }
  })
})

describe('mentor guide — source audit listing', () => {
  it('prints every claim missing a lesson-or-ruling source', () => {
    const missing: string[] = []
    for (const block of mentor.blocks) {
      if (block.kind === 'callout') {
        for (const item of block.items) {
          if (!hasSource(item.cite ?? ''))
            missing.push(`callout: ${item.title}`)
        }
      }
      if (block.kind === 'cards') {
        for (const card of block.cards) {
          if (!hasSource(card.tag ?? '')) missing.push(`card: ${card.title}`)
        }
      }
      if (block.kind === 'knobs') {
        for (const knob of block.knobs) {
          if (!hasSource(knob.recommended ?? ''))
            missing.push(`knob: ${knob.label}`)
        }
      }
    }

    console.log('MISSING SOURCES:\n' + missing.join('\n'))
    expect(missing, 'every claim must carry a lesson-or-ruling source').toEqual(
      []
    )
  })
})

// c52 W12 (docs sweep): the guide must not state again what the code and the
// course disproved, and the parked course items must be disclosed as NOT built.
describe('mentor guide — c52 W12 doc sweep', () => {
  const all = JSON.stringify(mentor)
  const STALE: Array<[string, string]> = [
    [
      'Never between two trigger lines',
      'the zone is FTGL to the buy line, there is ONE trigger line',
    ],
    ['Line partials (deferred)', 'one runner-partial line IS built (mode B)'],
    ['deferred to release #31', 'release #31 shipped the runner partial'],
    [
      'buổi tối mà nó đi chậm thì đánh khung 5 phút thôi',
      'not a transcript sentence; X11 @18:03 says "Trade khung 5 phút only"',
    ],
    [
      'largest 1m candle of the last 30',
      'the key-level pair rule reads the CURRENT closed 1m candle',
    ],
    [
      'that rule is NOT built in #25',
      'the fresh RTH pair is built (m429, default ON)',
    ],
    [
      '2m and 5m Heikin Ashi are 2025 extras',
      '5mha is the two-swing algorithm on HA, not the X10 zone',
    ],
  ]
  it.each(STALE)('does not say "%s" (%s)', (phrase) => {
    expect(all.includes(phrase)).toBe(false)
  })

  it('discloses every parked course item as not built', () => {
    const callout = mentor.blocks.find(
      (b) => b.kind === 'callout' && b.title.startsWith('Not built')
    )
    expect(callout, 'the "Not built" callout is missing').toBeTruthy()
    if (!callout || callout.kind !== 'callout') return
    const titles = callout.items.map((i) => i.title).join(' | ')
    for (const needle of [
      'Sub-minute',
      'line partial',
      'switch the EMA off',
      'wick zone',
      'nine-line',
      'VWAP',
      '2m',
    ]) {
      expect(titles.toLowerCase(), `no not-built note for ${needle}`).toContain(
        needle.toLowerCase()
      )
    }
  })

  it('labels the ours-not-course inferences (doji, two losses, swing re-arm)', () => {
    expect(all).toContain('DOJI candle 1 has no colour')
    expect(all).toContain('the “off for the day” part is OURS')
    expect(all).toContain('re-arms the line is OURS')
  })
})
