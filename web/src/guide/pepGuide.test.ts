// PEP GUIDE TRUTH (owner order 2026-10-29): every default on the Pep page is READ
// from store/pep.go (the one resolver's constants) and every stored path on the
// page must be a real schema path in the knob registry, so the page cannot
// describe a number the bot does not run.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'

const repo = (p: string) =>
  readFileSync(resolve(process.cwd(), '..', p), 'utf8')

const goSrc = repo('store/pep.go')
const registry = repo('store/knob_registry_pep.go')

function goConst(name: string): string {
  const m = goSrc.match(
    new RegExp(`\\b${name}\\s*=\\s*("?)([A-Za-z0-9_.:]+)\\1`)
  )
  if (!m) throw new Error(`Go constant ${name} not found — re-anchor this pin`)
  return m[2]
}

const section = GUIDE_SECTIONS.find((s) => s.id === 'pep')
const table = section?.blocks.find(
  (b) => b.kind === 'table' && b.title === 'Pep settings (default / range)'
)
const rows: string[][] = table && table.kind === 'table' ? table.rows : []
const row = (path: string) => {
  const r = rows.find((x) => x[0] === path)
  if (!r) throw new Error(`no settings row ${path}`)
  return r
}

describe('Pep guide page', () => {
  it('is a registered section and carries no knob cards (the Studio panel ships separately)', () => {
    expect(section).toBeTruthy()
    expect(section!.blocks.some((b) => b.kind === 'knobs')).toBe(false)
  })

  it('lists exactly the 22 stored pep paths the knob registry registers', () => {
    const registered = [...registry.matchAll(/live\("(pep\.[a-z_.]+)"/g)].map(
      (m) => m[1]
    )
    expect(registered).toHaveLength(22)
    expect(rows.map((r) => r[0]).sort()).toEqual([...registered].sort())
  })

  it('every default equals the Go constant', () => {
    expect(row('pep.pep_contracts')[1]).toBe(goConst('PepContractsDefault'))
    expect(row('pep.pep_contracts')[2]).toContain(
      goConst('PepContractsCeiling')
    )
    expect(row('pep.pep_box_start_ct')[1]).toBe(goConst('PepBoxStartDefault'))
    expect(row('pep.pep_flat_time_ct')[1]).toBe(goConst('PepFlatTimeDefault'))
    expect(row('pep.pep_tick_size')[1]).toBe(goConst('PepTickDefault'))
    expect(row('pep.pep_max_trades_per_day')[1]).toBe(
      goConst('PepMaxTradesDefault')
    )
    expect(Number(row('pep.pep_fee_per_contract_usd')[1])).toBe(
      Number(goConst('PepFeeDefault'))
    )
    expect(row('pep.pep_signal_max_age_min')[1]).toBe(
      goConst('PepSignalMaxAgeDefault')
    )
    expect(row('pep.style_a.pep_a_box_minutes')[1]).toBe(
      goConst('PepABoxMinutesDefault')
    )
    expect(Number(row('pep.style_a.pep_a_sl_fraction')[1])).toBe(
      Number(goConst('PepASLFractionDefault'))
    )
    expect(Number(row('pep.style_a.pep_a_tp_fraction')[1])).toBe(
      Number(goConst('PepATPFractionDefault'))
    )
    expect(row('pep.style_e.pep_e_box_minutes')[1]).toBe(
      goConst('PepEBoxMinutesDefault')
    )
    expect(Number(row('pep.style_e.pep_e_sl_fraction')[1])).toBe(
      Number(goConst('PepESLFractionDefault'))
    )
    expect(Number(row('pep.style_e.pep_e_tp_fraction')[1])).toBe(
      Number(goConst('PepETPFractionDefault'))
    )
    expect(row('pep.style_e.pep_e_ema_period')[1]).toBe(
      goConst('PepEEMAPeriodDefault')
    )
    expect(row('pep.style_e.pep_e_ema_tf_min')[1]).toBe(
      goConst('PepEEMATFDefault')
    )
    expect(row('pep.style_a.pep_a_both_edges')[1]).toBe(
      goConst('PepBothEdgesNoTrade')
    )
    expect(row('pep.style_e.pep_e_signal_source')[1]).toBe(
      goConst('PepSourceEMA')
    )
    expect(row('pep.pep_sl_rounding')[1]).toBe(goConst('PepSLRoundOutward'))
    expect(row('pep.pep_style')[1]).toBe(goConst('PepStyleA'))
  })

  it('names the mode-conflict codes the server returns', () => {
    const text = JSON.stringify(section!.blocks)
    expect(text).toContain(goConst('PepConflictMentor'))
    expect(text).toContain(goConst('PepConflictDayPlan'))
  })
})
