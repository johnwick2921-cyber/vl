// Chart overlays (release #22, UI-GUIDE-TRUTH T5 + T7).
//
// Pure functions: the dashboard chart draws ONLY what these return, so the
// tests that pin them pin the chart. Nothing here invents a price — every line
// comes from a field the API sent (/api/open-orders, /api/mentor/truth).
import type { MentorSwingLeg, MentorTruth } from '../../lib/api/traders'
import { openOrderSideLabel } from './openOrderSide'

/** One horizontal price line. `style`: 0 solid · 1 dotted · 2 dashed. */
export interface ChartLineSpec {
  price: number
  label: string
  color?: string
  style?: 0 | 1 | 2
}

/** One sloped segment (a trendline): points in chart time (UTC seconds). */
export interface TrendSegment {
  label: string
  color: string
  points: { time: number; value: number }[]
}

/** The /api/open-orders row as the chart reads it. */
export interface ChartOpenOrder {
  order_id?: string
  symbol?: string // the instrument the row is an order on (UG-2)
  side?: string // BUY/SELL
  position_side?: string
  type: string // LIMIT / STOP_MARKET / TAKE_PROFIT_MARKET
  price: number // ledger rows: the ENTRY price
  stop_price: number // ledger rows: the bracket STOP; venue rows: the trigger
  quantity: number
  status?: string
  arm_state?: string
  source?: string
  target_price?: number
  zone_lo?: number
  zone_hi?: number
  /** UG-5 (release #24): a split mentor arm — leg 1's own TP for leg1_qty
   * contracts; target_price is then the runner's (quantity − leg1_qty). */
  leg1_tp?: number
  leg1_qty?: number
}

export const ORDER_COLORS = {
  limitEntry: '#F0B90B', // yellow — a resting limit entry
  stopEntry: '#FF8C00', // orange — a resting stop(-limit) entry, never the red SL
  stopLoss: '#F6465D',
  takeProfit: '#0ECB81',
  zone: '#B58CFF',
} as const

export const MENTOR_COLOR = '#E8A33D' // mentor amber — distinct from order lines

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2))

// UG-2 (release #24): CME roots, longest first so "MNQ" wins over "NQ" —
// the same set and rule as market/futures_symbol.go futuresRoot.
const CME_ROOTS = [
  'MNQ',
  'MES',
  'M2K',
  'MYM',
  'MCL',
  'MGC',
  'NQ',
  'ES',
  'RTY',
  'YM',
  'CL',
  'NG',
  'GC',
  'SI',
  'ZB',
  'ZN',
  'ZF',
  'ZT',
]

/** The CME root of any symbol form ("MNQ", "MNQZ6", "MNQ 12-26", "NQ.c.0"),
 * or '' when no known root matches. */
export function chartFuturesRoot(symbol: string | undefined): string {
  const s = (symbol ?? '').trim().toUpperCase()
  for (const r of CME_ROOTS) {
    if (
      s === r ||
      s.startsWith(r + '.') ||
      s.startsWith(r + ' ') ||
      (s.length === r.length + 2 &&
        /^[FGHJKMNQUVXZ][0-9]$/.test(s.slice(r.length)))
    )
      return r
  }
  return ''
}

/** Do two symbols name the same instrument? (same CME root; otherwise an
 * exact, case-insensitive match). An empty side never matches. */
export function sameChartSymbol(
  a: string | undefined,
  b: string | undefined
): boolean {
  const x = (a ?? '').trim()
  const y = (b ?? '').trim()
  if (!x || !y) return false
  const rx = chartFuturesRoot(x)
  const ry = chartFuturesRoot(y)
  if (rx || ry) return rx === ry
  return x.toUpperCase() === y.toUpperCase()
}

/** UG-2: only the rows that are orders on the chart's symbol. A row with no
 * symbol field is kept (the server already answered for this symbol). */
export function ordersForChartSymbol<T extends { symbol?: string }>(
  orders: T[],
  chartSymbol: string
): T[] {
  return orders.filter(
    (o) => !o.symbol || sameChartSymbol(o.symbol, chartSymbol)
  )
}

/** UG-3 (release #24): the mark a stale mentor read carries on every line —
 * the lines stay drawn (never hidden), and say how old they are. */
export function mentorStaleMark(staleSinceMs: number | undefined): string {
  if (staleSinceMs === undefined) return ''
  if (!(staleSinceMs > 0)) return ' · STALE'
  const hhmm = new Date(staleSinceMs).toLocaleTimeString('en-US', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'America/Chicago',
  })
  return ` · stale since ${hhmm} CT`
}

/**
 * A row from the armed_orders LEDGER is always an ENTRY order: `price` is the
 * entry and `stop_price` is the protective stop it will carry once filled.
 * (Before release #22 a ledger stop-entry was drawn as a red "SL 1" at its
 * protective stop — the entry was never drawn and the size was wrong.)
 */
export function isLedgerEntry(o: ChartOpenOrder): boolean {
  return (
    (o.source ?? '').startsWith('armed_orders ledger') ||
    (o.arm_state ?? '') !== ''
  )
}

/** Not at the broker (authorized but never sent, or sent and unconfirmed). */
export function orderNotAtBroker(o: ChartOpenOrder): boolean {
  return o.status === 'ARMED' || (o.status ?? '').startsWith('PENDING')
}

/** The price lines one open order draws. Empty = draw nothing. */
export function orderPriceLines(o: ChartOpenOrder): ChartLineSpec[] {
  if (orderNotAtBroker(o)) return []
  const type = o.type ?? ''
  const isStopKind = type.includes('STOP') || type.includes('SL')
  if (isLedgerEntry(o)) {
    if (!(o.price > 0)) return []
    const kind = isStopKind ? 'Stop entry' : 'Limit'
    const side = openOrderSideLabel(o) // #512 T6: one canonical side label (SHORT never renders as BUY)
    const sl = o.stop_price > 0 ? ` · SL ${fmt(o.stop_price)}` : ''
    // UG-5 (release #24): a split arm has TWO take-profits — leg 1's own for
    // leg1_qty contracts and the runner's for the rest. Both, from the row.
    const leg1Qty = o.leg1_qty ?? 0
    const split = (o.leg1_tp ?? 0) > 0 && leg1Qty > 0 && leg1Qty < o.quantity
    const tp = split
      ? ` · TP1 ${fmt(o.leg1_tp!)} ×${leg1Qty}` +
        ((o.target_price ?? 0) > 0
          ? ` · TP2 ${fmt(o.target_price!)} ×${o.quantity - leg1Qty}`
          : '')
      : (o.target_price ?? 0) > 0
        ? ` · TP ${fmt(o.target_price!)}`
        : ''
    // T29: a cancel that is sent but not yet confirmed is not a resting
    // order — draw it dotted and say so.
    const cancelSent = (o.status ?? '').startsWith('CANCEL_PENDING')
    const lines: ChartLineSpec[] = [
      {
        price: o.price,
        label:
          `${kind} ${side} ${o.quantity}${sl}${tp}${cancelSent ? ' · cancel sent' : ''}`.replace(
            /\s+/g,
            ' '
          ),
        color: isStopKind ? ORDER_COLORS.stopEntry : ORDER_COLORS.limitEntry,
        style: cancelSent ? 1 : 2,
      },
    ]
    if ((o.zone_lo ?? 0) > 0 && (o.zone_hi ?? 0) > 0) {
      lines.push(
        {
          price: o.zone_lo!,
          label: 'zone low',
          color: ORDER_COLORS.zone,
          style: 1,
        },
        {
          price: o.zone_hi!,
          label: 'zone high',
          color: ORDER_COLORS.zone,
          style: 1,
        }
      )
    }
    return lines
  }
  // A venue's own order book: a STOP order is the stop, a TP is the target.
  const price =
    isStopKind && o.stop_price > 0
      ? o.stop_price
      : o.price > 0
        ? o.price
        : o.stop_price
  if (!(price > 0)) return []
  if (isStopKind) {
    return [
      {
        price,
        label: `SL ${o.quantity}`,
        color: ORDER_COLORS.stopLoss,
        style: 2,
      },
    ]
  }
  if (type.includes('TAKE_PROFIT') || type.includes('TP')) {
    return [
      {
        price,
        label: `TP ${o.quantity}`,
        color: ORDER_COLORS.takeProfit,
        style: 2,
      },
    ]
  }
  if (type === 'LIMIT') {
    return [
      {
        price,
        label: `Limit ${openOrderSideLabel(o)} ${o.quantity}`,
        color: ORDER_COLORS.limitEntry,
        style: 2,
      },
    ]
  }
  return [
    {
      price,
      label: `${type} ${o.quantity}`,
      color: ORDER_COLORS.limitEntry,
      style: 2,
    },
  ]
}

/** Readable name for a mentor level kind (the evaluator's own taxonomy). */
export function mentorKindLabel(kind: string, key = ''): string {
  switch (kind) {
    case 'key_level':
      return 'key level'
    case 'old_extreme':
      return 'old high/low'
    case 'ema34':
      return 'EMA34'
    case 'ema9':
      return 'EMA9'
    case 'ema34_htf':
      return 'EMA34 location'
    case 'trigger_retest':
      return '5m trigger'
    case 'htf_trigger':
      return key.includes('4h')
        ? '4h trigger'
        : key.includes('1h')
          ? '1h trigger'
          : 'HTF trigger'
    case 'ftgh_edge':
      return 'box top'
    case 'ftgl_edge':
      return 'box bottom'
    case 'trendline':
      return 'trendline'
    case 'wick_microscalp':
      return 'wick target'
    default:
      return kind
  }
}

/**
 * Horizontal lines for everything the mentor evaluator holds this tick:
 * levels (key levels, old highs/lows, EMAs, trigger lines, EMA34 location,
 * box edges), the ISB rest boxes, the ORB and the swing line with the swing's
 * order / position. Trendlines are SLOPED — see mentorTrendSegments.
 */
export function mentorChartLines(
  truth: MentorTruth | null | undefined,
  staleSinceMs?: number
): ChartLineSpec[] {
  if (!truth || !truth.enabled) return []
  const out: ChartLineSpec[] = []
  for (const l of truth.levels ?? []) {
    if (!(l.price > 0) || l.kind === 'trendline') continue
    const merged = (l.merged_from ?? []).filter((p) => p !== l.price)
    const also =
      merged.length > 0 ? ` · also ${merged.map(fmt).join(', ')}` : ''
    out.push({
      price: l.price,
      label: `mentor ${mentorKindLabel(l.kind, l.key)}${also}`,
      color: MENTOR_COLOR,
      style: 2,
    })
  }
  for (const b of truth.isb_boxes ?? []) {
    out.push(
      {
        price: b.high,
        label: `mentor ${b.label} high`,
        color: MENTOR_COLOR,
        style: 1,
      },
      {
        price: b.low,
        label: `mentor ${b.label} low`,
        color: MENTOR_COLOR,
        style: 1,
      }
    )
  }
  if (truth.orb) {
    const st = truth.orb.status ? ` (${truth.orb.status})` : ''
    out.push(
      {
        price: truth.orb.high,
        label: `mentor ORB high${st}`,
        color: MENTOR_COLOR,
        style: 1,
      },
      {
        price: truth.orb.low,
        label: `mentor ORB low${st}`,
        color: MENTOR_COLOR,
        style: 1,
      }
    )
  }
  if (truth.swing) {
    out.push({
      price: truth.swing.line,
      label: 'mentor swing line (4h EMA34)',
      color: MENTOR_COLOR,
      style: 0,
    })
    // The swing's RESTING order is an armed_orders row and is drawn with the
    // open orders; the FILLED leg's stop and target are drawn only here.
    const pos: MentorSwingLeg | undefined = truth.swing.pos
    if (pos && pos.stop > 0)
      out.push({
        price: pos.stop,
        label: swingStopLabel(pos),
        color: ORDER_COLORS.stopLoss,
        style: 2,
      })
    if (pos && pos.target > 0)
      out.push({
        price: pos.target,
        label: 'swing position TP',
        color: ORDER_COLORS.takeProfit,
        style: 2,
      })
  }
  const mark = mentorStaleMark(staleSinceMs)
  return out
    .filter((l) => l.price > 0)
    .map((l) => (mark ? { ...l, label: l.label + mark } : l))
}

/** UG-1 (release #24): the filled swing leg's stop says where it was read. */
function swingStopLabel(pos: MentorSwingLeg): string {
  if (pos.stop_source === 'broker')
    return pos.moved_be && pos.stop === pos.entry
      ? 'swing position SL · break-even (NT8)'
      : 'swing position SL (NT8)'
  if (pos.stop_source === 'entry')
    return pos.moved_be
      ? 'swing position SL · entry stop — BE move sent, NT8 book unread'
      : 'swing position SL · entry stop (NT8 book unread)'
  return 'swing position SL'
}

/**
 * Trendline segments. Each VALID line runs from its first swing anchor to the
 * value it trades at on the current bar (or to its second anchor while a box
 * suppresses it). UG-6 (release #24): every point is snapped to the open time
 * of the chart bar that CONTAINS it — read from the bars the chart actually
 * drew (`barTimes`, UTC seconds, ascending) — and re-valued ON THE SAME LINE.
 * A fixed grid missed 6h/8h/12h/3d/1w (they fell back to 1m) and put 4h/1d on
 * the UTC grid while NT8's bars are session-aligned, so a point could land on
 * a time slot no candle has. No bars yet → nothing to snap to → no segment.
 */
export function mentorTrendSegments(
  trendlines: MentorTruth['trendlines'],
  barTimes: readonly number[],
  staleSinceMs?: number
): TrendSegment[] {
  const out: TrendSegment[] = []
  if (barTimes.length === 0) return out
  const snap = (ms: number): number => {
    const sec = ms / 1000
    let lo = 0
    let hi = barTimes.length - 1
    if (sec <= barTimes[0]) return barTimes[0]
    // the last bar that opened at or before the point
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1
      if (barTimes[mid] <= sec) lo = mid
      else hi = mid - 1
    }
    return barTimes[lo]
  }
  const mark = mentorStaleMark(staleSinceMs)
  for (const tl of trendlines ?? []) {
    if (tl.p1_ms === tl.p0_ms) continue
    const slope = (tl.p1_px - tl.p0_px) / (tl.p1_ms - tl.p0_ms)
    const valueAt = (ms: number) => tl.p0_px + slope * (ms - tl.p0_ms)
    const endMs = tl.in_level && tl.now_ms > tl.p1_ms ? tl.now_ms : tl.p1_ms
    const times = Array.from(
      new Set([tl.p0_ms, tl.p1_ms, endMs].map(snap))
    ).sort((a, b) => a - b)
    if (times.length < 2) continue
    const role = tl.side === 'long' ? 'support' : 'resistance'
    out.push({
      label: `mentor trendline (${role}${tl.in_level ? '' : ', inside a box — not traded'})${mark}`,
      color: MENTOR_COLOR,
      points: times.map((sec) => ({
        time: sec,
        value: valueAt(sec * 1000),
      })),
    })
  }
  return out
}
