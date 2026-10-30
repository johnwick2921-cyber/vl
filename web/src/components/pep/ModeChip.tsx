// Dashboard mode chip: AI / Mentor / Pep A|E. It states a mode only when BOTH
// truths have answered — "pep is off" and "pep is unknown" are different claims
// (the F3 rule: unknown must never read as the quiet default). Unknown renders
// nothing.

import { pt } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { MentorTruth, PepTruth } from '../../lib/api/traders'

export type ModeChipValue =
  | { mode: 'ai' }
  | { mode: 'mentor' }
  | { mode: 'pep'; style: string }
  | null

export function dashboardMode(
  mentor: MentorTruth | null,
  pep: PepTruth | null,
  pepStatus: 'loading' | 'ok' | 'error'
): ModeChipValue {
  if (pep?.enabled) return { mode: 'pep', style: pep.style ?? '?' }
  if (mentor?.enabled) return { mode: 'mentor' }
  if (mentor && pep && pepStatus === 'ok') return { mode: 'ai' }
  return null
}

export function ModeChip({
  value,
  language,
}: {
  value: ModeChipValue
  language: string
}) {
  if (!value) return null
  const lang = language as Language
  const text =
    value.mode === 'pep'
      ? pt('chipPep', lang, { style: value.style })
      : value.mode === 'mentor'
        ? pt('chipMentor', lang)
        : pt('chipAI', lang)
  const color =
    value.mode === 'pep'
      ? '#4FC3F7'
      : value.mode === 'mentor'
        ? '#E8A33D'
        : '#848E9C'
  return (
    <span
      data-testid="mode-chip"
      data-mode={value.mode}
      title={pt('chipTitle', lang)}
      className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wide"
      style={{ color, border: `1px solid ${color}` }}
    >
      {text}
    </span>
  )
}
