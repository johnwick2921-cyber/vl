// MENTOR-TRUTH PANEL (release #10) — the live-refresh hook.
//
// The card titled "what trades" must not show stale truth: the hook refetches
// every 30s (and on focus). This test pins that the 30s timer advances a second
// fetch, and that a window focus triggers another.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, act, cleanup } from '@testing-library/react'
import { useMentorTruth } from './useMentorTruth'

const getMentorTruth = vi.fn()

vi.mock('../../lib/api/traders', () => ({
  traderApi: {
    getMentorTruth: (...args: unknown[]) => getMentorTruth(...args),
  },
}))

function Harness({ traderId }: { traderId: string }) {
  const truth = useMentorTruth(traderId)
  return <span data-testid="enabled">{String(truth?.enabled ?? 'null')}</span>
}

describe('useMentorTruth', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getMentorTruth.mockReset()
    getMentorTruth.mockResolvedValue({ enabled: true, levels: [], depth: {} })
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('fetches once on mount, then again when the 30s timer advances', async () => {
    render(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(getMentorTruth).toHaveBeenCalledTimes(2)
  })

  it('fetches again on window focus', async () => {
    render(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      await Promise.resolve()
    })
    expect(getMentorTruth).toHaveBeenCalledTimes(2)
  })

  it('does not fetch without a traderId', async () => {
    const { rerender } = render(<Harness traderId="" />)
    expect(getMentorTruth).not.toHaveBeenCalled()
    rerender(<Harness traderId="t1" />)
    expect(getMentorTruth).toHaveBeenCalledTimes(1)
  })
})
