import { describe, expect, it } from 'vitest'
import { mentorNotAppliedCaveat } from './RiskControlEditor'
import type { EffectiveKnob } from '../../lib/api/strategyEffective'

const knob = (origin: string, effective: unknown): EffectiveKnob => ({
  path: 'ai_config.risk_control.breakeven_enabled',
  status: 'live',
  ui_label: '',
  stored: { present: true, value: true },
  effective,
  origin,
  scope: 'strategy',
  resolver: 'x',
  resolved: true,
})

describe('not-applied caveat reads only the mentor marker from the server origin', () => {
  it('still reads the mentor marker', () => {
    expect(
      mentorNotAppliedCaveat(
        knob(
          'mentor_mode on — saved true, not used',
          'not applied while mentor mode is ON'
        )
      )
    ).toBe('not applied while mentor mode is ON')
  })
  // No producer emits a pep marker (store/trader grep: only "mentor_mode on"),
  // so the editor reads none: a hand-built pep origin must NOT light a caveat
  // the server never wrote. Mutant: re-add the pep_enabled branch -> RED.
  it('does not read a pep marker that no resolver row emits', () => {
    expect(
      mentorNotAppliedCaveat(
        knob(
          'pep_enabled on — saved true, not used',
          'not applied while pep mode is ON'
        )
      )
    ).toBeNull()
  })
  it('renders nothing for a plain AI strategy or a missing row', () => {
    expect(mentorNotAppliedCaveat(knob('saved value', true))).toBeNull()
    expect(mentorNotAppliedCaveat(undefined)).toBeNull()
  })
})
