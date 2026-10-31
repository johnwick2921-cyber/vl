import { useState } from 'react'

import { pt, type PepKey } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { StudioEffective } from '../../lib/api/strategyEffective'
import type { PepConfig } from '../../types'
import { EffectiveLine } from './EffectiveChip'
import {
  PEP_DEFAULTS,
  PEP_LOCKED,
  PEP_RANGES,
  PEP_TF_CHOICES,
  fullPepBlock,
  isHHMM,
  type PepStyle,
} from './pepModel'
import { ClampedNumberInput } from './RiskControlEditor'

// PEP MODE settings panel (owner order 2026-10-29; m461 owner ruling 2026-10-30:
// "A or E all follow their rule, cannot change any way, they add custom").
// Layout: a state banner, the SHARED block (style picker A / E / Custom,
// contracts, tick, fee, signal age - editable in EVERY style), then the RULES of
// the chosen style. Styles A and E show their PDF rule values READ-ONLY with a
// "locked to the PDF" note (the server ignores any stored number for them);
// only Custom shows editable inputs.
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
type Group = 'shared' | 'rule' | 'a' | 'e' | 'c'

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
  /** Custom rows only: shown when this says so (entry type picks which). */
  visible?: (b: PepConfig) => boolean
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

/** Rule leaves at the ROOT of the block: the A / E rules own them (locked to the
 *  PDF); only the Custom style reads what is stored. */
const rule = (
  leaf: keyof PepConfig,
  kind: Kind,
  label: PepKey,
  help: PepKey,
  extra: Partial<PepField> = {}
): PepField => ({ ...shared(leaf, kind, label, help, extra), group: 'rule' })

const styleC = (
  leaf: keyof NonNullable<PepConfig['style_custom']>,
  kind: Kind,
  label: PepKey,
  help: PepKey,
  extra: Partial<PepField> = {}
): PepField => ({
  path: `pep.style_custom.${leaf}`,
  leaf,
  group: 'c',
  kind,
  label,
  help,
  get: (b) => b.style_custom?.[leaf] as number | string | undefined,
  set: (b, v) => ({ ...b, style_custom: { ...b.style_custom, [leaf]: v } }),
  ...extra,
})

const isBreakout = (b: PepConfig) =>
  b.style_custom?.pep_c_entry_type === 'breakout'
const isCross = (b: PepConfig) => !isBreakout(b)

export const PEP_FIELDS: PepField[] = [
  shared('pep_style', 'enum', 'runsStyle', 'runsStyleHelp', {
    // the server case-folds the style; show the canonical name
    get: (b) => {
      const v = (b.pep_style ?? '').trim().toUpperCase()
      return v === 'CUSTOM' ? 'Custom' : v || undefined
    },
    options: [
      { value: 'A', label: 'styleAName' },
      { value: 'E', label: 'styleEName' },
      { value: 'Custom', label: 'styleCustomName' },
    ],
    fallback: D.style,
  }),
  shared('pep_contracts', 'int', 'fContracts', 'fContractsHelp', {
    ...R.contracts,
    fallback: D.contracts,
  }),
  rule('pep_box_start_ct', 'time', 'fBoxStart', 'fBoxStartHelp', {
    fallback: D.boxStartCT,
  }),
  rule('pep_flat_time_ct', 'time', 'fFlatTime', 'fFlatTimeHelp', {
    fallback: D.flatTimeCT,
  }),
  shared('pep_tick_size', 'float', 'fTick', 'fTickHelp', {
    ...R.tickSize,
    step: 0.25,
    fallback: D.tickSize,
  }),
  rule('pep_sl_rounding', 'enum', 'fSlRounding', 'fSlRoundingHelp', {
    options: [
      { value: 'outward', label: 'optOutward' },
      { value: 'nearest', label: 'optNearest' },
    ],
    fallback: D.slRounding,
  }),
  rule('pep_max_trades_per_day', 'int', 'fMaxTrades', 'fMaxTradesHelp', {
    ...R.maxTradesPerDay,
    fallback: D.maxTradesPerDay,
  }),
  shared('pep_fee_per_contract_usd', 'float', 'fFee', 'fFeeHelp', {
    ...R.feePerContractUSD,
    step: 0.5,
    fallback: D.feePerContractUSD,
  }),
  rule('pep_max_box_points', 'float', 'fMaxBox', 'fMaxBoxHelp', {
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

  styleC('pep_c_entry_type', 'enum', 'fEntryType', 'fEntryTypeHelp', {
    options: [
      { value: 'ema_cross', label: 'optEmaCross' },
      { value: 'breakout', label: 'optBreakout' },
    ],
    fallback: D.custom.entryType,
  }),
  styleC('pep_c_box_minutes', 'int', 'fBoxMinutes', 'fCBoxMinutesHelp', {
    ...R.boxMinutes,
    fallback: D.custom.boxMinutes,
  }),
  styleC('pep_c_sl_fraction', 'float', 'fSlFraction', 'fCSlHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.custom.slFraction,
  }),
  styleC('pep_c_tp_fraction', 'float', 'fTpFraction', 'fCTpHelp', {
    ...R.fraction,
    step: 0.05,
    fallback: D.custom.tpFraction,
  }),
  styleC('pep_c_both_edges', 'enum', 'fBothEdges', 'fBothEdgesHelp', {
    options: [
      { value: 'no_trade', label: 'optNoTrade' },
      { value: 'nearest_open', label: 'optNearestOpen' },
    ],
    fallback: D.custom.bothEdges,
    visible: isBreakout,
  }),
  styleC('pep_c_ema_period', 'int', 'fEmaPeriod', 'fEmaPeriodHelp', {
    ...R.emaPeriod,
    fallback: D.custom.emaPeriod,
    visible: isCross,
  }),
  styleC('pep_c_ema_tf_min', 'tf', 'fEmaTf', 'fEmaTfHelp', {
    fallback: D.custom.emaTFMin,
    visible: isCross,
  }),
  styleC('pep_c_signal_source', 'enum', 'fSignalSource', 'fSignalSourceHelp', {
    options: [
      { value: 'ema', label: 'optEma' },
      { value: 'close', label: 'optClose' },
    ],
    fallback: D.custom.signalSource,
    visible: isCross,
  }),
  styleC('pep_c_warmup_bars', 'int', 'fWarmup', 'fWarmupHelp', {
    ...R.warmupBars,
    fallback: D.custom.warmupBars,
    visible: isCross,
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
  disabled: disabledProp,
  lockedValue,
  lang,
  effective,
  onChange,
}: {
  f: PepField
  block: PepConfig
  disabled?: boolean
  /** Set for styles A / E: the PDF's value, shown read-only. */
  lockedValue?: number | string
  lang: Language
  effective?: StudioEffective
  onChange: (b: PepConfig) => void
}) {
  const [timeDraft, setTimeDraft] = useState<string | null>(null)
  const locked = lockedValue !== undefined
  const disabled = disabledProp || locked
  const cur = locked ? lockedValue : (f.get(block) ?? f.fallback)
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
        {locked && (
          <span
            className="text-[10px] ml-2 px-1.5 py-0.5 rounded"
            style={{ background: '#2B3139', color: '#F0B90B' }}
            data-testid={`${testId}-locked`}
          >
            {pt('lockedBadge', lang)}
          </span>
        )}
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
  const v = (block.pep_style ?? '').trim().toUpperCase()
  // junk runs the shipped default E (the server's resolver does the same)
  const running: PepStyle = v === 'A' ? 'A' : v === 'CUSTOM' ? 'Custom' : 'E'
  const on = block.pep_enabled === true

  const emit = (b: PepConfig) => {
    if (!disabled) onChange(fullPepBlock(b))
  }
  const row = (f: PepField, lockedValue?: number | string) => (
    <FieldRow
      key={f.path}
      f={f}
      block={block}
      disabled={disabled}
      lockedValue={lockedValue}
      lang={lang}
      effective={effective}
      onChange={emit}
    />
  )
  const sharedRows = PEP_FIELDS.filter((f) => f.group === 'shared').map((f) =>
    row(f)
  )
  // A / E: the rule rows with the PDF value, read-only. Custom: editable rows.
  const styleGroup = running === 'A' ? 'a' : running === 'E' ? 'e' : 'c'
  const ruleRows = PEP_FIELDS.filter(
    (f) => f.group === 'rule' || f.group === styleGroup
  )
    .filter((f) => running !== 'Custom' || !f.visible || f.visible(block))
    .map((f) => {
      if (running === 'Custom') return row(f)
      // never editable under A / E: a missing table entry falls back to the PDF default
      return row(
        f,
        PEP_LOCKED[running][f.path] ?? (f.fallback as number | string)
      )
    })

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
        {sharedRows}
      </div>
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      >
        <div className="text-sm font-medium" style={{ color: '#EAECEF' }}>
          {pt('rulesBlock', lang)}
        </div>
        <p className="text-xs mt-3 font-medium" style={{ color: '#EAECEF' }}>
          {pt(
            running === 'A'
              ? 'styleAName'
              : running === 'E'
                ? 'styleEName'
                : 'styleCustomName',
            lang
          )}
        </p>
        <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
          {pt(
            running === 'A'
              ? 'styleAHelp'
              : running === 'E'
                ? 'styleEHelp'
                : 'styleCustomHelp',
            lang
          )}
        </p>
        {running !== 'Custom' && (
          <p
            className="text-xs mt-1 font-medium"
            style={{ color: '#F0B90B' }}
            data-testid="pep-locked-note"
          >
            {pt('lockedNote', lang)}
          </p>
        )}
        <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
          {pt('pdfSource', lang)}
        </p>
        <div data-testid={`pep-style-${running}-block`}>{ruleRows}</div>
      </div>
    </div>
  )
}
