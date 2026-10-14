// UI-GUIDE-TRUTH LF-1 (release #22): the public, un-linked /faq route shipped
// the inherited NoFx crypto/Docker FAQ, whose "database locked" answer told the
// reader to run `pkill vl` (under systemd a clean SIGTERM exit is NOT
// relaunched) and `rm -f data/data.db-wal data/data.db-shm` (deletes committed,
// un-checkpointed writes). Mutant: restore either string or the FAQPage route
// → RED.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { translations } from '../i18n/translations'

describe('legacy /faq', () => {
  it('no language tells the owner to pkill the bot or delete the WAL', () => {
    const all = JSON.stringify(translations)
    expect(all).not.toMatch(/or \\?"pkill vl\\?"/)
    expect(all).not.toMatch(/rm -f data\/data\.db-wal/)
    for (const lang of Object.keys(
      translations
    ) as (keyof typeof translations)[]) {
      const a = (translations[lang] as Record<string, string>)
        .faqDatabaseLockedAnswer
      if (a === undefined) continue
      expect(a, `${lang} answer`).toMatch(/pkill/) // names it only to forbid it
      expect(a, `${lang} answer`).toMatch(/RESTORE\.md/)
    }
  })

  it('/faq redirects to the in-app Guide instead of rendering the inherited FAQ', () => {
    const src = readFileSync(
      resolve(process.cwd(), 'src/router/AppRoutes.tsx'),
      'utf8'
    )
    expect(src).toMatch(
      /<Route\s+path=\{ROUTES\.faq\}\s+element=\{<Navigate to=\{ROUTES\.guide\} replace \/>\}\s*\/>/
    )
    expect(src).not.toMatch(/<FAQPage \/>/)
  })
})
