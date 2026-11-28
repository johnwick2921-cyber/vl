// CROSS-LANE CONTRACT: the UI's PepTruth type <-> the trader lane's Go struct
// (trader/pep_truth.go). Review round 1 found the two had been written against
// different shapes and every UI test passed on the UI's own invented JSON.
//
// Two halves:
//  1. ALWAYS: the golden JSON (the bytes Go emits) only uses keys the UI type
//     knows, and the signal words the card translates are the card's own keys.
//  2. WHEN trader/pep_truth.go is in the tree (the integration merge): every
//     key the UI reads is a json tag of the Go struct, the Go signal words equal
//     the card's table, and the Go struct sends `symbol` (the chart overlay needs
//     it). Before that merge the Go file does not exist and half 2 is skipped -
//     it goes RED at the merge, which is the point.
// Mutant: rename a key in PepTruth (UI) or drop a json tag in Go -> RED.
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { SIGNAL_KEYS } from './PepTruthCard'
import {
  GOLDEN_AFTER_FLAT,
  GOLDEN_CONFLICT,
  GOLDEN_NO_DATA,
  GOLDEN_OFF,
  GOLDEN_POSITION_OPEN,
  GOLDEN_WAITING_SIGNAL,
} from './pepTruthGolden'

// Every top-level key the UI type declares (lib/api/traders.ts PepTruth).
const UI_KEYS = [
  'enabled',
  'mode',
  'conflict',
  'symbol',
  'style',
  'entry',
  'contracts',
  'day',
  'box',
  'signal',
  'refusals',
  'trades_today',
  'max_trades_per_day',
  'position_open',
  'stop',
  'target',
  'side',
  'flat_time_ct',
  'flat_countdown_sec',
  'last_evaluated_ms',
  'clamps',
]
// Nested keys the UI reads.
const BOX_KEYS = ['high', 'low', 'size', 'drawn']
const REFUSAL_KEYS = ['code', 'severity', 'detail']

const goldens = {
  GOLDEN_WAITING_SIGNAL,
  GOLDEN_POSITION_OPEN,
  GOLDEN_NO_DATA,
  GOLDEN_AFTER_FLAT,
  GOLDEN_OFF,
  GOLDEN_CONFLICT,
}

const goFile = resolve(process.cwd(), '../trader/pep_truth.go')
const goSrc = existsSync(goFile) ? readFileSync(goFile, 'utf8') : ''

function tagsOf(struct: string): string[] {
  const m = goSrc.match(
    new RegExp(`type ${struct} struct \\{([\\s\\S]*?)\\n\\}`)
  )
  if (!m) return []
  return [...m[1].matchAll(/json:"([a-z_0-9]+)[,"]/g)].map((x) => x[1])
}

describe('pep-truth contract (UI half, always on)', () => {
  it('every golden key is a key the UI type declares', () => {
    for (const [name, json] of Object.entries(goldens)) {
      const o = JSON.parse(json) as Record<string, unknown>
      for (const k of Object.keys(o))
        expect(
          UI_KEYS,
          `${name}: server key "${k}" unknown to the UI`
        ).toContain(k)
      const box = o.box as Record<string, unknown>
      for (const k of Object.keys(box)) expect(BOX_KEYS).toContain(k)
      for (const r of (o.refusals as Record<string, unknown>[]) ?? [])
        for (const k of Object.keys(r)) expect(REFUSAL_KEYS).toContain(k)
    }
  })
  it('the goldens are non-vacuous: they cover every signal word the card knows', () => {
    const words = new Set(
      Object.values(goldens).map((j) => JSON.parse(j).signal as string)
    )
    for (const w of [
      'waiting_signal',
      'position_open',
      'no_data',
      'after_flat',
    ])
      expect(words.has(w)).toBe(true)
    expect(Object.keys(SIGNAL_KEYS).length).toBe(9)
  })
})

describe.skipIf(goSrc === '')(
  'pep-truth contract (Go half, at integration)',
  () => {
    const goTags = tagsOf('PepTruth')
    it('parsed the Go struct (non-vacuous)', () => {
      expect(goTags.length).toBeGreaterThanOrEqual(15)
    })
    it('every top-level key the UI reads is a json tag of trader.PepTruth', () => {
      for (const k of UI_KEYS)
        expect(goTags, `trader/pep_truth.go has no json:"${k}"`).toContain(k)
    })
    it('the box and refusal nested keys exist in Go', () => {
      for (const k of BOX_KEYS) expect(tagsOf('PepTruthBox')).toContain(k)
      for (const k of REFUSAL_KEYS)
        expect(tagsOf('PepTruthRefusal')).toContain(k)
    })
    it('the Go signal words are exactly the words the card translates', () => {
      const fn = goSrc.match(/func pepSignalWord[\s\S]*?\n\}/)
      expect(fn).not.toBeNull()
      const words = new Set(
        [...fn![0].matchAll(/return "([a-z_]+)"/g)].map((m) => m[1])
      )
      expect([...words].sort()).toEqual(Object.keys(SIGNAL_KEYS).sort())
    })
  }
)
