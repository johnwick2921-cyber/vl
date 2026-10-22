import type { SeriesMarker, Time } from 'lightweight-charts'

// Trade markers (m242) — the pure builder that turns recorded trades into
// lightweight-charts v5 series markers. Extracted from AdvancedChart.tsx so the
// snapping/glyph rules are testable without mounting the chart.

export interface TradeMarkerInput {
  time: number // epoch seconds of the fill
  price: number
  side: 'long' | 'short' // the POSITION side
  rawSide: string // the actual trade side (long opens with a buy, exits with a sell)
  action: 'open' | 'close'
  symbol?: string
}

export function intervalToSeconds(interval: string): number {
  const m = /^(\d+)([mhd])$/.exec(String(interval).trim().toLowerCase())
  if (!m) return 300
  const n = Number(m[1])
  switch (m[2]) {
    case 'm':
      return n * 60
    case 'h':
      return n * 3600
    case 'd':
      return n * 86400
    default:
      return 300
  }
}

// buildTradeMarkers maps trades onto the chart's candles.
//   - A trade snaps to the candle whose OPEN time <= trade time. A fill inside
//     the still-forming candle (trade time < newestOpen + intervalSec) snaps to
//     the NEWEST candle instead of being dropped (F1 — it used to appear one
//     candle late, up to 5 min on 5m).
//   - Entries are arrows (arrowUp for a buy, arrowDown for a sell); exits are
//     squares ("xB" / "xS"). All are placed AT the fill price via
//     'atPriceMiddle' + price (F3 — they used to be circles at the bar edge,
//     and entry/exit looked the same). A same-candle entry+exit stays as two
//     markers, never merged.
export function buildTradeMarkers(
  trades: TradeMarkerInput[],
  klineTimes: number[],
  intervalSec: number
): SeriesMarker<Time>[] {
  if (klineTimes.length === 0 || trades.length === 0) return []

  const klineMinTime = klineTimes[0]
  const klineMaxTime = klineTimes[klineTimes.length - 1]

  const snap = (t: number): number | null => {
    // F1: the forming candle counts — accept t up to (exclusive) the next open.
    if (t < klineMinTime || t >= klineMaxTime + intervalSec) return null
    let left = 0
    let right = klineTimes.length - 1
    while (left < right) {
      const mid = Math.ceil((left + right + 1) / 2)
      if (klineTimes[mid] <= t) left = mid
      else right = mid - 1
    }
    return klineTimes[left]
  }

  const out: SeriesMarker<Time>[] = []
  for (const tr of trades) {
    if (!(tr.time > 0) || !(tr.price > 0)) continue
    const candleTime = snap(tr.time)
    if (candleTime === null) continue
    const buy = tr.rawSide === 'buy'
    const entry = tr.action === 'open'
    out.push({
      time: candleTime as Time,
      position: 'atPriceMiddle',
      price: tr.price,
      shape: entry ? (buy ? 'arrowUp' : 'arrowDown') : 'square',
      color: buy ? '#0ECB81' : '#F6465D',
      text: entry ? (buy ? 'B' : 'S') : buy ? 'xB' : 'xS',
      size: 1,
    })
  }
  out.sort((a, b) => (a.time as number) - (b.time as number))
  return out
}
