import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  MentorTruth,
  PepSignalState,
  PepTruth,
} from '../../lib/api/traders'
import {
  PepTruthCard,
  SIGNAL_KEYS,
  conflictKey,
  fmtCountdown,
} from './PepTruthCard'
import { pt } from '../../i18n/pep-translations'
import { ModeChip, dashboardMode } from './ModeChip'
import { pepChartLines, PEP_COLOR } from './pepOverlays'
import { usePepTruth } from './usePepTruth'
import {
  GOLDEN_AFTER_FLAT,
  GOLDEN_CONFLICT,
  GOLDEN_CONFLICT_DAY_PLAN,
  GOLDEN_NO_DATA,
  GOLDEN_OFF,
  GOLDEN_POSITION_OPEN,
  GOLDEN_WAITING_SIGNAL,
} from './pepTruthGolden'

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

// The trader lane's REAL JSON, parsed (pepTruthGolden.ts) - never a UI-shaped
// object. `symbol` is the one field the server does not send yet; the chart-line
// tests that need it add it explicitly.
const g = (json: string) => JSON.parse(json) as PepTruth
const base: PepTruth = g(GOLDEN_WAITING_SIGNAL)

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
  // Mutant: read signal.state / signal.side (the pre-review UI shape) -> the cell prints a dash -> RED.
  it('the server signal WORD reads as text, with the side for an open position', () => {
    const a = render(
      <PepTruthCard truth={g(GOLDEN_WAITING_SIGNAL)} language="en" />
    )
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'waiting for a break'
    )
    a.unmount()
    render(<PepTruthCard truth={g(GOLDEN_POSITION_OPEN)} language="en" />)
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'position open · LONG'
    )
    expect(screen.getByTestId('pep-truth-stoptarget')).toHaveTextContent(
      '25020.25 / 25180.75'
    )
    expect(screen.getByTestId('pep-truth-trades')).toHaveTextContent(
      'Position open'
    )
  })
  it('every signal word the server can send has text in all three languages', () => {
    for (const w of Object.keys(SIGNAL_KEYS) as PepSignalState[])
      for (const l of ['en', 'zh', 'id'] as const)
        expect(pt(SIGNAL_KEYS[w], l).trim(), `${w}.${l}`).not.toBe('')
    // an unknown (newer) word prints as itself, not as a dash or a guess
    render(
      <PepTruthCard
        truth={{ ...base, signal: 'brand_new' as PepSignalState }}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'brand_new'
    )
  })
  // Mutant: read flat_in_sec (the pre-review name) -> no countdown -> RED.
  it('the countdown comes from flat_countdown_sec; 0 reads "flat time passed"', () => {
    const a = render(
      <PepTruthCard truth={g(GOLDEN_WAITING_SIGNAL)} language="en" />
    )
    expect(screen.getByTestId('pep-truth-flat')).toHaveTextContent(
      '15:00 CT · in 3h 05m'
    )
    a.unmount()
    render(<PepTruthCard truth={g(GOLDEN_AFTER_FLAT)} language="en" />)
    expect(screen.getByTestId('pep-truth-flat')).toHaveTextContent(
      'flat time passed'
    )
    expect(screen.getByTestId('pep-truth-refusal')).toHaveTextContent(
      'pep_after_flat_time'
    )
  })
  it('no countdown is printed when the server sent none', () => {
    render(
      <PepTruthCard
        truth={{ ...base, flat_countdown_sec: undefined }}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-truth-flat')).not.toHaveTextContent(' in ')
  })
  // Mutant: read as_of_ms (the pre-review name) -> "as of" never prints -> RED.
  it('the as-of time is last_evaluated_ms; before the first evaluation it says so', () => {
    const a = render(
      <PepTruthCard truth={g(GOLDEN_WAITING_SIGNAL)} language="en" />
    )
    expect(a.container).toHaveTextContent('as of 09:07:05 CT')
    a.unmount()
    const b = render(<PepTruthCard truth={g(GOLDEN_NO_DATA)} language="en" />)
    expect(b.container).toHaveTextContent('not evaluated yet')
    expect(screen.getByTestId('pep-truth-box-undrawn')).toBeInTheDocument()
  })
  it('a block refusal outranks a wait one; the stale note shows', () => {
    render(
      <PepTruthCard
        stale
        truth={{
          ...base,
          refusals: [
            { code: 'pep_no_break_yet', severity: 'wait' },
            { code: 'pep_bars_stale', severity: 'block', detail: 'old' },
          ],
        }}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-truth-refusal')).toHaveTextContent(
      'pep_bars_stale'
    )
    expect(screen.getByTestId('pep-truth-stale')).toBeInTheDocument()
  })
  it('a mode conflict is a loud alert even though pep is not enabled', () => {
    render(<PepTruthCard truth={g(GOLDEN_CONFLICT)} language="en" />)
    expect(screen.getByRole('alert')).toHaveTextContent('NOT started')
    expect(screen.getByRole('alert')).toHaveTextContent('both ON')
  })
  // Mutant: always print the mentor headline -> the day-plan card says
  // "Mentor ... both ON" and RED here.
  it('the conflict headline names the modes that are really both ON', () => {
    const a = render(
      <PepTruthCard truth={g(GOLDEN_CONFLICT_DAY_PLAN)} language="en" />
    )
    const t = screen.getByRole('alert').textContent ?? ''
    expect(t).toContain('Pep and the Day Plan are both ON')
    expect(t).not.toContain('Mentor')
    a.unmount()
    const b = render(
      <PepTruthCard
        truth={{
          ...g(GOLDEN_CONFLICT),
          conflict: { code: 'pep_mode_conflict_future', message: '' },
        }}
        language="en"
      />
    )
    const u = screen.getByRole('alert').textContent ?? ''
    expect(u).toContain('two trading modes are both ON')
    expect(u).not.toContain('Mentor')
    b.unmount()
    for (const k of [
      'truthConflictMentor',
      'truthConflictDayPlan',
      'truthConflictOther',
    ] as const)
      for (const l of ['en', 'zh', 'id'] as const)
        expect(pt(k, l).trim(), `${k}.${l}`).not.toBe('')
    expect(conflictKey('pep_mode_conflict_mentor')).toBe('truthConflictMentor')
  })
  // Mutant: drop the Risk Control sentence from modePepHelp -> RED. The UI no
  // longer reads a "pep_enabled on" marker (no producer emits one), so the
  // selector help is where the owner learns which AI knobs pep ignores.
  it('the Pep mode help says which Risk Control settings pep ignores', () => {
    const en = pt('modePepHelp', 'en')
    expect(en).toContain('no break-even and no trailing')
    expect(en).toContain('Risk Control')
    expect(pt('modePepHelp', 'zh')).toContain('移动止损')
    expect(pt('modePepHelp', 'id')).toContain('break-even')
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
  const off = g(GOLDEN_OFF)
  it('names a mode only when it is known', () => {
    expect(dashboardMode(null, null, 'loading')).toBeNull()
    expect(dashboardMode(m(false), null, 'error')).toBeNull()
    expect(dashboardMode(m(false), null, 'loading')).toBeNull()
    expect(dashboardMode(m(false), off, 'ok')).toEqual({ mode: 'ai' })
    expect(dashboardMode(m(true), off, 'ok')).toEqual({ mode: 'mentor' })
    expect(dashboardMode(null, base, 'ok')).toEqual({ mode: 'pep', style: 'E' })
  })
  // Mutant: swap the conflict line below the pep line (or drop it) -> RED.
  it('mentor AND pep ON, or the server mode "conflict", is a conflict - never the first mode checked', () => {
    expect(dashboardMode(m(true), base, 'ok')).toEqual({ mode: 'conflict' })
    expect(dashboardMode(m(false), g(GOLDEN_CONFLICT), 'ok')).toEqual({
      mode: 'conflict',
    })
    expect(dashboardMode(null, g(GOLDEN_CONFLICT), 'ok')).toEqual({
      mode: 'conflict',
    })
  })
  it('renders the chip text in three languages', () => {
    const v = { mode: 'pep', style: 'E' } as const
    const a = render(<ModeChip value={v} language="en" />)
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('Pep E')
    a.unmount()
    const b = render(<ModeChip value={{ mode: 'mentor' }} language="zh" />)
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('导师')
    b.unmount()
    const c = render(<ModeChip value={{ mode: 'conflict' }} language="id" />)
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('Konflik mode')
    c.unmount()
    render(<ModeChip value={null} language="id" />)
    expect(screen.queryByTestId('mode-chip')).toBeNull()
  })
})

describe('pepChartLines', () => {
  // The server sends the box as {high,low,size,drawn}; never a line at 0.
  it('draws the box high/low only once the box is drawn', () => {
    const lines = pepChartLines(base)
    expect(lines.map((l) => l.price)).toEqual([25100.5, 25020.25])
    expect(lines.every((l) => l.color === PEP_COLOR)).toBe(true)
    expect(
      pepChartLines({ ...base, box: { ...base.box!, drawn: false } })
    ).toEqual([])
    expect(pepChartLines(g(GOLDEN_NO_DATA))).toEqual([])
    expect(pepChartLines({ ...base, enabled: false })).toEqual([])
    expect(pepChartLines(g(GOLDEN_OFF))).toEqual([])
    expect(pepChartLines(null)).toEqual([])
  })
  // Mutant: draw stop/target regardless of position_open -> RED.
  it('adds the stop and target only while a position is open', () => {
    // a CLOSED day still carries stop/target (the newest entry's bracket)
    expect(
      pepChartLines({ ...base, stop: 70, target: 120, side: 'long' })
    ).toHaveLength(2)
    const open = pepChartLines(g(GOLDEN_POSITION_OPEN))
    expect(open.map((l) => l.label)).toEqual([
      expect.stringContaining('pep box high'),
      expect.stringContaining('pep box low'),
      expect.stringContaining('pep stop 25020.25'),
      expect.stringContaining('pep target 25180.75'),
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
