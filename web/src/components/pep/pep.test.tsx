import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MentorTruth, PepTruth } from '../../lib/api/traders'
import { PepTruthCard, fmtCountdown } from './PepTruthCard'
import { ModeChip, dashboardMode } from './ModeChip'
import { pepChartLines, PEP_COLOR } from './pepOverlays'
import { usePepTruth } from './usePepTruth'

const getPepTruth = vi.fn()
vi.mock('../../lib/api/traders', async (orig) => {
  const actual = await orig<typeof import('../../lib/api/traders')>()
  return {
    ...actual,
    traderApi: {
      ...actual.traderApi,
      getPepTruth: (...a: unknown[]) => getPepTruth(...a),
    },
  }
})

const base: PepTruth = {
  enabled: true,
  as_of_ms: Date.UTC(2026, 9, 30, 14, 0, 0),
  symbol: 'MNQ',
  style: 'A',
  box: { high: 100, low: 80, size: 20, start_ms: 0, end_ms: 0, drawn: true },
  signal: { state: 'waiting' },
  trades_today: 0,
  max_trades_per_day: 1,
  flat_time_ct: '15:00',
  flat_in_sec: 600,
}

afterEach(cleanup)

describe('PepTruthCard', () => {
  it('renders nothing for null / off', () => {
    const a = render(<PepTruthCard truth={null} language="en" />)
    expect(a.container).toBeEmptyDOMElement()
    const b = render(
      <PepTruthCard truth={{ ...base, enabled: false }} language="en" />
    )
    expect(b.container).toBeEmptyDOMElement()
  })
  // Mutant: show the box whenever `box` exists (ignore drawn) -> RED.
  it('an undrawn box says so and prints no numbers', () => {
    render(
      <PepTruthCard
        truth={{
          ...base,
          box: { ...base.box!, drawn: false, high: 0, low: 0 },
        }}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-truth-box-undrawn')).toBeInTheDocument()
    expect(screen.getByTestId('pep-truth-box')).not.toHaveTextContent('high')
  })
  it('an ABSENT box is also "not drawn", not a box at 0', () => {
    render(<PepTruthCard truth={{ ...base, box: undefined }} language="en" />)
    expect(screen.getByTestId('pep-truth-box-undrawn')).toBeInTheDocument()
  })
  it('shows entered side, stop/target, flat passed, a block refusal and the stale note', () => {
    render(
      <PepTruthCard
        stale
        truth={{
          ...base,
          signal: { state: 'entered', side: 'long' },
          position_open: true,
          stop: 90,
          target: 120,
          flat_in_sec: 0,
          refusals: [
            { code: 'pep_no_break_yet', severity: 'wait' },
            { code: 'pep_bars_stale', severity: 'block', detail: 'old' },
          ],
        }}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'entered · LONG'
    )
    // the BLOCK refusal outranks the wait one
    expect(screen.getByTestId('pep-truth-refusal')).toHaveTextContent(
      'pep_bars_stale'
    )
    expect(screen.getByTestId('pep-truth-stoptarget')).toHaveTextContent(
      '90 / 120'
    )
    expect(screen.getByTestId('pep-truth-flat')).toHaveTextContent(
      'flat time passed'
    )
    expect(screen.getByTestId('pep-truth-trades')).toHaveTextContent(
      'Position open'
    )
    expect(screen.getByTestId('pep-truth-stale')).toBeInTheDocument()
  })
  it('no countdown is printed when the server sent none', () => {
    render(
      <PepTruthCard truth={{ ...base, flat_in_sec: undefined }} language="en" />
    )
    expect(screen.getByTestId('pep-truth-flat')).not.toHaveTextContent(' in ')
  })
  it('renders in id and zh', () => {
    const a = render(<PepTruthCard truth={base} language="id" />)
    expect(a.container).toHaveTextContent('apa yang diperdagangkan')
    a.unmount()
    const b = render(<PepTruthCard truth={base} language="zh" />)
    expect(b.container).toHaveTextContent('交易内容')
  })
  it('fmtCountdown', () => {
    expect(fmtCountdown(3 * 3600 + 5 * 60)).toBe('3h 05m')
    expect(fmtCountdown(59 * 60 + 59)).toBe('59m')
    expect(fmtCountdown(-5)).toBe('0m')
  })
})

describe('ModeChip / dashboardMode', () => {
  const m = (enabled: boolean) => ({ enabled }) as MentorTruth
  it('names a mode only when it is known', () => {
    expect(dashboardMode(null, null, 'loading')).toBeNull()
    expect(dashboardMode(m(false), null, 'error')).toBeNull()
    expect(dashboardMode(m(false), null, 'loading')).toBeNull()
    expect(dashboardMode(m(false), { ...base, enabled: false }, 'ok')).toEqual({
      mode: 'ai',
    })
    expect(dashboardMode(m(true), { ...base, enabled: false }, 'ok')).toEqual({
      mode: 'mentor',
    })
    expect(dashboardMode(null, base, 'ok')).toEqual({ mode: 'pep', style: 'A' })
  })
  it('renders the chip text in three languages', () => {
    const v = { mode: 'pep', style: 'E' } as const
    const a = render(<ModeChip value={v} language="en" />)
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('Pep E')
    a.unmount()
    const b = render(<ModeChip value={{ mode: 'mentor' }} language="zh" />)
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('导师')
    b.unmount()
    render(<ModeChip value={null} language="id" />)
    expect(screen.queryByTestId('mode-chip')).toBeNull()
  })
})

describe('pepChartLines', () => {
  it('draws the box high/low only once the box is drawn', () => {
    const lines = pepChartLines(base)
    expect(lines.map((l) => l.price)).toEqual([100, 80])
    expect(lines.every((l) => l.color === PEP_COLOR)).toBe(true)
    expect(
      pepChartLines({ ...base, box: { ...base.box!, drawn: false } })
    ).toEqual([])
    expect(pepChartLines({ ...base, enabled: false })).toEqual([])
    expect(pepChartLines(null)).toEqual([])
  })
  // Mutant: draw stop/target regardless of position_open -> RED.
  it('adds the stop and target only while a position is open', () => {
    expect(pepChartLines({ ...base, stop: 70, target: 120 })).toHaveLength(2)
    const open = pepChartLines({
      ...base,
      position_open: true,
      stop: 70,
      target: 120,
    })
    expect(open.map((l) => l.label)).toEqual([
      expect.stringContaining('pep box high'),
      expect.stringContaining('pep box low'),
      expect.stringContaining('pep stop 70'),
      expect.stringContaining('pep target 120'),
    ])
  })
  it('marks every line when the read is stale', () => {
    const lines = pepChartLines(base, Date.UTC(2026, 9, 30, 15, 7))
    expect(lines[0].label).toContain('stale since 10:07 CT')
  })
})

function Harness({ id }: { id: string }) {
  const { truth, stale, status } = usePepTruth(id)
  return (
    <div>
      <span data-testid="t">{String(truth?.enabled ?? 'null')}</span>
      <span data-testid="s">{String(stale)}</span>
      <span data-testid="st">{status}</span>
    </div>
  )
}

describe('usePepTruth', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getPepTruth.mockReset()
  })
  afterEach(() => vi.useRealTimers())

  it('polls every 30s and reports ok', async () => {
    getPepTruth.mockResolvedValue(base)
    render(<Harness id="a" />)
    expect(screen.getByTestId('st')).toHaveTextContent('loading')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByTestId('st')).toHaveTextContent('ok')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(getPepTruth).toHaveBeenCalledTimes(2)
  })
  it('a first failure is "error" (unknown), never a payload', async () => {
    getPepTruth.mockRejectedValue(new Error('404'))
    render(<Harness id="a" />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByTestId('t')).toHaveTextContent('null')
    expect(screen.getByTestId('st')).toHaveTextContent('error')
  })
  it('a later failure keeps the last good payload and marks it stale', async () => {
    getPepTruth.mockResolvedValueOnce(base)
    render(<Harness id="a" />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    getPepTruth.mockRejectedValue(new Error('net'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(screen.getByTestId('t')).toHaveTextContent('true')
    expect(screen.getByTestId('s')).toHaveTextContent('true')
    expect(screen.getByTestId('st')).toHaveTextContent('ok')
  })
})
