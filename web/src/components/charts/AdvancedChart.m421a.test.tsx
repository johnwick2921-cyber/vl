// m421a pins through the PRODUCTION call site (AdvancedChart + mocked HTTP):
//  C1/C2 — an open position row carrying `bracket` draws its SL/TP price lines
//          and (entry_time present) its entry arrow.
//  C14   — /api/positions/history is NOT re-fetched every 5s tick; it is
//          fetched once, again after 30s, and at once when the open count moves.
import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AdvancedChart } from './AdvancedChart'

const state = vi.hoisted(() => ({
  request: vi.fn(),
  createPriceLine: vi.fn(),
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
    return { setMarkers: vi.fn() }
  },
  createChart: () => ({
    addSeries: () => ({
      setData: vi.fn(),
      priceScale: () => ({ applyOptions: vi.fn() }),
      attachPrimitive: vi.fn(),
      detachPrimitive: vi.fn(),
      createPriceLine: state.createPriceLine,
      removePriceLine: vi.fn(),
    }),
    applyOptions: vi.fn(),
    remove: vi.fn(),
    removeSeries: vi.fn(),
    subscribeCrosshairMove: vi.fn(),
    timeScale: () => ({ fitContent: vi.fn() }),
  }),
}))

const t0 = 1_700_000_000
const lastOpenMs = t0 * 1000 + 600_000
const klines = {
  success: true,
  data: [-600_000, -300_000, 0].map((d) => ({
    openTime: lastOpenMs + d,
    open: 30700,
    high: 30780,
    low: 30650,
    close: 30750,
    volume: 1,
  })),
}

let openRows: any[] = []
const count = (frag: string) =>
  state.request.mock.calls.filter((c) => String(c[0]).includes(frag)).length

beforeEach(() => {
  state.request.mockReset()
  state.createPriceLine.mockReset()
  state.createdMarkers = []
  openRows = [
    {
      side: 'LONG',
      symbol: 'MNQ',
      entry_price: 30750,
      entry_time: lastOpenMs + 60_000,
      status: 'OPEN',
      bracket: { stop: 30700, target: 30850 },
    },
  ]
  state.request.mockImplementation((url: unknown) => {
    const u = String(url)
    if (u.includes('/api/klines?')) return Promise.resolve(klines)
    if (u.includes('/api/positions/history?'))
      return Promise.resolve({ success: true, data: { positions: [] } })
    if (u.includes('/api/positions?'))
      return Promise.resolve({ success: true, data: openRows })
    return Promise.resolve({ success: true, data: [] })
  })
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

const mount = async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(t0 * 1000))
  render(
    <AdvancedChart symbol="MNQ" traderID="trader" selectedAccount="SimA" />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500)
  })
}

it('C1/C2 — an open position with bracket + entry_time draws SL/TP lines and the entry arrow', async () => {
  await mount()
  const prices = state.createPriceLine.mock.calls.map((c) => c[0].price)
  expect(prices).toEqual(expect.arrayContaining([30700, 30850]))
  expect(state.createdMarkers).toHaveLength(1)
  expect(state.createdMarkers[0]).toMatchObject({ text: 'B', price: 30750 })
})

it('C14 — position history is not fetched every 5s tick', async () => {
  await mount()
  expect(count('/api/positions/history?')).toBe(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000) // two 5s ticks
  })
  expect(count('/api/positions?')).toBeGreaterThan(1) // ticks happened
  expect(count('/api/positions/history?')).toBe(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(25_000) // past 30s
  })
  expect(count('/api/positions/history?')).toBe(2)
})

it('C14 — history is re-fetched at once when the open-position count changes', async () => {
  await mount()
  expect(count('/api/positions/history?')).toBe(1)
  openRows = [] // the trade closed
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5_500)
  })
  expect(count('/api/positions/history?')).toBe(2)
})
