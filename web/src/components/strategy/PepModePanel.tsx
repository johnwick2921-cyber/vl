import { useState } from 'react'

import { pt, type PepKey } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { StudioEffective } from '../../lib/api/strategyEffective'
import type { PepConfig } from '../../types'
import { EffectiveLine } from './EffectiveChip'
import {
  PEP_DEFAULTS,
  PEP_RANGES,
  PEP_TF_CHOICES,
  fullPepBlock,
  isHHMM,
  type PepStyle,
} from './pepModel'
import { ClampedNumberInput } from './RiskControlEditor'

// PEP MODE settings panel (owner order 2026-10-29). Layout: a state banner, the
// SHARED block (style that runs, contracts, box start, flat time, tick, ...),
// then a Style A / Style E tab that swaps which style's own numbers are shown.
//
// Rules this panel keeps (the explorer's UI recs):
//  - EVERY field shows a CONCRETE value (the stored one, else the shipped
//    default) — never "blank = default".
//  - EVERY edit sends the WHOLE pep block (fullPepBlock): the server-side merge
//    keeps absent keys, so a partial block could leave a stale number behind.
//  - Each field ends with the server's EffectiveLine (the value the bot RUNS,
//    its origin, any clamp). The panel never derives an effective value.
//  - PEP_FIELDS below IS the control set: the guide's honesty test reads it, so
//    a guide card that names a control the Studio lacks (or vice versa) is RED.

type Kind = 'int' | 'float' | 'time' | 'enum' | 'tf'
type Group = 'shared' | 'a' | 'e'

export interface PepField {
  /** The stored path in the strategy config (= the knob-registry path). */
  path: string
  leaf: string
  group: Group
  kind: Kind
  label: PepKey
  help: PepKey
  min?: number
  max?: number
  step?: number
  fallback?: number | string
  options?: { value: string; label: PepKey }[]
  get: (b: PepConfig) => number | string | undefined
  set: (b: PepConfig, v: number | string) => PepConfig
}

const D = PEP_DEFAULTS
const R = PEP_RANGES

const shared = (
  leaf: keyof PepConfig,
  kind: Kind,
  label: PepKey,
  help: PepKey,
  extra: Partial<PepField> = {}
): PepField => ({
  path: `pep.${leaf}`,
  leaf,
  group: 'shared',
  kind,
  label,
  help,
  get: (b) => b[leaf] as number | string | undefined,
  set: (b, v) => ({ ...b, [leaf]: v }),
  ...extra,
})

const styleA = (
  leaf: keyof NonNullable<PepConfig['style_a']>,
  kind: Kind,
  label: PepKey,
  help: PepKey,
  extra: Partial<PepField> = {}
): PepField => ({
  path: `pep.style_a.${leaf}`,
  leaf,
  group: 'a',
  kind,
  label,
  help,
  get: (b) => b.style_a?.[leaf] as number | string | undefined,
  set: (b, v) => ({ ...b, style_a: { ...b.style_a, [leaf]: v } }),
  ...extra,
})

const styleE = (
  leaf: keyof NonNullable<PepConfig['style_e']>,
  kind: Kind,
  label: PepKey,
  help: PepKey,
  extra: Partial<PepField> = {}
): PepField => ({
  path: `pep.style_e.${leaf}`,
  leaf,
  group: 'e',
  kind,
  label,
  help,
  get: (b) => b.style_e?.[leaf] as number | string | undefined,
  set: (b, v) => ({ ...b, style_e: { ...b.style_e, [leaf]: v } }),
  ...extra,
})

export const PEP_FIELDS: PepField[] = [
  shared('pep_style', 'enum', 'runsStyle', 'runsStyleHelp', {
    // the server case-folds the style; show the canonical letter
    get: (b) => (b.pep_style ?? '').toUpperCase() || undefined,
    options: [
      { value: 'A', label: 'styleAName' },
      { value: 'E', label: 'styleEName' },
    ],
    fallback: D.style,
  }),
  shared('pep_contracts', 'int', 'fContracts', 'fContractsHelp', {
    ...R.contracts,
    fallback: D.contracts,
  }),
  shared('pep_box_start_ct', 'time', 'fBoxStart', 'fBoxStartHelp', {
    fallback: D.boxStartCT,
  }),
  shared('pep_flat_time_ct', 'time', 'fFlatTime', 'fFlatTimeHelp', {
    fallback: D.flatTimeCT,
  }),
  shared('pep_tick_size', 'float', 'fTick', 'fTickHelp', {
    ...R.tickSize,
    step: 0.25,
    fallback: D.tickSize,
  }),
  shared('pep_sl_rounding', 'enum', 'fSlRounding', 'fSlRoundingHelp', {
    options: [
      { value: 'outward', label: 'optOutward' },
      { value: 'nearest', label: 'optNearest' },
    ],
    fallback: D.slRounding,
  }),
  shared('pep_max_trades_per_day', 'int', 'fMaxTrades', 'fMaxTradesHelp', {
    ...R.maxTradesPerDay,
    fallback: D.maxTradesPerDay,
  }),
  shared('pep_fee_per_contract_usd', 'float', 'fFee', 'fFeeHelp', {
    ...R.feePerContractUSD,
    step: 0.5,
    fallback: D.feePerContractUSD,
  }),
  shared('pep_max_box_points', 'float', 'fMaxBox', 'fMaxBoxHelp', {
    ...R.maxBoxPoints,
    step: 1,
    fallback: D.maxBoxPoints,
  }),
  shared('pep_signal_max_age_min', 'int', 'fSignalAge', 'fSignalAgeHelp', {
    ...R.signalMaxAgeMin,
    fallback: D.signalMaxAgeMin,
  }),

  styleA('pep_a_box_minutes', 'int', 'fBoxMinutes', 'fABoxMinutesHelp', {
    ...R.boxMinutes,
    fallback: D.a.boxMinutes,
  }),
  styleA('pep_a_sl_fraction', 'float', 'fSlFraction', 'fASlHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.a.slFraction,
  }),
  styleA('pep_a_tp_fraction', 'float', 'fTpFraction', 'fATpHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.a.tpFraction,
  }),
  styleA('pep_a_both_edges', 'enum', 'fBothEdges', 'fBothEdgesHelp', {
    options: [
      { value: 'no_trade', label: 'optNoTrade' },
      { value: 'nearest_open', label: 'optNearestOpen' },
    ],
    fallback: D.a.bothEdges,
  }),

  styleE('pep_e_box_minutes', 'int', 'fBoxMinutes', 'fEBoxMinutesHelp', {
    ...R.boxMinutes,
    fallback: D.e.boxMinutes,
  }),
  styleE('pep_e_sl_fraction', 'float', 'fSlFraction', 'fESlHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.e.slFraction,
  }),
  styleE('pep_e_tp_fraction', 'float', 'fTpFraction', 'fETpHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.e.tpFraction,
  }),
  styleE('pep_e_ema_period', 'int', 'fEmaPeriod', 'fEmaPeriodHelp', {
    ...R.emaPeriod,
    fallback: D.e.emaPeriod,
  }),
  styleE('pep_e_ema_tf_min', 'tf', 'fEmaTf', 'fEmaTfHelp', {
    fallback: D.e.emaTFMin,
  }),
  styleE('pep_e_signal_source', 'enum', 'fSignalSource', 'fSignalSourceHelp', {
    options: [
      { value: 'ema', label: 'optEma' },
      { value: 'close', label: 'optClose' },
    ],
    fallback: D.e.signalSource,
  }),
  styleE('pep_e_warmup_bars', 'int', 'fWarmup', 'fWarmupHelp', {
    ...R.warmupBars,
    fallback: D.e.warmupBars,
  }),
]

const inputStyle = {
  background: '#1E2329',
  border: '1px solid #2B3139',
  color: '#EAECEF',
} as const

function FieldRow({
  f,
  block,
  disabled,
  lang,
  effective,
  onChange,
}: {
  f: PepField
  block: PepConfig
  disabled?: boolean
  lang: Language
  effective?: StudioEffective
  onChange: (b: PepConfig) => void
}) {
  const [timeDraft, setTimeDraft] = useState<string | null>(null)
  const cur = f.get(block) ?? f.fallback
  const testId = `pep-${f.leaf}`
  // The a/e tabs share label keys (fBoxMinutes ...): the help text names the style.
  let control: React.ReactNode
  if (f.kind === 'int' || f.kind === 'float') {
    control = (
      <ClampedNumberInput
        value={cur as number}
        fallback={f.fallback as number}
        min={f.min ?? 0}
        max={f.max ?? 1e9}
        step={f.step}
        disabled={disabled}
        testId={testId}
        onCommit={(n) =>
          onChange(f.set(block, f.kind === 'int' ? Math.round(n) : n))
        }
      />
    )
  } else if (f.kind === 'time') {
    const bad = timeDraft !== null && !isHHMM(timeDraft)
    control = (
      <>
        <input
          type="time"
          data-testid={testId}
          disabled={disabled}
          value={timeDraft ?? String(cur)}
          onChange={(e) => {
            if (disabled) return
            const v = e.target.value
            setTimeDraft(v)
            if (isHHMM(v)) {
              setTimeDraft(null)
              onChange(f.set(block, v))
            }
          }}
          onBlur={() => setTimeDraft(null)}
          className="ml-2 px-2 py-1 rounded"
          style={inputStyle}
        />
        {bad && (
          <span
            className="text-xs ml-2"
            style={{ color: '#F6465D' }}
            role="alert"
            data-testid={`${testId}-error`}
          >
            {pt('badTime', lang)}
          </span>
        )}
      </>
    )
  } else if (f.kind === 'tf') {
    const shown = Number(cur)
    const choices = PEP_TF_CHOICES.includes(shown)
      ? PEP_TF_CHOICES
      : [...PEP_TF_CHOICES, shown].sort((a, b) => a - b)
    control = (
      <select
        data-testid={testId}
        disabled={disabled}
        value={shown}
        onChange={(e) =>
          !disabled && onChange(f.set(block, Number(e.target.value)))
        }
        className="ml-2 px-2 py-1 rounded"
        style={inputStyle}
      >
        {choices.map((n) => (
          <option key={n} value={n}>
            {n}
          </option>
        ))}
      </select>
    )
  } else {
    const opts = f.options ?? []
    const shown = String(cur)
    const known = opts.some((o) => o.value === shown)
    control = (
      <select
        data-testid={testId}
        disabled={disabled}
        value={shown}
        onChange={(e) => !disabled && onChange(f.set(block, e.target.value))}
        className="ml-2 px-2 py-1 rounded"
        style={inputStyle}
      >
        {!known && <option value={shown}>{shown}</option>}
        {opts.map((o) => (
          <option key={o.value} value={o.value}>
            {pt(o.label, lang)}
          </option>
        ))}
      </select>
    )
  }
  return (
    <div className="py-2" data-testid={`${testId}-row`}>
      <label className="text-xs" style={{ color: '#EAECEF' }}>
        {pt(f.label, lang)}
        {control}
      </label>
      <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
        {pt(f.help, lang)}
      </p>
      <EffectiveLine knob={effective?.byPath[f.path]} />
    </div>
  )
}

export function PepModePanel({
  config,
  onChange,
  disabled,
  language,
  effective,
}: {
  config?: PepConfig
  onChange: (c: PepConfig) => void
  disabled?: boolean
  language: string
  effective?: StudioEffective
}) {
  const lang = language as Language
  const block = fullPepBlock(config)
  const running: PepStyle =
    (block.pep_style ?? '').toUpperCase() === 'A' ? 'A' : 'E' // junk runs the shipped default E
  const [tab, setTab] = useState<PepStyle | null>(null)
  const shownTab: PepStyle = tab ?? running
  const on = block.pep_enabled === true

  const emit = (b: PepConfig) => {
    if (!disabled) onChange(fullPepBlock(b))
  }
  const rows = (g: Group) =>
    PEP_FIELDS.filter((f) => f.group === g).map((f) => (
      <FieldRow
        key={f.path}
        f={f}
        block={block}
        disabled={disabled}
        lang={lang}
        effective={effective}
        onChange={emit}
      />
    ))

  return (
    <div className="space-y-4" data-testid="pep-panel">
      <p
        className="text-xs"
        style={{ color: on ? '#0ECB81' : '#848E9C' }}
        data-testid="pep-state-banner"
      >
        {on ? pt('panelOn', lang, { style: running }) : pt('panelOff', lang)}
      </p>
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
        data-testid="pep-shared-block"
      >
        <div className="text-sm font-medium" style={{ color: '#EAECEF' }}>
          {pt('sharedBlock', lang)}
        </div>
        {rows('shared')}
      </div>
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      >
        <div role="tablist" className="flex gap-2">
          {(['A', 'E'] as PepStyle[]).map((s) => (
            <button
              key={s}
              type="button"
              role="tab"
              aria-selected={shownTab === s}
              data-testid={`pep-tab-${s}`}
              onClick={() => setTab(s)}
              className="px-3 py-1.5 rounded text-xs font-semibold"
              style={{
                background: shownTab === s ? '#F0B90B' : '#1E2329',
                color: shownTab === s ? '#0B0E11' : '#EAECEF',
                border: `1px solid ${shownTab === s ? '#F0B90B' : '#2B3139'}`,
              }}
            >
              {pt(s === 'A' ? 'tabA' : 'tabE', lang)}
              {running === s ? ' ●' : ''}
            </button>
          ))}
        </div>
        <p className="text-xs mt-3 font-medium" style={{ color: '#EAECEF' }}>
          {pt(shownTab === 'A' ? 'styleAName' : 'styleEName', lang)}
        </p>
        <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
          {pt(shownTab === 'A' ? 'styleAHelp' : 'styleEHelp', lang)}
        </p>
        <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
          {pt('pdfSource', lang)}
        </p>
        <div data-testid={`pep-style-${shownTab}-block`}>
          {rows(shownTab === 'A' ? 'a' : 'e')}
        </div>
      </div>
    </div>
  )
}
