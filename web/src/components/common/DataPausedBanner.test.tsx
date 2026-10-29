// m445 — the data-integrity pause line never invents a state.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SWRConfig } from 'swr'
import { DataPausedBanner } from './DataPausedBanner'
import { updatesApi } from '../../lib/api/updates'

const mount = (lang: 'en' | 'zh' | 'id' = 'en') =>
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <DataPausedBanner language={lang} />
    </SWRConfig>
  )

beforeEach(() => vi.restoreAllMocks())

describe('DataPausedBanner', () => {
  it('renders nothing when the backend sends no trading_paused (old bot / no NT8 bridge)', async () => {
    const spy = vi
      .spyOn(updatesApi, 'health')
      .mockResolvedValue({ status: 'ok' })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('data-paused-banner')).toBeNull()
  })
  it('renders nothing when trading is not paused', async () => {
    const spy = vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: { paused: false, reasons: [], windows: [] },
    })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('data-paused-banner')).toBeNull()
  })
  it('names the missing-bars window in CT', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['10-26 17:00–21:57 CT'],
      },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'TRADING PAUSED — missing bars 10-26 17:00–21:57 CT'
    )
  })
  it('counts further holes', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['10-26 17:00–21:57 CT', '10-27 03:10–03:20 CT'],
      },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'missing bars 10-26 17:00–21:57 CT (+1)'
    )
  })
  it('an unknown detector (no window) says so rather than inventing one', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: { paused: true, reasons: ['data_integrity_unknown'] },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'TRADING PAUSED — bar-data check not ready'
    )
  })
  it('a stale-feed-only pause is the FEED DOWN line, not this one', async () => {
    const spy = vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: { paused: true, reasons: ['stale_data'] },
    })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('data-paused-banner')).toBeNull()
  })
})
