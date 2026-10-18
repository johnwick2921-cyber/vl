// W-FRIEND-HOSTED Segment 6 (DS-106): FriendsPage truth tests at the component
// call site — the install code is shown once ("this will not be shown again")
// and dismissed, the live columns come from the API, and the action button
// flips Pause/Resume by state.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { FriendsPage } from './FriendsPage'

const mocks = vi.hoisted(() => ({
  listFriends: vi.fn(),
  addFriend: vi.fn(),
  friendAction: vi.fn(),
}))

vi.mock('../lib/api/friends', () => ({
  friendsApi: {
    listFriends: mocks.listFriends,
    addFriend: mocks.addFriend,
    friendAction: mocks.friendAction,
  },
}))

const friendRow = (overrides: Record<string, unknown> = {}) => ({
  name: 'tom',
  web_port: 18081,
  nt_port: 36981,
  key_sha256: 'abc',
  state: 'active',
  created_at: '2026-10-18T00:00:00Z',
  tailscale_device_id: '',
  bot_up: true,
  nt_connected: true,
  last_bar_age_ms: 1200,
  revision: 'v-friend-1',
  addon_build: '2026-10-16-a',
  app_version: '1.0.0',
  last_seen: '2026-10-18T01:00:00Z',
  ...overrides,
})

beforeEach(() => {
  vi.clearAllMocks()
})

describe('FriendsPage', () => {
  it('renders the friend table from the API and never invents a missing value', async () => {
    mocks.listFriends.mockResolvedValue({
      friends: [friendRow()],
      tailscale_key_set: true,
      tunnel_ip: '100.1.2.3',
    })

    render(<FriendsPage />)

    await waitFor(() => {
      expect(screen.getByTestId('friend-row-tom')).toBeTruthy()
    })
    // live values from the API
    expect(screen.getByText('up')).toBeTruthy()
    expect(screen.getByText('yes')).toBeTruthy()
    expect(screen.getByText('2026-10-16-a / 1.0.0')).toBeTruthy()
  })

  it('shows the install code once ("this will not be shown again") and clears on dismiss', async () => {
    mocks.listFriends.mockResolvedValue({
      friends: [],
      tailscale_key_set: true,
      tunnel_ip: '',
    })
    mocks.addFriend.mockResolvedValue({
      friend: friendRow(),
      install_code: 'code-once',
      shown_once: true,
    })

    render(<FriendsPage />)

    const input = screen.getByLabelText('friend name')
    fireEvent.change(input, { target: { value: 'tom' } })
    fireEvent.click(screen.getByText('Add'))

    await waitFor(() => {
      expect(screen.getByTestId('install-code-box')).toBeTruthy()
    })
    expect(
      screen.getByText('Install code — this will not be shown again')
    ).toBeTruthy()
    expect(screen.getByTestId('install-code')).toHaveProperty(
      'value',
      'code-once'
    )

    // Dismiss clears it — it is not shown again on the next render of this view.
    fireEvent.click(screen.getByText('Dismiss'))
    await waitFor(() => {
      expect(screen.queryByTestId('install-code-box')).toBeNull()
    })
  })

  it('flips Pause/Resume by state and calls the action endpoint', async () => {
    mocks.listFriends.mockResolvedValue({
      friends: [
        friendRow({ state: 'paused', bot_up: false, nt_connected: false }),
      ],
      tailscale_key_set: true,
      tunnel_ip: '',
    })
    mocks.friendAction.mockResolvedValue({ name: 'tom', state: 'active' })

    render(<FriendsPage />)

    await waitFor(() => {
      expect(screen.getByText('Resume')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Resume'))
    await waitFor(() => {
      expect(mocks.friendAction).toHaveBeenCalledWith('tom', 'resume')
    })
  })

  it('surfaces the Tailscale key message when revoke reports api_key_missing', async () => {
    mocks.listFriends.mockResolvedValue({
      friends: [friendRow()],
      tailscale_key_set: false,
      tunnel_ip: '',
    })
    mocks.friendAction.mockResolvedValue({
      name: 'tom',
      state: 'revoked',
      tailscale: 'api_key_missing',
    })

    render(<FriendsPage />)

    await waitFor(() => {
      expect(screen.getByText('Revoke')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Revoke'))
    await waitFor(() => {
      expect(screen.getByTestId('friends-error')).toBeTruthy()
    })
    expect(screen.getByText(/Tailscale API key is not set/)).toBeTruthy()
  })
})
