// PEP MODEL pins. The shipped defaults and ranges the Studio pre-fills are READ
// from store/pep.go (the one resolver) — never typed twice — so a Go change
// without the Studio (or the reverse) is RED. Mutants named per test.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { StrategyConfig } from '../../types'
import {
  PEP_DEFAULTS,
  PEP_RANGES,
  applyTradingMode,
  fullPepBlock,
  tradingModeOf,
  type TradingMode,
} from './pepModel'

const goSrc = readFileSync(resolve(process.cwd(), '../store/pep.go'), 'utf8')
const registry = readFileSync(
  resolve(process.cwd(), '../store/knob_registry_pep.go'),
  'utf8'
)
function goConst(name: string): string {
  const m = goSrc.match(
    new RegExp(`\\b${name}\\s*=\\s*("?)([A-Za-z0-9_.:]+)\\1`)
  )
  if (!m) throw new Error(`Go constant ${name} not found — re-anchor this pin`)
  return m[2]
}

describe('pepModel defaults equal store/pep.go', () => {
  // Mutant: change any PEP_DEFAULTS number (e.g. contracts 3 -> 6) -> RED.
  it('every shipped default is the Go constant', () => {
    const d = PEP_DEFAULTS
    expect(String(d.contracts)).toBe(goConst('PepContractsDefault'))
    expect(d.boxStartCT).toBe(goConst('PepBoxStartDefault'))
    expect(d.flatTimeCT).toBe(goConst('PepFlatTimeDefault'))
    expect(d.tickSize).toBe(Number(goConst('PepTickDefault')))
    expect(String(d.maxTradesPerDay)).toBe(goConst('PepMaxTradesDefault'))
    expect(d.feePerContractUSD).toBe(Number(goConst('PepFeeDefault')))
    expect(String(d.signalMaxAgeMin)).toBe(goConst('PepSignalMaxAgeDefault'))
    expect(String(d.a.boxMinutes)).toBe(goConst('PepABoxMinutesDefault'))
    expect(d.a.slFraction).toBe(Number(goConst('PepASLFractionDefault')))
    expect(d.a.tpFraction).toBe(Number(goConst('PepATPFractionDefault')))
    expect(String(d.e.boxMinutes)).toBe(goConst('PepEBoxMinutesDefault'))
    expect(d.e.slFraction).toBe(Number(goConst('PepESLFractionDefault')))
    expect(d.e.tpFraction).toBe(Number(goConst('PepETPFractionDefault')))
    expect(String(d.e.emaPeriod)).toBe(goConst('PepEEMAPeriodDefault'))
    expect(String(d.e.emaTFMin)).toBe(goConst('PepEEMATFDefault'))
    expect(d.style).toBe(goConst('PepStyleA'))
    expect(d.slRounding).toBe(goConst('PepSLRoundOutward'))
    expect(d.a.bothEdges).toBe(goConst('PepBothEdgesNoTrade'))
    expect(d.e.signalSource).toBe(goConst('PepSourceEMA'))
  })

  // Mutant: raise PEP_RANGES.contracts.max to 12 (a contract number the server
  // would clamp to 10) -> RED.
  it('every range ceiling/floor is the Go bound', () => {
    expect(PEP_RANGES.contracts.max).toBe(
      Number(goConst('PepContractsCeiling'))
    )
    expect(PEP_RANGES.maxTradesPerDay.max).toBe(
      Number(goConst('PepMaxTradesCeiling'))
    )
    expect(PEP_RANGES.signalMaxAgeMin.max).toBe(
      Number(goConst('PepSignalMaxAgeCeiling'))
    )
    expect(PEP_RANGES.boxMinutes.max).toBe(
      Number(goConst('PepBoxMinutesCeiling'))
    )
    expect(PEP_RANGES.fraction.max).toBe(Number(goConst('PepFractionCeiling')))
    expect(PEP_RANGES.emaPeriod.min).toBe(Number(goConst('PepEMAPeriodFloor')))
    expect(PEP_RANGES.emaPeriod.max).toBe(
      Number(goConst('PepEMAPeriodCeiling'))
    )
    expect(PEP_RANGES.warmupBars.max).toBe(Number(goConst('PepWarmupCeiling')))
  })
})

describe('fullPepBlock', () => {
  // Mutant: drop one leaf from fullPepBlock -> RED (the server-side merge keeps
  // absent keys, so a missing leaf would let a stale stored number survive).
  it('carries a concrete value for EVERY registered pep leaf', () => {
    const registered = [...registry.matchAll(/live\("pep\.([a-z_.]+)"/g)].map(
      (m) => m[1]
    )
    expect(registered).toHaveLength(22)
    const b = fullPepBlock(undefined) as unknown as Record<string, unknown>
    const have: string[] = []
    for (const [k, v] of Object.entries(b)) {
      if (k === 'style_a' || k === 'style_e') {
        for (const [kk, vv] of Object.entries(v as Record<string, unknown>)) {
          expect(vv, k + '.' + kk).not.toBeUndefined()
          have.push(`${k}.${kk}`)
        }
      } else {
        expect(v, k).not.toBeUndefined()
        have.push(k)
      }
    }
    expect(have.sort()).toEqual([...registered].sort())
  })

  it('keeps a stored value, including a legal 0, and fills only the absent ones', () => {
    const b = fullPepBlock({
      pep_contracts: 6,
      pep_fee_per_contract_usd: 0,
      style_e: { pep_e_warmup_bars: 0, pep_e_ema_period: 12 },
    })
    expect(b.pep_contracts).toBe(6)
    expect(b.pep_fee_per_contract_usd).toBe(0)
    expect(b.style_e?.pep_e_ema_period).toBe(12)
    expect(b.style_e?.pep_e_warmup_bars).toBe(0)
    expect(b.pep_flat_time_ct).toBe('15:00')
    expect(b.style_a?.pep_a_box_minutes).toBe(5)
  })

  it('switching style never loses the other style numbers', () => {
    const b = fullPepBlock({
      pep_style: 'E',
      style_a: { pep_a_sl_fraction: 0.8 },
    })
    expect(b.style_a?.pep_a_sl_fraction).toBe(0.8)
  })
})

const base = (mentor: boolean, pep?: boolean): StrategyConfig => ({
  strategy_type: 'ai_trading',
  ai_config: {
    risk_control: { mentor_mode: mentor },
  } as unknown as StrategyConfig['ai_config'],
  pep: pep === undefined ? undefined : { pep_enabled: pep },
})

describe('applyTradingMode: both-ON is unreachable', () => {
  const MODES: TradingMode[] = ['ai', 'mentor', 'pep']
  const STARTS: [string, StrategyConfig][] = [
    ['ai', base(false)],
    ['mentor', base(true)],
    ['pep', base(false, true)],
    ['illegal both-on (loaded)', base(true, true)],
  ]
  // Mutant: make applyTradingMode('pep') leave mentor_mode untouched -> RED
  // (the 'mentor'->'pep' and both-on starts keep mentor ON).
  for (const [name, start] of STARTS) {
    for (const m of MODES) {
      it(`${name} -> ${m} leaves exactly one mode`, () => {
        const next = applyTradingMode(start, m)
        const mentorOn = next.ai_config?.risk_control?.mentor_mode === true
        const pepOn = next.pep?.pep_enabled === true
        expect(mentorOn && pepOn).toBe(false)
        expect(mentorOn).toBe(m === 'mentor')
        expect(pepOn).toBe(m === 'pep')
        expect(
          tradingModeOf({ mentorMode: mentorOn, pepEnabled: pepOn }).mode
        ).toBe(m)
      })
    }
  }

  it('choosing pep sends the full block; choosing AI on a never-pep strategy adds no pep key and no mentor_mode key', () => {
    const toPep = applyTradingMode(base(false), 'pep')
    expect(Object.keys(toPep.pep ?? {})).toContain('pep_contracts')
    expect(toPep.pep?.pep_enabled).toBe(true)
    const plain: StrategyConfig = {
      strategy_type: 'ai_trading',
      ai_config: { risk_control: {} } as unknown as StrategyConfig['ai_config'],
    }
    const stillAI = applyTradingMode(plain, 'ai')
    expect('pep' in stillAI && stillAI.pep !== undefined).toBe(false)
    expect('mentor_mode' in (stillAI.ai_config?.risk_control ?? {})).toBe(false)
  })

  it('tradingModeOf names the illegal both-on state', () => {
    expect(tradingModeOf({ mentorMode: true, pepEnabled: true })).toEqual({
      mode: 'pep',
      conflict: true,
    })
  })
})
