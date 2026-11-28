// m242 trade-marker pins (P1, P3) — the pure builder extracted from
// AdvancedChart.tsx. These pin the snap rules directly so the chart's marker
// placement is testable without mounting the chart.
//
// P1 (same-5m / different-1m snap): two fills inside ONE 5m candle snap to the
//   SAME candle open; two fills one minute apart on a 1m chart snap to
//   DIFFERENT candles. Mutant: snap by nearest candle (rounding) instead of the
//   candle whose open <= t → the same-candle pair splits (RED).
// P3 (interval switch re-snap): the SAME trade list re-snaps when the interval
//   changes. A fill at t0+90s snaps to t0 on 5m but to t0+60 on 1m. Mutant:
//   hard-code intervalSec to 300 → the 1m build returns t0 instead of t0+60
//   (RED).

import { describe, expect, it } from 'vitest'
import {
  buildTradeMarkers,
  intervalToSeconds,
  type TradeMarkerInput,
} from './tradeMarkers'

const t0 = 1_700_000_000 // a 5m boundary (divisible by 300)

const trade = (
  time: number,
  price = 100,
  side: 'long' | 'short' = 'long',
  action: 'open' | 'close' = 'open',
  rawSide = side === 'long' ? 'buy' : 'sell'
): TradeMarkerInput => ({ time, price, side, rawSide, action })

const klines = (open: number, step: number, n: number): number[] =>
  Array.from({ length: n }, (_, i) => open + i * step)

describe('intervalToSeconds', () => {
  it('parses the chart interval suffixes', () => {
    expect(intervalToSeconds('1m')).toBe(60)
    expect(intervalToSeconds('5m')).toBe(300)
    expect(intervalToSeconds('15m')).toBe(900)
    expect(intervalToSeconds('1h')).toBe(3600)
    expect(intervalToSeconds('4h')).toBe(14400)
    expect(intervalToSeconds('1d')).toBe(86400)
    expect(intervalToSeconds('bogus')).toBe(300)
  })
})

describe('buildTradeMarkers — P1 snap', () => {
  it('snaps two fills inside one 5m candle to the SAME candle open', () => {
    const times = klines(t0, 300, 6)
    const markers = buildTradeMarkers(
      [trade(t0 + 30), trade(t0 + 90)],
      times,
      300
    )
    expect(markers).toHaveLength(2)
    expect(markers[0].time).toBe(t0 as never)
    expect(markers[1].time).toBe(t0 as never)
  })

  it('snaps two fills a minute apart on a 1m chart to DIFFERENT candles', () => {
    const times = klines(t0, 60, 6)
    const markers = buildTradeMarkers(
      [trade(t0 + 30), trade(t0 + 90)],
      times,
      60
    )
    expect(markers).toHaveLength(2)
    expect(markers[0].time).toBe(t0 as never)
    expect(markers[1].time).toBe((t0 + 60) as never)
  })
})

describe('buildTradeMarkers — P3 interval switch', () => {
  it('re-snaps the same trade list when the interval changes', () => {
    const trade5 = buildTradeMarkers([trade(t0 + 90)], klines(t0, 300, 4), 300)
    const trade1 = buildTradeMarkers([trade(t0 + 90)], klines(t0, 60, 8), 60)
    expect(trade5[0].time).toBe(t0 as never) // 5m: [t0, t0+300)
    expect(trade1[0].time).toBe((t0 + 60) as never) // 1m: [t0+60, t0+120)
    expect(trade5[0].time).not.toBe(trade1[0].time)
  })
})

describe('buildTradeMarkers — F1/F3 invariants', () => {
  it('accepts a fill inside the still-forming candle (t < newestOpen + intervalSec)', () => {
    const times = klines(t0, 300, 3) // newest open = t0+600
    const markers = buildTradeMarkers([trade(t0 + 600 + 120)], times, 300)
    expect(markers).toHaveLength(1)
    expect(markers[0].time).toBe((t0 + 600) as never)
  })

  it('drops a fill beyond the forming-candle window', () => {
    const times = klines(t0, 300, 3) // newest open = t0+600
    const markers = buildTradeMarkers([trade(t0 + 600 + 300)], times, 300)
    expect(markers).toHaveLength(0)
  })

  it('draws entries as arrows and exits as xB/xS squares at the fill price', () => {
    // One trade per candle: a lone marker keeps its fill price (m439 moves
    // only same-candle markers off it).
    const times = klines(t0, 300, 4)
    const markers = buildTradeMarkers(
      [
        trade(t0 + 10, 101, 'long', 'open', 'buy'),
        trade(t0 + 310, 102, 'long', 'close', 'sell'),
        trade(t0 + 610, 103, 'short', 'open', 'sell'),
        trade(t0 + 910, 104, 'short', 'close', 'buy'),
      ],
      times,
      300
    )
    expect(markers).toHaveLength(4)
    expect(markers[0]).toMatchObject({
      shape: 'arrowUp',
      text: 'B',
      position: 'atPriceMiddle',
      price: 101,
    })
    expect(markers[1]).toMatchObject({
      shape: 'square',
      text: 'xS',
      position: 'atPriceMiddle',
      price: 102,
    })
    expect(markers[2]).toMatchObject({
      shape: 'arrowDown',
      text: 'S',
      position: 'atPriceMiddle',
      price: 103,
    })
    expect(markers[3]).toMatchObject({
      shape: 'square',
      text: 'xB',
      position: 'atPriceMiddle',
      price: 104,
    })
  })
})

// m439 (owner 2026-10-29: "label B and sell overlap on one candle"): 2+
// markers on one candle leave the fill price — buys stack BELOW the bar,
// sells ABOVE it — so a B and an xS at near-equal prices no longer draw on
// top of each other. Mutant: drop the per-candle rule → both stay
// atPriceMiddle (RED).
describe('buildTradeMarkers — m439 same-candle labels do not overlap', () => {
  it('splits a same-candle B + xS below/above the bar; a lone marker keeps its price', () => {
    const times = klines(t0, 300, 3)
    const markers = buildTradeMarkers(
      [
        trade(t0 + 10, 29706.5, 'long', 'open', 'buy'),
        trade(t0 + 80, 29710, 'long', 'close', 'sell'),
        trade(t0 + 310, 29720, 'short', 'open', 'sell'),
      ],
      times,
      300
    )
    expect(markers).toHaveLength(3)
    expect(markers[0]).toMatchObject({
      text: 'B',
      shape: 'arrowUp',
      position: 'belowBar',
    })
    expect(markers[1]).toMatchObject({
      text: 'xS',
      shape: 'square',
      position: 'aboveBar',
    })
    expect(markers[0]).not.toHaveProperty('price')
    expect(markers[1]).not.toHaveProperty('price')
    expect(markers[0].time).toBe(markers[1].time)
    expect(markers[2]).toMatchObject({
      text: 'S',
      position: 'atPriceMiddle',
      price: 29720,
    })
  })

  it('keeps both sells above the bar when two sells share a candle', () => {
    const markers = buildTradeMarkers(
      [
        trade(t0 + 10, 100, 'short', 'open', 'sell'),
        trade(t0 + 20, 101, 'long', 'close', 'sell'),
      ],
      klines(t0, 300, 2),
      300
    )
    expect(markers.map((m) => m.position)).toEqual(['aboveBar', 'aboveBar'])
  })
})
