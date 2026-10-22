import { describe, it, expect } from 'vitest'
import {
  positionSortTimeMs,
  SESSION_DAY_TOOLTIP,
  computeDayTotal,
} from './PositionHistory'
import type { HistoricalPosition } from '../../types'

function row(over: Partial<HistoricalPosition>): HistoricalPosition {
  return {
    id: 1,
    trader_id: 't',
    exchange_id: 'e',
    exchange_type: 'ninjatrader',
    symbol: 'MNQ',
    side: 'LONG',
    quantity: 1,
    entry_quantity: 2,
    entry_price: 100,
    entry_order_id: 'x',
    entry_time: '',
    exit_price: 0,
    exit_order_id: '',
    exit_time: '',
    realized_pnl: 10,
    pnl_corrected: 10,
    fee: 0,
    leverage: 1,
    status: 'OPEN',
    close_reason: '',
    created_at: '',
    updated_at: '',
    ...over,
  } as unknown as HistoricalPosition
}

describe('M251-S2 history time sort + session-day tooltip', () => {
  it('open partial sorts by updated_at, not as epoch 0', () => {
    const open = row({ exit_time: '', updated_at: '2026-10-21T20:00:00Z' })
    const old = row({
      id: 2,
      status: 'CLOSED',
      exit_time: '2026-10-21T10:00:00Z',
      updated_at: '2026-10-21T10:00:00Z',
    })
    expect(positionSortTimeMs(open)).toBeGreaterThan(positionSortTimeMs(old))
    const sorted = [old, open].sort(
      (a, b) => positionSortTimeMs(b) - positionSortTimeMs(a)
    )
    expect(sorted[0].id).toBe(1)
  })
  it('closed row keeps exit_time over updated_at', () => {
    const c = row({
      status: 'CLOSED',
      exit_time: '2026-10-21T10:00:00Z',
      updated_at: '2026-10-21T23:00:00Z',
    })
    expect(positionSortTimeMs(c)).toBe(
      new Date('2026-10-21T10:00:00Z').getTime()
    )
  })
  it('tooltip states open booked partials are counted and the gate is closed-only', () => {
    const open = row({ updated_at: '2026-10-21T20:00:00Z', pnl_corrected: 10 })
    // what is counted: the open partial IS in the day total
    expect(
      computeDayTotal([open], new Date('2026-10-21T15:00:00Z').getTime())
    ).toBe(10)
    expect(SESSION_DAY_TOOLTIP).toMatch(
      /partial legs already booked on trades that are still OPEN/
    )
    expect(SESSION_DAY_TOOLTIP).toMatch(
      /daily-loss gate counts CLOSED trades only/
    )
    expect(SESSION_DAY_TOOLTIP).not.toMatch(/Bot trades closed in/)
  })
})
