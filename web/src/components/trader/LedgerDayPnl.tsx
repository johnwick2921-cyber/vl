// P&L-TRUTH WAVE (2026-09-01) — the dashboard-header ledger day total.
//
// The header used to show the NT8-native figure (permanently 0.00) beside a
// +212.00 day total in the position-history footer. This chip renders the
// SAME number the footer computes (server-side, strict pnl_corrected), with
// its resolved n and unresolved exclusion count — never a rate or a total
// without its n.
//
// R22 PNL-TRUTH: the day is the CME SESSION day from 17:00 CT (the risk
// gates' day — it used to be the CT calendar day), the figure is the BOT's
// (manual NT8 rows excluded, owner ruling R1), and any manual rows in the day
// are named in the chip instead of vanishing.
import type { AccountInfo } from '../../types/trading'

function signed(v: number): string {
  return `${v >= 0 ? '+' : '−'}${Math.abs(v).toFixed(2)}`
}

export function ledgerDayLabel(
  account: Pick<
    AccountInfo,
    | 'ledger_day_pnl'
    | 'ledger_day_resolved'
    | 'ledger_day_unresolved'
    | 'ledger_day_status'
    | 'ledger_day_manual_excluded'
    | 'ledger_day_manual_excluded_pnl'
  >
): string {
  if (typeof account.ledger_day_pnl !== 'number') {
    return `LEDGER_DAY::${account.ledger_day_status ?? 'UNRESOLVED'}`
  }
  const sign = account.ledger_day_pnl >= 0 ? '+' : ''
  const resolved = account.ledger_day_resolved ?? 0
  const unresolved = account.ledger_day_unresolved ?? 0
  let label = `LEDGER_DAY::${sign}${account.ledger_day_pnl.toFixed(2)} (${resolved} resolved, ${unresolved} unresolved excluded)`
  const manual = account.ledger_day_manual_excluded ?? 0
  if (manual > 0) {
    label += ` · manual ${manual} excluded (${signed(account.ledger_day_manual_excluded_pnl ?? 0)})`
  }
  return label
}

export function LedgerDayPnl({ account }: { account: AccountInfo }) {
  const positive = (account.ledger_day_pnl ?? 0) >= 0
  return (
    <span
      data-testid="ledger-day-pnl"
      title="Bot P&L for the CME session day (from 17:00 CT — the same day the risk gates count). Same rule as the position-history footer: pnl_corrected only; unresolved rows counted and excluded, never coerced; manual NT8 trades are not the bot's and are counted separately."
      style={{
        color:
          typeof account.ledger_day_pnl === 'number'
            ? positive
              ? '#0ECB81'
              : '#F6465D'
            : 'var(--vl-faint)',
      }}
    >
      {ledgerDayLabel(account)}
    </span>
  )
}
