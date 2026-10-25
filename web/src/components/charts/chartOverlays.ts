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
  /** m421b: false = draw the line + title but no right-axis label (a nearby
   * line already carries the axis label; see dedupeAxisLabels). */
  axisLabel?: boolean
}

/** One sloped segment (a trendline): points in chart time (UTC seconds). */
export interface TrendSegment {
  /** A stable identity (side:anchors) so the chart can DIFF series by key
   * instead of remove/re-add on every poll (m150). */
  key: string
  label: string
  color: string
  points: { time: number; value: number }[]
  /** 0 solid · 1 dotted · 2 dashed (m134: VALID solid, FORMING dashed). */
  style?: 0 | 1 | 2
  /** m134: VALID = 2, FORMING = 1 (lightweight-charts LineWidth subset). */
  lineWidth?: 1 | 2
}

/** One EMA curve point snapped to a drawn bar's time. */
export interface EMACurvePoint {
  time: number // UTC seconds — a bar the chart actually drew
  value: number
}

/**
 * m150/m169: the EMA 9/34 curve, snapped onto the chart's drawn bars. The
 * backend series is per-1m-bar (33 h deep); plotting it directly adds extra
 * slots to the shared time axis (candle gaps) on any other interval. For each
 * drawn bar we take the LAST 1m EMA whose own bar has CLOSED at or before the
 * drawn bar's close (open + 60 s <= close) and drop anything before
 * barTimes[0], so every plotted time is a drawn bar and the curve never leads
 * price by one bar (a point stamped at a bar's open already folds in that
 * bar's close, so it must not be read until the bar has closed).
 * m397: a bar whose newest usable point is stale (a hole in the series) gets
 * NO point, so the caller draws a gap instead of a held flat line.
 */
export function mentorEMACurves(
  emaSeries: MentorTruth['ema_series'],
  barTimes: readonly number[]
): { ema9: EMACurvePoint[]; ema34: EMACurvePoint[] } {
  const ema9: EMACurvePoint[] = []
  const ema34: EMACurvePoint[] = []
  const pts = emaSeries ?? []
  if (pts.length === 0 || barTimes.length === 0) return { ema9, ema34 }
  // m402: each point carries its own bar length (tf_min 1 or 2; absent = 1).
  const barMsOf = (pt: { tf_min?: number }) =>
    pt.tf_min === 2 ? 120_000 : 60_000
  const EMA_BAR_MS = 60_000 // the 1m floor for the interval / nominal window
  // The chart's nominal interval: the smallest positive bar spacing (robust to
  // a session-break gap between the first two bars).
  let intervalSec = 0
  for (let k = 1; k < barTimes.length; k++) {
    const d = barTimes[k] - barTimes[k - 1]
    if (d > 0 && (intervalSec === 0 || d < intervalSec)) intervalSec = d
  }
  // m397: a bar is drawn only while the newest consumed 1m point is no more
  // than ONE chart interval (min one 1m bar) older than the bar's nominal
  // window end. A hole in the backend series (e.g. a bot restart) must leave a
  // visible gap, never a held flat value. Tolerance = one interval: it admits
  // a single missing 1m bar on a 1m chart and ordinary lag on higher charts.
  let i = 0
  let e9 = 0
  let e34 = 0
  for (let bi = 0; bi < barTimes.length; bi++) {
    const bt = barTimes[bi]
    const closeSec =
      bi + 1 < barTimes.length ? barTimes[bi + 1] : bt + intervalSec
    // m169: include a point only once its OWN bar has closed. `ms <= close`
    // included the 1m bar that OPENS at the close, whose value already folds
    // in that bar's close — one bar of look-ahead on a 1m chart.
    while (i < pts.length && pts[i].ms + barMsOf(pts[i]) <= closeSec * 1000) {
      e9 = pts[i].ema9
      e34 = pts[i].ema34
      i++
    }
    const nominalCloseMs =
      (bt + Math.max(intervalSec, EMA_BAR_MS / 1000)) * 1000
    const newestBarMs = i > 0 ? barMsOf(pts[i - 1]) : EMA_BAR_MS
    const newestClosedMs = i > 0 ? pts[i - 1].ms + newestBarMs : -Infinity
    // m402: tolerance covers one chart interval OR one bar of the newest
    // point's own frame (a 2m point is naturally up to 2m behind a 1m bar).
    const toleranceMs = Math.max(intervalSec * 1000, newestBarMs)
    if (i > 0 && nominalCloseMs - newestClosedMs <= toleranceMs) {
      ema9.push({ time: bt, value: e9 })
      ema34.push({ time: bt, value: e34 })
    }
  }
  return { ema9, ema34 }
}

/**
 * m402: the frame (1 or 2 minutes) the engine's EMA is using NOW = the tf_min
 * of the LAST point (the series is sorted by ms). Absent tf_min = 1 (older
 * backend).
 */
export function mentorEMATf(emaSeries: MentorTruth['ema_series']): 1 | 2 {
  const pts = emaSeries ?? []
  return pts.length > 0 && pts[pts.length - 1].tf_min === 2 ? 2 : 1
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
export const MENTOR_MUTED_COLOR = '#9A8A5E' // m134: a FORMING trendline (waiting for its 3rd touch)

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
  const pairedEdges = pairedBoxEdgeKeys(truth)
  for (const l of truth.levels ?? []) {
    // m134/m325: ema9/ema34 are drawn as CURVES (mentorEMASeries), not flat
    // lines; ema34_htf (the EMA34 location line) is drawn as a short right-edge
    // segment by mentorEMALocationSegment, never a full-width flat line.
    if (
      !(l.price > 0) ||
      l.kind === 'trendline' ||
      l.kind === 'ema9' ||
      l.kind === 'ema34' ||
      l.kind === 'ema34_htf'
    )
      continue
    const merged = (l.merged_from ?? []).filter((p) => p !== l.price)
    const also =
      merged.length > 0 ? ` · also ${merged.map(fmt).join(', ')}` : ''
    if (pairedEdges.has(l.key)) continue // drawn as a rectangle (mentorBoxRects)
    out.push({
      price: l.price,
      label: `mentor ${mentorLevelLabel(l)}${also}`,
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
  return dedupeAxisLabels(
    out
      .filter((l) => l.price > 0)
      .map((l) => (mark ? { ...l, label: l.label + mark } : l))
  )
}

/** m421b: a level's chart label names its kind AND its price, so two key
 * levels (an old high, a 1H open) are told apart instead of all reading
 * "mentor key level". */
export function mentorLevelLabel(l: {
  kind: string
  key: string
  price: number
}): string {
  const base = mentorKindLabel(l.kind, l.key)
  switch (l.kind) {
    case 'key_level':
      return `${base} ${fmt(l.price)}`
    case 'old_extreme':
      return `${base} ${fmt(l.price)}`
    case 'trigger_retest':
    case 'htf_trigger':
    case 'wick_microscalp':
      return `${base} ${fmt(l.price)}`
    default:
      return base
  }
}

/** Price distance (points) under which two right-axis labels would overlap. */
export const AXIS_LABEL_MIN_GAP = 4

/**
 * m421b: when several lines sit within `minGap` points of each other, keep
 * every LINE but show ONE axis label per cluster: the first (by input order)
 * keeps its label and absorbs the others' titles; the rest lose their axis
 * label and title. Never drops a line.
 */
export function dedupeAxisLabels(
  specs: ChartLineSpec[],
  minGap = AXIS_LABEL_MIN_GAP
): ChartLineSpec[] {
  const order = specs
    .map((s, i) => ({ s, i }))
    .sort((a, b) => a.s.price - b.s.price || a.i - b.i)
  const out: ChartLineSpec[] = specs.map((s) => ({ ...s }))
  let k = 0
  while (k < order.length) {
    let j = k + 1
    while (j < order.length && order[j].s.price - order[j - 1].s.price < minGap)
      j++
    if (j - k > 1) {
      const cluster = order.slice(k, j).sort((a, b) => a.i - b.i)
      const head = cluster[0].i
      out[head].label = cluster.map((c) => c.s.label).join(' + ')
      for (const c of cluster.slice(1)) {
        out[c.i].axisLabel = false
        out[c.i].label = ''
      }
    }
    k = j
  }
  return out
}

/** One FTGH/FTGL box as a filled rectangle (m421b). Times are UTC seconds of
 * bars the chart actually drew. */
export interface MentorBoxRect {
  key: string
  label: string
  top: number
  bottom: number
  startTime: number
  endTime: number
  color: string
  fill: string
}

type BoxLevel = NonNullable<MentorTruth['levels']>[number]

function boxEdgePairs(
  truth: MentorTruth | null | undefined
): { base: string; top: BoxLevel; bottom: BoxLevel }[] {
  if (!truth || !truth.enabled) return []
  const tops = new Map<string, BoxLevel>()
  const bottoms = new Map<string, BoxLevel>()
  for (const l of truth.levels ?? []) {
    if (!(l.price > 0)) continue
    if (l.kind === 'ftgh_edge' && l.key.endsWith(':top'))
      tops.set(l.key.slice(0, -4), l)
    else if (l.kind === 'ftgl_edge' && l.key.endsWith(':bottom'))
      bottoms.set(l.key.slice(0, -7), l)
  }
  const out: { base: string; top: BoxLevel; bottom: BoxLevel }[] = []
  for (const [base, top] of tops) {
    const bottom = bottoms.get(base)
    if (bottom) out.push({ base, top, bottom })
  }
  return out
}

/** Keys of the box edges that mentorBoxRects draws as a rectangle (they are
 * then NOT also drawn as horizontal lines). */
export function pairedBoxEdgeKeys(
  truth: MentorTruth | null | undefined
): Set<string> {
  const s = new Set<string>()
  for (const p of boxEdgePairs(truth)) {
    s.add(p.top.key)
    s.add(p.bottom.key)
  }
  return s
}

/**
 * m421b: each box = a rectangle from its formation to its end (or the last
 * bar while it is live). Start = the level's formed_ms, else drawn_at (>0),
 * else the first drawn bar; end = ended_ms, else the last drawn bar. Both are
 * snapped onto drawn bars (barTimes ascending, UTC seconds). Nothing is
 * drawn when there are no bars.
 */
export function mentorBoxRects(
  truth: MentorTruth | null | undefined,
  barTimes: number[],
  staleSinceMs?: number
): MentorBoxRect[] {
  if (barTimes.length === 0) return []
  const first = barTimes[0]
  const last = barTimes[barTimes.length - 1]
  const snap = (sec: number): number => {
    let r = first
    for (const t of barTimes) {
      if (t <= sec) r = t
      else break
    }
    return r
  }
  const mark = mentorStaleMark(staleSinceMs)
  const out: MentorBoxRect[] = []
  for (const { base, top, bottom } of boxEdgePairs(truth)) {
    const startMs = top.formed_ms ?? (top.drawn_at > 0 ? top.drawn_at : 0)
    const start = startMs > 0 ? snap(Math.floor(startMs / 1000)) : first
    const end = top.ended_ms ? snap(Math.floor(top.ended_ms / 1000)) : last
    if (end < start) continue
    const kind = base.startsWith('ftgl') ? 'FTGL' : 'FTGH'
    const tf = top.tf ? ` ${top.tf}` : ''
    out.push({
      key: base,
      label: `mentor ${kind} box${tf} ${fmt(top.price)}-${fmt(bottom.price)}${mark}`,
      top: top.price,
      bottom: bottom.price,
      startTime: start,
      endTime: end,
      color: MENTOR_COLOR,
      fill: 'rgba(232, 163, 61, 0.14)',
    })
  }
  return out
}

/**
 * m325: the EMA34 location line (kind ema34_htf) must never draw as a
 * full-width flat line through history — it is a CURRENT-VALUE snapshot. It is
 * drawn instead as a SHORT horizontal segment at the right edge (the last
 * drawn bar). The truth payload does not carry the ema34_tf knob, so we cannot
 * know whether it is the 1m EMA34 (already drawn as a curve) or a
 * higher-timeframe EMA with no curve — so we always keep it short, which is
 * correct in both cases and never a flat line through every candle.
 */
export function mentorEMALocationSegment(
  price: number | undefined,
  barTimes: readonly number[],
  staleSinceMs?: number,
  emaTf: 1 | 2 = 1
): TrendSegment | null {
  if (!(price && price > 0) || barTimes.length === 0) return null
  const last = barTimes[barTimes.length - 1]
  const prev = barTimes.length > 1 ? barTimes[barTimes.length - 2] : last
  return {
    key: 'ema34_htf',
    label: `EMA34 location (${emaTf}m, current value)${mentorStaleMark(staleSinceMs)}`,
    color: MENTOR_COLOR,
    style: 2,
    lineWidth: 1,
    points: [
      { time: prev, value: price },
      { time: last, value: price },
    ],
  }
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
 * Trendline segments (m134): FORMING lines (two anchors, no 3rd touch) draw as
 * thin dashed muted "waiting for 3rd touch"; VALID lines (3rd touch, not dead)
 * draw as solid width-2 "trend line ✓", extended to the right edge. UG-6
 * (release #24): every point is snapped to the open time of the chart bar that
 * CONTAINS it — read from the bars the chart actually drew (`barTimes`, UTC
 * seconds, ascending) — and re-valued ON THE SAME LINE. A fixed grid missed
 * 6h/8h/12h/3d/1w (they fell back to 1m) and put 4h/1d on the UTC grid while
 * NT8's bars are session-aligned, so a point could land on a time slot no
 * candle has. No bars yet → nothing to snap to → no segment.
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
  const lastBar = barTimes[barTimes.length - 1]

  // m150: FORMING lines are unbounded — every same-role swing pair of the day
  // becomes its own series, rebuilt each poll. Cap to the NEWEST 3 per side;
  // VALID lines stay uncapped (they are the ones the bot actually trades).
  type MentorTrendline = NonNullable<MentorTruth['trendlines']>[number]
  const FORMING_CAP = 3
  const formingBySide = new Map<string, MentorTrendline[]>()
  for (const tl of trendlines ?? []) {
    if (tl.valid) continue
    const arr = formingBySide.get(tl.side) ?? []
    arr.push(tl)
    formingBySide.set(tl.side, arr)
  }
  const formingAllowed = new Set<MentorTrendline>()
  for (const arr of formingBySide.values()) {
    arr.sort((a, b) => b.p1_ms - a.p1_ms)
    for (let i = 0; i < Math.min(arr.length, FORMING_CAP); i++) {
      formingAllowed.add(arr[i])
    }
  }

  for (const tl of trendlines ?? []) {
    if (tl.p1_ms === tl.p0_ms) continue
    if (!tl.valid && !formingAllowed.has(tl)) continue
    const slope = (tl.p1_px - tl.p0_px) / (tl.p1_ms - tl.p0_ms)
    const valueAt = (ms: number) => tl.p0_px + slope * (ms - tl.p0_ms)
    // m134/m328: EVERY trendline extends to the right edge ("kéo dài" — the
    // mentor physically extends the line to see where price meets it). Only a
    // TRADED valid line is solid; a valid line inside a box (not traded) and a
    // FORMING line extend DASHED + muted to the right edge too.
    const inBox = tl.valid && !tl.in_level
    const traded = tl.valid && !inBox
    const endMs = lastBar * 1000
    const times = Array.from(
      new Set([tl.p0_ms, tl.p1_ms, endMs].map(snap))
    ).sort((a, b) => a - b)
    if (times.length < 2) continue
    const role = tl.side === 'long' ? 'support' : 'resistance'
    out.push({
      key: `${tl.side}:${tl.p0_ms}:${tl.p1_ms}`,
      label: tl.valid
        ? `trend line ✓ (${role}${inBox ? ', inside a box — not traded' : ''})${mark}`
        : `trend line (${role} — waiting for 3rd touch)${mark}`,
      color: traded ? MENTOR_COLOR : MENTOR_MUTED_COLOR,
      style: traded ? 0 : 2,
      lineWidth: traded ? 2 : 1,
      points: times.map((sec) => ({
        time: sec,
        value: valueAt(sec * 1000),
      })),
    })
  }
  return out
}

/** The working protective bracket of an OPEN position (T32, ui-studio-truth):
 * `{ stop, target, source }` from /api/positions. Absent field / zero price
 * draws nothing — never a fabricated SL/TP line. */
export interface PositionBracket {
  stop?: number
  target?: number
  source?: string
}

export function positionBracketLines(
  bracket: PositionBracket | null | undefined
): ChartLineSpec[] {
  if (!bracket) return []
  const out: ChartLineSpec[] = []
  if ((bracket.stop ?? 0) > 0) {
    out.push({
      price: bracket.stop!,
      label: 'position SL',
      color: ORDER_COLORS.stopLoss,
      style: 2,
    })
  }
  if ((bracket.target ?? 0) > 0) {
    out.push({
      price: bracket.target!,
      label: 'position TP',
      color: ORDER_COLORS.takeProfit,
      style: 2,
    })
  }
  return out
}
