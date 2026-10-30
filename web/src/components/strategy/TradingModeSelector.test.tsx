import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { TradingModeSelector } from './TradingModeSelector'
import type { TradingMode } from './pepModel'

function setup(
  props: Partial<React.ComponentProps<typeof TradingModeSelector>> = {}
) {
  const onSelect = vi.fn()
  render(
    <TradingModeSelector
      mode="ai"
      language="en"
      strategyName="MNQ Pep"
      onSelect={onSelect}
      {...props}
    />
  )
  return { onSelect }
}

it('shows the three modes and marks the current one', () => {
  setup({ mode: 'mentor' })
  expect(screen.getByTestId('trading-mode-ai')).toHaveAttribute(
    'aria-checked',
    'false'
  )
  expect(screen.getByTestId('trading-mode-mentor')).toHaveAttribute(
    'aria-checked',
    'true'
  )
  expect(screen.getByTestId('trading-mode-pep')).toHaveAttribute(
    'aria-checked',
    'false'
  )
})

// Mutant: call onSelect('pep') straight from the click -> RED (no confirm).
it('Pep asks first with SIM-only wording; nothing is written until OK', () => {
  const { onSelect } = setup({ pepStyle: 'E', pepContracts: 3 })
  fireEvent.click(screen.getByTestId('trading-mode-pep'))
  expect(onSelect).not.toHaveBeenCalled()
  const dlg = screen.getByTestId('trading-mode-confirm')
  expect(dlg).toHaveTextContent('MNQ Pep')
  expect(dlg).toHaveTextContent('NinjaTrader SIM only')
  expect(dlg).toHaveTextContent('3 contract(s), style E')
  fireEvent.click(screen.getByTestId('trading-mode-confirm-ok'))
  expect(onSelect).toHaveBeenCalledWith('pep' as TradingMode)
})

it('cancel changes nothing', () => {
  const { onSelect } = setup()
  fireEvent.click(screen.getByTestId('trading-mode-pep'))
  fireEvent.click(screen.getByTestId('trading-mode-confirm-cancel'))
  expect(onSelect).not.toHaveBeenCalled()
  expect(screen.queryByTestId('trading-mode-confirm')).not.toBeInTheDocument()
})

it('Mentor also asks first; AI is immediate', () => {
  const { onSelect } = setup({ mode: 'pep' })
  fireEvent.click(screen.getByTestId('trading-mode-mentor'))
  expect(onSelect).not.toHaveBeenCalled()
  expect(screen.getByTestId('trading-mode-confirm')).toHaveTextContent(
    'SIM only'
  )
  fireEvent.click(screen.getByTestId('trading-mode-confirm-ok'))
  expect(onSelect).toHaveBeenLastCalledWith('mentor')
  fireEvent.click(screen.getByTestId('trading-mode-ai'))
  expect(onSelect).toHaveBeenLastCalledWith('ai')
})

// Mutant: drop the dayPlanEnabled gate -> RED (the server would 400 the save).
it('Pep cannot be chosen while the Day Plan is ON', () => {
  const { onSelect } = setup({ dayPlanEnabled: true })
  expect(screen.getByTestId('trading-mode-pep')).toBeDisabled()
  expect(screen.getByTestId('trading-mode-dayplan-block')).toBeInTheDocument()
  fireEvent.click(screen.getByTestId('trading-mode-pep'))
  expect(screen.queryByTestId('trading-mode-confirm')).not.toBeInTheDocument()
  expect(onSelect).not.toHaveBeenCalled()
})

it('a loaded both-ON config shows the conflict and a re-pick clears it', () => {
  const { onSelect } = setup({ mode: 'pep', conflict: true })
  expect(screen.getByTestId('trading-mode-conflict')).toBeInTheDocument()
  fireEvent.click(screen.getByTestId('trading-mode-ai'))
  expect(onSelect).toHaveBeenCalledWith('ai')
})

it('hides the Pep option for a non-futures strategy', () => {
  setup({ showPep: false })
  expect(screen.queryByTestId('trading-mode-pep')).not.toBeInTheDocument()
})

it('renders the Indonesian and Chinese copy', () => {
  const { unmount } = render(
    <TradingModeSelector mode="ai" language="id" onSelect={() => {}} />
  )
  expect(screen.getAllByText('Mode trading').length).toBeGreaterThan(0)
  unmount()
  render(<TradingModeSelector mode="ai" language="zh" onSelect={() => {}} />)
  expect(screen.getAllByText('交易模式').length).toBeGreaterThan(0)
})

it('shows the server effective line for pep_enabled', () => {
  const knob = {
    path: 'pep.pep_enabled',
    status: 'live',
    ui_label: '',
    stored: { present: true, value: true },
    effective: true,
    origin: 'saved value',
    scope: 'strategy',
    resolver: 'x',
    resolved: true,
  }
  render(
    <TradingModeSelector
      mode="pep"
      language="en"
      onSelect={() => {}}
      eff={(p) => (p === 'pep.pep_enabled' ? knob : undefined)}
    />
  )
  expect(screen.getByTestId('effective-chip')).toHaveAttribute(
    'data-path',
    'pep.pep_enabled'
  )
})
