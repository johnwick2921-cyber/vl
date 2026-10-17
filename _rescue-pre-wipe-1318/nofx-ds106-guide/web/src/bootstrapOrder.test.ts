import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
// W3 survivor fold (CHECK D2-WEB): the storage migration MUST run before any
// module that reads the six storage keys. main.tsx is the bootstrap entry and
// the migration is its FIRST import, as a side-effect import. ESM evaluates
// imports in order, so if the import is moved below ANY other import (e.g. the
// App import), a reader module's module-scope code runs before the migration
// and can read or write a pre-rename key the migration then deletes or copies
// stale. This test reads the PRODUCTION main.tsx and pins the import to line 0,
// so a moved import fails here without any execution.
describe('bootstrap order (W3)', () => {
  it('imports the storage migration as the very first line of main.tsx', () => {
    const main = readFileSync('src/main.tsx', 'utf8')
    const lines = main.split('\n')
    const migrationIndex = lines.findIndex(
      (line) => line.trim() === "import './lib/storageMigration'"
    )
    expect(migrationIndex).toBe(0)
  })
})
