import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { RiskControlEditor } from './RiskControlEditor'
import type { RiskControlConfig } from '../../types'
import type { StudioEffective } from '../../lib/api/strategyEffective'

function eff(mentorPlace?: boolean): StudioEffective {
  return { byPath: {}, bySession: {}, mentorPlace }
}

function renderEditor(
  config: Partial<RiskControlConfig>,
  mentorPlace?: boolean,
  strategyName = 'MNQ Mentor'
) {
  const onChange = vi.fn()
  const full = { max_positions: 1, ...config } as RiskControlConfig
  render(
    <RiskControlEditor
      config={full}
      onChange={onChange}
      language="en"
      isFutures
      effective={eff(mentorPlace)}
      strategyName={strategyName}
    />
  )
  return { onChange, full }
}

it('renders the stored mentor_mode value', () => {
  renderEditor({ mentor_mode: true })
  expect(screen.getByTestId('mentor-mode-toggle')).toHaveAttribute(
    'aria-checked',
    'true'
  )
})

it('an absent mentor_mode reads OFF', () => {
  renderEditor({})
  expect(screen.getByTestId('mentor-mode-toggle')).toHaveAttribute(
    'aria-checked',
    'false'
  )
})

it('turning ON asks for confirmation naming the strategy and the order gate, then saves mentor_mode=true', () => {
  const { onChange, full } = renderEditor({}, false, 'MNQ Mentor')
  fireEvent.click(screen.getByTestId('mentor-mode-toggle'))
  expect(onChange).not.toHaveBeenCalled() // nothing until confirmed
  const dialog = screen.getByTestId('mentor-confirm')
  expect(dialog).toHaveTextContent('MNQ Mentor')
  expect(dialog).toHaveTextContent('Orders: DRY RUN (MENTOR_PLACE off)')
  fireEvent.click(screen.getByTestId('mentor-confirm-ok'))
  expect(onChange).toHaveBeenCalledWith({ ...full, mentor_mode: true })
  expect(screen.queryByTestId('mentor-confirm')).not.toBeInTheDocument()
})

it('cancelling the confirmation changes nothing', () => {
  const { onChange } = renderEditor({})
  fireEvent.click(screen.getByTestId('mentor-mode-toggle'))
  fireEvent.click(screen.getByTestId('mentor-confirm-cancel'))
  expect(onChange).not.toHaveBeenCalled()
  expect(screen.queryByTestId('mentor-confirm')).not.toBeInTheDocument()
})

it('turning OFF is immediate and saves mentor_mode=false', () => {
  const { onChange, full } = renderEditor({ mentor_mode: true })
  fireEvent.click(screen.getByTestId('mentor-mode-toggle'))
  expect(screen.queryByTestId('mentor-confirm')).not.toBeInTheDocument()
  expect(onChange).toHaveBeenCalledWith({ ...full, mentor_mode: false })
})

it('the gate line renders DRY RUN, SIM orders ON and unavailable', () => {
  const dry = renderEditor({}, false)
  expect(screen.getByTestId('mentor-order-gate')).toHaveTextContent(
    'Orders: DRY RUN (MENTOR_PLACE off)'
  )
  expect(dry.onChange).not.toHaveBeenCalled()
})

it('the gate line says SIM orders ON when MENTOR_PLACE=1', () => {
  renderEditor({}, true)
  expect(screen.getByTestId('mentor-order-gate')).toHaveTextContent(
    'Orders: SIM orders ON (MENTOR_PLACE=1)'
  )
})

it('the gate line never invents a state when the server did not say', () => {
  renderEditor({}, undefined)
  expect(screen.getByTestId('mentor-order-gate')).toHaveTextContent(
    'Orders: gate status unavailable'
  )
})

it('a read-only (default) strategy cannot be toggled', () => {
  const onChange = vi.fn()
  render(
    <RiskControlEditor
      config={{ max_positions: 1 } as RiskControlConfig}
      onChange={onChange}
      language="en"
      disabled
    />
  )
  fireEvent.click(screen.getByTestId('mentor-mode-toggle'))
  expect(screen.queryByTestId('mentor-confirm')).not.toBeInTheDocument()
  expect(onChange).not.toHaveBeenCalled()
})

// ── trading window controls (mentor_window_start / mentor_window_minutes) ──

it('shows the default window (08:30, 60 min) when nothing is stored', () => {
  renderEditor({})
  expect(screen.getByTestId('mentor-window-start')).toHaveValue('08:30')
  expect(screen.getByTestId('mentor-window-minutes')).toHaveValue('60')
  expect(screen.getByTestId('mentor-window-preview')).toHaveTextContent(
    'Mentor trades 08:30–09:30 CT'
  )
})

it('renders the stored window and previews a window that crosses midnight', () => {
  renderEditor({ mentor_window_start: '23:00', mentor_window_minutes: 120 })
  expect(screen.getByTestId('mentor-window-start')).toHaveValue('23:00')
  expect(screen.getByTestId('mentor-window-minutes')).toHaveValue('120')
  expect(screen.getByTestId('mentor-window-preview')).toHaveTextContent(
    'Mentor trades 23:00–01:00 CT'
  )
})

it('a valid HH:MM start is saved on the same path', () => {
  const { onChange, full } = renderEditor({})
  fireEvent.change(screen.getByTestId('mentor-window-start'), {
    target: { value: '09:15' },
  })
  expect(onChange).toHaveBeenCalledWith({
    ...full,
    mentor_window_start: '09:15',
  })
})

it('an invalid start is NOT saved and says why', () => {
  const { onChange } = renderEditor({})
  fireEvent.change(screen.getByTestId('mentor-window-start'), {
    target: { value: '' },
  })
  expect(onChange).not.toHaveBeenCalled()
  expect(screen.getByTestId('mentor-window-start-error')).toHaveTextContent(
    'HH:MM'
  )
})

it('the length select saves 30/90/120 and "no window" as -1', () => {
  const { onChange, full } = renderEditor({})
  const select = screen.getByTestId('mentor-window-minutes')
  fireEvent.change(select, { target: { value: '90' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_window_minutes: 90,
  })
  fireEvent.change(select, { target: { value: '-1' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...full,
    mentor_window_minutes: -1,
  })
})

it('a stored no-window (-1) reads "any hour" in the select and the preview', () => {
  renderEditor({ mentor_window_minutes: -1 })
  expect(screen.getByTestId('mentor-window-minutes')).toHaveValue('-1')
  expect(screen.getByTestId('mentor-window-preview')).toHaveTextContent(
    'Mentor trades at any hour'
  )
})

it('a stored length outside the presets is still shown, not silently replaced', () => {
  renderEditor({ mentor_window_minutes: 45 })
  expect(screen.getByTestId('mentor-window-minutes')).toHaveValue('45')
  expect(screen.getByTestId('mentor-window-preview')).toHaveTextContent(
    'Mentor trades 08:30–09:15 CT'
  )
})

it('the window controls are inert on a read-only strategy', () => {
  const onChange = vi.fn()
  render(
    <RiskControlEditor
      config={{ max_positions: 1 } as RiskControlConfig}
      onChange={onChange}
      language="en"
      disabled
    />
  )
  expect(screen.getByTestId('mentor-window-start')).toBeDisabled()
  expect(screen.getByTestId('mentor-window-minutes')).toBeDisabled()
})
