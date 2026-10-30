// Label parity: every pep string has non-empty en, zh AND id, and the {param}
// placeholders of a string are the same set in all three languages (a missing
// {n} in zh would print a raw "{n}" or silently drop the number).
import { describe, expect, it } from 'vitest'
import { pepStrings, pt, type PepKey } from './pep-translations'

const params = (s: string) =>
  [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()

describe('pep translations', () => {
  const keys = Object.keys(pepStrings) as PepKey[]
  it('is non-trivial', () => {
    expect(keys.length).toBeGreaterThan(80)
  })
  // Mutant: blank one zh or id string -> RED.
  it('every key has non-empty en, zh and id', () => {
    for (const k of keys) {
      for (const l of ['en', 'zh', 'id'] as const) {
        expect(pepStrings[k][l].trim(), `${k}.${l}`).not.toBe('')
      }
    }
  })
  it('zh and id are actually translated, not an English copy (except proper nouns)', () => {
    const same = keys.filter(
      (k) =>
        pepStrings[k].zh === pepStrings[k].en &&
        /[a-z]{4,}/.test(pepStrings[k].en)
    )
    // Allowed: option tokens that are literal stored values, and the chip
    // "Pep {style}" (a proper noun plus a letter).
    expect(same.filter((k) => !k.startsWith('opt') && k !== 'chipPep')).toEqual(
      []
    )
  })
  it('placeholders match across languages', () => {
    for (const k of keys) {
      const e = params(pepStrings[k].en)
      expect(params(pepStrings[k].zh), `${k}.zh`).toEqual(e)
      expect(params(pepStrings[k].id), `${k}.id`).toEqual(e)
    }
  })
  it('pt interpolates and falls back to English then the key', () => {
    expect(pt('chipPep', 'id', { style: 'E' })).toBe('Pep E')
    expect(pt('confirmPepBody', 'zh', { n: 3, style: 'A' })).toContain('3 口')
    expect(pt('nope' as PepKey, 'en')).toBe('nope')
  })
  it('SIM-only wording is in the pep mode copy in all three languages', () => {
    for (const l of ['en', 'zh', 'id'] as const) {
      expect(pepStrings.confirmPepBody[l]).toMatch(/SIM/)
      expect(pepStrings.modeDesc[l]).toMatch(/SIM/)
    }
  })
})
