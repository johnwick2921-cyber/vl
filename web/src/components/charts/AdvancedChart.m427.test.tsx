// m427/3 — an EMPTY /api/klines answer (2m on an older backend, or an empty ring
// right after boot) must say so over the plot instead of leaving a silent blank
// canvas, keep polling, and clear the note when bars arrive. Production call
// site: AdvancedChart + mocked HTTP (same harness as AdvancedChart.m421a).
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AdvancedChart } from './AdvancedChart'

const state = vi.hoisted(() => ({ request: vi.fn() }))
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
  createSeriesMarkers: () => ({ setMarkers: vi.fn() }),
  createChart: () => ({
    addSeries: () => ({
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

const t0 = 1_700_000_000
const bars = {
  success: true,
  data: [-120_000, -60_000, 0].map((d) => ({
    openTime: t0 * 1000 + d,
    open: 30700,
    high: 30780,
    low: 30650,
    close: 30750,
    volume: 1,
  })),
}
let klinesAnswer: any = { success: true, data: [] }
const klineCalls = () =>
  state.request.mock.calls.filter((c) => String(c[0]).includes('/api/klines?'))
    .length

beforeEach(() => {
  state.request.mockReset()
  klinesAnswer = { success: true, data: [] }
  state.request.mockImplementation((url: unknown) => {
    if (String(url).includes('/api/klines?'))
      return Promise.resolve(klinesAnswer)
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

it('an empty kline answer shows "No bars yet for 2m", keeps polling, and clears when bars arrive', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(t0 * 1000))
  render(<AdvancedChart symbol="MNQ" interval="2m" />)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500)
  })
  expect(screen.getByTestId('chart-no-bars')).toHaveTextContent(
    'No bars yet for 2m'
  )
  const first = klineCalls()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15_000)
  })
  expect(klineCalls()).toBeGreaterThan(first) // still polling
  expect(screen.getByTestId('chart-no-bars')).toBeInTheDocument()

  klinesAnswer = bars
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15_000)
  })
  expect(screen.queryByTestId('chart-no-bars')).toBeNull()
})

it('a chart that has bars never shows the note', async () => {
  klinesAnswer = bars
  vi.useFakeTimers()
  vi.setSystemTime(new Date(t0 * 1000))
  render(<AdvancedChart symbol="MNQ" interval="5m" />)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500)
  })
  expect(screen.queryByTestId('chart-no-bars')).toBeNull()
})
