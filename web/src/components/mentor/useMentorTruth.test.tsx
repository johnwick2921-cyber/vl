// MENTOR-TRUTH PANEL (release #10) — the live-refresh hook.
//
// The card titled "what trades" must not show stale truth: the hook refetches
// every 30s (and on focus). This test pins that the 30s timer advances a second
// fetch, and that a window focus triggers another.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, act, cleanup, screen } from '@testing-library/react'
import { useMentorTruth } from './useMentorTruth'

const getMentorTruth = vi.fn()

vi.mock('../../lib/api/traders', () => ({
  traderApi: {
    getMentorTruth: (...args: unknown[]) => getMentorTruth(...args),
  },
}))

function Harness({
  traderId,
  poll = true,
}: {
  traderId: string
  poll?: boolean
}) {
  const { truth, stale } = useMentorTruth(traderId, poll)
  return (
    <div>
      <span data-testid="enabled">{String(truth?.enabled ?? 'null')}</span>
      <span data-testid="stale">{String(stale)}</span>
    </div>
  )
}

describe('useMentorTruth', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getMentorTruth.mockReset()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('fetches once on mount, then again when the 30s timer advances', async () => {
    getMentorTruth.mockResolvedValue({ enabled: true, levels: [], depth: {} })
    render(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(getMentorTruth).toHaveBeenCalledTimes(2)
  })

  it('fetches again on window focus', async () => {
    getMentorTruth.mockResolvedValue({ enabled: true, levels: [], depth: {} })
    render(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      await Promise.resolve()
    })
    expect(getMentorTruth).toHaveBeenCalledTimes(2)
  })

  it('does not fetch without a traderId', async () => {
    getMentorTruth.mockResolvedValue({ enabled: true, levels: [], depth: {} })
    const { rerender } = render(<Harness traderId="" />)
    expect(getMentorTruth).not.toHaveBeenCalled()
    rerender(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)
  })

  it('does not fetch (and does not poll) when poll=false — caller owns the truth', async () => {
    getMentorTruth.mockResolvedValue({ enabled: true, levels: [], depth: {} })
    render(<Harness traderId="t1" poll={false} />)
    expect(getMentorTruth).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(getMentorTruth).not.toHaveBeenCalled()
    expect(screen.getByTestId('enabled').textContent).toBe('null')
  })

  it('keeps the last good payload (stale) on error, then clears on success', async () => {
    getMentorTruth
      .mockResolvedValueOnce({ enabled: true, levels: [], depth: {} })
      .mockRejectedValueOnce(new Error('blip'))
      .mockResolvedValueOnce({ enabled: true, levels: [], depth: {} })

    render(<Harness traderId="t1" />)
    await act(async () => {})
    expect(screen.getByTestId('enabled').textContent).toBe('true')
    expect(screen.getByTestId('stale').textContent).toBe('false')

    // 30s refresh fails → keep truth, mark stale.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(screen.getByTestId('enabled').textContent).toBe('true')
    expect(screen.getByTestId('stale').textContent).toBe('true')

    // Next 30s refresh succeeds → stale cleared.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(screen.getByTestId('enabled').textContent).toBe('true')
    expect(screen.getByTestId('stale').textContent).toBe('false')
  })
})
