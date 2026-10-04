import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { RiskControlEditor } from './RiskControlEditor'
import type { RiskControlConfig } from '../../types'
import { MENTOR_TUNING_DEFAULTS, withTuning } from './MentorTuningPanel'

function renderEditor(config: Partial<RiskControlConfig>) {
  const onChange = vi.fn()
  const full = { max_positions: 1, ...config } as RiskControlConfig
  render(
    <RiskControlEditor
      config={full}
      onChange={onChange}
      language="en"
      isFutures
      strategyName="MNQ Mentor"
    />
  )
  return { onChange, full }
}

it('shows the tuning block only while Mentor mode is ON', () => {
  const off = renderEditor({ mentor_mode: false })
  expect(screen.queryByTestId('mentor-tuning-block')).toBeNull()
  off.onChange.mockClear()
})

it('writes a typed number under mentor_tuning and shows the ruled default as placeholder', () => {
  const { onChange, full } = renderEditor({ mentor_mode: true })
  const gap = screen.getByTestId('mentor-tuning-ping_pong_min_gap_pts')
  expect(gap).toHaveAttribute('placeholder', 'default: 50')
  fireEvent.change(gap, { target: { value: '100' } })
  expect(onChange).toHaveBeenCalledWith({
    ...full,
    mentor_tuning: { ping_pong_min_gap_pts: 100 },
  })
})

it('selects school 2 and clears back to the default', () => {
  const { onChange, full } = renderEditor({
    mentor_mode: true,
    mentor_tuning: { trigger_school: 2 },
  })
  const sel = screen.getByTestId('mentor-tuning-trigger_school')
  expect(sel).toHaveValue('2')
  fireEvent.change(sel, { target: { value: '' } })
  // the only key is removed, so the empty block is dropped entirely
  expect(onChange).toHaveBeenCalledWith({ ...full, mentor_tuning: undefined })
})

it('a tri-state boolean stores true / false and removes the key on default', () => {
  const { onChange, full } = renderEditor({ mentor_mode: true })
  const rev = screen.getByTestId('mentor-tuning-isb_reverse_ema9_enabled')
  expect(rev).toHaveValue('default')
  fireEvent.change(rev, { target: { value: 'off' } })
  expect(onChange).toHaveBeenCalledWith({
    ...full,
    mentor_tuning: { isb_reverse_ema9_enabled: false },
  })
})

it('an explicit 0 visits is kept (cap off) while a blank removes the key', () => {
  expect(withTuning({}, { level_max_visits: 0 })).toEqual({
    level_max_visits: 0,
  })
  expect(
    withTuning({ level_max_visits: 4 }, { level_max_visits: undefined })
  ).toBeUndefined()
})

it('the placeholders carry the owner-ruled defaults', () => {
  expect(MENTOR_TUNING_DEFAULTS).toMatchObject({
    day_gate_spent_pts: 300,
    day_gate_target_cap_pts: 15,
    swing_max_stop_pts: 100,
    isb_reverse_ema9_enabled: true,
  })
})
