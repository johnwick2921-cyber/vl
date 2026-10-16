import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { RiskControlEditor } from './RiskControlEditor'
import type { MentorTuning, RiskControlConfig } from '../../types'
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

// Release #25 course-entry knobs: every new control writes its own key under
// mentor_tuning, and its blank / default option removes that key again.
const COURSE_ENTRY_WRITES: Array<[string, string, unknown]> = [
  ['15m_ib_requires_5m_isb', 'no_trade_until_escape', 'no_trade_until_escape'],
  ['tf_conflict_end', 'both_boxes', 'both_boxes'],
  ['news_wait_both_lines', 'on', true],
  ['news_bars_excluded', 'on', true],
  ['isb_box_escape', 'close', 'close'],
  ['ema34_loss_relock', 'session', 'session'],
  ['orb_edge_veto_pts', '2.5', 2.5],
  ['d43_trend_source', 'structure', 'structure'],
  ['level_entries_need_trigger_side', 'on', true],
  ['trendline_is_target', 'off', false],
  ['trendline_between_boxes', 'on', true],
  ['leg_reset_on_wave_break', 'off', false],
]

it.each(COURSE_ENTRY_WRITES)(
  'course-entry knob %s writes its key and the blank option removes it',
  (key, input, stored) => {
    const first = renderEditor({ mentor_mode: true })
    fireEvent.change(screen.getByTestId(`mentor-tuning-${key}`), {
      target: { value: input },
    })
    expect(first.onChange).toHaveBeenCalledWith({
      ...first.full,
      mentor_tuning: { [key]: stored },
    })
    cleanup()

    // blank: a bool's "default", an enum's "", a number's empty box
    const second = renderEditor({
      mentor_mode: true,
      mentor_tuning: { trigger_school: 2, [key]: stored } as MentorTuning,
    })
    const ctl = screen.getByTestId(`mentor-tuning-${key}`)
    const blank = typeof stored === 'boolean' ? 'default' : ''
    fireEvent.change(ctl, { target: { value: blank } })
    expect(second.onChange).toHaveBeenCalledWith({
      ...second.full,
      mentor_tuning: { trigger_school: 2 },
    })
  }
)

it('an enum knob shows its ruled default on the blank option', () => {
  renderEditor({ mentor_mode: true })
  const esc = screen.getByTestId('mentor-tuning-isb_box_escape')
  expect(esc).toHaveValue('')
  expect(within(esc).getAllByRole('option')[0]).toHaveTextContent(
    'default: body — open AND close beyond the edge'
  )
  const ib = screen.getByTestId('mentor-tuning-15m_ib_requires_5m_isb')
  expect(within(ib).getAllByRole('option')[0]).toHaveTextContent(
    'default: require a 5m ISB in the 15m direction'
  )
  expect(screen.getByTestId('mentor-tuning-orb_edge_veto_pts')).toHaveAttribute(
    'placeholder',
    'default: 0'
  )
})

it('the course-entry defaults follow the knob rule (a) ON / (b) OFF', () => {
  expect(MENTOR_TUNING_DEFAULTS).toMatchObject({
    '15m_ib_requires_5m_isb': 'require_5m_isb',
    tf_conflict_end: 'both_boxes', // course-defaults-30
    news_wait_both_lines: true, // course-defaults-30
    news_bars_excluded: true, // course-defaults-30 (CE-4, release #29 rework)
    isb_box_escape: 'body',
    ema34_loss_relock: 'until_zone_exit',
    orb_edge_veto_pts: 0,
    d43_trend_source: 'structure', // course-defaults-30
    level_entries_need_trigger_side: false,
    trendline_is_target: true,
    trendline_between_boxes: true, // course-defaults-30
    leg_reset_on_wave_break: true,
    flag_wall_target: true, // course-defaults-30
    htf_gate_news_only: false, // U-6 open — FOLD 2026-10-16 reverted to the all-day gate
  })
})
