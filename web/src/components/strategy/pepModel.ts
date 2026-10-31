// PEP MODE — the Studio's model of the root "pep" block (owner order 2026-10-29).
//
// Every shipped default and every range below is the Go resolver's
// (store/pep.go: Pep*Default / Pep*Ceiling / pepInt ranges). They are typed here
// because the Studio must show a CONCRETE value in every field, and they are
// READ BACK from store/pep.go by pepModel.test.ts and by the guide's honesty
// test (pepGuide.test.ts), so a drift on either side is RED. The "eff" lines
// under each field are the server's resolved values; this module never derives
// an effective value, it only pre-fills what the bot would run if nothing were
// saved.

import type {
  PepConfig,
  PepStyleAConfig,
  PepStyleCustomConfig,
  PepStyleEConfig,
  StrategyConfig,
} from '../../types'

export type TradingMode = 'ai' | 'mentor' | 'pep'

export const PEP_STYLES = ['A', 'E', 'Custom'] as const
export type PepStyle = (typeof PEP_STYLES)[number]

/** Shipped values — the SAME numbers as store/pep.go Pep*Default. */
export const PEP_DEFAULTS = {
  style: 'E', // owner 2026-10-30 "keep default E 6"
  contracts: 6,
  boxStartCT: '08:30',
  flatTimeCT: '15:00',
  tickSize: 0.25,
  slRounding: 'outward',
  maxTradesPerDay: 1,
  feePerContractUSD: 1,
  maxBoxPoints: 0, // 0 = OFF
  signalMaxAgeMin: 2,
  a: { boxMinutes: 5, slFraction: 1, tpFraction: 1, bothEdges: 'no_trade' },
  e: {
    boxMinutes: 2,
    slFraction: 0.5,
    tpFraction: 1,
    emaPeriod: 9,
    emaTFMin: 2,
    signalSource: 'ema',
    warmupBars: 0, // 0 = auto (3 x period)
  },
  /** Custom is SEEDED from style E's PDF values, so a fresh Custom trades like E. */
  custom: {
    entryType: 'ema_cross',
    boxMinutes: 2,
    slFraction: 0.5,
    tpFraction: 1,
    bothEdges: 'no_trade',
    emaPeriod: 9,
    emaTFMin: 2,
    signalSource: 'ema',
    warmupBars: 0,
  },
} as const

/** The PDF rules styles A and E are LOCKED to, by stored path (owner ruling
 *  2026-10-30: "A or E all follow their rule, cannot change any way"). The
 *  server's resolver (store.ResolvePep) enforces them whatever is stored; the
 *  Studio shows them READ-ONLY. Only a path listed here is a locked rule.
 *  Contracts, tick, fee and signal age are NOT rules and stay editable. */
const D0 = PEP_DEFAULTS
const PDF_RULE: Record<string, number | string> = {
  'pep.pep_box_start_ct': D0.boxStartCT,
  'pep.pep_flat_time_ct': D0.flatTimeCT,
  'pep.pep_sl_rounding': D0.slRounding,
  'pep.pep_max_trades_per_day': D0.maxTradesPerDay,
  'pep.pep_max_box_points': 0, // the A / E rules have no box cap
}
export const PEP_LOCKED: Record<'A' | 'E', Record<string, number | string>> = {
  A: {
    ...PDF_RULE,
    'pep.style_a.pep_a_box_minutes': D0.a.boxMinutes,
    'pep.style_a.pep_a_sl_fraction': D0.a.slFraction,
    'pep.style_a.pep_a_tp_fraction': D0.a.tpFraction,
    'pep.style_a.pep_a_both_edges': D0.a.bothEdges,
  },
  E: {
    ...PDF_RULE,
    'pep.style_e.pep_e_box_minutes': D0.e.boxMinutes,
    'pep.style_e.pep_e_sl_fraction': D0.e.slFraction,
    'pep.style_e.pep_e_tp_fraction': D0.e.tpFraction,
    'pep.style_e.pep_e_ema_period': D0.e.emaPeriod,
    'pep.style_e.pep_e_ema_tf_min': D0.e.emaTFMin,
    'pep.style_e.pep_e_signal_source': D0.e.signalSource,
    'pep.style_e.pep_e_warmup_bars': D0.e.warmupBars,
  },
}

/** Ranges — the Go resolver's bounds, EXCEPT two UI-only floors: the server
 *  accepts any fraction / tick above 0 (store/pep.go pepPosFloat), the Studio
 *  box takes fractions of 0.05 or more and a tick of 0.01 or more so a typo like
 *  0.0005 cannot be saved as a near-zero stop. A typed number outside the range
 *  is clamped on blur. The guide's range text states both (pepGuide.test.ts). */
export const PEP_RANGES = {
  contracts: { min: 1, max: 10 }, // PepContractsCeiling is the HARD maximum
  tickSize: { min: 0.01, max: 1000 },
  maxTradesPerDay: { min: 1, max: 10 },
  feePerContractUSD: { min: 0, max: 1000 },
  maxBoxPoints: { min: 0, max: 100000 },
  signalMaxAgeMin: { min: 1, max: 30 },
  boxMinutes: { min: 1, max: 120 },
  fraction: { min: 0.05, max: 10 },
  emaPeriod: { min: 2, max: 200 },
  warmupBars: { min: 0, max: 5000 },
} as const

/** Candle sizes that divide an hour (store/pep.go pepValidTF). */
export const PEP_TF_CHOICES = [1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60]

const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/
export const isHHMM = (v: string): boolean => HHMM.test(v)

/** Which trading mode a strategy is in. pep wins only when it is the single
 *  one ON; if the (illegal) both-ON state is ever loaded it reads as 'pep'
 *  AND `conflict` is true so the selector can say so instead of hiding it. */
export function tradingModeOf(cfg: {
  mentorMode?: boolean
  pepEnabled?: boolean
}): { mode: TradingMode; conflict: boolean } {
  const m = cfg.mentorMode === true
  const p = cfg.pepEnabled === true
  if (m && p) return { mode: 'pep', conflict: true }
  if (p) return { mode: 'pep', conflict: false }
  if (m) return { mode: 'mentor', conflict: false }
  return { mode: 'ai', conflict: false }
}

/** The ONE place a pep block gets a concrete value for every leaf: the stored
 *  value where present, the shipped default otherwise. The Studio sends this
 *  whole block on every save (the server-side merge keeps absent keys, so a
 *  blank can never clear a stored number — a concrete value always can). */
export function fullPepBlock(p?: PepConfig): PepConfig {
  const a: PepStyleAConfig = p?.style_a ?? {}
  const e: PepStyleEConfig = p?.style_e ?? {}
  const c: PepStyleCustomConfig = p?.style_custom ?? {}
  const d = PEP_DEFAULTS
  return {
    pep_enabled: p?.pep_enabled === true,
    pep_style: p?.pep_style ?? d.style,
    pep_contracts: p?.pep_contracts ?? d.contracts,
    pep_box_start_ct: p?.pep_box_start_ct ?? d.boxStartCT,
    pep_flat_time_ct: p?.pep_flat_time_ct ?? d.flatTimeCT,
    pep_tick_size: p?.pep_tick_size ?? d.tickSize,
    pep_sl_rounding: p?.pep_sl_rounding ?? d.slRounding,
    pep_max_trades_per_day: p?.pep_max_trades_per_day ?? d.maxTradesPerDay,
    pep_fee_per_contract_usd:
      p?.pep_fee_per_contract_usd ?? d.feePerContractUSD,
    pep_max_box_points: p?.pep_max_box_points ?? d.maxBoxPoints,
    pep_signal_max_age_min: p?.pep_signal_max_age_min ?? d.signalMaxAgeMin,
    style_a: {
      pep_a_box_minutes: a.pep_a_box_minutes ?? d.a.boxMinutes,
      pep_a_sl_fraction: a.pep_a_sl_fraction ?? d.a.slFraction,
      pep_a_tp_fraction: a.pep_a_tp_fraction ?? d.a.tpFraction,
      pep_a_both_edges: a.pep_a_both_edges ?? d.a.bothEdges,
    },
    style_e: {
      pep_e_box_minutes: e.pep_e_box_minutes ?? d.e.boxMinutes,
      pep_e_sl_fraction: e.pep_e_sl_fraction ?? d.e.slFraction,
      pep_e_tp_fraction: e.pep_e_tp_fraction ?? d.e.tpFraction,
      pep_e_ema_period: e.pep_e_ema_period ?? d.e.emaPeriod,
      pep_e_ema_tf_min: e.pep_e_ema_tf_min ?? d.e.emaTFMin,
      pep_e_signal_source: e.pep_e_signal_source ?? d.e.signalSource,
      pep_e_warmup_bars: e.pep_e_warmup_bars ?? d.e.warmupBars,
    },
    style_custom: {
      pep_c_entry_type: c.pep_c_entry_type ?? d.custom.entryType,
      pep_c_box_minutes: c.pep_c_box_minutes ?? d.custom.boxMinutes,
      pep_c_sl_fraction: c.pep_c_sl_fraction ?? d.custom.slFraction,
      pep_c_tp_fraction: c.pep_c_tp_fraction ?? d.custom.tpFraction,
      pep_c_both_edges: c.pep_c_both_edges ?? d.custom.bothEdges,
      pep_c_ema_period: c.pep_c_ema_period ?? d.custom.emaPeriod,
      pep_c_ema_tf_min: c.pep_c_ema_tf_min ?? d.custom.emaTFMin,
      pep_c_signal_source: c.pep_c_signal_source ?? d.custom.signalSource,
      pep_c_warmup_bars: c.pep_c_warmup_bars ?? d.custom.warmupBars,
    },
  }
}

/** The style and contract count the SERVER would run for a stored block (the
 *  resolver case-folds the style and runs the shipped default E for junk, and
 *  clamps contracts into 1..10), so the confirm dialog never states a number the
 *  bot will not run (rereview m461 P3). Pure; the eff lines stay the authority. */
export function pepRunsStyle(p?: PepConfig): PepStyle {
  const v = (p?.pep_style ?? '').trim().toUpperCase()
  return v === 'A' ? 'A' : v === 'CUSTOM' ? 'Custom' : 'E'
}
export function pepRunsContracts(p?: PepConfig): number {
  const n = p?.pep_contracts ?? PEP_DEFAULTS.contracts
  if (!Number.isFinite(n)) return PEP_DEFAULTS.contracts
  return Math.min(
    PEP_RANGES.contracts.max,
    Math.max(PEP_RANGES.contracts.min, Math.trunc(n))
  )
}

/** Switches the strategy to one trading mode. mentor_mode (risk_control) and
 *  pep.pep_enabled are written TOGETHER from this single function, so the
 *  illegal both-ON combination cannot be produced by the Studio. Turning pep
 *  ON also sends the full pep block. Pure: returns a new config. */
export function applyTradingMode(
  cfg: StrategyConfig,
  mode: TradingMode
): StrategyConfig {
  const ai = cfg.ai_config
  // Touch mentor_mode only when it must change: a strategy that never carried
  // the key keeps its stored shape when the owner picks "AI".
  const wantMentor = mode === 'mentor'
  const haveMentor = ai?.risk_control?.mentor_mode === true
  const nextAI =
    ai && wantMentor !== haveMentor
      ? {
          ...ai,
          risk_control: { ...ai.risk_control, mentor_mode: wantMentor },
        }
      : ai
  const pepBase =
    mode === 'pep' || cfg.pep ? fullPepBlock(cfg.pep) : (cfg.pep as undefined)
  return {
    ...cfg,
    ai_config: nextAI,
    pep: pepBase ? { ...pepBase, pep_enabled: mode === 'pep' } : undefined,
  }
}
