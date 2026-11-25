// T5 (release #22, UI-GUIDE-TRUTH): a working mentor stop-ENTRY (ledger row
// 362: SELL 3 @ 30742.75, bracket stop 30760.25) was drawn as a red "SL 1" at
// the stop — the entry was never drawn and the size was wrong. These pins run
// the PRODUCTION call site: AdvancedChart fetches /api/open-orders and draws
// with createPriceLine. Mutant: re-label a ledger stop-entry as "SL" at its
// stop_price (chartOverlays.orderPriceLines) → RED.
import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AdvancedChart } from './AdvancedChart'

const state = vi.hoisted(() => ({
  line: vi.fn(),
  request: vi.fn(),
  addSeries: vi.fn(),
  lineData: vi.fn(),
  removeSeries: vi.fn(),
  applyOptions: vi.fn(),
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
  createSeriesMarkers: () => ({ setMarkers: vi.fn() }),
  createChart: () => ({
    addSeries: (kind: string, opts: unknown) => {
      state.addSeries(kind, opts)
      return {
        setData: kind === 'line' ? state.lineData : vi.fn(),
        applyOptions: state.applyOptions,
        priceScale: () => ({ applyOptions: vi.fn() }),
        attachPrimitive: vi.fn(),
        detachPrimitive: vi.fn(),
        createPriceLine: state.line,
        removePriceLine: vi.fn(),
      }
    },
    applyOptions: vi.fn(),
    remove: vi.fn(),
    removeSeries: state.removeSeries,
    subscribeCrosshairMove: vi.fn(),
    timeScale: () => ({ fitContent: vi.fn() }),
  }),
}))

const LEDGER =
  'armed_orders ledger (broker acceptance is a separate observation)'
const bars = {
  success: true,
  data: [
    {
      openTime: 1700000000000,
      open: 30750,
      high: 30760,
      low: 30740,
      close: 30750,
      volume: 1,
    },
  ],
}

beforeEach(() => {
  state.line.mockReset().mockReturnValue({})
  state.request.mockReset()
  state.addSeries.mockReset()
  state.lineData.mockReset()
  state.removeSeries.mockReset()
  state.applyOptions.mockReset()
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    }
  )
  vi.spyOn(console, 'log').mockImplementation(() => {})
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('draws a working ledger stop-entry as an ENTRY at its entry price with the real side and size — never "SL"', async () => {
  vi.useFakeTimers()
  state.request.mockImplementation((url: string) =>
    Promise.resolve(
      url.includes('/open-orders?')
        ? {
            success: true,
            data: [
              {
                order_id: 'sig-362',
                side: 'SELL',
                position_side: 'SHORT',
                type: 'STOP_MARKET',
                price: 30742.75,
                stop_price: 30760.25,
                quantity: 3,
                status: 'NEW',
                arm_state: 'working',
                source: LEDGER,
              },
            ],
          }
        : bars
    )
  )
  render(
    <AdvancedChart symbol="MNQ" traderID="trader" selectedAccount="SimA" />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  const drawn = state.line.mock.calls.map(
    (c) => c[0] as { price: number; title: string }
  )
  expect(drawn).toHaveLength(1)
  expect(drawn[0].price).toBe(30742.75)
  expect(drawn[0].title).toMatch(/^Stop entry SELL 3/)
  expect(drawn[0].title).toContain('SL 30760.25')
  expect(drawn.some((d) => /^SL /.test(d.title))).toBe(false)
  expect(drawn.some((d) => d.price === 30760.25)).toBe(false)
})

it('draws the mentor trendline as a sloped segment and the other mentor places as labelled lines', async () => {
  vi.useFakeTimers()
  const t0 = 1789480800000 // a 5m boundary
  // UG-6 (release #24): the trendline snaps to the bars the chart DREW, so the
  // chart is fed the 1m bars the line spans (t0 … t0+15m).
  const minuteBars = {
    success: true,
    data: Array.from({ length: 16 }, (_, i) => ({
      openTime: t0 + i * 60_000,
      open: 30750,
      high: 30760,
      low: 30740,
      close: 30750,
      volume: 1,
    })),
  }
  state.request.mockImplementation((url: string) =>
    Promise.resolve(
      url.includes('/open-orders?') ? { success: true, data: [] } : minuteBars
    )
  )
  render(
    <AdvancedChart
      symbol="MNQ"
      traderID="trader"
      interval="1m"
      mentorLevels={[{ price: 30700, label: 'mentor box top' }]}
      mentorTrendlines={[
        {
          side: 'long',
          p0_ms: t0 + 120_000,
          p0_px: 97,
          p1_ms: t0 + 480_000,
          p1_px: 98.2,
          now_ms: t0 + 780_000,
          now_px: 99.2,
          in_level: true,
          valid: true,
        },
      ]}
    />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  const titles = state.line.mock.calls.map(
    (c) => (c[0] as { title: string }).title
  )
  expect(titles).toContain('mentor box top')
  const lineSeries = state.addSeries.mock.calls.filter(
    (c) =>
      c[0] === 'line' &&
      String((c[1] as { title?: string }).title).startsWith('trend line')
  )
  expect(lineSeries).toHaveLength(1)
  const pts = state.lineData.mock.calls.at(-1)?.[0] as {
    time: number
    value: number
  }[]
  // m134: VALID extends to the right edge (the last drawn 1m bar).
  expect(pts.map((p) => Math.round(p.value * 100) / 100)).toEqual([
    97, 98.2, 99.6,
  ])
  expect(pts.map((p) => p.time)).toEqual([
    (t0 + 120_000) / 1000,
    (t0 + 480_000) / 1000,
    (t0 + 900_000) / 1000,
  ])
})

it('m150: trendline series are DIFFED by key — same keys update in place, one vanished key removes exactly one', async () => {
  vi.useFakeTimers()
  const t0 = 1789480800000
  const minuteBars = {
    success: true,
    data: Array.from({ length: 16 }, (_, i) => ({
      openTime: t0 + i * 60_000,
      open: 30750,
      high: 30760,
      low: 30740,
      close: 30750,
      volume: 1,
    })),
  }
  state.request.mockImplementation((url: string) =>
    Promise.resolve(
      url.includes('/open-orders?') ? { success: true, data: [] } : minuteBars
    )
  )
  const mk = (side: string, p0: number, p1: number) => ({
    side,
    p0_ms: p0,
    p0_px: 97,
    p1_ms: p1,
    p1_px: 98,
    now_ms: p1,
    now_px: 98,
    in_level: true,
    valid: true,
  })
  const two = [
    mk('long', t0 + 120_000, t0 + 480_000),
    mk('short', t0 + 240_000, t0 + 600_000),
  ]
  const { rerender } = render(
    <AdvancedChart
      symbol="MNQ"
      traderID="trader"
      interval="1m"
      mentorTrendlines={two}
    />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  const trendAdds = () =>
    state.addSeries.mock.calls.filter(
      (c) =>
        c[0] === 'line' &&
        String((c[1] as { title?: string }).title).startsWith('trend line')
    ).length
  expect(trendAdds()).toBe(2)
  const addBefore = state.addSeries.mock.calls.length
  const removeBefore = state.removeSeries.mock.calls.length

  // Same keys, NEW array reference → the effect re-runs but must update in
  // place: no removeSeries and no new addSeries.
  rerender(
    <AdvancedChart
      symbol="MNQ"
      traderID="trader"
      interval="1m"
      mentorTrendlines={two.map((t) => ({ ...t }))}
    />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  expect(state.removeSeries.mock.calls.length - removeBefore).toBe(0)
  expect(state.addSeries.mock.calls.length - addBefore).toBe(0)

  // One key gone → exactly one removeSeries, no re-add of the survivor.
  rerender(
    <AdvancedChart
      symbol="MNQ"
      traderID="trader"
      interval="1m"
      mentorTrendlines={[two[0]]}
    />
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
  })
  expect(state.removeSeries.mock.calls.length - removeBefore).toBe(1)
  expect(state.addSeries.mock.calls.length - addBefore).toBe(0)
})
