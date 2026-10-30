// PEP GUIDE TRUTH (owner order 2026-10-29), T16-style: the Pep page may not
// describe a number the bot does not run or a control the Studio does not
// render. Every expectation is READ from the code that decides it — the Go
// resolver (store/pep.go), the knob registry, the Studio panel's PEP_FIELDS and
// the TradingModeSelector source — never typed twice.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'
import { PEP_FIELDS } from '../components/strategy/PepModePanel'
import { PEP_DEFAULTS, PEP_RANGES } from '../components/strategy/pepModel'
import { pepStrings } from '../i18n/pep-translations'
import type { KnobSpec } from './types'

const repo = (p: string) =>
  readFileSync(resolve(process.cwd(), '..', p), 'utf8')
const web = (p: string) => readFileSync(resolve(process.cwd(), p), 'utf8')

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
const cards: KnobSpec[] =
  section?.blocks.flatMap((b) => (b.kind === 'knobs' ? b.knobs : [])) ?? []
const card = (path: string): KnobSpec => {
  const k = cards.find((x) => x.settingId === path)
  if (!k) throw new Error(`no pep card for ${path}`)
  return k
}
const registered = [...registry.matchAll(/live\("(pep\.[a-z_.]+)"/g)].map(
  (m) => m[1]
)

describe('Pep guide page: cards', () => {
  it('is a registered section with one card per registered stored path (22)', () => {
    expect(section).toBeTruthy()
    expect(registered).toHaveLength(22)
    expect(cards.map((c) => c.settingId).sort()).toEqual([...registered].sort())
    expect(new Set(cards.map((c) => c.settingId)).size).toBe(cards.length)
  })

  it('every card is live exactly when the registry says live', () => {
    for (const c of cards) {
      expect(c.live, c.settingId).toBe(true)
      expect(registry, c.settingId).toContain(`live("${c.settingId}"`)
    }
  })

  it('every card cites its consumer and the SIM/PDF source in words, not blanks', () => {
    for (const c of cards) {
      expect(c.consumer).toMatch(/ResolvePep|PepEnabled|pep-backtest/)
      expect(c.recommended.length).toBeGreaterThan(20)
    }
  })
})

describe('Pep guide page: cards name the Studio control that exists (T16)', () => {
  // Mutant: drop one PEP_FIELDS entry (the control vanishes but the card still
  // names it) -> RED; or add a field with no card -> RED.
  it('every non-switch card has a PepModePanel field with the SAME path, label and tab', () => {
    const fields = new Map(PEP_FIELDS.map((f) => [f.path, f]))
    for (const c of cards) {
      if (c.settingId === 'pep.pep_enabled') continue
      const f = fields.get(c.settingId!)
      expect(f, `${c.settingId} has no Studio control`).toBeDefined()
      expect(c.where).not.toMatch(/No Studio control/)
      expect(c.where).toContain('Pep mode accordion')
      expect(c.where).toContain(
        f!.group === 'a'
          ? 'Style A tab'
          : f!.group === 'e'
            ? 'Style E tab'
            : 'Shared by both styles'
      )
      // the on-screen text is the panel's own English label
      const onScreen = pepStrings[f!.label].en
      expect(c.label.startsWith(onScreen), `${c.label} vs ${onScreen}`).toBe(
        true
      )
    }
    expect(PEP_FIELDS).toHaveLength(21)
  })

  it('the master-switch card names the real Trading mode selector control', () => {
    const k = card('pep.pep_enabled')
    expect(k.where).toContain('Trading mode')
    const sel = web('src/components/strategy/TradingModeSelector.tsx')
    expect(sel).toContain('data-testid={`trading-mode-${m}`}')
    expect(sel).toContain("'pep'")
    expect(pepStrings.modeTitle.en).toBe('Trading mode')
    expect(pepStrings.modePep.en).toBe('Pep')
  })
})

describe('Pep guide page: numbers equal the code', () => {
  const row = (p: string) => card(p).systemDefault
  it('every default is the Go constant AND the Studio pre-fill', () => {
    expect(row('pep.pep_contracts')).toBe(goConst('PepContractsDefault'))
    expect(row('pep.pep_contracts')).toBe(String(PEP_DEFAULTS.contracts))
    expect(card('pep.pep_contracts').range).toContain(
      `hard ceiling ${goConst('PepContractsCeiling')}`
    )
    expect(row('pep.pep_box_start_ct')).toBe(goConst('PepBoxStartDefault'))
    expect(row('pep.pep_flat_time_ct')).toBe(goConst('PepFlatTimeDefault'))
    expect(row('pep.pep_tick_size')).toBe(goConst('PepTickDefault'))
    expect(row('pep.pep_max_trades_per_day')).toBe(
      goConst('PepMaxTradesDefault')
    )
    expect(Number(row('pep.pep_fee_per_contract_usd'))).toBe(
      Number(goConst('PepFeeDefault'))
    )
    expect(row('pep.pep_signal_max_age_min')).toBe(
      goConst('PepSignalMaxAgeDefault')
    )
    expect(row('pep.style_a.pep_a_box_minutes')).toBe(
      goConst('PepABoxMinutesDefault')
    )
    expect(Number(row('pep.style_a.pep_a_sl_fraction'))).toBe(
      Number(goConst('PepASLFractionDefault'))
    )
    expect(Number(row('pep.style_a.pep_a_tp_fraction'))).toBe(
      Number(goConst('PepATPFractionDefault'))
    )
    expect(row('pep.style_e.pep_e_box_minutes')).toBe(
      goConst('PepEBoxMinutesDefault')
    )
    expect(Number(row('pep.style_e.pep_e_sl_fraction'))).toBe(
      Number(goConst('PepESLFractionDefault'))
    )
    expect(Number(row('pep.style_e.pep_e_tp_fraction'))).toBe(
      Number(goConst('PepETPFractionDefault'))
    )
    expect(row('pep.style_e.pep_e_ema_period')).toBe(
      goConst('PepEEMAPeriodDefault')
    )
    expect(row('pep.style_e.pep_e_ema_tf_min')).toBe(
      goConst('PepEEMATFDefault')
    )
    expect(row('pep.style_a.pep_a_both_edges')).toBe(
      goConst('PepBothEdgesNoTrade')
    )
    expect(row('pep.style_e.pep_e_signal_source')).toBe(goConst('PepSourceEMA'))
    expect(row('pep.pep_sl_rounding')).toBe(goConst('PepSLRoundOutward'))
    expect(row('pep.pep_style')).toBe(goConst('PepStyleDefault'))
    expect(row('pep.pep_enabled')).toBe('off')
  })

  // OWNER RULING 2026-10-30 ("keep default E 6"): the shipped defaults are
  // style E and 6 contracts, with a 2 minute E box and a 5 minute A box, in
  // EVERY layer. Typed as literals on purpose: a drift in the Go constant, the
  // Studio pre-fill or the guide card is RED here.
  // Mutant: set any of the four back (A / 3 / box 5 for E) -> RED.
  it('owner defaults: style E, 6 contracts, E box 2 min, A box 5 min (guide card, Studio pre-fill, Go)', () => {
    expect(row('pep.pep_style')).toBe('E')
    expect(row('pep.pep_contracts')).toBe('6')
    expect(row('pep.style_e.pep_e_box_minutes')).toBe('2')
    expect(row('pep.style_a.pep_a_box_minutes')).toBe('5')
    expect(goConst('PepStyleDefault')).toBe('E')
    expect(goConst('PepContractsDefault')).toBe('6')
    expect(goConst('PepEBoxMinutesDefault')).toBe('2')
    expect(goConst('PepABoxMinutesDefault')).toBe('5')
    expect(PEP_DEFAULTS.style).toBe('E')
    expect(PEP_DEFAULTS.contracts).toBe(6)
    expect(PEP_DEFAULTS.e.boxMinutes).toBe(2)
    expect(PEP_DEFAULTS.a.boxMinutes).toBe(5)
  })

  // Mutant: change a card's range ceiling (e.g. signal age "1 to 30" -> "1 to 60") -> RED.
  it('every range text carries the Go bound the resolver clamps to', () => {
    const r = (p: string) => card(p).range
    expect(r('pep.pep_max_trades_per_day')).toContain(
      `1 to ${goConst('PepMaxTradesCeiling')}`
    )
    expect(r('pep.pep_signal_max_age_min')).toContain(
      `1 to ${goConst('PepSignalMaxAgeCeiling')}`
    )
    expect(r('pep.style_a.pep_a_box_minutes')).toContain(
      `1 to ${goConst('PepBoxMinutesCeiling')}`
    )
    expect(r('pep.style_e.pep_e_box_minutes')).toContain(
      `1 to ${goConst('PepBoxMinutesCeiling')}`
    )
    expect(r('pep.style_a.pep_a_sl_fraction')).toContain(
      `at most ${Number(goConst('PepFractionCeiling'))}`
    )
    expect(r('pep.style_e.pep_e_tp_fraction')).toContain(
      `at most ${Number(goConst('PepFractionCeiling'))}`
    )
    expect(r('pep.style_e.pep_e_ema_period')).toBe(
      `${goConst('PepEMAPeriodFloor')} to ${goConst('PepEMAPeriodCeiling')}`
    )
    expect(r('pep.style_e.pep_e_warmup_bars')).toContain(
      `0 to ${goConst('PepWarmupCeiling')}`
    )
    // The two UI-only floors are STATED on the cards, not hidden (review round
    // 1, defect 3): the server takes any value above 0, the Studio box does not.
    for (const id of [
      'pep.style_a.pep_a_sl_fraction',
      'pep.style_a.pep_a_tp_fraction',
      'pep.style_e.pep_e_sl_fraction',
      'pep.style_e.pep_e_tp_fraction',
    ])
      expect(r(id)).toContain(`takes ${PEP_RANGES.fraction.min} or more`)
    expect(r('pep.pep_tick_size')).toContain(
      `takes ${PEP_RANGES.tickSize.min} or more`
    )
    expect(r('pep.pep_tick_size')).toContain('accepts any value above 0')
    // and the Studio clamps to the same bounds the guide states
    expect(PEP_RANGES.signalMaxAgeMin.max).toBe(
      Number(goConst('PepSignalMaxAgeCeiling'))
    )
    expect(PEP_RANGES.contracts.max).toBe(
      Number(goConst('PepContractsCeiling'))
    )
  })

  // Mutant: delete the Day Plan warning key (or the guide sentence) -> RED.
  it('the guide claim about the Day Plan block is backed by the shipped warning', () => {
    const text = JSON.stringify(section!.blocks)
    expect(text).toContain('the Day Plan block shows a warning')
    expect(pepStrings.dayPlanPepBlocks.en).toMatch(/cannot be turned on/)
    expect(web('src/components/strategy/DayPlanEditor.tsx')).toContain(
      'dayPlanPepBlocks'
    )
    expect(text).toContain('Mode conflict')
    expect(pepStrings.chipConflict.en).toBe('Mode conflict')
  })

  it('the fee card says backtest/reporting only, and the Go comment says the same', () => {
    expect(goSrc).toMatch(/BACKTEST \/ REPORTING ONLY/)
    expect(card('pep.pep_fee_per_contract_usd').what).toMatch(
      /backtest and the reports ONLY/
    )
  })

  it('style boxes: A is 5 minutes and E is 2, on the cards and on the A/E cards', () => {
    const text = JSON.stringify(section!.blocks)
    expect(text).toContain('first 5 minutes from 08:30 CT')
    expect(text).toContain('first 2 minutes from 08:30 CT')
    expect(card('pep.style_a.pep_a_box_minutes').systemDefault).toBe(
      goConst('PepABoxMinutesDefault')
    )
    expect(card('pep.style_e.pep_e_box_minutes').systemDefault).toBe(
      goConst('PepEBoxMinutesDefault')
    )
  })

  it('cites the PDF with its page numbers on the A and E strategy cards', () => {
    const cardsBlock = section!.blocks.find((b) => b.kind === 'cards')
    expect(cardsBlock && cardsBlock.kind === 'cards').toBe(true)
    if (!cardsBlock || cardsBlock.kind !== 'cards') return
    const a = cardsBlock.cards.find((c) => c.title.startsWith('Style A'))
    const e = cardsBlock.cards.find((c) => c.title.startsWith('Style E'))
    expect(a?.tag).toMatch(/PDF p\.1/)
    expect(e?.tag).toMatch(/PDF p\.1/)
    // the PDF results quoted on the cards
    expect(a?.body).toContain('$17,595.90')
    expect(e?.body).toContain('$10,070.10')
  })

  it('names the mode-conflict codes the server returns', () => {
    const text = JSON.stringify(section!.blocks)
    expect(text).toContain(goConst('PepConflictMentor'))
    expect(text).toContain(goConst('PepConflictDayPlan'))
  })
})

describe('mentor-only notes say "mentor or pep" where it is true', () => {
  const text = (id: string) =>
    JSON.stringify(GUIDE_SECTIONS.find((s) => s.id === id)?.blocks)
  // Mutant: revert any one of these sentences -> RED.
  it('the settings cards name pep beside mentor (confidence, break-even, trailing, max contracts)', () => {
    const s = text('settings')
    expect(s).toContain(
      'to pep mode (pep entries come from the two fixed PDF rules and carry no confidence)'
    )
    expect(s).toContain('not for mentor or pep entries or armed plan scenarios')
    expect(s).toContain('A pep trade is never moved by it either')
    expect(s).toContain('nor to a pep trade (pep has no trailing')
    expect(s).toContain('PEP MODE IS NOT CLAMPED BY THIS EITHER')
  })
  it('welcome, the EOD-flat FAQ and the glossary mention pep', () => {
    expect(text('welcome')).toContain('PEP MODE (another per-strategy switch')
    expect(text('faq')).toContain('A pep trader does not use this 14:45 flat')
    expect(text('glossary')).toContain('Pep mode')
  })
  it('the pep contracts claim matches the Go ceiling the settings card quotes', () => {
    expect(text('settings')).toContain(
      `default ${goConst('PepContractsDefault')}, hard ceiling ${goConst('PepContractsCeiling')}`
    )
  })
})
