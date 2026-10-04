// Mentor guide honesty (K2, 2026-10-04): a card may say "live: yes" only when
// the setting has a control. The evaluator's Config.* / SwingCfg.* / BoxCfg.*
// defaults that have neither a store field nor a Studio control are code
// constants, and their cards must say so instead of pointing at a control that
// does not exist.
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'

const mentorKnobs =
  GUIDE_SECTIONS.find((s) => s.id === 'mentor')?.blocks.flatMap((b) =>
    b.kind === 'knobs' ? b.knobs : []
  ) ?? []

const CODE_CONSTANTS = [
  'Config.KeyLevelTFMinutes',
  'Config.KeyLevelRTHOnly',
  'Config.KeyLevelPrunePts',
  'Config.EMAPeriod34',
  'Config.EMAPeriod9',
  'Config.EMATFMinutes',
  'Config.EMALocationTFMinutes',
  'Config.TouchBandPts',
  'Config.ISBBufferPts',
  'Config.ISBTwentiesPts',
  'Config.PHLMinCandlesFromExtreme',
  'Config.PHLTargetShyPts',
  'Config.StopCeilingPts',
  'Config.RoomMultiple',
  'Config.RangeGapPts',
  'SwingCfg.EMAPeriod',
  'SwingCfg.LineOffsetPts',
  'SwingCfg.StopBeyondLinePts',
  'SwingCfg.EntryBufferPts',
  'SwingCfg.TargetEMA5mPeriod',
  'SwingCfg.LeewayCandles',
  'SwingCfg.Hold4hBars',
  'SwingCfg.Respects5mZone',
  'BoxCfg.TF',
  'BoxCfg.TouchBandPts',
]

describe('mentor guide knob honesty', () => {
  it('lists the 25 code-constant evaluator defaults as live:false, "default, code constant"', () => {
    expect(CODE_CONSTANTS).toHaveLength(25)
    for (const id of CODE_CONSTANTS) {
      const card = mentorKnobs.find((k) => k.settingId === id)
      expect(card, `${id} has a card`).toBeDefined()
      expect(card?.live, `${id} live`).toBe(false)
      expect(card?.where, `${id} where`).toMatch(/code constant/i)
      expect(card?.whenToTouch, `${id} whenToTouch`).toMatch(/code change/i)
    }
  })

  it('has no card for the retired ISBStopMinPts field (no runtime reader)', () => {
    expect(
      mentorKnobs.find((k) => k.settingId === 'Config.ISBStopMinPts')
    ).toBeUndefined()
  })
})
