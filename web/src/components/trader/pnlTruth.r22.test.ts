import { describe, it, expect } from 'vitest'
import {
  classifyPosition,
  computeDayTotal,
  historyScopeLabel,
  manualFooterLine,
} from './PositionHistory'
import { ledgerDayLabel } from './LedgerDayPnl'
import {
  accountVsInitialCard,
  botRecordCard,
  botRecordScopeLine,
  headerAccountPnl,
  manualLine,
} from './accountPnl'
import type { AccountInfo, HistoricalPosition } from '../../types'

// R22 PNL-TRUTH — owner ruling R1 + RISK-PNL P1-2 + the ONE "today".

function row(over: Partial<HistoricalPosition>): HistoricalPosition {
  return {
    id: 1,
    trader_id: 't',
    exchange_id: 'e',
    exchange_type: 'ninjatrader',
    symbol: 'MNQ',
    side: 'SHORT',
    quantity: 1,
    entry_quantity: 1,
    entry_price: 30000,
    entry_order_id: '',
    entry_time: '',
    exit_price: 30000,
    exit_order_id: '',
    exit_time: new Date().toISOString(),
    realized_pnl: 0,
    pnl_corrected: 0,
    fee: 0,
    leverage: 1,
    status: 'CLOSED',
    close_reason: 'stop',
    created_at: '',
    updated_at: '',
    ...over,
  }
}

const acct = (over: Partial<AccountInfo>): AccountInfo =>
  ({
    total_equity: 47695,
    wallet_balance: 47695,
    unrealized_profit: 0,
    available_balance: 47695,
    total_pnl: 74,
    total_pnl_pct: 0.16,
    initial_balance: 50000,
    daily_pnl: 0,
    position_count: 0,
    margin_used: 0,
    margin_used_pct: 0,
    ...over,
  }) as AccountInfo

describe('R1 — manual NT8 rows are listed, never in a bot total', () => {
  const sessionStart = Date.now() - 3600_000
  const rows = [
    row({ id: 648, pnl_corrected: -31.5 }),
    row({ id: 651, pnl_corrected: 105.5 }),
    row({
      id: 640,
      pnl_corrected: -1127.5,
      origin: 'manual',
      close_reason: 'manual',
      entry_quantity: 20,
    }),
  ]
  it('classifies origin=manual as its own kind', () => {
    expect(classifyPosition(rows[2], rows)).toBe('manual')
    expect(classifyPosition(rows[0], rows)).toBe('normal')
  })
  it('the session-day total excludes the manual row', () => {
    expect(computeDayTotal(rows, sessionStart)).toBe(74)
  })
  it('the manual rows get their own visible line', () => {
    expect(manualFooterLine([rows[2]])).toBe(
      'Manual (excluded): 1 trade, −$1,127.50'
    )
    expect(manualLine(4, -3202)).toBe('Manual (excluded): 4 trades, −$3,202.00')
  })
})

describe('ONE "today" — the CME session day from 17:00 CT', () => {
  it('a row closed before the session start is out, even on the same calendar day', () => {
    const start = Date.now() - 60_000
    const rows = [
      row({
        id: 1,
        pnl_corrected: 10,
        exit_time: new Date(start + 1000).toISOString(),
      }),
      row({
        id: 2,
        pnl_corrected: 99,
        exit_time: new Date(start - 1000).toISOString(),
      }),
    ]
    expect(computeDayTotal(rows, start)).toBe(10)
  })
  it('without the server session start the day total is unknown (null), never a guess', () => {
    expect(computeDayTotal([row({ pnl_corrected: 5 })])).toBeNull()
  })
  it('LEDGER_DAY names manual rows in the day instead of dropping them', () => {
    expect(
      ledgerDayLabel({
        ledger_day_pnl: 74,
        ledger_day_resolved: 2,
        ledger_day_unresolved: 0,
        ledger_day_manual_excluded: 1,
        ledger_day_manual_excluded_pnl: -1127.5,
      })
    ).toBe(
      'LEDGER_DAY::+74.00 (2 resolved, 0 unresolved excluded) · manual 1 excluded (−1127.50)'
    )
  })
})

describe('P1-2 — label, value and tooltip agree', () => {
  it('NT8 session figure is labelled as the NT8 session, not equity − initial', () => {
    const h = headerAccountPnl(
      acct({ nt8_session_pnl: 74, total_pnl_source: 'nt8_session' })
    )
    expect(h.label).toBe('NT8_SESSION::+74.00')
    expect(h.title).toContain('NT8 resets it every session')
    expect(h.title).not.toMatch(/^.*\(equity − initial balance\)$/)
  })
  it('account vs initial is equity − initial (−2,305.00), on the equity card', () => {
    const c = accountVsInitialCard(
      acct({ account_pnl_vs_initial: -2305, account_pnl_vs_initial_pct: -4.61 })
    )
    expect(c.value).toBe('−2,305.00')
    expect(c.subtitle).toBe('Account vs initial: −2,305.00 (initial 50,000)')
    expect(c.positive).toBe(false)
    expect(c.changePct).toBe(-4.61)
  })
  it('the bot card shows the bot record and the manual line, not the NT8 session', () => {
    const c = botRecordCard(
      acct({
        bot_record_pnl: -1024.18,
        bot_record_trades: 164,
        bot_record_unresolved: 115,
        manual_excluded_trades: 4,
        manual_excluded_pnl: -3202,
      })
    )
    expect(c.value).toBe('−1,024.18')
    expect(c.subtitle).toBe(
      '164 bot trades (115 unresolved excluded) · Manual (excluded): 4 trades, −$3,202.00'
    )
  })
  it('absent figures read n/a, never 0', () => {
    expect(accountVsInitialCard(acct({})).value).toBe('n/a')
    expect(botRecordCard(acct({})).value).toBe('n/a')
    expect(headerAccountPnl(acct({})).label).toBe('ACCT_PNL::n/a')
  })
})

// RT-7 (release #24, N1): two "bot record" figures on one dashboard — the
// account-scoped card and the trader-wide history total. No figure changes;
// each says its scope, and the scoped card names the bot rows it leaves out
// (the server's count: hoang's 16 rows, −547.00, ids 383–489).
describe('RT-7 bot record scope', () => {
  const acct = (over: Partial<AccountInfo>) => over as AccountInfo
  it('the scoped card counts the bot trades on other accounts, from the server', () => {
    const a = acct({
      bot_record_pnl: -1024.18,
      bot_record_trades: 164,
      bot_record_scope: 'this account',
      bot_record_other_accounts_trades: 16,
      bot_record_other_accounts_pnl: -547,
    })
    expect(botRecordScopeLine(a)).toBe(
      '16 bot trades (−547.00) on other accounts not counted here'
    )
    expect(botRecordCard(a).value).toBe('−1,024.18')
    expect(botRecordCard(a).subtitle).toContain(
      '16 bot trades (−547.00) on other accounts not counted here'
    )
  })
  it('no other-account rows, a trader-wide read, and an older server', () => {
    expect(
      botRecordScopeLine(
        acct({
          bot_record_scope: 'this account',
          bot_record_other_accounts_trades: 0,
        })
      )
    ).toBe('this account only (no bot trades on other accounts)')
    expect(botRecordScopeLine(acct({ bot_record_scope: 'all accounts' }))).toBe(
      'all accounts'
    )
    expect(botRecordScopeLine(acct({}))).toBe('')
  })
  it('the history total says its scope; absent reads scope n/a', () => {
    expect(historyScopeLabel('all accounts')).toBe('all accounts')
    expect(historyScopeLabel(undefined)).toBe('scope n/a')
  })
})
