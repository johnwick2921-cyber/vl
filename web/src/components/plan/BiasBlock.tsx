// P4.3 — the bias block: direction word (display font) + conviction + the
// explicit flip-condition line. Direction word is long/short/neutral-tinted.

import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'
import type { PlanBias } from '../../lib/api/plan'

export function BiasBlock({
  bias,
  language,
  adviceOnly = false,
}: {
  bias: PlanBias
  language: Language
  /** mentor mode ON → this card is advice only; the mentor places the trades. */
  adviceOnly?: boolean
}) {
  const dir = (bias.direction || 'neutral').toLowerCase()
  const color =
    dir === 'long'
      ? 'var(--vl-long)'
      : dir === 'short'
        ? 'var(--vl-short)'
        : 'var(--vl-muted)'
  const word =
    dir === 'long'
      ? tp('biasLong', language)
      : dir === 'short'
        ? tp('biasShort', language)
        : tp('biasNeutral', language)

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline gap-3">
        <span
          className="text-[10px] uppercase tracking-widest"
          style={{ color: 'var(--vl-faint)', fontFamily: 'var(--vl-font-ui)' }}
        >
          {tp('bias', language)}
        </span>
        {adviceOnly && (
          <span
            className="text-[10px] uppercase tracking-widest"
            style={{ color: 'var(--vl-warn)', fontFamily: 'var(--vl-font-ui)' }}
          >
            {tp('mentorAdviceOnly', language)}
          </span>
        )}
        <span
          style={{
            color,
            fontFamily: 'var(--vl-font-display)',
            fontSize: 30,
            lineHeight: 1,
            fontWeight: 600,
          }}
        >
          {word}
        </span>
        {bias.conviction && (
          <span
            className="text-[11px]"
            style={{
              color: 'var(--vl-muted)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {bias.conviction} {tp('conviction', language)}
          </span>
        )}
      </div>
      {bias.flip_condition && bias.flip_condition !== 'n/a' && (
        <div
          className="text-[11px]"
          style={{ color: 'var(--vl-faint)', fontFamily: 'var(--vl-font-ui)' }}
        >
          ↻ {tp('flipPrefix', language)} ·{' '}
          <span className="vl-num" style={{ color: 'var(--vl-muted)' }}>
            {bias.flip_condition}
          </span>
        </div>
      )}
      {/* CLASS 50b — the dual label: AI · tree · regime, a LABEL not a direction */}
      {bias.bias_label && (
        <div
          className="text-[11px]"
          style={{ color: 'var(--vl-faint)', fontFamily: 'var(--vl-font-ui)' }}
        >
          {bias.bias_label}
        </div>
      )}
    </div>
  )
}
