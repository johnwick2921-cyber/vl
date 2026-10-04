import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
// R5 rename (2026-10-02): the pre-rename storage migration is REMOVED —
// the compat layer is gone and nothing needs to run before the readers any
// more. This test pins the ABSENCE of the old side-effect import, so nobody
// quietly re-adds a pre-rename reader path.
describe('bootstrap order (R5)', () => {
  it('main.tsx no longer imports the storage migration', () => {
    const main = readFileSync('src/main.tsx', 'utf8')
    expect(main).not.toContain("import './lib/storageMigration'")
  })
})
