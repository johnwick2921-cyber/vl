import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
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
  // rereview m461 P2-1: the break-even / trailing effective rows NOW emit the pep
  // marker. The marker string is READ from the Go producer (trader/effective_
  // settings.go pepModeOnOrigin), never typed twice, and the caveat reads it the
  // way it reads the mentor one. Mutant: drop the 'pep_enabled on' branch of
  // mentorNotAppliedCaveat, or change the Go marker -> RED.
  it('reads the pep marker the resolver rows emit', () => {
    const go = readFileSync(
      resolve(process.cwd(), '../trader/effective_settings.go'),
      'utf8'
    )
    const marker = go.match(/pepModeOnOrigin\s*=\s*"([^"]+)"/)?.[1]
    expect(marker).toBe('pep_enabled on')
    expect(
      mentorNotAppliedCaveat(
        knob(
          `${marker} — saved true not used`,
          "not applied while pep mode is ON (pep's fixed stop and target own the exit)"
        )
      )
    ).toBe(
      "not applied while pep mode is ON (pep's fixed stop and target own the exit)"
    )
  })
  // A pep origin WITHOUT the "not used" verdict (no saved value lost) lights
  // nothing, the same as the mentor marker.
  it('does not light a caveat for a pep origin that lost no saved value', () => {
    expect(
      mentorNotAppliedCaveat(
        knob('pep_enabled on', 'not applied while pep mode is ON')
      )
    ).toBeNull()
  })
  it('renders nothing for a plain AI strategy or a missing row', () => {
    expect(mentorNotAppliedCaveat(knob('saved value', true))).toBeNull()
    expect(mentorNotAppliedCaveat(undefined)).toBeNull()
  })
})
