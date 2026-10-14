// P&L-TRUTH WAVE — F5 (FE half): the header chip shows the ledger day total
// with its counts, and never fabricates a 0 when the backend has no figure.
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { LedgerDayPnl, ledgerDayLabel } from './LedgerDayPnl'
import { computeDayTotal } from './PositionHistory'
import type { AccountInfo } from '../../types/trading'
import type { HistoricalPosition } from '../../types/trading'

const base: AccountInfo = {
  total_equity: 52428,
  wallet_balance: 52428,
  unrealized_profit: 0,
  available_balance: 52428,
  total_pnl: 0,
  total_pnl_pct: 0,
  initial_balance: 52428,
  daily_pnl: 0,
  position_count: 0,
  margin_used: 0,
  margin_used_pct: 0,
}

describe('LedgerDayPnl — the header reads the ledger, not NT8 daily_pnl', () => {
  it('renders the ledger day total with resolved / unresolved counts', () => {
    render(
      <LedgerDayPnl
        account={{
          ...base,
          ledger_day_pnl: 212,
          ledger_day_resolved: 6,
          ledger_day_unresolved: 0,
        }}
      />
    )
    expect(screen.getByTestId('ledger-day-pnl').textContent).toBe(
      'LEDGER_DAY::+212.00 (6 resolved, 0 unresolved excluded)'
    )
  })

  it('never fabricates a zero when the backend has no figure', () => {
    render(
      <LedgerDayPnl
        account={{ ...base, ledger_day_status: 'UNRESOLVED: db' }}
      />
    )
    const text = screen.getByTestId('ledger-day-pnl').textContent!
    expect(text).toContain('UNRESOLVED')
    expect(text).not.toMatch(/[+-]?0\.00/)
  })

  it('equals the position-history footer for the same rows (the footer rule)', () => {
    const today = new Date().toISOString()
    const row = (over: Partial<HistoricalPosition>): HistoricalPosition =>
      ({
        id: 1,
        symbol: 'MNQ',
        side: 'LONG',
        entry_price: 100,
        exit_price: 110,
        quantity: 1,
        realized_pnl: 10,
        pnl_corrected: 10,
        exit_time: today,
        close_reason: 'sync',
        ...over,
      }) as unknown as HistoricalPosition
    const rows = [
      row({ id: 1, pnl_corrected: 50 }),
      row({ id: 2, pnl_corrected: -20 }),
      row({ id: 3, pnl_corrected: 10 }),
      row({
        id: 4,
        pnl_corrected: null as unknown as number,
        realized_pnl: -100,
      }), // unresolved
      row({ id: 5, close_reason: 'e7_farside_test', pnl_corrected: 6 }), // hidden
    ]
    const footer = computeDayTotal(rows, Date.now() - 3600_000)
    expect(footer).toBe(40)
    // The backend's GetLedgerDayTotal applies the same rule (Go test
    // TestPnlTruthLedgerDayTotalMatchesTheFooterRule); the chip renders it.
    expect(
      ledgerDayLabel({
        ledger_day_pnl: footer,
        ledger_day_resolved: 3,
        ledger_day_unresolved: 1,
      })
    ).toBe('LEDGER_DAY::+40.00 (3 resolved, 1 unresolved excluded)')
  })
})
