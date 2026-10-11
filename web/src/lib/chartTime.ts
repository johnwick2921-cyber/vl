// THE ONE chart-timezone site (chart-timestamp dispatch, 2026-08-19).
//
// lightweight-charts does NO timezone conversion — axis and crosshair labels
// render the raw epoch in UTC. The owner contract pins EVERY viewer to Houston
// (America/Chicago), and NT8's own chart labels in that zone, so every chart
// surface must format through THESE helpers. No component may do its own
// timezone math on bar times; the conversion is Intl-rule-based (DST-safe: a
// January bar renders CST −6, an August bar CDT −5 — never a fixed constant).
//
// Extracted from AdvancedChart (which already carried the correct formatters)
// so PlanMiniChart — which used to hand the lib raw epochs and therefore
// showed UTC labels, +5h vs NT8 — shares the identical single implementation.
// (TradingViewChart is the one exception: the external widget takes
// timezone:'America/Chicago' natively.)

import { TickMarkType, type Time } from 'lightweight-charts'

export const CHART_TZ = 'America/Chicago'

function timeToMs(time: Time): number {
  if (typeof time === 'number') return time * 1000
  if (typeof time === 'string') return new Date(time).getTime() // "YYYY-MM-DD"
  return Date.UTC(time.year, time.month - 1, time.day) // BusinessDay
}

/** X-axis tick labels in CT, respecting the granularity the lib asks for. */
export function ctTickMarkFormatter(
  time: Time,
  tickMarkType: TickMarkType
): string {
  const d = new Date(timeToMs(time))
  switch (tickMarkType) {
    case TickMarkType.Year:
      return d.toLocaleString('zh-CN', { year: 'numeric', timeZone: CHART_TZ })
    case TickMarkType.Month:
      return d.toLocaleString('zh-CN', { month: 'short', timeZone: CHART_TZ })
    case TickMarkType.DayOfMonth:
      return d.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        timeZone: CHART_TZ,
      })
    default: // Time / TimeWithSeconds — intraday ticks
      return d.toLocaleString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
        timeZone: CHART_TZ,
      })
  }
}

/** Crosshair time label — same zone as the axis, so the two always agree. */
export function ctCrosshairTimeFormatter(time: number): string {
  return new Date(time * 1000).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: CHART_TZ,
  })
}

// ── F6 equity-chart "Invalid Date" (2026-10-10) ────────────────────────────
// The equity-history API used to send ONLY a display string (kernel.FormatCT →
// "2026-09-17 14:12 CT"). `new Date()` on that string returns Invalid Date on
// every browser, so every axis label rendered "Invalid Date" since 08-18.
// The API now also sends timestamp_ms (epoch ms); the chart must prefer it and
// NEVER parse the display string with new Date(). The string fallback is parsed
// BY HAND (its parts are already CT) for older servers.

/** Reformat a kernel.FormatCT string ("YYYY-MM-DD HH:mm CT") to "MM/DD HH:mm"
 *  by hand — never via new Date(). Unparseable → "—". */
export function formatCTDisplayString(s: string | undefined | null): string {
  const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}) CT$/.exec(
    (s ?? '').trim()
  )
  if (!m) return '—'
  return `${m[2]}/${m[3]} ${m[4]}:${m[5]}`
}

/** The one equity-point time label: epoch ms (preferred) formatted in CT as
 *  "MM/DD HH:mm"; else the hand-parsed CT display string; else "—". */
export function formatEquityTimestamp(
  ms: number | undefined | null,
  ctFallback?: string | null
): string {
  if (typeof ms === 'number' && ms > 0) {
    // zh-CN renders "MM/DD HH:mm" with a space (en-US inserts a comma) — the
    // same shape ctCrosshairTimeFormatter pins, so axis and tooltip agree.
    return new Date(ms).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
      timeZone: CHART_TZ,
    })
  }
  if (ctFallback) return formatCTDisplayString(ctFallback)
  return '—'
}

/** Dev-only diagnosis (dispatch 2.3): raw epoch, UTC render, CT render of the
 *  newest bar — the next mismatch becomes a one-glance read. */
export function logBarDebug(tag: string, epochMs: number | undefined): void {
  if (!import.meta.env.DEV || !epochMs) return
  const utc = new Date(epochMs).toISOString()
  const ct = new Intl.DateTimeFormat('en-US', {
    timeZone: CHART_TZ,
    dateStyle: 'short',
    timeStyle: 'medium',
    hour12: false,
  }).format(epochMs)

  console.debug(`[chartTime] ${tag} raw=${epochMs} utc=${utc} ct=${ct} CT`)
}
