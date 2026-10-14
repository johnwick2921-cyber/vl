// DR-3 (release #24): the guide's revision banner, one honest sentence per
// state. types.ts promises that an unusable production rev degrades to
// 'unknown' ("cannot verify the build") and a dev build shows 'dev' ("not a
// release build", never a matching revision) — but the page rendered both as
// a generic REVISION DRIFT. This pure verdict is what GuidePage renders.

/** Both sides truncate to this before comparing: the bot can only ever send a
 * short sha, so a full-length equality test can never succeed. */
export const REV_COMPARE_LEN = 12

export interface GuideRevBanner {
  kind: 'unknown' | 'dev' | 'drift'
  text: string
}

/**
 * builtRev: GUIDE_BUILT_REV (a 40-hex sha, 'unknown' or 'dev').
 * revision: /api/health `revision` — undefined until read (or when the read
 * failed; GuidePage says that separately), '' while the bot does not know its
 * own rev yet.
 */
export function guideRevBanner(
  builtRev: string,
  revision: string | undefined
): GuideRevBanner | null {
  const bot =
    revision === undefined || revision === ''
      ? 'the bot has not reported its revision'
      : `the bot reports ${revision}`
  if (builtRev === 'unknown')
    return {
      kind: 'unknown',
      text: `⚠ CANNOT VERIFY THE BUILD — this page bundle carries no build revision (VITE_GUIDE_BUILT_REV was missing or not a 40-hex sha), so it cannot tell whether it matches the running bot; ${bot}.`,
    }
  if (builtRev === 'dev')
    return {
      kind: 'dev',
      text: `⚠ NOT A RELEASE BUILD — this page bundle is a development build (rev "dev"), never a matching revision; ${bot}. Its cites are not tied to the running binary.`,
    }
  // Drift only when there is a revision to disagree with: '' means the boot
  // assertion has not run yet — not knowing is not disagreeing.
  if (
    revision !== undefined &&
    revision !== '' &&
    revision.slice(0, REV_COMPARE_LEN) !== builtRev.slice(0, REV_COMPARE_LEN)
  )
    return {
      kind: 'drift',
      text: `⚠ REVISION DRIFT — this page bundle was built at ${builtRev}, the bot reports ${revision}: the UI and the bot come from different builds. Reload the page; some cites may be stale until they match.`,
    }
  return null
}
