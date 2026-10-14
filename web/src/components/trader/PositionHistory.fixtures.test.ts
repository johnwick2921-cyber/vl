import { describe, it, expect } from 'vitest'
import {
  classifyPosition,
  computeDayTotal,
  duplicateOfId,
  effectivePnl,
} from './PositionHistory'
import type { HistoricalPosition } from '../../types'

// LEDGER-SURFACE HONESTY (2026-08-31) — fixture per state.

function row(over: Partial<HistoricalPosition>): HistoricalPosition {
  const base: HistoricalPosition = {
    id: 1,
    trader_id: 't',
    exchange_id: 'e',
    exchange_type: 'ninjatrader',
    symbol: 'MNQ',
    side: 'LONG',
    quantity: 1,
    entry_quantity: 1,
    entry_price: 29413,
    entry_order_id: '',
    entry_time: '',
    exit_price: 29459,
    exit_order_id: '',
    exit_time: new Date().toISOString(), // today (CT clock-safe: fixture runs same day)
    realized_pnl: 92,
    fee: 0,
    leverage: 1,
    status: 'CLOSED',
    close_reason: 'sync',
    created_at: '',
    updated_at: '',
  }
  return { ...base, ...over }
}

describe('classifyPosition — one fixture per state', () => {
  it('unresolved → shown with "—" state', () => {
    const p = row({
      id: 579,
      close_reason: 'unresolved',
      realized_pnl: 0,
      exit_price: 0,
    })
    expect(classifyPosition(p, [p])).toBe('unresolved')
    expect(effectivePnl(p)).toBe(0)
  })

  it('duplicate → reconcile_flat sharing entry_order_id with a real close', () => {
    const real = row({
      id: 578,
      entry_order_id: 'f0bbe9af',
      realized_pnl: 92,
      pnl_corrected: 92,
    })
    const dupe = row({
      id: 577,
      entry_order_id: 'f0bbe9af',
      close_reason: 'reconcile_flat',
      realized_pnl: 0,
      exit_price: 29413,
      pnl_corrected: null,
    })
    const all = [real, dupe]
    expect(classifyPosition(dupe, all)).toBe('duplicate')
    expect(duplicateOfId(dupe, all)).toBe(578)
    expect(classifyPosition(real, all)).toBe('normal')
  })

  it('hidden → test-seam rows and evidence-less reconcile_flat', () => {
    const testSeam = row({
      id: 573,
      close_reason: 'e7_farside_test',
      realized_pnl: 6,
    })
    const orphan = row({
      id: 110,
      close_reason: 'reconcile_flat',
      realized_pnl: 0,
      entry_order_id: 'legacy',
    })
    expect(classifyPosition(testSeam, [testSeam])).toBe('hidden')
    expect(classifyPosition(orphan, [orphan])).toBe('hidden')
  })

  it('normal → real close renders its ledger-effective P&L', () => {
    const p = row({
      id: 575,
      close_reason: 'sync',
      realized_pnl: 30,
      pnl_corrected: 32.5,
    })
    expect(classifyPosition(p, [p])).toBe('normal')
    expect(effectivePnl(p)).toBe(32.5) // corrections win
  })
})

describe('computeDayTotal — same rule as the ledger', () => {
  it('today recomputed: +164.00 (32.5 + 92 + 39.5), duplicates/test/unknown excluded', () => {
    const now = new Date().toISOString()
    const yesterday = new Date(Date.now() - 86400000).toISOString()
    const all = [
      row({
        id: 575,
        side: 'SHORT',
        close_reason: 'sync',
        realized_pnl: 32.5,
        pnl_corrected: 32.5,
        exit_time: now,
        entry_order_id: '0aa6032e',
      }),
      row({
        id: 576,
        side: 'short',
        close_reason: 'reconcile_flat',
        realized_pnl: 0,
        pnl_corrected: null,
        exit_time: now,
        entry_order_id: '0aa6032e',
        exit_price: 29437,
        entry_price: 29437,
      }),
      row({
        id: 578,
        close_reason: 'sync',
        realized_pnl: 92,
        pnl_corrected: 92,
        exit_time: now,
        entry_order_id: 'f0bbe9af',
      }),
      row({
        id: 577,
        side: 'long',
        close_reason: 'reconcile_flat',
        realized_pnl: 0,
        pnl_corrected: null,
        exit_time: now,
        entry_order_id: 'f0bbe9af',
        exit_price: 29413,
      }),
      row({
        id: 579,
        side: 'short',
        close_reason: 'unresolved',
        realized_pnl: 0,
        pnl_corrected: null,
        exit_time: now,
        exit_price: 0,
        entry_price: 29459,
      }),
      row({
        id: 573,
        close_reason: 'e7_farside_test',
        realized_pnl: 6,
        pnl_corrected: 6,
        exit_time: now,
      }),
      row({
        id: 574,
        close_reason: 'e7_farside_test',
        realized_pnl: -1,
        pnl_corrected: -1,
        exit_time: now,
      }),
      row({
        id: 580,
        side: 'SHORT',
        close_reason: 'sync',
        realized_pnl: 39.5,
        pnl_corrected: 39.5,
        exit_time: now,
        entry_price: 29417.25,
        exit_price: 29397.5,
      }),
      // A-2: a real close today with NULL pnl_corrected is excluded by the ledger too.
      row({
        id: 581,
        close_reason: 'sync',
        realized_pnl: 50,
        pnl_corrected: null,
        exit_time: now,
      }),
      // Not today → out of the day total.
      row({
        id: 570,
        close_reason: 'sync',
        realized_pnl: 17,
        pnl_corrected: 17,
        exit_time: yesterday,
      }),
    ]
    // R22: the window is the server's CME session-day start (here: 1h ago).
    expect(computeDayTotal(all, Date.now() - 3600_000)).toBe(164.0)
  })
})
