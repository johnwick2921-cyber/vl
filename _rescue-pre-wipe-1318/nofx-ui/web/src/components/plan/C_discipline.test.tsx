// C-fix — DisciplinePanel: the adherence + matched-random endpoints finally
// have readers. Honesty assertions mirror the API: no data → says so; weekly
// null → "first eval Sunday"; warming progress never claims significance.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DisciplinePanel } from './DisciplinePanel'
import { planApi } from '../../lib/api/plan'

vi.mock('../../lib/api/plan', () => ({
  planApi: {
    getPlanTrades: vi.fn(),
    getPlanStats: vi.fn(),
  },
}))

const mockTrades = vi.mocked(planApi.getPlanTrades)
const mockStats = vi.mocked(planApi.getPlanStats)

describe('DisciplinePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders GPA + grade chips when graded trades exist', async () => {
    mockTrades.mockResolvedValue({
      trades: [],
      summary: { counts: { A: 2, B: 1, D: 1 }, total: 4, gpa: 3.0 },
    })
    mockStats.mockResolvedValue({
      weekly: null,
      progress: [
        {
          level_type: 'equal-H/L',
          n: 320,
          reactions: 90,
          target_n: 1565,
          react_rate: 0.28,
          warming: true,
        },
      ],
      target_n: 1565,
      alpha: 0.00625,
    })
    render(<DisciplinePanel traderId="t1" language="en" />)
    await screen.findByText(/GPA 3\.00/)
    const chips = document.querySelectorAll('span.font-mono')
    const chipText = Array.from(chips)
      .map((c) => c.textContent ?? '')
      .join(' ')
    expect(chipText).toContain('A')
    expect(chipText).toContain('B')
    expect(chipText).toContain('D')
    expect(screen.getByText(/320\/1565/)).toBeTruthy()
    expect(screen.getByText(/WARMING/)).toBeTruthy()
    expect(screen.getByText('first eval Sunday')).toBeTruthy()
  })

  it('says so honestly when there is no data', async () => {
    mockTrades.mockResolvedValue({
      trades: [],
      summary: { counts: {}, total: 0, gpa: 0 },
    })
    mockStats.mockResolvedValue({
      weekly: null,
      progress: [],
      target_n: 1565,
      alpha: 0.00625,
    })
    render(<DisciplinePanel traderId="t1" language="en" />)
    expect(await screen.findByText('no graded trades yet')).toBeTruthy()
  })

  it('renders the frozen weekly verdict', async () => {
    mockTrades.mockResolvedValue({
      trades: [],
      summary: { counts: {}, total: 0, gpa: 0 },
    })
    mockStats.mockResolvedValue({
      weekly: {
        iso_week: '2026-W33',
        computed_at: 1,
        verdicts: [
          { level_type: 'equal-H/L', status: 'BEATS-RANDOM' } as never,
        ],
      },
      progress: [],
      target_n: 1565,
      alpha: 0.00625,
    })
    render(<DisciplinePanel traderId="t1" language="en" />)
    expect(await screen.findByText(/equal-H\/L BEATS-RANDOM/)).toBeTruthy()
  })
})
