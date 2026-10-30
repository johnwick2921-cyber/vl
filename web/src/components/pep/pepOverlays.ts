// Pep chart overlay: the opening-range BOX (and, while a pep position is open,
// its stop and target) as horizontal price lines through the same
// ChartLineSpec plumbing the mentor lines use. Pure: the chart draws ONLY what
// this returns, and every price comes from a field /pep-truth sent — nothing is
// invented. A box that is not drawn yet returns NO lines (never a line at 0).

import type { Language } from '../../i18n/translations'
import { pt } from '../../i18n/pep-translations'
import type { PepTruth } from '../../lib/api/traders'
import type { ChartLineSpec } from '../charts/chartOverlays'

export const PEP_COLOR = '#4FC3F7' // pep sky blue — distinct from mentor amber and order lines

/** The stale mark a pep line carries, in the UI language. The English text is
 * byte-identical to the mentor's mentorStaleMark (pinned); zh and id used to get
 * that English suffix glued to a translated label. */
export function pepStaleMark(
  staleSinceMs: number | undefined,
  lang: Language = 'en'
): string {
  if (staleSinceMs === undefined) return ''
  if (!(staleSinceMs > 0)) return pt('staleUnknown', lang)
  const t = new Date(staleSinceMs).toLocaleTimeString('en-US', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'America/Chicago',
  })
  return pt('staleSince', lang, { t })
}

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2))

export function pepChartLines(
  truth: PepTruth | null | undefined,
  staleSinceMs?: number,
  lang: Language = 'en'
): ChartLineSpec[] {
  if (!truth || !truth.enabled) return []
  const stale = pepStaleMark(staleSinceMs, lang)
  const out: ChartLineSpec[] = []
  const b = truth.box
  if (b && b.drawn && b.high > 0 && b.low > 0) {
    out.push(
      {
        price: b.high,
        label: `${pt('lineBoxHigh', lang, { price: fmt(b.high) })}${stale}`,
        color: PEP_COLOR,
        style: 1,
      },
      {
        price: b.low,
        label: `${pt('lineBoxLow', lang, { price: fmt(b.low) })}${stale}`,
        color: PEP_COLOR,
        style: 1,
      }
    )
  }
  if (truth.position_open) {
    if (truth.stop && truth.stop > 0)
      out.push({
        price: truth.stop,
        label: `${pt('lineStop', lang, { price: fmt(truth.stop) })}${stale}`,
        color: PEP_COLOR,
        style: 2,
      })
    if (truth.target && truth.target > 0)
      out.push({
        price: truth.target,
        label: `${pt('lineTarget', lang, { price: fmt(truth.target) })}${stale}`,
        color: PEP_COLOR,
        style: 2,
      })
  }
  return out
}
