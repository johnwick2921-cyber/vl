// DR-3 (release #24): types.ts promises 'unknown' → "cannot verify the build"
// and 'dev' → "not a release build, never a matching revision"; the page
// rendered both as a generic REVISION DRIFT. Mutant: drop the 'dev' branch
// (fall through to the drift compare) → RED.
import { describe, expect, it } from 'vitest'
import { guideRevBanner } from './guideRevBanner'

const SHA = '662c79bd236f43fb15eb0c7950880123896be8c0'

describe('guideRevBanner', () => {
  it("a 'dev' bundle says it is not a release build — never drift, never a match", () => {
    for (const rev of ['0ldcafe12345678', 'dev', '', undefined]) {
      const b = guideRevBanner('dev', rev)
      expect(b?.kind).toBe('dev')
      expect(b?.text).toMatch(/NOT A RELEASE BUILD/)
      expect(b?.text).not.toMatch(/DRIFT/)
    }
  })
  it("an 'unknown' bundle says the build cannot be verified", () => {
    const b = guideRevBanner('unknown', SHA.slice(0, 12))
    expect(b?.kind).toBe('unknown')
    expect(b?.text).toMatch(/CANNOT VERIFY THE BUILD/)
  })
  it('a real sha drifts only against a different revision', () => {
    expect(guideRevBanner(SHA, '0ldcafe12345678')?.kind).toBe('drift')
    expect(guideRevBanner(SHA, SHA.slice(0, 12))).toBeNull()
    expect(guideRevBanner(SHA, SHA)).toBeNull()
    expect(guideRevBanner(SHA, '')).toBeNull()
    expect(guideRevBanner(SHA, undefined)).toBeNull()
  })
})
