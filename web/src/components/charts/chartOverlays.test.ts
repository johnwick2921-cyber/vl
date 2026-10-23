// T7 (release #22): the mentor places the chart draws, from /api/mentor/truth.
// Every line comes from a payload field; nothing is computed that the API did
// not send. Mutant: drop the ISB / ORB / swing blocks → RED.
import { describe, expect, it } from 'vitest'
import type { MentorTruth } from '../../lib/api/traders'
import {
  chartFuturesRoot,
  mentorChartLines,
  mentorEMACurves,
  mentorEMATf,
  mentorEMALocationSegment,
  mentorStaleMark,
  mentorTrendSegments,
  orderPriceLines,
  ordersForChartSymbol,
  positionBracketLines,
  sameChartSymbol,
} from './chartOverlays'

const truth = (extra: Partial<MentorTruth>): MentorTruth =>
  ({
    enabled: true,
    as_of_ms: 0,
    htf: {
      four_h_dir: '',
      four_h_since: 0,
      one_h_dir: '',
      one_h_since: 0,
      verdict: '',
      gate_active: false,
    },
    trigger_5m: { dir: '', price: 0, since: 0 },
    depth_line: '',
    window: { start: '08:30', minutes: 60, active: true, ended: false },
    done_after_win: false,
    stop_after_loss: false,
    ...extra,
  }) as MentorTruth

describe('mentorChartLines', () => {
  it('labels every place the evaluator holds and names merged old highs/lows', () => {
    const lines = mentorChartLines(
      truth({
        levels: [
          {
            key: 'kl',
            kind: 'key_level',
            price: 30000,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'oe',
            kind: 'old_extreme',
            price: 30100,
            drawn_at: 0,
            visits_today: 0,
            merged_from: [30100, 30110],
          },
          {
            key: 'trigger_retest',
            kind: 'trigger_retest',
            price: 30050,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'htf_4h_trigger',
            kind: 'htf_trigger',
            price: 29900,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'ema',
            kind: 'ema34_htf',
            price: 30020,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'b:top',
            kind: 'ftgh_edge',
            price: 30200,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'b:bottom',
            kind: 'ftgl_edge',
            price: 30180,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'tl',
            kind: 'trendline',
            price: 30010,
            drawn_at: 0,
            visits_today: 0,
          },
        ],
        isb_boxes: [{ label: 'ISB 5m', high: 30060, low: 30040 }],
        orb: { label: 'ORB', high: 30070, low: 30030, status: 'escaped long' },
        swing: {
          line: 29800,
          pos: { side: 'long', entry: 29810, stop: 29780, target: 29900 },
        },
      })
    )
    const byLabel = Object.fromEntries(lines.map((l) => [l.label, l.price]))
    expect(byLabel['mentor key level']).toBe(30000)
    expect(byLabel['mentor old high/low · also 30110']).toBe(30100)
    expect(byLabel['mentor 5m trigger']).toBe(30050)
    expect(byLabel['mentor 4h trigger']).toBe(29900)
    // m325: the EMA34 location line is NOT a flat line — it is a short
    // right-edge segment (mentorEMALocationSegment), never a full-width line.
    expect(byLabel['mentor EMA34 location']).toBeUndefined()
    expect(byLabel['mentor box top']).toBe(30200)
    expect(byLabel['mentor box bottom']).toBe(30180)
    expect(byLabel['mentor ISB 5m high']).toBe(30060)
    expect(byLabel['mentor ISB 5m low']).toBe(30040)
    expect(byLabel['mentor ORB high (escaped long)']).toBe(30070)
    expect(byLabel['mentor swing line (4h EMA34)']).toBe(29800)
    expect(byLabel['swing position SL']).toBe(29780)
    expect(byLabel['swing position TP']).toBe(29900)
    // A trendline is sloped: never drawn as a flat line at today's value.
    expect(lines.some((l) => l.label.includes('trendline'))).toBe(false)
  })

  it('never draws ema34 / ema9 / ema34_htf as flat lines (key_level still drawn)', () => {
    const lines = mentorChartLines(
      truth({
        levels: [
          {
            key: 'kl',
            kind: 'key_level',
            price: 30000,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'e34',
            kind: 'ema34',
            price: 29609.78,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'e9',
            kind: 'ema9',
            price: 29612.99,
            drawn_at: 0,
            visits_today: 0,
          },
          {
            key: 'ehtf',
            kind: 'ema34_htf',
            price: 29609.52,
            drawn_at: 0,
            visits_today: 0,
          },
        ],
      })
    )
    expect(lines.some((l) => l.label === 'mentor key level')).toBe(true)
    expect(
      lines.some((l) => l.label.includes('EMA34') || l.label.includes('EMA9'))
    ).toBe(false)
  })

  it('draws nothing when mentor mode is off, and nothing invented while computing', () => {
    expect(
      mentorChartLines(
        truth({
          enabled: false,
          levels: [
            {
              key: 'k',
              kind: 'key_level',
              price: 1,
              drawn_at: 0,
              visits_today: 0,
            },
          ],
        })
      )
    ).toEqual([])
    expect(mentorChartLines(truth({ computing: true }))).toEqual([])
  })
})

// The chart's 5m bars from t0 (UTC seconds) — what AdvancedChart drew.
const bars = (t0Ms: number, stepMs: number, n: number) =>
  Array.from({ length: n }, (_, i) => (t0Ms + i * stepMs) / 1000)

describe('mentorEMALocationSegment', () => {
  it('draws the EMA34 location as a SHORT right-edge segment, never a flat line', () => {
    const t0 = 1789480800000
    const b = bars(t0, 300_000, 6) // 6 five-minute bars, UTC seconds
    const seg = mentorEMALocationSegment(30020, b)
    expect(seg).not.toBeNull()
    expect(seg!.key).toBe('ema34_htf')
    expect(seg!.label).toBe('EMA34 location (1m, current value)')
    expect(seg!.points).toHaveLength(2)
    // both points hold the CURRENT value, spanning only the last drawn bar.
    expect(seg!.points[0].value).toBe(30020)
    expect(seg!.points[1].value).toBe(30020)
    expect(seg!.points[0].time).toBe(b[b.length - 2])
    expect(seg!.points[1].time).toBe(b[b.length - 1])
  })

  it('returns null with no price or no bars', () => {
    expect(mentorEMALocationSegment(undefined, [1])).toBeNull()
    expect(mentorEMALocationSegment(0, [1])).toBeNull()
    expect(mentorEMALocationSegment(30020, [])).toBeNull()
  })
})

describe('mentorTrendSegments', () => {
  it('snaps anchors to the bar grid and re-values them on the same line', () => {
    const t0 = 1789480800000
    const [seg] = mentorTrendSegments(
      [
        {
          side: 'short',
          p0_ms: t0 + 60_000,
          p0_px: 110,
          p1_ms: t0 + 660_000,
          p1_px: 104,
          now_ms: t0 + 960_000,
          now_px: 101,
          in_level: true,
          valid: true,
        },
      ],
      bars(t0, 300_000, 6)
    )
    expect(seg.label).toBe('trend line ✓ (resistance)')
    expect(seg.lineWidth).toBe(2)
    expect(seg.style).toBe(0)
    // m134: VALID extends to the right edge (the last drawn bar).
    expect(seg.points.map((p) => p.time)).toEqual([
      t0 / 1000,
      (t0 + 600_000) / 1000,
      (t0 + 1_500_000) / 1000,
    ])
    // slope = -6 / 600000 per ms: 110.6 at t0, 104.6 at +10m, 95.6 at +25m
    expect(seg.points.map((p) => Math.round(p.value * 100) / 100)).toEqual([
      110.6, 104.6, 95.6,
    ])
  })

  it('a FORMING line is thin dashed muted, says "waiting for 3rd touch", and extends dashed to the right edge', () => {
    const [seg] = mentorTrendSegments(
      [
        {
          side: 'long',
          p0_ms: 0,
          p0_px: 1,
          p1_ms: 120_000,
          p1_px: 3,
          now_ms: 0,
          now_px: 0,
          in_level: false,
          valid: false,
        },
      ],
      bars(0, 60_000, 5)
    )
    expect(seg.label).toBe('trend line (support — waiting for 3rd touch)')
    expect(seg.label).not.toContain('not traded')
    expect(seg.lineWidth).toBe(1)
    expect(seg.style).toBe(2)
    // m328: a forming line extends DASHED to the right edge ("kéo dài"), never
    // mislabelled "not traded" and never stopped at its second anchor.
    expect(seg.points).toHaveLength(3)
    expect(seg.points[seg.points.length - 1].time).toBe(240_000 / 1000)
  })

  it('a box-suppressed VALID line is muted dashed, says "not traded", and extends dashed to the right edge', () => {
    const [seg] = mentorTrendSegments(
      [
        {
          side: 'long',
          p0_ms: 0,
          p0_px: 1,
          p1_ms: 120_000,
          p1_px: 3,
          now_ms: 0,
          now_px: 0,
          in_level: false,
          valid: true,
        },
      ],
      bars(0, 60_000, 5)
    )
    // m328: a VALID line inside a box (not traded) is muted + thin + dashed but
    // STILL extends to the right edge — only a traded valid line is solid.
    expect(seg.label).toContain('trend line ✓')
    expect(seg.label).toContain('not traded')
    expect(seg.lineWidth).toBe(1)
    expect(seg.style).toBe(2)
    expect(seg.points).toHaveLength(3)
    expect(seg.points[seg.points.length - 1].time).toBe(240_000 / 1000)
  })
})

// UG-6 (release #24): points snap to the bars the chart REALLY drew. The
// old fixed grid had no 6h/8h/12h/3d/1w (1m fallback: a point between candles)
// and put 4h on the UTC grid while NT8's 4h bars open on the 17:00 CT session.
// Mutant: snap with Math.floor(ms / 14_400_000) * 14_400_000 → RED.
describe('UG-6 mentorTrendSegments snaps to the chart bar times', () => {
  // Session-aligned 4h bars: 17:00 CT = 22:00 UTC (CDT), so they open at
  // 22:00, 02:00, 06:00, … UTC — never on the UTC 00/04/08 grid.
  const open = Date.UTC(2026, 9, 12, 22, 0) // 2026-10-12 22:00 UTC
  const h4 = bars(open, 4 * 3_600_000, 8)
  const tl = {
    side: 'long',
    p0_ms: open + 5 * 3_600_000, // 03:00 UTC → inside the 02:00 bar
    p0_px: 100,
    p1_ms: open + 13 * 3_600_000, // 11:00 UTC → inside the 10:00 bar
    p1_px: 108,
    now_ms: open + 21 * 3_600_000, // 19:00 UTC → inside the 18:00 bar
    now_px: 116,
    in_level: true,
    valid: true,
  }
  it('every point is a bar the chart drew (4h, session-aligned)', () => {
    const [seg] = mentorTrendSegments([tl], h4)
    const drawn = new Set(h4)
    // m134: VALID extends to the right edge (the last drawn 4h bar).
    expect(seg.points.map((p) => p.time)).toEqual([
      (open + 4 * 3_600_000) / 1000,
      (open + 12 * 3_600_000) / 1000,
      (open + 28 * 3_600_000) / 1000,
    ])
    for (const p of seg.points) expect(drawn.has(p.time)).toBe(true)
    // re-valued on the same line: slope 1 pt/h → 99 at the 02:00 bar open
    expect(seg.points[0].value).toBeCloseTo(99, 6)
  })
  it('a timeframe the old grid lacked (12h) still lands on its bars', () => {
    const h12 = bars(open, 12 * 3_600_000, 4)
    const [seg] = mentorTrendSegments([tl], h12)
    const drawn = new Set(h12)
    for (const p of seg.points) expect(drawn.has(p.time)).toBe(true)
    // m134: p0, p1 and the right edge (the last 12h bar).
    expect(seg.points).toHaveLength(3)
  })
  it('no bars yet → nothing drawn (never a guessed grid)', () => {
    expect(mentorTrendSegments([tl], [])).toEqual([])
  })
})

// UG-1 (release #24): the filled swing leg's stop says where it was read.
describe('UG-1 swing stop label', () => {
  const sw = (pos: Partial<NonNullable<MentorTruth['swing']>['pos']>) =>
    mentorChartLines(
      truth({
        swing: {
          line: 29800,
          pos: {
            side: 'long',
            entry: 29810,
            stop: 29780,
            target: 29900,
            ...pos,
          },
        },
      })
    ).find((l) => l.label.startsWith('swing position SL'))
  it('a broker stop at the entry is the break-even, drawn at the BE price', () => {
    const l = sw({ stop: 29810, stop_source: 'broker', moved_be: true })
    expect(l?.price).toBe(29810)
    expect(l?.label).toBe('swing position SL · break-even (NT8)')
  })
  it('an unread book says it is the entry stop and that a BE move was sent', () => {
    const l = sw({ stop: 29780, stop_source: 'entry', moved_be: true })
    expect(l?.price).toBe(29780)
    expect(l?.label).toBe(
      'swing position SL · entry stop — BE move sent, NT8 book unread'
    )
  })
})

// UG-3 (release #24): a stale read keeps its lines and marks every one.
// Mutant: return '' from mentorStaleMark → RED.
describe('UG-3 stale mentor lines are marked, never hidden', () => {
  const asOf = Date.UTC(2026, 9, 14, 15, 7) // 10:07 CT
  const t = truth({
    as_of_ms: asOf,
    levels: [
      {
        key: 'kl',
        kind: 'key_level',
        price: 30000,
        drawn_at: 0,
        visits_today: 0,
      },
    ],
    trendlines: [
      {
        side: 'long',
        p0_ms: 0,
        p0_px: 1,
        p1_ms: 120_000,
        p1_px: 3,
        now_ms: 0,
        now_px: 0,
        in_level: true,
      },
    ],
  })
  it('every horizontal line carries "stale since HH:MM CT"', () => {
    const lines = mentorChartLines(t, asOf)
    expect(lines).toHaveLength(1)
    expect(lines[0].label).toBe('mentor key level · stale since 10:07 CT')
    expect(lines[0].price).toBe(30000)
  })
  it('trendlines carry it too', () => {
    const [seg] = mentorTrendSegments(t.trendlines, bars(0, 60_000, 4), asOf)
    expect(seg.label).toContain('stale since 10:07 CT')
  })
  it('a fresh read carries no mark', () => {
    expect(mentorChartLines(t)[0].label).toBe('mentor key level')
    expect(mentorStaleMark(undefined)).toBe('')
  })
})

// UG-2 (release #24): an overlay is a price on ONE instrument.
// Mutant: make sameChartSymbol return true → RED.
describe('UG-2 symbol matching', () => {
  it('reads the CME root of every symbol form', () => {
    expect(chartFuturesRoot('MNQ')).toBe('MNQ')
    expect(chartFuturesRoot('mnqz6')).toBe('MNQ')
    expect(chartFuturesRoot('MNQ 12-26')).toBe('MNQ')
    expect(chartFuturesRoot('NQ.c.0')).toBe('NQ')
    expect(chartFuturesRoot('AAPL')).toBe('')
  })
  it('matches the same instrument only', () => {
    expect(sameChartSymbol('MNQ', 'MNQZ6')).toBe(true)
    expect(sameChartSymbol('NQ', 'MNQ')).toBe(false)
    expect(sameChartSymbol('ES', 'MNQ')).toBe(false)
    expect(sameChartSymbol('AAPL', 'MNQ')).toBe(false)
    expect(sameChartSymbol('MNQ', undefined)).toBe(false)
  })
  it('drops order rows that name another instrument', () => {
    const rows = [{ symbol: 'MNQ', id: 1 }, { symbol: 'ES', id: 2 }, { id: 3 }]
    expect(ordersForChartSymbol(rows, 'MNQ').map((r) => r.id)).toEqual([1, 3])
    expect(ordersForChartSymbol(rows, 'ES').map((r) => r.id)).toEqual([2, 3])
  })
})

describe('orderPriceLines', () => {
  // UG-5 (release #24): row 362 — SHORT stop-entry, 3 contracts, leg 1 = 2 at
  // 30725.25, runner = 1 at 30703. Both TPs are drawn in the label.
  // Mutant: ignore leg1_tp → RED.
  it("UG-5: a split mentor stop-entry names both legs' take-profits", () => {
    const [l] = orderPriceLines({
      type: 'STOP_MARKET',
      side: 'SELL',
      position_side: 'SHORT',
      price: 30742.75,
      stop_price: 30760.25,
      quantity: 3,
      status: 'NEW',
      arm_state: 'working',
      source:
        'armed_orders ledger (broker acceptance is a separate observation)',
      target_price: 30703,
      leg1_tp: 30725.25,
      leg1_qty: 2,
    })
    expect(l.price).toBe(30742.75)
    expect(l.label).toBe(
      'Stop entry SELL 3 · SL 30760.25 · TP1 30725.25 ×2 · TP2 30703 ×1'
    )
  })

  it('keeps a venue STOP order as the SL (non-ledger books are unchanged)', () => {
    expect(
      orderPriceLines({
        type: 'STOP_MARKET',
        price: 0,
        stop_price: 100,
        quantity: 1,
        status: 'NEW',
      })
    ).toEqual([{ price: 100, label: 'SL 1', color: '#F6465D', style: 2 }])
  })
  it('draws a ledger limit at its entry with the planner zone', () => {
    const lines = orderPriceLines({
      type: 'LIMIT',
      side: 'SELL',
      price: 30815,
      stop_price: 30854.97,
      quantity: 1,
      status: 'NEW',
      arm_state: 'working',
      source:
        'armed_orders ledger (broker acceptance is a separate observation)',
      zone_lo: 30810,
      zone_hi: 30820,
    })
    expect(lines.map((l) => [l.label, l.price])).toEqual([
      ['Limit SELL 1 · SL 30854.97', 30815],
      ['zone low', 30810],
      ['zone high', 30820],
    ])
  })
  it('T29: a cancel_pending ledger row says the cancel was sent and draws dotted', () => {
    const [l] = orderPriceLines({
      type: 'LIMIT',
      side: 'SELL',
      price: 30815,
      stop_price: 30854.97,
      quantity: 1,
      status: 'CANCEL_PENDING — cancel sent, not yet confirmed',
      arm_state: 'cancel_pending',
      source:
        'armed_orders ledger (broker acceptance is a separate observation)',
    })
    expect(l.label).toBe('Limit SELL 1 · SL 30854.97 · cancel sent')
    expect(l.style).toBe(1)
  })
  it('draws nothing for a row that is not at the broker', () => {
    expect(
      orderPriceLines({
        type: 'LIMIT',
        price: 1,
        stop_price: 0,
        quantity: 1,
        status: 'ARMED',
        arm_state: 'armed',
      })
    ).toEqual([])
  })
})

describe('positionBracketLines (T32)', () => {
  it('draws the open position SL and TP from the broker bracket', () => {
    expect(
      positionBracketLines({
        stop: 30500,
        target: 30750,
        source: 'nt8_order_snapshot',
      })
    ).toEqual([
      { price: 30500, label: 'position SL', color: '#F6465D', style: 2 },
      { price: 30750, label: 'position TP', color: '#0ECB81', style: 2 },
    ])
  })

  it('draws only the known side and never a zero line', () => {
    expect(
      positionBracketLines({ stop: 30500, source: 'nt8_order_snapshot' })
    ).toEqual([
      { price: 30500, label: 'position SL', color: '#F6465D', style: 2 },
    ])
    // stop: 0 must not draw a line — unknown is absent, never zero.
    expect(
      positionBracketLines({ stop: 0, target: 0, source: 'nt8_order_snapshot' })
    ).toEqual([])
    expect(positionBracketLines(undefined)).toEqual([])
    expect(positionBracketLines(null)).toEqual([])
  })
})

// m150 — the EMA 9/34 curve is snapped to the drawn bars (it is a 1m series
// served to charts on any interval; plotted raw it adds extra time slots).
describe('mentorEMACurves (m150 snap)', () => {
  const t0 = 1789480800000 // a 5m bar open
  // 5m chart bars
  const fiveM = bars(t0, 300_000, 4)
  // 1m EMA series reaching 33h back — starts well left of barTimes[0].
  const ema = Array.from({ length: 25 }, (_, i) => ({
    ms: t0 - 120_000 + i * 60_000, // -2m .. +22m (covers all four 5m bars)
    ema9: 100 + i,
    ema34: 200 + i,
  }))

  it('plots only at drawn bar times, never at the raw 1m times', () => {
    const curves = mentorEMACurves(ema, fiveM)
    const drawn = new Set(fiveM)
    for (const p of [...curves.ema9, ...curves.ema34]) {
      expect(drawn.has(p.time)).toBe(true)
    }
    // Every drawn bar gets a point; the pre-chart -2m/-1m points are dropped.
    expect(curves.ema9.map((p) => p.time)).toEqual(fiveM)
  })

  it('takes the LAST EMA value whose own 1m bar has closed at or before each bar close', () => {
    const curves = mentorEMACurves(ema, fiveM)
    // bar[0] (t0): close = t0+5m → the last point with open+60s <= close is
    // ema[6] (open t0+4m → closes t0+5m), value 106.
    expect(curves.ema9[0].value).toBe(106)
    expect(curves.ema9[1].value).toBe(111)
  })

  it('m169: never looks ahead — a point that OPENS at the bar close is excluded until its own bar has closed', () => {
    // 5m bars [t0, t0+5m]; 1m points at t0+3m (closes t0+4m), t0+4m (closes
    // t0+5m == bar[0] close) and t0+5m (opens AT bar[0] close, closes t0+6m).
    const straddle = [
      { ms: t0 + 180_000, ema9: 1, ema34: 10 },
      { ms: t0 + 240_000, ema9: 2, ema34: 20 },
      { ms: t0 + 300_000, ema9: 3, ema34: 30 },
    ]
    const twoBars = bars(t0, 300_000, 2)
    const curves = mentorEMACurves(straddle, twoBars)
    // bar[0] close t0+5m: the t0+5m point (open == close) is NOT yet valid.
    expect(curves.ema9.map((p) => p.time)).toEqual([
      t0 / 1000,
      (t0 + 300_000) / 1000,
    ])
    expect(curves.ema9[0].value).toBe(2)
    expect(curves.ema34[0].value).toBe(20)
    // bar[1] close t0+10m: the t0+5m point has now closed → it is the value.
    expect(curves.ema9[1].value).toBe(3)
    expect(curves.ema34[1].value).toBe(30)
  })

  it('m397: a 458-min hole in the series leaves NO held flat values and drawing resumes after it', () => {
    const m = 60_000
    const bars1m = bars(t0, m, 600) // 600 continuous 1m candles
    const holeFrom = 50 // minutes after t0
    const holeLen = 458
    const series = Array.from({ length: 600 }, (_, i) => ({
      ms: t0 + i * m,
      ema9: 1000 + i,
      ema34: 2000 + i,
    })).filter((p) => {
      const k = (p.ms - t0) / m
      return k < holeFrom || k >= holeFrom + holeLen
    })
    const c = mentorEMACurves(series, bars1m)
    const drawn = new Set(c.ema9.map((p) => (p.time * 1000 - t0) / m))
    // inside the hole (past one tolerance interval) nothing is drawn
    for (let k = holeFrom + 3; k < holeFrom + holeLen; k++) {
      expect(drawn.has(k)).toBe(false)
    }
    // at most ONE bar (the one-1m-bar tolerance) inside the hole holds a value
    const inHole = c.ema9.filter((p) => {
      const k = (p.time * 1000 - t0) / m
      return k >= holeFrom && k < holeFrom + holeLen
    })
    expect(inHole.length).toBeLessThanOrEqual(1)
    // before the hole and after it drawing is live
    expect(drawn.has(holeFrom - 1)).toBe(true)
    expect(drawn.has(holeFrom + holeLen + 1)).toBe(true)
    expect(drawn.has(599)).toBe(true)
  })

  it('m397: a 5m chart also breaks across a hole but not across ordinary lag', () => {
    const five = bars(t0, 300_000, 120)
    const series = Array.from({ length: 600 }, (_, i) => ({
      ms: t0 + i * 60_000,
      ema9: i,
      ema34: i,
    })).filter((p) => {
      const k = (p.ms - t0) / 60_000
      return k < 100 || k >= 400
    })
    const c = mentorEMACurves(series, five)
    const idx = new Set(c.ema9.map((p) => (p.time * 1000 - t0) / 300_000))
    expect(idx.has(10)).toBe(true) // before the hole
    expect(idx.has(30)).toBe(false) // inside
    expect(idx.has(90)).toBe(true) // after (k=450)
  })

  it('an empty series or no bars yields empty curves', () => {
    expect(mentorEMACurves([], fiveM)).toEqual({ ema9: [], ema34: [] })
    expect(mentorEMACurves(ema, [])).toEqual({ ema9: [], ema34: [] })
  })
})

// m402 — the engine's EMA follows the mentor's execution frame: 1m for the
// first 30 min after the 08:30 CT open, then 2m. Each point is read only after
// ITS OWN bar (60s or 120s) has closed.
describe('mentorEMACurves (m402 per-point tf_min)', () => {
  const t0 = 1789480800000
  const m = 60_000
  const mixed = [
    // 1m points: minutes 0..29 (value = minute)
    ...Array.from({ length: 30 }, (_, i) => ({
      ms: t0 + i * m,
      ema9: i,
      ema34: 500 + i,
      tf_min: 1 as const,
    })),
    // 2m points: bar opens at minutes 30,32,..,58 (value = 1000 + minute)
    ...Array.from({ length: 15 }, (_, j) => ({
      ms: t0 + (30 + 2 * j) * m,
      ema9: 1000 + 30 + 2 * j,
      ema34: 1500 + 30 + 2 * j,
      tf_min: 2 as const,
    })),
  ]
  const at = (c: { time: number; value: number }[], minute: number) =>
    c.find((p) => p.time === t0 / 1000 + minute * 60)?.value

  it('5m chart: correct value per drawn bar, no look-ahead', () => {
    const c = mentorEMACurves(mixed, bars(t0, 300_000, 12))
    expect(at(c.ema9, 0)).toBe(4) // close 5m -> 1m pt 4
    expect(at(c.ema9, 25)).toBe(29) // close 30m -> 1m pt 29
    // bar 30 (close 35m): 2m pt 32 (closes 34) yes, pt 34 (closes 36) NO
    expect(at(c.ema9, 30)).toBe(1032)
    expect(at(c.ema34, 30)).toBe(1532)
    // bar 35 (close 40m): pt 38 closes 40 -> 1038; pt 40 must not be read
    expect(at(c.ema9, 35)).toBe(1038)
    expect(c.ema9.length).toBe(12)
  })

  it('1m chart: switches at the boundary, 2m points wait for their own close, no flat hold', () => {
    const c = mentorEMACurves(mixed, bars(t0, m, 60))
    expect(at(c.ema9, 29)).toBe(29) // close 30m -> 1m pt 29
    expect(at(c.ema9, 30)).toBe(29) // 2m pt 30 closes 32 -> not yet
    expect(at(c.ema9, 31)).toBe(1030) // close 32m -> 2m pt 30 closed
    expect(at(c.ema9, 32)).toBe(1030)
    expect(at(c.ema9, 33)).toBe(1032)
    expect(at(c.ema9, 58)).toBe(1056) // close 59m -> 2m pt 58 NOT yet
    expect(at(c.ema9, 59)).toBe(1058)
    // every bar drawn: a 2m point's natural lag is not mistaken for a hole
    expect(c.ema9.length).toBe(60)
  })

  it('1m chart: a missing 2m span breaks the line instead of holding flat', () => {
    const holed = mixed.filter(
      (p) => !(p.tf_min === 2 && p.ms >= t0 + 40 * m && p.ms < t0 + 52 * m)
    )
    const c = mentorEMACurves(holed, bars(t0, m, 60))
    expect(at(c.ema9, 50)).toBeUndefined()
    expect(at(c.ema9, 58)).toBeDefined()
  })

  it('absent tf_min behaves exactly as 1m (legacy payload)', () => {
    const legacy = Array.from({ length: 25 }, (_, i) => ({
      ms: t0 - 120_000 + i * m,
      ema9: 100 + i,
      ema34: 200 + i,
    }))
    const tagged = legacy.map((p) => ({ ...p, tf_min: 1 as const }))
    const five = bars(t0, 300_000, 4)
    expect(mentorEMACurves(legacy, five)).toEqual(mentorEMACurves(tagged, five))
    expect(mentorEMACurves(legacy, five).ema9[0].value).toBe(106)
    expect(mentorEMATf(legacy)).toBe(1)
  })

  it('labels follow the LAST point tf_min', () => {
    expect(mentorEMATf(mixed)).toBe(2)
    expect(mentorEMATf(mixed.slice(0, 30))).toBe(1)
    expect(mentorEMATf(undefined)).toBe(1)
    const bt = [t0 / 1000, t0 / 1000 + 60]
    expect(mentorEMALocationSegment(100, bt, undefined, 2)!.label).toBe(
      'EMA34 location (2m, current value)'
    )
    expect(mentorEMALocationSegment(100, bt, undefined, 1)!.label).toBe(
      'EMA34 location (1m, current value)'
    )
  })
})

// m150 — FORMING trendlines are capped at the newest 3 per side.
describe('mentorTrendSegments forming cap (m150)', () => {
  const t0 = 1789480800000
  const mk = (side: string, p1Ms: number, valid = false) => ({
    side,
    p0_ms: p1Ms - 120_000,
    p0_px: 100,
    p1_ms: p1Ms,
    p1_px: 102,
    now_ms: p1Ms,
    now_px: 102,
    in_level: true,
    valid,
  })
  const grid = bars(t0, 300_000, 40)

  it('keeps only the newest 3 forming lines per side', () => {
    const tls = [
      mk('long', t0 + 1 * 300_000),
      mk('long', t0 + 2 * 300_000),
      mk('long', t0 + 3 * 300_000),
      mk('long', t0 + 4 * 300_000), // newest long
      mk('long', t0 + 5 * 300_000), // newest long (dropped: older than 3)
      mk('short', t0 + 6 * 300_000),
    ]
    const segs = mentorTrendSegments(tls, grid)
    const long = segs.filter((s) => s.key.startsWith('long:'))
    const short = segs.filter((s) => s.key.startsWith('short:'))
    expect(long).toHaveLength(3)
    expect(short).toHaveLength(1)
    // The kept forming longs are the newest 3: +3m, +4m, +5m (p1_ms desc).
    expect(long.map((s) => s.key).sort()).toEqual(
      [
        `long:${t0 + 3 * 300_000 - 120_000}:${t0 + 3 * 300_000}`,
        `long:${t0 + 4 * 300_000 - 120_000}:${t0 + 4 * 300_000}`,
        `long:${t0 + 5 * 300_000 - 120_000}:${t0 + 5 * 300_000}`,
      ].sort()
    )
  })

  it('VALID lines are never capped', () => {
    const tls = Array.from({ length: 8 }, (_, i) =>
      mk('long', t0 + i * 300_000, true)
    )
    expect(mentorTrendSegments(tls, grid)).toHaveLength(8)
  })
})
