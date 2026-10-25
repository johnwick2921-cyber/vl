// m420a/B6 — a thin top-of-page truth line. /api/health used to say "ok" with
// the NT8 feed Disconnected for 30h; this reads the two fields m420b adds
// (cme_open, feed_stale) and says it out loud. An older backend sends neither
// field: then nothing renders — the banner never invents a state.
import useSWR from 'swr'
import { updatesApi } from '../../lib/api/updates'
import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'
import { cmeNextReopenMs, formatCT } from '../plan/cmeSession'

function ageText(secs?: number): string {
  if (typeof secs !== 'number' || !isFinite(secs) || secs < 0) return ''
  if (secs < 90) return `${Math.round(secs)}s`
  if (secs < 5400) return `${Math.round(secs / 60)}m`
  return `${Math.round(secs / 3600)}h`
}

export function StatusBanner({ language }: { language: Language }) {
  const { data } = useSWR(
    'status-banner-health',
    () => updatesApi.health(true),
    {
      revalidateOnFocus: false,
      refreshInterval: 30_000,
    }
  )
  if (!data) return null

  let text = ''
  if (data.cme_open === false) {
    const server = data.cme_reopens_at ? Date.parse(data.cme_reopens_at) : NaN
    const when = formatCT(isNaN(server) ? cmeNextReopenMs() : server)
    text = tp('cmeClosedBanner', language, { when })
  } else if (data.cme_open === true && data.feed_stale === true) {
    text = tp('feedDownBanner', language, {
      age: ageText(data.feed_stale_secs),
    }).trim()
  }
  if (!text) return null

  const down = data.cme_open === true
  return (
    <div
      role="status"
      data-testid="status-banner"
      className="relative z-50 w-full text-center text-[11px] font-semibold py-0.5 px-2 truncate"
      style={{
        background: down ? 'rgba(224,108,108,0.18)' : 'rgba(240,185,11,0.12)',
        color: down ? 'var(--vl-short)' : 'var(--vl-gold)',
        borderBottom: '1px solid var(--vl-hair)',
      }}
    >
      {text}
    </div>
  )
}
