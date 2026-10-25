// m420a/B6 — the top status banner never invents a state.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SWRConfig } from 'swr'
import { StatusBanner } from './StatusBanner'
import { updatesApi } from '../../lib/api/updates'

const mount = (lang: 'en' | 'zh' | 'id' = 'en') =>
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <StatusBanner language={lang} />
    </SWRConfig>
  )

beforeEach(() => vi.restoreAllMocks())

describe('StatusBanner', () => {
  it('renders nothing when the backend sends neither field (old bot)', async () => {
    const spy = vi
      .spyOn(updatesApi, 'health')
      .mockResolvedValue({ status: 'ok' })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('status-banner')).toBeNull()
  })
  it('CME closed shows the reopen line', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({ cme_open: false })
    mount()
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(
      /CME CLOSED — reopens \w{3} \d\d:\d\d CT/
    )
  })
  it('open + stale feed shows FEED DOWN with the age', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      cme_open: true,
      feed_stale: true,
      feed_stale_secs: 600,
    })
    mount()
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(
      'FEED DOWN 10m'
    )
  })
  it('open + fresh feed renders nothing', async () => {
    const spy = vi
      .spyOn(updatesApi, 'health')
      .mockResolvedValue({ cme_open: true, feed_stale: false })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('status-banner')).toBeNull()
  })
})
