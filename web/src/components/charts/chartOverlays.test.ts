// T7 (release #22): the mentor places the chart draws, from /api/mentor/truth.
// Every line comes from a payload field; nothing is computed that the API did
// not send. Mutant: drop the ISB / ORB / swing blocks → RED.
import { describe, expect, it } from 'vitest'
import type { MentorTruth } from '../../lib/api/traders'
import {
  mentorChartLines,
  mentorTrendSegments,
  orderPriceLines,
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
    expect(byLabel['mentor EMA34 location']).toBe(30020)
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
        },
      ],
      '5m'
    )
    expect(seg.label).toBe('mentor trendline (resistance)')
    expect(seg.points.map((p) => p.time)).toEqual([
      t0 / 1000,
      (t0 + 600_000) / 1000,
      (t0 + 900_000) / 1000,
    ])
    // slope = -6 / 600000 per ms: 110.6 at t0, 104.6 at +10m, 101.6 at +15m
    expect(seg.points.map((p) => Math.round(p.value * 100) / 100)).toEqual([
      110.6, 104.6, 101.6,
    ])
  })

  it('ends a box-suppressed line at its second anchor and says it is not traded', () => {
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
        },
      ],
      '1m'
    )
    expect(seg.points).toHaveLength(2)
    expect(seg.label).toContain('not traded')
  })
})

describe('orderPriceLines', () => {
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
