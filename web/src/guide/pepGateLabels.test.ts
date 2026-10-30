// Every named refusal the pep code can return or COUNT has a gate-panel label (a
// gate without one shows its raw counter name). Scans the production sources.
// Mutant: delete a pep_* GATE_LABELS entry -> RED.
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GATE_LABELS } from '../components/plan/GateBlocksPanel'

const root = (p: string) => resolve(process.cwd(), '..', p)

// The counters fix/m458-pep-trader increments (trader/pep_admit.go,
// pep_mode.go, pep_tick.go) and the admission classes it returns. Listed here
// because that file set is not in this tree until the integration merge; the
// scan below re-derives the set from the Go source the moment it is.
const TRADER_COUNTERS = [
  'pep_ai_entries_off',
  'pep_ai_closes_off',
  'pep_not_enabled',
  'pep_no_pending_claim',
  'pep_day_unwritable',
  'pep_day_unreadable',
  'pep_positions_unreadable',
  'pep_book_unreadable',
  'pep_broker_not_flat',
  'pep_no_live_price',
  'pep_slippage',
  'pep_wire_rounding',
  'pep_bracket_wrong_side',
  'pep_rr_distorted',
  'pep_no_point_value',
  'pep_entry_failed',
  'pep_mode_conflict_mentor',
  'pep_mode_conflict_day_plan',
]

describe('pep gate labels', () => {
  it('labels each kernel/pep Code* refusal', () => {
    const src = readFileSync(root('kernel/pep/types.go'), 'utf8')
    const codes = [...src.matchAll(/Code\w+\s*=\s*"(pep_[a-z0-9_]+)"/g)].map(
      (m) => m[1]
    )
    expect(codes.length).toBeGreaterThanOrEqual(19) // non-vacuous
    for (const c of codes)
      expect(GATE_LABELS[c], `GATE_LABELS lacks ${c}`).toBeDefined()
  })
  // Mutant: delete one of the trader-wiring labels (e.g. pep_entry_failed) -> RED.
  it('labels each counter the trader wiring increments', () => {
    for (const c of TRADER_COUNTERS)
      expect(GATE_LABELS[c], `GATE_LABELS lacks ${c}`).toBeDefined()
  })
  // Active at the integration merge: every pep_* name the trader's Go code
  // counts or returns as an admission class must have a label.
  const traderDir = root('trader')
  const goFiles = existsSync(traderDir)
    ? readdirSync(traderDir).filter(
        (f) => /^pep_.*\.go$/.test(f) && !f.endsWith('_test.go')
      )
    : []
  it.skipIf(goFiles.length === 0)(
    'every pep_* gate the trader Go code counts or returns has a label',
    () => {
      const names = new Set<string>()
      for (const f of goFiles) {
        const src = readFileSync(resolve(traderDir, f), 'utf8')
        for (const m of src.matchAll(
          /(?:IncGateBlock\(at\.id,\s*|pepBlock\(|return\s+)"(pep_[a-z0-9_]+)"/g
        ))
          names.add(m[1])
      }
      expect(names.size).toBeGreaterThan(0)
      for (const n of names)
        expect(GATE_LABELS[n], `GATE_LABELS lacks ${n}`).toBeDefined()
    }
  )
  it('every pep_* label is the Pep-prefixed human sentence', () => {
    for (const [k, v] of Object.entries(GATE_LABELS))
      if (k.startsWith('pep_')) expect(v.en.startsWith('Pep')).toBe(true)
  })
})
