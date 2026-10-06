// P2-1 (rel10-truth-card) — PlanCard renders its own mentor card + poll ONLY
// when the page has not passed its live truth down. The dashboard passes
// mentorTruth, so on the dashboard PlanCard must neither poll (useMentorTruth
// with poll=false) nor render a second card.

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PlanCard } from './PlanCard'
import type { MentorTruth } from '../../lib/api/traders'

const useMentorTruth = vi.fn()
const truth = { enabled: true } as MentorTruth

vi.mock('../../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('./usePlan', () => ({
  usePlanToday: () => ({ plan: null, isLoading: false, error: null, mutate: vi.fn() }),
  usePlanVersions: () => ({ versions: [], latestVersion: 0 }),
}))
vi.mock('../mentor/useMentorTruth', () => ({
  useMentorTruth: (...args: unknown[]) => useMentorTruth(...args),
}))
vi.mock('../mentor/MentorTruthCard', () => ({
  MentorTruthCard: () => <div data-testid="mentor-truth-card" />,
}))
vi.mock('./SessionTimelineStrip', () => ({ SessionTimelineStrip: () => null }))
vi.mock('./SessionTabs', () => ({ SessionTabs: () => null }))
vi.mock('./SessionPlanCard', () => ({ SessionPlanCard: () => null }))
vi.mock('./RereadButton', () => ({ RereadButton: () => null }))
vi.mock('./ResetButton', () => ({ ResetButton: () => null }))
vi.mock('./ApproveButton', () => ({ ApproveButton: () => null }))
vi.mock('./AlertCenter', () => ({ AlertCenter: () => null }))
vi.mock('./DeskStrip', () => ({ DeskStrip: () => null }))
vi.mock('./GateBlocksPanel', () => ({ GateBlocksPanel: () => null }))
vi.mock('./ExpectancyPanel', () => ({ ExpectancyPanel: () => null }))
vi.mock('./InstrumentsDrawer', () => ({ InstrumentsDrawer: () => null }))

describe('PlanCard mentor-truth ownership', () => {
  it('polls and renders its own card when the page does NOT pass the truth', () => {
    useMentorTruth.mockReturnValue({ truth: null, stale: false })
    render(<PlanCard traderId="t1" />)
    expect(useMentorTruth).toHaveBeenCalledWith('t1', true)
    expect(screen.getByTestId('mentor-truth-card')).toBeTruthy()
  })

  it('does NOT poll or render its own card when the page passes the truth', () => {
    useMentorTruth.mockReturnValue({ truth: null, stale: false })
    render(<PlanCard traderId="t1" mentorTruth={truth} />)
    expect(useMentorTruth).toHaveBeenCalledWith('t1', false)
    expect(screen.queryByTestId('mentor-truth-card')).toBeNull()
  })
})
