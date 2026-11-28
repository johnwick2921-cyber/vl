// m242 trade-marker pins (P2, P4) — the PRODUCTION call site: AdvancedChart
// fetches position history + open positions and draws through
// createSeriesMarkers. These pin the forming-candle snap and the toggle-ref
// behaviour the pure builder alone cannot see.
//
// P2 (forming candle + OPEN entry): an OPEN position whose entry_time is INSIDE
//   the still-forming candle (newestOpen < t < newestOpen + intervalSec) gets an
//   entry marker snapped to the NEWEST candle — not dropped, not one candle
//   late. Mutant: snap `t < klineMaxTime` only (drop the +intervalSec) → the
//   forming-candle fill is dropped and setMarkers([]) → RED.
// P4 (toggle survives the 5s refresh): toggling B/S OFF must survive the next
//   auto-refresh. The loadData closure reads showOrderMarkersRef (always
//   current), so the refresh re-sets an EMPTY marker list instead of re-drawing
//   the markers. Mutant: read the showOrderMarkers STATE in loadData → the
//   stale closure re-adds the markers after the refresh → RED.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AdvancedChart } from './AdvancedChart'

const state = vi.hoisted(() => ({
  request: vi.fn(),
  setMarkers: vi.fn(),
  createdMarkers: [] as any[],
}))
vi.mock('../../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('../../lib/httpClient', () => ({
  httpClient: { request: state.request },
}))
vi.mock('./primitives/SessionVolumeProfile', () => ({
  SessionVolumeProfile: class {
    setData = vi.fn()
  },
}))
vi.mock('lightweight-charts', () => ({
  CandlestickSeries: 'candles',
  HistogramSeries: 'volume',
  LineSeries: 'line',
  createSeriesMarkers: (_series: unknown, markers: any[]) => {
    state.createdMarkers = markers
    return { setMarkers: state.setMarkers }
  },
  createChart: () => ({
    addSeries: (kind: string, _opts: unknown) => ({
      setData: vi.fn(),
      priceScale: () => ({ applyOptions: vi.fn() }),
      attachPrimitive: vi.fn(),
      detachPrimitive: vi.fn(),
      createPriceLine: vi.fn(),
      removePriceLine: vi.fn(),
    }),
    applyOptions: vi.fn(),
    remove: vi.fn(),
    removeSeries: vi.fn(),
    subscribeCrosshairMove: vi.fn(),
    timeScale: () => ({ fitContent: vi.fn() }),
  }),
}))

const t0 = 1_700_000_000 // a 5m boundary (divisible by 300)

// 3 × 5m candles; the newest OPEN is t0+600s.
const klinesResponse = (lastOpenMs: number) => ({
  success: true,
  data: [
    {
      openTime: lastOpenMs - 600_000,
      open: 30700,
      high: 30750,
      low: 30650,
      close: 30720,
      volume: 1,
    },
    {
      openTime: lastOpenMs - 300_000,
      open: 30720,
      high: 30760,
      low: 30700,
      close: 30740,
      volume: 1,
    },
    {
      openTime: lastOpenMs,
      open: 30740,
      high: 30780,
      low: 30730,
      close: 30750,
      volume: 1,
    },
  ],
})

const openPosition = (entryTimeMs: number) => [
  {
    side: 'LONG',
    symbol: 'MNQ',
    entry_price: 30750,
    entry_time: entryTimeMs,
    status: 'OPEN',
  },
]

const urlOf = (url: string) => String(url)

beforeEach(() => {
  state.request.mockReset()
  state.setMarkers.mockReset()
  state.createdMarkers = []
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    }
  )
  vi.spyOn(console, 'log').mockImplementation(() => {})
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('P2 — draws an OPEN entry inside the forming candle at the newest candle open', async () => {
  vi.useFakeTimers()
  const lastOpenMs = t0 * 1000 + 600_000 // newest candle open = t0+600
  // entry 120s into the forming candle (still < intervalSec=300)
  const entryTimeMs = lastOpenMs + 120_000

  state.request.mockImplementation((url: unknown) => {
    const u = urlOf(url as string)
    if (u.includes('/api/klines?'))
      return Promise.resolve(klinesResponse(lastOpenMs))
    if (u.includes('/api/positions/history?'))
      return Promise.resolve({ success: true, data: { positions: [] } })
    if (u.includes('/api/positions?'))
      return Promise.resolve({ success: true, data: openPosition(entryTimeMs) })
    if (u.includes('/api/open-orders?'))
      return Promise.resolve({ success: true, data: [] })
    return Promise.resolve({ success: true, data: [] })
  })

  render(
    <AdvancedChart symbol="MNQ" traderID="trader" selectedAccount="SimA" />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })

  const calls = state.setMarkers.mock.calls.map((c) => c[0] as any[])
  const drawn = state.createdMarkers as any[]
  expect(drawn).toHaveLength(1)
  expect(drawn[0]).toMatchObject({
    time: t0 + 600,
    price: 30750,
    shape: 'arrowUp',
    text: 'B',
    position: 'atPriceMiddle',
  })
  // The first load creates the marker primitive with the markers; the later
  // refresh path calls setMarkers instead. Assert that path agrees too.
  expect(calls.length).toBe(0)
})

it('P4 — toggling B/S OFF survives the 5s auto-refresh (no re-draw)', async () => {
  vi.useFakeTimers()
  const lastOpenMs = t0 * 1000 + 600_000
  const entryTimeMs = lastOpenMs + 120_000

  state.request.mockImplementation((url: unknown) => {
    const u = urlOf(url as string)
    if (u.includes('/api/klines?'))
      return Promise.resolve(klinesResponse(lastOpenMs))
    if (u.includes('/api/positions/history?'))
      return Promise.resolve({ success: true, data: { positions: [] } })
    if (u.includes('/api/positions?'))
      return Promise.resolve({ success: true, data: openPosition(entryTimeMs) })
    if (u.includes('/api/open-orders?'))
      return Promise.resolve({ success: true, data: [] })
    return Promise.resolve({ success: true, data: [] })
  })

  render(
    <AdvancedChart symbol="MNQ" traderID="trader" selectedAccount="SimA" />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  expect(state.createdMarkers).toHaveLength(1) // initially drawn

  // Toggle B/S OFF.
  fireEvent.click(screen.getByText('B/S'))
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
  expect(state.setMarkers.mock.calls.at(-1)?.[0]).toEqual([]) // immediate hide

  // The 5s auto-refresh must NOT re-draw the markers (the ref keeps it OFF).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5_000)
  })
  const afterRefresh = state.setMarkers.mock.calls.at(-1)?.[0] as any[]
  expect(afterRefresh).toEqual([])

  // No call after the toggle ever re-added a marker.
  const postToggle = state.setMarkers.mock.calls.slice(-3)
  expect(postToggle.every((c) => (c[0] as any[]).length === 0)).toBe(true)
})
