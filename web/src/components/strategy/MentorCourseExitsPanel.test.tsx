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
    mentor_resonance_runner_tp_old_high: true,
    mentor_be_before_partial: true,
    mentor_isb_c3_partial_any_profit: true,
    mentor_reverse_isb_as_phl: true,
    mentor_box_fresh_rth_pair: false,
    mentor_swing4h_contracts: 2,
    mentor_isb_max_pts: 30,
  })
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
