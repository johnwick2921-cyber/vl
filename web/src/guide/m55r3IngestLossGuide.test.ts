// m55r3-h1: the guide says what the ingest_loss pause does when the NT8
// re-request budget is spent, and that the re-request obeys the missing-bars
// tracker's send rules. MUTANT (shown RED): revert the guards.ts sentence.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'

const guideText = JSON.stringify(GUIDE_SECTIONS)

describe('m55r3 — ingest_loss spent-budget text', () => {
  it('names the spent-budget case and every exit from it', () => {
    expect(guideText).toContain('IF THAT BUDGET IS SPENT')
    expect(guideText).toContain('the pause STANDS')
    expect(guideText).toContain('ages out of the ring')
    expect(guideText).toContain('restart the bot')
  })
  it('says the re-request waits for the send window and the link/feed/market', () => {
    expect(guideText).toContain('only in the first 15 seconds of a minute')
  })
  it('the Go pause text carries the same exits', () => {
    const src = readFileSync(
      resolve(process.cwd(), '..', 'internal/datatruth/checks/ingestloss.go'),
      'utf8'
    )
    expect(src).toContain('once that is spent nothing more is asked')
    expect(src).toContain('or when the bot is restarted')
  })
})
