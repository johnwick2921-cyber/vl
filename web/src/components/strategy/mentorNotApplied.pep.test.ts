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

describe('not-applied caveat reads the mode marker from the server origin', () => {
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
  // Mutant: drop the pep_enabled marker -> RED.
  it('reads the pep marker too, verbatim from the resolver', () => {
    expect(
      mentorNotAppliedCaveat(
        knob(
          'pep_enabled on — saved true, not used',
          'not applied while pep mode is ON (no break-even, no trailing)'
        )
      )
    ).toBe('not applied while pep mode is ON (no break-even, no trailing)')
  })
  it('renders nothing for a plain AI strategy or a missing row', () => {
    expect(mentorNotAppliedCaveat(knob('saved value', true))).toBeNull()
    expect(mentorNotAppliedCaveat(undefined)).toBeNull()
  })
})
