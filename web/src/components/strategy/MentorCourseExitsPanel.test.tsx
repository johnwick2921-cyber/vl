import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { RiskControlEditor } from './RiskControlEditor'
import { MENTOR_COURSE_EXITS_DEFAULTS } from './MentorCourseExitsPanel'
import type { RiskControlConfig } from '../../types'
import type { StudioEffective } from '../../lib/api/strategyEffective'

// Release #25 course-exits switches (lane mentor-course-exits): rendered under
// mentor mode, "default" REMOVES the key (the server runs the knob-rule
// default), ON/OFF and the numbers are stored as typed.

function renderEditor(config: Partial<RiskControlConfig>) {
  const onChange = vi.fn()
  const full = { max_positions: 1, ...config } as RiskControlConfig
  const eff: StudioEffective = { byPath: {}, bySession: {} }
  render(
    <RiskControlEditor
      config={full}
      onChange={onChange}
      language="en"
      isFutures
      effective={eff}
      strategyName="MNQ Mentor"
    />
  )
  return { onChange, full }
}

it('the course-exits block shows only under mentor mode', () => {
  renderEditor({})
  expect(screen.queryByTestId('mentor-course-exits-block')).toBeNull()
})

it('absent keys read "default" with the knob-rule default named', () => {
  renderEditor({ mentor_mode: true })
  const be = screen.getByTestId(
    'mentor-course-mentor_be_before_partial'
  ) as HTMLSelectElement
  expect(be.value).toBe('default')
  expect(be).toHaveTextContent('default: ON') // course-defaults-30: nil → ON
  const first = screen.getByTestId(
    'mentor-course-mentor_swing_ema34_is_first_target'
  ) as HTMLSelectElement
  expect(first).toHaveTextContent('default: ON')
})

it('the course-exits defaults are the code defaults (course-defaults-30)', () => {
  // Each mutant (swing_runner / reverse_isb_as_phl → false, swing4h_contracts
  // → 1, isb_max_pts → 0) must turn this RED — the "default:" option label and
  // the number placeholders are rendered from THIS table.
  expect(MENTOR_COURSE_EXITS_DEFAULTS).toMatchObject({
    mentor_swing_ema34_is_first_target: true,
    mentor_swing_runner: true,
    mentor_resonance_runner_tp_old_high: false,
    mentor_be_before_partial: true,
    mentor_isb_c3_partial_any_profit: true,
    mentor_reverse_isb_as_phl: true,
    mentor_box_fresh_rth_pair: true,
    mentor_swing4h_contracts: 2,
    mentor_isb_max_pts: 30,
  })
})

it('the ISB max stop label names it ISB-only (30-pt ceiling; PHL/box use the 25-pt stop ceiling)', () => {
  renderEditor({ mentor_mode: true })
  expect(screen.getByText(/ISB only/)).toBeTruthy()
})

it('ON saves true; default removes the key', () => {
  const { onChange, full } = renderEditor({ mentor_mode: true })
  fireEvent.change(
    screen.getByTestId('mentor-course-mentor_be_before_partial'),
    {
      target: { value: 'on' },
    }
  )
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_be_before_partial: true,
  })
})

it('choosing default on a stored key removes it from the config', () => {
  const { onChange } = renderEditor({
    mentor_mode: true,
    mentor_reverse_isb_as_phl: true,
  })
  fireEvent.change(
    screen.getByTestId('mentor-course-mentor_reverse_isb_as_phl'),
    { target: { value: 'default' } }
  )
  const saved = onChange.mock.lastCall![0] as Record<string, unknown>
  expect('mentor_reverse_isb_as_phl' in saved).toBe(false)
})

it('the EMA 9 mode and the numbers are stored as typed', () => {
  const { onChange, full } = renderEditor({ mentor_mode: true })
  fireEvent.change(screen.getByTestId('mentor-course-mentor_ema9_visit_cap'), {
    target: { value: 'exempt' },
  })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_ema9_visit_cap: 'exempt',
  })
  fireEvent.change(
    screen.getByTestId('mentor-course-mentor_box_max_height_pts'),
    {
      target: { value: '40' },
    }
  )
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_box_max_height_pts: 40,
  })
})

// m463 (D5.2-R31 [p2 @19:34–21:43] "100 điểm quá nhiều rủi ro… đừng vô"; owner
// order 2026-11-01 "full of it" reverses m186): the swing skip ceiling is a
// course-exits number, default 90, stored as typed.
it('the SWING max stop defaults to 90 and is stored as typed', () => {
  expect(MENTOR_COURSE_EXITS_DEFAULTS.mentor_swing_max_stop_pts).toBe(90)
  const { onChange, full } = renderEditor({ mentor_mode: true })
  const input = screen.getByTestId('mentor-course-mentor_swing_max_stop_pts')
  expect(screen.getByText(/SWING max stop/)).toBeTruthy()
  fireEvent.change(input, { target: { value: '110' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_swing_max_stop_pts: 110,
  })
})

// m465 P2 (release #60, D4.2-R27 "tôi bọc cái khu vực này lại. Tôi sẽ không
// trade bên trong" [D4.2 p2 @03:43–04:07]): the ISB loss box is a course-exits
// switch, default range; "default" removes the key.
it('the ISB loss box defaults to range and stores band / removes on default', () => {
  expect(MENTOR_COURSE_EXITS_DEFAULTS.mentor_isb_loss_box).toBe('range')
  const { onChange, full } = renderEditor({ mentor_mode: true })
  const sel = screen.getByTestId('mentor-course-mentor_isb_loss_box')
  expect(screen.getByText(/ISB loss box/)).toBeTruthy()
  fireEvent.change(sel, { target: { value: 'band' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_isb_loss_box: 'band',
  })
  fireEvent.change(sel, { target: { value: '' } })
  expect(onChange).toHaveBeenLastCalledWith({ ...full })
})

// m466 P4 (release #61, swing D5.2): the inside bar without close-back (R26,
// "đợi đóng ngược lại cũng được nhưng hơi lâu" [D5.2 p3 @04:49–05:09]), the
// inside-bar order kept on a close through (R25 [p2 @10:23–10:51]) and the
// runner with no TP (R6 [p1 @05:40–06:14]); "default" removes the key.
it('the swing ISB / runner TP knobs default to the course and store / remove', () => {
  expect(MENTOR_COURSE_EXITS_DEFAULTS.mentor_swing_isb_close_back).toBe(
    'not_required'
  )
  expect(MENTOR_COURSE_EXITS_DEFAULTS.mentor_swing_isb_keep).toBe(true)
  expect(MENTOR_COURSE_EXITS_DEFAULTS.mentor_swing_runner_tp).toBe('none')
  const { onChange, full } = renderEditor({ mentor_mode: true })
  const cb = screen.getByTestId('mentor-course-mentor_swing_isb_close_back')
  fireEvent.change(cb, { target: { value: 'required' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_swing_isb_close_back: 'required',
  })
  fireEvent.change(cb, { target: { value: '' } })
  expect(onChange).toHaveBeenLastCalledWith({ ...full })
  const tp = screen.getByTestId('mentor-course-mentor_swing_runner_tp')
  fireEvent.change(tp, { target: { value: 'capped' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_swing_runner_tp: 'capped',
  })
  const keep = screen.getByTestId('mentor-course-mentor_swing_isb_keep')
  fireEvent.change(keep, { target: { value: 'off' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_swing_isb_keep: false,
  })
})
