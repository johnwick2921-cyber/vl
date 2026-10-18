// W-ACCOUNT-LOCK-QUIET (DS-106): the always-on "⛔ Stop the trader first…" red
// line is gone. A running trader keeps the disabled dropdown + its title
// tooltip; the account text is shown only via the tooltip, never as an always-on
// error-looking block. Server refusal (api/handler_account.go) is unchanged.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AccountSelector } from './AccountSelector'

const mocks = vi.hoisted(() => ({ getAccounts: vi.fn() }))
vi.mock('../../lib/api', () => ({ api: { getAccounts: mocks.getAccounts } }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.getAccounts.mockResolvedValue({
    current: 'Sim101',
    selected: 'Sim101',
    accounts: [{ name: 'Sim101', is_sim: true, is_current: true }],
  })
})

describe('AccountSelector (account-lock-quiet)', () => {
  it('running: no ⛔ block in the DOM, button disabled, title present', async () => {
    render(<AccountSelector traderId="t1" isRunning />)
    await waitFor(() => expect(screen.getByRole('button')).toBeTruthy())

    expect(screen.queryByText(/Stop the trader first/)).toBeNull()
    const btn = screen.getByRole('button')
    expect(btn).toHaveProperty('disabled', true)
    expect(btn.getAttribute('title')).toBe(
      'Stop the trader first, then choose the account, then start it.'
    )
  })

  it('stopped: button enabled and no title', async () => {
    render(<AccountSelector traderId="t1" />)
    await waitFor(() => expect(screen.getByRole('button')).toBeTruthy())

    const btn = screen.getByRole('button')
    expect(btn).toHaveProperty('disabled', false)
    expect(btn.getAttribute('title')).toBeNull()
  })
})
