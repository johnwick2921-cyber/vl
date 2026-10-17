// W-ONE-BUTTON M5 (U3) — UpdateBadge: one test per state at the pure mapping,
// plus the mounted component's poll behaviour: Unknown on any error, settles
// within the cap (never a spinner forever).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'

const mocks = vi.hoisted(() => ({ updatesStatus: vi.fn() }))
vi.mock('../../lib/api/updates', () => ({
  updatesApi: { updatesStatus: mocks.updatesStatus },
}))

import { UpdateBadge, badgeFromStatus } from './UpdateBadge'

describe('badgeFromStatus — one state each', () => {
  it('null status → Unknown', () => {
    expect(badgeFromStatus(null)).toBe('unknown')
  })
  it('install_enabled=false → Blocked (the only M3-reachable verdict)', () => {
    expect(
      badgeFromStatus({
        enrolled: true,
        manifest_verifier: 'stub',
        install_enabled: false,
      })
    ).toBe('blocked')
  })
  it('install_state=installing → Installing', () => {
    expect(
      badgeFromStatus({
        enrolled: true,
        manifest_verifier: 'configured',
        install_enabled: true,
        install_state: 'installing',
      })
    ).toBe('installing')
  })
  it('update_available=true → Update available', () => {
    expect(
      badgeFromStatus({
        enrolled: true,
        manifest_verifier: 'configured',
        install_enabled: true,
        update_available: true,
      })
    ).toBe('update-available')
  })
  it('update_available=false → Up to date', () => {
    expect(
      badgeFromStatus({
        enrolled: true,
        manifest_verifier: 'configured',
        install_enabled: true,
        update_available: false,
      })
    ).toBe('up-to-date')
  })
  it('an enabled-but-silent status → Unknown (no invented verdict)', () => {
    expect(
      badgeFromStatus({
        enrolled: true,
        manifest_verifier: 'configured',
        install_enabled: true,
      })
    ).toBe('unknown')
  })
})

describe('UpdateBadge (mounted)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mocks.updatesStatus.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls GET /api/updates and renders Blocked for the M3 payload', async () => {
    mocks.updatesStatus.mockResolvedValue({
      enrolled: true,
      manifest_verifier: 'stub',
      install_enabled: false,
    })
    render(<UpdateBadge />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByTestId('update-badge')).toHaveAttribute(
      'data-state',
      'blocked'
    )
    expect(screen.getByText('Blocked')).toBeTruthy()
    expect(mocks.updatesStatus).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(mocks.updatesStatus).toHaveBeenCalledTimes(2)
  })

  it('shows Unknown on an error and never spins forever', async () => {
    mocks.updatesStatus.mockRejectedValue(new Error('boom'))
    render(<UpdateBadge />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByTestId('update-badge')).toHaveAttribute(
      'data-state',
      'unknown'
    )
    expect(screen.getByText('Unknown')).toBeTruthy()
  })

  it('settles to Unknown even when the fetch hangs past the 10 s cap', async () => {
    mocks.updatesStatus.mockReturnValue(
      new Promise(() => {
        /* never resolves */
      })
    )
    render(<UpdateBadge />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_001)
    })
    expect(screen.getByTestId('update-badge')).toHaveAttribute(
      'data-state',
      'unknown'
    )
    expect(screen.getByText('Unknown')).toBeTruthy()
  })
})
