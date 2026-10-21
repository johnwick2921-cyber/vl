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
        expect(src.trim().length, `callout "${item.title}" has an empty cite`).toBeGreaterThan(0)
        expect(hasSource(src), `callout "${item.title}" cite has no lesson/ruling source: "${src}"`).toBe(true)
      }
    }
  })

  it('every setup card tags its lesson', () => {
    for (const block of mentor.blocks) {
      if (block.kind !== 'cards') continue
      for (const card of block.cards) {
        const src = card.tag ?? ''
        expect(src.trim().length, `card "${card.title}" has an empty tag`).toBeGreaterThan(0)
        expect(hasSource(src), `card "${card.title}" tag has no lesson source: "${src}"`).toBe(true)
      }
    }
  })

  it('every knob names a lesson, a ruling, or "not from the course"', () => {
    for (const block of mentor.blocks) {
      if (block.kind !== 'knobs') continue
      for (const knob of block.knobs) {
        const src = knob.recommended ?? ''
        expect(src.trim().length, `knob "${knob.label}" has an empty recommended`).toBeGreaterThan(0)
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
          if (!hasSource(item.cite ?? '')) missing.push(`callout: ${item.title}`)
        }
      }
      if (block.kind === 'cards') {
        for (const card of block.cards) {
          if (!hasSource(card.tag ?? '')) missing.push(`card: ${card.title}`)
        }
      }
      if (block.kind === 'knobs') {
        for (const knob of block.knobs) {
          if (!hasSource(knob.recommended ?? '')) missing.push(`knob: ${knob.label}`)
        }
      }
    }
    // eslint-disable-next-line no-console
    console.log('MISSING SOURCES:\n' + missing.join('\n'))
    expect(missing, 'every claim must carry a lesson-or-ruling source').toEqual([])
  })
})
