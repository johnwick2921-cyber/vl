// Chart overlays (release #22, UI-GUIDE-TRUTH T5 + T7).
//
// Pure functions: the dashboard chart draws ONLY what these return, so the
// tests that pin them pin the chart. Nothing here invents a price — every line
// comes from a field the API sent (/api/open-orders, /api/mentor/truth).
import type { MentorSwingLeg, MentorTruth } from '../../lib/api/traders'

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
    const side = (o.side ?? '').toUpperCase()
    const sl = o.stop_price > 0 ? ` · SL ${fmt(o.stop_price)}` : ''
    const tp = (o.target_price ?? 0) > 0 ? ` · TP ${fmt(o.target_price!)}` : ''
    const lines: ChartLineSpec[] = [
      {
        price: o.price,
        label: `${kind} ${side} ${o.quantity}${sl}${tp}`.replace(/\s+/g, ' '),
        color: isStopKind ? ORDER_COLORS.stopEntry : ORDER_COLORS.limitEntry,
        style: 2,
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
        label: `Limit ${o.side ?? ''} ${o.quantity}`,
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
  truth: MentorTruth | null | undefined
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
        label: 'swing position SL',
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
  return out.filter((l) => l.price > 0)
}

const INTERVAL_MS: Record<string, number> = {
  '1m': 60_000,
  '3m': 180_000,
  '5m': 300_000,
  '15m': 900_000,
  '30m': 1_800_000,
  '1h': 3_600_000,
  '2h': 7_200_000,
  '4h': 14_400_000,
  '1d': 86_400_000,
}

/**
 * Trendline segments. Each VALID line runs from its first swing anchor to the
 * value it trades at on the current bar (or to its second anchor while a box
 * suppresses it). Points are snapped to the chart's bar grid and re-valued ON
 * THE SAME LINE, so a 1m anchor never inserts a stray time slot on a 5m chart.
 */
export function mentorTrendSegments(
  trendlines: MentorTruth['trendlines'],
  interval = '5m'
): TrendSegment[] {
  const step = INTERVAL_MS[interval] ?? 60_000
  const out: TrendSegment[] = []
  for (const tl of trendlines ?? []) {
    if (tl.p1_ms === tl.p0_ms) continue
    const slope = (tl.p1_px - tl.p0_px) / (tl.p1_ms - tl.p0_ms)
    const valueAt = (ms: number) => tl.p0_px + slope * (ms - tl.p0_ms)
    const endMs = tl.in_level && tl.now_ms > tl.p1_ms ? tl.now_ms : tl.p1_ms
    const times = Array.from(
      new Set(
        [tl.p0_ms, tl.p1_ms, endMs].map((ms) => Math.floor(ms / step) * step)
      )
    ).sort((a, b) => a - b)
    if (times.length < 2) continue
    const role = tl.side === 'long' ? 'support' : 'resistance'
    out.push({
      label: `mentor trendline (${role}${tl.in_level ? '' : ', inside a box — not traded'})`,
      color: MENTOR_COLOR,
      points: times.map((ms) => ({
        time: Math.floor(ms / 1000),
        value: valueAt(ms),
      })),
    })
  }
  return out
}
