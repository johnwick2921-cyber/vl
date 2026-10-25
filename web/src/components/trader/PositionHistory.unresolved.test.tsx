import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import {
  PositionHistory,
  pnlSortKey,
  comparePnl,
  pointValueFor,
  formatFee,
} from './PositionHistory'
import type { HistoricalPosition } from '../../types'

// Trading-P2 RED→GREEN (audit/0926-trading-pipeline, the 618 class): a NULL
// pnl_corrected must READ "unresolved" in the row and be COUNTED in the
// footer — never 0, never a fabricated realized_pnl number.

vi.mock('../../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('../../lib/autoRefresh', () => ({
  useAutoRefresh: () => {},
  REFRESH_HISTORY_MS: 60000,
}))
vi.mock('../../lib/api', () => ({
  api: { getPositionHistory: vi.fn() },
}))
vi.mock('../common/MetricTooltip', () => ({
  MetricTooltip: () => null,
}))

import { api } from '../../lib/api'

const mockGet = api.getPositionHistory as ReturnType<typeof vi.fn>

function row(over: Partial<HistoricalPosition>): HistoricalPosition {
  const base: HistoricalPosition = {
    id: 618,
    trader_id: 't1',
    exchange_id: 'e',
    exchange_type: 'ninjatrader',
    symbol: 'MNQ',
    side: 'LONG',
    quantity: 1,
    entry_quantity: 1,
    entry_price: 30736.5,
    entry_order_id: '',
    entry_time: '2026-09-24T14:00:00Z',
    exit_price: 0,
    exit_order_id: '',
    exit_time: new Date().toISOString(),
    realized_pnl: 777, // distinctive: must NOT appear once pnl_corrected is NULL
    fee: 0,
    leverage: 1,
    status: 'CLOSED',
    close_reason: 'sync',
    created_at: '',
    updated_at: '',
  }
  return { ...base, ...over }
}

function mockResponse(positions: HistoricalPosition[]) {
  mockGet.mockResolvedValue({
    positions,
    stats: null,
    symbol_stats: [],
    direction_stats: [],
  })
}

beforeEach(() => mockGet.mockReset())
afterEach(() => cleanup())

describe('PositionHistory — NULL pnl_corrected reads unresolved', () => {
  it('renders "unresolved" (never the realized number) and counts it in the footer', async () => {
    const p = row({ pnl_corrected: undefined })
    delete (p as Partial<HistoricalPosition>).pnl_corrected // wire-shape: field ABSENT
    mockResponse([p])

    render(<PositionHistory traderId="t1" />)

    // The row's P&L cell says unresolved…
    expect(await screen.findByText('unresolved')).toBeTruthy()
    // …and the fabricated realized_pnl number never appears in the cell.
    expect(screen.queryByText('+777.00')).toBeNull()
    expect(screen.queryByText('777.00')).toBeNull()
    // The footer shows the count (canon class 40: count shown).
    expect(screen.getByText('1 unresolved')).toBeTruthy()
  })

  it('renders the corrected number for a RESOLVED row and no unresolved count', async () => {
    mockResponse([row({ id: 619, pnl_corrected: -32, realized_pnl: -32 })])

    render(<PositionHistory traderId="t1" />)

    expect((await screen.findAllByText('-32.00')).length).toBeGreaterThan(0)
    expect(screen.queryByText('unresolved')).toBeNull()
    expect(screen.queryByText(/unresolved$/)).toBeNull()
  })
})

// ── C13 (release #50): VALUE x point value, pnl_corrected sort, footer label,
// "-0.00" fee. All through the production render of PositionHistory.
describe('PositionHistory — C13 table honesty', () => {
  it('VALUE column multiplies by the $/pt (MNQ $2): 5 x 20000 x 2 = 200.00K', async () => {
    mockResponse([
      row({
        id: 1,
        quantity: 5,
        entry_quantity: 5,
        entry_price: 20000,
        pnl_corrected: 10,
      }),
    ])
    render(<PositionHistory traderId="t1" />)
    expect(await screen.findByText('200.00K')).toBeTruthy()
    expect(screen.queryByText('100.00K')).toBeNull()
  })

  it('zero fee reads "0.00" (never "-0.00") in the row and the stats card', async () => {
    mockResponse([row({ id: 1, pnl_corrected: 10, fee: 0 })])
    const { container } = render(<PositionHistory traderId="t1" />)
    await screen.findByText(/Session day/)
    expect(container.textContent).not.toContain('-0.00')
  })

  it('footer total is labelled "(last N loaded)"', async () => {
    mockResponse([
      row({ id: 1, pnl_corrected: 10 }),
      row({ id: 2, pnl_corrected: 20, entry_time: '2026-09-24T15:00:00Z' }),
    ])
    render(<PositionHistory traderId="t1" />)
    expect(await screen.findByText(/last 2 loaded/)).toBeTruthy()
  })

  it('P&L sort uses pnl_corrected and puts NULL (unresolved) last', async () => {
    const a = row({
      id: 1,
      realized_pnl: 999,
      pnl_corrected: 5,
      exit_price: 30001,
      exit_time: new Date().toISOString(),
    })
    const b = row({
      id: 2,
      realized_pnl: 1,
      pnl_corrected: 50,
      exit_price: 30002,
      exit_time: new Date().toISOString(),
    })
    const c = row({
      id: 3,
      realized_pnl: 5000,
      exit_price: 30003,
      exit_time: new Date().toISOString(),
    })
    delete (c as Partial<HistoricalPosition>).pnl_corrected
    expect(pnlSortKey(a)).toBe(5)
    expect(pnlSortKey(c)).toBeNull()
    const sorted = (order: 'asc' | 'desc') =>
      [a, b, c]
        .sort((x, y) => {
          const r = comparePnl(x, y)
          if (r !== null) return r
          const cmp = (pnlSortKey(x) as number) - (pnlSortKey(y) as number)
          return order === 'desc' ? -cmp : cmp
        })
        .map((p) => p.id)
    expect(sorted('asc')).toEqual([1, 2, 3])
    expect(sorted('desc')).toEqual([2, 1, 3])
  })
})

describe('pointValueFor / formatFee', () => {
  it('table + row override + unknown', () => {
    expect(pointValueFor({ symbol: 'MNQ' })).toBe(2)
    expect(pointValueFor({ symbol: 'MNQ 06-26' })).toBe(2)
    expect(pointValueFor({ symbol: 'NQ' })).toBe(20)
    expect(pointValueFor({ symbol: 'MNQ', point_value: 5 })).toBe(5)
    expect(pointValueFor({ symbol: 'XYZ' })).toBe(1)
  })
  it('formatFee', () => {
    expect(formatFee(0)).toBe('0.00')
    expect(formatFee(1.24)).toBe('-1.24')
    expect(formatFee(0.005)).toBe('-0.0050')
  })
})
