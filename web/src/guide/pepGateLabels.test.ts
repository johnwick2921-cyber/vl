// Every named refusal kernel/pep can return has a gate-panel label (a gate
// without one shows its raw counter name). Scans the production constants.
// Mutant: delete a pep_* GATE_LABELS entry -> RED.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GATE_LABELS } from '../components/plan/GateBlocksPanel'

describe('pep gate labels', () => {
  it('labels each kernel/pep Code* refusal', () => {
    const src = readFileSync(
      resolve(process.cwd(), '../kernel/pep/types.go'),
      'utf8'
    )
    const codes = [...src.matchAll(/Code\w+\s*=\s*"(pep_[a-z0-9_]+)"/g)].map(
      (m) => m[1]
    )
    expect(codes.length).toBeGreaterThanOrEqual(19) // non-vacuous
    for (const c of codes)
      expect(GATE_LABELS[c], `GATE_LABELS lacks ${c}`).toBeDefined()
  })
  it('every pep_* label is the Pep-prefixed human sentence', () => {
    for (const [k, v] of Object.entries(GATE_LABELS))
      if (k.startsWith('pep_')) expect(v.en.startsWith('Pep — ')).toBe(true)
  })
})
