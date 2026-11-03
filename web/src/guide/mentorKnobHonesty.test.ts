// Mentor guide honesty (K2, 2026-10-04): a card may say "live: yes" only when
// the setting has a control. The evaluator's Config.* / SwingCfg.* / BoxCfg.*
// defaults that have neither a store field nor a Studio control are code
// constants, and their cards must say so instead of pointing at a control that
// does not exist.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
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
  'Config.PHLMinCandlesFromExtreme',
  'Config.StopCeilingPts',
  'Config.RoomMultiple',
  'Config.RangeGapPts',
  'SwingCfg.EMAPeriod',
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
  it('lists the 22 code-constant evaluator defaults as live:false, "default, code constant"', () => {
    expect(CODE_CONSTANTS).toHaveLength(22)
    for (const id of CODE_CONSTANTS) {
      const card = mentorKnobs.find((k) => k.settingId === id)
      expect(card, `${id} has a card`).toBeDefined()
      expect(card?.live, `${id} live`).toBe(false)
      expect(card?.where, `${id} where`).toMatch(/code constant/i)
      expect(card?.whenToTouch, `${id} whenToTouch`).toMatch(/code change/i)
    }
  })

  // m463 (D5.2-R12 "không được gần đụng"): the 5-pt near-touch allowance is
  // removed from the kernel; its card must not come back.
  it('has no card for the retired SwingCfg.LineOffsetPts (m463: literal touch)', () => {
    expect(
      mentorKnobs.find((k) => k.settingId === 'SwingCfg.LineOffsetPts')
    ).toBeUndefined()
  })

  it('the swing skip card defaults to the kernel SkipStopPts (m463, 90)', () => {
    const go = readFileSync(
      resolve(process.cwd(), '..', 'kernel/mentor/swing4h.go'),
      'utf8'
    )
    const m = /SkipStopPts:\s+(\d+),/.exec(go)
    expect(m).not.toBeNull()
    const card = mentorKnobs.find(
      (k) => k.settingId === 'mentor_swing_max_stop_pts'
    )
    expect(card?.live).toBe(true)
    expect(card?.systemDefault).toBe(m![1])
  })

  // m465 P2 (release #60, D4.2-R27): the ISB loss box card's default is the
  // kernel DefaultConfig value, read — never retyped.
  it('the ISB loss box card defaults to the kernel ISBLossBox (m465, range)', () => {
    const goCfg = readFileSync(
      resolve(process.cwd(), '..', 'kernel/mentor/mentor.go'),
      'utf8'
    )
    const goLim = readFileSync(
      resolve(process.cwd(), '..', 'kernel/mentor/limits.go'),
      'utf8'
    )
    const c = /ISBLossBox:\s+(\w+),/.exec(goCfg)
    expect(c).not.toBeNull()
    const v = new RegExp(c![1] + '\\s+=\\s+"(\\w+)"').exec(goLim)
    expect(v).not.toBeNull()
    const card = mentorKnobs.find((k) => k.settingId === 'mentor_isb_loss_box')
    expect(card?.live).toBe(true)
    expect(card?.systemDefault).toBe(v![1])
  })

  // m466 P4 (release #61): the three swing knob cards' defaults are the Go
  // resolver defaults, read — never retyped.
  it('the swing ISB / runner TP cards default to the Go resolver defaults (m466 P4)', () => {
    const swing = readFileSync(
      resolve(process.cwd(), '..', 'kernel/mentor/swing4h.go'),
      'utf8'
    )
    const exits = readFileSync(
      resolve(process.cwd(), '..', 'trader/mentor_course_exits.go'),
      'utf8'
    )
    const cb = /SwingISBCloseBackNotRequired\s+=\s+"(\w+)"/.exec(swing)
    expect(cb).not.toBeNull()
    const tp = /mentorSwingRunnerTPNone\s+=\s+"(\w+)"/.exec(exits)
    expect(tp).not.toBeNull()
    const keep = /mentorBoolKnob\(rc\.MentorSwingISBKeep, (true|false)\)/.exec(
      exits
    )
    expect(keep).not.toBeNull()
    const card = (id: string) => mentorKnobs.find((k) => k.settingId === id)
    expect(card('mentor_swing_isb_close_back')?.live).toBe(true)
    expect(card('mentor_swing_isb_close_back')?.systemDefault).toBe(cb![1])
    expect(card('mentor_swing_runner_tp')?.live).toBe(true)
    expect(card('mentor_swing_runner_tp')?.systemDefault).toBe(tp![1])
    expect(card('mentor_swing_isb_keep')?.live).toBe(true)
    expect(card('mentor_swing_isb_keep')?.systemDefault).toBe(
      keep![1] === 'true' ? 'ON' : 'OFF'
    )
  })

  it('has no card for the retired ISBStopMinPts field (no runtime reader)', () => {
    expect(
      mentorKnobs.find((k) => k.settingId === 'Config.ISBStopMinPts')
    ).toBeUndefined()
  })

  it('Swing hold bars systemDefault matches kernel/mentor DefaultSwingCfg.Hold4hBars', () => {
    const go = readFileSync(
      resolve(process.cwd(), '..', 'kernel/mentor/swing4h.go'),
      'utf8'
    )
    const m = /Hold4hBars:\s+(\d+),/.exec(go)
    expect(m).not.toBeNull()
    const card = mentorKnobs.find((k) => k.settingId === 'SwingCfg.Hold4hBars')
    expect(card?.systemDefault).toBe(m![1])
  })
})
