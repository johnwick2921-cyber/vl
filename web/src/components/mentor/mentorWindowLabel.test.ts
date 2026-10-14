// T30 — the live config saves mentor_window_minutes = -1 on both bound
// strategies; the card must say "no window", never "open"/"disabled".
// Mutant: drop the minutes <= 0 branch → RED.
import { expect, it } from 'vitest'
import { mentorWindowLabel } from './mentorWindowLabel'

it('names each window state the API distinguishes', () => {
  expect(mentorWindowLabel({ minutes: -1, active: true, ended: false })).toBe(
    'no window (entries at any hour)'
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
