// m445 (release #54) — the DATA-INTEGRITY pause line. Owner, 2026-10-29: "this
// is a financial product, not a game — nothing related to data loss can
// continue to trade." When the bot refuses every NEW entry because 1m bars are
// missing from a data window (or because it cannot tell), the owner must SEE
// it, in the window and in CT, not discover it from a silent day.
//
// Reads /api/health `trading_paused` (trader/ninjatrader/bar_integrity.go
// TradingPausedSnapshot via api/handler_health.go). ABSENT (an older bot, or no
// NT8 bridge in that process) or paused=false -> nothing renders: this line
// never invents a state. The stale-feed pause already has its own line
// (StatusBanner: FEED DOWN), so only the data classes are shown here.
import useSWR from 'swr'
import { updatesApi } from '../../lib/api/updates'
import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'

const DATA_CLASSES = ['data_integrity_hole', 'data_integrity_unknown']
// release #55: 'data_truth' (with data_truth_checks) is the m446/m447 pause

// scope (bar_integrity.go PausedView.Scope) -> the i18n key naming it
const SCOPE_KEYS: Record<
  string,
  'dataPausedScopeSwing' | 'dataPausedScopeDeep' | 'dataPausedScopeMentor'
> = {
  swing_entries: 'dataPausedScopeSwing',
  swing_and_deep_entries: 'dataPausedScopeDeep',
  mentor_entries: 'dataPausedScopeMentor',
}

// release #55 round 3 (review 4 D1): the scope of a DATA-TRUTH pause (the bot
// pauses by reader or by trader/account; a banner never claims more than the bot
// refuses). The hole-class scopes keep SCOPE_KEYS above.
const TRUTH_SCOPE_KEYS: Record<
  string,
  | 'dataPausedScopeSwing'
  | 'dataPausedScopeDeep'
  | 'dataPausedTruthScopeMentor'
  | 'dataPausedTruthScopeAI'
  | 'dataPausedTruthScopeTraders'
> = {
  swing_entries: 'dataPausedScopeSwing',
  swing_and_deep_entries: 'dataPausedScopeDeep',
  mentor_entries: 'dataPausedTruthScopeMentor',
  ai_entries: 'dataPausedTruthScopeAI',
  trader_entries: 'dataPausedTruthScopeTraders',
}

// "acct:SIM101" -> "account SIM101", "trader:abc" -> "trader abc"
function scopeLabel(token: string): string {
  if (token.startsWith('acct:')) return `account ${token.slice(5)}`
  if (token.startsWith('trader:')) return `trader ${token.slice(7)}`
  return token
}

export function DataPausedBanner({ language }: { language: Language }) {
  const { data } = useSWR('data-paused-health', () => updatesApi.health(true), {
    revalidateOnFocus: false,
    refreshInterval: 30_000,
  })
  const tpz = data?.trading_paused
  if (!tpz || tpz.paused !== true) return null
  const reasons = tpz.reasons ?? []
  const truthChecks = tpz.data_truth_checks ?? []
  const hasTruth = reasons.includes('data_truth') && truthChecks.length > 0
  if (!reasons.some((r) => DATA_CLASSES.includes(r)) && !hasTruth) return null

  const windows = tpz.windows ?? []
  const hasMissing = reasons.some((r) => DATA_CLASSES.includes(r))
  let text: string
  if (hasTruth && !hasMissing) {
    // release #55: only the data-truth checks pause trading - name them
    const scopeKey = TRUTH_SCOPE_KEYS[tpz.scope ?? '']
    // a check scoped to traders/accounts names them next to it
    const who = tpz.data_truth_scopes ?? {}
    const checks = truthChecks
      .map((c) => {
        const w = who[c]
        return w && w.length > 0 ? `${c} (${w.map(scopeLabel).join(', ')})` : c
      })
      .join(', ')
    text = scopeKey
      ? tp('dataPausedTruthScopedBanner', language, {
          scope: tp(scopeKey, language),
          checks,
        })
      : tp('dataPausedTruthBanner', language, { checks })
  } else if (windows.length > 0) {
    const more = windows.length > 1 ? ` (+${windows.length - 1})` : ''
    const scopeKey = SCOPE_KEYS[tpz.scope ?? '']
    // all_entries (or a scope an older bot does not send) keeps the plain line;
    // a narrower pause says WHICH entries are paused, so a banner never claims
    // more than the bot refuses (round 3, D3)
    text = scopeKey
      ? tp('dataPausedScopedBanner', language, {
          scope: tp(scopeKey, language),
          window: windows[0] + more,
        })
      : tp('dataPausedBanner', language, { window: windows[0] + more })
  } else {
    text = tp('dataPausedUnknownBanner', language)
  }

  return (
    <div
      role="status"
      data-testid="data-paused-banner"
      className="relative z-50 w-full text-center font-semibold px-2 text-[12px] py-1 break-words"
      style={{
        background: 'rgba(224,108,108,0.18)',
        color: 'var(--vl-short)',
        borderBottom: '1px solid var(--vl-hair)',
      }}
    >
      {text}
    </div>
  )
}
