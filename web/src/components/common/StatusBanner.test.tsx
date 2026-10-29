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
      "FEED DOWN 10m — connect NinjaTrader's data connection"
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

  // m427/11 — ageText edges: <90s seconds, <5400s minutes (rounded), else hours.
  it.each([
    [89, 'FEED DOWN 89s'],
    [90, 'FEED DOWN 2m'],
    [5399, 'FEED DOWN 90m'],
    [5400, 'FEED DOWN 2h'],
  ])('feed age %is reads "%s" (edge)', async (secs, want) => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      cme_open: true,
      feed_stale: true,
      feed_stale_secs: secs,
    })
    mount()
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(want)
    // exact text, not a prefix of a longer age — and it names the fix (m432)
    expect(screen.getByTestId('status-banner').textContent).toBe(
      `${want} — connect NinjaTrader's data connection`
    )
  })
  it('feed_stale WITHOUT cme_open (older backend) renders nothing', async () => {
    const spy = vi
      .spyOn(updatesApi, 'health')
      .mockResolvedValue({ feed_stale: true, feed_stale_secs: 600 })
    mount()
    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(screen.queryByTestId('status-banner')).toBeNull()
  })
  it('FEED DOWN is red (short) and CME CLOSED is gold', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue({
      cme_open: true,
      feed_stale: true,
      feed_stale_secs: 60,
    })
    const first = mount()
    const down = await screen.findByTestId('status-banner')
    expect(down.style.color).toBe('var(--vl-short)')
    first.unmount()
    vi.restoreAllMocks()
    vi.spyOn(updatesApi, 'health').mockResolvedValue({ cme_open: false })
    mount()
    const closed = await screen.findByTestId('status-banner')
    expect(closed.style.color).toBe('var(--vl-gold)')
  })

  // m444 — a 1m hole the bot could not refill (or cannot reach) is named on the
  // dashboard, with the one action the owner push carries word for word.
  describe('MISSING BARS (m444)', () => {
    const hole = {
      symbol: 'MNQ',
      state: 'retired',
      window: '10-26 17:00–21:57 CT',
      missing_min: 297,
      source: 'audit',
    }
    it('an unfilled hole shows the window, minutes and the reload action', async () => {
      vi.spyOn(updatesApi, 'health').mockResolvedValue({
        cme_open: true,
        feed_stale: false,
        bars_holes: [{ ...hole, state: 'unfilled' }],
      })
      mount()
      expect(
        await screen.findByTestId('missing-bars-banner')
      ).toHaveTextContent(
        'MISSING BARS: MNQ 10-26 17:00–21:57 CT, 297 min — NT8: Reload All Historical Data'
      )
      expect(screen.queryByTestId('status-banner')).toBeNull()
    })
    // review round 2: a RETIRED hole cannot be refilled by a reload or a
    // restart, so the banner must NOT tell the owner to reload.
    it('a retired hole says the bot cannot refill it, not "reload"', async () => {
      vi.spyOn(updatesApi, 'health').mockResolvedValue({
        cme_open: true,
        feed_stale: false,
        bars_holes: [hole],
      })
      mount()
      const el = await screen.findByTestId('missing-bars-banner')
      expect(el).toHaveTextContent(
        'MISSING BARS: MNQ 10-26 17:00–21:57 CT, 297 min'
      )
      expect(el).toHaveTextContent('the bot cannot refill it')
      expect(el.textContent).not.toContain('Reload All Historical Data')
    })
    it('a hole still being re-requested (refilling) shows nothing', async () => {
      const spy = vi.spyOn(updatesApi, 'health').mockResolvedValue({
        cme_open: true,
        feed_stale: false,
        bars_holes: [{ ...hole, state: 'refilling' }],
      })
      mount()
      await waitFor(() => expect(spy).toHaveBeenCalled())
      expect(screen.queryByTestId('missing-bars-banner')).toBeNull()
    })
    it('shows beside FEED DOWN, names the biggest, counts the rest', async () => {
      vi.spyOn(updatesApi, 'health').mockResolvedValue({
        cme_open: true,
        feed_stale: true,
        feed_stale_secs: 600,
        bars_holes: [
          { ...hole, state: 'unfilled', missing_min: 40 },
          { ...hole, symbol: 'ES', state: 'unfilled', missing_min: 12 },
          hole,
        ],
      })
      mount()
      expect(await screen.findByTestId('status-banner')).toHaveTextContent(
        'FEED DOWN 10m'
      )
      // the biggest ACTIONABLE (unfilled) hole leads, even though the retired
      // one is bigger; the rest are counted
      expect(
        await screen.findByTestId('missing-bars-banner')
      ).toHaveTextContent(
        'MISSING BARS: MNQ 10-26 17:00–21:57 CT, 40 min — NT8: Reload All Historical Data (+2 more)'
      )
    })
    it('an older bot (no bars_holes) renders no hole line', async () => {
      const spy = vi
        .spyOn(updatesApi, 'health')
        .mockResolvedValue({ cme_open: true, feed_stale: false })
      mount()
      await waitFor(() => expect(spy).toHaveBeenCalled())
      expect(screen.queryByTestId('missing-bars-banner')).toBeNull()
    })
  })
})
