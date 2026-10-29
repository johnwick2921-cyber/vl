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

export function DataPausedBanner({ language }: { language: Language }) {
  const { data } = useSWR('data-paused-health', () => updatesApi.health(true), {
    revalidateOnFocus: false,
    refreshInterval: 30_000,
  })
  const tpz = data?.trading_paused
  if (!tpz || tpz.paused !== true) return null
  const reasons = tpz.reasons ?? []
  if (!reasons.some((r) => DATA_CLASSES.includes(r))) return null

  const windows = tpz.windows ?? []
  let text: string
  if (windows.length > 0) {
    const more = windows.length > 1 ? ` (+${windows.length - 1})` : ''
    text = tp('dataPausedBanner', language, { window: windows[0] + more })
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
