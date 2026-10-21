// T30 — the live config saves mentor_window_minutes = -1 on both bound
// strategies; the card must say "no window", never "open"/"disabled".
// Mutant: drop the minutes <= 0 branch → RED.
//
// UG-4 (release #24): "no window" must not claim entries at any hour — the
// 15:45 CT cutoff (mentorPastDailyHaltCutoff) and the 14:45 CT EOD flat
// (enforceEODFlatAt) still bind. Each fact is read from the Go source that
// decides it, so moving the code without the label fails here.
// Mutant: restore 'no window (entries at any hour)' → RED.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'
import { MENTOR_NO_WINDOW_LABEL, mentorWindowLabel } from './mentorWindowLabel'

const repo = (p: string) =>
  readFileSync(resolve(process.cwd(), '..', p), 'utf8')

it('names each window state the API distinguishes', () => {
  expect(mentorWindowLabel({ minutes: -1, active: true, ended: false })).toBe(
    MENTOR_NO_WINDOW_LABEL
  )
  expect(mentorWindowLabel({ minutes: 60, active: true, ended: false })).toBe(
    'open'
  )
  expect(mentorWindowLabel({ minutes: 60, active: false, ended: true })).toBe(
    'ended'
  )
  expect(mentorWindowLabel({ minutes: 60, active: false, ended: false })).toBe(
    'closed (no new entries now)'
  )
})

it('UG-4: "no window" states the clock rules that still bind', () => {
  const label = mentorWindowLabel({ minutes: -1, active: true, ended: false })
  expect(label).not.toMatch(/any hour/)
  // the 15:45 cutoff, as the code decides it
  const mm = repo('trader/mentor_mode.go')
  expect(mm).toMatch(/ct\.Hour\(\) == 15 && ct\.Minute\(\) >= 45/)
  expect(label).toContain('no new entry 15:45–17:00 CT')
  // the EOD flat cancels intraday mentor orders until the 17:00 reopen
  expect(repo('trader/auto_trader_clock.go')).toMatch(
    /cancelArmedOrdersSyncFiltered\("session close — EOD flat", skipSwingArms\)/
  )
  expect(repo('kernel/session_registry.go')).toMatch(/14:45/)
  expect(label).toContain('EOD flat (14:45 CT')
  expect(label).toContain('cancels intraday orders until 17:00')
})
