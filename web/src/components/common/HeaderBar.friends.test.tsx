// m386 — the Friends tab is hidden while the /api/health flag is unknown and
// shown once friends_page=true. The flag is read through useFriendsPageEnabled,
// which fetches updatesApi.health (the EXISTING /api/health endpoint).
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { SWRConfig } from 'swr'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import HeaderBar from './HeaderBar'

const mocks = vi.hoisted(() => ({ health: vi.fn() }))

vi.mock('../../lib/api/updates', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api/updates')>()
  return {
    ...actual,
    updatesApi: { ...actual.updatesApi, health: mocks.health },
  }
})

function renderBar() {
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <MemoryRouter>
        <HeaderBar language="en" isLoggedIn={true} />
      </MemoryRouter>
    </SWRConfig>
  )
}

describe('Friends tab gating (m386)', () => {
  beforeEach(() => {
    mocks.health.mockReset()
  })

  it('hides the Friends tab while the health flag is unknown', async () => {
    mocks.health.mockResolvedValue(null)
    renderBar()
    // SWR resolves asynchronously; the hook is false while unknown and stays
    // false for a null health, so the label never appears.
    await screen.findByText('Agent')
    expect(screen.queryAllByText('Friends')).toHaveLength(0)
  })

  it('shows the Friends tab once friends_page=true', async () => {
    mocks.health.mockResolvedValue({ friends_page: true })
    renderBar()
    expect(await screen.findAllByText('Friends')).not.toHaveLength(0)
  })
})
