// Dispatch 102 freezes load-bearing identifiers, including their surrounding guards.
// Lock baseline advanced after the separately authorized lock-keeper wave:
// deploy/nofx-lock.sh @ ace51598 (fix/lock-defects-release-meta-halfbuilt),
// following keeper @ 97a6525cb6d10d6c8898b2d277c0fe7581872c24.
// Only its recorded hash changes; protected-file mutation checks remain enforced.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'
import baseline from './test/brand-scope-baseline.json'

function verifyScope(path: string, bytes: Buffer, expected: string) {
  if (createHash('sha256').update(bytes).digest('hex') !== expected) {
    throw new Error(`Dispatch 102 protected file changed: ${path}`)
  }
}
it.each(Object.entries(baseline))(
  'preserves %s byte for byte',
  (path, hash) => {
    expect(() =>
      verifyScope(path, readFileSync(resolve('..', path)), hash)
    ).not.toThrow()
  }
)
it('rejects a removed protected guard, instead of only checking the issuer word', () => {
  const path = 'auth/auth.go'
  const original = readFileSync(resolve('..', path), 'utf8')
  const removed = original.replace('&& token.Valid', '')
  expect(removed).not.toBe(original)
  expect(() => verifyScope(path, Buffer.from(removed), baseline[path])).toThrow(
    'protected file changed'
  )
})

it('preserves every existing TypeScript import target in changed files', async () => {
  const { execFileSync } = await import('node:child_process')
  const ts = await import('typescript')
  const base = '954f11b15f2e7615678f7d2b708c47895faebf1e'
  const git = (args: string[]) =>
    execFileSync('git', args, {
      cwd: resolve('..'),
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  const targets = (source: string) => {
    const file = ts.createSourceFile(
      'source.tsx',
      source,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX
    )
    return file.statements.flatMap((node) =>
      ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)
        ? [node.moduleSpecifier.text]
        : []
    )
  }
  for (const path of git(['diff', '--name-only', base, '--', '*.ts', '*.tsx'])
    .trim()
    .split('\n')
    .filter(Boolean)) {
    let old: string
    try {
      old = git(['show', `${base}:${path}`])
    } catch {
      continue
    }
    const current = targets(readFileSync(resolve('..', path), 'utf8'))
    for (const target of targets(old))
      expect(current, `${path}: existing import target ${target}`).toContain(
        target
      )
  }
})
