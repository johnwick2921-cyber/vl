// m427 (guide + label): S2 (a trader-refused touch is spent for its reference
// bar) and S3 (authoring refused inside the flat window) are documented, and the
// mentor_eod_flat gate chip is labelled for what it counts — it also fires for
// forceFlatWindowAt windows that are not end-of-day (the T1 news lead).
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'
import { GATE_LABELS } from '../components/plan/GateBlocksPanel'

const guideText = JSON.stringify(GUIDE_SECTIONS)

describe('m427 — S2/S3 guide text and the flat-window gate label', () => {
  it('labels mentor_eod_flat as a flat window, not end-of-day only', () => {
    expect(GATE_LABELS.mentor_eod_flat.en).toBe(
      'Mentor — flat window (EOD / forced flat)'
    )
    expect(guideText).not.toContain('Mentor — end-of-day flat window')
    expect(guideText).toContain('Mentor — flat window (EOD / forced flat)')
  })
  it('the Go gate site still counts forced-flat windows under that chip', () => {
    const src = readFileSync(
      resolve(process.cwd(), '..', 'trader/mentor_tick.go'),
      'utf8'
    )
    expect(src).toContain('at.forceFlatWindowAt(now, nil)')
    expect(src).toContain('at.mentorGateBlock("mentor_eod_flat")')
  })
  it('says a refused touch is spent for its reference bar (S2)', () => {
    expect(guideText).toContain('that touch is spent for its reference bar')
  })
  it('says authoring is refused inside the flat window (S3)', () => {
    expect(guideText).toContain('Authoring is refused inside the flat window')
    expect(guideText).toContain('refused AUTHORING inside the flat window')
  })
})
