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
  it('a narrower scope says WHICH entries are paused (SWING4H only)', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['10-26 17:00–21:57 CT'],
        scope: 'swing_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('TRADING PAUSED (SWING4H entries only')
    expect(el).toHaveTextContent('other setups trade normally')
    expect(el).toHaveTextContent('missing bars 10-26 17:00–21:57 CT')
  })
  it('the mentor key-level scope and the AI/planner scope are named too', async () => {
    const spy = vi.spyOn(updatesApi, 'health')
    spy.mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['10-26 17:00–21:57 CT'],
        scope: 'swing_and_deep_entries',
      },
    })
    const { unmount } = mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'intraday mentor setups trade normally'
    )
    unmount()
    spy.mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['09-14 09:36–09:58 CT'],
        scope: 'mentor_entries',
      },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'TRADING PAUSED (mentor entries'
    )
  })
  it('all_entries keeps the plain TRADING PAUSED line', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_integrity_hole'],
        windows: ['10-26 17:00–21:57 CT'],
        scope: 'all_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent(
      'TRADING PAUSED — missing bars 10-26 17:00–21:57 CT'
    )
    expect(el).not.toHaveTextContent('(')
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
  it('a data-truth pause names the failing checks (release #55)', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['bad_print', 'cross_frame'],
        scope: 'all_entries',
      },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'TRADING PAUSED — data check failed: bad_print, cross_frame'
    )
  })
  it('a swing4h-only data-truth pause says SWING4H entries only', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['indicator_lookback_swing4h'],
        scope: 'swing_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('TRADING PAUSED (SWING4H entries only')
    expect(el).toHaveTextContent('indicator_lookback_swing4h')
  })
  it('data_truth with no named check renders nothing (never invents one)', async () => {
    const spy = vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: { paused: true, reasons: ['data_truth'] },
    })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('data-paused-banner')).toBeNull()
  })
  it('a stale-feed-only pause is the FEED DOWN line, not this one', async () => {
    const spy = vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: { paused: true, reasons: ['stale_data'] },
    })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('data-paused-banner')).toBeNull()
  })
  // release #55 round 3 (review 4 D1): a reader- or account-scoped data-truth pause
  // never reads as the plain "TRADING PAUSED" (the bot still trades the others).
  it('a mentor-reader-only data-truth pause says the AI path trades normally', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['cross_frame'],
        data_truth_readers: { cross_frame: ['mentor'] },
        scope: 'mentor_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('TRADING PAUSED (mentor traders only')
    expect(el).toHaveTextContent('AI/planner entries trade normally')
    expect(el).toHaveTextContent('cross_frame')
  })
  it('an AI-reader-only data-truth pause says the mentor traders trade normally', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['cross_frame'],
        scope: 'ai_entries',
      },
    })
    mount()
    expect(await screen.findByTestId('data-paused-banner')).toHaveTextContent(
      'TRADING PAUSED (AI/planner entries only; mentor traders trade normally)'
    )
  })
  it('an account-scoped data-truth pause names the account', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['fill_price'],
        data_truth_scopes: { fill_price: ['acct:SIM101'] },
        scope: 'trader_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('other traders trade normally')
    expect(el).toHaveTextContent('fill_price (account SIM101)')
  })
  it('a mixed pause (mentor-only check + an account check) never claims AI trades normally', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        data_truth_checks: ['broker_agreement', 'cross_frame'],
        data_truth_scopes: { broker_agreement: ['acct:SIM101'] },
        data_truth_readers: { cross_frame: ['mentor'] },
        scope: 'mixed_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('some entries only')
    expect(el).not.toHaveTextContent('AI/planner entries trade normally')
    expect(el).not.toHaveTextContent('mentor traders trade normally')
    expect(el).toHaveTextContent('broker_agreement (account SIM101)')
  })
  it('the quiet boot wait is stated, not silent', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth_starting'],
        scope: 'all_entries',
      },
    })
    mount()
    const el = await screen.findByTestId('data-paused-banner')
    expect(el).toHaveTextContent('data checks are starting after a restart')
    expect(el).toHaveTextContent('exits and stops are unaffected')
  })

  // round 4 (re-review 4 P3): the banner names the way out for the two checks that
  // do not clear when the data merely looks fine again.
  it('fill_price and ingest_loss pauses show their remedy on screen', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        windows: [],
        data_truth_checks: ['fill_price', 'ingest_loss'],
        scope: 'all_entries',
      },
    })
    mount()
    const remedies = await screen.findAllByTestId('data-paused-remedy')
    expect(remedies).toHaveLength(2)
    expect(remedies[0]).toHaveTextContent(
      'POST /api/risk/ack-fill-price?symbol=MNQ'
    )
    expect(remedies[0]).toHaveTextContent('not by a new CME session')
    expect(remedies[1]).toHaveTextContent(
      'after a restart in a flat/safe window'
    )
  })
  it('a check that clears by itself shows no remedy line', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      trading_paused: {
        paused: true,
        reasons: ['data_truth'],
        windows: [],
        data_truth_checks: ['cross_frame'],
        scope: 'all_entries',
      },
    })
    mount()
    await screen.findByTestId('data-paused-banner')
    expect(screen.queryByTestId('data-paused-remedy')).toBeNull()
  })
})
