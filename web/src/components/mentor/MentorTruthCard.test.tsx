// MENTOR-TRUTH PANEL (release #10) — card + bias re-label tests.
//
// mentor ON  → the "Mentor — what trades" card renders AND the AI bias card is
//              labelled "advice only".
// mentor OFF → the card is absent and the AI bias card keeps its unchanged label.

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MentorTruthCard } from './MentorTruthCard'
import { BiasBlock } from '../plan/BiasBlock'
import type { MentorTruth } from '../../lib/api/traders'
import type { PlanBias } from '../../lib/api/plan'
import { LanguageProvider } from '../../contexts/LanguageContext'

const truth = (over: Partial<MentorTruth>): MentorTruth => ({
  enabled: true,
  htf: {
    four_h_dir: 'long',
    four_h_since: 1000,
    one_h_dir: 'long',
    one_h_since: 2000,
    verdict: 'follow',
    verdict_side: 'long',
    gate_active: true,
  },
  trigger_5m: { dir: 'long', price: 30000, since: 3000 },
  levels: [
    {
      key: 'kl:30000',
      kind: 'key_level',
      price: 30000,
      drawn_at: 4000,
      visits_today: 2,
    },
  ],
  depth: { '4h EMA34': 102 },
  depth_line: '',
  window: { start: '08:30', minutes: 60, active: true, ended: false },
  done_after_win: false,
  stop_after_loss: false,
  ...over,
})

const bias: PlanBias = {
  direction: 'long',
  conviction: 'low',
  flip_condition: 'flips below 31308.75',
}

describe('MentorTruthCard', () => {
  it('renders nothing when mentor mode is OFF', () => {
    const { container } = render(
      <MentorTruthCard truth={truth({ enabled: false })} />
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders the panel with the HTF verdict and key levels when ON', () => {
    render(
      <LanguageProvider>
        <MentorTruthCard truth={truth({})} />
      </LanguageProvider>
    )
    expect(screen.getByText('Mentor — what trades')).toBeTruthy()
    expect(screen.getByText('follow LONG')).toBeTruthy()
    expect(screen.getByText('key_level')).toBeTruthy()
    expect(screen.getByText('30000.00')).toBeTruthy()
  })
})

describe('BiasBlock advice-only label', () => {
  it('keeps the unchanged label when mentor mode is OFF', () => {
    render(
      <LanguageProvider>
        <BiasBlock bias={bias} language="en" />
      </LanguageProvider>
    )
    expect(screen.queryByText(/advice only/i)).toBeNull()
  })

  it('labels the AI bias card advice-only when mentor mode is ON', () => {
    render(
      <LanguageProvider>
        <BiasBlock bias={bias} language="en" adviceOnly />
      </LanguageProvider>
    )
    expect(screen.getByText(/AI planner — advice only/i)).toBeTruthy()
  })
})
