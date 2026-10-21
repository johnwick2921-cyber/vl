// Friend add-on banner truth tests at the component call site: shown only when
// the friend bot reports outdated=true, hidden when outdated=false, and hidden
// on the owner's bot (the friend route 404s → client returns null).
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AddonOutdatedBanner } from './AddonOutdatedBanner'

const mocks = vi.hoisted(() => ({
  friendAddonStatus: vi.fn(),
}))

vi.mock('../../lib/api/friends', () => ({
  friendsApi: {
    friendAddonStatus: mocks.friendAddonStatus,
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
})

describe('AddonOutdatedBanner', () => {
  it('shows the banner when the add-on is outdated', async () => {
    mocks.friendAddonStatus.mockResolvedValue({
      connected_build: '2026-10-16-a',
      latest_build: '2026-10-18-a',
      outdated: true,
    })

    render(<AddonOutdatedBanner />)

    await waitFor(() => {
      expect(screen.getByTestId('addon-outdated-banner')).toBeTruthy()
    })
    expect(screen.getByText(/double-click the VL icon to update/)).toBeTruthy()
  })

  it('hides the banner when the add-on is current', async () => {
    mocks.friendAddonStatus.mockResolvedValue({
      connected_build: '2026-10-18-a',
      latest_build: '2026-10-18-a',
      outdated: false,
    })

    render(<AddonOutdatedBanner />)

    await waitFor(() => {
      expect(mocks.friendAddonStatus).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('addon-outdated-banner')).toBeNull()
  })

  it('hides the banner on the owner bot (route absent → null)', async () => {
    mocks.friendAddonStatus.mockResolvedValue(null)

    render(<AddonOutdatedBanner />)

    await waitFor(() => {
      expect(mocks.friendAddonStatus).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('addon-outdated-banner')).toBeNull()
  })
})
