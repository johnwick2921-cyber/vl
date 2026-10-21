// R22 PNL-TRUTH — every money figure on the dashboard says what it IS.
//
// RISK-PNL P1-2: the "Total P&L" card and the header PNL:: showed total_pnl,
// which for an NT8 account is NT8's SESSION realized+unrealized (it resets
// every session — +74.00 on 10-14), while the tooltip said "equity − initial
// balance" (−2,305.00 that night). The label, the value and the tooltip now
// come from ONE place and describe the same number:
//
//   • NT8_SESSION — an ACCOUNT figure, as NT8 reports it (session-reset).
//   • Account vs initial — an ACCOUNT figure: equity − initial balance.
//   • Bot P&L (record) — the BOT's own closed trades (pnl_corrected), with the
//     manual NT8 trades (owner ruling R1) shown as their own line, never hidden.
//
// An absent figure is UNKNOWN and renders "n/a" — never a fabricated 0.
import type { AccountInfo } from '../../types/trading'

export function signedMoney(v: number): string {
  const abs = Math.abs(v).toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
  return `${v >= 0 ? '+' : '−'}${abs}`
}

export interface HeaderPnl {
  label: string
  title: string
}

// headerAccountPnl is the header chip that used to read "PNL::" over a
// tooltip that described a different number.
export function headerAccountPnl(account: AccountInfo): HeaderPnl {
  if (typeof account.nt8_session_pnl === 'number') {
    return {
      label: `NT8_SESSION::${signedMoney(account.nt8_session_pnl)}`,
      title:
        "Account figure from NT8: this NT8 session's realized + unrealized P&L. NT8 resets it every session. It is not the bot's record and not equity − initial balance.",
    }
  }
  if (typeof account.account_pnl_vs_initial === 'number') {
    return {
      label: `ACCT_VS_INITIAL::${signedMoney(account.account_pnl_vs_initial)}`,
      title:
        'Account figure: total equity − the configured initial balance. Not the bot record.',
    }
  }
  return {
    label: 'ACCT_PNL::n/a',
    title: 'The broker reported no session P&L and no initial balance is set.',
  }
}

export interface CardText {
  value: string
  subtitle: string
  positive: boolean
  changePct?: number
}

// accountVsInitialCard feeds the Total Equity card: its % and subtitle are
// equity − initial balance (it used to show the NT8 session %).
export function accountVsInitialCard(account: AccountInfo): CardText {
  const v = account.account_pnl_vs_initial
  if (typeof v !== 'number') {
    return {
      value: 'n/a',
      subtitle: 'Account vs initial: n/a (no initial balance set)',
      positive: true,
    }
  }
  const base =
    typeof account.initial_balance === 'number' && account.initial_balance > 0
      ? ` (initial ${account.initial_balance.toLocaleString('en-US')})`
      : ''
  return {
    value: signedMoney(v),
    subtitle: `Account vs initial: ${signedMoney(v)}${base}`,
    positive: v >= 0,
    changePct:
      typeof account.account_pnl_vs_initial_pct === 'number'
        ? account.account_pnl_vs_initial_pct
        : undefined,
  }
}

// manualLine is the separate, visible line for the manual NT8 trades.
export function manualLine(trades?: number, pnl?: number): string {
  const n = trades ?? 0
  if (n <= 0) return 'Manual (excluded): none'
  return `Manual (excluded): ${n} trade${n === 1 ? '' : 's'}, ${signedMoney(pnl ?? 0).replace(/^([+−])/, '$1$')}`
}

// botRecordCard is the card that used to be titled "Total P&L": the BOT's
// closed-trade record with the manual trades on their own line.
export function botRecordCard(account: AccountInfo): CardText {
  if (typeof account.bot_record_pnl !== 'number') {
    return {
      value: 'n/a',
      subtitle: account.bot_record_status ?? 'Bot record unavailable',
      positive: true,
    }
  }
  const n = account.bot_record_trades ?? 0
  const unres = account.bot_record_unresolved ?? 0
  const unresTxt = unres > 0 ? ` (${unres} unresolved excluded)` : ''
  const scope = botRecordScopeLine(account)
  return {
    value: signedMoney(account.bot_record_pnl),
    subtitle: `${n} bot trades${unresTxt} · ${manualLine(account.manual_excluded_trades, account.manual_excluded_pnl)}${scope ? ` · ${scope}` : ''}`,
    positive: account.bot_record_pnl >= 0,
  }
}

// botRecordScopeLine (RT-7, release #24) says which scope the card's figure
// covers. Scoped to the selected account, it names the bot trades on OTHER
// accounts it leaves out — the server's count, never a literal — because
// Position History's total is trader-wide and the two used to disagree with
// no word of why. Which scope is "the record" is an owner decision. '' = an
// older server that sent no scope.
export function botRecordScopeLine(account: AccountInfo): string {
  const k = account.bot_record_other_accounts_trades
  if (typeof k === 'number') {
    if (k <= 0) return 'this account only (no bot trades on other accounts)'
    const pnl = signedMoney(account.bot_record_other_accounts_pnl ?? 0)
    return `${k} bot trade${k === 1 ? '' : 's'} (${pnl}) on other accounts not counted here`
  }
  if (account.bot_record_other_accounts_status) {
    return `other accounts: ${account.bot_record_other_accounts_status}`
  }
  if (account.bot_record_scope === 'all accounts') return 'all accounts'
  return ''
}
