import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { RiskControlEditor } from './RiskControlEditor'
import type { RiskControlConfig } from '../../types'
import {
  RISK_DEFAULT_MAX_CONTRACTS_PER_ORDER,
  RISK_DEFAULT_MAX_POSITIONS,
  RISK_DEFAULT_TRAILING_ARM_POINTS,
} from '../../riskControlDefaults'

it('keeps the owner daily-loss control without requiring a per-trade cap', () => {
  const onChange = vi.fn()
  const config = {
    guardrails_enabled: true,
    daily_loss_enabled: true,
    daily_loss_limit_usd: 450,
  } as RiskControlConfig
  render(
    <RiskControlEditor
      config={config}
      onChange={onChange}
      language="en"
      isFutures
    />
  )
  expect(
    screen.queryByRole('spinbutton', { name: 'MNQ per-trade risk cap' })
  ).not.toBeInTheDocument()
  fireEvent.change(screen.getByDisplayValue('450'), {
    target: { value: '400' },
  })
  expect(onChange).toHaveBeenCalledWith({
    ...config,
    daily_loss_limit_usd: 400,
  })
})

// ── W1 (settings truth, 2026-09-23) — the breaker row is PRESENCE-AWARE ─────
// absent/null = inherit (the runtime enforces env BREAKER_HALT_N, else 8 — ON),
// an explicit 0 = OFF, N = N. The old row showed an absent breaker as OFF while
// the runtime enforced 8, and ON wrote a literal 2.

function renderBreaker(config: RiskControlConfig) {
  const onChange = vi.fn()
  render(
    <RiskControlEditor
      config={config}
      onChange={onChange}
      language="en"
      isFutures
    />
  )
  return {
    onChange,
    toggle: screen.getByTestId('breaker-toggle'),
    input: screen.getByTestId('breaker-halt-input') as HTMLInputElement,
  }
}

it('W1: an ABSENT breaker reads ON (inherit) with an empty box, never a fake number', () => {
  const { toggle, input } = renderBreaker({} as RiskControlConfig)
  expect(toggle.getAttribute('aria-checked')).toBe('true')
  expect(input.value).toBe('')
  expect(input.placeholder).toBe('inherit')
})

it('W1: OFF writes an explicit 0', () => {
  const config = {} as RiskControlConfig
  const { onChange, toggle } = renderBreaker(config)
  fireEvent.click(toggle)
  expect(onChange).toHaveBeenCalledWith({ ...config, consecutive_loss_halt: 0 })
})

it('W1: a stored 0 reads OFF, and ON writes null (inherit) — never a literal 2', () => {
  const config = { consecutive_loss_halt: 0 } as RiskControlConfig
  const { onChange, toggle } = renderBreaker(config)
  expect(toggle.getAttribute('aria-checked')).toBe('false')
  fireEvent.click(toggle)
  const next = onChange.mock.calls[0][0] as RiskControlConfig
  expect(next.consecutive_loss_halt).toBeNull()
  // The wire shape the PUT sends: the key is PRESENT as null — the merge keeps
  // absent keys, so an omitted key could never clear the stored 0.
  expect(JSON.stringify(next)).toContain('"consecutive_loss_halt":null')
})

it('W1: clearing the number box writes null (inherit), never 0', () => {
  const config = { consecutive_loss_halt: 5 } as RiskControlConfig
  const { onChange, input } = renderBreaker(config)
  expect(input.value).toBe('5')
  fireEvent.change(input, { target: { value: '' } })
  expect(onChange).toHaveBeenCalledWith({
    ...config,
    consecutive_loss_halt: null,
  })
})

it('W1: typing a number writes it; typing 0 writes OFF', () => {
  const config = { consecutive_loss_halt: 5 } as RiskControlConfig
  const { onChange, input } = renderBreaker(config)
  fireEvent.change(input, { target: { value: '3' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...config,
    consecutive_loss_halt: 3,
  })
  fireEvent.change(input, { target: { value: '0' } })
  expect(onChange).toHaveBeenLastCalledWith({
    ...config,
    consecutive_loss_halt: 0,
  })
})

// ── U2 (owed-ui-ci, 2026-09-23) — UI fallbacks are the SAME constants the
// Go runtime resolves; component + tests read one module, never two literals ──

it('U2: the shared constants equal the Go runtime defaults', () => {
  expect(RISK_DEFAULT_MAX_POSITIONS).toBe(1) // store/strategy.go ClampLimits floor
  expect(RISK_DEFAULT_TRAILING_ARM_POINTS).toBe(0) // RiskControlConfig zero value
  expect(RISK_DEFAULT_MAX_CONTRACTS_PER_ORDER).toBe(1) // kernel.StageAContractCapDefault
})

it('U2: an UNSET max_positions shows the ClampLimits floor, never the 3-ceiling', () => {
  render(
    <RiskControlEditor
      config={{} as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  const input = screen.getByTestId('max-positions-input') as HTMLInputElement
  expect(input.value).toBe(String(RISK_DEFAULT_MAX_POSITIONS))
})

it('U2: an UNSET trailing_arm_points shows the runtime zero, never 50', () => {
  render(
    <RiskControlEditor
      config={{ trailing_arm: 'after_trigger_points' } as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  const input = screen.getByTestId(
    'trailing-arm-points-input'
  ) as HTMLInputElement
  expect(input.value).toBe(String(RISK_DEFAULT_TRAILING_ARM_POINTS))
})

it('U2: the max_contracts placeholder is the Stage-A effective default', () => {
  render(
    <RiskControlEditor
      config={{} as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  const input = screen.getByTestId('max-contracts-input') as HTMLInputElement
  expect(input.placeholder).toBe(String(RISK_DEFAULT_MAX_CONTRACTS_PER_ORDER))
})

// r22 RISK-GATES — min_confidence has no meaning for mentor rules (no AI
// confidence). The editor says so when mentor mode is ON, and only then.
it('r22: min confidence says it does not apply in mentor mode (and only then)', () => {
  const { unmount } = render(
    <RiskControlEditor
      config={{ mentor_mode: true, min_confidence: 60 } as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  expect(screen.getByTestId('min-confidence-mentor-note')).toHaveTextContent(
    'Does not apply in mentor mode'
  )
  unmount()
  render(
    <RiskControlEditor
      config={{ mentor_mode: false, min_confidence: 60 } as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  expect(
    screen.queryByTestId('min-confidence-mentor-note')
  ).not.toBeInTheDocument()
})

// r22 RISK-GATES — the daily-loss row says what the switch does when ON.
it('r22: the daily-loss row states refuse + cancel + flatten + latch to 17:00 CT', () => {
  render(
    <RiskControlEditor
      config={
        {
          guardrails_enabled: false,
          daily_loss_enabled: false,
          daily_loss_limit_usd: 450,
        } as RiskControlConfig
      }
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  const note = screen.getByTestId('daily-loss-what-it-does')
  for (const want of [
    'refuses new entries',
    'cancelled',
    'flattened',
    '17:00 CT',
    'manual trades excluded',
  ]) {
    expect(note).toHaveTextContent(want)
  }
})

// r22 RISK-GATES (owner R4 + R5): a strategy that never saved the guardrails
// master reads OFF — the switch shows what the runtime resolves
// (kernel.ResolveStrategyGuardrails: absent = OFF). MUTANT: `?? true` → RED.
it('r22: an ABSENT guardrails master shows OFF; a saved ON shows ON', () => {
  const { unmount } = render(
    <RiskControlEditor
      config={{} as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  expect(screen.getByTestId('guardrails-master-toggle')).toHaveAttribute(
    'aria-checked',
    'false'
  )
  unmount()
  render(
    <RiskControlEditor
      config={{ guardrails_enabled: true } as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  expect(screen.getByTestId('guardrails-master-toggle')).toHaveAttribute(
    'aria-checked',
    'true'
  )
})

// Release #25 (STRICT-12345 F-8) — breakeven in R: absent = 0 = OFF (the points
// rule), inert while breakeven is off, writes breakeven_trigger_r when on.
it('F-8: breakeven_trigger_r reads 0 (off) when absent and is disabled while breakeven is off', () => {
  render(
    <RiskControlEditor
      config={{} as RiskControlConfig}
      onChange={vi.fn()}
      language="en"
      isFutures
    />
  )
  const input = screen.getByTestId(
    'breakeven-trigger-r-input'
  ) as HTMLInputElement
  expect(input.value).toBe('0')
  expect(input.disabled).toBe(true)
})

it('F-8: with breakeven on, committing 1 writes breakeven_trigger_r: 1', () => {
  const onChange = vi.fn()
  const config = { breakeven_enabled: true } as RiskControlConfig
  render(
    <RiskControlEditor
      config={config}
      onChange={onChange}
      language="en"
      isFutures
    />
  )
  const input = screen.getByTestId(
    'breakeven-trigger-r-input'
  ) as HTMLInputElement
  fireEvent.focus(input)
  fireEvent.change(input, { target: { value: '1' } })
  fireEvent.blur(input)
  expect(onChange).toHaveBeenCalledWith({ ...config, breakeven_trigger_r: 1 })
})
