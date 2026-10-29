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
  // m444: a 1m hole the bot could not refill (unfilled) or cannot reach
  // (retired) — shown whatever the market state, in its own line below.
  const holes = (data.bars_holes ?? []).filter(
    (h) => h.state === 'unfilled' || h.state === 'retired'
  )
  let holeText = ''
  if (holes.length > 0) {
    const h = holes.reduce((a, b) =>
      (b.missing_min ?? 0) > (a.missing_min ?? 0) ? b : a
    )
    holeText = tp('missingBarsBanner', language, {
      symbol: h.symbol,
      window: h.window ?? '',
      min: String(h.missing_min ?? '?'),
      more: holes.length > 1 ? ` (+${holes.length - 1} more)` : '',
    }).trim()
  }
  if (!text && !holeText) return null

  const down = data.cme_open === true
  return (
    <>
      {text && banner(text, down)}
      {holeText && (
        <div
          role="status"
          data-testid="missing-bars-banner"
          className="relative z-50 w-full text-center font-semibold px-2 text-[12px] py-1 break-words"
          style={{
            background: 'rgba(224,108,108,0.18)',
            color: 'var(--vl-short)',
            borderBottom: '1px solid var(--vl-hair)',
          }}
        >
          {holeText}
        </div>
      )}
    </>
  )
}

function banner(text: string, down: boolean) {
  return (
    <div
      role="status"
      data-testid="status-banner"
      className={`relative z-50 w-full text-center font-semibold px-2 ${
        down ? 'text-[12px] py-1 break-words' : 'text-[11px] py-0.5 truncate'
      }`}
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
