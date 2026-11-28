// K9 — through the REAL friendsApi client with only httpClient.request stubbed:
// the 404-on-owner path stays hidden, and a 60s refetch both shows and clears.
import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AddonOutdatedBanner } from './AddonOutdatedBanner'
import { friendsApi, resetFriendAddonProbeForTest } from '../../lib/api/friends'

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
}))

vi.mock('../../lib/httpClient', () => ({
  httpClient: { request: mocks.request },
}))

beforeEach(() => {
  vi.clearAllMocks()
  resetFriendAddonProbeForTest()
  vi.useRealTimers()
})

describe('AddonOutdatedBanner (real client)', () => {
  it('hides on the owner bot — the friend route 404s through the real client', async () => {
    // The real friendsApi.friendAddonStatus catches the thrown 404 and returns
    // null, so the banner never mounts.
    mocks.request.mockRejectedValue(new Error('API not found'))

    render(<AddonOutdatedBanner />)

    await waitFor(() => {
      expect(mocks.request).toHaveBeenCalledWith('/api/friend/addon-status', {
        method: 'GET',
        silent: true,
      })
    })
    expect(screen.queryByTestId('addon-outdated-banner')).toBeNull()
  })

  it('shows the banner through the real client when outdated=true', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        connected_build: '2026-10-16-a',
        latest_build: '2026-10-18-a',
        outdated: true,
      },
    })

    render(<AddonOutdatedBanner />)

    await waitFor(() => {
      expect(screen.getByTestId('addon-outdated-banner')).toBeTruthy()
    })
  })

  it('refetches every 60s and clears once the add-on is current', async () => {
    vi.useFakeTimers()
    mocks.request
      .mockResolvedValueOnce({
        success: true,
        data: {
          connected_build: '2026-10-16-a',
          latest_build: '2026-10-18-a',
          outdated: true,
        },
      })
      .mockResolvedValue({
        success: true,
        data: {
          connected_build: '2026-10-18-a',
          latest_build: '2026-10-18-a',
          outdated: false,
        },
      })

    render(<AddonOutdatedBanner />)

    // Flush the initial poll's microtask chain.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByTestId('addon-outdated-banner')).toBeTruthy()

    // Next 60s poll: the add-on is now current → banner clears.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(screen.queryByTestId('addon-outdated-banner')).toBeNull()

    vi.useRealTimers()
  })
  it('C-friends: a 404 (owner bot) is probed ONCE, then never re-requested', async () => {
    mocks.request.mockRejectedValue(new Error('API not found'))
    expect(await friendsApi.friendAddonStatus()).toBeNull()
    expect(await friendsApi.friendAddonStatus()).toBeNull()
    expect(await friendsApi.friendAddonStatus()).toBeNull()
    expect(mocks.request).toHaveBeenCalledTimes(1)
  })

  it('a transient network error does NOT latch (friend bot keeps probing)', async () => {
    mocks.request.mockRejectedValueOnce(new Error('Network error'))
    mocks.request.mockResolvedValue({
      success: true,
      data: { connected_build: 'a', latest_build: 'b', outdated: true },
    })
    expect(await friendsApi.friendAddonStatus()).toBeNull()
    expect((await friendsApi.friendAddonStatus())?.outdated).toBe(true)
  })
})
