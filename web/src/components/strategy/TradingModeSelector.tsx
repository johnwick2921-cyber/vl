import { useState } from 'react'

import { pt } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { EffectiveKnob } from '../../lib/api/strategyEffective'
import { EffectiveLine } from './EffectiveChip'
import type { TradingMode } from './pepModel'

// Three-way "Trading mode" selector (AI planner / Mentor / Pep). A strategy is
// exactly ONE of them. The selector hands the chosen mode to ONE callback
// (applyTradingMode in pepModel.ts) that writes risk_control.mentor_mode and
// pep.pep_enabled TOGETHER, so the illegal both-ON state cannot be produced
// here — and the mentor switch inside MentorModeToggle is routed through the
// same callback by RiskControlEditor. Turning Mentor or Pep ON asks first
// (SIM-only wording); turning back to AI is immediate. The server still refuses
// pep + mentor and pep + day plan by name (store.TradingModeConflict).

const MODES: TradingMode[] = ['ai', 'mentor', 'pep']

const LABEL = {
  ai: 'modeAI',
  mentor: 'modeMentor',
  pep: 'modePep',
} as const
const HELP = {
  ai: 'modeAIHelp',
  mentor: 'modeMentorHelp',
  pep: 'modePepHelp',
} as const

export function TradingModeSelector({
  mode,
  conflict = false,
  dayPlanEnabled = false,
  showPep = true,
  disabled,
  language,
  strategyName,
  pepStyle = 'A',
  pepContracts = 3,
  onSelect,
  eff,
}: {
  mode: TradingMode
  /** The loaded config has mentor AND pep both ON (the server refuses to save it). */
  conflict?: boolean
  /** day_plan.plan_enabled — the server refuses pep together with the day plan. */
  dayPlanEnabled?: boolean
  /** Pep is a futures mode; hidden for a non-futures strategy. */
  showPep?: boolean
  disabled?: boolean
  language: string
  strategyName?: string
  pepStyle?: string
  pepContracts?: number
  onSelect: (mode: TradingMode) => void
  eff?: (path: string) => EffectiveKnob | null | undefined
}) {
  const lang = language as Language
  const [pending, setPending] = useState<TradingMode | null>(null)
  const modes = showPep ? MODES : MODES.filter((m) => m !== 'pep')
  const pepBlocked = dayPlanEnabled && mode !== 'pep'

  const choose = (m: TradingMode) => {
    if (disabled) return
    if (m === 'pep' && pepBlocked) return
    // A conflicted config is re-chosen even when the clicked radio reads as
    // current: the write is what clears the illegal combination.
    if (m === mode && !conflict) return
    if (m === 'ai') {
      setPending(null)
      onSelect('ai')
      return
    }
    setPending(m)
  }

  return (
    <div
      className="p-4 rounded-lg"
      style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      data-testid="trading-mode-block"
    >
      <div className="text-sm font-medium" style={{ color: '#EAECEF' }}>
        {pt('modeTitle', lang)}
      </div>
      <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
        {pt('modeDesc', lang)}
      </p>
      <div
        role="radiogroup"
        aria-label={pt('modeTitle', lang)}
        className="mt-3 flex flex-wrap gap-2"
      >
        {modes.map((m) => {
          const on = m === mode
          const blocked = m === 'pep' && pepBlocked
          return (
            <button
              key={m}
              type="button"
              role="radio"
              aria-checked={on}
              aria-disabled={blocked || disabled ? true : undefined}
              disabled={disabled || blocked}
              data-testid={`trading-mode-${m}`}
              onClick={() => choose(m)}
              title={pt(HELP[m], lang)}
              className="px-3 py-1.5 rounded text-xs font-semibold"
              style={{
                background: on ? '#F0B90B' : '#1E2329',
                color: on ? '#0B0E11' : '#EAECEF',
                border: `1px solid ${on ? '#F0B90B' : '#2B3139'}`,
                opacity: disabled || blocked ? 0.5 : 1,
              }}
            >
              {pt(LABEL[m], lang)}
            </button>
          )
        })}
      </div>
      <p
        className="text-xs mt-2"
        style={{ color: '#848E9C' }}
        data-testid="trading-mode-help"
      >
        {pt(HELP[mode], lang)}
      </p>
      {pepBlocked && showPep && (
        <p
          className="text-xs mt-2"
          style={{ color: '#F0B90B' }}
          role="note"
          data-testid="trading-mode-dayplan-block"
        >
          {pt('modeDayPlanBlocks', lang)}
        </p>
      )}
      {conflict && (
        <p
          className="text-xs mt-2"
          style={{ color: '#F6465D' }}
          role="alert"
          data-testid="trading-mode-conflict"
        >
          {pt('modeConflict', lang)}
        </p>
      )}
      <EffectiveLine
        knob={eff?.('pep.pep_enabled')}
        label={pt('effEnabled', lang)}
      />
      {pending && (
        <div
          role="alertdialog"
          aria-label={pt(
            pending === 'pep' ? 'confirmPepTitle' : 'confirmMentorTitle',
            lang
          )}
          data-testid="trading-mode-confirm"
          className="mt-3 p-3 rounded"
          style={{ background: '#1E2329', border: '1px solid #F0B90B' }}
        >
          <p className="text-sm font-medium" style={{ color: '#EAECEF' }}>
            {pt(
              pending === 'pep' ? 'confirmPepTitle' : 'confirmMentorTitle',
              lang
            )}
          </p>
          <p className="text-xs mt-1" style={{ color: '#EAECEF' }}>
            {pt('confirmStrategy', lang)}: {strategyName || '—'}
          </p>
          <p className="text-xs mt-1" style={{ color: '#EAECEF' }}>
            {pending === 'pep'
              ? pt('confirmPepBody', lang, {
                  n: pepContracts,
                  style: pepStyle,
                })
              : pt('confirmMentorBody', lang)}
          </p>
          <div className="flex gap-2 mt-3">
            <button
              type="button"
              data-testid="trading-mode-confirm-ok"
              className="px-3 py-1 rounded text-xs font-semibold"
              style={{ background: '#0ECB81', color: '#0B0E11' }}
              onClick={() => {
                const m = pending
                setPending(null)
                onSelect(m)
              }}
            >
              {pt('confirmOk', lang)}
            </button>
            <button
              type="button"
              data-testid="trading-mode-confirm-cancel"
              className="px-3 py-1 rounded text-xs"
              style={{ background: '#2B3139', color: '#EAECEF' }}
              onClick={() => setPending(null)}
            >
              {pt('confirmCancel', lang)}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
