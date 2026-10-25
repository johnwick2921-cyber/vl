// m420a — B1 (backlog never re-toasts) + B2 (banner only for this session-day).
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import { toast } from 'sonner'
import { AlertCenter } from './AlertCenter'
import { api } from '../../lib/api'
import type { PlanAlert } from '../../lib/api/plan'
import { cmeSessionStartUnix } from './cmeSession'

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const now = () => Math.floor(Date.now() / 1000)
const alert = (over: Partial<PlanAlert>): PlanAlert => ({
  id: 1,
  trader_id: 't',
  level: 'P0',
  event_id: 'e',
  kind: 'feed-down',
  title: 'Feed down',
  body: '',
  acked: false,
  created_at: now(),
  ...over,
})

beforeEach(() => {
  vi.restoreAllMocks()
  vi.mocked(toast.error).mockClear()
})

describe('B1 — toast seed waits for the first real response', () => {
  it('a backlog arriving AFTER the loading render produces 0 toasts; a new P0 produces 1', async () => {
    let resolve!: (v: { alerts: PlanAlert[]; unacked: number }) => void
    const backlog = [alert({ id: 1 }), alert({ id: 2 })]
    const spy = vi.spyOn(api, 'getPlanAlerts').mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r
        })
    )
    render(<AlertCenter traderId="b1-trader" language="en" />)
    // first render: SWR is loading, alerts is the empty fallback
    expect(toast.error).not.toHaveBeenCalled()
    await act(async () => resolve({ alerts: backlog, unacked: 2 }))
    await screen.findByTestId('alert-bell')
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(toast.error).not.toHaveBeenCalled()

    // a genuinely new P0 after the seed
    spy.mockResolvedValue({
      alerts: [...backlog, alert({ id: 3, title: 'NEW halt' })],
      unacked: 3,
    })
    const { mutate } = await import('swr')
    await act(async () => {
      await mutate('plan-alerts-b1-trader')
    })
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(vi.mocked(toast.error).mock.calls[0][0]).toBe('NEW halt')
  })
})

describe('B2 — banner only for a P0 raised this CME session-day', () => {
  it('month-old P0 and close/fill P0 never light the banner; older ones show a count', async () => {
    const start = cmeSessionStartUnix(Date.now())
    vi.spyOn(api, 'getPlanAlerts').mockResolvedValue({
      alerts: [
        alert({ id: 1, created_at: start - 86400 * 30, title: 'old feed' }),
        alert({ id: 2, kind: 'close', title: 'closed trade' }),
        alert({ id: 3, kind: 'fill', title: 'filled' }),
      ],
      unacked: 3,
    })
    render(<AlertCenter traderId="b2-old" language="en" />)
    expect(await screen.findByTestId('alert-older-unacked')).toHaveTextContent(
      '1 older unacked'
    )
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('a fresh feed-down P0 lights the banner', async () => {
    vi.spyOn(api, 'getPlanAlerts').mockResolvedValue({
      alerts: [alert({ id: 9, title: 'Feed down NOW' })],
      unacked: 1,
    })
    render(<AlertCenter traderId="b2-new" language="en" />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Feed down NOW')
  })
})
