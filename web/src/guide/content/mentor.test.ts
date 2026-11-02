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
      // '2m' (B26) left this list in release #60: the 2m size cut is BUILT
      // (D3.1-R30, switch mentor_2m_reduce_size) — pinned below.
    ]) {
      expect(titles.toLowerCase(), `no not-built note for ${needle}`).toContain(
        needle.toLowerCase()
      )
    }
  })

  it('release #60: the 2m size cut and the raw-ISB resonance are built, not parked', () => {
    const callout = mentor.blocks.find(
      (b) => b.kind === 'callout' && b.title.startsWith('Not built')
    )
    if (callout && callout.kind === 'callout') {
      const titles = callout.items.map((i) => i.title).join(' | ')
      expect(titles).not.toMatch(/2m/)
    }
    expect(all).toContain('Smaller size on a 2m entry')
    expect(all).toContain('exec_2m_reduce')
    expect(all).toContain('Resonance on any same-way inside bar')
    expect(all).toContain('resonance_armed_raw_isb')
    expect(all).not.toContain('there is NO frame-based size cut')
  })

  it('labels the ours-not-course inferences (doji, two losses)', () => {
    expect(all).toContain('DOJI candle 1 has no colour')
    expect(all).toContain('the “off for the day” part is OURS')
  })

  // m463 (release #59): the swing re-arm inference is GONE — only the first
  // touching candle counts [D5.2 p1 @13:23–13:40]; the colour rule and the
  // ~100-pt "take 1–2 MNQ" ruling (m186) are reversed by the course.
  it('describes the swing exactly as D5.2 (m463)', () => {
    expect(all).not.toContain('re-arms the line is OURS')
    expect(all).toContain('swing_not_first_touch')
    expect(all).toContain('swing_level_invalid')
    expect(all).toContain('không quan trọng màu')
    expect(all).toContain('mentor_swing_max_stop_pts')
    expect(all).toContain('REVERSES the earlier owner ruling m186')
    expect(all).not.toContain('whose colour matches the side')
  })

  // m465 P2 (release #60): an ISB loss boxes the whole 5m range [D4.2-R27].
  it('describes the ISB loss range box (m465 P2, D4.2-R27/R28/R30)', () => {
    expect(all).toContain('mentor_isb_loss_box')
    expect(all).toContain('loss_range_blocked')
    expect(all).toContain('loss_range_seed_only')
    expect(all).toContain('em đợi cho cái điểm entry nằm ở vòng vòng bên ngoài')
    expect(all).toContain('28,880.50–28,934.00')
  })

  // m466 P4 (release #61): the swing inside bar without close-back (D5.2-R26),
  // the inside-bar order kept on a close through (D5.2-R25), no runner TP (R6).
  it('describes the release #61 swing ISB and runner knobs (m466 P4)', () => {
    expect(all).toContain('mentor_swing_isb_close_back')
    expect(all).toContain('swing_isb_entry_beyond_line')
    expect(all).toContain('mentor_swing_isb_keep')
    expect(all).toContain('Để cái cây inside bar này cho em')
    expect(all).toContain('mentor_swing_runner_tp')
    expect(all).toContain('swing_runner_no_tp')
    expect(all).toContain('nhưng chị đợi hơi lâu')
  })

  // release #59 fix round: one setup per level (an open swing refuses the
  // inside bar) and the PHL rays are the drawn 0.50 / 0.50 [D2.2-R10].
  it('names the open-swing inside-bar refusal and the drawn PHL rays', () => {
    expect(all).toContain('swing_position_open')
    expect(all).toContain('stop 0.50 under that candle’s low')
    expect(all).toContain('0.50 / 0.50')
    expect(all).toContain('rolling_phl_15m_box_ban')
    expect(all).not.toContain('the PHL entry buffer, default 1.00')
  })

  // release #59 review round 2: the closing 4h candle's last bar is read
  // against its own line and arms nothing (D5.2-R27); a cancelled inside bar
  // does not spend the level (D5.2-R25); the 09-11 worked buy is skipped by
  // the Day-4 15m short ISB box + the 5m trigger, named, a mentor question.
  it('names the closed-candle and spent inside-bar refusals and the 15m box conflict', () => {
    expect(all).toContain('swing_candle_closed')
    expect(all).toContain('swing_isb_spent')
    expect(all).toContain('CANCELLED unfilled')
    expect(all).not.toContain('ONE inside-bar entry per 4h candle')
    expect(all).toContain('a SHORT 15m ISB box')
    expect(all).toContain('also refused rolling_room')
  })
})

// m462 (release #59): the 4h/1h trigger gate applies ALL SESSION by default
// (D5.1-R9/R11). The guide must say so, name the per-setup refusals, keep the
// SWING4H exemption explicit (D5.2-R7), and never again call news + open the
// default. Kernel twin: kernel/mentor/m462_htf_allday_test.go.
describe('mentor guide — m462 all-session 4h/1h gate', () => {
  const all = JSON.stringify(mentor)
  it('states the all-session default with the D5.1 sources', () => {
    expect(all).toContain('ALL SESSION')
    expect(all).toContain('D5.1-R9')
    expect(all).toContain('D5.1-R11')
    expect(all).toContain('systemDefault":"false')
  })
  it('names the per-setup refusals and the SWING4H exemption', () => {
    for (const needle of [
      'htf_conflict',
      'htf_side_mismatch',
      'htf_no_4h',
      'D5.2-R7',
    ]) {
      expect(all, `missing ${needle}`).toContain(needle)
    }
  })
  it('no longer calls news + open the default', () => {
    expect(all).not.toContain('ON — news + open is the default')
    expect(all).not.toContain('NEWS + OPEN (the mentor’s rule')
  })
})
