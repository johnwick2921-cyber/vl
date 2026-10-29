// m420a/B6 — the top status banner never invents a state.
import { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SWRConfig, useSWRConfig } from 'swr'
import { StatusBanner } from './StatusBanner'
import { updatesApi } from '../../lib/api/updates'

const mount = (lang: 'en' | 'zh' | 'id' = 'en', retryMs = 5000) =>
  render(
    <SWRConfig
      value={{ provider: () => new Map(), errorRetryInterval: retryMs }}
    >
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

  // m443/#39 — a dead backend must say so (was: nothing rendered).
  it('a failed first poll (network error) shows BOT UNREACHABLE in red', async () => {
    vi.spyOn(updatesApi, 'health').mockRejectedValue(new Error('Network error'))
    mount()
    const el = await screen.findByTestId('status-banner')
    expect(el.textContent).toMatch(/^BOT UNREACHABLE/)
    expect(el.style.color).toBe('var(--vl-short)')
  })
  it('an unreadable health (null) is also BOT UNREACHABLE', async () => {
    vi.spyOn(updatesApi, 'health').mockResolvedValue(null)
    mount()
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(
      /^BOT UNREACHABLE/
    )
  })
  it('BOT UNREACHABLE clears when a later poll succeeds', async () => {
    const spy = vi
      .spyOn(updatesApi, 'health')
      .mockRejectedValueOnce(new Error('Network error'))
      .mockResolvedValue({ status: 'ok' })
    mount('en', 10)
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(
      /^BOT UNREACHABLE/
    )
    await waitFor(() =>
      expect(screen.queryByTestId('status-banner')).toBeNull()
    )
    expect(spy.mock.calls.length).toBeGreaterThanOrEqual(2)
  })
  it('unreachable wins over the last good FEED DOWN line (no stale mask)', async () => {
    vi.spyOn(updatesApi, 'health')
      .mockResolvedValueOnce({
        cme_open: true,
        feed_stale: true,
        feed_stale_secs: 60,
      })
      .mockRejectedValue(new Error('Network error'))
    // refreshInterval is 30s in prod: force the second poll through the
    // same (per-test) SWR cache the banner reads.
    const box: { revalidate: () => Promise<unknown> } = {
      revalidate: async () => undefined,
    }
    const Grab = () => {
      const { mutate } = useSWRConfig()
      useEffect(() => {
        box.revalidate = () => mutate('status-banner-health')
      }, [mutate])
      return null
    }
    render(
      <SWRConfig value={{ provider: () => new Map(), errorRetryInterval: 10 }}>
        <StatusBanner language="en" />
        <Grab />
      </SWRConfig>
    )
    expect(await screen.findByTestId('status-banner')).toHaveTextContent(
      /FEED DOWN/
    )
    await box.revalidate()
    await waitFor(() =>
      expect(screen.getByTestId('status-banner').textContent).toMatch(
        /^BOT UNREACHABLE/
      )
    )
  })
  it('BOT UNREACHABLE has en/zh/id text', async () => {
    vi.spyOn(updatesApi, 'health').mockRejectedValue(new Error('x'))
    for (const [lang, re] of [
      ['zh', /机器人无法连接/],
      ['id', /BOT TIDAK DAPAT DIJANGKAU/],
    ] as const) {
      const r = mount(lang)
      expect(await screen.findByTestId('status-banner')).toHaveTextContent(re)
      r.unmount()
    }
  })
})
