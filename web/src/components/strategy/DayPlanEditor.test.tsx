// P4.5 — DayPlanEditor tests: master switch materializes defaults, filters/mode
// write through onChange, and per-session override toggles set/clear pointer
// fields (⚪ inherit / 🔸 override).

import { describe, it, expect, vi } from 'vitest'
import {
  cleanup,
  render,
  screen,
  fireEvent,
  within,
} from '@testing-library/react'
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
    // W-KNOB-PRUNE: folded knobs are not seeded — the engine's constants apply.
    expect(next.acceptance_rule).toBeUndefined()
    expect(next.scenario_cap).toBeUndefined()
    expect(next.realign_cap).toBeUndefined()
    expect(next.evening_digest).toBeUndefined()
    expect(next.wake_min_interval_min).toBeUndefined()
    expect(next.wake_on_level_events).toBeUndefined()
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

  it('W-KNOB-PRUNE: the folded/removed controls are gone, flip re-read and the one wake switch remain', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    expect(screen.queryByTestId('structure-map-toggle')).toBeNull()
    expect(screen.queryByTestId('fresh-by-tf-toggle')).toBeNull()
    expect(screen.queryByText(/HTF score multiplier/)).toBeNull()
    expect(screen.queryByText(/^Max scenarios$/)).toBeNull()
    expect(screen.queryByText(/^Acceptance$/)).toBeNull()
    expect(screen.queryByText(/^Digest$/)).toBeNull()
    expect(screen.queryByText(/Max re-alignments/)).toBeNull()
    expect(screen.queryByText(/Wake on 15m zones/)).toBeNull()
    expect(screen.queryByText(/Min wake interval/)).toBeNull()
    expect(screen.queryByText(/Guarantee a 1h S\/D seat/)).toBeNull()
    const fr = screen.getByTestId('flip-reread-toggle')
    expect(fr.getAttribute('aria-checked')).toBe('false')
    // absent = ON (mirrors the Go pointer-bool)
    const wk = screen.getByTestId('wake-on-level-events-toggle')
    expect(wk.getAttribute('aria-checked')).toBe('true')
  })

  it('W-KNOB-PRUNE: a stored folded value passes through a save untouched', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={
          {
            plan_enabled: true,
            structure_map: true,
            scenario_cap: 5,
            realign_cap: 10,
            levels_fresh_by_tf: true,
            wake_on_htf_ob: true,
          } as DayPlanConfig
        }
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.click(screen.getByTestId('flip-reread-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        flip_reread: true,
        structure_map: true,
        scenario_cap: 5,
        realign_cap: 10,
        levels_fresh_by_tf: true,
        wake_on_htf_ob: true,
      })
    )
    onChange.mockClear()
    // W-DEATH-REREAD (2026-09-18): default ON — the first click turns it OFF.
    fireEvent.click(screen.getByTestId('death-reread-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ death_reread: false })
    )
    onChange.mockClear()
    fireEvent.click(screen.getByTestId('wake-on-level-events-toggle'))
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ wake_on_level_events: false })
    )
  })

  it('W-T1-CURRENCIES: the currency field parses a comma list, upper-cases, and clears to absent (default USD)', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
      />
    )
    const input = screen.getByTestId('t1-currencies-input') as HTMLInputElement
    expect(input.placeholder).toBe('USD')
    expect(input.value).toBe('')
    fireEvent.change(input, { target: { value: 'usd, eur ,' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ t1_currencies: ['USD', 'EUR'] })
    )
    onChange.mockClear()
    fireEvent.change(input, { target: { value: '' } })
    const cleared = onChange.mock.calls[0][0] as DayPlanConfig
    expect(cleared.t1_currencies).toBeUndefined()
  })

  it('W-T1-CURRENCIES: switching strategies resyncs the field, and an edit saves only to the new one (review of #171)', () => {
    const onChangeA = vi.fn()
    const onChangeB = vi.fn()
    const { rerender } = render(
      <DayPlanEditor
        config={{ plan_enabled: true, t1_currencies: ['USD', 'EUR'] }}
        onChange={onChangeA}
        language="en"
      />
    )
    const field = () =>
      screen.getByTestId('t1-currencies-input') as HTMLInputElement
    expect(field().value).toBe('USD,EUR')
    // a trailing comma mid-typing is NOT eaten by the resync
    fireEvent.change(field(), { target: { value: 'USD,EUR,' } })
    rerender(
      <DayPlanEditor
        config={{ plan_enabled: true, t1_currencies: ['USD', 'EUR'] }}
        onChange={onChangeA}
        language="en"
      />
    )
    expect(field().value).toBe('USD,EUR,')
    // strategy B has no list → the field shows the default placeholder, not A's text
    rerender(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChangeB}
        language="en"
      />
    )
    expect(field().value).toBe('')
    expect(field().placeholder).toBe('USD')
    fireEvent.change(field(), { target: { value: 'jpy' } })
    expect(onChangeB).toHaveBeenCalledWith(
      expect.objectContaining({ t1_currencies: ['JPY'] })
    )
    const savedB = onChangeB.mock.calls[0][0] as DayPlanConfig
    expect(savedB.t1_currencies).not.toContain('EUR')
    expect(onChangeA).toHaveBeenCalledTimes(1) // only the pre-switch keystroke
  })

  it('W-T1-CURRENCIES: a saved list renders in the field', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true, t1_currencies: ['ALL'] }}
        onChange={vi.fn()}
        language="en"
      />
    )
    expect(
      (screen.getByTestId('t1-currencies-input') as HTMLInputElement).value
    ).toBe('ALL')
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

// ── W1 (settings truth, 2026-09-23) — the strategy-level replan cap is
// PRESENCE-AWARE: absent/null = the shipped default 2 (an empty box, not a
// fake 2), an explicit 0 = no re-plan and is WRITTEN, clearing writes null.
describe('DayPlanEditor W1 replan cap', () => {
  it('absent shows an empty box with an inherit placeholder, not a fake 2', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    const input = screen.getByTestId('replan-cap-strategy') as HTMLInputElement
    expect(input.value).toBe('')
    expect(input.placeholder).toBe('inherit (2)')
  })

  it('typing 0 writes an explicit 0', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true, replan_cap: 4 }}
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.change(screen.getByTestId('replan-cap-strategy'), {
      target: { value: '0' },
    })
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.replan_cap).toBe(0)
    expect(JSON.stringify(next)).toContain('"replan_cap":0')
  })

  it('a session override seeds the EFFECTIVE strategy value, not a literal 2', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true, replan_cap: 4 }}
        onChange={onChange}
        language="en"
      />
    )
    fireEvent.click(screen.getByRole('button', { name: 'Max re-plans' }))
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.sessions?.find((x) => x.session === 'NY')?.replan_cap).toBe(4)
  })

  it('clearing the box writes null (inherit), which the PUT carries', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true, replan_cap: 0 }}
        onChange={onChange}
        language="en"
      />
    )
    const input = screen.getByTestId('replan-cap-strategy') as HTMLInputElement
    expect(input.value).toBe('0')
    fireEvent.change(input, { target: { value: '' } })
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.replan_cap).toBeNull()
    expect(JSON.stringify(next)).toContain('"replan_cap":null')
  })
})

// Release #25 (STRICT-12345 F-2/F-3/F-5/F-7) — the loss-fix switches: every one
// absent = OFF (COMMON-25 case b), never seeded by the master switch, and each
// control writes its own day_plan key.
describe('DayPlanEditor · release #25 loss-fix switches', () => {
  const renderOn = (cfg: DayPlanConfig = { plan_enabled: true }) => {
    const onChange = vi.fn()
    render(<DayPlanEditor config={cfg} onChange={onChange} language="en" />)
    return onChange
  }
  const segActive = (testId: string) => {
    const seg = screen.getByTestId(testId)
    return within(seg)
      .getAllByRole('button')
      .find((b) => b.style.color === 'var(--vl-gold)')?.textContent
  }

  it('all six controls render and read OFF when the keys are absent', () => {
    renderOn()
    expect(screen.getByTestId('loss-fix-switches')).toBeTruthy()
    expect(segActive('planner-room-gate')).toBe('OFF')
    expect(segActive('planner-exit-on-thesis-death')).toBe('OFF')
    expect(
      screen
        .getByTestId('planner-no-chase-enforce-toggle')
        .getAttribute('aria-checked')
    ).toBe('false')
    expect(
      screen
        .getByTestId('planner-atr-floor-cap-toggle')
        .getAttribute('aria-checked')
    ).toBe('false')
    const minR = screen.getByTestId('planner-room-min-r') as HTMLInputElement
    expect(minR.value).toBe('')
    expect(minR.disabled).toBe(true) // gate off → min R inert
    const mom = screen.getByTestId(
      'planner-momentum-into-zone'
    ) as HTMLInputElement
    expect(mom.value).toBe('')
  })

  it('the master switch never seeds them (absent = off)', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor config={undefined} onChange={onChange} language="en" />
    )
    fireEvent.click(screen.getAllByRole('switch')[0])
    const next = onChange.mock.calls[0][0] as DayPlanConfig
    expect(next.planner_room_gate).toBeUndefined()
    expect(next.planner_room_min_r).toBeUndefined()
    expect(next.planner_exit_on_thesis_death).toBeUndefined()
    expect(next.planner_no_chase_enforce).toBeUndefined()
    expect(next.planner_momentum_into_zone_max_atr).toBeUndefined()
    expect(next.planner_atr_floor_cap_at_invalidation).toBeUndefined()
  })

  it('room gate writes planner_room_gate; min R writes planner_room_min_r once on', () => {
    let onChange = renderOn()
    fireEvent.click(
      within(screen.getByTestId('planner-room-gate')).getByText('REFUSE')
    )
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ planner_room_gate: 'refuse' })
    )
    cleanup()
    onChange = renderOn({ plan_enabled: true, planner_room_gate: 'refuse' })
    const minR = screen.getByTestId('planner-room-min-r') as HTMLInputElement
    expect(minR.disabled).toBe(false)
    fireEvent.change(minR, { target: { value: '1.2' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ planner_room_min_r: 1.2 })
    )
  })

  it('thesis death writes planner_exit_on_thesis_death', () => {
    const onChange = renderOn()
    fireEvent.click(
      within(screen.getByTestId('planner-exit-on-thesis-death')).getByText(
        'INVALIDATION ONLY'
      )
    )
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        planner_exit_on_thesis_death: 'invalidation_only',
      })
    )
  })

  it('the two toggles and the momentum ceiling write their own keys', () => {
    const onChange = renderOn()
    fireEvent.click(screen.getByTestId('planner-no-chase-enforce-toggle'))
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ planner_no_chase_enforce: true })
    )
    fireEvent.click(screen.getByTestId('planner-atr-floor-cap-toggle'))
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ planner_atr_floor_cap_at_invalidation: true })
    )
    fireEvent.change(screen.getByTestId('planner-momentum-into-zone'), {
      target: { value: '1.5' },
    })
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ planner_momentum_into_zone_max_atr: 1.5 })
    )
  })
})
