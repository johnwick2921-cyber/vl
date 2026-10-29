// m420a/B6 — a thin top-of-page truth line. /api/health used to say "ok" with
// the NT8 feed Disconnected for 30h; this reads the two fields m420b adds
// (cme_open, feed_stale) and says it out loud. An older backend sends neither
// field: then nothing renders — the banner never invents a state.
// m443/#39 — the converse: a DEAD backend used to render nothing either (SWR's
// error was ignored and the last good `data` — or null — stayed on screen).
// A failed poll now shows BOT UNREACHABLE (red) until a poll succeeds.
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
  const { data, error } = useSWR(
    'status-banner-health',
    async () => {
      // health() maps a non-2xx business response to null; a banner that
      // cannot read /api/health cannot vouch for the bot, so null is a failed
      // poll too (network errors already throw).
      const h = await updatesApi.health(true)
      if (!h) throw new Error('health unreadable')
      return h
    },
    {
      revalidateOnFocus: false,
      refreshInterval: 30_000,
    }
  )

  // Unreachable wins over stale `data`: SWR keeps the last good value across a
  // failed poll, and the old CME/FEED line would then mask a dead backend.
  // SWR clears `error` on the next successful poll.
  const unreachable = !!error
  if (!unreachable && !data) return null

  let text = ''
  if (unreachable) {
    text = tp('botUnreachableBanner', language)
  } else if (data && data.cme_open === false) {
    const server = data.cme_reopens_at ? Date.parse(data.cme_reopens_at) : NaN
    const when = formatCT(isNaN(server) ? cmeNextReopenMs() : server)
    text = tp('cmeClosedBanner', language, { when })
  } else if (data && data.cme_open === true && data.feed_stale === true) {
    text = tp('feedDownBanner', language, {
      age: ageText(data.feed_stale_secs),
    }).trim()
  }
  // m444: a 1m hole the bot could not refill (unfilled) or cannot reach
  // (retired) — shown whatever the market state, in its own line below.
  // Merge of m443 + m444: when the poll failed `data` may be undefined, and any
  // stale hole list is no more trustworthy than the stale CME/FEED line.
  const holes = unreachable
    ? []
    : (data?.bars_holes ?? []).filter(
        (h) => h.state === 'unfilled' || h.state === 'retired'
      )
  let holeText = ''
  if (holes.length > 0) {
    // an unfilled hole is the one the owner can act on (reload NT8 history), so
    // it leads; a retired one (older than NT8's replay) cannot be refilled by
    // anyone and gets its own, honest wording
    const actionable = holes.filter((h) => h.state === 'unfilled')
    const pool = actionable.length > 0 ? actionable : holes
    const h = pool.reduce((a, b) =>
      (b.missing_min ?? 0) > (a.missing_min ?? 0) ? b : a
    )
    holeText = tp(
      h.state === 'retired' ? 'missingBarsRetiredBanner' : 'missingBarsBanner',
      language,
      {
        symbol: h.symbol,
        window: h.window ?? '',
        min: String(h.missing_min ?? '?'),
        more: holes.length > 1 ? ` (+${holes.length - 1} more)` : '',
      }
    ).trim()
  }
  if (!text && !holeText) return null

  const down = unreachable || data?.cme_open === true
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
