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
