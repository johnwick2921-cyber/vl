// P4.5 — DayPlanEditor tests: master switch materializes defaults, filters/mode
// write through onChange, and per-session override toggles set/clear pointer
// fields (⚪ inherit / 🔸 override).

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { DayPlanEditor } from './DayPlanEditor'
import type { DayPlanConfig } from '../../types/strategy'

describe('DayPlanEditor', () => {
  it('enabling the master switch materializes the default block', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor config={undefined} onChange={onChange} language="en" />
    )
    // the master toggle is the first switch
    fireEvent.click(screen.getAllByRole('switch')[0])
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.plan_enabled).toBe(true)
    expect(next.max_levels).toBe(8) // spec default carried through
    expect(next.acceptance_rule).toBe('5m_close')
  })

  it('changing max levels writes through onChange', () => {
    const onChange = vi.fn()
    const cfg: DayPlanConfig = { plan_enabled: true, max_levels: 8 }
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    const input = screen.getByDisplayValue('8')
    fireEvent.change(input, { target: { value: '6' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ max_levels: 6 })
    )
  })

  it('W-KNOB-UI: renders the structure-map and by-TF freshness toggles', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    const sm = screen.getByTestId('structure-map-toggle')
    const ft = screen.getByTestId('fresh-by-tf-toggle')
    const fr = screen.getByTestId('flip-reread-toggle')
    expect(sm.getAttribute('aria-checked')).toBe('false')
    expect(ft.getAttribute('aria-checked')).toBe('false')
    expect(fr.getAttribute('aria-checked')).toBe('false')
  })

  it('W-KNOB-UI: toggling calls update with the right key', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.click(screen.getByTestId('structure-map-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ structure_map: true })
    )
    onChange.mockClear()
    fireEvent.click(screen.getByTestId('fresh-by-tf-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ levels_fresh_by_tf: true })
    )
    onChange.mockClear()
    fireEvent.click(screen.getByTestId('flip-reread-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ flip_reread: true })
    )
  })

  it('a per-session tri-state knob sets then inherits (clears) the field', () => {
    const onChange = vi.fn()
    const cfg: DayPlanConfig = { plan_enabled: true }
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    // NY accordion is open by default; pick B on the min-grade tri-state
    const seg = screen.getByTestId('session-min-grade-NY')
    fireEvent.click(within(seg).getByRole('button', { name: 'B' }))
    const call = onChange.mock.calls[0][0] as DayPlanConfig
    expect(call.sessions?.find((s) => s.session === 'NY')?.min_grade).toBe('B')
    // inherit clears the field again (stores nothing)
    onChange.mockClear()
    fireEvent.click(within(seg).getByRole('button', { name: 'inherit' }))
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(
      next.sessions?.find((s) => s.session === 'NY')?.min_grade
    ).toBeUndefined()
  })

  it('session plan_mode tri-state stores nothing on inherit and writes explicit values', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    const seg = screen.getByTestId('session-plan-mode-NY')
    // default = inherit (no stored field)
    fireEvent.click(within(seg).getByRole('button', { name: 'STRICT' }))
    let next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.sessions?.find((s) => s.session === 'NY')?.plan_mode).toBe(
      'strict'
    )
    onChange.mockClear()
    fireEvent.click(within(seg).getByRole('button', { name: 'inherit' }))
    next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(
      next.sessions?.find((s) => s.session === 'NY')?.plan_mode
    ).toBeUndefined()
  })

  it('migrates a stored override EQUAL to the global value to inherit (S layering)', () => {
    const onChange = vi.fn()
    const cfg: DayPlanConfig = {
      plan_enabled: true,
      plan_mode: 'strict',
      min_scenario_quality: 'C',
      sessions: [
        { session: 'NY', plan_mode: 'strict', min_scenario_quality: 'C' },
        { session: 'ASIA', plan_mode: 'advisory' },
      ],
    }
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    // the mount migration emits the cleaned config (equals-global dropped)
    expect(onChange).toHaveBeenCalled()
    const cleaned = onChange.mock.calls
      .map((c) => c[0] as DayPlanConfig)
      .find((x) => x !== cfg)
    expect(cleaned).toBeTruthy()
    const ny = cleaned?.sessions?.find((s) => s.session === 'NY')
    expect(ny?.plan_mode).toBeUndefined()
    expect(ny?.min_scenario_quality).toBeUndefined()
    // a REAL differing override survives
    const asia = cleaned?.sessions?.find((s) => s.session === 'ASIA')
    expect(asia?.plan_mode).toBe('advisory')
  })

  it('the global plan_mode row shows the live effect when sessions override (S layering)', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{
          plan_enabled: true,
          plan_mode: 'strict',
          sessions: [
            { session: 'NY', plan_mode: 'advisory' },
            { session: 'ASIA', plan_mode: 'advisory' },
          ],
        }}
        onChange={onChange}
        language="en"
      />
    )
    const warn = screen.getByTestId('plan-mode-override-warning')
    expect(warn.textContent).toContain('STRICT')
    expect(warn.textContent).toContain('NY')
    expect(warn.textContent).toContain('ASIA')
    expect(warn.textContent).toContain('⚠')
  })

  // S layering (2026-08-27) — PUT-path verification: an inherit edit must
  // REMOVE the stored key from the serialized payload (JSON.stringify drops
  // undefined), never write null/'' that the Go resolver could read as an
  // override. Both the CREATE and EDIT paths are locked here.
  it('PUT edit path: setting a session plan_mode to inherit serializes WITHOUT the key', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{
          plan_enabled: true,
          plan_mode: 'strict',
          sessions: [{ session: 'NY', plan_mode: 'advisory' }],
        }}
        onChange={onChange}
        language="en"
      />
    )
    const seg = screen.getByTestId('session-plan-mode-NY')
    fireEvent.click(within(seg).getByRole('button', { name: 'inherit' }))
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    const ny = next.sessions?.find((s) => s.session === 'NY')
    expect(ny?.plan_mode).toBeUndefined()
    // the exact wire shape the PUT sends: no key, not null and not ''
    const wire = JSON.stringify(ny)
    expect(wire.includes('"plan_mode"')).toBe(false)
  })

  it('PUT create path: a fresh config with all four tri-state knobs on inherit serializes WITHOUT the keys', () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    // pick STRICT, re-render with the emitted config (controlled component),
    // then back to inherit
    const seg = screen.getByTestId('session-plan-mode-NY')
    fireEvent.click(within(seg).getByRole('button', { name: 'STRICT' }))
    const strictCfg = onChange.mock.calls[0][0] as DayPlanConfig
    rerender(
      <DayPlanEditor config={strictCfg} onChange={onChange} language="en" />
    )
    fireEvent.click(
      within(screen.getByTestId('session-plan-mode-NY')).getByRole('button', {
        name: 'inherit',
      })
    )
    const next = onChange.mock.calls[1][0] as DayPlanConfig
    const ny = next.sessions?.find((s) => s.session === 'NY')
    expect(ny?.plan_mode).toBeUndefined()
    const wire = JSON.stringify(ny)
    for (const key of [
      'plan_mode',
      'min_grade',
      'min_scenario_quality',
      'max_trades',
    ]) {
      expect(wire.includes(`"${key}"`)).toBe(false)
    }
  })

  it('planner timeframes are an editable multiselect (toggle in/out)', () => {
    const onChange = vi.fn()
    const cfg: DayPlanConfig = {
      plan_enabled: true,
      planner_timeframes: ['D', '4h', '1h', '15m'],
    }
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    // toggling an OFF tf ('5m') adds it, preserving order
    const tf5 = screen.getByRole('switch', { name: '5m' })
    expect(tf5).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(tf5)
    let next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.planner_timeframes).toEqual(['D', '4h', '1h', '15m', '5m'])
    // toggling an ON tf ('1h') removes it
    onChange.mockClear()
    fireEvent.click(screen.getByRole('switch', { name: '1h' }))
    next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.planner_timeframes).toEqual(['D', '4h', '15m'])
  })

  it('the whole body is disabled when the plan is off', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: false }}
        onChange={onChange}
        language="en"
      />
    )
    // number fields exist but are disabled while off
    const levels = screen.getByDisplayValue('8') as HTMLInputElement
    expect(levels.disabled).toBe(true)
  })
})

// PART A — the reported bug: no enable toggle on ANY session row. These lock the
// fix: a toggle on every row, defaults matching the resolver, and a flip that
// writes the per-session `enable` override (the field the Go gates read).
describe('DayPlanEditor · session enable toggles', () => {
  it('renders an enable toggle on ALL THREE session rows', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    for (const s of ['NY', 'ASIA', 'LONDON']) {
      const el = screen.getByTestId(`session-enable-${s}`)
      expect(el).toBeTruthy()
      expect(el.getAttribute('role')).toBe('switch')
      // accessible name (a bare role=switch announces as just "switch")
      expect(el.getAttribute('aria-label')).toContain(s)
    }
  })

  it('defaults mirror the Go resolver: NY on, ASIA/LONDON off', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    expect(
      screen.getByTestId('session-enable-NY').getAttribute('aria-checked')
    ).toBe('true')
    expect(
      screen.getByTestId('session-enable-ASIA').getAttribute('aria-checked')
    ).toBe('false')
    expect(
      screen.getByTestId('session-enable-LONDON').getAttribute('aria-checked')
    ).toBe('false')
  })

  it('flipping ASIA on writes sessions[].enable = true (what the gates read)', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.click(screen.getByTestId('session-enable-ASIA'))
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    const asia = next.sessions?.find((x) => x.session === 'ASIA')
    expect(asia?.enable).toBe(true)
  })

  it('flipping NY off writes an explicit false (not just an absent field)', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.click(screen.getByTestId('session-enable-NY'))
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    const ny = next.sessions?.find((x) => x.session === 'NY')
    expect(ny?.enable).toBe(false) // explicit off — an absent field would inherit ON
  })

  it('an explicit override shows the 🔸 chip; inherit shows none', () => {
    const { rerender } = render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    expect(screen.queryByTestId('session-enable-chip-ASIA')).toBeNull()
    rerender(
      <DayPlanEditor
        config={{
          plan_enabled: true,
          sessions: [{ session: 'ASIA', enable: true }],
        }}
        onChange={vi.fn()}
        language="en"
      />
    )
    expect(screen.getByTestId('session-enable-chip-ASIA')).toBeTruthy()
  })
})

// C3 (2026-08-26) — last_entry_ct / eod_flat_ct are LEGACY day-scoped clocks,
// UNREACHABLE since the P2 session-scope rework (auto_trader_clock.go: nothing
// evaluates them). The editor hides the controls; stored values are untouched.
describe('DayPlanEditor · the day-trader clock + re-align cap', () => {
  it('hides the vestigial last-entry / EOD-flat controls (C3)', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    expect(screen.queryByTestId('last-entry-ct')).toBeNull()
    expect(screen.queryByTestId('eod-flat-ct')).toBeNull()
    expect(screen.queryByTestId('eod-flat-warning')).toBeNull()
  })

  it('one setup: absent reads ON (mirrors Go pointer-bool), toggle stores false, grade selectable', () => {
    const onChange = vi.fn()
    const cfg: DayPlanConfig = { plan_enabled: true }
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    // absent one_setup_enabled renders ON (defaults object, pointer semantics)
    const sw = screen.getByTestId('one-setup-toggle')
    expect(sw).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(sw)
    const off = onChange.mock.calls[0][0] as DayPlanConfig
    expect(off.one_setup_enabled).toBe(false)
    // grade: default B, selectable to A
    const seg = screen.getByTestId('one-setup-min-grade')
    expect(seg.textContent).toContain('B')
    fireEvent.click(within(seg).getByRole('button', { name: 'A' }))
    const withA = onChange.mock.calls[1][0] as DayPlanConfig
    expect(withA.one_setup_min_grade).toBe('A')
  })
})
